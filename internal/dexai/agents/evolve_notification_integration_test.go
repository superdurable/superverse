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
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
)

// TestEvolveNotificationBudgetDexIntegration sends synthetic agent reviews
// to a mock Session through SessionFlowService.ReportEvolveNotification, as
// EvolveFlow does, on the lane's real Dex, Worker and Sandbox:
//   - evolve-budget input 1 chains background steps until automatic
//     continuation pauses at its bound; the review's notification lifts that
//     pause and opens its own turn within fresh bounds, whose reply declares
//     the evolve source, and no second paused marker is recorded;
//   - input 2 runs a long check that the test stops; a review sent after the
//     Stop is taken without an entry and opens no turn, so it expires;
//   - input 3 waits 30 seconds in a timer; a review sent meanwhile is not
//     taken inside the turn: its notification follows the turn's last reply
//     and opens its own turn after it.
//
// The pause of an AgentState stored before its cause existed reads as a
// Stop's (the pause cause of agentloop.AgentState); no RPC writes such a state, so
// that compatibility is not exercised here.
func TestEvolveNotificationBudgetDexIntegration(t *testing.T) {
	const taskTurns = 5
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "evolve-budget", 1).Content)
	service, projectID := session.service(), session.identity.ProjectID
	session.commandsEnded(ctx, taskTurns+1)
	paused := session.waitForSnapshot(ctx, "the pause at the automatic-turn bound", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && slices.ContainsFunc(agent.History.Messages, func(message conversation.SequencedMessage) bool {
			return message.Message.Role == conversation.MessageRoleSystem && message.Message.Content == agentloop.PausedMarkerForTest
		})
	})

	reviewNotice(ctx, t, service, projectID, "r-1")
	lifted := session.waitForSnapshot(ctx, "the review's own turn", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Turn == paused.Description.Turn+1 && endsWithReplyAfter(agent, paused.Description.LastSequence)
	})
	_, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	if reply := lastReply(t, messages); !strings.Contains(reply, "within fresh bounds") {
		t.Fatalf("the review's turn did not answer the evolve notification: %q", reply)
	}
	if notice := evolveEntry(t, messages, "r-1"); notice.Sequence <= paused.Description.LastSequence {
		t.Fatalf("the review's notification was recorded at %d, before the pause at %d", notice.Sequence, paused.Description.LastSequence)
	}
	if pauses := markers(messages, agentloop.PausedMarkerForTest); pauses != 1 {
		t.Fatalf("the paused marker was recorded %d times", pauses)
	}

	before := lifted.Description.LastSequence
	session.send(ctx, mockInput(t, "evolve-budget", 2).Content)
	running := session.waitForSnapshot(ctx, "the long check in flight", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.LastSequence > before && agent.Description.Status == agentloop.AgentStatusExecutingTool
	})
	if err := session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Turn: running.Description.Turn}); err != nil {
		t.Fatalf("the Stop of turn %d was not admitted: %v", running.Description.Turn, err)
	}
	stopped := session.waitForSnapshot(ctx, "the end of the stopped turn", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && agent.Description.LastSequence > before
	})
	reviewNotice(ctx, t, service, projectID, "r-2")
	session.awaitNotificationsTaken(ctx, "the review after the Stop")
	afterStop, messages := session.history(ctx)
	if recordedEvolve(messages, "r-2") {
		t.Fatal("the review after the Stop was recorded, so the model would see it in the user's next turn")
	}
	if afterStop.Description.Turn != stopped.Description.Turn || lastReply(t, messages) != lastReply(t, messagesUpTo(messages, stopped.Description.LastSequence)) {
		t.Fatalf("a review after the Stop opened turn %d after %d", afterStop.Description.Turn, stopped.Description.Turn)
	}

	before = afterStop.Description.LastSequence
	session.send(ctx, mockInput(t, "evolve-budget", 3).Content)
	session.waitForSnapshot(ctx, "the timer of the long turn", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForTimer
	})
	reviewNotice(ctx, t, service, projectID, "r-3")
	ended := session.waitForSnapshot(ctx, "the review's turn after the long turn", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && endsWithReplyAfter(agent, before) &&
			strings.Contains(agent.History.Messages[len(agent.History.Messages)-1].Message.Content, "waited for the end of the turn")
	})
	_, messages = session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	notice, turnEnd := evolveEntry(t, messages, "r-3"), replyContaining(t, messages, "this turn is done")
	if notice.Sequence <= turnEnd.Sequence {
		t.Fatalf("the review sent during the timer entered the turn at %d, before its last reply at %d", notice.Sequence, turnEnd.Sequence)
	}
	t.Logf("Session %s took reviews r-1 (cap lifted), r-2 (after a Stop, dropped) and r-3 (after the turn, turn %d)", session.id, ended.Description.Turn)
}

