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
	"slices"
	"testing"

	"github.com/google/uuid"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/studio/confighost/configitems"
)

// TestReleaseAdmissionDexIntegration publishes the HEAD of one throwaway READY
// Project through the Release client's admission as StartReleaseBuild does,
// with the frozen copy DeriveLiveConfiguration builds from Live's next-publish
// layer, and checks each publishing rule on the real path:
//   - with no Preview start and no reason the build is refused, and with a
//     reason it is SKIPPED, which the read model's preview_validation column
//     holds;
//   - a READY applying Preview start makes the copy stale (Preview's applied
//     version moved); built again, a build is VALIDATED by that start, and is
//     deployed;
//   - the exact request of that build, retried after Live changed, returns its
//     original acceptance;
//   - the copy is stale after the new Live and after a layer write; built
//     again, a copy revision other than the requested one is REVISION_CHANGED,
//     and a configuration-only build of the Release Live serves is LIVE_CODE
//     and deploys;
//   - rolling back to the VALIDATED build restores its configuration on Live
//     and leaves the layer alone, so its connection waits (differs from Live).
//
// Dex, the Sandbox, the Git host, the local build service, the Live runtime,
// MinIO and PostgreSQL are real; only the model is the mock.
func TestReleaseAdmissionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+liveSourceTimeout+3*liveBuildTimeout+3*liveDeploymentTimeout)
	defer cancel()
	session, _ := newAppliedPreviewSession(t, ctx)
	project, account, head := session.identity.ProjectID, session.account, session.identity.HeadCommit
	live := newLiveDelivery(session.mockSession)
	if release := live.prepare(ctx, head, 1); release.PreparedSource.Status != releases.Ready {
		t.Fatalf("the source of the Release of %s is %s", head, release.PreparedSource.Status)
	}
	if version := live.awaitRelease(ctx, head, "the read", func(releases.Snapshot) bool { return true }).PreparedSource.Version; version.Subject == "" {
		t.Fatalf("the Release of %s recorded no version", head)
	}
	connectionA := session.summaryConnection
	connectionB, connectionC := session.newSummaryConnection(ctx), session.newSummaryConnection(ctx)
	session.configureConnections(ctx, live.configuration, live.target(head))
	copied := live.publishCopy(ctx, head)

	// No Preview start of the commit and no reason: refused.
	if state, err := session.previews.Get(ctx, project); err != nil || slices.ContainsFunc(state.RecentStarts, func(r preview.StartRecord) bool { return r.Commit == head && r.Outcome == preview.StartReady }) {
		t.Fatalf("the Preview already ran %s, or was not read (%v)", head, err)
	}
	if _, err := live.admit(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: copied}); !errors.Is(err, releases.ErrPreviewValidationRequired) {
		t.Fatalf("a build without a Preview start or a reason was answered %v", err)
	}

	// A written reason: SKIPPED, in the Flow and in the read model.
	reason := "Hotfix verified by hand"
	skipped := live.awaitBuild(ctx, head, live.mustAdmit(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: copied, PreviewSkipReason: reason}).BuildRevisionID)
	requireValidation(t, skipped, releases.PreviewValidation{Kind: releases.PreviewSkipped, SkipReason: reason, ActorAccountID: account})
	session.await(ctx, "the SKIPPED build's preview_validation column", func() bool {
		projection, err := live.releases.ReadProjection(ctx, project, head)
		return err == nil && slices.ContainsFunc(projection.Builds, func(b releases.ReleaseBuildRevision) bool {
			return b.BuildRevisionID == skipped.BuildRevisionID && b.PreviewValidation != nil && *b.PreviewValidation == *skipped.PreviewValidation
		})
	})

	// A READY applying Preview start of the commit: the copy is stale, then VALIDATED by that start.
	start, started := session.start(ctx, true, session.headRevision(ctx))
	record := startRecord(t, started, start.ID)
	if record.Outcome != preview.StartReady || record.Commit != head {
		t.Fatalf("the Preview start of %s ended %s on %s", head, record.Outcome, record.Commit)
	}
	if _, err := live.admit(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: copied}); !errors.Is(err, releases.ErrConfigurationStale) {
		t.Fatalf("a build of a copy built before the Preview applied was answered %v", err)
	}
	copied = live.publishCopy(ctx, head)
	validatedRequest := live.mustAdmit(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: copied})
	validated := live.awaitBuild(ctx, head, validatedRequest.BuildRevisionID)
	requireValidation(t, validated, releases.PreviewValidation{Kind: releases.PreviewValidated, StartCommandID: start.ID, StartedAt: record.EndedAt, ActorAccountID: account})
	first := live.deployLive(ctx, head, validated)

	// The exact request again, after Live changed: its original acceptance.
	if id, err := live.releases.BuildRevision(ctx, account, project, head, validatedRequest); err != nil || id != validated.BuildRevisionID {
		t.Fatalf("the retried request of %s was answered %s (%v)", validated.BuildRevisionID, id, err)
	}
	// The copy was built before this Live: STALE until it is built again.
	if _, err := live.admit(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: copied}); !errors.Is(err, releases.ErrConfigurationStale) {
		t.Fatalf("a build of a copy built before the new Live was answered %v", err)
	}
	copied = live.publishCopy(ctx, head)
	// A layer write: STALE too.
	session.seeded[summaryModelConnection] = connectionB
	session.configureConnections(ctx, live.configuration, live.target(head))
	if _, err := live.admit(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: copied}); !errors.Is(err, releases.ErrConfigurationStale) {
		t.Fatalf("a build of a copy built before a layer write was answered %v", err)
	}
	changed := live.publishCopy(ctx, head)
	if _, err := live.admit(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: changed - 1}); !isStaleRejection(err) {
		t.Fatalf("a build of copy revision %d under revision %d was answered %v", changed-1, changed, err)
	}
	// A configuration-only build of the Release Live serves: LIVE_CODE.
	liveCode := live.awaitBuild(ctx, head, live.mustAdmit(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: changed}).BuildRevisionID)
	requireValidation(t, liveCode, releases.PreviewValidation{Kind: releases.PreviewLiveCode, ActorAccountID: account})
	if liveCode.Configuration.Object == validated.Configuration.Object {
		t.Fatalf("the configuration-only build froze the same configuration %s", liveCode.Configuration.Object.Version)
	}
	live.deployLive(ctx, head, liveCode)

	// The person changes the layer, then Live rolls back: the layer stays, and waits.
	session.seeded[summaryModelConnection] = connectionC
	session.configureConnections(ctx, live.configuration, live.target(head))
	rolledBack := live.deployLive(ctx, head, validated)
	if rolledBack.Inputs.Configuration.Object != validated.Configuration.Object || rolledBack.DeploymentID == first.DeploymentID {
		t.Fatalf("the rollback deployment %s pins %s, not the VALIDATED build's %s", rolledBack.DeploymentID, rolledBack.Inputs.Configuration.Object.Version, validated.Configuration.Object.Version)
	}
	_, items, err := live.configuration.Get(ctx, account, &pb.ProjectConfigurationTarget{ProjectId: project, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, ReleaseId: head, NextPublish: true})
	if err != nil {
		t.Fatalf("Live's next publish was not read after the rollback: %v", err)
	}
	summary := slices.IndexFunc(items, func(item configitems.Item) bool {
		return item.Kind == configitems.KindConnection && item.ConnectionName == summaryModelConnection
	})
	if summary < 0 || items[summary].State != configitems.Own || !items[summary].DiffersFromLive {
		t.Fatalf("after the rollback the layer's summary connection is not Live's own and waiting: %+v", items)
	}
	t.Logf("FlowID=%s Release %s: SKIPPED %s, VALIDATED %s by %s, LIVE_CODE %s; connections %s, %s, %s; rollback %s restored %s",
		session.id, head, skipped.BuildRevisionID, validated.BuildRevisionID, start.ID, liveCode.BuildRevisionID, connectionA, connectionB, connectionC,
		rolledBack.DeploymentID, validated.Configuration.Object.Version)
}

