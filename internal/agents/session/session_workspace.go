// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// workspaceWaitBudget bounds the wait for the outcome of a request: it
// outlasts the SessionWorkspaceFlow's reclamation of an earlier instance (15
// minutes), its wait for the provisioning's report and the retries of its own
// report.
const workspaceWaitBudget = provision.Budget + 30*time.Minute

// workspaceDemandResult answers a WorkspaceDemandRequest: the workspace after
// the demand, whether a READY instance was lent to the Preview, the lease a
// subagent takes, and Closed for a Session that is no longer ACTIVE, which
// lends and requests nothing, or a subagent that no longer runs.
type workspaceDemandResult struct {
	Summary agentloop.WorkspaceSummary `json:"summary"`
	Lent    bool                       `json:"lent,omitempty"`
	Lease   *workspace.Lease           `json:"lease,omitempty"`
	Closed  bool                       `json:"closed,omitempty"`
}

// workspaceDemand is the input of the cold-path Steps.
type workspaceDemand struct {
	ResumeAt agentloop.Continuation `json:"return"`           // "" = RPC-triggered branch: DeadEnd when done
	Lost     string                 `json:"lost,omitempty"`   // RequestID the transport proved unreachable
	CallID   conversation.CallID    `json:"callId,omitempty"` // call that observed the loss
	// RequestID is the request the branch owns once RequestSessionWorkspace
	// opened it. PromptOnly marks a branch that owns no request and only puts
	// the repository context of a READY lease into the prompt. Reclaims marks
	// the branch that sends the reclaim command of the revoked request at its
	// Sandbox deadline, which owns no request either.
	RequestID  string `json:"requestId,omitempty"`
	PromptOnly bool   `json:"promptOnly,omitempty"`
	Reclaims   bool   `json:"reclaims,omitempty"`
	// Observed is the outcome AwaitSessionWorkspace took from the report or
	// read from the SessionWorkspaceFlow. Failure is the rejection or timeout
	// a Step decided, which RecordWorkspaceFailure records; TimedOut marks a
	// request whose outcome did not arrive within the wait.
	Observed *agentloop.WorkspaceGrant `json:"observed,omitempty"`
	Failure  string                    `json:"failure,omitempty"`
	TimedOut bool                      `json:"timedOut,omitempty"`
}

// newWorkspaceRequestID derives the instance identity from the Step execution
// that records it, "<Step>-<n>". The execution counter never repeats within
// the Flow.
func newWorkspaceRequestID(ctx dex.Context) string {
	_, number, ok := agentloop.StepExecutionParts(ctx.StepExecutionID())
	if !ok {
		panic("a Dex Step execution ID is not <Step>-<n>: " + ctx.StepExecutionID())
	}
	return "ws-" + number
}

// admitWorkspaceUse reports whether the tools about to run may use the accepted
// lease, marks its instance in use and extends its Sandbox deadline: the main
// agent's workspace use holds the Sandbox for the Sandbox hold. Callers hold
// the Workspace lock, which makes the extension atomic with the deadline's
// reclamation decision.
func (flow *SessionAgentFlow) admitWorkspaceUse(ctx dex.Context) bool {
	ws, now := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)), time.Now()
	if !ws.IsUsable() {
		return false
	}
	comm.MustDo(workspaceUseAttribute.Set(ctx, agentloop.WorkspaceUse{Active: true, RequestID: ws.Lease.RequestID}))
	if ws.ExtendDeadline(now, flow.sandboxHold) {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
	return true
}

// reclaimAtDeadline decides, under the Workspace lock, when the Sandbox
// deadline timer of requestID fired. It reports waitsAgain when the deadline
// moved later, or a tool, a background command or an unfinished subagent
// still uses the instance, which holds it for another Sandbox hold; every
// task has a hard limit.
// Otherwise it revokes the lease in the same commit and reports reclaims, so
// a following Step sends the reclaim command only once no tool can be
// dispatched on the instance; neither is reported when the request settled
// otherwise. The Session knows by itself whether its tools stopped, so no
// other Flow is asked.
func (flow *SessionAgentFlow) reclaimAtDeadline(ctx dex.Context, requestID string) (reclaims, waitsAgain bool) {
	ws, now := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)), time.Now()
	if ws.RequestID != requestID || ws.State != agentloop.WorkspaceReady {
		return false, false
	}
	if ws.SandboxDeadline.After(now) {
		return false, true
	}
	use := dexflow.ValueOrZero(workspaceUseAttribute.Get(ctx))
	if use.Active && use.RequestID == ws.Lease.RequestID || slices.ContainsFunc(agenttasks.RunningBackgroundCommands(loadedBackgroundCommands(ctx)), func(command agenttasks.BackgroundCommand) bool {
		return command.WorkspaceRequestID == ws.Lease.RequestID
	}) || len(agenttasks.UnfinishedSubagents(loadedSubagents(ctx), now)) > 0 {
		ws.ExtendDeadline(now, flow.sandboxHold)
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
		return false, true
	}
	ws.Revoke()
	comm.MustDo(workspaceAttribute.Set(ctx, ws))
	return true, false
}

