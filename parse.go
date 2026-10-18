package fb2

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/go-richdoc/richdoc"
)

// Limits bound what a file may ask for. A zero field means the default, so
// Limits{} is the default everywhere.
type Limits struct {
	// MaxDepth is how deeply elements may nest.
	//
	// ⛔ The stack a deep file overflows belongs to encoding/xml and not to
	// this package, so this is checked in a token pass of its own BEFORE the
	// document is built — a guard placed where our own code recurses never
	// runs. go-odf/odf learned that with a 115 MiB allocation inside the
	// decoder while its own counter stood at two.
	MaxDepth int

	// MaxBinary is the largest a single <binary> may be once decoded, in
	// bytes. An FB2 carries its images as base64 in the same file, so a
	// small file can still name a large picture.
	MaxBinary int
}

const (
	defaultMaxDepth  = 256
	defaultMaxBinary = 64 << 20

	// headingLevels is as many as richdoc's writers have. A section nested
	// deeper than this gets the last level rather than none: text somebody
	// made a heading is a heading whatever depth they gave it.
	headingLevels = 6
)

func (l Limits) orDefaults() Limits {
	if l.MaxDepth <= 0 {
		l.MaxDepth = defaultMaxDepth
	}
	if l.MaxBinary <= 0 {
		l.MaxBinary = defaultMaxBinary
	}
	return l
}

// Parse reads a FictionBook into a document.
func Parse(src []byte, lim Limits) (*richdoc.Document, error) {
	lim = lim.orDefaults()
	if err := checkDepth(src, lim.MaxDepth); err != nil {
		return nil, err
	}
	var bk fictionBook
	if err := xml.Unmarshal(src, &bk); err != nil {
		return nil, fmt.Errorf("fb2: %w", err)
	}
	p := &parser{limits: lim, binaries: map[string]binary{}}
	for _, b := range bk.Binaries {
		p.binaries[b.ID] = b
	}
	out := &richdoc.Document{Meta: bk.meta()}
	for _, body := range bk.Bodies {
		out.Blocks = append(out.Blocks, p.body(body)...)
	}
	return out, nil
}

// checkDepth is a token pass that refuses a file nested deeper than the
// limit, before anything builds a tree out of it.
//
// ⛔ It returns nil on a SYNTAX error rather than reporting it: this pass
// answers one question — is it too deep — and a file that does not parse is
// [Parse]'s to refuse, in one place.
//
// No test can tell the two apart, and that is worth saying rather than
// leaving as an apparent hole: both routes wrap the same error from the same
// decoder, so a malformed file is refused with the same words either way.
// The rule is about where the responsibility lives.
func checkDepth(src []byte, max int) error {
	dec := xml.NewDecoder(bytes.NewReader(src))
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return nil
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
			if depth > max {
				return fmt.Errorf("fb2: nested deeper than %d elements", max)
			}
		case xml.EndElement:
			depth--
		}
	}
}

// a parser carries what the whole file needs: its limits, its images, and ONE
// data URL per image.
type parser struct {
	limits   Limits
	binaries map[string]binary
	// urls is the data URL built for each binary, kept so that a picture named
	// a thousand times costs one copy.
	//
	// ⛔ Without it, every <image> built its own "data:...;base64,..." string,
	// so the same picture was allocated once per REFERENCE. Measured on a file
	// carrying one 512 KiB image referenced 2000 times: 757 KB of source held
	// 1.41 GB of heap, and the growth is linear in the number of references,
	// so a 10 MB file reaches tens of gigabytes. The heap was read with
	// runtime.MemStats rather than inferred from string lengths, because a sum
	// of lengths is not an allocation.
	//
	// A Go string assignment shares its backing array, so one entry here is
	// shared by every reference to it: the SAME document now holds one copy.
	//
	// ⛔ And this is why no MaxTotalBinary follows, though go-richdoc/epub has
	// a MaxTotalBytes beside its per-entry ceiling. That reader inflates ZIP
	// entries, which can be far smaller compressed than they are inflated, so
	// its total cannot be deduced from the file's size. An FB2 carries its
	// images as base64 IN the file, so the sum of its distinct binaries is at
	// most four thirds of the source. Bounded by the input, once the repeats
	// are shared -- the difference between the siblings is in the format, not
	// in how carefully each was written.
	urls map[string]string
}

