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
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// The connector subagents on the Session of a throwaway READY Project and the
// actual Worker of the default local lane, whose server runs the mock model and
// names no upstream connectors library. A connector_select subagent searches
// the published catalog, which the Sandbox fetches for real, and its report
// becomes the requirements card. A connector_develop subagent works in its
// worktree of the connectors library, a clone the Sandbox fetches from the
// lane's library (the local Git host's copy, refreshed from GitHub), and its
// finish commits the change and pushes the branch there; the contribution's
// Flow records it unpublished, because this server opens no pull request. Dex,
// the Sandbox, the local Git host and GitHub's public library are real; only
// the model is the mock. The pull request runs in the manual GitHub tests.

// automaticTurnGrace bounds how long a Session may wait for a message after
// its subagent ended without the automatic turn's reply.
const automaticTurnGrace = 2 * time.Minute

// connectorSubagent waits for the Session's one subagent of kind to report
// and for the automatic turn its notification opens to end with a reply, and
// returns the record, the history and the subagent's own state. A Session
// that keeps waiting for a message after the subagent ended, with no such
// reply, fails the test with what it holds.
func (s *mockSession) connectorSubagent(ctx context.Context, kind agenttasks.SubagentKind) (agenttasks.SubagentRecord, []conversation.SequencedMessage, subagent.SnapshotForTest) {
	s.t.Helper()
	ended := func(agent agentloop.AgentSnapshot) bool {
		records := agent.Description.Subagents
		return len(records) == 1 && records[0].Status.Finished()
	}
	s.automaticTurn(ctx, "the "+kind.String()+" subagent's report", ended, func(notification *conversation.SessionNotification) bool {
		return notification.Kind == conversation.NotificationKindTask && notification.Source == conversation.NotificationSourceSubagent
	})
	agent, messages := s.history(ctx)
	requireNoFailure(s.t, conversation.HistoryPage{Messages: messages})
	record := onlySubagent(s.t, agent)
	if record.Kind != kind || record.Status != agenttasks.SubagentCompleted {
		s.t.Fatalf("the %s subagent did not complete: %+v", kind, record)
	}
	subagentNotification(s.t, messages, record, agenttasks.SubagentCompleted)
	return record, messages, s.awaitSubagentEnd(ctx, record)
}

// automaticTurn waits until the Session, once ready accepts it, holds a
// notification that matches and has replied after it and waits for a message:
// the automatic turn the notification opened ended with a reply or at the
// pending input it asks for. A ready Session that keeps waiting for a message
// without such a reply fails the test with what it holds after
// automaticTurnGrace, instead of the whole turn timeout.
func (s *mockSession) automaticTurn(ctx context.Context, after string, ready func(agentloop.AgentSnapshot) bool, matches func(*conversation.SessionNotification) bool) {
	s.t.Helper()
	var idleSince time.Time
	s.waitForSnapshot(ctx, "the automatic turn after "+after, func(agent agentloop.AgentSnapshot) bool {
		if !ready(agent) || agent.Description.Status != agentloop.AgentStatusWaitingForMessage {
			idleSince = time.Time{}
			return false
		}
		notified, isNotified := notificationSequence(agent.History.Messages, matches)
		if isNotified && hasReplyAfter(agent.History.Messages, notified) {
			return true
		}
		if idleSince.IsZero() {
			idleSince = time.Now()
		} else if time.Since(idleSince) > automaticTurnGrace {
			s.t.Fatalf("Session %s has waited for a message for %s after %s without a reply after its notification (notified: %t at %d); %s",
				s.id, automaticTurnGrace, after, isNotified, notified, describeLastMessage(agent))
		}
		return false
	})
}

// notificationSequence is the sequence of the last notification in messages
// that matches.
func notificationSequence(messages []conversation.SequencedMessage, matches func(*conversation.SessionNotification) bool) (conversation.Sequence, bool) {
	var sequence conversation.Sequence
	found := false
	for _, message := range ofRole(messages, conversation.MessageRoleNotification) {
		if notification := message.Message.Notification; notification != nil && matches(notification) {
			sequence, found = message.Sequence, true
		}
	}
	return sequence, found
}

