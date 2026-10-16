// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	workspacegit "github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// actorAttribute reads only a named value from this test's disposable Session.
// It is an engine diagnostic, not a replacement business API or dependency.
func (s *actorSession) actorAttribute(ctx context.Context, key string, out any) bool {
	s.t.Helper()
	id := string(agentloop.SessionFlowID(s.project.ProjectID))
	state, err := s.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: id})
	if err != nil {
		s.t.Fatalf("actor state read: %T", err)
	}
	for _, attribute := range state.GetAttributes() {
		if attribute.GetKey() != key {
			continue
		}
		value := attribute.GetValue()
		if blob := value.GetInternalBlobIdForObjValue(); blob != "" {
			loaded, err := s.reader.LoadBlobs(ctx, &dexpb.LoadBlobsRequest{Entries: []*dexpb.LoadBlobRequestEntry{{FlowId: id, BlobValue: value}}})
			if err != nil {
				s.t.Fatalf("actor state blob read: %T", err)
			}
			value = loaded.GetValues()[blob]
		}
		object := value.GetObjValue()
		if object == nil || object.GetEncoding() != "json" {
			s.t.Fatal("actor state is not JSON")
		}
		if err := json.Unmarshal(object.GetPayload(), out); err != nil {
			s.t.Fatal("actor state decode failed")
		}
		return true
	}
	return false
}

