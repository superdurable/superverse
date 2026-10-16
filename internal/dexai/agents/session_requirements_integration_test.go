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
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// The mock model numbers its tool calls by input and reply, so a Session that runs
// two scenarios repeats call IDs. These scenarios therefore tell a card or a
// reply of one turn from the earlier ones by message sequence.

// waitForSnapshot polls the Session until done accepts its Snapshot. A Session
// that ends first, or whose engine execution stops, fails the test.
func (s *mockSession) waitForSnapshot(ctx context.Context, what string, done func(agentloop.AgentSnapshot) bool) agentloop.AgentSnapshot {
	s.t.Helper()
	for {
		snapshot, err := s.client.GetSnapshot(ctx, s.id)
		if err == nil {
			if snapshot.Session.Status != agentloop.SessionStatusActive {
				s.t.Fatalf("Session %s ended as %s before %s", s.id, snapshot.Session.Status, what)
			}
			if snapshot.ExecutionFailure != agentloop.ExecutionRunning {
				s.t.Fatalf("the engine execution of Session %s stopped as %s before %s", s.id, snapshot.ExecutionFailure, what)
			}
			if snapshot.Agent != nil && done(*snapshot.Agent) {
				return *snapshot.Agent
			}
		}
		select {
		case <-ctx.Done():
			s.t.Fatalf("%s never happened in %s; reconcile it (%v)", what, s.id, err)
		case <-time.After(2 * time.Second):
		}
	}
}

// recordedRequirements waits until the turn after sequence ended with a reply
// while the current requirements are version, and fails unless that version
// was recorded without waiting: no input waits for the user, no one confirmed
// it, and every card the turn showed after sequence has a result.
func (s *mockSession) recordedRequirements(ctx context.Context, after conversation.Sequence, version int64) agentloop.AgentSnapshot {
	s.t.Helper()
	agent := s.waitForSnapshot(ctx, "version "+strconv.FormatInt(version, 10)+" of the requirements", func(agent agentloop.AgentSnapshot) bool {
		stored := agent.Description.ConfirmedRequirements
		return stored != nil && stored.Revision == version && endsWithReplyAfter(agent, after)
	})
	requireNoFailure(s.t, agent.History)
	if pending := agent.Description.PendingUserInput; pending != nil {
		s.t.Fatalf("an input waits for the user after the card: %+v", pending)
	}
	if stored := agent.Description.ConfirmedRequirements; stored.ConfirmedBy != nil || stored.ConfirmedAt.IsZero() {
		s.t.Fatalf("version %d is not a recorded version: %+v", version, stored)
	}
	_, messages := s.history(ctx)
	cards, results := 0, 0
	for _, message := range messages {
		if message.Sequence <= after {
			continue
		}
		for _, call := range message.Message.ToolCalls {
			if call.Name == conversation.ToolConfirmRequirements {
				cards++
			}
		}
		if message.Message.Role == conversation.MessageRoleTool && *message.Message.ToolName == conversation.ToolConfirmRequirements {
			results++
		}
	}
	if cards == 0 || results != cards {
		s.t.Fatalf("the turn after %d showed %d cards with %d results", after, cards, results)
	}
	return agent
}

// steer sends content and steers it at once, as Studio's change box does.
func (s *mockSession) steer(ctx context.Context, content string) {
	s.t.Helper()
	id := conversation.MessageID("steer-" + time.Now().UTC().Format("150405.000000000"))
	if err := s.client.SendPendingMessage(ctx, s.id, conversation.PendingUserMessage{MessageID: id, Value: conversation.UserMessage{Content: content}}); err != nil {
		s.t.Fatalf("sending a message to %s: %v", s.id, err)
	}
	// The API steers as the authenticated caller; the Session records it as SteeredBy.
	if err := s.client.SteerMessage(ctx, s.id, conversation.SteerMessageRequest{Actor: &conversation.Actor{AccountID: s.account}, MessageID: id}); err != nil {
		s.t.Fatalf("steering message %s of %s: %v", id, s.id, err)
	}
}

