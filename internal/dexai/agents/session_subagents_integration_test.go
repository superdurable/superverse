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
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// These scenarios run subagents on the Session of a throwaway READY Project and
// the actual Worker, whose server runs the mock Claude model. The main agent's
// start_agent creates the subagent's worktree in the real coding container and
// starts its SubagentFlow through the real Session Service; the subagent's own
// model calls reach the mock model, which serves the script its task names;
// its tools run in its worktree, its report reaches the Session as a task
// notification, and merge_agent_changes merges in the Sandbox. Dex, the
// Sandbox and Git are real; only the model is the mock.

// subagentOf is the identity of the Session's subagent of record.
func (s *mockSession) subagentOf(record agenttasks.SubagentRecord) agenttasks.SubagentIdentity {
	return agenttasks.SubagentIdentity{ProjectID: s.identity.ProjectID, AgentID: record.AgentID}
}

// awaitSubagentEnd waits until the subagent's own execution recorded how it
// ended and what became of its report, and returns its retained read.
func (s *mockSession) awaitSubagentEnd(ctx context.Context, record agenttasks.SubagentRecord) subagent.SnapshotForTest {
	s.t.Helper()
	var state subagent.SnapshotForTest
	s.await(ctx, "the end of subagent "+record.AgentID, func() bool {
		var err error
		state, err = s.subagents.SnapshotForTest(ctx, s.subagentOf(record))
		return err == nil && state.Outcome != nil && state.Outcome.Delivery != (subagent.ReportDeliveryForTest{})
	})
	return state
}

// stepAttempts lists the final attempt of every completed Execute of stepType
// in the current run of flowID, in history order: a diagnostic read of the
// attempts a Worker replacement cut.
func (s *mockSession) stepAttempts(ctx context.Context, flowID, stepType string) []int32 {
	s.t.Helper()
	var attempts []int32
	for _, event := range runHistory(s.t, ctx, s.reader, flowID, currentRun(s.t, ctx, s.reader, flowID)) {
		if completed := event.GetStepExecuteCompleted(); completed != nil && completed.GetContext().GetStepType() == stepType {
			attempts = append(attempts, completed.GetContext().GetFinalAttempt())
		}
	}
	return attempts
}

// completedSteps lists the Step types an execution completed in its current
// run, in history order: a diagnostic read of what it ran.
func (s *mockSession) completedSteps(ctx context.Context, flowID string) []string {
	s.t.Helper()
	return completedStepsAfter(runHistory(s.t, ctx, s.reader, flowID, currentRun(s.t, ctx, s.reader, flowID)), 0)
}

