// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// The model's tool calls wait in AgentState.PendingToolCalls; PendingToolIndex
// is the cursor of the next call to route. The functions below compute cursor
// moves and execution waves as values; the Flow Steps execute the calls and
// store the results.

// PendingToolCall is the call at the cursor. A Step asks for it only while a
// call is pending.
func PendingToolCall(state AgentState) conversation.ToolCall {
	return state.PendingToolCalls[state.PendingToolIndex]
}

// toolBatchID is the persisted identity of the wave that starts at the call
// index first of the response ending at state.LastSequence. The result
// ChannelMap instance key of the wave is it.
func toolBatchID(state AgentState, first int) string {
	return fmt.Sprintf("tool-batch-%d-%d", state.LastSequence, first)
}

// AdvanceToolCursor moves the cursor past count finished calls. Calls left to
// route continue with RouteTool; once none is left the cursor is cleared and
// the conversation continues with the model, after a possible compaction.
func AdvanceToolCursor(state AgentState, count int) (AgentState, Continuation) {
	state.PendingToolIndex += count
	if state.PendingToolIndex < len(state.PendingToolCalls) {
		return state, ContinueRouteTool
	}
	state.PendingToolCalls = []conversation.ToolCall{}
	state.PendingToolIndex = 0
	state.PendingToolsAdmission = remoteexec.Fence{}
	return state, ContinueCompactContext
}

// FinishToolBatch moves the cursor past a batch whose results are recorded,
// in call order, and tracks whether the turn changed source after its last
// unblocked commit_and_push.
func FinishToolBatch(state AgentState, batch ToolBatchState) (AgentState, Continuation) {
	records := slices.SortedFunc(slices.Values(batch.Records), func(left, right ToolBatchRecord) int { return left.Index - right.Index })
	for _, record := range records {
		isChanged, isHandedOff := sourceChange(record.Call, *record.Result)
		state.SourceChangedSinceHandoff = isChanged || state.SourceChangedSinceHandoff && !isHandedOff
	}
	return AdvanceToolCursor(state, len(batch.Records))
}

// CancelPendingToolCalls clears the cursor and returns the calls from index on,
// which the caller answers with a cancellation result.
func CancelPendingToolCalls(state AgentState, from int) (AgentState, []conversation.ToolCall) {
	cancelled := slices.Clone(state.PendingToolCalls[from:])
	state.PendingToolCalls = []conversation.ToolCall{}
	state.PendingToolIndex = 0
	state.PendingToolsAdmission = remoteexec.Fence{}
	return state, cancelled
}

// ParallelToolBatch selects the next wave of consecutive calls that may run in
// parallel, or an empty batch when the call at the cursor runs serially.
// toolNames are the segment's tools. needsWorkspace reports whether any branch
// is dispatched on the Session lease.
func ParallelToolBatch(toolNames []conversation.ToolName, state AgentState) (batch ToolBatchState, needsWorkspace bool) {
	start := state.PendingToolIndex
	records := make([]ToolBatchRecord, 0, maximumParallelToolCalls)
	for index := start; index < len(state.PendingToolCalls) && len(records) < maximumParallelToolCalls; index++ {
		call := state.PendingToolCalls[index]
		definition, isOffered := SegmentTool(toolNames, call.Name)
		if !isOffered || definition.RequiresApproval || !definition.supportsParallelExecution {
			break
		}
		records = append(records, ToolBatchRecord{Index: index, Call: call})
		needsWorkspace = needsWorkspace || definition.UsesWorkspace()
	}
	if len(records) < 2 {
		return ToolBatchState{}, false
	}
	return ToolBatchState{BatchID: toolBatchID(state, start), FirstIndex: start, Records: records}, needsWorkspace
}

// ToolApproval is one durable approval decision.
type ToolApproval struct {
	Approved bool `json:"approved"`
}