// startRecorded starts a mock Session with the describing input of a
// requirements scenario and returns the Snapshot once its turn ended after
// recording version 1.
func startRecorded(t *testing.T, ctx context.Context, describe mockmodel.Input) (*mockSession, agentloop.AgentSnapshot) {
	t.Helper()
	if describe.Kind != mockmodel.InputMessage {
		t.Fatalf("the scenario lists its first input as %s, not a message", describe.Kind)
	}
	session := newMockSession(t, ctx, describe.Content)
	return session, session.recordedRequirements(ctx, 0, 1)
}

// endsWithReplyAfter reports whether the turn after sequence ended: the
// Agent waits and its last message is a reply that calls no tool.
func endsWithReplyAfter(agent agentloop.AgentSnapshot, sequence conversation.Sequence) bool {
	messages := agent.History.Messages
	if len(messages) == 0 || agent.Description.Status != agentloop.AgentStatusWaitingForMessage {
		return false
	}
	last := messages[len(messages)-1]
	return last.Sequence > sequence && last.Message.Role == conversation.MessageRoleAssistant && len(last.Message.ToolCalls) == 0
}

// callsAfter returns the tool calls of the replies after sequence, in order.
func callsAfter(messages []conversation.SequencedMessage, sequence conversation.Sequence) []conversation.ToolCallSummary {
	var calls []conversation.ToolCallSummary
	for _, message := range messages {
		if message.Sequence > sequence && message.Message.Role == conversation.MessageRoleAssistant {
			calls = append(calls, message.Message.ToolCalls...)
		}
	}
	return calls
}

// requireRejected fails unless the Session refused the command without any effect.
func requireRejected(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, sessionagent.ErrCommandRejected) {
		t.Fatalf("%s must be rejected by the current state, got %v", what, err)
	}
}

// A new application shows the requirements card, which the Session records as
// version 1 in the commit that answers the call, and the turn goes on: the
// script's progress list and final reply follow in the same turn. Nothing
// waits for the user, so no answer is admitted for the card.
func TestSessionMockRequirementsCardDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, agent := startRecorded(t, ctx, mockInput(t, "requirements-card", 1))
	stored := *agent.Description.ConfirmedRequirements
	if spec := stored.Spec; spec.Title != "Release newsletter" || len(spec.Features) != 2 || spec.Features[0].ID != "R1" || spec.Features[1].ID != "R2" ||
		len(spec.Connections) != 1 || spec.Connections[0].Status != agentloop.RequirementConnectionNew || spec.ReplacesRequirements {
		t.Fatalf("version 1 does not hold the mock requirements: %+v", spec)
	}
	if stored.LastFeatureNumber != 2 {
		t.Fatalf("version 1 does not have 2 as its highest number: %+v", stored)
	}
	assistant := replies(agent.History)
	if len(assistant) != 3 || len(assistant[0].Message.ToolCalls) != 1 || assistant[0].Message.ToolCalls[0].Name != conversation.ToolConfirmRequirements ||
		assistant[0].Message.ToolCalls[0].Target != "Release newsletter" {
		t.Fatalf("the turn did not start with the confirm_requirements call: %+v", assistant)
	}
	requireMock(t, assistant[0], mockInputTokens)
	if len(assistant[1].Message.ToolCalls) != 1 || assistant[1].Message.ToolCalls[0].Name != conversation.ToolWriteTodos ||
		len(assistant[2].Message.ToolCalls) != 0 || !strings.Contains(assistant[2].Message.Content, "version 1") {
		t.Fatalf("the turn did not go on after the card without a second one: %d replies", len(assistant))
	}
	if recorded := toolResult(t, agent.History.Messages, conversation.ToolConfirmRequirements, 0); recorded.Outcome != conversation.ToolOutcomeSucceeded || recorded.ResultCode != "" {
		t.Fatalf("the card was not recorded: %+v", recorded)
	}
	if todo := agent.Description.TodoList; todo == nil || len(todo.Items) != 2 {
		t.Fatalf("the progress list was not recorded: %+v", todo)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-card", Input: 1})
	call := assistant[0].Message.ToolCalls[0].ID
	requireRejected(t, "an answer to the card", session.client.AnswerQuestions(ctx, session.id, agentloop.AnswerQuestionsRequest{CallID: call, Answers: []agentloop.UserInputAnswer{{QuestionID: "q", Answer: "a"}}}))
}

