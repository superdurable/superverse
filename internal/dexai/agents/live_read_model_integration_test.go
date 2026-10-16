// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi"
	"github.com/superdurable/superversev2/internal/dexai/studio/runs"
)

// TestLiveReadModelDexIntegration deploys the HEAD of a throwaway Project to
// Live (agents.NewLiveProjectForTest) and checks what reads it from the read
// model:
//   - the Release, its build and the Deployment that ReadProjection decodes
//     from their rows equal what their Flows hold, except the fields the read
//     model does not sync;
//   - Live Runs resolve the deployment from the read model's Live target,
//     and two engine reads share one pooled, verified Dex connection;
//   - while SHARE locks on projects and deployments hold the rows back, the
//     Project removes Live: a read still resolves the deployment the stale
//     rows select, and a write, which confirms the Live pointer with
//     ProjectFlow, is refused as no longer current;
//   - once Dex writes the rows, the read is refused too.
func TestLiveReadModelDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Minute)
	defer cancel()
	live := agents.NewLiveProjectForTest(t, ctx)
	awaitProjections(ctx, t, live)

	flowRuns := &runs.Runs{Projects: live.Projects, Deployments: live.Deployments, Objects: live.Objects, Connections: live.Connections,
		Names: deploymentnames.MustFor(deploymentnames.EnvironmentTest), Logger: slog.Default(), DexConnections: 4}
	t.Cleanup(func() { _ = flowRuns.Close() })
	target := &pb.FlowApiContext{Target: &pb.FlowApiTarget{ProjectId: live.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE,
		ReleaseId: live.ReleaseID, DeploymentId: live.DeploymentID, Generation: live.Generation}}
	if document, err := flowRuns.Definitions(ctx, live.Account, target); err != nil || document.StatusCode != 200 {
		t.Fatalf("the Live definitions of deployment %s: %d: %v", live.DeploymentID, document.StatusCode, err)
	}
	requireEngineReads(ctx, t, flowRuns, live.Account, target, 1)

	// The locks hold the rows of the removal back; Dex retries the waiting
	// batches, and the Flows' RPCs wait for them (superdurable/dex#623).
	releaseProjects := postgrestest.LockTableForTest(ctx, t, "projects")
	releaseDeployments := postgrestest.LockTableForTest(ctx, t, "deployments")
	if _, _, err := live.Projects.RemoveLive(ctx, live.Account, live.ProjectID, uuid.NewString(), live.DeploymentID, live.Generation); err != nil {
		t.Fatalf("the removal of Live was not admitted (%T)", err)
	}
	for {
		project, err := live.Projects.Read(ctx, live.ProjectID)
		if err == nil && project.CurrentLive == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ProjectFlow did not remove Live (%T)", err)
		case <-time.After(2 * time.Second):
		}
	}
	if document, err := flowRuns.Definitions(ctx, live.Account, target); err != nil || document.StatusCode != 200 {
		t.Fatalf("a read while the rows still select deployment %s: %d: %v", live.DeploymentID, document.StatusCode, err)
	}
	_, err := flowRuns.Invoke(ctx, live.Account, target, runs.Write, func(context.Context, *flowapi.FlowAPI) flowapi.Document {
		t.Fatal("a write reached the removed Live deployment")
		return flowapi.Document{}
	})
	if !noLongerCurrent(err) {
		t.Fatalf("a write after the removal was not refused as no longer current: %v", err)
	}
	releaseProjects()
	releaseDeployments()
	deadline := time.Now().Add(2 * time.Minute)
	for _, err = flowRuns.Definitions(ctx, live.Account, target); !noLongerCurrent(err); _, err = flowRuns.Definitions(ctx, live.Account, target) {
		if time.Now().After(deadline) {
			t.Fatalf("a read after Dex wrote the removal still resolves deployment %s: %v", live.DeploymentID, err)
		}
		time.Sleep(time.Second)
	}
	t.Logf("Project %s: the projections equal the Flows; after the Live removal a write was refused while the rows still selected deployment %s, and the read once they followed", live.ProjectID, live.DeploymentID)
}

// noLongerCurrent reports the rejection of a Runs target that the Project's
// Live pointer no longer names.
func noLongerCurrent(err error) bool {
	var rejection *dexflow.Rejection
	return errors.As(err, &rejection) && rejection.HasCode(dexflow.RejectionFailedPrecondition) && rejection.Message == "Requested Deployment is no longer current in Live"
}