// hasReplyAfter reports whether an assistant reply follows sequence.
func hasReplyAfter(messages []conversation.SequencedMessage, sequence conversation.Sequence) bool {
	for _, message := range ofRole(messages, conversation.MessageRoleAssistant) {
		if message.Sequence > sequence {
			return true
		}
	}
	return false
}

// describeMessages lists messages for a failure: each one's role, tool,
// outcome and the start of its content.
func describeMessages(messages []conversation.SequencedMessage) string {
	var described strings.Builder
	for _, message := range messages {
		tool, outcome := "", ""
		if message.Message.ToolName != nil {
			tool = string(*message.Message.ToolName)
		}
		if execution := message.Message.ToolExecution; execution != nil {
			outcome = fmt.Sprintf("%s %s", execution.Outcome, execution.ResultCode)
		}
		content := message.Message.Content
		if len(content) > 600 {
			content = content[:600] + "..."
		}
		fmt.Fprintf(&described, "%d %s %s %s: %q\n", message.Sequence, message.Message.Role, tool, outcome, content)
	}
	return described.String()
}

// describeLastMessage names the Session's last message, for a failure.
func describeLastMessage(agent agentloop.AgentSnapshot) string {
	messages := agent.History.Messages
	if len(messages) == 0 {
		return "its history is empty"
	}
	last := messages[len(messages)-1]
	content := last.Message.Content
	if len(content) > 300 {
		content = content[:300] + "..."
	}
	return fmt.Sprintf("its %d messages end with %s message %d (%d tool calls): %q", len(messages), last.Message.Role, last.Sequence, len(last.Message.ToolCalls), content)
}

// A connector_select subagent finds the released llm connector, edits nothing,
// and its report becomes the existing connection of the requirements card,
// which the Session records as version 1.
func TestSessionMockConnectorSelectExistingDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "connector-select-existing", 1).Content)
	record, messages, state := session.connectorSubagent(ctx, agenttasks.SubagentConnectorSelect)
	card := session.recordedRequirements(ctx, subagentNotification(t, messages, record, agenttasks.SubagentCompleted).Sequence, 1)
	if record.Changes != nil || record.Connector != nil || state.Config == nil ||
		state.Config.Effort != conversation.EffortMedium || !strings.Contains(state.Outcome.Report, "recommended: llm") {
		t.Fatalf("the selection subagent is wrong: %+v, %+v, %+v", record, state.Config, state.Outcome)
	}
	connections := card.Description.ConfirmedRequirements.Spec.Connections
	if len(connections) != 1 || connections[0].Connector != "llm" || connections[0].Status != agentloop.RequirementConnectionExisting {
		t.Fatalf("the card does not list the selected connector as existing: %+v", connections)
	}
	t.Logf("FlowID=%s selected llm through %s", session.id, session.subagentOf(record).FlowID())
}

// A connector_select subagent reports two connectors; the main agent asks the
// user which one, and the answer puts gmail on the card, which the Session
// records as version 1.
func TestSessionMockConnectorSelectChoiceDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "connector-select-choice", 1).Content)
	// The question comes in the automatic turn the subagent's notification opens.
	record, messages, state := session.connectorSubagent(ctx, agenttasks.SubagentConnectorSelect)
	agent := session.pendingInput(ctx, "", subagentNotification(t, messages, record, agenttasks.SubagentCompleted).Sequence)
	if !strings.Contains(state.Outcome.Report, "alternatives: outlook-mail") {
		t.Fatalf("the selection report names no alternative: %+v (%+v)", state.Outcome, record)
	}
	pending := agent.Description.PendingUserInput
	answer := mockInput(t, "connector-select-choice", 2)
	request := agentloop.AnswerQuestionsRequest{CallID: pending.CallID, Answers: []agentloop.UserInputAnswer{{QuestionID: pending.Questions[0].ID, Answer: answer.Answers[0]}}}
	if err := session.client.AnswerQuestions(ctx, session.id, request); err != nil {
		t.Fatalf("answering the choice: %v", err)
	}
	card := session.recordedRequirements(ctx, agent.Description.LastSequence, 1)
	if connections := card.Description.ConfirmedRequirements.Spec.Connections; len(connections) != 1 || connections[0].Connector != "gmail" {
		t.Fatalf("the card does not list the chosen connector: %+v", connections)
	}
}

