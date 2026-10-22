// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package docker_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	deploymentsdocker "github.com/superdurable/superversev2/internal/dexai/delivery/deployments/docker"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/docker/dockertest"
)

// A real registered candidate loses HTTP readiness. The actual VerifyReadiness
// limit expires, then the production failure path stops it before FAILED is
// observable. Admission alone is seeded; Dex, Docker, grants and handlers are real.
func TestDockerCandidateFailureFlowIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 18*time.Minute)
	defer cancel()
	d := candidateDeployer(ctx, t)
	live := d.deploy(ctx, 1, "1", false, 0)
	candidate := d.prepare(ctx, 2, "candidate", false, 1, false)
	d.requireWorker(ctx, candidate, true)
	snapshot := candidate.snapshot
	snapshot.Inputs.HealthPath = "/missing-health-path"
	snapshot.Lease.DeploymentID = snapshot.DeploymentID
	snapshot.Lease.ExpectedLiveRevision = 1
	snapshot.Status = deployments.Deploying
	snapshot.Providers.Phase = "VerifyReadiness"
	snapshot.Providers.Requests = []deployments.DeploymentRequestReceipt{{RequestID: "initial"}}
	snapshot.ReplacedLive = deployments.ReplacedGeneration{DeploymentID: live.snapshot.DeploymentID, Generation: 1}
	project := projects.Project{ProjectID: d.project, ActiveDeployment: &snapshot.Lease,
		CurrentLive: &projects.LivePointer{DeploymentID: live.snapshot.DeploymentID, Revision: 1}, Storage: d.storage, LiveExecution: d.execution}
	client, deployFlow, projectClient, id := startDeploymentPhase(ctx, t, d, snapshot, project)
	failed := awaitCandidateFailure(ctx, t, client, deployFlow, id, func(s deployments.Snapshot) {
		// Every read that observes FAILED must already observe absence through the real Docker API.
		worker, err := d.k.API().InspectContainer(ctx, candidate.snapshot.Resources.Worker.UID)
		if err != nil || worker != nil {
			t.Fatalf("FAILED preceded candidate Worker exit: %v", err)
		}
	})
	if failed.Failure == nil || failed.Failure.Phase != "VerifyReadiness" || failed.Providers.Phase != "VerifyReadiness" {
		t.Fatalf("original readiness failure was lost: %+v", failed.Failure)
	}
	if failed.Resources.Worker != candidate.snapshot.Resources.Worker {
		t.Fatal("failure lost the exact accepted Worker")
	}
	current, err := projectClient.Read(ctx, d.project)
	if err != nil || current.CurrentLive == nil || current.CurrentLive.DeploymentID != live.snapshot.DeploymentID || current.ActiveDeployment == nil || *current.ActiveDeployment != snapshot.Lease {
		t.Fatalf("candidate failure changed Live or released the lease: %v", err)
	}
	live.call(ctx, http.MethodGet, "/healthz")
	for range 4 {
		probeID := "after-candidate-" + flowid.RandomID(8)
		live.call(ctx, http.MethodPost, "/probes/"+probeID)
		state := live.awaitProbe(ctx, probeID, func(p probe) bool { return p.Outcome == "waiting" })
		if state.StartedBy != "1" {
			t.Fatalf("failed candidate took another task: %+v", state)
		}
		live.call(ctx, http.MethodPost, "/probes/"+probeID+"/release")
		live.awaitProbe(ctx, probeID, func(p probe) bool { return p.Outcome == "completed" })
	}
	d.cleanup(ctx, candidate)
	t.Logf("readiness limit elapsed for real; Worker absent before FAILED; N still serves; candidate lease retained")
}