// body turns one <body> into blocks.
//
// ⛔ A second body named "notes" is the footnotes, and it comes out as a
// section of its own with that name as a heading rather than woven into the
// text: FB2 links to a note by id, and resolving those would mean deciding
// where in a paragraph a reader wants it.
func (p *parser) body(e element) []richdoc.Block {
	var out []richdoc.Block
	if name := e.attr("name"); name != "" {
		out = append(out, richdoc.Heading{Level: 1, Inlines: text(name)})
	}
	return append(out, p.blocks(e.children(), 1)...)
}

// blocks turns a run of elements into richdoc blocks, in the order they were
// written.
//
// ⛔ The ORDER is the reason everything goes through one list. A section is
// prose, a quotation, more prose and a table, and a field per element name
// would hand back tidy slices that say nothing about which came first.
func (p *parser) blocks(es []element, depth int) []richdoc.Block {
	var out []richdoc.Block
	for _, e := range es {
		switch e.Name.Local {
		case "title":
			if h := p.title(e, depth); h != nil {
				out = append(out, h)
			}
		case "subtitle":
			if s := strings.TrimSpace(e.text()); s != "" {
				out = append(out, richdoc.Heading{Level: level(depth + 1), Inlines: text(s)})
			}
		case "section":
			out = append(out, p.section(e, depth)...)
		case "p", "v":
			if in := p.inlines(e); len(in) > 0 {
				out = append(out, richdoc.Paragraph{Inlines: in})
			}
		case "empty-line":
			// Vertical space between paragraphs, and the break is already
			// there. richdoc has no word for it, and inventing one would
			// make every writer decide what to do with a blank nothing.
			//
			// The case is here to SAY that rather than to do it: an
			// <empty-line/> has no content, so the default below would drop
			// it too, and no test can tell the two apart.
		case "cite", "epigraph", "annotation":
			if blocks := p.blocks(e.children(), depth); len(blocks) > 0 {
				out = append(out, richdoc.BlockQuote{Blocks: blocks})
			}
		case "poem":
			out = append(out, p.poem(e, depth)...)
		case "stanza":
			out = append(out, p.stanza(e)...)
		case "table":
			if t, ok := p.table(e); ok {
				out = append(out, t)
			}
		case "image":
			if img, ok := p.image(e); ok {
				out = append(out, richdoc.Paragraph{Inlines: []richdoc.Inline{img}})
			}
		case "binary", "description":
			// Read elsewhere: the images by id, the metadata into Meta.
		default:
			// An element this does not know is still content: its text is
			// kept as a paragraph rather than dropped, because FB2 has
			// extensions and a reader losing them silently is worse than one
			// rendering them plainly.
			if in := p.inlines(e); len(in) > 0 {
				out = append(out, richdoc.Paragraph{Inlines: in})
			}
		}
	}
	return out
}

// section turns one <section> into blocks: its own title at this depth, and
// anything nested inside it one deeper.
func (p *parser) section(e element, depth int) []richdoc.Block {
	var out []richdoc.Block
	for _, c := range e.children() {
		if c.Name.Local == "section" {
			out = append(out, p.section(c, depth+1)...)
			continue
		}
		out = append(out, p.blocks([]element{c}, depth)...)
	}
	return out
}

