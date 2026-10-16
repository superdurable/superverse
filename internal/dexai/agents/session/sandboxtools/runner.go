// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxtools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// ErrWorkspaceUnavailable proves that no Sandbox request was dispatched.
var ErrWorkspaceUnavailable = errors.New("session workspace is unavailable")

// Runner binds accepted leases to pooled Sandbox capabilities. It reads no Flow.
type Runner struct {
	workspaces *workspace.SandboxResolver
	commits    *git.GitOperations
	catalog    connectorCatalogCache
}

// Call is trusted Session context, never a model-supplied lease or identity.
type Call struct {
	Tool       string
	Arguments  string
	Identity   sandboxd.InvocationIdentity
	Lease      *workspace.Lease
	HeadCommit string
	// Requirements is the Session's current requirements; nil before any
	// card. commit_and_push checks its trace against it, so the tool reads no
	// Flow.
	Requirements *ConfirmedRequirements
	// Root is the agent root, where commands run: the workspace for the main
	// agent, its worktree for a subagent.
	Root string
	// Subagent marks a call of a subagent, which shares the Sandbox with the
	// main agent: its commands run in the foreground only, at a lower priority
	// and with bounded parallelism.
	Subagent bool
	// NotBefore is the time of the call's first attempt, set from the second
	// attempt on: the command ledger refuses to start a command that an earlier
	// instance may have run.
	NotBefore *time.Time
	// Mutating marks a tool that may change the workspace or the remote, whose
	// unexpected failure leaves an unknown effect.
	Mutating bool
	// Progress receives bounded, human-readable progress for the UI and must
	// return promptly. It is best effort and never keeps the call alive: the
	// caller heartbeats its Step while the call runs.
	Progress func(string)
}

// ConfirmedRequirements is the version and the feature IDs (R1, R2, ...) of
// the current requirements.
type ConfirmedRequirements struct {
	Revision int64
	IDs      []string
}

// Execution holds the capabilities of one call. Only Runner creates it.
type Execution struct {
	catalog    *connectorCatalogCache
	commits    *git.GitOperations
	repository git.Repository
	executor   *sandboxd.SandboxClient
}

// Tool is one Sandbox operation referenced by the Agent catalog.
type Tool func(*Execution, context.Context, Call) (Result, error)

var (
	Read             Tool = (*Execution).read
	Edit             Tool = (*Execution).edit
	Write            Tool = (*Execution).write
	Grep             Tool = (*Execution).grep
	Glob             Tool = (*Execution).glob
	ConnectorCatalog Tool = (*Execution).connectorCatalog
	CommitAndPush    Tool = (*Execution).commitAndPush
)

// The bash tool's ledger operations (tool_bash.go): BeginCommand is the
// catalog tool, the others serve its polling and settling.
var (
	BeginCommand        Tool = (*Execution).beginCommand
	CommandStatus       Tool = (*Execution).commandStatus
	MoveTimedOutCommand Tool = (*Execution).moveTimedOutCommand
	SettleCommand       Tool = (*Execution).settleCommand
)

// A subagent's command never moves to the background (tool_bash.go): once its
// timeout passed StopTimedOutCommand stops it, and once its attempts ran out
// SettleForegroundCommand reads its end from the ledger or stops it.
var (
	StopTimedOutCommand     Tool = (*Execution).stopTimedOutCommand
	SettleForegroundCommand Tool = (*Execution).settleForegroundCommand
)

// The task tools (tool_tasks.go): ReadTaskOutput and StopTask are catalog
// tools; the Session also stops tasks with StopTask and watches one with
// CommandTaskStatus.
var (
	ReadTaskOutput    Tool = (*Execution).readTaskOutput
	StopTask          Tool = (*Execution).stopTask
	CommandTaskStatus Tool = (*Execution).commandTaskStatus
)

