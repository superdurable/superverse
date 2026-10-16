// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
)

// writeActivity writes an activity event to the best-effort Stream; a failed
// write, such as after a Stop cancelled the Step, loses only the live event.
func writeActivity(ctx dex.Context, event conversation.AgentEvent) {
	event.Message = agentloop.CondenseActivityMessage(event.Message)
	dexflow.StreamWritten(ctx, agentActivityStream.Write(ctx, event))
}

// writeSnapshotRequired tells live readers to read the Snapshot again.
func writeSnapshotRequired(ctx dex.Context) {
	writeActivity(ctx, conversation.AgentEvent{Kind: conversation.EventKindSnapshotRequired, Message: "Durable interaction state changed."})
}

// writeInputConsumption reports the inputs a Step consumed, if any.
func writeInputConsumption(ctx dex.Context, queued, steered []conversation.PendingUserMessage, messageSequence *conversation.Sequence) {
	if event, isConsumed := agentloop.InputConsumptionEvent(queued, steered, messageSequence); isConsumed {
		writeActivity(ctx, event)
	}
}

// modelProgress writes the best-effort activity of one model reply.
type modelProgress struct {
	ctx             dex.Context
	messageSequence conversation.Sequence
}

func (progress modelProgress) write(event conversation.AgentEvent) {
	event.Message, event.MessageSequence = agentloop.CondenseActivityMessage(event.Message), &progress.messageSequence
	dexflow.StreamWritten(progress.ctx, agentActivityStream.Write(progress.ctx, event))
}

// writeActivity is the conversation.ActivityWriter of the model call.
func (progress modelProgress) writeActivity(event conversation.AgentEvent) error {
	progress.write(event)
	return nil
}

// writeToolProgress reports the attempt of a tool call that starts running.
// Progress carries no command output; dexflow.KeepAlive, not progress, keeps
// the attempt alive.
func writeToolProgress(ctx dex.Context, call conversation.ToolCall) {
	event := agentloop.ToolActivity(conversation.EventKindToolProgress, "Running "+string(call.Name)+".", call)
	attempt := ctx.Attempt()
	event.Attempt = &attempt
	dexflow.StreamWritten(ctx, agentActivityStream.Write(ctx, event))
}

// readExecutor admits a scoped read without initializing or changing the
// executor. Older open Sessions have no Attribute and remain platform epoch 0.
func readExecutor(ctx dex.Context, input agentloop.SessionScopeInput) (*dex.RPCResult[remoteexec.Executor], error) {
	projectID, err := projectIDAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	status, err := sessionStatusAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if projectID != input.ProjectID || status != agentloop.SessionStatusActive {
		return nil, ErrCommandRejected
	}
	executor, err := executorAttribute.Get(ctx)
	if dexflow.IsAttributeNotFound(err) {
		executor, err = remoteexec.Executor{Mode: remoteexec.ModePlatform}, nil
	}
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[remoteexec.Executor]{Output: executor}, nil
}

// The executor side of a turn: which segment a platform model replays, when
// a new segment opens, and how a reply or failure is recorded. agentloop decides
// over values; the functions below read and write the Flow primitives in the
// commit of the calling Step.

// completeModelCall only produces a bounded outcome; authoritative recording
// occurs in a fresh locked Step after the provider transport has returned.
func (flow *SessionAgentFlow) completeModelCall(ctx dex.Context, segment agentloop.ContextSegment, request conversation.ModelRequest, admission modelAdmission) (platformModelOutcome, error) {
	progress, attempt := modelProgress{ctx: ctx, messageSequence: comm.Must(agentStateAttribute.Get(ctx)).LastSequence + 1}, ctx.Attempt()
	progress.write(conversation.AgentEvent{Kind: conversation.EventKindModelStarted, Message: "Calling " + segment.Model.ProviderModel() + ".", Attempt: &attempt})
	request.WriteActivity = progress.writeActivity
	defer dexflow.KeepAlive(ctx, agentloop.ModelHeartbeat{Phase: agentloop.HeartbeatPhaseModelCall})()
	reply, err := flow.modelClient.Complete(ctx, request)
	outcome := platformModelOutcome{Admission: admission, StartedAt: ctx.FirstAttemptAt().UTC()}
	if rejected, ok := errors.AsType[*conversation.ModelRejectedError](err); ok {
		outcome.Failure = publicModelRejection(rejected)
		return boundedModelOutcome(outcome), nil
	}
	if err != nil {
		progress.write(conversation.AgentEvent{Kind: conversation.EventKindModelFailed, Message: "Model request failed.", Attempt: &attempt})
		return outcome, dex.ErrorWithStack(fmt.Errorf("call model %s: %w", segment.Model, err))
	}
	reply.Metrics.Effort = segment.EffectiveEffort()
	progress.write(conversation.AgentEvent{Kind: conversation.EventKindModelCompleted, Message: agentloop.ModelReplyActivityMessage(reply), Attempt: &attempt})
	outcome.Reply = &reply
	return boundedModelOutcome(outcome), nil
}

// The outcome is a durable Dex movement payload, not an Attribute. Reply
// blocks retain only the provider replay material the existing model path stored.
type platformModelOutcome struct {
	Admission  modelAdmission                   `json:"admission"`
	StartedAt  time.Time                        `json:"started_at"`
	Reply      *conversation.ModelReply         `json:"reply,omitempty"`
	Compaction *contextCompaction               `json:"compaction,omitempty"`
	Summary    string                           `json:"summary,omitempty"`
	Correction bool                             `json:"correction,omitempty"`
	Failure    *conversation.ModelRejectedError `json:"failure,omitempty"`
}

