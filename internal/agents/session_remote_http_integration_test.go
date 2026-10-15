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
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
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
	var firstContext bridgeapi.SyncResponse
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, Context: true}, &firstContext, http.StatusOK)
	if firstContext.AgentContextState != bridgeapi.AgentContextPending {
		t.Fatalf("first remote context state = %q, want pending", firstContext.AgentContextState)
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
	var synced bridgeapi.SyncResponse
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, Context: true}, &synced, http.StatusOK)
	for _, name := range []conversation.ToolName{
		conversation.ToolStartAgent, conversation.ToolMergeAgentChanges,
		conversation.ToolSetEffort, conversation.ToolStartTimer,
	} {
		if strings.Contains(synced.AgentContext, string(name)) {
			t.Fatalf("remote agent context exposed platform-only tool %s", name)
		}
	}
	if strings.Contains(synced.AgentContext, "A subagent is a task too") {
		t.Fatal("remote agent context exposed platform subagent task instructions")
	}
	for _, passage := range []string{
		"Choose connectors with `connector_catalog` and the Dex skills.",
		"remote mode cannot contribute connectors yet",
		"`request_user_input`",
		"propose a workaround if one exists",
	} {
		if !strings.Contains(synced.AgentContext, passage) {
			t.Fatalf("remote agent context lacks %q", passage)
		}
	}
	platformPrompt := agentloop.SessionPromptFor(conversation.ModelMock).Prompts.System
	if !strings.Contains(platformPrompt, "## Choosing and contributing connectors\nFor a new application or a large change, start one connector_select subagent") ||
		!strings.Contains(platformPrompt, "start one connector_develop subagent per gap") {
		t.Fatal("platform model segment lost connector instructions")
	}
	report := bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1,
		ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolRead), Arguments: json.RawMessage(`{"path":"AGENTS.md"}`)}}
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
	// A tool reported during workspace preparation joins that request and executes on its lease.
	for {
		var current bridgeapi.SyncResponse
		remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, Context: true}, &current, http.StatusOK)
		if current.AgentContextState == bridgeapi.AgentContextReady && current.SettledThrough >= 1 {
			if !strings.Contains(current.AgentContext, "<repository_context>") || !strings.Contains(current.AgentContext, "AGENTS.md") {
				t.Fatal("ready remote context lacks the repository section and AGENTS.md")
			}
			break
		}
		if current.AgentContextState == bridgeapi.AgentContextUnavailable {
			t.Fatal("fresh workspace could not prepare repository context")
		}
		select {
		case <-ctx.Done():
			t.Fatal("remote repository context did not become ready")
		case <-time.After(100 * time.Millisecond):
		}
	}
	remote.awaitResult(ctx, active.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-1", active.Epoch)), "")
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
	publicReply := bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 4,
		Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindAssistantMessage, Text: "Remote HTTP proof"}}}
	remote.post(ctx, bridgeapi.PathReport, publicReply, &accepted, http.StatusOK)
	for stop := 0; stop < 2; stop++ {
		if stop > 0 {
			current, err := session.client.ReadExecutor(ctx, session.identity.ProjectID)
			if err != nil {
				t.Fatal(err)
			}
			next := bridgeapi.Report{Epoch: active.Epoch, Generation: current.Generation, Sequence: 5, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: "Start the second remote turn"}}}
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
		stale := bridgeapi.Report{Epoch: active.Epoch, Generation: generation, Sequence: uint64(5 + stop), TurnCompleted: true}
		remote.post(ctx, bridgeapi.PathReport, stale, &refused, http.StatusConflict)
		if refused.Code != bridgeapi.ErrorStaleGeneration {
			t.Fatalf("Stop %d stale refusal = %s", stop+1, refused.Code)
		}
		// A lost answer retried after Stop: the admitted sequence stays admitted.
		var replayed bridgeapi.ReportResponse
		remote.post(ctx, bridgeapi.PathReport, report, &replayed, http.StatusOK)
		if !replayed.Duplicate || replayed.Reported != uint64(4+stop) {
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
	// Reentering remote mode with stored context must not schedule another
	// RequestSessionWorkspace Step, even when the existing lease is usable.
	workspaceBefore := session.workspace(ctx)
	requestSteps := func() int {
		t.Helper()
		state, err := session.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(session.id)})
		if err != nil {
			t.Fatal(err)
		}
		stepType := dex.GetFinalStepType(sessionagent.RequestSessionWorkspace{})
		count := 0
		for _, step := range state.GetCompletedSteps() {
			if step.GetCompletedStepType() == stepType {
				count++
			}
		}
		for _, step := range state.GetActiveStepExecutions() {
			if step.GetStepType() == stepType {
				count++
			}
		}
		for _, step := range state.GetQueuedSteps() {
			if step.GetStepType() == stepType {
				count++
			}
		}
		return count
	}
	requestsBefore := requestSteps()
	switched, err := session.client.SetExecutor(ctx, remoteexec.SetRequest{ProjectID: session.identity.ProjectID, BasisEpoch: active.Epoch,
		Mode: remoteexec.ModePlatform, Owner: conversation.Actor{AccountID: owner, Email: caller.GetCanonicalEmail()}, Reason: remoteexec.HandoffSwitched})
	if err != nil || !switched.Accepted {
		t.Fatalf("switch to platform failed: %v", err)
	}
	reentrant := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var reactivated bridgeapi.ActivateResponse
	reentrant.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "http-proof-return", ProviderThreadID: "http-proof-return", HostLabel: "integration host"}}, &reactivated, http.StatusOK)
	if reactivated.Epoch <= active.Epoch {
		t.Fatal("remote reactivation did not advance executor epoch")
	}
	if after := session.workspace(ctx); after.RequestID != workspaceBefore.RequestID || requestSteps() != requestsBefore {
		t.Fatal("remote reactivation with repository context requested the workspace again")
	}
}
