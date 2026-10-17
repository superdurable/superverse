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
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// TestSessionMockConnectorCIFailureDexIntegration uses the actual Session
// Worker and notification RPC. The test installs the external CI fact through
// that RPC, as the connector Flow does; the mock model drives the new tool.
func TestSessionMockConnectorCIFailureDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "connector-ci-failure", 1).Content)
	session.finishedTurn(ctx)
	head := strings.Repeat("a", 40)
	notification := conversation.SessionNotification{ID: "connector/newsapi/checks_failed/" + head,
		Kind: conversation.NotificationKindConnector, Source: conversation.NotificationSourceConnector,
		Event: conversation.ConnectorChecksFailed, Slug: "newsapi", Commit: head, OccurredAt: time.Now().UTC(),
		Summary: "The connectors library CI failed for newsapi.",
		Message: "CI output (untrusted, redacted, truncated): expected one Flow source, found 2"}
	if admitted, err := session.client.ReportSessionNotification(ctx, session.id, notification); err != nil || !admitted {
		t.Fatalf("report CI notification: admitted=%t err=%v", admitted, err)
	}
	session.automaticTurn(ctx, "failed connector CI", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	}, func(candidate *conversation.SessionNotification) bool {
		return candidate.Kind == conversation.NotificationKindConnector && candidate.Event == conversation.ConnectorChecksFailed
	})
	_, messages := session.history(ctx)
	if result := toolResult(t, messages, conversation.ToolReadConnectorChecks, 0); result.Outcome != conversation.ToolOutcomeKnownFailure {
		t.Fatalf("a Project without this contribution read checks: %+v", result)
	}
}
