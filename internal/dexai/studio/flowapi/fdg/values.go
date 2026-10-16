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
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm"
	"google.golang.org/protobuf/types/known/structpb"
)

// Values reach this file as decoded JSON (string, bool, json.Number, []any,
// map[string]any) or as Dex values that flowapi mapped (int64 and float64).

// ValidateViewOutput checks a Summary or Display RPC output against its view.
func ValidateViewOutput(view V2RPCView, values map[string]any) error {
	if len(values) != len(view.Fields) {
		return fmt.Errorf("%s returned %d fields; contract declares %d", view.RPCName, len(values), len(view.Fields))
	}
	for _, field := range view.Fields {
		value, exists := values[field.AttributeKey]
		if !exists {
			return fmt.Errorf("%s omitted declared field %q", view.RPCName, field.AttributeKey)
		}
		if value != nil && !isV2ValueType(value, field.ValueType) {
			return fmt.Errorf("%s field %q does not match %s", view.RPCName, field.AttributeKey, field.ValueType)
		}
	}
	return nil
}

// ResponseValue renders an int64 value as a decimal string, which JavaScript
// reads without losing precision.
func ResponseValue(value any, typeName valueType) any {
	if value == nil || typeName != valueTypeInt64 {
		return value
	}
	integer, isInteger := JSONNumberInt64(value)
	if !isInteger {
		return value
	}
	return strconv.FormatInt(integer, 10)
}

// ResponseSnapshot renders every int64 Attribute value as a decimal string.
func ResponseSnapshot(snapshot map[string]any) map[string]any {
	response := make(map[string]any, len(snapshot))
	for key, value := range snapshot {
		if integer, isInteger := value.(int64); isInteger {
			response[key] = strconv.FormatInt(integer, 10)
			continue
		}
		response[key] = value
	}
	return response
}

func isV2ValueType(value any, kind valueType) bool {
	switch kind {
	case valueTypeString:
		_, isText := value.(string)
		return isText
	case valueTypeDatetime:
		text, isText := value.(string)
		if !isText {
			return false
		}
		_, err := time.Parse(time.RFC3339Nano, text)
		return err == nil
	case valueTypeInt64:
		_, isInteger := JSONNumberInt64(value)
		return isInteger
	case valueTypeDouble:
		_, isFinite := JSONNumberFloat64(value)
		return isFinite
	case valueTypeBool:
		_, isBool := value.(bool)
		return isBool
	case valueTypeStringArray:
		values, isArray := value.([]any)
		if !isArray {
			return false
		}
		for _, item := range values {
			if _, isText := item.(string); !isText {
				return false
			}
		}
		return true
	case valueTypeObject, valueTypeAttributeMap:
		_, isObject := value.(map[string]any)
		return isObject
	case valueTypeArray:
		_, isArray := value.([]any)
		return isArray
	case valueTypeJSON:
		return true
	default:
		return false
	}
}

// RequestValue returns a user-supplied value as the value type typeName: an
// int64, which may also arrive in the decimal string form ResponseValue
// renders, or the value itself. It reports false when the value does not match.
func RequestValue(value any, typeName valueType) (any, bool) {
	if typeName == valueTypeInt64 {
		integer, isInteger := v2RequestInt64(value)
		return integer, isInteger
	}
	return value, isV2ValueType(value, typeName)
}

// EncodeValue encodes a user-supplied value as the Dex value of the value type
// typeName.
func EncodeValue(value any, typeName valueType) (*dexpb.Value, error) {
	typed, matches := RequestValue(value, typeName)
	if !matches {
		return nil, fmt.Errorf("value does not match %s", typeName)
	}
	// RequestValue matched typed to the value type.
	switch typeName {
	case valueTypeInt64:
		integer, _ := typed.(int64)
		return &dexpb.Value{Kind: &dexpb.Value_IntValue{IntValue: integer}}, nil
	case valueTypeString, valueTypeDatetime:
		text, _ := typed.(string)
		return &dexpb.Value{Kind: &dexpb.Value_StringValue{StringValue: text}}, nil
	case valueTypeDouble:
		number, _ := JSONNumberFloat64(typed)
		return &dexpb.Value{Kind: &dexpb.Value_DoubleValue{DoubleValue: number}}, nil
	case valueTypeBool:
		boolean, _ := typed.(bool)
		return &dexpb.Value{Kind: &dexpb.Value_BoolValue{BoolValue: boolean}}, nil
	default:
		return JSONDexValue(typed), nil
	}
}

// IndexType returns the Dex index type of a validated index type.
func IndexType(name indexType) dexpb.IndexType {
	mapped, known := v2IndexTypes[name]
	if !known {
		panic("validated index type " + name + " has no Dex index type")
	}
	return mapped.dex
}

func JSONNumberInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case json.Number:
		integer, err := number.Int64()
		return integer, err == nil
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number ||
			number < math.MinInt64 || number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}

// v2RequestInt64 also accepts the decimal string form ResponseValue renders.
func v2RequestInt64(value any) (int64, bool) {
	if text, isText := value.(string); isText {
		integer, err := strconv.ParseInt(text, 10, 64)
		return integer, err == nil
	}
	return JSONNumberInt64(value)
}

func JSONNumberFloat64(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case int64:
		number = float64(typed)
	case float64:
		number = typed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func NullDexValue() *dexpb.Value {
	return &dexpb.Value{Kind: &dexpb.Value_NullValue{NullValue: structpb.NullValue_NULL_VALUE}}
}

// JSONDexValue encodes decoded JSON as a JSON-encoded Dex object value.
func JSONDexValue(value any) *dexpb.Value {
	return jsonPayloadDexValue(comm.Must(json.Marshal(value)))
}

// jsonObjectEncoding is the encoding that Dex names for an object value of JSON
// text. The Dex SDK declares it unexported.
const jsonObjectEncoding = "json"

func jsonPayloadDexValue(payload []byte) *dexpb.Value {
	return &dexpb.Value{Kind: &dexpb.Value_ObjValue{ObjValue: &dexpb.EncodedObject{Encoding: jsonObjectEncoding, Payload: payload}}}
}
