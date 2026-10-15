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
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// ConnectorContributions is keyed by the canonical connector slug. It holds
// only the remote worktree attempt; ConnectorContributionFlow owns PR and RC.
var connectorContributionsAttribute = dex.DefineAttributeMap[agentloop.ConnectorContributionAttempt]("ConnectorContributions")

const connectorContributorSkill = "/opt/superverse/dex-skills/dex-connector-contributor/SKILL.md"

func connectorContributionSlugs(ctx dex.Context, platform []string) []string {
	slugs := slices.Clone(platform)
	for _, slug := range connectorContributionsAttribute.AllInstanceKeys(ctx) {
		if !slices.Contains(slugs, slug) {
			slugs = append(slugs, slug)
		}
	}
	return slugs
}

func remoteConnectorOpen(ctx dex.Context, slug string) bool {
	record, err := connectorContributionsAttribute.Get(ctx, slug)
	if dexflow.IsAttributeNotFound(err) {
		return false
	}
	return comm.Must(record, err).Status == agentloop.ConnectorContributionOpen
}

func connectorSlugOfCall(call conversation.ToolCall) string {
	switch call.Name {
	case conversation.ToolStartConnectorContribution:
		input, err := toolinput.StartConnectorContribution.Decode(call.Arguments.String())
		if err == nil {
			return input.Slug
		}
	case conversation.ToolSubmitConnectorContribution:
		input, err := toolinput.SubmitConnectorContribution.Decode(call.Arguments.String())
		if err == nil {
			return input.Slug
		}
	}
	return ""
}

// admitConnectorContribution runs under RouteTool's interrupt lock. A new
// attempt of an already submitted slug uses a new worktree and the same branch.
func admitConnectorContribution(ctx dex.Context, call conversation.ToolCall) bool {
	input, err := toolinput.StartConnectorContribution.Decode(call.Arguments.String())
	if err != nil {
		appendSessionFailure(ctx, call, toolfailure.InvalidArguments, err.Error())
		return false
	}
	var kind conversation.ContributionKind
	if err := kind.UnmarshalText([]byte(input.Contribution)); err != nil {
		appendSessionFailure(ctx, call, toolfailure.InvalidArguments, err.Error())
		return false
	}
	current, err := connectorContributionsAttribute.Get(ctx, input.Slug)
	if err == nil {
		if current.StartCallID == call.ID {
			return true
		}
		if current.Status == agentloop.ConnectorContributionOpen {
			appendSessionFailure(ctx, call, toolfailure.ConnectorContributionRunning, "this connector already has an open remote worktree")
			return false
		}
	} else if !dexflow.IsAttributeNotFound(err) {
		panic(err)
	}
	for _, record := range agenttasks.UnfinishedSubagents(loadedSubagents(ctx), time.Now().UTC()) {
		if record.Connector != nil && record.Connector.Slug == input.Slug {
			appendSessionFailure(ctx, call, toolfailure.ConnectorContributionRunning, "a platform subagent already works on this connector")
			return false
		}
	}
	slugs := connectorContributionSlugs(ctx, comm.Must(agentStateAttribute.Get(ctx)).ConnectorSlugs)
	if !slices.Contains(slugs, input.Slug) && len(slugs) >= agenttasks.MaximumConnectorContributions {
		appendSessionFailure(ctx, call, toolfailure.ConnectorContributionLimitReached, "this Session already contributes to 16 connectors")
		return false
	}
	worktreeID := flowid.AgentTaskID(ctx.FlowID(), string(call.ID))
	attempt := current.Attempt + 1
	comm.MustDo(connectorContributionsAttribute.Set(ctx, input.Slug, agentloop.ConnectorContributionAttempt{
		Contribution: kind, WorktreeID: worktreeID, WorktreePath: git.ContributionWorktreeRoot(worktreeID),
		Status: agentloop.ConnectorContributionOpen, StartCallID: call.ID, Attempt: attempt,
	}))
	writeSnapshotRequired(ctx)
	return true
}

func connectorToolResult(payload any) agentloop.ToolExecutionResult {
	return agentloop.ToolExecutionResult{Content: string(comm.Must(json.Marshal(payload))), Outcome: conversation.ToolOutcomeSucceeded}
}

