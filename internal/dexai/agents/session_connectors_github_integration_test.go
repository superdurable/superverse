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
	"os"
	"strings"
	"testing"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// TestSessionMockConnectorPullRequestRetryGitHubIntegration runs the
// connector-pr-retry scenario on a lane whose server names an upstream
// connectors library that the GitHub App's installation does not include, so
// that GitHub refuses every attempt to open the draft pull request: a real,
// deterministic refusal. The develop subagent builds mockretry and pushes its
// branch to the lane's library; the contribution's Flow records
// pull_request_failed with GitHub's reason, and its one connector
// notification, connector/mockretry/pull_request_failed/<head12>, opens an
// automatic turn in which the main agent tells the user. It is a manual test,
// like TestPlatformIssueGitHubIntegration: it needs the GitHub App's key on
// the lane's server and the lane started with github.connectors naming the
// repository of SUPERVERSE_TEST_CONNECTORS_REFUSING_UPSTREAM (a Local Native
// lane, whose local Git host takes the branch), and it fails, never skips,
// without them. Run it only when the connector contribution code changes.
func TestSessionMockConnectorPullRequestRetryGitHubIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("requires explicit isolated V2 GitHub effects")
	}
	if strings.Count(os.Getenv("SUPERVERSE_TEST_CONNECTORS_REFUSING_UPSTREAM"), "/") != 1 {
		t.Fatal("SUPERVERSE_TEST_CONNECTORS_REFUSING_UPSTREAM must name the owner/name the lane's github.connectors names, outside the App's installation")
	}
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, _ := startRecorded(t, ctx, mockInput(t, "connector-pr-retry", 1))
	record, _, state := session.connectorSubagent(ctx, agenttasks.SubagentConnectorDevelop)
	made := state.Outcome.Contribution
	if made == nil || made.Slug != "mockretry" || record.Connector == nil {
		t.Fatalf("the develop subagent contributed nothing: %+v, %+v", record, state.Outcome)
	}
	failed := session.awaitContribution(ctx, contribution.Identity{ProjectID: session.identity.ProjectID, Slug: "mockretry"}, func(record contribution.Record) bool {
		return record.Status == contribution.StatusPullRequestFailed
	})
	if failed.Record.Failure == "" || failed.Record.PullRequest != nil {
		t.Fatalf("the failed pull request is not recorded with its reason: %+v", failed.Record)
	}
	session.automaticTurn(ctx, "the connector notification", func(agentloop.AgentSnapshot) bool { return true }, func(notification *conversation.SessionNotification) bool {
		return notification.Kind == conversation.NotificationKindConnector
	})
	_, messages := session.history(ctx)
	var notifications []conversation.SequencedMessage
	for _, message := range ofRole(messages, conversation.MessageRoleNotification) {
		if notification := message.Message.Notification; notification != nil && notification.Kind == conversation.NotificationKindConnector {
			notifications = append(notifications, message)
		}
	}
	want := "connector/mockretry/pull_request_failed/" + made.HeadCommit[:12]
	if len(notifications) != 1 || notifications[0].Message.Notification.ID != want || notifications[0].Message.Notification.Event != conversation.ConnectorPullRequestFailed {
		t.Fatalf("expected the one connector notification %s, got %+v", want, notifications)
	}
	t.Logf("FlowID=%s recorded pull_request_failed: %s", session.id, failed.Record.Failure)
}
