// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
)

// The scenarios below deploy a READY Release build on the existing Local Kind
// Worker with every real dependency: Dex, the Project's namespace, its MinIO,
// its Temporal and its Live Dex. They start no substitute Worker or provider.
// A scenario changes the cluster outside the Flows only to inject the fault
// it names: a NetworkPolicy of another owner, the project's Temporal scaled to
// zero or a replaced server Pod.

// deployScenario deploys one fixture build: a READY build of a READY
// Project, named by the completed image-build request in a fixture file.
type deployScenario struct {
	t                     *testing.T
	kubeconfig            string
	sdk                   *dex.Client
	reader                dexpb.FlowServiceClient
	projects              *projects.ProjectFlowClient
	deployments           *deployments.ProjectDeployFlowClient
	build                 build.BuildRequest
	owner                 string
	configurationRevision uint64
}

func newDeployScenario(ctx context.Context, t *testing.T, fixture string) *deployScenario {
	t.Helper()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	kubeconfig := dexflowtest.Setting(t, "SUPERVERSE_TEST_KIND_KUBECONFIG")
	data, err := os.ReadFile(dexflowtest.Setting(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	var request build.BuildRequest
	if err = json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	if err = build.ValidateIdentity(request); err != nil || request.SourceOnly {
		t.Fatalf("%s must name a completed image build: %v", fixture, err)
	}
	projectFlow, releaseFlow, deployFlow := &projects.ProjectFlow{}, &releases.ProjectReleaseFlow{}, &deployments.ProjectDeployFlow{}
	// The client registry supplies definitions only; the existing Worker runs
	// the Steps with every real dependency.
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, releaseFlow, deployFlow)
	db := postgrestest.OpenForTest(t)
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), Members: memberdao.New(db), ProjectIDLength: len(request.ProjectID)}
	releaseClient := releases.NewProjectReleaseFlowClient(releaseFlow, projectClient, nil)
	deployClient := deployments.NewProjectDeployFlowClient(projectClient, nil, nil)
	deployClient.Releases = releaseClient
	project, err := projectClient.Read(ctx, request.ProjectID)
	if err != nil || project.ProjectStatus != projects.StatusReady || project.ActiveDeployment != nil {
		t.Fatalf("requires the fixture's READY Project %s without an unresolved deployment; no deployment admitted (%T)", request.ProjectID, err)
	}
	ready, err := releaseClient.ReadBuildRevision(ctx, request.ProjectID, request.ReleaseID, request.BuildRevisionID)
	if err != nil || ready.Status != releases.Ready || ready.Configuration == nil {
		t.Fatalf("requires the fixture's READY build %s; no deployment admitted (%T)", request.BuildRevisionID, err)
	}
	return &deployScenario{
		t: t, kubeconfig: kubeconfig, sdk: sdk, reader: dexflowtest.ExecutionReaderForTest(t, address), projects: projectClient, deployments: deployClient,
		build: request, owner: project.CreatorAccountID, configurationRevision: ready.Configuration.ConfigurationRevision,
	}
}

// project reads the fixture's Project.
func (s *deployScenario) project(ctx context.Context) projects.Project {
	s.t.Helper()
	project, err := s.projects.Read(ctx, s.build.ProjectID)
	if err != nil {
		s.t.Fatalf("authoritative Project read failed (%T)", err)
	}
	return project
}

// liveRevision is the Live generation a new deployment replaces, zero without
// Live.
func liveRevision(project projects.Project) uint64 {
	if project.CurrentLive == nil {
		return 0
	}
	return project.CurrentLive.Revision
}

// deploy deploys the fixture build under a new ID and returns the ID the
// client accepted.
func (s *deployScenario) deploy(ctx context.Context) string {
	s.t.Helper()
	start := deployments.Start{BuildRevisionID: s.build.BuildRevisionID, ProjectID: s.build.ProjectID, DeploymentID: uuid.NewString(), ReleaseID: s.build.ReleaseID,
		ConfigurationRevision: s.configurationRevision, ExpectedLiveRevision: liveRevision(s.project(ctx))}
	deployment, err := s.deployments.DeployReadyBuild(ctx, s.owner, uuid.NewString(), start)
	if err != nil {
		s.t.Fatalf("actual deployment admission failed: %v", err)
	}
	s.t.Logf("deployment FlowID=deploy-%s-%s", s.build.ProjectID, deployment)
	return deployment
}

