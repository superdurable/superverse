// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
)

// closestExampleLane reaches the lane's Dex, PostgreSQL, existing Worker and
// Git host from this process: the Project and Session Flow clients, and API
// servers of this build with agent.closestExamplePick on or off that a
// registered creator calls in a TeamSpace of their own.
type closestExampleLane struct {
	projects    *projects.ProjectFlowClient
	sessions    *sessionagent.SessionAgentFlowClient
	server      func(pick bool) pb.ProjectServiceClient
	creator     *pb.CallerContext
	teamSpaceID string
}

// newClosestExampleLane connects to the lane that the test inputs name, which
// CreateProject's Setup creates repositories on: it requires
// SUPERVERSE_TEST_GITHUB_EFFECTS=true.
func newClosestExampleLane(ctx context.Context, t *testing.T) closestExampleLane {
	t.Helper()
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("CreateProject runs a Project's Setup, which creates its repository: requires SUPERVERSE_TEST_GITHUB_EFFECTS=true")
	}
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")

	projectFlow, setupFlow, sessionFlow := &projects.ProjectFlow{}, &setup.ProjectSetupFlow{}, &sessionagent.SessionAgentFlow{}
	// This client registry supplies definitions only; the existing Worker
	// runs the Flows with every dependency of its server.
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Pull: true}, projectFlow, setupFlow, sessionFlow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	db := postgrestest.OpenForTest(t)
	idLength := projecttest.IDLengthForTest(t)
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), ProjectIDLength: idLength, PlatformAdminEmails: []string{"admin@superverse.test"}}
	accountService := &accounts.Accounts{DAO: accountdao.New(db, []string{"admin@superverse.test"}), Signer: comm.Must(accounts.NewTokenSigner([]byte(strings.Repeat("f", 32))))}
	creator := registerCaller(ctx, t, accountService)
	return closestExampleLane{
		projects: projectClient,
		sessions: sessionagent.NewSessionAgentFlowClientForTest(sdk, sessionFlow, reader, ""),
		server: func(pick bool) pb.ProjectServiceClient {
			return pb.NewProjectServiceClient(serveLoopback(ctx, t, &Server{Boundary: &boundary.Boundary{Logger: slog.Default(), Accounts: accountService},
				ProjectIDLength: idLength, Projects: projectClient, Members: memberdao.New(db), SessionModels: SessionModels{Claude: true, Mock: true, Local: true},
				ClosestExamplePick: pick}))
		},
		creator:     creator,
		teamSpaceID: projecttest.TeamSpaceForTest(ctx, t, creator.GetAccountId()),
	}
}

