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
	"encoding/json"
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
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/backgroundcommand"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput/generated"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

type parallelToolExecutionInput struct {
	ResultInstance string                `json:"result_instance"`
	Index          int                   `json:"index"`
	Call           conversation.ToolCall `json:"call"`
	Admission      remoteexec.Fence      `json:"admission"`
	Turn           int64                 `json:"turn"`
}

type toolInvocation struct {
	Index     int                   `json:"index"`
	Call      conversation.ToolCall `json:"call"`
	Admission remoteexec.Fence      `json:"admission"`
	Turn      int64                 `json:"turn"`
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
// the user reads the card and changes it with a message.
func confirmRequirements(ctx dex.Context, call conversation.ToolCall) {
	stored := dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx))
	card, design, err := agentloop.RequestedRequirements(call, stored)
	if err != nil {
		appendSessionFailure(ctx, call, toolfailure.InvalidUserInput, err.Error())
		return
	}
	recorded := agentloop.RecordRequirements(stored, card, ctx.FirstAttemptAt().UTC())
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

// serialToolStepOptions is the execution policy of one serial ExecuteTool
// movement. A call whose retries are exhausted is recorded as a platform
// failure the model reads; a bash call has the polling options of its own
// timeout and is settled from the command ledger.
func serialToolStepOptions(call conversation.ToolCall) *dex.StepOptions {
	if call.Name == conversation.ToolBash {
		return commandStepOptions(call)
	}
	options := parallelToolStepOptions(agentloop.CatalogTool(call.Name))
	options.ExecuteLoadAttributeMaps = messageMaps
	configureConnectorToolOptions(options, call)
	configureTaskWorktreeToolOptions(options, call)
	switch agentID, isSubagent := subagentOfCall(call); {
	case call.Name == conversation.ToolStartAgent:
		// The new worktree keeps the worktrees of the unfinished subagents.
		options.ExecuteLoadAttributeMaps = taskMessageMaps
	case isSubagent:
		options.ExecuteLoadAttributeMapInstances = []dex.AttributeMapLoad{subagentsAttribute.Load(agentID)}
	}
	options.ExecuteFailure = ExecuteTool{}.GetStepOptions().ExecuteFailure
	return options
}

// commandStepOptions is the policy of an ExecuteTool movement for bash: a
// polling Step bounded by the call's own timeout, whose failure route settles
// the command from the Sandbox's command ledger.
func commandStepOptions(call conversation.ToolCall) *dex.StepOptions {
	options := dexflow.PollingStepOptions(sandboxtools.CommandTimeout(call.Arguments.String()), ExecuteTool{command: true}.GetStepOptions().ExecuteFailure)
	options.ExecuteLoadAttributeMaps = []dex.AttributeDef{currentMessagesAttribute, backgroundCommandsAttribute}
	return options
}

// parallelToolStepOptions is the execution policy of one ExecuteParallelTool
// branch. A branch whose retries are exhausted always publishes a result, so
// that the join can resolve the batch.
func parallelToolStepOptions(definition agentloop.ToolDefinition) *dex.StepOptions {
	return &dex.StepOptions{
		ExecuteMethodTimeout:     definition.AttemptTimeout,
		ExecuteLoadAttributeMaps: remoteWorktreeFileMapLoads(definition.Name),
		HeartbeatTimeout:         time.Minute,
		ExecuteDurability:        toolExecuteDurability(definition),
		ExecuteRetry: &dex.RetryPolicy{
			InitialInterval: agentloop.ToolRetryInitialInterval, BackoffCoefficient: agentloop.ToolRetryBackoffCoefficient, MaximumInterval: agentloop.ToolRetryMaximumInterval,
			// #nosec G115 -- the catalog init check bounds MaximumAttempts to int32.
			MaximumAttempts: int32(definition.MaximumAttempts), TotalDuration: definition.RetryTotalDuration,
		},
		ExecuteFailure: ExecuteParallelTool{}.GetStepOptions().ExecuteFailure,
	}
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
	if agentID, isSubagent := subagentOfCall(call); isSubagent || call.Name == conversation.ToolStartAgent {
		return flow.executeSubagentTool(ctx, execution, call, agentID)
	}
	return flow.runTool(ctx, execution, call)
}