// A message changes the requirements while the Agent works. The first card is
// recorded as version 1 and the turn goes on to a durable timer; the message,
// sent and steered as Studio's change box does, interrupts the timer at once
// and starts the turn that revises the card as version 2: R1 and R2 of
// version 1 and a new R3. The rest of the interrupted turn never runs.
func TestSessionMockRequirementsSteeredChangeDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "requirements-draft-changes", 1).Content)
	agent := session.waitForSnapshot(ctx, "version 1 while the Agent waits on its timer", func(agent agentloop.AgentSnapshot) bool {
		stored := agent.Description.ConfirmedRequirements
		return stored != nil && stored.Revision == 1 && agent.Description.PendingTimer != nil && agent.Description.Status == agentloop.AgentStatusWaitingForTimer
	})
	requireNoFailure(t, agent.History)
	first := *agent.Description.ConfirmedRequirements
	if agent.Description.PendingUserInput != nil {
		t.Fatalf("the card waits for the user: %+v", agent.Description.PendingUserInput)
	}

	before := agent.Description.LastSequence
	change := mockInput(t, "requirements-draft-changes", 2)
	session.steer(ctx, change.Content)
	agent = session.recordedRequirements(ctx, before, 2)
	second := *agent.Description.ConfirmedRequirements
	if features := second.Spec.Features; len(features) != 3 || !reflect.DeepEqual(features[:2], first.Spec.Features) || features[2].ID != "R3" {
		t.Fatalf("version 2 does not keep R1 and R2 of version 1 and add R3: %+v", features)
	}
	if second.LastFeatureNumber != 3 || second.Spec.ReplacesRequirements || !second.ConfirmedAt.After(first.ConfirmedAt) {
		t.Fatalf("version 2 does not revise version 1: %+v", second)
	}
	_, messages := session.history(ctx)
	if timer := toolResult(t, messages, conversation.ToolStartTimer, 0); timer.ResultCode != toolfailure.SupersededBySteeredUserMessage.String() {
		t.Fatalf("the steered change did not interrupt the timer: %+v", timer)
	}
	var steered bool
	for _, message := range messages {
		steered = steered || message.Sequence > before && message.Message.Role == conversation.MessageRoleUser && message.Message.SteeredBy != nil
		if message.Message.Role == conversation.MessageRoleAssistant && strings.Contains(message.Message.Content, "so I build version 1") {
			t.Fatalf("the interrupted turn went on after the steered change: %q", message.Message.Content)
		}
	}
	if !steered {
		t.Fatal("the change did not start its turn as a steered message")
	}
	if final := agent.History.Messages[len(agent.History.Messages)-1].Message.Content; !strings.Contains(final, "version 2") {
		t.Fatalf("the turn of the change did not end with version 2: %q", final)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-draft-changes", Input: 2})
}

