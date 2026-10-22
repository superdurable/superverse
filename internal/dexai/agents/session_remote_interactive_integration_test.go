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
	"strings"
	"testing"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Interactive mock history validates public units without claiming scripted
// replay binding. HTTP, SQL, Dex and the write/read Sandbox effects stay real.
func TestSessionRemoteInteractiveHandoffHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "interactive-handoff", ProviderThreadID: "interactive-handoff", HostLabel: "integration host"}}, &active, http.StatusOK)
	var accepted bridgeapi.ReportResponse
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: "Write the unscripted remote proof."}}}, &accepted, http.StatusOK)
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 2, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindAssistantMessage, Text: "Writing the real proof through the remote endpoint."}}}, &accepted, http.StatusOK)
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 3, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolWrite), Arguments: json.RawMessage(`{"path":"raexec-interactive.txt","content":"Interactive remote proof\n"}`)}}, &accepted, http.StatusOK)
	remote.awaitResult(ctx, active.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-3", active.Epoch)), "Created raexec-interactive.txt")
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 4, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolRead), Arguments: json.RawMessage(`{"path":"raexec-interactive.txt"}`)}}, &accepted, http.StatusOK)
	remote.awaitResult(ctx, active.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-4", active.Epoch)), "Interactive remote proof")
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 5, TurnCompleted: true}, &accepted, http.StatusOK)
	session.await(ctx, "interactive remote reached idle", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	before, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: session.identity.ProjectID, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	session.send(ctx, "Continue after the interactive remote handoff.")
	finished := session.finishedTurnAfter(ctx, before.Agent.Description.LastSequence)
	requireNoFailure(t, finished.History)
	last := finished.History.Messages[len(finished.History.Messages)-1].Message
	if !strings.Contains(last.Content, "The mock model answers only scenario inputs") {
		t.Fatal("the validated interactive path changed the normal default mock reply")
	}
	if finished.Description.Executor.Mode != remoteexec.ModePlatform || finished.Description.Executor.Handoff != nil {
		t.Fatal("the interactive switch did not consume its deferred handoff")
	}
	markers, calls, results := 0, 0, 0
	for _, item := range finished.History.Messages {
		message := item.Message
		if message.Content == remoteexec.HandoffNoticeLabel {
			markers++
		}
		for _, call := range message.ToolCalls {
			if remoteexec.IsCallID(active.Epoch, call.ID) {
				calls++
			}
		}
		if message.ToolCallID != nil && remoteexec.IsCallID(active.Epoch, *message.ToolCallID) {
			results++
			if message.ToolExecution == nil || message.ToolExecution.Outcome != conversation.ToolOutcomeSucceeded {
				t.Fatal("interactive remote history lost a successful canonical result")
			}
		}
	}
	if markers != 1 || calls != 2 || results != 2 {
		t.Fatal("interactive handoff lost complete units or duplicated public history")
	}
}
