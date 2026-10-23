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
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/modelendpoint"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestProjectAnthropicCredentialDexIntegration composes the production server over the
// Native lane's real dependencies; only the provider endpoint is the existing mock model.
func TestProjectAnthropicCredentialDexIntegration(t *testing.T) {
	if dexflowtest.LocalLaneForTest(t).String() != "native" {
		t.Fatal("requires Local Native")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	logger := slog.New(slog.DiscardHandler)
	firstKey := "synthetic-byok-first-" + flowid.RandomID(20)
	secondKey := "synthetic-byok-second-" + flowid.RandomID(20)
	var probe *mockmodel.CredentialProbeForTest
	modelURL, probe, stop, err := mockmodel.StartCredentialProbeForTest(ctx, logger, firstKey)
	if err != nil {
		t.Fatal("start mock model credential endpoint")
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
	config.agent.Model = conversation.ModelHaiku45
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
	project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())
	_, caller := projecttest.LoginForTest(ctx, t, project.CreatorAccountID)
	id := agentloop.SessionFlowID(project.ProjectID)
	reader := dexflowtest.ExecutionReaderForTest(t, settings.Dex.ServerAddress)
	sessions := sessionagent.NewSessionAgentFlowClientForTest(server.dex, &sessionagent.SessionAgentFlow{}, reader, conversation.ModelHaiku45)
	credentials := pb.NewModelCredentialServiceClient(transport)
	agent := pb.NewAgentServiceClient(transport)
	var credential *pb.ModelCredentialInfo
	readInfo := func() {
		t.Helper()
		response, err := credentials.GetModelCredentialInfo(ctx, &pb.GetModelCredentialInfoRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, Caller: caller})
		if err != nil {
			t.Fatalf("read key metadata (%s)", status.Code(err))
		}
		credential = response.GetCredential()
		projected, snapshotErr := sessions.GetSnapshot(ctx, id)
		if snapshotErr != nil || projected.Agent != nil && !strings.Contains(projected.Agent.Description.ModelCredentialVersion, fmt.Sprintf(";anthropic/%d/", credential.Revision)) {
			t.Fatal("Snapshot has no matching credential version")
		}
		encoded, marshalErr := proto.Marshal(response)
		if marshalErr != nil || credential == nil {
			t.Fatal("read typed credential metadata")
		}
		if bytes.Contains(encoded, []byte(firstKey)) || bytes.Contains(encoded, []byte(secondKey)) {
			t.Fatal("public metadata contains a key")
		}
	}
	readInfo()
	if credential.Revision != 0 || credential.Last4 != "" {
		t.Fatal("an open Session inherited a platform key")
	}
	if _, err = agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project.ProjectID, MessageId: flowid.RandomID(20), Text: "No key", Caller: caller}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("keyless platform send returned %s", status.Code(err))
	}
	set := func(key, operation string, revision uint64) error {
		_, err := credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: operation, ExpectedRevision: &revision, ApiKey: key, Caller: caller})
		return err
	}
	// Real adapter/HTTP error diagnostics retain the typed cause and safe request ID.
	validationPool := modelendpoint.NewPool(modelendpoint.Config{AllowLocalLoopback: true})
	defer validationPool.Close()
	validationHTTP, err := validationPool.Client(ctx, modelURL)
	if err != nil {
		t.Fatal("admit credential validation endpoint")
	}
	validationErr := modelprovider.ValidateCredentialForTest(ctx, validationHTTP, "synthetic-byok-invalid-0000", conversation.ModelHaiku45, modelURL, logger)
	validationHTTP.CloseIdleConnections()
	if _, ok := errors.AsType[*anthropic.Error](validationErr); !ok {
		t.Fatal("provider rejection lost its typed cause")
	}
	if validationErr == nil || !strings.Contains(validationErr.Error(), mockmodel.CredentialProbeRequestIDForTest) || strings.Contains(validationErr.Error(), "synthetic-byok-invalid-0000") {
		t.Fatal("provider diagnostics lost its safe identity or included a key")
	}
	if err = set("synthetic-byok-invalid-0000", "reject-invalid", 0); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("invalid key validation returned %s", status.Code(err))
	}
	readInfo()
	if credential.Revision != 0 {
		t.Fatal("failed credential validation changed metadata")
	}
	if err = set(firstKey, "set-first", 0); err != nil {
		t.Fatalf("set first key (%s)", status.Code(err))
	}
	readInfo()
	if credential.Revision != 1 || credential.Status != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_ACTIVE || credential.Last4 != firstKey[len(firstKey)-4:] {
		t.Fatal("set did not publish ModelKeyInfo")
	}
	if setAt, parseErr := time.Parse(time.RFC3339Nano, credential.SetAt); parseErr != nil || setAt.IsZero() {
		t.Fatal("set did not publish the admitted writer timestamp")
	}
	send := func(text string) {
		t.Helper()
		_, err := agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project.ProjectID, MessageId: flowid.RandomID(20), Text: text, Caller: caller})
		if err != nil {
			t.Fatalf("send with Project key (%s)", status.Code(err))
		}
	}
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Minute)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal(label + " timed out")
			case <-time.After(250 * time.Millisecond):
			}
		}
		t.Fatal(label + " did not converge")
	}
	if err = set(firstKey, "set-first", 0); err != nil {
		t.Fatalf("same-operation replay failed (%s)", status.Code(err))
	}
	// Replace the real Worker with a fresh production composition; the envelope must survive.
	shutdown, done := context.WithTimeout(ctx, 30*time.Second)
	err = server.worker.Stop(shutdown)
	done()
	if err != nil {
		t.Fatalf("stop real Worker for recovery (%T)", err)
	}
	<-workerEnded
	config.blobCache.Dir = filepath.Join(cacheRoot, "restart")
	server, err = compose(ctx, config, logger, &owned)
	if err != nil {
		t.Fatalf("recompose real Worker (%T)", err)
	}
	workerEnded = make(chan error, 1)
	go func() { workerEnded <- server.worker.Start() }()
	send("Hello from the Project key")
	wait("provider header and completed Session turn", func() bool {
		snapshot, err := sessions.GetSnapshot(ctx, id)
		return err == nil && snapshot.Agent != nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage && snapshot.Agent.Description.Turn > 0 && probe.RequestsForTest() >= 2
	})
	// Two replicas with the same expected revision cannot both reserve the writer.
	probe.SetKeyForTest(secondKey)
	var writes sync.WaitGroup
	results := make(chan error, 2)
	for _, op := range []string{"replace-a", "replace-b"} {
		writes.Add(1)
		go func(op string) { defer writes.Done(); results <- set(secondKey, op, 1) }(op)
	}
	writes.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if status.Code(err) == codes.Aborted {
			conflict++
		} else {
			t.Fatalf("concurrent key mutation failed (%s)", status.Code(err))
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("credential writer did not serialize concurrent replacement")
	}
	readInfo()
	if credential.Revision != 2 || credential.Last4 != secondKey[len(secondKey)-4:] {
		t.Fatal("replacement metadata mismatch")
	}
	// The real delegation tool creates a real Sandbox worktree and SubagentFlow.
	send(mockInput(t, "byok-copy", 1).Content)
	children := subagent.NewSubagentFlowClientForTest(server.dex, &subagent.SubagentFlow{}, reader)
	var child string
	var copied agenttasks.SubagentRecord
	wait("subagent copied Project key", func() bool {
		snapshot, err := sessions.GetSnapshot(ctx, id)
		if err != nil || snapshot.Agent == nil {
			return false
		}
		ready := 0
		for _, record := range snapshot.Agent.Description.Subagents {
			if record.Status.Finished() {
				continue
			}
			childState, err := children.SnapshotForTest(ctx, agenttasks.SubagentIdentity{ProjectID: project.ProjectID, AgentID: record.AgentID})
			key, keyErr := children.ReadModelCredential(ctx, agenttasks.SubagentIdentity{ProjectID: project.ProjectID, AgentID: record.AgentID})
			if err == nil && keyErr == nil && key != nil && key.Revision == 2 && childState.LastSequence >= 3 {
				child, copied = record.AgentID, record
				ready++
			}
		}
		info, infoErr := credentials.GetModelCredentialInfo(ctx, &pb.GetModelCredentialInfoRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, Caller: caller})
		return ready == agenttasks.MaximumRunningSubagents && infoErr == nil && info.GetCredential().GetStatus() == pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_ACTIVE
	})
	// Reproduce the real Dex checkpoint after a copy reservation but without its key.
	send(mockInput(t, "byok-copy", 2).Content)
	wait("parent completes while both children run", func() bool {
		snapshot, err := sessions.GetSnapshot(ctx, id)
		return err == nil && snapshot.Agent != nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage && snapshot.Agent.Description.Turn >= 3
	})
	copyInfo, err := sessions.ReadModelCredentialInfo(ctx, id, modelcredential.Anthropic)
	if err != nil {
		t.Fatal("read the admitted copy checkpoint")
	}
	start := agenttasks.SubagentStart{Identity: agenttasks.SubagentIdentity{ProjectID: project.ProjectID, AgentID: child},
		Call: agenttasks.SubagentCall{Kind: copied.Kind, Description: copied.Description}, Fingerprint: copied.Fingerprint,
		BaseCommit: copied.BaseCommit, Model: conversation.ModelHaiku45}
	if !copyInfo.Active() || len(copyInfo.Holders) == 0 || copyInfo.ChangeReason != modelcredential.ChangedReason {
		t.Fatal("copy has no durable holder or disabled parent metadata")
	}
	start.CredentialRevision = copyInfo.Revision
	parentKey, err := sessions.ReadModelCredential(ctx, id, modelcredential.Anthropic)
	if err != nil || parentKey == nil {
		t.Fatal("read transient parent key for replay")
	}
	if err = children.WriteModelCredential(ctx, start.Identity, "missing-copy-checkpoint", nil); err != nil {
		t.Fatal("remove the interrupted child copy")
	}
	recovery := &agents.ModelCredentialFlowService{Sessions: sessions, Subagents: children, Logger: logger}
	launcher := &agents.SessionFlowService{ModelCredentials: recovery, Logger: logger}
	if err = launcher.StartSubagent(ctx, start, *parentKey, 2); !errors.Is(err, modelcredential.ErrCopyStopped) {
		t.Fatalf("recover interrupted copy (%T)", err)
	}
	readInfo()
	if credential.Status != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_ACTIVE || credential.Revision != 2 {
		t.Fatal("an interrupted child copy disabled the parent's credential")
	}
	modelRevision := uint64(0)
	if _, err = agent.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: project.ProjectID, Model: pb.AgentModel_AGENT_MODEL_HAIKU_4_5, ExpectedRevision: &modelRevision, Caller: caller}); err != nil {
		t.Fatalf("parent model admission after copy recovery (%s)", status.Code(err))
	}
	t.Log("An interrupted child copy was stopped and cleared; the parent credential remains usable")
	wait("interrupted copy child stopped", func() bool {
		snapshot, err := sessions.GetSnapshot(ctx, id)
		if err != nil || snapshot.Agent == nil {
			return false
		}
		for _, record := range snapshot.Agent.Description.Subagents {
			if record.AgentID == child {
				return record.Status.Finished()
			}
		}
		return false
	})
	childEnd, err := children.SnapshotForTest(ctx, start.Identity)
	if err != nil || childEnd.Outcome == nil || childEnd.Outcome.CredentialChangeReason != modelcredential.ChangedReason {
		t.Fatal("credential stop did not record a typed reason")
	}
	var aborted modelcredential.Holder
	for _, holder := range copyInfo.Holders {
		if holder.AgentID != child {
			aborted = holder
			break
		}
	}
	if aborted.AgentID == "" {
		t.Fatal("no other child to exercise abnormal holder cleanup")
	}
	if err := server.dex.StopFlow(ctx, flowid.SubagentFlowID(project.ProjectID, aborted.AgentID), dex.StopOptions{Type: dex.TerminateFlow, Reason: "credential integration: aborted holder"}); err != nil {
		t.Fatal("terminate the owned test child")
	}
	// The next copy and deletion run concurrently through their real Dex boundaries.
	send(mockInput(t, "byok-copy", 3).Content)
	var raceHolders []modelcredential.Holder
	var raceCallID conversation.CallID
	wait("credential holder admitted before racing deletion", func() bool {
		info, err := sessions.ReadModelCredentialInfo(ctx, id, modelcredential.Anthropic)
		if err != nil {
			return false
		}
		for _, holder := range info.Holders {
			if !slices.ContainsFunc(copyInfo.Holders, func(old modelcredential.Holder) bool { return old.AgentID == holder.AgentID }) {
				raceCallID = conversation.CallID(holder.CallID)
				raceHolders = append(append([]modelcredential.Holder{}, copyInfo.Holders...), info.Holders...)
				return !slices.ContainsFunc(info.Holders, func(old modelcredential.Holder) bool { return old.AgentID == aborted.AgentID })
			}
		}
		return false
	})
	if remaining, err := children.ReadModelCredential(ctx, agenttasks.SubagentIdentity{ProjectID: project.ProjectID, AgentID: aborted.AgentID}); err != nil || remaining != nil {
		t.Fatal("a new start did not reconcile the aborted holder")
	}
	raceStarted := time.Now()
	probe.SetKeyForTest(firstKey)
	if err = set(firstKey, "replace-racing-copy", 2); err != nil {
		t.Fatalf("replace during admitted copy (%s)", status.Code(err))
	}
	readInfo()
	if credential.Revision != 3 || credential.Status != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_ACTIVE {
		t.Fatal("copy race did not leave the new source credential Active")
	}
	var raceResultCode string
	wait("racing tool call answered promptly", func() bool {
		if time.Since(raceStarted) > 30*time.Second {
			t.Fatal("stale credential copy entered a Step retry loop")
		}
		snapshot, err := sessions.GetSnapshot(ctx, id)
		if err != nil || snapshot.Agent == nil || snapshot.Agent.Description.Status != agentloop.AgentStatusWaitingForMessage {
			return false
		}
		for _, message := range snapshot.Agent.History.Messages {
			if message.Message.ToolCallID != nil && *message.Message.ToolCallID == raceCallID && message.Message.ToolExecution != nil {
				raceResultCode = message.Message.ToolExecution.ResultCode
				return true
			}
		}
		return false
	})
	for _, holder := range raceHolders {
		remaining, readErr := children.ReadModelCredential(ctx, agenttasks.SubagentIdentity{ProjectID: project.ProjectID, AgentID: holder.AgentID})
		if readErr != nil || remaining != nil {
			t.Fatal("the credential race left an old child copy")
		}
	}
	switch raceResultCode {
	case "":
		t.Log("Credential replacement race branch: admitted copy cleaned up")
	case toolfailure.ModelCredentialUnavailable.String():
		t.Log("Credential replacement race branch: immediate typed rejection")
	case toolfailure.InterruptedByUser.String():
		t.Log("Credential replacement race branch: in-flight start interrupted and copy cleaned up")
	default:
		t.Fatal("credential replacement race produced an unexpected tool failure")
	}
	newKeyRequests := probe.RequestsForTest()
	send(mockInput(t, "byok-copy", 4).Content)
	wait("parent uses the new credential after the copy race", func() bool {
		snapshot, err := sessions.GetSnapshot(ctx, id)
		return err == nil && snapshot.Agent != nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage && snapshot.Agent.Description.Turn >= 5 && probe.RequestsForTest() > newKeyRequests
	})
	revision := uint64(3)
	if _, err = credentials.DeleteModelCredential(ctx, &pb.DeleteModelCredentialRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: "delete-final", ExpectedRevision: &revision, Caller: caller}); err != nil {
		t.Fatalf("delete Project key (%s)", status.Code(err))
	}
	if _, err = credentials.DeleteModelCredential(ctx, &pb.DeleteModelCredentialRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: "delete-final", ExpectedRevision: &revision, Caller: caller}); err != nil {
		t.Fatalf("same-operation delete replay failed (%s)", status.Code(err))
	}
	readInfo()
	if credential.Revision != 4 || credential.Status != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_REVOKED || credential.Last4 != "" {
		t.Fatal("delete did not publish revoked metadata")
	}
	targets := []struct{ id, key string }{{string(id), "ModelKeys/anthropic"}}
	for _, holder := range raceHolders {
		targets = append(targets, struct{ id, key string }{flowid.SubagentFlowID(project.ProjectID, holder.AgentID), "ModelKey"})
	}
	for _, target := range targets {
		response, err := reader.GetAttributes(ctx, &dexpb.GetAttributesRequest{FlowId: target.id, Keys: []string{target.key}})
		if status.Code(err) == codes.NotFound {
			continue
		}
		if err != nil || len(response.GetAttributes()) != 0 {
			t.Fatal("exact reread still exposes a deleted credential")
		}
		assertBYOKHistory(t, ctx, reader, target.id, firstKey, secondKey)
	}

	wait("subagent credential-change reason", func() bool {
		snapshot, err := sessions.GetSnapshot(ctx, id)
		if err != nil || snapshot.Agent == nil {
			return false
		}
		for _, record := range snapshot.Agent.Description.Subagents {
			if record.AgentID == child {
				return record.Status.Finished() && strings.Contains(record.Summary, "model credential changed")
			}
		}
		return false
	})

	// A real paired Bridge stays on its epoch/generation when the platform key changes.
	remote := pb.NewRemoteAgentServiceClient(transport)
	pairing, err := remote.CreateRemoteAgentPairing(ctx, &pb.CreateRemoteAgentPairingRequest{ProjectId: project.ProjectID, Caller: caller})
	if err != nil {
		t.Fatalf("create actual remote pairing (%T)", err)
	}
	endpoint := httptest.NewServer(server.remoteAgent)
	defer endpoint.Close()
	httpClient := &http.Client{Timeout: 15 * time.Second}
	defer httpClient.CloseIdleConnections()
	post := func(path, token string, input, output any) {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal("encode remote request")
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal("construct remote request")
		}
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := httpClient.Do(request)
		if err != nil {
			t.Fatalf("actual remote HTTP transport (%T)", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("actual remote HTTP status %d", response.StatusCode)
		}
		if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output) != nil {
			t.Fatal("decode remote response")
		}
	}
	var paired bridgeapi.PairResponse
	post(bridgeapi.PathPair, "", bridgeapi.PairRequest{Code: pairing.GetCode(), Bridge: bridgeapi.BridgeInfo{Version: config.remoteAgent.MinimumBridgeVersion, OS: runtime.GOOS, Arch: runtime.GOARCH}}, &paired)
	var active bridgeapi.ActivateResponse
	post(bridgeapi.PathActivate, paired.Token, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "byok", HostLabel: "integration host"}}, &active)
	send("Continue through the connected local agent")
	wait("active remote turn", func() bool {
		snapshot, err := sessions.GetSnapshot(ctx, id)
		return err == nil && snapshot.Agent != nil && snapshot.Agent.Description.Status != agentloop.AgentStatusWaitingForMessage && snapshot.Agent.Description.Status != agentloop.AgentStatusInitializing
	})
	beforeRemote, err := sessions.GetSnapshot(ctx, id)
	if err != nil {
		t.Fatal("read active remote executor")
	}
	providerRequests := probe.RequestsForTest()
	probe.SetKeyForTest(secondKey)
	if err = set(secondKey, "set-during-remote", 4); err != nil {
		t.Fatalf("set during remote turn (%s)", status.Code(err))
	}
	revision = 5
	if _, err = credentials.DeleteModelCredential(ctx, &pb.DeleteModelCredentialRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: "delete-during-remote", ExpectedRevision: &revision, Caller: caller}); err != nil {
		t.Fatalf("delete during remote turn (%s)", status.Code(err))
	}
	if err = sessions.InterruptAgent(ctx, id, agentloop.InterruptAgentRequest{Turn: beforeRemote.Agent.Description.Turn, PlatformOnly: true}); err != nil {
		t.Fatal("platform-only interruption did not acknowledge remote no-op")
	}
	afterRemote, err := sessions.GetSnapshot(ctx, id)
	if err != nil || afterRemote.Agent == nil || afterRemote.Agent.Description.Executor.Epoch != active.Epoch || afterRemote.Agent.Description.Executor.Generation != active.Generation || afterRemote.Agent.Description.Turn != beforeRemote.Agent.Description.Turn || afterRemote.Agent.Description.Status != beforeRemote.Agent.Description.Status {
		t.Fatal("platform key mutation interrupted the remote executor")
	}
	if probe.RequestsForTest() != providerRequests+1 {
		t.Fatal("remote execution called the provider beyond explicit key validation")
	}
	if _, err = remote.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: project.ProjectID, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
		t.Fatalf("switch back after remote key regression (%s)", status.Code(err))
	}
	if _, err = remote.RevokeRemoteAgentConnection(ctx, &pb.RevokeRemoteAgentConnectionRequest{ProjectId: project.ProjectID, ConnectionId: paired.ConnectionID, Caller: caller}); err != nil {
		t.Fatalf("revoke actual regression connection (%s)", status.Code(err))
	}
	// Reserve on the actual Flow to reproduce a lost set before its secret write.
	// The public retry must cancel, restore the prior metadata and never reuse its revision.
	pendingMutation := modelcredential.Mutation{Provider: modelcredential.Anthropic, OperationID: "pending-save-without-write", ExpectedRevision: 6}
	reserved, err := sessions.ReserveModelCredential(ctx, id, pendingMutation)
	if err != nil || !reserved.Fresh {
		t.Fatal("reserve the lost set checkpoint")
	}
	readInfo()
	if credential.PendingOperation != pendingMutation.OperationID || credential.PendingRevision != pendingMutation.ExpectedRevision || credential.Status != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_PENDING {
		t.Fatal("a reloaded Builder cannot recover the pending operation identity")
	}
	if err = set("", pendingMutation.OperationID, pendingMutation.ExpectedRevision); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("absent pending set did not cancel")
	}
	readInfo()
	if credential.Status != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_REVOKED || credential.Revision != 6 {
		t.Fatal("cancel did not restore prior metadata")
	}
	probe.SetKeyForTest(firstKey)
	if err = set(firstKey, "save-after-cancel", 6); err != nil {
		t.Fatal("save after cancellation failed")
	}
	readInfo()
	if credential.Revision != 8 || credential.Status != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_ACTIVE {
		t.Fatal("a canceled revision was reused")
	}
	pendingMutation = modelcredential.Mutation{Provider: modelcredential.Anthropic, OperationID: "pending-replace-with-old-envelope", ExpectedRevision: 8}
	if _, err = sessions.ReserveModelCredential(ctx, id, pendingMutation); err != nil {
		t.Fatal("reserve a replacement checkpoint")
	}
	if err = set("", pendingMutation.OperationID, 8); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("old envelope did not cancel pending replacement")
	}
	readInfo()
	if credential.Revision != 8 || credential.Status != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_ACTIVE {
		t.Fatal("pending replace cancellation disabled the old key")
	}
	pendingMutation = modelcredential.Mutation{Provider: modelcredential.Anthropic, OperationID: "pending-set-confirmed", ExpectedRevision: 8}
	reserved, err = sessions.ReserveModelCredential(ctx, id, pendingMutation)
	if err != nil {
		t.Fatal("reserve a confirmed-write checkpoint")
	}
	if err = sessions.WriteModelCredential(ctx, id, pendingMutation, &modelcredential.Envelope{Revision: reserved.Info.Revision, Key: firstKey}); err != nil {
		t.Fatal("persist pending target on actual Dex")
	}
	if err = set("", pendingMutation.OperationID, 8); err != nil {
		t.Fatal("confirmed pending set did not finalize without a key")
	}
	pendingMutation = modelcredential.Mutation{Provider: modelcredential.Anthropic, OperationID: "pending-delete-before-write", ExpectedRevision: 10, Delete: true}
	if _, err = sessions.ReserveModelCredential(ctx, id, pendingMutation); err != nil {
		t.Fatal("reserve pending delete checkpoint")
	}
	revision = 10
	if _, err = credentials.DeleteModelCredential(ctx, &pb.DeleteModelCredentialRequest{ProjectId: project.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: pendingMutation.OperationID, ExpectedRevision: &revision, Caller: caller}); err != nil {
		t.Fatal("pending delete did not reconcile through a derived write identity")
	}
	if remaining, err := sessions.ReadModelCredential(ctx, id, modelcredential.Anthropic); err != nil || remaining != nil {
		t.Fatal("pending deletion has no exact absence proof")
	}
	t.Log("Pending set cancellation, confirmed-write finalization and delete reconciliation use real Dex")
	// Abnormal engine completion leaves no Step to delete the source envelope.
	// Actual Project cleanup must still exact-delete it before reporting DELETED.
	if err = set(firstKey, "key-before-abnormal-cleanup", 11); err != nil {
		t.Fatal("set key before abnormal cleanup")
	}
	seedBYOKProviders(t, ctx, sessions, credentials, id, project.ProjectID, caller, firstKey)
	if err = server.dex.StopFlow(ctx, string(id), dex.StopOptions{Type: dex.TerminateFlow, Reason: "credential integration: stopped source cleanup"}); err != nil {
		t.Fatal("stop the owned test Session")
	}
	projecttest.DeleteForTest(ctx, t, projectClient, project.ProjectID)
	if remaining, err := sessions.ReadModelCredential(ctx, id, modelcredential.Anthropic); err != nil || remaining != nil {
		t.Fatal("abnormal Project cleanup retained its source credential")
	}
	assertBYOKProvidersAbsent(t, ctx, sessions, id)
	assertBYOKHistory(t, ctx, reader, string(id), firstKey, secondKey)
	// A normal Project deletion is a system action, not the former setter's action.
	normal := projecttest.CreateForTest(ctx, t, projectClient, t.Name()+"-cleanup-actor")
	_, normalCaller := projecttest.LoginForTest(ctx, t, normal.CreatorAccountID)
	zero := uint64(0)
	if _, err = credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: normal.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: "normal-cleanup-key", ExpectedRevision: &zero, ApiKey: firstKey, Caller: normalCaller}); err != nil {
		t.Fatalf("set normal-cleanup credential (%s)", status.Code(err))
	}
	seedBYOKProviders(t, ctx, sessions, credentials, agentloop.SessionFlowID(normal.ProjectID), normal.ProjectID, normalCaller, firstKey)
	retainedChild := exerciseBYOKChildIsolation(t, ctx, sessions, children, credentials, agent, normal.ProjectID, normalCaller, firstKey, probe, conversation.ModelGPT6Astra, wait)
	projecttest.DeleteForTest(ctx, t, projectClient, normal.ProjectID)
	assertBYOKChildAbsent(t, ctx, sessions, children, agentloop.SessionFlowID(normal.ProjectID), retainedChild, modelcredential.ForModel(conversation.ModelGPT6Astra))
	assertBYOKProvidersAbsent(t, ctx, sessions, agentloop.SessionFlowID(normal.ProjectID))
	// Repeat the actual running-child isolation contract on the Messages wire.
	messagesProject := projecttest.CreateForTest(ctx, t, projectClient, t.Name()+"-messages-isolation")
	_, messagesCaller := projecttest.LoginForTest(ctx, t, messagesProject.CreatorAccountID)
	if _, err = credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: messagesProject.ProjectID, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: "messages-isolation-key", ExpectedRevision: &zero, ApiKey: firstKey, Caller: messagesCaller}); err != nil {
		t.Fatal("set Messages isolation source key")
	}
	messagesChild := exerciseBYOKChildIsolation(t, ctx, sessions, children, credentials, agent, messagesProject.ProjectID, messagesCaller, firstKey, probe, conversation.ModelMuseWonder10, wait)
	projecttest.DeleteForTest(ctx, t, projectClient, messagesProject.ProjectID)
	assertBYOKChildAbsent(t, ctx, sessions, children, agentloop.SessionFlowID(messagesProject.ProjectID), messagesChild, modelcredential.ForModel(conversation.ModelMuseWonder10))
	assertBYOKProvidersAbsent(t, ctx, sessions, agentloop.SessionFlowID(messagesProject.ProjectID))
	cleanedInfo, err := sessions.ReadModelCredentialInfo(ctx, agentloop.SessionFlowID(normal.ProjectID), modelcredential.Anthropic)
	if err != nil || cleanedInfo.Status != modelcredential.StatusRevoked || cleanedInfo.SetBy != (conversation.Actor{}) || cleanedInfo.ChangeReason != modelcredential.ProjectDeletionReason {
		t.Fatal("Project cleanup did not record its system actor and typed reason")
	}
	paired.Token = ""
	empty := projecttest.CreateForTest(ctx, t, projectClient, t.Name()+"-unused-credentials")
	projecttest.DeleteForTest(ctx, t, projectClient, empty.ProjectID)
	for _, provider := range modelcredential.Providers() {
		info, err := sessions.ReadModelCredentialInfo(ctx, agentloop.SessionFlowID(empty.ProjectID), provider)
		if err != nil || info.Revision != 0 || info.Status != (modelcredential.Status{}) {
			t.Fatal("deleting an unused provider created a credential tombstone")
		}
	}
	t.Log("Actual Bridge pairing/activation: key set/delete preserve the active remote epoch, generation and turn")
	t.Logf("FlowID=%s: set, model request header, concurrent replace, subagent copy, delete and history redaction passed", id)
	if directory := os.Getenv("SUPERVERSE_TEST_BYOK_STUDIO_DIR"); directory != "" {
		probe.SetKeyForTest("synthetic-byok-studio-1234")
		runBYOKStudioSpec(t, ctx, listener.Addr().String(), directory)
	}
}

