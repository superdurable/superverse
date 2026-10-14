// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/projects/infra/kube"
	"github.com/superdurable/superversev2/internal/projects/infra/kube/connect"
)

// kindWorkerRefresh is longer than the interval at which the project Dex
// resolves the Live Worker endpoint again.
const kindWorkerRefresh = 5 * time.Second

// Three versions of the generation application (package generationapp) run
// as consecutive Live generations of one project on its own MinIO, Temporal
// and Live Dex in Local Kind, as the server deploys them. Readiness requires
// each generation's Worker to accept connections, and activation moves the
// headless Live Worker Service to it before the route. A Flow that generation
// 1 started runs its next Step on generation 2 as soon as the Service selects
// generation 2, while generation 1 still runs: the project Dex follows the
// endpoint, not an established connection. Generation 3 no longer registers
// the Step a Flow waits in, which then takes its failure route within the
// Step's retry budget. Removing the Live generation removes the Service.
func TestKindLiveWorkerFollowsGeneration(t *testing.T) {
	fixture := newKindProjectFixture(t)
	// Activation probes the public route through the gateway, which the lane
	// forwards to this machine on the routes' port.
	fixture.k.Routes.LocalGatewayURL = "http://127.0.0.1:" + strconv.Itoa(fixture.k.Routes.LocalPort)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	live := fixture.prepareStorage(ctx)
	fixture.prepareTemporal(ctx, live)
	fixture.prepareDex(ctx, live)

	first := fixture.deployGeneration(ctx, live, 1, "1", false, 0)
	fixture.requireWorker(ctx, live, first, true)
	carried := "probe-" + flowid.RandomID(8)
	fixture.callGeneration(ctx, first, http.MethodPost, "/probes/"+carried)
	fixture.awaitProbe(ctx, first, carried, func(p kindProbe) bool { return p.StartedBy == "1" && p.Outcome == "waiting" })

	second := fixture.deployGeneration(ctx, live, 2, "2", false, 1)
	fixture.requireWorker(ctx, live, second, true)
	fixture.requireWorker(ctx, live, first, false)
	if _, ready, err := fixture.k.DeploymentReady(ctx, fixture.project, infra.WorkloadName(first.DeploymentID, 1), infrakube.ComponentApplication, first.Providers.Application.UID); err != nil || !ready {
		t.Fatalf("generation 1 is not running beside generation 2: %v", err)
	}
	time.Sleep(kindWorkerRefresh)
	fixture.callGeneration(ctx, second, http.MethodPost, "/probes/"+carried+"/release")
	fixture.awaitProbe(ctx, second, carried, func(p kindProbe) bool { return p.Outcome == "completed" && p.ReleasedBy == "2" })

	removed := "probe-" + flowid.RandomID(8)
	fixture.callGeneration(ctx, second, http.MethodPost, "/probes/"+removed)
	fixture.awaitProbe(ctx, second, removed, func(p kindProbe) bool { return p.StartedBy == "2" && p.Outcome == "waiting" })
	fixture.cleanupGeneration(ctx, first)

	third := fixture.deployGeneration(ctx, live, 3, "3", true, 2)
	fixture.cleanupGeneration(ctx, second)
	time.Sleep(kindWorkerRefresh)
	released := time.Now()
	fixture.callGeneration(ctx, third, http.MethodPost, "/probes/"+removed+"/release")
	budget, stop := context.WithTimeout(ctx, 3*time.Minute)
	defer stop()
	fixture.awaitProbe(budget, third, removed, func(p kindProbe) bool { return p.Outcome == "failure-route:3" })
	t.Logf("the Flow left the removed Step through its failure route %s after its release", time.Since(released).Round(time.Second))

	project := projects.Project{ProjectID: fixture.project, Storage: live.storage, CurrentLive: &projects.LivePointer{DeploymentID: third.DeploymentID, Revision: 3}}
	fixture.awaitReady(ctx, "the Live removal", func() error {
		err := fixture.k.RemoveServing(ctx, project)
		if errors.Is(err, projects.ErrCleanupPending) {
			return errors.Join(infra.ErrPending, err)
		}
		return err
	})
	endpoint, _, err := fixture.k.API.Call(ctx, http.MethodGet, fixture.k.workerEndpointPath(fixture.project), nil)
	if err != nil || endpoint != nil && !kubeapi.Deleting(endpoint) {
		t.Fatalf("the Live removal left the Live Worker Service: %v", err)
	}
	fixture.destroyLive(ctx, live)
}

