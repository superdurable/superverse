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
	"fmt"
	"net/http"
	"testing"

	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

// The real foreground Sandbox command gives five reports time to queue behind
// it. The Session, not the test, owns their four-call wave and remaining call.
func TestSessionRemoteBatchCatalogHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "batch-catalog", ProviderThreadID: "batch-catalog", HostLabel: "integration host"}}, &active, http.StatusOK)
	var admitted bridgeapi.ReportResponse
	reportTool := func(sequence uint64, name conversation.ToolName, arguments string) {
		t.Helper()
		remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence, ToolCall: &bridgeapi.ToolCall{Name: string(name), Arguments: json.RawMessage(arguments)}}, &admitted, http.StatusOK)
	}
	callID := func(sequence int) conversation.CallID {
		return conversation.CallID(fmt.Sprintf("remote-%d-%d", active.Epoch, sequence))
	}
	reportTool(1, conversation.ToolWrite, `{"path":"raexec-batch.txt","content":"Batch proof\n"}`)
	remote.awaitResult(ctx, active.Epoch, callID(1), "")
	reportTool(2, conversation.ToolBash, `{"command":"sleep 30","description":"Hold the real foreground command while the next reports queue","timeout_seconds":120}`)
	session.await(ctx, "real foreground command admission", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && snapshot.Agent.Description.Status == agentloop.AgentStatusExecutingTool
	})
	for sequence := uint64(3); sequence <= 7; sequence++ {
		reportTool(sequence, conversation.ToolRead, `{"path":"raexec-batch.txt"}`)
	}
	// Server schema/catalog enforcement remains necessary even when a client
	// ignored the catalog it received at activation.
	reportTool(8, conversation.ToolStartAgent, `{}`)
	reportTool(9, conversation.ToolStopTask, `{"task_id":"agent-forbidden"}`)
	seen := make(map[string]bridgeapi.ToolResult)
	for len(seen) < 8 {
		var response bridgeapi.SyncResponse
		remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, After: remote.after, StateDigest: remote.digest}, &response, http.StatusOK)
		if response.ExaminedThrough < remote.after || response.ExaminedThrough > response.LastSequence {
			t.Fatal("batch sync cursor regressed or skipped beyond history")
		}
		remote.after, remote.digest = response.ExaminedThrough, response.StateDigest
		for _, entry := range response.Entries {
			for _, result := range entry.ToolResults {
				seen[result.CallID] = result
			}
		}
		if ctx.Err() != nil {
			t.Fatal("batch results did not settle before deadline")
		}
	}
	for sequence := 2; sequence <= 9; sequence++ {
		result, exists := seen[string(callID(sequence))]
		if !exists || result.IsError != (sequence >= 8) {
			t.Fatalf("tool %d has no correctly classified result", sequence)
		}
	}
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	boundedBatch := false
	for _, entry := range snapshot.Agent.History.Messages {
		calls := entry.Message.ToolCalls
		if len(calls) > 4 {
			t.Fatal("remote report batch exceeded four calls")
		}
		if len(calls) == 4 && calls[0].ID == callID(3) && calls[3].ID == callID(6) {
			boundedBatch = true
		}
	}
	if !boundedBatch || len(snapshot.Agent.Description.Subagents) != 0 {
		t.Fatal("queued head reports did not form exactly one four-call batch, or a forbidden subagent started")
	}
}
