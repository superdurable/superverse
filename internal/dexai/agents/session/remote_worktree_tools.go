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

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

const connectorContributorSkill = "/opt/superverse/dex-skills/dex-connector-contributor/SKILL.md"

func isRemoteWorktreeTool(name conversation.ToolName) bool {
	switch name {
	case conversation.ToolStartConnectorContribution, conversation.ToolSubmitConnectorContribution,
		conversation.ToolOpenTaskWorktree, conversation.ToolMergeTaskWorktree:
		return true
	}
	return false
}

func (flow *SessionAgentFlow) executeRemoteWorktreeTool(ctx dex.Context, execution context.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	switch call.Name {
	case conversation.ToolStartConnectorContribution, conversation.ToolSubmitConnectorContribution:
		return flow.executeConnectorTool(ctx, execution, call)
	case conversation.ToolOpenTaskWorktree, conversation.ToolMergeTaskWorktree:
		return flow.executeTaskWorktreeTool(ctx, execution, call)
	}
	panic("unknown remote worktree tool")
}

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
	return connectorPathRoot(ctx, fileToolPath(call))
}

func fileToolPath(call conversation.ToolCall) string {
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
	return path
}

func connectorPathRoot(ctx dex.Context, path string) string {
	if !filepath.IsAbs(path) {
		return ""
	}
	for _, slug := range connectorContributionsAttribute.AllInstanceKeys(ctx) {
		record := comm.Must(connectorContributionsAttribute.Get(ctx, slug))
		if record.Status == agentloop.ConnectorContributionOpen && withinRoot(path, record.WorktreePath) {
			return record.WorktreePath
		}
	}
	return ""
}

func withinRoot(path, root string) bool {
	clean := filepath.Clean(path)
	return clean == root || strings.HasPrefix(clean, root+"/")
}

func taskWorktreePathRoot(ctx dex.Context, path string) string {
	if !filepath.IsAbs(path) {
		return ""
	}
	for _, name := range taskWorktreesAttribute.AllInstanceKeys(ctx) {
		record := comm.Must(taskWorktreesAttribute.Get(ctx, name))
		if record.Status == agentloop.TaskWorktreeOpen && record.BaseCommit != "" && withinRoot(path, record.WorktreePath) {
			return record.WorktreePath
		}
	}
	return ""
}

func remoteWorktreeFileMapLoads(name conversation.ToolName) []dex.AttributeDef {
	switch name {
	case conversation.ToolRead, conversation.ToolEdit, conversation.ToolWrite, conversation.ToolGrep, conversation.ToolGlob:
		return []dex.AttributeDef{connectorContributionsAttribute, taskWorktreesAttribute}
	}
	return nil
}

func configureConnectorToolOptions(options *dex.StepOptions, call conversation.ToolCall) {
	options.ExecuteLoadAttributeMaps = append(options.ExecuteLoadAttributeMaps, remoteWorktreeFileMapLoads(call.Name)...)
	if slug := connectorSlugOfCall(call); slug != "" {
		options.ExecuteLoadAttributeMapInstances = []dex.AttributeMapLoad{connectorContributionsAttribute.Load(slug)}
	}
}

func sandboxCallRoot(ctx dex.Context, call conversation.ToolCall) string {
	if currentExecutor(ctx).Mode == remoteexec.ModeRemote {
		path := fileToolPath(call)
		if root := taskWorktreePathRoot(ctx, path); root != "" {
			return root
		}
		if root := connectorPathRoot(ctx, path); root != "" {
			return root
		}
	}
	return sandboxd.WorkspaceRoot
}

func taskWorktreeNameOfCall(call conversation.ToolCall) string {
	switch call.Name {
	case conversation.ToolOpenTaskWorktree:
		input, err := toolinput.OpenTaskWorktree.Decode(call.Arguments.String())
		if err == nil {
			return input.Name
		}
	case conversation.ToolMergeTaskWorktree:
		input, err := toolinput.MergeTaskWorktree.Decode(call.Arguments.String())
		if err == nil {
			return input.Name
		}
	}
	return ""
}

