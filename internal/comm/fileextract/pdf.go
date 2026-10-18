// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fileextract

import (
	"bytes"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// The bounds of a PDF's structure that the reader walks.
const (
	maxPDFPages   = 10_000
	maxPDFNodes   = 50_000
	maxPDFOutline = 1_024
	maxPDFNames   = 10_000
)

// pdfSections reads a PDF's text layer page by page with
// github.com/ledongthuc/pdf for its objects, streams and encryption, and its
// own text assembly: ToUnicode maps first, glyph positions for spaces and
// lines. Outline entries that name a page start sections; without them every
// page is a section "Page N". A PDF that only an owner password restricts
// opens; one that needs a password is encrypted, one without text no_text.
func pdfSections(data []byte, into *sections) *Refusal {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return &Refusal{Code: CodeUnsupportedContent, Detail: "the file does not start with %PDF-"}
	}
	reader, err := pdf.NewReader(pdfHeader{data}, int64(len(data)))
	if err != nil {
		if errors.Is(err, pdf.ErrInvalidPassword) || bytes.Contains(data, []byte("/Encrypt")) {
			return &Refusal{Code: CodeEncrypted, Detail: "the PDF needs a password: " + err.Error()}
		}
		return &Refusal{Code: CodeUnsupportedContent, Detail: "the PDF is not readable: " + err.Error()}
	}
	root := reader.Trailer().Key("Root")
	pages := pdfPages(root.Key("Pages"))
	if len(pages) == 0 {
		return &Refusal{Code: CodeNoText, Detail: "the PDF has no pages"}
	}
	index := make(map[string]int, len(pages))
	var all strings.Builder
	starts := make([]int, len(pages)+1)
	for number, page := range pages {
		if _, seen := index[page.V.String()]; !seen {
			index[page.V.String()] = number
		}
		starts[number] = all.Len()
		if all.Len() < maxDocumentText+maxSectionBytes {
			all.WriteString(trimLines(pageText(page, maxDocumentText+maxSectionBytes-all.Len())))
			all.WriteString("\n")
		}
	}
	starts[len(pages)] = all.Len()
	text := all.String()
	if strings.TrimSpace(text) == "" {
		return &Refusal{Code: CodeNoText, Detail: "the PDF has no text layer; it may be scanned"}
	}
	into.document.PageCount = len(pages)
	entries := pdfOutline(root, index)
	if len(entries) == 0 {
		for number := range pages {
			if page := text[starts[number]:starts[number+1]]; strings.TrimSpace(page) != "" {
				into.add("Page "+strconv.Itoa(number+1), 0, page)
			}
		}
		return nil
	}
	// Each entry starts where its title appears on its page, or else at the
	// page's start, never before the previous entry.
	previous := 0
	for position := range entries {
		entry := &entries[position]
		entry.start = max(previous, starts[entry.page])
		window := ""
		if entry.start < starts[entry.page+1] {
			window = text[entry.start:starts[entry.page+1]]
		}
		if found := titlePattern(entry.title).FindStringIndex(window); found != nil {
			entry.start, entry.end = entry.start+found[0], entry.start+found[1]
		} else {
			entry.end = entry.start
		}
		previous = entry.end
	}
	into.add("", 0, text[:entries[0].start])
	for position, entry := range entries {
		end := len(text)
		if position+1 < len(entries) {
			end = entries[position+1].start
		}
		into.add(entry.title, entry.level, text[entry.end:max(entry.end, end)])
	}
	return nil
}