// runTool runs a call that reads no Session record within its attempt
// (execution): a platform call on the server for the Session's own Project,
// any other on the lease admitted for its wave. Only such calls join a wave.
func (flow *SessionAgentFlow) runTool(ctx dex.Context, execution context.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	definition := agentloop.CatalogTool(call.Name)
	if definition.Platform {
		return flow.diagnostics.ReadDiagnostics(execution, readSessionIdentity(ctx), call)
	}
	result, err := flow.sandbox.Run(execution, definition.Run, newSandboxCall(ctx, call))
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

// toolExecuteDurability starts a long-running call directly as a synchronous
// activity; others use the default asynchronous local execution.
func toolExecuteDurability(definition agentloop.ToolDefinition) dex.StepDurability {
	if definition.LongRunning {
		return dex.StepDurabilitySync
	}
	return dex.StepDurabilityDefault
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
// Steps a write or edit declares and the design that result built.
func appendToolBatchResults(ctx dex.Context, batch agentloop.ToolBatchState) {
	records := slices.SortedFunc(slices.Values(batch.Records), func(left, right agentloop.ToolBatchRecord) int { return left.Index - right.Index })
	for _, record := range records {
		sequence := appendToolResult(ctx, record.Call, *record.Result, record.Timing)
		confirmSessionHead(ctx, record.Result.HeadCommit, time.Now().UTC())
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

// The subagents of the agent: the admission of a start_agent call, the
// start, stop and merge of a subagent, the records its admission and report
// write, and the Stop of the ones not admitted yet. A record is the Session's
// view of its subagent; the subagent's SubagentFlow owns its execution and
// reaches the Session only through the Session Service.

// subagentStarted is what start_agent answers: the task ID the report will
// name and the snapshot the worktree started from, with the library branch of
// a develop subagent, or the status of a subagent a Stop ended before it
// started.
type subagentStarted struct {
	TaskID     string `json:"task_id"`
	BaseCommit string `json:"base_commit,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Status     string `json:"status,omitempty"`
}

// loadedSubagents reads every Subagents record the calling Step or RPC loaded.
func loadedSubagents(ctx dex.Context) map[string]agenttasks.SubagentRecord {
	records := map[string]agenttasks.SubagentRecord{}
	for _, agentID := range subagentsAttribute.AllInstanceKeys(ctx) {
		records[agentID] = comm.Must(subagentsAttribute.Get(ctx, agentID))
	}
	return records
}

// subagentIdentity is the identity of the Session's subagent agentID.
func subagentIdentity(ctx dex.Context, agentID string) agenttasks.SubagentIdentity {
	return agenttasks.SubagentIdentity{ProjectID: comm.Must(projectIDAttribute.Get(ctx)), AgentID: agentID}
}

// subagentOfCall is the agent a stop_task call of a subagent's task, or a
// merge_agent_changes call, names; false for any other call, such as the
// stop of a command, and for arguments that name no agent.
func subagentOfCall(call conversation.ToolCall) (string, bool) {
	switch call.Name {
	case conversation.ToolStopTask:
		if input, err := toolinput.StopTask.Decode(call.Arguments.String()); err == nil && strings.HasPrefix(input.TaskID, flowid.SubagentTaskID("")) {
			return flowid.ParseSubagentTaskID(input.TaskID)
		}
	case conversation.ToolMergeAgentChanges:
		if input, err := toolinput.MergeAgentChanges.Decode(call.Arguments.String()); err == nil {
			return flowid.ParseSubagentTaskID(input.AgentID)
		}
	default:
		// Any other tool names no agent.
	}
	return "", false
}

// isSubagentRunning reports whether the record of agentID, which the calling
// RPC loaded, admitted its subagent and has not ended.
func isSubagentRunning(ctx dex.Context, agentID string) bool {
	record, err := subagentsAttribute.Get(ctx, agentID)
	if dexflow.IsAttributeNotFound(err) {
		return false
	}
	return comm.Must(record, err).Status == agenttasks.SubagentRunning
}

// admitSubagentCall admits start_agent in RouteTool's commit. A previously
// admitted call runs again. Otherwise fewer than three may run and fewer than
// six may start since the user's last message (AdmitSubagentStart). Admission
// writes its STARTING record; rejection answers the model without an effect.
func admitSubagentCall(ctx dex.Context, call conversation.ToolCall) bool {
	requested, err := agentloop.RequestedSubagent(call)
	if err != nil {
		appendSessionFailure(ctx, call, toolfailure.InvalidArguments, err.Error())
		return false
	}
	agentID, records, now := flowid.AgentTaskID(ctx.FlowID(), string(call.ID)), loadedSubagents(ctx), time.Now().UTC()
	if _, isAdmitted := records[agentID]; isAdmitted {
		return true
	}
	if requested.Connector != nil && remoteConnectorOpen(ctx, requested.Connector.Slug) {
		appendSessionFailure(ctx, call, toolfailure.ConnectorContributionRunning, "a remote agent already works on this connector")
		return false
	}
	state := comm.Must(agentStateAttribute.Get(ctx))
	if code, message, isAdmitted := agenttasks.AdmitSubagentStart(records, requested, state.SubagentsStartedWithoutUser, connectorContributionSlugs(ctx, state.ConnectorSlugs), now); !isAdmitted {
		appendSessionFailure(ctx, call, code, message)
		return false
	}
	// Keep one cleanup ID until confirmed; later starts retry it without eviction.
	if dexflow.ValueOrZero(pendingSubagentWorktreeCleanupAttribute.Get(ctx)) == "" {
		if expired := agenttasks.ExpiredSubagents(records); len(expired) > 0 {
			comm.MustDo(pendingSubagentWorktreeCleanupAttribute.Set(ctx, expired[0]))
			comm.MustDo(subagentsAttribute.Delete(ctx, expired[0]))
		}
	}
	comm.MustDo(subagentsAttribute.Set(ctx, agentID, agenttasks.NewSubagentRecord(agentID, call.ID, requested, requested.Fingerprint(ctx.FlowID(), agentID), now)))
	state.SubagentsStartedWithoutUser, state.ConnectorSlugs = state.SubagentsStartedWithoutUser+1, agenttasks.ConnectorSlugsWith(state.ConnectorSlugs, requested)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	writeSnapshotRequired(ctx)
	return true
}

// executeSubagentTool runs a delegation tool within the call's attempt
// (execution): start_agent, the stop_task of a subagent's task or
// merge_agent_changes of the agent the call names.
func (flow *SessionAgentFlow) executeSubagentTool(ctx dex.Context, execution context.Context, call conversation.ToolCall, agentID string) (agentloop.ToolExecutionResult, error) {
	switch call.Name {
	case conversation.ToolStartAgent:
		return flow.startSubagent(ctx, execution, call)
	case conversation.ToolStopTask:
		return flow.stopSubagentTask(ctx, execution, agentID)
	case conversation.ToolMergeAgentChanges:
		return flow.mergeSubagentChanges(ctx, execution, call, agentID)
	default:
		panic(fmt.Sprintf("%q is no delegation tool", call.Name))
	}
}

// startSubagent runs an admitted start_agent call: it creates the subagent's
// worktree, keeping the worktrees of the Session's other unfinished
// subagents, and starts its SubagentFlow through the Session Service with the
// call, the snapshot, the Session's model and HEAD and the admitted lease.
// Both effects are idempotent, so a retry repeats neither. A worktree the
// Sandbox refuses (snapshot_too_large, workspace_disk_low) is the call's
// failure: a refusal proves that no attempt created the worktree, so no
// attempt started the subagent, and the record is deleted and the start no
// longer counts. Any other failure, and a lost Sandbox after the first
// attempt, may follow an attempt that started the subagent: it is retried,
// not rerouted, and exhausted retries fail the start in RecordToolFailure,
// which keeps the record of a subagent that runs. A record that a Stop ended
// before starts nothing.
func (flow *SessionAgentFlow) startSubagent(ctx dex.Context, execution context.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	agentID, records := flowid.AgentTaskID(ctx.FlowID(), string(call.ID)), loadedSubagents(ctx)
	record, isRecorded := records[agentID]
	switch {
	case !isRecorded:
		panic(fmt.Sprintf("start_agent call %s runs without the record its route admitted", call.ID))
	case record.Status.Finished():
		return subagentToolResult(subagentStarted{TaskID: record.TaskID(), Status: record.Status.String()}), nil
	}
	keep := make([]string, 0, len(records))
	for _, unfinished := range agenttasks.UnfinishedSubagents(records, time.Now()) {
		keep = append(keep, unfinished.AgentID)
	}
	keep = append(keep, openTaskWorktreeIDs(ctx)...)
	// A connector_develop subagent works in a worktree of the connectors library.
	sandboxCall, create := newSandboxCall(ctx, call), sandboxtools.CreateAgentWorktree
	sandboxCall.Arguments = sandboxtools.AgentWorktreeArguments(agentID, keep)
	if record.Connector != nil {
		create, sandboxCall.Arguments = sandboxtools.CreateContributionWorktree, sandboxtools.ContributionArguments(subagentIdentity(ctx, agentID).Contribution(*record.Connector), "", false)
	}
	created, err := flow.sandbox.Run(execution, create, sandboxCall)
	switch {
	case errors.Is(err, sandboxtools.ErrWorkspaceUnavailable) && ctx.Attempt() > 1:
		return agentloop.ToolExecutionResult{}, fmt.Errorf("create the worktree of subagent %s: the Sandbox became unreachable: %v", agentID, err) //nolint:errorlint // deliberately not ErrWorkspaceUnavailable
	case err != nil:
		return agentloop.ToolExecutionResult{}, err
	case created.Failure != nil && !git.IsCreationRefusal(created.Failure.Code):
		return agentloop.ToolExecutionResult{}, fmt.Errorf("create the worktree of subagent %s: %s", agentID, created.Failure.Text())
	case created.Failure != nil:
		forgetSubagentStart(ctx, agentID)
		return agentloop.SandboxToolResult(created), nil
	}
	started := subagentStarted{TaskID: record.TaskID()}
	if created.Contribution != nil {
		started.BaseCommit, started.Branch = created.Contribution.Base, created.Contribution.Branch
	} else {
		started.BaseCommit = created.Worktree.Base
	}
	start := agenttasks.SubagentStart{Identity: subagentIdentity(ctx, agentID), Call: comm.Must(agentloop.RequestedSubagent(call)), Fingerprint: record.Fingerprint,
		BaseCommit: started.BaseCommit, Branch: started.Branch, Model: comm.Must(sessionModelAttribute.Get(ctx)).Model, HeadCommit: comm.Must(headCommitAttribute.Get(ctx)), Lease: *sandboxCall.Lease}
	if err := flow.dependency.StartSubagent(execution, start); err != nil {
		return agentloop.ToolExecutionResult{}, fmt.Errorf("start subagent %s: %w", agentID, err)
	}
	return subagentToolResult(started), nil
}

// subagentToolResult encodes the answer of a delegation tool that succeeded.
func subagentToolResult(payload any) agentloop.ToolExecutionResult {
	return agentloop.ToolExecutionResult{Content: string(comm.Must(json.Marshal(payload))), Outcome: conversation.ToolOutcomeSucceeded}
}

// stopSubagentTask runs stop_task for the task of the Session's subagent
// agentID: an unknown agent is task_not_found, an ended one answers its
// status, a lost one says so, and any other is asked to stop through the
// Session Service. It then saves its changes and reports as stopped; one not
// admitted yet stops right after its admission.
func (flow *SessionAgentFlow) stopSubagentTask(ctx dex.Context, execution context.Context, agentID string) (agentloop.ToolExecutionResult, error) {
	record, err := subagentsAttribute.Get(ctx, agentID)
	if dexflow.IsAttributeNotFound(err) {
		return agentloop.SessionFailureResult(toolfailure.TaskNotFound, "no subagent of this Session has the task ID "+flowid.SubagentTaskID(agentID)), nil
	}
	switch record = comm.Must(record, err); {
	case record.Status.Finished():
		return agentloop.ToolExecutionResult{Content: fmt.Sprintf("Task %s: %s.", record.TaskID(), record.Status), Outcome: conversation.ToolOutcomeSucceeded}, nil
	case record.IsLost(time.Now()):
		return agentloop.ToolExecutionResult{Content: fmt.Sprintf("Task %s: lost. It has not reported within an hour after its time limit; "+
			"merge_agent_changes merges what its worktree holds, and a late report still arrives as a task notification.", record.TaskID()), Outcome: conversation.ToolOutcomeSucceeded}, nil
	}
	ended, err := flow.dependency.StopSubagent(execution, subagentIdentity(ctx, agentID))
	if err != nil {
		return agentloop.ToolExecutionResult{}, fmt.Errorf("stop subagent %s: %w", agentID, err)
	}
	content := fmt.Sprintf("Task %s: stopping. It saves its changes and reports as stopped in a task notification.", record.TaskID())
	if ended {
		content = fmt.Sprintf("Task %s: ended. Its report arrives as a task notification.", record.TaskID())
	}
	return agentloop.ToolExecutionResult{Content: content, Outcome: conversation.ToolOutcomeSucceeded}, nil
}

// mergeSubagentChanges runs merge_agent_changes for the Session's subagent
// agentID: an unknown agent is task_not_found and a running one
// agent_not_finished, while a lost one counts as ended. The Sandbox tool
// merges or discards the saved changes, and first saves changes that the
// subagent's end could not save from its worktree, idempotently through the
// request's receipt. A discard or a merge of every file closes the record with
// its receipt in the commit of the result, and every later call answers that
// receipt; a merge of selected paths leaves the record open, so the other
// files stay mergeable. Only a subagent that left nothing to merge is answered
// without the Sandbox.
func (flow *SessionAgentFlow) mergeSubagentChanges(ctx dex.Context, execution context.Context, call conversation.ToolCall, agentID string) (agentloop.ToolExecutionResult, error) {
	record, err := subagentsAttribute.Get(ctx, agentID)
	if dexflow.IsAttributeNotFound(err) {
		return agentloop.SessionFailureResult(toolfailure.TaskNotFound, "no subagent of this Session has the task ID "+flowid.SubagentTaskID(agentID)), nil
	}
	if record = comm.Must(record, err); !record.IsEnded(time.Now()) {
		return agentloop.SessionFailureResult(toolfailure.AgentNotFinished, fmt.Sprintf("subagent %s is %s", record.TaskID(), record.Status)), nil
	}
	input := comm.Must(toolinput.MergeAgentChanges.Decode(call.Arguments.String()))
	isDiscard := input.Action == generated.MergeAgentChangesActionDiscard
	var merged sandboxtools.Result
	if record.HoldsNoChanges() {
		receipt := git.MergeReceipt{Outcome: git.MergeNothing, Applied: []string{}, Conflicts: []git.MergeConflict{}, Unchanged: []string{}, Unknown: input.Paths}
		if isDiscard {
			receipt.Outcome = git.MergeDiscarded
		}
		merged = sandboxtools.MergeResult(agentID, receipt)
	} else if merged, err = flow.sandbox.Run(execution, sandboxtools.MergeAgentChanges, newSandboxCall(ctx, call)); err != nil {
		return agentloop.ToolExecutionResult{}, err
	}
	if merged.Merge != nil && !record.Status.Closed() && (isDiscard || len(input.Paths) == 0) {
		comm.MustDo(subagentsAttribute.Set(ctx, agentID, record.Close(*merged.Merge)))
		writeSnapshotRequired(ctx)
	}
	return agentloop.SandboxToolResult(merged), nil
}

// recordSubagentEnd records the end that a subagent's task notification
// reports in the record, which the Session Service locked and loaded with the
// report, also for a Session that is no longer ACTIVE; a record that ended
// before keeps its end. Any other notification records nothing.
// It runs in an RPC, which writes no Stream: the Step that delivers the
// notification tells live readers, and a Snapshot read shows the record.
func recordSubagentEnd(ctx dex.Context, notification conversation.SessionNotification) error {
	agentID, isSubagent := flowid.ParseSubagentTaskID(notification.TaskID)
	if notification.Source != conversation.NotificationSourceSubagent || !isSubagent {
		return nil
	}
	record, err := subagentsAttribute.Get(ctx, agentID)
	if dexflow.IsAttributeNotFound(err) {
		return nil
	}
	ended, err := comm.Must(record, err).Finish(notification)
	if err != nil {
		return err
	}
	if ended.Status != record.Status {
		comm.MustDo(subagentsAttribute.Set(ctx, agentID, ended))
	}
	return nil
}

// forgetSubagentStart deletes the record of a start_agent call that started no
// subagent, in the commit that answers the call, and gives the start back: it
// no longer counts toward the starts since the user's message.
func forgetSubagentStart(ctx dex.Context, agentID string) {
	comm.MustDo(subagentsAttribute.Delete(ctx, agentID))
	state := comm.Must(agentStateAttribute.Get(ctx))
	state.SubagentsStartedWithoutUser = max(state.SubagentsStartedWithoutUser-1, 0)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	writeSnapshotRequired(ctx)
}

// abandonSubagentStart resolves the record of call, a pending start_agent call
// that a Step answers without its ExecuteTool: steering superseded it, or the
// workspace it waited for could not be prepared. Only an ExecuteTool attempt
// starts a subagent, and one whose call is still pending either never ran or
// found the workspace unreachable before it could start one, so the STARTING
// record is forgotten. The caller loaded the records; any other call, and a
// record a Stop already ended, is left as it is.
func abandonSubagentStart(ctx dex.Context, call conversation.ToolCall) {
	if call.Name != conversation.ToolStartAgent {
		return
	}
	agentID := flowid.AgentTaskID(ctx.FlowID(), string(call.ID))
	if record, err := subagentsAttribute.Get(ctx, agentID); !dexflow.IsAttributeNotFound(err) && comm.Must(record, err).Status == agenttasks.SubagentStarting {
		forgetSubagentStart(ctx, agentID)
	}
}

// abandonPendingSubagentStart resolves, in the commit of CheckSteered, the
// record of the call at the cursor when steering supersedes it: a start_agent
// call that RouteTool admitted before its ExecuteTool started a subagent.
func abandonPendingSubagentStart(ctx dex.Context) {
	if state := comm.Must(agentStateAttribute.Get(ctx)); state.PendingToolIndex < len(state.PendingToolCalls) {
		abandonSubagentStart(ctx, agentloop.PendingToolCall(state))
	}
}

// failSubagentStart records, in the commit of RecordToolFailure, that a
// start_agent call ran out of retries before its subagent was admitted: the
// record fails, so a late admission is denied and no subagent runs whose
// start the model reads as failed. The Step holds the AgentInterrupt lock.
func failSubagentStart(ctx dex.Context, call conversation.ToolCall) {
	if call.Name != conversation.ToolStartAgent {
		return
	}
	agentID := flowid.AgentTaskID(ctx.FlowID(), string(call.ID))
	record, err := subagentsAttribute.Get(ctx, agentID)
	if dexflow.IsAttributeNotFound(err) {
		return
	}
	// Pending cleanup means this call created no worktree or SubagentFlow.
	if dexflow.ValueOrZero(pendingSubagentWorktreeCleanupAttribute.Get(ctx)) != "" && comm.Must(record, err).AdmittedAt.IsZero() {
		forgetSubagentStart(ctx, agentID)
		return
	}
	if failed, isFailed := comm.Must(record, err).FailStart(time.Now().UTC()); isFailed {
		comm.MustDo(subagentsAttribute.Set(ctx, agentID, failed))
		writeSnapshotRequired(ctx)
	}
}

// stopStartingSubagents stops, in the commit of the Step that applies a
// Stop, every subagent whose start was not admitted yet, so its admission is
// denied. The caller holds the AgentInterrupt lock and loaded the records.
func stopStartingSubagents(ctx dex.Context) {
	records, now := loadedSubagents(ctx), time.Now().UTC()
	starting := agenttasks.StartingSubagents(records)
	for _, agentID := range starting {
		comm.MustDo(subagentsAttribute.Set(ctx, agentID, records[agentID].StopBeforeStart(now)))
	}
	if len(starting) > 0 {
		writeSnapshotRequired(ctx)
	}
}

// admitSubagent checks the loaded STARTING record under the RPC interrupt lock.
func admitSubagent(ctx dex.Context, admission agenttasks.SubagentAdmission) (*dex.RPCResult[agenttasks.SubagentAdmissionAnswer], error) {
	record, err := subagentsAttribute.Get(ctx, admission.AgentID)
	if dexflow.IsAttributeNotFound(err) {
		return &dex.RPCResult[agenttasks.SubagentAdmissionAnswer]{}, nil
	}
	isOpen := acceptsUserCommand(ctx) && currentExecutor(ctx).Mode != remoteexec.ModeRemote && dexflow.OptionalValue(agentInterruptAttribute.Get(ctx)) == nil
	admitted, answer, isChanged := comm.Must(record, err).Admit(admission, isOpen, time.Now().UTC())
	if isChanged {
		comm.MustDo(subagentsAttribute.Set(ctx, admission.AgentID, admitted))
	}
	return &dex.RPCResult[agenttasks.SubagentAdmissionAnswer]{Output: answer}, nil
}