// ToolApprovalRequest applies a decision to one exact tool call.
type ToolApprovalRequest struct {
	CallID   conversation.CallID `json:"call_id"`
	Approved bool                `json:"approved"`
}

// PendingApproval describes the call awaiting user approval.
type PendingApproval struct {
	CallID    conversation.CallID     `json:"call_id"`
	ToolName  conversation.ToolName   `json:"tool_name"`
	Arguments conversation.JSONObject `json:"arguments"`
	StartedAt *time.Time              `json:"started_at,omitempty"`
}

// ToolBatchRecord is one call of a tool batch with its result once known.
type ToolBatchRecord struct {
	Index  int                   `json:"index"`
	Call   conversation.ToolCall `json:"call"`
	Result *ToolExecutionResult  `json:"result,omitempty"`
	Timing ToolMessageTiming     `json:"timing"`
}

// ToolBatchState is one wave of consecutive tool calls.
type ToolBatchState struct {
	BatchID    string            `json:"batch_id"`
	FirstIndex int               `json:"first_index"`
	Records    []ToolBatchRecord `json:"records"`
}

// ToolExecutionResult stores one provider-neutral tool result.
type ToolExecutionResult struct {
	Content string                   `json:"content"`
	Outcome conversation.ToolOutcome `json:"outcome"`
	// FailureCode is the spelling of the toolfailure.Code that Content reports,
	// if any. A stored result keeps the spelling it was recorded with.
	FailureCode string `json:"failure_code,omitempty"`
	// FailureEffect is the spelling of the failure's toolfailure.Effect, which
	// the report of a platform fault names; empty without a failure.
	FailureEffect string `json:"failure_effect,omitempty"`
	// HeadCommit is the remote HEAD the tool verified. The Session commits it with
	// the tool result; it is never part of the content sent to the model.
	HeadCommit string `json:"head_commit,omitempty"`
	// ExitCode and OmittedBytes measure a command result for its metrics.
	ExitCode     *int   `json:"exit_code,omitempty"`
	OmittedBytes *int64 `json:"omitted_bytes,omitempty"`
	// Background is the task of a command that runs in the background; the
	// Session records it in BackgroundCommands with the result.
	Background *sandboxtools.BackgroundStart `json:"background,omitempty"`
}

// ToolResultPayload is the model-visible content of a result the Session
// itself decides for a built-in tool that succeeded.
type ToolResultPayload struct {
	Status          ToolResultStatus    `json:"status"`
	Reason          string              `json:"reason,omitempty"`
	Revision        int64               `json:"revision,omitempty"`
	Effort          conversation.Effort `json:"effort,omitempty"`
	ItemCount       int                 `json:"item_count,omitempty"`
	Questions       []UserInputQuestion `json:"questions,omitempty"`
	Version         int64               `json:"version,omitempty"`
	DurationSeconds int64               `json:"duration_seconds,omitempty"`
}

// ToolResultStatus is the status of a built-in tool result.
type ToolResultStatus string

const (
	ToolResultStatusUpdated        ToolResultStatus = "updated"
	ToolResultStatusCleared        ToolResultStatus = "cleared"
	ToolResultStatusWaitingForUser ToolResultStatus = "waiting_for_user"
	ToolResultStatusCompleted      ToolResultStatus = "completed"
)

// ToolResult encodes the result of a built-in tool that succeeded. The payload
// holds only plain values, so encoding fails only through a programming error.
func ToolResult(payload ToolResultPayload) ToolExecutionResult {
	return ToolExecutionResult{Content: string(comm.Must(json.Marshal(payload))), Outcome: conversation.ToolOutcomeSucceeded}
}

// FailureResult records a typed failure for the model: its text, and a known
// failure or, when its effect is unknown, an unknown outcome.
func FailureResult(failure conversation.ToolFailure) ToolExecutionResult {
	return ToolExecutionResult{Content: failure.Text(), Outcome: failure.Outcome(), FailureCode: failure.Code.String(), FailureEffect: failure.Effect.String()}
}