// trimLines drops the spaces that end lines and the blank lines beyond one in
// a row, which fixed-width layouts leave.
func trimLines(text string) string {
	lines := strings.Split(text, "\n")
	kept := lines[:0]
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" && len(kept) > 0 && kept[len(kept)-1] == "" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// pdfHeader presents a PDF whose header names any version as PDF 1.7, the
// newest header the library opens; the header is only a comment.
type pdfHeader struct{ data []byte }

func (h pdfHeader) ReadAt(out []byte, offset int64) (int, error) {
	if offset >= int64(len(h.data)) {
		return 0, errors.New("read beyond the PDF")
	}
	n := copy(out, h.data[offset:])
	header := []byte("%PDF-1.7\n")
	if len(h.data) > len(header) && h.data[5] >= '1' && h.data[5] <= '9' && h.data[6] == '.' {
		for at := int(offset); at < len(header) && at < int(offset)+n; at++ {
			out[at-int(offset)] = header[at]
		}
	}
	if n < len(out) {
		return n, errors.New("read beyond the PDF")
	}
	return n, nil
}

// pdfPages walks the page tree in order, bounded in pages, nodes and depth.
func pdfPages(node pdf.Value) []pdf.Page {
	var pages []pdf.Page
	visited := 0
	var walk func(node pdf.Value, depth int)
	walk = func(node pdf.Value, depth int) {
		if visited++; visited > maxPDFNodes || depth > 32 || len(pages) >= maxPDFPages || node.Kind() != pdf.Dict {
			return
		}
		kids := node.Key("Kids")
		if node.Key("Type").Name() == "Page" || kids.Kind() != pdf.Array {
			pages = append(pages, pdf.Page{V: node})
			return
		}
		for at := 0; at < kids.Len(); at++ {
			walk(kids.Index(at), depth+1)
		}
	}
	walk(node, 0)
	return pages
}

// outlineEntry is one outline entry that names a page.
type outlineEntry struct {
	title       string
	level, page int
	// start and end are where its title is in the document's text.
	start, end int
}

// pdfOutline lists the outline entries in reading order that name a page of
// index, at most maxPDFOutline, levels 1 to 6.
func pdfOutline(root pdf.Value, index map[string]int) []outlineEntry {
	names := pdfNamedDestinations(root)
	var entries []outlineEntry
	visited := map[string]bool{}
	var walk func(item pdf.Value, level int)
	walk = func(item pdf.Value, level int) {
		for ; item.Kind() == pdf.Dict && len(visited) < maxPDFOutline*4 && len(entries) < maxPDFOutline; item = item.Key("Next") {
			identity := item.String()
			if visited[identity] || level > 16 {
				return
			}
			visited[identity] = true
			if page, found := destinationPage(item, root, names, index); found {
				if title := strings.Join(strings.Fields(cleanText(item.Key("Title").Text())), " "); title != "" {
					entries = append(entries, outlineEntry{title: title, level: min(level, 6), page: page})
				}
			}
			walk(item.Key("First"), level+1)
		}
	}
	walk(root.Key("Outlines").Key("First"), 1)
	return entries
}

// destinationPage resolves the page an outline item's destination or GoTo
// action names: an explicit destination, or a named one.
func destinationPage(item, root pdf.Value, names map[string]pdf.Value, index map[string]int) (int, bool) {
	destination := item.Key("Dest")
	if action := item.Key("A"); destination.Kind() == pdf.Null && action.Key("S").Name() == "GoTo" {
		destination = action.Key("D")
	}
	switch destination.Kind() {
	case pdf.Name:
		destination = root.Key("Dests").Key(destination.Name())
	case pdf.String:
		destination = names[destination.RawString()]
	default:
	}
	if destination.Kind() == pdf.Dict {
		destination = destination.Key("D")
	}
	if destination.Kind() != pdf.Array || destination.Len() == 0 || destination.Index(0).Kind() != pdf.Dict {
		return 0, false
	}
	page, found := index[destination.Index(0).String()]
	return page, found
}

// pdfNamedDestinations reads the Dests name tree, bounded in entries, nodes
// and depth.
func pdfNamedDestinations(root pdf.Value) map[string]pdf.Value {
	names := map[string]pdf.Value{}
	visited := 0
	var walk func(node pdf.Value, depth int)
	walk = func(node pdf.Value, depth int) {
		if visited++; visited > maxPDFNodes || depth > 16 || node.Kind() != pdf.Dict {
			return
		}
		pairs := node.Key("Names")
		for at := 0; at+1 < pairs.Len() && len(names) < maxPDFNames; at += 2 {
			names[pairs.Index(at).RawString()] = pairs.Index(at + 1)
		}
		kids := node.Key("Kids")
		for at := 0; at < kids.Len(); at++ {
			walk(kids.Index(at), depth+1)
		}
	}
	walk(root.Key("Names").Key("Dests"), 0)
	return names
}

// titlePattern finds an outline title in page text, whatever the spacing.
func titlePattern(title string) *regexp.Regexp {
	fields := strings.Fields(title)
	for at, field := range fields {
		fields[at] = regexp.QuoteMeta(field)
	}
	return regexp.MustCompile(`(?i)` + strings.Join(fields, `\s*`))
}
