// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowapi

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const blobReferenceField = "__dexBlobReference"

func mapSummary(summary *dexpb.GetFlowSummaryResponse, currentRevision string) flowSummary {
	binding, revision := startRequestBinding(summary.GetRequestId(), currentRevision)
	execution := summary.GetFlowExecutionId()
	return flowSummary{
		DefinitionBinding: binding, DefinitionRevision: revision,
		FlowID:         execution.GetFlowId(),
		RunID:          execution.GetRunId(),
		FirstRunID:     summary.GetFirstRunId(),
		RequestID:      summary.GetRequestId(),
		FlowType:       summary.GetFlowType(),
		FlowStatus:     flowStatusLabel(summary.GetFlowStatus()),
		FlowStatusCode: int32(summary.GetFlowStatus()),
		StartTime:      timestamp(summary.GetStartTime()),
		CloseTime:      timestamp(summary.GetCloseTime()),
	}
}

func mapStreamMessage(message *dexpb.StreamMessage) streamMessage {
	return streamMessage{
		Value:       dexValue(message.GetValue()),
		ResumeToken: message.GetResumeToken(),
		CreatedTime: timestamp(message.GetCreatedTime()),
		Source:      message.GetSource(),
	}
}

func mapHistoryEvent(event *dexpb.FlowHistoryEvent) (historyEvent, error) {
	var (
		eventType string
		payload   proto.Message
	)
	switch value := event.GetPayload().(type) {
	case *dexpb.FlowHistoryEvent_FlowStartedOrContinued:
		eventType, payload = "FlowStartedOrContinued", value.FlowStartedOrContinued
	case *dexpb.FlowHistoryEvent_FlowClosed:
		eventType, payload = "FlowClosed", value.FlowClosed
	case *dexpb.FlowHistoryEvent_StepWaitForCompleted:
		eventType, payload = "StepWaitForCompleted", value.StepWaitForCompleted
	case *dexpb.FlowHistoryEvent_StepWaitForFailed:
		eventType, payload = "StepWaitForFailed", value.StepWaitForFailed
	case *dexpb.FlowHistoryEvent_StepExecuteCompleted:
		eventType, payload = "StepExecuteCompleted", value.StepExecuteCompleted
	case *dexpb.FlowHistoryEvent_StepExecuteFailed:
		eventType, payload = "StepExecuteFailed", value.StepExecuteFailed
	case *dexpb.FlowHistoryEvent_StepWaitForPending:
		eventType, payload = "StepWaitForPending", value.StepWaitForPending
	case *dexpb.FlowHistoryEvent_StepExecutePending:
		eventType, payload = "StepExecutePending", value.StepExecutePending
	case *dexpb.FlowHistoryEvent_RpcExecutionCompleted:
		eventType, payload = "RpcExecutionCompleted", value.RpcExecutionCompleted
	case *dexpb.FlowHistoryEvent_ChannelExternalPublish:
		eventType, payload = "ChannelExternalPublish", value.ChannelExternalPublish
	case *dexpb.FlowHistoryEvent_TimeTravelFork:
		eventType, payload = "TimeTravelFork", value.TimeTravelFork
	default:
		return historyEvent{}, fmt.Errorf("history event %d has no Dex payload", event.GetEventId())
	}
	mappedPayload, err := protoMap(payload)
	if err != nil {
		return historyEvent{}, fmt.Errorf("map history event %d: %w", event.GetEventId(), err)
	}
	return historyEvent{
		EventID:   event.GetEventId(),
		EventTime: timestamp(event.GetEventTime()),
		Type:      eventType,
		Payload:   mappedPayload,
	}, nil
}

// mapFlowState maps the response once; its nested messages are read from that
// one map, where an absent message is nil.
func mapFlowState(response *dexpb.GetFlowStateResponse) (flowState, error) {
	state, err := protoMap(response)
	if err != nil {
		return flowState{}, fmt.Errorf("map flow state: %w", err)
	}
	// protojson writes every element of a repeated field in order, so the
	// mapped steps align with the typed ones.
	mappedSteps := interfaceSlice(state["activeStepExecutions"])
	activeSteps := make([]activeStepExecution, 0, len(response.GetActiveStepExecutions()))
	for index, step := range response.GetActiveStepExecutions() {
		mapped, _ := mappedSteps[index].(map[string]any)
		activeSteps = append(activeSteps, mapActiveStep(step, mapped))
	}
	flowConfig, _ := state["flowConfig"].(map[string]any)
	return flowState{
		FlowConfig:             flowConfig,
		Attributes:             mapKeyValues(response.GetAttributes()),
		ActiveStepExecutions:   activeSteps,
		QueuedSteps:            interfaceSlice(state["queuedSteps"]),
		PendingChannelMessages: interfaceMap(state["pendingChannelMessages"]),
		CompletedSteps:         interfaceSlice(state["completedSteps"]),
	}, nil
}

// mapActiveStep maps step, whose JSON form is mapped.
func mapActiveStep(step *dexpb.ActiveStepExecutionState, mapped map[string]any) activeStepExecution {
	movement, _ := mapped["movement"].(map[string]any)
	waitingCondition, _ := mapped["waitingCondition"].(map[string]any)
	completedConditions, _ := mapped["completedConditions"].(map[string]any)
	lastFailureInfo, _ := mapped["lastFailureInfo"].(map[string]any)
	return activeStepExecution{
		StepExecutionID:     step.GetStepExecutionId(),
		FromStepExecutionID: step.GetFromStepExecutionId(),
		StepType:            step.GetStepType(),
		Phase:               activeStepPhase(step.GetPhase()),
		Movement:            movement,
		WaitingCondition:    waitingCondition,
		CompletedConditions: completedConditions,
		StepExecutionLocals: mapKeyValues(step.GetStepExecutionLocals()),
		Timers:              interfaceSlice(mapped["timers"]),
		LastFailureInfo:     lastFailureInfo,
	}
}

