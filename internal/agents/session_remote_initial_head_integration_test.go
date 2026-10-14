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
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// READY setup publishes a validated source SHA before the Session starts;
// activation and switch need neither a Sandbox tool nor an unknown HEAD fallback.
func TestSessionRemoteInitialHeadBeforeWorkspaceHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	before, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || !flowid.IsCommitSHA(before.Session.Identity.HeadCommit) || before.Session.Workspace.RequestID != "" || before.Agent.Description.ContextModel != "" {
		t.Fatal("fresh Session does not have its READY source HEAD before workspace/context")
	}
	head := before.Session.Identity.HeadCommit
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "initial-head", ProviderThreadID: "initial-head", HostLabel: "integration host"}}, &active, http.StatusOK)
	if _, err = api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: session.identity.ProjectID, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	switched, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || switched.Session.Identity.HeadCommit != head || switched.Session.Workspace.RequestID != "" || switched.Agent.Description.Executor.Mode != remoteexec.ModePlatform || switched.Agent.Description.Executor.Handoff == nil || switched.Agent.Description.ContextModel != "" {
		t.Fatal("pre-workspace switch did not retain HEAD and deferred handoff without a model/context")
	}
	session.send(ctx, "Continue after switching before my first remote tool.")
	finished := session.finishedTurnAfter(ctx, switched.Agent.Description.LastSequence)
	requireNoFailure(t, finished.History)
	if finished.Description.Executor.Handoff != nil || !strings.Contains(finished.History.Messages[len(finished.History.Messages)-1].Message.Content, "The mock model answers only scenario inputs") {
		t.Fatal("first platform turn could not render the typed handoff with its initial HEAD")
	}
	if current := session.summary(ctx); current.Identity.HeadCommit != head || current.Workspace.RequestID != "" {
		t.Fatal("context-only continuation replaced HEAD or unnecessarily provisioned a workspace")
	}
}