const platformModelOutcomeLimit = 16 << 20

func publicModelRejection(rejected *conversation.ModelRejectedError) *conversation.ModelRejectedError {
	return &conversation.ModelRejectedError{Code: comm.TruncateUTF8(rejected.Code, 1024), Status: rejected.Status,
		Hint: comm.TruncateUTF8(rejected.Hint, 1024), UserText: comm.TruncateUTF8(rejected.UserText, 1024)}
}

// Preflight bounds encoded allocation, including HTML escaping, before the
// whole SDK payload marshal; the final marshal checks the actual total.
func boundedModelOutcome(outcome platformModelOutcome) platformModelOutcome {
	budget := platformModelOutcomeLimit
	if boundedJSONValue(reflect.ValueOf(outcome), &budget) {
		if encoded, err := json.Marshal(outcome); err == nil && len(encoded) <= platformModelOutcomeLimit {
			return outcome
		}
	}
	outcome.Reply, outcome.Summary, outcome.Correction = nil, "", false
	outcome.Failure = &conversation.ModelRejectedError{Code: modelOutcomeTooLarge.name, Hint: modelOutcomeTooLarge.hint}
	return outcome
}

func boundedJSONValue(value reflect.Value, budget *int) bool {
	spend := func(size int) bool {
		if size > *budget {
			return false
		}
		*budget -= size
		return true
	}
	if value.Type() == reflect.TypeFor[time.Time]() {
		return spend(40)
	}
	if value.Type() == reflect.TypeFor[json.RawMessage]() || value.Type() == reflect.TypeFor[conversation.JSONObject]() {
		if value.Len() > *budget-4 {
			return false
		}
		var raw string
		if value.Kind() == reflect.String {
			raw = value.String()
		} else {
			raw = string(value.Bytes())
		}
		if !spend(len(raw) + 4) {
			return false
		}
		for _, character := range raw {
			switch character {
			case '<', '>', '&':
				if !spend(5) {
					return false
				}
			case '\u2028', '\u2029':
				if !spend(3) {
					return false
				}
			}
		}
		return true
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return spend(4)
		}
		return boundedJSONValue(value.Elem(), budget)
	case reflect.String:
		if !spend(2) {
			return false
		}
		for _, character := range value.String() {
			cost := utf8.RuneLen(character)
			switch {
			case character < 0x20 || character == '<' || character == '>' || character == '&' || character == '\u2028' || character == '\u2029' || character == utf8.RuneError:
				cost = 6
			case character == '\\' || character == '"':
				cost = 2
			}
			if !spend(cost) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		if !spend(2) || value.Len() > *budget {
			return false
		}
		for index := 0; index < value.Len(); index++ {
			if !spend(1) || !boundedJSONValue(value.Index(index), budget) {
				return false
			}
		}
		return true
	case reflect.Struct:
		if !spend(2) {
			return false
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if field.PkgPath != "" {
				continue
			}
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			if !spend(len(name)+4) || !boundedJSONValue(value.Field(index), budget) {
				return false
			}
		}
		return true
	default:
		return spend(32)
	}
}

type platformModelRecord struct {
	discard    bool
	compaction *contextCompaction
	next       agentloop.Continuation
	incident   *agentloop.PlatformIncident
}

func (flow *SessionAgentFlow) recordPlatformModelResult(ctx dex.Context, input platformModelOutcome) platformModelRecord {
	if !acceptsUserCommand(ctx) || !modelAdmissionMatches(ctx, input.Admission) {
		return platformModelRecord{discard: true}
	}
	setRunningWork(ctx, nil)
	if input.Compaction != nil {
		compaction := *input.Compaction
		if compaction.Admission != input.Admission {
			panic("compaction outcome admission mismatch")
		}
		if input.Correction {
			compaction.ToolChoiceNone = true
			setAgentStatus(ctx, agentloop.AgentStatusCompactingContext)
			setRunningWork(ctx, &agentloop.RunningWork{Kind: agentloop.RunningModel})
			return platformModelRecord{compaction: &compaction}
		}
		if input.Failure != nil {
			return platformModelRecord{next: flow.compactionFailed(ctx, compaction, modelFailure(input.Failure), true)}
		}
		model := comm.Must(contextSegmentAttribute.Get(ctx)).Model
		if compaction.Reason == agentloop.SegmentReasonRollover {
			model = comm.Must(sessionModelAttribute.Get(ctx)).Model
		}
		opened := flow.openSegment(ctx, compaction.Reason, model, input.Summary, loadedEntry)
		writeActivity(ctx, conversation.AgentEvent{Kind: conversation.EventKindCompacted, Message: agentloop.SegmentMarker(compaction.Reason) + ".", MessageSequence: &opened.FirstSequence})
		return platformModelRecord{next: agentloop.ContinueCallModel}
	}
	if input.Failure != nil {
		recordTurnFailure(ctx, modelFailure(input.Failure))
		if incident, ok := agentloop.ModelIncident(input.Failure, time.Now().UTC()); ok {
			return platformModelRecord{next: agentloop.ContinueAwaitUser, incident: &incident}
		}
		return platformModelRecord{next: agentloop.ContinueAwaitUser}
	}
	if input.Reply == nil {
		panic("model outcome has no reply")
	}
	return platformModelRecord{next: recordModelReply(ctx, *input.Reply, input.StartedAt, input.Admission.Authority)}
}

