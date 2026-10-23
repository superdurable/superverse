// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/backgroundcommand"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
)

type parallelToolExecutionInput struct {
	ResultInstance string                `json:"result_instance"`
	Index          int                   `json:"index"`
	Call           conversation.ToolCall `json:"call"`
	Admission      remoteexec.Fence      `json:"admission"`
	Turn           int64                 `json:"turn"`
}

type toolInvocation struct {
	CredentialPrepared bool                  `json:"credential_prepared,omitempty"`
	Index              int                   `json:"index"`
	Call               conversation.ToolCall `json:"call"`
	Admission          remoteexec.Fence      `json:"admission"`
	Turn               int64                 `json:"turn"`
}

type parallelToolResult struct {
	Index  int                           `json:"index"`
	Call   conversation.ToolCall         `json:"call"`
	Result agentloop.ToolExecutionResult `json:"result"`
	Timing agentloop.ToolMessageTiming   `json:"timing"`
	// ErrorType is the engine's type of the final error of a branch whose
	// retries are exhausted, which the report of its platform fault names.
	ErrorType string `json:"error_type,omitempty"`
	// WorkspaceLost marks a branch that was not dispatched; the join discards the
	// wave and routes its calls again.
	WorkspaceLost bool `json:"workspace_lost,omitempty"`
}

// exhaustedRetriesMessage is the failure the model reads for a call whose Dex
// retries are exhausted.
const exhaustedRetriesMessage = "The platform could not complete the call: its transient faults outlasted the retries."

// pendingUserInputMessage answers a question batch while another one waits
// for the user.
const pendingUserInputMessage = "a question batch already waits for the user"

func newToolExecutionContext(
	ctx context.Context,
	attemptTimeout time.Duration,
) (context.Context, context.CancelFunc) {
	if attemptTimeout == 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, attemptTimeout)
}

// refuseToolCall answers, in RouteTool's commit, a tool the segment did not offer.
func refuseToolCall(ctx dex.Context, call conversation.ToolCall) {
	appendSessionFailure(ctx, call, toolfailure.UnknownOrDisabledTool, fmt.Sprintf("tool %q is unknown or not enabled", call.Name))
}

// pendingToolApproval is the approval the call at the cursor waits for, started at RouteTool's first attempt.
func pendingToolApproval(ctx dex.Context, call conversation.ToolCall) agentloop.PendingApproval {
	startedAt := ctx.FirstAttemptAt().UTC()
	return agentloop.PendingApproval{CallID: call.ID, ToolName: call.Name, Arguments: call.Arguments, StartedAt: &startedAt}
}

// builtInToolContinuation runs the built-in call at the cursor in the commit of RouteTool and returns
// the continuation: a timer waits, a question ends the turn like a reply does, and any other built-in
// call, a requirements card too, has its result and advances the cursor.
func builtInToolContinuation(ctx dex.Context, call conversation.ToolCall, state agentloop.AgentState) agentloop.Continuation {
	if next := routeBuiltInTool(ctx, call, state); next != "" {
		return next
	}
	return advancePendingTools(ctx, 1)
}

// routeBuiltInTool runs the built-in call at the cursor in the commit of
// RouteTool. It returns the continuation that waits for the call, or "" once
// the call has its result. Invalid arguments are a failure the model reads.
func routeBuiltInTool(ctx dex.Context, call conversation.ToolCall, state agentloop.AgentState) agentloop.Continuation {
	switch call.Name {
	case conversation.ToolWriteTodos:
		writeTodos(ctx, call, state)
	case conversation.ToolStartTimer:
		timer, err := agentloop.RequestedTimer(call, ctx.FirstAttemptAt().UTC())
		if err != nil {
			appendSessionFailure(ctx, call, toolfailure.InvalidDurationSeconds, err.Error())
			return ""
		}
		comm.MustDo(pendingTimerAttribute.Set(ctx, timer))
		writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindTimerStarted, "Durable timer started.", call))
		return agentloop.ContinueAwaitTimer
	case conversation.ToolRequestUserInput:
		return requestUserInput(ctx, call)
	case conversation.ToolConfirmRequirements:
		confirmRequirements(ctx, call)
	case conversation.ToolSetEffort:
		effort, _, err := agentloop.RequestedEffort(call)
		if err != nil {
			appendSessionFailure(ctx, call, toolfailure.InvalidEffort, err.Error())
			return ""
		}
		appendSessionToolResult(ctx, call, agentloop.ToolResultPayload{Status: agentloop.ToolResultStatusUpdated, Effort: raiseEffort(ctx, effort)})
	default:
		panic(fmt.Sprintf("%q is not a built-in tool", call.Name))
	}
	return ""
}

