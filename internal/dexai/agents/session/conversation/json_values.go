// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
)

// JSONObject holds one validated JSON object without dynamic domain maps.
type JSONObject string

// ParseJSONObject validates one complete JSON object.
func ParseJSONObject(encoded string) (JSONObject, error) {
	trimmed := bytes.TrimSpace([]byte(encoded))
	if len(trimmed) == 0 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || !json.Valid(trimmed) {
		return "", errors.New("value must be one valid JSON object")
	}
	return JSONObject(trimmed), nil
}

// MustJSONObject returns a JSON object compiled into the binary.
func MustJSONObject(encoded string) JSONObject {
	return comm.Must(ParseJSONObject(encoded))
}

// String returns the encoded object.
func (object JSONObject) String() string {
	return string(object)
}

// MarshalJSON writes the object as JSON, not a quoted string. Every JSONObject
// was validated when it was parsed.
func (object JSONObject) MarshalJSON() ([]byte, error) {
	return []byte(object), nil
}

// UnmarshalJSON validates and stores one object.
func (object *JSONObject) UnmarshalJSON(data []byte) error {
	validated, err := ParseJSONObject(string(data))
	if err != nil {
		return err
	}
	*object = validated
	return nil
}

// DecodeEnum decodes a persisted or external enum value and rejects unknown ones.
func DecodeEnum[T ~string](data []byte, destination *T, validate func(T) error) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if err := validate(T(value)); err != nil {
		return err
	}
	*destination = T(value)
	return nil
}

// NewEnumValidationError reports an unknown value of an enum type.
func NewEnumValidationError(typeName string, value string) error {
	return fmt.Errorf("invalid %s value %q", typeName, value)
}

// partialJSONState is what a partial JSON text expects next.
type partialJSONState int

const (
	expectValue partialJSONState = iota
	expectValueOrEnd
	expectKeyOrEnd
	expectKey
	expectColon
	expectCommaOrEnd
	expectNothing
)

// ClosePartialJSON completes a JSON object or array that a writer cut off,
// such as a tool input a model is still streaming. An open string value is
// closed where it was cut, without a trailing partial escape; an unfinished
// key, number or literal is dropped back to the last complete value, with a
// comma before it; and every open array and object is closed. Text after a
// complete top-level value is ignored. It reports false when partial does not
// start with an object or array, holds nothing complete yet, or is not valid
// JSON up to its cut. The scan is linear in partial.
func ClosePartialJSON(partial string) (string, bool) {
	var stack []byte
	state, safe, safeStack := expectValue, -1, ""
	mark := func(at int) { safe, safeStack = at, string(stack) }
	for index := 0; index < len(partial); {
		character := partial[index]
		if character == ' ' || character == '\t' || character == '\n' || character == '\r' {
			index++
			continue
		}
		switch state {
		case expectNothing:
			return closedAt(partial, safe, safeStack)
		case expectColon:
			if character != ':' {
				return "", false
			}
			state, index = expectValue, index+1
		case expectKeyOrEnd, expectKey:
			switch {
			case character == '}' && state == expectKeyOrEnd:
				stack, index = stack[:len(stack)-1], index+1
				state = afterValue(stack)
				mark(index)
			case character == '"':
				end, isClosed := scanJSONString(partial, index)
				if !isClosed {
					return closedAt(partial, safe, safeStack)
				}
				state, index = expectColon, end
			default:
				return "", false
			}
		case expectCommaOrEnd:
			top := stack[len(stack)-1]
			switch {
			case character == ',' && top == '{':
				state, index = expectKey, index+1
			case character == ',':
				state, index = expectValue, index+1
			case character == '}' && top == '{', character == ']' && top == '[':
				stack, index = stack[:len(stack)-1], index+1
				state = afterValue(stack)
				mark(index)
			default:
				return "", false
			}
		case expectValueOrEnd, expectValue:
			switch {
			case character == ']' && state == expectValueOrEnd:
				stack, index = stack[:len(stack)-1], index+1
				state = afterValue(stack)
				mark(index)
			case character == '{' || character == '[':
				stack, index = append(stack, character), index+1
				state = expectKeyOrEnd
				if character == '[' {
					state = expectValueOrEnd
				}
				mark(index)
			case len(stack) == 0:
				return "", false
			case character == '"':
				end, isClosed := scanJSONString(partial, index)
				if !isClosed {
					if closed, isValid := completeJSON(partial[:end]+`"`, string(stack)); isValid {
						return closed, true
					}
					return closedAt(partial, safe, safeStack)
				}
				state, index = expectCommaOrEnd, end
				mark(index)
			default:
				end := index
				for end < len(partial) && !strings.ContainsRune(",]} \t\n\r", rune(partial[end])) {
					end++
				}
				if end == len(partial) {
					return closedAt(partial, safe, safeStack)
				}
				if !json.Valid([]byte(partial[index:end])) {
					return "", false
				}
				state, index = expectCommaOrEnd, end
				mark(index)
			}
		}
	}
	return closedAt(partial, safe, safeStack)
}

// afterValue is what follows a complete value inside stack.
func afterValue(stack []byte) partialJSONState {
	if len(stack) == 0 {
		return expectNothing
	}
	return expectCommaOrEnd
}

// scanJSONString reads the string that starts at the quote at start. It
// returns the index after its closing quote, or, for a string cut off, the
// index where it can be closed: the end, or the start of a partial escape.
func scanJSONString(text string, start int) (int, bool) {
	for index := start + 1; index < len(text); index++ {
		switch text[index] {
		case '"':
			return index + 1, true
		case '\\':
			width := 2
			if index+1 < len(text) && text[index+1] == 'u' {
				width = 6
			}
			if index+width > len(text) {
				return index, false
			}
			index += width - 1
		}
	}
	return len(text), false
}

// closedAt is the text up to the last complete value with its containers
// closed, or false when nothing is complete yet.
func closedAt(partial string, safe int, stack string) (string, bool) {
	if safe < 0 {
		return "", false
	}
	return completeJSON(partial[:safe], stack)
}

// completeJSON closes the containers of stack after text and checks the result.
func completeJSON(text, stack string) (string, bool) {
	var closed strings.Builder
	closed.Grow(len(text) + len(stack))
	closed.WriteString(text)
	for index := len(stack) - 1; index >= 0; index-- {
		if stack[index] == '{' {
			closed.WriteByte('}')
		} else {
			closed.WriteByte(']')
		}
	}
	if !json.Valid([]byte(closed.String())) {
		return "", false
	}
	return closed.String(), true
}