func awaitCandidateFailure(ctx context.Context, t *testing.T, client *dex.Client, flow *deploymentPhaseFlow, id string, check func(deployments.Snapshot)) deployments.Snapshot {
	t.Helper()
	for {
		var state deployments.Snapshot
		err := client.InvokeRPC(ctx, id, flow.GetProjectDeployment, nil, &state)
		if err == nil && state.Status == deployments.Failed {
			check(state)
			return state
		}
		select {
		case <-ctx.Done():
			t.Fatalf("candidate failure not observed (status %s): %v", state.Status, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

func candidateDeployer(ctx context.Context, t *testing.T) *generationDeployer {
	t.Helper()
	k := dockertest.InfrastructureForTest(t)
	project := flowid.RandomID(4)
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, k, project) })
	var storage infra.StorageRefs
	dockertest.EventuallyForTest(ctx, t, "project storage", func() (err error) { storage, err = k.EnsureStorage(ctx, project, storage); return err })
	var execution infra.ExecutionRefs
	dockertest.EventuallyForTest(ctx, t, "project Temporal", func() (err error) { execution, err = k.EnsureTemporal(ctx, project, storage, execution); return err })
	if err := k.EnsureExecutionAccess(ctx, project, storage); err != nil {
		t.Fatal(err)
	}
	dockertest.EventuallyForTest(ctx, t, "project Dex", func() (err error) { execution, err = k.StartProjectDex(ctx, project, storage, execution); return err })
	return &generationDeployer{t: t, k: k, serving: &deploymentsdocker.LiveServing{Infrastructure: k}, registry: dockertest.RegistryForTest(ctx, t, k), project: project, storage: storage, execution: execution}
}

// deploymentPhaseProject seeds only a disposable Project's admission, then uses the production Project RPCs.
// It performs no replacement of a provider API or production handler.
type deploymentPhaseProject struct{ *projects.ProjectFlow }

func (f *deploymentPhaseProject) GetSteps() []dex.StepDef {
	return append([]dex.StepDef{dex.DefineStartStep(seedProject{})}, f.ProjectFlow.GetSteps()[1:]...)
}

func (f *deploymentPhaseProject) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(f.GetProject, dexflow.ShortRPC()), dex.DefineRPC(f.ReleaseDeployment, dexflow.ShortRPC(dex.LockAttribute(projects.ProjectRevision)))}
}
func (f *deploymentPhaseProject) GetProject(ctx dex.Context, in dex.None) (*dex.RPCResult[projects.Project], error) {
	return f.ProjectFlow.GetProject(ctx, in)
}
func (f *deploymentPhaseProject) ReleaseDeployment(ctx dex.Context, in projects.DeploymentLease) (*dex.RPCResult[projects.ResultForTest], error) {
	return f.ProjectFlow.ReleaseDeployment(ctx, in)
}

type seedProject struct {
	dex.StepDefaultsNoWaitFor[projects.Project]
}

func (seedProject) Execute(ctx dex.Context, p projects.Project) (*dex.StepDecision, error) {
	comm.MustDo(projects.ProjectStatus.Set(ctx, projects.StatusReady))
	comm.MustDo(projects.ProjectActiveDeployment.Set(ctx, p.ActiveDeployment))
	comm.MustDo(projects.ProjectCurrentLive.Set(ctx, p.CurrentLive))
	comm.MustDo(projects.ProjectStorage.Set(ctx, p.Storage))
	comm.MustDo(projects.ProjectLiveExecution.Set(ctx, p.LiveExecution))
	return dex.DeadEnd(), nil
}

// deploymentPhaseFlow starts at the named production phase on resources created through real APIs.
// All later Steps, options, persistence, RPCs and recovery paths are the production implementation.
type deploymentPhaseFlow struct{ *deployments.ProjectDeployFlow }

func (f *deploymentPhaseFlow) GetSteps() []dex.StepDef {
	return append([]dex.StepDef{dex.DefineStartStep(seedDeployment{})}, f.ProjectDeployFlow.GetSteps()[1:]...)
}

func (f *deploymentPhaseFlow) GetRPCs() []dex.RPCDef {
	write := dexflow.ShortRPC(dex.LockAttribute(deployments.Revision))
	return []dex.RPCDef{dex.DefineRPC(f.GetProjectDeployment, dexflow.ShortRPC()), dex.DefineRPC(f.RequestLiveDeployment, write), dex.DefineRPC(f.RequestLiveRetirement, write)}
}
func (f *deploymentPhaseFlow) GetProjectDeployment(ctx dex.Context, in dex.None) (*dex.RPCResult[deployments.Snapshot], error) {
	if _, err := deployments.StartIdentity.Get(ctx); err != nil {
		return nil, err
	}
	return f.ProjectDeployFlow.GetProjectDeployment(ctx, in)
}
func (f *deploymentPhaseFlow) RequestLiveDeployment(ctx dex.Context, in deployments.MutationForTest) (*dex.RPCResult[deployments.MutationResultForTest], error) {
	return f.ProjectDeployFlow.RequestLiveDeployment(ctx, in)
}
func (f *deploymentPhaseFlow) RequestLiveRetirement(ctx dex.Context, in deployments.RetirementRequestForTest) (*dex.RPCResult[deployments.MutationResultForTest], error) {
	return f.ProjectDeployFlow.RequestLiveRetirement(ctx, in)
}

type seedDeployment struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (seedDeployment) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	switch comm.Must(deployments.ProviderOperationRefs.Get(ctx)).Phase {
	case "VerifyReadiness":
		return dex.GoTo(deployments.VerifyReadiness{}, nil), nil
	case "StopReplacedWorker":
		return dex.GoTo(deployments.StopReplacedWorker{}, nil), nil
	default:
		return dex.GoTo(deployments.AwaitDeploymentRecovery{}, nil), nil
	}
}

// deploymentPhaseProjectAttributes initializes the production schema before the admission fixture runs.
func deploymentPhaseProjectAttributes(p projects.Project) []dex.InitialAttributeDef {
	return (&projects.ProjectFlow{}).InitialAttributes(projects.ProjectStart{
		Identity: dexflow.StartIdentity{RequestID: p.ProjectID, Fingerprint: p.ProjectID}, ProjectID: p.ProjectID,
		CreatorAccountID: "fixture-owner", Name: "pull deployment fixture", TeamSpaceID: "fixture-teamspace",
	})
}