// writeTodos replaces the progress list. A response may write it once.
func writeTodos(ctx dex.Context, call conversation.ToolCall, state agentloop.AgentState) {
	writes := 0
	for _, pending := range state.PendingToolCalls {
		if pending.Name == conversation.ToolWriteTodos {
			writes++
		}
	}
	if writes > 1 {
		appendSessionFailure(ctx, call, toolfailure.MultipleWriteTodosCalls, "write_todos may be called once per response")
		return
	}
	items, err := agentloop.TodoItems(call)
	if err != nil {
		appendSessionFailure(ctx, call, toolfailure.InvalidTodos, err.Error())
		return
	}
	status := agentloop.ToolResultStatusUpdated
	if len(items) == 0 {
		status = agentloop.ToolResultStatusCleared
	}
	revision := replaceTodoList(ctx, items)
	appendSessionToolResult(ctx, call, agentloop.ToolResultPayload{Status: status, Revision: revision, ItemCount: len(items)})
}

// requestUserInput records the questions of the call and waits for their
// answers. Only one question batch may wait at a time.
func requestUserInput(ctx dex.Context, call conversation.ToolCall) agentloop.Continuation {
	if dexflow.OptionalValue(pendingUserInputAttribute.Get(ctx)) != nil {
		appendSessionFailure(ctx, call, toolfailure.UserInputAlreadyPending, pendingUserInputMessage)
		return ""
	}
	input, err := agentloop.RequestedUserInput(call, ctx.FirstAttemptAt().UTC())
	if err != nil {
		appendSessionFailure(ctx, call, toolfailure.InvalidUserInput, err.Error())
		return ""
	}
	return waitForUser(ctx, call, input, agentloop.ToolResultPayload{Questions: input.Questions}, fmt.Sprintf("Requested answers to %d question(s).", len(input.Questions)))
}

// confirmRequirements records the card of the call as the next version of the
// requirements, with its Flow design in FlowDesigns, and lets the turn go on:
// the user reads the card and changes it with a message. A card without a
// design drops a design a push built (RecordRequirements).
func confirmRequirements(ctx dex.Context, call conversation.ToolCall) {
	stored := dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx))
	card, design, err := agentloop.RequestedRequirements(call, stored)
	if err != nil {
		appendSessionFailure(ctx, call, toolfailure.InvalidUserInput, err.Error())
		return
	}
	recorded := agentloop.RecordRequirements(stored, card, dexflow.ValueOrZero(builtFlowDesignAttribute.Get(ctx)), ctx.FirstAttemptAt().UTC())
	recordFlowDesign(ctx, stored, recorded.Spec.Design, design)
	comm.MustDo(confirmedRequirementsAttribute.Set(ctx, recorded))
	appendSessionToolResult(ctx, call, agentloop.RecordedRequirementsResult(recorded.Revision))
	writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindToolCompleted, fmt.Sprintf("Recorded the requirements (version %d).", recorded.Revision), call))
	writeSnapshotRequired(ctx)
}

// waitForUser records the pending input of the call, answers the call as
// waiting, supersedes the calls after it and ends the turn at AwaitUser.
func waitForUser(ctx dex.Context, call conversation.ToolCall, input agentloop.PendingUserInput, result agentloop.ToolResultPayload, summary string) agentloop.Continuation {
	comm.MustDo(pendingUserInputAttribute.Set(ctx, input))
	result.Status = agentloop.ToolResultStatusWaitingForUser
	appendSessionToolResult(ctx, call, result)
	interruptPendingTools(ctx, comm.Must(agentStateAttribute.Get(ctx)).PendingToolIndex+1, agentloop.CancelledByUserInput)
	writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindUserInputRequested, summary, call))
	return agentloop.ContinueAwaitUser
}

