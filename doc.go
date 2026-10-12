// Package fb2 reads FictionBook 2 (.fb2) into the richdoc document model.
//
// FictionBook is an XML ebook format, widely used in Russia and eastern
// Europe and almost unknown elsewhere — which is the reason anybody converts
// one. Unlike EPUB it is a SINGLE XML FILE: no ZIP, no package document, no
// spine, and its images are base64 inside the same file. That makes it the
// one ebook format a reader can do with encoding/xml and nothing else.
//
// # What it maps onto
//
//	<body>, <section>    nesting, which is where a heading's LEVEL comes from
//	<title>, <subtitle>  a heading
//	<p>                  a paragraph
//	<poem>, <stanza>     a paragraph per stanza, its verses separated by
//	                     line breaks — which is what a stanza IS
//	<cite>, <epigraph>   a block quotation
//	<table>              a table
//	<image>              an image, with the <binary> it names turned into a
//	                     data URL, because the bytes are in the same file and
//	                     a reader that dropped them would lose the cover
//	<strong>, <emphasis> strong and emphasis; also strikethrough, code, links
//
// # What it does not keep
//
// An <empty-line/> is vertical space between paragraphs, and richdoc has no
// word for it: the paragraph break is already there. <sub> and <sup> have no
// richdoc equivalent either, so their text is kept and their position is not
// — losing "H2O"'s subscript is better than losing the 2.
//
// A second <body name="notes"> is the footnotes, and it comes out as a
// section of its own at the end rather than being woven into the text: FB2
// links to a note by id, and resolving those would mean deciding where in a
// paragraph a reader wants it.
//
// Nothing here is CGO.
package fb2
