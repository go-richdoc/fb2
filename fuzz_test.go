// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package fb2

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"github.com/go-richdoc/richdoc"
)

// FuzzParse points this reader at bytes nobody wrote. An FB2 is one XML file
// carrying a whole book AND its pictures as base64, which makes it the member
// of this family where a small file reaches for the most memory.
//
// Five properties:
//
//   - Parse does not panic.
//   - its two results are exclusive: a document OR an error, never neither.
//   - Parse is DETERMINISTIC. The reader keeps binaries, and now their data
//     URLs, in maps; a single range over either reaching the output would make
//     a book depend on nothing the file said.
//   - the DEPTH limit holds. It is checked in a token pass of its own before
//     the document is built, because the stack a deep file overflows belongs
//     to encoding/xml rather than to this package.
//   - ⛔ THE DOCUMENT IS LINEAR IN THE FILE. This is the one that earns the
//     target, and it is the invariant a fix in the same change established:
//     every <image> used to build its own data URL, so one picture named a
//     thousand times was allocated a thousand times — 757 KB of source held
//     1.41 GB of heap. One shared string per binary brings that to 360 KB,
//     and the assertion here is that the DISTINCT image payloads add up to no
//     more than the file could possibly carry. Base64 is 4/3 of its bytes, so
//     twice the source is generous slack.
//
// The distinct payloads are counted by BACKING ARRAY (unsafe.StringData), not
// by value: counting by value would pass just as well on a thousand identical
// copies, which is precisely the defect.
func FuzzParse(f *testing.F) {
	pic := base64.StdEncoding.EncodeToString([]byte("a little picture"))
	head := `<?xml version="1.0" encoding="utf-8"?>` +
		`<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" ` +
		`xmlns:l="http://www.w3.org/1999/xlink">`
	seeds := []string{
		head + `<description><title-info><book-title>T</book-title><author>` +
			`<first-name>A</first-name><last-name>L</last-name></author></title-info></description>` +
			`<body><section><title><p>One</p></title><p>text</p></section></body></FictionBook>`,
		// The shape the fix is about: one binary, several references.
		head + `<body><section><p><image l:href="#pic"/></p><p><image l:href="#pic"/></p>` +
			`<p><image l:href="#pic"/></p></section></body>` +
			`<binary id="pic" content-type="image/png">` + pic + `</binary></FictionBook>`,
		// A reference to a binary that is not there, and one with no href.
		head + `<body><section><p><image l:href="#missing"/><image/></p></section></body></FictionBook>`,
		// Nesting, for the depth pass, and the notes body with its own rule.
		head + `<body><section>` + strings.Repeat(`<section><title><p>x</p></title>`, 20) +
			`<p>deep</p>` + strings.Repeat(`</section>`, 20) + `</section></body></FictionBook>`,
		head + `<body name="notes"><section id="n1"><p>a note</p></section></body></FictionBook>`,
		// Inline markup, a table, a poem, an epigraph.
		head + `<body><section><p><strong>s</strong><emphasis>e</emphasis><style name="x">y</style>` +
			`<a l:href="#n1">ref</a><code>c</code></p>` +
			`<table><tr><th>h</th></tr><tr><td>a</td></tr></table>` +
			`<poem><stanza><v>one</v><v>two</v></stanza></poem>` +
			`<epigraph><p>q</p></epigraph></section></body></FictionBook>`,
		// Malformed, which must be an error rather than a panic.
		head + `<body><section><p>unclosed`,
		`<FictionBook>`,
		`not xml at all`,
		"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		lim := Limits{MaxDepth: 24, MaxBinary: 1 << 16}
		doc, err := Parse(src, lim)
		if err != nil {
			if doc != nil {
				t.Fatalf("both a document and an error: %v", err)
			}
			return
		}
		if doc == nil {
			t.Fatal("neither a document nor an error")
		}

		again, err2 := Parse(src, lim)
		if err2 != nil || again == nil {
			t.Fatalf("the second Parse disagreed with the first: doc=%v err=%v", again != nil, err2)
		}
		if first, second := fmt.Sprintf("%#v", doc), fmt.Sprintf("%#v", again); first != second {
			t.Fatalf("two parses of the same bytes differ\nfirst:\n%.400s\nsecond:\n%.400s", first, second)
		}

		// The document is linear in the file: distinct payloads only, counted
		// by backing array.
		seen := map[*byte]bool{}
		var payload int
		for _, u := range fuzzImageURLs(doc) {
			if !strings.HasPrefix(u, "data:") {
				continue // a href the file did not carry a binary for
			}
			p := unsafe.StringData(u)
			if seen[p] {
				continue
			}
			seen[p] = true
			payload += len(u)
		}
		if ceiling := 2*len(src) + 4096; payload > ceiling {
			t.Errorf("the document carries %d bytes of distinct image payload from %d bytes of source, over the %d allowed",
				payload, len(src), ceiling)
		}
	})
}

func fuzzImageURLs(d *richdoc.Document) []string {
	var out []string
	var blocks func([]richdoc.Block)
	blocks = func(bs []richdoc.Block) {
		for _, b := range bs {
			switch v := b.(type) {
			case richdoc.Paragraph:
				out = append(out, fuzzURLsIn(v.Inlines)...)
			case richdoc.Heading:
				out = append(out, fuzzURLsIn(v.Inlines)...)
			case richdoc.BlockQuote:
				blocks(v.Blocks)
			case richdoc.List:
				for _, it := range v.Items {
					blocks(it.Blocks)
				}
			case richdoc.Table:
				for _, row := range append([][]richdoc.Cell{v.Header}, v.Rows...) {
					for _, c := range row {
						out = append(out, fuzzURLsIn(c.Inlines)...)
						blocks(c.Blocks)
					}
				}
			}
		}
	}
	blocks(d.Blocks)
	return out
}

func fuzzURLsIn(xs []richdoc.Inline) []string {
	var out []string
	for _, x := range xs {
		switch v := x.(type) {
		case richdoc.Image:
			out = append(out, v.URL)
		case richdoc.Link:
			out = append(out, fuzzURLsIn(v.Inlines)...)
		case richdoc.Emph:
			out = append(out, fuzzURLsIn(v.Inlines)...)
		case richdoc.Strong:
			out = append(out, fuzzURLsIn(v.Inlines)...)
		}
	}
	return out
}