// executeTool runs the serial call at the cursor: a command through the
// command ledger, a delegation tool on the Session's subagents and any other
// as a branch of a wave runs it.
func (flow *SessionAgentFlow) executeTool(ctx dex.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	if isRemoteWorktreeTool(call.Name) {
		execution, end := beginToolAttempt(ctx, call)
		defer end()
		return flow.executeRemoteWorktreeTool(ctx, execution, call)
	}
	if call.Name == conversation.ToolBash {
		return flow.executeCommand(ctx, call)
	}
	execution, end := beginToolAttempt(ctx, call)
	defer end()
	if call.Name == conversation.ToolReportEvolveReview {
		return flow.reportEvolveReview(ctx, execution, call)
	}
	if agentID, isSubagent := subagentOfCall(call); isSubagent || call.Name == conversation.ToolStartAgent {
		return flow.executeSubagentTool(ctx, execution, call, agentID)
	}
	return flow.runTool(ctx, execution, call)
}

// reportEvolveReview runs report_evolve_review: it reads a proposal's files
// from the change subagent's result through sandboxd, reports the review with
// the platform model usage of the review's turns and subagents to the
// Project's goal measurement through the Session Service, and records what
// the answer settles: a proposal pins its change's record, an applied or
// discarded change unpins it, and a reported review ends the Agent's review.
// A refusal is the call's failure.
func (flow *SessionAgentFlow) reportEvolveReview(ctx dex.Context, execution context.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	input, err := toolinput.ReportEvolveReview.Decode(call.Arguments.String())
	if err != nil {
		return agentloop.SessionFailureResult(toolfailure.InvalidArguments, err.Error()), nil
	}
	report, agentID, err := agentloop.RequestedEvolveReport(input)
	if err != nil {
		return agentloop.SessionFailureResult(toolfailure.InvalidArguments, err.Error()), nil
	}
	records, state := loadedSubagents(ctx), comm.Must(agentStateAttribute.Get(ctx))
	var files *evolve.ReviewFiles
	if report.Proposal != nil {
		if record, known := records[agentID]; !known || !record.IsEnded(time.Now()) {
			return agentloop.SessionFailureResult(toolfailure.AgentNotFinished, "the change subagent "+flowid.SubagentTaskID(agentID)+" is not an ended subagent of this Session"), nil
		}
		sandboxCall := newSandboxCall(ctx, call)
		sandboxCall.Arguments = sandboxtools.EvolveResultArguments(agentID, agenttasks.EvolveResultFiles(report.ReviewID))
		read, err := flow.sandbox.Run(execution, sandboxtools.ReadEvolveResult, sandboxCall)
		if err != nil || read.Failure != nil {
			return agentloop.SandboxToolResult(read), err
		}
		files = agenttasks.EvolveReviewFiles(report.ReviewID, read.EvolveFiles)
	}
	if review := state.EvolveReview; report.Phase == evolve.ReportReported && review != nil && review.ReviewID == report.ReviewID {
		report.Usage = evolveUsage(agenttasks.EvolveUsage(review.Usage, review.Agents, records))
	}
	answer, err := flow.dependency.ReportEvolveReview(execution, comm.Must(projectIDAttribute.Get(ctx)), report, files)
	if errors.Is(err, evolve.ErrInvalidReport) {
		return agentloop.SessionFailureResult(toolfailure.InvalidArguments, err.Error()), nil
	}
	if err != nil {
		return agentloop.ToolExecutionResult{}, fmt.Errorf("report review %s: %w", report.ReviewID, err)
	}
	if answer.Refusal != "" {
		return agentloop.SessionFailureResult(agenttasks.EvolveRefusalCode(answer.Refusal), strings.TrimSpace(string(answer.Refusal)+": "+answer.Detail)), nil
	}
	var changed []agenttasks.SubagentRecord
	switch report.Phase {
	case evolve.ReportReported:
		if report.Proposal != nil {
			changed = agenttasks.PinFor(records, agentID, report.ReviewID, answer.Open)
		}
		if state.EvolveReview != nil && state.EvolveReview.ReviewID == report.ReviewID {
			state.EvolveReview = nil
			comm.MustDo(agentStateAttribute.Set(ctx, state))
		}
	case evolve.ReportApplied, evolve.ReportDiscarded:
		changed = agenttasks.Unpin(records, report.ReviewID)
	}
	for _, record := range changed {
		comm.MustDo(subagentsAttribute.Set(ctx, record.AgentID, record))
	}
	return subagentToolResult(agentloop.EvolveReportResult(report, answer)), nil
}

