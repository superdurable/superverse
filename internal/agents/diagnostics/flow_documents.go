// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The documents below are what the platform and Live run tools show of one
// Flow: shaped like dexcli flow summary, state and history --no-hydrate, but
// without any run identity, request identity or blob reference. A read
// always addresses the current run of a FlowID. Every text is redacted before
// it is cut to its bound, so a cut never keeps part of a secret.

// maximumValueBytes bounds the JSON of one Attribute value in a state.
const maximumValueBytes = 1 << 10

// omittedBlob stands for a value the engine keeps as a blob, which no tool
// loads.
const omittedBlob = "<blob value omitted>"

type flowSummary struct {
	FlowID      string       `json:"flow_id"`
	FlowType    string       `json:"flow_type"`
	Status      string       `json:"status"`
	StartTime   string       `json:"start_time,omitempty"`
	CloseTime   string       `json:"close_time,omitempty"`
	ActiveSteps []activeStep `json:"active_steps,omitempty"`
}

type activeStep struct {
	Step            string        `json:"step"`
	StepExecutionID string        `json:"step_execution_id"`
	Phase           string        `json:"phase"`
	WaitingFor      []string      `json:"waiting_for,omitempty"`
	LastFailure     *methodFailed `json:"last_failure,omitempty"`
}

