// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dockertest

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
)

// registryPort is the port of the registry container.
const registryPort = 5000

// RegistryForTest runs the pinned registry image as the Native lane's local registry
// and returns its address on this machine, localhost:<port>, under which the
// engine pushes and pulls alike. The test's cleanup removes it.
func RegistryForTest(ctx context.Context, t testing.TB, k *docker.Infrastructure) string {
	t.Helper()
	pins := readPins(t)
	spec := dockerapi.ContainerSpec{Image: pins.registry, Labels: map[string]string{deploymentnames.ManagedByLabel: testOwner}}
	spec.Publish(registryPort)
	if err := k.EnsureImage(ctx, spec.Image); err != nil {
		t.Fatal(err)
	}
	name := "sv2-test-registry-" + flowid.RandomID(8)
	id, err := k.API().CreateContainer(ctx, name, spec)
	if err != nil {
		t.Fatal(err)
	}
	removeOnCleanup(ctx, t, k.API(), name, id)
	if err = k.API().StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	container, err := k.API().InspectContainer(ctx, id)
	if err != nil || container == nil {
		t.Fatal(err)
	}
	address, err := container.PublishedAddress(registryPort)
	if err != nil {
		t.Fatal(err)
	}
	return "localhost:" + strings.TrimPrefix(address, dockerapi.Loopback+":")
}

// manifestTypes are the media types of the manifests a registry may hold
// under a tag: an index of platform images, or one image's manifest.
const manifestTypes = "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json"

// ManifestDigestForTest asks the registry at registry, with HEAD, for the digest of
// the manifest of repository under reference, a tag or a digest, as the
// Native builder records a pushed image and rechecks it before READY.
func ManifestDigestForTest(ctx context.Context, t testing.TB, registry, repository, reference string) string {
	t.Helper()
	host := strings.Replace(registry, "localhost:", dockerapi.Loopback+":", 1)
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodHead, "http://"+host+"/v2/"+repository+"/manifests/"+reference, nil))
	request.Header.Set("Accept", manifestTypes)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	digest := response.Header.Get("Docker-Content-Digest")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("the registry has no manifest of %s:%s (%s)", repository, reference, response.Status)
	}
	return digest
}

// PushImageForTest pushes the local image source to the registry as repository and
// returns the reference pinned by the digest the registry holds, after
// removing the pushed tag, so the image is pulled again by its digest.
func PushImageForTest(ctx context.Context, t testing.TB, registry, source, repository string) string {
	t.Helper()
	tag := registry + "/" + repository + ":test"
	for _, arguments := range [][]string{{"tag", source, tag}, {"push", "--quiet", tag}} {
		if output, err := exec.CommandContext(ctx, "docker", arguments...).CombinedOutput(); err != nil { //nolint:gosec // the docker CLI with the test's own arguments
			t.Fatalf("docker %s: %v: %s", arguments[0], err, output)
		}
	}
	digest := ManifestDigestForTest(ctx, t, registry, repository, "test")
	if output, err := exec.CommandContext(ctx, "docker", "image", "rm", tag).CombinedOutput(); err != nil { //nolint:gosec // the tag this helper pushed
		t.Fatalf("remove the pushed tag: %v: %s", err, output)
	}
	return registry + "/" + repository + "@" + digest
}

// GenerationAppImageForTest builds the deployment tests' Live application
// (package generationapp) as version, without AwaitRelease when removeAwait,
// on the pinned gateway image, whose /tmp its unprivileged user can write, and
// returns its local tag. It runs a Dex Worker, so it needs the project's Live
// Dex.
func GenerationAppImageForTest(ctx context.Context, t testing.TB, version string, removeAwait bool) string {
	t.Helper()
	directory := t.TempDir()
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", filepath.Join(directory, "generationapp"), //nolint:gosec // the Go toolchain building the repository's test application
		"-ldflags", "-X main.version="+version+" -X main.removeAwait="+strconv.FormatBool(removeAwait), "./internal/dexai/delivery/deployments/generationapp")
	build.Dir = repositoryRoot(t)
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0", "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the generation application: %v: %s", err, output)
	}
	dockerfile := "FROM " + readPins(t).gateway + "\nCOPY --chmod=0755 generationapp /generationapp\nENTRYPOINT [\"/generationapp\"]\nCMD []\n"
	if err := os.WriteFile(filepath.Join(directory, "Dockerfile"), []byte(dockerfile), 0o600); err != nil {
		t.Fatal(err)
	}
	tag := "sv2-test-generationapp:" + flowid.RandomID(8)
	image := exec.CommandContext(ctx, "docker", "build", "--quiet", "--tag", tag, directory) //nolint:gosec // a tag of this helper's own random name
	if output, err := image.CombinedOutput(); err != nil {
		t.Fatalf("build the generation application image: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.CommandContext(context.WithoutCancel(ctx), "docker", "image", "rm", tag).Run() }) //nolint:gosec // the tag built above
	return tag
}
