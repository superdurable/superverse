// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package subagent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
)

// A subagent runs its tools like the main agent: one call at a time from the
// cursor of AgentState.PendingToolCalls, or a wave of consecutive read-only
// calls in parallel. Every call runs on the subagent's copy of the lease in
// its worktree, through the sandboxd transport alone, or for a platform tool
// on the server for its Session's Project; no tool call reaches a Flow.

type parallelToolExecutionInput struct {
	ResultInstance string                `json:"result_instance"`
	Index          int                   `json:"index"`
	Call           conversation.ToolCall `json:"call"`
}

type parallelToolResult struct {
	Index  int                           `json:"index"`
	Call   conversation.ToolCall         `json:"call"`
	Result agentloop.ToolExecutionResult `json:"result"`
	Timing agentloop.ToolMessageTiming   `json:"timing"`
	// WorkspaceLost marks a branch that was not dispatched; the join asks
	// the Session for a lease and routes the wave again.
	WorkspaceLost bool `json:"workspace_lost,omitempty"`
}

// exhaustedRetriesMessage is the failure the model reads for a call whose
// retries are exhausted.
const exhaustedRetriesMessage = "The platform could not complete the call: its transient faults outlasted the retries."

// toolRoute is how RouteSubagentTools handles the call at the cursor.
type toolRoute int

const (
	// routeAnswered: the call has its result; the loop goes on.
	routeAnswered toolRoute = iota
	// routeWave: the call starts a parallel wave.
	routeWave
	// routeSerial: the call runs alone in ExecuteSubagentTool.
	routeSerial
)

// routeCall answers a call the subagent may not make, or set_effort, in the
// commit of RouteSubagentTools, and otherwise tells how it runs. A wave is
// returned with its batch.
func routeCall(ctx dex.Context) (toolRoute, agentloop.ToolBatchState) {
	state, toolNames := comm.Must(agentStateAttribute.Get(ctx)), comm.Must(contextSegmentAttribute.Get(ctx)).ToolNames
	call := agentloop.PendingToolCall(state)
	if _, isOffered := agentloop.SegmentTool(toolNames, call.Name); !isOffered {
		appendFailure(ctx, call, toolfailure.UnknownOrDisabledTool, fmt.Sprintf("tool %q is unknown or not available to a subagent", call.Name))
		return routeAnswered, agentloop.ToolBatchState{}
	}
	if call.Name == conversation.ToolSetEffort {
		effort, _, err := agentloop.RequestedEffort(call)
		if err != nil {
			appendFailure(ctx, call, toolfailure.InvalidEffort, err.Error())
		} else {
			appendToolResult(ctx, call, agentloop.ToolResult(agentloop.ToolResultPayload{Status: agentloop.ToolResultStatusUpdated, Effort: raiseEffort(ctx, effort)}), agentloop.ToolMessageTiming{})
		}
		return routeAnswered, agentloop.ToolBatchState{}
	}
	if batch, _ := agentloop.ParallelToolBatch(toolNames, state); len(batch.Records) > 0 {
		return routeWave, batch
	}
	return routeSerial, agentloop.ToolBatchState{}
}

// raiseEffort records the effort a set_effort call asks for, clamped to the
// model's ceiling, unless the segment already runs at it. A subagent has no
// user turns, so a raise lasts until it ends.
func raiseEffort(ctx dex.Context, effort conversation.Effort) conversation.Effort {
	segment := comm.Must(contextSegmentAttribute.Get(ctx))
	if effort = effort.AtMost(modelprovider.ProfileOf(segment.Model).MaxEffort); effort == segment.EffectiveEffort() {
		return effort
	}
	appendEntry(ctx, conversation.EffortEntry(effort))
	segment = comm.Must(contextSegmentAttribute.Get(ctx))
	segment.CurrentEffort = effort
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	return effort
}

// serialToolStepOptions is the execution policy of one serial
// ExecuteSubagentTool movement: bash polls the ledger within its own timeout
// and is settled from it afterwards; any other tool has its catalog policy.
func serialToolStepOptions(call conversation.ToolCall, settle, record *dex.ExecuteFailure) *dex.StepOptions {
	if call.Name == conversation.ToolBash {
		options := dexflow.PollingStepOptions(sandboxtools.CommandTimeout(call.Arguments.String()), settle)
		options.ExecuteLoadAttributeMaps = messageMaps
		options.ExecuteLockAttributes = messageLocks
		return options
	}
	options := parallelToolStepOptions(agentloop.CatalogTool(call.Name), record)
	options.ExecuteLoadAttributeMaps = messageMaps
	options.ExecuteLockAttributes = messageLocks
	return options
}

