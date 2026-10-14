// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package relay

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/state"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
)

// scriptedAnswerOrca replaces only the Orca boundary for the owner-approved
// completed-turn race. Other Orca methods are not used by this case.
type scriptedAnswerOrca struct {
	Orca
	sent []SendRequest
}

func (orca *scriptedAnswerOrca) Runtime(context.Context) (Runtime, error) {
	return Runtime{RuntimeID: "test-runtime"}, nil
}

func (orca *scriptedAnswerOrca) Send(_ context.Context, request SendRequest) (SendResult, error) {
	orca.sent = append(orca.sent, request)
	return SendResult{State: SubmissionAccepted, ProviderItemID: "answer-message"}, nil
}

func TestRelayAnswerAfterTurnCompleted(t *testing.T) {
	dir, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const (
		connectionID = "remote-test"
		turnID       = "turn-lifecycle:asking"
		callID       = "remote-2-21"
		answer       = "Superverse message (JSON): answer to GitHub signal source"
	)
	binding, err := dir.AddBinding(state.BindingRecord{Endpoint: "http://127.0.0.1:26089", ConnectionID: connectionID,
		ProjectID: "9oj2", ProjectName: "Hotspot Radar", OrcaSessionID: "test-session", ProviderThreadID: "test-thread"}, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	progress := state.Progress{Activated: true, Epoch: 2, Generation: 2, Cursor: 88,
		Boundary: state.Position{Epoch: "test-epoch", Sequence: 1},
		Turns:    []state.TurnGeneration{{TurnID: turnID, Generation: 2}}}
	if err := binding.SaveProgress(progress); err != nil {
		t.Fatal(err)
	}
	orca := &scriptedAnswerOrca{}
	r, err := newRelay(binding, nil, orca, slog.Default(), func() {})
	if err != nil {
		t.Fatal(err)
	}
	r.runningTurn = turnID
	r.readCursor, r.queuedThrough = progress.Cursor, progress.Cursor
	call := newPendingCall(turnID)
	call.callID = callID
	r.calls[callID], r.slotCalls[21] = call, call

	// The question's MCP call remains open, but Codex completed the turn
	// after its functions.exec cell yielded. The answer arrives minutes later.
	completion := Event{Kind: EventTurnCompleted, TurnID: turnID, Position: state.Position{Epoch: "test-epoch", Sequence: 2}}
	if err := r.handleEvents([]Event{completion}, completion.Position); err != nil {
		t.Fatal(err)
	}
	if r.calls[callID] != nil || r.slotCalls[21] != nil {
		t.Fatal("the completed turn still owns the pending question call")
	}
	select {
	case result := <-call.result:
		if !strings.Contains(result.Text, "arrive as a message") || strings.Contains(result.Text, answer) {
			t.Fatalf("the orphaned MCP call received the answer: %q", result.Text)
		}
	default:
		t.Fatal("the completed turn did not release its MCP call")
	}

	entry := bridgeapi.Entry{Sequence: 89, Role: bridgeapi.EntryRoleUser, Kind: bridgeapi.EntryKindUserMessage,
		AnsweredInputCallID: callID, Text: answer}
	r.queueAnswerLocked(bridgeapi.SyncResponse{Entries: []bridgeapi.Entry{entry}, ExaminedThrough: 89})
	if len(r.queue) != 1 || r.queue[0].entrySequence != 89 || r.queue[0].text != answer {
		t.Fatalf("the answer was not queued as a message: %+v", r.queue)
	}
	if err := r.deliverEntry(t.Context(), r.queue[0]); err != nil {
		t.Fatal(err)
	}
	if len(orca.sent) != 1 || orca.sent[0].Text != answer {
		t.Fatalf("Codex did not receive exactly one answer message: %+v", orca.sent)
	}
	saved, err := binding.Progress()
	if err != nil || saved.Cursor != 89 {
		t.Fatalf("the answer delivery cursor is %d (%v), want 89", saved.Cursor, err)
	}
}
