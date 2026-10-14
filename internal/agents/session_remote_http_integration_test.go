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
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/projects/projecttest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// This client is external user behavior, not a substitute application
// dependency. The existing lane owns HTTP, SQL, Dex, Sandbox, Git and storage.
func TestSessionRemoteHTTPAdmissionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	connection, err := grpc.NewClient(dexflowtest.Setting(t, "SUPERVERSE_TEST_API_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	caller := registerCaller(ctx, t, pb.NewAccountServiceClient(connection))
	owner := caller.GetAccountId()
	team := projecttest.TeamSpaceForTest(ctx, t, owner)
	session := newMockSessionIn(t, ctx, owner, team, t.Name(), "")
	api := pb.NewRemoteAgentServiceClient(connection)
	before, err := pb.NewSessionServiceClient(connection).GetSession(ctx, &pb.GetSessionRequest{ProjectId: session.identity.ProjectID, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	executor := before.GetSession().GetAgent().GetDescription().GetExecutor()
	if executor.GetMode() != pb.SessionExecutorMode_SESSION_EXECUTOR_MODE_PLATFORM || executor.GetEpoch() != 0 || executor.GetGeneration() != 0 {
		t.Fatalf("never-connected executor = %+v", executor)
	}
	pair, err := api.CreateRemoteAgentPairing(ctx, &pb.CreateRemoteAgentPairingRequest{ProjectId: session.identity.ProjectID, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	remote := remoteHTTPClient{t: t, endpoint: pair.GetEndpoint(), client: &http.Client{Timeout: 30 * time.Second}}
	var paired bridgeapi.PairResponse
	remote.post(ctx, bridgeapi.PathPair, bridgeapi.PairRequest{Code: pair.GetCode(), Bridge: bridgeapi.BridgeInfo{Version: bridgeapi.DevBridgeVersion, OS: "darwin", Arch: "arm64"}}, &paired, http.StatusOK)
	remote.token = paired.Token
	var active bridgeapi.ActivateResponse
	binding := bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "http-proof", ProviderThreadID: "http-proof", HostLabel: "integration host"}}
	remote.post(ctx, bridgeapi.PathActivate, binding, &active, http.StatusOK)
	if active.Epoch == 0 || active.Generation == 0 {
		t.Fatalf("activation has zero fence: epoch=%d generation=%d", active.Epoch, active.Generation)
	}
	var retry bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, binding, &retry, http.StatusOK)
	if retry.Epoch != active.Epoch || retry.Generation != active.Generation {
		t.Fatal("same-connection retry changed authority")
	}
	for _, tool := range active.Tools {
		switch conversation.ToolName(tool.Name) {
		case conversation.ToolStartAgent, conversation.ToolMergeAgentChanges, conversation.ToolSetEffort, conversation.ToolStartTimer:
			t.Fatalf("remote catalog exposed %s", tool.Name)
		}
	}
	if active.ToolSetDigest != agentloop.RemoteToolDigest() {
		t.Fatal("remote catalog digest differs from server catalog")
	}
	report := bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindAssistantMessage, Text: "Remote HTTP proof"}}}
	var zeroRefusal bridgeapi.ErrorBody
	zero := report
	zero.Generation = 0
	remote.post(ctx, bridgeapi.PathReport, zero, &zeroRefusal, http.StatusConflict)
	if zeroRefusal.Code != bridgeapi.ErrorStaleGeneration {
		t.Fatal("generation zero admitted")
	}
	var accepted bridgeapi.ReportResponse
	remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
	remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
	if !accepted.Duplicate || accepted.Reported != 1 {
		t.Fatal("duplicate had another admission")
	}
	gap := report
	gap.Sequence = 3
	var refused bridgeapi.ErrorBody
	remote.post(ctx, bridgeapi.PathReport, gap, &refused, http.StatusConflict)
	if refused.Code != bridgeapi.ErrorSequenceGap || refused.ExpectedSequence != 2 {
		t.Fatalf("gap refusal = %s expected=%d", refused.Code, refused.ExpectedSequence)
	}
	report.Sequence, report.Events = 2, nil
	report.ToolCall = &bridgeapi.ToolCall{Name: string(conversation.ToolWrite), Arguments: json.RawMessage(`{"path":"raexec-proof.txt","content":"Remote executor proof\n"}`)}
	remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
	remote.awaitResult(ctx, active.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-2", active.Epoch)), "")
	report.Sequence = 3
	report.ToolCall = &bridgeapi.ToolCall{Name: string(conversation.ToolRead), Arguments: json.RawMessage(`{"path":"raexec-proof.txt"}`)}
	remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
	remote.awaitResult(ctx, active.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-3", active.Epoch)), "Remote executor proof")
	for stop := 0; stop < 2; stop++ {
		if stop > 0 {
			current, err := session.client.ReadExecutor(ctx, session.identity.ProjectID)
			if err != nil {
				t.Fatal(err)
			}
			next := bridgeapi.Report{Epoch: active.Epoch, Generation: current.Generation, Sequence: 4, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: "Start the second remote turn"}}}
			remote.post(ctx, bridgeapi.PathReport, next, &accepted, http.StatusOK)
			session.await(ctx, "new remote turn", func() bool {
				snap, err := session.client.GetSnapshot(ctx, session.id)
				return err == nil && snap.Agent.Description.Status != agentloop.AgentStatusWaitingForMessage
			})
		}
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		if err != nil {
			t.Fatal(err)
		}
		generation := snapshot.Agent.Description.Executor.Generation
		if err = session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Actor: &conversation.Actor{AccountID: owner, Email: caller.GetCanonicalEmail()}, Turn: snapshot.Agent.Description.Turn}); err != nil {
			t.Fatal(err)
		}
		session.await(ctx, "remote Stop generation fence", func() bool {
			current, err := session.client.ReadExecutor(ctx, session.identity.ProjectID)
			return err == nil && current.Generation > generation
		})
		stale := bridgeapi.Report{Epoch: active.Epoch, Generation: generation, Sequence: uint64(4 + stop), TurnCompleted: true}
		remote.post(ctx, bridgeapi.PathReport, stale, &refused, http.StatusConflict)
		if refused.Code != bridgeapi.ErrorStaleGeneration {
			t.Fatalf("Stop %d stale refusal = %s", stop+1, refused.Code)
		}
		// A lost answer retried after Stop: the admitted sequence stays admitted.
		var replayed bridgeapi.ReportResponse
		remote.post(ctx, bridgeapi.PathReport, report, &replayed, http.StatusOK)
		if !replayed.Duplicate || replayed.Reported != uint64(3+stop) {
			t.Fatalf("Stop %d admitted-sequence replay was not a duplicate", stop+1)
		}
	}
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	replies := 0
	for _, entry := range snapshot.Agent.History.Messages {
		if entry.Message.ModelCall != nil {
			t.Fatal("remote mode recorded a platform model request")
		}
		if entry.Message.Content == "Remote HTTP proof" {
			replies++
		}
	}
	if replies != 1 {
		t.Fatalf("remote public reply persisted %d times", replies)
	}
	if snapshot.Agent.Description.ContextModel != "" {
		t.Fatal("remote mode opened a platform context segment")
	}
}
