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
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"net/http"
	"strings"
	"testing"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The checked-in Kind profiles bind the existing stateless mock in each replica.
const kindCustomEndpoint = "http://127.0.0.1:18083/custom/one/v1"

func TestCustomEndpointKindSessionDexIntegration(t *testing.T) {
	if dexflowtest.LocalLaneForTest(t) != dexflowtest.LaneKindForTest {
		t.Fatal("requires the configured Kind main Worker")
	}
	for _, candidate := range []struct {
		model    conversation.Model
		provider pb.ModelProvider
	}{
		{conversation.ModelAnthropicCustom, pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC},
		{conversation.ModelOpenAICustom, pb.ModelProvider_MODEL_PROVIDER_OPENAI},
		{conversation.ModelXAICustom, pb.ModelProvider_MODEL_PROVIDER_XAI},
		{conversation.ModelGeminiCustom, pb.ModelProvider_MODEL_PROVIDER_GEMINI},
		{conversation.ModelQwenCustom, pb.ModelProvider_MODEL_PROVIDER_QWEN},
	} {
		t.Run(string(candidate.model), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
			defer cancel()
			session := newSelectableSession(t, ctx, candidate.model, "")
			connection, caller := projecttest.LoginForTest(ctx, t, session.account)
			api := pb.NewModelCredentialServiceClient(connection)
			if candidate.provider == pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC {
				for _, probe := range []struct {
					path, reason string
					code         codes.Code
				}{{"probe-timeout", "timed out", codes.FailedPrecondition}} {
					revision := uint64(1)
					_, err := api.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: session.identity.ProjectID, Provider: candidate.provider, RequestId: flowid.RandomID(20), ExpectedRevision: &revision, ApiKey: mockmodel.APIKeyForTest, BaseUrl: "http://127.0.0.1:18083/custom/" + probe.path + "/v1", CustomModelId: mockmodel.CustomDiscoveredModelIDForTest, Caller: caller})
					if status.Code(err) != probe.code || !strings.Contains(status.Convert(err).Message(), probe.reason) {
						t.Fatalf("Kind probe %s: code=%s reason=%s", probe.path, status.Code(err), status.Convert(err).Message())
					}
				}
			}
			one := uint64(1)
			for _, test := range []struct {
				name   string
				count  int
				result pb.ModelDiscoveryResult
				code   codes.Code
			}{
				{"empty", 0, pb.ModelDiscoveryResult_MODEL_DISCOVERY_RESULT_EMPTY, codes.OK},
				{"one", 1, pb.ModelDiscoveryResult_MODEL_DISCOVERY_RESULT_SUPPORTED, codes.OK},
				{"many", 2, pb.ModelDiscoveryResult_MODEL_DISCOVERY_RESULT_SUPPORTED, codes.OK},
				{"unsupported", 0, pb.ModelDiscoveryResult_MODEL_DISCOVERY_RESULT_UNSUPPORTED, codes.OK},
				{"max-count", 100, pb.ModelDiscoveryResult_MODEL_DISCOVERY_RESULT_SUPPORTED, codes.OK},
				{"max-id", 1, pb.ModelDiscoveryResult_MODEL_DISCOVERY_RESULT_SUPPORTED, codes.OK},
				{"count", 0, 0, codes.ResourceExhausted}, {"id", 0, 0, codes.ResourceExhausted}, {"bytes", 0, 0, codes.ResourceExhausted},
			} {
				base := strings.Replace(kindCustomEndpoint, "/one/", "/"+test.name+"/", 1)
				response, err := api.ListModelCredentialModels(ctx, &pb.ListModelCredentialModelsRequest{ProjectId: session.identity.ProjectID, Provider: candidate.provider, BaseUrl: base, ApiKey: mockmodel.APIKeyForTest, Caller: caller})
				if status.Code(err) != test.code || err == nil && (len(response.GetModelIds()) != test.count || response.GetResult() != test.result) {
					t.Fatalf("Kind discovery %s: count=%d code=%s", test.name, len(response.GetModelIds()), status.Code(err))
				}
			}
			if _, err := api.ListModelCredentialModels(ctx, &pb.ListModelCredentialModelsRequest{ProjectId: session.identity.ProjectID, Provider: candidate.provider, BaseUrl: kindCustomEndpoint, ExpectedRevision: &one, Caller: caller}); err != nil {
				t.Fatalf("stored Kind discovery: %s", status.Code(err))
			}
			for _, base := range []string{"https://10.0.0.1/v1", "https://169.254.169.254/latest", "https://metadata.google.internal/v1"} {
				if _, err := api.ListModelCredentialModels(ctx, &pb.ListModelCredentialModelsRequest{ProjectId: session.identity.ProjectID, Provider: candidate.provider, BaseUrl: base, ApiKey: mockmodel.APIKeyForTest, Caller: caller}); status.Code(err) != codes.InvalidArgument {
					t.Fatal("Kind custom validator admitted a private or metadata target")
				}
			}
			session.send(ctx, mockInput(t, "custom-endpoint", 1).Content)
			snapshot := session.waitForSnapshot(ctx, "custom child report", func(value agentloop.AgentSnapshot) bool {
				if value.Description.Status != agentloop.AgentStatusWaitingForMessage {
					return false
				}
				for _, message := range value.History.Messages {
					if strings.Contains(message.Message.Content, "The custom endpoint child completed") {
						return true
					}
				}
				return false
			})
			requireNoFailure(t, snapshot.History)
			compacted := false
			for _, message := range snapshot.History.Messages {
				compacted = compacted || message.Message.Content == agentloop.SegmentMarker(agentloop.SegmentReasonCompaction)
			}
			if snapshot.Description.ContextModel != candidate.model || !compacted || len(snapshot.Description.Subagents) != 1 || snapshot.Description.Subagents[0].Status != agenttasks.SubagentCompleted {
				t.Fatal("Kind custom tool/child/compaction path did not complete")
			}
			state := session.awaitSubagentEnd(ctx, snapshot.Description.Subagents[0])
			if state.Config == nil || state.Config.Endpoint.BaseURL != kindCustomEndpoint || state.Config.Endpoint.EffectiveModelID != mockmodel.CustomDiscoveredModelIDForTest || state.Config.Endpoint.CustomProfileVersion != 1 {
				t.Fatal("Kind child did not freeze the effective custom selection")
			}
			snapshot = verifyKindCustomHistoryBoundaries(t, ctx, session, connection, caller, candidate.provider, snapshot)
			encoded, err := json.Marshal(snapshot)
			if err != nil || strings.Contains(string(encoded), mockmodel.APIKeyForTest) || strings.Contains(string(encoded), "opaque_replay") {
				t.Fatal("custom key or replay escaped the Kind public Snapshot")
			}
			t.Logf("FlowID=%s custom=%s; real Kind tools, child and compaction passed", session.id, candidate.model)
		})
	}
}

