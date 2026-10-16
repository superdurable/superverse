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
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	sandboxdocker "github.com/superdurable/superversev2/internal/dexai/agents/sandbox/docker"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/docker/dockertest"
)

// Requires the local Docker Engine, helm, and SUPERVERSE_TEST_CODING_IMAGE: a
// coding image built from the current source whose engine records a digest of
// it (the containerd image store does for a local build). It renders the
// chart's coding profile with that image and runs a Project's Sandbox on
// Docker as the server does: the instance with the Pod's hardening, its
// workspace written through sandboxd's user, a Preview grant bound to the
// Sandbox network and workspace volumes, the instance's reclamation that
// keeps the volumes, and a cold start that attaches them again under the same
// principal. Replaced identities and a second instance on the same volumes are
// refused. The Preview route is the Project's: a later instance replaces it,
// another holder of the host is refused, stopping the Preview withdraws it,
// and the next expose publishes it again under the same host and rewrites a
// route that differs from the platform's spec.
// The deleting Project's release, which names no instance, removes the route
// whichever instance published it and leaves nothing of the Sandbox; repeated,
// it finds every part gone. Its cleanup leaves nothing of the project.
func TestDockerSandboxInstances(t *testing.T) {
	k := dockertest.InfrastructureForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	project := flowid.RandomID(8)
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, k, project) })
	var storage infra.StorageRefs
	dockertest.EventuallyForTest(ctx, t, "the project storage", func() (err error) {
		storage, err = k.EnsureStorage(ctx, project, storage)
		return err
	})
	name, path := renderProfile(t, codingImage(ctx, t, k))
	provider := &sandboxdocker.InstanceProvider{Infrastructure: k, ProfileName: name, ProfilePath: path, Connections: k}
	workspaces := workspace.NewCapabilities(nil, nil, provider.SandboxAddresses)
	t.Cleanup(func() { _ = workspaces.Close() })
	provider.Workspace = workspaces
	identity := sandbox.Identity{ProjectID: project}
	first := sandbox.Instance{Identity: identity, Generation: 1, WorkspaceRequestID: "request-1"}
	t.Cleanup(func() { removeSandbox(t, provider, first.Identity) })

	first.Resources = provision(ctx, t, provider, first)
	r := first.Resources
	if r.Boundary.Name != k.SessionNetwork(project) || r.Volume.UID == "" || r.Identity != r.Volume || r.Instance.UID == "" || r.ServiceName != infradocker.SandboxAlias {
		t.Fatalf("unexpected instance resources %+v", r)
	}
	container, err := k.API().InspectContainer(ctx, r.ServiceUID)
	if err != nil || container == nil {
		t.Fatalf("read the instance's container: %v", err)
	}
	if !slices.Contains(container.Config.Env, "SUPERVERSE_POD_UID="+r.Instance.UID) || container.Config.Labels[infradocker.InstanceUIDLabel] != r.Instance.UID {
		t.Fatal("the instance UID is not the container's Pod UID")
	}
	requireHardened(ctx, t, k, r.ServiceUID)
	if err = workspaces.Reach(ctx, first.Binding()); err != nil {
		t.Fatalf("sandboxd is not reachable through the published ports: %v", err)
	}
	if code, err := k.API().Exec(ctx, r.ServiceUID, []string{"sh", "-c", "echo kept > /workspace/marker && test ! -w /etc"}); err != nil || code != 0 {
		t.Fatalf("the workspace is not writable or the root is (exit %d): %v", code, err)
	}

	grant := previewGrant(ctx, t, k, identity, storage, r)
	if err = k.IssueWorkload(ctx, grant); err != nil {
		t.Fatalf("issue the Preview grant bound to the instance's principal: %v", err)
	}
	foreign := grant
	foreign.Principal = &infra.Principal{Boundary: grant.Principal.Boundary, Identity: infra.ResourceRef{Boundary: grant.Principal.Identity.Boundary, Name: grant.Principal.Identity.Name, UID: "replaced-workspace"}}
	if err = k.IssueWorkload(ctx, foreign); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a grant reached a replaced principal: %v", err)
	}

	replaced := first
	replaced.Resources.Instance.UID = "replaced-instance"
	if _, _, err = provider.Provision(ctx, replaced); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a replaced instance was adopted: %v", err)
	}
	replaced = first
	replaced.Resources.Volume.UID = "replaced-volume"
	if _, _, err = provider.Provision(ctx, replaced); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a replaced workspace was adopted: %v", err)
	}
	second := sandbox.Instance{Identity: identity, Generation: 1, WorkspaceRequestID: "request-2", Resources: first.Resources.Retained().Resources}
	if _, _, err = provider.Provision(ctx, second); !errors.Is(err, sandbox.ErrVolumeInUse) {
		t.Fatalf("a second instance took the workspace of a running one: %v", err)
	}

	dockertest.EventuallyForTest(ctx, t, "the instance's reclamation", func() error { return done(provider.DeletePod(ctx, first)) })
	if left, err := k.API().InspectContainer(ctx, r.ServiceUID); err != nil || left != nil {
		t.Fatalf("the reclaimed instance's container remains: %v", err)
	}
	second.Resources = provision(ctx, t, provider, second)
	s := second.Resources
	if s.Volume != r.Volume || s.Identity != r.Identity || s.Boundary != r.Boundary || s.Instance.UID == r.Instance.UID {
		t.Fatalf("the cold start did not attach the retained workspace: %+v", s)
	}
	if code, err := k.API().Exec(ctx, s.ServiceUID, []string{"test", "-f", "/workspace/marker"}); err != nil || code != 0 {
		t.Fatalf("the retained workspace lost its file (exit %d): %v", code, err)
	}
	if err = k.IssueWorkload(ctx, previewGrant(ctx, t, k, identity, storage, s)); err != nil {
		t.Fatalf("the principal changed across the cold start: %v", err)
	}
	if grant.Principal.Boundary != infraRef(s.Boundary) || grant.Principal.Identity != infraRef(s.Identity) {
		t.Fatal("the cold start changed the Preview principal")
	}

	dockertest.EventuallyForTest(ctx, t, "the Preview engine", func() error {
		answer, err := provider.EnsurePreviewEnvironment(ctx, second)
		if err == nil && answer.State != workspace.PreviewStateReady {
			return errors.Join(infra.ErrPending, errors.New(answer.State))
		}
		return err
	})
	target := second.DexTarget()
	target.Sandbox.UID = "replaced-instance"
	if err = k.ProbeSandboxDex(ctx, target); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("the Dex of a replaced instance was reached: %v", err)
	}

	// The Preview route is the Project's, whichever of its instances published
	// it: a later instance replaces the route of an earlier one, and a route of
	// another holder on the host is refused. No application runs, so the
	// gateway does not answer through it.
	host := k.PublicRoutes().Host(infra.ScopePreview, project)
	if _, _, err = provider.ExposePreview(ctx, first); err != nil || heldRoute(t, k, host) == nil {
		t.Fatalf("publish the Preview route of the reclaimed instance: %v", err)
	}
	if _, _, err = provider.ExposePreview(ctx, second); err != nil {
		t.Fatalf("a later instance did not replace the Preview route: %v", err)
	}
	if route := heldRoute(t, k, host); route == nil || route.Owner.WorkspaceRequestID != second.WorkspaceRequestID || route.Container != s.ServiceUID {
		t.Fatalf("the Preview route does not serve the later instance: %+v", route)
	}
	holder := infradocker.RouteOwner{Scope: infra.ScopeLive.String(), ProjectID: project, DeploymentID: "foreign"}
	foreignRoute := infradocker.Route{Host: host, Owner: holder, Generation: 1, Container: s.ServiceUID, Port: workspace.PreviewApplicationPort}
	if err = k.Routes().Publish(ctx, foreignRoute, func(infradocker.Route) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, _, err = provider.ExposePreview(ctx, second); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("the Preview took over a route of another holder: %v", err)
	}
	release := sandbox.Instance{Identity: identity, Generation: sandbox.InstanceGeneration, Resources: s.WithoutPod()}
	if released, err := provider.ReleasePreviewRoute(ctx, release); err != nil || !released {
		t.Fatalf("the release of the Preview route failed: %v", err)
	}
	if route := heldRoute(t, k, host); route == nil || route.Owner != holder {
		t.Fatal("the release removed a route of another holder")
	}
	if _, err = k.Routes().Withdraw(ctx, host, func(current infradocker.Route) bool { return current.Owner == holder }); err != nil {
		t.Fatal(err)
	}
	if _, _, err = provider.ExposePreview(ctx, second); err != nil {
		t.Fatalf("publish the Preview route again: %v", err)
	}
	// While the route is published the gateway forwards the host to the
	// instance's application port, where nothing listens.
	dockertest.EventuallyForTest(ctx, t, "the gateway's Preview route", func() error {
		if status, unanswered := k.Routes().Probe(ctx, host, "/"); status == 0 || status == http.StatusNotFound {
			return errors.Join(infra.ErrPending, fmt.Errorf("the gateway answered %d: %s", status, unanswered))
		}
		return nil
	})
	// Stopping the Preview, as a stop command and the reclamation do,
	// withdraws the Project's route, so nothing on the application's port is
	// reachable through the gateway while it is stopped: the gateway answers
	// 404 for the host.
	if stopped, err := provider.StopPreview(ctx, second, "stop-preview"); err != nil || !stopped {
		t.Fatalf("the Preview did not stop: %v", err)
	}
	if route := heldRoute(t, k, host); route != nil {
		t.Fatalf("stopping the Preview left the Project's route of %s", route.Owner.WorkspaceRequestID)
	}
	dockertest.EventuallyForTest(ctx, t, "the removal of the Preview route", func() error {
		if status, unanswered := k.Routes().Probe(ctx, host, "/"); status != http.StatusNotFound {
			return errors.Join(infra.ErrPending, fmt.Errorf("the gateway answered %d: %s", status, unanswered))
		}
		return nil
	})
	// The next start publishes it again under the same host, and a route
	// that differs from the platform's spec is rewritten.
	drifted := infradocker.Route{Host: host, Owner: infradocker.RouteOwner{Scope: infra.ScopePreview.String(), ProjectID: project, WorkspaceRequestID: second.WorkspaceRequestID},
		Generation: second.Generation, Container: s.ServiceUID, Port: workspace.PreviewApplicationPort + 1}
	if err = k.Routes().Publish(ctx, drifted, func(infradocker.Route) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, _, err = provider.ExposePreview(ctx, second); err != nil {
		t.Fatalf("publish the Preview route after the stop: %v", err)
	}
	if route := heldRoute(t, k, host); route == nil || route.Port != workspace.PreviewApplicationPort || route.Owner.WorkspaceRequestID != second.WorkspaceRequestID {
		t.Fatalf("the next start did not restore the Project's route: %+v", route)
	}

	// The release in the Session Service's order, with the resources the
	// Session recorded, leaves nothing of the Sandbox.
	known := sandbox.Instance{Identity: identity, Generation: sandbox.InstanceGeneration, Resources: s}
	found, err := provider.DiscoverResources(ctx, known)
	if err != nil || found.Boundary != s.Boundary || found.Volume != s.Volume || found.Instance != s.Instance {
		t.Fatalf("the release found %+v instead of the recorded resources: %v", found, err)
	}
	dockertest.EventuallyForTest(ctx, t, "the release of the instance", func() error { return done(provider.DeletePod(ctx, known)) })
	known.Resources = found.WithoutPod()
	dockertest.EventuallyForTest(ctx, t, "the release of the Preview route", func() error { return done(provider.ReleasePreviewRoute(ctx, known)) })
	if route := heldRoute(t, k, host); route != nil {
		t.Fatalf("the release left the Project's Preview route of %s", route.Owner.WorkspaceRequestID)
	}
	dockertest.EventuallyForTest(ctx, t, "the Project's Preview grants", func() error {
		return k.DestroyPreviewGrants(ctx, project, storage, s.Boundary.Name)
	})
	dockertest.EventuallyForTest(ctx, t, "the release of the workspace", func() error { return done(provider.DeleteVolume(ctx, known)) })
	dockertest.EventuallyForTest(ctx, t, "the release of the Sandbox network", func() error { return done(provider.DeleteNamespace(ctx, known)) })
	requireSandboxGone(ctx, t, k, identity)
	// Repeated, the release finds every part gone at once: absence is success.
	known.Resources = s
	if found, err = provider.DiscoverResources(ctx, known); err != nil {
		t.Fatalf("the repeated release did not read the Sandbox's absent resources: %v", err)
	}
	for _, part := range releaseParts(provider) {
		if gone, err := part.release(ctx, sandbox.Instance{Identity: identity, Generation: sandbox.InstanceGeneration, Resources: found}); err != nil || !gone {
			t.Fatalf("the repeated release of the %s did not find it gone: %v", part.what, err)
		}
	}
	dockertest.DestroyProjectForTest(t, k, project)
}

