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
	"strings"
	"testing"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// TestSessionMockAppTestsDexIntegration runs the app-tests scenario on a fresh
// Project's Session with a real coding container: the template's make test-app
// starts the Sandbox's test engine, and the commit gate runs the API tests as its
// fourth stage. The first push fails application_tests_failed on a wrong
// expectation, the second requirements_untested on a trace that leaves out R2,
// the third test_environment_unavailable while a detached server holds the
// engine's Dex port, and the fourth pushes with the tests stage passed and the
// trace checked against the recorded requirement IDs of version 1. Dex, the
// Sandbox, the test engine and the Git host are real; only the model is the
// mock. The server detaches from a foreground command rather than running as a
// background task: a Session whose turn grows this long routes no further tool
// while a background task's watch runs (Kind, 2026-10-09: the turn stalled
// until the watch ended).
func TestSessionMockAppTestsDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	session, agent := startRecorded(t, ctx, mockInput(t, "app-tests", 1))
	if confirmed := agent.Description.ConfirmedRequirements; confirmed == nil || len(confirmed.Spec.Features) != 2 {
		t.Fatalf("the card was not recorded with R1 and R2: %+v", confirmed)
	}
	messages := agent.History.Messages
	wantCodes := []string{toolfailure.ApplicationTestsFailed.String(), toolfailure.RequirementsUntested.String(), toolfailure.TestEnvironmentUnavailable.String(), agentloop.CommitPushedForTest}
	for index, want := range wantCodes {
		if got := toolResult(t, messages, conversation.ToolCommitAndPush, index); got.ResultCode != want {
			t.Fatalf("commit_and_push %d ended as %q (%s), want %q", index+1, got.ResultCode, got.Outcome, want)
		}
	}
	// The busy engine's failure carries its own retry hint instead of the
	// platform's advice not to repeat the call.
	if busy := session.toolResultContent(ctx, messages, conversation.ToolCommitAndPush, 2); !strings.Contains(busy, toolfailure.TestEnvironmentUnavailable.Hint()) ||
		strings.Contains(busy, toolfailure.OwnerPlatform.HintForTest()) {
		t.Fatalf("the busy engine's failure does not carry its own hint alone:\n%s", busy)
	}
	var report struct {
		Commit string `json:"commit"`
		Gate   struct {
			Stages []struct {
				Stage  string `json:"stage"`
				Status string `json:"status"`
			} `json:"stages"`
		} `json:"gate"`
		Tests struct {
			Passed    int      `json:"passed"`
			Failed    int      `json:"failed"`
			Flows     int      `json:"flows"`
			Uncovered []string `json:"uncovered"`
		} `json:"tests"`
	}
	if err := json.Unmarshal([]byte(session.toolResultContent(ctx, messages, conversation.ToolCommitAndPush, 3)), &report); err != nil {
		t.Fatalf("the pushed result is not the commit report: %v", err)
	}
	stages := report.Gate.Stages
	if len(stages) != 4 || stages[3].Stage != "tests" || stages[3].Status != "passed" || report.Tests.Passed != 10 || report.Tests.Failed != 0 ||
		report.Tests.Flows != 1 || len(report.Tests.Uncovered) != 0 {
		t.Fatalf("the pushed commit's gate did not pass its tests stage: %+v", report)
	}
	if head := session.summary(ctx).Identity.HeadCommit; head != report.Commit {
		t.Fatalf("the Session's HEAD is %s, not the pushed %s", head, report.Commit)
	}
	t.Logf("FlowID=%s pushed %s after application_tests_failed, requirements_untested and test_environment_unavailable; tests %+v", session.id, report.Commit, report.Tests)
}

// toolResultContent is the content of the result of tool at index, in order,
// as the Session stored it. The Snapshot's messages carry no tool result
// content, so it reads the stored entry, CurrentMessages/<sequence>, from the
// engine state of the Session (a diagnostic read, blobs loaded as the Flow API
// loads them).
func (s *mockSession) toolResultContent(ctx context.Context, messages []conversation.SequencedMessage, tool conversation.ToolName, index int) string {
	s.t.Helper()
	var sequences []conversation.Sequence
	for _, message := range ofRole(messages, conversation.MessageRoleTool) {
		if *message.Message.ToolName == tool {
			sequences = append(sequences, message.Sequence)
		}
	}
	if index >= len(sequences) {
		s.t.Fatalf("expected a %s result at %d, got %d results", tool, index, len(sequences))
	}
	state, err := s.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(s.id)})
	if err != nil {
		s.t.Fatalf("the engine state of %s did not read (%s)", s.id, grpcFailure(err))
	}
	key := "CurrentMessages/" + agentloop.SequenceKey(sequences[index])
	for _, attribute := range state.GetAttributes() {
		if attribute.GetKey() != key {
			continue
		}
		var entry conversation.Entry
		decodeHistoryJSON(ctx, s.t, s.reader, string(s.id), attribute.GetValue(), &entry)
		for _, block := range entry.Blocks {
			var result struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			}
			if json.Unmarshal(block, &result) == nil && result.Type == conversation.BlockTypeToolResult.String() {
				return result.Content
			}
		}
		s.t.Fatalf("the stored entry %s holds no tool result", key)
	}
	s.t.Fatalf("the engine state of %s holds no %s", s.id, key)
	return ""
}
