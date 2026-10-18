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
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/comm"
)

// The bounds of a document's text.
const (
	maxDocumentText = 2 << 20
	maxSections     = 512
	maxSectionBytes = 32 << 10
	maxHeadingBytes = 200
	// textPartBytes is the size plain text is packed into parts of.
	textPartBytes = 16 << 10
)

// sections builds a Document from headings and their text in reading order,
// within the document bounds.
type sections struct {
	document Document
	// path holds the levels and headings above the next section.
	path []pathHeading
}

type pathHeading struct {
	level   int
	heading string
}

// full reports whether the document takes no more text.
func (s *sections) full() bool {
	return len(s.document.Sections) >= maxSections || s.document.TextBytes >= maxDocumentText
}

// add appends the section heading at level with text, split into parts of at
// most maxSectionBytes that keep the heading; level 0 is a heading outside
// the outline, such as "Page 3". Text beyond the document's bounds is cut and
// the document marked truncated. Text before the first heading that is blank
// adds nothing.
func (s *sections) add(heading string, level int, text string) {
	heading = comm.TruncateUTF8(strings.Join(strings.Fields(heading), " "), maxHeadingBytes)
	text = strings.Trim(text, "\n")
	if heading == "" && strings.TrimSpace(text) == "" {
		return
	}
	if s.full() {
		s.document.Truncated = true
		return
	}
	for len(s.path) > 0 && (level == 0 || s.path[len(s.path)-1].level >= level) {
		s.path = s.path[:len(s.path)-1]
	}
	path := make([]string, 0, len(s.path)+1)
	for _, above := range s.path {
		path = append(path, above.heading)
	}
	if heading != "" {
		path = append(path, heading)
	}
	if level > 0 {
		s.path = append(s.path, pathHeading{level, heading})
	}
	for part := 1; ; part++ {
		if s.full() {
			s.document.Truncated = true
			return
		}
		piece, rest := cutSection(text)
		if remaining := maxDocumentText - s.document.TextBytes; len(piece) > remaining {
			piece, rest = comm.TruncateUTF8(piece, remaining), ""
			s.document.Truncated = true
		}
		title := heading
		if part > 1 {
			title = heading + " (part " + strconv.Itoa(part) + ")"
		}
		s.document.Sections = append(s.document.Sections, Section{
			ID: "s" + strconv.Itoa(len(s.document.Sections)+1), Heading: title, Level: level, Path: path, Text: piece,
		})
		s.document.TextBytes += len(piece)
		if text = strings.TrimLeft(rest, "\n"); text == "" {
			return
		}
	}
}

// cutSection cuts the first at most maxSectionBytes of text at a paragraph,
// a line or a word boundary in its second half, or else at a character
// boundary.
func cutSection(text string) (string, string) {
	if len(text) <= maxSectionBytes {
		return text, ""
	}
	window := comm.TruncateUTF8(text, maxSectionBytes)
	for _, separator := range []string{"\n\n", "\n", " "} {
		if at := strings.LastIndex(window, separator); at >= maxSectionBytes/2 {
			return text[:at], text[at+len(separator):]
		}
	}
	return window, text[len(window):]
}

// decodeText decodes the bytes of a Markdown or plain text file: UTF-8
// without its BOM, or UTF-16 with a BOM. Invalid UTF-8, an unpaired surrogate
// or a NUL is no text file.
func decodeText(data []byte) (string, bool) {
	var text string
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		text = string(data[3:])
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}), bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		units := data[2:]
		if len(units)%2 != 0 {
			return "", false
		}
		decoded := make([]uint16, len(units)/2)
		for index := range decoded {
			if data[0] == 0xFE {
				decoded[index] = uint16(units[2*index])<<8 | uint16(units[2*index+1])
			} else {
				decoded[index] = uint16(units[2*index+1])<<8 | uint16(units[2*index])
			}
		}
		for index := 0; index < len(decoded); index++ {
			if !utf16.IsSurrogate(rune(decoded[index])) {
				continue
			}
			if index+1 == len(decoded) || utf16.DecodeRune(rune(decoded[index]), rune(decoded[index+1])) == unicode.ReplacementChar {
				return "", false
			}
			index++
		}
		text = string(utf16.Decode(decoded))
	default:
		text = string(data)
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", false
	}
	return cleanText(text), true
}

// cleanText ends every line with a line feed and drops the control
// characters but tab and line feed.
func cleanText(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}

var (
	atxHeading    = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	setextHeading = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	codeFence     = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	// blockStart is a line that starts a block a setext underline does not
	// make a heading of: a list item, a quote or a table row.
	blockStart = regexp.MustCompile(`^ {0,3}([-*+>|]|\d{1,9}[.)])`)
)

// markdownSections reads ATX and setext headings; fenced code, and the front
// matter of a file that starts with ---, stay text of their section.
func markdownSections(text string, into *sections) {
	lines := strings.Split(text, "\n")
	heading, level := "", 0
	var body strings.Builder
	var paragraph []string
	fence := ""
	commit := func() {
		for _, line := range paragraph {
			body.WriteString(line + "\n")
		}
		paragraph = nil
	}
	open := func(next string, nextLevel int) {
		commit()
		into.add(heading, level, body.String())
		heading, level = next, nextLevel
		body.Reset()
	}
	start := 0
	if len(lines) > 0 && strings.TrimRight(lines[0], " \t") == "---" {
		for end := 1; end < len(lines) && end <= 200; end++ {
			if marker := strings.TrimRight(lines[end], " \t"); marker == "---" || marker == "..." {
				body.WriteString(strings.Join(lines[:end+1], "\n") + "\n")
				start = end + 1
				break
			}
		}
	}
	for _, line := range lines[start:] {
		if fence != "" {
			body.WriteString(line + "\n")
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if marker := codeFence.FindStringSubmatch(line); marker != nil {
			commit()
			fence = marker[1]
			body.WriteString(line + "\n")
			continue
		}
		if match := atxHeading.FindStringSubmatch(line); match != nil {
			open(match[2], len(match[1]))
			continue
		}
		if match := setextHeading.FindStringSubmatch(line); match != nil && len(paragraph) > 0 && !blockStart.MatchString(paragraph[0]) {
			title := strings.Join(paragraph, " ")
			paragraph = nil
			nextLevel := 2
			if match[1][0] == '=' {
				nextLevel = 1
			}
			open(title, nextLevel)
			continue
		}
		if strings.TrimSpace(line) == "" {
			commit()
			body.WriteString("\n")
			continue
		}
		paragraph = append(paragraph, line)
	}
	open("", 0)
}

// textSections packs the paragraphs of plain text into parts of about
// textPartBytes, titled Part 1, Part 2 and so on.
func textSections(text string, into *sections) {
	var part strings.Builder
	number := 0
	emit := func() {
		if strings.TrimSpace(part.String()) != "" {
			number++
			into.add("Part "+strconv.Itoa(number), 0, part.String())
		}
		part.Reset()
	}
	for _, paragraph := range strings.SplitAfter(text, "\n\n") {
		if part.Len() > 0 && part.Len()+len(paragraph) > textPartBytes {
			emit()
		}
		if into.full() {
			into.document.Truncated = true
			return
		}
		part.WriteString(paragraph)
	}
	emit()
}
