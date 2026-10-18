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
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
)

// These scenarios run the automatic Preview deployment on the Session of a
// throwaway READY Project and the actual Worker of the mock Session tests, on
// either local lane: a turn that pushes through the real commit gate hands its
// commit to the Project's Preview, which deploys exactly that commit in the
// Session's Sandbox and reports the outcome to the Session. Dex, the Sandbox,
// the Git host and the Preview engine are real; only the model is the mock.
// The mock scenarios push to the Project's default branch: preview-autodeploy
// changes one note file, and preview-failure-fix and preview-broken-checkpoint
// end with the source they began with. Every scenario stops its Preview at
// the end as Studio does, and the Project's deletion ends the PreviewFlow. A
// deployment runs beside the continuation after a successful push, so the deployed
// server's sandbox.hold must outlast a Preview build (600 seconds suffice for
// the template).
//
// A Project's Preview configuration is its own, and a deployment validates
// it, so a test that needs a READY Preview first configures the Preview of
// its fresh Project as a person does in Studio before relying on automatic
// deployment (newConfiguredPreviewSession): a read-only first turn brings up
// the Session's READY instance, the first Preview command prepares the source
// of the Session's HEAD there, and configurePreview configures the template's
// connection with the Preview configuration inputs. Only then does the test
// send the scenario's pushing input.

// previewDeployTimeout bounds a turn that pushes through the commit gate and
// the deployment of the pushed commit.
const previewDeployTimeout = mockTurnTimeout + previewCommandTimeout

// previewSetupTimeout bounds newConfiguredPreviewSession: the read-only
// first turn and the first source preparation.
const previewSetupTimeout = mockTurnTimeout + previewCommandTimeout

// newConfiguredPreviewSession starts a mock Session whose first turn
// inspects the workspace read-only, which brings up its READY instance, then
// prepares the Preview's source there with the Session's first Preview
// command and configures the Preview (configurePreview). It returns the
// Session and its Agent after the first turn.
func newConfiguredPreviewSession(t *testing.T, ctx context.Context) (*mockSession, agentloop.AgentSnapshot) {
	t.Helper()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	session.preparePreview(ctx)
	session.configurePreview(ctx)
	return session, agent
}

// preparePreview prepares the source of the Session's HEAD on its READY
// instance with a prepare-preview-source command, as Studio's configuration
// view does, and returns the Preview after it. A Session's first Preview
// command, in the first-demand form, starts its Preview; a later one names the
// Preview it observed.
func (s *mockSession) preparePreview(ctx context.Context) preview.Snapshot {
	s.t.Helper()
	ready := s.workspace(ctx)
	prepare := preview.Command{ID: "prepare-" + time.Now().UTC().Format("20060102150405.000000"), Kind: preview.CommandPreparePreviewSource}
	current, err := s.previews.Get(ctx, s.identity.ProjectID)
	_, missing := errors.AsType[*dex.FlowNotFoundError](err)
	switch {
	case err == nil:
		prepare.WorkspaceRequestID, prepare.Generation, prepare.ExpectedRevision = current.WorkspaceRequestID, current.Generation, current.Revision
	case !missing:
		s.t.Fatalf("the Preview read failed (%T)", err)
	}
	if err = s.service().PreparePreviewSource(ctx, s.account, s.identity.ProjectID, prepare); err != nil {
		failure, uncertain := errors.AsType[*dex.ServiceError](err)
		if !uncertain {
			s.t.Fatalf("the source preparation was not admitted (%T)", err)
		}
		s.t.Logf("source admission response failed: operation=%s code=%v substatus=%v command=%s", failure.Op, failure.Code, failure.SubStatus, prepare.ID)
		// A lost response must not make cleanup race the admitted build. Read
		// the exact command identity before deciding that admission failed.
		observed, readErr := s.previews.Get(ctx, s.identity.ProjectID)
		active, last := observed.ActiveCommand, observed.LastCommand
		if readErr != nil || observed.PreparedSource.RequestID != prepare.ID ||
			!(active.ID == prepare.ID && active.Kind == prepare.Kind || last.ID == prepare.ID && last.Kind == prepare.Kind) {
			s.t.Fatalf("the uncertain source admission was not confirmed (%T, read %T)", err, readErr)
		}
		s.t.Logf("the exact source command %s is durably admitted; no command retry was sent", prepare.ID)
	}
	prepared := s.awaitPreview(ctx, prepare.ID)
	if prepared.PreparedSource.Status != preview.PreviewSourceReadyForTest || prepared.WorkspaceRequestID != ready.InstanceID {
		s.t.Fatalf("the source of instance %s was not prepared: %s (%s)", ready.InstanceID, prepared.PreparedSource.Status, failureCode(prepared))
	}
	return prepared
}

// since keeps the messages after sequence.
func since(messages []conversation.SequencedMessage, sequence conversation.Sequence) []conversation.SequencedMessage {
	return slices.DeleteFunc(slices.Clone(messages), func(message conversation.SequencedMessage) bool { return message.Sequence <= sequence })
}

