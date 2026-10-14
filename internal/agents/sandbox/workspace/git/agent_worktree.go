// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package git

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"path"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// A subagent works in its own git worktree of the workspace repository, which
// one helper process per operation creates, finishes and merges
// (agent_worktree.py, fixed program text that no model input selects). The
// worktree lives inside the Git directory, where git add --all, git clean and
// the main agent's file tools never reach it:
//
//	/workspace/.git/superverse/worktrees/<agent ID>   the checkout
//	refs/superverse/agents/<agent ID>/base            the snapshot it started from
//	refs/superverse/agents/<agent ID>/result          its changes, once finished
//	/workspace/.git/superverse/agents/<agent ID>/     the merge plans and receipts
//
// The refs outlive the checkout, so a finished subagent's result stays
// mergeable after the worktree is removed. Every operation queries before it
// writes, so a retry after a lost answer, a Worker restart or a replaced
// Sandbox repeats nothing it finished.

//go:embed agent_worktree.py
var agentWorktreeProgram string

const (
	// maximumSnapshotBytes bounds the uncommitted and untracked files a
	// worktree's snapshot copies; minimumFreeBytes is the free space of the
	// workspace volume a new worktree needs.
	maximumSnapshotBytes = 100 << 20
	minimumFreeBytes     = 1 << 30
	// maximumWorktreeAnswerBytes bounds one helper answer, whose merge lists
	// name every merged file.
	maximumWorktreeAnswerBytes = 4 << 20
)

// AgentWorktreeRoot is the agent root of the subagent agentID: its worktree.
func AgentWorktreeRoot(agentID string) string {
	return path.Join(sandboxd.WorkspaceRoot, ".git", "superverse", "worktrees", agentID)
}

// AgentWorktree is a subagent's created worktree: the snapshot commit it
// started from and its root.
type AgentWorktree struct {
	Base string `json:"base"`
	Root string `json:"worktree"`
}

