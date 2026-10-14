// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/platform"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/projectdao"
	"github.com/superdurable/superversev2/internal/projects/projecttest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// bridgeTestTimeout bounds the Project's creation, its Session's cold
// workspace, which the first Sandbox tool starts, and the whole exchange.
const bridgeTestTimeout = 25 * time.Minute

// callBudget bounds one client call. The client retries network failures and
// transient refusals until its context ends, so an unreachable endpoint fails
// the test after this budget instead of holding it until bridgeTestTimeout.
const callBudget = 2 * time.Minute

// The Bridge's own platform client against the lane server's actual remote
// agent listener, whose address CreateRemoteAgentPairing returns: the business
// API, SQL, Dex, the Session and its Sandbox are real. Orca is not involved;
// the relay's Orca side is owner-run acceptance. Remote mode makes no model call.
func TestBridgePlatformClientDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), bridgeTestTimeout)
	defer cancel()
	connection, err := grpc.NewClient(dexflowtest.Setting(t, "SUPERVERSE_TEST_API_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	caller := registerBridgeCaller(ctx, t, pb.NewAccountServiceClient(connection))
	projectID := createBridgeProject(ctx, t, caller.GetAccountId())
	sessions := pb.NewSessionServiceClient(connection)
	awaitSessionWaiting(ctx, t, sessions, caller, projectID)

	// 1. Pair with a code created through the business API.
	pairing, err := pb.NewRemoteAgentServiceClient(connection).CreateRemoteAgentPairing(ctx, &pb.CreateRemoteAgentPairingRequest{ProjectId: projectID, Caller: caller})
	if err != nil {
		t.Fatalf("CreateRemoteAgentPairing failed (%s)", status.Code(err))
	}
	bridge := bridgeapi.BridgeInfo{Version: bridgeapi.DevBridgeVersion, OS: runtime.GOOS, Arch: runtime.GOARCH}
	unpaired, err := platform.New(pairing.GetEndpoint(), "", bridge, slog.Default())
	if err != nil {
		t.Fatalf("the pairing endpoint %q is not a Bridge endpoint: %v", pairing.GetEndpoint(), err)
	}
	manifest, err := within(ctx, func(ctx context.Context) (bridgeapi.DownloadsManifest, error) { return unpaired.DownloadsManifest(ctx) })
	if err != nil || manifest.MinimumBridgeVersion == "" {
		t.Fatalf("downloads manifest: %v", err)
	}
	paired, err := within(ctx, func(ctx context.Context) (bridgeapi.PairResponse, error) {
		return unpaired.Pair(ctx, pairing.GetCode(), bridge)
	})
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	if paired.ConnectionID != pairing.GetConnectionId() || paired.ProjectID != projectID {
		t.Fatalf("pair answered connection %s of Project %s, not %s of %s", paired.ConnectionID, paired.ProjectID, pairing.GetConnectionId(), projectID)
	}
	_, err = within(ctx, func(ctx context.Context) (bridgeapi.PairResponse, error) {
		return unpaired.Pair(ctx, pairing.GetCode(), bridge)
	})
	requireRefusal(t, "a second redemption", err, bridgeapi.ErrorPairingCodeInvalid)
	client, err := platform.New(pairing.GetEndpoint(), paired.Token, bridge, slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	// 2. Activate: the connection becomes the Session's executor with the remote tool set.
	binding := bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "bridge-client", ProviderThreadID: "bridge-client", HostLabel: "integration host"}
	active, err := within(ctx, func(ctx context.Context) (bridgeapi.ActivateResponse, error) { return client.Activate(ctx, binding) })
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if active.Epoch == 0 || active.Generation == 0 || active.Reported != 0 {
		t.Fatalf("activation fence epoch=%d generation=%d reported=%d", active.Epoch, active.Generation, active.Reported)
	}
	if active.ToolSetDigest != agentloop.RemoteToolDigest() || len(active.Tools) != len(agentloop.RemoteTools()) {
		t.Fatal("activation returned another tool set than the server's remote catalog")
	}
	for _, name := range []conversation.ToolName{conversation.ToolWrite, conversation.ToolRead} {
		if !slices.ContainsFunc(active.Tools, func(tool bridgeapi.Tool) bool { return tool.Name == string(name) && json.Valid(tool.InputSchema) }) {
			t.Fatalf("activation omitted the %s tool or its schema", name)
		}
	}
	repeated, err := within(ctx, func(ctx context.Context) (bridgeapi.ActivateResponse, error) { return client.Activate(ctx, binding) })
	if err != nil || repeated.Epoch != active.Epoch || repeated.Generation != active.Generation {
		t.Fatalf("a repeated activation changed the executor (%v)", err)
	}
	executor := sessionAgent(ctx, t, sessions, caller, projectID).GetDescription().GetExecutor()
	if executor.GetMode() != pb.SessionExecutorMode_SESSION_EXECUTOR_MODE_REMOTE || executor.GetEpoch() != active.Epoch || executor.GetRemote().GetConnectionId() != paired.ConnectionID {
		t.Fatalf("the Session's executor is %+v after activation", executor)
	}

	// 3. Sync delivers a message sent through the business API.
	cursor := &syncCursor{client: client, connectionID: paired.ConnectionID, epoch: active.Epoch, after: active.LastSequence}
	cursor.next(ctx, t)
	text := "Bridge client proof " + flowid.RandomID(12)
	if _, err = pb.NewAgentServiceClient(connection).SendMessage(ctx, &pb.SendMessageRequest{ProjectId: projectID, MessageId: flowid.RandomID(24), Text: text, Caller: caller}); err != nil {
		t.Fatalf("SendMessage failed (%s)", status.Code(err))
	}
	delivered := cursor.await(ctx, t, "the delivered message", 3*time.Minute, func(entry bridgeapi.Entry) bool {
		return entry.Kind == bridgeapi.EntryKindUserMessage && strings.Contains(entry.Text, text)
	})
	if !delivered.Delivered(paired.ConnectionID) || delivered.Via != "" {
		t.Fatalf("the sent message is not delivered input: %+v", delivered)
	}

	// 4. Tool calls run in the Sandbox; each result returns under its canonical call ID.
	sequence := active.Reported
	report := func(name conversation.ToolName, arguments string) string {
		t.Helper()
		sequence++
		sent := bridgeapi.Report{Epoch: cursor.epoch, Generation: cursor.generation, Sequence: sequence,
			ToolCall: &bridgeapi.ToolCall{Name: string(name), Arguments: json.RawMessage(arguments)}}
		answer, err := within(ctx, func(ctx context.Context) (bridgeapi.ReportResponse, error) { return client.Report(ctx, sent) })
		if err != nil || answer.Duplicate || answer.Reported != sequence {
			t.Fatalf("report %d of %s: reported=%d duplicate=%t (%v)", sequence, name, answer.Reported, answer.Duplicate, err)
		}
		return bridgeapi.RemoteCallID(cursor.epoch, sequence)
	}
	content := "Bridge client proof " + flowid.RandomID(12)
	written := report(conversation.ToolWrite, `{"path":"bridge-client-proof.txt","content":"`+content+`\n"}`)
	if result := cursor.awaitResult(ctx, t, written); result.IsError {
		t.Fatalf("write failed: %s", result.Content)
	}
	read := report(conversation.ToolRead, `{"path":"bridge-client-proof.txt"}`)
	if result := cursor.awaitResult(ctx, t, read); result.IsError || !strings.Contains(result.Content, content) {
		t.Fatalf("read did not return the written content: %s", result.Content)
	}
	sequence++
	completed := bridgeapi.Report{Epoch: cursor.epoch, Generation: cursor.generation, Sequence: sequence, TurnCompleted: true}
	if _, err = within(ctx, func(ctx context.Context) (bridgeapi.ReportResponse, error) { return client.Report(ctx, completed) }); err != nil {
		t.Fatalf("turn completion: %v", err)
	}

	// 5. Disconnect revokes the credential: the next requests are unauthorized.
	disconnect, stop := context.WithTimeout(ctx, callBudget)
	err = client.Disconnect(disconnect)
	stop()
	if err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	_, err = within(ctx, func(ctx context.Context) (bridgeapi.SyncResponse, error) {
		return client.Sync(ctx, cursor.epoch, cursor.after, cursor.digest, false)
	})
	requireRefusal(t, "sync after disconnect", err, bridgeapi.ErrorUnauthorized)
	_, err = within(ctx, func(ctx context.Context) (bridgeapi.ActivateResponse, error) { return client.Activate(ctx, binding) })
	requireRefusal(t, "activate after disconnect", err, bridgeapi.ErrorUnauthorized)
}

// syncCursor is the Bridge's read position: the previous answer's
// examinedThrough and state digest, and the executor fence it reported.
type syncCursor struct {
	client            *platform.Client
	connectionID      string
	epoch, generation uint64
	after             uint64
	digest            string
}

// next runs one sync and moves the cursor.
func (cursor *syncCursor) next(ctx context.Context, t *testing.T) []bridgeapi.Entry {
	t.Helper()
	answer, err := within(ctx, func(ctx context.Context) (bridgeapi.SyncResponse, error) {
		return cursor.client.Sync(ctx, cursor.epoch, cursor.after, cursor.digest, false)
	})
	if err != nil {
		t.Fatalf("sync after %d: %v", cursor.after, err)
	}
	if answer.Epoch != cursor.epoch || answer.Generation == 0 || answer.ExaminedThrough > answer.LastSequence || answer.ToolSetDigest != agentloop.RemoteToolDigest() {
		t.Fatalf("sync answered epoch=%d generation=%d examined=%d last=%d", answer.Epoch, answer.Generation, answer.ExaminedThrough, answer.LastSequence)
	}
	for _, entry := range answer.Entries {
		if entry.Role != bridgeapi.EntryRoleUser || entry.Sequence <= cursor.after || entry.Sequence > answer.ExaminedThrough {
			t.Fatalf("sync after %d returned entry %d (%s) through %d", cursor.after, entry.Sequence, entry.Kind, answer.ExaminedThrough)
		}
	}
	cursor.after, cursor.digest, cursor.generation = answer.ExaminedThrough, answer.StateDigest, answer.Generation
	return answer.Entries
}

// await syncs until an entry matches, within budget.
func (cursor *syncCursor) await(ctx context.Context, t *testing.T, what string, budget time.Duration, match func(bridgeapi.Entry) bool) bridgeapi.Entry {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		for _, entry := range cursor.next(ctx, t) {
			if match(entry) {
				return entry
			}
		}
	}
	t.Fatalf("%s did not arrive within %s", what, budget)
	return bridgeapi.Entry{}
}