// awaitAdmittedStart polls the Session every quarter second, much faster than
// it provisions an instance, until RouteTool admitted a start_agent call, and
// returns that Snapshot.
func (s *mockSession) awaitAdmittedStart(ctx context.Context) agentloop.AgentSnapshot {
	s.t.Helper()
	for {
		if snapshot, err := s.client.GetSnapshot(ctx, s.id); err == nil && snapshot.Agent != nil && len(snapshot.Agent.Description.Subagents) == 1 {
			return *snapshot.Agent
		}
		select {
		case <-ctx.Done():
			s.t.Fatalf("the start_agent call was never admitted; reconcile %s", s.id)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// onlySubagent returns the one subagent record of a Snapshot, or fails.
// mergeTurnEnded reports whether the turn that merged the Session's only
// subagent ended. The replies are counted in the full history afterwards: the
// Snapshot holds only the current window, which a long merge turn outgrows.
func mergeTurnEnded(agent agentloop.AgentSnapshot) bool {
	records := agent.Description.Subagents
	return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && len(records) == 1 && records[0].Status == agenttasks.SubagentMerged
}

func onlySubagent(t *testing.T, agent agentloop.AgentSnapshot) agenttasks.SubagentRecord {
	t.Helper()
	if records := agent.Description.Subagents; len(records) == 1 {
		return records[0]
	}
	t.Fatalf("expected one subagent record, got %+v", agent.Description.Subagents)
	return agenttasks.SubagentRecord{}
}

// subagentNotification returns the one notification of the subagent's end and
// fails unless it reports status from the subagent under its task's ID.
func subagentNotification(t *testing.T, messages []conversation.SequencedMessage, record agenttasks.SubagentRecord, status agenttasks.SubagentStatus) conversation.SequencedMessage {
	t.Helper()
	var found []conversation.SequencedMessage
	for _, message := range ofRole(messages, conversation.MessageRoleNotification) {
		if notification := message.Message.Notification; notification != nil && notification.TaskID == record.TaskID() {
			found = append(found, message)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected one notification of %s, got %d", record.TaskID(), len(found))
	}
	notification := found[0].Message.Notification
	if notification.ID != "task/"+record.TaskID()+"/"+status.String() || notification.Kind != conversation.NotificationKindTask || notification.Source != conversation.NotificationSourceSubagent ||
		notification.TaskStatus != status.String() || found[0].Message.Content != notification.Summary || !strings.Contains(notification.Summary, record.Description) {
		t.Fatalf("the notification does not report the subagent's end %s: %+v", status, found[0].Message)
	}
	return found[0]
}

// An investigate subagent: start_agent answers at once with the task ID and the
// first turn ends; the subagent greps in its own worktree and reports, and its
// task notification opens an automatic turn. The record carries the subagent
// from STARTING through RUNNING to COMPLETED; while it runs, the Session admits
// only the call its record admitted, again and unchanged. The agent ID derives
// from the Session's FlowID and the call, and the FlowID from the Project and
// the agent ID. A repeated start of the closed execution joins it, and the
// Session denies a late admission.
func TestSessionMockSubagentInvestigateDexIntegration(t *testing.T) {
	for _, model := range []conversation.Model{conversation.ModelMock, conversation.ModelMockResponses, conversation.ModelMockChat, conversation.ModelDeepSeekFlash, conversation.ModelMiniMaxM3, conversation.ModelMuseWonder10, conversation.ModelGPT6Astra, conversation.ModelGrok47, conversation.ModelGemini4Argon} {
		t.Run(string(model), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
			defer cancel()
			session := newSelectableSession(t, ctx, model, mockInput(t, "subagent-investigate", 1).Content)
			running := onlySubagent(t, session.waitForSnapshot(ctx, "the subagent running", func(agent agentloop.AgentSnapshot) bool {
				records := agent.Description.Subagents
				return len(records) == 1 && records[0].Status == agenttasks.SubagentRunning
			}))
			other := agenttasks.SubagentAdmission{AgentID: running.AgentID, Fingerprint: "another call", BaseCommit: running.BaseCommit}
			if answer, err := session.client.AdmitSubagent(ctx, session.id, other); err != nil || answer.Admitted || answer.Stopped {
				t.Fatalf("the Session admitted another call for its running subagent (%+v, %T)", answer, err)
			}
			same := agenttasks.SubagentAdmission{AgentID: running.AgentID, Fingerprint: running.Fingerprint, BaseCommit: running.BaseCommit}
			if answer, err := session.client.AdmitSubagent(ctx, session.id, same); err != nil || !answer.Admitted {
				t.Fatalf("the Session did not admit its running subagent's own call again (%+v, %T)", answer, err)
			}
			snapshot, err := session.client.GetSnapshot(ctx, session.id)
			if err != nil || snapshot.Agent == nil {
				t.Fatalf("the Session read after the admissions failed (%T)", err)
			}
			if again := onlySubagent(t, *snapshot.Agent); again.Status != agenttasks.SubagentRunning || !again.AdmittedAt.Equal(running.AdmittedAt) || again.Fingerprint != running.Fingerprint {
				t.Fatalf("an admission changed the running record: %+v, then %+v", running, again)
			}
			session.waitForSnapshot(ctx, "the automatic turn after the subagent's report", func(agent agentloop.AgentSnapshot) bool {
				return len(replies(agent.History)) >= 3 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
			})
			agent, messages := session.history(ctx)
			requireNoFailure(t, conversation.HistoryPage{Messages: messages})
			assistant := ofRole(messages, conversation.MessageRoleAssistant)
			if len(assistant) != 3 || !strings.Contains(assistant[2].Message.Content, "web/src/App.tsx defines the page heading") {
				t.Fatalf("expected the 3 mock replies ending with the relayed report; a stopped scenario answers with one text: %+v", assistant)
			}
			record := onlySubagent(t, agent)
			if record.Kind != agenttasks.SubagentInvestigate || record.Status != agenttasks.SubagentCompleted || record.Description != "Find the page heading" ||
				!flowid.IsCommitSHA(record.BaseCommit) || record.AdmittedAt.IsZero() || record.FinishedAt.IsZero() || record.Changes != nil {
				t.Fatalf("the record of the investigation is wrong: %+v", record)
			}
			identity := session.subagentOf(record)
			if record.AgentID != flowid.AgentTaskID(string(session.id), string(record.CallID)) ||
				identity.FlowID() != "subagent-"+session.identity.ProjectID+"-"+record.AgentID {
				t.Fatalf("the subagent's identity does not derive from the Session and the call: %+v", record)
			}
			if notification := subagentNotification(t, messages, record, agenttasks.SubagentCompleted); notification.Sequence < assistant[1].Sequence || notification.Sequence > assistant[2].Sequence {
				t.Fatal("the notification is not between the first turn's end and the automatic turn")
			}
			state := session.awaitSubagentEnd(ctx, record)
			if state.Status != subagent.StatusCompleted || state.Outcome.Delivery != subagent.ReportDeliveredForTest || !strings.Contains(state.Outcome.Report, "line 51") ||
				state.Config == nil || state.Config.Model != model || state.Config.Kind != agenttasks.SubagentInvestigate {
				t.Fatalf("the subagent's own state is wrong: %+v, %+v, %+v", state, state.Config, state.Outcome)
			}
			before := session.execution(ctx, identity.FlowID())
			start := agenttasks.SubagentStart{Identity: identity, Call: agenttasks.SubagentCall{Kind: record.Kind, Description: record.Description, Prompt: "A repeated start."},
				Fingerprint: record.Fingerprint, BaseCommit: record.BaseCommit, Model: model}
			if err := session.subagents.Start(ctx, start); err != nil {
				t.Fatalf("a repeated start of the closed subagent did not join it (%T)", err)
			}
			if after := session.execution(ctx, identity.FlowID()); after.GetFlowExecutionId().GetRunId() != before.GetFlowExecutionId().GetRunId() ||
				after.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_COMPLETED {
				t.Fatalf("a repeated start ran the subagent again: %v, then %v", before.GetFlowExecutionId(), after.GetFlowExecutionId())
			}
			for _, fingerprint := range []string{record.Fingerprint, "another call"} {
				admission := agenttasks.SubagentAdmission{AgentID: record.AgentID, Fingerprint: fingerprint, BaseCommit: record.BaseCommit}
				if answer, err := session.client.AdmitSubagent(ctx, session.id, admission); err != nil || answer.Admitted || answer.Stopped {
					t.Fatalf("the Session admitted a finished subagent again (%+v, %s)", answer, dexflowtest.ErrorCategory(err))
				}
			}
			t.Logf("FlowID=%s started subagent %s, which reported and completed", session.id, identity.FlowID())

			child := *session
			child.id = agentloop.FlowID(identity.FlowID())
			for _, item := range compatibleCanonicalEntries(ctx, t, &child) {
				for _, block := range item.Entry.Blocks {
					if conversation.IsOpaqueReplay(block) {
						t.Fatal("completed subagent retained opaque replay")
					}
				}
			}

		})
	}
}

// A change subagent edits the page heading in its own worktree while the
// Worker is replaced: its check, a command that counts its runs and sleeps,
// runs once on the ledger although the replacement cut the attempt that began
// it, and its worktree is saved once. Its notification opens the turn
// that first merges a path the subagent did not change, which merges nothing,
// lists the path as unknown and leaves the record open, then merges every
// file into the workspace, where the read sees the new heading, and that merge
// closes the record with its receipt.
func TestSessionMockSubagentChangeMergeDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "subagent-change-merge", 1).Content)
	running := session.waitForSnapshot(ctx, "the change subagent running", func(agent agentloop.AgentSnapshot) bool {
		records := agent.Description.Subagents
		return len(records) == 1 && records[0].Status == agenttasks.SubagentRunning
	})
	identity := session.subagentOf(onlySubagent(t, running))
	session.await(ctx, "the subagent's check", func() bool {
		state, err := session.subagents.SnapshotForTest(ctx, identity)
		return err == nil && state.LastSequence >= 5
	})
	// Sequence 5 is the second model reply, which asks for the 30-second check.
	// The Worker is replaced while that check runs.
	time.Sleep(3 * time.Second)
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, err := session.subagents.SnapshotForTest(ctx, identity)
		return err
	})
	session.waitForSnapshot(ctx, "the merge turn and its continuations", mergeTurnEnded)
	agent, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 9 || assistant[8].Message.Content != "Nothing else changed; the merged heading waits to be committed." {
		t.Fatalf("expected the 9 mock replies of the merges; a stopped scenario answers with one text: %+v", assistant)
	}
	record := onlySubagent(t, agent)
	if record.Status != agenttasks.SubagentMerged || record.Merge == nil || record.Merge.Outcome != git.MergeMerged || record.Merge.Applied != 1 || record.Merge.Conflicts != 0 ||
		record.Changes == nil || record.Changes.Files != 1 || record.Changes.Insertions != 1 || record.Changes.Deletions != 1 || !flowid.IsCommitSHA(record.Changes.ResultCommit) {
		t.Fatalf("the merged record is wrong: %+v, %+v, %+v", record, record.Merge, record.Changes)
	}
	subagentNotification(t, messages, record, agenttasks.SubagentCompleted)
	// The mock model required the first merge to merge nothing and name the
	// path unknown, and the second, which a closed record would have answered
	// with the first receipt, to merge the change.
	for index := range 2 {
		if merge := toolResult(t, messages, conversation.ToolMergeAgentChanges, index); merge.Outcome != conversation.ToolOutcomeSucceeded || merge.ResultCode != "" {
			t.Fatalf("merge %d did not succeed: %+v", index+1, merge)
		}
	}
	state := session.awaitSubagentEnd(ctx, record)
	if state.Status != subagent.StatusCompleted || state.Outcome.Changes == nil || state.Outcome.Changes.ResultCommit != record.Changes.ResultCommit || state.Outcome.Failure != "" || state.Outcome.Unsaved != "" {
		t.Fatalf("the subagent's own outcome is wrong: %+v", state.Outcome)
	}
	if summary := session.execution(ctx, identity.FlowID()); summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_COMPLETED {
		t.Fatalf("the subagent's execution is %s after the Worker replacement", summary.GetFlowStatus())
	}
	// The mock model required the check's output to be its first run; the
	// history shows that the replacement cut the attempt that began it and that
	// the worktree was saved once.
	if attempts := session.stepAttempts(ctx, identity.FlowID(), dex.GetFinalStepType(subagent.ExecuteSubagentTool{})); len(attempts) != 2 || attempts[1] < 2 {
		t.Fatalf("the Worker replacement did not cut the check's attempt, so the run was not exercised; rerun (final attempts %v)", attempts)
	}
	if finishes := count(session.completedSteps(ctx, identity.FlowID()), dex.GetFinalStepType(subagent.FinishSubagent{})); finishes != 1 {
		t.Fatalf("the worktree was saved by %d finishes", finishes)
	}
	t.Logf("FlowID=%s merged %s of subagent %s across a Worker replacement", session.id, record.Changes.ResultCommit, identity.FlowID())
}