// evolveUsage is the usage of a review's report, as goal measurement keeps it.
func evolveUsage(usage []conversation.ModelUsage) []evolve.ModelUsage {
	reported := make([]evolve.ModelUsage, 0, len(usage))
	for _, model := range usage {
		reported = append(reported, evolve.ModelUsage{Model: model.Model, Calls: model.Calls, InputTokens: model.InputTokens, OutputTokens: model.OutputTokens})
	}
	return reported
}

// runTool runs a call that reads no Session record within its attempt
// (execution): a platform call on the server for the Session's own Project,
// any other on the lease admitted for its wave. Only such calls join a wave.
func (flow *SessionAgentFlow) runTool(ctx dex.Context, execution context.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	definition := agentloop.CatalogTool(call.Name)
	if definition.Platform {
		return flow.diagnostics.ReadDiagnostics(execution, readSessionIdentity(ctx), call)
	}
	sandboxCall, err := flow.withTrustedSource(ctx, execution, call, newSandboxCall(ctx, call))
	if err != nil {
		return agentloop.ToolExecutionResult{}, err
	}
	result, err := flow.sandbox.Run(execution, definition.Run, sandboxCall)
	return agentloop.SandboxToolResult(result), err
}

// beginToolAttempt writes the progress of the call and returns the context of
// its attempt, bounded by the tool's attempt timeout, with the function that
// ends it. The attempt heartbeats because progress is sparse: commit_and_push
// reports it only between source gate stages that may each run for minutes.
func beginToolAttempt(ctx dex.Context, call conversation.ToolCall) (context.Context, func()) {
	writeToolProgress(ctx, call)
	execution, cancel := newToolExecutionContext(ctx, agentloop.CatalogTool(call.Name).AttemptTimeout)
	stopHeartbeats := dexflow.KeepAlive(ctx, agentloop.ToolHeartbeat{Phase: agentloop.HeartbeatPhaseToolCall, ToolName: call.Name})
	return execution, func() {
		stopHeartbeats()
		cancel()
	}
}

// executeBranch runs the call of one branch of a wave. A branch that found its
// workspace unreachable dispatched nothing; it owns no shared state, so the
// join decides whether the wave is repeated or the call fails.
func (flow *SessionAgentFlow) executeBranch(ctx dex.Context, input parallelToolExecutionInput) (parallelToolResult, error) {
	if ctx.Attempt() == 1 && (!input.Admission.Matches(currentExecutor(ctx)) || input.Turn != 0 && input.Turn != comm.Must(agentStateAttribute.Get(ctx)).Turn) {
		return parallelToolResult{Index: input.Index, Call: input.Call, Result: agentloop.InterruptedToolResult(input.Call, false)}, nil
	}
	execution, end := beginToolAttempt(ctx, input.Call)
	defer end()
	result, err := flow.runTool(ctx, execution, input.Call)
	branch := parallelToolResult{Index: input.Index, Call: input.Call, Result: result,
		Timing: agentloop.ToolMessageTiming{StartedAt: ctx.FirstAttemptAt(), CompletedAt: time.Now().UTC()}}
	switch {
	case errors.Is(err, sandboxtools.ErrWorkspaceUnavailable):
		branch.Result, branch.WorkspaceLost = agentloop.WorkspaceUnavailableResult(), true
	case err != nil:
		return parallelToolResult{}, dex.ErrorWithStack(fmt.Errorf("execute tool %q call %s: %w", input.Call.Name, input.Call.ID, err))
	}
	return branch, nil
}

// Polling of a running command: a status request waits up to
// sandboxtools.CommandStatusWait, and a round adds the transport's margin.
const (
	commandPollInterval = time.Second
	commandPollRound    = sandboxtools.CommandStatusWait + 10*time.Second
)

// executeCommand runs a foreground bash call within this attempt: it begins
// the command through the ledger and polls its status until it ends. The
// engine bounds the attempts by the call's timeout (commandStepOptions) and
// then settles the command in SettleForegroundCommand. From the second
// attempt on, begin passes the first attempt's time less a minute of clock
// skew, so that a replaced instance answers instance_replaced instead of
// starting the command again.
func (flow *SessionAgentFlow) executeCommand(ctx dex.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	writeToolProgress(ctx, call)
	sandboxCall := newSandboxCall(ctx, call)
	if ctx.Attempt() > 1 {
		notBefore := ctx.FirstAttemptAt().Add(-time.Minute)
		sandboxCall.NotBefore = &notBefore
	}
	result, err := flow.sandbox.Run(ctx, sandboxtools.BeginCommand, sandboxCall)
	if err != nil || !result.Pending {
		return agentloop.SandboxToolResult(result), err
	}
	err = dexflow.PollUntil(ctx, commandPollInterval, commandPollRound, func(round context.Context) (bool, error) {
		result, err = flow.sandbox.Run(round, sandboxtools.CommandStatus, sandboxCall)
		return err == nil && !result.Pending, err
	})
	if errors.Is(err, sandboxtools.ErrWorkspaceUnavailable) {
		// The command started; only a new attempt, which asks the ledger with
		// the first attempt's time, can tell whether its instance is still the
		// one that runs it. The workspace error must not reroute the call.
		return agentloop.ToolExecutionResult{}, fmt.Errorf("poll the command: the Sandbox became unreachable: %v", err) //nolint:errorlint // deliberately not ErrWorkspaceUnavailable
	}
	return agentloop.SandboxToolResult(result), err
}