// contextCompaction is the input of CompactContext: why the segment is renewed.
type contextCompaction struct {
	Reason         agentloop.SegmentReason `json:"reason"`
	Admission      modelAdmission          `json:"admission"`
	ToolChoiceNone bool                    `json:"tool_choice_none,omitempty"`
}

// Existing null CallModel inputs decode to legacy platform epoch/generation0.
type modelAdmission struct {
	Authority remoteexec.Fence `json:"authority"`
	Turn      int64            `json:"turn"`
}

func modelAdmissionMatches(ctx dex.Context, admission modelAdmission) bool {
	executor, state := currentExecutor(ctx), comm.Must(agentStateAttribute.Get(ctx))
	return executor.Mode == remoteexec.ModePlatform && admission.Authority.Matches(executor) &&
		state.Status != agentloop.AgentStatusWaitingForMessage && state.Status != agentloop.AgentStatusInitializing && (admission.Turn == 0 || admission.Turn == state.Turn)
}

// Only routing boundaries use this: no call in the pending suffix was
// dispatched there. External attempts settle through their existing policy.
func rejectStaleToolBatch(ctx dex.Context) bool {
	state := comm.Must(agentStateAttribute.Get(ctx))
	if len(state.PendingToolCalls) == 0 || state.PendingToolsAdmission.Matches(currentExecutor(ctx)) {
		return false
	}
	state, calls := agentloop.CancelPendingToolCalls(state, state.PendingToolIndex)
	state.Status, state.Running = agentloop.AgentStatusWaitingForMessage, nil
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	for _, call := range calls {
		appendToolResult(ctx, call, agentloop.InterruptedToolResult(call, false), agentloop.ToolMessageTiming{})
	}
	writeSnapshotRequired(ctx)
	return true
}

// The options of the model Steps, CallModel and CompactContext, and of their
// failure Steps; each declares its failure route with them. A model Step also
// loads the archive chunks of its segment and the segment's prompts, which
// each movement to it selects (segmentLoad).
var (
	modelStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 20 * time.Minute, HeartbeatTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteLoadAttributeMaps: messageMaps,
		ExecuteRetry:             dexflow.CappedStepRetry(3, time.Hour)}
	modelFailureStepOptions = &dex.StepOptions{ExecuteMethodTimeout: time.Minute, ExecuteRetry: shortStepRetry, ExecuteLoadAttributeMaps: messageMaps,
		ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(messageWindowLockAttribute), executorLock, interruptLock}}
)

// modelCallNext is where a model continuation goes from a boundary Step.
type modelCallNext int

const (
	nextCallModel modelCallNext = iota
	nextCompactContext
	nextRequestWorkspace
	nextAwaitRemote
	nextLoadExecutorContext
)

// modelCallPlan is the movement a boundary Step builds for a model
// continuation: the call, a compaction first or the workspace cold path that
// prepares the repository context of the first segment. load selects the
// archive chunks and prompts the model Step reads, and for a compaction the
// same ones for its failure route.
type modelCallPlan struct {
	next       modelCallNext
	compaction contextCompaction
	admission  modelAdmission
	load       *dex.StepOptions
}

// planModelCall decides the model continuation next at a boundary. The first
// segment opens once the repository context was read, or the cold path is
// asked to read it first; the cold path opens the segment either way. A turn
// start may roll the segment over, and any boundary but a direct model call
// may compact it.
func (flow *SessionAgentFlow) planModelCall(ctx dex.Context, next agentloop.Continuation) (plan modelCallPlan) {
	defer func() {
		if plan.next == nextCallModel || plan.next == nextCompactContext {
			plan.admission = modelAdmission{Authority: currentExecutor(ctx).Admitted(), Turn: comm.Must(agentStateAttribute.Get(ctx)).Turn}
			plan.compaction.Admission = plan.admission
			status := agentloop.AgentStatusCallingModel
			if plan.next == nextCompactContext {
				status = agentloop.AgentStatusCompactingContext
			}
			setAgentStatus(ctx, status)
		}
	}()
	if currentExecutor(ctx).Mode == remoteexec.ModeRemote {
		setRunningWork(ctx, &agentloop.RunningWork{Kind: agentloop.RunningRemote})
		return modelCallPlan{next: nextAwaitRemote}
	}
	executor := currentExecutor(ctx)
	if executor.Handoff != nil && executor.Handoff.Epoch == executor.Epoch {
		if loads := executorContextLoads(ctx); len(loads) > 0 {
			return modelCallPlan{next: nextLoadExecutorContext, load: &dex.StepOptions{ExecuteLoadAttributeMapInstances: loads}}
		}
		segment := flow.openExecutorSegment(ctx, executor)
		setRunningWork(ctx, &agentloop.RunningWork{Kind: agentloop.RunningModel})
		return modelCallPlan{next: nextCallModel, load: segmentLoad(comm.Must(agentStateAttribute.Get(ctx)), segment, false)}
	}
	segment := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx))
	if segment == nil {
		if dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).RepositoryContext == "" {
			return modelCallPlan{next: nextRequestWorkspace}
		}
		opened := flow.openSessionStartSegment(ctx)
		segment = &opened
	}
	// The model call or compaction is the work in flight until its Step commits.
	setRunningWork(ctx, &agentloop.RunningWork{Kind: agentloop.RunningModel})
	state := comm.Must(agentStateAttribute.Get(ctx))
	if next != agentloop.ContinueCallModel {
		selected := comm.Must(sessionModelAttribute.Get(ctx)).Model
		if reason := agentloop.SegmentChange(*segment, selected, flow.settings, state.Turn, next == agentloop.ContinueStartTurn); reason != "" {
			return modelCallPlan{next: nextCompactContext, compaction: contextCompaction{Reason: reason}, load: segmentLoad(state, *segment, true)}
		}
	}
	return modelCallPlan{next: nextCallModel, load: segmentLoad(state, *segment, false)}
}