func assertBYOKHistory(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, id string, keys ...string) {
	t.Helper()
	summary, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: id})
	if err != nil {
		t.Fatal("read credential Flow summary")
	}
	for start := int64(1); ; {
		page, err := reader.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{FlowId: id, RunId: summary.GetFlowExecutionId().GetRunId(), StartInternalEventId: start, EstimatePageSize: 200})
		if err != nil {
			t.Fatal("read credential Flow history")
		}
		for _, event := range page.GetEvents() {
			encoded, err := proto.Marshal(event)
			if err != nil {
				t.Fatal("encode diagnostic event")
			}
			for _, key := range keys {
				if bytes.Contains(encoded, []byte(key)) {
					t.Fatal("credential bytes entered Flow history")
				}
			}
		}
		if len(page.GetNextPageToken()) == 0 {
			return
		}
		start = page.GetNextInternalEventId()
	}
}

// runBYOKStudioSpec runs the production BFF against this production-composed server.
// The only replacement remains the existing mock model HTTP endpoint.
func runBYOKStudioSpec(t *testing.T, ctx context.Context, grpcAddress, directory string, customEndpoint ...string) {
	t.Helper()
	socket, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := socket.Addr().(*net.TCPAddr).Port
	_ = socket.Close()
	origin := "http://127.0.0.1:" + strconv.Itoa(port)
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal("generate browser signing key")
	}
	protoPath, err := filepath.Abs("../../../api/superverse/v2/superverse.proto")
	if err != nil {
		t.Fatal(err)
	}
	environment := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "TMPDIR=" + os.Getenv("TMPDIR"), "NODE_ENV=production", "NEXT_TELEMETRY_DISABLED=1", "HOSTNAME=127.0.0.1", "PORT=" + strconv.Itoa(port), "BROWSER_SIGNING_KEY=" + base64.RawURLEncoding.EncodeToString(key), "SUPERVERSE_STUDIO_PUBLIC_ORIGIN=" + origin, "SUPERVERSE_GRPC_ADDRESS=" + grpcAddress, "SUPERVERSE_PROTO_PATH=" + protoPath}
	logDirectory, err := os.MkdirTemp("", "byokant-browser-")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDirectory, "studio.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	nodeCtx, stopNode := context.WithCancel(ctx)
	defer stopNode()
	node := exec.CommandContext(nodeCtx, "node", "server.js")
	node.Dir = directory
	node.Env = environment
	node.Stdout = log
	node.Stderr = log
	if err = node.Start(); err != nil {
		t.Fatal("start production Studio fixture")
	}
	defer func() { stopNode(); _ = node.Wait() }()
	client := &http.Client{Timeout: time.Second}
	ready := false
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get(origin + "/login")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("Studio fixture readiness timed out")
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatalf("production Studio fixture was not ready; log %s", logPath)
	}
	studioSource, err := filepath.Abs("../../../web/studio")
	if err != nil {
		t.Fatal(err)
	}
	specs := []string{"tests/e2e/integration/modelcredential/model-credential.spec.ts", "tests/e2e/integration/catalog/model-catalog.spec.ts"}
	if len(customEndpoint) > 0 {
		environment = append(environment, "SUPERVERSE_E2E_CUSTOM_ENDPOINT="+customEndpoint[0])
		specs = []string{"tests/e2e/integration/modelcredential/custom-endpoint.spec.ts"}
	}
	arguments := append([]string{filepath.Join(studioSource, "node_modules/@playwright/test/cli.js"), "test"}, specs...)
	arguments = append(arguments, "--config", "playwright.e2e.config.ts")
	spec := exec.CommandContext(ctx, "node", arguments...)
	spec.Dir = studioSource
	spec.Env = append(environment, "SUPERVERSE_E2E_BASE_URL="+origin, "SUPERVERSE_E2E_CAPTURE=off", "SUPERVERSE_E2E_BYOK_FIXTURE=true", "SUPERVERSE_E2E_COVERAGE_DIR="+os.Getenv("SUPERVERSE_E2E_COVERAGE_DIR"))
	browserLog := filepath.Join(logDirectory, "browser.log")
	output, err := os.OpenFile(browserLog, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	spec.Stdout = output
	spec.Stderr = output
	if err = spec.Run(); err != nil {
		t.Fatalf("Model credential Studio spec failed (%T); log %s", err, browserLog)
	}
	t.Logf("Studio specs %v passed through the real production BFF/gRPC/Dex path", specs)
}

func mockInput(t *testing.T, scenario string, number int) mockmodel.Input {
	t.Helper()
	for _, candidate := range mockmodel.Catalog() {
		if candidate.Name == scenario && number >= 1 && number <= len(candidate.Inputs) {
			return candidate.Inputs[number-1]
		}
	}
	t.Fatalf("the mock catalog has no input %d of scenario %s", number, scenario)
	return mockmodel.Input{}
}