// awaitProjections waits until the Release, its build and the Deployment that
// the read model holds equal their Flows' state without the fields that are
// not synced, with times to the microsecond that PostgreSQL keeps.
func awaitProjections(ctx context.Context, t *testing.T, live agents.LiveProjectForTest) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		difference := projectionDifference(ctx, t, live)
		if difference == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Project %s: %s", live.ProjectID, difference)
		}
		time.Sleep(2 * time.Second)
	}
}

// projectionDifference names the first value the read model holds otherwise
// than the Flows, or returns "".
func projectionDifference(ctx context.Context, t *testing.T, live agents.LiveProjectForTest) string {
	t.Helper()
	release, err := live.Releases.ReadSnapshot(ctx, live.ProjectID, live.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	build, err := live.Releases.ReadBuildRevision(ctx, live.ProjectID, live.ReleaseID, live.BuildRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := live.Deployments.ReadSnapshotForTest(ctx, live.ProjectID, live.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	releaseProjection, err := live.Releases.ReadProjection(ctx, live.ProjectID, live.ReleaseID)
	if err != nil {
		return "the Release's row: " + err.Error()
	}
	deploymentProjection, err := live.Deployments.ReadProjection(ctx, live.ProjectID, live.DeploymentID)
	if err != nil {
		return "the Deployment's row: " + err.Error()
	}
	if len(releaseProjection.Builds) == 0 {
		return "the Release's projection holds no build"
	}
	// The fields the read model does not sync, which a projection leaves zero.
	rowRelease, rowBuild, rowDeployment := releaseProjection.Release, releaseProjection.Builds[0], deploymentProjection.Deployment
	release.Identity, release.Repository, release.BuildProfileDigest, release.CancellationRequested = rowRelease.Identity, rowRelease.Repository, rowRelease.BuildProfileDigest, rowRelease.CancellationRequested
	build.Fingerprint, build.BuildProfileDigest, build.Operation, build.ExpectedLiveRevision = rowBuild.Fingerprint, rowBuild.BuildProfileDigest, rowBuild.Operation, rowBuild.ExpectedLiveRevision
	deployment.Identity, deployment.ExpectedLiveRevision, deployment.CleanupRequested = rowDeployment.Identity, rowDeployment.ExpectedLiveRevision, rowDeployment.CleanupRequested
	for _, pair := range []struct {
		what       string
		flow, rows any
	}{
		{"the Release", truncateTimes(release), truncateTimes(rowRelease)},
		{"the build", truncateBuild(build), truncateBuild(rowBuild)},
		{"the Deployment", truncateDeployment(deployment), truncateDeployment(rowDeployment)},
	} {
		flow, rows := mustJSON(t, pair.flow), mustJSON(t, pair.rows)
		if flow != rows {
			return pair.what + " differs: Flow " + flow + ", rows " + rows
		}
	}
	return ""
}

func truncateTimes(s releases.Snapshot) releases.Snapshot {
	s.CreatedAt, s.UpdatedAt = microseconds(s.CreatedAt), microseconds(s.UpdatedAt)
	return s
}

func truncateBuild(b releases.ReleaseBuildRevision) releases.ReleaseBuildRevision {
	b.CreatedAt, b.UpdatedAt = microseconds(b.CreatedAt), microseconds(b.UpdatedAt)
	return b
}

func truncateDeployment(d deployments.Snapshot) deployments.Snapshot {
	d.CreatedAt, d.UpdatedAt = microseconds(d.CreatedAt), microseconds(d.UpdatedAt)
	return d
}

// microseconds is a time as PostgreSQL keeps it, in UTC.
func microseconds(at time.Time) time.Time { return at.Truncate(time.Microsecond).UTC() }

// mustJSON is a value's JSON encoding, the form in which Dex stores it.
func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// requireEngineReads reads the target's engine twice through Invoke, a Flow
// summary that the engine answers NotFound, and requires that Runs dialed
// dials connections in all: both reads lease one pooled connection.
func requireEngineReads(ctx context.Context, t *testing.T, flowRuns *runs.Runs, account string, target *pb.FlowApiContext, dials uint64) {
	t.Helper()
	for range 2 {
		document, err := flowRuns.Invoke(ctx, account, target, runs.Read, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
			return flows.Summary(ctx, flowapi.FlowRun{FlowID: "runs-cache-probe"})
		})
		if err != nil || document.StatusCode != http.StatusNotFound {
			t.Fatalf("an engine read of the Runs target was not served by its Dex: %d: %v", document.StatusCode, err)
		}
	}
	if got := flowRuns.DexDialsForTest(); got != dials {
		t.Fatalf("Runs dialed %d Dex connections, not %d: the pooled connection was not reused", got, dials)
	}
}