// deployGeneration builds version, deploys it as generation n replacing the
// Live generation expected and activates it, as ProjectDeployFlow's phases do.
func (f *kindProjectFixture) deployGeneration(ctx context.Context, live *kindProjectScope, n uint64, version string, removeAwait bool, expected uint64) deployments.Snapshot {
	t := f.test
	t.Helper()
	image := kindGenerationImage(ctx, t, version, removeAwait)
	s := deployments.Snapshot{ProjectID: f.project, DeploymentID: flowid.RandomID(8), ReleaseID: "release-" + version, BuildRevisionID: "build-" + uuid.NewString(),
		ExpectedLiveRevision: expected, Lease: projects.DeploymentLease{Generation: n},
		Inputs:    deployments.Inputs{ApplicationPort: 8080, HealthPath: "/healthz", ImageReference: image, ImageDigest: "sha256:" + strings.Repeat("0", 64), BundleDigest: "sha256:" + flowid.RandomID(64)},
		Resources: deployments.DeploymentResources{Storage: live.storage, Execution: live.execution}}
	definition := appmanifest.FlowDefinition{FlowType: "GenerationProbeFlow", SourcePath: "main.go", SchemaVersion: "2.0", Digest: "sha256:" + flowid.RandomID(64), Valid: true, Graph: json.RawMessage(`{}`)}
	if err := deployments.PublishFlowDefinitionsForTest(ctx, f.k.Infrastructure, s, []appmanifest.FlowDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	f.awaitReady(ctx, "the generation Service", func() (err error) {
		s.Resources.Service, err = f.k.EnsureGenerationService(ctx, s)
		return err
	})
	grant := f.liveGrant(ctx, s)
	f.awaitReady(ctx, "the generation grant", func() error { return f.k.IssueWorkload(ctx, grant) })
	environment, err := f.k.GrantEnvironment(ctx, grant)
	if err != nil {
		t.Fatal(err)
	}
	f.awaitReady(ctx, "the generation application", func() (err error) {
		s.Providers.Application, err = f.k.ApplyApplication(ctx, s, environment)
		return err
	})
	f.awaitReady(ctx, "the readiness of generation "+version, func() error {
		_, err := f.k.VerifyReadiness(ctx, s)
		return err
	})
	var activation deployments.Activation
	f.awaitReady(ctx, "the activation of generation "+version, func() (err error) {
		activation, err = f.k.Activate(ctx, s)
		return err
	})
	if activation.Worker.Name != infra.LiveWorkerEndpoint || activation.Worker.UID == "" || activation.Manifest.Version == "" {
		t.Fatalf("unexpected activation %+v", activation)
	}
	s.Resources.Worker = activation.Worker
	return s
}

// liveGrant stores an empty configuration head of the project and returns the
// grant of the deployment's generation.
func (f *kindProjectFixture) liveGrant(ctx context.Context, s deployments.Snapshot) infra.Workload {
	f.test.Helper()
	objects, err := f.k.S3(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID)
	if err != nil {
		f.test.Fatal(err)
	}
	live := infra.Scope{Kind: infra.ScopeLive}
	head, err := objects.Put(ctx, infra.ConfigurationKeyForTest(s.ProjectID, live, "configuration/head"), []byte(`{"environment":{}}`), false)
	if err != nil {
		f.test.Fatal(err)
	}
	return infra.Workload{ProjectID: s.ProjectID, WorkloadID: s.DeploymentID, Scope: live, Generation: s.Lease.Generation, ConfigurationRevision: 1,
		Storage: s.Resources.Storage, Configuration: head, GrantID: infra.WorkloadName(s.DeploymentID, s.Lease.Generation)}
}

// requireWorker requires the Live Worker Service to select the generation, or
// not.
func (f *kindProjectFixture) requireWorker(ctx context.Context, live *kindProjectScope, s deployments.Snapshot, selected bool) {
	f.test.Helper()
	connections := &connect.Connections{Infrastructure: f.k.Infrastructure}
	err := connections.CheckWorker(ctx, infra.WorkerTarget{ProjectID: f.project, Namespace: live.storage.Boundary, Controller: s.Providers.Application, Service: s.Resources.Service, DeploymentID: s.DeploymentID, Generation: s.Lease.Generation})
	if (err == nil) != selected {
		f.test.Fatalf("the Live Worker Service selects generation %d: %t, want %t (%v)", s.Lease.Generation, err == nil, selected, err)
	}
}

// cleanupGeneration revokes a replaced generation's grant and removes it, as
// its retirement does.
func (f *kindProjectFixture) cleanupGeneration(ctx context.Context, s deployments.Snapshot) {
	f.test.Helper()
	f.awaitReady(ctx, "the replaced generation's grant revocation", func() error { return f.k.RevokeWorkload(ctx, f.liveGrant(ctx, s)) })
	f.awaitReady(ctx, "the replaced generation's removal", func() error {
		err := f.k.CleanupDeployment(ctx, s)
		if errors.Is(err, projects.ErrCleanupPending) {
			return errors.Join(infra.ErrPending, err)
		}
		return err
	})
}

// kindProbe is what the generation application reads of one probe Flow.
type kindProbe struct {
	StartedBy, ReleasedBy, Outcome, ReadBy string
}

func (f *kindProjectFixture) requestGeneration(ctx context.Context, s deployments.Snapshot, method, path string) ([]byte, int, error) {
	address := kubeapi.ServiceAddress(f.k.ProjectNamespace(f.project), infra.WorkloadName(s.DeploymentID, s.Lease.Generation), 8080)
	request, err := http.NewRequestWithContext(ctx, method, "http://"+address+path, nil)
	if err != nil {
		return nil, 0, err
	}
	response, err := f.k.ServiceHTTP().Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	return body, response.StatusCode, err
}

// callGeneration sends one request to the generation's application and fails
// the test unless it succeeds.
func (f *kindProjectFixture) callGeneration(ctx context.Context, s deployments.Snapshot, method, path string) {
	f.test.Helper()
	body, status, err := f.requestGeneration(ctx, s, method, path)
	if err != nil || status != http.StatusOK {
		f.test.Fatalf("%s %s on generation %d: %d %s %v", method, path, s.Lease.Generation, status, body, err)
	}
}

// awaitProbe reads the probe Flow id through the generation's application
// until done holds.
func (f *kindProjectFixture) awaitProbe(ctx context.Context, s deployments.Snapshot, id string, done func(kindProbe) bool) {
	f.test.Helper()
	var last kindProbe
	var detail string
	for {
		body, status, err := f.requestGeneration(ctx, s, http.MethodGet, "/probes/"+id)
		if err == nil && status == http.StatusOK && json.Unmarshal(body, &last) == nil && done(last) {
			return
		}
		detail = strings.TrimSpace(fmt.Sprintf("%d %s %v", status, body, err))
		select {
		case <-ctx.Done():
			f.test.Fatalf("probe %s reads %+v (%s): %v", id, last, detail, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// kindGenerationImage builds the generation application as version for the
// Kind node's architecture, loads it into the Kind nodes and returns its
// reference. The Pod mounts its own /tmp, so the image is the binary alone.
func kindGenerationImage(ctx context.Context, t *testing.T, version string, removeAwait bool) string {
	t.Helper()
	directory := t.TempDir()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", filepath.Join(directory, "generationapp"), //nolint:gosec // the Go toolchain building the repository's test application
		"-ldflags", "-X main.version="+version+" -X main.removeAwait="+strconv.FormatBool(removeAwait), "./internal/delivery/deployments/generationapp")
	build.Dir = root
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0", "GOWORK=off")
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build the generation application: %v: %s", buildErr, output)
	}
	if err = os.WriteFile(filepath.Join(directory, "Dockerfile"), []byte("FROM scratch\nCOPY --chmod=0755 generationapp /generationapp\nENTRYPOINT [\"/generationapp\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tag := "sv2-test-generationapp:" + flowid.RandomID(8)
	for _, arguments := range [][]string{{"docker", "build", "--quiet", "--tag", tag, directory}, {"kind", "load", "docker-image", tag, "--name", "superverse-v2"}} {
		if output, runErr := exec.CommandContext(ctx, arguments[0], arguments[1:]...).CombinedOutput(); runErr != nil { //nolint:gosec // docker and kind with the test's own arguments
			t.Fatalf("%s: %v: %s", strings.Join(arguments[:3], " "), runErr, output)
		}
	}
	t.Cleanup(func() { _ = exec.CommandContext(context.WithoutCancel(ctx), "docker", "image", "rm", tag).Run() }) //nolint:gosec // the tag built above
	return tag
}