// After version 1, a completely different request is a large change whose card
// replaces the requirements instead of revising them and says so with
// replaces_requirements: version 2 is the new application alone, with its
// features numbered from R1 again, none of version 1's, and the numbering's
// high mark restarted.
func TestSessionMockRequirementsCompletelyChangesDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, agent := startRecorded(t, ctx, mockInput(t, "requirements-completely-changes", 1))
	first := *agent.Description.ConfirmedRequirements

	before := agent.Description.LastSequence
	session.send(ctx, mockInput(t, "requirements-completely-changes", 2).Content)
	agent = session.recordedRequirements(ctx, before, 2)
	stored := *agent.Description.ConfirmedRequirements
	second := stored.Spec
	if !second.ReplacesRequirements {
		t.Fatal("the card of a completely different request does not mark that it replaces the requirements")
	}
	if second.Title == first.Spec.Title || len(second.Features) == 0 || second.Features[0].ID != "R1" {
		t.Fatalf("version 2 is not a new application numbered from R1: %+v", second)
	}
	if want := second.Features[len(second.Features)-1].ID; "R"+strconv.FormatInt(stored.LastFeatureNumber, 10) != want {
		t.Fatalf("the replacement did not restart the highest feature number at %s: %d", want, stored.LastFeatureNumber)
	}
	for _, feature := range second.Features {
		if slices.ContainsFunc(first.Spec.Features, func(old agentloop.RequirementFeature) bool { return old.Text == feature.Text }) {
			t.Fatalf("version 2 kept the feature %q of version 1", feature.Text)
		}
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-completely-changes", Input: 2})
}