// sandboxPreviewHold is the deployed server's sandbox.previewHold, which
// SUPERVERSE_TEST_SANDBOX_PREVIEW_HOLD_SECONDS repeats; a test that waits it
// out admits at most maximum.
func sandboxPreviewHold(t *testing.T, maximum time.Duration) time.Duration {
	t.Helper()
	seconds, err := strconv.Atoi(dexflowtest.Setting(t, "SUPERVERSE_TEST_SANDBOX_PREVIEW_HOLD_SECONDS"))
	if hold := time.Duration(seconds) * time.Second; err == nil && hold >= time.Minute && hold <= maximum {
		return hold
	}
	t.Fatalf("SUPERVERSE_TEST_SANDBOX_PREVIEW_HOLD_SECONDS must repeat, in seconds, the deployed server's sandbox.previewHold, 60 to %d", int(maximum.Seconds()))
	return 0
}

// TestSessionMockPreviewAutoDeployDexIntegration configures the fresh
// Project's Preview (newConfiguredPreviewSession), then pushes a note. When
// the push succeeds, the Session hands the pushed commit to its Preview, which
// deploys exactly that commit, prepared from its own exported tree, and
// serves it. The ready outcome holds the instance for the Preview hold and
// enters no conversation.
func TestSessionMockPreviewAutoDeployDexIntegration(t *testing.T) {
	previewHold := sandboxPreviewHold(t, 7*24*time.Hour)
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+previewDeployTimeout)
	defer cancel()
	session, first := newConfiguredPreviewSession(t, ctx)
	note := "deploy-" + time.Now().UTC().Format("20060102150405")
	session.send(ctx, mockInput(t, "preview-autodeploy", 1).Content+" "+mockmodel.ArgumentMarkerForTest("note", note))
	agent := session.finishedTurnAfter(ctx, first.Description.LastSequence)
	requireNoFailure(t, agent.History)
	if pushed := lastToolResult(t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the note was not pushed: %+v", pushed)
	}
	head := session.summary(ctx).Identity.HeadCommit
	if head == session.identity.HeadCommit {
		t.Fatalf("the push left the Session's HEAD at %s", head)
	}
	deployed := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == head })
	last := deployed.Deployment.Last
	if last.Status != preview.DeploymentReady || !strings.HasPrefix(last.CommandID, string(preview.CommandDeployPreview)+"-") || deployed.Preview.Status != preview.PreviewReady ||
		deployed.Preview.SourceCommit != head || deployed.PreparedSource.SourceCommit != head || deployed.PreparedSource.RequestID != last.CommandID {
		t.Fatalf("the Preview does not serve the pushed commit %s: %s %s (%s)", head, last.Status, deployed.Preview.Status, failureCode(deployed))
	}
	requireImmediatePreviewHandoff(t, ctx, session)
	// The ready notification holds the instance the Preview serves on for the
	// Preview hold, and the Session records nothing of it.
	held := session.awaitWorkspace(ctx, "the Preview hold", func(ws workspaceView) bool {
		return !ws.SandboxDeadline.Before(last.UpdatedAt.Add(previewHold - time.Minute))
	})
	if held.InstanceID != deployed.WorkspaceRequestID || held.Instance != sessionworkspace.InstanceReady {
		t.Fatalf("the Preview served on %s, but the Session holds %s %s", deployed.WorkspaceRequestID, held.InstanceID, held.Instance)
	}
	if _, messages := session.history(ctx); len(ofRole(messages, conversation.MessageRoleNotification)) != 0 {
		t.Fatal("the ready Preview entered the conversation")
	}
	session.stopPreview(ctx)
	t.Logf("FlowID=%s pushed %s; %s deployed it on %s, and the Session holds the instance until %s", session.id, head, last.CommandID, held.InstanceID, held.SandboxDeadline)
}

