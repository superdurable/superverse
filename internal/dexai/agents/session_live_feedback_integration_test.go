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
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/dex"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deliverydao"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	buildlocal "github.com/superdurable/superversev2/internal/dexai/delivery/releases/build/local"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
)

// These scenarios run the Live delivery feedback on the Session of a
// throwaway READY Project and the actual Worker of the mock Session tests, on
// either local lane: a Release or a Live deployment of the Session's pushed
// commit fails, ProjectReleaseFlow or ProjectDeployFlow records the failure
// with its owner, and the Session receives it as a delivery notification,
// which opens an automatic turn only for a failure of its HEAD that the
// application owns. Dex, the Sandbox, the Git host, the local build service
// and the Live runtime are real; only the model is the mock. Each test that
// pushes turns the Preview's automatic deployment off first, so no Preview
// deployment takes part, and the Project's deletion at the end of the test
// removes its Releases and deployments.

const (
	// liveSourceTimeout bounds a Release's source preparation: its admission
	// and the 5m30s wait limit of PrepareReleasePublishingSource.
	liveSourceTimeout = 8 * time.Minute
	// liveBuildTimeout bounds one build of the template on the local build
	// service, with its configuration freezing and publication.
	liveBuildTimeout = 20 * time.Minute
	// liveDeploymentTimeout bounds a first Live deployment whose application
	// never becomes ready: the Live preparation of the Project, its Dex and
	// the 10-minute readiness wait limit.
	liveDeploymentTimeout = 45 * time.Minute
	// staleLiveRevision is a Live generation that a fresh Project never has,
	// so an automatic deployment that expects it is rejected.
	staleLiveRevision = 7
	// skipPreview is the reason the scenarios that publish a pushed commit
	// without running it on Preview give for skipping the Preview validation.
	skipPreview = "The scenario publishes a pushed commit without running it on Preview"
)

// liveDelivery reaches the Release and Live deployment Flows of a Session's
// Project on the existing Worker, and the configuration of its Releases, for
// the Session's owner.
type liveDelivery struct {
	session       *mockSession
	projects      *projects.ProjectFlowClient
	releases      *releases.ProjectReleaseFlowClient
	deployments   *deployments.ProjectDeployFlowClient
	configuration *projectconfig.Configuration
}