// A change subagent's instance is removed while its check runs, as a lost node
// does, with the operator's tools of the local lane (sandboxObjects): the
// check's outcome is unknown and the subagent reports. Its finish finds its
// lease unreachable, takes from the Session the lease of the instance the
// Session provisions next on the same volume and saves the worktree there, so
// the merge applies the change. The subagent's history shows that the finish,
// not its loop, refreshed the lease.
func TestSessionMockSubagentLostInstanceDexIntegration(t *testing.T) {
	objects := newSandboxObjects(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "subagent-lost-instance", 1).Content)
	running := session.waitForSnapshot(ctx, "the change subagent running", func(agent agentloop.AgentSnapshot) bool {
		records := agent.Description.Subagents
		return len(records) == 1 && records[0].Status == agenttasks.SubagentRunning
	})
	identity := session.subagentOf(onlySubagent(t, running))
	session.await(ctx, "the subagent's check", func() bool {
		state, err := session.subagents.SnapshotForTest(ctx, identity)
		return err == nil && state.LastSequence >= 5
	})
	// Sequence 5 is the second model reply, which asks for the two-minute check.
	// The instance goes away while that check runs.
	time.Sleep(5 * time.Second)
	lost := session.workspace(ctx)
	objects.removeInstance(ctx, lost.Resources)
	session.await(ctx, "the removal of the instance", func() bool { return objects.instanceGone(ctx, lost.Resources) })
	session.waitForSnapshot(ctx, "the merge turn and its continuations", func(agent agentloop.AgentSnapshot) bool {
		return len(replies(agent.History)) >= 8 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	agent, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 8 || assistant[7].Message.Content != "Nothing else changed; the merged heading waits to be committed." {
		t.Fatalf("expected the 8 mock replies of the merge; a stopped scenario answers with one text: %+v", assistant)
	}
	record := onlySubagent(t, agent)
	if record.Status != agenttasks.SubagentMerged || record.Merge == nil || record.Merge.Outcome != git.MergeMerged || record.Merge.Applied != 1 ||
		record.ChangesUnsaved || record.Changes == nil || record.Changes.Files != 1 {
		t.Fatalf("the merged record is wrong: %+v, %+v, %+v", record, record.Merge, record.Changes)
	}
	state := session.awaitSubagentEnd(ctx, record)
	if state.Status != subagent.StatusCompleted || state.Outcome.Unsaved != "" || state.Outcome.Changes == nil || state.Outcome.Changes.ResultCommit != record.Changes.ResultCommit {
		t.Fatalf("the subagent did not save its change on the next instance: %+v", state.Outcome)
	}
	steps := session.completedSteps(ctx, identity.FlowID())
	if slices.Contains(steps, dex.GetFinalStepType(subagent.RefreshSubagentWorkspace{})) || !slices.Contains(steps, dex.GetFinalStepType(subagent.RefreshFinishingWorkspace{})) {
		t.Fatalf("the instance went away before the check began, so the loop refreshed the lease instead of the finish; rerun: %v", steps)
	}
	if replaced := session.workspace(ctx); replaced.InstanceID == lost.InstanceID || replaced.Resources.Volume.UID != lost.Resources.Volume.UID {
		t.Fatalf("the change was not saved on a new instance of the same volume: %s, then %s", lost.InstanceID, replaced.InstanceID)
	}
	t.Logf("FlowID=%s lost %s while subagent %s checked its change, which it saved on the next instance", session.id, lost.InstanceID, identity.FlowID())
}

// A start_agent call that RouteTool admitted waits on the Session's cold path,
// because its instance was removed behind the Session's back, and a steered
// message supersedes it before its subagent starts: the call is answered
// superseded_by_steered_user_message, its STARTING record goes away instead of
// holding a running place and the Sandbox, and no SubagentFlow exists for it.
func TestSessionMockSubagentSteeredStartDexIntegration(t *testing.T) {
	objects := newSandboxObjects(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "subagent-steered-start", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	lost := session.workspace(ctx)
	objects.removeInstance(ctx, lost.Resources)
	session.await(ctx, "the removal of the instance", func() bool { return objects.instanceGone(ctx, lost.Resources) })
	session.send(ctx, mockInput(t, "subagent-steered-start", 2).Content)
	record := onlySubagent(t, session.awaitAdmittedStart(ctx))
	if record.Status != agenttasks.SubagentStarting {
		t.Fatalf("the admitted start did not wait for the next instance: %+v", record)
	}
	steer := conversation.PendingUserMessage{MessageID: conversation.MessageID("steer-" + record.AgentID),
		Value: conversation.UserMessage{Content: mockInput(t, "subagent-steered-start", 3).Content}}
	if err := session.client.SendPendingMessage(ctx, session.id, steer); err != nil {
		t.Fatalf("queuing the steering message failed (%T)", err)
	}
	if err := session.client.SteerMessage(ctx, session.id, conversation.SteerMessageRequest{MessageID: steer.MessageID}); err != nil {
		t.Fatalf("steering failed (%T)", err)
	}
	session.waitForSnapshot(ctx, "the steered turn", func(agent agentloop.AgentSnapshot) bool {
		return len(replies(agent.History)) >= 4 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	agent, messages := session.history(ctx)
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 4 || assistant[3].Message.Content != "The page heading reads Text Summary; I started no investigation." {
		t.Fatalf("expected the 4 mock replies with the steered one; a stopped scenario answers with one text: %+v", assistant)
	}
	if start := toolResult(t, messages, conversation.ToolStartAgent, 0); start.ResultCode != toolfailure.SupersededBySteeredUserMessage.String() {
		t.Fatalf("steering did not supersede the start: %+v", start)
	}
	if len(agent.Description.Subagents) != 0 {
		t.Fatalf("the superseded start left a record: %+v", agent.Description.Subagents)
	}
	var missing *dex.FlowNotFoundError
	if _, err := session.subagents.SnapshotForTest(ctx, session.subagentOf(record)); !errors.As(err, &missing) {
		t.Fatalf("a subagent started for the superseded call (%T)", err)
	}
	t.Logf("FlowID=%s: steering superseded the start of %s while it waited for the next instance", session.id, record.TaskID())
}

// A start_agent call waits on the Session's cold path, because its instance was
// removed, and the user's Stop overtakes it: the call is answered
// interrupted_by_user and its record ends stopped before the start. The test
// then starts a subagent for that record, as an ExecuteTool that started one
// while the Stop marked the record would: its admission is denied as stopped,
// and it ends STOPPED without an effect and reports, so a model that read the
// start as started learns that it stopped.
func TestSessionMockSubagentStoppedStartDexIntegration(t *testing.T) {
	objects := newSandboxObjects(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "subagent-stopped-start", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	lost := session.workspace(ctx)
	objects.removeInstance(ctx, lost.Resources)
	session.await(ctx, "the removal of the instance", func() bool { return objects.instanceGone(ctx, lost.Resources) })
	session.send(ctx, mockInput(t, "subagent-stopped-start", 2).Content)
	starting := session.awaitAdmittedStart(ctx)
	if record := onlySubagent(t, starting); record.Status != agenttasks.SubagentStarting {
		t.Fatalf("the admitted start did not wait for the next instance: %+v", record)
	}
	if err := session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Turn: starting.Description.Turn}); err != nil {
		t.Fatalf("the Stop of the waiting start was refused (%T)", err)
	}
	stopped := session.waitForSnapshot(ctx, "the stopped turn", func(agent agentloop.AgentSnapshot) bool {
		records := agent.Description.Subagents
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && len(records) == 1 && records[0].Status == agenttasks.SubagentStopped
	})
	record := onlySubagent(t, stopped)
	if !record.AdmittedAt.IsZero() || toolResult(t, stopped.History.Messages, conversation.ToolStartAgent, 0).ResultCode != toolfailure.InterruptedByUser.String() {
		t.Fatalf("the Stop did not stop the start before it started: %+v", record)
	}
	start := agenttasks.SubagentStart{Identity: session.subagentOf(record),
		Call:        agenttasks.SubagentCall{Kind: record.Kind, Description: record.Description, Prompt: "A start that raced with the Stop."},
		Fingerprint: record.Fingerprint, BaseCommit: record.BaseCommit, Model: conversation.ModelMock}
	if err := session.subagents.Start(ctx, start); err != nil {
		t.Fatalf("starting the subagent of the stopped record failed (%T)", err)
	}
	state := session.awaitSubagentEnd(ctx, record)
	if state.Status != subagent.StatusStopped || state.Config != nil || state.Outcome.Delivery != subagent.ReportDeliveredForTest ||
		state.Outcome.Report != "The subagent was stopped before it started, so it changed nothing." {
		t.Fatalf("the subagent of the stopped record did not report its stop without an effect: %+v, %+v", state, state.Outcome)
	}
	session.waitForSnapshot(ctx, "the recorded stop", func(agent agentloop.AgentSnapshot) bool {
		return len(ofRole(agent.History.Messages, conversation.MessageRoleNotification)) == 1
	})
	_, messages := session.history(ctx)
	subagentNotification(t, messages, record, agenttasks.SubagentStopped)
	t.Logf("FlowID=%s: the Stop overtook the start of %s, whose subagent reported the stop", session.id, record.TaskID())
}

// A change subagent and the main agent change the same line: the merge
// reports a content conflict, applies nothing to that file and leaves the
// diff3 markers, which the main agent resolves to the subagent's side. The
// record keeps the receipt's counts.
func TestSessionMockSubagentConflictDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "subagent-conflict", 1).Content)
	session.waitForSnapshot(ctx, "the merge turn and its continuations", mergeTurnEnded)
	agent, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 12 || assistant[11].Message.Content != "Nothing else changed; the resolved heading waits to be committed." {
		t.Fatalf("expected the 12 mock replies of the conflict; a stopped scenario answers with one text: %+v", assistant)
	}
	record := onlySubagent(t, agent)
	if record.Status != agenttasks.SubagentMerged || record.Merge == nil || record.Merge.Outcome != git.MergeMergedWithConflicts || record.Merge.Applied != 0 || record.Merge.Conflicts != 1 {
		t.Fatalf("the record does not keep the conflict: %+v, %+v", record, record.Merge)
	}
	if merge := toolResult(t, messages, conversation.ToolMergeAgentChanges, 0); merge.Outcome != conversation.ToolOutcomeSucceeded || merge.ResultCode != "" {
		t.Fatalf("a merge with a conflict is a result, not a failure: %+v", merge)
	}
	t.Logf("FlowID=%s merged subagent %s with a content conflict and resolved it", session.id, record.TaskID())
}