// segmentLoad selects the archive chunks that hold the segment's entries and
// its prompts. A compaction also reads the turn's pending request, and so does
// its failure route: at the hard limit RecordCompactionFailure opens a segment
// without a summary that quotes it. The movement therefore replaces the route
// CompactContext registers with one to the same Step that loads the same
// instances; the failure Step keeps the rest of its registered options, the
// current window and the message window lock. A model call gains no load.
func segmentLoad(state agentloop.AgentState, segment agentloop.ContextSegment, withPendingRequest bool) *dex.StepOptions {
	first := segment.FirstSequence
	if withPendingRequest {
		for _, sequence := range state.TurnRequestSequences {
			first = min(first, sequence)
		}
	}
	loads := []dex.AttributeMapLoad{contextSegmentPromptsAttribute.Load(segment.PromptsKey())}
	for _, key := range agentloop.ArchiveChunksFrom(state, first) {
		loads = append(loads, archivedMessagesAttribute.Load(key))
	}
	options := &dex.StepOptions{ExecuteLoadAttributeMapInstances: loads}
	if withPendingRequest {
		options.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordCompactionFailure{}, &dex.StepOptions{ExecuteLoadAttributeMapInstances: loads})
	}
	return options
}

// segmentRequest is the model request that replays segment with its frozen
// prompts, followed by the given transient entries, which are never stored.
func (flow *SessionAgentFlow) segmentRequest(ctx dex.Context, kind agentloop.ModelCallKind, segment agentloop.ContextSegment, prompts conversation.SegmentPrompts, transient ...conversation.Entry) conversation.ModelRequest {
	state := comm.Must(agentStateAttribute.Get(ctx))
	read := retainedEntries(ctx, state)
	entries := make([]conversation.SequencedEntry, 0, int(state.LastSequence-segment.FirstSequence+1)+len(transient))
	for sequence := segment.FirstSequence; sequence <= state.LastSequence; sequence++ {
		entries = append(entries, conversation.SequencedEntry{Sequence: sequence, Entry: read(sequence)})
	}
	for index, entry := range transient {
		entries = append(entries, conversation.SequencedEntry{Sequence: state.LastSequence + conversation.Sequence(index) + 1, Entry: entry})
	}
	profile := modelprovider.ProfileOf(segment.Model)
	shape := profile.RequestShape()
	shape.ExecutorContext = segment.Reason == agentloop.SegmentReasonExecutor
	return conversation.ModelRequest{
		Model: segment.Model, Effort: segment.Effort, Prompts: prompts,
		Messages:       conversation.AssembleRequest(entries, segment.CacheAnchor, shape),
		PrefixMismatch: flow.settings.PrefixMismatch, CallID: modelCallID(ctx, kind), ToolTarget: agentloop.ToolTarget,
		WriteAssistant: discardText, WriteReasoning: discardText, WriteActivity: func(conversation.AgentEvent) error { return nil },
	}
}

func discardText(string) error { return nil }

// openSessionStartSegment opens the first segment of the Session on the
// selected model.
func (flow *SessionAgentFlow) openSessionStartSegment(ctx dex.Context) agentloop.ContextSegment {
	return flow.openSegment(ctx, agentloop.SegmentReasonSessionStart, comm.Must(sessionModelAttribute.Get(ctx)).Model, "", currentEntry)
}

// openSegment opens the next segment for reason on model with summary: one
// segment_start entry with the summary, the repository context, the confirmed
// requirements, the todo list and the turn's pending request as read finds it,
// then the notice of a reporting turn. The segment and its prompts replace the
// previous ones, and archive chunks before it become eligible for retention.
func (flow *SessionAgentFlow) openSegment(ctx dex.Context, reason agentloop.SegmentReason, model conversation.Model, summary string, read entryReader) agentloop.ContextSegment {
	previous := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx))
	start := agentloop.SegmentStart{Summary: summary, RepositoryContext: dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).RepositoryContext,
		ConfirmedRequirements: dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx)),
		TodoList:              dexflow.OptionalValue(todoListAttribute.Get(ctx)), PendingRequest: pendingRequestText(ctx, read)}
	entry := conversation.SegmentStartEntry(start.Text(), agentloop.SegmentMarker(reason))
	first := appendEntry(ctx, entry)
	segment := agentloop.NewSegment(previous, first, reason, model, flow.settings, entry.Bytes())
	segment.Summary = comm.TruncateUTF8(summary, 8<<10)
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	comm.MustDo(contextSegmentPromptsAttribute.Set(ctx, segment.PromptsKey(), agentloop.SessionPromptFor(model).Prompts))
	if previous != nil {
		comm.MustDo(contextSegmentPromptsAttribute.Delete(ctx, previous.PromptsKey()))
	}
	trimRetainedEntries(ctx)
	return comm.Must(contextSegmentAttribute.Get(ctx))
}

// pendingRequestText quotes the user messages the current turn works on that
// read finds. A compaction and its failure route load the chunks that hold
// them (segmentLoad) and read them with loadedEntry. The first segment opens in
// Steps that load no archive chunk and reads only CurrentMessages
// (currentEntry), so a message archived before it is left out.
func pendingRequestText(ctx dex.Context, read entryReader) string {
	state := comm.Must(agentStateAttribute.Get(ctx))
	var entries []conversation.Entry
	for _, sequence := range state.TurnRequestSequences {
		if entry, found := read(ctx, state, sequence); found {
			entries = append(entries, entry)
		}
	}
	return conversation.PendingRequestText(entries)
}