// Exercises actual authentication, Dex, Sandbox commands and Git push. The real
// staged source gate runs inside the uninterruptible commit_and_push call.
// Both Stops must succeed with the SAME pending identity: stale rejection is
// deliberately not accepted as evidence for the non-ending join branch.
func TestSessionFirstActorStopDuringCommitDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	s := newActorSession(t, ctx)
	initial := s.await(ctx, "mock default", func(v agentloop.AgentSnapshot) bool {
		return v.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	if initial.Description.Model != conversation.ModelMock {
		t.Fatal("requires mock-default Worker; no real model call allowed")
	}
	disabled := false
	if _, err := s.preview.UpdatePreviewAutoDeploy(ctx, &pb.UpdatePreviewAutoDeployRequest{ProjectId: s.project.ProjectID, IsEnabled: &disabled, Caller: s.owner}); err != nil {
		t.Fatal(err)
	}
	baseline, err := s.client.GetSessionSummary(ctx, agentloop.SessionFlowID(s.project.ProjectID))
	if err != nil {
		t.Fatalf("initial Session identity: %v", err)
	}
	startSequence := initial.Description.LastSequence
	s.send(ctx, s.first, mockInput(t, "actor-stop-commit", 1).Content)
	var running agentloop.AgentState
	awaitTrue(ctx, t, "uninterruptible real commit", func() bool {
		running = agentloop.AgentState{}
		if !s.actorAttribute(ctx, "AgentState", &running) {
			return false
		}
		if running.Turn > initial.Description.Turn && running.Status == agentloop.AgentStatusWaitingForMessage {
			t.Fatal("scenario ended before Stop could observe the running commit")
		}
		return running.Running != nil && running.Running.Kind == agentloop.RunningTool && running.Running.ToolName == conversation.ToolCommitAndPush
	})
	commitCall := agentloop.PendingToolCall(running)
	if commitCall.Name != conversation.ToolCommitAndPush {
		t.Fatal("running tool cursor does not identify the commit call")
	}
	for _, caller := range []*pb.CallerContext{s.first, s.second} {
		if _, err := s.agent.InterruptAgent(ctx, &pb.InterruptAgentRequest{ProjectId: s.project.ProjectID, Turn: running.Turn, Caller: caller}); err != nil {
			t.Fatalf("both current non-ending Stops must be admitted (%T)", err)
		}
	}
	var pending agentloop.AgentInterrupt
	if !s.actorAttribute(ctx, "AgentInterrupt", &pending) || pending.EndsTurn || pending.Turn != running.Turn || pending.RequestedAt.IsZero() {
		t.Fatal("did not retain the non-ending Stop across both admissions")
	}
	requireActor(t, pending.Actor, s.first)
	stopped := s.await(ctx, "safe boundary and stopped background task", func(v agentloop.AgentSnapshot) bool {
		if v.Description.Status != agentloop.AgentStatusWaitingForMessage || v.Description.Turn != running.Turn || len(v.Description.BackgroundCommands) != 1 {
			return false
		}
		return v.Description.BackgroundCommands[0].Status == agenttasks.BackgroundCommandStopped
	})
	markers := 0
	for _, item := range stopped.History.Messages {
		if item.Message.Role == conversation.MessageRoleSystem && item.Message.Content == "Stopped by "+s.first.GetCanonicalEmail() {
			markers++
			requireActor(t, item.Message.Actor, s.first)
		}
		if item.Message.Actor != nil && item.Message.Actor.AccountID == s.second.GetAccountId() {
			t.Fatal("joined Stop invented a second author")
		}
	}
	if markers != 1 {
		t.Fatalf("Stop markers = %d, want one first-author marker", markers)
	}
	var resultSequence conversation.Sequence
	for _, item := range stopped.History.Messages {
		message := item.Message
		if item.Sequence <= startSequence || message.Role != conversation.MessageRoleTool || message.ToolCallID == nil || *message.ToolCallID != commitCall.ID {
			continue
		}
		if resultSequence != 0 || message.ToolName == nil || *message.ToolName != conversation.ToolCommitAndPush || message.ToolExecution == nil || message.ToolExecution.Outcome != conversation.ToolOutcomeSucceeded || message.ToolExecution.ResultCode != agentloop.CommitPushedForTest {
			t.Fatal("scenario commit requires exactly one successful verified push result")
		}
		resultSequence = item.Sequence
	}
	if resultSequence == 0 {
		t.Fatal("scenario commit has no completed tool result after its starting history")
	}
	entry := s.entry(ctx, resultSequence)
	if len(entry.Blocks) != 1 {
		t.Fatal("commit result must have one retained provider block")
	}
	var block struct {
		Type      string              `json:"type"`
		ToolUseID conversation.CallID `json:"tool_use_id"`
		Content   string              `json:"content"`
		IsError   bool                `json:"is_error"`
	}
	if err := json.Unmarshal(entry.Blocks[0], &block); err != nil {
		t.Fatalf("commit result block: %v", err)
	}
	if block.Type != "tool_result" || block.ToolUseID != commitCall.ID || block.IsError {
		t.Fatal("retained block does not report the scenario commit's successful result")
	}
	var pushed struct {
		Commit string `json:"commit"`
		Pushed bool   `json:"pushed"`
		Gate   *struct {
			Stages []workspacegit.SourceGateStage `json:"stages"`
		} `json:"gate"`
		Tests *workspacegit.ApplicationTestReport `json:"tests"`
	}
	if err := json.Unmarshal([]byte(block.Content), &pushed); err != nil {
		t.Fatalf("commit push report: %v", err)
	}
	if pushed.Gate == nil || len(pushed.Gate.Stages) == 0 || pushed.Tests == nil {
		t.Fatal("verified push must retain the real source gate and application test report")
	}
	for _, stage := range pushed.Gate.Stages {
		t.Logf("source gate stage=%s status=%s duration_ms=%d", stage.Stage, stage.Status, stage.DurationMS)
	}
	completed, err := s.client.GetSessionSummary(ctx, agentloop.SessionFlowID(s.project.ProjectID))
	if err != nil {
		t.Fatalf("completed Session identity: %v", err)
	}
	head := completed.Identity.HeadCommit
	sha, err := hex.DecodeString(head)
	if err != nil || len(sha) != 20 || head == baseline.Identity.HeadCommit || !pushed.Pushed || pushed.Commit != head {
		t.Fatal("successful verified push must advance Session HEAD to its reported valid commit SHA")
	}
	var final agentloop.AgentState
	if !s.actorAttribute(ctx, "AgentState", &final) || final.Running != nil || len(final.PendingToolCalls) != 0 || !final.AutomaticTurnsPaused {
		t.Fatal("safe-boundary Stop did not clean up and suppress automatic turns")
	}
	if s.actorAttribute(ctx, "AgentInterrupt", &pending) {
		t.Fatal("safe boundary did not consume Stop")
	}
	var handoff agentloop.PreviewHandoff
	if s.actorAttribute(ctx, "PreviewHandoff", &handoff) && handoff.Unpublished() {
		t.Fatal("safe-boundary Stop retained an unpublished Preview handoff")
	}
}

func (s *actorSession) startIdleStopTask(ctx context.Context) agentloop.AgentSnapshot {
	s.t.Helper()
	t := s.t
	s.send(ctx, s.first, mockInput(t, "actor-idle-stop", 1).Content)
	return s.await(ctx, "idle real background task", func(v agentloop.AgentSnapshot) bool {
		return v.Description.Status == agentloop.AgentStatusWaitingForMessage && len(v.Description.BackgroundCommands) == 1 && v.Description.BackgroundCommands[0].Status == agenttasks.BackgroundCommandRunning
	})
}
func (s *actorSession) stopAsFirst(ctx context.Context, turn int64) {
	s.t.Helper()
	if _, err := s.agent.InterruptAgent(ctx, &pb.InterruptAgentRequest{ProjectId: s.project.ProjectID, Turn: turn, Caller: s.first}); err != nil {
		s.t.Fatal(err)
	}
}
func (s *actorSession) requireSingleStopBefore(ctx context.Context, snapshot agentloop.AgentSnapshot, content string) {
	s.t.Helper()
	markers, stopSequence, inputSequence := 0, conversation.Sequence(0), conversation.Sequence(0)
	for _, item := range snapshot.History.Messages {
		if item.Message.Role == conversation.MessageRoleSystem && item.Message.Content == "Stopped by "+s.first.GetCanonicalEmail() {
			markers++
			stopSequence = item.Sequence
			requireActor(s.t, item.Message.Actor, s.first)
		}
		if item.Message.Role == conversation.MessageRoleUser && item.Message.Content == content {
			inputSequence = item.Sequence
			requireActor(s.t, item.Message.Actor, s.second)
		}
	}
	if markers != 1 || stopSequence >= inputSequence || inputSequence == 0 {
		s.t.Fatal("new user input did not follow exactly one first-actor Stop marker")
	}
	var state agentloop.AgentState
	if !s.actorAttribute(ctx, "AgentState", &state) || state.AutomaticTurnsPaused {
		s.t.Fatal("explicit input must resume without an extra automatic pause")
	}
}

// Full actual API/Sandbox input path. The ordering driver separately proves
// both exact commit orders; this does not infer which concurrent writer won.
func TestSessionIdleStopNewInputDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	s := newActorSession(t, ctx)
	idle := s.startIdleStopTask(ctx)
	s.stopAsFirst(ctx, idle.Description.Turn)
	text := "Resume after the idle Stop attribution check."
	s.send(ctx, s.second, text)
	snapshot := s.await(ctx, "new input and stopped task", func(v agentloop.AgentSnapshot) bool {
		return v.Description.Turn == idle.Description.Turn+1 && v.Description.Status == agentloop.AgentStatusWaitingForMessage && len(v.Description.BackgroundCommands) == 1 && v.Description.BackgroundCommands[0].Status == agenttasks.BackgroundCommandStopped
	})
	s.requireSingleStopBefore(ctx, snapshot, text)
	var pending agentloop.AgentInterrupt
	if s.actorAttribute(ctx, "AgentInterrupt", &pending) {
		t.Fatal("new input did not consume idle Stop")
	}
}

