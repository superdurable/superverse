// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package comm

import (
	"strings"
	"unicode/utf8"
)

// TruncateUTF8 cuts value to at most maximumBytes without splitting a UTF-8
// character; invalid bytes are replaced first.
func TruncateUTF8(value string, maximumBytes int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maximumBytes {
		return value
	}
	cut := maximumBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

// TailUTF8 keeps the last maximumBytes of value without splitting a UTF-8
// character; invalid bytes are replaced first. A failing process explains
// itself at the end of its output.
func TailUTF8(value string, maximumBytes int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maximumBytes {
		return value
	}
	cut := len(value) - maximumBytes
	for cut < len(value) && !utf8.RuneStart(value[cut]) {
		cut++
	}
	return value[cut:]
}
