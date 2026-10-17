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
	"strings"
	"testing"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// newMatchedSession creates, through the lane's merged business API, a
// Project on the mock model whose first message is first, input 1 of a
// scenario that starts from the example the server matches to that message,
// and returns its Session once the Project is READY. The lane's server must
// have agent.closestExamplePick on, as native.yaml and local-kind.yaml set it;
// a Project that starts from another revision fails the test.
func newMatchedSession(t *testing.T, ctx context.Context, first mockmodel.Input) *mockSession {
	t.Helper()
	owner := projecttest.RegisterAccountForTest(ctx, t)
	teamSpaceID := projecttest.TeamSpaceForTest(ctx, t, owner)
	connection, caller := projecttest.LoginForTest(ctx, t, owner)
	api := pb.NewProjectServiceClient(connection)
	example := comm.Must(apptemplate.ExampleTemplate(exampleID))
	return newSessionOf(t, ctx, conversation.ModelMock, "", func(client *projects.ProjectFlowClient) projects.Project {
		created, err := api.CreateProject(ctx, &pb.CreateProjectRequest{Caller: caller, RequestId: flowid.RandomID(24), Name: t.Name(), TeamSpaceId: teamSpaceID,
			ManagedRepository: &pb.ManagedRepositorySource{}, InitialMessage: first.Content, MessageId: "first-" + flowid.RandomID(12),
			Model: pb.AgentModel_AGENT_MODEL_MOCK})
		if err != nil {
			t.Fatalf("CreateProject with a matched scenario's first input: %v", err)
		}
		project := projecttest.AwaitReadyForTest(ctx, t, client, created.GetProjectId())
		if project.TemplateRevision != example.Revision {
			t.Fatalf("the Project starts from %q, not %s: the lane's server must have agent.closestExamplePick on", project.TemplateRevision, example.Revision)
		}
		return project
	})
}

// finalReply is the content of the last assistant message: a Preview or
// configuration notice may land after the turn's last reply.
func finalReply(messages []conversation.SequencedMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Message.Role == conversation.MessageRoleAssistant {
			return messages[i].Message.Content
		}
	}
	return ""
}

// requireMatchedStart fails unless the Session started quietly from the
// matched example: it names the example as matched, its conversation opens
// with the creator's first message and no greeting, and the version 1 it
// holds is the agent's own card, recorded in its first turn.
func requireMatchedStart(t *testing.T, agent agentloop.AgentSnapshot, messages []conversation.SequencedMessage) {
	t.Helper()
	started := agent.Description.StartingExample
	if started == nil || started.ExampleID != exampleID || started.Choice != agentloop.ExampleChoiceMatched || len(started.MatchedOn) == 0 {
		t.Fatalf("the Session does not start from %s as matched: %+v", exampleID, started)
	}
	if len(messages) == 0 || messages[0].Message.Role != conversation.MessageRoleUser {
		t.Fatalf("the matched Session's conversation does not open with the first message: %+v", messages)
	}
	for _, message := range messages {
		if message.Message.Role == conversation.MessageRoleSystem && strings.Contains(message.Message.Content, "starts from the") {
			t.Fatalf("the matched Session greets with the example: %q", message.Message.Content)
		}
		if message.Message.Role == conversation.MessageRoleAssistant && strings.Contains(strings.ToLower(message.Message.Content), "example") {
			t.Fatalf("a reply announces the example: %q", message.Message.Content)
		}
	}
	confirmed := agent.Description.ConfirmedRequirements
	if confirmed == nil || confirmed.Revision != 1 || confirmed.Spec.Design == nil {
		t.Fatalf("the agent recorded no version 1 with a design of its own: %+v", confirmed)
	}
}

// exampleSteps are the Steps of the slack-thread-approval example's design:
// RecordThread, PostCompletionReply, CompleteThread and RecordReplyFailure.
const exampleSteps = 4

// requireFirstBuild fails unless the matched Session recorded its first
// build, the push that built version 1's design, with what it kept of the
// example: the example's Steps, version 1's, how many of the first the second
// keeps, and the pushed commit.
func requireFirstBuild(t *testing.T, agent agentloop.AgentSnapshot, builtSteps, keptSteps int) {
	t.Helper()
	want := agentloop.StartingExampleBuild{ExampleSteps: exampleSteps, BuiltSteps: builtSteps, KeptSteps: keptSteps, Commit: agent.Description.ConfirmedDesignBuiltCommit}
	if build := agent.Description.StartingExample.FirstBuild; build == nil || *build != want || !flowid.IsCommitSHA(want.Commit) {
		t.Fatalf("the matched Session recorded its first build as %+v, not %+v", build, want)
	}
}

