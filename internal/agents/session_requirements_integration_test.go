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

	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
)

// The mock model numbers its tool calls by input and reply, so a Session that runs
// two scenarios repeats call IDs. These scenarios therefore tell a card, an
// answer or a reply of one turn from the earlier ones by message sequence.

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

// requirementsCard waits for a requirements card that a message after
// sequence added and that ends the turn.
func (s *mockSession) requirementsCard(ctx context.Context, after conversation.Sequence) agentloop.AgentSnapshot {
	s.t.Helper()
	return s.waitForSnapshot(ctx, "the requirements card", func(agent agentloop.AgentSnapshot) bool {
		pending := agent.Description.PendingUserInput
		return pending != nil && pending.Kind == agentloop.PendingUserInputKindRequirements && agent.Description.LastSequence > after &&
			agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
}

// decide answers the card with decision and returns the answer: the user
// message after sequence that names the card's call, once it is recorded and
// the card no longer waits. A request for changes may already show the
// revised card, whose call is another one.
func (s *mockSession) decide(ctx context.Context, card agentloop.PendingUserInput, decision agentloop.RequirementsDecision, after conversation.Sequence) conversation.SequencedMessage {
	s.t.Helper()
	if err := s.client.AnswerQuestions(ctx, s.id, agentloop.AnswerQuestionsRequest{CallID: card.CallID, Requirements: &decision}); err != nil {
		s.t.Fatalf("answering the card of %s: %v", card.CallID, err)
	}
	var answered conversation.SequencedMessage
	s.waitForSnapshot(ctx, "the answer to the card", func(agent agentloop.AgentSnapshot) bool {
		for _, message := range agent.History.Messages {
			if message.Sequence > after && message.Message.Role == conversation.MessageRoleUser && message.Message.AnsweredInputCallID != nil && *message.Message.AnsweredInputCallID == card.CallID {
				answered = message
			}
		}
		pending := agent.Description.PendingUserInput
		return answered.Sequence > 0 && (pending == nil || pending.CallID != card.CallID)
	})
	return answered
}

// confirmed answers the card with the confirmation, requires the answer of
// version, and returns the Snapshot once the turn it starts ended with a reply.
func (s *mockSession) confirmed(ctx context.Context, card agentloop.PendingUserInput, after conversation.Sequence, version int64) agentloop.AgentSnapshot {
	s.t.Helper()
	answered := s.decide(ctx, card, agentloop.RequirementsDecision{IsConfirmed: true}, after)
	if want := "Requirements confirmed (version " + strconv.FormatInt(version, 10) + ")"; answered.Message.Content != want {
		s.t.Fatalf("the confirmation answered %q, want %q", answered.Message.Content, want)
	}
	agent := s.waitForSnapshot(ctx, "the turn after the confirmation", func(agent agentloop.AgentSnapshot) bool {
		return endsWithReplyAfter(agent, answered.Sequence)
	})
	requireNoFailure(s.t, agent.History)
	if stored := agent.Description.ConfirmedRequirements; stored == nil || stored.Revision != version || !reflect.DeepEqual(stored.Spec, *card.Requirements) || stored.ConfirmedAt.IsZero() {
		s.t.Fatalf("the confirmation did not store the card as version %d: %+v", version, stored)
	}
	return agent
}

// startConfirmed starts a mock Session with the describing input of a
// requirements scenario, confirms its card with the scenario's confirmation
// and returns the Snapshot once that turn ended, holding version 1.
func startConfirmed(t *testing.T, ctx context.Context, describe, confirm mockmodel.Input) (*mockSession, agentloop.AgentSnapshot) {
	t.Helper()
	if describe.Kind != mockmodel.InputMessage || confirm.Kind != mockmodel.InputRequirementsConfirm {
		t.Fatalf("the scenario lists its first inputs as %s and %s, not a message and a confirmation", describe.Kind, confirm.Kind)
	}
	session := newMockSession(t, ctx, describe.Content)
	agent := session.requirementsCard(ctx, 0)
	requireNoFailure(t, agent.History)
	if agent.Description.ConfirmedRequirements != nil {
		t.Fatalf("a new Session holds confirmed requirements: %+v", agent.Description.ConfirmedRequirements)
	}
	return session, session.confirmed(ctx, *agent.Description.PendingUserInput, 0, 1)
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

// A new application shows the requirements card and ends the turn there. The
// card refuses a queued message and any answer of another shape or call. The
// confirmation writes ConfirmedRequirements in the commit that publishes
// "Requirements confirmed (version 1)", and the script goes on without asking
// again.
func TestSessionMockRequirementsCardDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "requirements-card", 1).Content)

	// The card ends the turn, and nothing is confirmed yet.
	agent := session.requirementsCard(ctx, 0)
	requireNoFailure(t, agent.History)
	card := *agent.Description.PendingUserInput
	if agent.Description.ConfirmedRequirements != nil || card.Requirements == nil || len(card.Questions) != 0 {
		t.Fatalf("the card is not an unconfirmed requirements wait: %+v", agent.Description)
	}
	if spec := card.Requirements; spec.Title != "Release newsletter" || len(spec.Features) != 2 || spec.Features[0].ID != "R1" || spec.Features[1].ID != "R2" ||
		len(spec.Connections) != 1 || spec.Connections[0].Status != agentloop.RequirementConnectionNew {
		t.Fatalf("the card does not hold the mock requirements: %+v", spec)
	}
	assistant := replies(agent.History)
	if len(assistant) != 1 || len(assistant[0].Message.ToolCalls) != 1 || assistant[0].Message.ToolCalls[0].Name != conversation.ToolConfirmRequirements ||
		assistant[0].Message.ToolCalls[0].Target != "Release newsletter" {
		t.Fatalf("the first turn did not end with the confirm_requirements call: %+v", assistant)
	}
	requireMock(t, assistant[0], mockInputTokens)

	// Only an exact decision of this card is admitted.
	requireRejected(t, "a queued message", session.client.SendPendingMessage(ctx, session.id, conversation.PendingUserMessage{MessageID: "card-pending", Value: conversation.UserMessage{Content: "hello"}}))
	for what, request := range map[string]agentloop.AnswerQuestionsRequest{
		"a stale call":                   {CallID: "another-call", Requirements: &agentloop.RequirementsDecision{IsConfirmed: true}},
		"answers to a card":              {CallID: card.CallID, Answers: []agentloop.UserInputAnswer{{QuestionID: "q", Answer: "a"}}},
		"no decision":                    {CallID: card.CallID},
		"a confirmation with text":       {CallID: card.CallID, Requirements: &agentloop.RequirementsDecision{IsConfirmed: true, RequestedChanges: "also"}},
		"a request for changes, no text": {CallID: card.CallID, Requirements: &agentloop.RequirementsDecision{RequestedChanges: "  "}},
	} {
		requireRejected(t, what, session.client.AnswerQuestions(ctx, session.id, request))
	}
	if again := session.requirementsCard(ctx, 0); again.Description.PendingUserInput.CallID != card.CallID || again.Description.ConfirmedRequirements != nil {
		t.Fatalf("a rejected answer changed the card: %+v", again.Description)
	}

	// The confirmation is version 1 and the script goes on to its final reply.
	agent = session.confirmed(ctx, card, 0, 1)
	assistant = replies(agent.History)
	if len(assistant) != 3 || len(assistant[1].Message.ToolCalls) != 1 || assistant[1].Message.ToolCalls[0].Name != conversation.ToolWriteTodos ||
		len(assistant[2].Message.ToolCalls) != 0 || !strings.Contains(assistant[2].Message.Content, "confirmed") {
		t.Fatalf("the script did not go on after the confirmation without a second card: %d replies", len(assistant))
	}
	if todo := agent.Description.TodoList; todo == nil || len(todo.Items) != 2 {
		t.Fatalf("the progress list was not recorded: %+v", todo)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-card", Input: 2})
}

// The user asks for changes to the draft card before anything is confirmed,
// in their own words: the mock model takes a text that answers the card as the
// scenario's request for changes, and the Session answers with the trimmed
// text. Nothing is confirmed while the revised card waits; it keeps the
// draft's R1 and R2 and adds R3, and its confirmation is version 1.
func TestSessionMockRequirementsDraftChangesDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	if change := mockInput(t, "requirements-draft-changes", 2); change.Kind != mockmodel.InputRequirementsChange {
		t.Fatalf("requirements-draft-changes lists input 2 as %s, not a request for changes", change.Kind)
	}
	session := newMockSession(t, ctx, mockInput(t, "requirements-draft-changes", 1).Content)
	agent := session.requirementsCard(ctx, 0)
	requireNoFailure(t, agent.History)
	draft := *agent.Description.PendingUserInput

	const changes = "Please let visitors filter the list by repository."
	answered := session.decide(ctx, draft, agentloop.RequirementsDecision{RequestedChanges: " " + changes + " "}, agent.Description.LastSequence)
	if answered.Message.Content != changes {
		t.Fatalf("the request for changes answered %q, want the user's text", answered.Message.Content)
	}
	agent = session.requirementsCard(ctx, answered.Sequence)
	revised := *agent.Description.PendingUserInput
	if agent.Description.ConfirmedRequirements != nil {
		t.Fatalf("a request for changes to the draft confirmed requirements: %+v", agent.Description.ConfirmedRequirements)
	}
	if features := revised.Requirements.Features; len(features) != 3 || !reflect.DeepEqual(features[:2], draft.Requirements.Features) || features[2].ID != "R3" {
		t.Fatalf("the revised card does not keep R1 and R2 of the draft and add R3: %+v", features)
	}

	agent = session.confirmed(ctx, revised, answered.Sequence, 1)
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-draft-changes", Input: 3})
}

