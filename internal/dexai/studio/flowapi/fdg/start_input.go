// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fdg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm"
)

// StartStepInput validates the Start input against its schema and returns
// the Dex step input with the decoded input that the Start fingerprint covers.
func StartStepInput(schema V2StartInputSchema, rawInput json.RawMessage) (*dexpb.Value, any, error) {
	var value any
	if err := comm.DecodeStrictJSONNumbers(rawInput, &value); err != nil {
		return nil, nil, fmt.Errorf("input is invalid JSON: %w", err)
	}
	if err := validateV2StartInputValue(schema, value, "$"); err != nil {
		return nil, nil, err
	}
	kind := schema.Kind
	if kind == startInputKindObject || kind == startInputKindArray || kind == startInputKindMap {
		// Composite input keeps the caller's exact JSON, even a nullable null.
		return jsonPayloadDexValue(bytes.TrimSpace(rawInput)), value, nil
	}
	if value == nil {
		return NullDexValue(), value, nil
	}
	switch kind {
	case startInputKindString:
		text, _ := value.(string)
		return &dexpb.Value{Kind: &dexpb.Value_StringValue{StringValue: text}}, value, nil
	case startInputKindInteger:
		number, _ := value.(json.Number)
		integer, err := strconv.ParseInt(string(number), 10, 64)
		if err != nil {
			return nil, nil, errors.New("$ must fit the Dex int64 value type")
		}
		return &dexpb.Value{Kind: &dexpb.Value_IntValue{IntValue: integer}}, value, nil
	case startInputKindNumber:
		number, _ := JSONNumberFloat64(value)
		return &dexpb.Value{Kind: &dexpb.Value_DoubleValue{DoubleValue: number}}, value, nil
	default:
		boolean, _ := value.(bool)
		return &dexpb.Value{Kind: &dexpb.Value_BoolValue{BoolValue: boolean}}, value, nil
	}
}

// validateV2StartInputValue checks value against schema. A value nests only
// as deep as its schema, whose depth load-time validation bounded.
func validateV2StartInputValue(schema V2StartInputSchema, value any, path string) error {
	kind := schema.Kind
	if value == nil {
		if kind == startInputKindNull || schema.Nullable {
			return nil
		}
		return fmt.Errorf("%s must not be null", path)
	}
	switch kind {
	case startInputKindNull:
		return fmt.Errorf("%s must be null", path)
	case startInputKindString:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if schema.Format == startInputFormatDateTime {
			if _, err := time.Parse(time.RFC3339, text); err != nil {
				return fmt.Errorf("%s must be an RFC3339 datetime", path)
			}
		}
		if !matchesV2StartStringEnum(text, schema.EnumValues) {
			return fmt.Errorf("%s must be one of the declared enum values", path)
		}
	case startInputKindInteger:
		number, ok := value.(json.Number)
		if !ok || !integerJSONPattern.MatchString(string(number)) {
			return fmt.Errorf("%s must be an integer", path)
		}
		// The pattern admits only decimal integers, and load-time validation
		// checked the range bounds.
		integerValue, _ := new(big.Int).SetString(string(number), 10)
		minimum, _ := new(big.Int).SetString(schema.Minimum, 10)
		maximum, _ := new(big.Int).SetString(schema.Maximum, 10)
		if integerValue.Cmp(minimum) < 0 || integerValue.Cmp(maximum) > 0 {
			return fmt.Errorf("%s is outside the allowed integer range", path)
		}
		if !matchesV2StartIntegerEnum(integerValue, schema.EnumValues) {
			return fmt.Errorf("%s must be one of the declared enum values", path)
		}
	case startInputKindNumber:
		if _, finite := JSONNumberFloat64(value); !finite {
			return fmt.Errorf("%s must be a finite number", path)
		}
	case startInputKindBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case startInputKindObject:
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		fields := make(map[string]v2StartInputField, len(schema.Fields))
		for _, field := range schema.Fields {
			fields[field.Name] = field
			fieldValue, exists := object[field.Name]
			if field.Required && !exists {
				return fmt.Errorf("%s.%s is required", path, field.Name)
			}
			if exists {
				if err := validateV2StartInputValue(field.Schema, fieldValue, path+"."+field.Name); err != nil {
					return err
				}
			}
		}
		for fieldName := range object {
			if _, exists := fields[fieldName]; !exists {
				return fmt.Errorf("%s.%s is unknown", path, fieldName)
			}
		}
	case startInputKindArray:
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if schema.FixedLength != nil && int64(len(items)) != *schema.FixedLength {
			return fmt.Errorf("%s must contain exactly %d items", path, *schema.FixedLength)
		}
		for index, item := range items {
			if err := validateV2StartInputValue(*schema.Items, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case startInputKindMap:
		entries, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object map", path)
		}
		for key, entry := range entries {
			if key == "" {
				return fmt.Errorf("%s map keys must not be empty", path)
			}
			if err := validateV2StartInputValue(*schema.Values, entry, path+"["+strconv.Quote(key)+"]"); err != nil {
				return err
			}
		}
	default:
		panic("validated Start input schema has unsupported kind " + string(schema.Kind))
	}
	return nil
}

func matchesV2StartStringEnum(value string, options []v2StartInputEnumValue) bool {
	if len(options) == 0 {
		return true
	}
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

// matchesV2StartIntegerEnum compares value with the enum's decimal strings,
// which load-time validation parsed.
func matchesV2StartIntegerEnum(value *big.Int, options []v2StartInputEnumValue) bool {
	if len(options) == 0 {
		return true
	}
	for _, option := range options {
		text, _ := option.Value.(string)
		if optionValue, _ := new(big.Int).SetString(text, 10); value.Cmp(optionValue) == 0 {
			return true
		}
	}
	return false
}