// admitWorkspaceOutcome admits a report of the SessionWorkspaceFlow under the
// receipts and Workspace locks and reports whether it is admitted; a repeated
// report is admitted again without another effect. A reclamation clears the
// Preview's hold of the instance. A ready or failed outcome is admitted only
// while the Session waits for exactly that request, and it reaches only
// AwaitSessionWorkspace through its WorkspaceOutcomes instance. Anything
// else, such as a late outcome after a replacement, is refused: the workspace
// Flow reclaims the instance it created.
func admitWorkspaceOutcome(ctx dex.Context, report agentloop.WorkspaceReport) bool {
	id := report.WorkspaceRequestID
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	if report.Reclaimed {
		if ws.PreviewHolds == id {
			ws.Reclaimed(id)
			comm.MustDo(workspaceAttribute.Set(ctx, ws))
		}
		return true
	}
	isNew, _, next := agentloop.AdmitNotification(dexflow.ValueOrZero(notificationReceiptsAttribute.Get(ctx)), 0, "workspace/"+id)
	if !isNew {
		return true
	}
	if report.Grant == nil || ws.State != agentloop.WorkspaceRequested || ws.RequestID != id {
		return false
	}
	comm.MustDo(workspaceOutcomesChannel.Publish(ctx, id, report))
	comm.MustDo(notificationReceiptsAttribute.Set(ctx, next))
	return true
}

// endToolWork records that no dispatched tool remains on the instance and no
// tool work is in flight. Finishing paths call it without the Workspace lock;
// only the reclamation decision reads WorkspaceUse.
func endToolWork(ctx dex.Context) {
	setRunningWork(ctx, nil)
	if dexflow.ValueOrZero(workspaceUseAttribute.Get(ctx)) != (agentloop.WorkspaceUse{}) {
		comm.MustDo(workspaceUseAttribute.Set(ctx, agentloop.WorkspaceUse{}))
	}
}

// lostWorkspaceDemand ends the tool work of a call or a wave that found its
// workspace unreachable before it dispatched anything, and is the cold-path
// demand that routes the calls again once a new instance is ready.
func lostWorkspaceDemand(ctx dex.Context, callID conversation.CallID) workspaceDemand {
	lost := dexflow.ValueOrZero(workspaceUseAttribute.Get(ctx)).RequestID
	endToolWork(ctx)
	return workspaceDemand{ResumeAt: agentloop.ContinueRouteTool, Lost: lost, CallID: callID}
}