func (flow *SessionAgentFlow) executeConnectorTool(ctx dex.Context, execution context.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	var slug, summary string
	switch call.Name {
	case conversation.ToolStartConnectorContribution:
		input, err := toolinput.StartConnectorContribution.Decode(call.Arguments.String())
		if err != nil {
			return agentloop.SessionFailureResult(toolfailure.InvalidArguments, err.Error()), nil
		}
		slug = input.Slug
	case conversation.ToolSubmitConnectorContribution:
		input, err := toolinput.SubmitConnectorContribution.Decode(call.Arguments.String())
		if err != nil {
			return agentloop.SessionFailureResult(toolfailure.InvalidArguments, err.Error()), nil
		}
		slug, summary = input.Slug, input.Summary
	default:
		panic("unknown connector contribution tool")
	}
	if slug == "" {
		return agentloop.SessionFailureResult(toolfailure.InvalidArguments, "invalid connector contribution arguments"), nil
	}
	record, err := connectorContributionsAttribute.Get(ctx, slug)
	if dexflow.IsAttributeNotFound(err) {
		return agentloop.SessionFailureResult(toolfailure.InvalidArguments, "start this connector contribution first"), nil
	}
	if err != nil {
		return agentloop.ToolExecutionResult{}, err
	}
	switch call.Name {
	case conversation.ToolStartConnectorContribution:
		if record.StartCallID != call.ID {
			return agentloop.SessionFailureResult(toolfailure.ConnectorContributionRunning, "a later attempt replaced this start"), nil
		}
		if record.BaseCommit != "" {
			return connectorToolResult(map[string]any{"worktree_path": record.WorktreePath, "base_commit": record.BaseCommit, "skill_path": connectorContributorSkill, "attempt": record.Attempt}), nil
		}
		target := git.ContributionTarget{AgentID: record.WorktreeID, ProjectID: comm.Must(projectIDAttribute.Get(ctx)), Slug: slug, Kind: record.Contribution}
		sandboxCall := newSandboxCall(ctx, call)
		sandboxCall.Arguments = sandboxtools.ContributionArguments(target, "", false)
		result, err := flow.sandbox.Run(execution, sandboxtools.CreateContributionWorktree, sandboxCall)
		if err != nil {
			return agentloop.ToolExecutionResult{}, err
		}
		if result.Failure != nil {
			record.Status = agentloop.ConnectorContributionFailed
			comm.MustDo(connectorContributionsAttribute.Set(ctx, slug, record))
			return agentloop.SandboxToolResult(result), nil
		}
		record.BaseCommit = result.Contribution.Base
		comm.MustDo(connectorContributionsAttribute.Set(ctx, slug, record))
		return connectorToolResult(map[string]any{"worktree_path": record.WorktreePath, "base_commit": record.BaseCommit, "skill_path": connectorContributorSkill, "attempt": record.Attempt}), nil
	case conversation.ToolSubmitConnectorContribution:
		if record.Status == agentloop.ConnectorContributionSubmitted {
			if record.Result == nil {
				return agentloop.SessionFailureResult(toolfailure.ToolError, "submitted connector result is unavailable"), nil
			}
			return connectorToolResult(map[string]any{"accepted": true, "slug": slug, "head_commit": record.Result.HeadCommit}), nil
		}
		if record.Status != agentloop.ConnectorContributionOpen || record.BaseCommit == "" {
			return agentloop.SessionFailureResult(toolfailure.InvalidArguments, "this connector has no prepared open worktree"), nil
		}
		if record.SubmitCallID != "" && record.SubmitCallID != call.ID {
			return agentloop.SessionFailureResult(toolfailure.ConnectorContributionRunning, "another submit call is being reconciled"), nil
		}
		record.SubmitCallID, record.Summary = call.ID, summary
		target := git.ContributionTarget{AgentID: record.WorktreeID, ProjectID: comm.Must(projectIDAttribute.Get(ctx)), Slug: slug, Kind: record.Contribution}
		sandboxCall := newSandboxCall(ctx, call)
		sandboxCall.Arguments = sandboxtools.ContributionArguments(target, summary, true)
		result, err := flow.sandbox.Run(execution, sandboxtools.FinishContribution, sandboxCall)
		if err != nil {
			return agentloop.ToolExecutionResult{}, err
		}
		if result.Failure != nil {
			return agentloop.SandboxToolResult(result), nil
		}
		contributed := result.Contributed
		// FinishContribution reads its per-worktree receipt on a retry after
		// Submit failed. The receipt returns the pushed head again, so an
		// Unchanged result here describes an actually unchanged first finish.
		if contributed.Missing || contributed.Unchanged || contributed.Rejected != "" {
			record.Status = agentloop.ConnectorContributionFailed
			comm.MustDo(connectorContributionsAttribute.Set(ctx, slug, record))
			reason := contributed.Rejected
			switch {
			case contributed.Missing:
				reason = "worktree is missing"
			case contributed.Unchanged:
				reason = "worktree has no connector changes"
			}
			return agentloop.SessionFailureResult(toolfailure.InvalidArguments, fmt.Sprintf("connector contribution refused: %s", reason)), nil
		}
		contribution := conversation.ConnectorContribution{Slug: slug, Module: contributed.Module, Kind: record.Contribution,
			Branch: contributed.Branch, HeadCommit: contributed.Commit, Location: contributed.Location, Summary: summary,
			ReleaseCandidate: contributed.ReleaseCandidate, CandidateRefused: contributed.CandidateRefused,
			CandidatePublished: false}
		if err := flow.dependency.SubmitConnectorContribution(execution, target.ProjectID, contribution, summary); err != nil {
			return agentloop.ToolExecutionResult{}, fmt.Errorf("submit connector %s: %w", slug, err)
		}
		now := time.Now().UTC()
		record.Status, record.Result, record.SubmittedAt = agentloop.ConnectorContributionSubmitted, &contribution, &now
		comm.MustDo(connectorContributionsAttribute.Set(ctx, slug, record))
		writeSnapshotRequired(ctx)
		return connectorToolResult(map[string]any{"accepted": true, "slug": slug, "head_commit": contributed.Commit}), nil
	}
	panic("unknown connector contribution tool")
}

