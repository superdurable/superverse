// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fdg

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// validateV2Definition is the load-time trust boundary of a V2 contract. A
// graph reaches the server from the application's repository, so nothing here
// assumes the analyzer wrote it; the request handlers rely on every rule below.
func validateV2Definition(definition V2Definition) error {
	if definition.Start != nil {
		if err := validateV2StartDefinition(*definition.Start); err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}
	indexedKeys := make(map[string]bool, len(definition.IndexedAttributes))
	indexKeys := make(map[string]bool, len(definition.IndexedAttributes))
	for _, attribute := range definition.IndexedAttributes {
		if attribute.AttributeKey == "" || !v2IndexKeyPattern.MatchString(attribute.IndexKey) ||
			v2IndexTypes[attribute.IndexType].indexes != attribute.ValueType {
			return fmt.Errorf("indexed Attribute %q has invalid metadata", attribute.AttributeKey)
		}
		if indexedKeys[attribute.AttributeKey] || indexKeys[attribute.IndexKey] {
			return errors.New("indexed Attribute or index key is repeated")
		}
		indexedKeys[attribute.AttributeKey] = true
		indexKeys[attribute.IndexKey] = true
	}
	if definition.Summary.RPCName != "GetDexSummary" || definition.Display.RPCName != "GetDexDisplay" {
		return errors.New("summary and display must use their fixed RPC names")
	}
	if err := validateV2View(definition.Summary, false, indexedKeys); err != nil {
		return fmt.Errorf("summary: %w", err)
	}
	if err := validateV2View(definition.Display, true, nil); err != nil {
		return fmt.Errorf("display: %w", err)
	}
	actionNames := make(map[string]bool, len(definition.Actions))
	for _, action := range definition.Actions {
		if action.RPCName == "" || strings.TrimSpace(action.Label) == "" || actionNames[action.RPCName] {
			return errors.New("action RPC names and labels must be non-empty and unique")
		}
		actionNames[action.RPCName] = true
		if !v2PermissionPattern.MatchString(action.RequiredPermission) {
			return fmt.Errorf("action %q has an invalid required permission %q", action.RPCName, action.RequiredPermission)
		}
		if action.Condition.AttributeKey == "" || action.Condition.Operator != actionConditionOperatorIn ||
			len(action.Condition.Values) == 0 {
			return fmt.Errorf("action %q has an invalid condition", action.RPCName)
		}
		for _, value := range action.Condition.Values {
			switch typed := value.(type) {
			case string, bool:
			case json.Number:
				// Dex indexes a condition number as an int64 or a finite double.
				if _, finite := JSONNumberFloat64(typed); !finite {
					return fmt.Errorf("action %q condition numbers must be finite", action.RPCName)
				}
			default:
				return fmt.Errorf("action %q condition values must be JSON scalars", action.RPCName)
			}
		}
		if err := validateV2ActionInput(action.Input); err != nil {
			return fmt.Errorf("action %q: %w", action.RPCName, err)
		}
	}
	return nil
}

var (
	v2PermissionPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
	v2IndexKeyPattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
)

func validateV2View(view V2RPCView, canEdit bool, forbiddenKeys map[string]bool) error {
	seen := make(map[string]bool, len(view.Fields))
	claimedUISlots := make(map[uiSlot]string, len(view.Fields))
	for _, field := range view.Fields {
		editable, known := v2ValueTypeEditable[field.ValueType]
		if field.AttributeKey == "" || !known || seen[field.AttributeKey] {
			return fmt.Errorf("field %q has invalid metadata", field.AttributeKey)
		}
		if field.Editable && (!canEdit || !editable) {
			return fmt.Errorf("field %q cannot be editable", field.AttributeKey)
		}
		if forbiddenKeys[field.AttributeKey] {
			return fmt.Errorf("field %q duplicates an indexed Attribute", field.AttributeKey)
		}
		if err := claimV2UISlot(field, claimedUISlots); err != nil {
			return err
		}
		seen[field.AttributeKey] = true
	}
	return nil
}

func claimV2UISlot(field V2ViewField, claimedUISlots map[uiSlot]string) error {
	if field.UISlot == "" {
		return nil
	}
	slot := field.UISlot
	unique, known := v2ViewUISlots[slot]
	if !known {
		return fmt.Errorf("field %q claims unknown UI slot %q", field.AttributeKey, field.UISlot)
	}
	if !unique {
		return nil
	}
	if holder, taken := claimedUISlots[slot]; taken {
		return fmt.Errorf("UI slot %q is claimed by both %q and %q", field.UISlot, holder, field.AttributeKey)
	}
	claimedUISlots[slot] = field.AttributeKey
	return nil
}