// Dedicated-lane scenario: the existing helper replaces the actual merged
// platform server, not merely a test registry/client. Coordinator schedules it
// alone after granting that lane; this Dispatch only compiles its source.
func TestSessionIdleStopServerRestartDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	s := newActorSession(t, ctx)
	idle := s.startIdleStopTask(ctx)
	s.stopAsFirst(ctx, idle.Description.Turn)
	var pending agentloop.AgentInterrupt
	awaitTrue(ctx, t, "committed idle Stop marker", func() bool { return s.actorAttribute(ctx, "AgentInterrupt", &pending) && pending.MarkerWritten })
	requireActor(t, pending.Actor, s.first)
	before := pending
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, err := s.client.GetSnapshot(ctx, agentloop.SessionFlowID(s.project.ProjectID))
		return err
	})
	if !s.actorAttribute(ctx, "AgentInterrupt", &pending) || pending.Turn != before.Turn || pending.EndsTurn != before.EndsTurn {
		t.Fatal("merged server replacement lost the pending marker ownership")
	}
	// Compare frozen value rather than pointer identity after JSON hydration.
	if !pending.RequestedAt.Equal(before.RequestedAt) || !pending.MarkerWritten {
		t.Fatal("restart changed Stop identity or marker flag")
	}
	requireActor(t, pending.Actor, s.first)
	text := "Resume after the actual platform-server replacement."
	s.send(ctx, s.second, text)
	snapshot := s.await(ctx, "resumed input after server replacement", func(v agentloop.AgentSnapshot) bool {
		return v.Description.Turn == idle.Description.Turn+1 && v.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	s.requireSingleStopBefore(ctx, snapshot, text)
}

// A real stopped subagent emits its report notification; explicitly stopped
// background commands intentionally do not. Commit-order coverage is separate.
func TestSessionIdleStopSubagentNotificationDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	s := newActorSession(t, ctx)
	s.send(ctx, s.first, mockInput(t, "subagent-stop", 1).Content)
	idle := s.await(ctx, "idle real subagent", func(v agentloop.AgentSnapshot) bool {
		return v.Description.Status == agentloop.AgentStatusWaitingForMessage && len(v.Description.Subagents) == 1 && v.Description.Subagents[0].Status == agenttasks.SubagentRunning
	})
	s.stopAsFirst(ctx, idle.Description.Turn)
	snapshot := s.await(ctx, "stopped subagent notification", func(v agentloop.AgentSnapshot) bool {
		if v.Description.Turn != idle.Description.Turn || v.Description.Status != agentloop.AgentStatusWaitingForMessage || len(v.Description.Subagents) != 1 || v.Description.Subagents[0].Status != agenttasks.SubagentStopped {
			return false
		}
		for _, item := range v.History.Messages {
			if item.Message.Notification != nil && item.Message.Notification.Source == conversation.NotificationSourceSubagent && item.Message.Notification.TaskStatus == conversation.TaskEndStopped {
				return true
			}
		}
		return false
	})
	markers, stopSequence, notificationSequence := 0, conversation.Sequence(0), conversation.Sequence(0)
	for _, item := range snapshot.History.Messages {
		if item.Message.Role == conversation.MessageRoleSystem && item.Message.Content == "Stopped by "+s.first.GetCanonicalEmail() {
			markers++
			stopSequence = item.Sequence
			requireActor(t, item.Message.Actor, s.first)
		}
		if item.Message.Notification != nil && item.Message.Notification.Source == conversation.NotificationSourceSubagent {
			notificationSequence = item.Sequence
			if item.Message.Actor != nil {
				t.Fatal("automatic notification invented a human author")
			}
		}
	}
	var state agentloop.AgentState
	if markers != 1 || stopSequence >= notificationSequence || !s.actorAttribute(ctx, "AgentState", &state) || !state.AutomaticTurnsPaused {
		t.Fatal("subagent notification did not follow one attributed marker and stay paused")
	}
}