// stop_task stops a running subagent: it stops its command, saves its
// worktree and reports stopped, and the report opens an automatic turn.
func TestSessionMockSubagentStopDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "subagent-stop", 1).Content)
	session.waitForSnapshot(ctx, "the first turn's end with the subagent running", func(agent agentloop.AgentSnapshot) bool {
		records := agent.Description.Subagents
		return len(replies(agent.History)) == 2 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage && len(records) == 1 && records[0].Status == agenttasks.SubagentRunning
	})
	session.send(ctx, mockInput(t, "subagent-stop", 2).Content)
	session.waitForSnapshot(ctx, "the automatic turn after the stopped report", func(agent agentloop.AgentSnapshot) bool {
		return len(replies(agent.History)) >= 5 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	agent, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 5 || assistant[4].Message.Content != "The subagent stopped before it finished; its saved work is kept." {
		t.Fatalf("expected the 5 mock replies; a stopped scenario answers with one text: %+v", assistant)
	}
	if stop := toolResult(t, messages, conversation.ToolStopTask, 0); stop.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("stop_task did not succeed: %+v", stop)
	}
	record := onlySubagent(t, agent)
	if record.Status != agenttasks.SubagentStopped || record.Changes != nil {
		t.Fatalf("the record is not stopped: %+v", record)
	}
	subagentNotification(t, messages, record, agenttasks.SubagentStopped)
	state := session.awaitSubagentEnd(ctx, record)
	if state.Status != subagent.StatusStopped || state.Outcome.Failure != "" || state.Outcome.Unsaved != "" || state.Outcome.Delivery != subagent.ReportDeliveredForTest || !strings.Contains(state.Outcome.Report, "stopped before it finished") {
		t.Fatalf("the subagent did not stop with its worktree saved: %+v", state.Outcome)
	}
	t.Logf("FlowID=%s stopped subagent %s with stop_task", session.id, record.TaskID())
}

