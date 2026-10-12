package fb2

import (
	"encoding/xml"
	"strings"

	"github.com/go-richdoc/richdoc"
)

// a fictionBook is the file.
//
// ⛔ Every element is matched by its LOCAL name, with no namespace. FB2's own
// namespace is http://www.gribuser.ru/xml/fictionbook/2.0 and producers write
// it, mis-write it, and leave it out — a reader that insisted would refuse
// files every other reader opens. The format has no second vocabulary to
// confuse it with, so the local name is unambiguous here in a way it would
// not be in OOXML.
type fictionBook struct {
	XMLName     xml.Name      `xml:"FictionBook"`
	Description descriptionEl `xml:"description"`
	Bodies      []element     `xml:"body"`
	Binaries    []binary      `xml:"binary"`
}

type descriptionEl struct {
	TitleInfo struct {
		BookTitle string `xml:"book-title"`
		Authors   []struct {
			First  string `xml:"first-name"`
			Middle string `xml:"middle-name"`
			Last   string `xml:"last-name"`
			Nick   string `xml:"nickname"`
		} `xml:"author"`
		Lang string `xml:"lang"`
		Date string `xml:"date"`
	} `xml:"title-info"`
}

// meta is what the file says about itself, in the keys richdoc's other
// readers use.
func (b fictionBook) meta() map[string]string {
	out := map[string]string{}
	info := b.Description.TitleInfo
	if s := strings.TrimSpace(info.BookTitle); s != "" {
		out["title"] = s
	}
	var names []string
	for _, a := range info.Authors {
		name := strings.Join(strings.Fields(strings.Join(
			[]string{a.First, a.Middle, a.Last}, " ")), " ")
		if name == "" {
			name = strings.TrimSpace(a.Nick)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		out["author"] = strings.Join(names, ", ")
	}
	if s := strings.TrimSpace(info.Lang); s != "" {
		out["language"] = s
	}
	if s := strings.TrimSpace(info.Date); s != "" {
		out["date"] = s
	}
	if len(out) == 0 {
		// A document that says nothing about itself has no metadata, not an
		// empty map: a caller testing for a title should not find a map with
		// nothing in it and have to look twice.
		return nil
	}
	return out
}

type binary struct {
	ID          string `xml:"id,attr"`
	ContentType string `xml:"content-type,attr"`
	Data        string `xml:",chardata"`
}

// An element is any part of a FictionBook's text: its name, its attributes,
// and the tokens between its tags.
//
// ⛔ ONE representation, not two. The first version had a struct with both
// `Children []contentElem` and an inline-token field, each tagged `,any` —
// and encoding/xml fills only one of those, silently. Every test came back
// with an empty document and no error. The tokens are the single source of
// truth now, and both the block children and the inline run are derived
// from them.
//
// ⛔ And the tokens, rather than a typed tree, because mixed content is what
// a paragraph IS: "a <strong>bold</strong> word" is three things in a row,
// and a typed struct separates the text from the markup and loses where each
// went.
type element struct {
	Name   xml.Name
	Attrs  []xml.Attr
	Tokens []xml.Token
}

// UnmarshalXML keeps an element's name, its attributes and its contents.
func (e *element) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	e.Name = start.Name
	e.Attrs = append([]xml.Attr(nil), start.Attr...)
	depth := 0
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			e.Tokens = append(e.Tokens, t.Copy())
		case xml.EndElement:
			if depth == 0 {
				return nil
			}
			depth--
			e.Tokens = append(e.Tokens, t)
		case xml.CharData:
			e.Tokens = append(e.Tokens, xml.CharData(append([]byte(nil), t...)))
		}
	}
}

// attr is an attribute's value by local name, whatever namespace it carries.
// FB2 writes a link as l:href with l bound to XLink, and producers bind it to
// other prefixes or to nothing at all.
func (e element) attr(local string) string {
	for _, a := range e.Attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// children is the elements directly inside this one, each with its own
// contents. Text between them is not a child; [parser.inlines] is what reads
// that.
func (e element) children() []element {
	var out []element
	for i := 0; i < len(e.Tokens); {
		start, ok := e.Tokens[i].(xml.StartElement)
		if !ok {
			i++
			continue
		}
		child := element{Name: start.Name, Attrs: append([]xml.Attr(nil), start.Attr...)}
		child.Tokens, i = run(e.Tokens, i+1)
		out = append(out, child)
	}
	return out
}

// run is the tokens from i until the element that opened them closes, and
// where to carry on from.
func run(tokens []xml.Token, i int) ([]xml.Token, int) {
	var out []xml.Token
	depth := 0
	for i < len(tokens) {
		switch t := tokens[i].(type) {
		case xml.StartElement:
			depth++
			out = append(out, t)
		case xml.EndElement:
			if depth == 0 {
				return out, i + 1
			}
			depth--
			out = append(out, t)
		default:
			out = append(out, tokens[i])
		}
		i++
	}
	// Unreachable through [Parse]: encoding/xml refuses a document whose
	// elements do not close, so every start in these tokens has its end.
	return out, i
}

// text is everything this element says, markup ignored.
func (e element) text() string {
	var b strings.Builder
	for _, t := range e.Tokens {
		if c, ok := t.(xml.CharData); ok {
			b.Write(c)
		}
	}
	return b.String()
}

// inlines turns an element's mixed content into richdoc inlines.
func (p *parser) inlines(e element) []richdoc.Inline {
	out, _ := p.inlineRun(e.Tokens, 0)
	return out
}

// inlineRun reads tokens from i until the element that opened them closes.
func (p *parser) inlineRun(tokens []xml.Token, i int) ([]richdoc.Inline, int) {
	var out []richdoc.Inline
	for i < len(tokens) {
		switch t := tokens[i].(type) {
		case xml.CharData:
			if s := string(t); s != "" {
				out = append(out, richdoc.Text{Value: s})
			}
			i++
		case xml.StartElement:
			var kids []richdoc.Inline
			var inner []xml.Token
			inner, i = run(tokens, i+1)
			kids, _ = p.inlineRun(inner, 0)
			out = append(out, p.mark(t, inner, kids)...)
		default:
			// ⛔ Anything else is SKIPPED, and `i` still advances. The token
			// lists this walks hold only text, starts and ends —
			// [element.UnmarshalXML] keeps nothing else — and every end has
			// already been matched by [run], so nothing should arrive here.
			// Advancing anyway is the difference between a token this does
			// not understand and a loop that never finishes.
			i++
		}
	}
	return out, i
}

// mark wraps a run of inlines in what its element says about them.
func (p *parser) mark(start xml.StartElement, inner []xml.Token, kids []richdoc.Inline) []richdoc.Inline {
	e := element{Name: start.Name, Attrs: start.Attr, Tokens: inner}
	switch start.Name.Local {
	case "strong":
		return []richdoc.Inline{richdoc.Strong{Inlines: kids}}
	case "emphasis":
		return []richdoc.Inline{richdoc.Emph{Inlines: kids}}
	case "strikethrough":
		return []richdoc.Inline{richdoc.Strikethrough{Inlines: kids}}
	case "code":
		return []richdoc.Inline{richdoc.Code{Value: inlineText(kids)}}
	case "a":
		return []richdoc.Inline{richdoc.Link{URL: e.attr("href"), Inlines: kids}}
	case "image":
		if img, ok := p.image(e); ok {
			return []richdoc.Inline{img}
		}
		return kids
	}
	// ⛔ <style>, <sub>, <sup>, and anything an extension added: the text is
	// kept and the marking is not. richdoc has no subscript, and losing the
	// 2 of "H2O" to say so would be the worse trade.
	return kids
}