func validateV2ActionInput(input V2ActionInput) error {
	kind := input.Kind
	if kind == actionInputKindNone {
		if len(input.Fields) != 0 {
			return errors.New("none input must not declare fields")
		}
		return nil
	}
	if kind != actionInputKindObject || len(input.Fields) == 0 {
		return errors.New("input kind must be none or a non-empty object")
	}
	seen := make(map[string]bool, len(input.Fields))
	for _, field := range input.Fields {
		fieldType, source, capture := field.ValueType, field.Source, field.Capture
		if field.FieldName == "" || !v2ValueTypeEditable[fieldType] || seen[field.FieldName] {
			return fmt.Errorf("input field %q has invalid metadata", field.FieldName)
		}
		switch {
		case source == actionInputSourceAttribute && field.AttributeKey == "":
			return fmt.Errorf("attribute input field %q requires attributeKey", field.FieldName)
		case source == actionInputSourceUser && field.AttributeKey != "":
			return fmt.Errorf("user input field %q forbids attributeKey", field.FieldName)
		case source != actionInputSourceUser && source != actionInputSourceAttribute:
			return fmt.Errorf("input field %q has invalid source", field.FieldName)
		case capture != "" && (capture != actionInputCaptureQRCode || source != actionInputSourceUser || fieldType != valueTypeString):
			return fmt.Errorf("input field %q has invalid capture", field.FieldName)
		}
		seen[field.FieldName] = true
	}
	return nil
}

const maximumV2StartInputDepth = 32

var integerJSONPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)

func validateV2StartDefinition(definition V2StartDefinition) error {
	if strings.TrimSpace(definition.StepType) == "" {
		return errors.New("step type must be non-empty")
	}
	return validateV2StartInputSchema(definition.Input, 0)
}

func validateV2StartInputSchema(schema V2StartInputSchema, depth int) error {
	if depth > maximumV2StartInputDepth {
		return fmt.Errorf("input nesting exceeds %d levels", maximumV2StartInputDepth)
	}
	switch schema.Kind {
	case startInputKindNull:
		if schema.Nullable {
			return errors.New("null schema must not declare nullable")
		}
	case startInputKindString:
		if schema.Format != "" && schema.Format != startInputFormatDateTime {
			return fmt.Errorf("string schema has invalid format %q", schema.Format)
		}
		return validateV2StartInputEnum(schema.EnumValues, startInputKindString, nil, nil)
	case startInputKindInteger:
		minimum, minimumOK := new(big.Int).SetString(schema.Minimum, 10)
		maximum, maximumOK := new(big.Int).SetString(schema.Maximum, 10)
		if !minimumOK || !maximumOK || minimum.Cmp(maximum) > 0 {
			return errors.New("integer schema has invalid range")
		}
		return validateV2StartInputEnum(schema.EnumValues, startInputKindInteger, minimum, maximum)
	case startInputKindNumber, startInputKindBoolean:
		if len(schema.EnumValues) != 0 {
			return fmt.Errorf("%s schema must not declare enum values", schema.Kind)
		}
	case startInputKindObject:
		fieldNames := make(map[string]bool, len(schema.Fields))
		for _, field := range schema.Fields {
			if field.Name == "" || fieldNames[field.Name] {
				return errors.New("object schema has an empty or repeated field name")
			}
			fieldNames[field.Name] = true
			if err := validateV2StartInputSchema(field.Schema, depth+1); err != nil {
				return fmt.Errorf("field %q: %w", field.Name, err)
			}
		}
	case startInputKindArray:
		if schema.Items == nil {
			return errors.New("array schema requires items")
		}
		if schema.FixedLength != nil && *schema.FixedLength < 0 {
			return errors.New("array fixed length must be non-negative")
		}
		if err := validateV2StartInputSchema(*schema.Items, depth+1); err != nil {
			return fmt.Errorf("array items: %w", err)
		}
	case startInputKindMap:
		if schema.Values == nil {
			return errors.New("map schema requires values")
		}
		if err := validateV2StartInputSchema(*schema.Values, depth+1); err != nil {
			return fmt.Errorf("map values: %w", err)
		}
	default:
		return fmt.Errorf("input has unknown kind %q", schema.Kind)
	}
	return nil
}

func validateV2StartInputEnum(values []v2StartInputEnumValue, kind startInputKind, minimum *big.Int, maximum *big.Int) error {
	seenNames := make(map[string]bool, len(values))
	seenValues := make(map[string]bool, len(values))
	for _, option := range values {
		if option.Name == "" || seenNames[option.Name] {
			return fmt.Errorf("%s enum has an empty or repeated name", kind)
		}
		// Integer enum values are decimal strings so that every int64 survives JSON.
		value, isString := option.Value.(string)
		if !isString {
			return fmt.Errorf("%s enum value %q must be a string", kind, option.Name)
		}
		if kind == startInputKindInteger {
			number, parsed := new(big.Int).SetString(value, 10)
			if !integerJSONPattern.MatchString(value) || !parsed || number.Cmp(minimum) < 0 || number.Cmp(maximum) > 0 {
				return fmt.Errorf("integer enum value %q must be an integer inside its range", option.Name)
			}
			value = number.String()
		}
		if seenValues[value] {
			return fmt.Errorf("%s enum repeats value %q", kind, value)
		}
		seenNames[option.Name] = true
		seenValues[value] = true
	}
	return nil
}