// settleCommand settles a foreground bash call whose attempts ended without a
// result: a command that still runs, after its timeout or exhausted retries,
// moves to the background as a task; any other has the ledger's answer. An
// unreachable workspace is an unknown outcome. The timing is the ledger's: the
// command's start, and its end, or now for a command without one.
func (flow *SessionAgentFlow) settleCommand(ctx dex.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, agentloop.ToolMessageTiming, error) {
	settle := sandboxtools.SettleCommand
	if dexflow.RecoveryTimedOut(ctx) {
		settle = sandboxtools.MoveTimedOutCommand
	}
	defer dexflow.KeepAlive(ctx, agentloop.ToolHeartbeat{Phase: agentloop.HeartbeatPhaseToolCall, ToolName: call.Name})()
	result, err := flow.sandbox.Run(ctx, settle, newSandboxCallBase(ctx, call))
	if errors.Is(err, sandboxtools.ErrWorkspaceUnavailable) {
		return agentloop.UnfinishedToolResult(call, "The Sandbox that ran the command is unreachable, so its outcome is unknown.", dexflow.RecoveryDetail(ctx)),
			agentloop.ToolMessageTiming{CompletedAt: time.Now().UTC()}, nil
	}
	timing := agentloop.ToolMessageTiming{StartedAt: result.StartedAt, CompletedAt: result.EndedAt}
	if timing.CompletedAt.IsZero() {
		timing.CompletedAt = time.Now().UTC()
	}
	return agentloop.SandboxToolResult(result), timing, err
}

// joinParallelToolResults records the branch results of a wave in its batch and
// returns the lowest branch that found its workspace unreachable, if any, and
// the incidents of the wave's platform failures (waveIncidents). Each branch
// publishes once with its commit, and every wave has its own result instance,
// so each result matches exactly one record.
func joinParallelToolResults(batch agentloop.ToolBatchState, results []parallelToolResult) (agentloop.ToolBatchState, *parallelToolResult, []agentloop.PlatformIncident) {
	batch.Records = slices.Clone(batch.Records)
	var lost *parallelToolResult
	for _, result := range results {
		if result.WorkspaceLost && (lost == nil || result.Index < lost.Index) {
			lost = &result
		}
		index := slices.IndexFunc(batch.Records, func(record agentloop.ToolBatchRecord) bool {
			return record.Index == result.Index && record.Call.ID == result.Call.ID
		})
		if index < 0 {
			panic(fmt.Sprintf("parallel tool result for call %q matches no record of batch %s", result.Call.ID, batch.BatchID))
		}
		record := &batch.Records[index]
		record.Result, record.Timing = &result.Result, result.Timing
	}
	return batch, lost, waveIncidents(results)
}

// The functions below record tool results in the commit of the calling Step and
// move the cursor of AgentState.PendingToolCalls past the calls they finish.

// appendToolResult records the result of a call in the entry log.
func appendToolResult(ctx dex.Context, call conversation.ToolCall, result agentloop.ToolExecutionResult, timing agentloop.ToolMessageTiming) conversation.Sequence {
	entry := agentloop.ToolResultEntry(call, result, timing)
	if executor := currentExecutor(ctx); executor.Mode == remoteexec.ModeRemote && executor.Remote != nil && remoteexec.IsCallID(executor.Epoch, call.ID) {
		via := executor.Remote.ConnectionID
		entry.Meta.Via = &via
	}
	return appendEntry(ctx, entry)
}