// TestSessionMockPreviewFailureFixDexIntegration configures the fresh
// Project's Preview (newConfiguredPreviewSession), then pushes a server that
// refuses to start without a setting the Preview does not have. The
// deployment of that commit fails PREVIEW_FAILED, owned by the application,
// for the Session's HEAD after it started the Preview, which the Preview
// therefore releases at once, keeping the engine; its delivery notification
// enters the conversation and opens an automatic turn, the mock reply that
// declares it, which removes the requirement and pushes the fix. The Preview
// then deploys the fix and serves it. The assertions read the conversation
// after the first turn and the Preview's history.
func TestSessionMockPreviewFailureFixDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+previewDeployTimeout+mockTurnTimeout+previewCommandTimeout)
	defer cancel()
	session, first := newConfiguredPreviewSession(t, ctx)
	session.send(ctx, mockInput(t, "preview-failure-fix", 1).Content)
	var messages []conversation.SequencedMessage
	session.await(ctx, "the automatic turn after the failed deployment", func() bool {
		agent, all := session.history(ctx)
		messages = since(all, first.Description.LastSequence)
		return len(ofRole(messages, conversation.MessageRoleAssistant)) >= 8 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 8 || !strings.Contains(assistant[7].Message.Content, "starts without WELCOME_TEXT") {
		t.Fatalf("expected the 8 mock replies ending with the fix; a stopped scenario answers with one text: %+v", assistant)
	}
	for index := range 2 {
		if pushed := toolResult(t, messages, conversation.ToolCommitAndPush, index); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest {
			t.Fatalf("push %d did not succeed: %+v", index+1, pushed)
		}
	}
	notifications := ofRole(messages, conversation.MessageRoleNotification)
	if len(notifications) != 1 {
		t.Fatalf("expected the one failure notification, got %d", len(notifications))
	}
	failed := notifications[0].Message.Notification
	if failed == nil || failed.Kind != conversation.NotificationKindDelivery || failed.Source != conversation.NotificationSourcePreview ||
		failed.Outcome != conversation.DeliveryFailure || failed.Owner != conversation.DeliveryOwnerApplication || failed.Code != "PREVIEW_FAILED" ||
		!strings.HasPrefix(failed.ID, "preview/"+failed.ResourceID+"/"+string(preview.CommandDeployPreview)+"-") || !strings.HasSuffix(failed.ID, "/failure") {
		t.Fatalf("the notification does not report the application's failed deployment: %+v", notifications[0].Message)
	}
	if notifications[0].Sequence < assistant[4].Sequence || notifications[0].Sequence > assistant[5].Sequence {
		t.Fatal("the notification is not between the first turn's end and the automatic turn")
	}
	head := session.summary(ctx).Identity.HeadCommit
	if failed.Commit == head || failed.Commit == session.identity.HeadCommit {
		t.Fatalf("the failure names %s, not the first push before the fix %s", failed.Commit, head)
	}
	fixed := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == head })
	if fixed.Deployment.Last.Status != preview.DeploymentReady || fixed.Preview.Status != preview.PreviewReady || fixed.Preview.SourceCommit != head {
		t.Fatalf("the Preview does not serve the fix %s: %s %s (%s)", head, fixed.Deployment.Last.Status, fixed.Preview.Status, failureCode(fixed))
	}
	// Nobody stops a deployment that failed after it started the Preview, so
	// the Preview released what it started (CommandRelease) before the fix
	// deployed: that release is the only stop in its history, and it kept the
	// engine.
	steps := session.completedSteps(ctx, preview.FlowID(session.identity.ProjectID))
	if stops := count(steps, dex.GetFinalStepType(preview.StopPreview{})); stops != 1 || slices.Contains(steps, dex.GetFinalStepType(preview.StopPreviewEnvironment{})) {
		t.Fatalf("the failed deployment was not released once with its engine kept: %d stops in %v", stops, steps)
	}
	session.stopPreview(ctx)
	t.Logf("FlowID=%s: %s failed PREVIEW_FAILED as %s and was released; the automatic turn pushed %s, which the Preview serves", session.id, failed.Commit, failed.ID, head)
}

// TestPreviewDeploymentAnswersDexIntegration asks the Project's Preview on the
// Session's READY instance to deploy the Session's HEAD with handoff sequences, as the
// Session's DeployPreviewHandoff does. The first request is accepted and runs;
// the same sequence again, the retry of a lost answer, is a duplicate. Once
// that deployment prepared its source, while its provider Steps run without a
// lock and save what they read when they began, a newer request is queued,
// and a newer one still supersedes it, so the newest runs next; a request
// older than the queued one is a duplicate, and the queued one again stays
// queued. No Step of the running deployment drops the queued request, which
// is admitted and deployed once the running one ends; the superseded one
// never runs: the deployments prepare exactly two sources. The Project's
// Preview is configured first (newConfiguredPreviewSession), whose source
// preparation the count leaves out.
func TestPreviewDeploymentAnswersDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+2*previewCommandTimeout)
	defer cancel()
	session, _ := newConfiguredPreviewSession(t, ctx)
	summary := session.summary(ctx)
	instance, head := session.workspace(ctx).InstanceID, summary.Identity.HeadCommit
	project := session.identity.ProjectID
	mark := markHistory(t, ctx, session.reader, preview.FlowID(project))
	type deploymentStep struct {
		sequence int64
		want     preview.DeploymentAnswer
	}
	request := func(steps ...deploymentStep) {
		t.Helper()
		for _, step := range steps {
			answer, err := session.previews.RequestDeployment(ctx, project, preview.DeploymentRequest{WorkspaceRequestID: instance, Commit: head, Sequence: step.sequence})
			if err != nil || answer != step.want {
				t.Fatalf("the request of sequence %d was answered %s, want %s (%T)", step.sequence, answer, step.want, err)
			}
		}
	}
	request(deploymentStep{1, preview.AnswerAccepted}, deploymentStep{1, preview.AnswerDuplicate})
	// Every Step of the deployment after its source preparation calls a
	// provider without a lock.
	first, newest := deployCommandID(1), deployCommandID(3)
	var running preview.Snapshot
	session.await(ctx, "the source of the first deployment", func() bool {
		var err error
		running, err = session.previews.Get(ctx, project)
		return err == nil && (running.PreparedSource.RequestID == first || running.Deployment.Last.Status != preview.DeploymentRunning)
	})
	if running.ActiveCommand.ID != first {
		t.Fatalf("the first deployment ended %s before its provider Steps ran (%s)", running.Deployment.Last.Status, failureCode(running))
	}
	request(deploymentStep{2, preview.AnswerQueued}, deploymentStep{3, preview.AnswerQueued}, deploymentStep{2, preview.AnswerDuplicate}, deploymentStep{3, preview.AnswerQueued})
	deployed := session.awaitDeployment(ctx, func(state preview.Snapshot) bool {
		if state.ActiveCommand.ID == first && (state.Deployment.Queued == nil || state.Deployment.Queued.Sequence != 3) {
			t.Fatalf("a Step of the running deployment dropped the queued request: %+v", state.Deployment)
		}
		return state.Deployment.Last.Sequence == 3
	})
	if last := deployed.Deployment.Last; last.Status != preview.DeploymentReady || last.CommandID != newest || deployed.Deployment.HandledSequence != 3 || deployed.Deployment.Queued != nil {
		t.Fatalf("the newest request did not deploy: %+v (%s)", deployed.Deployment, failureCode(deployed))
	}
	steps := completedStepsSince(t, ctx, session.reader, mark)
	if prepared := len(slices.DeleteFunc(steps, func(step string) bool { return step != dex.GetFinalStepType(preview.PreparePreviewSource{}) })); prepared != 2 {
		t.Fatalf("the Preview prepared %d sources; only sequences 1 and 3 may run", prepared)
	}
	session.stopPreview(ctx)
	t.Logf("FlowID=%s answered ACCEPTED, DUPLICATE, then, during the provider Steps of %s, QUEUED, QUEUED, DUPLICATE and QUEUED; %s deployed %s and sequence 2 never ran", session.id, first, newest, head)
}