// AgentResult is a finished worktree: its changes saved as Result on top of
// Base and their diffstat. Missing marks a subagent whose worktree was never
// created or was lost before it was saved, which left nothing to merge.
type AgentResult struct {
	Base       string `json:"base"`
	Result     string `json:"result"`
	Files      int    `json:"files"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
	Missing    bool   `json:"missing"`
}

// MergeOutcome is what merge_agent_changes did: merged every file, merged with
// conflicts the main agent resolves, found nothing to merge, or discarded the
// changes. It is a struct that string literals cannot convert to.
type MergeOutcome struct{ name string }

var (
	MergeMerged              = MergeOutcome{"merged"}
	MergeMergedWithConflicts = MergeOutcome{"merged_with_conflicts"}
	MergeNothing             = MergeOutcome{"nothing_to_merge"}
	MergeDiscarded           = MergeOutcome{"discarded"}
)

func (outcome MergeOutcome) String() string               { return outcome.name }
func (outcome MergeOutcome) MarshalText() ([]byte, error) { return []byte(outcome.name), nil }
func (outcome *MergeOutcome) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, outcome, MergeOutcome.String, MergeMerged, MergeMergedWithConflicts, MergeNothing, MergeDiscarded)
}

// mergeConflictKind is why the merge left one file to the main agent: both
// sides changed its text (its conflict markers are in the file), it is binary,
// one side deleted it while the other changed it, both sides added it, or the
// workspace changed it between the plan and the write. Every kind but content
// keeps the workspace's version.
type mergeConflictKind struct{ name string }

var (
	conflictContent            = mergeConflictKind{"content"}
	conflictBinary             = mergeConflictKind{"binary"}
	conflictDeleteModify       = mergeConflictKind{"delete_modify"}
	conflictAddAdd             = mergeConflictKind{"add_add"}
	conflictChangedDuringMerge = mergeConflictKind{"changed_during_merge"}
)

func (kind mergeConflictKind) String() string               { return kind.name }
func (kind mergeConflictKind) MarshalText() ([]byte, error) { return []byte(kind.name), nil }
func (kind *mergeConflictKind) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, kind, mergeConflictKind.String, conflictContent, conflictBinary, conflictDeleteModify, conflictAddAdd, conflictChangedDuringMerge)
}

// MergeConflict is one file the merge left to the main agent.
type MergeConflict struct {
	Path string            `json:"path"`
	Kind mergeConflictKind `json:"kind"`
}

// MergeReceipt is the recorded result of merging or discarding a subagent's
// changes; a repeated request answers it again. Unknown lists the requested
// paths that name no file the subagent changed.
type MergeReceipt struct {
	Outcome   MergeOutcome    `json:"outcome"`
	Applied   []string        `json:"applied"`
	Conflicts []MergeConflict `json:"conflicts"`
	Unchanged []string        `json:"unchanged"`
	Unknown   []string        `json:"unknown"`
}

// MergeRequest merges (Discard false) or discards a subagent's changes; Paths
// restricts a merge to the changed files they name, workspace-relative files
// or directories, and leaves the others mergeable. A discard or a merge of
// every file closes the changes.
type MergeRequest struct {
	Discard bool
	Paths   []string
}

// worktreeRefusal is a helper answer that refuses the operation.
type worktreeRefusal struct {
	Refusal string `json:"refusal"`
	Detail  string `json:"detail"`
}

// refusals are the codes of the helper's refusals, which only a creation
// answers.
var refusals = map[string]toolfailure.Code{
	"snapshot_too_large": toolfailure.SnapshotTooLarge,
	"workspace_disk_low": toolfailure.WorkspaceDiskLow,
}

// IsCreationRefusal reports whether code is a refusal of CreateAgentWorktree.
// The helper refuses only a creation that found no snapshot of the agent, so a
// refusal proves that no earlier creation succeeded.
func IsCreationRefusal(code toolfailure.Code) bool {
	for _, refusal := range refusals {
		if refusal == code {
			return true
		}
	}
	return false
}

// CreateAgentWorktree snapshots the workspace, its uncommitted and untracked
// files included, into the base ref of agentID and checks the snapshot out
// as the subagent's worktree, without touching the workspace's index, HEAD or
// files. An existing base ref is reused, so a repeated creation creates
// nothing. A first creation saves and removes the worktrees of earlier
// subagents that keep does not name, the Session's unfinished ones, and is
// refused (snapshot_too_large, workspace_disk_low) when the snapshot would
// exceed 100 MiB or the volume has less than 1 GiB free.
func (operations *GitOperations) CreateAgentWorktree(ctx context.Context, client *sandboxd.SandboxClient, agentID string, keep []string) (AgentWorktree, error) {
	if keep == nil {
		keep = []string{}
	}
	request := struct {
		Keep                 []string `json:"keep"`
		MinimumFreeBytes     int64    `json:"minimumFreeBytes"`
		MaximumSnapshotBytes int64    `json:"maximumSnapshotBytes"`
	}{keep, minimumFreeBytes, maximumSnapshotBytes}
	var answer AgentWorktree
	err := runAgentWorktree(ctx, client, "create", agentID, request, &answer)
	if err == nil && (!flowid.IsCommitSHA(answer.Base) || answer.Root != AgentWorktreeRoot(agentID)) {
		err = sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the worktree helper created %q at %q, not a commit at %s", answer.Base, answer.Root, AgentWorktreeRoot(agentID)))
	}
	return answer, err
}

// FinishAgentWorktree saves the worktree of agentID on top of its base into
// the result ref, counts the changes and removes the checkout. A saved result
// is reused, so a repeated finish saves nothing again.
func (operations *GitOperations) FinishAgentWorktree(ctx context.Context, client *sandboxd.SandboxClient, agentID string) (AgentResult, error) {
	var answer AgentResult
	err := runAgentWorktree(ctx, client, "finish", agentID, struct{}{}, &answer)
	if err == nil && !answer.Missing && (!flowid.IsCommitSHA(answer.Base) || !flowid.IsCommitSHA(answer.Result)) {
		err = sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the worktree helper finished with %q on %q, which are not commits", answer.Result, answer.Base))
	}
	return answer, err
}

// MergeAgentChanges merges the result of agentID into the workspace, file by
// file and three ways, or discards it. The base is the snapshot, ours the
// workspace now and theirs the result: a file only one side changed takes that
// side, a text file both changed is merged with diff3 conflict markers, and a
// binary file, a deletion against a change and a file both added keep the
// workspace's version and are reported. The merge writes its plan first, then
// only the files that still hold their planned pre-image, then its receipt, so
// a retry writes each file once and answers the receipt. Each request has its
// own plan and receipt, so a merge of other selected paths merges them; after
// a discard or a merge of every file, every call answers that receipt. A
// worktree that the subagent's end could not save
// is saved first, and a subagent whose worktree was lost before it was saved
// has nothing to merge. The caller merges only a subagent that ended.
func (operations *GitOperations) MergeAgentChanges(ctx context.Context, client *sandboxd.SandboxClient, agentID string, request MergeRequest) (MergeReceipt, error) {
	action := "apply"
	if request.Discard {
		action = "discard"
	}
	input := struct {
		Action string   `json:"action"`
		Paths  []string `json:"paths,omitempty"`
	}{action, request.Paths}
	var receipt MergeReceipt
	err := runAgentWorktree(ctx, client, "merge", agentID, input, &receipt)
	if err == nil && receipt.Outcome == (MergeOutcome{}) {
		err = sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, errors.New("the worktree helper answered a merge without its outcome"))
	}
	return receipt, err
}

// runAgentWorktree runs one helper operation for agentID with request on its
// stdin and decodes its answer: a refusal is the tool failure of its code, a
// helper error is transient, and an answer outside the contract is a platform
// fault.
func runAgentWorktree(ctx context.Context, client *sandboxd.SandboxClient, operation, agentID string, request, answer any) error {
	if !flowid.IsAgentID(agentID) {
		panic("a subagent worktree needs an agent ID that flowid.AgentTaskID derived, not " + agentID)
	}
	output, err := client.RunHelperJSON(ctx, []string{"python3", "-c", agentWorktreeProgram, operation, sandboxd.WorkspaceRoot, agentID},
		comm.Must(json.Marshal(request)), maximumWorktreeAnswerBytes)
	if err != nil {
		return err
	}
	var refusal worktreeRefusal
	if json.Unmarshal(output, &refusal) == nil && refusal.Refusal != "" {
		code, isKnown := refusals[refusal.Refusal]
		if !isKnown {
			return sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the worktree helper refused %s with the unknown reason %q", operation, refusal.Refusal))
		}
		return sandboxd.NewToolFailure(code, fmt.Errorf("subagent %s: %s", agentID, refusal.Detail))
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(answer); err != nil {
		return sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the worktree helper answered %s with an unreadable answer: %w", operation, err))
	}
	return nil
}
