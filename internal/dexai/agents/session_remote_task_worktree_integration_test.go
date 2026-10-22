// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
)

type remoteTaskTree struct {
	Name         string `json:"name"`
	WorktreePath string `json:"worktree_path"`
	BaseCommit   string `json:"base_commit"`
	Status       string `json:"status"`
	Attempt      uint64 `json:"attempt"`
}

type remoteTaskMerge struct {
	Outcome   string   `json:"outcome"`
	Applied   []string `json:"applied"`
	Conflicts []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"conflicts"`
	Unchanged []string `json:"unchanged"`
	Unknown   []string `json:"unknown"`
}

func taskTreeOpen(ctx context.Context, t *testing.T, driver *remoteConnectorDriver, name string) (remoteTaskTree, bridgeapi.Report) {
	t.Helper()
	result, report := driver.call(ctx, conversation.ToolOpenTaskWorktree, map[string]any{"name": name, "reason": "Change one bounded piece of the application"})
	if result.IsError {
		t.Fatalf("open %s: %s", name, result.Content)
	}
	var tree remoteTaskTree
	if err := json.Unmarshal([]byte(result.Content), &tree); err != nil {
		t.Fatal(err)
	}
	if tree.Name != name || !strings.HasPrefix(tree.WorktreePath, "/workspace/.git/superverse/worktrees/") || len(tree.BaseCommit) != 40 || tree.Status != "OPEN" {
		t.Fatalf("bad task worktree %s: %+v", name, tree)
	}
	return tree, report
}

func taskTreeMerge(ctx context.Context, t *testing.T, driver *remoteConnectorDriver, name string, discard bool, paths ...string) (remoteTaskMerge, bridgeapi.Report) {
	t.Helper()
	args := map[string]any{"name": name, "discard": discard}
	if len(paths) > 0 {
		args["paths"] = paths
	}
	result, report := driver.call(ctx, conversation.ToolMergeTaskWorktree, args)
	if result.IsError {
		t.Fatalf("merge %s: %s", name, result.Content)
	}
	var receipt remoteTaskMerge
	if err := json.Unmarshal([]byte(result.Content), &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt, report
}

// TestSessionRemoteTaskWorktreeHTTPDexIntegration drives Codex-only calls
// through real remote HTTP, Dex, Sandbox, Git and Session handlers.
func TestSessionRemoteTaskWorktreeHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	driver, _ := newRemoteConnectorDriver(ctx, t)
	driver.success(ctx, conversation.ToolWrite, map[string]any{"path": "rasubwt-shared.txt", "content": "base\n"}, "")
	first, firstReport := taskTreeOpen(ctx, t, &driver, "first")
	var synced bridgeapi.SyncResponse
	driver.remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: driver.epoch, After: driver.remote.after, StateDigest: driver.remote.digest}, &synced, http.StatusOK)
	if synced.LastSequence < synced.ExaminedThrough {
		t.Fatal("task worktree sync cursor exceeded the retained result sequence")
	}
	second, _ := taskTreeOpen(ctx, t, &driver, "second")
	var duplicate bridgeapi.ReportResponse
	driver.remote.post(ctx, bridgeapi.PathReport, firstReport, &duplicate, http.StatusOK)
	if !duplicate.Duplicate {
		t.Fatal("duplicate open report was admitted twice")
	}
	repeated, _ := taskTreeOpen(ctx, t, &driver, "first")
	if repeated.WorktreePath != first.WorktreePath || repeated.BaseCommit != first.BaseCommit {
		t.Fatalf("repeated open created another worktree: %+v, %+v", first, repeated)
	}
	driver.success(ctx, conversation.ToolWrite, map[string]any{"path": first.WorktreePath + "/rasubwt-first.txt", "content": "first\n"}, "")
	driver.success(ctx, conversation.ToolWrite, map[string]any{"path": second.WorktreePath + "/rasubwt-second.txt", "content": "second\n"}, "")
	driver.success(ctx, conversation.ToolEdit, map[string]any{"path": first.WorktreePath + "/rasubwt-shared.txt", "old_string": "base\n", "new_string": "first\n"}, "")
	driver.success(ctx, conversation.ToolEdit, map[string]any{"path": second.WorktreePath + "/rasubwt-shared.txt", "old_string": "base\n", "new_string": "second\n"}, "")
	driver.success(ctx, conversation.ToolRead, map[string]any{"path": first.WorktreePath + "/rasubwt-first.txt"}, "first")
	driver.success(ctx, conversation.ToolGrep, map[string]any{"path": second.WorktreePath, "pattern": "second", "glob": "rasubwt-*.txt"}, "rasubwt-second.txt")
	driver.success(ctx, conversation.ToolGlob, map[string]any{"path": first.WorktreePath, "pattern": "rasubwt-first.txt"}, "rasubwt-first.txt")
	driver.success(ctx, conversation.ToolBash, map[string]any{"command": "cd '" + first.WorktreePath + "' && pwd", "description": "Check the first task checkout"}, first.WorktreePath)
	for _, path := range []string{first.WorktreePath + "/.git", "/workspace/.git/superverse/worktrees/unknown/rasubwt-shared.txt", "/etc/passwd"} {
		result, _ := driver.call(ctx, conversation.ToolRead, map[string]any{"path": path})
		if !result.IsError || !strings.Contains(result.Content, "path_outside_roots") {
			t.Fatalf("outside path %s was not refused: %s", path, result.Content)
		}
	}
	partial, _ := taskTreeMerge(ctx, t, &driver, "first", false, "rasubwt-first.txt", "unknown.txt")
	if partial.Outcome != "merged" || len(partial.Applied) != 1 || partial.Applied[0] != "rasubwt-first.txt" || len(partial.Unknown) != 1 || partial.Unknown[0] != "unknown.txt" {
		t.Fatalf("wrong partial receipt: %+v", partial)
	}
	completed, _ := taskTreeMerge(ctx, t, &driver, "first", false)
	if completed.Outcome != "merged" || len(completed.Applied) != 1 || completed.Applied[0] != "rasubwt-shared.txt" {
		t.Fatalf("wrong first full receipt: %+v", completed)
	}
	driver.success(ctx, conversation.ToolRead, map[string]any{"path": "rasubwt-first.txt"}, "first")
	conflicted, conflictReport := taskTreeMerge(ctx, t, &driver, "second", false)
	if conflicted.Outcome != "merged_with_conflicts" || len(conflicted.Conflicts) != 1 || conflicted.Conflicts[0].Path != "rasubwt-shared.txt" || conflicted.Conflicts[0].Kind != "content" || len(conflicted.Applied) != 1 || conflicted.Applied[0] != "rasubwt-second.txt" {
		t.Fatalf("wrong conflict receipt: %+v", conflicted)
	}
	driver.success(ctx, conversation.ToolRead, map[string]any{"path": "rasubwt-shared.txt"}, "<<<<<<<")
	driver.success(ctx, conversation.ToolRead, map[string]any{"path": "rasubwt-second.txt"}, "second")
	driver.remote.post(ctx, bridgeapi.PathReport, conflictReport, &duplicate, http.StatusOK)
	if !duplicate.Duplicate {
		t.Fatal("duplicate merge report was admitted twice")
	}
	again, _ := taskTreeMerge(ctx, t, &driver, "second", false)
	if again.Outcome != conflicted.Outcome || len(again.Conflicts) != len(conflicted.Conflicts) {
		t.Fatalf("repeated merge changed receipt: %+v", again)
	}
	driver.success(ctx, conversation.ToolBash, map[string]any{"command": "mv /workspace/.git/HEAD /workspace/.git/HEAD.rasubwt", "description": "Break task checkout creation at real Git"}, "")
	headBroken := true
	defer func() {
		if headBroken {
			driver.success(ctx, conversation.ToolBash, map[string]any{"command": "mv /workspace/.git/HEAD.rasubwt /workspace/.git/HEAD", "description": "Restore task checkout Git HEAD"}, "")
		}
	}()
	failedOpen, _ := driver.call(ctx, conversation.ToolOpenTaskWorktree, map[string]any{"name": "failed-open", "reason": "Exercise a real Git creation failure"})
	driver.success(ctx, conversation.ToolBash, map[string]any{"command": "mv /workspace/.git/HEAD.rasubwt /workspace/.git/HEAD", "description": "Restore task checkout Git HEAD"}, "")
	headBroken = false
	if !failedOpen.IsError || !strings.Contains(failedOpen.Content, "tool_unavailable") || !strings.Contains(failedOpen.Content, "git rev-parse") {
		t.Fatalf("real Git creation failure was not a non-refusal: %s", failedOpen.Content)
	}
	ordinaryMerge, _ := driver.call(ctx, conversation.ToolMergeTaskWorktree, map[string]any{"name": "failed-open"})
	if !ordinaryMerge.IsError || !strings.Contains(ordinaryMerge.Content, "the task worktree was not created") {
		t.Fatalf("uncreated task worktree merged without discard: %s", ordinaryMerge.Content)
	}
	failedDiscard, _ := taskTreeMerge(ctx, t, &driver, "failed-open", true)
	if failedDiscard.Outcome != "discarded" || len(failedDiscard.Applied) != 0 {
		t.Fatalf("uncreated task worktree was not discarded: %+v", failedDiscard)
	}
	taskTreeOpen(ctx, t, &driver, "failed-open")
	taskTreeMerge(ctx, t, &driver, "failed-open", true)
	third, _ := taskTreeOpen(ctx, t, &driver, "third")
	driver.success(ctx, conversation.ToolWrite, map[string]any{"path": third.WorktreePath + "/rasubwt-discard.txt", "content": "discard\n"}, "")
	discarded, _ := taskTreeMerge(ctx, t, &driver, "third", true)
	if discarded.Outcome != "discarded" || len(discarded.Applied) != 0 {
		t.Fatalf("wrong discard receipt: %+v", discarded)
	}
	missing, _ := driver.call(ctx, conversation.ToolRead, map[string]any{"path": "rasubwt-discard.txt"})
	if !missing.IsError {
		t.Fatal("discarded file reached the workspace")
	}
	reopened, _ := taskTreeOpen(ctx, t, &driver, "third")
	if reopened.WorktreePath == third.WorktreePath || reopened.Attempt != third.Attempt+1 {
		t.Fatalf("closed name did not start a new attempt: %+v, %+v", third, reopened)
	}
	oldID := strings.TrimPrefix(third.WorktreePath, "/workspace/.git/superverse/worktrees/")
	check := fmt.Sprintf("if git show-ref --quiet --verify refs/superverse/agents/%s/base; then echo kept; else echo cleaned; fi", oldID)
	driver.success(ctx, conversation.ToolBash, map[string]any{"command": check, "description": "Check old task refs were cleaned"}, "cleaned")
	taskTreeOpen(ctx, t, &driver, "fourth")
	taskTreeOpen(ctx, t, &driver, "fifth")
	limit, _ := driver.call(ctx, conversation.ToolOpenTaskWorktree, map[string]any{"name": "sixth", "reason": "Check the three checkout limit"})
	if !limit.IsError || !strings.Contains(limit.Content, "task_worktree_limit_reached") {
		t.Fatalf("fourth open task worktree was admitted: %s", limit.Content)
	}
}

// TestSessionRemoteTaskWorktreePlatformKeepHTTPDexIntegration proves that a
// later platform subagent's real Git checkout does not prune Codex's OPEN one.
func TestSessionRemoteTaskWorktreePlatformKeepHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{
		CodexVersion: "integration", ProviderThreadID: "task-keep", HostLabel: "integration host",
	}}, &active, http.StatusOK)
	driver := remoteConnectorDriver{remote: remote, epoch: active.Epoch, generation: active.Generation}
	tree, _ := taskTreeOpen(ctx, t, &driver, "codex-task")
	driver.success(ctx, conversation.ToolWrite, map[string]any{"path": tree.WorktreePath + "/rasubwt-kept.txt", "content": "keep me\n"}, "")
	driver.sequence++
	var accepted bridgeapi.ReportResponse
	driver.remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: driver.epoch, Generation: driver.generation, Sequence: driver.sequence, TurnCompleted: true}, &accepted, http.StatusOK)
	session.await(ctx, "remote task turn completed", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	before, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: session.identity.ProjectID, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	input := mockInput(t, "remote-task-worktree-platform-keep", 1).Content + " " + mockmodel.ArgumentMarkerForTest("head", before.Session.Identity.HeadCommit)
	session.send(ctx, input)
	session.await(ctx, "platform subagent running", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && len(snapshot.Agent.Description.Subagents) == 1 && snapshot.Agent.Description.Subagents[0].Status == agenttasks.SubagentRunning
	})
	kept := remoteSandboxOperator(ctx, t, session.workspace(ctx).Resources, "if test -f '"+tree.WorktreePath+"/rasubwt-kept.txt'; then cat '"+tree.WorktreePath+"/rasubwt-kept.txt'; fi")
	if kept != "keep me" {
		t.Fatalf("platform subagent creation pruned the open Codex checkout: %q", kept)
	}
	session.await(ctx, "platform subagent completed", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && len(snapshot.Agent.Description.Subagents) == 1 && snapshot.Agent.Description.Subagents[0].Status == agenttasks.SubagentCompleted
	})
}