// resume requests the recovery of a FAILED deployment, as StartDeployment
// with the deployment's accepted inputs and a new request does.
func (s *deployScenario) resume(ctx context.Context, failed deployments.Snapshot) {
	s.t.Helper()
	start := deployments.Start{BuildRevisionID: failed.BuildRevisionID, ProjectID: failed.ProjectID, DeploymentID: failed.DeploymentID, ReleaseID: failed.ReleaseID,
		ConfigurationRevision: failed.ConfigurationRevision, ExpectedLiveRevision: failed.ExpectedLiveRevision}
	if _, err := s.deployments.DeployReadyBuild(ctx, s.owner, uuid.NewString(), start); err != nil {
		s.t.Fatalf("actual deployment recovery admission failed: %v", err)
	}
}

// await polls the deployment every second until it reaches want; any other
// settled status fails the scenario with the deployment's failure.
func (s *deployScenario) await(ctx context.Context, deployment string, want deployments.Status) deployments.Snapshot {
	s.t.Helper()
	settled := []deployments.Status{deployments.Ready, deployments.Failed, deployments.Removed}
	for {
		// The read needs a Worker, so it fails while none serves.
		snapshot, err := s.deployments.ReadSnapshotForTest(ctx, s.build.ProjectID, deployment)
		switch {
		case err == nil && snapshot.Status == want:
			return snapshot
		case err == nil && slices.Contains(settled, snapshot.Status) && snapshot.Status != want:
			message := ""
			if snapshot.Failure != nil {
				message = snapshot.Failure.Phase + ": " + snapshot.Failure.Message
			}
			s.t.Fatalf("deployment %s is %s, not %s: %s", deployment, snapshot.Status, want, message)
		}
		s.pause(ctx, "deployment "+deployment+" reaching "+want)
	}
}

// awaitProject polls the Project every second until done holds.
func (s *deployScenario) awaitProject(ctx context.Context, what string, done func(projects.Project) bool) projects.Project {
	s.t.Helper()
	for {
		if project, err := s.projects.Read(ctx, s.build.ProjectID); err == nil && done(project) {
			return project
		}
		s.pause(ctx, what)
	}
}

// branch reports whether a Step of the deployment is active and whether an
// attempt of it failed, which is how the engine records a heartbeat timeout.
func (s *deployScenario) branch(ctx context.Context, deployment, step string) (active, failed bool) {
	state, err := s.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: "deploy-" + s.build.ProjectID + "-" + deployment})
	if err != nil {
		return false, false
	}
	for _, execution := range state.GetActiveStepExecutions() {
		if execution.GetStepType() == step {
			return true, execution.GetLastFailureInfo().GetAttempt() > 0
		}
	}
	return false, false
}

// requireNoPendingResults fails when a LivePreparationResults message is still
// pending: the join consumed exactly the two results of each round.
func (s *deployScenario) requireNoPendingResults(ctx context.Context, deployment string) {
	s.t.Helper()
	state, err := s.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: "deploy-" + s.build.ProjectID + "-" + deployment})
	if err != nil {
		s.t.Fatalf("actual bounded state read failed (%T)", err)
	}
	for key, values := range state.GetPendingChannelMessages() {
		if strings.Contains(key, "LivePreparationResults") && len(values.GetMessages()) > 0 {
			s.t.Fatalf("deployment %s keeps %d unconsumed preparation results under %s", deployment, len(values.GetMessages()), key)
		}
	}
}

// requireBranchTimings requires one measured timing of each preparation
// branch and returns them.
func (s *deployScenario) requireBranchTimings(snapshot deployments.Snapshot) (temporal, access dexflow.PhaseTiming) {
	s.t.Helper()
	for _, timing := range snapshot.Timings {
		if timing.Duration <= 0 || timing.StartedAt.IsZero() {
			s.t.Fatalf("deployment %s committed the unmeasured phase %s", snapshot.DeploymentID, timing.Phase)
		}
		switch timing.Phase {
		case "PrepareLiveTemporal":
			temporal = timing
		case "PrepareLiveAccess":
			access = timing
		}
	}
	if temporal.Phase == "" || access.Phase == "" {
		s.t.Fatalf("deployment %s did not measure both preparation branches", snapshot.DeploymentID)
	}
	return temporal, access
}

