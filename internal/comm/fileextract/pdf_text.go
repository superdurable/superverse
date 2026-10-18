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
	"encoding/hex"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/ledongthuc/pdf"
)

// The bounds of the content the text assembly reads: the decoded bytes of
// one stream and of one page, the operators of one page and the nesting of
// form objects; a ToUnicode CMap's bytes and entries.
const (
	maxStreamBytes = 32 << 20
	maxPageBytes   = 64 << 20
	maxPageOps     = 2_000_000
	maxFormNesting = 4
	maxCMapBytes   = 1 << 20
	maxCMapEntries = 20_000
)

// matrix is a PDF transformation [a b c d e f]: x' = a·x + c·y + e and
// y' = b·x + d·y + f.
type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

// times is m then n.
func (m matrix) times(n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func (m matrix) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// pageText assembles the text of one page's content, at most budget bytes.
// The library reports a malformed object by panicking; the page then keeps
// the text read before it.
func pageText(page pdf.Page, budget int) (text string) {
	assembler := textAssembler{budget: budget, fonts: map[string]*pdfFont{}}
	defer func() {
		if recover() != nil {
			text = assembler.out.String()
		}
	}()
	assembler.run(page.V.Key("Contents"), page.Resources(), identity, 0)
	return assembler.out.String()
}

// textAssembler turns the text-showing operators of content streams into
// lines: a jump to another baseline is a line break and a gap on the same
// baseline a space.
type textAssembler struct {
	out                    strings.Builder
	budget, ops, readBytes int
	fonts                  map[string]*pdfFont
	lastX, lastY, lastSize float64
	pendingSpace, started  bool
	exhausted              bool
}

// textState is the graphics and text state of one content stream.
type textState struct {
	ctm, tm, tlm               matrix
	font                       *pdfFont
	size, charSpace, wordSpace float64
	scale, leading             float64
	stack                      []matrix
	resources                  pdf.Value
	fontsKey                   string
}

// run reads content, a stream or an array of streams, within the page's
// bounds; a malformed stream ends its reading and keeps the text read so far.
func (a *textAssembler) run(content, resources pdf.Value, ctm matrix, nesting int) {
	if a.exhausted {
		return
	}
	var data []byte
	streams := []pdf.Value{content}
	if content.Kind() == pdf.Array {
		streams = streams[:0]
		for at := 0; at < content.Len() && at < 1000; at++ {
			streams = append(streams, content.Index(at))
		}
	}
	for _, stream := range streams {
		if stream.Kind() == pdf.Stream {
			read := readStream(stream, min(maxStreamBytes, maxPageBytes-a.readBytes))
			a.readBytes += len(read)
			data = append(append(data, read...), '\n')
		}
	}
	state := &textState{ctm: ctm, tm: identity, tlm: identity, scale: 1, resources: resources, fontsKey: resources.String()}
	lexer := pdfLexer{data: data}
	var operands []operand
	for {
		value, operator, ok := lexer.next()
		if !ok {
			return
		}
		if operator == "" {
			if len(operands) < 64 {
				operands = append(operands, value)
			}
			continue
		}
		if a.ops++; a.ops > maxPageOps || a.out.Len() > a.budget {
			a.exhausted = true
			return
		}
		if operator == "BI" {
			lexer.skipInlineImage()
		} else {
			a.operate(state, operator, operands, nesting)
		}
		operands = operands[:0]
	}
}

// readStream reads the decoded bytes of stream, at most limit of them. A
// filter the library refuses ends the stream where it stopped.
func readStream(stream pdf.Value, limit int) (data []byte) {
	if limit <= 0 {
		return nil
	}
	defer func() {
		if recover() != nil {
			data = nil
		}
	}()
	reader := stream.Reader()
	defer func() { _ = reader.Close() }()
	data, _ = io.ReadAll(io.LimitReader(reader, int64(limit)))
	return data
}

func number(value pdf.Value) float64 {
	if value.Kind() == pdf.Integer {
		return float64(value.Int64())
	}
	return value.Float64()
}

func (a *textAssembler) operate(state *textState, operator string, operands []operand, nesting int) {
	argument := func(at int) float64 {
		if at < len(operands) {
			return operands[at].number
		}
		return 0
	}
	last := func() operand {
		if len(operands) == 0 {
			return operand{}
		}
		return operands[len(operands)-1]
	}
	switch operator {
	case "q":
		if len(state.stack) < 64 {
			state.stack = append(state.stack, state.ctm)
		}
	case "Q":
		if len(state.stack) > 0 {
			state.ctm, state.stack = state.stack[len(state.stack)-1], state.stack[:len(state.stack)-1]
		}
	case "cm":
		state.ctm = matrix{argument(0), argument(1), argument(2), argument(3), argument(4), argument(5)}.times(state.ctm)
	case "BT":
		state.tm, state.tlm = identity, identity
	case "Tf":
		if len(operands) == 2 {
			state.font, state.size = a.font(state, operands[0].text), argument(1)
		}
	case "Tc":
		state.charSpace = argument(0)
	case "Tw":
		state.wordSpace = argument(0)
	case "Tz":
		state.scale = argument(0) / 100
	case "TL":
		state.leading = argument(0)
	case "Td", "TD":
		if operator == "TD" {
			state.leading = -argument(1)
		}
		state.tlm = matrix{1, 0, 0, 1, argument(0), argument(1)}.times(state.tlm)
		state.tm = state.tlm
	case "Tm":
		state.tlm = matrix{argument(0), argument(1), argument(2), argument(3), argument(4), argument(5)}
		state.tm = state.tlm
	case "T*":
		state.tlm = matrix{1, 0, 0, 1, 0, -state.leading}.times(state.tlm)
		state.tm = state.tlm
	case "Tj":
		a.show(state, last().text)
	case "'", "\"":
		if operator == "\"" && len(operands) == 3 {
			state.wordSpace, state.charSpace = argument(0), argument(1)
		}
		state.tlm = matrix{1, 0, 0, 1, 0, -state.leading}.times(state.tlm)
		state.tm = state.tlm
		a.show(state, last().text)
	case "TJ":
		for _, element := range last().items {
			if element.kind == operandString {
				a.show(state, element.text)
				continue
			}
			// A negative adjustment moves right; a large one is a word gap.
			state.tm = matrix{1, 0, 0, 1, -element.number / 1000 * state.size * state.scale, 0}.times(state.tm)
			if -element.number > 200 {
				a.pendingSpace = true
			}
		}
	case "Do":
		a.form(state, last().text, nesting)
	}
}

// form reads the text of the named form object, inside the current
// transformation.
func (a *textAssembler) form(state *textState, name string, nesting int) {
	form := state.resources.Key("XObject").Key(name)
	if nesting >= maxFormNesting || form.Key("Subtype").Name() != "Form" {
		return
	}
	resources := form.Key("Resources")
	if resources.Kind() == pdf.Null {
		resources = state.resources
	}
	placement := identity
	if values := form.Key("Matrix"); values.Len() == 6 {
		for at := range placement {
			placement[at] = number(values.Index(at))
		}
	}
	a.run(form, resources, placement.times(state.ctm), nesting+1)
}

// font loads the named font of the stream's resources once per page.
func (a *textAssembler) font(state *textState, name string) *pdfFont {
	key := state.fontsKey + "/" + name
	if font, found := a.fonts[key]; found {
		return font
	}
	font := loadFont(state.resources.Key("Font").Key(name))
	a.fonts[key] = font
	return font
}

// show places the text of raw at the current text position and advances it.
func (a *textAssembler) show(state *textState, raw string) {
	if state.font == nil || raw == "" {
		return
	}
	device := state.tm.times(state.ctm)
	x, y := device.apply(0, 0)
	size := state.size * math.Hypot(device[2], device[3])
	text, advance := state.font.decode(raw, state)
	state.tm = matrix{1, 0, 0, 1, advance, 0}.times(state.tm)
	if text == "" {
		return
	}
	if a.started {
		lineHeight := math.Max(math.Min(size, a.lastSize), 1)
		switch {
		case math.Abs(y-a.lastY) > lineHeight*0.5:
			a.out.WriteByte('\n')
		case a.pendingSpace || x-a.lastX > lineHeight*0.15 || a.lastX-x > lineHeight:
			if !strings.HasSuffix(a.out.String(), " ") && !strings.HasPrefix(text, " ") {
				a.out.WriteByte(' ')
			}
		}
	}
	a.out.WriteString(text)
	a.lastX, a.lastY = state.tm.times(state.ctm).apply(0, 0)
	a.lastSize, a.pendingSpace, a.started = size, false, true
}

// operandKind is the type of a content operand the text assembly reads.
type operandKind int

const (
	operandOther operandKind = iota
	operandNumber
	operandString
	operandName
	operandArray
)

// operand is one content operand: a number, a string's bytes, a name or an
// array of operands; dictionaries are read as other.
type operand struct {
	kind   operandKind
	number float64
	text   string
	items  []operand
}

// pdfLexer reads the operands and operators of a content stream or a CMap.
type pdfLexer struct {
	data []byte
	at   int
}

// next reads the next operand, or the next operator; ok is false at the end.
func (l *pdfLexer) next() (operand, string, bool) {
	for l.at < len(l.data) {
		c := l.data[l.at]
		switch {
		case isPDFSpace(c):
			l.at++
		case c == '%':
			for l.at < len(l.data) && l.data[l.at] != '\n' && l.data[l.at] != '\r' {
				l.at++
			}
		case c == '(':
			return operand{kind: operandString, text: l.literal()}, "", true
		case c == '<' && l.at+1 < len(l.data) && l.data[l.at+1] == '<':
			l.skipDictionary()
			return operand{}, "", true
		case c == '<':
			return operand{kind: operandString, text: l.hexString()}, "", true
		case c == '/':
			l.at++
			return operand{kind: operandName, text: l.word()}, "", true
		case c == '[':
			l.at++
			array := operand{kind: operandArray}
			for {
				item, operator, ok := l.next()
				if !ok || operator == "]" {
					return array, "", true
				}
				if operator == "" && len(array.items) < 10_000 {
					array.items = append(array.items, item)
				}
			}
		case c == ']' || c == '>' || c == ')' || c == '{' || c == '}':
			l.at++
			return operand{}, string(c), true
		default:
			word := l.word()
			if value, err := strconv.ParseFloat(word, 64); err == nil {
				return operand{kind: operandNumber, number: value}, "", true
			}
			if word == "true" || word == "false" || word == "null" {
				return operand{}, "", true
			}
			return operand{}, word, true
		}
	}
	return operand{}, "", false
}

func isPDFSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == 0
}