// appendSessionToolResult records the success the Session decided for a
// built-in call.
func appendSessionToolResult(ctx dex.Context, call conversation.ToolCall, payload agentloop.ToolResultPayload) {
	appendToolResult(ctx, call, agentloop.ToolResult(payload), agentloop.ToolMessageTiming{})
}

// appendSessionFailure records the failure the Session decided for a call.
func appendSessionFailure(ctx dex.Context, call conversation.ToolCall, code toolfailure.Code, message string) {
	appendToolResult(ctx, call, agentloop.SessionFailureResult(code, message), agentloop.ToolMessageTiming{})
}

// advancePendingTools moves the cursor past count finished calls and returns
// where the conversation continues.
func advancePendingTools(ctx dex.Context, count int) agentloop.Continuation {
	state, next := agentloop.AdvanceToolCursor(comm.Must(agentStateAttribute.Get(ctx)), count)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	return next
}

// interruptPendingTools answers the pending calls from index on with an
// interrupted result, clears the cursor and returns the interrupted calls.
func interruptPendingTools(ctx dex.Context, from int, reason agentloop.CancelReason) []conversation.ToolCall {
	state, interrupted := agentloop.CancelPendingToolCalls(comm.Must(agentStateAttribute.Get(ctx)), from)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	for _, call := range interrupted {
		appendToolResult(ctx, call, agentloop.CancelledToolResult(reason), agentloop.ToolMessageTiming{})
	}
	return interrupted
}

// cancelTimerBySteering answers the timer call at the cursor, and every call after it, cancelled by
// steering and begins the turn of the steered messages.
func cancelTimerBySteering(ctx dex.Context, state agentloop.AgentState, call conversation.ToolCall, steered []conversation.PendingUserMessage) {
	writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindTimerResolved, "Durable timer interrupted.", call))
	appendToolResult(ctx, call, agentloop.CancelledToolResult(agentloop.CancelledBySteering), agentloop.ToolMessageTiming{})
	interruptPendingTools(ctx, state.PendingToolIndex+1, agentloop.CancelledBySteering)
	beginSteeredTurn(ctx, steered)
}

// completeTimer records the result of the timer call at the cursor once its duration passed.
func completeTimer(ctx dex.Context, call conversation.ToolCall, timer agentloop.PendingTimer) {
	writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindTimerResolved, "Durable timer completed.", call))
	appendSessionToolResult(ctx, call, agentloop.ToolResultPayload{Status: agentloop.ToolResultStatusCompleted, DurationSeconds: timer.DurationSeconds, Reason: timer.Reason})
}

// finishToolExecution records the result of the serial call at the cursor.
func finishToolExecution(ctx dex.Context, call conversation.ToolCall, result agentloop.ToolExecutionResult, timing agentloop.ToolMessageTiming) agentloop.Continuation {
	index := comm.Must(agentStateAttribute.Get(ctx)).PendingToolIndex
	return finishToolBatch(ctx, agentloop.ToolBatchState{FirstIndex: index, Records: []agentloop.ToolBatchRecord{{Index: index, Call: call, Result: &result, Timing: timing}}})
}

// finishToolBatch records the results of a finished wave, ends its use of the
// workspace and moves the cursor past it.
func finishToolBatch(ctx dex.Context, batch agentloop.ToolBatchState) agentloop.Continuation {
	appendToolBatchResults(ctx, batch)
	endToolWork(ctx)
	state := comm.Must(agentStateAttribute.Get(ctx))
	if state.PendingToolIndex != batch.FirstIndex {
		panic("tool batch no longer matches the pending cursor")
	}
	state, next := agentloop.FinishToolBatch(state, batch)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	return next
}

// appendToolBatchResults records the results of a batch in call order. A
// verified remote HEAD becomes the Session HEAD in the commit of the result
// that reports it, and the commit a pushed, unblocked commit_and_push hands
// off becomes the Preview handoff at the result's sequence, which the result's
// decision publishes asynchronously; recordDesignProgress marks the designed
// Steps a write or edit declares and the design that result built, after
// recordStartingExampleBuild recorded what a matched Session's first build
// kept of its example.
func appendToolBatchResults(ctx dex.Context, batch agentloop.ToolBatchState) {
	records := slices.SortedFunc(slices.Values(batch.Records), func(left, right agentloop.ToolBatchRecord) int { return left.Index - right.Index })
	for _, record := range records {
		sequence := appendToolResult(ctx, record.Call, *record.Result, record.Timing)
		confirmSessionHead(ctx, record.Result.HeadCommit, time.Now().UTC())
		recordStartingExampleBuild(ctx, record.Call, *record.Result)
		recordDesignProgress(ctx, record.Call, *record.Result)
		if handoff := dexflow.ValueOrZero(previewHandoffAttribute.Get(ctx)); handoff.HandOff(record.Call, *record.Result, sequence) {
			comm.MustDo(previewHandoffAttribute.Set(ctx, handoff))
		}
		writeActivity(ctx, agentloop.ToolTerminalEvent(record.Call, *record.Result))
	}
}

