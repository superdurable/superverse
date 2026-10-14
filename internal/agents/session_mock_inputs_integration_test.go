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
	"slices"
	"strings"
	"testing"

	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/modelprovider/mockmodel"
)

// These scenarios send the mock model's ordered inputs exactly as Studio's
// mock composer does: the content ListMockScenarios lists for each input, a
// message through SendMessage and an answer through AnswerQuestions. Like the
// other mock scenarios they run on the Session of a throwaway READY Project
// and the actual Worker, with Dex, the Sandbox and every tool real.

// mockInput is input number of a scenario as the catalog lists it.
func mockInput(t *testing.T, scenario string, number int) mockmodel.Input {
	t.Helper()
	for _, candidate := range mockmodel.Catalog() {
		if candidate.Name == scenario && number >= 1 && number <= len(candidate.Inputs) {
			return candidate.Inputs[number-1]
		}
	}
	t.Fatalf("the mock catalog has no input %d of scenario %s", number, scenario)
	return mockmodel.Input{}
}

// pendingKindName names a pending-input kind for a failure; the questions of
// request_user_input have the empty kind.
func pendingKindName(kind agentloop.PendingUserInputKind) string {
	if kind == "" {
		return "request_user_input questions"
	}
	return string(kind)
}

// pendingInput waits until a reply after sequence ended the turn at a pending
// input of kind and returns that Snapshot. A turn that ends without one fails
// the test with its last reply.
func (s *mockSession) pendingInput(ctx context.Context, kind agentloop.PendingUserInputKind, after conversation.Sequence) agentloop.AgentSnapshot {
	s.t.Helper()
	return s.waitForSnapshot(ctx, "the pending "+pendingKindName(kind)+" input", func(agent agentloop.AgentSnapshot) bool {
		if agent.Description.Status != agentloop.AgentStatusWaitingForMessage || agent.Description.LastSequence <= after {
			return false
		}
		if pending := agent.Description.PendingUserInput; pending != nil && pending.Kind == kind {
			return true
		}
		if endsWithReplyAfter(agent, after) {
			last := agent.History.Messages[len(agent.History.Messages)-1]
			s.t.Fatalf("the turn after %d ended without the pending %s input; its last reply: %q", after, pendingKindName(kind), last.Message.Content)
		}
		return false
	})
}

// answer answers the pending input and waits until its answer starts a turn
// that ends with a reply, which it returns with that Snapshot.
func (s *mockSession) answer(ctx context.Context, request agentloop.AnswerQuestionsRequest, after conversation.Sequence) (agentloop.AgentSnapshot, conversation.SequencedMessage) {
	s.t.Helper()
	if err := s.client.AnswerQuestions(ctx, s.id, request); err != nil {
		s.t.Fatalf("answering %s: %v", request.CallID, err)
	}
	agent := s.waitForSnapshot(ctx, "the turn after the answer", func(agent agentloop.AgentSnapshot) bool {
		return endsWithReplyAfter(agent, after)
	})
	return agent, agent.History.Messages[len(agent.History.Messages)-1]
}

