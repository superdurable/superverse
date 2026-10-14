// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package toolinput

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

// The envelope budget applies before generated decoding/validation. Individual
// string byte/NUL and workspace/provider constraints remain handler-owned.
const maximumToolArgumentsBytes = 32 << 20

func decodeStrictArguments[T any](encoded json.RawMessage) (T, error) {
	var result T
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || len(encoded) > maximumToolArgumentsBytes {
		return result, fmt.Errorf("arguments must contain 1-%d bytes", maximumToolArgumentsBytes)
	}
	if !utf8.Valid(trimmed) {
		return result, errors.New("arguments must be valid UTF-8")
	}
	if trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return result, errors.New("arguments must be one JSON object")
	}
	if err := validateTopLevelArgumentFields(trimmed, jsonFieldNames[T]()); err != nil {
		return result, err
	}
	// json.Unmarshal checks the whole input, trailing values included, before
	// the generated decoder rejects any field the schema does not declare.
	if err := json.Unmarshal(trimmed, &result); err != nil {
		return result, fmt.Errorf("decode arguments: %w", err)
	}
	return result, nil
}

func validateTopLevelArgumentFields(encoded []byte, allowed map[string]struct{}) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("decode arguments: %w", err)
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode argument name: %w", err)
		}
		name, ok := token.(string)
		if !ok {
			return errors.New("argument name must be a string")
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("argument %q appears more than once", name)
		}
		if _, exists := allowed[name]; !exists {
			names := make([]string, 0, len(allowed))
			for allowedName := range allowed {
				names = append(names, allowedName)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown argument %q; valid arguments: %s", name, strings.Join(names, ", "))
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("decode argument %q: %w", name, err)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("argument %q must not be null", name)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("decode arguments: %w", err)
	}
	return nil
}

func jsonFieldNames[T any]() map[string]struct{} {
	typeOfArguments := reflect.TypeOf((*T)(nil)).Elem()
	fields := make(map[string]struct{}, typeOfArguments.NumField())
	for index := 0; index < typeOfArguments.NumField(); index++ {
		name := strings.Split(typeOfArguments.Field(index).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = struct{}{}
		}
	}
	return fields
}
