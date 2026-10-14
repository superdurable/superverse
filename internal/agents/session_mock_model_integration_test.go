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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/agents/platformissue"
	"github.com/superdurable/superversev2/internal/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/projectdao"
	"github.com/superdurable/superversev2/internal/projects/projecttest"
)

// These scenarios run whole Agent turns on the one Session of a throwaway
// READY Project that each test creates (projecttest), on the actual Worker
// whose server offers the mock Claude model (agent.mockModel: true, the Local
// Kind and Native default), on which every new Session starts. Dex, the
// Sandbox and its tools are real: each Session starts its own workspace
// through the real cold path, and every tool runs in the coding container.
// Only the model is the mock, so every model call is a real HTTP request that
// the mock model's replay check validates: a call that replayed a changed
// block is answered with a 400 and ends its turn with a recorded failure,
// which these scenarios would observe. None changes the Project's source, and
// the Project's deletion at the end of the test ends the Session.

// mockTurnTimeout bounds a Project's creation, its Session's cold workspace
// and one turn.
const mockTurnTimeout = 25 * time.Minute

// mockInputTokens is the input usage the mock model reports for a reply
// that scripts none; a real model reports another number.
const mockInputTokens = 1000

// mockSession is the Session of a Project a test created, mostly on the mock
// model, with the clients of its Project and of the Flows that own and
// provision its instances, serve its Preview, run its subagents and report its platform
// faults, the engine reader of their histories and the Project's owner
// account.
type mockSession struct {
	t          *testing.T
	client     *sessionagent.SessionAgentFlowClient
	projects   *projects.ProjectFlowClient
	provisions *provision.SandboxProvisionFlowClient
	workspaces *sessionworkspace.SessionWorkspaceFlowClient
	previews   *preview.PreviewFlowClient
	subagents  *subagent.SubagentFlowClient
	issues     *platformissue.PlatformIssueFlowClient
	// contributions fronts the Session's connector contributions.
	contributions *contribution.ConnectorContributionFlowClient
	reader        dexpb.FlowServiceClient
	id            agentloop.FlowID
	identity      agentloop.SessionIdentity
	account       string
	// teamSpaceID is the TeamSpace of the Session's Project, whose
	// connections the Project selects.
	teamSpaceID string
	// seeded are TeamSpace connections a test created itself, by connection
	// name, which configureConnections selects instead of creating one, and
	// bindings the trigger binding configurations it saves.
	seeded   map[string]string
	bindings []triggerBindingConfiguration
}

// triggerBindingConfiguration is one trigger binding a person configures in
// Studio.
type triggerBindingConfiguration struct {
	connectorID, connectionName, triggerName, bindingName string
	configuration                                         map[string]any
}

// newMockSession creates a Project, whose Session starts on the mock model,
// and sends content as the Session's first message.
func newMockSession(t *testing.T, ctx context.Context, content string) *mockSession {
	t.Helper()
	return newSession(t, ctx, conversation.ModelMock, content)
}

// newSession creates a Project and waits until its Session, which the
// Project started when it became READY, waits for a message. It selects model
// unless the Session started on it or model is empty, and sends content as the
// first message unless it is empty. The Project's deletion at the end of the
// test ends the Session and releases its workspace.
func newSession(t *testing.T, ctx context.Context, model conversation.Model, content string) *mockSession {
	t.Helper()
	return newSessionOf(t, ctx, model, content, func(client *projects.ProjectFlowClient) projects.Project {
		return projecttest.CreateForTest(ctx, t, client, t.Name())
	})
}

// newMockSessionIn is newMockSession for a Project of owner in owner's
// TeamSpace teamSpaceID, named name.
func newMockSessionIn(t *testing.T, ctx context.Context, owner, teamSpaceID, name, content string) *mockSession {
	t.Helper()
	return newSessionOf(t, ctx, conversation.ModelMock, content, func(client *projects.ProjectFlowClient) projects.Project {
		return projecttest.CreateInTeamSpaceForTest(ctx, t, client, name, owner, teamSpaceID)
	})
}