// title is a heading made of a title's paragraphs, or nil where it says
// nothing: a section without a title is common and is not an empty heading.
//
// ⛔ A title broken over several <p> is ONE heading. FB2 does that for the
// line breaks, and a document with three headings where a reader sees one
// navigates wrongly.
func (p *parser) title(e element, depth int) richdoc.Block {
	var parts []string
	for _, c := range e.children() {
		if s := strings.TrimSpace(c.text()); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		if s := strings.TrimSpace(e.text()); s != "" {
			parts = []string{s}
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return richdoc.Heading{Level: level(depth), Inlines: text(strings.Join(parts, " "))}
}

// poem is its title, then a paragraph per stanza.
func (p *parser) poem(e element, depth int) []richdoc.Block {
	var out []richdoc.Block
	for _, c := range e.children() {
		switch c.Name.Local {
		case "title":
			if h := p.title(c, depth+1); h != nil {
				out = append(out, h)
			}
		case "stanza":
			out = append(out, p.stanza(c)...)
		default:
			out = append(out, p.blocks([]element{c}, depth)...)
		}
	}
	return out
}

// stanza is ONE paragraph whose verses are separated by line breaks, which is
// what a stanza is: lines that belong together and are not paragraphs.
func (p *parser) stanza(e element) []richdoc.Block {
	var in []richdoc.Inline
	for _, c := range e.children() {
		if c.Name.Local != "v" {
			continue
		}
		if len(in) > 0 {
			in = append(in, richdoc.LineBreak{})
		}
		in = append(in, p.inlines(c)...)
	}
	if len(in) == 0 {
		return nil
	}
	return []richdoc.Block{richdoc.Paragraph{Inlines: in}}
}

// table turns <tr> and <td> into a grid.
//
// ⛔ The first row is the header only when its cells SAY they are, with <th>.
// FB2 marks them, so nothing is guessed — unlike a grid found in a PDF by
// looking at alignment, which cannot know.
func (p *parser) table(e element) (richdoc.Table, bool) {
	var rows [][]richdoc.Cell
	var header []richdoc.Cell
	for i, tr := range e.children() {
		if tr.Name.Local != "tr" {
			continue
		}
		var cells []richdoc.Cell
		allTH := true
		for _, td := range tr.children() {
			if td.Name.Local != "td" && td.Name.Local != "th" {
				continue
			}
			if td.Name.Local != "th" {
				allTH = false
			}
			cells = append(cells, richdoc.Cell{Inlines: p.inlines(td)})
		}
		if len(cells) == 0 {
			continue
		}
		if i == 0 && allTH {
			header = cells
			continue
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 && len(header) == 0 {
		return richdoc.Table{}, false
	}
	return richdoc.Table{Header: header, Rows: rows}, true
}

// image resolves an <image> against the <binary> it names.
//
// ⛔ The bytes become a DATA URL. They are in the same file — that is what
// FB2 is — so a reader handing back "#cover.jpg" would be handing back a name
// nothing can resolve once the file is closed. A binary past the limit is
// left out rather than truncated: half a JPEG is not a smaller picture.
func (p *parser) image(e element) (richdoc.Image, bool) {
	href := e.attr("href")
	if href == "" {
		return richdoc.Image{}, false
	}
	img := richdoc.Image{URL: href, Alt: e.attr("alt"), Title: e.attr("title")}
	b, ok := p.binaries[strings.TrimPrefix(href, "#")]
	if !ok {
		// A binary the file names and does not carry: the href says what was
		// wanted and the alt text is still worth something.
		return img, true
	}
	if url, ok := p.urls[b.ID]; ok {
		img.URL = url
		return img, true
	}
	data := strings.Join(strings.Fields(b.Data), "")
	// Base64 is four characters for every three bytes.
	if len(data)/4*3 > p.limits.MaxBinary {
		return img, true
	}
	kind := b.ContentType
	if kind == "" {
		kind = "application/octet-stream"
	}
	img.URL = "data:" + kind + ";base64," + data
	if p.urls == nil {
		p.urls = map[string]string{}
	}
	p.urls[b.ID] = img.URL
	return img, true
}

// level clamps a heading to the levels richdoc's writers have.
//
// There is no guard for a depth below one: every caller starts at one — a
// body's children are depth 1 — and counts upward, so a zero would be a
// defect here rather than something a file can ask for.
func level(depth int) int {
	if depth > headingLevels {
		return headingLevels
	}
	return depth
}

// text is one string as inline content.
//
// Every caller has already found the string non-empty — a heading is only
// made where there is something to put in it — so there is no guard here for
// a string that says nothing.
func text(s string) []richdoc.Inline {
	return []richdoc.Inline{richdoc.Text{Value: s}}
}

// inlineText reads inline content back as a string, through richdoc's own
// rule so that there is one of them.
func inlineText(in []richdoc.Inline) string {
	if len(in) == 0 {
		return ""
	}
	return richdoc.PlainText(&richdoc.Document{
		Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: in}},
	})
}