// requireContribution checks the contribution a completed develop subagent
// made on this lane, which names no upstream library: its notification and
// outcome carry it, the record of its ConnectorContributionFlow is unpublished
// with the same branch and head, and the Session's slugs name it.
func (s *mockSession) requireContribution(ctx context.Context, record agenttasks.SubagentRecord, messages []conversation.SequencedMessage, state subagent.SnapshotForTest,
	slug string, kind conversation.ContributionKind,
) contribution.Snapshot {
	s.t.Helper()
	made := state.Outcome.Contribution
	branch := "/" + s.identity.ProjectID + "/" + slug
	if record.Connector == nil || record.Connector.Slug != slug || record.Changes != nil || made == nil || made.Slug != slug || made.Kind != kind ||
		!strings.HasPrefix(made.Branch, "superverse/") || !strings.HasSuffix(made.Branch, branch) || !flowid.IsCommitSHA(made.HeadCommit) ||
		!strings.HasPrefix(made.Module, connectorlibrary.ModulePath+"/connectors/") || made.Location == connectorlibrary.LocationUpstream {
		s.t.Fatalf("the develop subagent's contribution is wrong: %+v, %+v", record, made)
	}
	if notification := subagentNotification(s.t, messages, record, agenttasks.SubagentCompleted).Message.Notification; notification.Contribution == nil || !reflect.DeepEqual(*notification.Contribution, *made) {
		s.t.Fatalf("the task notification does not carry the contribution: %+v", notification)
	}
	id := contribution.Identity{ProjectID: s.identity.ProjectID, Slug: slug}
	var recorded contribution.Snapshot
	s.await(ctx, "the contribution of "+slug, func() bool {
		var err error
		recorded, err = s.contributions.Snapshot(ctx, id)
		return err == nil && recorded.Record.Status == contribution.StatusUnpublished
	})
	if recorded.Record.Branch != made.Branch || recorded.Record.HeadCommit != made.HeadCommit || recorded.Record.Module != made.Module || recorded.Record.PullRequest != nil {
		s.t.Fatalf("the contribution's record differs from the subagent's contribution: %+v, %+v", recorded.Record, made)
	}
	snapshot, err := s.client.GetSnapshot(ctx, s.id)
	if err != nil || snapshot.Agent == nil || len(snapshot.Agent.Description.ConnectorSlugs) != 1 || snapshot.Agent.Description.ConnectorSlugs[0] != slug {
		s.t.Fatalf("the Session does not name the contributed connector (%v)", err)
	}
	s.t.Logf("FlowID=%s contributed %s on %s at %s, %s", s.id, slug, made.Branch, made.HeadCommit, made.Location)
	return recorded
}

// A new connector: after the card the develop subagent writes the
// mockfeed module and its catalog entry in its library worktree; its finish
// verifies, commits and pushes the branch, and the contribution is recorded.
func TestSessionMockConnectorDevelopNewDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, _ := startRecorded(t, ctx, mockInput(t, "connector-develop-new", 1))
	record, messages, state := session.connectorSubagent(ctx, agenttasks.SubagentConnectorDevelop)
	if state.Config.Effort != conversation.EffortHigh && state.Config.Effort != conversation.EffortMedium {
		t.Fatalf("the develop subagent runs at %s", state.Config.Effort)
	}
	recorded := session.requireContribution(ctx, record, messages, state, "mockfeed", conversation.ContributionNew)
	if recorded.Record.Module != connectorlibrary.ModulePath+"/connectors/superverse-mock/mockfeed" {
		t.Fatalf("the new connector is the module %s", recorded.Record.Module)
	}
}

