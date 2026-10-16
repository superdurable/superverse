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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// These scenarios cancel builds and deployments of a throwaway Project's HEAD
// on the actual Worker of either local lane (decision 0027): Dex, PostgreSQL,
// the local build service and the Live runtime are real; only the Session's
// model is the mock. The Release's draft selects an llm connection whose key
// is the HEAD's commit, which the build log prints, so the commit is a
// configured value that a log read must redact; nothing calls the model.

// cancellationTimeout bounds a cancellation's settlement: the provider build's
// stop, or the generation's removal and the lease's release.
const cancellationTimeout = 15 * time.Minute

// A build is cancelled before it reaches the provider and while it polls the
// provider, once across a Worker restart; each settles CANCELLED once with the
// first request's cancellation, and a new build is admitted only after it
// settled. A cancellation racing the artifact commit gives exactly one
// outcome. A finished build refuses a cancellation, the build log of a READY
// build pages with its cursor to the end, redacted and shared within a
// replica, and the read model holds the cancellations.
func TestReleaseBuildCancellationDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Minute)
	defer cancel()
	c := newCancellationScenario(ctx, t)

	// Before the provider: replays and another request change nothing.
	first, request := c.admit(ctx, false, 0), uuid.NewString()
	for _, requestID := range []string{request, request, uuid.NewString()} {
		if err := c.cancelBuild(ctx, first, requestID); err != nil {
			t.Fatalf("the cancellation of build %s was refused (%T)", first, err)
		}
	}
	cancelled := c.awaitBuild(ctx, first, "CANCELLED", func(b releases.ReleaseBuildRevision) bool { return b.Status == releases.Cancelled })
	c.requireCancelled(cancelled, request)
	if release := c.release(ctx); release.ReadyBuildRevisionID != "" || release.Status != releases.Cancelled {
		t.Fatalf("the Release of the cancelled build reads %s with ready build %q", release.Status, release.ReadyBuildRevisionID)
	}
	c.requireBuildRow(ctx, cancelled)

	// While it polls the provider, across a Worker restart: one commit settles it.
	polling := c.admit(ctx, false, 0)
	c.awaitBuild(ctx, polling, "polling the provider", func(b releases.ReleaseBuildRevision) bool { return b.Operation.Phase == releases.PhaseAwaitForTest })
	request = uuid.NewString()
	if err := c.cancelBuild(ctx, polling, request); err != nil {
		t.Fatalf("the cancellation of build %s while it polls was refused (%T)", polling, err)
	}
	admitted := c.live.awaitRelease(ctx, c.head, "the cancellation", func(releases.Snapshot) bool { return true })
	cancelling, err := c.live.releases.ReadBuildRevision(ctx, c.project, c.head, polling)
	if err != nil || cancelling.Cancellation == nil {
		t.Fatalf("build %s records no cancellation (%v)", polling, err)
	}
	// A new build is admitted only once the cancelled one settled.
	next := releases.BuildRevisionRequest{RequestID: uuid.NewString(), BuildRevisionID: "build-" + uuid.NewString(), ExpectedRevision: admitted.Revision,
		ConfigurationRevision: c.configurationRevision, PreviewSkipReason: skipPreview}
	if _, err = c.live.releases.BuildRevision(ctx, c.live.session.account, c.project, c.head, next); err == nil {
		if settled, readErr := c.live.releases.ReadBuildRevision(ctx, c.project, c.head, polling); readErr != nil || settled.Status != releases.Cancelled || !settled.ProviderSettled {
			t.Fatalf("a new build was admitted while build %s was %s (%v)", polling, settled.Status, readErr)
		}
	} else if !failedPrecondition(err) {
		t.Fatalf("a build while %s is cancelling answered %T, want FAILED_PRECONDITION", polling, err)
	}
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, err := c.live.releases.ReadBuildRevision(ctx, c.project, c.head, polling)
		return err
	})
	stopped := c.awaitBuild(ctx, polling, "CANCELLED", func(b releases.ReleaseBuildRevision) bool { return b.Status == releases.Cancelled })
	c.requireCancelled(stopped, request)
	if stopped.Revision != cancelling.Revision+1 {
		t.Fatalf("build %s committed %d revisions after its cancellation, want one", polling, stopped.Revision-cancelling.Revision)
	}
	c.awaitLogComplete(ctx, polling)

	// At the artifact commit: CANCELLED without artifacts, or READY and refused.
	ready := c.admitAfter(ctx, polling, next)
	c.awaitBuild(ctx, ready, "publishing", func(b releases.ReleaseBuildRevision) bool {
		return b.Operation.Phase == releases.PhasePublishForTest || b.Status == releases.Ready || b.Status == releases.Failed
	})
	err = c.cancelBuild(ctx, ready, uuid.NewString())
	raced := c.awaitBuild(ctx, ready, "settled", func(b releases.ReleaseBuildRevision) bool {
		return b.Status == releases.Cancelled || b.Status == releases.Ready || b.Status == releases.Failed
	})
	switch {
	case err == nil && raced.Status == releases.Cancelled && raced.Artifacts == (releases.ArtifactRefs{}) && c.release(ctx).ReadyBuildRevisionID != ready:
		ready = c.admitAfter(ctx, ready, releases.BuildRevisionRequest{})
		c.awaitBuild(ctx, ready, "READY", func(b releases.ReleaseBuildRevision) bool { return b.Status == releases.Ready })
	case errors.Is(err, releases.ErrBuildNotCancellable) && raced.Status == releases.Ready && raced.Cancellation == nil:
	default:
		t.Fatalf("the cancellation at the artifact commit answered %v and left %s with cancellation %+v", err, raced.Status, raced.Cancellation)
	}
	// A READY build, and a cancelled one under a new request, refuse or change nothing.
	if err = c.cancelBuild(ctx, ready, uuid.NewString()); !errors.Is(err, releases.ErrBuildNotCancellable) {
		t.Fatalf("the READY build %s answered %v, want ErrBuildNotCancellable", ready, err)
	}
	if err = c.cancelBuild(ctx, first, uuid.NewString()); err != nil {
		t.Fatalf("the cancelled build %s refused another request (%T)", first, err)
	} else if again := c.awaitBuild(ctx, first, "unchanged", func(releases.ReleaseBuildRevision) bool { return true }); again.Revision != cancelled.Revision {
		t.Fatalf("another request changed the cancelled build %s", first)
	}
	c.requireLogPages(ctx, ready)
	t.Logf("Project %s Release %s: %s cancelled before the provider, %s while polling across a Worker restart; %s READY with its log redacted",
		c.project, c.head, first, polling, ready)
}

