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
	"strings"
	"testing"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// The file-edit-failure scenario writes a note whose two lines are equal and
// edits one of them with a text that occurs twice. The helper in the coding
// container refuses the edit with edit_ambiguous and changes nothing; the
// failure returns to the model as an error result. The mock model goes on
// only when that result holds the code, the owner and the effect and the hint,
// so the next replies prove what the model read: it reads the note again and
// edits it with one line more of context, which is unique and succeeds. Every
// tool runs in the coding container through the real superverse-files helper.
//
// The scenario leaves its note uncommitted. A successful write or edit marks
// the turn's source as changed, so the Session continues the turn after its
// final text, three times, with the unfinished source handoff reminder; the
// scenario answers each continuation with the same text, so the turn ends with
// 6 + 3 = 9 mock replies and none of them calls a tool.
func TestSessionMockFileEditFailureDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "file-edit-failure", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	assistant := replies(agent.History)
	if len(assistant) != 9 {
		t.Fatalf("expected the 9 mock replies (6, and 3 for the handoff continuations), got %d; a stopped scenario answers with one text", len(assistant))
	}
	for _, reply := range assistant {
		requireMock(t, reply, mockInputTokens)
	}
	wantTools := []conversation.ToolName{conversation.ToolWrite, conversation.ToolEdit, conversation.ToolRead, conversation.ToolEdit, conversation.ToolRead}
	wantCodes := []string{"", toolfailure.EditAmbiguous.String(), "", "", ""}
	var results []conversation.SequencedMessage
	for _, message := range agent.History.Messages {
		if message.Message.Role == conversation.MessageRoleTool {
			results = append(results, message)
		}
	}
	if len(results) != len(wantTools) {
		t.Fatalf("expected %d tool results, got %d", len(wantTools), len(results))
	}
	for index, result := range results {
		execution := result.Message.ToolExecution
		if *result.Message.ToolName != wantTools[index] || execution == nil || execution.ResultCode != wantCodes[index] {
			t.Fatalf("tool result %d is %s with %+v; want %s with the code %q", index, *result.Message.ToolName, execution, wantTools[index], wantCodes[index])
		}
		wantOutcome := conversation.ToolOutcomeSucceeded
		if wantCodes[index] != "" {
			wantOutcome = conversation.ToolOutcomeKnownFailure
		}
		if execution.Outcome != wantOutcome {
			t.Fatalf("tool result %d (%s) has the outcome %s, want %s", index, *result.Message.ToolName, execution.Outcome, wantOutcome)
		}
	}
	if report := assistant[5].Message; len(report.ToolCalls) != 0 || !strings.Contains(report.Content, "The ambiguous edit was refused and changed nothing") {
		t.Fatalf("the sixth reply is not the mock report: %q", report.Content)
	}
	for _, continuation := range assistant[6:] {
		if message := continuation.Message; len(message.ToolCalls) != 0 || !strings.Contains(message.Content, "stays in the workspace on purpose") {
			t.Fatalf("a reply after the report is not the mock answer to a handoff continuation: %q", message.Content)
		}
	}
	if agent.Description.PendingUserInput != nil || agent.Description.PendingApproval != nil {
		t.Fatalf("a refused edit left a wait for the user: %+v", agent.Description)
	}
}

// The file-permission-denied scenario writes a note and locks it with chmod 000.
// The coding container's file helper shares the UID of the model's commands, so
// each file tool call on the note is refused at once as the call's own failure,
// path_permission_denied with the effect none, and never as a transient fault that
// Dex would repeat until it records tool_unavailable for the platform: a serial
// read, a read and a grep in a wave beside the readable dex-app.yaml, an edit and a
// write. The mock model goes on only when each failure holds the code, owner and
// effect (and, for the first, the message and hint), so the replies prove what the
// model read; it restores the permissions and reads the note as written.
//
// The written note marks the turn's source as changed, so the Session continues the
// turn three times with the handoff reminder: 8 + 3 = 11 mock replies. The turn
// outgrows the Snapshot's current window, so the test reads the whole history.
func TestSessionMockFilePermissionDeniedDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "file-permission-denied", 1).Content)
	session.finishedTurn(ctx)
	agent, messages := session.history(ctx)
	history := conversation.HistoryPage{Messages: messages}
	requireNoFailure(t, history)
	assistant := replies(history)
	if len(assistant) != 11 {
		t.Fatalf("expected the 11 mock replies (8, and 3 for the handoff continuations), got %d; a stopped scenario answers with one text", len(assistant))
	}
	for _, reply := range assistant {
		requireMock(t, reply, mockInputTokens)
	}
	denied := toolfailure.PathPermissionDenied.String()
	wantTools := []conversation.ToolName{conversation.ToolWrite, conversation.ToolBash, conversation.ToolRead,
		conversation.ToolRead, conversation.ToolGrep, conversation.ToolRead, conversation.ToolEdit, conversation.ToolWrite, conversation.ToolBash, conversation.ToolRead}
	wantCodes := []string{"", "", denied, denied, denied, "", denied, denied, "", ""}
	results := ofRole(messages, conversation.MessageRoleTool)
	if len(results) != len(wantTools) {
		t.Fatalf("expected %d tool results, got %d", len(wantTools), len(results))
	}
	for index, result := range results {
		execution := result.Message.ToolExecution
		if *result.Message.ToolName != wantTools[index] || execution == nil || execution.ResultCode != wantCodes[index] {
			t.Fatalf("tool result %d is %s with %+v; want %s with the code %q", index, *result.Message.ToolName, execution, wantTools[index], wantCodes[index])
		}
		wantOutcome := conversation.ToolOutcomeSucceeded
		if wantCodes[index] != "" {
			wantOutcome = conversation.ToolOutcomeKnownFailure
		}
		if execution.Outcome != wantOutcome {
			t.Fatalf("tool result %d (%s) has the outcome %s, want %s", index, *result.Message.ToolName, execution.Outcome, wantOutcome)
		}
	}
	if final := assistant[7].Message; len(final.ToolCalls) != 0 || !strings.Contains(final.Content, "refused as my own call's failure") {
		t.Fatalf("the eighth reply is not the mock report: %q", final.Content)
	}
	if agent.Description.PendingUserInput != nil || agent.Description.PendingApproval != nil {
		t.Fatalf("a refused file call left a wait for the user: %+v", agent.Description)
	}
	t.Logf("FlowID=%s", session.id)
}