// A release candidate: the develop subagent builds the complete connector
// superversemock, and its finish publishes the head's candidate on the local
// Git host's copy, with assets that the library's own tooling at main built,
// and names it in the task notification. The contribution records it, the
// main agent switches the application to that version and pushes it through
// the real commit gate. The automatic Preview deployment of the pushed commit
// reads the candidate's release from the local Git host, so it asks only for
// the connection, never for a release it cannot resolve. Once a person
// configured it, the Preview serves the commit, built in the Sandbox with the
// candidate's module from the local Git host's copy; a Release of the commit,
// which the local builder builds from that copy too, then serves in Live.
func TestSessionMockConnectorReleaseCandidateDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+3*previewCommandTimeout+liveSourceTimeout+liveBuildTimeout+liveDeploymentTimeout)
	defer cancel()
	session, _ := startRecorded(t, ctx, mockInput(t, "connector-release-candidate", 1))
	record, messages, state := session.connectorSubagent(ctx, agenttasks.SubagentConnectorDevelop)
	made := state.Outcome.Contribution
	if made == nil || !made.CandidatePublished || made.ReleaseCandidate == nil || made.CandidateRefused != "" {
		t.Fatalf("the develop subagent's finish published no release candidate: %+v", made)
	}
	candidate := *made.ReleaseCandidate
	if !regexp.MustCompile(`^v0\.1\.0-rc\.[1-9][0-9]*$`).MatchString(candidate.Version) || candidate.Tag != "connectors/superversemock/"+candidate.Version ||
		candidate.Commit != made.HeadCommit || candidate.Directory != "connectors/superversemock" {
		t.Fatalf("the release candidate is not the head's next candidate of v0.1.0: %+v of %s", candidate, made.HeadCommit)
	}
	recorded := session.requireContribution(ctx, record, messages, state, "superversemock", conversation.ContributionNew)
	recorded = session.awaitContribution(ctx, recorded.Identity, func(record contribution.Record) bool { return record.ReleaseCandidate != nil })
	if got := recorded.Record.ReleaseCandidate; got.Status != contribution.ReleaseCandidatePublished || got.ReleaseCandidate != candidate {
		t.Fatalf("the contribution records %+v, not the published candidate %+v", got, candidate)
	}
	// The automatic turn of the task notification switched to the candidate and pushed.
	_, messages = session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	if !slices.ContainsFunc(messages, func(message conversation.SequencedMessage) bool {
		return message.Message.ToolName != nil && *message.Message.ToolName == conversation.ToolCommitAndPush
	}) {
		t.Fatalf("the automatic turn did not push; it ended with:\n%s", describeMessages(messages[max(0, len(messages)-8):]))
	}
	if pushed := lastToolResult(t, conversation.HistoryPage{Messages: messages}, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the application on the release candidate was not pushed: %+v", pushed)
	}
	if slices.ContainsFunc(ofRole(messages, conversation.MessageRoleNotification), func(message conversation.SequencedMessage) bool {
		return message.Message.Notification != nil && message.Message.Notification.Kind == conversation.NotificationKindConnector
	}) {
		t.Fatal("the candidate that the task notification named was reported again")
	}
	head := session.summary(ctx).Identity.HeadCommit
	deployed := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == head })
	gaps := deployed.Preview.ConfigurationGaps
	if failureCode(deployed) != preview.FailureIncompatibleForTest || !slices.ContainsFunc(gaps, func(gap appmanifest.RequirementGap) bool { return gap.ConnectionName == "story-feed" }) {
		t.Fatalf("the Preview of %s on the release candidate %s ended %s with the gaps %+v", head, candidate.Version, failureCode(deployed), gaps)
	}

	// A person configures the Preview and starts it.
	session.preparePreview(ctx)
	session.configurePreview(ctx)
	if started := session.startPreview(ctx); started.Preview.SourceCommit != head {
		t.Fatalf("the Preview serves %s, not %s", started.Preview.SourceCommit, head)
	}
	session.stopPreview(ctx)

	// The Release of the commit builds on the local builder and serves in Live.
	live := newLiveDelivery(session)
	if release := live.prepare(ctx, head, 1); release.PreparedSource.Status != releases.Ready {
		t.Fatalf("the source of %s was not prepared: %+v", head, release.PreparedSource.Failure)
	}
	session.configureConnections(ctx, live.configuration, live.target(head))
	built, release := live.build(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: live.publishCopy(ctx, head), PreviewSkipReason: skipPreview})
	if built.Status != releases.Ready {
		// The Project's deletion removes the build's log, so the failure keeps its end.
		page, err := live.releases.ReadBuildLog(ctx, releases.BuildSnapshot{Release: release, Build: built}, build.LogRead{Tail: true, MaximumBytes: 8192})
		tail := comm.TailUTF8(strings.ReplaceAll(page.Text, openAIKey(t), "[REDACTED]"), 6000)
		t.Fatalf("the build of %s on the release candidate failed: %+v; the end of its log (%v):\n%s", head, built.Failure, err, tail)
	}
	if deployment := live.deploy(ctx, head, built); deployment.Status != deployments.Ready {
		t.Fatalf("the deployment of %s on the release candidate failed: %+v", built.BuildRevisionID, deployment.Failure)
	}
	t.Logf("FlowID=%s published %s at %s; the application pushed %s on it, which the Preview served and build %s (profile %s) serves in Live",
		session.id, candidate.Tag, candidate.Commit, head, built.BuildRevisionID, built.BuildProfileVersion)
}