// SessionFailureResult records a failure the Session decides for a call: the
// call is the model's to correct and changed nothing.
func SessionFailureResult(code toolfailure.Code, message string) ToolExecutionResult {
	return FailureResult(conversation.ToolFailure{Code: code, Effect: toolfailure.EffectNone, Message: message})
}

// CancelReason is why the Session skips the pending calls of a response, with
// the failure code and the message the model reads for each skipped call.
type CancelReason struct {
	code    toolfailure.Code
	message string
}

var (
	// CancelledBySteering: the user sent a new message.
	CancelledBySteering = CancelReason{code: toolfailure.SupersededBySteeredUserMessage, message: "The user sent a new message, so this call was skipped."}
	// CancelledByUserInput: the Agent asked the user and waits for the answers.
	CancelledByUserInput = CancelReason{code: toolfailure.SupersededByUserInput, message: "The Agent is waiting for the user's answers, so this call was skipped."}
)

// CancelledToolResult records a pending call the Session will not run.
func CancelledToolResult(reason CancelReason) ToolExecutionResult {
	return SessionFailureResult(reason.code, reason.message)
}

// InterruptedToolResult answers a call of a turn the user stopped. A call that
// was in flight and may change the workspace has an unknown effect; any other
// call did nothing.
func InterruptedToolResult(call conversation.ToolCall, inFlight bool) ToolExecutionResult {
	failure := conversation.ToolFailure{Code: toolfailure.InterruptedByUser, Effect: toolfailure.EffectNone,
		Message: "The user pressed Stop before this call finished, so it did not run."}
	if inFlight && CatalogTool(call.Name).WorkspaceAccess == WorkspaceAccessMutating {
		failure.Effect, failure.Message = toolfailure.EffectUnknown, "The user pressed Stop while this call ran, so it was interrupted."
	}
	return FailureResult(failure)
}

// UnfinishedToolResult records a call the platform could not complete, such as
// one whose retries are exhausted: a platform failure whose effect is none for
// a read and unknown for a tool that may change the workspace. An interrupted
// command has its own code. detail is the final error, if any.
func UnfinishedToolResult(call conversation.ToolCall, message, detail string) ToolExecutionResult {
	failure := conversation.ToolFailure{Code: toolfailure.ToolUnavailable, Effect: toolfailure.EffectNone, Message: message, Detail: detail}
	if CatalogTool(call.Name).WorkspaceAccess == WorkspaceAccessMutating {
		failure.Effect = toolfailure.EffectUnknown
		if call.Name == conversation.ToolBash {
			failure.Code = toolfailure.CommandInterrupted
		}
	}
	return FailureResult(failure)
}

// WorkspaceUnavailableResult answers a call that was never dispatched because
// the Session workspace could not be provided.
func WorkspaceUnavailableResult() ToolExecutionResult {
	return FailureResult(conversation.ToolFailure{Code: toolfailure.WorkspaceUnavailable, Effect: toolfailure.EffectNone, Message: sandboxtools.ErrWorkspaceUnavailable.Error()})
}

// SandboxToolResult records a Sandbox tool result as a tool execution result.
func SandboxToolResult(result sandboxtools.Result) ToolExecutionResult {
	outcome, code, effect := conversation.ToolOutcomeSucceeded, "", ""
	switch {
	case result.Failure != nil:
		outcome, code, effect = result.Failure.Outcome(), result.Failure.Code.String(), result.Failure.Effect.String()
	case result.KnownFailure:
		outcome = conversation.ToolOutcomeKnownFailure
	}
	return ToolExecutionResult{Content: result.Content, Outcome: outcome, FailureCode: code, FailureEffect: effect, HeadCommit: result.HeadCommit, ExitCode: result.ExitCode,
		OmittedBytes: result.OmittedBytes, Background: result.Background}
}

