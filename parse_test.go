package fb2

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// book wraps content in the smallest FictionBook that is one.
func book(inner string) []byte {
	return []byte(`<?xml version="1.0" encoding="utf-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0"
             xmlns:l="http://www.w3.org/1999/xlink">` + inner + `</FictionBook>`)
}

// shape describes a document as a line per block, so a test says what it
// expects in one readable string rather than in a tree of literals.
func shape(blocks []richdoc.Block) string {
	var out []string
	for _, b := range blocks {
		switch n := b.(type) {
		case richdoc.Heading:
			out = append(out, fmt.Sprintf("h%d %q", n.Level, inlineText(n.Inlines)))
		case richdoc.Paragraph:
			out = append(out, fmt.Sprintf("p %q", inlineText(n.Inlines)))
		case richdoc.BlockQuote:
			out = append(out, "quote{ "+strings.ReplaceAll(shape(n.Blocks), "\n", " ; ")+" }")
		case richdoc.Table:
			var rows []string
			if len(n.Header) > 0 {
				rows = append(rows, row(n.Header))
			}
			for _, r := range n.Rows {
				rows = append(rows, row(r))
			}
			out = append(out, "table "+strings.Join(rows, " / "))
		default:
			out = append(out, fmt.Sprintf("?%T", b))
		}
	}
	return strings.Join(out, "\n")
}

func row(cells []richdoc.Cell) string {
	var out []string
	for _, c := range cells {
		out = append(out, inlineText(c.Inlines))
	}
	return "[" + strings.Join(out, "|") + "]"
}

