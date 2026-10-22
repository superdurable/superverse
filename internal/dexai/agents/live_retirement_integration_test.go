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
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/kubetest"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// These scenarios deploy the template's READY build of a throwaway Project's
// HEAD to Live several times on the actual Worker of either local lane, which
// retires each replaced generation after the server's live.retirementDrain
// (30 seconds in the local profiles, decision 0025). Dex, the local build
// service and the Live runtime are real; only the Session's model is the mock.

// retirementTimeout bounds a replaced generation's retirement: its drain and
// the removal of its objects.
const retirementTimeout = 10 * time.Minute

// The first generation is RETIRED once the second commits and the drain
// passed: its grant is revoked, its objects are gone and the Live Worker
// endpoint selects the second. A rollback deploys the same build again under
// a new deployment ID as a third generation, which retires the second, and
// the retired deployment ID admits no new deployment. A Project deleted while
// the second drains ends it REMOVED through Project cleanup.
func TestLiveRetirementDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Minute)
	defer cancel()
	retire := newRetirementScenario(ctx, t)
	first := retire.deploy(ctx, 0)
	second := retire.deploy(ctx, first.Lease.Generation)
	if second.ReplacedLive != (deployments.ReplacedGeneration{DeploymentID: first.DeploymentID, Generation: first.Lease.Generation}) {
		t.Fatalf("deployment %s replaced %+v, not deployment %s", second.DeploymentID, second.ReplacedLive, first.DeploymentID)
	}
	retired := retire.awaitStatus(ctx, first.DeploymentID, deployments.Retired)
	if !retired.Grant.Revoked || retired.Retirement.ReplacedBy.DeploymentID != second.DeploymentID || retired.Retirement.RetiredAt.Before(retired.Retirement.RequestedAt.Add(30*time.Second)) {
		t.Fatalf("the retired deployment %s reads grant %+v and retirement %+v", first.DeploymentID, retired.Grant, retired.Retirement)
	}
	retire.requireGeneration(ctx, first, false)
	retire.requireGeneration(ctx, second, true)

	// A rollback is a new deployment of an earlier build, never the retired one.
	if _, err := retire.live.deployments.DeployReadyBuild(ctx, retire.live.session.account, uuid.NewString(), retire.start(first.DeploymentID, first.ExpectedLiveRevision)); !failedPrecondition(err) {
		t.Fatalf("the retired deployment %s admitted a new deployment: %v", first.DeploymentID, err)
	}
	third := retire.deploy(ctx, second.Lease.Generation)
	if third.Lease.Generation <= second.Lease.Generation {
		t.Fatalf("the rollback took generation %d after %d", third.Lease.Generation, second.Lease.Generation)
	}
	draining := retire.awaitStatus(ctx, second.DeploymentID, deployments.Retiring)
	projecttest.DeleteForTest(ctx, t, retire.live.session.projects, retire.project)
	removed, err := retire.live.deployments.ReadSnapshotForTest(ctx, retire.project, second.DeploymentID)
	if err != nil || removed.Status != deployments.Removed || !removed.CleanupRequested || removed.Retirement.RetiredAt != (time.Time{}) {
		t.Fatalf("deployment %s, deleted while it drained since %s, reads %s (%T)", second.DeploymentID, draining.Retirement.RequestedAt, removed.Status, err)
	}
	t.Logf("Project %s: deployment %s retired %s after its request; rollback %s replaced %s, which the Project's deletion removed while it drained",
		retire.project, first.DeploymentID, retired.Retirement.RetiredAt.Sub(retired.Retirement.RequestedAt).Round(time.Second), third.DeploymentID, second.DeploymentID)
}

// A Worker replaced while the first generation drains does not extend the
// drain or lose the retirement: the restarted Worker retires it once.
func TestLiveRetirementWorkerRestartDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Minute)
	defer cancel()
	retire := newRetirementScenario(ctx, t)
	first := retire.deploy(ctx, 0)
	retire.deploy(ctx, first.Lease.Generation)
	draining := retire.awaitStatus(ctx, first.DeploymentID, deployments.Retiring)
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, err := retire.live.deployments.ReadSnapshotForTest(ctx, retire.project, first.DeploymentID)
		return err
	})
	retired := retire.awaitStatus(ctx, first.DeploymentID, deployments.Retired)
	if retired.Retirement.RequestedAt != draining.Retirement.RequestedAt || retired.Retirement.RetiredAt.Sub(retired.Retirement.RequestedAt) > retirementTimeout {
		t.Fatalf("the retirement of %s moved from %+v to %+v", first.DeploymentID, draining.Retirement, retired.Retirement)
	}
	retire.requireGeneration(ctx, first, false)
	t.Logf("Project %s: deployment %s retired %s after its request across a Worker restart", retire.project, first.DeploymentID,
		retired.Retirement.RetiredAt.Sub(retired.Retirement.RequestedAt).Round(time.Second))
}

// retirementScenario is a throwaway Project with one READY build of its HEAD,
// deployed as often as a scenario asks.
type retirementScenario struct {
	t       *testing.T
	live    *liveDelivery
	project string
	head    string
	built   releases.ReleaseBuildRevision
}