// The platform faults that recorded tool results hold, which the Step
// ReportPlatformIssue reports beside the turn to the PlatformIssueFlow of
// each fault's signature.

// toolIncident is the incident of a tool result whose failure the platform
// owns, with the type of the error that moved the call to the failure-route
// Step recording it, if any.
func toolIncident(ctx dex.Context, call conversation.ToolCall, result agentloop.ToolExecutionResult) (agentloop.PlatformIncident, bool) {
	return agentloop.ToolIncident(call, result, dexflow.RecoveryErrorType(ctx), time.Now().UTC())
}

// unfinishedIncident is the incident of a call the platform could not finish
// (agentloop.UnfinishedToolResult), whose failure the platform always owns.
func unfinishedIncident(ctx dex.Context, call conversation.ToolCall, result agentloop.ToolExecutionResult) agentloop.PlatformIncident {
	incident, isPlatform := toolIncident(ctx, call, result)
	if !isPlatform {
		panic(fmt.Sprintf("the unfinished result of call %s is not a platform failure", call.ID))
	}
	return incident
}

// waveIncidents are the incidents of the platform failures among the results
// of a wave, in call order and one per signature: the reports of one
// signature would only join one PlatformIssueFlow run.
func waveIncidents(results []parallelToolResult) []agentloop.PlatformIncident {
	var incidents []agentloop.PlatformIncident
	signatures, now := map[string]bool{}, time.Now().UTC()
	for _, result := range slices.SortedFunc(slices.Values(results), func(left, right parallelToolResult) int { return left.Index - right.Index }) {
		if incident, isPlatform := agentloop.ToolIncident(result.Call, result.Result, result.ErrorType, now); isPlatform && !signatures[incident.Signature()] {
			signatures[incident.Signature()] = true
			incidents = append(incidents, incident)
		}
	}
	return incidents
}

// The background commands of the agent: the record a background start
// creates, the watch input of the BackgroundCommandFlow that follows the
// command in the Sandbox's ledger, and the stop of every running one.

// loadedBackgroundCommands reads every BackgroundCommands record the calling
// Step or RPC loaded.
func loadedBackgroundCommands(ctx dex.Context) map[string]agenttasks.BackgroundCommand {
	commands := map[string]agenttasks.BackgroundCommand{}
	for _, taskID := range backgroundCommandsAttribute.AllInstanceKeys(ctx) {
		commands[taskID] = comm.Must(backgroundCommandsAttribute.Get(ctx, taskID))
	}
	return commands
}

// recordBackgroundCommand records the task of a command that runs in the
// background, in the commit of its tool result, and returns the watch of the
// BackgroundCommandFlow that follows it on the instance that runs it. The
// oldest ended records beyond the retained ones make room.
func recordBackgroundCommand(ctx dex.Context, call conversation.ToolCall, start sandboxtools.BackgroundStart) backgroundcommand.BackgroundCommandWatch {
	for _, expired := range agenttasks.ExpiredBackgroundCommands(loadedBackgroundCommands(ctx)) {
		comm.MustDo(backgroundCommandsAttribute.Delete(ctx, expired))
	}
	lease := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).Lease
	comm.MustDo(backgroundCommandsAttribute.Set(ctx, start.TaskID, agenttasks.BackgroundCommand{
		TaskID: start.TaskID, CallID: call.ID, Description: start.Description, WorkspaceRequestID: lease.RequestID,
		Status: agenttasks.BackgroundCommandRunning, StartedAt: start.StartedAt, Deadline: start.Deadline,
	}))
	writeSnapshotRequired(ctx)
	return backgroundcommand.BackgroundCommandWatch{TaskID: start.TaskID, SessionFlowID: ctx.FlowID(), Lease: lease}
}

// backgroundCommandStop is the message of BackgroundCommandStops, under a running command's task
// ID, with which the Session's close ends that command's AwaitBackgroundCommand.
type backgroundCommandStop struct{}

