// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// All application dependencies stay real; only model inference uses the approved mock.
func TestCustomEndpointSessionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Minute)
	defer cancel()
	logger := slog.Default()
	modelURL, officialProbe, stop, err := mockmodel.StartCustomEndpointProbeForTest(ctx, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	paths := strings.Split(os.Getenv("SUPERVERSE_CONFIG_FILE"), ",")
	settings, err := serverconfig.Load(paths...)
	if err != nil {
		t.Fatal("load the Native server configuration")
	}
	config, err := loadConfig(ctx, logger, settings)
	if err != nil {
		t.Fatalf("load Native providers (%T)", err)
	}
	// This test-only port is advertised to the real Dex Server, which reaches the host Worker.
	socket, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := socket.Addr().(*net.TCPAddr).Port
	_ = socket.Close()
	target := dex.WorkerTarget{Address: fmt.Sprintf("host.docker.internal:%d", port)}
	config.dexWorker.Pull = nil // This fixture owns a test-process push Worker.
	config.dexWorker.BindAddress = fmt.Sprintf(":%d", port)
	config.dexWorker.WorkerTarget = target
	config.dexClient.WorkerTarget = &target
	config.mockModel = false
	config.agent.Model = conversation.ModelMock
	config.agent.Mock = &modelprovider.MockClientConfig{ClaudeClientConfig: modelprovider.ClaudeClientConfig{APIKey: mockmodel.APIKeyForTest, BaseURL: modelURL}, CompatibleTransports: true}
	config.providers = modelprovider.ProviderConfig{Enabled: []conversation.Provider{conversation.ProviderAnthropic, conversation.ProviderOpenAI, conversation.ProviderXAI, conversation.ProviderGemini, conversation.ProviderQwen}, MockEndpoint: true, CustomEndpoints: true}
	config.agent.Claude.BaseURL = modelURL
	cacheRoot := t.TempDir()
	config.blobCache.Dir = filepath.Join(cacheRoot, "initial")
	config.rpcCoverageFile = os.Getenv("SUPERVERSE_TEST_RPC_COVERAGE_FILE")
	var owned resources
	t.Cleanup(func() {
		if err := owned.release(); err != nil {
			t.Errorf("release BYOK server resources (%T)", err)
		}
	})
	server, err := compose(ctx, config, logger, &owned)
	if err != nil {
		t.Fatalf("compose actual Native server (%T)", err)
	}
	workerEnded := make(chan error, 1)
	go func() { workerEnded <- server.worker.Start() }()
	t.Cleanup(func() {
		shutdown, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := server.worker.Stop(shutdown); err != nil {
			t.Errorf("stop BYOK Worker (%T)", err)
		}
		<-workerEnded
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	business := server.business
	served := make(chan error, 1)
	go func() { served <- business.Serve(listener) }()
	t.Cleanup(func() { business.Stop(); <-served })
	transport, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transport.Close() })
	db, err := postgres.Open(ctx, config.postgres)
	if err != nil {
		t.Fatal("open Native read model")
	}
	t.Cleanup(func() { _ = db.Close() })
	projectClient := &projects.ProjectFlowClient{Dex: server.dex, Flow: &projects.ProjectFlow{}, Projections: projectdao.New(db), ProjectIDLength: config.projectIDLength, PlatformAdminEmails: config.platformAdminEmails}

	reader := dexflowtest.ExecutionReaderForTest(t, settings.Dex.ServerAddress)
	sessions := sessionagent.NewSessionAgentFlowClientForTest(server.dex, &sessionagent.SessionAgentFlow{}, reader, conversation.ModelMock)
	credentials := pb.NewModelCredentialServiceClient(transport)
	agent := pb.NewAgentServiceClient(transport)
	for _, test := range []struct {
		model    conversation.Model
		wire     pb.AgentModel
		provider pb.ModelProvider
	}{
		{conversation.ModelAnthropicCustom, pb.AgentModel_AGENT_MODEL_ANTHROPIC_CUSTOM, pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC},
		{conversation.ModelOpenAICustom, pb.AgentModel_AGENT_MODEL_OPENAI_CUSTOM, pb.ModelProvider_MODEL_PROVIDER_OPENAI},
		{conversation.ModelXAICustom, pb.AgentModel_AGENT_MODEL_XAI_CUSTOM, pb.ModelProvider_MODEL_PROVIDER_XAI},
		{conversation.ModelGeminiCustom, pb.AgentModel_AGENT_MODEL_GEMINI_CUSTOM, pb.ModelProvider_MODEL_PROVIDER_GEMINI},
		{conversation.ModelQwenCustom, pb.AgentModel_AGENT_MODEL_QWEN_CUSTOM, pb.ModelProvider_MODEL_PROVIDER_QWEN},
	} {
		t.Run(string(test.model), func(t *testing.T) {
			modelURL, oldProbe, stop, err := mockmodel.StartCustomEndpointProbeForTest(ctx, logger)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			replacementURL, newProbe, stopNew, err := mockmodel.StartCustomEndpointProbeForTest(ctx, logger)
			if err != nil {
				t.Fatal(err)
			}
			defer stopNew()
			project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())
			_, caller := projecttest.LoginForTest(ctx, t, project.CreatorAccountID)
			id := agentloop.SessionFlowID(project.ProjectID)
			zero := uint64(0)
			listed, err := credentials.ListModelCredentialModels(ctx, &pb.ListModelCredentialModelsRequest{ProjectId: project.ProjectID, Provider: test.provider, ApiKey: mockmodel.APIKeyForTest, BaseUrl: modelURL + "/custom/one/v1", Caller: caller})
			if err != nil || len(listed.GetModelIds()) != 1 || listed.GetModelIds()[0] != mockmodel.CustomDiscoveredModelIDForTest {
				t.Fatalf("discover through real gRPC: %s", status.Code(err))
			}
			operation := flowid.RandomID(20)
			saved, err := credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project.ProjectID, Provider: test.provider, RequestId: operation, ExpectedRevision: &zero, ApiKey: mockmodel.APIKeyForTest, BaseUrl: modelURL + "/custom/one/v1", Caller: caller})
			if err != nil || saved.GetCredential().GetEffectiveModelId() != mockmodel.CustomDiscoveredModelIDForTest || saved.GetCredential().GetCustomModelId() != "" {
				t.Fatalf("save discovered endpoint: %s", status.Code(err))
			}
			beforeReplay := oldProbe.RequestsForTest()
			if _, err := credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project.ProjectID, Provider: test.provider, RequestId: operation, ExpectedRevision: &zero, ApiKey: mockmodel.APIKeyForTest, BaseUrl: modelURL + "/custom/timeout/v1", Caller: caller}); err != nil {
				t.Fatalf("replay should not rediscover endpoint: %s", status.Code(err))
			}
			if _, err := credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project.ProjectID, Provider: test.provider, RequestId: flowid.RandomID(20), ExpectedRevision: &zero, ApiKey: mockmodel.APIKeyForTest, BaseUrl: modelURL + "/custom/timeout/v1", Caller: caller}); status.Code(err) != codes.Aborted {
				t.Fatal("stale revision should refuse before discovery")
			}
			if oldProbe.RequestsForTest() != beforeReplay {
				t.Fatal("replay or stale revision made a provider request")
			}
			one := uint64(1)
			if _, err = credentials.ListModelCredentialModels(ctx, &pb.ListModelCredentialModelsRequest{ProjectId: project.ProjectID, Provider: test.provider, BaseUrl: modelURL + "/custom/one/v1", ExpectedRevision: &one, Caller: caller}); err != nil {
				t.Fatalf("stored-key discovery: %s", status.Code(err))
			}
			if _, err = credentials.ListModelCredentialModels(ctx, &pb.ListModelCredentialModelsRequest{ProjectId: project.ProjectID, Provider: test.provider, BaseUrl: replacementURL + "/custom/one/v1", ExpectedRevision: &one, Caller: caller}); status.Code(err) != codes.FailedPrecondition {
				t.Fatal("stored key was sent to a new URL")
			}
			if _, err = agent.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: project.ProjectID, Model: test.wire, ExpectedRevision: &zero, Caller: caller}); err != nil {
				t.Fatalf("select custom model: %s", status.Code(err))
			}
			send := func(number int) {
				t.Helper()
				if _, err := agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project.ProjectID, MessageId: flowid.RandomID(20), Text: mockInput(t, "custom-endpoint", number).Content + strings.Repeat(" retained context", 100), Caller: caller}); err != nil {
					t.Fatalf("send custom input: %s", status.Code(err))
				}
			}
			sendContinuation := func() {
				t.Helper()
				if _, err := agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project.ProjectID, MessageId: flowid.RandomID(20), Text: mockInput(t, "custom-endpoint-continuation", 1).Content + strings.Repeat(" retained context", 100), Caller: caller}); err != nil {
					t.Fatalf("send custom continuation: %s", status.Code(err))
				}
			}
			lastAwait := conversation.Sequence(0)
			await := func(text string) agentloop.AgentSnapshot {
				t.Helper()
				deadline := time.Now().Add(5 * time.Minute)
				for time.Now().Before(deadline) {
					snapshot, err := sessions.GetSnapshot(ctx, id)
					if err != nil {
						t.Fatalf("read Session snapshot: %T", err)
					}
					if snapshot.Agent != nil {
						for _, message := range snapshot.Agent.History.Messages {
							if message.Message.Role == conversation.MessageRoleSystem && strings.HasPrefix(message.Message.Content, "Model call failed") {
								t.Fatalf("model failure: %s", message.Message.Content)
							}
							if snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage && message.Sequence > lastAwait && strings.Contains(message.Message.Content, text) {
								lastAwait = snapshot.Agent.Description.LastSequence
								return *snapshot.Agent
							}
						}
					}
					select {
					case <-ctx.Done():
						t.Fatal("custom Session deadline")
					case <-time.After(200 * time.Millisecond):
					}
				}
				t.Fatal("custom Session did not complete")
				return agentloop.AgentSnapshot{}
			}
			send(1)
			first := await("The custom endpoint child completed")
			if first.Description.ContextModel != test.model || len(first.Description.Subagents) != 1 || first.Description.Subagents[0].Status != agenttasks.SubagentCompleted {
				t.Fatal("custom tool/child execution did not complete")
			}
			compacted := false
			for _, message := range first.History.Messages {
				compacted = compacted || message.Message.Content == agentloop.SegmentMarker(agentloop.SegmentReasonCompaction)
			}
			wireBytes, firstWasCompaction, compactions := oldProbe.CustomBudgetForTest()
			t.Logf("full first custom wire request=%d bytes; first compaction=%t; long-context compactions=%d", wireBytes, firstWasCompaction, compactions)
			if wireBytes <= 0 || firstWasCompaction || compactions == 0 {
				t.Fatal("empty context compacted or long context did not compact")
			}
			if !compacted {
				t.Fatal("custom Session never compacted")
			}
			// More than one CurrentMessages window forces exact archive loads at both boundaries.
			for turn := 0; turn < 12; turn++ {
				send(turn + 2)
				first = await("replacement continues with earlier context as text")
			}
			if first.Description.LastSequence <= 20 {
				t.Fatal("long-history regression did not reach archived entries")
			}
			beforeHandoff, err := sessions.GetSnapshot(ctx, id)
			if err != nil {
				t.Fatal("read handoff HEAD")
			}
			remote := pb.NewRemoteAgentServiceClient(transport)
			pairing, err := remote.CreateRemoteAgentPairing(ctx, &pb.CreateRemoteAgentPairingRequest{ProjectId: project.ProjectID, Caller: caller})
			if err != nil {
				t.Fatal("create long-history pairing")
			}
			endpoint := httptest.NewServer(server.remoteAgent)
			defer endpoint.Close()
			post := func(path, token string, input, output any) {
				t.Helper()
				raw, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL+path, bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Content-Type", "application/json")
				if token != "" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
				if err != nil {
					t.Fatal("remote HTTP request failed")
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output) != nil {
					t.Fatal("remote HTTP response failed")
				}
			}
			var paired bridgeapi.PairResponse
			post(bridgeapi.PathPair, "", bridgeapi.PairRequest{Code: pairing.GetCode(), Bridge: bridgeapi.BridgeInfo{Version: config.remoteAgent.MinimumBridgeVersion, OS: runtime.GOOS, Arch: runtime.GOARCH}}, &paired)
			var active bridgeapi.ActivateResponse
			post(bridgeapi.PathActivate, paired.Token, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "custom-long-history", HostLabel: "integration host"}}, &active)
			if _, err := remote.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: project.ProjectID, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
				t.Fatal("return long history to platform")
			}
			if _, err := agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project.ProjectID, MessageId: flowid.RandomID(20), Text: mockInput(t, "custom-endpoint-handoff", 1).Content + " " + mockmodel.ArgumentMarkerForTest("head", beforeHandoff.Session.Identity.HeadCommit) + strings.Repeat(" retained context", 100), Caller: caller}); err != nil {
				t.Fatal("send custom handoff input")
			}

			first = await("replacement continues with earlier context as text")
			if _, err := remote.RevokeRemoteAgentConnection(ctx, &pb.RevokeRemoteAgentConnectionRequest{ProjectId: project.ProjectID, ConnectionId: paired.ConnectionID, Caller: caller}); err != nil {
				t.Fatal("revoke long-history connection")
			}
			if _, err = credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project.ProjectID, Provider: test.provider, RequestId: flowid.RandomID(20), ExpectedRevision: &one, ApiKey: mockmodel.APIKeyForTest, BaseUrl: replacementURL + "/custom/one/v1", Caller: caller}); err != nil {
				t.Fatalf("replace custom endpoint: %s", status.Code(err))
			}
			retiredCalls, newCalls := oldProbe.RequestsForTest(), newProbe.RequestsForTest()
			sendContinuation()
			second := await("replacement continues with earlier context as text")
			postBytes, _, _ := newProbe.CustomBudgetForTest()
			profile := modelprovider.ProfileOf(test.model)
			if (int64(postBytes)+3)/4+profile.MaximumOutputTokens() > profile.HardLimit {
				t.Fatalf("first rollover request exceeds hard limit: %d wire bytes", postBytes)
			}
			for _, prior := range first.History.Messages {
				found := false
				for _, current := range second.History.Messages {
					if current.Sequence == prior.Sequence {
						found = true
						if !reflect.DeepEqual(current.Message, prior.Message) {
							t.Fatal("credential rollover rewrote prior conversation history")
						}
					}
				}
				if !found {
					t.Fatal("credential rollover lost prior conversation history")
				}
			}
			notice := false
			for _, message := range second.History.Messages {
				notice = notice || strings.Contains(message.Message.Content, "The model endpoint or key changed.")
			}
			if !notice || second.Description.LastSequence <= first.Description.LastSequence || oldProbe.RequestsForTest() != retiredCalls || newProbe.RequestsForTest() <= newCalls {
				t.Fatalf("replacement: notice=%t sequence advanced=%t retired calls=%d/%d replacement calls=%d/%d", notice, second.Description.LastSequence > first.Description.LastSequence, oldProbe.RequestsForTest(), retiredCalls, newProbe.RequestsForTest(), newCalls)
			}
			t.Logf("FlowID=%s custom=%s; real tools, child, compaction and endpoint replacement passed", id, test.model)
		})
	}
	t.Run("legacy-official", func(t *testing.T) {
		project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())
		_, caller := projecttest.LoginForTest(ctx, t, project.CreatorAccountID)
		id := agentloop.SessionFlowID(project.ProjectID)
		zero, one := uint64(0), uint64(1)
		if _, err := credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: flowid.RandomID(20), ExpectedRevision: &zero, ApiKey: mockmodel.APIKeyForTest, Caller: caller}); err != nil {
			t.Fatalf("seed official credential: %s", status.Code(err))
		}
		if _, err := agent.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: project.ProjectID, Model: pb.AgentModel_AGENT_MODEL_HAIKU_4_5, ExpectedRevision: &zero, Caller: caller}); err != nil {
			t.Fatal("select official model")
		}
		if _, err := agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project.ProjectID, MessageId: flowid.RandomID(20), Text: "Keep this official context.", Caller: caller}); err != nil {
			t.Fatal("start official context")
		}
		awaitCustomEndpointSnapshot(t, ctx, sessions, id, "The mock model answers only scenario inputs")
		// Seed exactly the pre-change optional field shape in this owned, idle test Session.
		values, err := reader.GetAttributes(ctx, &dexpb.GetAttributesRequest{FlowId: string(id), Keys: []string{"ContextSegment"}})
		if err != nil || len(values.GetAttributes()) != 1 {
			t.Fatal("read exact legacy segment")
		}
		value := values.GetAttributes()[0].GetValue()
		if blob := value.GetInternalBlobIdForObjValue(); blob != "" {
			loaded, err := reader.LoadBlobs(ctx, &dexpb.LoadBlobsRequest{Entries: []*dexpb.LoadBlobRequestEntry{{FlowId: string(id), BlobValue: value}}})
			if err != nil {
				t.Fatal("load exact legacy segment blob")
			}
			value = loaded.GetValues()[blob]
		}
		object := value.GetObjValue()
		if object == nil || object.Encoding != "json" || len(object.Payload) > 1<<20 {
			t.Fatal("legacy segment is not bounded inline JSON")
		}
		var segment map[string]json.RawMessage
		if json.Unmarshal(object.Payload, &segment) != nil {
			t.Fatal("decode legacy segment")
		}
		delete(segment, "endpoint")
		delete(segment, "credential_revision")
		payload, err := json.Marshal(segment)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = reader.SetAttributes(ctx, &dexpb.SetAttributesRequest{FlowId: string(id), RequestId: "legacy-segment-" + project.ProjectID, Attributes: []*dexpb.AttributeWrite{{Key: "ContextSegment", Value: &dexpb.Value{Kind: &dexpb.Value_ObjValue{ObjValue: &dexpb.EncodedObject{Encoding: "json", Payload: payload}}}}}}); err != nil {
			t.Fatal("seed legacy optional field shape")
		}
		customURL, newProbe, stopCustom, err := mockmodel.StartCustomEndpointProbeForTest(ctx, logger)
		if err != nil {
			t.Fatal(err)
		}
		defer stopCustom()
		if _, err = credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: flowid.RandomID(20), ExpectedRevision: &one, ApiKey: mockmodel.APIKeyForTest, BaseUrl: customURL + "/custom/one/v1", Caller: caller}); err != nil {
			t.Fatalf("replace legacy official endpoint: %s", status.Code(err))
		}
		if _, err = agent.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: project.ProjectID, Model: pb.AgentModel_AGENT_MODEL_ANTHROPIC_CUSTOM, ExpectedRevision: &one, Caller: caller}); err != nil {
			t.Fatal("select custom from legacy official context")
		}
		before, retiredCalls := newProbe.RequestsForTest(), officialProbe.RequestsForTest()
		if _, err = agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project.ProjectID, MessageId: flowid.RandomID(20), Text: mockInput(t, "custom-endpoint", 1).Content, Caller: caller}); err != nil {
			t.Fatal("continue legacy context")
		}
		snapshot := awaitCustomEndpointSnapshot(t, ctx, sessions, id, "The custom endpoint child completed")
		notice := false
		for _, message := range snapshot.History.Messages {
			notice = notice || message.Message.Content == conversation.CredentialContextNotice
		}
		if !notice || newProbe.RequestsForTest() <= before || officialProbe.RequestsForTest() != retiredCalls {
			t.Fatal("legacy official context did not roll over to the custom endpoint")
		}
		t.Logf("FlowID=%s; official segment without optional fields safely continued through the custom endpoint", id)
	})

	t.Run("context", func(t *testing.T) {
		for _, wire := range []struct {
			model    pb.AgentModel
			provider pb.ModelProvider
		}{
			{pb.AgentModel_AGENT_MODEL_ANTHROPIC_CUSTOM, pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC},
			{pb.AgentModel_AGENT_MODEL_OPENAI_CUSTOM, pb.ModelProvider_MODEL_PROVIDER_OPENAI},
			{pb.AgentModel_AGENT_MODEL_QWEN_CUSTOM, pb.ModelProvider_MODEL_PROVIDER_QWEN},
		} {
			for _, legacy := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/legacy=%t", wire.provider, legacy), func(t *testing.T) {
					endpointURL, probe, stopEndpoint, err := mockmodel.StartCustomEndpointProbeForTest(ctx, logger)
					if err != nil {
						t.Fatal(err)
					}
					defer stopEndpoint()
					project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())
					_, caller := projecttest.LoginForTest(ctx, t, project.CreatorAccountID)
					id, zero := agentloop.SessionFlowID(project.ProjectID), uint64(0)
					provider := modelcredential.ForModel(conversation.ModelAnthropicCustom)
					if wire.provider == pb.ModelProvider_MODEL_PROVIDER_OPENAI {
						provider = modelcredential.ForModel(conversation.ModelOpenAICustom)
					}
					if wire.provider == pb.ModelProvider_MODEL_PROVIDER_QWEN {
						provider = modelcredential.ForModel(conversation.ModelQwenCustom)
					}
					request := &pb.SetModelCredentialRequest{ProjectId: project.ProjectID, Provider: wire.provider, RequestId: flowid.RandomID(20), ExpectedRevision: &zero, ApiKey: mockmodel.APIKeyForTest, BaseUrl: endpointURL + "/custom/one/v1", Caller: caller}
					if !legacy {
						request.ContextWindow, request.MaxOutputTokens = 131072, 8192
					}
					// Bounds fail before any network probe or writer admission.
					for _, invalid := range []struct{ context, output int64 }{{16384, 0}, {1000001, 0}, {32768, 4097}, {131072, -1}} {
						bad := proto.Clone(request).(*pb.SetModelCredentialRequest)
						bad.RequestId = flowid.RandomID(20)
						bad.ContextWindow, bad.MaxOutputTokens = invalid.context, invalid.output
						if _, err := credentials.SetModelCredential(ctx, bad); status.Code(err) != codes.InvalidArgument {
							t.Fatalf("invalid budget admitted: %s", status.Code(err))
						}
					}
					saved, err := credentials.SetModelCredential(ctx, request)
					if err != nil {
						t.Fatalf("save context: %s", status.Code(err))
					}
					wantContext, wantOutput := int64(131072), int64(8192)
					if legacy {
						wantContext, wantOutput = 32768, 4096
					}
					if saved.GetCredential().GetContextWindow() != wantContext || saved.GetCredential().GetMaxOutputTokens() != wantOutput {
						t.Fatal("normalized budget metadata mismatch")
					}
					replay := proto.Clone(request).(*pb.SetModelCredentialRequest)
					replay.ContextWindow, replay.MaxOutputTokens = 262144, 16384
					replayed, err := credentials.SetModelCredential(ctx, replay)
					if err != nil || replayed.GetCredential().GetContextWindow() != wantContext || replayed.GetCredential().GetMaxOutputTokens() != wantOutput {
						t.Fatal("operation replay changed its admitted budget")
					}

					if !legacy && wire.provider == pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC {
						info, err := sessions.ReadModelCredentialInfo(ctx, id, provider)
						if err != nil {
							t.Fatal(err)
						}
						valid, err := json.Marshal(info)
						if err != nil {
							t.Fatal(err)
						}
						info.Endpoint.ContextWindow = 16384
						invalid, err := json.Marshal(info)
						if err != nil {
							t.Fatal(err)
						}
						writeMetadata := func(operation string, payload []byte) {
							t.Helper()
							_, err := reader.SetAttributes(ctx, &dexpb.SetAttributesRequest{FlowId: string(id), RequestId: operation + "-" + project.ProjectID, Attributes: []*dexpb.AttributeWrite{{Key: "ModelKeyInfo/" + provider.String(), Value: &dexpb.Value{Kind: &dexpb.Value_ObjValue{ObjValue: &dexpb.EncodedObject{Encoding: "json", Payload: payload}}}}}})
							if err != nil {
								t.Fatal("write owned metadata fixture")
							}
						}
						// A corrupt nonzero declaration must not masquerade as an omitted default.
						writeMetadata("invalid-budget", invalid)
						_, readErr := credentials.GetModelCredentialInfo(ctx, &pb.GetModelCredentialInfoRequest{ProjectId: project.ProjectID, Provider: wire.provider, Caller: caller})
						writeMetadata("restore-budget", valid)
						if status.Code(readErr) != codes.Internal {
							t.Fatalf("invalid stored budget read: %s", status.Code(readErr))
						}
					}
					if legacy {
						info, err := sessions.ReadModelCredentialInfo(ctx, id, provider)
						if err != nil {
							t.Fatal(err)
						}
						info.Endpoint.ContextWindow, info.Endpoint.MaxOutputTokens = 0, 0
						payload, err := json.Marshal(info)
						if err != nil {
							t.Fatal(err)
						}
						// This owned idle fixture deliberately reproduces an existing key's absent optional fields.
						_, err = reader.SetAttributes(ctx, &dexpb.SetAttributesRequest{FlowId: string(id), RequestId: "legacy-budget-" + project.ProjectID, Attributes: []*dexpb.AttributeWrite{{Key: "ModelKeyInfo/" + provider.String(), Value: &dexpb.Value{Kind: &dexpb.Value_ObjValue{ObjValue: &dexpb.EncodedObject{Encoding: "json", Payload: payload}}}}}})
						if err != nil {
							t.Fatal("seed legacy metadata")
						}
					}
					if _, err := agent.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: project.ProjectID, Model: wire.model, ExpectedRevision: &zero, Caller: caller}); err != nil {
						t.Fatal(err)
					}
					send := func(number int, text string) agentloop.AgentSnapshot {
						t.Helper()
						if _, err := agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project.ProjectID, MessageId: flowid.RandomID(20), Text: mockInput(t, "custom-context", number).Content, Caller: caller}); err != nil {
							t.Fatal(err)
						}
						return awaitCustomEndpointSnapshot(t, ctx, sessions, id, text)
					}
					send(1, "The first custom context boundary completed.")
					_, _, compactions := probe.CustomBudgetForTest()
					if legacy && compactions != 1 || !legacy && compactions != 0 {
						t.Fatalf("23K boundary compacted %d times, legacy=%t", compactions, legacy)
					}
					if probe.CustomOutputBudgetForTest() != wantOutput {
						t.Fatalf("wire output budget=%d, want %d", probe.CustomOutputBudgetForTest(), wantOutput)
					}
					if !legacy {
						send(2, "The declared custom compaction completed.")
						_, _, after := probe.CustomBudgetForTest()
						if after != compactions+1 {
							t.Fatalf("91K boundary compactions=%d", after)
						}
						snapshot := send(3, "The declared hard limit recovered without a summary.")
						unsummarized := false
						for _, message := range snapshot.History.Messages {
							unsummarized = unsummarized || message.Message.Content == agentloop.SegmentMarker(agentloop.SegmentReasonUnsummarized)
						}
						if !unsummarized {
							t.Fatal("99K failed compaction did not recover through the declared hard limit")
						}
						childSnapshot := send(4, "The declared context child completed.")
						if len(childSnapshot.Description.Subagents) != 1 {
							t.Fatal("declared budget child missing")
						}
						children := subagent.NewSubagentFlowClientForTest(server.dex, &subagent.SubagentFlow{}, reader)
						child, err := children.SnapshotForTest(ctx, agenttasks.SubagentIdentity{ProjectID: project.ProjectID, AgentID: childSnapshot.Description.Subagents[0].AgentID})
						if err != nil || child.Config == nil || child.Config.Endpoint.ContextWindow != wantContext || child.Config.Endpoint.MaxOutputTokens != wantOutput {
							t.Fatal("child did not freeze the declared endpoint budgets")
						}

					}
					info, err := credentials.GetModelCredentialInfo(ctx, &pb.GetModelCredentialInfoRequest{ProjectId: project.ProjectID, Provider: wire.provider, Caller: caller})
					if err != nil || info.GetCredential().GetContextWindow() != wantContext || info.GetCredential().GetMaxOutputTokens() != wantOutput {
						t.Fatal("saved/legacy metadata did not round trip")
					}
					t.Logf("FlowID=%s; context=%d output=%d compaction=%d hard=%d legacy=%t", id, wantContext, wantOutput, wantContext*11/16, wantContext*3/4, legacy)
				})
			}
		}
	})

	if directory := os.Getenv("SUPERVERSE_TEST_BYOK_STUDIO_DIR"); directory != "" {
		t.Run("studio", func(t *testing.T) {
			runBYOKStudioSpec(t, ctx, listener.Addr().String(), directory, modelURL+"/custom/many/v1")
		})
	}

}

func awaitCustomEndpointSnapshot(t testing.TB, ctx context.Context, sessions *sessionagent.SessionAgentFlowClient, id agentloop.FlowID, text string) agentloop.AgentSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		snapshot, err := sessions.GetSnapshot(ctx, id)
		if err != nil {
			t.Fatalf("read Session snapshot: %T", err)
		}
		if snapshot.Agent != nil {
			for _, message := range snapshot.Agent.History.Messages {
				if message.Message.Role == conversation.MessageRoleSystem && strings.HasPrefix(message.Message.Content, "Model call failed") {
					t.Fatalf("model failure: %s", message.Message.Content)
				}
				if snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage && strings.Contains(message.Message.Content, text) {
					return *snapshot.Agent
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("custom Session deadline")
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("custom Session did not complete")
	return agentloop.AgentSnapshot{}
}