// ToolMessageTiming is when a tool call ran; the zero value means unknown. Its
// field names are persisted keys of batch records.
type ToolMessageTiming struct {
	StartedAt   time.Time
	CompletedAt time.Time
}

// maximumResultCodeBytes bounds the result code the UI shows.
const maximumResultCodeBytes = 64

// toolExecutionMetrics measures a result; command results add their exit code
// and the output bytes they omitted.
func toolExecutionMetrics(call conversation.ToolCall, result ToolExecutionResult, timing ToolMessageTiming) *conversation.ToolExecutionMetrics {
	metrics := &conversation.ToolExecutionMetrics{Outcome: result.Outcome, ResultBytes: int64(len(result.Content)), ExitCode: result.ExitCode,
		OmittedBytes: result.OmittedBytes, ResultCode: resultCode(call, result)}
	if !timing.StartedAt.IsZero() && !timing.CompletedAt.Before(timing.StartedAt) {
		duration := timing.CompletedAt.Sub(timing.StartedAt).Milliseconds()
		metrics.DurationMS = &duration
	}
	return metrics
}

// maximumToolResultBytes is the backstop of a tool result when it is recorded;
// what is stored is what is sent. The tools size their own results within it,
// bash to 30,000 bytes with the path of its full output, so the backstop cuts
// only a tool that did not.
const maximumToolResultBytes = 64 << 10

// ToolResultEntry records a tool result: one tool_result block, an error
// unless the result succeeded, kept within maximumToolResultBytes when it is
// recorded.
func ToolResultEntry(call conversation.ToolCall, result ToolExecutionResult, timing ToolMessageTiming) conversation.Entry {
	content, omitted := boundedToolResult(result.Content)
	if omitted > 0 && result.OmittedBytes == nil {
		result.OmittedBytes = &omitted
	}
	summary := ToolSummary(call)
	entry := conversation.Entry{Role: conversation.EntryRoleUser, Kind: conversation.EntryKindToolResult, Display: conversation.EntryDisplayPublic,
		Blocks: []json.RawMessage{conversation.ToolResultBlock(call.ID, content, result.Outcome != conversation.ToolOutcomeSucceeded)},
		Meta:   conversation.EntryMeta{ToolCall: &summary, ToolExecution: toolExecutionMetrics(call, result, timing)}, CreatedAt: timing.CompletedAt.UTC()}
	if !timing.StartedAt.IsZero() {
		startedAt := timing.StartedAt.UTC()
		entry.StartedAt = &startedAt
	}
	return entry
}

// commitPushed and commitBlocked are the result codes of a successful
// commit_and_push: a push that hands its commit off, and a blocked
// checkpoint, which hands nothing off.
const (
	commitPushed  = "pushed"
	commitBlocked = "blocked"
)

// resultCode is the code the UI shows: a failure's own code, commitPushed or
// commitBlocked for a successful commit_and_push, and the outcome of any other
// result that did not succeed, such as a command that exited with a nonzero
// code.
func resultCode(call conversation.ToolCall, result ToolExecutionResult) string {
	switch {
	case result.FailureCode != "":
		return comm.TruncateUTF8(result.FailureCode, maximumResultCodeBytes)
	case result.Outcome != conversation.ToolOutcomeSucceeded:
		return string(result.Outcome)
	case call.Name != conversation.ToolCommitAndPush:
		return ""
	}
	if isBlockedCommit(call) {
		return commitBlocked
	}
	return commitPushed
}

// boundedToolResult keeps a result within maximumToolResultBytes: a longer
// one keeps its head and tail around a notice of the omitted bytes.
func boundedToolResult(content string) (string, int64) {
	if len(content) <= maximumToolResultBytes {
		return content, 0
	}
	const half = maximumToolResultBytes/2 - 256
	head, tail := comm.TruncateUTF8(content, half), comm.TailUTF8(content, half)
	omitted := int64(len(content) - len(head) - len(tail))
	return fmt.Sprintf("%s\n[%d bytes omitted from this result; narrow the request to read them]\n%s", head, omitted, tail), omitted
}