// stopBackgroundCommandWatches publishes the stop of every running command's watch, in the commit
// that closes the Session, so the close completes only after each AwaitBackgroundCommand ended.
func stopBackgroundCommandWatches(ctx dex.Context) {
	for _, command := range agenttasks.RunningBackgroundCommands(loadedBackgroundCommands(ctx)) {
		comm.MustDo(backgroundCommandStopsChannel.Publish(ctx, command.TaskID, backgroundCommandStop{}))
	}
}

// watchedCommandEnd is how the watched command ended: as its BackgroundCommandFlow observed it
// (backgroundcommand.WatchedEnd), or stopped when the Session's close stopped the watch, which
// stops the still-running SubFlow through the Session Service; a SubFlow that already ended is
// stopped.
func (flow *SessionAgentFlow) watchedCommandEnd(ctx dex.Context, watch backgroundcommand.BackgroundCommandWatch) (agenttasks.BackgroundCommandEnd, error) {
	if len(comm.Must(backgroundCommandStopsChannel.GetConditionResults(ctx, watch.TaskID))) == 0 {
		return backgroundcommand.WatchedEnd(ctx), nil
	}
	subFlowID := comm.Must(dex.SubFlowID(ctx))
	if err := flow.dependency.StopBackgroundCommandWatch(ctx, subFlowID); err != nil {
		return agenttasks.BackgroundCommandEnd{}, dex.ErrorWithStack(fmt.Errorf("stop the watch %s of background command %s: %w", subFlowID, watch.TaskID, err))
	}
	return agenttasks.BackgroundCommandEnd{EndReason: agenttasks.BackgroundCommandEndStopped, EndedAt: time.Now().UTC()}, nil
}

// stopTasks stops, idempotently by their ledger IDs, the foreground command of a
// stopped bash call and every background command that runs on the current
// instance, and every running subagent through the Session Service, which
// saves its changes and reports as stopped. Each stopped command's watch
// records it stopped, with no notification, and a Sandbox that is gone ended
// its commands with it. Every stop is tried before a failure is returned, so
// one task that cannot be stopped now delays no other; the retry repeats them
// all. A subagent not admitted yet is stopped by FinishInterrupt or the turn's
// end, which deny its admission.
func (flow *SessionAgentFlow) stopTasks(ctx dex.Context, interruption agentInterruption) error {
	defer dexflow.KeepAlive(ctx, agentloop.ToolHeartbeat{Phase: agentloop.HeartbeatPhaseToolCall, ToolName: conversation.ToolStopTask})()
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	var taskIDs []string
	var failures []error
	if running := interruption.Running; interruption.EndsTurn && running != nil && running.Kind == agentloop.RunningTool && running.ToolName == conversation.ToolBash {
		stopped, err := originalForegroundStop(ctx, interruption)
		if err == nil {
			taskID := string(sandboxd.CommandTaskID(sandboxd.InvocationIdentity{FlowID: ctx.FlowID(), CallID: string(stopped.CallID)}))
			_, err = flow.sandbox.Run(ctx, sandboxtools.StopTask, taskCall(ctx, *stopped.Lease, taskID))
			// An unreachable captured lease proves no Stop effect, never command success.
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("stop original foreground command: %w", err))
		}
	}
	for _, command := range agenttasks.RunningBackgroundCommands(loadedBackgroundCommands(ctx)) {
		if interruption.PreserveBackground {
			break
		}
		if command.WorkspaceRequestID == ws.Lease.RequestID {
			taskIDs = append(taskIDs, command.TaskID)
		}
	}
	for _, taskID := range taskIDs {
		_, err := flow.sandbox.Run(ctx, sandboxtools.StopTask, taskCall(ctx, ws.Lease, taskID))
		if errors.Is(err, sandboxtools.ErrWorkspaceUnavailable) {
			break
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("stop task %s: %w", taskID, err))
		}
	}
	for _, record := range agenttasks.UnfinishedSubagents(loadedSubagents(ctx), time.Now()) {
		if record.Status != agenttasks.SubagentRunning {
			continue
		}
		if _, err := flow.dependency.StopSubagent(ctx, subagentIdentity(ctx, record.AgentID)); err != nil {
			failures = append(failures, fmt.Errorf("stop subagent %s: %w", record.AgentID, err))
		}
	}
	return errors.Join(failures...)
}