// A deployment is cancelled before its lease, and while it polls its
// provider with a current Live; the current Live keeps serving and stays
// READY, the cancelled generation's objects are gone and its lease is
// released, so a new deployment is admitted. A cancellation racing the commit
// of the new Live gives exactly one outcome. A READY deployment refuses one, a
// cancelled one refuses to resume, a published build that starts its
// deployment names that deployment, whose cancellation the Live history
// lists, and the read model holds the cancellations.
func TestDeploymentCancellationDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Minute)
	defer cancel()
	c := newCancellationScenario(ctx, t)
	built := c.awaitBuild(ctx, c.admit(ctx, false, 0), "READY", func(b releases.ReleaseBuildRevision) bool { return b.Status == releases.Ready })
	retire := &retirementScenario{t: t, live: c.live, project: c.project, head: c.head, built: built}
	live := retire.deploy(ctx, 0)
	if err := c.live.deployments.Cancel(ctx, c.live.session.account, c.project, live.DeploymentID, uuid.NewString()); !errors.Is(err, deployments.ErrDeploymentNotCancellable) {
		t.Fatalf("the READY deployment %s answered %v, want ErrDeploymentNotCancellable", live.DeploymentID, err)
	}

	// Before the lease.
	early := c.startDeployment(ctx, retire, live.Lease.Generation)
	request := uuid.NewString()
	if err := c.live.deployments.Cancel(ctx, c.live.session.account, c.project, early, request); err != nil {
		t.Fatalf("the cancellation of deployment %s was refused (%T)", early, err)
	}
	removed := c.awaitCancelled(ctx, early, request)
	t.Logf("deployment %s was cancelled in %s, holding generation %d", early, removed.Cancellation.Phase, removed.Lease.Generation)
	if _, err := c.live.deployments.DeployReadyBuild(ctx, c.live.session.account, uuid.NewString(), retire.start(early, live.Lease.Generation)); !failedPrecondition(err) {
		t.Fatalf("the cancelled deployment %s admitted serving again: %v", early, err)
	}
	if err := c.live.deployments.Cancel(ctx, c.live.session.account, c.project, early, uuid.NewString()); err != nil {
		t.Fatalf("the cancelled deployment %s refused another request (%T)", early, err)
	}
	c.requireLiveServes(ctx, retire, live)

	// While it polls its provider, against the commit of the new Live, across a Worker restart.
	raced := c.startDeployment(ctx, retire, live.Lease.Generation)
	c.awaitDeployment(ctx, raced, "polling its provider", func(d deployments.Snapshot) bool {
		return slices.Contains(deployments.PollingPhasesForTest, d.Providers.Phase) || d.Status == deployments.Ready
	})
	request = uuid.NewString()
	switch err := c.live.deployments.Cancel(ctx, c.live.session.account, c.project, raced, request); {
	case err == nil:
		dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
			_, err := c.live.deployments.ReadSnapshotForTest(ctx, c.project, raced)
			return err
		})
		cancelled := c.awaitCancelled(ctx, raced, request)
		t.Logf("deployment %s was cancelled in %s and settled CANCELLED across a Worker restart", raced, cancelled.Cancellation.Phase)
		if generationPresent(ctx, t, cancelled) {
			t.Fatalf("the cancelled generation %d of deployment %s still runs", cancelled.Lease.Generation, raced)
		}
		c.requireLiveServes(ctx, retire, live)
	case errors.Is(err, deployments.ErrDeploymentNotCancellable):
		// The commit came first: the deployment goes live and every cancellation is refused.
		live = retire.awaitStatus(ctx, raced, deployments.Ready)
		if live.Cancellation != nil {
			t.Fatalf("the deployment %s that went live records a cancellation", raced)
		}
		t.Logf("deployment %s committed the new Live first and refused the cancellation", raced)
	default:
		t.Fatalf("the cancellation of deployment %s answered %T", raced, err)
	}

	// While it applies the generation: a preempted ApplyApplication round may still create objects, which the
	// cleanup removes until that round ended.
	applying := c.startDeployment(ctx, retire, live.Lease.Generation)
	c.awaitPhase(ctx, applying, deployments.PollingPhasesForTest[1:])
	request = uuid.NewString()
	switch err := c.live.deployments.Cancel(ctx, c.live.session.account, c.project, applying, request); {
	case err == nil:
		cancelled := c.awaitCancelled(ctx, applying, request)
		if generationPresent(ctx, t, cancelled) {
			t.Fatalf("the cancelled generation %d of deployment %s still runs", cancelled.Lease.Generation, applying)
		}
		c.requireLiveServes(ctx, retire, live)
		t.Logf("deployment %s was cancelled in %s (preempted: %t) and settled %s after its request", applying, cancelled.Cancellation.Phase,
			cancelled.Cancellation.Preempted, cancelled.UpdatedAt.Sub(cancelled.Cancellation.RequestedAt).Round(time.Second))
	case errors.Is(err, deployments.ErrDeploymentNotCancellable):
		live = retire.awaitStatus(ctx, applying, deployments.Ready)
		t.Logf("deployment %s committed the new Live first and refused the cancellation", applying)
	default:
		t.Fatalf("the cancellation of deployment %s answered %T", applying, err)
	}

	// The build's window ends at its artifact commit; its deployment is cancelled instead. The frozen
	// copy is built again from the next-publish layer against the Live that now serves, as publishing does.
	c.configurationRevision = c.live.publishCopy(ctx, c.head)
	automatic := c.admit(ctx, true, live.Lease.Generation)
	published := c.awaitBuild(ctx, automatic, "READY with its deployment", func(b releases.ReleaseBuildRevision) bool {
		return b.Status == releases.Ready && b.AutoDeploymentID != "" || b.AutoDeployFailure != nil || b.Status == releases.Failed
	})
	err := c.cancelBuild(ctx, automatic, uuid.NewString())
	if _, isDeploying := errors.AsType[*releases.BuildDeployingError](err); published.AutoDeploymentID == "" || !isDeploying || !strings.Contains(err.Error(), published.AutoDeploymentID) {
		t.Fatalf("the published build %s answered %v, want BuildDeployingError naming %q", automatic, err, published.AutoDeploymentID)
	}
	request = uuid.NewString()
	switch err := c.live.deployments.Cancel(ctx, c.live.session.account, c.project, published.AutoDeploymentID, request); {
	case err == nil:
		cancelled := c.awaitCancelled(ctx, published.AutoDeploymentID, request)
		c.requireLiveServes(ctx, retire, live)
		c.requireLiveEvent(ctx, "build:"+automatic)
		t.Logf("the automatic deployment %s was cancelled in %s", published.AutoDeploymentID, cancelled.Cancellation.Phase)
	case errors.Is(err, deployments.ErrDeploymentNotCancellable):
		retire.awaitStatus(ctx, published.AutoDeploymentID, deployments.Ready)
		t.Logf("the automatic deployment %s went live before its cancellation", published.AutoDeploymentID)
	default:
		t.Fatalf("the cancellation of the automatic deployment %s answered %T", published.AutoDeploymentID, err)
	}
	c.requireLiveEvent(ctx, "deploy:"+early)
	c.requireDeploymentRow(ctx, removed)
	t.Logf("Project %s: deployment %s cancelled before its lease, %s raced the commit, %s started by build %s; Live %s kept serving",
		c.project, early, raced, published.AutoDeploymentID, automatic, live.DeploymentID)
}