// deployCommandID is the ID of the deploy-preview command of a handoff
// sequence, as the Preview derives it.
func deployCommandID(sequence int64) string {
	return fmt.Sprintf("%s-%d", preview.CommandDeployPreview, sequence)
}

// TestPreviewRefusedDeploymentKeepsServingProcessesDexIntegration builds a
// blocked checkpoint whose Flow does not compile. The new build reports FAILED,
// while the previously READY Preview keeps serving its processes and configuration.
// Retrying the build of current restored HEAD records READY again.
func TestPreviewRefusedDeploymentKeepsServingProcessesDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+2*mockTurnTimeout+3*previewCommandTimeout)
	defer cancel()
	session, first := newConfiguredPreviewSession(t, ctx)
	session.send(ctx, mockInput(t, "preview-broken-checkpoint", 1).Content)
	agent := session.finishedTurnAfter(ctx, first.Description.LastSequence)
	requireNoFailure(t, agent.History)
	if pushed := lastToolResult(t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitBlockedForTest {
		t.Fatalf("the checkpoint was not pushed as a blocked commit: %+v", pushed)
	}
	checkpoint := session.summary(ctx).Identity.HeadCommit
	if checkpoint == session.identity.HeadCommit {
		t.Fatalf("the checkpoint left the Session's HEAD at %s", checkpoint)
	}
	session.send(ctx, mockInput(t, "preview-broken-checkpoint", 2).Content)
	agent = session.finishedTurnAfter(ctx, agent.Description.LastSequence)
	requireNoFailure(t, agent.History)
	restored := session.summary(ctx).Identity.HeadCommit
	if pushed := lastToolResult(t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest || restored == checkpoint {
		t.Fatalf("the restored source was not pushed: %+v at %s", pushed, restored)
	}
	deployed := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == restored })
	if deployed.Deployment.Last.Status != preview.DeploymentReady || deployed.PreparedSource.SourceCommit != restored {
		t.Fatalf("the Preview did not deploy the restored %s: %s (%s)", restored, deployed.Deployment.Last.Status, failureCode(deployed))
	}

	project := session.identity.ProjectID
	request := preview.DeploymentRequest{WorkspaceRequestID: deployed.WorkspaceRequestID, Commit: checkpoint, Sequence: deployed.Deployment.HandledSequence + 1}
	if answer, err := session.previews.RequestDeployment(ctx, project, request); err != nil || answer != preview.AnswerAccepted {
		t.Fatalf("the deployment of the checkpoint was answered %s (%T)", answer, err)
	}
	refused := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Sequence == request.Sequence })
	switch last := refused.Deployment.Last; {
	case last.Status != preview.DeploymentFailed || last.Failure == nil || last.Failure.Code != "PREVIEW_FDG_INVALID" || last.Owner != conversation.DeliveryOwnerApplication:
		t.Fatalf("the checkpoint's deployment was not refused for its definitions: %s %s (%s)", last.Status, last.Owner, failureCode(refused))
	case refused.PreparedSource.Status != preview.PreviewSourceFailedForTest || refused.PreparedSource.SourceCommit != checkpoint || refused.PreparedSource.RequestID != deployCommandID(request.Sequence):
		t.Fatalf("the refused build did not record its failed processes: %s %s", refused.PreparedSource.Status, refused.PreparedSource.SourceCommit)
	case refused.AppManifest == nil || refused.AppManifest.SourceCommit != restored:
		t.Fatalf("the Preview pairs the preparation of %s with another commit's AppManifest", restored)
	case refused.Preview.Status != preview.PreviewReady || refused.Preview.SourceCommit != restored || refused.LastCommand.Kind != preview.CommandDeployPreview:
		// Refused before it started the Preview, the deployment leaves the
		// READY Preview serving and releases nothing.
		t.Fatalf("the refused deployment did not leave the Preview of %s serving: %s after %s", restored, refused.Preview.Status, refused.LastCommand.Kind)
	}
	manifest, err := session.service().ReadPreviewTarget(ctx, project)
	if err != nil || manifest.GetSourceCommit() != restored {
		t.Fatalf("the Preview configuration does not read the AppManifest of the prepared %s (%T)", restored, err)
	}
	session.await(ctx, "the notice of the refused checkpoint", func() bool {
		_, messages := session.history(ctx)
		return slices.ContainsFunc(ofRole(messages, conversation.MessageRoleNotification), func(message conversation.SequencedMessage) bool {
			notice := message.Message.Notification
			return notice != nil && notice.Commit == checkpoint && notice.Outcome == conversation.DeliveryNotice &&
				notice.Owner == conversation.DeliveryOwnerApplication && notice.Code == "PREVIEW_FDG_INVALID"
		})
	})

	// A build retry uses current pushed HEAD; the serving processes stayed untouched.
	session.preparePreview(ctx)
	if started := session.startPreview(ctx); started.Preview.SourceCommit != restored {
		t.Fatalf("the start after the refused deployment does not serve the prepared %s but %s", restored, started.Preview.SourceCommit)
	}
	session.stopPreview(ctx)
	t.Logf("FlowID=%s: the deployment of checkpoint %s was refused PREVIEW_FDG_INVALID; the preparation and AppManifest of %s stayed, and a start served it", session.id, checkpoint, restored)
}