// provision provisions the instance until it is ready.
func provision(ctx context.Context, t *testing.T, provider *sandboxdocker.InstanceProvider, s sandbox.Instance) sandbox.Resources {
	t.Helper()
	dockertest.EventuallyForTest(ctx, t, "the instance "+s.WorkspaceRequestID, func() error {
		resources, ready, err := provider.Provision(ctx, s)
		s.Resources = resources
		if err == nil && !ready {
			return infra.ErrPending
		}
		return err
	})
	return s.Resources
}

// done turns a reclamation step's answer into a convergence.
func done(gone bool, err error) error {
	if err == nil && !gone {
		return infra.ErrPending
	}
	return err
}

// requireHardened requires the container the coding profile's Pod hardening:
// UID 1000, a read-only root, no capabilities, no new privileges, CPU and
// memory limits, no restart policy and the two networks.
func requireHardened(ctx context.Context, t *testing.T, k *infradocker.Infrastructure, id string) {
	t.Helper()
	output, err := exec.CommandContext(ctx, "docker", "inspect", "--format",
		"{{.Config.User}} {{.HostConfig.ReadonlyRootfs}} {{.HostConfig.CapDrop}} {{.HostConfig.SecurityOpt}} {{.HostConfig.NanoCpus}} {{.HostConfig.Memory}} {{.HostConfig.RestartPolicy.Name}} {{len .NetworkSettings.Networks}}", id).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(output)); got != "1000:1000 true [ALL] [no-new-privileges:true] 2000000000 4294967296 no 2" {
		t.Fatalf("the instance is not hardened as the Pod: %s", got)
	}
}