// cancellationScenario is a throwaway Project whose HEAD's Release is
// prepared, with the frozen copy of its configuration at configurationRevision.
type cancellationScenario struct {
	t                     *testing.T
	live                  *liveDelivery
	project, head         string
	configurationRevision uint64
	// logs reads build logs as the server does, two reads a second, with the
	// lane's redactor.
	logs     *deployments.PublishingFlowService
	redactor infra.Redactor
}

func newCancellationScenario(ctx context.Context, t *testing.T) *cancellationScenario {
	t.Helper()
	session := newMockSession(t, ctx, "")
	live := newLiveDelivery(session)
	head := session.identity.HeadCommit
	if release := live.prepare(ctx, head, 1); release.PreparedSource.Status != releases.Ready {
		t.Fatalf("the source of the Release of %s is %s", head, release.PreparedSource.Status)
	}
	// The llm connection's key is the commit, which the build log prints.
	list, _ := session.configurationRevision(ctx, live.configuration, live.target(head))
	session.seeded = map[string]string{}
	for _, connection := range list.Connections {
		if connection.ConnectorID == llmConnector {
			settings := map[string]json.RawMessage{"provider": comm.Must(json.Marshal(openAIProvider)), "model": comm.Must(json.Marshal(openAIPreviewModel))}
			session.seeded[connection.ConnectionName] = session.createConnection(ctx, live.configuration, connection.ConnectionName, connection.ConnectorID, connection.ModulePath,
				connection.ModuleVersion, connection.AuthMethodID, settings, map[string]json.RawMessage{"api_key": comm.Must(json.Marshal(head))})
		}
	}
	if len(session.seeded) == 0 {
		t.Fatal("the source declares no llm connection whose model the build log could hold")
	}
	session.configureConnections(ctx, live.configuration, live.target(head))
	redactor, ok := laneStorage(t).(infra.Redactor)
	if !ok {
		t.Fatal("the lane's runtime redacts no internal name")
	}
	logs := deployments.NewPublishingFlowService(live.projects, live.deployments, live.configuration, nil, deployments.BuildLogReads{Redactor: redactor, PerSecond: 2})
	return &cancellationScenario{t: t, live: live, project: session.identity.ProjectID, head: head, configurationRevision: live.publishCopy(ctx, head), logs: logs, redactor: redactor}
}