func openTaskWorktreeIDs(ctx dex.Context) []string {
	ids := make([]string, 0, agentloop.MaximumOpenTaskWorktrees)
	for _, name := range taskWorktreesAttribute.AllInstanceKeys(ctx) {
		record := comm.Must(taskWorktreesAttribute.Get(ctx, name))
		if record.Status == agentloop.TaskWorktreeOpen {
			ids = append(ids, record.WorktreeID)
		}
	}
	return ids
}

// admitTaskWorktree reserves a stable attempt under RouteTool's interrupt lock.
func admitTaskWorktree(ctx dex.Context, call conversation.ToolCall) bool {
	input, err := toolinput.OpenTaskWorktree.Decode(call.Arguments.String())
	if err != nil {
		appendSessionFailure(ctx, call, toolfailure.InvalidArguments, err.Error())
		return false
	}
	records := make(map[string]agentloop.TaskWorktree)
	for _, name := range taskWorktreesAttribute.AllInstanceKeys(ctx) {
		records[name] = comm.Must(taskWorktreesAttribute.Get(ctx, name))
	}
	record, evicted, admitted := agentloop.AdmitTaskWorktree(records, ctx.FlowID(), input.Name, call.ID)
	if !admitted {
		appendSessionFailure(ctx, call, toolfailure.TaskWorktreeLimitReached, "This Session allows three open task worktrees and retains at most sixteen names.")
		return false
	}
	if evicted != "" {
		comm.MustDo(taskWorktreesAttribute.Delete(ctx, evicted))
	}
	comm.MustDo(taskWorktreesAttribute.Set(ctx, input.Name, record))
	return true
}

func configureTaskWorktreeToolOptions(options *dex.StepOptions, call conversation.ToolCall) {
	if call.Name == conversation.ToolOpenTaskWorktree {
		options.ExecuteLoadAttributeMaps = append(options.ExecuteLoadAttributeMaps, taskWorktreesAttribute, subagentsAttribute)
	} else if name := taskWorktreeNameOfCall(call); name != "" {
		options.ExecuteLoadAttributeMapInstances = append(options.ExecuteLoadAttributeMapInstances, taskWorktreesAttribute.Load(name))
	}
}

func taskWorktreeResult(payload any) agentloop.ToolExecutionResult {
	return agentloop.ToolExecutionResult{Content: string(comm.Must(json.Marshal(payload))), Outcome: conversation.ToolOutcomeSucceeded}
}

func taskWorktreeOpenResult(record agentloop.TaskWorktree) agentloop.ToolExecutionResult {
	return taskWorktreeResult(map[string]any{
		"name": record.Name, "worktree_path": record.WorktreePath, "base_commit": record.BaseCommit,
		"status": record.Status, "attempt": record.Attempt,
		"instruction": "Give this path to the Codex subagent and keep its edits under it. For bash, start with cd '" + record.WorktreePath + "' && because bash starts in /workspace.",
	})
}

