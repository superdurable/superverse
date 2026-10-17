// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package diagnostics

import (
	"context"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// TestReadConnectorChecksMissingProjectDexIntegration verifies that the
// Session-scoped tool reads the real Dex contribution identity and refuses a
// missing contribution without creating an execution or consulting GitHub.
func TestReadConnectorChecksMissingProjectDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	flow := &contribution.ConnectorContributionFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, flow)
	runtime := newLocalRuntime(t)
	reader := &ProjectDiagnostics{Contributions: contribution.NewConnectorContributionFlowClientForTest(client, flow), Redactor: runtime.redactor}
	projectID, slug := flowid.RandomID(4), "stripe"
	flowID := flowid.ConnectorContributionFlowID(projectID, slug)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	dexflowtest.RequireAbsentForTest(ctx, t, client, flowID)
	result, err := reader.ReadDiagnostics(ctx, agentloop.SessionIdentity{ProjectID: projectID}, conversation.ToolCall{
		ID: "missing-connector-checks", Name: conversation.ToolReadConnectorChecks,
		Arguments: conversation.MustJSONObject(`{"slug":"stripe"}`),
	})
	if err != nil {
		t.Fatalf("read missing contribution: %s", dexflowtest.ErrorCategory(err))
	}
	if result.Outcome != conversation.ToolOutcomeKnownFailure || result.FailureCode != toolfailure.FlowNotFound.String() {
		t.Fatalf("missing contribution result: outcome=%s code=%s", result.Outcome, result.FailureCode)
	}
	dexflowtest.RequireAbsentForTest(ctx, t, client, flowID)
	t.Logf("FlowID=%s; missing read created no execution", flowID)
}
