// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fileextract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The bounds of a Word archive's parts: only these two are opened, each at
// most docxPartBytes uncompressed and at most docxExpansion times its
// compressed size, and the XML nests at most docxDepth elements.
const (
	docxDocument  = "word/document.xml"
	docxStyles    = "word/styles.xml"
	docxPartBytes = 64 << 20
	docxExpansion = 100
	docxDepth     = 256
	wordNamespace = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
)

// cfbSignature starts an OLE compound file: a legacy .doc, or an encrypted
// Office document, which keeps its EncryptionInfo stream inside one.
var cfbSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// docxSections reads the paragraphs of a Word document. Paragraphs styled
// Title or Heading 1 to 6 (or whose style or own properties give an outline
// level from 0 to 5) start sections; tables become tab-separated rows;
// tracked deletions, drawings, embedded objects and field codes are skipped.
func docxSections(data []byte, into *sections) *Refusal {
	if bytes.HasPrefix(data, cfbSignature) {
		if bytes.Contains(data, utf16LE("EncryptionInfo")) {
			return &Refusal{Code: CodeEncrypted, Detail: "the Word document is encrypted"}
		}
		return &Refusal{Code: CodeUnsupportedContent, Detail: "the file is a legacy Word document (.doc), not .docx"}
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return &Refusal{Code: CodeUnsupportedContent, Detail: "the file is not a ZIP archive: " + err.Error()}
	}
	parts := map[string][]byte{}
	for _, file := range archive.File {
		if file.Name != docxDocument && file.Name != docxStyles {
			continue
		}
		if _, seen := parts[file.Name]; seen {
			return &Refusal{Code: CodeUnsupportedContent, Detail: "the archive holds " + file.Name + " twice"}
		}
		part, refusal := readPart(file)
		if refusal != nil {
			return refusal
		}
		parts[file.Name] = part
	}
	if parts[docxDocument] == nil {
		return &Refusal{Code: CodeUnsupportedContent, Detail: "the archive has no " + docxDocument}
	}
	levels, err := headingStyles(parts[docxStyles])
	if err != nil {
		return &Refusal{Code: CodeUnsupportedContent, Detail: "the styles are not readable XML: " + err.Error()}
	}
	reader := wordReader{levels: levels, into: into}
	if err = reader.read(parts[docxDocument]); err != nil {
		return &Refusal{Code: CodeUnsupportedContent, Detail: "the document is not readable XML: " + err.Error()}
	}
	return nil
}

// readPart reads one archive member within its bounds.
func readPart(file *zip.File) ([]byte, *Refusal) {
	limit := min(docxPartBytes, docxExpansion*file.CompressedSize64)
	if file.UncompressedSize64 > limit {
		return nil, &Refusal{Code: CodeUnsupportedContent, Detail: fmt.Sprintf("%s expands to %d bytes, beyond %d", file.Name, file.UncompressedSize64, limit)}
	}
	member, err := file.Open()
	if err != nil {
		return nil, &Refusal{Code: CodeUnsupportedContent, Detail: file.Name + " is not readable: " + err.Error()}
	}
	defer func() { _ = member.Close() }()
	data, err := io.ReadAll(io.LimitReader(member, int64(limit)+1))
	switch {
	case err != nil:
		return nil, &Refusal{Code: CodeUnsupportedContent, Detail: file.Name + " is not readable: " + err.Error()}
	case uint64(len(data)) > limit:
		return nil, &Refusal{Code: CodeUnsupportedContent, Detail: fmt.Sprintf("%s expands beyond %d bytes", file.Name, limit)}
	}
	return data, nil
}

// utf16LE is text in UTF-16LE, as a compound file names its streams.
func utf16LE(text string) []byte {
	encoded := make([]byte, 0, 2*len(text))
	for _, b := range []byte(text) {
		encoded = append(encoded, b, 0)
	}
	return encoded
}

// wordStyle is one paragraph style of styles.xml.
type wordStyle struct {
	name, basedOn string
	outline       int // the outline level plus one; 0 is none
}

// headingStyles maps each paragraph style ID to its heading level, 1 to 6, or
// 0: Title is 1, Heading N is N, and a style without either takes the outline
// level of its own properties or of the styles it is based on.
func headingStyles(data []byte) (map[string]int, error) {
	styles := map[string]*wordStyle{}
	if data != nil {
		decoder := xml.NewDecoder(bytes.NewReader(data))
		var current *wordStyle
		depth := 0
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			switch element := token.(type) {
			case xml.StartElement:
				if depth++; depth > docxDepth {
					return nil, errors.New("elements nest too deeply")
				}
				if element.Name.Space != wordNamespace {
					continue
				}
				switch element.Name.Local {
				case "style":
					current = nil
					if wordAttribute(element, "type") == "paragraph" {
						current = &wordStyle{}
						styles[wordAttribute(element, "styleId")] = current
					}
				case "name":
					if current != nil {
						current.name = wordAttribute(element, "val")
					}
				case "basedOn":
					if current != nil {
						current.basedOn = wordAttribute(element, "val")
					}
				case "outlineLvl":
					if current != nil {
						current.outline = outlineLevel(wordAttribute(element, "val"))
					}
				}
			case xml.EndElement:
				depth--
				if element.Name.Local == "style" {
					current = nil
				}
			}
		}
	}
	levels := map[string]int{}
	for id := range styles {
		levels[id] = styleLevel(id, styles)
	}
	return levels, nil
}