// An input sent before the one the scenario expects is answered with a text
// that names the expected input, and no tool runs. The scenario then passes in
// order: the card, a request for changes with input 2's marked text, the
// revised card and its confirmation.
func TestSessionMockInputOrderDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	describe, change, confirm := mockInput(t, "requirements-draft-changes", 1), mockInput(t, "requirements-draft-changes", 2), mockInput(t, "requirements-draft-changes", 3)
	if change.Kind != mockmodel.InputRequirementsChange || confirm.Kind != mockmodel.InputRequirementsConfirm {
		t.Fatalf("requirements-draft-changes lists inputs 2 and 3 as %s and %s", change.Kind, confirm.Kind)
	}
	session := newMockSession(t, ctx, change.Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	assistant := replies(agent.History)
	if len(assistant) != 1 || len(assistant[0].Message.ToolCalls) != 0 || !strings.Contains(assistant[0].Message.Content, "input 2 is out of order; send input 1 first: "+describe.Label) {
		t.Fatalf("input 2 before input 1 was not answered with the out-of-order text alone: %+v", assistant)
	}
	requireMock(t, assistant[0], mockInputTokens)
	if tools := ofRole(agent.History.Messages, conversation.MessageRoleTool); len(tools) != 0 || agent.Description.PendingUserInput != nil {
		t.Fatalf("the out-of-order input ran %d tools or left a pending input", len(tools))
	}
	if agent.Description.MockProgress != nil {
		t.Fatalf("an input out of order started the scenario: %+v", agent.Description.MockProgress)
	}

	session.send(ctx, describe.Content)
	agent = session.requirementsCard(ctx, agent.Description.LastSequence)
	card := *agent.Description.PendingUserInput
	answered := session.decide(ctx, card, agentloop.RequirementsDecision{RequestedChanges: change.Content}, agent.Description.LastSequence)
	agent = session.requirementsCard(ctx, answered.Sequence)
	revised := *agent.Description.PendingUserInput
	if len(revised.Requirements.Features) != 3 {
		t.Fatalf("input 2 did not get the revised card: %+v", revised.Requirements)
	}
	answered = session.decide(ctx, revised, agentloop.RequirementsDecision{IsConfirmed: true}, answered.Sequence)
	agent = session.waitForSnapshot(ctx, "the turn after input 3", func(agent agentloop.AgentSnapshot) bool {
		return endsWithReplyAfter(agent, answered.Sequence)
	})
	requireNoFailure(t, agent.History)
	if final := agent.History.Messages[len(agent.History.Messages)-1].Message.Content; !strings.Contains(final, "revised requirements are confirmed") {
		t.Fatalf("the scenario did not end after its three inputs: %q", final)
	}
	if confirmed := agent.Description.ConfirmedRequirements; confirmed == nil || confirmed.Revision != 1 || len(confirmed.Spec.Features) != 3 {
		t.Fatalf("the confirmation did not store the revised card as version 1: %+v", confirmed)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "requirements-draft-changes", Input: 3})
}

// requireProgress fails unless the Snapshot reports the progress of the
// newest mock reply as want.
func requireProgress(t *testing.T, agent agentloop.AgentSnapshot, want conversation.MockProgress) {
	t.Helper()
	if got := agent.Description.MockProgress; got == nil || *got != want {
		t.Fatalf("the Snapshot reports the mock progress %+v, want %+v", got, want)
	}
}

// The Session admits a 101st model call in the same turn after 100 completed
// calls. The mock model supplies replies while Dex and write_todos remain real.
func TestSessionMockLongTurnDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "long-turn", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	if agent.Description.PendingUserInput != nil {
		t.Fatalf("the long turn left a pending question: %+v", agent.Description.PendingUserInput)
	}
	if final := agent.History.Messages[len(agent.History.Messages)-1].Message.Content; !strings.Contains(final, "finished after 101 model calls") {
		t.Fatalf("the 101st model call did not complete the turn: %q", final)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "long-turn", Input: 1})
}

// request_user_input with two questions ends the turn; input 2 answers both in
// question order. Before the questions the model raises its effort and
// searches the connector catalog; after the answers it waits on a durable
// timer and records the plan.
func TestSessionMockQuestionAnswersDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	answers := mockInput(t, "question-answers", 2)
	if answers.Kind != mockmodel.InputAnswers || len(answers.Answers) != 2 {
		t.Fatalf("question-answers lists input 2 as %s with %d answers", answers.Kind, len(answers.Answers))
	}
	session := newMockSession(t, ctx, mockInput(t, "question-answers", 1).Content)
	agent := session.pendingInput(ctx, "", 0)
	pending := agent.Description.PendingUserInput
	if len(pending.Questions) != len(answers.Answers) {
		t.Fatalf("the model asked %d questions, the scenario answers %d", len(pending.Questions), len(answers.Answers))
	}
	request := agentloop.AnswerQuestionsRequest{CallID: pending.CallID}
	for index, question := range pending.Questions {
		request.Answers = append(request.Answers, agentloop.UserInputAnswer{QuestionID: question.ID, Answer: answers.Answers[index]})
	}
	agent, final := session.answer(ctx, request, agent.Description.LastSequence)
	requireNoFailure(t, agent.History)
	if !strings.Contains(final.Message.Content, "goes out from Gmail every Monday morning") {
		t.Fatalf("the answers did not lead to the scenario's last reply: %q", final.Message.Content)
	}
	_, messages := session.history(ctx)
	for _, tool := range []conversation.ToolName{conversation.ToolSetEffort, conversation.ToolConnectorCatalog, conversation.ToolStartTimer, conversation.ToolWriteTodos} {
		if result := toolResult(t, messages, tool, 0); result.Outcome != conversation.ToolOutcomeSucceeded {
			t.Fatalf("%s did not succeed: %+v", tool, result)
		}
	}
	answered := slices.IndexFunc(messages, func(message conversation.SequencedMessage) bool {
		return message.Message.Role == conversation.MessageRoleUser && message.Message.AnsweredInputCallID != nil && *message.Message.AnsweredInputCallID == pending.CallID
	})
	if answered < 0 || !strings.Contains(messages[answered].Message.Content, answers.Answers[0]) || !strings.Contains(messages[answered].Message.Content, answers.Answers[1]) {
		t.Fatal("the answers were not recorded as the user message that answers the questions")
	}
}