// newLiveDelivery builds the clients of the Session's Project. The Release
// client admits a Release only with a build service, so it holds the client
// of the lane's local build service (SUPERVERSE_LOCAL_BUILD_URL and
// SUPERVERSE_LOCAL_BUILD_TOKEN_FILE), which the test never calls: the Worker
// builds with its own. The configuration opens the Project's storage through
// the lane's runtime (laneStorage).
func newLiveDelivery(s *mockSession) *liveDelivery {
	s.t.Helper()
	token, err := os.ReadFile(dexflowtest.Setting(s.t, "SUPERVERSE_LOCAL_BUILD_TOKEN_FILE"))
	if err != nil {
		s.t.Fatal(err)
	}
	builder, err := buildlocal.New(dexflowtest.Setting(s.t, "SUPERVERSE_LOCAL_BUILD_URL"), strings.TrimSpace(string(token)))
	if err != nil {
		s.t.Fatal(err)
	}
	address := dexflowtest.Setting(s.t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectFlow, deployFlow := &projects.ProjectFlow{}, &deployments.ProjectDeployFlow{}
	projectClient := &projects.ProjectFlowClient{Flow: projectFlow, Projections: s.projects.Projections, ProjectIDLength: s.projects.ProjectIDLength, PlatformAdminEmails: []string{"admin@superverse.test"}}
	deploymentClient := deployments.NewProjectDeployFlowClient(projectClient, nil, nil)
	// Build admission runs in this client: it checks the Live draft through the publishing Service and
	// reads the Preview's starts through the Session Service, as the server's composition does.
	configuration := &projectconfig.Configuration{}
	publishing := deployments.NewPublishingFlowService(projectClient, deploymentClient, configuration, nil, deployments.BuildLogReads{PerSecond: 2})
	// The Flow's recipes are the local lanes', so the provider requests of a build log read name the recipe the lane built with.
	recipes := build.NewProfiles()
	recipes.SelectLocalDefault()
	releaseFlow := releases.NewProjectReleaseFlow(projectClient, nil, builder, recipes, nil, deploymentnames.EnvironmentTest, publishing, nil)
	// This client registry supplies definitions only; the existing Worker
	// runs the Flows with every dependency of its server.
	projectClient.Dex = dexflowtest.ClientForTest(s.t, address, &dex.WorkerTarget{Pull: true}, projectFlow, releaseFlow, deployFlow)
	releaseClient := releases.NewProjectReleaseFlowClient(releaseFlow, projectClient, build.NewProfiles())
	deploymentClient.Releases = releaseClient
	rows := deliverydao.New(postgrestest.OpenForTest(s.t))
	releaseClient.Rows, deploymentClient.Rows, releaseClient.Previews = rows, rows, s.service()
	*configuration = projectconfig.Configuration{Logger: slog.Default(), Projects: projectClient, Releases: releaseClient, Storage: laneStorage(s.t), Preview: s.service(), ReleaseCandidates: laneReleaseCandidates(s.t), Shared: laneTeamSpaceConnections(s.t.Context(), s.t, projectClient)}
	s.t.Cleanup(configuration.Close)
	return &liveDelivery{session: s, projects: projectClient, releases: releaseClient, deployments: deploymentClient, configuration: configuration}
}

// target is the configuration target of Live's next-publish layer checked
// against the source of the Release of commit, as Setup · Live edits it.
func (l *liveDelivery) target(commit string) *pb.FlowApiTarget {
	return &pb.FlowApiTarget{ProjectId: l.session.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, ReleaseId: commit, NextPublish: true}
}

// publishCopy builds the frozen copy of the Release of commit from Live's
// next-publish layer at the revisions the Release's configuration shows, as
// publishing does with DeriveLiveConfiguration, and returns the copy's
// revision, which a build freezes.
func (l *liveDelivery) publishCopy(ctx context.Context, commit string) uint64 {
	l.session.t.Helper()
	project := l.session.identity.ProjectID
	shown, _, err := l.configuration.Get(ctx, l.session.account, &pb.ProjectConfigurationTarget{ProjectId: project, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, ReleaseId: commit})
	if err != nil {
		l.session.t.Fatalf("Live's next publish of %s was not read: %v", commit, err)
	}
	copied, err := l.configuration.DeriveLive(ctx, l.session.account, project, commit, shown.GetNextPublishRevision(), shown.GetPreviewConfigurationRevision())
	if err != nil {
		l.session.t.Fatalf("the frozen copy of %s was not built: %v", commit, err)
	}
	return copied.ConfigurationRevision
}

// prepare asks for the Release of commit as PrepareRelease does, which starts
// it or, after a failed source preparation, admits a retry, and returns the
// Release once its source preparation of that attempt (1 for the first)
// ended.
func (l *liveDelivery) prepare(ctx context.Context, commit string, attempt int) releases.Snapshot {
	l.session.t.Helper()
	if _, err := l.releases.Prepare(ctx, l.session.account, l.session.identity.ProjectID, uuid.NewString(), commit); err != nil {
		l.session.t.Fatalf("the Release of %s was not admitted (%T)", commit, err)
	}
	return l.awaitRelease(ctx, commit, "source preparation "+strconv.Itoa(attempt), func(release releases.Snapshot) bool {
		source := release.PreparedSource
		return len(source.Requests) >= attempt && (source.Status == releases.Ready || source.Status == releases.Failed)
	})
}

// awaitRelease waits until the Release of commit satisfies done and returns it.
func (l *liveDelivery) awaitRelease(ctx context.Context, commit, what string, done func(releases.Snapshot) bool) releases.Snapshot {
	l.session.t.Helper()
	var release releases.Snapshot
	l.session.await(ctx, what+" of the Release of "+commit, func() bool {
		var err error
		release, err = l.releases.ReadSnapshot(ctx, l.session.identity.ProjectID, commit)
		return err == nil && done(release)
	})
	return release
}

// build admits a Save and Build of the Release of commit as StartReleaseBuild
// does, at the Release's current Revision, and returns the build revision once
// it settled, failed or READY with its automatic deployment decided, with the
// Release after it.
func (l *liveDelivery) build(ctx context.Context, commit string, request releases.BuildRevisionRequest) (releases.ReleaseBuildRevision, releases.Snapshot) {
	l.session.t.Helper()
	project := l.session.identity.ProjectID
	release := l.awaitRelease(ctx, commit, "the read", func(releases.Snapshot) bool { return true })
	request.RequestID, request.BuildRevisionID, request.ExpectedRevision = uuid.NewString(), "build-"+uuid.NewString(), release.Revision
	if _, err := l.releases.BuildRevision(ctx, l.session.account, project, commit, request); err != nil {
		l.session.t.Fatalf("the build of %s was not admitted (%T)", commit, err)
	}
	var revision releases.ReleaseBuildRevision
	l.session.await(ctx, "build "+request.BuildRevisionID, func() bool {
		var err error
		revision, err = l.releases.ReadBuildRevision(ctx, project, commit, request.BuildRevisionID)
		decided := !request.AutoDeploy || revision.AutoDeployFailure != nil || revision.AutoDeploymentID != ""
		return err == nil && (revision.Status == releases.Failed || revision.Status == releases.Ready && decided)
	})
	return revision, l.awaitRelease(ctx, commit, "the read", func(releases.Snapshot) bool { return true })
}

// deploy deploys a READY build of the Release of commit as StartDeployment
// does, where Live serves nothing yet, and returns the deployment once it
// failed or is READY.
func (l *liveDelivery) deploy(ctx context.Context, commit string, built releases.ReleaseBuildRevision) deployments.Snapshot {
	l.session.t.Helper()
	project := l.session.identity.ProjectID
	start := deployments.Start{ProjectID: project, DeploymentID: uuid.NewString(), ReleaseID: commit, BuildRevisionID: built.BuildRevisionID, ConfigurationRevision: built.Configuration.ConfigurationRevision}
	deploymentID, err := l.deployments.DeployReadyBuild(ctx, l.session.account, uuid.NewString(), start)
	if err != nil {
		l.session.t.Fatalf("the deployment of %s was not admitted (%T)", built.BuildRevisionID, err)
	}
	var deployment deployments.Snapshot
	l.session.await(ctx, "deployment "+deploymentID, func() bool {
		deployment, err = l.deployments.ReadSnapshotForTest(ctx, project, deploymentID)
		return err == nil && (deployment.Status == deployments.Failed || deployment.Status == deployments.Ready)
	})
	// The start identity reaches the read model, where it names the actor of the Live timeline.
	if deployment.Identity.ActorAccountID != l.session.account {
		l.session.t.Fatalf("the deployment %s records the actor %q, want the Session's account", deploymentID, deployment.Identity.ActorAccountID)
	}
	postgrestest.AwaitRowForTest(ctx, l.session.t, postgrestest.OpenForTest(l.session.t), "deployments", func() map[string]any {
		return map[string]any{"deployment_id": deploymentID, "start_identity": deployment.Identity}
	}, "project_id = $1 AND deployment_id = $2", project, deploymentID)
	events, _, err := deployments.ListLiveEvents(ctx, l.deployments.Rows, project, nil, 10)
	if err != nil || len(events) == 0 || events[0].DeploymentID != deploymentID || events[0].Published || events[0].ActorAccountID != l.session.account || events[0].ReleaseID != commit {
		l.session.t.Fatalf("the Live timeline lists %+v (%v), want the deployment of %s by the Session's account first", events, err, commit)
	}
	return deployment
}

// setPreviewAutoDeploy turns the Preview's automatic deployment on or off
// through the Session Service, as PreviewService.UpdatePreviewAutoDeploy does.
func (s *mockSession) setPreviewAutoDeploy(ctx context.Context, isEnabled bool) {
	s.t.Helper()
	if err := s.service().UpdatePreviewAutoDeploy(ctx, accounts.AccountSummary{AccountID: s.account}, s.identity.ProjectID, isEnabled); err != nil {
		s.t.Fatalf("the automatic deployment of %s was not turned to %t (%T)", s.id, isEnabled, err)
	}
}

// description reads the Session's Agent description from its Snapshot.
func (s *mockSession) description(ctx context.Context) agentloop.AgentDescription {
	s.t.Helper()
	snapshot, err := s.client.GetSnapshot(ctx, s.id)
	if err != nil || snapshot.Agent == nil {
		s.t.Fatalf("reading the Snapshot of %s failed (%T)", s.id, err)
	}
	return snapshot.Agent.Description
}

// awaitNotification waits until the Session recorded the notification id and
// returns it.
func (s *mockSession) awaitNotification(ctx context.Context, id string) conversation.SequencedMessage {
	s.t.Helper()
	var found conversation.SequencedMessage
	s.await(ctx, "notification "+id, func() bool {
		_, messages := s.history(ctx)
		index := slices.IndexFunc(ofRole(messages, conversation.MessageRoleNotification), func(message conversation.SequencedMessage) bool {
			return message.Message.Notification != nil && message.Message.Notification.ID == id
		})
		if index >= 0 {
			found = ofRole(messages, conversation.MessageRoleNotification)[index]
		}
		return index >= 0
	})
	return found
}

// requireNoTurnAfter waits as long as a turn takes to start and fails when the
// Session replied after sequence or left turn.
func (s *mockSession) requireNoTurnAfter(ctx context.Context, sequence conversation.Sequence, turn int64, what string) {
	s.t.Helper()
	time.Sleep(15 * time.Second)
	agent, messages := s.history(ctx)
	if replies := ofRole(since(messages, sequence), conversation.MessageRoleAssistant); len(replies) != 0 || agent.Description.Turn != turn {
		s.t.Fatalf("%s opened a turn: %d replies, turn %d instead of %d", what, len(replies), agent.Description.Turn, turn)
	}
}

// requireDelivery fails unless notification is a delivery notification of
// source with outcome, owner, code and the remedy the code declared for
// commit, whose summary names neither the code nor the commit.
func requireDelivery(t *testing.T, notification *conversation.SessionNotification, source conversation.NotificationSource, outcome conversation.DeliveryOutcome, owner conversation.DeliveryOwner, code string, remedy dexflow.FailureRemedy, commit string) {
	t.Helper()
	if notification == nil || notification.Kind != conversation.NotificationKindDelivery || notification.Source != source || notification.Outcome != outcome ||
		notification.Owner != owner || notification.Code != code || notification.Remedy != remedy || notification.Commit != commit {
		t.Fatalf("the notification is not the %s %s of %s that the %s owns with %s and %s: %+v", source, outcome, commit, owner, code, remedy, notification)
	}
	if strings.Contains(notification.Summary, code) || strings.Contains(notification.Summary, commit[:7]) {
		t.Fatalf("the summary %q names the code or the commit", notification.Summary)
	}
}

// TestSessionMockLiveFailureFixDexIntegration turns the Preview's automatic
// deployment off, then pushes a server that refuses to start without a
// setting that nothing sets in Live (live-failure-fix). It prepares a Release
// of the pushed commit and builds it with the Release's unconfigured draft,
// which FreezeReleaseBuildConfiguration refuses with the draft's gaps: a
// notice the configuration owns. Once the draft is configured, a build whose
// automatic deployment expects a Live generation the Project does not have
// stays READY with its deployment rejected: a notice the platform owns. Then
// it deploys that build: the deployed application exits, the deployment fails
// in a phase the application owns, and its failure notification, for the
// Session's HEAD, opens an automatic turn, in which the mock reads the
// delivery status, pushes the fix and asks the user to publish it. Only that
// failure opens a turn; each ID names the Revision its failure was committed
// at, and the failure delivered again records nothing.
func TestSessionMockLiveFailureFixDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*mockTurnTimeout+liveSourceTimeout+2*liveBuildTimeout+liveDeploymentTimeout)
	defer cancel()
	session := newMockSession(t, ctx, "")
	session.setPreviewAutoDeploy(ctx, false)
	// This case tests a required configuration refusal. The default
	// template's connection is optional, so make this fixture explicit.
	session.send(ctx, mockInput(t, "configuration-gap-commit", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	session.send(ctx, mockInput(t, "live-failure-fix", 1).Content)
	first := session.finishedTurn(ctx)
	requireNoFailure(t, first.History)
	if pushed := lastToolResult(t, first.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the startup check was not pushed: %+v", pushed)
	}
	head := session.summary(ctx).Identity.HeadCommit
	live := newLiveDelivery(session)
	if release := live.prepare(ctx, head, 1); release.PreparedSource.Status != releases.Ready {
		t.Fatalf("the source of %s was not prepared: %+v", head, release.PreparedSource.Failure)
	}

	// The unconfigured draft: refused before it froze, owned by the configuration.
	refused, release := live.build(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: live.publishCopy(ctx, head), PreviewSkipReason: skipPreview})
	if refused.Status != releases.Failed || refused.Failure == nil || refused.Failure.Code != "RELEASE_BUILD_FAILED" || refused.Configuration != nil {
		t.Fatalf("the build of the unconfigured draft was not refused before it froze: %s %+v", refused.Status, refused.Failure)
	}
	gapsNotice := session.awaitNotification(ctx, fmt.Sprintf("release/%s/%s/r%d", head, refused.BuildRevisionID, release.Revision))
	requireDelivery(t, gapsNotice.Message.Notification, conversation.NotificationSourceRelease, conversation.DeliveryNotice, conversation.DeliveryOwnerConfiguration, "RELEASE_BUILD_FAILED", dexflow.RemedyOpenSetup, head)
	if gaps := gapsNotice.Message.Notification; gaps.GapCount == 0 || len(gaps.Gaps) == 0 || gaps.ResourceID != refused.BuildRevisionID {
		t.Fatalf("the configuration notice names no gap of build %s: %+v", refused.BuildRevisionID, gaps)
	}
	session.requireNoTurnAfter(ctx, gapsNotice.Sequence, first.Description.Turn, "the configuration notice")

	// A rejected automatic deployment leaves the build READY, owned by the platform.
	session.configureConnections(ctx, live.configuration, live.target(head))
	built, release := live.build(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: live.publishCopy(ctx, head), AutoDeploy: true, ExpectedLiveRevision: staleLiveRevision, PreviewSkipReason: skipPreview})
	if built.Status != releases.Ready || built.AutoDeployFailure == nil || built.AutoDeploymentID != "" || built.Configuration == nil {
		t.Fatalf("the automatic deployment of a READY build was not rejected: %s %+v", built.Status, built.AutoDeployFailure)
	}
	rejectedNotice := session.awaitNotification(ctx, fmt.Sprintf("release/%s/%s/r%d", head, built.BuildRevisionID, release.Revision))
	requireDelivery(t, rejectedNotice.Message.Notification, conversation.NotificationSourceRelease, conversation.DeliveryNotice, conversation.DeliveryOwnerPlatform, built.AutoDeployFailure.Code, dexflow.RemedyPublishAgain, head)
	session.requireNoTurnAfter(ctx, rejectedNotice.Sequence, first.Description.Turn, "the rejected automatic deployment")

	// The application exits in Live: a failure of HEAD that opens a turn.
	deployment := live.deploy(ctx, head, built)
	if deployment.Status != deployments.Failed || deployment.Failure == nil || deployment.Failure.Code != deployments.FailureDeploymentForTest {
		t.Fatalf("the deployment of an application that exits did not fail: %s %+v", deployment.Status, deployment.Failure)
	}
	failedNotice := session.awaitNotification(ctx, fmt.Sprintf("deployment/%s/g%d/r%d", deployment.DeploymentID, deployment.Lease.Generation, deployment.Revision))
	failed := failedNotice.Message.Notification
	requireDelivery(t, failed, conversation.NotificationSourceDeployment, conversation.DeliveryFailure, conversation.DeliveryOwnerApplication, deployments.FailureDeploymentForTest, dexflow.RemedyAskAgent, head)
	if failed.ResourceID != deployment.DeploymentID || failed.Phase != deployment.Failure.Phase {
		t.Fatalf("the failure names deployment %s at %s, not %s at %s", failed.ResourceID, failed.Phase, deployment.DeploymentID, deployment.Failure.Phase)
	}
	session.finishedTurnAfter(ctx, failedNotice.Sequence)
	agent, all := session.history(ctx)
	messages := since(all, first.Description.LastSequence)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	automatic := ofRole(since(messages, failedNotice.Sequence), conversation.MessageRoleAssistant)
	if len(automatic) != 4 || !strings.Contains(automatic[3].Message.Content, "publish this commit to Live") {
		t.Fatalf("expected the 4 replies of the automatic turn ending with the fix; a stopped scenario answers with one text: %+v", automatic)
	}
	if notifications := ofRole(messages, conversation.MessageRoleNotification); len(notifications) != 3 {
		t.Fatalf("expected the configuration notice, the platform notice and the deployment failure, got %d notifications", len(notifications))
	}
	if pushed := toolResult(t, all, conversation.ToolCommitAndPush, 1); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the automatic turn did not push the fix: %+v", pushed)
	}
	if fixedHead := session.summary(ctx).Identity.HeadCommit; fixedHead == head {
		t.Fatalf("the fix left the Session's HEAD at %s", fixedHead)
	}

	// A repeated delivery of the same failure, as a retried notify Step makes
	// it, is admitted once.
	repeat := releases.DeliveryFailure{ID: failed.ID, ProjectID: session.identity.ProjectID, Source: releases.SourceDeployment, Owner: conversation.DeliveryOwnerApplication,
		Commit: head, ResourceID: deployment.DeploymentID, Failure: *deployment.Failure, OccurredAt: failed.OccurredAt}
	if err := session.service().ReportDeliveryFailure(ctx, repeat); err != nil {
		t.Fatalf("the repeated delivery failed (%T)", err)
	}
	session.requireNoTurnAfter(ctx, agent.Description.LastSequence, agent.Description.Turn, "the repeated failure")
	_, all = session.history(ctx)
	if count := len(slices.DeleteFunc(ofRole(all, conversation.MessageRoleNotification), func(message conversation.SequencedMessage) bool {
		return message.Message.Notification.ID != failed.ID
	})); count != 1 {
		t.Fatalf("the failure %s was recorded %d times", failed.ID, count)
	}
	t.Logf("FlowID=%s: %s refused %s for its gaps, rejected the automatic deployment of %s and failed deployment %s at %s; the automatic turn pushed the fix",
		session.id, head, refused.BuildRevisionID, built.BuildRevisionID, deployment.DeploymentID, deployment.Failure.Phase)
}