func parse(t *testing.T, inner string) *richdoc.Document {
	t.Helper()
	d, err := Parse(book(inner), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAChapterWithAHeadingAndProse(t *testing.T) {
	d := parse(t, `<body>
	  <title><p>The Book</p></title>
	  <section>
	    <title><p>Chapter One</p></title>
	    <p>It was a dark and stormy night.</p>
	    <empty-line/>
	    <p>The rain fell in torrents.</p>
	  </section>
	</body>`)
	want := strings.Join([]string{
		`h1 "The Book"`,
		`h1 "Chapter One"`,
		`p "It was a dark and stormy night."`,
		`p "The rain fell in torrents."`,
	}, "\n")
	if got := shape(d.Blocks); got != want {
		t.Errorf("read\n%s\nwant\n%s", got, want)
	}
}

// ⛔ The heading LEVEL comes from the nesting, because that is the only thing
// in an FB2 that says one: there is no <h2>, only a <section> inside a
// <section>.
func TestTheLevelComesFromTheNesting(t *testing.T) {
	d := parse(t, `<body>
	  <section><title><p>One</p></title>
	    <p>a</p>
	    <section><title><p>Two</p></title>
	      <p>b</p>
	      <section><title><p>Three</p></title><p>c</p></section>
	    </section>
	  </section>
	</body>`)
	got := shape(d.Blocks)
	for _, want := range []string{`h1 "One"`, `h2 "Two"`, `h3 "Three"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is missing:\n%s", want, got)
		}
	}
}

// Deeper than the levels richdoc's writers have: the last one, not none.
// Text somebody made a heading is a heading whatever depth they gave it.
func TestNestedDeeperThanThereAreLevels(t *testing.T) {
	inner := "<body>"
	for i := 1; i <= 9; i++ {
		inner += fmt.Sprintf("<section><title><p>L%d</p></title><p>t%d</p>", i, i)
	}
	inner += strings.Repeat("</section>", 9) + "</body>"
	got := shape(parse(t, inner).Blocks)
	if !strings.Contains(got, `h6 "L9"`) {
		t.Errorf("the ninth level is not the sixth:\n%s", got)
	}
	if strings.Contains(got, "h7 ") || strings.Contains(got, "h9 ") {
		t.Errorf("a level past the sixth was written:\n%s", got)
	}
}

// ⛔ A title broken over several <p> is ONE heading. FB2 does that for the
// line breaks, and a document with three headings where a reader sees one
// navigates wrongly.
func TestATitleOverSeveralParagraphs(t *testing.T) {
	d := parse(t, `<body><section>
	  <title><p>A Long Title</p><p>And Its Second Line</p></title>
	  <p>text</p>
	</section></body>`)
	want := "h1 \"A Long Title And Its Second Line\"\np \"text\""
	if got := shape(d.Blocks); got != want {
		t.Errorf("read\n%s\nwant\n%s", got, want)
	}
}

// A section with no title is common, and is not an empty heading.
func TestASectionWithNoTitle(t *testing.T) {
	d := parse(t, `<body><section><p>just text</p></section></body>`)
	if got := shape(d.Blocks); got != `p "just text"` {
		t.Errorf("read %s", got)
	}
}

// ⛔ A stanza is one paragraph whose verses are separated by LINE BREAKS.
// That is what a stanza is: lines that belong together and are not
// paragraphs, and a paragraph each would lose the grouping.
func TestAPoem(t *testing.T) {
	d := parse(t, `<body><section>
	  <poem>
	    <title><p>A Verse</p></title>
	    <stanza><v>Line one</v><v>Line two</v></stanza>
	    <stanza><v>Line three</v></stanza>
	  </poem>
	</section></body>`)
	blocks := d.Blocks
	if len(blocks) != 3 {
		t.Fatalf("%d blocks, want a heading and two stanzas:\n%s", len(blocks), shape(blocks))
	}
	first, ok := blocks[1].(richdoc.Paragraph)
	if !ok {
		t.Fatalf("the stanza is a %T", blocks[1])
	}
	breaks := 0
	for _, in := range first.Inlines {
		if _, ok := in.(richdoc.LineBreak); ok {
			breaks++
		}
	}
	if breaks != 1 {
		t.Errorf("%d line breaks in a two-verse stanza:\n%#v", breaks, first.Inlines)
	}
}

func TestQuotationsAndEpigraphs(t *testing.T) {
	d := parse(t, `<body><section>
	  <epigraph><p>An epigraph.</p></epigraph>
	  <cite><p>A quotation.</p></cite>
	  <p>Prose.</p>
	</section></body>`)
	got := shape(d.Blocks)
	for _, want := range []string{`quote{ p "An epigraph." }`, `quote{ p "A quotation." }`, `p "Prose."`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is missing:\n%s", want, got)
		}
	}
}

// ⛔ THE ORDER SURVIVES. A section is prose, a quotation, more prose and a
// table; decoding the children as a field per element name would hand back
// tidy slices that say nothing about which came first.
func TestTheOrderOfMixedChildren(t *testing.T) {
	d := parse(t, `<body><section>
	  <p>first</p>
	  <cite><p>quoted</p></cite>
	  <p>second</p>
	  <subtitle>a subtitle</subtitle>
	  <p>third</p>
	</section></body>`)
	want := strings.Join([]string{
		`p "first"`,
		`quote{ p "quoted" }`,
		`p "second"`,
		`h2 "a subtitle"`,
		`p "third"`,
	}, "\n")
	if got := shape(d.Blocks); got != want {
		t.Errorf("read\n%s\nwant\n%s", got, want)
	}
}

// ⛔ Mixed content: "a <strong>bold</strong> word" is three things IN A ROW,
// and any typed struct would separate the text from the markup and lose
// where each went.
func TestMarkupInsideAParagraph(t *testing.T) {
	d := parse(t, `<body><section><p>a <strong>bold</strong> and <emphasis>italic</emphasis> word</p></section></body>`)
	p, ok := d.Blocks[0].(richdoc.Paragraph)
	if !ok {
		t.Fatalf("the block is a %T", d.Blocks[0])
	}
	if got := inlineText(p.Inlines); got != "a bold and italic word" {
		t.Errorf("the text reads %q", got)
	}
	var strong, emph bool
	for _, in := range p.Inlines {
		switch in.(type) {
		case richdoc.Strong:
			strong = true
		case richdoc.Emph:
			emph = true
		}
	}
	if !strong || !emph {
		t.Errorf("strong=%v emph=%v: %#v", strong, emph, p.Inlines)
	}
}

func TestLinksCodeAndStrikethrough(t *testing.T) {
	d := parse(t, `<body><section><p>see <a l:href="http://example.invalid/x">there</a>, `+
		`<code>Parse()</code>, <strikethrough>gone</strikethrough></p></section></body>`)
	p := d.Blocks[0].(richdoc.Paragraph)
	var link *richdoc.Link
	var code, struck bool
	for _, in := range p.Inlines {
		switch n := in.(type) {
		case richdoc.Link:
			link = &n
		case richdoc.Code:
			code = n.Value == "Parse()"
		case richdoc.Strikethrough:
			struck = true
		}
	}
	if link == nil || link.URL != "http://example.invalid/x" {
		t.Errorf("the link is %#v", link)
	}
	if !code || !struck {
		t.Errorf("code=%v struck=%v: %#v", code, struck, p.Inlines)
	}
}

// ⛔ An element this does not know keeps its TEXT. <sub> and <sup> have no
// richdoc equivalent, and losing the 2 of "H2O" to say so would be the worse
// trade.
func TestAnElementWithNoRichdocEquivalent(t *testing.T) {
	d := parse(t, `<body><section><p>H<sub>2</sub>O and x<sup>2</sup>, <style name="x">styled</style></p></section></body>`)
	p := d.Blocks[0].(richdoc.Paragraph)
	if got := inlineText(p.Inlines); got != "H2O and x2, styled" {
		t.Errorf("the text reads %q", got)
	}
}

func TestATable(t *testing.T) {
	d := parse(t, `<body><section><table>
	  <tr><th>Name</th><th>Age</th></tr>
	  <tr><td>Ada</td><td>36</td></tr>
	</table></section></body>`)
	if got, want := shape(d.Blocks), "table [Name|Age] / [Ada|36]"; got != want {
		t.Errorf("read\n%s\nwant\n%s", got, want)
	}
	table := d.Blocks[0].(richdoc.Table)
	if len(table.Header) != 2 || len(table.Rows) != 1 {
		t.Errorf("%d header cells and %d rows, want 2 and 1", len(table.Header), len(table.Rows))
	}
}

// ⛔ The first row is the header only when its cells say they are. FB2 marks
// them <th>, so nothing has to be guessed — unlike a PDF, where a grid found
// by looking at alignment cannot know.
func TestATableWhoseFirstRowIsNotAHeader(t *testing.T) {
	d := parse(t, `<body><section><table>
	  <tr><td>a</td><td>b</td></tr>
	  <tr><td>c</td><td>d</td></tr>
	</table></section></body>`)
	// ⛔ Header is asserted DIRECTLY and not through the printed shape: a
	// header plus one row and two rows print the same, so the shape cannot
	// tell a table that invented a header from one that did not.
	table, ok := d.Blocks[0].(richdoc.Table)
	if !ok {
		t.Fatalf("the block is a %T", d.Blocks[0])
	}
	if len(table.Header) != 0 {
		t.Errorf("a row of <td> became a header: %v", row(table.Header))
	}
	if len(table.Rows) != 2 {
		t.Errorf("%d rows, want 2: %s", len(table.Rows), shape(d.Blocks))
	}
}

// ⛔ An image's bytes are in the SAME FILE, so they become a data URL: a
// reader handing back "#cover.jpg" would be handing back a name nothing can
// resolve once the file is closed.
func TestAnImageBecomesItsBytes(t *testing.T) {
	d := parse(t, `<body><section><p>before</p><image l:href="#cover.jpg" alt="the cover"/></section></body>
	  <binary id="cover.jpg" content-type="image/jpeg">/9j/4AAQSkZJRg==</binary>`)
	var img *richdoc.Image
	for _, b := range d.Blocks {
		p, ok := b.(richdoc.Paragraph)
		if !ok {
			continue
		}
		for _, in := range p.Inlines {
			if i, ok := in.(richdoc.Image); ok {
				img = &i
			}
		}
	}
	if img == nil {
		t.Fatalf("no image came back:\n%s", shape(d.Blocks))
	}
	if img.URL != "data:image/jpeg;base64,/9j/4AAQSkZJRg==" {
		t.Errorf("the image is %q", img.URL)
	}
	if img.Alt != "the cover" {
		t.Errorf("the alt text is %q", img.Alt)
	}
}

// A binary the file names and does not carry: the href and the alt text are
// still worth something.
func TestAnImageWhoseBytesAreNotThere(t *testing.T) {
	d := parse(t, `<body><section><image l:href="#gone.jpg" alt="missing"/></section></body>`)
	got := shape(d.Blocks)
	if got == "" {
		t.Fatal("the image was dropped entirely")
	}
}

// ⛔ A binary past the limit is LEFT OUT rather than truncated: half a JPEG
// is not a smaller picture.
func TestABinaryPastTheLimit(t *testing.T) {
	big := strings.Repeat("A", 4096)
	d, err := Parse(book(`<body><section><image l:href="#big.jpg"/></section></body>
	  <binary id="big.jpg" content-type="image/jpeg">`+big+`</binary>`), Limits{MaxBinary: 16})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range d.Blocks {
		p, ok := b.(richdoc.Paragraph)
		if !ok {
			continue
		}
		for _, in := range p.Inlines {
			if i, ok := in.(richdoc.Image); ok && strings.HasPrefix(i.URL, "data:") {
				t.Errorf("a binary past the limit was inlined: %.40q", i.URL)
			}
		}
	}
}

// What the file says about itself, in the keys richdoc's other readers use.
func TestWhatTheFileSaysAboutItself(t *testing.T) {
	d := parse(t, `<description><title-info>
	    <book-title>War and Peace</book-title>
	    <author><first-name>Lev</first-name><last-name>Tolstoy</last-name></author>
	    <author><nickname>Anon</nickname></author>
	    <lang>ru</lang><date>1869</date>
	  </title-info></description>
	  <body><section><p>text</p></section></body>`)
	want := map[string]string{
		"title": "War and Peace", "author": "Lev Tolstoy, Anon",
		"language": "ru", "date": "1869",
	}
	for k, v := range want {
		if d.Meta[k] != v {
			t.Errorf("%s is %q, want %q", k, d.Meta[k], v)
		}
	}
}

// A document that says nothing about itself has NO metadata, not an empty
// map: a caller testing for a title should not find a map with nothing in it
// and have to look twice.
func TestAFileThatSaysNothingAboutItself(t *testing.T) {
	d := parse(t, `<body><section><p>text</p></section></body>`)
	if d.Meta != nil {
		t.Errorf("the metadata is %#v, want none at all", d.Meta)
	}
}

// ⛔ The notes body comes out as a section of its own, named, rather than
// woven into the text: FB2 links to a note by id, and resolving those would
// mean deciding where in a paragraph a reader wants it.
func TestTheNotesBody(t *testing.T) {
	d := parse(t, `<body><section><p>text</p></section></body>
	  <body name="notes"><section><p>A note.</p></section></body>`)
	got := shape(d.Blocks)
	if !strings.Contains(got, `h1 "notes"`) {
		t.Errorf("the notes body is not named:\n%s", got)
	}
	if !strings.Contains(got, `p "A note."`) {
		t.Errorf("the note itself is missing:\n%s", got)
	}
}

// ⛔ Namespaces are not matched, and that is measured against the format
// rather than lazy: producers write FB2's namespace, mis-write it, and leave
// it out, and a reader that insisted would refuse files every other reader
// opens.
func TestAFileWithNoNamespaceAtAll(t *testing.T) {
	d, err := Parse([]byte(`<FictionBook><body><section><p>plain</p></section></body></FictionBook>`), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := shape(d.Blocks); got != `p "plain"` {
		t.Errorf("read %s", got)
	}
}

func TestWhatItRefuses(t *testing.T) {
	if _, err := Parse([]byte("<FictionBook><body><p>unclosed"), Limits{}); err == nil {
		t.Error("a file that does not parse was accepted")
	}
	// ⛔ The depth guard is a token pass of its own, BEFORE anything builds a
	// tree: the stack a deep file overflows belongs to encoding/xml.
	deep := "<FictionBook>" + strings.Repeat("<section>", 40) + strings.Repeat("</section>", 40) + "</FictionBook>"
	_, err := Parse([]byte(deep), Limits{MaxDepth: 10})
	if err == nil || !strings.Contains(err.Error(), "deeper than 10") {
		t.Errorf("a file 40 deep at a limit of 10 said %v", err)
	}
	// And the same file is fine at a limit that allows it.
	if _, err := Parse([]byte(deep), Limits{}); err != nil {
		t.Errorf("the default limit refused a file 40 deep: %v", err)
	}
}

// ⛔ The depth pass returns nil on a SYNTAX error rather than reporting it.
// It answers one question, and a file that does not parse is Parse's to
// refuse — in one place, with one message. Both complaints at once made
// every malformed file produce whichever ran first.
func TestAFileThatIsBothTooDeepAndBroken(t *testing.T) {
	broken := "<FictionBook>" + strings.Repeat("<section>", 40)
	_, err := Parse([]byte(broken), Limits{MaxDepth: 10})
	if err == nil {
		t.Fatal("it was accepted")
	}
	// Either complaint is defensible; what matters is that there is ONE.
	if strings.Count(err.Error(), "fb2:") != 1 {
		t.Errorf("it said %q", err)
	}
}

func TestAnEmptyBook(t *testing.T) {
	d, err := Parse(book(""), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Blocks) != 0 {
		t.Errorf("%d blocks from an empty book: %s", len(d.Blocks), shape(d.Blocks))
	}
}

// Nested markup: the inner run has to end at its own close and hand control
// back, or the outer one swallows the rest of the paragraph.
func TestMarkupInsideMarkup(t *testing.T) {
	d := parse(t, `<body><section><p>a <strong>bold <emphasis>and italic</emphasis></strong> tail</p></section></body>`)
	p := d.Blocks[0].(richdoc.Paragraph)
	if got := inlineText(p.Inlines); got != "a bold and italic tail" {
		t.Errorf("the text reads %q", got)
	}
	strong, ok := p.Inlines[1].(richdoc.Strong)
	if !ok {
		t.Fatalf("the second inline is a %T", p.Inlines[1])
	}
	var nested bool
	for _, in := range strong.Inlines {
		if _, ok := in.(richdoc.Emph); ok {
			nested = true
		}
	}
	if !nested {
		t.Errorf("the emphasis inside the strong was lost: %#v", strong.Inlines)
	}
}

// A comment inside a paragraph is neither text nor markup, and skipping it
// must not skip what follows.
func TestACommentInsideAParagraph(t *testing.T) {
	d := parse(t, `<body><section><p>before<!-- a note -->after</p></section></body>`)
	if got := shape(d.Blocks); got != `p "beforeafter"` {
		t.Errorf("read %s", got)
	}
}

// An image inside a paragraph rather than beside one.
func TestAnImageInsideAParagraph(t *testing.T) {
	d := parse(t, `<body><section><p>see <image l:href="#x.png"/> there</p></section></body>
	  <binary id="x.png" content-type="image/png">iVBOR</binary>`)
	p := d.Blocks[0].(richdoc.Paragraph)
	var found bool
	for _, in := range p.Inlines {
		if img, ok := in.(richdoc.Image); ok {
			found = strings.HasPrefix(img.URL, "data:image/png;base64,")
		}
	}
	if !found {
		t.Errorf("the inline image is missing or unresolved: %#v", p.Inlines)
	}
}

// An <image> with no href says nothing and is left out, rather than becoming
// an image of nowhere.
func TestAnImageWithNoHref(t *testing.T) {
	d := parse(t, `<body><section><p>text <image/></p><image/></section></body>`)
	if got := shape(d.Blocks); got != `p "text "` {
		t.Errorf("read %s", got)
	}
}

// A binary with no content type still resolves: the bytes are what matter,
// and a reader can sniff them.
func TestABinaryWithNoContentType(t *testing.T) {
	d := parse(t, `<body><section><image l:href="#x"/></section></body>
	  <binary id="x">AAAA</binary>`)
	p := d.Blocks[0].(richdoc.Paragraph)
	img := p.Inlines[0].(richdoc.Image)
	if img.URL != "data:application/octet-stream;base64,AAAA" {
		t.Errorf("the image is %q", img.URL)
	}
}

// A stanza outside a poem, which some producers write.
func TestAStanzaOutsideAPoem(t *testing.T) {
	d := parse(t, `<body><section><stanza><v>one</v><v>two</v></stanza></section></body>`)
	if len(d.Blocks) != 1 {
		t.Fatalf("%d blocks:\n%s", len(d.Blocks), shape(d.Blocks))
	}
	if got := inlineText(d.Blocks[0].(richdoc.Paragraph).Inlines); got != "onetwo" {
		t.Errorf("the stanza reads %q", got)
	}
}

// An empty stanza and an empty table say nothing, and nothing is what they
// become: an empty paragraph and a table with no rows are both worse.
func TestEmptyStanzasAndTables(t *testing.T) {
	d := parse(t, `<body><section><stanza/><table/><table><tr/></table><p>text</p></section></body>`)
	if got := shape(d.Blocks); got != `p "text"` {
		t.Errorf("read %s", got)
	}
}

// A <poem> whose children are neither a title nor a stanza.
func TestAPoemWithOtherChildren(t *testing.T) {
	d := parse(t, `<body><section><poem><epigraph><p>before</p></epigraph>
	  <stanza><v>verse</v></stanza><text-author>Someone</text-author></poem></section></body>`)
	got := shape(d.Blocks)
	for _, want := range []string{`quote{ p "before" }`, `p "verse"`, "Someone"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is missing:\n%s", want, got)
		}
	}
}

// A <title> whose text is written straight into it rather than in a <p>,
// which producers do.
func TestATitleWithNoParagraphInIt(t *testing.T) {
	d := parse(t, `<body><section><title>Straight Text</title><p>x</p></section></body>`)
	if got := shape(d.Blocks); !strings.Contains(got, `h1 "Straight Text"`) {
		t.Errorf("read\n%s", got)
	}
}

// A <title> that says nothing is not an empty heading.
func TestATitleThatSaysNothing(t *testing.T) {
	d := parse(t, `<body><section><title><p>   </p></title><p>text</p></section></body>`)
	if got := shape(d.Blocks); got != `p "text"` {
		t.Errorf("read %s", got)
	}
}

// ⛔ An element this does not know keeps its TEXT. FB2 has extensions, and a
// reader losing them silently is worse than one rendering them plainly.
func TestAnElementThisDoesNotKnow(t *testing.T) {
	d := parse(t, `<body><section><nonsense>still words</nonsense><p>text</p></section></body>`)
	got := shape(d.Blocks)
	if !strings.Contains(got, `p "still words"`) {
		t.Errorf("an unknown element was dropped:\n%s", got)
	}
}

// A <binary> written inside a body rather than beside the bodies: the base64
// is not prose, and dumping it into the text is what the case exists to stop.
func TestABinaryInsideABody(t *testing.T) {
	d := parse(t, `<body><binary id="x" content-type="image/png">AAAABBBBCCCC</binary><p>text</p></body>`)
	if got := shape(d.Blocks); got != `p "text"` {
		t.Errorf("read %s", got)
	}
}

// ⛔ run stops at the end of what it was given. Through Parse it never has
// to — encoding/xml refuses a document whose elements do not close — but it
// is the kind of defence whose absence is a loop, so it is driven directly.
func TestRunStopsAtTheEndOfWhatItWasGiven(t *testing.T) {
	tokens := []xml.Token{xml.CharData("a"), xml.StartElement{Name: xml.Name{Local: "b"}}}
	out, i := run(tokens, 0)
	if i != len(tokens) {
		t.Errorf("it stopped at %d of %d", i, len(tokens))
	}
	if len(out) != 2 {
		t.Errorf("it kept %d tokens of %d", len(out), len(tokens))
	}
}

// A stanza, a table row and a table cell with something else inside them:
// each skips what it does not recognise rather than counting it as a verse,
// a row or a cell.
func TestStraysInsideAStanzaAndATable(t *testing.T) {
	d := parse(t, `<body><section>
	  <stanza><title><p>ignored</p></title><v>kept</v></stanza>
	  <table><p>stray</p><tr><td>a</td><p>stray</p></tr></table>
	</section></body>`)
	got := shape(d.Blocks)
	if !strings.Contains(got, `p "kept"`) {
		t.Errorf("the verse is missing:\n%s", got)
	}
	if strings.Contains(got, "ignored") {
		t.Errorf("a title inside a stanza became a verse:\n%s", got)
	}
	if !strings.Contains(got, "table [a]") {
		t.Errorf("the row is wrong:\n%s", got)
	}
	if strings.Contains(got, "stray") {
		t.Errorf("a stray element became a row or a cell:\n%s", got)
	}
}

// An empty <code/> has nothing in it, and asking richdoc what nothing says
// is how that is written.
func TestAnEmptyCodeSpan(t *testing.T) {
	d := parse(t, `<body><section><p>a <code/> b</p></section></body>`)
	p := d.Blocks[0].(richdoc.Paragraph)
	var found bool
	for _, in := range p.Inlines {
		if c, ok := in.(richdoc.Code); ok {
			found = c.Value == ""
		}
	}
	if !found {
		t.Errorf("the empty code span is %#v", p.Inlines)
	}
}

// ⛔ inlineRun advances past a token it does not understand. Through Parse it
// never sees one — element.UnmarshalXML keeps only text, starts and ends —
// but the difference between skipping and not advancing is a loop that never
// finishes, so it is driven directly.
func TestInlineRunAdvancesPastWhatItDoesNotUnderstand(t *testing.T) {
	p := &parser{limits: Limits{}.orDefaults()}
	out, i := p.inlineRun([]xml.Token{
		xml.Comment("ignored"),
		xml.CharData("kept"),
		xml.ProcInst{Target: "x"},
	}, 0)
	if i != 3 {
		t.Errorf("it stopped at %d of 3", i)
	}
	if got := inlineText(out); got != "kept" {
		t.Errorf("it read %q", got)
	}
}

// ⛔ Only the FIRST row can be the header. A table that repeats its headings
// partway down — which a long one does — has one header and the repeat is a
// row, or the last repeat silently replaces the first and the rows above it
// lose their heading.
func TestASecondRowOfHeadings(t *testing.T) {
	d := parse(t, `<body><section><table>
	  <tr><th>Name</th><th>Age</th></tr>
	  <tr><td>Ada</td><td>36</td></tr>
	  <tr><th>Name</th><th>Age</th></tr>
	  <tr><td>Bob</td><td>41</td></tr>
	</table></section></body>`)
	table := d.Blocks[0].(richdoc.Table)
	if got := row(table.Header); got != "[Name|Age]" {
		t.Errorf("the header is %s", got)
	}
	if len(table.Rows) != 3 {
		t.Errorf("%d rows, want 3 — the repeated headings are a row:\n%s",
			len(table.Rows), shape(d.Blocks))
	}
	if got := row(table.Rows[0]); got != "[Ada|36]" {
		t.Errorf("the first row is %s", got)
	}
}
