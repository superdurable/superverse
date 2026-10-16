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
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput/generated"
)

// The delegation tools reach a subagent's worktree in the workspace
// repository through the worktree helper (git.GitOperations): the Session
// creates the worktree for start_agent, the subagent saves it when it ends,
// and merge_agent_changes merges or discards what it saved. None reads a Flow.

// AgentWorktreeArguments are the trusted arguments of the worktree operations
// of the subagent agentID, which the Session and the subagent build; keep
// names the Session's unfinished subagents, whose worktrees a creation keeps.
func AgentWorktreeArguments(agentID string, keep []string) string {
	return string(comm.Must(json.Marshal(agentWorktreeArguments{AgentID: agentID, Keep: keep})))
}

// TaskWorktreeMergeArguments adapts a server-owned Codex worktree identity to
// the same validated merge operation used for platform subagent worktrees.
func TaskWorktreeMergeArguments(agentID string, discard bool, paths []string) string {
	action := generated.MergeAgentChangesActionApply
	if discard {
		action = generated.MergeAgentChangesActionDiscard
	}
	return string(comm.Must(json.Marshal(struct {
		AgentID string                            `json:"agent_id"`
		Action  generated.MergeAgentChangesAction `json:"action"`
		Paths   []string                          `json:"paths,omitempty"`
	}{AgentID: flowid.SubagentTaskID(agentID), Action: action, Paths: paths})))
}

type agentWorktreeArguments struct {
	AgentID string   `json:"agent_id"`
	Keep    []string `json:"keep"`
}

// worktreeArguments decodes the platform's own arguments, so a malformed one
// is a programming error.
func worktreeArguments(call Call) agentWorktreeArguments {
	var arguments agentWorktreeArguments
	comm.MustDo(json.Unmarshal([]byte(call.Arguments), &arguments))
	return arguments
}

// createAgentWorktree snapshots the workspace into a new worktree of the
// subagent, or answers the one an earlier attempt created.
func (execution *Execution) createAgentWorktree(ctx context.Context, call Call) (Result, error) {
	arguments := worktreeArguments(call)
	worktree, err := execution.commits.CreateAgentWorktree(ctx, execution.executor, arguments.AgentID, arguments.Keep)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: "Created the worktree of subagent " + arguments.AgentID + " at " + worktree.Base + ".", Worktree: &worktree}, nil
}

// finishAgentWorktree saves the subagent's worktree into its result and
// removes the checkout, or answers the result an earlier attempt saved.
func (execution *Execution) finishAgentWorktree(ctx context.Context, call Call) (Result, error) {
	finished, err := execution.commits.FinishAgentWorktree(ctx, execution.executor, worktreeArguments(call).AgentID)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: fmt.Sprintf("Saved %d changed file(s).", finished.Files), Finished: &finished}, nil
}

// cleanupAgentWorktree removes a closed task attempt's refs and receipts.
func (execution *Execution) cleanupAgentWorktree(ctx context.Context, call Call) (Result, error) {
	id := worktreeArguments(call).AgentID
	if err := execution.commits.CleanupAgentWorktree(ctx, execution.executor, id); err != nil {
		return Result{}, err
	}
	return Result{Content: "Cleaned the closed task worktree."}, nil
}

// ContributionArguments are the trusted arguments of the contribution
// operations of a develop subagent, which the Session and the subagent build;
// the finish only reads summary, which titles the commit, and contributes,
// which only a completed subagent sets.
func ContributionArguments(target git.ContributionTarget, summary string, contributes bool) string {
	return string(comm.Must(json.Marshal(contributionArguments{Target: target, Summary: summary, Contributes: contributes})))
}

type contributionArguments struct {
	Target      git.ContributionTarget `json:"target"`
	Summary     string                 `json:"summary,omitempty"`
	Contributes bool                   `json:"contributes,omitempty"`
}

// decodeContributionArguments decodes the platform's own arguments, so a
// malformed one is a programming error.
func decodeContributionArguments(call Call) contributionArguments {
	var arguments contributionArguments
	comm.MustDo(json.Unmarshal([]byte(call.Arguments), &arguments))
	return arguments
}