// TestPreviewIncompatibleDeploymentDexIntegration asks, through the Session
// Service as the Session's DeployPreviewHandoff does, to deploy the HEAD of a
// fresh Project whose Preview nobody configured. The deployment passes
// VerifyPreviewSource without a lease and is refused
// PREVIEW_CONFIGURATION_INCOMPATIBLE (PreparePreviewConfiguration) before it
// started anything, so there is nothing to release: the Preview stays
// NOT_STARTED with the refusal, its gaps and its READY preparation, a state a
// person starts from. The Session records the refusal as a notice owned by
// the configuration. Once a person configured it, the Preview starts from
// that state: the next turn runs on the instance, or on a new one if its
// deadline reclaimed it meanwhile, and the source is prepared again first, as
// Studio's configuration view does.
func TestPreviewIncompatibleDeploymentDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*previewSetupTimeout+3*previewCommandTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	summary := session.summary(ctx)
	head := summary.Identity.HeadCommit
	handoff := agentloop.PreviewHandoff{Commit: head, Sequence: 1, PublishedSequence: 1}
	if err := session.service().RequestPreviewDeployment(ctx, session.identity, session.workspace(ctx).InstanceID, handoff); err != nil {
		t.Fatalf("the deployment request failed (%T)", err)
	}
	refused := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Sequence == 1 })
	switch last := refused.Deployment.Last; {
	case last.Status != preview.DeploymentIncompatible || last.Owner != conversation.DeliveryOwnerConfiguration ||
		refused.Failure == nil || refused.Failure.Code != preview.FailureIncompatibleForTest ||
		refused.Failure.Remedy != dexflow.RemedyOpenSetup || refused.FailureOwner != conversation.DeliveryOwnerConfiguration:
		t.Fatalf("the deployment of the unconfigured Preview was not refused for its configuration: %s %s (%s)", last.Status, last.Owner, failureCode(refused))
	case refused.LastCommand.Kind != preview.CommandDeployPreview || refused.Preview.Status != preview.PreviewNotStarted:
		t.Fatalf("the refused deployment started something to release: %s after %s", refused.Preview.Status, refused.LastCommand.Kind)
	case len(refused.Preview.ConfigurationGaps) == 0 || !refused.PreparedForForTest(head):
		t.Fatalf("the refused Preview keeps no gaps or no READY preparation of %s", head)
	}
	configurationNoticeID := ""
	session.await(ctx, "the configuration notice", func() bool {
		_, messages := session.history(ctx)
		return slices.ContainsFunc(ofRole(messages, conversation.MessageRoleNotification), func(message conversation.SequencedMessage) bool {
			notice := message.Message.Notification
			if notice == nil || notice.Commit != head || notice.Outcome != conversation.DeliveryNotice ||
				notice.Owner != conversation.DeliveryOwnerConfiguration || notice.Code != preview.FailureIncompatibleForTest ||
				notice.Remedy != dexflow.RemedyOpenSetup || !strings.HasPrefix(notice.Summary, "The Preview needs configuration: ") {
				return false
			}
			configurationNoticeID = notice.ID
			return true
		})
	})

	// A person returns, configures the Preview and starts it.
	session.send(ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurnAfter(ctx, agent.Description.LastSequence).History)
	session.preparePreview(ctx)
	session.configurePreview(ctx)
	started := session.startPreview(ctx)
	if started.Preview.SourceCommit != head {
		t.Fatalf("the Preview serves %s, not %s", started.Preview.SourceCommit, head)
	}
	session.await(ctx, "the resolving Preview-ready notification", func() bool {
		_, messages := session.history(ctx)
		return slices.ContainsFunc(ofRole(messages, conversation.MessageRoleNotification), func(message conversation.SequencedMessage) bool {
			ready := message.Message.Notification
			return ready != nil && ready.Outcome == conversation.DeliveryReady && ready.Commit == head &&
				ready.ResolvedNoticeID == configurationNoticeID && ready.URL == started.Preview.URL
		})
	})
	session.stopPreview(ctx)
	t.Logf("FlowID=%s: the deployment of %s was refused PREVIEW_CONFIGURATION_INCOMPATIBLE before it started anything; the resolving READY notification recorded the configured Preview", session.id, head)
}

