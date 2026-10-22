// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// A test-owned finalizer makes the exact old Worker deletion remain pending past
// the real twelve-minute production limit. No timeout or API is substituted.
func TestKindReplacedWorkerStopRecovery(t *testing.T) {
	f := newKindProjectFixture(t)
	f.k.Routes.LocalGatewayURL = "http://127.0.0.1:" + strconv.Itoa(f.k.Routes.LocalPort)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Minute)
	defer cancel()
	live := f.prepareStorage(ctx)
	f.prepareTemporal(ctx, live)
	f.prepareDex(ctx, live)
	old := f.deployGeneration(ctx, live, 1, "blocked-drain", false, 0)
	f.workerFinalizer(ctx, old, true)
	pod := f.holdWorkerPod(ctx, old)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		f.resourceFinalizer(cleanup, "pod", pod.Name, pod.UID, false)
		f.workerFinalizer(cleanup, old, false)
	})
	current := f.deployGeneration(ctx, live, 2, "2", false, 1)
	current.Lease.DeploymentID = current.DeploymentID
	current.Lease.ExpectedLiveRevision = 1
	current.Status = deployments.Deploying
	current.Providers.Phase = "StopReplacedWorker"
	current.Providers.Requests = []deployments.DeploymentRequestReceipt{{RequestID: "initial"}}
	current.ReplacedLive = deployments.ReplacedGeneration{DeploymentID: old.DeploymentID, Generation: 1}
	old.Status = deployments.Ready
	old.Providers.Phase = "ReleaseProjectDeployment"
	old.Grant.ID = infra.WorkloadName(old.DeploymentID, 1)
	old.ConfigurationRevision = 1
	project := projects.Project{ProjectID: f.project, ActiveDeployment: &current.Lease, CurrentLive: &projects.LivePointer{DeploymentID: current.DeploymentID, Revision: 2}, Storage: live.storage, LiveExecution: live.execution}
	projectFlow := &deploymentPhaseProject{ProjectFlow: &projects.ProjectFlow{}}
	projectClient := &projects.ProjectFlowClient{Flow: projectFlow.ProjectFlow, ProjectIDLength: 4}
	deployClient := deployments.NewProjectDeployFlowClient(projectClient, f.k, f.k.Infrastructure)
	feedback := &agents.SessionFlowService{Logger: slog.Default()}
	deployFlow := &deploymentPhaseFlow{ProjectDeployFlow: deployments.NewProjectDeployFlow(projectClient, nil, deployClient, 0, nil, nil, nil, f.k, f.k.Infrastructure, f.k.Infrastructure, f.k.Infrastructure, f.k.Infrastructure, feedback)}
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	client := dexflowtest.WorkerClientForTest(ctx, t, address, dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), projectFlow, deployFlow)
	sessionFlow := &sessionagent.SessionAgentFlow{}
	realClient := dexflowtest.ClientForTest(t, address, nil, projectFlow.ProjectFlow, deployFlow.ProjectDeployFlow, sessionFlow)
	projectClient.Dex = realClient
	feedback.Agent = sessionagent.NewSessionAgentFlowClientForTest(realClient, sessionFlow, nil, conversation.Model(""))
	if _, err := client.StartFlow(ctx, projectFlow, flowid.ProjectFlowID(f.project), project, dex.StartFlowOptions{Attributes: deploymentPhaseProjectAttributes(project), IDReusePolicy: dex.IDReuseDisallow}); err != nil {
		t.Fatal(err)
	}
	f.awaitReady(ctx, "fixture Project lease", func() error {
		p, err := projectClient.Read(ctx, f.project)
		if err != nil {
			return err
		}
		if p.ActiveDeployment == nil {
			return infra.ErrPending
		}
		return nil
	})
	oldID := comm.Must(deployments.FlowID(f.project, old.DeploymentID))
	if _, err := client.StartFlow(ctx, deployFlow, oldID, nil, dex.StartFlowOptions{Attributes: deploymentPhaseAttributes(old), IDReusePolicy: dex.IDReuseDisallow}); err != nil {
		t.Fatal(err)
	}
	f.awaitReady(ctx, "old deployment retained state", func() error {
		var s deployments.Snapshot
		err := client.InvokeRPC(ctx, oldID, deployFlow.GetProjectDeployment, nil, &s)
		if err != nil {
			return err
		}
		if s.Status != deployments.Ready {
			return infra.ErrPending
		}
		return nil
	})
	id := comm.Must(deployments.FlowID(f.project, current.DeploymentID))
	run, err := client.StartFlow(ctx, deployFlow, id, nil, dex.StartFlowOptions{Attributes: deploymentPhaseAttributes(current), IDReusePolicy: dex.IDReuseDisallow})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("replaced stop FlowID=%s RunID=%s FlowType=deploymentPhaseFlow", id, run)
	objects, err := f.k.S3(ctx, f.project, live.storage.Boundary.UID)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, err := objects.Get(ctx, appmanifest.ActiveFlowDefinitionManifestKey, "")
	if err != nil {
		t.Fatal(err)
	}
	host := comm.Must(url.Parse(f.k.Routes.URL(infra.ScopeLive, f.project))).Host
	started := time.Now()
	var failed deployments.Snapshot
	for {
		// The public generation and the authoritative pointer continue to serve throughout the full limit.
		f.callGeneration(ctx, current, http.MethodGet, "/healthz")
		if err := probeHealth(ctx, http.DefaultClient, f.k.Routes.LocalGatewayURL+current.Inputs.HealthPath, host); err != nil {
			t.Fatalf("N+1 public route stopped serving: %v", err)
		}
		route, err := f.k.readLiveRoute(ctx, f.project)
		if err != nil || !ownedByGeneration(route, current) {
			t.Fatalf("postcommit stop moved the HTTP route: %v", err)
		}
		active, _, err := objects.Get(ctx, appmanifest.ActiveFlowDefinitionManifestKey, "")
		if err != nil || !bytes.Equal(active, manifest) {
			t.Fatalf("postcommit stop changed the active FDG: %v", err)
		}

		p, err := projectClient.Read(ctx, f.project)
		if err != nil || p.CurrentLive == nil || p.CurrentLive.DeploymentID != current.DeploymentID || p.ActiveDeployment == nil || *p.ActiveDeployment != current.Lease {
			t.Fatalf("postcommit stop moved Live or released the lease: %v", err)
		}
		var previous deployments.Snapshot
		if err := client.InvokeRPC(ctx, oldID, deployFlow.GetProjectDeployment, nil, &previous); err != nil {
			t.Fatal(err)
		}
		if previous.Retirement.RequestID != "" || previous.Status != deployments.Ready {
			t.Fatal("retirement started before exact Worker absence")
		}
		err = client.InvokeRPC(ctx, id, deployFlow.GetProjectDeployment, nil, &failed)
		if err == nil && failed.Status == deployments.Failed {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
	if time.Since(started) < 12*time.Minute {
		t.Fatal("stop did not use its actual twelve-minute limit")
	}
	if failed.Failure == nil || failed.Failure.Phase != "StopReplacedWorker" || !strings.Contains(failed.Failure.Message, old.DeploymentID) {
		t.Fatalf("stop failure did not name the replaced Worker: %+v", failed.Failure)
	}
	// The Pod finalizer preserves the actual termination result after the eleven-minute forced exit.
	args := []string{"--kubeconfig", f.kubeconfig, "--context", "kind-superverse-v2", "-n", f.k.ProjectNamespace(f.project), "get", "pod", pod.Name, "-o", "json"}
	data, err := exec.CommandContext(ctx, "kubectl", args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	var terminated struct {
		Metadata struct{ UID string }
		Status   struct {
			ContainerStatuses []struct {
				State struct{ Terminated *struct{ ExitCode int } }
			}
		}
	}
	if err := json.Unmarshal(data, &terminated); err != nil {
		t.Fatal(err)
	}
	if terminated.Metadata.UID != pod.UID || len(terminated.Status.ContainerStatuses) != 1 || terminated.Status.ContainerStatuses[0].State.Terminated == nil || terminated.Status.ContainerStatuses[0].State.Terminated.ExitCode != 137 {
		t.Fatal("old Worker did not record its forced exit after the real Pod grace")
	}
	f.resourceFinalizer(ctx, "pod", pod.Name, pod.UID, false)
	f.workerFinalizer(ctx, old, false)
	var resumed deployments.MutationResultForTest
	if err := client.InvokeRPC(ctx, id, deployFlow.RequestLiveDeployment, deployments.MutationForTest{RequestID: "retry-stop", ExpectedRevision: failed.Revision}, &resumed); err != nil {
		t.Fatal(err)
	}
	if resumed.Rejection != nil {
		t.Fatalf("exact recovery rejected: %+v", resumed.Rejection)
	}
	f.awaitReady(ctx, "new deployment recovery", func() error {
		var s deployments.Snapshot
		err := client.InvokeRPC(ctx, id, deployFlow.GetProjectDeployment, nil, &s)
		if err != nil {
			return err
		}
		if s.Status != deployments.Ready {
			return infra.ErrPending
		}
		return nil
	})
	f.awaitReady(ctx, "replaced retirement", func() error {
		var s deployments.Snapshot
		err := client.InvokeRPC(ctx, oldID, deployFlow.GetProjectDeployment, nil, &s)
		if err != nil {
			return err
		}
		if s.Status != deployments.Retired {
			return infra.ErrPending
		}
		return nil
	})
	f.callGeneration(ctx, current, http.MethodGet, "/healthz")
	t.Log("full stop limit exhausted while N+1 served; failure retained lease, exact retry stopped N then retired it")
}

const stopTestFinalizer = "superdurable.io/pulllive-stop-test"

// workerFinalizer mutates only the fixture's exact accepted Deployment, preserving all other finalizers.
func (f *kindProjectFixture) workerFinalizer(ctx context.Context, s deployments.Snapshot, hold bool) {
	f.resourceFinalizer(ctx, "deployment", s.Resources.Worker.Name, s.Resources.Worker.UID, hold)
}

func (f *kindProjectFixture) resourceFinalizer(ctx context.Context, kind, name, uid string, hold bool) {
	f.test.Helper()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	api := &kubeapi.Client{ServerURL: dexflowtest.Setting(f.test, "SUPERVERSE_TEST_KUBERNETES_API"), HTTP: http.DefaultClient}
	path := f.k.NamespacePath(f.project) + "/pods/" + name
	if kind == "deployment" {
		path = "/apis/apps/v1/namespaces/" + f.k.ProjectNamespace(f.project) + "/deployments/" + name
	}
	for {
		object, code, err := api.Call(ctx, http.MethodGet, path, nil)
		if err != nil {
			f.test.Fatal(fmt.Errorf("read Worker test finalizer: %w", err))
		}
		if code == http.StatusNotFound && !hold {
			return
		}
		if kubeapi.UID(object) != uid {
			f.test.Fatal("refusing finalizer mutation of another Worker")
		}
		metadata := kubeapi.Child(object, "metadata")
		values := []any{}
		current, _ := metadata["finalizers"].([]any)
		for _, value := range current {
			if value != stopTestFinalizer {
				values = append(values, value)
			}
		}
		if hold {
			values = append(values, stopTestFinalizer)
		}
		metadata["finalizers"] = values
		_, code, err = api.Call(ctx, http.MethodPut, path, object)
		if code == http.StatusNotFound && !hold || err == nil {
			return
		}
		if code != http.StatusConflict {
			f.test.Fatal(fmt.Errorf("write Worker test finalizer: %w", err))
		}
		select {
		case <-ctx.Done():
			f.test.Fatal(fmt.Errorf("Worker test finalizer conflicts: %w", err))
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (f *kindProjectFixture) holdWorkerPod(ctx context.Context, s deployments.Snapshot) infra.ResourceRef {
	f.test.Helper()
	selector := applicationProcessLabel + "=worker," + kubeapi.WorkloadIDLabel + "=" + s.DeploymentID
	args := []string{"--kubeconfig", f.kubeconfig, "--context", "kind-superverse-v2", "-n", f.k.ProjectNamespace(f.project), "get", "pods", "-l", selector, "-o", "json"}
	data, err := exec.CommandContext(ctx, "kubectl", args...).Output()
	if err != nil {
		f.test.Fatal(err)
	}
	var list struct{ Items []kubeapi.Object }
	if err := json.Unmarshal(data, &list); err != nil {
		f.test.Fatal(err)
	}
	if len(list.Items) != 1 || !ownedByGeneration(list.Items[0], s) {
		f.test.Fatal("Worker Pod ownership was not exact")
	}
	metadata := kubeapi.Child(list.Items[0], "metadata")
	ref := infra.ResourceRef{Name: kubeapi.String(metadata, "name"), UID: kubeapi.String(metadata, "uid"), Generation: s.Lease.Generation}
	f.resourceFinalizer(ctx, "pod", ref.Name, ref.UID, true)
	return ref
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