// modelFailureCode is a model call without a usable reply that the Session
// classifies itself, declared once with the hint that reports it; a rejection
// by the model API keeps the API's error class and has no hint.
type modelFailureCode struct{ name, hint string }

var (
	modelOutcomeTooLarge = modelFailureCode{name: "model_outcome_too_large",
		hint: "The completed provider outcome exceeded its recording limit and was discarded. Continue from the recorded tool results; do not repeat the successful provider request automatically."}
	// modelRefused ends a turn whose reply is a refusal.
	modelRefused = modelFailureCode{name: "model_refused",
		hint: "The model declined this request. Do not repeat it unchanged; follow what the user asks next."}
	// modelUnexpectedStop ends a turn whose reply stopped for a reason the
	// Session does not handle.
	modelUnexpectedStop = modelFailureCode{name: "model_unexpected_stop",
		hint: "The reply stopped for a reason the Session does not handle and was discarded. Check what the turn already did before you continue."}
	// modelUnavailable ends a turn or a compaction whose model call exhausted
	// its retries.
	modelUnavailable = modelFailureCode{name: "model_unavailable",
		hint: "The model API did not answer before the retries ran out."}
	// summaryUnavailable rejects a compaction reply without a summary.
	summaryUnavailable = modelFailureCode{name: "summary_unavailable",
		hint: "The model's reply held no usable summary of the context."}
)

// failure is the turn failure of the code with the reply's detail, if any.
func (code modelFailureCode) failure(detail string) turnFailure {
	return turnFailure{code: code.name, hint: code.hint, detail: detail}
}

// turnFailure is a bounded record of a model call that ended its turn: an
// error class, its hint and status, never provider text.
type turnFailure struct {
	code     string
	hint     string
	userText string
	status   int
	detail   string
}

// modelFailure classifies the error of a model call: a rejection keeps its
// class, status and hint, and a missing summary its hint; a call whose retries
// are exhausted is unavailable.
func modelFailure(err error) turnFailure {
	var rejected *conversation.ModelRejectedError
	switch {
	case !errors.As(err, &rejected):
		return modelUnavailable.failure("")
	case rejected.Code == summaryUnavailable.name:
		return summaryUnavailable.failure("")
	default:
		return turnFailure{code: rejected.Code, hint: rejected.Hint, userText: rejected.UserText, status: rejected.Status}
	}
}

func (failure turnFailure) description() string {
	if failure.userText != "" {
		return failure.userText
	}
	text := failure.code
	if failure.detail != "" {
		text += " (" + failure.detail + ")"
	}
	if failure.status != 0 {
		text += fmt.Sprintf(", HTTP %d", failure.status)
	}
	return text
}

// modelFailureMarkerPrefix marks the durable notice and its matching activity.
const modelFailureMarkerPrefix = "Model call failed: "

// recordTurnFailure ends a turn after a failed model call: a marker notice the
// UI shows after a reload, which the model also reads with the failure's hint,
// and the model_failed activity. The trailing gap holds the turn's user entry,
// so the notice is a system entry.
func recordTurnFailure(ctx dex.Context, failure turnFailure) {
	description := failure.description()
	punctuated := description
	if !strings.HasSuffix(punctuated, ".") {
		punctuated += "."
	}
	notice := "The previous model request ended without a usable reply: " + punctuated + " The turn ended; continue from here when the user replies."
	if failure.hint != "" {
		notice += "\nhint: " + failure.hint
	}
	sequence := appendEntry(ctx, conversation.NoticeEntry(conversation.EntryRoleSystem, notice, modelFailureMarkerPrefix+description))
	writeActivity(ctx, conversation.AgentEvent{Kind: conversation.EventKindModelFailed, Message: modelFailureMarkerPrefix + punctuated, MessageSequence: &sequence})
}

// completeCompaction returns only an outcome. A tool-bearing first reply
// schedules a separately admitted correction; it never calls the provider twice.
func (flow *SessionAgentFlow) completeCompaction(ctx dex.Context, input contextCompaction) (*platformModelOutcome, *dex.StepOptions, error) {
	if !modelAdmissionMatches(ctx, input.Admission) {
		return nil, nil, nil
	}
	if input.Admission.Turn == 0 {
		input.Admission.Turn = comm.Must(agentStateAttribute.Get(ctx)).Turn
	}
	segment := comm.Must(contextSegmentAttribute.Get(ctx))
	prompts := comm.Must(contextSegmentPromptsAttribute.Get(ctx, segment.PromptsKey()))
	request := flow.segmentRequest(ctx, agentloop.ModelCallKindCompaction, segment, prompts, conversation.NoticeEntry(conversation.EntryRoleSystem, conversation.CompactionInstruction, ""))
	request.ToolChoiceNone = input.ToolChoiceNone
	defer dexflow.KeepAlive(ctx, agentloop.CompactionHeartbeat{Phase: agentloop.HeartbeatPhaseCompacting, Segment: segment.Number})()
	reply, err := flow.modelClient.Complete(ctx, request)
	outcome := platformModelOutcome{Admission: input.Admission, Compaction: &input, StartedAt: ctx.FirstAttemptAt().UTC()}
	if rejected, ok := errors.AsType[*conversation.ModelRejectedError](err); ok {
		outcome.Failure = publicModelRejection(rejected)
	} else if err != nil {
		return nil, nil, dex.ErrorWithStack(fmt.Errorf("summarize context segment %d with %s: %w", segment.Number, segment.Model, err))
	} else if !input.ToolChoiceNone && (len(reply.ToolCalls) > 0 || reply.StopReason == conversation.StopReasonToolUse) {
		outcome.Correction = true
	} else {
		outcome.Summary = agentloop.ExtractSummary(reply.Text)
		if outcome.Summary == "" || reply.StopReason == conversation.StopReasonRefusal {
			outcome.Failure = &conversation.ModelRejectedError{Code: summaryUnavailable.name}
		}
	}
	loads := segmentLoad(comm.Must(agentStateAttribute.Get(ctx)), segment, true)
	// The outcome input differs from CompactContext; never inherit its failure route.
	loads.ExecuteFailure = nil
	outcome = boundedModelOutcome(outcome)
	return &outcome, loads, nil
}

