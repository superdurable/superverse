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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// The bounds of a JSON file: its size, its nesting and its number of values.
const (
	MaxJSONBytes  = 4 << 20
	maxJSONDepth  = 64
	maxJSONValues = 200_000
)

// errJSONBounds reports a JSON file beyond its bounds.
var errJSONBounds = errors.New("the JSON exceeds its bounds")

// jsonKind is the type of a JSON value.
type jsonKind int

const (
	jsonObject jsonKind = iota
	jsonArray
	jsonString
	// jsonLiteral is a number, true, false or null, kept as written.
	jsonLiteral
)

// jsonValue is a parsed JSON value that keeps the order of an object's keys,
// so the redacted file reads like the original.
type jsonValue struct {
	kind jsonKind
	// text is a string's value or a literal as written.
	text string
	// keys are an object's keys, in order; items its values or an array's.
	keys  []string
	items []*jsonValue
}

// parseJSON parses data, a UTF-8 JSON file, within the bounds. A syntax error
// is invalid_json; a file beyond the bounds errJSONBounds.
func parseJSON(data []byte) (*jsonValue, int, int, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(data) {
		return nil, 0, 0, errors.New("the file is not UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	parser := jsonParser{decoder: decoder}
	root, err := parser.value(0)
	if err != nil {
		return nil, 0, 0, err
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, 0, 0, errors.New("the file has data after its JSON value")
	}
	return root, parser.values, parser.depth, nil
}

type jsonParser struct {
	decoder       *json.Decoder
	values, depth int
}

// value reads the next value at depth.
func (p *jsonParser) value(depth int) (*jsonValue, error) {
	if p.values++; p.values > maxJSONValues {
		return nil, fmt.Errorf("%w: more than %d values", errJSONBounds, maxJSONValues)
	}
	// depth counts from 0, so a value at depth d is d+1 levels deep.
	if depth >= maxJSONDepth {
		return nil, fmt.Errorf("%w: nested deeper than %d levels", errJSONBounds, maxJSONDepth)
	}
	p.depth = max(p.depth, depth+1)
	token, err := p.decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token := token.(type) {
	case json.Delim:
		value := &jsonValue{kind: jsonArray}
		if token == '{' {
			value.kind = jsonObject
		}
		for p.decoder.More() {
			if value.kind == jsonObject {
				key, err := p.decoder.Token()
				if err != nil {
					return nil, err
				}
				// The decoder answers only a string where an object key belongs.
				value.keys = append(value.keys, key.(string)) //nolint:forcetypeassert // a key token is a string
			}
			item, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			value.items = append(value.items, item)
		}
		if _, err = p.decoder.Token(); err != nil {
			return nil, err
		}
		return value, nil
	case string:
		return &jsonValue{kind: jsonString, text: token}, nil
	case json.Number:
		return &jsonValue{kind: jsonLiteral, text: token.String()}, nil
	case bool:
		return &jsonValue{kind: jsonLiteral, text: strconv.FormatBool(token)}, nil
	default:
		return &jsonValue{kind: jsonLiteral, text: "null"}, nil
	}
}

// field is the value of key in an object, or nil.
func (v *jsonValue) field(key string) *jsonValue {
	if v == nil || v.kind != jsonObject {
		return nil
	}
	for index, name := range v.keys {
		if name == key {
			return v.items[index]
		}
	}
	return nil
}

// stringField is the string value of key in an object, or "".
func (v *jsonValue) stringField(key string) string {
	if field := v.field(key); field != nil && field.kind == jsonString {
		return field.text
	}
	return ""
}

// appendCompact appends v as compact JSON without HTML escaping.
func (v *jsonValue) appendCompact(out []byte) []byte {
	switch v.kind {
	case jsonObject:
		out = append(out, '{')
		for index, key := range v.keys {
			if index > 0 {
				out = append(out, ',')
			}
			out = append(appendJSONString(out, key), ':')
			out = v.items[index].appendCompact(out)
		}
		return append(out, '}')
	case jsonArray:
		out = append(out, '[')
		for index, item := range v.items {
			if index > 0 {
				out = append(out, ',')
			}
			out = item.appendCompact(out)
		}
		return append(out, ']')
	case jsonString:
		return appendJSONString(out, v.text)
	default:
		return append(out, v.text...)
	}
}

// appendJSONString appends text as a JSON string, escaping only what JSON
// and JavaScript require.
func appendJSONString(out []byte, text string) []byte {
	out = append(out, '"')
	for _, r := range text {
		switch {
		case r == '"':
			out = append(out, '\\', '"')
		case r == '\\':
			out = append(out, '\\', '\\')
		case r == '\n':
			out = append(out, '\\', 'n')
		case r == '\r':
			out = append(out, '\\', 'r')
		case r == '\t':
			out = append(out, '\\', 't')
		case r < 0x20 || r == ' ' || r == ' ':
			out = fmt.Appendf(out, `\u%04x`, r)
		default:
			out = utf8.AppendRune(out, r)
		}
	}
	return append(out, '"')
}