// requireRedacted fails when text names an internal resource of the Project
// or the deployment.
func (s *deployScenario) requireRedacted(project projects.Project, deployment, text string) {
	s.t.Helper()
	for _, name := range []string{project.Storage.Boundary.Name, "app-" + project.ProjectID, "app-" + deployment} {
		if strings.Contains(text, name) {
			s.t.Fatalf("text shown outside the platform names the internal resource %s", name)
		}
	}
}

// requireProgressPhases requires every retained progress frame of the
// deployment to carry a Step name as its phase; reasons belong in Detail.
func (s *deployScenario) requireProgressPhases(ctx context.Context, deployment string, want ...string) {
	s.t.Helper()
	seen := map[string]bool{}
	for before := ""; ; {
		var page dex.StreamMessagesPage[dexflow.ProgressEvent]
		if err := s.sdk.ListStreamMessages(ctx, "deploy-"+s.build.ProjectID+"-"+deployment, deployments.DeploymentProgress, 100, before, &page); err != nil {
			s.t.Fatalf("progress read failed (%T)", err)
		}
		for _, message := range page.Messages {
			if !slices.Contains(deployments.DeployPhasesForTest, message.Value.Phase) {
				s.t.Fatalf("a progress frame of deployment %s carries the phase %q, not a Step name", deployment, message.Value.Phase)
			}
			seen[message.Value.Phase] = true
		}
		if before = page.NextPageToken; before == "" {
			break
		}
	}
	for _, phase := range want {
		if !seen[phase] {
			s.t.Fatalf("deployment %s wrote no %s progress frame", deployment, phase)
		}
	}
}

// requireServiceUID fails unless the project namespace's Service name still
// has the recorded UID.
func (s *deployScenario) requireServiceUID(ctx context.Context, namespace, name, uid string) {
	s.t.Helper()
	found, err := s.kubectl(ctx, "-n", namespace, "get", "service", name, "-o", "jsonpath={.metadata.uid}")
	if err != nil || found != uid {
		s.t.Fatalf("Service %s in %s is %q, not the recorded %s: %v", name, namespace, found, uid, err)
	}
}

// kubectl runs one bounded command against the isolated Local Kind cluster.
func (s *deployScenario) kubectl(ctx context.Context, args ...string) (string, error) {
	return s.kubectlWithInput(ctx, nil, args...)
}