// A new operation of the existing newsapi connector: the change stays in its
// module and leaves catalog.yaml as it is.
func TestSessionMockConnectorDevelopOperationDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, _ := startRecorded(t, ctx, mockInput(t, "connector-develop-operation", 1))
	record, messages, state := session.connectorSubagent(ctx, agenttasks.SubagentConnectorDevelop)
	session.requireContribution(ctx, record, messages, state, "newsapi", conversation.ContributionOperation)
}

// A defect fix: the user agrees to submit it with request_user_input before
// the develop subagent of kind fix starts, which changes only the newsapi
// module.
func TestSessionMockConnectorDevelopFixDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "connector-develop-fix", 1).Content)
	agent := session.pendingInput(ctx, "", 0)
	if records := agent.Description.Subagents; len(records) != 0 {
		t.Fatalf("a subagent started before the user agreed to the fix: %+v", records)
	}
	pending := agent.Description.PendingUserInput
	answer := mockInput(t, "connector-develop-fix", 2)
	request := agentloop.AnswerQuestionsRequest{CallID: pending.CallID, Answers: []agentloop.UserInputAnswer{{QuestionID: pending.Questions[0].ID, Answer: answer.Answers[0]}}}
	if err := session.client.AnswerQuestions(ctx, session.id, request); err != nil {
		t.Fatalf("agreeing to the fix: %v", err)
	}
	record, messages, state := session.connectorSubagent(ctx, agenttasks.SubagentConnectorDevelop)
	session.requireContribution(ctx, record, messages, state, "newsapi", conversation.ContributionFix)
}