// TestCreateProjectClosestExampleDexIntegration drives the closest-example
// pick of CreateProject through the actual gRPC boundary against the lane's
// Dex, PostgreSQL, existing Worker and Git host, with two API servers of this
// build, one with agent.closestExamplePick on and one with it off:
//   - with the pick on, the first input of the example-matched scenario starts
//     the Project from the example this build's decision matches it to,
//     slack-thread-approval, under that example's revision
//     app-template/<id>@<tree>, which GetProject names; its Session starts
//     from it as matched, with the facets the decision found, greets with no
//     notice and admits the first message once as the request;
//   - a retry of the same request returns the same Project and keeps its
//     revision, also on the server with the pick off, which would start the
//     request from the default;
//   - the same text with the pick off, and a text that is only a mock-model
//     marker with the pick on, start from the default revision
//     app-template@<tree>, whose Sessions name no starting example.
func TestCreateProjectClosestExampleDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	lane := newClosestExampleLane(ctx, t)
	projectClient, sessions := lane.projects, lane.sessions
	picking, plain := lane.server(true), lane.server(false)
	creator, teamSpaceID := lane.creator, lane.teamSpaceID
	content := mockInput(t, "example-matched", 1).Content
	request := func(initial string) *pb.CreateProjectRequest {
		return &pb.CreateProjectRequest{Caller: creator, RequestId: flowid.RandomID(24), Name: t.Name(), ManagedRepository: &pb.ManagedRepositorySource{},
			TeamSpaceId: teamSpaceID, InitialMessage: initial, MessageId: "first-" + flowid.RandomID(12), Model: pb.AgentModel_AGENT_MODEL_MOCK}
	}
	expected, isMatch := apptemplate.ClosestExample(mockmodel.WithoutMarkers(content))
	if !isMatch || expected.ExampleID != "slack-thread-approval" {
		t.Fatalf("this build matches the example-matched first message to %+v, not slack-thread-approval", expected)
	}
	example := comm.Must(apptemplate.ExampleTemplate(expected.ExampleID))

	// With the pick on, the Project starts from the matched example's tree.
	create := request(content)
	matchedID := createWithFirstTurn(ctx, t, picking, create, projectClient)
	project := projecttest.AwaitReadyForTest(ctx, t, projectClient, matchedID)
	if project.TemplateRevision != example.Revision {
		t.Fatalf("the matched Project's revision is %q, not %s", project.TemplateRevision, example.Revision)
	}
	if retried, err := picking.CreateProject(ctx, create); err != nil || retried.GetProjectId() != matchedID {
		t.Fatalf("the same request answered %v (%v), not Project %s", retried, err, matchedID)
	}
	if retried, err := plain.CreateProject(ctx, create); err != nil || retried.GetProjectId() != matchedID {
		t.Fatalf("the same request on a server with the pick off answered %v (%v), not Project %s", retried, err, matchedID)
	}
	read, err := picking.GetProject(ctx, &pb.GetProjectRequest{Caller: creator, ProjectId: matchedID})
	if err != nil || read.GetProject().GetTemplateRevision() != example.Revision || read.GetProject().GetExample().GetExampleId() != expected.ExampleID {
		t.Fatalf("GetProject after the retry answered %v (%v), not the example's revision %s", read.GetProject(), err, example.Revision)
	}
	agent := awaitSessionSnapshot(ctx, t, sessions, agentloop.SessionFlowID(matchedID), "the matched Session's start", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.StartingExample != nil && len(agent.Queued) == 0
	})
	started := agent.Description.StartingExample
	if started.ExampleID != expected.ExampleID || started.Tree != example.TreeID || started.Choice != agentloop.ExampleChoiceMatched || !slices.Equal(started.MatchedOn, expected.Facets) {
		t.Fatalf("the Session starts from %+v, not %s at %s matched on %v", started, expected.ExampleID, example.TreeID, expected.Facets)
	}
	var users []conversation.SequencedMessage
	for _, message := range agent.History.Messages {
		if message.Message.Role == conversation.MessageRoleUser {
			users = append(users, message)
		}
		if message.Message.Role == conversation.MessageRoleSystem && strings.Contains(message.Message.Content, "starts from the") {
			t.Fatalf("a matched Session greets with the example: %q", message.Message.Content)
		}
	}
	if len(users) != 1 || users[0].Message.Content != content || users[0].Sequence != agent.History.Messages[0].Sequence {
		t.Fatalf("the matched Session does not open with the creator's first message, once: %+v", users)
	}

	// With the pick off, and for a marker alone, the Project starts from the default.
	marker := content[strings.Index(content, "[["):]
	for _, creation := range []struct {
		what string
		api  pb.ProjectServiceClient
		text string
	}{{"the pick off", plain, content}, {"a marker alone", picking, marker}} {
		projectID := createWithFirstTurn(ctx, t, creation.api, request(creation.text), projectClient)
		if revision := projecttest.AwaitReadyForTest(ctx, t, projectClient, projectID).TemplateRevision; revision != apptemplate.Current().Revision {
			t.Fatalf("with %s the Project's revision is %q, not the default %s", creation.what, revision, apptemplate.Current().Revision)
		}
		plainAgent := awaitSessionSnapshot(ctx, t, sessions, agentloop.SessionFlowID(projectID), "the default Session's start", func(agent agentloop.AgentSnapshot) bool {
			return agent.Description.LastSequence > 0
		})
		if plainAgent.Description.StartingExample != nil {
			t.Fatalf("with %s the Session starts from %+v", creation.what, plainAgent.Description.StartingExample)
		}
	}
	t.Logf("Project %s started from %s matched on %v (score %d)", matchedID, example.Revision, expected.Facets, expected.Score)
}
