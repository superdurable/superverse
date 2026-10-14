// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package comm

import "fmt"

// UnmarshalName reads the persisted name of a value of a closed set, a struct
// that string literals cannot convert to: it sets *value to the declared value
// whose name is text. An empty text is the zero value, and any other text is
// refused.
func UnmarshalName[T comparable](text []byte, value *T, name func(T) string, declared ...T) error {
	var zero T
	if len(text) == 0 {
		*value = zero
		return nil
	}
	for _, candidate := range declared {
		if name(candidate) == string(text) {
			*value = candidate
			return nil
		}
	}
	return fmt.Errorf("unknown %T %q", zero, text)
}
