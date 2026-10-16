// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package appmanifest

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// environmentDeclaration is a source requirement, never a configuration value or secret reference.
type environmentDeclaration struct {
	Name      string   `json:"name"`
	Required  bool     `json:"required"`
	Secret    bool     `json:"secret"`
	MinLength int      `json:"minLength"`
	Enum      []string `json:"enum"`
}

var applicationEnvironmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

// isApplicationEnvironmentName accepts a variable name an application may
// declare: upper case, at most 128 characters and not one that the platform,
// the language runtimes or the network stack own.
func isApplicationEnvironmentName(name string) bool {
	if !applicationEnvironmentName.MatchString(name) {
		return false
	}
	for _, prefix := range []string{"SUPERVERSE_", "DEX_", "AWS_", "LD_", "GO", "GIT_"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	switch name {
	case "PATH", "HOME", "PORT", "HOST", "NODE_OPTIONS", "SSL_CERT_FILE", "SSL_CERT_DIR", "PUBLIC_BASE_URL", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
		return false
	}
	return true
}

// environmentDeclarations validates the declared environment fields and
// returns them as Struct values in canonical order: by name, with sorted enum
// options. The Agent repairs dex-app.yaml from these messages, so each one
// names the field and the rule it broke.
func environmentDeclarations(fields []environmentDeclaration) ([]any, error) {
	if len(fields) > 128 {
		return nil, errors.New("application.environment declares more than 128 fields")
	}
	fields = append([]environmentDeclaration{}, fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	entries := make([]any, 0, len(fields))
	for index, field := range fields {
		name := fmt.Sprintf("application.environment %q", field.Name)
		switch {
		case !isApplicationEnvironmentName(field.Name):
			return nil, fmt.Errorf("%s: the name is reserved or does not match ^[A-Z][A-Z0-9_]{0,127}$", name)
		case index > 0 && fields[index-1].Name == field.Name:
			return nil, fmt.Errorf("%s is declared twice", name)
		case field.MinLength < 0 || field.MinLength > 32768:
			return nil, fmt.Errorf("%s: minLength must be 0 to 32768", name)
		case len(field.Enum) > 128:
			return nil, fmt.Errorf("%s has more than 128 enum values", name)
		case field.Secret && len(field.Enum) > 0:
			return nil, fmt.Errorf("%s: a secret field cannot have enum values", name)
		}
		options := append([]string{}, field.Enum...)
		sort.Strings(options)
		values := make([]any, 0, len(options))
		for optionIndex, value := range options {
			switch {
			case len(value) > 32768:
				return nil, fmt.Errorf("%s has an enum value longer than 32768 bytes", name)
			case !utf8.ValidString(value):
				return nil, fmt.Errorf("%s has an enum value %q that is not valid UTF-8", name, value)
			case strings.ContainsRune(value, '\x00'):
				return nil, fmt.Errorf("%s has an enum value %q that contains NUL", name, value)
			case utf8.RuneCountInString(value) < field.MinLength:
				return nil, fmt.Errorf("%s has an enum value %q shorter than its minLength %d", name, value, field.MinLength)
			case optionIndex > 0 && options[optionIndex-1] == value:
				return nil, fmt.Errorf("%s repeats the enum value %q", name, value)
			}
			values = append(values, value)
		}
		entries = append(entries, map[string]any{"name": field.Name, "required": field.Required, "secret": field.Secret, "minLength": field.MinLength, "enum": values})
	}
	return entries, nil
}