// The delegation tools (tool_agents.go): the Session creates a subagent's
// worktree for start_agent with CreateAgentWorktree, the subagent saves it
// with FinishAgentWorktree, and MergeAgentChanges is the catalog tool
// merge_agent_changes.
var (
	CreateAgentWorktree  Tool = (*Execution).createAgentWorktree
	FinishAgentWorktree  Tool = (*Execution).finishAgentWorktree
	CleanupAgentWorktree Tool = (*Execution).cleanupAgentWorktree
	MergeAgentChanges    Tool = (*Execution).mergeAgentChanges
	// A connector_develop subagent's root is a worktree of the connectors
	// library instead, which its finish commits and pushes.
	CreateContributionWorktree Tool = (*Execution).createContributionWorktree
	FinishContribution         Tool = (*Execution).finishContribution
)

func NewRunner(workspaces *workspace.SandboxResolver, commits *git.GitOperations) *Runner {
	return &Runner{workspaces: workspaces, commits: commits}
}

// Run executes one tool on the Sandbox of the call's lease and classifies its
// error once, see classifyToolError.
func (runner *Runner) Run(ctx context.Context, tool Tool, call Call) (Result, error) {
	resolution, err := runner.bindWorkspace(ctx, call)
	if err != nil {
		return Result{}, err
	}
	defer resolution.Release()
	repository := call.Lease.Repository
	repository.ExpectedHeadSHA = call.HeadCommit
	execution := &Execution{catalog: &runner.catalog, commits: runner.commits, repository: repository, executor: resolution.Client}
	result, err := tool(execution, ctx, call)
	return classifyToolError(call, result, err)
}

// classifyToolError decides what an error of a tool means, once:
//
//   - invalid arguments are the call's own failure;
//   - a typed failure is the result, with its owner and effect;
//   - a transient transport or capacity fault is returned, so that Dex repeats
//     the call within the retry budget of the tool, as is a cancellation, which
//     ends the Step and never answers the model;
//   - any other error is an unexpected platform failure, recorded at once: a
//     Step cannot end a Dex retry sequence with an error, and repeating an
//     error that is not transient would not help.
func classifyToolError(call Call, result Result, err error) (Result, error) {
	var invalid *argumentsError
	var failure *sandboxd.ToolFailure
	switch {
	case err == nil:
		return result, nil
	case errors.As(err, &invalid):
		return failureResult(conversation.ToolFailure{Code: toolfailure.InvalidArguments, Effect: toolfailure.EffectNone, Message: invalid.Error()}), nil
	case errors.As(err, &failure):
		failed := failureResult(modelFailure(failure))
		failed.StartedAt, failed.EndedAt = result.StartedAt, result.EndedAt
		return failed, nil
	case sandboxd.IsTransient(err) || errors.Is(err, context.Canceled):
		return result, err
	default:
		effect := toolfailure.EffectNone
		if call.Mutating {
			effect = toolfailure.EffectUnknown
		}
		return failureResult(conversation.ToolFailure{Code: toolfailure.ToolError, Effect: effect, Message: "The tool failed with an unexpected error.", Detail: err.Error()}), nil
	}
}

// modelFailure is what the model reads of a typed failure.
func modelFailure(failure *sandboxd.ToolFailure) conversation.ToolFailure {
	return conversation.ToolFailure{Code: failure.Code(), Effect: failure.Effect(), Message: failure.Error(), Detail: failure.Detail()}
}

// bindWorkspace borrows the pooled Sandbox of the admitted lease. The Session
// fenced the lease to itself when it accepted the grant.
func (runner *Runner) bindWorkspace(ctx context.Context, call Call) (workspace.Resolution, error) {
	if call.Lease == nil {
		return workspace.Resolution{}, ErrWorkspaceUnavailable
	}
	resolution, err := runner.workspaces.Bind(ctx, call.Lease.Binding)
	if errors.Is(err, workspace.ErrUnreachable) {
		return workspace.Resolution{}, fmt.Errorf("%w: %w", ErrWorkspaceUnavailable, err)
	}
	return resolution, err
}
