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
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput/generated"
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
		if contributed.CheckFailed != "" {
			return agentloop.SessionFailureResult(toolfailure.ConnectorCheckFailed, contributed.CheckFailed), nil
		}
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

// newSandboxCall is the trusted context of a Sandbox call: the admitted lease,
// the confirmed HEAD, the current requirements, the agent root and the call's
// stable identity.
func newSandboxCall(ctx dex.Context, call conversation.ToolCall) sandboxtools.Call {
	result := newSandboxCallBase(ctx, call)
	result.Root = sandboxCallRoot(ctx, call)
	return result
}

// Command settlement only handles bash calls, which have no file root. Keep
// its trusted call context separate from file-root resolution, which reads
// the open worktree AttributeMaps.
func newSandboxCallBase(ctx dex.Context, call conversation.ToolCall) sandboxtools.Call {
	return sandboxtools.Call{
		Tool: string(call.Name), Arguments: call.Arguments.String(), HeadCommit: comm.Must(headCommitAttribute.Get(ctx)), Progress: func(string) {},
		Mutating:     agentloop.CatalogTool(call.Name).WorkspaceAccess == agentloop.WorkspaceAccessMutating,
		Lease:        agentloop.AdmittedLease(dexflow.ValueOrZero(workspaceUseAttribute.Get(ctx)), dexflow.ValueOrZero(workspaceAttribute.Get(ctx))),
		Requirements: dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx)).TraceScope(),
		Identity:     sandboxd.InvocationIdentity{FlowID: ctx.FlowID(), CallID: string(call.ID)},
	}
}

// withTrustedSource adds to the trusted context of a call the source the Session
// read for it: for place_attachment the file of the attachment it names, from
// its Project's bound attachments, and for place_evolve_export the export of
// the review it names, from its Project's evolve objects and repository. A call
// whose arguments the tool refuses reads none. A read error retries the Step.
func (flow *SessionAgentFlow) withTrustedSource(ctx dex.Context, execution context.Context, call conversation.ToolCall, sandboxCall sandboxtools.Call) (sandboxtools.Call, error) {
	switch call.Name {
	case conversation.ToolPlaceAttachment:
		input, err := toolinput.PlaceAttachment.Decode(call.Arguments.String())
		if err != nil {
			return sandboxCall, nil
		}
		file, found, err := flow.renditions.BoundFile(execution, comm.Must(projectIDAttribute.Get(ctx)), input.AttachmentID)
		if err != nil {
			return sandboxCall, dex.ErrorWithStack(fmt.Errorf("read the file of attachment %s: %w", input.AttachmentID, err))
		}
		if found {
			sandboxCall.Attachment = &file
		}
	case conversation.ToolPlaceEvolveExport:
		input, err := toolinput.PlaceEvolveExport.Decode(call.Arguments.String())
		if err != nil || sandboxCall.Lease == nil {
			return sandboxCall, nil
		}
		export, err := flow.dependency.EvolveExport(execution, comm.Must(projectIDAttribute.Get(ctx)), input.ReviewID, sandboxCall.Lease.Repository)
		if err != nil {
			return sandboxCall, dex.ErrorWithStack(fmt.Errorf("read the export of review %s: %w", input.ReviewID, err))
		}
		sandboxCall.EvolveExport = export
	default:
		// Any other call reads no source of its own.
	}
	return sandboxCall, nil
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
			cleanupCall.Arguments = sandboxtools.AgentWorktreeArguments(record.CleanupID, nil, nil)
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
		sandboxCall.Arguments = sandboxtools.AgentWorktreeArguments(record.WorktreeID, keep, nil)
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
			sandboxCall.Arguments = sandboxtools.AgentWorktreeArguments(record.WorktreeID, nil, nil)
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
	record := agenttasks.NewSubagentRecord(agentID, call.ID, requested, requested.Fingerprint(ctx.FlowID(), agentID), now)
	record.Model = comm.Must(sessionModelAttribute.Get(ctx)).Model
	comm.MustDo(subagentsAttribute.Set(ctx, agentID, record))
	state.SubagentsStartedWithoutUser, state.ConnectorSlugs = state.SubagentsStartedWithoutUser+1, agenttasks.ConnectorSlugsWith(state.ConnectorSlugs, requested)
	comm.MustDo(agentStateAttribute.Set(ctx, agentloop.WithEvolveSubagent(state, agentID)))
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

// startSubagent creates the admitted worktree and starts the child with the frozen
// model and lease. Credentials stay transient; the owning subagent contract describes recovery.
func (flow *SessionAgentFlow) startSubagent(ctx dex.Context, execution context.Context, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	agentID, records := flowid.AgentTaskID(ctx.FlowID(), string(call.ID)), loadedSubagents(ctx)
	record, isRecorded := records[agentID]
	switch {
	case !isRecorded:
		panic(fmt.Sprintf("start_agent call %s runs without the record its route admitted", call.ID))
	case record.Status.Finished():
		return subagentToolResult(subagentStarted{TaskID: record.TaskID(), Status: record.Status.String()}), nil
	}
	if _, err := preparedSubagentCredential(ctx, agentID, subagentCredentialModel(ctx, call)); err != nil {
		return credentialStartFailure(ctx, call, agentID, err)
	}
	keep := make([]string, 0, len(records))
	for _, unfinished := range agenttasks.UnfinishedSubagents(records, time.Now()) {
		keep = append(keep, unfinished.AgentID)
	}
	keep = append(keep, openTaskWorktreeIDs(ctx)...)
	// A connector_develop subagent works in a worktree of the connectors library.
	sandboxCall, create := newSandboxCall(ctx, call), sandboxtools.CreateAgentWorktree
	sandboxCall.Arguments = sandboxtools.AgentWorktreeArguments(agentID, keep, record.Inputs)
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
		BaseCommit: started.BaseCommit, Branch: started.Branch, Model: subagentCredentialModel(ctx, call), HeadCommit: comm.Must(headCommitAttribute.Get(ctx)), Lease: *sandboxCall.Lease}
	if err := flow.startCredentialSubagent(ctx, execution, start); err != nil {
		return credentialStartFailure(ctx, call, agentID, err)
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

// cleanupExpiredAgentWorktree removes, in its own tool attempt before a start_agent call runs, the
// worktree of the expired subagent record that the call's admission evicted.
func (flow *SessionAgentFlow) cleanupExpiredAgentWorktree(ctx dex.Context, call conversation.ToolCall, expired string) error {
	execution, end := beginToolAttempt(ctx, call)
	defer end()
	return flow.sandbox.CleanupExpiredAgentWorktree(execution, newSandboxCall(ctx, call), expired)
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