// requireValidation fails unless build recorded want.
func requireValidation(t *testing.T, build releases.ReleaseBuildRevision, want releases.PreviewValidation) {
	t.Helper()
	if build.PreviewValidation == nil || build.PreviewValidation.Kind != want.Kind || build.PreviewValidation.StartCommandID != want.StartCommandID ||
		!build.PreviewValidation.StartedAt.Equal(want.StartedAt) || build.PreviewValidation.SkipReason != want.SkipReason || build.PreviewValidation.ActorAccountID != want.ActorAccountID {
		t.Fatalf("build %s recorded the validation %+v, not %+v", build.BuildRevisionID, build.PreviewValidation, want)
	}
}

// isStaleRejection reports whether err is a revision fence that no longer
// holds, which the API answers as REVISION_CHANGED.
func isStaleRejection(err error) bool {
	rejected, isRejected := errors.AsType[*dexflow.Rejection](err)
	return isRejected && rejected.Stale
}

// newSummaryConnection creates another TeamSpace connection for the
// template's summary model, as the TeamSpace's Connections page does.
func (s *appliedPreviewSession) newSummaryConnection(ctx context.Context) string {
	s.t.Helper()
	list, _ := s.configurationRevision(ctx, s.configuration, &pb.FlowApiTarget{ProjectId: s.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW})
	for _, declared := range list.Connections {
		if declared.ConnectionName == summaryModelConnection {
			settings := map[string]json.RawMessage{"provider": comm.Must(json.Marshal(openAIProvider)), "model": comm.Must(json.Marshal(openAIPreviewModel))}
			credentials := map[string]json.RawMessage{"api_key": comm.Must(json.Marshal(openAIKey(s.t)))}
			return s.createConnection(ctx, s.configuration, declared.ConnectionName, declared.ConnectorID, declared.ModulePath, declared.ModuleVersion, declared.AuthMethodID, settings, credentials)
		}
	}
	s.t.Fatalf("the template declares no %s connection", summaryModelConnection)
	return ""
}