func (s *deployScenario) kubectlWithInput(ctx context.Context, input []byte, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", s.kubeconfig, "--context", "kind-superverse-v2", "--request-timeout=15s"}, args...)...)
	command.Stdin = bytes.NewReader(input)
	out, err := command.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// scaleTemporal scales the project's Temporal and waits until its Pods match:
// at zero the Temporal branch stays pending and its data plane is unreachable.
func (s *deployScenario) scaleTemporal(ctx context.Context, namespace string, replicas int) {
	s.t.Helper()
	if out, err := s.kubectl(ctx, "-n", namespace, "scale", "deployment", "temporal", "--replicas="+strconv.Itoa(replicas)); err != nil {
		s.t.Fatalf("scaling the project's Temporal failed: %s", out)
	}
	for {
		ready, err := s.kubectl(ctx, "-n", namespace, "get", "deployment", "temporal", "-o", "jsonpath={.status.readyReplicas}")
		pods, podsErr := s.kubectl(ctx, "-n", namespace, "get", "pods", "-l", kube.LabelSelectorForTest(kube.ComponentTemporal), "-o", "name")
		if err == nil && podsErr == nil && (replicas == 0 && pods == "" || replicas == 1 && ready == "1") {
			return
		}
		s.pause(ctx, "the project's Temporal at "+strconv.Itoa(replicas)+" replicas")
	}
}

// stopTemporal scales the project's Temporal to zero until the scenario ends.
func (s *deployScenario) stopTemporal(ctx context.Context, namespace string) {
	s.t.Helper()
	s.t.Cleanup(func() {
		restore, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer done()
		if out, err := s.kubectl(restore, "-n", namespace, "scale", "deployment", "temporal", "--replicas=1"); err != nil {
			s.t.Errorf("restoring the project's Temporal failed: %s", out)
		}
	})
	s.scaleTemporal(ctx, namespace, 0)
}

// requireLiveExecution deploys the fixture build once when the Project has
// no complete Live execution environment yet, and returns the Project.
func (s *deployScenario) requireLiveExecution(ctx context.Context) projects.Project {
	s.t.Helper()
	if project := s.project(ctx); project.LiveExecution.Complete() {
		return project
	}
	s.await(ctx, s.deploy(ctx), deployments.Ready)
	return s.project(ctx)
}

// deleteProject requests the Project's deletion until its snapshot is
// DELETED, joining a deletion the scenario already requested.
func (s *deployScenario) deleteProject(ctx context.Context) {
	s.t.Helper()
	request := uuid.NewString()
	for {
		project, err := s.projects.Read(ctx, s.build.ProjectID)
		if err == nil && project.ProjectStatus == projects.StatusDeleted {
			s.t.Logf("Project %s DELETED at revision %d", project.ProjectID, project.Revision)
			return
		}
		if err == nil && project.ProjectStatus != projects.StatusDeleting {
			_, err = s.projects.RequestDeletion(ctx, s.build.ProjectID, request, project.Revision)
			var rejected *dexflow.Rejection
			if errors.As(err, &rejected) && rejected.HasCode(dexflow.RejectionAborted) {
				err = nil
			}
		}
		if err != nil {
			// The Worker may be restarting; the next read decides.
			s.t.Logf("deletion of Project %s waits (%s)", s.build.ProjectID, dexflowtest.ErrorCategory(err))
		}
		s.pause(ctx, "the deletion of Project "+s.build.ProjectID)
	}
}

// pause waits a second, failing the scenario at its deadline.
func (s *deployScenario) pause(ctx context.Context, what string) {
	s.t.Helper()
	select {
	case <-ctx.Done():
		s.t.Fatalf("%s did not converge before the deadline", what)
	case <-time.After(time.Second):
	}
}

// TestDeployFirstLivePreparationIntegration consumes a READY Project that
// never prepared its Live execution environment and deletes it at the end
// (SUPERVERSE_TEST_FIRST_DEPLOY_BUILD_REQUEST names a completed image build of
// it). Its first deployment runs the two preparation branches in parallel; an
// allow-dex NetworkPolicy of another owner fails the access branch by
// decision, while the Temporal branch hands its identities to the Project.
// Recovery runs a second round that reuses them and becomes READY. A second
// deployment reuses the whole environment, Live removal keeps it for the next
// deployment, and a Project deleted while the Temporal branch waits ends the
// deployment REMOVED and leaves no namespace.
func TestDeployFirstLivePreparationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Minute)
	defer cancel()
	s := newDeployScenario(ctx, t, "SUPERVERSE_TEST_FIRST_DEPLOY_BUILD_REQUEST")
	t.Cleanup(func() { s.deleteProject(context.WithoutCancel(ctx)) })
	project := s.project(ctx)
	if project.LiveExecution != (infra.ExecutionRefs{}) || project.CurrentLive != nil {
		t.Fatal("requires a Project that never prepared its Live execution environment")
	}
	namespace := project.Storage.Boundary.Name

	// Partial handoff: the access branch fails by decision, the Temporal
	// branch's identities reach the Project, and both are measured in parallel.
	foreign := `{"apiVersion":"networking.k8s.io/v1","kind":"NetworkPolicy","metadata":{"name":"allow-dex"},"spec":{"podSelector":{"matchLabels":{"superverse-test":"foreign"}},"policyTypes":["Ingress"]}}`
	if out, err := s.kubectlWithInput(ctx, []byte(foreign), "-n", namespace, "create", "-f", "-"); err != nil {
		t.Fatalf("creating the foreign allow-dex policy failed: %s", out)
	}
	first := s.deploy(ctx)
	failed := s.await(ctx, first, deployments.Failed)
	if failed.Failure == nil || failed.Failure.Code != deployments.FailureDeploymentForTest || failed.Failure.Phase != deployments.PhasePrepareForTest || !strings.Contains(failed.Failure.Message, deployments.BranchAccessForTest) {
		t.Fatalf("the first deployment did not fail its preparation in the access branch: %+v", failed.Failure)
	}
	s.requireRedacted(project, first, failed.Failure.Message)
	if failed.Providers.PreparationRound != 1 || failed.Providers.Phase != deployments.PhasePrepareForTest {
		t.Fatalf("the failed preparation is round %d in phase %s", failed.Providers.PreparationRound, failed.Providers.Phase)
	}
	temporal, access := s.requireBranchTimings(failed)
	if !temporal.StartedAt.Before(access.StartedAt.Add(access.Duration)) || !access.StartedAt.Before(temporal.StartedAt.Add(temporal.Duration)) {
		t.Fatalf("the preparation branches did not overlap: Temporal %s for %s, access %s for %s", temporal.StartedAt, temporal.Duration, access.StartedAt, access.Duration)
	}
	handed := s.project(ctx).LiveExecution
	if handed.TemporalNamespace == "" || handed.TemporalCredential.UID == "" || handed.DexService != (infra.ResourceRef{}) || failed.Resources.Execution != handed {
		t.Fatalf("the Temporal branch's identities did not reach the Project alone: %+v", handed)
	}
	if out, err := s.kubectl(ctx, "-n", namespace, "delete", "networkpolicy", "allow-dex"); err != nil {
		t.Fatalf("deleting the foreign allow-dex policy failed: %s", out)
	}
	s.resume(ctx, failed)
	recovered := s.await(ctx, first, deployments.Ready)
	execution := s.project(ctx).LiveExecution
	if recovered.Providers.PreparationRound != 2 || !execution.Complete() || execution.TemporalNamespace != handed.TemporalNamespace || execution.TemporalCredential != handed.TemporalCredential {
		t.Fatalf("recovery round %d did not reuse the handed Temporal identities: %+v", recovered.Providers.PreparationRound, execution)
	}
	if recovered.Resources.Execution != execution || recovered.Resources.Storage != project.Storage || recovered.Resources.Service.UID == "" {
		t.Fatalf("the READY deployment records other references than the Project's: %+v", recovered.Resources)
	}
	s.requireServiceUID(ctx, namespace, "temporal", execution.TemporalCredential.UID)
	s.requireServiceUID(ctx, namespace, "dex", execution.DexService.UID)
	s.requireNoPendingResults(ctx, first)
	s.requireProgressPhases(ctx, first, "PrepareLiveTemporal", "PrepareLiveAccess", "StartLiveDex")

	// A second deployment reuses the environment in one round.
	second := s.deploy(ctx)
	reused := s.await(ctx, second, deployments.Ready)
	if reused.Providers.PreparationRound != 1 || s.project(ctx).LiveExecution != execution || reused.Resources.Execution != execution {
		t.Fatalf("the second deployment did not reuse the Live execution environment in one round: round %d", reused.Providers.PreparationRound)
	}
	s.requireBranchTimings(reused)
	s.requireServiceUID(ctx, namespace, "temporal", execution.TemporalCredential.UID)
	s.requireServiceUID(ctx, namespace, "dex", execution.DexService.UID)
	s.requireProgressPhases(ctx, second, "PrepareLiveTemporal", "PrepareLiveAccess")

	// Live removal keeps the environment, and the next deployment reuses it.
	if _, _, err := s.projects.RemoveLive(ctx, s.owner, s.build.ProjectID, uuid.NewString(), second, reused.Lease.Generation); err != nil {
		t.Fatalf("actual Live removal admission failed: %v", err)
	}
	removed := s.awaitProject(ctx, "Live removal", func(p projects.Project) bool { return p.CurrentLive == nil && p.ActiveCommand == nil })
	if removed.LiveExecution != execution {
		t.Fatalf("Live removal changed the Live execution environment to %+v", removed.LiveExecution)
	}
	third := s.deploy(ctx)
	if redeployed := s.await(ctx, third, deployments.Ready); redeployed.Providers.PreparationRound != 1 || s.project(ctx).LiveExecution != execution {
		t.Fatal("the deployment after Live removal did not reuse the Live execution environment")
	}

	// Deletion while the Temporal branch waits for the stopped Temporal, which
	// the deletion removes.
	s.scaleTemporal(ctx, namespace, 0)
	fourth := s.deploy(ctx)
	for active := false; !active; {
		active, _ = s.branch(ctx, fourth, "PrepareLiveTemporal")
		s.pause(ctx, "the Temporal branch of "+fourth)
	}
	// The Project is DELETED only after its children completed their cleanup.
	s.deleteProject(ctx)
	cancelled, err := s.deployments.ReadSnapshotForTest(ctx, s.build.ProjectID, fourth)
	if err != nil || cancelled.Status != deployments.Removed || !cancelled.CleanupRequested || cancelled.Providers.PreparationRound != 1 {
		t.Fatalf("deployment %s is %s, not REMOVED by Project cleanup during its first round (%T)", fourth, cancelled.Status, err)
	}
	remaining, err := s.kubectl(ctx, "get", "namespace", namespace, "--ignore-not-found", "-o", "name")
	if err != nil || remaining != "" {
		t.Fatalf("the deleted Project left namespace %s: %s", namespace, remaining)
	}
	t.Logf("Project=%s deployments %s (2 rounds), %s, %s READY; %s REMOVED during preparation; namespace %s gone", s.build.ProjectID, first, second, third, fourth, namespace)
}