// previewGrant stores a Preview configuration head of the Project and returns
// its first grant generation, bound to the instance's principal.
func previewGrant(ctx context.Context, t *testing.T, k *infradocker.Infrastructure, id sandbox.Identity, storage infra.StorageRefs, r sandbox.Resources) infra.Workload {
	t.Helper()
	objects, err := k.S3(ctx, id.ProjectID, storage.Boundary.UID)
	if err != nil {
		t.Fatal(err)
	}
	keys := infra.PreviewKeysOf(id.ProjectID)
	head, err := objects.Put(ctx, infra.ConfigurationKeyForTest(id.ProjectID, keys.Scope, "configuration/head"), []byte(`{"environment":{}}`), false)
	if err != nil {
		t.Fatal(err)
	}
	return infra.Workload{ProjectID: id.ProjectID, WorkloadID: keys.GrantWorkloadID, Scope: keys.Scope, Generation: 1, ConfigurationRevision: 1, Storage: storage,
		Configuration: head, GrantID: infra.WorkloadName(keys.GrantWorkloadID, 1), Principal: &infra.Principal{Boundary: infraRef(r.Boundary), Identity: infraRef(r.Identity)}}
}

func infraRef(r sandbox.ResourceRef) infra.ResourceRef {
	return infra.ResourceRef{Boundary: r.Boundary, Name: r.Name, UID: r.UID}
}