// methodFailed is a failed Step method attempt with the bounded detail of its
// error.
type methodFailed struct {
	Attempt   int32  `json:"attempt,omitempty"`
	ErrorType string `json:"error_type,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type flowState struct {
	flowSummary
	Attributes             map[string]any `json:"attributes"`
	PendingChannelMessages map[string]int `json:"pending_channel_messages,omitempty"`
	LeftOutAttributes      int            `json:"left_out_attributes,omitempty"`
}

// historyPage is one page of the events of a Flow's current run, which started
// at StartTime. Event IDs count within a run, so a page with another start time
// than the page before it does not continue it.
type historyPage struct {
	FlowID           string         `json:"flow_id"`
	StartTime        string         `json:"start_time,omitempty"`
	Events           []historyEvent `json:"events"`
	NextAfterEventID int64          `json:"next_after_event_id,omitempty"`
}

type historyEvent struct {
	EventID         int64         `json:"event_id"`
	Time            string        `json:"time,omitempty"`
	Type            string        `json:"type"`
	Step            string        `json:"step,omitempty"`
	StepExecutionID string        `json:"step_execution_id,omitempty"`
	Attempt         int32         `json:"attempt,omitempty"`
	Next            []string      `json:"next,omitempty"`
	Close           string        `json:"close,omitempty"`
	RPC             string        `json:"rpc,omitempty"`
	Channels        []string      `json:"channels,omitempty"`
	Status          string        `json:"status,omitempty"`
	Failure         *methodFailed `json:"failure,omitempty"`
}

// readSummary reads the current run of flowID; a FlowID without one is
// not_found.
func readSummary(ctx context.Context, flows dexpb.FlowServiceClient, flowID string) (*dexpb.GetFlowSummaryResponse, error) {
	summary, err := flows.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if status.Code(err) == codes.NotFound {
		return nil, &callFailureError{code: toolfailure.FlowNotFound, message: "Flow " + flowID + " does not exist"}
	}
	if err != nil {
		return nil, fmt.Errorf("read the summary of %s: %w", flowID, err)
	}
	return summary, nil
}

// readState reads the state of a running Flow; a closed one keeps only its
// history.
func readState(ctx context.Context, flows dexpb.FlowServiceClient, summary *dexpb.GetFlowSummaryResponse) (*dexpb.GetFlowStateResponse, error) {
	flowID := summary.GetFlowExecutionId().GetFlowId()
	if summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		return nil, &callFailureError{code: toolfailure.FlowNotFound, message: "Flow " + flowID + " is " + flowStatus(summary.GetFlowStatus()) + " and has no current state; read its history"}
	}
	state, err := flows.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowID})
	if code := status.Code(err); code == codes.NotFound || code == codes.FailedPrecondition {
		return nil, &callFailureError{code: toolfailure.FlowNotFound, message: "Flow " + flowID + " closed while it was read; read its history"}
	}
	if err != nil {
		return nil, fmt.Errorf("read the state of %s: %w", flowID, err)
	}
	return state, nil
}

// summaryOf is the summary of a Flow with its current Steps while it runs.
func summaryOf(ctx context.Context, flows dexpb.FlowServiceClient, r redactor, summary *dexpb.GetFlowSummaryResponse) (flowSummary, error) {
	if summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		return summaryDocument(r, summary, nil), nil
	}
	state, err := readState(ctx, flows, summary)
	var closed *callFailureError
	if errors.As(err, &closed) {
		return summaryDocument(r, summary, nil), nil
	}
	return summaryDocument(r, summary, state), err
}

// summaryDocument is the summary of a Flow with the Steps its running
// execution is in, which state supplies.
func summaryDocument(r redactor, summary *dexpb.GetFlowSummaryResponse, state *dexpb.GetFlowStateResponse) flowSummary {
	document := flowSummary{
		FlowID: summary.GetFlowExecutionId().GetFlowId(), FlowType: summary.GetFlowType(), Status: flowStatus(summary.GetFlowStatus()),
		StartTime: timestamp(summary.GetStartTime()), CloseTime: timestamp(summary.GetCloseTime()),
	}
	for _, step := range state.GetActiveStepExecutions() {
		phase := "active"
		if step.GetPhase() == dexpb.ActiveStepPhase_ACTIVE_STEP_PHASE_WAITING {
			phase = "waiting"
		}
		document.ActiveSteps = append(document.ActiveSteps, activeStep{
			Step: step.GetStepType(), StepExecutionID: step.GetStepExecutionId(), Phase: phase,
			WaitingFor: waitingFor(step.GetWaitingCondition(), step.GetTimers()), LastFailure: methodFailure(r, step.GetLastFailureInfo()),
		})
	}
	return document
}

// waitingFor names the Channels and the timers a waiting Step waits for.
func waitingFor(condition *dexpb.WaitingConditionState, timers []*dexpb.TimerInfo) []string {
	var waits []string
	for _, channel := range condition.GetChannelConditions() {
		waits = append(waits, "channel "+channel.GetChannelName())
	}
	for _, timer := range timers {
		waits = append(waits, "timer at "+time.Unix(timer.GetFiringUnixTimestampSeconds(), 0).UTC().Format(time.RFC3339))
	}
	return waits
}

// stateDocument is the state of a running Flow with the Attributes that keep
// admits, each value at most maximumValueBytes of JSON, and the count of each
// Channel's pending messages.
func stateDocument(r redactor, summary *dexpb.GetFlowSummaryResponse, state *dexpb.GetFlowStateResponse, keep func(key string, value any) (any, bool)) flowState {
	document := flowState{flowSummary: summaryDocument(r, summary, state), Attributes: map[string]any{}}
	for _, attribute := range state.GetAttributes() {
		if value, kept := keep(attribute.GetKey(), dexValue(attribute.GetValue())); kept {
			document.Attributes[attribute.GetKey()] = boundedValue(r, value)
		}
	}
	for channel, messages := range state.GetPendingChannelMessages() {
		if count := len(messages.GetMessages()); count > 0 {
			if document.PendingChannelMessages == nil {
				document.PendingChannelMessages = map[string]int{}
			}
			document.PendingChannelMessages[channel] = count
		}
	}
	return document
}

// readHistory reads up to pageSize events after afterEventID of the run that
// summary names. The engine serves history by run, so the caller resolves the
// Flow's current run with readSummary; the run is sent with this one read and
// never reaches a result. The cursor is an event ID within that run, and the
// page names the ID to continue after when the history goes on. When the Flow
// starts another run between two pages, such as after a restart or a reset, the
// next page reads the new current run, whose events count from 1 again, and the
// page's start time tells the two runs apart.
func readHistory(ctx context.Context, flows dexpb.FlowServiceClient, r redactor, summary *dexpb.GetFlowSummaryResponse, afterEventID int64, pageSize int) (historyPage, error) {
	flowID := summary.GetFlowExecutionId().GetFlowId()
	response, err := flows.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{FlowId: flowID, RunId: summary.GetFlowExecutionId().GetRunId(), StartInternalEventId: afterEventID + 1, EstimatePageSize: int32(pageSize)}) // #nosec G115 -- the schema bounds pageSize to 50.
	if status.Code(err) == codes.NotFound {
		return historyPage{}, &callFailureError{code: toolfailure.FlowNotFound, message: "Flow " + flowID + " lost its history while it was read; read again"}
	}
	if err != nil {
		return historyPage{}, fmt.Errorf("read the history of %s: %w", flowID, err)
	}
	page := historyPage{FlowID: flowID, StartTime: timestamp(summary.GetStartTime()), Events: []historyEvent{}}
	for _, event := range response.GetEvents() {
		if len(page.Events) == pageSize {
			// The engine returned more events than the page holds: the next page
			// starts after the last one it keeps.
			page.NextAfterEventID = page.Events[pageSize-1].EventID
			return page, nil
		}
		page.Events = append(page.Events, historyEventOf(r, event))
	}
	if len(response.GetNextPageToken()) > 0 {
		// The engine read the history up to NextInternalEventId, which can lie
		// after the page's last event or leave the page without events. The
		// next page starts there: starting after the last event would cover the
		// rest of that stretch again, find no event in it and end the read in
		// front of the events beyond.
		page.NextAfterEventID = response.GetNextInternalEventId() - 1
	}
	return page, nil
}

// fittedHistory encodes as many of the page's events as fit in a result; the
// next page starts after the last event it keeps.
func fittedHistory(r redactor, page historyPage) []byte {
	return r.fitted(len(page.Events), func(count int) any {
		fitted := page
		fitted.Events = page.Events[:count]
		if count < len(page.Events) {
			fitted.NextAfterEventID = page.Events[count-1].EventID
		}
		return fitted
	})
}

func historyEventOf(r redactor, event *dexpb.FlowHistoryEvent) historyEvent {
	mapped := historyEvent{EventID: event.GetEventId(), Time: timestamp(event.GetEventTime())}
	stepContext := func(context *dexpb.StepMethodEventContext) {
		mapped.Step, mapped.StepExecutionID, mapped.Attempt = context.GetStepType(), context.GetStepExecutionId(), context.GetFinalAttempt()
	}
	switch payload := event.GetPayload().(type) {
	case *dexpb.FlowHistoryEvent_FlowStartedOrContinued:
		mapped.Type = "FlowStarted"
	case *dexpb.FlowHistoryEvent_FlowClosed:
		mapped.Type, mapped.Status = "FlowClosed", flowStatus(payload.FlowClosed.GetFlowStatus())
		if message := payload.FlowClosed.GetErrorMessage(); message != "" {
			mapped.Failure = &methodFailed{ErrorType: payload.FlowClosed.GetErrorType().String(), Detail: comm.TailUTF8(r.text(message), maximumFailureMessageBytes)}
		}
	case *dexpb.FlowHistoryEvent_StepWaitForCompleted:
		mapped.Type = "StepWaitForCompleted"
		stepContext(payload.StepWaitForCompleted.GetContext())
		mapped.Channels = publishedChannels(payload.StepWaitForCompleted.GetOutput().GetPublishToChannel())
	case *dexpb.FlowHistoryEvent_StepWaitForFailed:
		mapped.Type = "StepWaitForFailed"
		stepContext(payload.StepWaitForFailed.GetContext())
		mapped.Failure = methodFailure(r, payload.StepWaitForFailed.GetOutput().GetFailure())
	case *dexpb.FlowHistoryEvent_StepExecuteCompleted:
		mapped.Type = "StepExecuteCompleted"
		stepContext(payload.StepExecuteCompleted.GetContext())
		mapped.Next, mapped.Close = decision(payload.StepExecuteCompleted.GetOutput().GetStepDecision())
		mapped.Channels = publishedChannels(payload.StepExecuteCompleted.GetOutput().GetPublishToChannel())
	case *dexpb.FlowHistoryEvent_StepExecuteFailed:
		mapped.Type = "StepExecuteFailed"
		stepContext(payload.StepExecuteFailed.GetContext())
		mapped.Failure = methodFailure(r, payload.StepExecuteFailed.GetOutput().GetFailure())
	case *dexpb.FlowHistoryEvent_StepWaitForPending:
		mapped.Type = "StepWaitForPending"
		stepContext(payload.StepWaitForPending.GetContext())
	case *dexpb.FlowHistoryEvent_StepExecutePending:
		mapped.Type = "StepExecutePending"
		stepContext(payload.StepExecutePending.GetContext())
	case *dexpb.FlowHistoryEvent_RpcExecutionCompleted:
		mapped.Type, mapped.RPC = "RpcExecutionCompleted", payload.RpcExecutionCompleted.GetRpcName()
		mapped.Next, mapped.Close = decision(payload.RpcExecutionCompleted.GetStepDecision())
		mapped.Channels = publishedChannels(payload.RpcExecutionCompleted.GetPublishToChannel())
	case *dexpb.FlowHistoryEvent_ChannelExternalPublish:
		mapped.Type, mapped.Channels = "ChannelExternalPublish", publishedChannels(payload.ChannelExternalPublish.GetMessages())
	case *dexpb.FlowHistoryEvent_ChannelExternalDelete:
		mapped.Type = "ChannelExternalDelete"
		for _, deletion := range payload.ChannelExternalDelete.GetMessages() {
			mapped.Channels = append(mapped.Channels, deletion.GetChannelName())
		}
	case *dexpb.FlowHistoryEvent_TimeTravelFork:
		mapped.Type = "TimeTravel"
	default:
		mapped.Type = "Unknown"
	}
	return mapped
}

// decision names the Steps a decision moves to and how it closes the Flow.
func decision(decision *dexpb.StepDecision) ([]string, string) {
	var next []string
	for _, movement := range decision.GetNextSteps() {
		next = append(next, movement.GetStepType())
	}
	closeType := ""
	if closing := decision.GetCloseDecision(); closing != nil {
		closeType = strings.TrimPrefix(closing.GetCloseDecisionType().String(), "CLOSE_DECISION_TYPE_")
	}
	return next, closeType
}

func publishedChannels(messages []*dexpb.ChannelMessage) []string {
	var channels []string
	for _, message := range messages {
		if !slices.Contains(channels, message.GetChannelName()) {
			channels = append(channels, message.GetChannelName())
		}
	}
	return channels
}

// methodFailure is a failed method attempt with the bounded detail of the
// error its handler returned.
func methodFailure(r redactor, failure *dexpb.StepMethodFailure) *methodFailed {
	if failure == nil {
		return nil
	}
	details := failure.GetDetails()
	detail := details.GetOriginalWorkerErrorDetail()
	if detail == "" {
		detail = details.GetDetail()
	}
	if detail == "" {
		detail = failure.GetBackendError()
	}
	return &methodFailed{Attempt: failure.GetAttempt(), ErrorType: details.GetOriginalWorkerErrorType(), Detail: comm.TailUTF8(r.text(detail), maximumFailureMessageBytes)}
}

// dexValue maps an engine value to JSON. A blob is never loaded.
func dexValue(value *dexpb.Value) any {
	switch kind := value.GetKind().(type) {
	case *dexpb.Value_InternalBlobIdForStringValue, *dexpb.Value_InternalBlobIdForObjValue:
		return omittedBlob
	case *dexpb.Value_StringValue:
		return kind.StringValue
	case *dexpb.Value_IntValue:
		return kind.IntValue
	case *dexpb.Value_DoubleValue:
		if math.IsNaN(kind.DoubleValue) || math.IsInf(kind.DoubleValue, 0) {
			// JSON has no such number.
			return fmt.Sprint(kind.DoubleValue)
		}
		return kind.DoubleValue
	case *dexpb.Value_BoolValue:
		return kind.BoolValue
	case *dexpb.Value_ObjValue:
		var decoded any
		if kind.ObjValue.GetEncoding() == "json" && comm.DecodeStrictJSONNumbers(kind.ObjValue.GetPayload(), &decoded) == nil {
			return decoded
		}
		return "<" + kind.ObjValue.GetEncoding() + " value omitted>"
	default:
		return nil
	}
}

// boundedValue redacts a value and keeps it when its JSON fits
// maximumValueBytes, or else replaces it with the head of its JSON.
func boundedValue(r redactor, value any) any {
	value = r.walk(value)
	encoded := comm.Must(json.Marshal(value))
	if len(encoded) <= maximumValueBytes {
		return value
	}
	return comm.TruncateUTF8(string(encoded), maximumValueBytes) + fmt.Sprintf("... (%d bytes of JSON in all)", len(encoded))
}

func flowStatus(flowStatus dexpb.FlowStatus) string {
	return strings.TrimPrefix(flowStatus.String(), "FLOW_STATUS_")
}

func timestamp(value *timestamppb.Timestamp) string {
	if value == nil {
		return ""
	}
	return value.AsTime().UTC().Format(time.RFC3339Nano)
}