// admit admits a build of HEAD at the Release's current Revision, deploying
// automatically when autoDeploy is set over Live generation expected, and
// returns its ID at once.
func (c *cancellationScenario) admit(ctx context.Context, autoDeploy bool, expected uint64) string {
	c.t.Helper()
	release := c.release(ctx)
	request := releases.BuildRevisionRequest{RequestID: uuid.NewString(), BuildRevisionID: "build-" + uuid.NewString(), ExpectedRevision: release.Revision,
		ConfigurationRevision: c.configurationRevision, AutoDeploy: autoDeploy, ExpectedLiveRevision: expected, PreviewSkipReason: skipPreview}
	if _, err := c.live.releases.BuildRevision(ctx, c.live.session.account, c.project, c.head, request); err != nil {
		c.t.Fatalf("the build of %s was not admitted: %v", c.head, err)
	}
	return request.BuildRevisionID
}

// admitAfter admits a build once the cancelled build previous settled: the
// request a racing admission already made, or a new one.
func (c *cancellationScenario) admitAfter(ctx context.Context, previous string, made releases.BuildRevisionRequest) string {
	c.t.Helper()
	if made.BuildRevisionID != "" {
		if _, err := c.live.releases.ReadBuildRevision(ctx, c.project, c.head, made.BuildRevisionID); err == nil {
			return made.BuildRevisionID
		}
	}
	c.awaitBuild(ctx, previous, "settled", func(b releases.ReleaseBuildRevision) bool { return b.Status == releases.Cancelled })
	return c.admit(ctx, false, 0)
}