// releasePart is one part of the deleting Project's release of its Sandbox's
// resources through the instance provider.
type releasePart struct {
	what    string
	release func(context.Context, sandbox.Instance) (bool, error)
}

// releaseParts are the provider's parts of the release in the Session
// Service's order: the instance, then without it the Preview route, the
// workspace and the Sandbox network.
func releaseParts(provider *sandboxdocker.InstanceProvider) []releasePart {
	withoutPod := func(release func(context.Context, sandbox.Instance) (bool, error)) func(context.Context, sandbox.Instance) (bool, error) {
		return func(ctx context.Context, s sandbox.Instance) (bool, error) {
			s.Resources = s.Resources.WithoutPod()
			return release(ctx, s)
		}
	}
	return []releasePart{
		{"instance", provider.DeletePod}, {"Preview route", withoutPod(provider.ReleasePreviewRoute)},
		{"workspace", withoutPod(provider.DeleteVolume)}, {"Sandbox network", withoutPod(provider.DeleteNamespace)},
	}
}

// removeSandbox releases what the Project's Sandbox may have left after a
// failure, as the deleting Project's release does without any recorded
// resource: its container, Preview route, workspace volumes and network.
func removeSandbox(t *testing.T, provider *sandboxdocker.InstanceProvider, id sandbox.Identity) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	release := sandbox.Instance{Identity: id, Generation: sandbox.InstanceGeneration}
	resources, err := provider.DiscoverResources(ctx, release)
	if err != nil {
		t.Errorf("discover the Sandbox's resources: %v", err)
		return
	}
	release.Resources = resources
	for _, part := range releaseParts(provider) {
		dockertest.EventuallyForTest(ctx, t, "the Sandbox's cleanup of its "+part.what, func() error { return done(part.release(ctx, release)) })
	}
}