// compactionFailed applies a compaction that could not produce a summary and
// returns where the turn continues. At the hard limit, or when a rollover was
// rejected, an unsummarized segment opens. Otherwise the turn continues in the
// old segment, which stays replayable because its prompts are stored, and the
// next turn tries again.
func (flow *SessionAgentFlow) compactionFailed(ctx dex.Context, input contextCompaction, failure turnFailure, isRejected bool) agentloop.Continuation {
	segment := comm.Must(contextSegmentAttribute.Get(ctx))
	isRollover := input.Reason == agentloop.SegmentReasonRollover
	if segment.AtHardLimit() || isRollover && isRejected {
		model := segment.Model
		if isRollover {
			model = comm.Must(sessionModelAttribute.Get(ctx)).Model
		}
		opened := flow.openSegment(ctx, agentloop.SegmentReasonUnsummarized, model, conversation.UnsummarizedContext, loadedEntry)
		writeActivity(ctx, conversation.AgentEvent{Kind: conversation.EventKindCompactionFailed, Message: compactionFailure(failure, "the earlier context was not summarized"), MessageSequence: &opened.FirstSequence})
		return agentloop.ContinueCallModel
	}
	segment.CompactionFailedTurn = comm.Must(agentStateAttribute.Get(ctx)).Turn
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	writeActivity(ctx, conversation.AgentEvent{Kind: conversation.EventKindCompactionFailed, Message: compactionFailure(failure, "the turn continues in the current context")})
	return agentloop.ContinueCallModel
}

// compactionFailure is the activity of a failed compaction: its failure, what
// became of the context and the hint of the failure, if any.
func compactionFailure(failure turnFailure, consequence string) string {
	message := "Context compaction failed: " + failure.description() + "; " + consequence + "."
	if failure.hint != "" {
		message += " " + failure.hint
	}
	return message
}

// currentExecutor preserves the additive Attribute's old-execution default.
func currentExecutor(ctx dex.Context) remoteexec.Executor {
	executor, err := executorAttribute.Get(ctx)
	if dexflow.IsAttributeNotFound(err) {
		return remoteexec.Executor{Mode: remoteexec.ModePlatform}
	}
	comm.MustDo(err)
	return executor
}

func interruptAgent(ctx dex.Context, input agentloop.InterruptAgentRequest) (agentInterruption, agentloop.InterruptPlan, error) {
	interruption, plan := admitInterrupt(ctx, input)
	if !plan.Admitted || plan.Joined {
		return interruption, plan, nil
	}
	if executor := currentExecutor(ctx); executor.Mode == remoteexec.ModeRemote {
		if executor.Generation == ^uint64(0) {
			return interruption, plan, errors.New("executor generation exhausted")
		}
		executor.Generation++
		comm.MustDo(executorAttribute.Set(ctx, executor))
	}
	return interruption, plan, nil
}

func setExecutor(ctx dex.Context, input remoteexec.SetRequest) (remoteexec.SetResult, *agentInterruption, error) {
	if err := input.Validate(); err != nil {
		return remoteexec.SetResult{}, nil, err
	}
	if !acceptsUserCommand(ctx) || input.ProjectID != comm.Must(projectIDAttribute.Get(ctx)) {
		return remoteexec.SetResult{}, nil, nil
	}
	executor := currentExecutor(ctx)
	result := executor.Change(input, time.Now().UTC())
	if !result.Changed {
		return result, nil, nil
	}
	state := comm.Must(agentStateAttribute.Get(ctx))
	pending := dexflow.OptionalValue(agentInterruptAttribute.Get(ctx))
	var interruption *agentInterruption
	if pending == nil && (state.Status != agentloop.AgentStatusWaitingForMessage && state.Status != agentloop.AgentStatusInitializing) {
		plan := agentloop.PlanInterrupt(state, agentloop.InterruptAgentRequest{Turn: state.Turn}, false, nil)
		at := time.Now().UTC()
		captured := captureForegroundStop(ctx, agentInterruption{Turn: state.Turn, RequestedAt: at, Actor: &input.Owner, EndsTurn: plan.EndsTurn,
			Running: state.Running, PreserveBackground: true})
		interruption = &captured
		comm.MustDo(agentInterruptAttribute.Set(ctx, agentloop.AgentInterrupt{Turn: state.Turn, RequestedAt: at, Actor: &input.Owner, EndsTurn: plan.EndsTurn, PreserveBackground: true}))
	}
	if !executorWakePending(ctx) {
		comm.MustDo(remoteInboxChannel.Publish(ctx, remoteexec.InboxItem{Control: &remoteexec.ExecutorChanged{Epoch: result.Executor.Epoch, Generation: result.Executor.Generation}}))
	}
	comm.MustDo(executorAttribute.Set(ctx, result.Executor))
	return result, interruption, nil
}