// startPreview starts the Session's Preview through the Session Service, as
// Studio's Start Preview does, with the Preview it reads now, and requires it
// READY.
func (s *mockSession) startPreview(ctx context.Context) preview.Snapshot {
	s.t.Helper()
	current, err := s.previews.Get(ctx, s.identity.ProjectID)
	if err != nil {
		s.t.Fatalf("the Preview read failed (%T)", err)
	}
	start := preview.Command{ID: "start-" + time.Now().UTC().Format("20060102150405.000000"), Kind: preview.CommandStartPreview,
		WorkspaceRequestID: current.WorkspaceRequestID, Generation: current.Generation, ExpectedRevision: current.Revision}
	if _, err = s.service().PreviewCommand(ctx, s.account, s.identity.ProjectID, start); err != nil {
		s.t.Fatalf("the Preview start was not admitted (%T)", err)
	}
	started := s.awaitPreview(ctx, start.ID)
	if started.Preview.Status != preview.PreviewReady {
		s.t.Fatalf("the Preview start ended %s (%s)", started.Preview.Status, failureCode(started))
	}
	return started
}

// TestPreviewBuildResumesReclaimedInstanceDexIntegration lets the Session
// reclaim its idle workspace, then admits a pinned commit build. It resumes a
// new instance and builds processes before reporting missing Preview settings.
func TestPreviewBuildResumesReclaimedInstanceDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+hold+sandboxReclaimTimeout+previewCommandTimeout)
	defer cancel()
	started := time.Now().UTC()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	ready := session.workspace(ctx)
	closed := session.awaitReclaim(ctx, ready)
	head := session.summary(ctx).Identity.HeadCommit
	handoff := agentloop.PreviewHandoff{Commit: head, Sequence: 1, PublishedSequence: 1}
	if err := session.service().RequestPreviewDeployment(ctx, session.identity, ready.InstanceID, handoff); err != nil {
		t.Fatalf("the deployment request failed (%T)", err)
	}
	built := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Sequence == 1 })
	if !built.PreparedForForTest(head) || built.Deployment.Last.Status != preview.DeploymentIncompatible {
		t.Fatalf("the reclaimed workspace did not build processes: %s %s (%s)", built.PreparedSource.Status, built.Deployment.Last.Status, failureCode(built))
	}
	after := session.workspace(ctx)
	if after.InstanceID == ready.InstanceID || after.Instance != sessionworkspace.InstanceReady || after.RequestID == closed.RequestID {
		t.Fatalf("the build did not acquire a new workspace: %s %s", after.RequestID, after.Instance)
	}
	if flowIDs := session.provisioningFlowIDs(ctx, started); len(flowIDs) != 2 {
		t.Fatalf("the resumed build provisioned %v; exactly two instances are expected", flowIDs)
	}
	t.Logf("FlowID=%s built %s on %s after reclaiming %s", session.id, built.PreparedSource.RequestID, after.InstanceID, ready.InstanceID)
}

// awaitDeployment waits until the Project's Preview ended the deployment that
// done names and no command runs, and returns the Preview after it.
func (s *mockSession) awaitDeployment(ctx context.Context, done func(preview.Snapshot) bool) preview.Snapshot {
	s.t.Helper()
	var state preview.Snapshot
	s.await(ctx, "the Preview deployment", func() bool {
		var err error
		state, err = s.previews.Get(ctx, s.identity.ProjectID)
		return err == nil && done(state) && state.Deployment.Last.Status != preview.DeploymentRunning && state.ActiveCommand.ID == ""
	})
	return state
}

// awaitWorkspace waits until the Session's workspace satisfies done and
// returns it.
func (s *mockSession) awaitWorkspace(ctx context.Context, what string, done func(workspaceView) bool) workspaceView {
	s.t.Helper()
	var ws workspaceView
	s.await(ctx, what, func() bool {
		ws = s.workspace(ctx)
		return done(ws)
	})
	return ws
}

// stopPreview stops the Project's Preview through the Session Service, as
// Studio does, with the Preview it reads now, and waits for the stop.
func (s *mockSession) stopPreview(ctx context.Context) {
	s.t.Helper()
	current, err := s.previews.Get(ctx, s.identity.ProjectID)
	if err != nil {
		s.t.Fatalf("the Preview read failed (%T)", err)
	}
	stop := preview.Command{ID: "stop-" + time.Now().UTC().Format("20060102150405.000000"), Kind: preview.CommandStopPreview,
		WorkspaceRequestID: current.WorkspaceRequestID, Generation: current.Generation, ExpectedRevision: current.Revision}
	if _, err = s.service().PreviewCommand(ctx, s.account, s.identity.ProjectID, stop); err != nil {
		s.t.Fatalf("the Preview stop was not admitted (%T)", err)
	}
	if stopped := s.awaitPreview(ctx, stop.ID); stopped.Preview.Status != preview.PreviewStoppedForTest {
		s.t.Fatalf("the Preview did not stop: %s (%s)", stopped.Preview.Status, failureCode(stopped))
	}
}