// newSessionOf is newSession for the Project that create creates.
func newSessionOf(t *testing.T, ctx context.Context, model conversation.Model, content string, create func(*projects.ProjectFlowClient) projects.Project) *mockSession {
	t.Helper()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	projectFlow, provisionFlow, previewFlow, flow, subagentFlow := &projects.ProjectFlow{}, &provision.SandboxProvisionFlow{}, &preview.PreviewFlow{}, &sessionagent.SessionAgentFlow{}, &subagent.SubagentFlow{}
	issueFlow, contributionFlow, workspaceFlow := &platformissue.PlatformIssueFlow{}, &contribution.ConnectorContributionFlow{}, &sessionworkspace.SessionWorkspaceFlow{}
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, provisionFlow, previewFlow, flow, subagentFlow, issueFlow, contributionFlow, workspaceFlow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	// This client registry supplies definitions only. Real handlers and every
	// application dependency belong to the existing Worker selected above,
	// which starts the Session with its own start model.
	projectsClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmail: "admin@superverse.test"}
	project := create(projectsClient)
	s := &mockSession{t: t, client: sessionagent.NewSessionAgentFlowClientForTest(sdk, flow, reader, ""), projects: projectsClient, reader: reader, account: project.CreatorAccountID, teamSpaceID: project.TeamSpaceID,
		id: agentloop.SessionFlowID(project.ProjectID), identity: agentloop.SessionIdentity{ProjectID: project.ProjectID, Profile: agentloop.ProfileSessionV1, HeadCommit: project.AppManifest.SourceCommit},
		provisions: provision.NewSandboxProvisionFlowClientForTest(sdk, provisionFlow, reader, deploymentnames.MustFor(deploymentnames.EnvironmentTest)),
		workspaces: sessionworkspace.NewSessionWorkspaceFlowClientForTest(sdk, workspaceFlow, reader),
		previews:   &preview.PreviewFlowClient{Dex: sdk, Flow: previewFlow, Execution: reader}, subagents: subagent.NewSubagentFlowClientForTest(sdk, subagentFlow),
		issues:        platformissue.NewPlatformIssueFlowClientForTest(sdk, issueFlow, deploymentnames.EnvironmentTest),
		contributions: contribution.NewConnectorContributionFlowClientForTest(sdk, contributionFlow)}
	started := s.awaitStarted(ctx)
	if model != "" && started.Model != model {
		if err := s.client.UpdateSessionModel(ctx, s.id, agentloop.UpdateSessionModelRequest{Model: model, ExpectedRevision: started.ModelRevision}); err != nil {
			t.Fatalf("selecting %s for %s failed (%T)", model, s.id, err)
		}
	}
	t.Logf("Session %s of the throwaway Project %s", s.id, project.ProjectID)
	if content != "" {
		s.send(ctx, content)
	}
	return s
}

// awaitStarted waits until the Project's Session, which ProjectFlow starts
// once the Project is READY, runs as the Session of exactly this Project and
// waits for its first message, and returns its description.
func (s *mockSession) awaitStarted(ctx context.Context) agentloop.AgentDescription {
	s.t.Helper()
	for {
		snapshot, err := s.client.GetSnapshot(ctx, s.id)
		if err == nil && snapshot.Session.Identity != s.identity {
			s.t.Fatalf("Session %s runs for %+v, not for %+v", s.id, snapshot.Session.Identity, s.identity)
		}
		if err == nil && snapshot.Agent != nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage {
			return snapshot.Agent.Description
		}
		select {
		case <-ctx.Done():
			s.t.Fatalf("the Session %s never waited for its first message (%s)", s.id, dexflowtest.ErrorCategory(err))
		case <-time.After(time.Second):
		}
	}
}

// finishedTurn waits until the Session consumed its message and waits for the
// next one, and returns that Snapshot. A Session that ends first, or whose
// engine execution stops, fails the test.
func (s *mockSession) finishedTurn(ctx context.Context) agentloop.AgentSnapshot {
	s.t.Helper()
	for {
		snapshot, err := s.client.GetSnapshot(ctx, s.id)
		if err == nil {
			if snapshot.Session.Status != agentloop.SessionStatusActive {
				s.t.Fatalf("Session %s ended as %s before its turn finished", s.id, snapshot.Session.Status)
			}
			if snapshot.ExecutionFailure != agentloop.ExecutionRunning {
				s.t.Fatalf("the engine execution of Session %s stopped as %s before its turn finished", s.id, snapshot.ExecutionFailure)
			}
			agent := snapshot.Agent
			if agent != nil && agent.Description.LastSequence > 0 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage &&
				agent.Description.PendingQueuedMessageCount == 0 && agent.Description.PendingSteeredMessageCount == 0 {
				return *agent
			}
		}
		select {
		case <-ctx.Done():
			s.t.Fatalf("the turn of %s never finished; reconcile it (%v)", s.id, err)
		case <-time.After(2 * time.Second):
		}
	}
}