// admit admits a build of the Release of commit as StartReleaseBuild does, at
// the Release's current Revision with new identities, and returns the request
// it sent, which a retry repeats.
func (l *liveDelivery) admit(ctx context.Context, commit string, request releases.BuildRevisionRequest) (releases.BuildRevisionRequest, error) {
	l.session.t.Helper()
	release := l.awaitRelease(ctx, commit, "the read", func(releases.Snapshot) bool { return true })
	request.RequestID, request.BuildRevisionID, request.ExpectedRevision = uuid.NewString(), "build-"+uuid.NewString(), release.Revision
	_, err := l.releases.BuildRevision(ctx, l.session.account, l.session.identity.ProjectID, commit, request)
	return request, err
}

// mustAdmit is admit that fails the test on a refusal.
func (l *liveDelivery) mustAdmit(ctx context.Context, commit string, request releases.BuildRevisionRequest) releases.BuildRevisionRequest {
	l.session.t.Helper()
	admitted, err := l.admit(ctx, commit, request)
	if err != nil {
		l.session.t.Fatalf("the build of %s was not admitted: %v", commit, err)
	}
	return admitted
}

// awaitBuild waits until the build of the Release of commit is READY.
func (l *liveDelivery) awaitBuild(ctx context.Context, commit, buildRevisionID string) releases.ReleaseBuildRevision {
	l.session.t.Helper()
	var build releases.ReleaseBuildRevision
	l.session.await(ctx, "build "+buildRevisionID, func() bool {
		var err error
		build, err = l.releases.ReadBuildRevision(ctx, l.session.identity.ProjectID, commit, buildRevisionID)
		if err == nil && build.Status == releases.Failed {
			l.session.t.Fatalf("build %s failed: %+v", buildRevisionID, build.Failure)
		}
		return err == nil && build.Status == releases.Ready
	})
	return build
}

// deployLive deploys a READY build of the Release of commit to Live as
// StartDeployment does, over the Live revision it reads, and returns the
// READY deployment.
func (l *liveDelivery) deployLive(ctx context.Context, commit string, built releases.ReleaseBuildRevision) deployments.Snapshot {
	l.session.t.Helper()
	project, err := l.projects.Read(ctx, l.session.identity.ProjectID)
	if err != nil {
		l.session.t.Fatalf("the Project was not read (%T)", err)
	}
	var expected uint64
	if project.CurrentLive != nil {
		expected = project.CurrentLive.Revision
	}
	start := deployments.Start{ProjectID: project.ProjectID, DeploymentID: uuid.NewString(), ReleaseID: commit, BuildRevisionID: built.BuildRevisionID,
		ConfigurationRevision: built.Configuration.ConfigurationRevision, ExpectedLiveRevision: expected}
	deploymentID, err := l.deployments.DeployReadyBuild(ctx, l.session.account, uuid.NewString(), start)
	if err != nil {
		l.session.t.Fatalf("the deployment of %s was not admitted: %v", built.BuildRevisionID, err)
	}
	var deployment deployments.Snapshot
	l.session.await(ctx, "deployment "+deploymentID, func() bool {
		deployment, err = l.deployments.ReadSnapshotForTest(ctx, project.ProjectID, deploymentID)
		if err == nil && deployment.Status == deployments.Failed {
			l.session.t.Fatalf("deployment %s failed: %+v", deploymentID, deployment.Failure)
		}
		return err == nil && deployment.Status == deployments.Ready
	})
	return deployment
}