// createContributionWorktree checks the connector's branch of the library out
// as the develop subagent's worktree, or answers the one an earlier attempt
// created.
func (execution *Execution) createContributionWorktree(ctx context.Context, call Call) (Result, error) {
	target := decodeContributionArguments(call).Target
	worktree, err := execution.commits.CreateContributionWorktree(ctx, execution.executor, target)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: "Created the worktree of subagent " + target.AgentID + " on " + worktree.Branch + " at " + worktree.Base + ".", Contribution: &worktree}, nil
}

// finishContribution verifies, commits and pushes the develop subagent's
// change and removes its worktree, or answers what an earlier attempt did.
func (execution *Execution) finishContribution(ctx context.Context, call Call) (Result, error) {
	arguments := decodeContributionArguments(call)
	contributed, err := execution.commits.FinishContribution(ctx, execution.executor, arguments.Target, arguments.Summary, arguments.Contributes)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: fmt.Sprintf("Contributed %d changed file(s) on %s.", contributed.Files, contributed.Branch), Contributed: &contributed}, nil
}

// mergeReport is what the model reads of a merge.
type mergeReport struct {
	Result    string              `json:"result"`
	Applied   []string            `json:"applied"`
	Conflicts []git.MergeConflict `json:"conflicts"`
	Unchanged []string            `json:"unchanged"`
	Unknown   []string            `json:"unknown,omitempty"`
	Next      string              `json:"next,omitempty"`
}

// mergeAgentChanges merges a finished subagent's changes into the workspace
// or discards them, and answers the recorded receipt of an earlier call. The
// Session checked that the agent is its own and finished before it ran.
func (execution *Execution) mergeAgentChanges(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.MergeAgentChanges.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	agentID, isAgent := flowid.ParseSubagentTaskID(input.AgentID)
	if !isAgent {
		return Result{}, invalidArguments(fmt.Errorf("agent_id %q is not the task ID of a subagent, such as agent-k3n8q5r1", input.AgentID))
	}
	request := git.MergeRequest{Discard: input.Action == generated.MergeAgentChangesActionDiscard}
	if request.Discard && len(input.Paths) > 0 {
		return Result{}, invalidArguments(errors.New("paths: only apply merges selected files"))
	}
	for index, name := range input.Paths {
		if err = validateText(fmt.Sprintf("paths[%d]", index), name, maximumPathBytes); err != nil {
			return Result{}, err
		}
		if cleaned := path.Clean(name); path.IsAbs(name) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return Result{}, invalidArguments(fmt.Errorf("paths[%d] %q must be relative to the workspace", index, name))
		}
		request.Paths = append(request.Paths, path.Clean(name))
	}
	receipt, err := execution.commits.MergeAgentChanges(ctx, execution.executor, agentID, request)
	if err != nil {
		return Result{}, err
	}
	return MergeResult(agentID, receipt), nil
}

// MergeResult is the result of merge_agent_changes with receipt for the
// subagent agentID: the report the model reads and the receipt.
func MergeResult(agentID string, receipt git.MergeReceipt) Result {
	report := mergeReport{Result: receipt.Outcome.String(), Applied: receipt.Applied, Conflicts: receipt.Conflicts, Unchanged: receipt.Unchanged, Unknown: receipt.Unknown}
	var next []string
	if len(receipt.Unknown) > 0 {
		next = append(next, "The paths under unknown name no file the subagent changed; its report names the files it changed.")
	}
	if len(receipt.Conflicts) > 0 {
		next = append(next, "A content conflict holds <<<<<<< workspace, ||||||| snapshot, ======= and >>>>>>> "+flowid.SubagentTaskID(agentID)+
			" markers in the file: resolve them. Every other conflict kept the workspace's version; read the subagent's version with git show refs/superverse/agents/"+
			agentID+"/result:<path>.")
	}
	report.Next = strings.Join(next, " ")
	result := jsonResult(report, false)
	result.Merge = &receipt
	return result
}
