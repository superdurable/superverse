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
	"net/http"
	"strings"
	"testing"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Real endpoint/SQL/Dex admission covers compatibility, removal and idle takeover.
func TestSessionRemoteIdleAdmissionHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	project := session.identity.ProjectID
	connectionID := func() string {
		t.Helper()
		listed, err := api.ListRemoteAgentConnections(ctx, &pb.ListRemoteAgentConnectionsRequest{ProjectId: project, Caller: caller})
		if err != nil || len(listed.GetConnections()) != 1 {
			t.Fatalf("expected one paired connection: %v", err)
		}
		return listed.Connections[0].ConnectionId
	}
	old := pairRemoteHTTP(ctx, t, api, caller, project)
	var active bridgeapi.ActivateResponse
	old.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{BridgeVersion: "0.0.1", Binding: bridgeapi.Binding{ProviderThreadID: "old-build"}}, &active, http.StatusOK)
	id := connectionID()
	if _, err := api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: project, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	var idle bridgeapi.SyncResponse
	old.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch}, &idle, http.StatusOK)
	if !idle.Idle {
		t.Fatal("Platform selection revoked or activated the old-build connection")
	}
	if _, err := api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: project, BasisEpoch: idle.Epoch, ConnectionId: id, Caller: caller}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("old-build resume: code=%s", status.Code(err))
	}
	if _, err := api.RevokeRemoteAgentConnection(ctx, &pb.RevokeRemoteAgentConnectionRequest{ProjectId: project, ConnectionId: id, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	afterRemoval, err := session.client.ReadExecutor(ctx, project)
	if err != nil || afterRemoval.Suspended != nil || afterRemoval.Epoch != idle.Epoch || afterRemoval.Generation != idle.Generation {
		t.Fatalf("removing idle connection changed Platform authority or retained resume identity: %v", err)
	}
	var refusal bridgeapi.ErrorBody
	old.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch}, &refusal, http.StatusUnauthorized)
	current := pairRemoteHTTP(ctx, t, api, caller, project)
	binding := bridgeapi.ActivateRequest{BridgeVersion: bridgeapi.DevBridgeVersion, Binding: bridgeapi.Binding{ProviderThreadID: "current-build"}}
	current.post(ctx, bridgeapi.PathActivate, binding, &active, http.StatusOK)
	id = connectionID()
	if _, err := api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: project, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	current.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch}, &idle, http.StatusOK)
	session.send(ctx, mockInput(t, "remote-switchback", 1).Content+" "+mockmodel.ArgumentMarkerForTest("head", session.identity.HeadCommit))
	platform := session.finishedTurn(ctx)
	if !strings.Contains(platform.History.Messages[len(platform.History.Messages)-1].Message.Content, "The Platform switchback turn is complete.") {
		t.Fatal("mock Platform turn did not replay its remote handoff")
	}
	// A fresh pairing at the Platform epoch takes over, permanently ending idle resume.
	takeover := pairRemoteHTTP(ctx, t, api, caller, project)
	var taken bridgeapi.ActivateResponse
	takeover.post(ctx, bridgeapi.PathActivate, binding, &taken, http.StatusOK)
	current.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch}, &refusal, http.StatusConflict)
	if refusal.Code != bridgeapi.ErrorExecutorChanged {
		t.Fatal("idle connection remained eligible after takeover")
	}
	if _, err := api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: project, BasisEpoch: taken.Epoch, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: project, BasisEpoch: taken.Epoch + 1, ConnectionId: id, Caller: caller}); status.Code(err) != codes.Aborted {
		t.Fatalf("replaced idle connection resumed: %s", status.Code(err))
	}
	// Explicit idle disconnect clears eligibility without interrupting Platform.
	var disconnected bridgeapi.DisconnectResponse
	takeover.post(ctx, bridgeapi.PathDisconnect, bridgeapi.DisconnectRequest{}, &disconnected, http.StatusOK)
	afterDisconnect, err := session.client.ReadExecutor(ctx, project)
	if err != nil || afterDisconnect.Suspended != nil || afterDisconnect.Epoch != taken.Epoch+1 {
		t.Fatalf("idle disconnect changed Platform epoch: %v", err)
	}
}
