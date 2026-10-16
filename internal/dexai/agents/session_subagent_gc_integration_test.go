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
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
)

func awaitExpirySubagent(ctx context.Context, session *mockSession, seen map[string]bool) agenttasks.SubagentRecord {
	session.t.Helper()
	var next agenttasks.SubagentRecord
	session.await(ctx, "a new completed subagent and its report turn", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		if err != nil || snapshot.Agent == nil || snapshot.Agent.Description.Status != agentloop.AgentStatusWaitingForMessage {
			return false
		}
		assistant := replies(snapshot.Agent.History)
		if len(assistant) == 0 || assistant[len(assistant)-1].Message.Content != "The investigation reported its result." {
			return false
		}
		for _, record := range snapshot.Agent.Description.Subagents {
			if !seen[record.AgentID] && record.Status == agenttasks.SubagentCompleted && record.Changes != nil && record.Changes.Files == 1 {
				next = record
				return true
			}
		}
		return false
	})
	seen[next.AgentID] = true
	return next
}

func runExpiryCommand(ctx context.Context, session *mockSession, objects sandboxObjects, command, expected string) {
	session.t.Helper()
	output := objects.runInstance(ctx, session.workspace(ctx).Resources, "sh", "-c", command)
	if !strings.Contains(output, expected) {
		session.t.Fatalf("workspace command did not report %q: %s", expected, output)
	}
}

func pendingExpiredSubagent(ctx context.Context, session *mockSession) string {
	session.t.Helper()
	state, err := session.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(session.id)})
	if err != nil {
		session.t.Fatalf("read Session engine state for pending cleanup failed (%T)", err)
	}
	for _, attribute := range state.GetAttributes() {
		if attribute.GetKey() == "PendingSubagentWorktreeCleanup" {
			return attribute.GetValue().GetStringValue()
		}
	}
	return ""
}