// The main Kind Worker executes the same archive retry plans as Native.
func verifyKindCustomHistoryBoundaries(t *testing.T, ctx context.Context, session *mockSession, connection *grpc.ClientConn, caller *pb.CallerContext, provider pb.ModelProvider, snapshot agentloop.AgentSnapshot) agentloop.AgentSnapshot {
	t.Helper()
	await := func(previous conversation.Sequence) agentloop.AgentSnapshot {
		t.Helper()
		value := session.finishedTurnAfter(ctx, previous)
		requireNoFailure(t, value.History)
		found := false
		for _, message := range value.History.Messages {
			found = found || message.Sequence > previous && message.Message.Role == conversation.MessageRoleAssistant && strings.Contains(message.Message.Content, "replacement continues with earlier context as text")
		}
		if !found {
			t.Fatal("Kind history boundary did not produce a fresh expected reply")
		}
		return value
	}
	for input := 2; input <= 13; input++ {
		session.send(ctx, mockInput(t, "custom-endpoint", input).Content+strings.Repeat(" retained context", 100))
		snapshot = await(snapshot.Description.LastSequence)
	}
	if snapshot.Description.LastSequence <= 20 {
		t.Fatal("Kind regression did not create archived history")
	}
	before, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	remoteAPI := pb.NewRemoteAgentServiceClient(connection)
	remote := pairRemoteHTTP(ctx, t, remoteAPI, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "custom-history", HostLabel: "integration host"}}, &active, http.StatusOK)
	if _, err := remoteAPI.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: session.identity.ProjectID, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	session.send(ctx, mockInput(t, "custom-endpoint-handoff", 1).Content+" "+mockmodel.ArgumentMarkerForTest("head", before.Session.Identity.HeadCommit)+strings.Repeat(" retained context", 100))
	snapshot = await(snapshot.Description.LastSequence)
	one := uint64(1)
	replacement := strings.Replace(kindCustomEndpoint, "/one/", "/unsupported/", 1)
	if _, err := pb.NewModelCredentialServiceClient(connection).SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: session.identity.ProjectID, Provider: provider, RequestId: flowid.RandomID(20), ExpectedRevision: &one, ApiKey: mockmodel.APIKeyForTest, BaseUrl: replacement, CustomModelId: mockmodel.CustomDiscoveredModelIDForTest, Caller: caller}); err != nil {
		t.Fatalf("replace Kind endpoint: %s", status.Code(err))
	}
	session.send(ctx, mockInput(t, "custom-endpoint-continuation", 1).Content+strings.Repeat(" retained context", 100))
	snapshot = await(snapshot.Description.LastSequence)
	listed, err := remoteAPI.ListRemoteAgentConnections(ctx, &pb.ListRemoteAgentConnectionsRequest{ProjectId: session.identity.ProjectID, Caller: caller})
	// Platform switching preserves the paired credential while suspending execution.
	if err != nil || len(listed.GetConnections()) != 1 || listed.GetConnections()[0].GetStatus() != pb.RemoteAgentConnectionStatus_REMOTE_AGENT_CONNECTION_STATUS_PAIRED {
		t.Fatalf("Kind switched connection lost pairing: code=%s count=%d, want OK and one paired connection", status.Code(err), len(listed.GetConnections()))
	}
	return snapshot
}