func mapKeyValues(values []*dexpb.KV) []keyValue {
	mapped := make([]keyValue, 0, len(values))
	for _, value := range values {
		mapped = append(mapped, keyValue{Key: value.GetKey(), Value: dexValue(value.GetValue())})
	}
	return mapped
}

func dexValue(value *dexpb.Value) any {
	if value == nil {
		return nil
	}
	switch kind := value.GetKind().(type) {
	case *dexpb.Value_InternalBlobIdForStringValue:
		return mappedBlobReference(kind.InternalBlobIdForStringValue, "string")
	case *dexpb.Value_InternalBlobIdForObjValue:
		return mappedBlobReference(kind.InternalBlobIdForObjValue, "object")
	case *dexpb.Value_StringValue:
		return kind.StringValue
	case *dexpb.Value_IntValue:
		return kind.IntValue
	case *dexpb.Value_DoubleValue:
		return kind.DoubleValue
	case *dexpb.Value_BoolValue:
		return kind.BoolValue
	case *dexpb.Value_NullValue:
		return nil
	case *dexpb.Value_ObjValue:
		if kind.ObjValue.GetEncoding() == "json" {
			var decoded any
			if err := comm.DecodeStrictJSONNumbers(kind.ObjValue.GetPayload(), &decoded); err == nil {
				return decoded
			}
			return map[string]any{
				"encoding": kind.ObjValue.GetEncoding(),
				"payload":  string(kind.ObjValue.GetPayload()),
			}
		}
		return map[string]any{
			"encoding": kind.ObjValue.GetEncoding(),
			"payload":  base64.StdEncoding.EncodeToString(kind.ObjValue.GetPayload()),
		}
	default:
		return nil
	}
}

func protoMap(message proto.Message) (map[string]any, error) {
	if message == nil || !message.ProtoReflect().IsValid() {
		return nil, nil
	}
	data, err := (protojson.MarshalOptions{UseEnumNumbers: true}).Marshal(message)
	if err != nil {
		return nil, err
	}
	// protojson writes exactly one JSON object.
	result := make(map[string]any)
	comm.MustDo(comm.DecodeStrictJSONNumbers(data, &result))
	normalized, _ := normalizeBlobReferences(result).(map[string]any)
	return normalized, nil
}

func mappedBlobReference(id string, kind string) map[string]any {
	return map[string]any{
		blobReferenceField: map[string]any{"id": id, "kind": kind},
	}
}

func normalizeBlobReferences(value any) any {
	switch current := value.(type) {
	case map[string]any:
		if len(current) == 1 {
			if id, ok := current["internalBlobIdForStringValue"].(string); ok {
				return mappedBlobReference(id, "string")
			}
			if id, ok := current["internalBlobIdForObjValue"].(string); ok {
				return mappedBlobReference(id, "object")
			}
		}
		for key, entry := range current {
			current[key] = normalizeBlobReferences(entry)
		}
		return current
	case []any:
		for index, entry := range current {
			current[index] = normalizeBlobReferences(entry)
		}
		return current
	default:
		return value
	}
}

// interfaceSlice returns value as a JSON array, empty when it is absent.
func interfaceSlice(value any) []any {
	if values, isArray := value.([]any); isArray {
		return values
	}
	return []any{}
}

// interfaceMap returns value as a JSON object, empty when it is absent.
func interfaceMap(value any) map[string]any {
	if values, isObject := value.(map[string]any); isObject {
		return values
	}
	return map[string]any{}
}

func timestamp(value *timestamppb.Timestamp) *string {
	if value == nil {
		return nil
	}
	formatted := value.AsTime().Format(time.RFC3339Nano)
	return &formatted
}

func flowStatusLabel(status dexpb.FlowStatus) string {
	switch status {
	case dexpb.FlowStatus_FLOW_STATUS_RUNNING:
		return "Running"
	case dexpb.FlowStatus_FLOW_STATUS_COMPLETED:
		return "Completed"
	case dexpb.FlowStatus_FLOW_STATUS_FAILED:
		return "Failed"
	case dexpb.FlowStatus_FLOW_STATUS_SERVER_SIDE_TIMEOUT_INTERNAL_ONLY:
		return "Server-side timeout (internal only)"
	case dexpb.FlowStatus_FLOW_STATUS_TERMINATED:
		return "Terminated"
	case dexpb.FlowStatus_FLOW_STATUS_CANCELED:
		return "Canceled"
	case dexpb.FlowStatus_FLOW_STATUS_CONTINUED_AS_NEW:
		return "Continued as new"
	default:
		return "Unspecified"
	}
}

func activeStepPhase(phase dexpb.ActiveStepPhase) string {
	switch phase {
	case dexpb.ActiveStepPhase_ACTIVE_STEP_PHASE_ACTIVE:
		return "Active"
	case dexpb.ActiveStepPhase_ACTIVE_STEP_PHASE_WAITING:
		return "Waiting"
	default:
		return "Unspecified"
	}
}