// word reads a run of regular characters, at least one.
func (l *pdfLexer) word() string {
	start := l.at
	for l.at < len(l.data) && !isPDFSpace(l.data[l.at]) && !strings.ContainsRune("()<>[]{}/%", rune(l.data[l.at])) {
		l.at++
	}
	if l.at == start && l.at < len(l.data) {
		l.at++
	}
	return string(l.data[start:l.at])
}

// literal reads a parenthesized string with its escapes.
func (l *pdfLexer) literal() string {
	var out []byte
	depth := 0
	for ; l.at < len(l.data); l.at++ {
		c := l.data[l.at]
		switch {
		case c == '\\' && l.at+1 < len(l.data):
			l.at++
			switch escaped := l.data[l.at]; escaped {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r', '\n':
				if escaped == '\r' && l.at+1 < len(l.data) && l.data[l.at+1] == '\n' {
					l.at++
				}
			default:
				if escaped >= '0' && escaped <= '7' {
					value := 0
					for digits := 0; digits < 3 && l.at < len(l.data) && l.data[l.at] >= '0' && l.data[l.at] <= '7'; digits++ {
						value = value*8 + int(l.data[l.at]-'0')
						l.at++
					}
					l.at--
					out = append(out, byte(value))
				} else {
					out = append(out, escaped)
				}
			}
		case c == '(':
			if depth++; depth > 1 {
				out = append(out, c)
			}
		case c == ')':
			if depth--; depth == 0 {
				l.at++
				return string(out)
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// hexString reads a string of hex digits, a missing last digit being 0.
func (l *pdfLexer) hexString() string {
	end := bytes.IndexByte(l.data[l.at:], '>')
	if end < 0 {
		end = len(l.data) - l.at
	}
	digits := strings.Map(func(r rune) rune {
		if strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return r
		}
		return -1
	}, string(l.data[l.at+1:l.at+end]))
	if len(digits)%2 == 1 {
		digits += "0"
	}
	l.at += end + 1
	decoded, _ := hex.DecodeString(digits)
	return string(decoded)
}

// skipDictionary skips a dictionary, nested ones included.
func (l *pdfLexer) skipDictionary() {
	l.at += 2
	for depth := 1; depth > 0 && l.at < len(l.data); {
		switch {
		case bytes.HasPrefix(l.data[l.at:], []byte("<<")):
			depth++
			l.at += 2
		case bytes.HasPrefix(l.data[l.at:], []byte(">>")):
			depth--
			l.at += 2
		case l.data[l.at] == '(':
			l.literal()
		default:
			l.at++
		}
	}
}

// skipInlineImage skips an inline image's parameters and data, up to EI
// alone between white space.
func (l *pdfLexer) skipInlineImage() {
	if start := bytes.Index(l.data[l.at:], []byte("ID")); start >= 0 {
		l.at += start + 2
	}
	for l.at < len(l.data) {
		end := bytes.Index(l.data[l.at:], []byte("EI"))
		if end < 0 {
			l.at = len(l.data)
			return
		}
		l.at += end + 2
		before, after := l.data[l.at-3], byte(' ')
		if l.at < len(l.data) {
			after = l.data[l.at]
		}
		if isPDFSpace(before) && isPDFSpace(after) {
			return
		}
	}
}

// pdfFont decodes a font's codes into text and their widths.
type pdfFont struct {
	// twoByte fonts, Type0 ones, use codes of two bytes unless their
	// ToUnicode map declares other code spaces.
	twoByte   bool
	spaces    []codeSpace
	toUnicode map[string]string
	ranges    []codeRange
	fallback  pdf.TextEncoding
	widths    map[int]float64
	// missing is the width, in thousandths of the text size, of a code that
	// widths does not name.
	missing float64
}

type codeSpace struct{ low, high string }

type codeRange struct {
	low, high string
	// destination is the text of low, whose last character the following
	// codes increment; or targets lists the text of each code.
	destination string
	targets     []string
}

// loadFont reads a font's ToUnicode map, its fallback encoding and its widths.
func loadFont(value pdf.Value) *pdfFont {
	font := &pdfFont{twoByte: value.Key("Subtype").Name() == "Type0", toUnicode: map[string]string{}, widths: map[int]float64{}, missing: 500}
	library := pdf.Font{V: value}
	font.fallback = library.Encoder()
	if toUnicode := value.Key("ToUnicode"); toUnicode.Kind() == pdf.Stream {
		font.readCMap(readStream(toUnicode, maxCMapBytes))
	}
	if font.twoByte {
		descendant := value.Key("DescendantFonts").Index(0)
		font.missing = 1000
		if width := descendant.Key("DW"); width.Kind() != pdf.Null {
			font.missing = number(width)
		}
		entries := descendant.Key("W")
		for at := 0; at+1 < entries.Len() && len(font.widths) < maxCMapEntries; {
			first := int(number(entries.Index(at)))
			if list := entries.Index(at + 1); list.Kind() == pdf.Array {
				for offset := 0; offset < list.Len() && len(font.widths) < maxCMapEntries; offset++ {
					font.widths[first+offset] = number(list.Index(offset))
				}
				at += 2
				continue
			}
			if at+2 >= entries.Len() {
				break
			}
			for code := first; code <= int(number(entries.Index(at+1))) && len(font.widths) < maxCMapEntries; code++ {
				font.widths[code] = number(entries.Index(at + 2))
			}
			at += 3
		}
		return font
	}
	if width := value.Key("FontDescriptor").Key("MissingWidth"); number(width) > 0 {
		font.missing = number(width)
	}
	widths := value.Key("Widths")
	for at, first := 0, int(number(value.Key("FirstChar"))); at < widths.Len() && at < 256; at++ {
		font.widths[first+at] = number(widths.Index(at))
	}
	return font
}

// readCMap reads the code spaces and mappings of a ToUnicode CMap: the
// strings before each endcodespacerange, endbfchar and endbfrange.
func (f *pdfFont) readCMap(data []byte) {
	lexer := pdfLexer{data: data}
	var operands []operand
	for {
		value, operator, ok := lexer.next()
		switch {
		case !ok:
			return
		case operator == "":
			if len(operands) < 3*maxCMapEntries {
				operands = append(operands, value)
			}
			continue
		case operator == "endcodespacerange":
			for at := 0; at+1 < len(operands); at += 2 {
				low, high := operands[at].text, operands[at+1].text
				if low != "" && len(low) == len(high) && len(low) <= 4 {
					f.spaces = append(f.spaces, codeSpace{low, high})
				}
			}
		case operator == "endbfchar":
			for at := 0; at+1 < len(operands) && len(f.toUnicode) < maxCMapEntries; at += 2 {
				f.toUnicode[operands[at].text] = utf16Text(operands[at+1].text)
			}
		case operator == "endbfrange":
			for at := 0; at+2 < len(operands) && len(f.ranges) < maxCMapEntries; at += 3 {
				target := operands[at+2]
				mapping := codeRange{low: operands[at].text, high: operands[at+1].text, destination: target.text}
				if target.kind == operandArray {
					mapping.targets = []string{}
					for _, item := range target.items[:min(len(target.items), 256)] {
						mapping.targets = append(mapping.targets, utf16Text(item.text))
					}
				}
				if mapping.low != "" && len(mapping.low) == len(mapping.high) {
					f.ranges = append(f.ranges, mapping)
				}
			}
		}
		operands = operands[:0]
	}
}

// utf16Text decodes a ToUnicode target, UTF-16BE.
func utf16Text(raw string) string {
	units := make([]uint16, 0, len(raw)/2)
	for at := 0; at+1 < len(raw); at += 2 {
		units = append(units, uint16(raw[at])<<8|uint16(raw[at+1]))
	}
	return string(utf16.Decode(units))
}

// decode turns raw into text and its advance in text space units.
func (f *pdfFont) decode(raw string, state *textState) (string, float64) {
	var text strings.Builder
	advance := 0.0
	for len(raw) > 0 {
		length := f.codeLength(raw)
		code := raw[:length]
		raw = raw[length:]
		value := 0
		for _, b := range []byte(code) {
			value = value<<8 | int(b)
		}
		width, found := f.widths[value]
		if !found {
			width = f.missing
		}
		advance += (width/1000*state.size + state.charSpace) * state.scale
		if length == 1 && code == " " {
			advance += state.wordSpace * state.scale
		}
		text.WriteString(f.text(code))
	}
	return normalizeGlyphs(text.String()), advance
}

// codeLength is the length of the code that starts raw.
func (f *pdfFont) codeLength(raw string) int {
	for length := 1; length <= 4 && length <= len(raw); length++ {
		for _, space := range f.spaces {
			if len(space.low) == length && raw[:length] >= space.low && raw[:length] <= space.high {
				return length
			}
		}
	}
	if f.twoByte && len(raw) >= 2 {
		return 2
	}
	return 1
}

// text is the text of one code: its ToUnicode mapping, or for a simple font
// its encoding's. A two-byte code without a mapping names a glyph, not text.
func (f *pdfFont) text(code string) string {
	if mapped, found := f.toUnicode[code]; found {
		return mapped
	}
	for _, mapping := range f.ranges {
		if len(code) != len(mapping.low) || code < mapping.low || code > mapping.high {
			continue
		}
		offset := int(code[len(code)-1]) - int(mapping.low[len(mapping.low)-1])
		if mapping.targets != nil {
			if offset < len(mapping.targets) {
				return mapping.targets[offset]
			}
			return ""
		}
		destination := []byte(mapping.destination)
		if len(destination) == 0 {
			return ""
		}
		destination[len(destination)-1] += byte(offset)
		return utf16Text(string(destination))
	}
	if f.twoByte {
		return ""
	}
	return f.fallback.Decode(code)
}

// glyphNames spell out typographic ligatures and drop soft hyphens, NULs and
// the replacement character of codes without text.
var glyphNames = strings.NewReplacer("ﬀ", "ff", "ﬁ", "fi", "ﬂ", "fl", "ﬃ", "ffi", "ﬄ", "ffl", "ﬅ", "st", "ﬆ", "st",
	"­", "", "�", "", "\x00", "")

func normalizeGlyphs(text string) string { return cleanText(glyphNames.Replace(text)) }