func (c *cancellationScenario) release(ctx context.Context) releases.Snapshot {
	c.t.Helper()
	return c.live.awaitRelease(ctx, c.head, "the read", func(releases.Snapshot) bool { return true })
}

func (c *cancellationScenario) cancelBuild(ctx context.Context, buildRevisionID, requestID string) error {
	return c.live.releases.CancelBuildRevision(ctx, c.live.session.account, c.project, c.head, releases.BuildRevisionCancellation{BuildRevisionID: buildRevisionID, RequestID: requestID})
}

// awaitBuild waits until the build satisfies done and returns it.
func (c *cancellationScenario) awaitBuild(ctx context.Context, buildRevisionID, what string, done func(releases.ReleaseBuildRevision) bool) releases.ReleaseBuildRevision {
	c.t.Helper()
	wait, cancel := context.WithTimeout(ctx, liveBuildTimeout+cancellationTimeout)
	defer cancel()
	var b releases.ReleaseBuildRevision
	c.live.session.await(wait, "build "+buildRevisionID+" "+what, func() bool {
		var err error
		b, err = c.live.releases.ReadBuildRevision(wait, c.project, c.head, buildRevisionID)
		return err == nil && done(b)
	})
	return b
}

// requireCancelled requires a build that settled CANCELLED, confirmed, without
// artifacts, with the cancellation of requestID by the Session's owner.
func (c *cancellationScenario) requireCancelled(b releases.ReleaseBuildRevision, requestID string) {
	c.t.Helper()
	cancellation := b.Cancellation
	if cancellation == nil || cancellation.RequestID != requestID || cancellation.ActorAccountID != c.live.session.account || cancellation.SettledAt.Before(cancellation.RequestedAt) {
		c.t.Fatalf("build %s records the cancellation %+v, want request %s settled", b.BuildRevisionID, cancellation, requestID)
	}
	if !b.ProviderSettled || b.Failure != nil || b.Artifacts != (releases.ArtifactRefs{}) {
		c.t.Fatalf("the cancelled build %s reads settled %t, failure %+v and artifacts %+v", b.BuildRevisionID, b.ProviderSettled, b.Failure, b.Artifacts)
	}
}

// awaitLogComplete waits until the cancelled build's log reads complete, which
// requires that its provider build no longer runs.
func (c *cancellationScenario) awaitLogComplete(ctx context.Context, buildRevisionID string) {
	c.t.Helper()
	project := c.projectState(ctx)
	read := build.LogRead{MaximumBytes: deployments.MaximumBuildLogPageBytes}
	c.live.session.await(ctx, "the complete log of build "+buildRevisionID, func() bool {
		page, err := c.logs.ReadBuildLog(ctx, project, c.head, buildRevisionID, read)
		if err != nil {
			c.t.Fatalf("read the log of build %s: %v", buildRevisionID, err)
		}
		read.Cursor = page.Next
		return page.Complete
	})
}