// After version 1, a request that adds a feature and drops another is a large
// change: the revised card keeps R1 and R3 exactly as recorded, removes R2
// without reusing its number and adds R4, recorded as version 2, exactly R1,
// R3 and R4, whose highest number used is 4. The work that follows in that
// turn covers that change alone. A card that then gives the dropped feature
// R2 back is refused with the next free number, R5, and the corrected card is
// version 3. A compaction afterwards keeps version 3 in the new segment: the
// mock model answers a reply after the compaction with a stop text unless the
// first message of the request, the segment_start, carries it with its version.
func TestSessionMockRequirementsAmendedDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, agent := startRecorded(t, ctx, mockInput(t, "requirements-amended", 1))
	first := *agent.Description.ConfirmedRequirements

	before := agent.Description.LastSequence
	session.send(ctx, mockInput(t, "requirements-amended", 2).Content)
	agent = session.recordedRequirements(ctx, before, 2)
	second := *agent.Description.ConfirmedRequirements
	old := first.Spec.Features
	if len(old) != 3 || old[1].ID != "R2" {
		t.Fatalf("version 1 is not R1 to R3: %+v", old)
	}
	if features := second.Spec.Features; len(features) != 3 || features[0] != old[0] || features[1] != old[2] || features[2].ID != "R4" || features[2].Text == old[1].Text {
		t.Fatalf("version 2 is not R1 and R3 as recorded plus a new R4: %+v", features)
	}
	if second.LastFeatureNumber != 4 || second.Spec.ReplacesRequirements {
		t.Fatalf("version 2 does not revise version 1 with 4 as its highest number: %+v", second)
	}
	_, messages := session.history(ctx)
	if calls := callsAfter(messages, before); len(calls) != 2 || calls[0].Name != conversation.ToolConfirmRequirements || calls[1].Name != conversation.ToolWriteTodos {
		t.Fatalf("the turn of version 2 is not its card and one progress list: %+v", calls)
	}
	if todo := agent.Description.TodoList; todo == nil || len(todo.Items) != 2 || !strings.HasPrefix(todo.Items[0].Content, "R4:") || !strings.Contains(todo.Items[1].Content, "R2") {
		t.Fatalf("the work after version 2 does not cover adding R4 and removing R2 alone: %+v", todo)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-amended", Input: 2, NextInput: 3})

	// A card that gives the dropped feature its removed number R2 back is refused
	// with the next free number, and the corrected card numbers it R5.
	before = agent.Description.LastSequence
	session.send(ctx, mockInput(t, "requirements-amended", 3).Content)
	agent = session.recordedRequirements(ctx, before, 3)
	_, messages = session.history(ctx)
	after := slices.DeleteFunc(slices.Clone(messages), func(message conversation.SequencedMessage) bool { return message.Sequence <= before })
	if refused := toolResult(t, after, conversation.ToolConfirmRequirements, 0); refused.Outcome != conversation.ToolOutcomeKnownFailure || refused.ResultCode != toolfailure.InvalidUserInput.String() {
		t.Fatalf("the card that reuses R2 was not refused: %+v", refused)
	}
	third := *agent.Description.ConfirmedRequirements
	if features := third.Spec.Features; len(features) != 4 || features[3].ID != "R5" || features[3].Text != old[1].Text {
		t.Fatalf("version 3 does not number the restored feature R5: %+v", features)
	}
	if third.LastFeatureNumber != 5 {
		t.Fatalf("version 3 does not have 5 as its highest number: %+v", third)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-amended", Input: 3})

	// A compaction keeps version 3: the scenario's last step checks the new segment_start.
	before = agent.Description.LastSequence
	session.send(ctx, mockInput(t, "requirements-compaction", 1).Content+" "+mockmodel.ArgumentMarkerForTest("version", strconv.FormatInt(third.Revision, 10))+" "+mockmodel.ArgumentMarkerForTest("title", third.Spec.Title))
	agent = session.waitForSnapshot(ctx, "the turn that compacts the context", func(agent agentloop.AgentSnapshot) bool {
		return endsWithReplyAfter(agent, before)
	})
	requireNoFailure(t, agent.History)
	var compacted bool
	for _, message := range agent.History.Messages {
		compacted = compacted || message.Sequence > before && message.Message.Role == conversation.MessageRoleSystem && message.Message.Content == agentloop.SegmentMarker(agentloop.SegmentReasonCompaction)
	}
	if !compacted {
		t.Fatal("the context was never compacted in the turn")
	}
	final := agent.History.Messages[len(agent.History.Messages)-1].Message.Content
	if !strings.Contains(final, "survived the compaction") {
		t.Fatalf("the segment after the compaction does not carry version %d of the requirements: %q", third.Revision, final)
	}
	if kept := agent.Description.ConfirmedRequirements; kept == nil || kept.Revision != third.Revision {
		t.Fatalf("the compaction changed the current requirements: %+v", kept)
	}
}

// After version 1, new copy leaves the requirements as they are, so it is a
// small change: the Agent edits the label in the workspace directly, with no
// card and no question, and version 1 stays exactly as recorded. The edit
// stays uncommitted, so the Session continues the turn three times with the
// source handoff reminder, which the scenario answers with the same text.
func TestSessionMockRequirementsSmallChangeDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, agent := startRecorded(t, ctx, mockInput(t, "requirements-small-change", 1))
	first := *agent.Description.ConfirmedRequirements

	before := agent.Description.LastSequence
	session.send(ctx, mockInput(t, "requirements-small-change", 2).Content)
	agent = session.waitForSnapshot(ctx, "the turn of the small change", func(agent agentloop.AgentSnapshot) bool {
		return endsWithReplyAfter(agent, before)
	})
	requireNoFailure(t, agent.History)
	if agent.Description.PendingUserInput != nil {
		t.Fatalf("the small change waits for the user: %+v", agent.Description.PendingUserInput)
	}
	if kept := agent.Description.ConfirmedRequirements; kept == nil || !reflect.DeepEqual(*kept, first) {
		t.Fatalf("the small change changed the current requirements: %+v", kept)
	}
	_, messages := session.history(ctx)
	if calls := callsAfter(messages, before); len(calls) != 1 || calls[0].Name != conversation.ToolEdit {
		t.Fatalf("the small change is not one direct edit: %+v", calls)
	}
	after := slices.DeleteFunc(slices.Clone(messages), func(message conversation.SequencedMessage) bool { return message.Sequence <= before })
	if edit := toolResult(t, after, conversation.ToolEdit, 0); edit.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("the edit of the label did not succeed: %+v", edit)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-small-change", Input: 2})
}

// send queues a message for the Session.
func (s *mockSession) send(ctx context.Context, content string) {
	s.t.Helper()
	pending := conversation.PendingUserMessage{MessageID: conversation.MessageID("mock-" + time.Now().UTC().Format("150405.000000000")), Value: conversation.UserMessage{Content: content}}
	if err := s.client.SendPendingMessage(ctx, s.id, pending); err != nil {
		s.t.Fatalf("sending a message to %s: %v", s.id, err)
	}
}