// requireImmediatePreviewHandoff inspects real Dex history: the successful push
// result schedules deployment alongside the Agent continuation, not at turn end.
func requireImmediatePreviewHandoff(t *testing.T, ctx context.Context, session *mockSession) {
	t.Helper()
	flowID := string(session.id)
	for _, event := range runHistory(t, ctx, session.reader, flowID, currentRun(t, ctx, session.reader, flowID)) {
		completed := event.GetStepExecuteCompleted()
		if completed == nil {
			continue
		}
		next := completed.GetOutput().GetStepDecision().GetNextSteps()
		pushes, continues := false, false
		for _, movement := range next {
			pushes = pushes || movement.GetStepType() == dex.GetFinalStepType(sessionagent.DeployPreviewHandoff{})
			continues = continues || movement.GetStepType() == dex.GetFinalStepType(sessionagent.CheckSteered{})
		}
		if pushes {
			if completed.GetContext().GetStepType() != dex.GetFinalStepType(sessionagent.ExecuteTool{}) || !continues {
				t.Fatalf("the handoff did not accompany the pushed tool result: %s", completed.GetContext().GetStepType())
			}
			return
		}
	}
	t.Fatal("the successful push scheduled no immediate Preview handoff")
}

// TestSessionPushBuildsProcessesBeforeConfigurationDexIntegration pushes through
// the real source gate with default auto refresh and no Preview settings. The
// pinned processes become READY despite the later configuration refusal.
func TestSessionPushBuildsProcessesBeforeConfigurationDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+previewDeployTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	first := session.finishedTurn(ctx)
	requireNoFailure(t, first.History)
	if !session.description(ctx).IsPreviewAutoDeployEnabled {
		t.Fatal("auto refresh is not on by default")
	}
	note := "processes-" + time.Now().UTC().Format("20060102150405")
	session.send(ctx, mockInput(t, "preview-autodeploy", 1).Content+" "+mockmodel.ArgumentMarkerForTest("note", note))
	agent := session.finishedTurnAfter(ctx, first.Description.LastSequence)
	requireNoFailure(t, agent.History)
	head := session.summary(ctx).Identity.HeadCommit
	built := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == head })
	if !built.PreparedForForTest(head) || built.Deployment.Last.Status != preview.DeploymentIncompatible || len(built.Preview.ConfigurationGaps) == 0 {
		t.Fatalf("processes did not become ready before the missing configuration: %s %s (%s)", built.PreparedSource.Status, built.Deployment.Last.Status, failureCode(built))
	}
	if built.PreparedSource.Version.Number == 0 || !strings.Contains(built.PreparedSource.Version.Subject, note) {
		t.Fatalf("built processes have no Git-owned pushed version: %+v", built.PreparedSource.Version)
	}
	requireImmediatePreviewHandoff(t, ctx, session)
	requirePublishedBuildVersion(t, ctx, session, built)
	t.Logf("FlowID=%s build=%s version=V%d is READY while Preview needs configuration", session.id, built.PreparedSource.RequestID, built.PreparedSource.Version.Number)
}

// requirePublishedBuildVersion reads real engine history through the pinned
// CLI: the named PREPARING state must commit before the compilation Step.
func requirePublishedBuildVersion(t *testing.T, ctx context.Context, session *mockSession, built preview.Snapshot) {
	t.Helper()
	flowID := preview.FlowID(session.identity.ProjectID)
	summary, err := session.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		t.Fatalf("the build execution identity read failed (%T)", err)
	}
	call, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(call, dexflowtest.Setting(t, "DEXCLI"), "flow", "history", flowID, //nolint:gosec // the operator's repository-pinned diagnostic CLI
		"--server", dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), "--run-id", summary.GetFlowExecutionId().GetRunId(), "--timeout", "15s", "--page-size", "500")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("the pinned CLI history read failed (%T)", err)
	}
	var history struct {
		Events []struct {
			Type    string
			Payload struct {
				Context struct{ StepType string }
				Output  json.RawMessage
			}
		}
		NextPageToken string
	}
	if err := json.Unmarshal(output, &history); err != nil || history.NextPageToken != "" {
		t.Fatal("the bounded process-build history is incomplete")
	}
	named := false
	for _, event := range history.Events {
		step := event.Payload.Context.StepType
		if event.Type != "StepExecuteCompleted" || step != dex.GetFinalStepType(preview.ReadPreviewSourceVersion{}) && step != dex.GetFinalStepType(preview.PreparePreviewSource{}) {
			continue
		}
		var output struct {
			UpsertAttributes []struct {
				Key   string
				Value json.RawMessage
			}
		}
		if err := json.Unmarshal(event.Payload.Output, &output); err != nil {
			t.Fatal("the process-build Step output is unreadable")
		}
		for _, attribute := range output.UpsertAttributes {
			if attribute.Key != preview.PreparedPreviewSourceAttribute.AttributeName() {
				continue
			}
			var source preview.PreparedPreviewSource
			if err := json.Unmarshal(attribute.Value, &source); err != nil {
				t.Fatal("the persisted building-version state is unreadable")
			}
			if source.RequestID != built.PreparedSource.RequestID || source.SourceCommit != built.PreparedSource.SourceCommit || source.Version != built.PreparedSource.Version {
				continue
			}
			if step == dex.GetFinalStepType(preview.ReadPreviewSourceVersion{}) && source.Status == preview.PreviewSourcePreparingForTest {
				named = true
			}
			if step == dex.GetFinalStepType(preview.PreparePreviewSource{}) && source.Status == preview.PreviewSourceReadyForTest {
				if !named {
					t.Fatal("compilation completed before its named PREPARING state committed")
				}
				return
			}
		}
	}
	t.Fatal("compilation had no preceding durable PREPARING state with its Git-owned version")
}

