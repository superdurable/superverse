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
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
)

// The ordered mock script substitutes only the model. The remote HTTP endpoint,
// Dex, SQL, actual Sandbox write/read, Git and storage belong to the real lane.
func TestSessionRemoteShortHandoffHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	session.send(ctx, mockInput(t, "remote-handoff", 1).Content)
	initial := session.finishedTurn(ctx)
	if !strings.Contains(initial.History.Messages[len(initial.History.Messages)-1].Message.Content, "platform prelude is complete") {
		t.Fatal("platform prelude did not end before activation")
	}
	platformBefore := session.remoteModelCheckpoint(ctx)
	requireModelPositiveControl(t, platformBefore.events)
	before, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	head := before.Session.Identity.HeadCommit
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	listed, err := api.ListRemoteAgentConnections(ctx, &pb.ListRemoteAgentConnectionsRequest{ProjectId: session.identity.ProjectID, Caller: caller})
	if err != nil || len(listed.GetConnections()) != 1 || listed.GetCurrentConnectionId() != "" || listed.GetConnections()[0].GetStatus() != pb.RemoteAgentConnectionStatus_REMOTE_AGENT_CONNECTION_STATUS_PAIRED {
		t.Fatalf("the real API did not list the paired but inactive remote connection: %v", err)
	}
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "short-handoff", HostLabel: "integration host"}}, &active, http.StatusOK)
	if active.Epoch != 1 || active.Generation != 1 {
		t.Fatal("fresh private fixture does not have the exact scripted remote identity")
	}
	var accepted bridgeapi.ReportResponse
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: mockInput(t, "remote-handoff", 2).Content}}}, &accepted, http.StatusOK)
	if accepted.Reported != 1 {
		t.Fatal("remote scripted input was not admitted at its exact sequence")
	}
	expected := mockmodel.RemoteReplyForTest("remote-handoff", 2, 1)
	sequence := uint64(2)
	if expected.Text != "" {
		remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindAssistantMessage, Text: expected.Text}}}, &accepted, http.StatusOK)
		sequence++
	}
	for _, call := range expected.ToolCalls {
		if call.ID != conversation.CallID(fmt.Sprintf("remote-%d-%d", active.Epoch, sequence)) {
			t.Fatal("actual report admission does not match its exact scripted identity")
		}
		remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence, ToolCall: &bridgeapi.ToolCall{Name: string(call.Name), Arguments: json.RawMessage(call.Arguments.String())}}, &accepted, http.StatusOK)
		if accepted.Reported != sequence {
			t.Fatal("the exact scripted report was not admitted")
		}
		remote.awaitResult(ctx, active.Epoch, call.ID, "Created raexec-handoff.txt")
		sequence++
	}
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence, TurnCompleted: true}, &accepted, http.StatusOK)
	session.await(ctx, "remote completed at idle boundary", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	remoteAfter := session.remoteModelCheckpoint(ctx)
	requireRemoteModelInterval(t, remoteAfter, platformBefore)
	requireRemoteExecutorCommits(t, remoteAfter, platformBefore, active.Epoch, active.Generation, false)
	if _, err = api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: session.identity.ProjectID, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	switchAfter := session.remoteModelCheckpoint(ctx)
	requireRemoteModelInterval(t, switchAfter, platformBefore)
	requireRemoteExecutorCommits(t, switchAfter, platformBefore, active.Epoch, active.Generation, true)
	session.send(ctx, mockInput(t, "remote-handoff", 3).Content+" "+mockmodel.ArgumentMarkerForTest("head", head))
	finished := session.finishedTurnAfter(ctx, remoteAfter.sequence)
	platformAfter := session.remoteModelCheckpoint(ctx)
	requireModelPositiveControl(t, platformAfter.since(t, switchAfter))
	if !strings.Contains(finished.History.Messages[len(finished.History.Messages)-1].Message.Content, "Remote handoff replay is coherent") {
		t.Fatal("strict model replay or real tool continuation failed")
	}
	handoffs, remoteCalls, remoteResults := 0, 0, 0
	for _, item := range finished.History.Messages {
		message := item.Message
		if message.Content == remoteexec.HandoffNoticeLabel {
			handoffs++
		}
		for _, call := range message.ToolCalls {
			if call.ID == conversation.CallID("remote-1-2") {
				remoteCalls++
			}
		}
		if message.ToolCallID != nil && *message.ToolCallID == conversation.CallID("remote-1-2") {
			remoteResults++
			if message.ToolExecution == nil || message.ToolExecution.Outcome != conversation.ToolOutcomeSucceeded {
				t.Fatal("remote handoff lost its canonical successful result")
			}
		}
	}
	if handoffs != 1 || remoteCalls != 1 || remoteResults != 1 {
		t.Fatal("handoff duplicated public history or lost a complete remote tool unit")
	}
	if finished.Description.Executor.Mode != "platform" || finished.Description.Executor.Handoff != nil {
		t.Fatal("the fresh platform segment did not consume its one handoff")
	}
}
