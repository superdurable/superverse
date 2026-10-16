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
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

const (
	// v2WorkQueuePermissionsIndex indexes the Action permissions a Flow's current state awaits.
	v2WorkQueuePermissionsIndex = "DexWorkQueuePermissions"
	// engineWorkflowVisibilityQuery limits a search to Flow executions.
	engineWorkflowVisibilityQuery = `WorkflowType = "Engine"`
	// v2StartPermission is the permission Start awaits; the Inbox includes it.
	v2StartPermission = "flows.start"
)

// SearchFilter selects Flows by one field. Values is a JSON array.
type SearchFilter struct {
	Field, Operator string
	Values          json.RawMessage
}

// filterOperator is the comparison a SearchFilter applies to its field, spelled
// in lower case. An empty operator means eq.
type filterOperator string

const (
	filterOperatorEq       filterOperator = "eq"
	filterOperatorIn       filterOperator = "in"
	filterOperatorGt       filterOperator = "gt"
	filterOperatorGte      filterOperator = "gte"
	filterOperatorLt       filterOperator = "lt"
	filterOperatorLte      filterOperator = "lte"
	filterOperatorContains filterOperator = "contains"
)

// orderedFilterOperators lists the operators that compare order, each with its
// symbol in a visibility query.
var orderedFilterOperators = map[filterOperator]string{
	filterOperatorGt:  ">",
	filterOperatorGte: ">=",
	filterOperatorLt:  "<",
	filterOperatorLte: "<=",
}

// executionField is a built-in field of a Flow execution that a SearchFilter
// can select by, besides the indexed Attributes.
type executionField string

const (
	executionFieldFlowID    executionField = "flowId"
	executionFieldStatus    executionField = "executionStatus"
	executionFieldStartTime executionField = "startTime"
	executionFieldCloseTime executionField = "closeTime"
)

// CompileQuery compiles a search into a Dex visibility query. Field names
// come from the validated contract, and every literal is quoted, so a filter
// value can never change the query's structure.
func CompileQuery(flowType string, workQueuePermissions []string, filters []SearchFilter, definition V2Definition) (string, error) {
	conditions := []string{"FlowType = " + quoteVisibilityString(flowType)}
	if len(workQueuePermissions) > 0 {
		conditions = append(conditions, compileWorkQueuePermissions(workQueuePermissions))
	}
	for _, filter := range filters {
		var values []any
		if err := comm.DecodeStrictJSONNumbers(filter.Values, &values); err != nil {
			return "", fmt.Errorf("filter %q values must be a JSON array", filter.Field)
		}
		if len(values) == 0 {
			return "", fmt.Errorf("filter %q must contain at least one value", filter.Field)
		}
		physicalKey, fieldType, index, known := v2FilterField(filter.Field, definition)
		if !known {
			return "", fmt.Errorf("unknown v2 filter field %q", filter.Field)
		}
		condition, err := compileV2Filter(physicalKey, fieldType, index, filter, values)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, condition)
	}
	return strings.Join(conditions, " AND "), nil
}

// EngineWorkflowSearchQuery limits query to Flow executions.
func EngineWorkflowSearchQuery(query string) string {
	return fmt.Sprintf("(%s) AND (%s)", query, engineWorkflowVisibilityQuery)
}

// InboxPermissions is every permission an Inbox lists Flows for: the Start
// permission and each declared Action permission of the bundle.
func InboxPermissions(bundle *Bundle) []string {
	permissions := []string{v2StartPermission}
	for _, definition := range bundle.Definitions {
		for _, action := range definition.Actions {
			if !slices.Contains(permissions, action.RequiredPermission) {
				permissions = append(permissions, action.RequiredPermission)
			}
		}
	}
	sort.Strings(permissions)
	return permissions
}