// After a confirmation, a completely different request is a large change whose
// card replaces the requirements instead of revising them and says so with
// replaces_requirements: version 1 stays until the new card is confirmed, and
// version 2 is the new application alone, with its features numbered from R1
// again, none of version 1's, and the numbering's high mark restarted.
func TestSessionMockRequirementsCompletelyChangesDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, agent := startConfirmed(t, ctx, mockInput(t, "requirements-completely-changes", 1), mockInput(t, "requirements-completely-changes", 2))
	first := *agent.Description.ConfirmedRequirements

	session.send(ctx, mockInput(t, "requirements-completely-changes", 3).Content)
	agent = session.requirementsCard(ctx, agent.Description.LastSequence)
	card := *agent.Description.PendingUserInput
	if kept := agent.Description.ConfirmedRequirements; kept == nil || !reflect.DeepEqual(*kept, first) {
		t.Fatalf("the new card changed version 1 before its confirmation: %+v", kept)
	}
	if !card.Requirements.ReplacesRequirements {
		t.Fatal("the card of a completely different request does not mark that it replaces the requirements")
	}

	agent = session.confirmed(ctx, card, agent.Description.LastSequence, 2)
	stored := *agent.Description.ConfirmedRequirements
	second := stored.Spec
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
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-completely-changes", Input: 4})
}