// requireLogPages pages through a READY build's log from its start to its end:
// each page is at most 64 KiB, the commit the log prints is redacted, and the
// viewers of a page share it. A read beyond the replica's budget is deferred,
// not refused.
func (c *cancellationScenario) requireLogPages(ctx context.Context, buildRevisionID string) {
	c.t.Helper()
	project := c.projectState(ctx)
	read := build.LogRead{MaximumBytes: deployments.MaximumBuildLogPageBytes}
	var text strings.Builder
	pages := 0
	for {
		page, err := c.logs.ReadBuildLog(ctx, project, c.head, buildRevisionID, read)
		if err != nil {
			c.t.Fatalf("read the log of build %s after %q: %v", buildRevisionID, read.Cursor, err)
		}
		if len(page.Text) > deployments.MaximumBuildLogPageBytes || page.RetryAfter <= 0 {
			c.t.Fatalf("a log page holds %d bytes and asks to read again after %s", len(page.Text), page.RetryAfter)
		}
		text.WriteString(page.Text)
		if page.Text != "" {
			pages++
		}
		if page.Complete {
			break
		}
		if page.Next == read.Cursor {
			time.Sleep(page.RetryAfter)
		}
		read.Cursor = page.Next
	}
	if pages == 0 || !strings.Contains(text.String(), "[REDACTED]") || strings.Contains(text.String(), c.head) {
		c.t.Fatalf("the log of build %s reads %d pages, redacted: %t, with the commit: %t", buildRevisionID, pages,
			strings.Contains(text.String(), "[REDACTED]"), strings.Contains(text.String(), c.head))
	}
	// A second viewer of the first page shares the read; a replica without budget defers it.
	time.Sleep(2 * time.Second)
	start := build.LogRead{MaximumBytes: deployments.MaximumBuildLogPageBytes}
	first, firstErr := c.logs.ReadBuildLog(ctx, project, c.head, buildRevisionID, start)
	shared, sharedErr := c.logs.ReadBuildLog(ctx, project, c.head, buildRevisionID, start)
	if firstErr != nil || sharedErr != nil || shared.Text != first.Text || shared.Next != first.Next || shared.RetryAfter > first.RetryAfter {
		c.t.Fatalf("the second viewer of the first page read another page (%v, %v)", firstErr, sharedErr)
	}
	scarce := deployments.NewPublishingFlowService(c.live.projects, c.live.deployments, c.live.configuration, nil, deployments.BuildLogReads{Redactor: c.redactor, PerSecond: 0.1})
	tail := build.LogRead{Tail: true, MaximumBytes: 1 << 10}
	if _, err := scarce.ReadBuildLog(ctx, project, c.head, buildRevisionID, tail); err != nil {
		c.t.Fatalf("read the end of the log of build %s (%T)", buildRevisionID, err)
	}
	if deferred, err := scarce.ReadBuildLog(ctx, project, c.head, buildRevisionID, start); err != nil || deferred.Text != "" || deferred.Next != "" || deferred.RetryAfter < 2*time.Second {
		c.t.Fatalf("a read beyond the budget answered %q after %s (%v), want an empty page deferred", deferred.Next, deferred.RetryAfter, err)
	}
	c.t.Logf("build %s: %d redacted log pages, %d bytes", buildRevisionID, pages, text.Len())
}

// projectState is the Project as the API authorized it.
func (c *cancellationScenario) projectState(ctx context.Context) projects.Project {
	c.t.Helper()
	project, err := c.live.projects.Read(ctx, c.project)
	if err != nil {
		c.t.Fatalf("read Project %s (%T)", c.project, err)
	}
	return project
}

// requireBuildRow requires the read model's row of the cancelled build to
// hold its status and cancellation.
func (c *cancellationScenario) requireBuildRow(ctx context.Context, b releases.ReleaseBuildRevision) {
	c.t.Helper()
	postgrestest.AwaitRowForTest(ctx, c.t, postgrestest.OpenForTest(c.t), "release_build_revisions", func() map[string]any {
		return map[string]any{"build_revision_id": b.BuildRevisionID, "status": b.Status, "provider_settled": b.ProviderSettled, "cancellation": b.Cancellation}
	}, "project_id = $1 AND build_revision_id = $2", c.project, b.BuildRevisionID)
}

// startDeployment starts a deployment of the scenario's build that replaces
// Live generation expected and returns its ID at once.
func (c *cancellationScenario) startDeployment(ctx context.Context, retire *retirementScenario, expected uint64) string {
	c.t.Helper()
	deploymentID, err := c.live.deployments.DeployReadyBuild(ctx, c.live.session.account, uuid.NewString(), retire.start(uuid.NewString(), expected))
	if err != nil {
		c.t.Fatalf("the deployment replacing generation %d was not admitted (%T)", expected, err)
	}
	return deploymentID
}

func (c *cancellationScenario) awaitDeployment(ctx context.Context, deploymentID, what string, done func(deployments.Snapshot) bool) deployments.Snapshot {
	c.t.Helper()
	wait, cancel := context.WithTimeout(ctx, liveDeploymentTimeout)
	defer cancel()
	var d deployments.Snapshot
	c.live.session.await(wait, "deployment "+deploymentID+" "+what, func() bool {
		var err error
		d, err = c.live.deployments.ReadSnapshotForTest(wait, c.project, deploymentID)
		return err == nil && done(d)
	})
	return d
}

