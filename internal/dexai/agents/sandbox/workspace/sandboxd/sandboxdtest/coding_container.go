// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxdtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
)

// codingContainer is a container of the coding image that one test owns: the
// image's entrypoint (tini, then sandboxd) with sandboxd's ports published on
// loopback. The test can restart it, which restarts PID 1 and keeps /tmp like
// a container restart in a Pod, and replace it with a new container, like a
// new Pod.
type codingContainer struct {
	t          testing.TB
	image      string
	name       string
	connection *grpc.ClientConn
	client     *sandboxd.SandboxClient
}

// StartCodingContainerForTest runs SUPERVERSE_TEST_CODING_IMAGE, a coding image built
// from the current source, and connects to its real sandboxd. A missing image
// or Docker fails the test; the container is removed with the test.
func StartCodingContainerForTest(t testing.TB) *codingContainer {
	t.Helper()
	image := os.Getenv("SUPERVERSE_TEST_CODING_IMAGE")
	if image == "" {
		t.Fatal("SUPERVERSE_TEST_CODING_IMAGE must name a coding image built from the current source")
	}
	container := &codingContainer{t: t, image: image, name: fmt.Sprintf("sv2-coding-test-%d", time.Now().UnixNano())}
	t.Cleanup(func() {
		if container.connection != nil {
			_ = container.connection.Close()
		}
		if output, err := docker(30*time.Second, "rm", "--force", "--volumes", container.name); err != nil {
			t.Errorf("remove the test container %s: %v: %s", container.name, err, output)
		}
	})
	container.run()
	return container
}

// Client is the client of the container's sandboxd, which a restart or a
// replacement connects anew.
func (container *codingContainer) Client() *sandboxd.SandboxClient { return container.client }

func (container *codingContainer) run() {
	container.t.Helper()
	output, err := docker(2*time.Minute, "run", "--detach", "--name", container.name,
		"--publish", "127.0.0.1::8080", "--publish", "127.0.0.1::9090",
		"--env", "SUPERVERSE_PREVIEW_STATE_DIR=/var/lib/superverse/preview", "--env", "SUPERVERSE_POD_UID="+container.name,
		container.image, "--root-dir=/workspace", "--rest-port=8080", "--grpc-port=9090")
	if err != nil {
		container.t.Fatalf("run the coding image: %v: %s", err, output)
	}
	container.connect()
}

// Restart restarts the container: a new PID 1, the same /tmp.
func (container *codingContainer) Restart() {
	container.t.Helper()
	if output, err := docker(time.Minute, "restart", "--time", "1", container.name); err != nil {
		container.t.Fatalf("restart the test container: %v: %s", err, output)
	}
	container.connect()
}

// Replace removes the container and starts a new one: a new instance with an
// empty /tmp.
func (container *codingContainer) Replace() {
	container.t.Helper()
	if output, err := docker(30*time.Second, "rm", "--force", "--volumes", container.name); err != nil {
		container.t.Fatalf("remove the test container: %v: %s", err, output)
	}
	container.run()
}

// connect dials the published sandboxd ports and waits until the process API
// answers a ledger request.
func (container *codingContainer) connect() {
	container.t.Helper()
	if container.connection != nil {
		_ = container.connection.Close()
	}
	grpcAddress, httpAddress := container.port("9090"), container.port("8080")
	connection, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		container.t.Fatal(err)
	}
	container.connection = connection
	container.client = sandboxd.NewSandboxClient(sandboxd.SandboxClientConfig{FilesystemBaseURL: "http://" + httpAddress, ProcessConnection: connection})
	var lastErr error
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, lastErr = container.client.CommandLedger().Status(ctx, "readiness-probe", 0, sandboxd.CommandOutputView{})
		cancel()
		if lastErr == nil {
			return
		}
	}
	container.t.Fatalf("the container's sandboxd did not answer: %v", lastErr)
}

func (container *codingContainer) port(containerPort string) string {
	container.t.Helper()
	output, err := docker(10*time.Second, "port", container.name, containerPort+"/tcp")
	if err != nil {
		container.t.Fatalf("read the published port %s: %v: %s", containerPort, err, output)
	}
	return strings.TrimSpace(strings.Split(output, "\n")[0])
}

func docker(timeout time.Duration, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", arguments...).CombinedOutput() //nolint:gosec // the docker CLI with the test's own arguments
	return string(output), err
}