// replies returns the assistant messages of a history, in order.
func replies(history conversation.HistoryPage) []conversation.SequencedMessage {
	var assistant []conversation.SequencedMessage
	for _, message := range history.Messages {
		if message.Message.Role == conversation.MessageRoleAssistant {
			assistant = append(assistant, message)
		}
	}
	return assistant
}

// requireMock fails unless the reply came from the mock model, which
// reports the input usage its script gives; a real model reports another.
func requireMock(t *testing.T, reply conversation.SequencedMessage, wantInput int64) {
	t.Helper()
	call := reply.Message.ModelCall
	if call == nil || call.InputTokens == nil || *call.InputTokens != wantInput {
		t.Fatalf("the server does not run the mock model; deploy a profile with agent.mockModel on (reply %d)", reply.Sequence)
	}
}

// requireNoFailure fails when the turn recorded a failed model call, as an
// answered replay mismatch or a rejected request does.
func requireNoFailure(t *testing.T, history conversation.HistoryPage) {
	t.Helper()
	for _, message := range history.Messages {
		if message.Message.Role == conversation.MessageRoleSystem && strings.HasPrefix(message.Message.Content, "Model call failed") {
			t.Fatalf("a model call failed at message %d: %s", message.Sequence, message.Message.Content)
		}
	}
}

// The inspect-basic scenario is a read-only tool loop in a real coding
// container: a parallel search and listing, a read, a command and a progress
// list, then a final text. Five mock replies each replay every earlier one,
// so the mock model's replay check runs on every call after the first.
func TestSessionMockToolLoopDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	assistant := replies(agent.History)
	if len(assistant) != 5 {
		t.Fatalf("expected the 5 mock replies, got %d", len(assistant))
	}
	for _, reply := range assistant {
		requireMock(t, reply, mockInputTokens)
	}
	wantTools := [][]conversation.ToolName{
		{conversation.ToolGrep, conversation.ToolGlob}, {conversation.ToolRead},
		{conversation.ToolBash}, {conversation.ToolWriteTodos}, {},
	}
	for index, reply := range assistant {
		var got []conversation.ToolName
		for _, call := range reply.Message.ToolCalls {
			got = append(got, call.Name)
		}
		if len(got) != len(wantTools[index]) {
			t.Fatalf("reply %d called %v, want %v", index, got, wantTools[index])
		}
		for position := range got {
			if got[position] != wantTools[index][position] {
				t.Fatalf("reply %d called %v, want %v", index, got, wantTools[index])
			}
		}
	}
	results := 0
	for _, message := range agent.History.Messages {
		if message.Message.Role != conversation.MessageRoleTool {
			continue
		}
		results++
		if execution := message.Message.ToolExecution; execution == nil || execution.Outcome != conversation.ToolOutcomeSucceeded {
			t.Fatalf("tool result %d (%s) did not succeed: %+v", message.Sequence, *message.Message.ToolName, execution)
		}
	}
	if results != 5 {
		t.Fatalf("expected 5 tool results, got %d", results)
	}
	final := assistant[len(assistant)-1].Message
	if len(final.ToolCalls) != 0 || !strings.Contains(final.Content, "Inspection finished") {
		t.Fatalf("the last reply is not the mock final text: %q", final.Content)
	}
	if todo := agent.Description.TodoList; todo == nil || len(todo.Items) != 1 || todo.Items[0].Status != conversation.TaskStatusCompleted {
		t.Fatalf("write_todos did not record the mock progress list: %+v", todo)
	}
	if agent.Description.PendingUserInput != nil || agent.Description.ConfirmedRequirements != nil {
		t.Fatalf("a small read-only turn showed a card or confirmed requirements: %+v", agent.Description)
	}
	if agent.Description.ContextModel != conversation.ModelMock {
		t.Fatalf("the Session runs on %q", agent.Description.ContextModel)
	}
}