// TestDeployLivePreparationWorkerRestartIntegration replaces the application
// Worker of Local Kind while the Temporal branch of a deployment waits for the
// project's stopped Temporal (SUPERVERSE_TEST_BUILD_FIXTURE_REQUEST names a
// READY build of a READY Project, which keeps the new Live). The branch
// resumes in a new attempt after the heartbeat timeout and publishes once, so
// the join consumes exactly the round's two results and the deployment
// becomes READY in its first round.
func TestDeployLivePreparationWorkerRestartIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Minute)
	defer cancel()
	s := newDeployScenario(ctx, t, "SUPERVERSE_TEST_BUILD_FIXTURE_REQUEST")
	namespace := s.requireLiveExecution(ctx).Storage.Boundary.Name
	s.stopTemporal(ctx, namespace)
	deployment := s.deploy(ctx)
	for active := false; !active; {
		active, _ = s.branch(ctx, deployment, "PrepareLiveTemporal")
		s.pause(ctx, "the Temporal branch of "+deployment)
	}
	if out, err := s.kubectl(ctx, "-n", "superverse-v2-system", "delete", "pod", "-l", "app=superverse-v2-server", "--wait=false"); err != nil {
		t.Fatalf("Worker replacement failed: %s", out)
	}
	for retried := false; !retried; {
		_, retried = s.branch(ctx, deployment, "PrepareLiveTemporal")
		s.pause(ctx, "a new attempt of the Temporal branch of "+deployment)
	}
	s.scaleTemporal(ctx, namespace, 1)
	ready := s.await(ctx, deployment, deployments.Ready)
	if ready.Providers.PreparationRound != 1 {
		t.Fatalf("deployment %s needed %d preparation rounds", deployment, ready.Providers.PreparationRound)
	}
	s.requireBranchTimings(ready)
	s.requireNoPendingResults(ctx, deployment)
	t.Logf("deployment %s READY in one round after a heartbeat-timeout retry of its Temporal branch", deployment)
}