// awaitPhase waits, reading every quarter second, until the deployment runs
// one of phases or has passed them.
func (c *cancellationScenario) awaitPhase(ctx context.Context, deploymentID string, phases []string) {
	c.t.Helper()
	wait, cancel := context.WithTimeout(ctx, liveDeploymentTimeout)
	defer cancel()
	for {
		d, err := c.live.deployments.ReadSnapshotForTest(wait, c.project, deploymentID)
		if err == nil && (slices.Contains(phases, d.Providers.Phase) || d.Status == deployments.Ready || d.Status == deployments.Failed) {
			return
		}
		select {
		case <-wait.Done():
			c.t.Fatalf("deployment %s reached none of %v", deploymentID, phases)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// awaitCancelled waits until the deployment is CANCELLED with the
// cancellation of requestID and requires its grant revoked and its lease
// released.
func (c *cancellationScenario) awaitCancelled(ctx context.Context, deploymentID, requestID string) deployments.Snapshot {
	c.t.Helper()
	d := c.awaitDeployment(ctx, deploymentID, "CANCELLED", func(d deployments.Snapshot) bool {
		if d.Status == deployments.Failed {
			c.t.Fatalf("the cancelled deployment %s failed in %s: %+v", deploymentID, d.Providers.Phase, d.Failure)
		}
		return d.Status == deployments.Cancelled
	})
	if d.Cancellation == nil || d.Cancellation.RequestID != requestID || d.Cancellation.ActorAccountID != c.live.session.account || d.CleanupRequested || d.Failure != nil {
		c.t.Fatalf("the cancelled deployment %s records %+v (cleanup %t, failure %+v)", deploymentID, d.Cancellation, d.CleanupRequested, d.Failure)
	}
	if d.Lease.Generation != 0 && !d.Grant.Revoked {
		c.t.Fatalf("the cancelled generation %d of deployment %s keeps its grant", d.Lease.Generation, deploymentID)
	}
	project := c.projectState(ctx)
	if project.ActiveDeployment != nil && project.ActiveDeployment.DeploymentID == deploymentID {
		c.t.Fatalf("the cancelled deployment %s still holds the Project's lease", deploymentID)
	}
	return d
}

// requireLiveServes requires the Live deployment to be the Project's current
// Live, READY and not retiring, with its generation running and holding the
// Live Worker endpoint.
func (c *cancellationScenario) requireLiveServes(ctx context.Context, retire *retirementScenario, live deployments.Snapshot) {
	c.t.Helper()
	current, err := c.live.deployments.ReadSnapshotForTest(ctx, c.project, live.DeploymentID)
	project := c.projectState(ctx)
	if err != nil || current.Status != deployments.Ready || current.Grant.Revoked || project.CurrentLive == nil || project.CurrentLive.DeploymentID != live.DeploymentID {
		c.t.Fatalf("Live %s reads %s with grant %+v (%v); the Project's Live is %+v", live.DeploymentID, current.Status, current.Grant, err, project.CurrentLive)
	}
	retire.requireGeneration(ctx, live, true)
}

// requireLiveEvent requires the Live history's entry eventID to be a
// cancelled publish.
func (c *cancellationScenario) requireLiveEvent(ctx context.Context, eventID string) {
	c.t.Helper()
	c.live.session.await(ctx, "the Live event "+eventID, func() bool {
		events, _, err := deployments.ListLiveEvents(ctx, c.live.deployments.Rows, c.project, nil, 50)
		if err != nil {
			c.t.Fatalf("list the Live events (%T)", err)
		}
		index := slices.IndexFunc(events, func(e deployments.LiveEvent) bool { return e.ID == eventID })
		return index >= 0 && events[index].Cancelled && events[index].Outcome == deployments.EventCancelled
	})
}

// requireDeploymentRow requires the read model's row of the cancelled
// deployment to hold its status and cancellation.
func (c *cancellationScenario) requireDeploymentRow(ctx context.Context, d deployments.Snapshot) {
	c.t.Helper()
	postgrestest.AwaitRowForTest(ctx, c.t, postgrestest.OpenForTest(c.t), "deployments", func() map[string]any {
		return map[string]any{"deployment_id": d.DeploymentID, "deployment_status": deployments.Cancelled, "cancellation": d.Cancellation}
	}, "project_id = $1 AND deployment_id = $2", c.project, d.DeploymentID)
}
