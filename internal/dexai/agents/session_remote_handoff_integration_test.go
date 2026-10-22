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
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{BridgeVersion: bridgeapi.DevBridgeVersion, Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "short-handoff", HostLabel: "integration host"}}, &active, http.StatusOK)
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
		if message.Content == remoteexec.HandoffNoticeLabel && !message.ExecutorChanged {
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
	switches := 0
	for _, item := range finished.History.Messages {
		if item.Message.ExecutorChanged {
			switches++
		}
	}
	if switches != 2 {
		t.Fatalf("visible executor boundaries=%d, want initial local and Platform", switches)
	}
	// Platform mode parks the exact pairing; HTTP activation cannot take it back.
	listed, err = api.ListRemoteAgentConnections(ctx, &pb.ListRemoteAgentConnectionsRequest{ProjectId: session.identity.ProjectID, Caller: caller})
	if err != nil || len(listed.GetConnections()) != 1 || listed.GetConnections()[0].GetStatus() != pb.RemoteAgentConnectionStatus_REMOTE_AGENT_CONNECTION_STATUS_PAIRED {
		t.Fatalf("Platform switch lost the paired connection: %v", err)
	}
	connectionID := listed.GetConnections()[0].GetConnectionId()
	var idle bridgeapi.SyncResponse
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, After: active.LastSequence}, &idle, http.StatusOK)
	if !idle.Idle || len(idle.Entries) != 0 || idle.ExaminedThrough != active.LastSequence || idle.AgentContext != "" {
		t.Fatal("idle sync delivered work or moved the Bridge cursor")
	}
	var repeated bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{BridgeVersion: bridgeapi.DevBridgeVersion, Binding: bridgeapi.Binding{ProviderThreadID: "short-handoff"}}, &repeated, http.StatusOK)
	if !repeated.Idle || repeated.Epoch != idle.Epoch {
		t.Fatal("returning activation reclaimed Platform authority")
	}
	var refused bridgeapi.ErrorBody
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence + 1, TurnCompleted: true}, &refused, http.StatusConflict)
	if refused.Code != bridgeapi.ErrorExecutorChanged {
		t.Fatal("idle report was admitted")
	}
	resume := &pb.SetSessionExecutorRequest{ProjectId: session.identity.ProjectID, BasisEpoch: idle.Epoch, ConnectionId: connectionID, Caller: caller}
	if _, err = api.SetSessionExecutor(ctx, resume); err != nil {
		t.Fatal(err)
	}
	resumedSnapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	last := resumedSnapshot.Agent.History.Messages[len(resumedSnapshot.Agent.History.Messages)-1].Message
	if !last.ExecutorChanged || last.Content != "Switched to the local agent" {
		t.Fatal("resume lost its canonical visible turn boundary")
	}
	// The same browser command after an uncertain response joins this activation.
	if _, err = api.SetSessionExecutor(ctx, resume); err != nil {
		t.Fatal(err)
	}
	// Decode each response into a fresh value: false idle is omitted on the wire.
	repeated = bridgeapi.ActivateResponse{}
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{BridgeVersion: bridgeapi.DevBridgeVersion, Binding: bridgeapi.Binding{ProviderThreadID: "short-handoff"}}, &repeated, http.StatusOK)
	if repeated.Idle || repeated.Epoch <= idle.Epoch || repeated.Generation <= idle.Generation {
		t.Fatal("resume did not create a new fenced remote epoch")
	}
	var history bridgeapi.SyncResponse
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, After: active.LastSequence}, &history, http.StatusOK)
	foundPlatformReply := false
	for _, entry := range history.Entries {
		if entry.Role == bridgeapi.EntryRoleAssistant && strings.Contains(entry.Text, "Remote handoff replay is coherent") {
			foundPlatformReply = true
			if entry.Sequence > history.ActivationSequence {
				t.Fatal("historical Platform reply passed the resume head")
			}
		}
	}
	if history.Idle || history.Epoch != repeated.Epoch || !foundPlatformReply {
		t.Fatal("same connection cannot recover the bounded public Platform history")
	}
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence + 1, TurnCompleted: true}, &refused, http.StatusConflict)
	if refused.Code != bridgeapi.ErrorExecutorChanged {
		t.Fatal("old epoch report survived same-ID resume")
	}
	checkpoint := session.remoteModelCheckpoint(ctx)
	session.send(ctx, "A new message after local-agent resume")
	session.await(ctx, "post-resume message appended", func() bool {
		value, err := session.client.GetSnapshot(ctx, session.id)
		if err != nil {
			return false
		}
		for _, item := range value.Agent.History.Messages {
			if item.Sequence > conversation.Sequence(repeated.LastSequence) && strings.Contains(item.Message.Content, "A new message after local-agent resume") {
				return true
			}
		}
		return false
	})
	var next bridgeapi.SyncResponse
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: repeated.Epoch, After: repeated.LastSequence}, &next, http.StatusOK)
	foundNewInput := false
	for _, entry := range next.Entries {
		if strings.Contains(entry.Text, "A new message after local-agent resume") {
			foundNewInput = true
			if entry.Sequence <= next.ActivationSequence {
				t.Fatal("post-resume input was classified as history")
			}
		}
		if strings.Contains(entry.Text, mockInput(t, "remote-handoff", 3).Content) {
			t.Fatal("Platform-era input was delivered as a new command")
		}
	}
	if !foundNewInput {
		t.Fatal("message sent after resume was lost")
	}
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: repeated.Epoch, Generation: repeated.Generation, Sequence: 1,
		Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindAssistantMessage, Text: "The same local connection resumed without pairing."}}}, &accepted, http.StatusOK)
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: repeated.Epoch, Generation: repeated.Generation, Sequence: 2, TurnCompleted: true}, &accepted, http.StatusOK)
	session.await(ctx, "resumed remote turn completed", func() bool {
		value, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && value.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage && value.Agent.Description.LastSequence > checkpoint.sequence
	})
	requireRemoteModelInterval(t, session.remoteModelCheckpoint(ctx), checkpoint)

}