// The Stop button stops a running subagent while no turn runs: the subagent
// saves its worktree and reports stopped, and the paused automatic turns
// record the report without answering it. Then nothing runs, so another Stop
// is refused.
func TestSessionMockSubagentStopButtonDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "subagent-stop", 1).Content)
	first := session.waitForSnapshot(ctx, "the first turn's end with the subagent running", func(agent agentloop.AgentSnapshot) bool {
		records := agent.Description.Subagents
		return len(replies(agent.History)) == 2 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage && len(records) == 1 && records[0].Status == agenttasks.SubagentRunning
	})
	if err := session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Turn: first.Description.Turn}); err != nil {
		t.Fatalf("the Stop of a running subagent was refused (%T)", err)
	}
	state := session.awaitSubagentEnd(ctx, onlySubagent(t, first))
	if state.Status != subagent.StatusStopped || state.Outcome.Failure != "" || state.Outcome.Unsaved != "" || state.Outcome.Delivery != subagent.ReportDeliveredForTest {
		t.Fatalf("the Stop did not stop the subagent with its worktree saved: %+v", state.Outcome)
	}
	session.waitForSnapshot(ctx, "the recorded report", func(agent agentloop.AgentSnapshot) bool {
		records := agent.Description.Subagents
		return len(records) == 1 && records[0].Status == agenttasks.SubagentStopped && len(ofRole(agent.History.Messages, conversation.MessageRoleNotification)) == 1
	})
	// The report entry is committed with the pause; an automatic turn would follow at once.
	time.Sleep(10 * time.Second)
	agent, messages := session.history(ctx)
	if len(replies(conversation.HistoryPage{Messages: messages})) != 2 || markers(messages, agentloop.PausedMarkerForTest) != 1 || agent.Description.Status != agentloop.AgentStatusWaitingForMessage {
		t.Fatalf("the stopped report opened an automatic turn or no pause was recorded: %+v", ofRole(messages, conversation.MessageRoleAssistant))
	}
	subagentNotification(t, messages, onlySubagent(t, agent), agenttasks.SubagentStopped)
	if err := session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Turn: agent.Description.Turn}); !errors.Is(err, sessionagent.ErrCommandRejected) {
		t.Fatalf("a Stop with nothing running was not refused: %v", err)
	}
	t.Logf("FlowID=%s stopped its subagent with the Stop button", session.id)
}

