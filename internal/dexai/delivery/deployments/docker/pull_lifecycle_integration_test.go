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
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/docker/dockertest"
)

func testArchive(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	if e := w.WriteHeader(&tar.Header{Name: name, Mode: 0444, Size: int64(len(body))}); e != nil {
		t.Fatal(e)
	}
	if _, e := w.Write(body); e != nil {
		t.Fatal(e)
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}

func TestDockerPullDrainCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	d := candidateDeployer(ctx, t)
	old := d.deploy(ctx, 1, "drain-1", false, 0)
	// runtime.yaml is the real configuration owner; this generation uses a five-second drain.
	uid := old.snapshot.Resources.Worker.UID
	if e := d.k.API().StopContainer(ctx, uid, time.Minute); e != nil {
		t.Fatal(e)
	}
	runtime := bytes.ReplaceAll(deployments.ApplicationRuntime(), []byte("stopDrainTimeout: 10m"), []byte("stopDrainTimeout: 5s"))
	if bytes.Equal(runtime, deployments.ApplicationRuntime()) {
		t.Fatal("runtime drain configuration was not changed")
	}
	if e := d.k.API().CopyToContainer(ctx, uid, "/tmp/superverse-app", testArchive(t, "runtime.yaml", runtime)); e != nil {
		t.Fatal(e)
	}
	if e := d.k.API().StartContainer(ctx, uid); e != nil {
		t.Fatal(e)
	}
	dockertest.EventuallyForTest(ctx, t, "configured old Worker", func() error { _, e := d.serving.VerifyReadiness(ctx, old.snapshot); return e })
	id := "drain-" + flowid.RandomID(8)
	old.call(ctx, http.MethodPost, "/drain/"+id)
	var effectVersion string
	dockertest.EventuallyForTest(ctx, t, "external effect before handover", func() error {
		logs, e := d.k.API().ContainerLogs(ctx, uid, 100, 32768)
		if e != nil {
			return e
		}
		match := regexp.MustCompile(`DRAIN_EFFECT_READY flow=` + id + ` version=([^\s]+)`).FindStringSubmatch(logs)
		if len(match) != 2 {
			return infra.ErrPending
		}
		effectVersion = match[1]
		return nil
	})
	next := d.deploy(ctx, 2, "drain-2", false, 1)
	// Keep a private real Docker log stream through removal to observe handler cancellation.
	logFile, e := os.CreateTemp(t.TempDir(), "worker-log-")
	if e != nil {
		t.Fatal(e)
	}
	defer logFile.Close()
	command := exec.CommandContext(ctx, "docker", "logs", "--follow", uid)
	command.Stdout = logFile
	command.Stderr = logFile
	if e := command.Start(); e != nil {
		t.Fatal(e)
	}
	defer command.Wait()
	s := next.snapshot
	s.Lease.DeploymentID = s.DeploymentID
	s.Lease.ExpectedLiveRevision = 1
	s.Status = deployments.Deploying
	s.Providers.Phase = "StopReplacedWorker"
	s.ReplacedLive = deployments.ReplacedGeneration{DeploymentID: old.snapshot.DeploymentID, Generation: 1}
	p := projects.Project{ProjectID: d.project, ActiveDeployment: &s.Lease, CurrentLive: &projects.LivePointer{DeploymentID: s.DeploymentID, Revision: 2}, Storage: d.storage, LiveExecution: d.execution}
	client, flow, _, phaseID := startDeploymentPhase(ctx, t, d, s, p)
	previous := old.snapshot
	previous.Status = deployments.Ready
	previous.Providers.Phase = "ReleaseProjectDeployment"
	previous.Grant.ID = infra.WorkloadName(previous.DeploymentID, 1)
	oldID := comm.Must(deployments.FlowID(d.project, previous.DeploymentID))
	if _, e := client.StartFlow(ctx, flow, oldID, nil, dex.StartFlowOptions{Attributes: deploymentPhaseAttributes(previous), IDReusePolicy: dex.IDReuseDisallow}); e != nil {
		t.Fatal(e)
	}
	var state struct {
		By            string
		Attempt       int32
		EffectVersion string
	}
	dockertest.EventuallyForTest(ctx, t, "drained Step retry", func() error {
		body, status, e := next.request(ctx, http.MethodGet, "/drain/"+id)
		if e != nil {
			return e
		}
		if status != http.StatusOK {
			return infra.ErrPending
		}
		if e = json.Unmarshal(body, &state); e != nil {
			return e
		}
		if state.By == "" {
			return infra.ErrPending
		}
		return nil
	})
	if state.By != "drain-2" || state.Attempt != 2 || state.EffectVersion != effectVersion {
		t.Fatalf("retry lost external-effect identity: %+v, original=%s", state, effectVersion)
	}
	dockertest.EventuallyForTest(ctx, t, "old Worker absent", func() error {
		c, e := d.k.API().InspectContainer(ctx, uid)
		if e != nil {
			return e
		}
		if c != nil {
			return infra.ErrPending
		}
		return nil
	})
	data, e := os.ReadFile(logFile.Name())
	if e != nil || !bytes.Contains(data, []byte("DRAIN_CANCELLED flow="+id)) {
		t.Fatalf("handler cancellation was not observed: %v", e)
	}
	dockertest.EventuallyForTest(ctx, t, "production lease release after stop", func() error {
		var state deployments.Snapshot
		e := client.InvokeRPC(ctx, phaseID, flow.GetProjectDeployment, nil, &state)
		if e != nil {
			return e
		}
		if state.Status != deployments.Ready {
			return infra.ErrPending
		}
		return nil
	})
	dockertest.EventuallyForTest(ctx, t, "old generation retirement", func() error {
		var state deployments.Snapshot
		e := client.InvokeRPC(ctx, oldID, flow.GetProjectDeployment, nil, &state)
		if e != nil {
			return e
		}
		if state.Status != deployments.Retired {
			return infra.ErrPending
		}
		return nil
	})
	t.Logf("Flow %s: five-second configured drain cancelled N; attempt 2 on N+1 reused external effect version %s", id, effectVersion)
}