func reportRemote(ctx dex.Context, input remoteexec.ReportRequest) (*dex.RPCResult[remoteexec.ReportResult], error) {
	if err := input.Report.Validate(); err != nil {
		return nil, err
	}
	executor := currentExecutor(ctx)
	result := &dex.RPCResult[remoteexec.ReportResult]{}
	if !acceptsUserCommand(ctx) || input.ProjectID != comm.Must(projectIDAttribute.Get(ctx)) {
		result.Output.Refusal = &remoteexec.ReportRefusal{Code: remoteexec.RefusalSessionClosed, Message: "Session does not accept reports"}
		return result, nil
	}
	if executor.Remote != nil {
		input.Report.Owner = executor.Remote.Owner
	}
	messages := comm.Must(remoteInboxChannel.PendingMessages(ctx))
	pending := make([]remoteexec.RemoteReport, 0, len(messages))
	for _, message := range messages {
		if message.Value.Report != nil {
			pending = append(pending, *message.Value.Report)
		}
	}
	updated, answer, err := executor.AdmitReport(input.Report, pending)
	result.Output.Answer = answer
	if err != nil {
		refusal := remoteexec.ReportRefusal{Epoch: executor.Epoch, Generation: executor.Generation}
		switch {
		case errors.Is(err, remoteexec.ErrInboxFull):
			refusal.Code, refusal.Message = remoteexec.RefusalInboxFull, "remote reports are pending"
		default:
			refusal.Code, refusal.Message = remoteexec.RefusalExecutorChanged, "executor changed"
			if executor.Mode == remoteexec.ModeRemote && executor.Remote != nil && executor.Remote.ConnectionID == input.Report.ConnectionID && executor.Epoch == input.Report.Epoch {
				refusal.Code, refusal.Message = remoteexec.RefusalStaleGeneration, "remote turn generation changed"
			}
		}
		if gap, ok := errors.AsType[*remoteexec.SequenceGapError](err); ok {
			refusal.Code, refusal.Message, refusal.ExpectedSequence = remoteexec.RefusalSequenceGap, "missing report sequence", gap.Expected
		}
		result.Output.Refusal = &refusal
		return result, nil
	}
	if !answer.Duplicate {
		comm.MustDo(executorAttribute.Set(ctx, updated))
		comm.MustDo(remoteInboxChannel.Publish(ctx, remoteexec.InboxItem{Report: &input.Report}))
	}
	return result, nil
}

func executorToolNames(ctx dex.Context) []conversation.ToolName {
	if currentExecutor(ctx).Mode == remoteexec.ModeRemote {
		return agentloop.RemoteToolNames()
	}
	return comm.Must(contextSegmentAttribute.Get(ctx)).ToolNames
}

func remoteToolAllowed(call conversation.ToolCall) bool {
	if call.Name != conversation.ToolStopTask {
		return true
	}
	arguments, err := toolinput.StopTask.Decode(call.Arguments.String())
	if err != nil || len(arguments.TaskID) < 5 || len(arguments.TaskID) > 64 || arguments.TaskID[:4] != "cmd-" {
		return false
	}
	for _, character := range arguments.TaskID[4:] {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func capturedPreviewHandoff(ctx dex.Context, handoff agentloop.PreviewHandoff) agentloop.PreviewHandoff {
	identity := readSessionIdentity(ctx)
	handoff.Session = &identity
	if ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)); ws.Lease.RequestID != "" {
		handoff.WorkspaceRequestID = ws.Lease.RequestID
	}
	return handoff
}

// executorContextLoads selects exact retained chunks; at most the newest 1000
// entries contribute to an executor handoff. Missing loads retry the same
// existing boundary Step, never the model or a new context lifecycle.
func executorContextLoads(ctx dex.Context) []dex.AttributeMapLoad {
	state := comm.Must(agentStateAttribute.Get(ctx))
	first := max(state.FirstRetainedSequence, state.LastSequence-999)
	first = (first-1)/agentloop.ArchiveMessageChunkSize*agentloop.ArchiveMessageChunkSize + 1
	var loads []dex.AttributeMapLoad
	for sequence := first; sequence < state.CurrentFirstSequence; sequence += agentloop.ArchiveMessageChunkSize {
		key := agentloop.SequenceKey(sequence)
		_, err := archivedMessagesAttribute.Get(ctx, key)
		if dexflow.IsAttributeMapNotLoaded(err) {
			loads = append(loads, archivedMessagesAttribute.Load(key))
		} else {
			comm.MustDo(err)
		}
	}
	return loads
}