// reviewNotice tells the Session of Project projectID of review reviewID, as
// EvolveFlow's RequestReview does, and fails unless it is admitted.
func reviewNotice(ctx context.Context, t *testing.T, service *SessionFlowService, projectID, reviewID string) {
	t.Helper()
	notice := evolve.EvolveNotice{ReviewID: reviewID, OccurredAt: time.Now().UTC(),
		Text: fmt.Sprintf("Review %s of the goals waits for your report. Follow references/evolve.md.", reviewID)}
	if err := service.ReportEvolveNotification(ctx, projectID, notice); err != nil {
		t.Fatalf("the Session did not take review %s: %v", reviewID, err)
	}
}

// awaitNotificationsTaken waits until the Session's notification Channel is
// empty: the Agent took every notification admitted before the call.
func (s *mockSession) awaitNotificationsTaken(ctx context.Context, what string) {
	s.t.Helper()
	for {
		state, err := s.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(s.id)})
		if err == nil && len(state.GetPendingChannelMessages()["SessionNotifications"].GetMessages()) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			s.t.Fatalf("the Session %s never took %s (%v)", s.id, what, err)
		case <-time.After(2 * time.Second):
		}
	}
}

// recordedEvolve reports whether the notification of review reviewID was recorded.
func recordedEvolve(messages []conversation.SequencedMessage, reviewID string) bool {
	for _, message := range ofRole(messages, conversation.MessageRoleNotification) {
		if n := message.Message.Notification; n != nil && n.IsEvolve() && n.Evolve.ReviewID == reviewID {
			return true
		}
	}
	return false
}

// evolveEntry is the recorded notification of review reviewID, or fails.
func evolveEntry(t *testing.T, messages []conversation.SequencedMessage, reviewID string) conversation.SequencedMessage {
	t.Helper()
	for _, message := range ofRole(messages, conversation.MessageRoleNotification) {
		if n := message.Message.Notification; n != nil && n.IsEvolve() && n.Evolve.ReviewID == reviewID {
			return message
		}
	}
	t.Fatalf("no evolve notification of review %s was recorded", reviewID)
	return conversation.SequencedMessage{}
}

// lastReply is the text of the last assistant message, or fails.
func lastReply(t *testing.T, messages []conversation.SequencedMessage) string {
	t.Helper()
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) == 0 {
		t.Fatal("the conversation holds no reply")
	}
	return assistant[len(assistant)-1].Message.Content
}

// replyContaining is the first assistant message that holds text, or fails.
func replyContaining(t *testing.T, messages []conversation.SequencedMessage, text string) conversation.SequencedMessage {
	t.Helper()
	for _, message := range ofRole(messages, conversation.MessageRoleAssistant) {
		if strings.Contains(message.Message.Content, text) {
			return message
		}
	}
	t.Fatalf("no reply holds %q", text)
	return conversation.SequencedMessage{}
}

// messagesUpTo are the messages up to sequence.
func messagesUpTo(messages []conversation.SequencedMessage, sequence conversation.Sequence) []conversation.SequencedMessage {
	var kept []conversation.SequencedMessage
	for _, message := range messages {
		if message.Sequence <= sequence {
			kept = append(kept, message)
		}
	}
	return kept
}
