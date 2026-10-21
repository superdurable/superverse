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
	"encoding/json"
	"fmt"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// actorSession uses real PostgreSQL, business gRPC, Dex, the existing Worker and its Sandbox.
type actorSession struct {
	reader               dexpb.FlowServiceClient
	session              pb.SessionServiceClient
	t                    *testing.T
	project              projects.Project
	service              *agents.SessionFlowService
	client               *sessionagent.SessionAgentFlowClient
	agent                pb.AgentServiceClient
	preview              pb.PreviewServiceClient
	owner, first, second *pb.CallerContext
	admin                accounts.AccountSummary
}

func newActorSession(t *testing.T, ctx context.Context) *actorSession {
	t.Helper()
	db := postgrestest.OpenForTest(t)
	projectFlow, flow, teamSpaceFlow := &projects.ProjectFlow{}, &sessionagent.SessionAgentFlow{}, &teamspaces.TeamSpaceFlow{}
	previewFlow, workspaceFlow := &preview.PreviewFlow{}, &sessionworkspace.SessionWorkspaceFlow{}
	sdk := dexflowtest.ServerWorkerClient(t, projectFlow, flow, teamSpaceFlow, previewFlow, workspaceFlow)
	reader := dexflowtest.ExecutionReaderForTest(t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"))
	client := sessionagent.NewSessionAgentFlowClientForTest(sdk, flow, reader, "")
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmails: []string{"admin@superverse.test"}}
	service := &agents.SessionFlowService{Projects: projectClient, Agent: client, Logger: slog.Default(), Previews: &preview.PreviewFlowClient{Dex: sdk, Flow: previewFlow, Execution: reader}, Workspaces: sessionworkspace.NewSessionWorkspaceFlowClientForTest(sdk, workspaceFlow, reader)}
	dao := accountdao.New(db, []string{"admin@superverse.test"})
	accountService := &accounts.Accounts{DAO: dao, Signer: comm.Must(accounts.NewTokenSigner([]byte(strings.Repeat("a", 32))))}
	connection := serveLoopback(ctx, t, &Server{Boundary: &boundary.Boundary{Logger: slog.Default(), Accounts: accountService}, Sessions: service, SessionAgents: client, SessionModels: SessionModels{Mock: true, Local: true}})
	api := pb.NewAccountServiceClient(connection)
	suffix := flowid.RandomID(10)
	owner, _ := registerNamedCaller(ctx, t, api, "actor-owner-"+suffix)
	first, _ := registerNamedCaller(ctx, t, api, "actor-first-"+suffix)
	second, _ := registerNamedCaller(ctx, t, api, "actor-second-"+suffix)
	teamSpaceID := projecttest.TeamSpaceForTest(ctx, t, owner.GetAccountId())
	teamSpaceClient := &teamspaces.TeamSpaceFlowClient{Dex: sdk, Flow: teamSpaceFlow}
	for _, caller := range []*pb.CallerContext{first, second} {
		if _, err := teamSpaceClient.AddMember(ctx, teamSpaceID, teamspaces.MemberChange{Actor: teamspaces.Actor{AccountID: owner.GetAccountId()}, AccountID: caller.GetAccountId(), Role: teamspaces.RoleMember}); err != nil {
			t.Fatalf("add TeamSpace member: %v", err)
		}
	}
	project := projecttest.CreateInTeamSpaceForTest(ctx, t, projectClient, t.Name(), owner.GetAccountId(), teamSpaceID)
	for _, caller := range []*pb.CallerContext{first, second} {
		if _, err := projectClient.AddMember(ctx, project.ProjectID, projects.MemberChange{ActorAccountID: owner.GetAccountId(), AccountID: caller.GetAccountId(), Role: projects.RoleBuilder}); err != nil {
			t.Fatalf("add Builder: %v", err)
		}
		awaitTrue(ctx, t, "Builder authorization", func() bool {
			_, err := service.AuthorizeSession(ctx, accounts.AccountSummary{AccountID: caller.GetAccountId(), CanonicalEmail: caller.GetCanonicalEmail()}, project.ProjectID)
			return err == nil
		})
	}
	admin, err := dao.AccountByEmail(ctx, "admin@superverse.test")
	if err != nil || admin.Status != accountdao.StatusActive {
		t.Fatalf("requires the actual ACTIVE administrator (%T)", err)
	}
	s := &actorSession{reader: reader, session: pb.NewSessionServiceClient(connection), t: t, project: project, service: service, client: client, agent: pb.NewAgentServiceClient(connection), preview: pb.NewPreviewServiceClient(connection), owner: owner, first: first, second: second, admin: accounts.AccountSummary{AccountID: admin.AccountID, CanonicalEmail: admin.CanonicalEmail, PlatformAdmin: true}}
	initialized := s.await(ctx, "initialized Session", func(v agentloop.AgentSnapshot) bool {
		return v.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	if initialized.Description.Model != conversation.ModelMock {
		t.Fatal("requires mock-default Worker; no real model call allowed")
	}
	return s
}

func (s *actorSession) await(ctx context.Context, what string, predicate func(agentloop.AgentSnapshot) bool) agentloop.AgentSnapshot {
	s.t.Helper()
	var result agentloop.AgentSnapshot
	awaitTrue(ctx, s.t, what, func() bool {
		snapshot, err := s.client.GetSnapshot(ctx, agentloop.SessionFlowID(s.project.ProjectID))
		if err != nil || snapshot.Agent == nil {
			return false
		}
		result = *snapshot.Agent
		return predicate(result)
	})
	return result
}

// entry loads only the selected conversation value, including a hydrated blob when needed.
func (s *actorSession) entry(ctx context.Context, sequence conversation.Sequence) conversation.Entry {
	s.t.Helper()
	id := string(agentloop.SessionFlowID(s.project.ProjectID))
	state, err := s.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: id})
	if err != nil {
		s.t.Fatalf("entry state read: %T", err)
	}
	key := "CurrentMessages/" + agentloop.SequenceKey(sequence)
	for _, attribute := range state.GetAttributes() {
		if attribute.GetKey() != key {
			continue
		}
		value := attribute.GetValue()
		if blob := value.GetInternalBlobIdForObjValue(); blob != "" {
			loaded, err := s.reader.LoadBlobs(ctx, &dexpb.LoadBlobsRequest{Entries: []*dexpb.LoadBlobRequestEntry{{FlowId: id, BlobValue: value}}})
			if err != nil {
				s.t.Fatalf("entry blob read: %T", err)
			}
			value = loaded.GetValues()[blob]
		}
		object := value.GetObjValue()
		if object == nil || object.GetEncoding() != "json" {
			s.t.Fatal("entry is not JSON")
		}
		var entry conversation.Entry
		if err := json.Unmarshal(object.GetPayload(), &entry); err != nil {
			s.t.Fatal("entry JSON decode failed")
		}
		return entry
	}
	s.t.Fatalf("current entry %s not found", key)
	return conversation.Entry{}
}

