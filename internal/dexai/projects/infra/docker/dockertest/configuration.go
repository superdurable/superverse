// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dockertest

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
)

// probeOutputBytes bounds the output of the configuration probe the test
// reads.
const probeOutputBytes = 64 << 10

// LoadConfigurationForTest runs the configuration probe (package configprobe) with
// environment, a workload grant's environment, in a container on network, a
// project network, as an application of the project runs there: the
// Connector SDK loads the project configuration through the storage endpoint
// of the environment and resolves the application environment it pins. It
// returns the probe's output, the configuration revision and the SHA-256 of
// each value of names, one line each, without what the SDK logs; and fails the
// test when the probe fails. The container is removed before it returns.
func LoadConfigurationForTest(ctx context.Context, t testing.TB, k *docker.Infrastructure, project, network string, environment map[string]string, names ...string) string {
	t.Helper()
	probe := buildProbe(ctx, t)
	variables := make([]string, 0, len(environment))
	for name, value := range environment {
		variables = append(variables, name+"="+value)
	}
	sort.Strings(variables)
	spec := dockerapi.ContainerSpec{Image: readPins(t).gateway, Entrypoint: []string{"/configprobe"}, Cmd: names, Env: variables,
		Labels: map[string]string{deploymentnames.ProjectLabel: project, deploymentnames.ManagedByLabel: testOwner}}
	spec.Attach(network)
	name := "sv2-test-configprobe-" + flowid.RandomID(8)
	id, err := k.API().CreateContainer(ctx, name, spec)
	if err != nil {
		t.Fatalf("create the configuration probe: %v", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		if removeErr := k.API().RemoveContainer(cleanup, id); removeErr != nil {
			t.Errorf("remove %s: %v", name, removeErr)
		}
	}()
	if err = k.API().CopyToContainer(ctx, id, "/", probe); err != nil {
		t.Fatalf("place the configuration probe: %v", err)
	}
	if err = k.API().StartContainer(ctx, id); err != nil {
		t.Fatalf("start the configuration probe: %v", err)
	}
	var container *dockerapi.Container
	for {
		if container, err = k.API().InspectContainer(ctx, id); err != nil || container == nil {
			t.Fatalf("read the configuration probe: %v", err)
		}
		if !container.State.Running && container.State.Status != dockerapi.StateCreated {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("the configuration probe did not finish")
		case <-time.After(250 * time.Millisecond):
		}
	}
	output, err := k.API().ContainerLogs(ctx, id, 100, probeOutputBytes)
	if err != nil {
		t.Fatalf("read the configuration probe's output: %v", err)
	}
	if container.State.ExitCode != 0 {
		t.Fatalf("the grant environment does not load the project configuration (exit code %d): %s", container.State.ExitCode, strings.TrimSpace(output))
	}
	results := map[string]bool{"revision": true}
	for _, name := range names {
		results[name] = true
	}
	var lines strings.Builder
	for line := range strings.Lines(output) {
		if field, _, _ := strings.Cut(line, " "); results[field] {
			lines.WriteString(line)
		}
	}
	return lines.String()
}

// buildProbe compiles the configuration probe for Linux on the engine's
// architecture and returns it as a tar archive of /configprobe.
func buildProbe(ctx context.Context, t testing.TB) []byte {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "configprobe")
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", binary, "./internal/dexai/projects/infra/docker/dockertest/configprobe") //nolint:gosec // the Go toolchain building the repository's probe
	build.Dir = repositoryRoot(t)
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0", "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the configuration probe: %v: %s", err, output)
	}
	data, err := os.ReadFile(binary) //nolint:gosec // the probe this helper just built
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err = writer.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "configprobe", Mode: 0o755, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

// repositoryRoot is the directory of go.mod above the test's directory.
func repositoryRoot(t testing.TB) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for !fileExists(filepath.Join(directory, "go.mod")) {
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("the repository root is not above the test's directory")
		}
		directory = parent
	}
	return directory
}