// compileWorkQueuePermissions matches Flows that await any of permissions.
func compileWorkQueuePermissions(permissions []string) string {
	fieldExpression := quoteVisibilityField(v2WorkQueuePermissionsIndex)
	parts := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		parts = append(parts, fieldExpression+" = "+quoteVisibilityString(permission))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// v2FilterField returns the index key, value type and index type a filter
// field searches: a built-in execution field or an indexed Attribute.
func v2FilterField(field string, definition V2Definition) (string, valueType, indexType, bool) {
	switch executionField(field) {
	case executionFieldFlowID:
		return "WorkflowId", valueTypeString, indexTypeKeyword, true
	case executionFieldStatus:
		return "ExecutionStatus", valueTypeString, indexTypeKeyword, true
	case executionFieldStartTime:
		return "StartTime", valueTypeDatetime, indexTypeDatetime, true
	case executionFieldCloseTime:
		return "CloseTime", valueTypeDatetime, indexTypeDatetime, true
	}
	for _, attribute := range definition.IndexedAttributes {
		if attribute.AttributeKey == field {
			return attribute.IndexKey, attribute.ValueType, attribute.IndexType, true
		}
	}
	return "", "", "", false
}

func compileV2Filter(physicalKey string, fieldType valueType, index indexType, filter SearchFilter, values []any) (string, error) {
	fieldExpression := quoteVisibilityField(physicalKey)
	operator := filterOperator(strings.ToLower(filter.Operator))
	if operator == "" {
		operator = filterOperatorEq
	}
	if operator == filterOperatorIn || len(values) > 1 {
		if operator != filterOperatorIn && operator != filterOperatorEq {
			return "", fmt.Errorf("filter %q does not support multiple values with %q", filter.Field, operator)
		}
		parts := make([]string, 0, len(values))
		for _, value := range values {
			literal, err := visibilityLiteral(value, fieldType)
			if err != nil {
				return "", fmt.Errorf("filter %q: %w", filter.Field, err)
			}
			parts = append(parts, fieldExpression+" = "+literal)
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil
	}
	literal, err := visibilityLiteral(values[0], fieldType)
	if err != nil {
		return "", fmt.Errorf("filter %q: %w", filter.Field, err)
	}
	switch operator {
	case filterOperatorEq:
		return fieldExpression + " = " + literal, nil
	case filterOperatorGt, filterOperatorGte, filterOperatorLt, filterOperatorLte:
		if index != indexTypeInt && index != indexTypeDouble && index != indexTypeDatetime {
			return "", fmt.Errorf("filter %q does not support ordered comparison", filter.Field)
		}
		return fieldExpression + " " + orderedFilterOperators[operator] + " " + literal, nil
	case filterOperatorContains:
		if index != indexTypeFulltext {
			return "", errors.New("contains requires a fulltext index")
		}
		text, isText := values[0].(string)
		if !isText {
			return "", errors.New("contains requires a string value")
		}
		return fieldExpression + " = " + quoteVisibilityString(text), nil
	default:
		return "", fmt.Errorf("unsupported filter operator %q", filter.Operator)
	}
}

// visibilityLiteral encodes value as a literal of the value type kind, one of
// the index value types the contract admits.
func visibilityLiteral(value any, kind valueType) (string, error) {
	switch kind {
	case valueTypeString, valueTypeStringArray, valueTypeDatetime:
		text, isText := value.(string)
		if !isText {
			return "", errors.New("value must be a string")
		}
		if kind == valueTypeDatetime {
			if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
				return "", errors.New("value must be RFC3339 datetime")
			}
		}
		return quoteVisibilityString(text), nil
	case valueTypeInt64:
		integer, isInteger := v2RequestInt64(value)
		if !isInteger {
			return "", errors.New("value must be an int64")
		}
		return strconv.FormatInt(integer, 10), nil
	case valueTypeDouble:
		number, isFinite := JSONNumberFloat64(value)
		if !isFinite {
			return "", errors.New("value must be a finite number")
		}
		return strconv.FormatFloat(number, 'g', -1, 64), nil
	case valueTypeBool:
		boolean, isBool := value.(bool)
		if !isBool {
			return "", errors.New("value must be a boolean")
		}
		return strconv.FormatBool(boolean), nil
	default:
		panic("validated index value type " + string(kind) + " has no visibility literal")
	}
}

func quoteVisibilityString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// quoteVisibilityField backquotes a field name that is not purely alphanumeric.
func quoteVisibilityField(value string) string {
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' {
			continue
		}
		return "`" + value + "`"
	}
	return value
}