// The read-only platform tools in one parallel wave: the delivery status, the
// ProjectFlow summary and the first page of its history, for the test's
// Project, succeed on the server; the Live log read may fail when the Project
// has no Live deployment, which the scenario expects. The model reads the
// history page itself: the scenario stops with one text, and no final reply,
// unless the results hold the page's first event, a FlowStarted.
func TestSessionMockPlatformReadsDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, "")
	projectID := session.identity.ProjectID
	session.send(ctx, mockInput(t, "platform-reads", 1).Content+" "+mockmodel.ArgumentMarkerForTest("project", projectID))
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	assistant := replies(agent.History)
	if len(assistant) != 2 || assistant[1].Message.Content != "The platform reads finished." {
		t.Fatalf("expected the wave and the final reply; a stopped scenario answers with one text: %+v", assistant)
	}
	if result := toolResult(t, agent.History.Messages, conversation.ToolReadDeliveryStatus, 0); result.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("%s did not succeed for Project %s: %+v", conversation.ToolReadDeliveryStatus, projectID, result)
	}
	// The summary and the history page are the two reads of the ProjectFlow.
	for index := range 2 {
		if result := toolResult(t, agent.History.Messages, conversation.ToolReadPlatformFlow, index); result.Outcome != conversation.ToolOutcomeSucceeded {
			t.Fatalf("%s read %d did not succeed for Project %s: %+v", conversation.ToolReadPlatformFlow, index+1, projectID, result)
		}
	}
	toolResult(t, agent.History.Messages, conversation.ToolReadLiveAppRuns, 0)
}

// A new Session switched to Opus and back to the mock model before its first
// message has no context segment yet, so its first segment opens on the mock:
// no Opus call and no rollover happen, and the switches cost nothing.
func TestSessionMockModelSwitchDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newSession(t, ctx, conversation.ModelOpus55, "")
	started := session.waitForSnapshot(ctx, "the empty Session on Opus", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && agent.Description.Model == conversation.ModelOpus55
	})
	if started.Description.ContextModel != "" {
		t.Fatalf("the Session opened a segment on %q before its first message", started.Description.ContextModel)
	}
	if err := session.client.UpdateSessionModel(ctx, session.id, agentloop.UpdateSessionModelRequest{Model: conversation.ModelMock, ExpectedRevision: started.Description.ModelRevision}); err != nil {
		t.Fatalf("switching to the mock model failed: %v", err)
	}
	session.send(ctx, mockInput(t, "explain", 1).Content)
	agent := session.finishedTurnAfter(ctx, started.Description.LastSequence)
	requireNoFailure(t, agent.History)
	if agent.Description.Model != conversation.ModelMock || agent.Description.ContextModel != conversation.ModelMock {
		t.Fatalf("the turn did not run on the mock model: selected %q, context %q", agent.Description.Model, agent.Description.ContextModel)
	}
	assistant := replies(agent.History)
	if len(assistant) != 1 {
		t.Fatalf("expected the one explain reply, got %d", len(assistant))
	}
	requireMock(t, assistant[0], mockInputTokens)
	_, messages := session.history(ctx)
	for _, reason := range []agentloop.SegmentReason{agentloop.SegmentReasonRollover, agentloop.SegmentReasonCompaction, agentloop.SegmentReasonUnsummarized} {
		if count := markers(messages, agentloop.SegmentMarker(reason)); count != 0 {
			t.Fatalf("the switch opened %d %s segments; the first segment opens on the mock model", count, reason)
		}
	}
	for _, message := range messages {
		if call := message.Message.ModelCall; call != nil && call.Model != conversation.ModelMock.ProviderModel() {
			t.Fatalf("message %d called %s", message.Sequence, call.Model)
		}
	}
}
