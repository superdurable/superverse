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
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
)

// A connector_develop subagent works in a worktree of the connectors library
// on its connector's branch, which the Session created with start_agent. Its
// first segment names the contribution instead of the application's
// repository context, and its finish commits and pushes the branch: only a
// completed subagent contributes, any other end only saves its work.

// repositoryContext is the context every segment of the subagent carries: the
// contribution of a develop subagent, or the trusted context of the
// application's HEAD on lease, whose absence only the model reads.
func (flow *SubagentFlow) repositoryContext(ctx context.Context, start startRecord, lease workspace.Lease) string {
	if target := start.Call.Connector; target != nil {
		return fmt.Sprintf("<connector_contribution>\nslug: %s\ncontribution: %s\nbranch: %s\nbase_commit: %s\nlibrary: %s\n</connector_contribution>\n"+
			"Your root is a worktree of the connectors library on this branch. Read AGENTS.md at its root first.",
			target.Slug, target.Contribution, start.Branch, start.BaseCommit, "superdurable/dex-connectors-library")
	}
	context, err := flow.repository.RepositoryContext(ctx, lease)
	if err != nil {
		// The context only orients the model; the worktree holds the files.
		return "The repository context could not be read: " + dexflow.TruncateCause(err.Error())
	}
	return context
}

// saveWork finishes the subagent's worktree into finish: an application
// worktree saves its changes for the merge, and a develop subagent's library
// worktree saves its work and, when the subagent completed, contributes it. A
// lease the transport proved unreachable is sandboxtools.ErrWorkspaceUnavailable.
func (flow *SubagentFlow) saveWork(ctx dex.Context, finish *subagentFinish) error {
	start := comm.Must(startAttribute.Get(ctx))
	if start.Call.Connector == nil {
		changes, unsaved, err := flow.finishWorktree(ctx)
		finish.Changes, finish.Unsaved = changes, unsaved
		return err
	}
	identity, ws := comm.Must(identityAttribute.Get(ctx)), comm.Must(workspaceAttribute.Get(ctx))
	completed := finish.Status == agenttasks.SubagentCompleted
	call := sandboxtools.Call{Tool: string(conversation.ToolStartAgent), Lease: &ws.Lease, Root: sandboxd.WorkspaceRoot, Mutating: true, Progress: func(string) {},
		Arguments: sandboxtools.ContributionArguments(identity.Contribution(*start.Call.Connector), start.Call.Description, completed),
		Identity:  sandboxd.InvocationIdentity{FlowID: ctx.FlowID(), CallID: "finish"}}
	defer dexflow.KeepAlive(ctx, agentloop.ToolHeartbeat{Phase: agentloop.HeartbeatPhaseToolCall, ToolName: conversation.ToolStartAgent})()
	result, err := flow.sandbox.Run(ctx, sandboxtools.FinishContribution, call)
	switch {
	case err != nil:
		return err
	case result.Failure != nil:
		finish.Unsaved = "the Sandbox could not save it: " + result.Failure.Text()
		return nil
	}
	contributed := *result.Contributed
	switch {
	case contributed.Missing:
		finish.Unsaved = "its worktree was gone before it was saved"
	case !completed:
	case contributed.Unchanged:
		finish.Refused = "it changed nothing in its worktree"
	case contributed.Rejected != "":
		finish.Refused = contributed.Rejected
	default:
		finish.Contribution = &conversation.ConnectorContribution{Slug: start.Call.Connector.Slug, Module: contributed.Module, Kind: start.Call.Connector.Contribution,
			Branch: contributed.Branch, HeadCommit: contributed.Commit, Location: contributed.Location, Summary: start.Call.Description,
			ReleaseCandidate: contributed.ReleaseCandidate, CandidateRefused: contributed.CandidateRefused,
			CandidatePublished: contributed.ReleaseCandidate != nil && contributed.Location == connectorlibrary.LocationLocalGitHost}
	}
	return nil
}

// contributionFinishOptions are a develop subagent's finish options: it also
// commits and pushes its branch and, for the local Git host, builds its
// release candidate with the library's tooling, so each of its 4 attempts has
// 15 minutes.
var contributionFinishOptions = &dex.StepOptions{ExecuteMethodTimeout: 15 * time.Minute, ExecuteRetry: dexflow.CappedStepRetry(4, time.Hour)}

// finishOptions are the options of the subagent's FinishSubagent movement:
// the registered ones, or contributionFinishOptions for a develop subagent.
func finishOptions(ctx dex.Context) *dex.StepOptions {
	if comm.Must(startAttribute.Get(ctx)).Call.Connector != nil {
		return contributionFinishOptions
	}
	return nil
}

// describeContribution is what the report adds about the contribution of a
// develop subagent: where its branch went, or why it contributed nothing.
func describeContribution(finish subagentFinish) string {
	switch {
	case finish.Contribution != nil:
		contribution := finish.Contribution
		described := fmt.Sprintf("Its contribution (%s) to %s: commit %s on the branch %s, %s.", contribution.Kind, contribution.Module, contribution.HeadCommit,
			contribution.Branch, contribution.Location.Description())
		switch candidate := contribution.ReleaseCandidate; {
		case contribution.CandidatePublished:
			described += fmt.Sprintf(" Its release candidate %s is published: run go get %s@%s, set the version of its connection in dex-app.yaml to %s, then commit and push.",
				candidate.Version, contribution.Module, candidate.Version, candidate.Version)
		case candidate != nil:
			described += fmt.Sprintf(" Its release candidate %s is being published; a connector notification names it when it is ready.", candidate.Version)
		case contribution.CandidateRefused != "":
			described += " The connectors library refused a release candidate of it: " + contribution.CandidateRefused + ". Start another connector_develop for this connector that fixes it."
		}
		return described
	case finish.Refused != "":
		return "It contributed nothing: " + finish.Refused + "."
	}
	return ""
}
