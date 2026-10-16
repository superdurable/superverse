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
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestCreateProjectFirstMessageDexIntegration drives CreateProject with the
// Session's first turn through the actual gRPC boundary against the lane's
// Dex, PostgreSQL, existing Worker and Git host:
//   - a message without its ID, an ID without a message and an unknown model
//     are InvalidArgument, and a model the server does not offer is refused,
//     each before any Flow starts, so the Project does not exist;
//   - a Project created with a mock scenario's first input on the mock model
//     starts its Session with that message admitted once, authored by the
//     creator, and its turn runs; ProjectFlow no longer holds the request. A
//     retry of the same request returns the same Project and admits nothing
//     more, and one that changes the message, its ID or the model is
//     ALREADY_EXISTS;
//   - a Project created with only a model starts its Session on that model at
//     revision 0, with no turn and no model call.
func TestCreateProjectFirstMessageDexIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("CreateProject runs a Project's Setup, which creates its repository: requires SUPERVERSE_TEST_GITHUB_EFFECTS=true")
	}
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	projectFlow, setupFlow, sessionFlow := &projects.ProjectFlow{}, &setup.ProjectSetupFlow{}, &sessionagent.SessionAgentFlow{}
	// This client registry supplies definitions only; the existing Worker
	// runs the Flows with every dependency of its server.
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, setupFlow, sessionFlow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	db := postgrestest.OpenForTest(t)
	idLength := projecttest.IDLengthForTest(t)
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), ProjectIDLength: idLength, PlatformAdminEmail: "admin@superverse.test"}
	sessions := sessionagent.NewSessionAgentFlowClientForTest(sdk, sessionFlow, reader, "")
	accountService := &accounts.Accounts{DAO: accountdao.New(db, "admin@superverse.test"), Signer: comm.Must(accounts.NewTokenSigner([]byte(strings.Repeat("f", 32))))}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	server := func(models SessionModels) pb.ProjectServiceClient {
		return pb.NewProjectServiceClient(serveLoopback(ctx, t, &Server{Boundary: &boundary.Boundary{Logger: slog.Default(), Accounts: accountService},
			ProjectIDLength: idLength, Projects: projectClient, Members: memberdao.New(db), SessionModels: models}))
	}
	local, prod := server(SessionModels{Claude: true, Mock: true, Local: true}), server(SessionModels{Claude: true})
	creator := registerCaller(ctx, t, accountService)
	teamSpaceID := projecttest.TeamSpaceForTest(ctx, t, creator.GetAccountId())
	content := mockInput(t, "requirements-card", 1).Content
	request := func(initial, messageID string, model pb.AgentModel) *pb.CreateProjectRequest {
		return &pb.CreateProjectRequest{Caller: creator, RequestId: flowid.RandomID(24), Name: t.Name(), ManagedRepository: &pb.ManagedRepositorySource{},
			TeamSpaceId: teamSpaceID, InitialMessage: initial, MessageId: messageID, Model: model}
	}

	// Refusals start nothing.
	for _, refusal := range []struct {
		name   string
		api    pb.ProjectServiceClient
		create *pb.CreateProjectRequest
		want   codes.Code
	}{
		{"a message without its ID", local, request(content, "", pb.AgentModel_AGENT_MODEL_MOCK), codes.InvalidArgument},
		{"an ID without a message", local, request("", "first-message", pb.AgentModel_AGENT_MODEL_MOCK), codes.InvalidArgument},
		{"a blank message", local, request("   ", "first-message", pb.AgentModel_AGENT_MODEL_MOCK), codes.InvalidArgument},
		{"an unknown model", local, request(content, "first-message", pb.AgentModel(99)), codes.InvalidArgument},
		{"a model the server does not offer", prod, request(content, "first-message", pb.AgentModel_AGENT_MODEL_MOCK), codes.FailedPrecondition},
	} {
		if _, err := refusal.api.CreateProject(ctx, refusal.create); status.Code(err) != refusal.want {
			t.Fatalf("%s answered %v, want %s", refusal.name, err, refusal.want)
		}
		projectID, _ := flowid.ProjectCreationIDs(creator.GetAccountId(), refusal.create.GetRequestId(), idLength)
		if _, err := local.GetProject(ctx, &pb.GetProjectRequest{Caller: creator, ProjectId: projectID}); status.Code(err) != codes.NotFound {
			t.Fatalf("%s started Project %s: %v", refusal.name, projectID, err)
		}
	}

	// The first message travels with the creation and its Session admits it once.
	messageID := "first-" + flowid.RandomID(12)
	create := request(content, messageID, pb.AgentModel_AGENT_MODEL_MOCK)
	projectID := createWithFirstTurn(ctx, t, local, create, projectClient)
	for _, changed := range []*pb.CreateProjectRequest{
		withChange(create, func(retry *pb.CreateProjectRequest) { retry.InitialMessage = content + " Also add a footer." }),
		withChange(create, func(retry *pb.CreateProjectRequest) { retry.MessageId = "another-" + messageID }),
		withChange(create, func(retry *pb.CreateProjectRequest) { retry.Model = pb.AgentModel_AGENT_MODEL_OPUS_5_5 }),
	} {
		if _, err := local.CreateProject(ctx, changed); status.Code(err) != codes.AlreadyExists {
			t.Fatalf("a retry that changes the Session's first turn answered %v, not AlreadyExists", err)
		}
	}
	projecttest.AwaitReadyForTest(ctx, t, projectClient, projectID)
	if retried, err := local.CreateProject(ctx, create); err != nil || retried.GetProjectId() != projectID {
		t.Fatalf("the same request answered %v (%v), not Project %s", retried, err, projectID)
	}
	id := agentloop.SessionFlowID(projectID)
	agent := awaitSessionSnapshot(ctx, t, sessions, id, "the first message's turn", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.ConfirmedRequirements != nil && agent.Description.Status == agentloop.AgentStatusWaitingForMessage && len(agent.Queued) == 0
	})
	if agent.Description.Model != conversation.ModelMock {
		t.Fatalf("the Session runs on %q, not the mock model of the request", agent.Description.Model)
	}
	var first []conversation.SequencedMessage
	for _, message := range agent.History.Messages {
		if message.Message.Role == conversation.MessageRoleUser {
			first = append(first, message)
		}
	}
	if len(first) != 1 || first[0].Message.Content != content || first[0].Message.Actor == nil || first[0].Message.Actor.AccountID != creator.GetAccountId() {
		t.Fatalf("the Session did not admit the creator's first message exactly once: %+v", first)
	}
	awaitTrue(ctx, t, "ProjectFlow's release of the Session request", func() bool {
		state, err := reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowid.ProjectFlowID(projectID)})
		if err != nil {
			return false
		}
		for _, attribute := range state.GetAttributes() {
			if attribute.GetKey() == "InitialSessionRequest" {
				return false
			}
		}
		return true
	})

	// A model alone starts the Session on it, and nothing runs.
	modelOnly := createWithFirstTurn(ctx, t, local, request("", "", pb.AgentModel_AGENT_MODEL_OPUS_5_5), projectClient)
	projecttest.AwaitReadyForTest(ctx, t, projectClient, modelOnly)
	started := awaitSessionSnapshot(ctx, t, sessions, agentloop.SessionFlowID(modelOnly), "the Session of a model alone", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	if started.Description.Model != conversation.ModelOpus55 || started.Description.ModelRevision != 0 || len(started.Queued) != 0 || started.Description.Turn != 0 {
		t.Fatalf("the Session of a model alone is %q at revision %d in turn %d", started.Description.Model, started.Description.ModelRevision, started.Description.Turn)
	}
	t.Logf("Project %s admitted its first message once on %s; Project %s started on %s", projectID, id, modelOnly, started.Description.Model)
}