// holdSandbox moves the Sandbox deadline of the READY instance to at least
// now + hold: after a task's end any current instance, for a ready Preview
// only instanceID, the instance it serves on. The caller holds the Workspace
// lock.
func holdSandbox(ctx dex.Context, instanceID string, hold time.Duration) {
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	if (instanceID == "" || ws.Lease.RequestID == instanceID) && ws.ExtendDeadline(time.Now(), hold) {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
}

// resumeAfterWorkspace prepares leaving the cold path in the commit of the
// Step that settled it and returns the continuation the waiting branch resumes
// through CheckSteered, or "" to end an RPC-triggered branch. A model
// continuation opens the Session's first segment if it has none, with the
// repository context if the lease provided it, so a workspace that cannot be
// prepared delays the model but never blocks it. A failed tool demand answers
// the pending call with a failure the model reads, and a start_agent call
// answered so started no subagent, whose record is forgotten. Nothing here
// fails the Session.
func (flow *SessionAgentFlow) resumeAfterWorkspace(ctx dex.Context, demand workspaceDemand, isReady bool) agentloop.Continuation {
	switch demand.ResumeAt {
	case "":
		return ""
	case agentloop.ContinueStartTurn, agentloop.ContinueCompactContext, agentloop.ContinueCallModel:
		if dexflow.OptionalValue(contextSegmentAttribute.Get(ctx)) == nil {
			flow.openSessionStartSegment(ctx)
		}
		return demand.ResumeAt
	case agentloop.ContinueRouteTool:
		if isReady {
			return agentloop.ContinueRouteTool
		}
		call := agentloop.PendingToolCall(comm.Must(agentStateAttribute.Get(ctx)))
		abandonSubagentStart(ctx, call)
		return finishToolExecution(ctx, call, agentloop.WorkspaceUnavailableResult(), agentloop.ToolMessageTiming{})
	default:
		panic(fmt.Sprintf("unsupported workspace continuation %q", demand.ResumeAt))
	}
}

// adoptWorkspaceHead makes the HEAD a newly granted instance observed the
// Session HEAD. Only the Step that owns the request applies it, before any tool
// is dispatched on that instance.
func adoptWorkspaceHead(ctx dex.Context, lease workspace.Lease) {
	confirmSessionHead(ctx, lease.Repository.ExpectedHeadSHA, ctx.FirstAttemptAt().UTC())
}

// confirmSessionHead records a verified HEAD in the commit of the calling Step.
func confirmSessionHead(ctx dex.Context, commit string, at time.Time) {
	if flowid.IsCommitSHA(commit) && comm.Must(headCommitAttribute.Get(ctx)) != commit {
		comm.MustDo(headCommitAttribute.Set(ctx, commit))
		comm.MustDo(sessionUpdatedAtAttribute.Set(ctx, at))
	}
}

// readRepositoryContext reads the trusted context of the leased checkout,
// which the next segment_start carries. The caller records it in Workspace in
// the same commit.
func (flow *SessionAgentFlow) readRepositoryContext(ctx dex.Context, lease workspace.Lease) (string, error) {
	repositoryContext, err := flow.repository.RepositoryContext(ctx, lease)
	return comm.TruncateUTF8(repositoryContext, agentloop.MaximumRepositoryContextBytes), err
}

// applyWorkspace settles the request of demand with the outcome
// AwaitSessionWorkspace took, then applies an accepted lease, and records
// Workspace when it changed. Each effect is idempotent and commits with the
// calling Step; only the owner of the request adopts its HEAD, announces the
// settlement to a waiting Preview and takes the joined continuation. It
// returns the demand with the continuation to resume and whether the request
// is READY.
func (flow *SessionAgentFlow) applyWorkspace(ctx dex.Context, ws agentloop.SessionWorkspace, demand workspaceDemand) (workspaceDemand, bool, error) {
	isOwner, isChanged, identity := demand.ownsRequest(), false, readSessionIdentity(ctx)
	if ws.State == agentloop.WorkspaceRequested {
		// Only AwaitSessionWorkspace reaches a request still in flight, with
		// the outcome it took.
		if err := ws.Accept(identity, *demand.Observed); err != nil {
			return demand, false, dex.ErrorWithStack(fmt.Errorf("accept Sandbox instance %s: %w", ws.RequestID, err))
		}
		ws.ExtendDeadline(time.Now(), flow.sandboxHold)
		isChanged = true
	}
	if isOwner && demand.ResumeAt == "" && ws.JoinedResumeAt != "" {
		demand.ResumeAt, isChanged = ws.TakeJoined(), true
	}
	if ws.State == agentloop.WorkspaceReady {
		if isOwner {
			adoptWorkspaceHead(ctx, ws.Lease)
		}
		if ws.RepositoryContext == "" && (isOwner || demand.ResumeAt != agentloop.ContinueRouteTool) {
			repositoryContext, err := flow.readRepositoryContext(ctx, ws.Lease)
			if err != nil {
				return demand, false, dex.ErrorWithStack(fmt.Errorf("read the repository context of Sandbox instance %s: %w", ws.RequestID, err))
			}
			ws.RepositoryContext, isChanged = repositoryContext, true
		}
	}
	if isOwner && ws.PreviewWaits != "" {
		if err := flow.announceSettlement(ctx, &ws); err != nil {
			return demand, false, err
		}
		isChanged = true
	}
	if isChanged {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
	return demand, ws.State == agentloop.WorkspaceReady, nil
}

// settleWorkspaceFailure records code, the failure of the Step that exhausted
// its retries, or the business rejection or timeout a Step decided, with the
// request of demand, which the caller found current. Only the owner settles
// the request, announces the settlement to a waiting Preview, takes a joined
// continuation and adopts the HEAD. It returns the demand with the
// continuation to resume and whether the owner's request is READY without a
// Sandbox deadline wait, which the caller then arms.
func (flow *SessionAgentFlow) settleWorkspaceFailure(ctx dex.Context, ws *agentloop.SessionWorkspace, demand workspaceDemand, code string) (workspaceDemand, bool, error) {
	detail := cmp.Or(dexflow.RecoveryDetail(ctx), dexflow.TruncateCause(demand.Failure))
	isOwner := demand.ownsRequest()
	switch {
	case isOwner && ws.State == agentloop.WorkspaceRequested:
		// The SessionWorkspaceFlow keeps the instance: a provisioning whose
		// outcome is unknown is reclaimed before the next one.
		ws.Fail(code, detail)
	default:
		// The owner's lease effects failed, or a branch that owns no request
		// (a prompt-only one, or the reclaim command's) failed: only the
		// detail is recorded.
		ws.FailureDetail = detail
	}
	if isOwner && ws.PreviewWaits != "" {
		if err := flow.announceSettlement(ctx, ws); err != nil {
			return demand, false, err
		}
	}
	if isOwner && demand.ResumeAt == "" {
		demand.ResumeAt = ws.TakeJoined()
	}
	comm.MustDo(workspaceAttribute.Set(ctx, *ws))
	if isOwner && ws.State == agentloop.WorkspaceReady {
		adoptWorkspaceHead(ctx, ws.Lease)
	}
	return demand, isOwner && ws.State == agentloop.WorkspaceReady, nil
}

// announceSettlement tells the Preview command that waits for the request of
// ws that it settled, READY or with its failure code, and records that no
// Preview waits any more; the caller commits ws. A failed request refuses the
// command, and a READY one is lent when the Preview asks again. A retried Step
// announces again; the Preview admits the announcement only while that
// command is active.
func (flow *SessionAgentFlow) announceSettlement(ctx dex.Context, ws *agentloop.SessionWorkspace) error {
	failure := ""
	if ws.State == agentloop.WorkspaceFailed {
		failure = ws.Failure
	}
	if err := flow.dependency.NotifyPreviewWorkspace(ctx, readSessionIdentity(ctx), ws.PreviewWaits, ws.RequestID, failure); err != nil {
		return dex.ErrorWithStack(fmt.Errorf("announce the settled workspace %s to Preview command %s: %w", ws.RequestID, ws.PreviewWaits, err))
	}
	ws.TakePreviewWaits()
	return nil
}

// workspaceFailureCode is the failure code of a request whose Step moved to
// RecordWorkspaceFailure: WORKSPACE_TIMEOUT when its outcome did not arrive
// within the wait, else WORKSPACE_START_FAILED.
func workspaceFailureCode(demand workspaceDemand) string {
	if demand.TimedOut {
		return sandbox.WorkspaceFailureTimeout
	}
	return sandbox.WorkspaceFailureStart
}

// ownsRequest reports whether the branch owns its request: it settles it,
// announces the settlement to a waiting Preview and takes a joined
// continuation.
func (demand workspaceDemand) ownsRequest() bool { return !demand.PromptOnly && !demand.Reclaims }

// Workspace option presets keep admission and reclamation beside their owner.
var (
	// The cold-path Steps that commit lock only Workspace, so RPCs and model calls never wait;
	// they load messages and subagent records as settling a request may answer a start_agent call.
	workspaceRequestStepOptions = &dex.StepOptions{ExecuteMethodTimeout: time.Minute, ExecuteDurability: dex.StepDurabilityAsync,
		ExecuteLoadAttributeMaps: taskMessageMaps, ExecuteLockAttributes: []dex.AttributeLock{workspaceLock}}
	workspaceApplyStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteLoadAttributeMaps: taskMessageMaps, ExecuteLockAttributes: []dex.AttributeLock{workspaceLock}, ExecuteRetry: shortStepRetry}
	// AwaitSandboxDeadline waits on a business timer and decides under the Workspace lock,
	// atomically with the dispatches that extend it: a running tool or task holds the Sandbox.
	sandboxDeadlineStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteLockAttributes: []dex.AttributeLock{workspaceLock}, ExecuteRetry: shortStepRetry, ExecuteLoadAttributeMaps: taskMaps}
)

// applyWorkspaceStepOptions attaches the workspace failure route to its settlement policy.
func applyWorkspaceStepOptions() *dex.StepOptions {
	options := *workspaceApplyStepOptions
	options.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordWorkspaceFailure{}, nil)
	return &options
}

// updatePreviewAutoDeploy records the account selection for pushed Preview handoffs.
func updatePreviewAutoDeploy(ctx dex.Context, input agentloop.PreviewAutoDeployRequest) (*dex.RPCResult[bool], error) {
	if !acceptsUserCommand(ctx) {
		return rpcAccepted(false), nil
	}
	if dexflow.ValueOrZero(previewAutoDeployAttribute.Get(ctx)) != input.IsEnabled {
		comm.MustDo(previewAutoDeployAttribute.Set(ctx, input.IsEnabled))
		if input.Actor != nil {
			comm.MustDo(previewAutoDeployChangedByAttribute.Set(ctx, *input.Actor))
		} else {
			comm.MustDo(previewAutoDeployChangedByAttribute.Delete(ctx))
		}
	}
	return rpcAccepted(true), nil
}