func (flow *SessionAgentFlow) openExecutorSegment(ctx dex.Context, executor remoteexec.Executor) agentloop.ContextSegment {
	state := comm.Must(agentStateAttribute.Get(ctx))
	read := retainedEntries(ctx, state)
	entries := make([]conversation.SequencedEntry, 0, 1000)
	for sequence := max(state.FirstRetainedSequence, state.LastSequence-999); sequence <= state.LastSequence; sequence++ {
		entries = append(entries, conversation.SequencedEntry{Sequence: sequence, Entry: read(sequence)})
	}
	first := agentloop.ExecutorContextStart(entries, flow.settings.CompactionTokens)
	previous := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx))
	summary := ""
	if previous != nil {
		summary = previous.Summary
	}
	handoff := executor.Handoff
	text := handoff.Notice(comm.Must(headCommitAttribute.Get(ctx)))
	section := func(label, value string, budget int) {
		if value != "" {
			text += "\n\n" + label + ":\n" + boundedHandoffText(value, budget)
		}
	}
	section("Older platform summary", summary, 8<<10)
	if requirements := dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx)); requirements != nil {
		section("Current requirements", (agentloop.SegmentStart{ConfirmedRequirements: requirements}).Text(), 2<<10)
	}
	if todos := dexflow.OptionalValue(todoListAttribute.Get(ctx)); todos != nil {
		section("Todos", (agentloop.SegmentStart{TodoList: todos}).Text(), 2<<10)
	}
	var commands []string
	for _, command := range loadedBackgroundCommands(ctx) {
		if command.IsRunning() {
			commands = append(commands, command.TaskID)
		}
	}
	section("Running background commands", strings.Join(commands, ", "), 1<<10)
	entry := conversation.NoticeEntry(conversation.EntryRoleUser, boundedHandoffText(text, remoteexec.MaximumHandoffBytes), "Switched to the Superverse agent")
	last := appendEntry(ctx, entry)
	if first == 0 {
		first = last
	}
	bytes := entry.Bytes()
	for _, item := range entries {
		if item.Sequence >= first {
			bytes += item.Entry.Bytes()
		}
	}
	model := comm.Must(sessionModelAttribute.Get(ctx)).Model
	segment := agentloop.NewSegment(previous, first, agentloop.SegmentReasonExecutor, model, flow.settings, bytes)
	segment.Summary = comm.TruncateUTF8(summary, 8<<10)
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	comm.MustDo(contextSegmentPromptsAttribute.Set(ctx, segment.PromptsKey(), agentloop.SessionPromptFor(model).Prompts))
	if previous != nil {
		comm.MustDo(contextSegmentPromptsAttribute.Delete(ctx, previous.PromptsKey()))
	}
	executor.Handoff = nil
	comm.MustDo(executorAttribute.Set(ctx, executor))
	trimRetainedEntries(ctx)
	return comm.Must(contextSegmentAttribute.Get(ctx))
}

func boundedHandoffText(text string, budget int) string {
	if len(text) <= budget {
		return text
	}
	const marker = "\n[earlier details omitted; open Studio for the full state]"
	return comm.TruncateUTF8(text, budget-len(marker)) + marker
}

// executorWakePending checks the actual bounded Channel snapshot, under
// executorLock. A selected wake is not a second durable authority.
func executorWakePending(ctx dex.Context) bool {
	for _, message := range comm.Must(remoteInboxChannel.PendingMessages(ctx)) {
		if message.Value.Control != nil {
			return true
		}
	}
	return false
}

func reconcileExecutorWake(ctx dex.Context) bool {
	changed := executorWakePending(ctx)
	for _, report := range comm.Must(remoteInboxChannel.GetConditionResults(ctx)) {
		if report.Control != nil {
			changed = true
		}
	}
	if changed {
		writeSnapshotRequired(ctx)
	}
	return changed
}

func settleExecutorQuestion(ctx dex.Context, changed bool) {
	if changed && dexflow.OptionalValue(pendingUserInputAttribute.Get(ctx)) != nil {
		comm.MustDo(pendingUserInputAttribute.Delete(ctx))
		appendEntry(ctx, conversation.NoticeEntry(conversation.EntryRoleUser, "The pending input was interrupted by an executor takeover.", "Input interrupted by takeover"))
		writeSnapshotRequired(ctx)
	}
}

// setExecutorRPC starts repository context preparation beside the existing conversation waiter.
func setExecutorRPC(ctx dex.Context, input remoteexec.SetRequest) (*dex.RPCResult[remoteexec.SetResult], error) {
	output, interruption, err := setExecutor(ctx, input)
	if err != nil {
		return nil, err
	}
	if interruption == nil {
		result := &dex.RPCResult[remoteexec.SetResult]{Output: output}
		if output.Changed && output.Executor.Mode == remoteexec.ModeRemote && dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).RepositoryContext == "" {
			result.NextSteps = []dex.StepMovement{dex.MovementOf(RequestSessionWorkspace{}, workspaceDemand{ResumeAt: agentloop.ContinueAwaitRemote})}
		}
		return result, nil
	}
	workspaceSteps := []dex.StepMovement{}
	if output.Changed && output.Executor.Mode == remoteexec.ModeRemote && dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).RepositoryContext == "" {
		workspaceSteps = append(workspaceSteps, dex.MovementOf(RequestSessionWorkspace{}, workspaceDemand{ResumeAt: agentloop.ContinueAwaitRemote}))
	}
	if interruption.EndsTurn {
		// Keep the cancellation set identical to InterruptAgent above.
		return (&dex.RPCResult[remoteexec.SetResult]{Output: output, NextSteps: append(workspaceSteps, dex.MovementOf(InterruptAgent{}, *interruption, dex.WithStepOptions(interruptMovementOptions(*interruption))))}).CancelSteps(CompactContext{}, CallModel{}, RecordPlatformModelResult{}, RecordModelFailure{}, RecordCompactionFailure{}, ExecuteTool{}, ExecuteParallelTool{},
			RecoverParallelToolExecution{}, AwaitParallelToolResults{}, RecordToolFailure{}, SettleForegroundCommand{}, AwaitAgentTimer{}, AwaitToolApproval{}, AwaitRemoteAgent{}), nil
	}
	return &dex.RPCResult[remoteexec.SetResult]{Output: output, NextSteps: append(workspaceSteps, dex.MovementOf(InterruptAgent{}, *interruption, dex.WithStepOptions(interruptMovementOptions(*interruption))))}, nil
}