// The compaction scenario reports usage above every threshold after one tool
// call, so the Session compacts the segment with the mock summary, opens a
// new segment from it and the script continues at its next reply there.
func TestSessionMockCompactionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "compaction", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	var marker, final *conversation.SequencedMessage
	for index, message := range agent.History.Messages {
		switch {
		case message.Message.Role == conversation.MessageRoleSystem && message.Message.Content == agentloop.SegmentMarker(agentloop.SegmentReasonCompaction):
			marker = &agent.History.Messages[index]
		case message.Message.Role == conversation.MessageRoleAssistant && len(message.Message.ToolCalls) == 0:
			final = &agent.History.Messages[index]
		}
	}
	if marker == nil {
		t.Fatal("the context was never compacted: no segment marker in the history")
	}
	if final == nil || final.Sequence < marker.Sequence || !strings.Contains(final.Message.Content, "compacted") {
		t.Fatalf("the script did not continue at step 1 after the compaction: %+v", final)
	}
	assistant := replies(agent.History)
	if len(assistant) != 2 {
		t.Fatalf("expected the tool reply and the final reply, got %d replies", len(assistant))
	}
	requireMock(t, assistant[1], mockInputTokens)
	if call := assistant[0].Message.ModelCall; call == nil || call.InputTokens == nil || *call.InputTokens != 850000 {
		t.Fatalf("the first reply did not report the mock compaction usage: %+v", call)
	}
}

// The compaction-failure scenario is a turn of 12 calls, which moves the user's
// request from the current window into the archive. Its first compaction, at the
// hard limit, succeeds, and the scenario requires the new segment to quote that
// request, which the compaction read from the archive chunk it loaded. The model
// API is overloaded for the second compaction, so Dex retries it until it gives
// up, and RecordCompactionFailure opens a segment without a summary because the
// context is at its hard limit. The failure route of the compaction loads the
// same archive chunks, so that segment quotes the request too, which the
// scenario's last reply requires, and the Session waits for the user with its
// engine execution running. Without the loads the Step panics on the unloaded
// chunk, and the execution fails.
func TestSessionMockCompactionFailureDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "compaction-failure", 1).Content)
	agent := session.finishedTurn(ctx)
	_, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	final := messages[len(messages)-1]

	// The turn moved its request out of the Snapshot's window into the archive.
	if request := messages[0]; request.Sequence != 1 || request.Message.Role != conversation.MessageRoleUser {
		t.Fatalf("the history does not start with the user's request: %+v", request)
	}
	if agent.History.NextBeforeSequence == nil {
		t.Fatal("the Snapshot's window still holds the user's request, so the turn never moved it into the archive")
	}
	// The first compaction's segment quotes it: the mock model answers with this
	// text only when the first message of its request holds the pending request.
	quoted := slices.IndexFunc(messages, func(message conversation.SequencedMessage) bool {
		return message.Message.Role == conversation.MessageRoleAssistant && strings.Contains(message.Message.Content, "quotes the original request")
	})
	if quoted < 0 {
		t.Fatalf("the segment of the first compaction does not quote the archived request, so the scenario stopped; the last message: %q", final.Message.Content)
	}
	// The failed compaction opened a segment without a summary, once, after it.
	var reasons []agentloop.SegmentReason
	var unsummarized conversation.Sequence
	for _, message := range ofRole(messages, conversation.MessageRoleSystem) {
		for _, reason := range []agentloop.SegmentReason{agentloop.SegmentReasonCompaction, agentloop.SegmentReasonUnsummarized} {
			if message.Message.Content == agentloop.SegmentMarker(reason) {
				reasons = append(reasons, reason)
				if reason == agentloop.SegmentReasonUnsummarized {
					unsummarized = message.Sequence
				}
			}
		}
	}
	if !slices.Equal(reasons, []agentloop.SegmentReason{agentloop.SegmentReasonCompaction, agentloop.SegmentReasonUnsummarized}) {
		t.Fatalf("expected one compaction and then one segment without a summary, got %v", reasons)
	}
	if unsummarized <= messages[quoted].Sequence || unsummarized <= conversation.Sequence(agentloop.CurrentMessageLimit) {
		t.Fatalf("the segment without a summary (message %d) did not open after the archive took the request", unsummarized)
	}
	// A model call ran in it. The mock model gives this reply only if the first
	// message of the request holds the pending request: its scenario marker
	// reaches the segment through that quote, and the step marker in the quoted
	// todo list continues the script.
	if final.Message.Role != conversation.MessageRoleAssistant || final.Sequence <= unsummarized || !strings.Contains(final.Message.Content, "segment without a summary quotes the original request") {
		t.Fatalf("the segment without a summary does not quote the archived request, or the scenario did not go on in it; the last message: %q", final.Message.Content)
	}
	requireMock(t, final, mockInputTokens)
}

