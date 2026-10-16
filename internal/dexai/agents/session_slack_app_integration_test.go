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

	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// TestSessionMockSlackThreadApprovalDexIntegration runs the slack-thread-approval
// scenario on a fresh Project's Session with a real coding container: after the
// confirmed card the Agent replaces the template's text summary with a Slack
// thread approval on the released Slack connector, and the commit gate builds
// it, validates its Flow definition with both trigger bindings and runs its API
// tests on the Sandbox's test engine. Slack has no mocks, so the push lists it as
// uncovered. Dex, the Sandbox, the test engine and the Git host are real; only
// the model is the mock.
func TestSessionMockSlackThreadApprovalDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	session, agent := startRecorded(t, ctx, mockInput(t, "slack-thread-approval", 1))
	if confirmed := agent.Description.ConfirmedRequirements; confirmed == nil || len(confirmed.Spec.Features) != 3 {
		t.Fatalf("the card was not confirmed with R1 to R3: %+v", confirmed)
	}
	requireNoFailure(t, agent.History)
	messages := agent.History.Messages
	if got := toolResult(t, messages, conversation.ToolCommitAndPush, 0); got.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("commit_and_push ended as %q (%s), want %q", got.ResultCode, got.Outcome, agentloop.CommitPushedForTest)
	}
	var report struct {
		Commit string `json:"commit"`
		Tests  struct {
			Passed    int      `json:"passed"`
			Failed    int      `json:"failed"`
			Flows     int      `json:"flows"`
			Uncovered []string `json:"uncovered"`
		} `json:"tests"`
	}
	if err := json.Unmarshal([]byte(session.toolResultContent(ctx, messages, conversation.ToolCommitAndPush, 0)), &report); err != nil {
		t.Fatalf("the pushed result is not the commit report: %v", err)
	}
	if tests := report.Tests; tests.Passed != 3 || tests.Failed != 0 || tests.Flows != 1 || len(tests.Uncovered) != 1 || !strings.Contains(tests.Uncovered[0], "slack") {
		t.Fatalf("the pushed commit's tests stage did not run the thread approval's tests: %+v", report)
	}
	if head := session.summary(ctx).Identity.HeadCommit; head != report.Commit {
		t.Fatalf("the Session's HEAD is %s, not the pushed %s", head, report.Commit)
	}
	t.Logf("FlowID=%s pushed the Slack thread approval as %s; tests %+v", session.id, report.Commit, report.Tests)
}