// The limits of one turn: a fourth start while three subagents run is refused
// with subagent_limit_reached, a seventh start since the user's message with
// subagent_budget_reached, neither with an effect. A delayed survey report
// opens the one automatic turn after the other reports arrive.
func TestSessionMockSubagentLimitDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "subagent-limit", 1).Content)
	// The Snapshot holds only the current window, which the eight starts and six
	// reports outgrow, so the replies are counted in the full history.
	var agent agentloop.AgentSnapshot
	var messages []conversation.SequencedMessage
	session.await(ctx, "the automatic turn after the survey's report", func() bool {
		agent, messages = session.history(ctx)
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && len(ofRole(messages, conversation.MessageRoleAssistant)) >= 7
	})
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 7 || assistant[6].Message.Content != "The survey subagent reported its findings." {
		t.Fatalf("expected the 7 mock replies; a stopped scenario answers with one text: %+v", assistant)
	}
	if refused := toolResult(t, messages, conversation.ToolStartAgent, 3); refused.ResultCode != toolfailure.SubagentLimitReached.String() {
		t.Fatalf("the fourth start was not refused at the running limit: %+v", refused)
	}
	if refused := toolResult(t, messages, conversation.ToolStartAgent, 7); refused.ResultCode != toolfailure.SubagentBudgetReached.String() {
		t.Fatalf("the seventh start was not refused at the budget: %+v", refused)
	}
	records := agent.Description.Subagents
	var survey agenttasks.SubagentRecord
	for _, record := range records {
		if record.Status != agenttasks.SubagentCompleted {
			t.Fatalf("a subagent did not complete: %+v", record)
		}
		if record.Description == "Survey the source" {
			survey = record
		}
	}
	if len(records) != 6 || survey.AgentID == "" {
		t.Fatalf("expected the six admitted subagents with the survey, and no record of a refused start: %+v", records)
	}
	if notifications := ofRole(messages, conversation.MessageRoleNotification); len(notifications) != 6 {
		t.Fatalf("expected one notification per subagent, got %d", len(notifications))
	}
	state := session.awaitSubagentEnd(ctx, survey)
	if state.Status != subagent.StatusCompleted ||
		state.Outcome.Report != "The survey found that web/src holds App.tsx and main.tsx." {
		t.Fatalf("the survey did not report: %+v", state.Outcome)
	}
	t.Logf("FlowID=%s refused starts at both limits; survey %s reported", session.id, survey.TaskID())
}