// After a confirmation, a request that adds a feature and drops another is a
// large change: the revised card keeps R1 and R3 exactly as confirmed, removes
// R2 without reusing its number and adds R4, and its confirmation is version 2,
// exactly R1, R3 and R4, whose highest number used is 4. The work that follows
// covers that change alone. A card that then gives the dropped feature R2 back
// is refused with the next free number, R5, and the corrected card is version
// 3. A compaction afterwards keeps version 3 in the new segment: the mock
// model answers a reply after the compaction with a stop text unless the first
// message of the request, the segment_start, carries it with its version.
func TestSessionMockRequirementsAmendedDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, agent := startConfirmed(t, ctx, mockInput(t, "requirements-amended", 1), mockInput(t, "requirements-amended", 2))
	first := *agent.Description.ConfirmedRequirements

	session.send(ctx, mockInput(t, "requirements-amended", 3).Content)
	agent = session.requirementsCard(ctx, agent.Description.LastSequence)
	revised := *agent.Description.PendingUserInput
	if kept := agent.Description.ConfirmedRequirements; kept == nil || !reflect.DeepEqual(*kept, first) {
		t.Fatalf("the revised card changed version 1 before its confirmation: %+v", kept)
	}

	confirmedAt := agent.Description.LastSequence
	agent = session.confirmed(ctx, revised, confirmedAt, 2)
	second := *agent.Description.ConfirmedRequirements
	old := first.Spec.Features
	if len(old) != 3 || old[1].ID != "R2" {
		t.Fatalf("version 1 is not R1 to R3: %+v", old)
	}
	if features := second.Spec.Features; len(features) != 3 || features[0] != old[0] || features[1] != old[2] || features[2].ID != "R4" || features[2].Text == old[1].Text {
		t.Fatalf("version 2 is not R1 and R3 as confirmed plus a new R4: %+v", features)
	}
	if second.LastFeatureNumber != 4 || second.Spec.ReplacesRequirements {
		t.Fatalf("version 2 does not revise version 1 with 4 as its highest number: %+v", second)
	}
	_, messages := session.history(ctx)
	if calls := callsAfter(messages, confirmedAt); len(calls) != 1 || calls[0].Name != conversation.ToolWriteTodos {
		t.Fatalf("the work after version 2 is not one progress list: %+v", calls)
	}
	if todo := agent.Description.TodoList; todo == nil || len(todo.Items) != 2 || !strings.HasPrefix(todo.Items[0].Content, "R4:") || !strings.Contains(todo.Items[1].Content, "R2") {
		t.Fatalf("the work after version 2 does not cover adding R4 and removing R2 alone: %+v", todo)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-amended", Input: 4, NextInput: 5})

	// A card that gives the dropped feature its removed number R2 back is refused
	// with the next free number, and the corrected card numbers it R5.
	before := agent.Description.LastSequence
	session.send(ctx, mockInput(t, "requirements-amended", 5).Content)
	agent = session.pendingInput(ctx, agentloop.PendingUserInputKindRequirements, before)
	_, messages = session.history(ctx)
	after := slices.DeleteFunc(slices.Clone(messages), func(message conversation.SequencedMessage) bool { return message.Sequence <= before })
	if refused := toolResult(t, after, conversation.ToolConfirmRequirements, 0); refused.Outcome != conversation.ToolOutcomeKnownFailure || refused.ResultCode != toolfailure.InvalidUserInput.String() {
		t.Fatalf("the card that reuses R2 was not refused: %+v", refused)
	}
	corrected := *agent.Description.PendingUserInput
	if features := corrected.Requirements.Features; len(features) != 4 || features[3].ID != "R5" || features[3].Text != old[1].Text {
		t.Fatalf("the corrected card does not number the restored feature R5: %+v", features)
	}
	agent = session.confirmed(ctx, corrected, agent.Description.LastSequence, 3)
	third := *agent.Description.ConfirmedRequirements
	if third.LastFeatureNumber != 5 {
		t.Fatalf("version 3 does not have 5 as its highest number: %+v", third)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-amended", Input: 6})

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
		t.Fatalf("the segment after the compaction does not carry version %d of the confirmed requirements: %q", third.Revision, final)
	}
	if kept := agent.Description.ConfirmedRequirements; kept == nil || kept.Revision != third.Revision {
		t.Fatalf("the compaction changed the confirmed requirements: %+v", kept)
	}
}