// awaitResult syncs until the tool_result of callID arrives; the first
// Sandbox tool starts the Session's cold workspace.
func (cursor *syncCursor) awaitResult(ctx context.Context, t *testing.T, callID string) bridgeapi.ToolResult {
	t.Helper()
	var found bridgeapi.ToolResult
	cursor.await(ctx, t, "the result of "+callID, 15*time.Minute, func(entry bridgeapi.Entry) bool {
		if entry.Kind != bridgeapi.EntryKindToolResult || entry.Via != cursor.connectionID || entry.Delivered(cursor.connectionID) {
			return false
		}
		for _, result := range entry.ToolResults {
			if result.CallID == callID {
				found = result
				return true
			}
		}
		return false
	})
	return found
}

// within runs one client call with its own budget.
func within[T any](ctx context.Context, call func(context.Context) (T, error)) (T, error) {
	bounded, cancel := context.WithTimeout(ctx, callBudget)
	defer cancel()
	return call(bounded)
}

func requireRefusal(t *testing.T, what string, err error, code bridgeapi.ErrorCode) {
	t.Helper()
	var refusal *platform.RefusalError
	if !errors.As(err, &refusal) || refusal.Body.Code != code || refusal.Status != code.Status() {
		t.Fatalf("%s: want the refusal %s (HTTP %d), got %v", what, code, code.Status(), err)
	}
}