// The real Worker and Sandbox run fifty-three platform subagents in one
// long-lived Session. A Git ref lock makes cleanup fail, then removing it lets
// the next start retry the same pending ID before creating another worktree.
func TestSessionExpiredSubagentWorktreeCleanupDexIntegration(t *testing.T) {
	objects := newSandboxObjects(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	started := time.Now()
	session := newMockSession(t, ctx, mockInput(t, "subagent-expiry", 1).Content)
	seen := map[string]bool{}
	first := awaitExpirySubagent(ctx, session, seen)
	t.Logf("first mock-model subagent round: %s", time.Since(started).Round(time.Second))
	ids := []string{first.AgentID}
	for round := range 50 {
		session.send(ctx, mockInput(t, "subagent-expiry", 1).Content)
		record := awaitExpirySubagent(ctx, session, seen)
		ids = append(ids, record.AgentID)
		if (round+1)%10 == 0 {
			t.Logf("%d finished subagents: %s", len(ids), time.Since(started).Round(time.Second))
		}
	}
	t.Logf("fifty-one finished subagents: %s", time.Since(started).Round(time.Second))
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || snapshot.Agent == nil || len(snapshot.Agent.Description.Subagents) != 50 {
		t.Fatalf("expected fifty retained subagents after expiry, got %d (%T)", len(snapshot.Agent.Description.Subagents), err)
	}
	if pending := pendingExpiredSubagent(ctx, session); pending != "" {
		t.Fatalf("cleanup of the first expired worktree remains pending: %s", pending)
	}
	firstID, secondID := ids[0], ids[1]
	if !flowid.IsAgentID(firstID) || !flowid.IsAgentID(secondID) {
		t.Fatalf("subagent IDs are not Git worktree identities: %q, %q", firstID, secondID)
	}
	firstGone := fmt.Sprintf("cd /workspace && test ! -e .git/superverse/agents/%s && test ! -e .git/superverse/worktrees/%s && ! git show-ref --quiet --verify refs/superverse/agents/%s/base && ! git show-ref --quiet --verify refs/superverse/agents/%s/result && echo expired-cleaned", firstID, firstID, firstID, firstID)
	runExpiryCommand(ctx, session, objects, firstGone, "expired-cleaned")
	lock := fmt.Sprintf("cd /workspace && mkdir -p .git/refs/superverse/agents/%s && : > .git/refs/superverse/agents/%s/base.lock && echo lock-installed", secondID, secondID)
	runExpiryCommand(ctx, session, objects, lock, "lock-installed")
	locked := true
	defer func() {
		if locked {
			runExpiryCommand(ctx, session, objects, fmt.Sprintf("cd /workspace && rm -f .git/refs/superverse/agents/%s/base.lock && echo lock-removed", secondID), "lock-removed")
		}
	}()
	session.send(ctx, mockInput(t, "subagent-expiry-fault", 1).Content)
	failed := session.waitForSnapshot(ctx, "failed cleanup start", func(agent agentloop.AgentSnapshot) bool {
		if agent.Description.Status != agentloop.AgentStatusWaitingForMessage {
			return false
		}
		assistant := replies(agent.History)
		return len(assistant) > 0 && assistant[len(assistant)-1].Message.Content == "The platform could not start the investigation."
	})
	if len(failed.Description.Subagents) != 49 || pendingExpiredSubagent(ctx, session) != secondID {
		t.Fatalf("failed cleanup did not retain only the pending ID: records=%d pending=%q", len(failed.Description.Subagents), pendingExpiredSubagent(ctx, session))
	}
	for _, record := range failed.Description.Subagents {
		if record.Status == agenttasks.SubagentStarting {
			t.Fatalf("cleanup failure left a STARTING record: %+v", record)
		}
	}
	runExpiryCommand(ctx, session, objects, fmt.Sprintf("cd /workspace && rm .git/refs/superverse/agents/%s/base.lock && echo lock-removed", secondID), "lock-removed")
	locked = false
	// Simulate cleanup that completed before its answer was lost, then lose the
	// real Sandbox instance while the Session still holds the pending ID.
	removed := fmt.Sprintf("cd /workspace && git update-ref -d refs/superverse/agents/%s/base && git update-ref -d refs/superverse/agents/%s/result && rm -rf .git/superverse/agents/%s .git/superverse/worktrees/%s && git worktree prune && echo checkout-absent", secondID, secondID, secondID, secondID)
	runExpiryCommand(ctx, session, objects, removed, "checkout-absent")
	lost := session.workspace(ctx)
	objects.removeInstance(ctx, lost.Resources)
	session.await(ctx, "the lost Sandbox instance", func() bool { return objects.instanceGone(ctx, lost.Resources) })
	sessionMark := markHistory(t, ctx, session.reader, string(session.id))
	session.send(ctx, mockInput(t, "subagent-expiry", 1).Content)
	retained := awaitExpirySubagent(ctx, session, seen)
	replaced := session.workspace(ctx)
	if replaced.InstanceID == lost.InstanceID || replaced.Resources.Volume.UID != lost.Resources.Volume.UID {
		t.Fatalf("pending cleanup did not resume on a new instance of the same volume: %s, then %s", lost.InstanceID, replaced.InstanceID)
	}
	steps := completedStepsSince(t, ctx, session.reader, sessionMark)
	if !slices.Contains(steps, stepType(sessionagent.RequestSessionWorkspace{})) || slices.Contains(steps, stepType(sessionagent.RecordToolFailure{})) {
		t.Fatalf("the lost workspace consumed a tool failure instead of taking the cold path: %v", steps)
	}
	if pending := pendingExpiredSubagent(ctx, session); pending != "" {
		t.Fatalf("successful retry kept pending cleanup %s", pending)
	}
	secondGone := fmt.Sprintf("cd /workspace && test ! -e .git/superverse/agents/%s && test ! -e .git/superverse/worktrees/%s && ! git show-ref --quiet --verify refs/superverse/agents/%s/base && ! git show-ref --quiet --verify refs/superverse/agents/%s/result && echo retried-cleaned", secondID, secondID, secondID, secondID)
	runExpiryCommand(ctx, session, objects, secondGone, "retried-cleaned")
	session.send(ctx, mockInput(t, "subagent-expiry-merge", 1).Content+" "+mockmodel.ArgumentMarkerForTest("task", retained.TaskID()))
	session.waitForSnapshot(ctx, "the retained subagent merge", func(agent agentloop.AgentSnapshot) bool {
		for _, reply := range replies(agent.History) {
			if reply.Message.Content == "The retained result was merged." {
				return true
			}
		}
		return false
	})
	runExpiryCommand(ctx, session, objects, "cd /workspace && test \"$(cat subgc-result.txt)\" = \"subagent result\" && echo retained-merged", "retained-merged")
	agent, messages := session.history(ctx)
	if agent.Description.Subagents[len(agent.Description.Subagents)-1].Status != agenttasks.SubagentMerged || toolResult(t, messages, conversation.ToolMergeAgentChanges, 0).Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("retained subagent %s did not merge", retained.TaskID())
	}
	t.Logf("FlowID=%s expired %s and %s, retained and merged %s; elapsed %s", session.id, firstID, secondID, retained.AgentID, time.Since(started).Round(time.Second))
}