// The ConnectorContributionFlow itself, on the Session of a throwaway Project:
// the FlowID derives from the Project and the slug; a repeated submission
// joins the execution; a later head of the connector updates the record of
// the running execution instead of starting another; this lane records it
// unpublished; and the Project's deletion closes it, after which a submission
// is refused as closed.
func TestConnectorContributionFlowDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newSession(t, ctx, conversation.ModelMock, "")
	id := contribution.Identity{ProjectID: session.identity.ProjectID, Slug: "mockidentity"}
	if id.FlowIDForTest() != "connector-contribution-"+session.identity.ProjectID+"-mockidentity" {
		t.Fatalf("the FlowID does not derive from the Project and the slug: %s", id.FlowIDForTest())
	}
	first := strings.Repeat("a", 40)
	submission := contribution.Submission{Identity: id, Report: "A contribution of the identity test.", Contribution: conversation.ConnectorContribution{
		Slug: id.Slug, Module: connectorlibrary.ModulePath + "/connectors/superverse-mock/mockidentity", Kind: conversation.ContributionNew,
		Branch: "superverse/test/" + id.ProjectID + "/" + id.Slug, HeadCommit: first, Location: connectorlibrary.LocationSandbox, Summary: "Build the mockidentity connector"}}
	for range 2 {
		if err := session.contributions.Submit(ctx, submission); err != nil {
			t.Fatalf("submitting the contribution: %v", err)
		}
	}
	before := session.execution(ctx, id.FlowIDForTest())
	recorded := session.awaitContribution(ctx, id, func(record contribution.Record) bool { return record.HeadCommit == first })
	if recorded.Record.Status != contribution.StatusUnpublished || recorded.Identity != id {
		t.Fatalf("the contribution of a branch that stays in the Sandbox is not unpublished: %+v", recorded)
	}
	later := submission
	later.Contribution.HeadCommit = strings.Repeat("b", 40)
	if err := session.contributions.Submit(ctx, later); err != nil {
		t.Fatalf("submitting a later head: %v", err)
	}
	session.awaitContribution(ctx, id, func(record contribution.Record) bool { return record.HeadCommit == later.Contribution.HeadCommit })
	if after := session.execution(ctx, id.FlowIDForTest()); after.GetFlowExecutionId().GetRunId() != before.GetFlowExecutionId().GetRunId() {
		t.Fatalf("a later head started another execution: %v, then %v", before.GetFlowExecutionId(), after.GetFlowExecutionId())
	}
	projecttest.DeleteForTest(ctx, t, session.projects, id.ProjectID)
	closed, err := session.contributions.Snapshot(ctx, id)
	if err != nil || closed.Record.Status != contribution.StatusClosed {
		t.Fatalf("the Project's deletion did not close the contribution: %+v (%v)", closed, err)
	}
	newer := submission
	newer.Contribution.HeadCommit = strings.Repeat("c", 40)
	for name, after := range map[string]contribution.Submission{"the first submission repeated": submission, "a later head": newer} {
		if err = session.contributions.Submit(ctx, after); !errors.Is(err, contribution.ErrContributionClosed) {
			var started *dex.FlowAlreadyStartedError
			t.Fatalf("%s after the close was not refused as closed: %v (already started: %t)", name, err, errors.As(err, &started))
		}
	}
	if after := session.execution(ctx, id.FlowIDForTest()); after.GetFlowExecutionId().GetRunId() != before.GetFlowExecutionId().GetRunId() {
		t.Fatalf("a submission after the close started another execution: %v, then %v", before.GetFlowExecutionId(), after.GetFlowExecutionId())
	}
	if final, err := session.contributions.Snapshot(ctx, id); err != nil || final.Record.Status != contribution.StatusClosed || final.Record.HeadCommit != later.Contribution.HeadCommit {
		t.Fatalf("a refused submission changed the closed contribution: %+v (%v)", final.Record, err)
	}
}

// awaitContribution waits until the contribution's record satisfies done and
// returns its snapshot.
func (s *mockSession) awaitContribution(ctx context.Context, id contribution.Identity, done func(contribution.Record) bool) contribution.Snapshot {
	s.t.Helper()
	var snapshot contribution.Snapshot
	s.await(ctx, "the contribution "+id.Slug, func() bool {
		var err error
		snapshot, err = s.contributions.Snapshot(ctx, id)
		return err == nil && done(snapshot.Record)
	})
	return snapshot
}