// parallelToolStepOptions is the execution policy of one call of the catalog
// definition: its attempt timeout and its retries after a transient fault.
func parallelToolStepOptions(definition agentloop.ToolDefinition, failure *dex.ExecuteFailure) *dex.StepOptions {
	durability := dex.StepDurabilityDefault
	if definition.LongRunning {
		durability = dex.StepDurabilitySync
	}
	return &dex.StepOptions{
		ExecuteMethodTimeout: definition.AttemptTimeout, HeartbeatTimeout: time.Minute, ExecuteDurability: durability,
		ExecuteRetry: &dex.RetryPolicy{
			InitialInterval: agentloop.ToolRetryInitialInterval, BackoffCoefficient: agentloop.ToolRetryBackoffCoefficient, MaximumInterval: agentloop.ToolRetryMaximumInterval,
			// #nosec G115 -- the catalog init check bounds MaximumAttempts to int32.
			MaximumAttempts: int32(definition.MaximumAttempts), TotalDuration: definition.RetryTotalDuration,
		},
		ExecuteFailure: failure,
	}
}

// executeTool runs one call: a platform tool on the server for the Session's
// Project, a command through the command ledger, any other on the lease copy
// in the worktree.
func (flow *SubagentFlow) executeTool(ctx dex.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	if call.Name == conversation.ToolBash {
		return flow.executeCommand(ctx, call)
	}
	definition := agentloop.CatalogTool(call.Name)
	writeToolProgress(ctx, call)
	attempt, cancel := context.WithTimeout(ctx, definition.AttemptTimeout)
	defer cancel()
	defer dexflow.KeepAlive(ctx, agentloop.ToolHeartbeat{Phase: agentloop.HeartbeatPhaseToolCall, ToolName: call.Name})()
	if definition.Platform {
		return flow.diagnostics.ReadDiagnostics(attempt, sessionIdentity(ctx), call)
	}
	result, err := flow.sandbox.Run(attempt, definition.Run, newSandboxCall(ctx, call))
	return agentloop.SandboxToolResult(result), err
}

// sessionIdentity is the identity of the subagent's Session, which scopes
// its platform reads to the Session's own Project.
func sessionIdentity(ctx dex.Context) agentloop.SessionIdentity {
	identity, config := comm.Must(identityAttribute.Get(ctx)), comm.Must(configAttribute.Get(ctx))
	return agentloop.SessionIdentity{ProjectID: identity.ProjectID, Profile: agentloop.ProfileSessionV1, HeadCommit: config.HeadCommit}
}

// newSandboxCall is the trusted context of a subagent's Sandbox call: the
// lease copy, the worktree as the agent root and the call's stable identity.
func newSandboxCall(ctx dex.Context, call conversation.ToolCall) sandboxtools.Call {
	ws := comm.Must(workspaceAttribute.Get(ctx))
	return sandboxtools.Call{
		Tool: string(call.Name), Arguments: call.Arguments.String(), Root: ws.Root, Subagent: true, Lease: &ws.Lease, Progress: func(string) {},
		Mutating: agentloop.CatalogTool(call.Name).WorkspaceAccess == agentloop.WorkspaceAccessMutating,
		Identity: sandboxd.InvocationIdentity{FlowID: ctx.FlowID(), CallID: string(call.ID)},
	}
}

// Polling of a running command: a status request waits up to
// sandboxtools.CommandStatusWait, and a round adds the transport's margin.
const (
	commandPollInterval = time.Second
	commandPollRound    = sandboxtools.CommandStatusWait + 10*time.Second
)

// executeCommand runs a foreground bash call within this attempt: it begins
// the command through the ledger and polls its status until it ends. From the
// second attempt on, begin passes the first attempt's time less a minute of
// clock skew, so a replaced instance answers instance_replaced instead of
// running the command again.
func (flow *SubagentFlow) executeCommand(ctx dex.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
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
		// the first attempt's time, can tell whether its instance still runs it.
		return agentloop.ToolExecutionResult{}, fmt.Errorf("poll the command: the Sandbox became unreachable: %v", err) //nolint:errorlint // deliberately not ErrWorkspaceUnavailable
	}
	return agentloop.SandboxToolResult(result), err
}

