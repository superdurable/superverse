// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectormanifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
)

// A Connector release declares the fields of a connection's configuration,
// of each authentication method's configuration and credentials, and of each
// trigger binding. Configuration values are ordinary JSON of the declared type;
// credential values may be secret. A required string is never blank.

// NormalizeConfigurationFields validates ordinary configuration values against
// their declared fields and rewrites values in place: a duration string
// becomes integer nanoseconds, which Go applications decode strictly. Writers
// store values only after normalizing them. A URL is https, or http too where
// plainHTTP admits private-network endpoints (local profiles), which the
// provider client's address policy then confines to private addresses.
func NormalizeConfigurationFields(fields []ManifestField, values map[string]json.RawMessage, plainHTTP bool) error {
	declared := make(map[string]ManifestField, len(fields))
	for _, field := range fields {
		if field.Type == fieldTypeSecretString {
			return errors.New("ordinary configuration cannot declare credential fields")
		}
		declared[field.Name] = field
		if field.Required && field.Default == nil && len(values[field.Name]) == 0 {
			return fmt.Errorf("required configuration field %q is missing", field.Name)
		}
	}
	for name, contents := range values {
		field, exists := declared[name]
		if !exists {
			return fmt.Errorf("configuration field %q is not declared", name)
		}
		decoder := json.NewDecoder(bytes.NewReader(contents))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil || value == nil {
			return fmt.Errorf("configuration field %q has invalid JSON", name)
		}
		normalized, err := configurationValue(field, value, plainHTTP)
		if err != nil {
			return fmt.Errorf("configuration field %q: %w", name, err)
		}
		if normalized != nil {
			values[name] = normalized
		}
	}
	return nil
}

// configurationValue checks value against field's type. It returns the
// encoding that replaces a duration string, or nil.
func configurationValue(field ManifestField, value any, plainHTTP bool) (json.RawMessage, error) {
	switch field.Type {
	case fieldTypeString, fieldTypeURL, fieldTypeEnum:
		text, isText := value.(string)
		if !isText || field.Required && strings.TrimSpace(text) == "" {
			return nil, errors.New("requires a non-blank string")
		}
		if field.Type == fieldTypeEnum && !slices.Contains(field.Enum, text) {
			return nil, errors.New("choice is not declared")
		}
		if field.Type == fieldTypeURL {
			target, err := url.Parse(text)
			if err != nil || !(target.Scheme == "https" || plainHTTP && target.Scheme == "http") || target.Host == "" || target.User != nil {
				if plainHTTP {
					return nil, errors.New("URL must be absolute HTTP or HTTPS")
				}
				return nil, errors.New("URL must be absolute HTTPS")
			}
		}
	case fieldTypeDuration:
		switch typed := value.(type) {
		case string:
			duration, err := time.ParseDuration(typed)
			if err != nil {
				return nil, errors.New("duration is invalid")
			}
			return comm.Must(json.Marshal(int64(duration))), nil
		case json.Number:
			if _, err := typed.Int64(); err != nil {
				return nil, errors.New("duration must be integer nanoseconds")
			}
		default:
			return nil, errors.New("duration is invalid")
		}
	case fieldTypeInteger:
		number, isNumber := value.(json.Number)
		if !isNumber {
			return nil, errors.New("requires an integer")
		}
		if _, err := number.Int64(); err != nil {
			return nil, errors.New("requires an integer")
		}
	case fieldTypeBoolean:
		if _, isBool := value.(bool); !isBool {
			return nil, errors.New("requires a boolean")
		}
	case fieldTypeStringList:
		entries, isList := value.([]any)
		if !isList {
			return nil, errors.New("requires a string list")
		}
		seen := map[string]bool{}
		for _, entry := range entries {
			text, isText := entry.(string)
			if !isText || len(field.Enum) > 0 && !slices.Contains(field.Enum, text) || field.UniqueItems && seen[text] {
				return nil, errors.New("list contains an invalid or duplicate item")
			}
			seen[text] = true
		}
	case fieldTypeStringMap:
		entries, isMap := value.(map[string]any)
		if !isMap {
			return nil, errors.New("requires a string map")
		}
		for _, entry := range entries {
			if _, isText := entry.(string); !isText {
				return nil, errors.New("requires string map values")
			}
		}
	default:
		return nil, fmt.Errorf("field type %q is unsupported", field.Type)
	}
	return nil, nil
}

