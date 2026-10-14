// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/studio/flowapi/fdg"
)

// ActionRequest invokes one declared Action of a running Flow. Input and
// AttributeSnapshot are JSON objects: the user's input fields and the
// Attributes the Display returned, which supply Attribute-sourced inputs.
type ActionRequest struct {
	FlowRef
	RPCName           string
	Input             json.RawMessage
	AttributeSnapshot json.RawMessage
}

// InvokeAction invokes an Action whose condition the Flow's current state still meets.
func (f *FlowAPI) InvokeAction(ctx context.Context, request ActionRequest) Document {
	var userInput, attributeSnapshot map[string]any
	if comm.DecodeStrictJSONNumbers(request.Input, &userInput) != nil || comm.DecodeStrictJSONNumbers(request.AttributeSnapshot, &attributeSnapshot) != nil {
		return errorDocument(http.StatusBadRequest, "input and attributeSnapshot must be JSON objects")
	}
	definition, declared := f.bundle.Definitions[request.FlowType]
	if !declared || request.FlowID == "" || request.RPCName == "" {
		return errorDocument(http.StatusBadRequest, "flowType, flowId, and rpcName are required")
	}
	action, declared := findV2Action(definition.Actions, request.RPCName)
	if !declared {
		return errorDocument(http.StatusBadRequest, "rpcName is not a declared Action")
	}
	if err := f.requireActiveFlow(ctx, request.FlowRef); err != nil {
		return grpcErrorDocument(err, "GetFlowSummary")
	}
	conditionAttributes, err := f.client.GetAttributes(ctx, &dexpb.GetAttributesRequest{
		FlowId: request.FlowID, Keys: []string{action.Condition.AttributeKey},
	})
	if err != nil {
		return grpcErrorDocument(err, "GetAttributes")
	}
	if !actionConditionMatches(action.Condition, keyValueMap(conditionAttributes.GetAttributes())[action.Condition.AttributeKey]) {
		return errorDocument(http.StatusConflict, "Action condition no longer matches the current Flow state")
	}
	input, err := buildActionInput(action, userInput, attributeSnapshot)
	if err != nil {
		return errorDocument(http.StatusBadRequest, err.Error())
	}
	result, err := f.client.InvokeRPC(ctx, &dexpb.InvokeRPCRequest{
		FlowId: request.FlowID, RpcName: action.RPCName, Input: input,
		TimeoutSeconds: int32(v2RPCTimeout.Seconds()), RequestId: uuid.NewString(),
	})
	if err != nil {
		return grpcErrorDocument(err, action.RPCName)
	}
	if dexValue(result.GetOutput()) != nil {
		return errorDocument(http.StatusBadGateway, "Action RPC returned a non-null value")
	}
	return JSONDocument(http.StatusOK, map[string]bool{"invoked": true})
}

func findV2Action(actions []fdg.V2Action, rpcName string) (fdg.V2Action, bool) {
	for _, action := range actions {
		if action.RPCName == rpcName {
			return action, true
		}
	}
	return fdg.V2Action{}, false
}

// buildActionInput assembles the Action's JSON object input from the user's
// fields and the Attribute snapshot, checking each against its declared type.
func buildActionInput(action fdg.V2Action, userInput, attributeSnapshot map[string]any) (*dexpb.Value, error) {
	if action.Input.IsNone() {
		if len(userInput) != 0 {
			return nil, errors.New("no-input Action must not receive input")
		}
		return fdg.NullDexValue(), nil
	}
	input := make(map[string]any, len(action.Input.Fields))
	userFields := make(map[string]bool, len(action.Input.Fields))
	for _, field := range action.Input.Fields {
		var value any
		var present bool
		if field.FromAttribute() {
			value, present = attributeSnapshot[field.AttributeKey]
		} else {
			userFields[field.FieldName] = true
			value, present = userInput[field.FieldName]
		}
		if !present || value == nil {
			if field.Required {
				return nil, fmt.Errorf("action input %q is required", field.FieldName)
			}
			input[field.FieldName] = nil
			continue
		}
		typed, matches := fdg.RequestValue(value, field.ValueType)
		if !matches {
			return nil, fmt.Errorf("action input %q does not match %s", field.FieldName, field.ValueType)
		}
		input[field.FieldName] = typed
	}
	for fieldName := range userInput {
		if !userFields[fieldName] {
			return nil, fmt.Errorf("unknown Action input %q", fieldName)
		}
	}
	return fdg.JSONDexValue(input), nil
}

// actionConditionMatches compares JSON encodings, so a json.Number condition
// value equals the int64 or float64 Dex reports for the same number.
func actionConditionMatches(condition fdg.V2ActionCondition, currentValue any) bool {
	currentJSON, err := json.Marshal(currentValue)
	if err != nil {
		return false
	}
	for _, expected := range condition.Values {
		expectedJSON, marshalErr := json.Marshal(expected)
		if marshalErr == nil && string(currentJSON) == string(expectedJSON) {
			return true
		}
	}
	return false
}

// actionPermissionMappingsForAttributeWrite returns the Action permission
// mappings Dex re-indexes when a write changes an Action's condition Attribute.
func actionPermissionMappingsForAttributeWrite(definition fdg.V2Definition, attributeKey string) *dexpb.ActionPermissionMappings {
	isConditionAttribute := false
	for _, action := range definition.Actions {
		isConditionAttribute = isConditionAttribute || action.Condition.AttributeKey == attributeKey
	}
	if !isConditionAttribute {
		return nil
	}
	mappings := make([]*dexpb.ActionPermissionMapping, 0, len(definition.Actions))
	for _, action := range definition.Actions {
		equalValues := make([]*dexpb.Value, 0, len(action.Condition.Values))
		for _, conditionValue := range action.Condition.Values {
			equalValues = append(equalValues, encodeConditionValue(conditionValue))
		}
		mappings = append(mappings, &dexpb.ActionPermissionMapping{
			AttributeKey: action.Condition.AttributeKey, EqualValues: equalValues, RequiredPermission: action.RequiredPermission,
		})
	}
	return &dexpb.ActionPermissionMappings{Mappings: mappings}
}

// encodeConditionValue encodes a condition value, which load-time validation
// restricted to a string, a boolean or a finite json.Number.
func encodeConditionValue(value any) *dexpb.Value {
	switch typed := value.(type) {
	case string:
		return &dexpb.Value{Kind: &dexpb.Value_StringValue{StringValue: typed}}
	case bool:
		return &dexpb.Value{Kind: &dexpb.Value_BoolValue{BoolValue: typed}}
	case json.Number:
		if integer, isInteger := fdg.JSONNumberInt64(typed); isInteger {
			return &dexpb.Value{Kind: &dexpb.Value_IntValue{IntValue: integer}}
		}
		number, _ := fdg.JSONNumberFloat64(typed)
		return &dexpb.Value{Kind: &dexpb.Value_DoubleValue{DoubleValue: number}}
	}
	panic(fmt.Sprintf("validated Action condition value has type %T", value))
}