// ToolTerminalEvent reports how a tool call ended.
func ToolTerminalEvent(call conversation.ToolCall, result ToolExecutionResult) conversation.AgentEvent {
	switch result.Outcome {
	case conversation.ToolOutcomeKnownFailure:
		if result.FailureCode != "" {
			return ToolActivity(conversation.EventKindToolFailed, fmt.Sprintf("%s failed: %s.", call.Name, result.FailureCode), call)
		}
		return ToolActivity(conversation.EventKindToolFailed, fmt.Sprintf("%s returned a known failure.", call.Name), call)
	case conversation.ToolOutcomeUnknown:
		return ToolActivity(conversation.EventKindToolFailed, fmt.Sprintf("%s ended with an unknown outcome.", call.Name), call)
	default:
		return ToolActivity(conversation.EventKindToolCompleted, fmt.Sprintf("Completed %s.", call.Name), call)
	}
}

// PlatformIncident is one platform fault of a Session, the input of the Step
// ReportPlatformIssue: identifiers and classification only, never a message,
// a tool argument, file contents or conversation text. Its field names are the
// persisted keys of that input.
type PlatformIncident struct {
	// CallID and Tool name the failed tool call; both are empty for a model call.
	CallID conversation.CallID   `json:"call_id,omitempty"`
	Tool   conversation.ToolName `json:"tool,omitempty"`
	// Code is the failure code of a tool result, or the class of a model call
	// the API rejected, such as invalid_request_error.
	Code string `json:"code"`
	// Owner and Effect are the spellings of the toolfailure.Owner and
	// toolfailure.Effect of the failure.
	Owner  string `json:"owner"`
	Effect string `json:"effect,omitempty"`
	// ErrorType is the type of the error behind the fault: the engine's type of
	// the final error of a call whose retries are exhausted, such as
	// StartToClose for an attempt that timed out, and http_<status> for a
	// rejected model call. It is empty for a failure that a tool classified.
	ErrorType string    `json:"error_type,omitempty"`
	At        time.Time `json:"at"`
}

// Signature identifies the kind of the fault across Sessions
// (flowid.PlatformIssueSignature), which names its PlatformIssueFlow.
func (incident PlatformIncident) Signature() string {
	return flowid.PlatformIssueSignature(incident.Owner, string(incident.Tool), incident.Code, incident.ErrorType)
}

// ToolIncident is the incident of a tool result whose failure the platform
// owns; false for any other result. errorType is the type of the error that
// moved the call to the Step recording it, if any.
func ToolIncident(call conversation.ToolCall, result ToolExecutionResult, errorType string, at time.Time) (PlatformIncident, bool) {
	code, isKnown := toolfailure.Parse(result.FailureCode)
	if !isKnown || code.Owner() != toolfailure.OwnerPlatform {
		return PlatformIncident{}, false
	}
	return PlatformIncident{CallID: call.ID, Tool: call.Name, Code: code.String(), Owner: code.Owner().String(), Effect: result.FailureEffect, ErrorType: errorType, At: at}, true
}

// ModelIncident is the incident of a model call that the API rejected with
// HTTP 400, 401 or 403, which a retry cannot change: the platform built a
// request the API refuses, or holds a credential it refuses. A rejection
// without an API status, such as a missing summary or a model the server does
// not offer, is none.
func ModelIncident(rejected *conversation.ModelRejectedError, at time.Time) (PlatformIncident, bool) {
	switch rejected.Status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return PlatformIncident{Code: rejected.Code, Owner: toolfailure.OwnerPlatform.String(), Effect: toolfailure.EffectNone.String(),
			ErrorType: "http_" + strconv.Itoa(rejected.Status), At: at}, true
	default:
		return PlatformIncident{}, false
	}
}

// The tests name these constants.
const (
	CommitPushedForTest  = commitPushed
	CommitBlockedForTest = commitBlocked
)