func (flow *SessionAgentFlow) executeTaskWorktreeTool(ctx dex.Context, execution context.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	name := taskWorktreeNameOfCall(call)
	if name == "" {
		return agentloop.SessionFailureResult(toolfailure.InvalidArguments, "invalid task worktree arguments"), nil
	}
	record, err := taskWorktreesAttribute.Get(ctx, name)
	if dexflow.IsAttributeNotFound(err) {
		return agentloop.SessionFailureResult(toolfailure.TaskNotFound, "open the task worktree first"), nil
	}
	if err != nil {
		return agentloop.ToolExecutionResult{}, err
	}
	switch call.Name {
	case conversation.ToolOpenTaskWorktree:
		if record.Status != agentloop.TaskWorktreeOpen {
			return taskWorktreeResult(map[string]any{"name": name, "status": record.Status, "base_commit": record.BaseCommit}), nil
		}
		if record.BaseCommit != "" {
			return taskWorktreeOpenResult(record), nil
		}
		if record.CleanupID != "" {
			cleanupCall := newSandboxCall(ctx, call)
			cleanupCall.Arguments = sandboxtools.AgentWorktreeArguments(record.CleanupID, nil)
			cleaned, err := flow.sandbox.Run(execution, sandboxtools.CleanupAgentWorktree, cleanupCall)
			if err != nil {
				return agentloop.ToolExecutionResult{}, err
			}
			if cleaned.Failure != nil {
				return agentloop.SandboxToolResult(cleaned), nil
			}
			record.CleanupID = ""
			comm.MustDo(taskWorktreesAttribute.Set(ctx, name, record))
		}
		keep := openTaskWorktreeIDs(ctx)
		for _, unfinished := range agenttasks.UnfinishedSubagents(loadedSubagents(ctx), time.Now()) {
			keep = append(keep, unfinished.AgentID)
		}
		sandboxCall := newSandboxCall(ctx, call)
		sandboxCall.Arguments = sandboxtools.AgentWorktreeArguments(record.WorktreeID, keep)
		created, err := flow.sandbox.Run(execution, sandboxtools.CreateAgentWorktree, sandboxCall)
		if err != nil {
			return agentloop.ToolExecutionResult{}, err
		}
		if created.Failure != nil {
			if git.IsCreationRefusal(created.Failure.Code) {
				comm.MustDo(taskWorktreesAttribute.Delete(ctx, name))
			}
			return agentloop.SandboxToolResult(created), nil
		}
		record.BaseCommit = created.Worktree.Base
		comm.MustDo(taskWorktreesAttribute.Set(ctx, name, record))
		return taskWorktreeOpenResult(record), nil
	case conversation.ToolMergeTaskWorktree:
		input := comm.Must(toolinput.MergeTaskWorktree.Decode(call.Arguments.String()))
		if input.Discard.Value && len(input.Paths) > 0 {
			return agentloop.SessionFailureResult(toolfailure.InvalidArguments, "paths apply only to merges"), nil
		}
		if record.Status == agentloop.TaskWorktreeMerged || record.Status == agentloop.TaskWorktreeDiscarded {
			return taskWorktreeResult(record.Receipt), nil
		}
		if record.BaseCommit == "" && !input.Discard.Value {
			return agentloop.SessionFailureResult(toolfailure.InvalidArguments, "the task worktree was not created"), nil
		}
		if record.Status == agentloop.TaskWorktreeOpen {
			sandboxCall := newSandboxCall(ctx, call)
			sandboxCall.Arguments = sandboxtools.AgentWorktreeArguments(record.WorktreeID, nil)
			finished, err := flow.sandbox.Run(execution, sandboxtools.FinishAgentWorktree, sandboxCall)
			if err != nil {
				return agentloop.ToolExecutionResult{}, err
			}
			if finished.Failure != nil {
				return agentloop.SandboxToolResult(finished), nil
			}
			if record.BaseCommit == "" && finished.Finished.Missing {
				now := time.Now().UTC()
				record.Status, record.MergeCallID, record.ClosedAt = agentloop.TaskWorktreeDiscarded, call.ID, &now
				record.Receipt = &git.MergeReceipt{Outcome: git.MergeDiscarded}
				comm.MustDo(taskWorktreesAttribute.Set(ctx, name, record))
				return taskWorktreeResult(record.Receipt), nil
			}
			if record.BaseCommit == "" {
				record.BaseCommit = finished.Finished.Base
			}
			record.Status = agentloop.TaskWorktreeSaved
			comm.MustDo(taskWorktreesAttribute.Set(ctx, name, record))
		}
		sandboxCall := newSandboxCall(ctx, call)
		sandboxCall.Arguments = sandboxtools.TaskWorktreeMergeArguments(record.WorktreeID, input.Discard.Value, input.Paths)
		merged, err := flow.sandbox.Run(execution, sandboxtools.MergeAgentChanges, sandboxCall)
		if err != nil {
			return agentloop.ToolExecutionResult{}, err
		}
		if merged.Failure != nil {
			return agentloop.SandboxToolResult(merged), nil
		}
		record.MergeCallID, record.Receipt = call.ID, merged.Merge
		now := time.Now().UTC()
		if input.Discard.Value {
			record.Status = agentloop.TaskWorktreeDiscarded
			record.ClosedAt = &now
		} else if len(input.Paths) == 0 {
			record.Status = agentloop.TaskWorktreeMerged
			record.ClosedAt = &now
		}
		comm.MustDo(taskWorktreesAttribute.Set(ctx, name, record))
		return taskWorktreeResult(merged.Merge), nil
	}
	panic("unknown task worktree tool")
}
