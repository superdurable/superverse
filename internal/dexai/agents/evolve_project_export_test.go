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

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// EvolveProjectForTest is a LiveProjectForTest whose Session can push the
// goals of the mock scenario evolve-goals, with a client of the Project's
// EvolveFlow, which the lane's server Worker runs and whose reports store
// proposals in the lane's storage, and a reader of its engine execution.
type EvolveProjectForTest struct {
	LiveProjectForTest
	Evolve    *evolve.EvolveFlowClient
	Execution dexpb.FlowServiceClient
	session   *mockSession
}

// NewEvolveProjectForTest deploys a throwaway Project's HEAD to Live, as
// NewLiveProjectForTest does, keeping its Session. The Project starts from
// the default template, or from the template's example when example names
// one, whose Preview is configured as well, as newConfiguredPreviewSession
// does: a read-only first turn brings up the workspace's READY instance, on
// which the Preview's source is prepared, and the configuration is checked
// against that source. Its llm connections use the local lanes' synthetic
// summary model, so its Live and Preview runs reach the model through the
// lanes' deterministic summary endpoint.
func NewEvolveProjectForTest(t *testing.T, ctx context.Context, example string) EvolveProjectForTest {
	t.Helper()
	var session *mockSession
	if example == "" {
		session = newMockSession(t, ctx, "")
	} else {
		owner := projecttest.RegisterAccountForTest(ctx, t)
		teamSpaceID := projecttest.TeamSpaceForTest(ctx, t, owner)
		revision := comm.Must(apptemplate.ExampleTemplate(example)).Revision
		session = newSessionOf(t, ctx, conversation.ModelMock, mockInput(t, "inspect-basic", 1).Content, func(client *projects.ProjectFlowClient) projects.Project {
			return projecttest.CreateAtRevisionForTest(ctx, t, client, t.Name(), owner, teamSpaceID, revision)
		})
		requireNoFailure(t, session.finishedTurn(ctx).History)
		session.preparePreview(ctx)
		session.configurePreview(ctx)
	}
	live := newLiveDelivery(session)
	head := session.identity.HeadCommit
	project := liveProjectOf(t, ctx, session, live, head)
	// This client registry supplies the definition only; the server Worker runs the Flow. The client stores proposals.
	measurement := evolve.NewEvolveFlowBootstrap(evolve.Settings{}, live.projects, nil, project.Objects, nil)
	measurement.Client().Bind(dexflowtest.ServerWorkerClient(t, measurement.Flow()))
	return EvolveProjectForTest{LiveProjectForTest: project, Evolve: measurement.Client(), Execution: session.reader, session: session}
}

// PushGoalsForTest sends content, an input of the evolve-goals scenario that
// writes .superverse/goals.md and pushes it through the commit gate, and
// returns the pushed commit, the Project's new HEAD.
func (p EvolveProjectForTest) PushGoalsForTest(ctx context.Context, content string) string {
	s := p.session
	s.t.Helper()
	before := s.description(ctx).LastSequence
	s.send(ctx, content)
	agent := s.finishedTurnAfter(ctx, before)
	requireNoFailure(s.t, agent.History)
	if pushed := lastToolResult(s.t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest {
		s.t.Fatalf("the goals were not pushed: %+v", pushed)
	}
	return s.summary(ctx).Identity.HeadCommit
}

// AwaitReviewTurnForTest waits until the Session recorded the evolve
// notification of review reviewID and the automatic turn it opened ended, so
// that the next input starts a turn of its own.
func (p EvolveProjectForTest) AwaitReviewTurnForTest(ctx context.Context, reviewID string) {
	s := p.session
	s.t.Helper()
	s.waitForSnapshot(ctx, "the agent's turn of review "+reviewID, func(agent agentloop.AgentSnapshot) bool {
		messages := agent.History.Messages
		for i, message := range messages {
			if n := message.Message.Notification; n != nil && n.IsEvolve() && n.Evolve.ReviewID == reviewID {
				return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && i < len(messages)-1 &&
					messages[len(messages)-1].Message.Role == conversation.MessageRoleAssistant
			}
		}
		return false
	})
}

// AwaitReplyForTest waits until the Agent waits for the user and its last
// message is a reply that holds text: the turn that wrote it ended, with
// every write of its tool calls committed.
func (p EvolveProjectForTest) AwaitReplyForTest(ctx context.Context, text string) {
	s := p.session
	s.t.Helper()
	s.waitForSnapshot(ctx, "the reply "+text, func(agent agentloop.AgentSnapshot) bool {
		messages := agent.History.Messages
		if agent.Description.Status != agentloop.AgentStatusWaitingForMessage || len(messages) == 0 {
			return false
		}
		last := messages[len(messages)-1].Message
		return last.Role == conversation.MessageRoleAssistant && strings.Contains(last.Content, text)
	})
}

// ActivateLocalAgentForTest pairs a local agent with the Project's Session
// through the real remote agent endpoint and activates it, as the person's
// bridge does, and returns once the Session runs in remote mode.
func (p EvolveProjectForTest) ActivateLocalAgentForTest(ctx context.Context) {
	s := p.session
	s.t.Helper()
	connection, caller := projecttest.LoginForTest(ctx, s.t, p.Account)
	remote := pairRemoteHTTP(ctx, s.t, pb.NewRemoteAgentServiceClient(connection), caller, p.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "evolve-" + p.ProjectID,
		HostLabel: "integration host"}}, &active, http.StatusOK)
	if executor, err := s.client.ReadExecutor(ctx, p.ProjectID); err != nil || executor.Mode != remoteexec.ModeRemote || executor.Epoch != active.Epoch {
		s.t.Fatalf("the activation did not switch the Session of %s to the local agent (%T)", p.ProjectID, err)
	}
}

// SendForTest sends content, an input of a mock scenario, and waits until the
// turn it starts ends without a failure.
func (p EvolveProjectForTest) SendForTest(ctx context.Context, content string) {
	s := p.session
	s.t.Helper()
	before := s.description(ctx).LastSequence
	s.send(ctx, content)
	requireNoFailure(s.t, s.finishedTurnAfter(ctx, before).History)
}

// HistoryForTest is the Session's retained conversation, archive included.
func (p EvolveProjectForTest) HistoryForTest(ctx context.Context) []conversation.SequencedMessage {
	p.session.t.Helper()
	_, messages := p.session.history(ctx)
	return messages
}

// ToolResultForTest is the content of the result of the tool message at
// sequence, as the Session stored it, current or archived: the history's
// public messages carry a result's outcome and failure code, never its
// content (storedToolResult).
func (p EvolveProjectForTest) ToolResultForTest(ctx context.Context, sequence conversation.Sequence) string {
	p.session.t.Helper()
	return p.session.storedToolResult(ctx, sequence)
}

// SubagentsForTest are the records of the Session's subagents, in start order.
func (p EvolveProjectForTest) SubagentsForTest(ctx context.Context) []agenttasks.SubagentRecord {
	p.session.t.Helper()
	return p.session.description(ctx).Subagents
}

// AwaitPreviewForTest waits until the Project's Preview ended the deployment
// of commit and returns whether it serves that commit.
func (p EvolveProjectForTest) AwaitPreviewForTest(ctx context.Context, commit string) bool {
	p.session.t.Helper()
	deployed := p.session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == commit })
	return deployed.Deployment.Last.Status == preview.DeploymentReady && deployed.Preview.Status == preview.PreviewReady && deployed.Preview.SourceCommit == commit
}
