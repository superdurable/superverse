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