func newRetirementScenario(ctx context.Context, t *testing.T) *retirementScenario {
	t.Helper()
	session := newMockSession(t, ctx, "")
	live := newLiveDelivery(session)
	head := session.identity.HeadCommit
	if release := live.prepare(ctx, head, 1); release.PreparedSource.Status != releases.Ready {
		t.Fatalf("the source of the Release of %s is %s", head, release.PreparedSource.Status)
	}
	session.configureConnections(ctx, live.configuration, live.target(head))
	built, _ := live.build(ctx, head, releases.BuildRevisionRequest{ConfigurationRevision: live.publishCopy(ctx, head), PreviewSkipReason: skipPreview})
	if built.Status != releases.Ready {
		t.Fatalf("the build of %s is %s: %+v", head, built.Status, built.Failure)
	}
	return &retirementScenario{t: t, live: live, project: session.identity.ProjectID, head: head, built: built}
}

// start is the deployment deploymentID of the scenario's build, replacing the
// Live generation expected.
func (r *retirementScenario) start(deploymentID string, expected uint64) deployments.Start {
	return deployments.Start{ProjectID: r.project, DeploymentID: deploymentID, ReleaseID: r.head, BuildRevisionID: r.built.BuildRevisionID,
		ConfigurationRevision: r.built.Configuration.ConfigurationRevision, ExpectedLiveRevision: expected}
}

// deploy deploys the build under a new deployment ID, replacing the Live
// generation expected, and returns the deployment once it is READY.
func (r *retirementScenario) deploy(ctx context.Context, expected uint64) deployments.Snapshot {
	r.t.Helper()
	deploymentID, err := r.live.deployments.DeployReadyBuild(ctx, r.live.session.account, uuid.NewString(), r.start(uuid.NewString(), expected))
	if err != nil {
		r.t.Fatalf("the deployment replacing generation %d was not admitted (%T)", expected, err)
	}
	deployment := r.awaitStatus(ctx, deploymentID, deployments.Ready)
	if deployment.Resources.Worker.UID == "" || deployment.Resources.Worker.UID == deployment.Providers.Application.UID {
		r.t.Fatalf("deployment %s did not take the Live Worker endpoint: %+v", deploymentID, deployment.Resources.Worker)
	}
	return deployment
}

// awaitStatus waits until the deployment reaches want and fails the test when
// it fails instead.
func (r *retirementScenario) awaitStatus(ctx context.Context, deploymentID string, want deployments.Status) deployments.Snapshot {
	r.t.Helper()
	wait, cancel := context.WithTimeout(ctx, max(liveDeploymentTimeout, retirementTimeout))
	defer cancel()
	var deployment deployments.Snapshot
	r.live.session.await(wait, "deployment "+deploymentID+" "+want, func() bool {
		var err error
		deployment, err = r.live.deployments.ReadSnapshotForTest(wait, r.project, deploymentID)
		if err == nil && deployment.Status == deployments.Failed {
			r.t.Fatalf("deployment %s failed in %s: %+v", deploymentID, deployment.Providers.Phase, deployment.Failure)
		}
		return err == nil && deployment.Status == want
	})
	return deployment
}

// requireGeneration requires the deployment's generation to run and hold the
// Live Worker endpoint, or its application to be gone.
func (r *retirementScenario) requireGeneration(ctx context.Context, s deployments.Snapshot, running bool) {
	r.t.Helper()
	present := generationPresent(ctx, r.t, s)
	worker := laneConnections(r.t).CheckWorker(ctx, infra.WorkerTarget{ProjectID: s.ProjectID, Namespace: s.Resources.Storage.Boundary, Controller: s.Resources.Worker,
		Service: s.Resources.Service, DeploymentID: s.DeploymentID, Generation: s.Lease.Generation})
	if present != running || (worker == nil) != running {
		r.t.Fatalf("generation %d of deployment %s runs: %t, holds the Worker endpoint: %t (%v); want %t", s.Lease.Generation, s.DeploymentID, present, worker == nil, worker, running)
	}
}

// generationPresent reports whether the generation's application object
// exists: its Deployment in Local Kind, its container in Native.
func generationPresent(ctx context.Context, t *testing.T, s deployments.Snapshot) bool {
	t.Helper()
	name := infra.WorkloadName(s.DeploymentID, s.Lease.Generation)
	if dexflowtest.LocalLaneForTest(t) == dexflowtest.LaneKindForTest {
		k := kubetest.InfrastructureForTest(t)
		o, code, err := k.API.Call(ctx, http.MethodGet, "/apis/apps/v1/namespaces/"+k.ProjectNamespace(s.ProjectID)+"/deployments/"+name, nil)
		if err != nil {
			t.Fatal(err)
		}
		return code != http.StatusNotFound && o != nil
	}
	k, ok := laneStorage(t).(*infradocker.Infrastructure)
	if !ok {
		t.Fatal("the Native lane's storage is not the Docker runtime")
	}
	container, err := k.API().InspectContainer(ctx, k.GenerationName(s.ProjectID, s.DeploymentID, s.Lease.Generation))
	if err != nil {
		t.Fatal(err)
	}
	return container != nil
}

// failedPrecondition reports a FAILED_PRECONDITION rejection.
func failedPrecondition(err error) bool {
	var rejection *dexflow.Rejection
	return errors.As(err, &rejection) && rejection.HasCode(dexflow.RejectionFailedPrecondition)
}
