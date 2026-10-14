// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package accounts

import (
	"net/mail"
	"strings"
)

// canonicalEmail is the account identity of a caller-supplied email: trimmed,
// lowercase printable ASCII in plain addr-spec form. Any other value is
// ErrInvalidEmail.
func canonicalEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	for _, character := range value {
		if character > 127 || character < 33 {
			return "", ErrInvalidEmail
		}
	}
	value = strings.ToLower(value)
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || parsed.Name != "" || len(value) > 254 || strings.ContainsAny(value, "/$:") {
		return "", ErrInvalidEmail
	}
	return value, nil
}

// accountKey is the account name a login token signs, for example
// account-alice@example.com.
func accountKey(canonicalEmail string) string {
	return "account-" + canonicalEmail
}
