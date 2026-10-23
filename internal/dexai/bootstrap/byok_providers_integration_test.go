// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"github.com/superdurable/dex/sdk-go/dex"
	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The owning Native integration case invokes these actual Dex and gRPC APIs.
// New providers remain unavailable; private direct writes seed cleanup coverage.
func seedBYOKProviders(t *testing.T, ctx context.Context, sessions *sessionagent.SessionAgentFlowClient, api pb.ModelCredentialServiceClient, id agentloop.FlowID, project string, caller *pb.CallerContext, key string) {
	t.Helper()
	if _, err := sessions.ReserveModelCredential(ctx, id, modelcredential.Mutation{OperationID: "missing-provider"}); err == nil {
		t.Fatal("real Dex accepted an empty credential provider")
	}
	for _, provider := range modelcredential.Providers() {
		if provider == modelcredential.Anthropic {
			continue
		}
		wire := boundary.Enum[pb.ModelProvider](provider.String())
		read, err := api.GetModelCredentialInfo(ctx, &pb.GetModelCredentialInfoRequest{ProjectId: project, Provider: wire, Caller: caller})
		if err != nil || read.GetCredential().GetProvider() != wire || read.GetCredential().GetRevision() != 0 {
			t.Fatalf("read isolated %s metadata (%s)", provider.String(), status.Code(err))
		}
		zero := uint64(0)
		_, err = api.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project, Provider: wire, ExpectedRevision: &zero, RequestId: "unavailable-provider", ApiKey: key, Caller: caller})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("unavailable %s save was admitted (%s)", provider.String(), status.Code(err))
		}
		// Reuse the operation ID across providers to prove SetAttributes identities do not collide.
		mutation := modelcredential.Mutation{Provider: provider, OperationID: "same-operation-different-provider"}
		admitted, err := sessions.ReserveModelCredential(ctx, id, mutation)
		if err != nil || !admitted.Fresh {
			t.Fatalf("reserve %s on real Dex (%T)", provider.String(), err)
		}
		if err = sessions.WriteModelCredential(ctx, id, mutation, &modelcredential.Envelope{Revision: admitted.Info.Revision, Key: key}); err != nil {
			t.Fatalf("write %s on real Dex (%T)", provider.String(), err)
		}
		if _, err = sessions.FinalizeModelCredential(ctx, id, mutation); err != nil {
			t.Fatalf("finalize %s on real Dex (%T)", provider.String(), err)
		}
		exact, err := sessions.ReadModelCredential(ctx, id, provider)
		if err != nil || exact == nil || exact.Key != key || exact.Revision != admitted.Info.Revision {
			t.Fatalf("isolated %s envelope not confirmed (%T)", provider.String(), err)
		}
		read, err = api.GetModelCredentialInfo(ctx, &pb.GetModelCredentialInfoRequest{ProjectId: project, Provider: wire, Caller: caller})
		if err != nil || read.GetCredential().GetProvider() != wire || read.GetCredential().GetStatus() != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_ACTIVE {
			t.Fatalf("isolated %s projection not confirmed (%s)", provider.String(), status.Code(err))
		}
	}
}

func assertBYOKProvidersAbsent(t *testing.T, ctx context.Context, sessions *sessionagent.SessionAgentFlowClient, id agentloop.FlowID) {
	t.Helper()
	for _, provider := range modelcredential.Providers() {
		exact, err := sessions.ReadModelCredential(ctx, id, provider)
		if err != nil || exact != nil {
			t.Fatalf("Project cleanup retained %s source credential (%T)", provider.String(), err)
		}
	}
}