// settleCommand settles a bash call whose attempts ended without a result:
// past its timeout the command is stopped and timed out, after exhausted
// retries it is read from the ledger and stopped when it still runs. An
// unreachable workspace leaves the outcome unknown.
func (flow *SubagentFlow) settleCommand(ctx dex.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, agentloop.ToolMessageTiming, error) {
	settle := sandboxtools.SettleForegroundCommand
	if dexflow.RecoveryTimedOut(ctx) {
		settle = sandboxtools.StopTimedOutCommand
	}
	defer dexflow.KeepAlive(ctx, agentloop.ToolHeartbeat{Phase: agentloop.HeartbeatPhaseToolCall, ToolName: call.Name})()
	result, err := flow.sandbox.Run(ctx, settle, newSandboxCall(ctx, call))
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

// lostWorkspace records that the transport proved the lease unreachable for
// call, which was not dispatched, and reports whether the subagent asks its
// Session for a lease again; past the bound of losses the call fails with
// workspace_unavailable instead, which the caller records.
func lostWorkspace(ctx dex.Context, call conversation.CallID) bool {
	ws := comm.Must(workspaceAttribute.Get(ctx))
	refreshes := ws.countLoss(call)
	comm.MustDo(workspaceAttribute.Set(ctx, ws))
	return refreshes
}

// stopRunningCommand stops the command of the bash call at the cursor, which
// a Stop or the deadline cancelled while it ran; the ledger's stop is
// idempotent, and an unreachable Sandbox ended it with itself.
func (flow *SubagentFlow) stopRunningCommand(ctx dex.Context) error {
	state, ws := comm.Must(agentStateAttribute.Get(ctx)), comm.Must(workspaceAttribute.Get(ctx))
	if state.PendingToolIndex >= len(state.PendingToolCalls) || agentloop.PendingToolCall(state).Name != conversation.ToolBash {
		return nil
	}
	taskID := string(sandboxd.CommandTaskID(sandboxd.InvocationIdentity{FlowID: ctx.FlowID(), CallID: string(agentloop.PendingToolCall(state).ID)}))
	call := sandboxtools.Call{Tool: string(conversation.ToolStopTask), Arguments: sandboxtools.TaskArguments(taskID), Lease: &ws.Lease, Root: ws.Root,
		Mutating: true, Subagent: true, Identity: sandboxd.InvocationIdentity{FlowID: ctx.FlowID(), CallID: taskID}, Progress: func(string) {}}
	result, err := flow.sandbox.Run(ctx, sandboxtools.StopTask, call)
	switch {
	case errors.Is(err, sandboxtools.ErrWorkspaceUnavailable), err == nil && result.Failure != nil && result.Failure.Code == toolfailure.TaskNotFound:
		return nil
	case err != nil:
		return fmt.Errorf("stop the command of call %s: %w", agentloop.PendingToolCall(state).ID, err)
	}
	return nil
}

// finishWorktree saves the worktree into its result and removes the checkout.
// It returns the saved changes, none for a worktree that holds none, and why a
// worktree could not be saved, whose changes then stay in it on the volume; a
// lease the transport proved unreachable is sandboxtools.ErrWorkspaceUnavailable.
func (flow *SubagentFlow) finishWorktree(ctx dex.Context) (*conversation.TaskChanges, string, error) {
	identity, ws := comm.Must(identityAttribute.Get(ctx)), comm.Must(workspaceAttribute.Get(ctx))
	call := sandboxtools.Call{Tool: string(conversation.ToolStartAgent), Arguments: sandboxtools.AgentWorktreeArguments(identity.AgentID, nil), Lease: &ws.Lease,
		Root: sandboxd.WorkspaceRoot, Mutating: true, Identity: sandboxd.InvocationIdentity{FlowID: ctx.FlowID(), CallID: "finish"}, Progress: func(string) {}}
	defer dexflow.KeepAlive(ctx, agentloop.ToolHeartbeat{Phase: agentloop.HeartbeatPhaseToolCall, ToolName: conversation.ToolStartAgent})()
	result, err := flow.sandbox.Run(ctx, sandboxtools.FinishAgentWorktree, call)
	switch {
	case err != nil:
		return nil, "", err
	case result.Failure != nil:
		return nil, "the Sandbox could not save it: " + result.Failure.Text(), nil
	case result.Finished.Missing:
		return nil, "its worktree was gone before it was saved", nil
	case result.Finished.Files == 0:
		return nil, "", nil
	}
	return &conversation.TaskChanges{ResultCommit: result.Finished.Result, Files: result.Finished.Files, Insertions: result.Finished.Insertions, Deletions: result.Finished.Deletions}, "", nil
}

// awaitLease asks the Session every ten seconds for the lease of its
// instance, reporting the lease of request lost that the transport proved
// unreachable, until the Session lends the lease of another request, after
// provisioning one when it has none, or no longer lends the subagent its
// workspace (WorkspaceRefresh.Closed).
func (flow *SubagentFlow) awaitLease(ctx dex.Context, identity agenttasks.SubagentIdentity, lost string) (WorkspaceRefresh, error) {
	var refreshed WorkspaceRefresh
	err := dexflow.PollUntil(ctx, 10*time.Second, 10*time.Second, func(round context.Context) (bool, error) {
		answer, err := flow.dependency.RefreshSubagentWorkspace(round, identity, lost)
		if err != nil {
			return false, fmt.Errorf("refresh the lease of subagent %s: %w", identity.AgentID, err)
		}
		refreshed = answer
		return answer.Closed || answer.Lease != nil && answer.Lease.RequestID != lost, nil
	})
	return refreshed, err
}

// joinParallelToolResults records the branch results of a wave in its batch
// and returns the lowest branch that found its workspace unreachable, if any.
func joinParallelToolResults(batch agentloop.ToolBatchState, results []parallelToolResult) (agentloop.ToolBatchState, *parallelToolResult) {
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
	return batch, lost
}

// The functions below record tool results in the commit of the calling Step
// and move the cursor past the calls they finish. They report whether calls
// remain to route; otherwise the loop calls the model.

// appendToolResult records the result of a call and its terminal activity.
func appendToolResult(ctx dex.Context, call conversation.ToolCall, result agentloop.ToolExecutionResult, timing agentloop.ToolMessageTiming) {
	appendEntry(ctx, agentloop.ToolResultEntry(call, result, timing))
	writeActivity(ctx, agentloop.ToolTerminalEvent(call, result))
}

// appendFailure records a failure the subagent decided for a call, which the
// model corrects, and moves past the call.
func appendFailure(ctx dex.Context, call conversation.ToolCall, code toolfailure.Code, message string) {
	appendToolResult(ctx, call, agentloop.SessionFailureResult(code, message), agentloop.ToolMessageTiming{})
}

// finishToolExecution records the result of the serial call at the cursor.
func finishToolExecution(ctx dex.Context, call conversation.ToolCall, result agentloop.ToolExecutionResult, timing agentloop.ToolMessageTiming) bool {
	index := comm.Must(agentStateAttribute.Get(ctx)).PendingToolIndex
	return finishToolBatch(ctx, agentloop.ToolBatchState{FirstIndex: index, Records: []agentloop.ToolBatchRecord{{Index: index, Call: call, Result: &result, Timing: timing}}})
}

// finishToolBatch records the results of a finished wave in call order and
// moves the cursor past it.
func finishToolBatch(ctx dex.Context, batch agentloop.ToolBatchState) bool {
	records := slices.SortedFunc(slices.Values(batch.Records), func(left, right agentloop.ToolBatchRecord) int { return left.Index - right.Index })
	for _, record := range records {
		appendToolResult(ctx, record.Call, *record.Result, record.Timing)
	}
	state, next := agentloop.FinishToolBatch(comm.Must(agentStateAttribute.Get(ctx)), batch)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	return next == agentloop.ContinueRouteTool
}

// advancePastCall moves the cursor past the call a route answered.
func advancePastCall(ctx dex.Context) bool {
	state, next := agentloop.AdvanceToolCursor(comm.Must(agentStateAttribute.Get(ctx)), 1)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	return next == agentloop.ContinueRouteTool
}