// createWithFirstTurn creates the Project of create and deletes it when the test ends.
func createWithFirstTurn(ctx context.Context, t *testing.T, api pb.ProjectServiceClient, create *pb.CreateProjectRequest, client *projects.ProjectFlowClient) string {
	t.Helper()
	created, err := api.CreateProject(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	projectID := created.GetProjectId()
	t.Cleanup(func() { projecttest.DeleteForTest(context.WithoutCancel(ctx), t, client, projectID) })
	return projectID
}

// withChange is a copy of create that change edits.
func withChange(create *pb.CreateProjectRequest, change func(*pb.CreateProjectRequest)) *pb.CreateProjectRequest {
	retry := &pb.CreateProjectRequest{Caller: create.GetCaller(), RequestId: create.GetRequestId(), Name: create.GetName(), ManagedRepository: create.GetManagedRepository(),
		TeamSpaceId: create.GetTeamSpaceId(), InitialMessage: create.GetInitialMessage(), MessageId: create.GetMessageId(), Model: create.GetModel()}
	change(retry)
	return retry
}

// awaitSessionSnapshot polls the Session until done accepts its conversation.
func awaitSessionSnapshot(ctx context.Context, t *testing.T, client *sessionagent.SessionAgentFlowClient, id agentloop.FlowID, what string, done func(agentloop.AgentSnapshot) bool) agentloop.AgentSnapshot {
	t.Helper()
	var agent agentloop.AgentSnapshot
	awaitTrue(ctx, t, what, func() bool {
		snapshot, err := client.GetSnapshot(ctx, id)
		if err != nil || snapshot.Agent == nil {
			return false
		}
		agent = *snapshot.Agent
		return done(agent)
	})
	return agent
}
