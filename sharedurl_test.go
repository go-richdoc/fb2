// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package fb2

import (
	"encoding/base64"
	"strings"
	"testing"
	"unsafe"

	"github.com/go-richdoc/richdoc"
)

// fb2WithImage builds a book carrying one binary, referenced refs times, plus
// an optional second distinct binary.
func fb2WithImage(t testing.TB, kib, refs int, second bool) []byte {
	t.Helper()
	b64 := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("A", kib*1024)))
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` +
		`<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" ` +
		`xmlns:l="http://www.w3.org/1999/xlink">` +
		`<description><title-info><book-title>T</book-title></title-info></description>` +
		`<body><section>`)
	for i := 0; i < refs; i++ {
		b.WriteString(`<p><image l:href="#pic"/></p>`)
	}
	if second {
		b.WriteString(`<p><image l:href="#other"/></p>`)
	}
	b.WriteString(`</section></body><binary id="pic" content-type="image/png">` + b64 + `</binary>`)
	if second {
		b.WriteString(`<binary id="other" content-type="image/gif">` +
			base64.StdEncoding.EncodeToString([]byte("a different picture")) + `</binary>`)
	}
	b.WriteString(`</FictionBook>`)
	return []byte(b.String())
}

// imageURLs is every image URL in the document, in order.
func imageURLs(d *richdoc.Document) []string {
	var out []string
	var walk func([]richdoc.Block)
	walk = func(bs []richdoc.Block) {
		for _, b := range bs {
			switch v := b.(type) {
			case richdoc.Paragraph:
				for _, in := range v.Inlines {
					if img, ok := in.(richdoc.Image); ok {
						out = append(out, img.URL)
					}
				}
			case richdoc.BlockQuote:
				walk(v.Blocks)
			case richdoc.List:
				for _, it := range v.Items {
					walk(it.Blocks)
				}
			}
		}
	}
	walk(d.Blocks)
	return out
}

// TestOneBinaryIsOneCopy is a memory-exhaustion defect, and the assertion is
// on the BACKING ARRAY rather than on a byte count, because a sum of string
// lengths is not an allocation — which is the whole reason the defect was
// invisible.
//
// Every <image> used to build its own "data:...;base64,..." string, so the
// same picture was allocated once per REFERENCE. Measured with
// runtime.MemStats on a file carrying one 512 KiB image named 2000 times:
// 757 KB of source held 1.41 GB of heap, in 2.3 s. Sharing one string per
// binary brings the same document to 360 KB and 15 ms.
//
// A Go string assignment shares its backing array, so comparing
// unsafe.StringData is what actually says "one copy" — an equality check
// would pass just as well on a thousand identical copies.
func TestOneBinaryIsOneCopy(t *testing.T) {
	doc, err := Parse(fb2WithImage(t, 64, 50, false), Limits{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	urls := imageURLs(doc)
	if len(urls) != 50 {
		t.Fatalf("got %d images, want 50", len(urls))
	}
	first := unsafe.StringData(urls[0])
	for i, u := range urls {
		if !strings.HasPrefix(u, "data:image/png;base64,") {
			t.Fatalf("image %d is not an inlined data URL: %.40q", i, u)
		}
		if unsafe.StringData(u) != first {
			t.Errorf("image %d holds its OWN copy of the picture; %d references means %d copies",
				i, len(urls), len(urls))
			break
		}
	}
}

// TestTwoBinariesStayTwo is the control: the cache is keyed by the binary's
// id, and a cache that collapsed distinct pictures into one would pass the
// test above perfectly.
func TestTwoBinariesStayTwo(t *testing.T) {
	doc, err := Parse(fb2WithImage(t, 1, 3, true), Limits{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	urls := imageURLs(doc)
	if len(urls) != 4 {
		t.Fatalf("got %d images, want 4", len(urls))
	}
	if urls[0] == urls[3] {
		t.Error("two distinct binaries came back as the same data URL")
	}
	if !strings.HasPrefix(urls[3], "data:image/gif;base64,") {
		t.Errorf("the second binary lost its own content type: %.40q", urls[3])
	}
	// And the three references to the FIRST one are still one copy.
	if unsafe.StringData(urls[0]) != unsafe.StringData(urls[1]) {
		t.Error("the repeated binary is not shared")
	}
}

// TestABinaryOverTheLimitIsStillNotInlined keeps the per-binary ceiling
// honest: the cache must not become a way past it.
func TestABinaryOverTheLimitIsStillNotInlined(t *testing.T) {
	doc, err := Parse(fb2WithImage(t, 64, 2, false), Limits{MaxBinary: 1024})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for i, u := range imageURLs(doc) {
		if strings.HasPrefix(u, "data:") {
			t.Errorf("image %d was inlined past the MaxBinary of 1024: %.40q", i, u)
		}
	}
}