// heldRoute is the route the gateway serves for host, nil when it serves
// none.
func heldRoute(t *testing.T, k *infradocker.Infrastructure, host string) *infradocker.Route {
	t.Helper()
	route, exists, err := k.Routes().Read(host)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		return nil
	}
	return &route
}

// requireSandboxGone fails the test while any container, network or volume
// carries the labels of the Project's Sandbox.
func requireSandboxGone(ctx context.Context, t *testing.T, k *infradocker.Infrastructure, id sandbox.Identity) {
	t.Helper()
	labeled := dockerapi.Labels(k.PreviewLabels(id.ProjectID))
	containers, err := k.API().ListContainers(ctx, labeled)
	if err != nil {
		t.Fatal(err)
	}
	networks, err := k.API().ListNetworks(ctx, labeled)
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := k.API().ListVolumes(ctx, labeled)
	if err != nil {
		t.Fatal(err)
	}
	if len(containers)+len(networks)+len(volumes) != 0 {
		t.Fatalf("the released Sandbox of %s left %d containers, %d networks and %d volumes", id.ProjectID, len(containers), len(networks), len(volumes))
	}
}

// codingImage is SUPERVERSE_TEST_CODING_IMAGE pinned by the digest the engine
// records for it.
func codingImage(ctx context.Context, t *testing.T, k *infradocker.Infrastructure) string {
	t.Helper()
	reference := os.Getenv("SUPERVERSE_TEST_CODING_IMAGE")
	if reference == "" {
		t.Fatal("SUPERVERSE_TEST_CODING_IMAGE must name a coding image built from the current source")
	}
	image, err := k.API().InspectImage(ctx, reference)
	if err != nil || image == nil || len(image.RepoDigests) == 0 {
		t.Fatalf("the engine records no digest of %s: %v", reference, err)
	}
	return image.RepoDigests[0]
}

// renderProfile renders the chart's coding profile with image, as the Native
// launcher does, into the test's directory, and returns its name and file.
func renderProfile(t *testing.T, image string) (string, string) {
	t.Helper()
	root := repositoryRoot(t)
	repository, digest, _ := strings.Cut(image, "@")
	shape := "@sha256:" + strings.Repeat("0", 64)
	output, err := exec.CommandContext(t.Context(), "helm", "template", "superverse-v2", filepath.Join(root, "infra-as-code/kubernetes/charts/superverse-platform"),
		"--namespace", "superverse-v2-system", "-f", filepath.Join(root, "infra-as-code/environments/local-kind/values.yaml"),
		"--set", "server.enabled=false", "--set", "studio.enabled=false",
		"--set-string", "localDependencies.temporalImage=temporal"+shape, "--set-string", "localDependencies.minioImage=minio"+shape,
		"--set-string", "localDependencies.postgresImage=postgres"+shape, "--set-string", "localDependencies.keycloakImage=keycloak"+shape,
		"--set", "sandboxProfiles.codingProfile.enabled=true", "--set-string", "sandboxProfiles.codingProfile.image.repository="+repository,
		"--set-string", "sandboxProfiles.codingProfile.image.digest="+digest, "--show-only", "templates/coding-profile.yaml").Output()
	if err != nil {
		t.Fatalf("render the coding profile: %v", err)
	}
	for _, document := range strings.Split(string(output), "\n---\n") {
		var object struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		if yaml.Unmarshal([]byte(document), &object) == nil && object.Kind == "ConfigMap" {
			path := filepath.Join(t.TempDir(), object.Metadata.Name+".yaml")
			if err = os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			return object.Metadata.Name, path
		}
	}
	t.Fatal("the chart rendered no coding profile")
	return "", ""
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err = os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		if parent := filepath.Dir(directory); parent != directory {
			directory = parent
			continue
		}
		t.Fatal("the repository root is not above the test's directory")
	}
}