// After a confirmation, new copy leaves the confirmed requirements as they
// are, so it is a small change: the Agent edits the label in the workspace
// directly, with no card and no question, and version 1 stays exactly as
// confirmed. The edit stays uncommitted, so the Session continues the turn
// three times with the source handoff reminder, which the scenario answers
// with the same text.
func TestSessionMockRequirementsSmallChangeDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, agent := startConfirmed(t, ctx, mockInput(t, "requirements-small-change", 1), mockInput(t, "requirements-small-change", 2))
	first := *agent.Description.ConfirmedRequirements

	before := agent.Description.LastSequence
	session.send(ctx, mockInput(t, "requirements-small-change", 3).Content)
	agent = session.waitForSnapshot(ctx, "the turn of the small change", func(agent agentloop.AgentSnapshot) bool {
		return endsWithReplyAfter(agent, before)
	})
	requireNoFailure(t, agent.History)
	if agent.Description.PendingUserInput != nil {
		t.Fatalf("the small change waits for the user: %+v", agent.Description.PendingUserInput)
	}
	if kept := agent.Description.ConfirmedRequirements; kept == nil || !reflect.DeepEqual(*kept, first) {
		t.Fatalf("the small change changed the confirmed requirements: %+v", kept)
	}
	_, messages := session.history(ctx)
	if calls := callsAfter(messages, before); len(calls) != 1 || calls[0].Name != conversation.ToolEdit {
		t.Fatalf("the small change is not one direct edit: %+v", calls)
	}
	after := slices.DeleteFunc(slices.Clone(messages), func(message conversation.SequencedMessage) bool { return message.Sequence <= before })
	if edit := toolResult(t, after, conversation.ToolEdit, 0); edit.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("the edit of the label did not succeed: %+v", edit)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-small-change", Input: 3})
}

// send queues a message for the Session.
func (s *mockSession) send(ctx context.Context, content string) {
	s.t.Helper()
	pending := conversation.PendingUserMessage{MessageID: conversation.MessageID("mock-" + time.Now().UTC().Format("150405.000000000")), Value: conversation.UserMessage{Content: content}}
	if err := s.client.SendPendingMessage(ctx, s.id, pending); err != nil {
		s.t.Fatalf("sending a message to %s: %v", s.id, err)
	}
}