// The model-failure scenario answers the first call with the 400 the API gives
// an invalid request. The turn records the failure as a marker the user sees,
// and the Session returns to waiting for a message instead of ending. The
// rejection is a platform failure, which CallModel reports beside the turn to
// the PlatformIssueFlow of its signature; the deployed server names no issue
// repository, so that Flow only logs it (platformIssueReports reads the
// engine history, requireLoggedIssues the Flow). The next scenario's input then
// runs although the request still holds the unanswered one.
func TestSessionMockModelFailureDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "model-failure", 1).Content)
	agent := session.finishedTurn(ctx)
	if replies := replies(agent.History); len(replies) != 0 {
		t.Fatalf("a rejected call produced %d replies", len(replies))
	}
	var failure string
	for _, message := range agent.History.Messages {
		if message.Message.Role == conversation.MessageRoleSystem && strings.HasPrefix(message.Message.Content, "Model call failed") {
			failure = message.Message.Content
		}
	}
	if !strings.Contains(failure, "invalid_request_error") || !strings.Contains(failure, "HTTP 400") {
		t.Fatalf("the turn did not record the mock rejection: %q", failure)
	}
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || snapshot.Session.Status != agentloop.SessionStatusActive {
		t.Fatalf("the Session did not stay active after the failed turn (%v)", err)
	}
	reports := session.platformIssueReports(ctx)
	if len(reports) != 1 {
		t.Fatalf("expected the report of the rejected model call, got %d: %+v", len(reports), reports)
	}
	requireBesideTurn(t, reports[0], "CallModel")
	session.requireLoggedIssues(ctx, reports)
	if incident := reports[0].incident; incident.CallID != "" || incident.Tool != "" || incident.Code != "invalid_request_error" || incident.ErrorType != "http_400" {
		t.Fatalf("the report does not name the rejected model call: %+v", incident)
	}

	// The next request repeats the unanswered input beside the new one; the new one runs.
	session.send(ctx, mockInput(t, "explain", 1).Content)
	agent = session.finishedTurnAfter(ctx, agent.Description.LastSequence)
	requireProgress(t, agent, conversation.MockProgress{Scenario: "explain", Input: 1})
}

// The tool-failure scenario runs a command that is only blanks. It never runs,
// and its failure returns to the model as an error result instead of waiting
// for the user: the mock model goes on only when that result is an error
// whose text starts with the code, so the next reply proves what the model
// read. The model runs a real command, which succeeds, and the turn ends with
// its final text.
func TestSessionMockToolFailureDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "tool-failure", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	assistant := replies(agent.History)
	if len(assistant) != 3 {
		t.Fatalf("expected the 3 mock replies, got %d; a stopped scenario answers with one text", len(assistant))
	}
	for _, reply := range assistant {
		requireMock(t, reply, mockInputTokens)
	}
	var results []conversation.ToolExecutionMetrics
	for _, message := range agent.History.Messages {
		if message.Message.Role != conversation.MessageRoleTool {
			continue
		}
		if *message.Message.ToolName != conversation.ToolBash || message.Message.ToolExecution == nil {
			t.Fatalf("tool result %d is not a measured bash result", message.Sequence)
		}
		results = append(results, *message.Message.ToolExecution)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(results))
	}
	if failed := results[0]; failed.Outcome != conversation.ToolOutcomeKnownFailure || failed.ResultCode != toolfailure.InvalidArguments.String() || failed.ExitCode != nil {
		t.Fatalf("the blank command did not fail with invalid_arguments before it ran: %+v", failed)
	}
	if corrected := results[1]; corrected.Outcome != conversation.ToolOutcomeSucceeded || corrected.ResultCode != "" || corrected.ExitCode == nil || *corrected.ExitCode != 0 {
		t.Fatalf("the corrected command did not succeed: %+v", corrected)
	}
	final := assistant[len(assistant)-1].Message
	if len(final.ToolCalls) != 0 || !strings.Contains(final.Content, "the empty command was corrected") {
		t.Fatalf("the last reply is not the mock final text: %q", final.Content)
	}
	if agent.Description.PendingUserInput != nil || agent.Description.PendingApproval != nil {
		t.Fatalf("a failed tool call left a wait for the user: %+v", agent.Description)
	}
}
