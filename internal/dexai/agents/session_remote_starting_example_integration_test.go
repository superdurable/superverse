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
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestSessionRemoteStartingExampleDexIntegration proves that a connected
// remote agent is told the starting example as a platform segment would carry
// it. A Project starts from the slack-thread-approval example as the server
// records a match, with the facets but, here, no first message, so that its
// Session waits; the test acts as the Bridge through the real remote-agent
// endpoint, syncs until the repository context is ready, as the Bridge does
// for its brief, and that agent context carries the repository context and,
// once after it, the starting_example section with the example, why the
// platform chose it and its trace, then the card_design section, which says
// that the agent's cards carry no Flow design. The lane owns HTTP, SQL, Dex,
// the Sandbox, Git and storage.
func TestSessionRemoteStartingExampleDexIntegration(t *testing.T) {
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
	matched := projects.MatchedExample{Facets: []string{"slack", "thread-reply", "approval"}, Score: 7}
	session := newSessionOf(t, ctx, "", "", func(client *projects.ProjectFlowClient) projects.Project {
		return projecttest.CreateMatchedForTest(ctx, t, client, t.Name(), owner, team, exampleID, matched)
	})
	started := session.waitForSnapshot(ctx, "the matched Session's start", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.StartingExample != nil
	})
	if started.Description.StartingExample.Choice != agentloop.ExampleChoiceMatched || started.Description.ConfirmedRequirements != nil {
		t.Fatalf("the Session did not start quietly from the matched example: %+v", started.Description)
	}
	pair, err := pb.NewRemoteAgentServiceClient(connection).CreateRemoteAgentPairing(ctx, &pb.CreateRemoteAgentPairingRequest{ProjectId: session.identity.ProjectID, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	remote := remoteHTTPClient{t: t, endpoint: pair.GetEndpoint(), client: &http.Client{Timeout: 30 * time.Second}}
	var paired bridgeapi.PairResponse
	remote.post(ctx, bridgeapi.PathPair, bridgeapi.PairRequest{Code: pair.GetCode(), Bridge: bridgeapi.BridgeInfo{Version: bridgeapi.DevBridgeVersion, OS: "darwin", Arch: "arm64"}}, &paired, http.StatusOK)
	remote.token = paired.Token
	var active bridgeapi.ActivateResponse
	binding := bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "starting-example", HostLabel: "integration host"}}
	remote.post(ctx, bridgeapi.PathActivate, binding, &active, http.StatusOK)
	var synced bridgeapi.SyncResponse
	for {
		remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, Context: true}, &synced, http.StatusOK)
		if synced.AgentContextState == bridgeapi.AgentContextReady {
			break
		}
		if synced.AgentContextState == bridgeapi.AgentContextUnavailable {
			t.Fatal("the workspace could not prepare the repository context")
		}
		select {
		case <-ctx.Done():
			t.Fatal("the remote repository context did not become ready")
		case <-time.After(100 * time.Millisecond):
		}
	}
	repository, starting := strings.Index(synced.AgentContext, "<repository_context>"), strings.Index(synced.AgentContext, "<starting_example>")
	if repository < 0 || starting < repository || strings.Contains(synced.AgentContext[starting+1:], "<starting_example>") {
		t.Fatal("the ready remote agent context does not carry the starting example once, after the repository context")
	}
	for _, passage := range []string{
		"<starting_example>",
		"(" + exampleID + ", Git tree",
		"The platform chose it as the closest example to the person's first message, which mentions Slack, a reply in a thread, an approval.",
		"tests: TestThreadApprovalStartsOnRootMessage",
		"</starting_example>",
		"<card_design>\nThe cards you record for this Project carry no Flow design",
	} {
		if !strings.Contains(synced.AgentContext, passage) {
			t.Fatalf("the remote agent context lacks %q", passage)
		}
	}
}
