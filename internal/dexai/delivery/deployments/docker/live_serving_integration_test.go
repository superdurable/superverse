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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	deploymentsdocker "github.com/superdurable/superversev2/internal/dexai/delivery/deployments/docker"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/docker/dockertest"
)

// applicationPort is the port the test application listens on.
const applicationPort = 3000

// workerRefresh is longer than the interval at which the project Dex resolves
// the Live Worker endpoint again.
const workerRefresh = 5 * time.Second

// Requires the local Docker Engine. Three versions of the generation
// application (package generationapp) are pushed to a local registry as the
// Native builder pushes a Release image and run as consecutive Live
// generations of one project on its own Temporal and Live Dex, as the server
// deploys them: the generation's network, the grant's environment, the
// container of the pinned image, readiness through its published ports,
// including its Worker, and activation of the Live Worker alias, the published
// Flow Definitions and the Live route. The diagnostics read the application's
// log and environment and refuse another application. A Flow that generation 1
// started runs its next Step on generation 2 as soon as generation 2 holds the
// alias, while generation 1 still runs. Generation 3 no longer registers the
// Step a Flow waits in, which then takes its failure route within the Step's
// retry budget. The removal of the Live generation takes the alias with it, and
// the project's deletion leaves nothing behind.
func TestDockerLiveDeployment(t *testing.T) {
	k := dockertest.InfrastructureForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	project := flowid.RandomID(4)
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, k, project) })
	var storage infra.StorageRefs
	dockertest.EventuallyForTest(ctx, t, "the project storage", func() (err error) {
		storage, err = k.EnsureStorage(ctx, project, storage)
		return err
	})
	var execution infra.ExecutionRefs
	dockertest.EventuallyForTest(ctx, t, "the project Temporal", func() (err error) {
		execution, err = k.EnsureTemporal(ctx, project, storage, execution)
		return err
	})
	if err := k.EnsureExecutionAccess(ctx, project, storage); err != nil {
		t.Fatal(err)
	}
	dockertest.EventuallyForTest(ctx, t, "the project Dex", func() (err error) {
		execution, err = k.StartProjectDex(ctx, project, storage, execution)
		return err
	})
	registry := dockertest.RegistryForTest(ctx, t, k)
	deployer := &generationDeployer{t: t, k: k, serving: &deploymentsdocker.LiveServing{Infrastructure: k}, registry: registry, project: project, storage: storage, execution: execution}

	first := deployer.deploy(ctx, 1, "1", false, 0)
	read, err := deployer.serving.ReadApplication(ctx, first.snapshot)
	if err != nil {
		t.Fatalf("read the application: %v", err)
	}
	log, err := read.Log(ctx, 50)
	if err != nil || log.State != "running" {
		t.Fatalf("the application log reads %q: %v", log.State, err)
	}
	values, err := read.Environment(ctx)
	if err != nil || !slices.Contains(values, first.environment[infra.SecretAccessKeyVariable]) {
		t.Fatalf("the diagnostics would not redact the grant's secret: %v", err)
	}
	replaced := first.snapshot
	replaced.Providers.Application.UID = "replaced-application"
	if _, err = deployer.serving.ReadApplication(ctx, replaced); !errors.Is(err, deployments.ErrApplicationChanged) {
		t.Fatalf("another application was read: %v", err)
	}
	deployer.requireWorker(ctx, first, true)
	carried := "probe-" + flowid.RandomID(8)
	first.call(ctx, http.MethodPost, "/probes/"+carried)
	first.awaitProbe(ctx, carried, func(p probe) bool { return p.StartedBy == "1" && p.Outcome == "waiting" })

	// Generation 2 takes the Worker alias while generation 1 still runs, so the
	// project Dex runs the waiting Flow's next Step on generation 2.
	second := deployer.deploy(ctx, 2, "2", false, 1)
	deployer.requireWorker(ctx, second, true)
	deployer.requireWorker(ctx, first, false)
	if _, err = k.API().InspectContainer(ctx, first.snapshot.Providers.Application.UID); err != nil {
		t.Fatalf("generation 1 is not running beside generation 2: %v", err)
	}
	time.Sleep(workerRefresh)
	second.call(ctx, http.MethodPost, "/probes/"+carried+"/release")
	second.awaitProbe(ctx, carried, func(p probe) bool { return p.Outcome == "completed" && p.ReleasedBy == "2" })

	removed := "probe-" + flowid.RandomID(8)
	second.call(ctx, http.MethodPost, "/probes/"+removed)
	second.awaitProbe(ctx, removed, func(p probe) bool { return p.StartedBy == "2" && p.Outcome == "waiting" })
	deployer.cleanup(ctx, first)

	// Generation 3 no longer registers AwaitRelease: the waiting Flow fails
	// that Step's attempts there and takes its failure route within the Step's
	// 45-second retry budget, never retrying for good.
	third := deployer.deploy(ctx, 3, "3", true, 2)
	deployer.cleanup(ctx, second)
	time.Sleep(workerRefresh)
	released := time.Now()
	third.call(ctx, http.MethodPost, "/probes/"+removed+"/release")
	budget, stop := context.WithTimeout(ctx, 3*time.Minute)
	defer stop()
	third.awaitProbe(budget, removed, func(p probe) bool { return p.Outcome == "failure-route:3" })
	t.Logf("the Flow left the removed Step through its failure route %s after its release", time.Since(released).Round(time.Second))

	live := projects.Project{ProjectID: project, Storage: storage, CurrentLive: &projects.LivePointer{DeploymentID: third.snapshot.DeploymentID, Revision: 3}}
	dockertest.EventuallyForTest(ctx, t, "the Live removal", func() error {
		err := deployer.serving.RemoveServing(ctx, live)
		if errors.Is(err, projects.ErrCleanupPending) {
			return errors.Join(infra.ErrPending, err)
		}
		return err
	})
	if _, exists, err := k.Routes().Read(k.PublicRoutes().Host(infra.ScopeLive, project)); err != nil || exists {
		t.Fatalf("the Live route remains: %v", err)
	}
	dockertest.DestroyProjectForTest(t, k, project)
}