// DeclaredSettings keeps the settings that fields declare. A connection's
// settings serve every release of its major version, whose minor releases
// only add optional fields, so a release that predates a field reads the
// settings without it.
func DeclaredSettings(fields []ManifestField, settings map[string]json.RawMessage) map[string]json.RawMessage {
	declared := make(map[string]json.RawMessage, len(settings))
	for _, field := range fields {
		if value, found := settings[field.Name]; found {
			declared[field.Name] = value
		}
	}
	return declared
}

// ValidateCredentialFields validates credential values against the selected
// methods' fields. A field in satisfied needs no value: a credential kept from
// the current one, or one the OAuth exchange supplies.
func ValidateCredentialFields(fields []ManifestField, values map[string]json.RawMessage, satisfied map[string]bool) error {
	declared := make(map[string]ManifestField, len(fields))
	for _, field := range fields {
		declared[field.Name] = field
	}
	for name, value := range values {
		field, exists := declared[name]
		if !exists {
			return fmt.Errorf("credential field %q is not declared", name)
		}
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return fmt.Errorf("credential field %q is invalid", name)
		}
		if field.Type.holdsString() && decoded != nil {
			text, isText := decoded.(string)
			if !isText || field.Required && strings.TrimSpace(text) == "" {
				return fmt.Errorf("credential field %q must be a non-blank string", name)
			}
		}
	}
	for _, field := range fields {
		if field.Required && field.Default == nil && len(values[field.Name]) == 0 && !satisfied[field.Name] {
			return fmt.Errorf("required credential field %q is missing", field.Name)
		}
	}
	return nil
}

// ValidateUseConfiguration admits a connector factory Step's use
// configuration: every value sits at a JSON pointer one of the Flow's Connector
// UI unit bindings declares, or inside an object on the way to one.
func ValidateUseConfiguration(configuration map[string]json.RawMessage, configurationUI fdg.V2ConnectorConfigurationUI) error {
	allowed := make(map[string]bool)
	for _, unit := range configurationUI.Units {
		for _, binding := range unit.Bindings {
			allowed[binding.JSONPointer] = true
		}
	}
	for key, raw := range configuration {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return errors.New("connector configuration value is invalid")
		}
		if err := validateUseValue(value, "/"+escapeJSONPointer(key), allowed); err != nil {
			return err
		}
	}
	return nil
}

// MissingRequiredUnits lists, in declaration order, each required Connector UI
// unit of configurationUI with a binding whose value in configuration is absent,
// null, a blank string, an empty list or an empty object. Each unit saves on
// its own, so a saved use configuration may still miss a required unit.
func MissingRequiredUnits(configuration map[string]json.RawMessage, configurationUI fdg.V2ConnectorConfigurationUI) []fdg.V2ConnectorUIUnit {
	var missing []fdg.V2ConnectorUIUnit
	for _, unit := range configurationUI.Units {
		if unit.Required && slices.ContainsFunc(unit.Bindings, func(binding fdg.V2ConnectorUIBinding) bool {
			return !hasValue(configuration, binding.JSONPointer)
		}) {
			missing = append(missing, unit)
		}
	}
	return missing
}

// hasValue reports whether pointer selects a value in configuration that is
// neither null, a blank string, an empty list nor an empty object.
func hasValue(configuration map[string]json.RawMessage, pointer string) bool {
	segments := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for index, segment := range segments {
		segments[index] = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
	}
	raw, exists := configuration[segments[0]]
	var value any
	if !exists || json.Unmarshal(raw, &value) != nil {
		return false
	}
	for _, segment := range segments[1:] {
		object, isObject := value.(map[string]any)
		if !isObject {
			return false
		}
		if value, exists = object[segment]; !exists {
			return false
		}
	}
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	}
	return true
}

func validateUseValue(value any, pointer string, allowed map[string]bool) error {
	object, isObject := value.(map[string]any)
	if !isObject {
		if !allowed[pointer] {
			return fmt.Errorf("connector configuration path %q is not declared by the Flow", pointer)
		}
		return nil
	}
	if !allowed[pointer] && !hasAllowedDescendant(pointer, allowed) {
		return fmt.Errorf("connector configuration path %q is not declared by the Flow", pointer)
	}
	for key, nested := range object {
		if err := validateUseValue(nested, pointer+"/"+escapeJSONPointer(key), allowed); err != nil {
			return err
		}
	}
	return nil
}

func hasAllowedDescendant(pointer string, allowed map[string]bool) bool {
	for path := range allowed {
		if strings.HasPrefix(path, pointer+"/") {
			return true
		}
	}
	return false
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
