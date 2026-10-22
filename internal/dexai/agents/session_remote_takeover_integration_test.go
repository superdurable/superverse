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
	"net/http"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func remoteSessionAPI(ctx context.Context, t *testing.T) (*mockSession, pb.RemoteAgentServiceClient, *pb.CallerContext) {
	t.Helper()
	connection, err := grpc.NewClient(dexflowtest.Setting(t, "SUPERVERSE_TEST_API_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	caller := registerCaller(ctx, t, pb.NewAccountServiceClient(connection))
	owner := caller.GetAccountId()
	team := projecttest.TeamSpaceForTest(ctx, t, owner)
	return newMockSessionIn(t, ctx, owner, team, t.Name(), ""), pb.NewRemoteAgentServiceClient(connection), caller
}

func pairRemoteHTTP(ctx context.Context, t *testing.T, api pb.RemoteAgentServiceClient, caller *pb.CallerContext, project string) remoteHTTPClient {
	t.Helper()
	pairing, err := api.CreateRemoteAgentPairing(ctx, &pb.CreateRemoteAgentPairingRequest{ProjectId: project, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	remote := remoteHTTPClient{t: t, endpoint: pairing.GetEndpoint(), client: &http.Client{Timeout: 30 * time.Second}}
	var paired bridgeapi.PairResponse
	remote.post(ctx, bridgeapi.PathPair, bridgeapi.PairRequest{Code: pairing.GetCode(), Bridge: bridgeapi.BridgeInfo{Version: bridgeapi.DevBridgeVersion, OS: "darwin", Arch: "arm64"}}, &paired, http.StatusOK)
	remote.token = paired.Token
	return remote
}

// Both actual HTTP activations carry the immutable basis recorded before the
// race. There is no test server, mocked SQL or alternate Session worker.
func TestSessionRemoteConcurrentActivationHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	clients := []remoteHTTPClient{pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID), pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)}
	binding := bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "activation-race", HostLabel: "integration host"}}
	type activation struct {
		index, status int
		body          []byte
		err           error
	}
	results := make(chan activation, 2)
	start := make(chan struct{})
	for index := range clients {
		go func(index int) {
			<-start
			status, body, err := clients[index].request(ctx, bridgeapi.PathActivate, binding)
			results <- activation{index: index, status: status, body: body, err: err}
		}(index)
	}
	close(start)
	winner := -1
	var admitted bridgeapi.ActivateResponse
	for count := 0; count < 2; count++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("activation request failed (%T)", result.err)
		}
		switch result.status {
		case http.StatusOK:
			if winner >= 0 {
				t.Fatal("concurrent same-basis activations both won")
			}
			winner = result.index
			if err := json.Unmarshal(result.body, &admitted); err != nil {
				t.Fatal(err)
			}
		case http.StatusConflict:
			var refusal bridgeapi.ErrorBody
			if err := json.Unmarshal(result.body, &refusal); err != nil {
				t.Fatal(err)
			}
			if refusal.Code != bridgeapi.ErrorExecutorChanged {
				t.Fatalf("activation loser returned %s", refusal.Code)
			}
		default:
			t.Fatalf("activation returned %d", result.status)
		}
	}
	if winner < 0 || admitted.Epoch != 1 || admitted.Generation != 1 {
		t.Fatalf("activation race has no unique first winner: index=%d epoch=%d generation=%d", winner, admitted.Epoch, admitted.Generation)
	}
	var same bridgeapi.ActivateResponse
	clients[winner].post(ctx, bridgeapi.PathActivate, binding, &same, http.StatusOK)
	if same.Epoch != admitted.Epoch || same.Generation != admitted.Generation {
		t.Fatal("winner retry advanced fence")
	}
	var refusal bridgeapi.ErrorBody
	clients[1-winner].post(ctx, bridgeapi.PathActivate, binding, &refusal, http.StatusConflict)
	if refusal.Code != bridgeapi.ErrorExecutorChanged {
		t.Fatal("delayed old-basis activation recaptured")
	}
	next := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var taken bridgeapi.ActivateResponse
	next.post(ctx, bridgeapi.PathActivate, binding, &taken, http.StatusOK)
	if taken.Epoch != 2 || taken.Generation != 2 {
		t.Fatal("new-basis takeover did not advance atomically")
	}
	clients[winner].post(ctx, bridgeapi.PathActivate, binding, &refusal, http.StatusConflict)
	if refusal.Code != bridgeapi.ErrorExecutorChanged {
		t.Fatal("replaced connection recaptured")
	}
	stale := bridgeapi.Report{Epoch: admitted.Epoch, Generation: admitted.Generation, Sequence: 1, TurnCompleted: true}
	clients[winner].post(ctx, bridgeapi.PathReport, stale, &refusal, http.StatusConflict)
	if refusal.Code != bridgeapi.ErrorExecutorChanged {
		t.Fatal("replaced connection report admitted")
	}
	current, err := session.client.ReadExecutor(ctx, session.identity.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Epoch != taken.Epoch || current.Generation != taken.Generation {
		t.Fatal("HTTP authority differs from canonical Session")
	}
}