// TestSessionMatchedExampleDexIntegration creates a Project through the lane's
// merged business API from the first input of the example-matched scenario,
// which the lane's server starts from the slack-thread-approval example, with
// Dex, the Worker, the Git host, PostgreSQL and MinIO all real:
//   - the Session starts quietly: it names the example as matched, records no
//     card and no greeting, and its first turn works on the creator's message;
//   - the session-start segment carries the starting_example section with the
//     example's card and trace, which the scenario's first reply requires, and
//     so does the segment the forced compaction opens next, which its second
//     reply requires;
//   - the agent records its own version 1, R1 to R3 with R3 posting Approved.,
//     changes the reply text and pushes, which builds the design; no reply
//     names the example;
//   - that push, the first build, records what it kept of the example: all
//     four of its Steps, in a design of four, and the pushed commit;
//   - input 2 compacts again, and the new segment no longer carries the
//     section, which its last reply requires; the first build stays as
//     recorded.
func TestSessionMatchedExampleDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+2*mockTurnTimeout)
	defer cancel()
	session := newMatchedSession(t, ctx, mockInput(t, "example-matched", 1))
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	_, messages := session.history(ctx)
	requireMatchedStart(t, agent, messages)
	if final := finalReply(messages); final != "A matching reply in the thread now gets Approved. back; the change is pushed and the Preview deploys it." {
		t.Fatalf("the first turn did not reach the scenario's last reply, which follows both section checks: %q", final)
	}
	if features := agent.Description.ConfirmedRequirements.Spec.Features; len(features) != 3 || !strings.Contains(features[2].Text, "Approved.") {
		t.Fatalf("version 1 is not the agent's card with R3 posting Approved.: %+v", features)
	}
	if count := markers(messages, agentloop.SegmentMarker(agentloop.SegmentReasonCompaction)); count != 1 {
		t.Fatalf("the first turn compacted %d times, want once between the card and the build", count)
	}
	if got := toolResult(t, messages, conversation.ToolCommitAndPush, 0); got.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the change's commit_and_push ended as %q (%s): %s", got.ResultCode, got.Outcome, session.toolResultContent(ctx, messages, conversation.ToolCommitAndPush, 0))
	}
	if !agent.Description.ConfirmedDesignBuilt {
		t.Fatalf("the push did not build version 1's design: %+v", agent.Description)
	}
	requireFirstBuild(t, agent, 4, 4)

	// After the build, a new segment no longer carries the section.
	last := agent.Description.LastSequence
	session.send(ctx, mockInput(t, "example-matched", 2).Content)
	after := session.finishedTurnAfter(ctx, last)
	requireNoFailure(t, after.History)
	_, messages = session.history(ctx)
	if final := finalReply(messages); final != "The context holds version 1 and nothing more to adapt." {
		t.Fatalf("the segment after the build still carries the starting example, or the scenario stopped: %q", final)
	}
	if after.Description.StartingExample == nil || after.Description.StartingExample.Choice != agentloop.ExampleChoiceMatched {
		t.Fatalf("the Session forgot how it started: %+v", after.Description.StartingExample)
	}
	if build := after.Description.StartingExample.FirstBuild; build == nil || *build != *agent.Description.StartingExample.FirstBuild {
		t.Fatalf("the first build changed after it was recorded: %+v", build)
	}
}

// TestSessionMatchedExampleRemovesDexIntegration creates a Project from the
// first input of the example-matched-wrong scenario, which the lane's server
// also starts from the slack-thread-approval example although the request
// wants nothing posted back: the Session starts quietly as matched, the agent
// writes version 1 with R1 and R2 and no posting feature, removes the reply
// Step, its operation and its test, adds a test of the recorded reply, and
// the push passes every stage of the commit gate. That first build records
// that version 1's design of two Steps keeps two of the example's four.
func TestSessionMatchedExampleRemovesDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+mockTurnTimeout)
	defer cancel()
	session := newMatchedSession(t, ctx, mockInput(t, "example-matched-wrong", 1))
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	_, messages := session.history(ctx)
	requireMatchedStart(t, agent, messages)
	if features := agent.Description.ConfirmedRequirements.Spec.Features; len(features) != 2 || features[1].ID != "R2" {
		t.Fatalf("version 1 does not keep only R1 and R2: %+v", features)
	}
	if got := toolResult(t, messages, conversation.ToolCommitAndPush, 0); got.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the change's commit_and_push ended as %q (%s): %s", got.ResultCode, got.Outcome, session.toolResultContent(ctx, messages, conversation.ToolCommitAndPush, 0))
	}
	requireFirstBuild(t, agent, 2, 2)
}