func deploymentPhaseAttributes(s deployments.Snapshot) []dex.InitialAttributeDef {
	b := &dexflow.InitialAttributes{}

	dexflow.Add(b, deployments.StartIdentity, s.Identity)
	dexflow.Add(b, deployments.ProjectID, s.ProjectID)
	dexflow.Add(b, deployments.DeploymentID, s.DeploymentID)
	dexflow.Add(b, deployments.ReleaseID, s.ReleaseID)
	dexflow.Add(b, deployments.BuildRevisionID, s.BuildRevisionID)
	dexflow.Add(b, deployments.ConfigurationRevision, s.ConfigurationRevision)
	dexflow.Add(b, deployments.ExpectedLiveRevision, s.ExpectedLiveRevision)
	dexflow.Add(b, deployments.DeploymentStatus, s.Status)
	dexflow.Add(b, deployments.ProjectDeploymentLease, s.Lease)
	dexflow.Add(b, deployments.PinnedInputs, s.Inputs)
	dexflow.Add(b, deployments.WorkloadGrantAttribute, s.Grant)
	dexflow.Add(b, deployments.DeploymentResourcesAttribute, s.Resources)
	dexflow.Add(b, deployments.ProviderOperationRefs, s.Providers)
	dexflow.Add(b, deployments.PhaseTimings, nil)
	dexflow.Add(b, deployments.ReadinessResult, s.Readiness)
	dexflow.Add(b, deployments.DeploymentURL, s.URL)
	dexflow.Add(b, deployments.CleanupRequested, false)
	dexflow.Add(b, deployments.ReplacedLiveAttribute, s.ReplacedLive)
	dexflow.Add(b, deployments.RetirementAttribute, s.Retirement)
	dexflow.Add(b, deployments.CancellationAttribute, nil)
	dexflow.Add(b, deployments.FailureAttribute, nil)
	dexflow.Add(b, deployments.Revision, uint64(1))
	dexflow.Add(b, deployments.CreatedAt, time.Now().UTC())
	dexflow.Add(b, deployments.UpdatedAt, time.Now().UTC())

	return b.Values()
}

// startDeploymentPhase admits real state and returns the production handler client.
func startDeploymentPhase(ctx context.Context, t *testing.T, d *generationDeployer, snapshot deployments.Snapshot, project projects.Project) (*dex.Client, *deploymentPhaseFlow, *projects.ProjectFlowClient, string) {
	projectFlow := &deploymentPhaseProject{ProjectFlow: &projects.ProjectFlow{}}
	projectClient := &projects.ProjectFlowClient{Flow: projectFlow.ProjectFlow, ProjectIDLength: 4}
	deployClient := deployments.NewProjectDeployFlowClient(projectClient, d.serving, d.k)
	feedback := &agents.SessionFlowService{Logger: slog.Default()}
	deployFlow := &deploymentPhaseFlow{ProjectDeployFlow: deployments.NewProjectDeployFlow(projectClient, nil, deployClient, 0, nil, nil, nil, d.serving, d.k, d.k, d.k, d.k, feedback)}
	sessionFlow := &sessionagent.SessionAgentFlow{}
	client := dexflowtest.WorkerClientForTest(ctx, t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), projectFlow, deployFlow)
	realClient := dexflowtest.ClientForTest(t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), nil, projectFlow.ProjectFlow, deployFlow.ProjectDeployFlow, sessionFlow)
	projectClient.Dex = realClient
	feedback.Agent = sessionagent.NewSessionAgentFlowClientForTest(realClient, sessionFlow, nil, conversation.Model(""))
	projectID := flowid.ProjectFlowID(d.project)
	if _, err := client.StartFlow(ctx, projectFlow, projectID, project, dex.StartFlowOptions{Attributes: deploymentPhaseProjectAttributes(project), IDReusePolicy: dex.IDReuseDisallow}); err != nil {
		t.Fatal(err)
	}
	// Wait for the real retained Project RPC to observe its accepted lease.
	dockertest.EventuallyForTest(ctx, t, "fixture Project lease", func() error {
		p, err := projectClient.Read(ctx, d.project)
		if err != nil {
			return err
		}
		if p.ActiveDeployment == nil {
			return infra.ErrPending
		}
		return nil
	})
	id := comm.Must(deployments.FlowID(d.project, snapshot.DeploymentID))
	run, err := client.StartFlow(ctx, deployFlow, id, nil, dex.StartFlowOptions{Attributes: deploymentPhaseAttributes(snapshot), IDReusePolicy: dex.IDReuseDisallow})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("candidate failure FlowID=%s RunID=%s FlowType=deploymentPhaseFlow", id, run)

	return client, deployFlow, projectClient, id
}
