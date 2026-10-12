# fb2

**FictionBook 2 (`.fb2`) into [richdoc](https://github.com/go-richdoc/richdoc).**
Pure Go, CGO-free, including `GOOS=js`. richdoc is the only dependency —
everything else is the standard library.

```go
doc, err := fb2.Parse(src, fb2.Limits{})
```

FictionBook is an XML ebook format, widely used in Russia and eastern Europe
and almost unknown elsewhere — which is the reason anybody converts one.

## It is one file, and that is the whole difference

Unlike EPUB there is **no ZIP, no package document and no spine**: a
FictionBook is a single XML file, and its images are base64 inside that same
file. That makes it the one ebook format a reader can do with `encoding/xml`
and nothing else, which is why this is a small library rather than a stack.

| | |
|---|---|
| `<body>`, `<section>` | nesting, which is where a heading's **level** comes from — there is no `<h2>` in FB2, only a section inside a section |
| `<title>`, `<subtitle>` | a heading |
| `<p>` | a paragraph |
| `<poem>`, `<stanza>` | a paragraph per stanza, its verses separated by **line breaks** — which is what a stanza is: lines that belong together and are not paragraphs |
| `<cite>`, `<epigraph>`, `<annotation>` | a block quotation |
| `<table>` | a table, its header row the one FB2 **marked** with `<th>` |
| `<image>` | an image, with the `<binary>` it names turned into a **data URL** |
| `<strong>`, `<emphasis>` | strong and emphasis; also strikethrough, code and links |

## ⛔ Decisions worth knowing before you read the output

**A title broken over several `<p>` is one heading.** FB2 does that for the
line breaks, and a document with three headings where a reader sees one
navigates wrongly.

**An image becomes its bytes.** They are in the same file — that is what FB2
is — so handing back `#cover.jpg` would be handing back a name nothing can
resolve once the file is closed. A binary past `Limits.MaxBinary` is left
**out** rather than truncated: half a JPEG is not a smaller picture, and the
`href` and alt text still say what was wanted.

**Only the first row can be a header, and only when every cell is `<th>`.** A
long table repeats its headings partway down; the repeat is a row, or the last
one silently replaces the first and everything above it loses its heading.

**A second `<body name="notes">` comes out as a section of its own**, with
that name as a heading. FB2 links to a note by id, and resolving those would
mean deciding where in a paragraph a reader wants it.

**Namespaces are not matched.** FB2's own is
`http://www.gribuser.ru/xml/fictionbook/2.0`, and producers write it,
mis-write it and leave it out — a reader that insisted would refuse files
every other reader opens. The format has no second vocabulary to confuse it
with, so a local name is unambiguous here in a way it would not be in OOXML.

**An element it does not know is still content**: its text becomes a
paragraph. FB2 has extensions, and losing them silently is worse than
rendering them plainly.

**What it deliberately drops**: an `<empty-line/>` is vertical space and the
paragraph break is already there; `<sub>` and `<sup>` have no richdoc
equivalent, so their text is kept and their position is not — losing the 2 of
`H2O` to say so would be the worse trade.

## ⛔ The depth limit is a pass of its own

The stack a deeply nested file overflows belongs to `encoding/xml`, not to
this package, so the depth is counted over **tokens, before anything builds a
tree**. A guard placed where our own code recurses never runs:
[go-odf/odf](https://github.com/go-odf/odf) learned that with a 115 MiB
allocation inside the decoder while its own counter stood at two.

`Limits{}` is the default everywhere: 256 levels of nesting, 64 MiB per
decoded binary.

## One representation, not two

The first version of the element type had both a typed `Children` field and a
raw-token field, each tagged `,any`. **`encoding/xml` fills only one of
those, silently** — every test came back with an empty document and no error.
The tokens are the single source of truth now, and both the block children
and the inline run are derived from them.

That is also the right shape for the job: mixed content is what a paragraph
**is**. `a <strong>bold</strong> word` is three things in a row, and any
typed struct separates the text from the markup and loses where each went.

## How it is checked

100% of statements with no uncovered block, `go vet` and `gofmt` clean, and
builds for amd64, arm64, riscv64, loong64, ppc64le, s390x and js/wasm. 19
mutations were run against the package and every one was caught — two of them
only after a test that asserts `Header` **directly** (a helper printing a
header and a first row identically could not tell them apart) and a fixture
whose table repeats its `<th>` partway down.

## The rest of the family

[**richdoc**](https://github.com/go-richdoc/richdoc) is the model ·
[**markdown**](https://github.com/go-richdoc/markdown) reads and writes it ·
[**rst**](https://github.com/go-richdoc/rst) ·
[**latex**](https://github.com/go-richdoc/latex) ·
[**data**](https://github.com/go-richdoc/data) is CSV, JSON, XML and plain
text.

[**go-pdfkit**](https://github.com/go-pdfkit) turns a richdoc document into a
PDF, which is what makes `.fb2` → PDF a two-step with no format-specific code
in between.

## Licence

BSD-3-Clause.