// registerBridgeCaller registers an ordinary account through the business API
// and returns its login, which the test keeps until it ends.
func registerBridgeCaller(ctx context.Context, t *testing.T, accounts pb.AccountServiceClient) *pb.CallerContext {
	t.Helper()
	request := flowid.RandomID(24)
	registered, err := accounts.RegisterAccount(ctx, &pb.RegisterAccountRequest{RequestId: request, Email: "bridge-test-" + request + "@example.com", Password: request})
	login := registered.GetLogin()
	if err != nil || login.GetAccount().GetAccountId() == "" {
		t.Fatalf("the account registration failed (%s)", status.Code(err))
	}
	caller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
	t.Cleanup(func() { _, _ = accounts.Logout(context.WithoutCancel(ctx), &pb.LogoutRequest{Caller: caller}) })
	return caller
}

// createBridgeProject creates a READY Project of owner in a TeamSpace of its
// own; projecttest deletes both when the test ends.
func createBridgeProject(ctx context.Context, t *testing.T, owner string) string {
	t.Helper()
	flow := &projects.ProjectFlow{}
	// This registry supplies definitions only; the lane's server Worker runs the Flows.
	client := &projects.ProjectFlowClient{Dex: dexflowtest.ServerWorkerClient(t, flow), Flow: flow,
		Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: projecttest.IDLengthForTest(t)}
	return projecttest.CreateInTeamSpaceForTest(ctx, t, client, t.Name(), owner, projecttest.TeamSpaceForTest(ctx, t, owner)).ProjectID
}

func sessionAgent(ctx context.Context, t *testing.T, sessions pb.SessionServiceClient, caller *pb.CallerContext, projectID string) *pb.AgentSnapshot {
	t.Helper()
	response, err := sessions.GetSession(ctx, &pb.GetSessionRequest{ProjectId: projectID, Caller: caller})
	if err != nil {
		t.Fatalf("GetSession failed (%s)", status.Code(err))
	}
	return response.GetSession().GetAgent()
}

// awaitSessionWaiting waits until the Project's Session waits for its first message.
func awaitSessionWaiting(ctx context.Context, t *testing.T, sessions pb.SessionServiceClient, caller *pb.CallerContext, projectID string) {
	t.Helper()
	for {
		response, err := sessions.GetSession(ctx, &pb.GetSessionRequest{ProjectId: projectID, Caller: caller})
		if err == nil && response.GetSession().GetAgent().GetDescription().GetStatus() == pb.AgentDescription_STATUS_WAITING_FOR_MESSAGE {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the Session of Project %s never waited for a message (%s)", projectID, status.Code(err))
		case <-time.After(time.Second):
		}
	}
}