// connectorFileRoot selects a server-owned worktree root for an absolute file
// path of an OPEN remote contribution. The file helper still confines it and
// forbids .git relative to that selected root.
func connectorFileRoot(ctx dex.Context, call conversation.ToolCall) string {
	var path string
	switch call.Name {
	case conversation.ToolRead:
		if input, err := toolinput.Read.Decode(call.Arguments.String()); err == nil {
			path = input.Path
		}
	case conversation.ToolEdit:
		if input, err := toolinput.Edit.Decode(call.Arguments.String()); err == nil {
			path = input.Path
		}
	case conversation.ToolWrite:
		if input, err := toolinput.Write.Decode(call.Arguments.String()); err == nil {
			path = input.Path
		}
	case conversation.ToolGrep:
		if input, err := toolinput.Grep.Decode(call.Arguments.String()); err == nil {
			path = input.Path.Value
		}
	case conversation.ToolGlob:
		if input, err := toolinput.Glob.Decode(call.Arguments.String()); err == nil {
			path = input.Path.Value
		}
	}
	if !filepath.IsAbs(path) {
		return ""
	}
	for _, slug := range connectorContributionsAttribute.AllInstanceKeys(ctx) {
		record := comm.Must(connectorContributionsAttribute.Get(ctx, slug))
		if record.Status == agentloop.ConnectorContributionOpen && strings.HasPrefix(path, record.WorktreePath+"/") {
			return record.WorktreePath
		}
	}
	return ""
}

func connectorToolMapLoads(name conversation.ToolName) []dex.AttributeDef {
	switch name {
	case conversation.ToolRead, conversation.ToolEdit, conversation.ToolWrite, conversation.ToolGrep, conversation.ToolGlob:
		return []dex.AttributeDef{connectorContributionsAttribute}
	}
	return nil
}

func configureConnectorToolOptions(options *dex.StepOptions, call conversation.ToolCall) {
	options.ExecuteLoadAttributeMaps = append(options.ExecuteLoadAttributeMaps, connectorToolMapLoads(call.Name)...)
	if slug := connectorSlugOfCall(call); slug != "" {
		options.ExecuteLoadAttributeMapInstances = []dex.AttributeMapLoad{connectorContributionsAttribute.Load(slug)}
	}
}

func sandboxCallRoot(ctx dex.Context, call conversation.ToolCall) string {
	if currentExecutor(ctx).Mode == remoteexec.ModeRemote {
		if root := connectorFileRoot(ctx, call); root != "" {
			return root
		}
	}
	return sandboxd.WorkspaceRoot
}