// A deleting Project stops its Session's running subagent, which is not a
// Project child and is found by search, before it releases the Sandbox: the
// subagent saves its worktree and ends stopped, and the Project ends DELETED.
func TestProjectDeletionStopsSubagentsDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+projectDeletionTimeout)
	defer cancel()
	probe := startCleanupProbeOn(t, ctx, conversation.ModelMock)
	pending := conversation.PendingUserMessage{MessageID: "subagent-deletion", Value: conversation.UserMessage{Content: mockInput(t, "subagent-stop", 1).Content}}
	if err := probe.client.SendPendingMessage(ctx, probe.id, pending); err != nil {
		t.Fatalf("sending the message failed (%T)", err)
	}
	var record agenttasks.SubagentRecord
	probe.await(ctx, "a running subagent", func() bool {
		snapshot, err := probe.client.GetSnapshot(ctx, probe.id)
		if err != nil || snapshot.Agent == nil || len(snapshot.Agent.Description.Subagents) != 1 {
			return false
		}
		record = snapshot.Agent.Description.Subagents[0]
		return record.Status == agenttasks.SubagentRunning
	})
	probe.requestDeletion(ctx, "delete-subagent")
	probe.awaitDeleted(ctx)
	identity := agenttasks.SubagentIdentity{ProjectID: probe.identity.ProjectID, AgentID: record.AgentID}
	state, err := probe.subagents.SnapshotForTest(ctx, identity)
	if err != nil || state.Status != subagent.StatusStopped || state.Outcome == nil || state.Outcome.Failure != "" || state.Outcome.Unsaved != "" {
		t.Fatalf("the deletion did not stop the subagent before it released the Sandbox: %+v (%T)", state, err)
	}
	probe.requireCompleted(ctx)
	t.Logf("FlowID=%s deleted; subagent %s stopped first", probe.id, identity.FlowID())
}