// Startup uses real Native dependencies while the enabled provider has no DNS record.
func TestProviderDNSOutageStartupDexIntegration(t *testing.T) {
	if dexflowtest.LocalLaneForTest(t).String() != "native" {
		t.Fatal("requires Local Native")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	logger := slog.New(slog.DiscardHandler)
	settings, err := serverconfig.Load(strings.Split(os.Getenv("SUPERVERSE_CONFIG_FILE"), ",")...)
	if err != nil {
		t.Fatal(err)
	}
	config, err := loadConfig(ctx, logger, settings)
	if err != nil {
		t.Fatal(err)
	}
	// Supply the configured model transport directly, without starting a local endpoint.
	config.mockModel = false
	config.agent.Mock = &modelprovider.MockClientConfig{ClaudeClientConfig: modelprovider.ClaudeClientConfig{APIKey: mockmodel.APIKeyForTest, BaseURL: "https://provider.invalid"}}
	config.providers = modelprovider.ProviderConfig{Enabled: []conversation.Provider{conversation.ProviderOpenAI}, MockEndpoint: true}
	config.blobCache.Dir = t.TempDir()
	config.rpcCoverageFile = ""
	var owned resources
	defer func() {
		if err := owned.release(); err != nil {
			t.Error(err)
		}
	}()
	server, err := compose(ctx, config, logger, &owned)
	if err != nil {
		t.Fatal("DNS outage prevented server composition:", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.business.Serve(listener) }()
	defer func() { server.business.Stop(); <-served }()
	transport, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	_, err = pb.NewAgentServiceClient(transport).ListAgentModels(ctx, &pb.ListAgentModelsRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatal("composed server did not serve authenticated API:", err)
	}
	runtime, err := modelprovider.NewCompatibleProviders(ctx, config.providers, config.modelEndpoints, config.agent.Mock.BaseURL, logger, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	var dns *net.DNSError
	if err := runtime.ValidateCredential(ctx, conversation.ProviderOpenAI, mockmodel.APIKeyForTest); !errors.As(err, &dns) {
		t.Fatal("provider call must fail closed at DNS dial:", err)
	}
}

// TestMessagesProviderPickerDexIntegration runs the real credential and picker
// browser specs independently of the longer credential recovery regression.
func TestMessagesProviderPickerDexIntegration(t *testing.T) {
	if dexflowtest.LocalLaneForTest(t).String() != "native" {
		t.Fatal("requires Local Native")
	}
	directory := os.Getenv("SUPERVERSE_TEST_BYOK_STUDIO_DIR")
	if directory == "" {
		t.Fatal("requires the owned Native production Studio directory")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	logger := slog.New(slog.DiscardHandler)
	modelURL, _, stop, err := mockmodel.StartCredentialProbeForTest(ctx, logger, "synthetic-byok-studio-1234")
	if err != nil {
		t.Fatal("start credential-capable mock model endpoint")
	}
	defer stop()
	settings, err := serverconfig.Load(strings.Split(os.Getenv("SUPERVERSE_CONFIG_FILE"), ",")...)
	if err != nil {
		t.Fatal("load Native configuration")
	}
	config, err := loadConfig(ctx, logger, settings)
	if err != nil {
		t.Fatal("load Native providers")
	}
	socket, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := socket.Addr().(*net.TCPAddr).Port
	_ = socket.Close()
	target := dex.WorkerTarget{Address: fmt.Sprintf("host.docker.internal:%d", port)}
	config.dexWorker.Pull = nil
	config.dexWorker.BindAddress = fmt.Sprintf(":%d", port)
	config.dexWorker.WorkerTarget = target
	config.dexClient.WorkerTarget = &target
	config.agent.Model = conversation.ModelHaiku45
	config.agent.Claude.BaseURL = modelURL
	config.blobCache.Dir = t.TempDir()
	config.rpcCoverageFile = os.Getenv("SUPERVERSE_TEST_RPC_COVERAGE_FILE")
	var owned resources
	defer func() {
		if err := owned.release(); err != nil {
			t.Errorf("release browser fixture resources (%T)", err)
		}
	}()
	server, err := compose(ctx, config, logger, &owned)
	if err != nil {
		t.Fatalf("compose production browser server (%T)", err)
	}
	ended := make(chan error, 1)
	go func() { ended <- server.worker.Start() }()
	defer func() {
		shutdown, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := server.worker.Stop(shutdown); err != nil {
			t.Errorf("stop browser fixture Worker (%T)", err)
		}
		<-ended
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.business.Serve(listener) }()
	defer func() { server.business.Stop(); <-served }()
	runBYOKStudioSpec(t, ctx, listener.Addr().String(), directory)
}