func TestDockerActivationReentry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	d := candidateDeployer(ctx, t)
	old := d.deploy(ctx, 1, "1", false, 0)
	next := d.prepare(ctx, 2, "2", false, 1, false)
	objects, e := d.k.S3(ctx, d.project, d.storage.Boundary.UID)
	if e != nil {
		t.Fatal(e)
	}
	publication, e := deployments.ReadPublication(ctx, d.k, next.snapshot)
	if e != nil {
		t.Fatal(e)
	}
	body := comm.Must(json.Marshal(publication.Manifest))
	// Reproduce an uncertain return after the guarded FDG write, before the HTTP route write.
	_, e = objects.PutGuarded(ctx, appmanifest.ActiveFlowDefinitionManifestKey, body, false, func() error {
		route, ok, e := d.k.Routes().Read(d.k.PublicRoutes().Host(infra.ScopeLive, d.project))
		if e != nil {
			return e
		}
		if !ok || route.Generation != 1 || route.Owner.DeploymentID != old.snapshot.DeploymentID {
			return errors.New("old route changed before activation")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	old.call(ctx, http.MethodGet, "/healthz")
	for range 2 {
		dockertest.EventuallyForTest(ctx, t, "activation re-entry", func() error { _, e := d.serving.Activate(ctx, next.snapshot); return e })
	}
	active, _, e := objects.Get(ctx, appmanifest.ActiveFlowDefinitionManifestKey, "")
	if e != nil || !bytes.Equal(active, body) {
		t.Fatalf("FDG did not converge: %v", e)
	}
	route, ok, e := d.k.Routes().Read(d.k.PublicRoutes().Host(infra.ScopeLive, d.project))
	if e != nil || !ok || route.Generation != 2 || route.Owner.DeploymentID != next.snapshot.DeploymentID {
		t.Fatalf("route did not converge: %v", e)
	}
	if _, e = d.serving.Activate(ctx, old.snapshot); e == nil {
		t.Fatal("stale generation regressed activation")
	}
	after, _, e := objects.Get(ctx, appmanifest.ActiveFlowDefinitionManifestKey, "")
	if e != nil || !bytes.Equal(after, body) {
		t.Fatal("stale re-entry changed FDG")
	}
	next.call(ctx, http.MethodGet, "/healthz")
	old.call(ctx, http.MethodGet, "/healthz")
	t.Log("guarded FDG write before route, repeated activation and stale activation converged without regression")
}

// Restrict only this disposable Project Dex; config bytes (which contain storage credentials) stay in memory.
func restrictProjectFlowTypes(ctx context.Context, t *testing.T, d *generationDeployer) {
	t.Helper()
	owned, e := d.k.OwnedContainer(ctx, d.execution.DexService.Name, d.k.OwnerLabels(d.project, infra.ComponentDex))
	if e != nil || owned == nil {
		t.Fatalf("read owned Project Dex: %v", e)
	}
	uid := owned.ID
	command := exec.CommandContext(ctx, "docker", "cp", uid+":/etc/dex/config.yaml", "-")
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	archive, e := command.Output()
	if e != nil {
		t.Fatalf("read owned Dex configuration: %v: %s", e, diagnostic.String())
	}
	reader := tar.NewReader(bytes.NewReader(archive))
	if _, e = reader.Next(); e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(reader)
	if e != nil {
		t.Fatal(e)
	}
	var config map[string]any
	if e = json.Unmarshal(data, &config); e != nil {
		t.Fatal("read private Project Dex configuration")
	}
	config["matching"].(map[string]any)["maxFlowTypes"] = 2
	changed, e := json.Marshal(config)
	if e != nil {
		t.Fatal("encode private Project Dex configuration")
	}
	if e = d.k.API().StopContainer(ctx, uid, time.Minute); e != nil {
		t.Fatal(e)
	}
	if e = d.k.API().CopyToContainer(ctx, uid, "/etc/dex", testArchive(t, "config.yaml", changed)); e != nil {
		t.Fatal(e)
	}
	if e = d.k.API().StartContainer(ctx, uid); e != nil {
		t.Fatal(e)
	}
}

func TestDockerPermanentPullRejection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 18*time.Minute)
	defer cancel()
	d := candidateDeployer(ctx, t)
	old := d.deploy(ctx, 1, "1", false, 0)
	restrictProjectFlowTypes(ctx, t, d)
	dockertest.EventuallyForTest(ctx, t, "old Worker reconnect", func() error { _, e := d.serving.VerifyReadiness(ctx, old.snapshot); return e })
	d.skipReadiness = true
	candidate := d.prepare(ctx, 2, "rejected", false, 1, false)
	dockertest.EventuallyForTest(ctx, t, "permanent Worker rejection", func() error {
		worker, e := d.k.API().InspectContainer(ctx, candidate.snapshot.Resources.Worker.UID)
		if e != nil {
			return e
		}
		if worker == nil {
			return fmt.Errorf("candidate disappeared before cleanup")
		}
		logs, e := d.k.API().ContainerLogs(ctx, worker.ID, 100, 32768)
		if e != nil {
			return e
		}
		if !strings.Contains(logs, "ResourceExhausted") && !strings.Contains(logs, "maxFlowTypes") && !strings.Contains(logs, "flow type") && !strings.Contains(logs, "flow_types") {
			return infra.ErrPending
		}
		if worker.State.Running && !worker.State.Restarting {
			return infra.ErrPending
		}
		return nil
	})
	s := candidate.snapshot
	s.Lease.DeploymentID = s.DeploymentID
	s.Lease.ExpectedLiveRevision = 1
	s.Status = deployments.Deploying
	s.Providers.Phase = "VerifyReadiness"
	s.ReplacedLive = deployments.ReplacedGeneration{DeploymentID: old.snapshot.DeploymentID, Generation: 1}
	p := projects.Project{ProjectID: d.project, ActiveDeployment: &s.Lease, CurrentLive: &projects.LivePointer{DeploymentID: old.snapshot.DeploymentID, Revision: 1}, Storage: d.storage, LiveExecution: d.execution}
	client, flow, projectClient, id := startDeploymentPhase(ctx, t, d, s, p)
	awaitCandidateFailure(ctx, t, client, flow, id, func(_ deployments.Snapshot) {
		worker, e := d.k.API().InspectContainer(ctx, s.Resources.Worker.UID)
		if e != nil || worker != nil {
			t.Fatalf("permanently rejected Worker survived failure: %v", e)
		}
	})
	current, e := projectClient.Read(ctx, d.project)
	if e != nil || current.CurrentLive == nil || current.CurrentLive.DeploymentID != old.snapshot.DeploymentID {
		t.Fatal("permanent rejection changed Live")
	}
	old.call(ctx, http.MethodGet, "/healthz")
	d.requireWorker(ctx, old, true)
	for range 3 {
		id := "kept-" + flowid.RandomID(8)
		old.call(ctx, http.MethodPost, "/probes/"+id)
		old.awaitProbe(ctx, id, func(p probe) bool { return p.StartedBy == "1" })
	}
	t.Log("permanent registration rejection exited candidate; production readiness/failure path removed it and retained N")
}