// TestDeployLiveTemporalWaitLimitIntegration keeps the project's Temporal
// stopped past the 20-minute wait limit of the Temporal branch, so it takes
// about 25 minutes and runs once per pull request that changes the
// deployment graph (SUPERVERSE_TEST_BUILD_FIXTURE_REQUEST as above). The
// failure route classifies the timed-out branch with its bounded data-plane
// query, the join fails the round, and recovery after Temporal returns
// becomes READY in a second round.
func TestDeployLiveTemporalWaitLimitIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Minute)
	defer cancel()
	s := newDeployScenario(ctx, t, "SUPERVERSE_TEST_BUILD_FIXTURE_REQUEST")
	project := s.requireLiveExecution(ctx)
	namespace := project.Storage.Boundary.Name
	s.stopTemporal(ctx, namespace)
	deployment := s.deploy(ctx)
	failed := s.await(ctx, deployment, deployments.Failed)
	if failed.Failure == nil {
		t.Fatalf("FAILED deployment %s holds no Failure", deployment)
	}
	message := failed.Failure.Message
	if failed.Failure.Phase != deployments.PhasePrepareForTest || !strings.Contains(message, "PrepareLiveTemporal did not finish within its wait limit of 20 minutes.") ||
		!strings.Contains(message, "The Temporal data plane stayed unreachable for 20 minutes.") {
		t.Fatalf("the timed-out Temporal branch was not classified unreachable: %s: %s", failed.Failure.Phase, message)
	}
	s.requireRedacted(project, deployment, message)
	if temporal, _ := s.requireBranchTimings(failed); temporal.Duration < 20*time.Minute {
		t.Fatalf("the Temporal branch is measured %s, shorter than its wait limit", temporal.Duration)
	}
	s.scaleTemporal(ctx, namespace, 1)
	s.resume(ctx, failed)
	if ready := s.await(ctx, deployment, deployments.Ready); ready.Providers.PreparationRound != 2 {
		t.Fatalf("recovery of deployment %s took %d preparation rounds, not 2", deployment, ready.Providers.PreparationRound)
	}
	s.requireNoPendingResults(ctx, deployment)
	t.Logf("deployment %s FAILED at its Temporal wait limit, then READY in round 2", deployment)
}
