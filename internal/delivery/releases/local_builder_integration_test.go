// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/releases/build"
	buildlocal "github.com/superdurable/superversev2/internal/delivery/releases/build/local"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/projectdao"
)

// localImagePhases are the phases the local build fixture measures for an
// image build.
var localImagePhases = []string{build.PhaseQueue, build.PhaseHostPreparation, build.PhaseDependencyPreparationForTest, build.PhaseAnalysisForTest, build.PhaseCompilationForTest, build.PhaseImagePublicationForTest}

// fixtureBuild returns the completed image-build request that
// SUPERVERSE_TEST_BUILD_FIXTURE_REQUEST names and the client of the local
// build fixture that ran it.
func fixtureBuild(t *testing.T) (build.BuildRequest, *buildlocal.Builder) {
	t.Helper()
	data, err := os.ReadFile(dexflowtest.Setting(t, "SUPERVERSE_TEST_BUILD_FIXTURE_REQUEST"))
	if err != nil {
		t.Fatal(err)
	}
	var request build.BuildRequest
	if err = json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	if err = build.ValidateIdentity(request); err != nil {
		t.Fatal(err)
	}
	if request.SourceOnly {
		t.Fatal("requires a completed image build, not a source preparation fixture")
	}
	token, err := os.ReadFile(dexflowtest.Setting(t, "SUPERVERSE_LOCAL_BUILD_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	builder, err := buildlocal.New(dexflowtest.Setting(t, "SUPERVERSE_LOCAL_BUILD_URL"), strings.TrimSpace(string(token)))
	if err != nil {
		t.Fatal(err)
	}
	return request, builder
}

// requireLocalImagePhases fails unless timings are the six phases of a local
// image build, each measured once with a start and a positive duration.
func requireLocalImagePhases(t *testing.T, timings []build.BuildPhaseTiming) {
	t.Helper()
	measured := map[string]bool{}
	for _, timing := range timings {
		if timing.StartedAt.IsZero() || timing.DurationMs <= 0 || measured[timing.Phase] {
			t.Fatalf("phase %q is unmeasured or repeated", timing.Phase)
		}
		measured[timing.Phase] = true
	}
	for _, phase := range localImagePhases {
		if !measured[phase] {
			t.Fatalf("phase %s is missing from %d measured phases", phase, len(timings))
		}
	}
	if len(timings) != len(localImagePhases) {
		t.Fatalf("the fixture measured %d phases, not %d", len(timings), len(localImagePhases))
	}
}

// The host fixture must have completed a real build and published its image:
// into the owned V2 Kind nodes, or to the Native lane's registry.
func TestLocalBuilderImmutableArtifactsIntegration(t *testing.T) {
	request, builder := fixtureBuild(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := builder.Inspect(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != build.BuildReady || !flowid.IsSHA256Digest(result.ImageDigest) || !strings.HasSuffix(result.ImageReference, "@"+result.ImageDigest) {
		t.Fatal("fixture did not publish immutable ready image")
	}
	requireLocalImagePhases(t, result.Timings)
	manifest, err := appmanifest.ExtractAppManifest(request.SourceCommit, []byte(result.Payloads.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	// Expectations come from the accepted request, never from the returned
	// bundle. This test checks provider payload identity; exact configuration
	// object versions and Flow admission require their own real boundaries.
	owner := BuildSnapshot{
		Release: Snapshot{ProjectID: request.ProjectID, ReleaseID: request.ReleaseID, SourceCommit: request.SourceCommit},
		Build: ReleaseBuildRevision{BuildRevisionID: request.BuildRevisionID, BuildProfileDigest: request.ProfileDigest, Configuration: &ReadyConfiguration{
			ReleaseID: request.ReleaseID, ConfigurationRevision: request.ConfigurationRevision,
			Object: objectstore.ObjectRef{Digest: request.ConfigurationDigest},
		}},
	}.artifacts()
	definitions, err := validatePayloads(owner, manifest, result.Payloads)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) == 0 {
		t.Fatal("real build supplied no Flow definitions")
	}
	t.Logf("verified project=%s commit=%s image=%s definitions=%d phases=%d", request.ProjectID, request.SourceCommit, result.ImageReference, len(definitions), len(result.Timings))
}

// This admits a new build of the fixture's Release in the local lane and
// replaces the actual application Worker while AwaitReleaseBuildRevision polls the
// local builder. The wait must resume in a new attempt after the heartbeat
// timeout, and the READY build must keep the timings of its committed
// observation: configuration freezing, the six phases exactly as the builder
// reports them, then artifact publication. The Release must have no active
// build, and the draft revision the fixture build froze must still be the
// draft's current one. The new build stays with the Release until Project
// deletion removes it.
func TestReleaseBuildTimingsWorkerRestartIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	fixture, builder := fixtureBuild(t)
	projectFlow, releaseFlow := &projects.ProjectFlow{}, &ProjectReleaseFlow{}
	// The client registry supplies definitions only; the existing Worker runs
	// the Steps with every real dependency.
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, releaseFlow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	projectsClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: len(fixture.ProjectID), PlatformAdminEmail: "admin@superverse.test"}
	client := NewProjectReleaseFlowClient(releaseFlow, projectsClient, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	project, err := projectsClient.Read(ctx, fixture.ProjectID)
	if err != nil || project.ProjectStatus != projects.StatusReady {
		t.Fatalf("requires the fixture's READY Project; no build admitted (%T)", err)
	}
	release, err := client.ReadSnapshot(ctx, fixture.ProjectID, fixture.ReleaseID)
	if err != nil || release.ActiveBuildRevisionID != "" {
		t.Fatalf("requires the fixture's Release without an active build; no build admitted (%T)", err)
	}
	completed, err := client.ReadBuildRevision(ctx, fixture.ProjectID, fixture.ReleaseID, fixture.BuildRevisionID)
	if err != nil || completed.Status != Ready {
		t.Fatalf("requires the fixture's READY build; no build admitted (%T)", err)
	}
	id := "build-" + uuid.NewString()
	flowID := comm.Must(FlowID(fixture.ProjectID, fixture.ReleaseID))
	t.Logf("Worker restart probe FlowID=%s build=%s", flowID, id)
	// Admission's Preview and Live draft checks need the Session Service and the configuration owner, which this
	// package's tests cannot compose; the build is requested as admission would, with a written skip reason.
	reason := "Worker restart probe of an already built version"
	request := BuildRevisionRequest{RequestID: uuid.NewString(), BuildRevisionID: id, ExpectedRevision: release.Revision, ConfigurationRevision: completed.ConfigurationRevision,
		ActorAccountID: project.CreatorAccountID, PreviewSkipReason: reason, PreviewValidation: &PreviewValidation{Kind: PreviewSkipped, SkipReason: reason, ActorAccountID: project.CreatorAccountID}}
	if _, err = client.requestBuild(ctx, fixture.ProjectID, fixture.ReleaseID, release, request); err != nil {
		t.Fatalf("actual build admission failed (%T)", err)
	}
	wait := func(what string, done func() bool) {
		t.Helper()
		for !done() {
			select {
			case <-ctx.Done():
				t.Fatalf("%s did not converge; reconcile build %s of %s", what, id, flowID)
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	// awaiting reports whether AwaitReleaseBuildRevision is active and whether
	// an attempt of it failed, which is how the engine records a heartbeat
	// timeout.
	awaiting := func() (bool, bool) {
		t.Helper()
		state, readErr := reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowID})
		if readErr != nil {
			t.Fatalf("actual bounded state read failed (%T)", readErr)
		}
		for _, step := range state.GetActiveStepExecutions() {
			if step.GetStepType() == phaseAwait {
				return true, step.GetLastFailureInfo().GetAttempt() > 0
			}
		}
		return false, false
	}
	wait("the build wait", func() bool { active, _ := awaiting(); return active })
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, readErr := client.ReadSnapshot(ctx, fixture.ProjectID, fixture.ReleaseID)
		return readErr
	})
	var retried bool
	var ready ReleaseBuildRevision
	wait("the READY build after the Worker replacement", func() bool {
		_, failed := awaiting()
		retried = retried || failed
		// The Release read needs a Worker, so it fails while none serves.
		record, readErr := client.ReadBuildRevision(ctx, fixture.ProjectID, fixture.ReleaseID, id)
		if readErr != nil {
			return false
		}
		if record.Status == Failed {
			t.Fatalf("build %s failed: %s", id, record.Failure.Message)
		}
		ready = record
		return record.Status == Ready
	})
	if !retried {
		t.Fatal("no AwaitReleaseBuildRevision attempt failed after the Worker replacement; the heartbeat timeout was not observed")
	}
	release, err = client.ReadSnapshot(ctx, fixture.ProjectID, fixture.ReleaseID)
	if err != nil {
		t.Fatalf("actual Release read failed (%T)", err)
	}
	provider, err := (&ProjectReleaseFlow{profiles: build.NewProfiles(), environment: fixture.Environment}).revisionRequest(release, ready)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := builder.Inspect(ctx, provider)
	if err != nil || observed.State != build.BuildReady {
		t.Fatalf("the builder does not report build %s READY (%T)", id, err)
	}
	requireLocalImagePhases(t, observed.Timings)
	timings := ready.Timings
	if len(timings) != len(observed.Timings)+2 || timings[0].Phase != timingConfigurationFreeze || timings[len(timings)-1].Phase != timingArtifactPublication {
		t.Fatalf("build %s committed %d timings, not freezing, the %d provider phases and publication", id, len(timings), len(observed.Timings))
	}
	for i, measured := range observed.Timings {
		committed := timings[i+1]
		if committed.Phase != measured.Phase || !committed.StartedAt.Equal(measured.StartedAt) || committed.Duration != time.Duration(measured.DurationMs)*time.Millisecond {
			t.Fatalf("committed phase %s differs from the builder's %s", committed.Phase, measured.Phase)
		}
	}
	for _, timing := range timings {
		if timing.Duration <= 0 || timing.StartedAt.IsZero() {
			t.Fatalf("committed phase %s is unmeasured", timing.Phase)
		}
	}
	t.Logf("FlowID=%s build=%s READY after a heartbeat-timeout retry with %d phases", flowID, id, len(timings))
}