// TestLiveSourceFailureNotifiesDexIntegration turns the Preview's automatic
// deployment off and pushes a blocked checkpoint whose Flow source does not
// compile (preview-broken-checkpoint input 1), which becomes the Session's
// HEAD. A Release of the checkpoint fails its source preparation, because the
// build service's strict analysis rejects the commit's source, which the
// application owns: its notification, whose ID names the Revision its failure
// was committed at, is a failure of HEAD and opens an automatic turn. A
// retried preparation is a recovery that moves the Revision; it fails again
// and is told again under its own ID. Input 2 restores the source.
func TestLiveSourceFailureNotifiesDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout+2*(liveSourceTimeout+mockTurnTimeout))
	defer cancel()
	session := newMockSession(t, ctx, "")
	session.setPreviewAutoDeploy(ctx, false)
	session.send(ctx, mockInput(t, "preview-broken-checkpoint", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	if pushed := lastToolResult(t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitBlockedForTest {
		t.Fatalf("the checkpoint was not pushed as a blocked commit: %+v", pushed)
	}
	checkpoint := session.summary(ctx).Identity.HeadCommit
	live := newLiveDelivery(session)
	var ids []string
	for attempt := 1; attempt <= 2; attempt++ {
		release := live.prepare(ctx, checkpoint, attempt)
		if source := release.PreparedSource; source.Status != releases.Failed || source.Failure == nil || source.Failure.Code != "RELEASE_SOURCE_FAILED" || source.FailureOwner != conversation.DeliveryOwnerApplication {
			t.Fatalf("preparation %d of the checkpoint did not fail for its source: %s %s %+v", attempt, source.Status, source.FailureOwner, source.Failure)
		}
		failedNotice := session.awaitNotification(ctx, fmt.Sprintf("release/%s/source/r%d", checkpoint, release.Revision))
		requireDelivery(t, failedNotice.Message.Notification, conversation.NotificationSourceRelease, conversation.DeliveryFailure, conversation.DeliveryOwnerApplication, "RELEASE_SOURCE_FAILED", dexflow.RemedyAskAgent, checkpoint)
		agent = session.finishedTurnAfter(ctx, failedNotice.Sequence)
		ids = append(ids, failedNotice.Message.Notification.ID)
	}
	if ids[0] == ids[1] {
		t.Fatalf("the retried preparation's failure repeated the ID %s", ids[0])
	}
	session.send(ctx, mockInput(t, "preview-broken-checkpoint", 2).Content)
	agent = session.finishedTurnAfter(ctx, agent.Description.LastSequence)
	requireNoFailure(t, agent.History)
	if pushed := lastToolResult(t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the restored source was not pushed: %+v", pushed)
	}
	t.Logf("FlowID=%s: the source preparations of checkpoint %s failed as %v, each opening an automatic turn", session.id, checkpoint, ids)
}

// TestLiveFailureAutomaticTurnBoundDexIntegration delivers Live failures to
// the Session through the Session Service, exactly as the notify Steps of the
// delivery Flows do. A failure the platform owns and an application failure
// of another commit are recorded as notices and open no turn. Each
// application failure of HEAD opens one automatic turn until the platform
// bound, agent.platformTurns of the server under test (3 by default); the
// next is only recorded and pauses automatic turns once, and the same failure
// delivered again records nothing. The user's next message resets the bound,
// so the next application failure opens a turn again.
func TestLiveFailureAutomaticTurnBoundDexIntegration(t *testing.T) {
	const platformTurns = 3
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	head, turn := session.summary(ctx).Identity.HeadCommit, agent.Description.Turn
	failure := func(index int, owner conversation.DeliveryOwner, commit string) releases.DeliveryFailure {
		deploymentID := fmt.Sprintf("bound-%d", index)
		return releases.DeliveryFailure{ID: fmt.Sprintf("deployment/%s/g1/r%d", deploymentID, index), ProjectID: session.identity.ProjectID, Source: releases.SourceDeployment,
			Owner: owner, Commit: commit, ResourceID: deploymentID, OccurredAt: time.Now().UTC(),
			Failure: dexflow.Failure{Code: deployments.FailureDeploymentForTest, Phase: deployments.PhasePrepareForTest, Message: "The deployment check failed.", Remedy: dexflow.RemedyPublishAgain}}
	}
	report := func(delivered releases.DeliveryFailure) conversation.SequencedMessage {
		t.Helper()
		if err := session.service().ReportDeliveryFailure(ctx, delivered); err != nil {
			t.Fatalf("delivering %s failed (%T)", delivered.ID, err)
		}
		return session.awaitNotification(ctx, delivered.ID)
	}

	platform := report(failure(1, conversation.DeliveryOwnerPlatform, head))
	requireDelivery(t, platform.Message.Notification, conversation.NotificationSourceDeployment, conversation.DeliveryNotice, conversation.DeliveryOwnerPlatform, deployments.FailureDeploymentForTest, dexflow.RemedyPublishAgain, head)
	session.requireNoTurnAfter(ctx, platform.Sequence, turn, "a platform failure")
	stale := strings.Repeat("a", 40)
	other := report(failure(2, conversation.DeliveryOwnerApplication, stale))
	requireDelivery(t, other.Message.Notification, conversation.NotificationSourceDeployment, conversation.DeliveryNotice, conversation.DeliveryOwnerApplication, deployments.FailureDeploymentForTest, dexflow.RemedyPublishAgain, stale)
	session.requireNoTurnAfter(ctx, other.Sequence, turn, "a failure of another commit")

	for index := 3; index < 3+platformTurns; index++ {
		opened := report(failure(index, conversation.DeliveryOwnerApplication, head))
		requireDelivery(t, opened.Message.Notification, conversation.NotificationSourceDeployment, conversation.DeliveryFailure, conversation.DeliveryOwnerApplication, deployments.FailureDeploymentForTest, dexflow.RemedyPublishAgain, head)
		agent = session.finishedTurnAfter(ctx, opened.Sequence)
		turn++
		if agent.Description.Turn != turn {
			t.Fatalf("failure %d left the Session at turn %d, not %d", index, agent.Description.Turn, turn)
		}
	}
	bounded := failure(3+platformTurns, conversation.DeliveryOwnerApplication, head)
	recorded := report(bounded)
	session.requireNoTurnAfter(ctx, recorded.Sequence, turn, "the failure beyond the bound")
	if err := session.service().ReportDeliveryFailure(ctx, bounded); err != nil {
		t.Fatalf("delivering %s again failed (%T)", bounded.ID, err)
	}
	time.Sleep(10 * time.Second)
	_, messages := session.history(ctx)
	paused := slices.DeleteFunc(ofRole(messages, conversation.MessageRoleSystem), func(message conversation.SequencedMessage) bool {
		return message.Message.Content != agentloop.PausedMarkerForTest
	})
	repeated := slices.DeleteFunc(ofRole(messages, conversation.MessageRoleNotification), func(message conversation.SequencedMessage) bool {
		return message.Message.Notification.ID != bounded.ID
	})
	if len(paused) != 1 || len(repeated) != 1 {
		t.Fatalf("the bound recorded %d pause markers and the repeated failure %d entries; want one each", len(paused), len(repeated))
	}

	session.send(ctx, mockInput(t, "inspect-basic", 1).Content)
	agent = session.finishedTurnAfter(ctx, recorded.Sequence)
	turn = agent.Description.Turn
	reset := report(failure(4+platformTurns, conversation.DeliveryOwnerApplication, head))
	if agent = session.finishedTurnAfter(ctx, reset.Sequence); agent.Description.Turn != turn+1 {
		t.Fatalf("the user's message did not reset the bound: turn %d after the failure, not %d", agent.Description.Turn, turn+1)
	}
	t.Logf("FlowID=%s: %d automatic turns, then a pause, and a turn again after the user's message", session.id, platformTurns)
}

// TestSessionPreviewAutoDeploySwitchDexIntegration reads the Preview's
// automatic deployment, on for a new Session, turns it off through the
// Session Service, as PreviewService.UpdatePreviewAutoDeploy does, and pushes a
// note (preview-autodeploy): the turn end publishes the handoff but deploys
// nothing, so the fresh Project never starts its Preview. Turned on again,
// the next turn end deploys nothing either: the handoff was published while
// the switch was off.
func TestSessionPreviewAutoDeploySwitchDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, "")
	if !session.description(ctx).IsPreviewAutoDeployEnabled {
		t.Fatal("a new Session does not deploy its pushes to the Preview")
	}
	mark := markHistory(t, ctx, session.reader, string(session.id))
	session.setPreviewAutoDeploy(ctx, false)
	session.setPreviewAutoDeploy(ctx, false)
	if session.description(ctx).IsPreviewAutoDeployEnabled {
		t.Fatal("the Snapshot reads automatic deployment on after it was turned off")
	}
	note := "switch-" + time.Now().UTC().Format("20060102150405")
	session.send(ctx, mockInput(t, "preview-autodeploy", 1).Content+" "+mockmodel.ArgumentMarkerForTest("note", note))
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	if pushed := lastToolResult(t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the note was not pushed: %+v", pushed)
	}
	session.setPreviewAutoDeploy(ctx, true)
	if !session.description(ctx).IsPreviewAutoDeployEnabled {
		t.Fatal("the Snapshot reads automatic deployment off after it was turned on")
	}
	session.send(ctx, mockInput(t, "inspect-basic", 1).Content)
	agent = session.finishedTurnAfter(ctx, agent.Description.LastSequence)
	requireNoFailure(t, agent.History)
	if steps := completedStepsSince(t, ctx, session.reader, mark); slices.Contains(steps, dex.GetFinalStepType(sessionagent.DeployPreviewHandoff{})) {
		t.Fatal("a turn end deployed the handoff published while automatic deployment was off")
	}
	var missing *dex.FlowNotFoundError
	if _, err := session.previews.Get(ctx, session.identity.ProjectID); !errors.As(err, &missing) {
		t.Fatalf("the Project's Preview started (%T)", err)
	}
	t.Logf("FlowID=%s pushed %s with automatic deployment off; no turn end deployed it", session.id, note)
}