func (s *actorSession) public(ctx context.Context) *pb.AgentSnapshot {
	s.t.Helper()
	response, err := s.session.GetSession(ctx, &pb.GetSessionRequest{ProjectId: s.project.ProjectID, Caller: s.owner})
	if err != nil {
		s.t.Fatalf("public Session: %v", err)
	}
	return response.GetSession().GetAgent()
}

func requirePublicActor(t *testing.T, got *pb.Actor, caller *pb.CallerContext) {
	t.Helper()
	if got == nil || got.GetAccountId() != caller.GetAccountId() || got.GetEmail() != caller.GetCanonicalEmail() {
		t.Fatalf("public actor = %+v, want account %s", got, caller.GetAccountId())
	}
}

func (s *actorSession) send(ctx context.Context, caller *pb.CallerContext, text string) string {
	s.t.Helper()
	id := flowid.RandomID(24)
	if _, err := s.agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: s.project.ProjectID, MessageId: id, Text: text, Caller: caller}); err != nil {
		s.t.Fatalf("send message (%s)", status.Code(err))
	}
	return id
}

func requireActor(t *testing.T, got *conversation.Actor, caller *pb.CallerContext) {
	t.Helper()
	if got == nil || got.AccountID != caller.GetAccountId() || got.Email != caller.GetCanonicalEmail() {
		t.Fatalf("actor = %+v, want authenticated account %s", got, caller.GetAccountId())
	}
}