// generationDeployer deploys generations of the generation application on
// one project as ProjectDeployFlow's phases do.
type generationDeployer struct {
	t                 *testing.T
	k                 *infradocker.Infrastructure
	serving           *deploymentsdocker.LiveServing
	registry, project string
	storage           infra.StorageRefs
	execution         infra.ExecutionRefs
}

// generation is one deployed generation and its grant's environment.
type generation struct {
	t           *testing.T
	k           *infradocker.Infrastructure
	snapshot    deployments.Snapshot
	environment map[string]string
}

// deploy builds version, deploys it as generation n replacing the Live
// generation expected and activates it.
func (d *generationDeployer) deploy(ctx context.Context, n uint64, version string, removeAwait bool, expected uint64) generation {
	t := d.t
	t.Helper()
	repository := "sv2-test-" + d.project + "-" + version
	image := dockertest.PushImageForTest(ctx, t, d.registry, dockertest.GenerationAppImageForTest(ctx, t, version, removeAwait), repository)
	digest := image[len(d.registry+"/"+repository+"@"):]
	if recheck := dockertest.ManifestDigestForTest(ctx, t, d.registry, repository, digest); recheck != digest {
		t.Fatalf("the registry rechecked %s as %s", digest, recheck)
	}
	s := deployments.Snapshot{ProjectID: d.project, DeploymentID: "deploy-" + flowid.RandomID(6), ReleaseID: "release-" + version, BuildRevisionID: "build-" + uuid.NewString(),
		ExpectedLiveRevision: expected, Lease: projects.DeploymentLease{Generation: n},
		Inputs:    deployments.Inputs{ApplicationPort: applicationPort, HealthPath: "/healthz", ImageReference: image, ImageDigest: digest, BundleDigest: "sha256:" + flowid.RandomID(64)},
		Resources: deployments.DeploymentResources{Storage: d.storage, Execution: d.execution}}
	definition := appmanifest.FlowDefinition{FlowType: "GenerationProbeFlow", SourcePath: "main.go", SchemaVersion: "2.0", Digest: "sha256:" + flowid.RandomID(64), Valid: true, Graph: json.RawMessage(`{}`)}
	if err := deployments.PublishFlowDefinitionsForTest(ctx, d.k, s, []appmanifest.FlowDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	service, err := d.serving.EnsureGenerationService(ctx, s)
	if err != nil || service.UID == "" || service.Generation != n {
		t.Fatalf("the generation network: %+v %v", service, err)
	}
	s.Resources.Service = service
	grant := liveGrant(ctx, t, d.k, s)
	if err = d.k.IssueWorkload(ctx, grant); err != nil {
		t.Fatal(err)
	}
	environment, err := d.k.GrantEnvironment(ctx, grant)
	if err != nil {
		t.Fatal(err)
	}
	application, err := d.serving.ApplyApplication(ctx, s, environment)
	if err != nil {
		t.Fatalf("run the application: %v", err)
	}
	s.Providers.Application = application
	dockertest.EventuallyForTest(ctx, t, "the readiness of generation "+version, func() error {
		_, err := d.serving.VerifyReadiness(ctx, s)
		return err
	})
	var activation deployments.Activation
	dockertest.EventuallyForTest(ctx, t, "the activation of generation "+version, func() (err error) {
		activation, err = d.serving.Activate(ctx, s)
		return err
	})
	if activation.URL != d.k.PublicRoutes().URL(infra.ScopeLive, d.project) || activation.Manifest.Version == "" ||
		activation.Worker.Name != infra.LiveWorkerEndpoint || activation.Worker.UID != application.UID || activation.Worker.Generation != n {
		t.Fatalf("unexpected activation %+v", activation)
	}
	s.Resources.Worker = activation.Worker
	return generation{t: t, k: d.k, snapshot: s, environment: environment}
}

// requireWorker requires the generation to hold the Live Worker alias, or not.
func (d *generationDeployer) requireWorker(ctx context.Context, g generation, holds bool) {
	d.t.Helper()
	target := infra.WorkerTarget{ProjectID: d.project, Namespace: d.storage.Boundary, Controller: g.snapshot.Providers.Application, Service: g.snapshot.Resources.Service,
		DeploymentID: g.snapshot.DeploymentID, Generation: g.snapshot.Lease.Generation}
	if err := d.k.CheckWorker(ctx, target); (err == nil) != holds {
		d.t.Fatalf("generation %d holds the Live Worker alias: %t, want %t (%v)", g.snapshot.Lease.Generation, err == nil, holds, err)
	}
}

// cleanup removes a replaced generation as its cleanup does.
func (d *generationDeployer) cleanup(ctx context.Context, g generation) {
	d.t.Helper()
	dockertest.EventuallyForTest(ctx, d.t, "the removal of a replaced generation", func() error {
		err := d.serving.CleanupDeployment(ctx, g.snapshot)
		if errors.Is(err, projects.ErrCleanupPending) {
			return errors.Join(infra.ErrPending, err)
		}
		return err
	})
	if container, err := d.k.API().InspectContainer(ctx, g.snapshot.Providers.Application.UID); err != nil || container != nil {
		d.t.Fatalf("the replaced generation's container remains: %v", err)
	}
	// Its grant is revoked with it, as retirement does.
	if err := d.k.RevokeWorkload(ctx, liveGrant(ctx, d.t, d.k, g.snapshot)); err != nil {
		d.t.Fatal(err)
	}
}

// probe is what the generation application reads of one probe Flow.
type probe struct {
	StartedBy, ReleasedBy, Outcome, ReadBy string
}

// call sends one request to the generation's application and fails the test
// unless it succeeds.
func (g generation) call(ctx context.Context, method, path string) []byte {
	g.t.Helper()
	body, status, err := g.request(ctx, method, path)
	if err != nil || status != http.StatusOK {
		g.t.Fatalf("%s %s on generation %d: %d %s %v", method, path, g.snapshot.Lease.Generation, status, body, err)
	}
	return body
}

func (g generation) request(ctx context.Context, method, path string) ([]byte, int, error) {
	container, err := g.k.API().InspectContainer(ctx, g.snapshot.Providers.Application.UID)
	if err != nil || container == nil {
		return nil, 0, fmt.Errorf("read the application container: %w", err)
	}
	address, err := container.PublishedAddress(applicationPort)
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://"+address+path, nil)
	if err != nil {
		return nil, 0, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	return body, response.StatusCode, err
}

// awaitProbe reads the probe Flow id through the generation's application
// until done holds.
func (g generation) awaitProbe(ctx context.Context, id string, done func(probe) bool) probe {
	g.t.Helper()
	var last probe
	var lastErr string
	for {
		body, status, err := g.request(ctx, http.MethodGet, "/probes/"+id)
		if err == nil && status == http.StatusOK && json.Unmarshal(body, &last) == nil && done(last) {
			return last
		}
		lastErr = strings.TrimSpace(fmt.Sprintf("%d %s %v", status, body, err))
		select {
		case <-ctx.Done():
			g.t.Fatalf("probe %s reads %+v (%s): %v", id, last, lastErr, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// liveGrant stores an empty configuration head of the project and returns
// the grant of the deployment's generation.
func liveGrant(ctx context.Context, t *testing.T, k interface {
	S3(context.Context, string, string) (*infra.ApplicationObjects, error)
}, s deployments.Snapshot) infra.Workload {
	t.Helper()
	objects, err := k.S3(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID)
	if err != nil {
		t.Fatal(err)
	}
	live := infra.Scope{Kind: infra.ScopeLive}
	head, err := objects.Put(ctx, infra.ConfigurationKeyForTest(s.ProjectID, live, "configuration/head"), []byte(`{"environment":{}}`), false)
	if err != nil {
		t.Fatal(err)
	}
	return infra.Workload{ProjectID: s.ProjectID, WorkloadID: s.DeploymentID, Scope: live, Generation: s.Lease.Generation, ConfigurationRevision: 1,
		Storage: s.Resources.Storage, Configuration: head, GrantID: infra.WorkloadName(s.DeploymentID, s.Lease.Generation)}
}