// styleLevel resolves the heading level of style id along at most eight
// basedOn links.
func styleLevel(id string, styles map[string]*wordStyle) int {
	for hop := 0; hop < 8; hop++ {
		style := styles[id]
		if style == nil {
			return namedLevel(id)
		}
		if level := namedLevel(style.name); level > 0 {
			return level
		}
		if level := namedLevel(id); level > 0 {
			return level
		}
		if style.outline > 0 {
			return style.outline
		}
		id = style.basedOn
	}
	return 0
}

// namedLevel is the heading level a style name or ID spells: Title, or
// Heading 1 to Heading 6 in any case and spacing.
func namedLevel(name string) int {
	name = strings.ToLower(strings.ReplaceAll(name, " ", ""))
	if name == "title" {
		return 1
	}
	if digits, found := strings.CutPrefix(name, "heading"); found {
		if level, err := strconv.Atoi(digits); err == nil && level >= 1 && level <= 6 {
			return level
		}
	}
	return 0
}

// outlineLevel is the heading level of an outline level 0 to 5, else 0.
func outlineLevel(value string) int {
	if level, err := strconv.Atoi(value); err == nil && level >= 0 && level <= 5 {
		return level + 1
	}
	return 0
}

func wordAttribute(element xml.StartElement, name string) string {
	for _, attribute := range element.Attr {
		if attribute.Name.Local == name && attribute.Name.Space == wordNamespace {
			return attribute.Value
		}
	}
	return ""
}

// skippedWordElements hold no text the document shows: tracked deletions and
// moves, drawings, embedded objects, field codes and the alternative content
// that duplicates a drawing. They are skipped whatever their namespace.
var skippedWordElements = map[string]bool{
	"del": true, "moveFrom": true, "delText": true, "drawing": true, "pict": true, "object": true, "instrText": true, "Fallback": true,
	// The former properties of a tracked change, such as an earlier style.
	"pPrChange": true, "rPrChange": true,
}

// wordTable is one table being read: its finished rows, the row and the cell.
type wordTable struct {
	rows      []string
	row, cell []string
}

// wordReader reads document.xml into sections.
type wordReader struct {
	levels  map[string]int
	into    *sections
	heading string
	level   int
	body    strings.Builder
	// paragraph is the text of the current paragraph and paragraphLevel its
	// heading level.
	paragraph      strings.Builder
	paragraphLevel int
	tables         []*wordTable
}

func (r *wordReader) read(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, skipping, inText := 0, 0, false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if depth++; depth > docxDepth {
				return errors.New("elements nest too deeply")
			}
			if skipping > 0 || skippedWordElements[element.Name.Local] {
				skipping++
				continue
			}
			inText = element.Name.Space == wordNamespace && r.start(element)
		case xml.EndElement:
			depth--
			if skipping > 0 {
				skipping--
				continue
			}
			inText = false
			if element.Name.Space == wordNamespace {
				r.end(element.Name.Local)
			}
		case xml.CharData:
			if inText && skipping == 0 {
				r.paragraph.Write(element)
			}
		}
		if r.into.full() {
			r.into.document.Truncated = true
			return nil
		}
	}
	r.into.add(r.heading, r.level, r.body.String())
	return nil
}

// start handles an element of the WordprocessingML namespace and reports
// whether its character data is text.
func (r *wordReader) start(element xml.StartElement) bool {
	switch element.Name.Local {
	case "p":
		r.paragraph.Reset()
		r.paragraphLevel = 0
	case "pStyle":
		style := wordAttribute(element, "val")
		if r.paragraphLevel = r.levels[style]; r.paragraphLevel == 0 {
			r.paragraphLevel = namedLevel(style)
		}
	case "outlineLvl":
		if r.paragraphLevel == 0 {
			r.paragraphLevel = outlineLevel(wordAttribute(element, "val"))
		}
	case "t":
		return true
	case "tab", "ptab":
		r.paragraph.WriteByte('\t')
	case "br", "cr":
		r.paragraph.WriteByte('\n')
	case "noBreakHyphen":
		r.paragraph.WriteByte('-')
	case "tbl":
		r.tables = append(r.tables, &wordTable{})
	}
	return false
}

// end handles the end of an element of the WordprocessingML namespace.
func (r *wordReader) end(local string) {
	table := r.table()
	switch local {
	case "p":
		text := strings.TrimRight(r.paragraph.String(), " \t")
		r.paragraph.Reset()
		switch {
		case table != nil:
			if text != "" {
				table.cell = append(table.cell, text)
			}
		case r.paragraphLevel > 0 && strings.TrimSpace(text) != "":
			r.into.add(r.heading, r.level, r.body.String())
			r.heading, r.level = text, r.paragraphLevel
			r.body.Reset()
		default:
			r.body.WriteString(text + "\n")
		}
	case "tc":
		if table != nil {
			table.row = append(table.row, strings.Join(table.cell, " "))
			table.cell = nil
		}
	case "tr":
		if table != nil {
			table.rows = append(table.rows, strings.Join(table.row, "\t"))
			table.row = nil
		}
	case "tbl":
		if table == nil {
			return
		}
		r.tables = r.tables[:len(r.tables)-1]
		if outer := r.table(); outer != nil {
			outer.cell = append(outer.cell, strings.Join(table.rows, " / "))
		} else {
			r.body.WriteString(strings.Join(table.rows, "\n") + "\n\n")
		}
	}
}

func (r *wordReader) table() *wordTable {
	if len(r.tables) == 0 {
		return nil
	}
	return r.tables[len(r.tables)-1]
}