func requireActorRefusal(t *testing.T, err error) {
	t.Helper()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("delete was not denied: %v", err)
	}
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Reason == "AGENT_COMMAND_NOT_ADMITTED" {
			return
		}
	}
	t.Fatal("delete refusal lacks AGENT_COMMAND_NOT_ADMITTED")
}

// mockInput uses the embedded scenario catalog, as the real Studio mock composer does.
func mockInput(t *testing.T, name string, number int) mockmodel.Input {
	t.Helper()
	for _, scenario := range mockmodel.Catalog() {
		if scenario.Name == name && number > 0 && number <= len(scenario.Inputs) {
			return scenario.Inputs[number-1]
		}
	}
	t.Fatalf("missing scenario %s input %d", name, number)
	return mockmodel.Input{}
}

func TestSessionActorsDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	s := newActorSession(t, ctx)
	project := s.project.ProjectID
	id := agentloop.SessionFlowID(project)
	// Trusted historical payloads carry no author; loading them remains compatible.
	if err := s.client.SendPendingMessage(ctx, id, conversation.PendingUserMessage{MessageID: "legacy", Value: conversation.UserMessage{Content: "Legacy unattributed message"}}); err != nil {
		t.Fatal(err)
	}
	legacy := s.await(ctx, "legacy history", func(v agentloop.AgentSnapshot) bool {
		for _, m := range v.History.Messages {
			if m.Message.Content == "Legacy unattributed message" {
				if m.Message.Actor != nil || m.Message.SteeredBy != nil {
					t.Fatal("legacy actor or steering invented")
				}
				return v.Description.Status == agentloop.AgentStatusWaitingForMessage
			}
		}
		return false
	})
	collaboration := mockInput(t, "collaboration", 1).Content
	s.send(ctx, s.first, collaboration)
	running := s.await(ctx, "collaboration foreground bash call", func(v agentloop.AgentSnapshot) bool {
		if v.Description.Status != agentloop.AgentStatusExecutingTool || v.Description.Turn <= legacy.Description.Turn {
			return false
		}
		var state agentloop.AgentState
		if !s.actorAttribute(ctx, "AgentState", &state) || state.Status != agentloop.AgentStatusExecutingTool || state.Turn != v.Description.Turn || state.Running == nil || state.Running.Kind != agentloop.RunningTool || state.Running.ToolName != conversation.ToolBash {
			return false
		}
		if state.PendingToolIndex < 0 || state.PendingToolIndex >= len(state.PendingToolCalls) {
			t.Fatal("running serial tool has no pending call at its cursor")
		}
		call := agentloop.PendingToolCall(state)
		if call.Name != conversation.ToolBash {
			t.Fatal("running bash tool cursor identifies another tool")
		}
		var arguments struct {
			Command         string `json:"command"`
			RunInBackground bool   `json:"run_in_background"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
			t.Fatal("running bash call arguments are not JSON")
		}
		if arguments.Command != "sleep 300" || arguments.RunInBackground {
			return false
		}
		var consumed conversation.Sequence
		for _, item := range v.History.Messages {
			if item.Sequence <= legacy.Description.LastSequence {
				continue
			}
			if item.Message.Role == conversation.MessageRoleUser && item.Message.Content == collaboration {
				requireActor(t, item.Message.Actor, s.first)
				consumed = item.Sequence
			}
			if consumed == 0 || item.Sequence <= consumed || item.Message.Role != conversation.MessageRoleAssistant {
				continue
			}
			for _, summary := range item.Message.ToolCalls {
				if summary.ID == call.ID && summary.Name == conversation.ToolBash && summary.Target == agentloop.ToolTarget(call) {
					return true
				}
			}
		}
		return false
	})
	deleteAs := func(message string, caller *pb.CallerContext) error {
		_, err := s.agent.DeleteQueuedMessage(ctx, &pb.DeleteQueuedMessageRequest{ProjectId: project, MessageId: message, Caller: caller})
		return err
	}
	own := s.send(ctx, s.first, "Delete my queued message")
	snapshot := s.await(ctx, "attributed queue", func(v agentloop.AgentSnapshot) bool { return len(v.Queued) == 1 })
	requireActor(t, snapshot.Queued[0].Value.Actor, s.first)
	requirePublicActor(t, s.public(ctx).GetQueued()[0].GetValue().GetActor(), s.first)
	requireActorRefusal(t, deleteAs(own, s.second))
	if err := deleteAs(own, s.first); err != nil {
		t.Fatal(err)
	}
	owned := s.send(ctx, s.first, "Owner may delete this")
	if err := deleteAs(owned, s.owner); err != nil {
		t.Fatal(err)
	}
	adminMessage := s.send(ctx, s.first, "Administrator may delete this")
	// The administrator uses the actual ACTIVE account and SQL authorization.
	// Ordinary callers above also pass the real authenticated gRPC boundary.
	admin, err := s.service.AuthorizeSession(ctx, s.admin, project)
	if err != nil || admin.Role != projects.RoleOwner {
		t.Fatalf("administrator role: %+v (%v)", admin, err)
	}
	if err = s.client.DeleteQueuedMessage(ctx, admin.FlowID, conversation.DeleteQueuedMessageRequest{MessageID: conversation.MessageID(adminMessage), Actor: admin.Actor, IsOwner: admin.Role == projects.RoleOwner}); err != nil {
		t.Fatal(err)
	}
	raced := s.send(ctx, s.first, "Concurrent queue deletion")
	var group sync.WaitGroup
	type deletionResult struct {
		caller *pb.CallerContext
		err    error
	}
	results := make(chan deletionResult, 3)
	for _, caller := range []*pb.CallerContext{s.second, s.first, s.owner} {
		group.Go(func() { results <- deletionResult{caller, deleteAs(raced, caller)} })
	}
	group.Wait()
	close(results)
	successes := 0
	for result := range results {
		err := result.err
		if result.caller == s.second && err == nil {
			t.Fatal("another Builder deleted the author’s message")
		}
		switch status.Code(err) {
		case codes.OK:
			successes++
		case codes.PermissionDenied:
			requireActorRefusal(t, err)
		case codes.FailedPrecondition:
		default:
			t.Fatalf("unexpected concurrent delete: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent deletes admitted %d, want 1", successes)
	}
	steered := s.send(ctx, s.first, mockInput(t, "requirements-card", 1).Content)
	if _, err = s.agent.SteerQueuedMessage(ctx, &pb.SteerQueuedMessageRequest{ProjectId: project, MessageId: steered, Caller: s.second}); err != nil {
		t.Fatal(err)
	}
	snapshot = s.await(ctx, "steered attribution", func(v agentloop.AgentSnapshot) bool { return len(v.Steered) == 1 })
	requireActor(t, snapshot.Steered[0].Value.Actor, s.first)
	requireActor(t, snapshot.Steered[0].SteeredBy, s.second)
	publicSteered := s.public(ctx).GetSteered()[0]
	requirePublicActor(t, publicSteered.GetValue().GetActor(), s.first)
	requirePublicActor(t, publicSteered.GetSteeredBy(), s.second)
	if snapshot.Steered[0].MessageID != conversation.MessageID(steered) {
		t.Fatal("steering changed message identity")
	}
	if _, err = s.agent.InterruptAgent(ctx, &pb.InterruptAgentRequest{ProjectId: project, Turn: running.Description.Turn, Caller: s.first}); err != nil {
		t.Fatal(err)
	}
	// A joined or stale Stop must not overwrite the first actor.
	_, err = s.agent.InterruptAgent(ctx, &pb.InterruptAgentRequest{ProjectId: project, Turn: running.Description.Turn, Caller: s.second})
	if err != nil && status.Code(err) != codes.FailedPrecondition {
		t.Fatal(err)
	}
	// The steered message's turn records the card as version 1 and goes on to its end.
	snapshot = s.await(ctx, "requirements after steering", func(v agentloop.AgentSnapshot) bool {
		return v.Description.ConfirmedRequirements != nil && v.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	stopped := false
	var consumedSequence conversation.Sequence
	for _, m := range snapshot.History.Messages {
		if strings.HasPrefix(m.Message.Content, "Stopped by ") {
			requireActor(t, m.Message.Actor, s.first)
			stopped = true
		}
		if m.Message.Content == mockInput(t, "requirements-card", 1).Content {
			requireActor(t, m.Message.Actor, s.first)
			requireActor(t, m.Message.SteeredBy, s.second)
			requireActor(t, s.entry(ctx, m.Sequence).Meta.SteeredBy, s.second)
			consumedSequence = m.Sequence
		}
	}
	if !stopped {
		t.Fatal("no attributed Stop marker")
	}
	if consumedSequence == 0 {
		t.Fatal("steered message missing from consumed history")
	}
	publicConsumed := false
	for _, item := range s.public(ctx).GetHistory().GetMessages() {
		if item.GetSequence() == int64(consumedSequence) {
			requirePublicActor(t, item.GetMessage().GetActor(), s.first)
			requirePublicActor(t, item.GetMessage().GetSteeredBy(), s.second)
			publicConsumed = true
		}
	}
	if !publicConsumed {
		t.Fatal("consumed steering attribution missing from public history")
	}
	// A recorded card has no confirmer and waits for no one.
	if recorded := snapshot.Description.ConfirmedRequirements; recorded.Revision != 1 || recorded.ConfirmedBy != nil || snapshot.Description.PendingUserInput != nil {
		t.Fatalf("the card is not a recorded version 1 without a confirmer: %+v", recorded)
	}
	s.send(ctx, s.first, mockInput(t, "question-answers", 1).Content)
	snapshot = s.await(ctx, "collaborator questions", func(v agentloop.AgentSnapshot) bool {
		return v.Description.PendingUserInput != nil && v.Description.PendingUserInput.Kind == ""
	})
	if _, err = s.agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project, MessageId: flowid.RandomID(24), Text: "Refused while pending", Caller: s.second}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("pending input admission: %v", err)
	}
	questions := snapshot.Description.PendingUserInput
	answers := mockInput(t, "question-answers", 2).Answers
	questionAnswer := &pb.AgentAnswerQuestionsInput{CallId: string(questions.CallID)}
	for index, question := range questions.Questions {
		questionAnswer.Answers = append(questionAnswer.Answers, &pb.AgentUserInputAnswer{QuestionId: string(question.ID), Answer: answers[index]})
	}
	if _, err = s.agent.AnswerQuestions(ctx, &pb.AnswerQuestionsRequest{ProjectId: project, Caller: s.second, Input: questionAnswer}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.agent.AnswerQuestions(ctx, &pb.AnswerQuestionsRequest{ProjectId: project, Caller: s.first, Input: questionAnswer}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("second question answer: %v", err)
	}
	snapshot = s.await(ctx, "question answer history", func(v agentloop.AgentSnapshot) bool {
		return v.Description.Status == agentloop.AgentStatusWaitingForMessage && v.Description.PendingUserInput == nil
	})
	answered := false
	for _, m := range snapshot.History.Messages {
		if m.Message.AnsweredInputCallID != nil && *m.Message.AnsweredInputCallID == questions.CallID {
			requireActor(t, m.Message.Actor, s.second)
			answered = true
		}
	}
	if !answered {
		t.Fatal("question answer actor absent")
	}
	revision := snapshot.Description.ModelRevision
	if _, err = s.agent.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: project, Model: pb.AgentModel_AGENT_MODEL_MOCK, ExpectedRevision: &revision, Caller: s.first}); err != nil {
		t.Fatal(err)
	}
	// A replay at the acknowledged revision preserves the original changer.
	if _, err = s.agent.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: project, Model: pb.AgentModel_AGENT_MODEL_MOCK, ExpectedRevision: &revision, Caller: s.second}); err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err = s.preview.UpdatePreviewAutoDeploy(ctx, &pb.UpdatePreviewAutoDeployRequest{ProjectId: project, IsEnabled: &disabled, Caller: s.second}); err != nil {
		t.Fatal(err)
	}
	snapshot = s.await(ctx, "settings actors", func(v agentloop.AgentSnapshot) bool {
		return v.Description.ModelRevision == revision+1 && !v.Description.IsPreviewAutoDeployEnabled
	})
	requireActor(t, snapshot.Description.ModelChangedBy, s.first)
	requireActor(t, snapshot.Description.PreviewAutoDeployChangedBy, s.second)
	mapped := s.public(ctx)
	requirePublicActor(t, mapped.GetDescription().GetModelChangedBy(), s.first)
	requirePublicActor(t, mapped.GetDescription().GetPreviewAutoDeployChangedBy(), s.second)
	if mapped.GetDescription().GetConfirmedRequirements().GetConfirmedBy() != nil {
		t.Fatal("the response mapper invented a confirmer of a recorded version")
	}
	if mapped.GetDescription().GetModelChangedBy().GetAccountId() != s.first.GetAccountId() {
		t.Fatal("response mapper dropped actor")
	}
	// New turns move the same immutable actors through the archive boundary.
	for n := 0; n < 12; n++ {
		before := snapshot.Description.LastSequence
		s.send(ctx, s.second, fmt.Sprintf("History retention probe %d", n))
		snapshot = s.await(ctx, "next history turn", func(v agentloop.AgentSnapshot) bool {
			return v.Description.Status == agentloop.AgentStatusWaitingForMessage && v.Description.LastSequence > before+1
		})
	}
	if snapshot.History.NextBeforeSequence == nil {
		t.Fatal("history did not archive")
	}
	history, err := s.session.ListArchivedMessages(ctx, &pb.ListArchivedMessagesRequest{ProjectId: project, BeforeSequence: int64(*snapshot.History.NextBeforeSequence), PageSize: 200, Caller: s.owner})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range history.GetHistory().GetMessages() {
		if m.GetMessage().GetContent() == mockInput(t, "requirements-card", 1).Content {
			requirePublicActor(t, m.GetMessage().GetActor(), s.first)
			requirePublicActor(t, m.GetMessage().GetSteeredBy(), s.second)
			found = true
		}
	}
	if !found {
		t.Fatal("archived author missing")
	}
}

// awaitMockInput fails promptly on a terminal model rejection while
// requiring success of the current input's actual provider-request validation.
func (s *actorSession) awaitMockInput(ctx context.Context, scenario string, input int, after conversation.Sequence, questions bool) agentloop.AgentSnapshot {
	s.t.Helper()
	return s.await(ctx, "validated current-input provider request", func(v agentloop.AgentSnapshot) bool {
		if v.Description.LastSequence <= after {
			return false
		}
		for _, item := range v.History.Messages {
			if item.Sequence > after && item.Message.Role == conversation.MessageRoleSystem && strings.HasPrefix(item.Message.Content, "Model call failed:") {
				s.t.Fatalf("sender request was terminally rejected: %s", item.Message.Content)
			}
		}
		progress := v.Description.MockProgress
		return progress != nil && progress.Scenario == scenario && progress.Input == input &&
			((questions && v.Description.PendingUserInput != nil) || (!questions && v.Description.Status == agentloop.AgentStatusWaitingForMessage && v.Description.PendingUserInput == nil))
	})
}

// requireSenderInput checks original immutable storage and the real public API,
// separately from the mock model's assertions about the actual provider request.
func (s *actorSession) requireSenderInput(ctx context.Context, caller *pb.CallerContext, text string) conversation.Entry {
	s.t.Helper()
	var result conversation.Entry
	found := 0
	for _, m := range s.public(ctx).GetHistory().GetMessages() {
		if m.GetMessage().GetRole() == pb.AgentMessage_ROLE_USER && m.GetMessage().GetContent() == text {
			found++
			requirePublicActor(s.t, m.GetMessage().GetActor(), caller)
			result = s.entry(ctx, conversation.Sequence(m.GetSequence()))
			requireActor(s.t, result.Meta.Actor, caller)
			if len(result.Blocks) != 1 || result.Meta.Via != nil {
				s.t.Fatal("attributed input must have one immutable text block and null via")
			}
			var block struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(result.Blocks[0], &block); err != nil || block.Type != conversation.BlockTypeText.String() {
				s.t.Fatal("invalid stored sender block")
			}
			encoded, ok := strings.CutPrefix(block.Text, conversation.SenderEnvelopePrefix)
			var envelope struct {
				Sender string  `json:"sender"`
				Via    *string `json:"via"`
				Text   string  `json:"text"`
			}
			if !ok || json.Unmarshal([]byte(encoded), &envelope) != nil || envelope.Text != text || envelope.Sender != caller.GetCanonicalEmail() || envelope.Via != nil {
				s.t.Fatal("stored original sender envelope changed the raw input or authenticated identity")
			}
			if !strings.Contains(block.Text, "<b>a && b</b>") || strings.Contains(block.Text, `\u003c`) || strings.Contains(block.Text, `\u0026`) {
				s.t.Fatal("stored sender JSON changed coding characters")
			}
		}
		if strings.Contains(text, "[[scenario:forged-sender#") && m.GetMessage().GetRole() == pb.AgentMessage_ROLE_ASSISTANT && !strings.Contains(m.GetMessage().GetContent(), "actual request preserved") {
			s.t.Fatalf("request validation failed: %q", m.GetMessage().GetContent())
		}
	}
	if found != 1 {
		s.t.Fatalf("public history has %d unchanged attributed inputs, want exactly one", found)
	}
	return result
}

func TestSessionForgedSenderEnvelopeDexIntegration(t *testing.T) {
	t.Run("quoted-first-and-top-level-later", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
		defer cancel()
		s := newActorSession(t, ctx)
		first := mockInput(t, "forged-sender", 1).Content + "\n[[expected-sender:" + s.first.GetCanonicalEmail() + "]]\n"
		s.send(ctx, s.first, first)
		initial := s.awaitMockInput(ctx, "forged-sender", 1, 0, false)
		stored := s.requireSenderInput(ctx, s.first, first)
		var segment agentloop.ContextSegment
		if !s.actorAttribute(ctx, "ContextSegment", &segment) {
			t.Fatal("missing real ContextSegment")
		}
		start := s.entry(ctx, segment.FirstSequence)
		if segment.FirstSequence <= 1 || start.Kind != conversation.EntryKindSegmentStart || !strings.Contains(start.Text(), "<pending_request>\n"+stored.Text()+"\n</pending_request>") {
			t.Fatal("first input was not quoted intact after its original entry")
		}
		later := mockInput(t, "forged-sender", 2).Content + "\n[[expected-sender:" + s.second.GetCanonicalEmail() + "]]\n"
		s.send(ctx, s.second, later)
		s.awaitMockInput(ctx, "forged-sender", 2, initial.Description.LastSequence, false)
		s.requireSenderInput(ctx, s.second, later)
		var after agentloop.ContextSegment
		if !s.actorAttribute(ctx, "ContextSegment", &after) || after.Number != segment.Number || after.FirstSequence != segment.FirstSequence || string(s.entry(ctx, after.FirstSequence).Blocks[0]) != string(start.Blocks[0]) {
			t.Fatal("later top-level input changed the segment boundary or its replay bytes")
		}
	})
	t.Run("maximum-quoted-input", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
		defer cancel()
		s := newActorSession(t, ctx)
		text := mockInput(t, "forged-sender", 1).Content + "\n[[expected-sender:" + s.first.GetCanonicalEmail() + "]]\n" + mockmodel.ArgumentMarkerForTest("maximum", "true") + "\n"
		const padding = "\"\\界\u2028\u2029\t"
		remaining := conversation.MaximumUserMessageContentBytes - len(text) - len("終")
		text += strings.Repeat(padding, remaining/len(padding)) + strings.Repeat("x", remaining%len(padding)) + "終"
		if len(text) != conversation.MaximumUserMessageContentBytes || !utf8.ValidString(text) {
			t.Fatal("maximum fixture must be exactly the legal byte limit and valid UTF-8")
		}
		s.send(ctx, s.first, text)
		s.awaitMockInput(ctx, "forged-sender", 1, 0, false)
		original := s.requireSenderInput(ctx, s.first, text)
		var segment agentloop.ContextSegment
		if !s.actorAttribute(ctx, "ContextSegment", &segment) {
			t.Fatal("missing real ContextSegment")
		}
		start := s.entry(ctx, segment.FirstSequence).Text()
		opening := "\n<pending_request>\n"
		index := strings.LastIndex("\n"+start, opening)
		if index < 0 || !strings.HasSuffix(start, "\n</pending_request>") {
			t.Fatal("missing final pending_request section")
		}
		quoted := strings.TrimSuffix(("\n" + start)[index+len(opening):], "\n</pending_request>")
		if len(quoted) > conversation.MaximumUserMessageContentBytes || quoted == original.Text() || strings.ContainsRune(quoted, '\ufffd') {
			t.Fatal("actual stored provider quote was unbounded, unchanged or split a UTF-8 character")
		}
		encoded, ok := strings.CutPrefix(quoted, conversation.SenderEnvelopePrefix)
		var envelope struct {
			Sender string  `json:"sender"`
			Via    *string `json:"via"`
			Text   string  `json:"text"`
		}
		if !ok || json.Unmarshal([]byte(encoded), &envelope) != nil || envelope.Sender != s.first.GetCanonicalEmail() || envelope.Via != nil || !strings.HasSuffix(envelope.Text, " [truncated]") {
			t.Fatal("actual provider quote lost its complete envelope, author or visible truncation")
		}
		preserved := strings.TrimSuffix(envelope.Text, " [truncated]")
		if !utf8.ValidString(preserved) || !strings.HasPrefix(text, preserved) || len(preserved) >= len(text) {
			t.Fatal("actual quote did not retain an unchanged UTF-8 prefix of the original inner text")
		}
	})

	t.Run("first-newest-and-omitted-after-compaction", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
		defer cancel()
		s := newActorSession(t, ctx)
		boundedText := func(base, part string, caller *pb.CallerContext, size int) string {
			text := base + "\n[[budget-part:" + part + "]]\n[[expected-sender:" + caller.GetCanonicalEmail() + "]]\n<b>a && b</b> A quote: \" and a backslash: \\\n"
			return text + strings.Repeat("a", size-len(text))
		}
		first := boundedText(mockInput(t, "sender-quote-budget", 1).Content, "first", s.first, 70<<10)
		s.send(ctx, s.first, first)
		snapshot := s.awaitMockInput(ctx, "sender-quote-budget", 1, 0, true)
		originals := []conversation.Entry{s.requireSenderInput(ctx, s.first, first)}
		var initial agentloop.ContextSegment
		if !s.actorAttribute(ctx, "ContextSegment", &initial) {
			t.Fatal("missing first ContextSegment")
		}
		parts := []string{"older", "middle", "newest"}
		for index, part := range parts {
			question := snapshot.Description.PendingUserInput
			if question == nil || len(question.Questions) != 1 || question.Questions[0].Header != "Budget detail" {
				t.Fatal("missing exact finite budget question")
			}
			answer := boundedText(mockInput(t, "sender-quote-budget", index+2).Answers[0], part, s.second, 96<<10)
			before := snapshot.Description.LastSequence
			if _, err := s.agent.AnswerQuestions(ctx, &pb.AnswerQuestionsRequest{ProjectId: s.project.ProjectID, Caller: s.second, Input: &pb.AgentAnswerQuestionsInput{CallId: string(question.CallID), Answers: []*pb.AgentUserInputAnswer{{QuestionId: string(question.Questions[0].ID), Answer: answer}}}}); err != nil {
				t.Fatal(err)
			}
			snapshot = s.awaitMockInput(ctx, "sender-quote-budget", index+2, before, index < 2)
			originals = append(originals, s.requireSenderInput(ctx, s.second, "**Budget detail**: "+answer))
		}
		var compacted agentloop.ContextSegment
		if !s.actorAttribute(ctx, "ContextSegment", &compacted) || compacted.Number != initial.Number+1 || compacted.Reason != agentloop.SegmentReasonCompaction {
			t.Fatal("budget case must exercise exactly one real compaction")
		}
		start := s.entry(ctx, compacted.FirstSequence).Text()
		expected := originals[0].Text() + "\n\n[2 pending request entries omitted]\n\n" + originals[3].Text()
		if len(expected) > conversation.MaximumUserMessageContentBytes || !strings.HasSuffix(start, "<pending_request>\n"+expected+"\n</pending_request>") {
			t.Fatal("compacted actual provider quote did not preserve first/newest whole envelopes and exact omission count")
		}
		if !strings.Contains(snapshot.History.Messages[len(snapshot.History.Messages)-1].Message.Content, "actual request retained the first and newest complete envelopes") {
			t.Fatal("actual provider did not validate the bounded multipart quote")
		}
	})
}
