// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package orca

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

// object is a JSON object of a mutation's fingerprinted fields.
type object = map[string]any

// payloadFingerprint is Orca's envelope fingerprint: the lowercase hex
// SHA-256 of the canonical JSON of {method, sessionId, fields}.
func payloadFingerprint(method, sessionID string, fields object) string {
	sum := sha256.Sum256([]byte(canonical(object{"method": method, "sessionId": sessionID, "fields": fields})))
	return hex.EncodeToString(sum[:])
}

// canonical is Orca's canonical JSON: JavaScript JSON.stringify escaping and
// object keys sorted by UTF-16 code units. Values are what the Bridge builds:
// objects, arrays, strings, booleans, integers and nil.
func canonical(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return quote(typed)
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case []any:
		parts := make([]string, len(typed))
		for i, element := range typed {
			parts[i] = canonical(element)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case object:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		slices.SortFunc(keys, compareUTF16)
		parts := make([]string, len(keys))
		for i, key := range keys {
			parts[i] = quote(key) + ":" + canonical(typed[key])
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		panic(fmt.Sprintf("canonical JSON of %T", value))
	}
}

// quote is JSON.stringify of a string: only the quote, the backslash and
// control characters are escaped, the latter with lowercase hex.
func quote(text string) string {
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			quoted.WriteString(`\"`)
		case '\\':
			quoted.WriteString(`\\`)
		case '\b':
			quoted.WriteString(`\b`)
		case '\f':
			quoted.WriteString(`\f`)
		case '\n':
			quoted.WriteString(`\n`)
		case '\r':
			quoted.WriteString(`\r`)
		case '\t':
			quoted.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&quoted, `\u%04x`, r)
			} else {
				quoted.WriteRune(r)
			}
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
}

func compareUTF16(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}