// TestSessionPushedProcessesSurviveStopDexIntegration leaves the turn running
// after its push, then stops it. The accepted asynchronous build still finishes.
func TestSessionPushedProcessesSurviveStopDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+previewDeployTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "preview-push-stop", 1).Content+" "+mockmodel.ArgumentMarkerForTest("note", "stop-"+time.Now().UTC().Format("20060102150405")))
	running := session.waitForSnapshot(ctx, "the check after the push", func(agent agentloop.AgentSnapshot) bool {
		return len(replies(agent.History)) == 4 && agent.Description.Status != agentloop.AgentStatusWaitingForMessage
	})
	head := session.summary(ctx).Identity.HeadCommit
	requireImmediatePreviewHandoff(t, ctx, session)
	if err := session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Turn: running.Description.Turn}); err != nil {
		t.Fatalf("Stop after push failed: %v", err)
	}
	session.waitForSnapshot(ctx, "the stopped turn", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	built := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == head })
	if !built.PreparedForForTest(head) || built.Deployment.Last.Status != preview.DeploymentIncompatible {
		t.Fatalf("Stop lost the pushed processes: %s %s (%s)", built.PreparedSource.Status, built.Deployment.Last.Status, failureCode(built))
	}
	t.Logf("FlowID=%s Stop left pushed build %s READY", session.id, built.PreparedSource.RequestID)
}

// TestPreviewProcessesSurviveWorkspaceReplacementDexIntegration loses a real
// Sandbox after a READY build, reads the immutable processes/configuration,
// and starts the same version on its replacement without rebuilding definitions.
func TestPreviewProcessesSurviveWorkspaceReplacementDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+mockTurnTimeout+2*previewCommandTimeout)
	defer cancel()
	session, first := newConfiguredPreviewSession(t, ctx)
	prepared, err := session.previews.Get(ctx, session.identity.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	lost, objects := session.workspace(ctx), newSandboxObjects(t)
	objects.removeInstance(ctx, lost.Resources)
	session.await(ctx, "the removed Sandbox", func() bool { return objects.instanceGone(ctx, lost.Resources) })
	session.send(ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurnAfter(ctx, first.Description.LastSequence).History)
	replaced := session.workspace(ctx)
	if replaced.InstanceID == lost.InstanceID || replaced.Resources.Volume.UID != lost.Resources.Volume.UID {
		t.Fatalf("the workspace was not replaced on its retained volume: %s", replaced.InstanceID)
	}
	read, definitions, err := session.previews.ReadDefinitions(ctx, session.identity.ProjectID)
	if err != nil || len(definitions) == 0 || read.PreparedSource.DefinitionRevision != prepared.PreparedSource.DefinitionRevision {
		t.Fatalf("workspace replacement hid the READY process bundle (%T)", err)
	}
	manifest, err := session.service().ReadPreviewTarget(ctx, session.identity.ProjectID)
	if err != nil || manifest.GetSourceCommit() != prepared.PreparedSource.SourceCommit {
		t.Fatalf("workspace replacement hid the prepared configuration (%T)", err)
	}
	mark := markHistory(t, ctx, session.reader, preview.FlowID(session.identity.ProjectID))
	started := session.startPreview(ctx)
	if started.PreparedSource.WorkspaceRequestID != replaced.InstanceID || started.Preview.SourceCommit != prepared.PreparedSource.SourceCommit || started.PreparedSource.DefinitionRevision != prepared.PreparedSource.DefinitionRevision {
		t.Fatal("the manual start did not rebind the existing processes to the replacement")
	}
	if steps := completedStepsSince(t, ctx, session.reader, mark); slices.Contains(steps, dex.GetFinalStepType(preview.PreparePreviewSource{})) {
		t.Fatal("manual start regenerated the immutable process bundle")
	}
	session.stopPreview(ctx)
	t.Logf("FlowID=%s kept processes %s and started the same version on %s instead of %s", session.id, prepared.PreparedSource.DefinitionRevision, replaced.InstanceID, lost.InstanceID)
}
