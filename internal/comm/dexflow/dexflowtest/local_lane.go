// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflowtest

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// localLane is a local lane that an integration test runs against.
type localLane struct{ name string }

// The local lanes.
var (
	// LaneKindForTest is Local Kind: the platform and every project in the Kind
	// cluster.
	LaneKindForTest = localLane{"kind"}
	// laneNative is Local Native: the server on this machine and every project
	// on the local Docker Engine.
	laneNative = localLane{"native"}
)

// laneSetting is the environment variable that names the lane a test runs
// against.
const laneSetting = "SUPERVERSE_TEST_LANE"

// String is the lane's name, as SUPERVERSE_TEST_LANE spells it.
func (l localLane) String() string { return l.name }

// LocalLaneForTest reads the lane that SUPERVERSE_TEST_LANE names, kind or native,
// and fails the test on anything else.
func LocalLaneForTest(t testing.TB) localLane {
	t.Helper()
	name := Setting(t, laneSetting)
	for _, lane := range []localLane{LaneKindForTest, laneNative} {
		if lane.name == name {
			return lane
		}
	}
	t.Fatalf("SUPERVERSE_TEST_LANE must be %s or %s, not %q", LaneKindForTest, laneNative, name)
	return localLane{}
}

// RestartServerForTest replaces the platform server, and with it the application
// Worker, of the local lane the test runs against, as an operator does, and
// returns once platform Dex invokes the new Worker. Local Kind rolls the
// server Deployment, requires replacement registration readiness before old
// Pod termination, and waits until the old Pod is gone; Local Native
// asks the launcher of its run (SUPERVERSE_TEST_NATIVE_RUN_ID) to restart the
// host server, which answers once the new server is ready. Platform Dex
// reaches a new Worker later than the Worker serves, so the next read could
// still meet the replacement, also in the next test: read, an RPC read of an
// existing Flow through platform Dex, repeats while Dex answers that it cannot
// invoke the Worker (*dex.WorkerInvocationError), for at most
// workerInvocationWait, and any other error fails the test.
func RestartServerForTest(ctx context.Context, t testing.TB, read func(context.Context) error) {
	t.Helper()
	var command *exec.Cmd
	switch LocalLaneForTest(t) {
	case LaneKindForTest:
		rollKindServer(ctx, t)
	case laneNative:
		command = exec.CommandContext(ctx, filepath.Join(repositoryRoot(t), "infra-as-code/sv2"), "native", "restart-server", //nolint:gosec // the repository's launcher
			"--instance", nativeInstance(t), "--run-id", Setting(t, "SUPERVERSE_TEST_NATIVE_RUN_ID"))
	}
	if command != nil {
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("Worker replacement failed: %s", out)
		}
	}
	replaced := time.Now()
	wait, cancel := context.WithTimeout(ctx, workerInvocationWait)
	defer cancel()
	for refused := 0; ; refused++ {
		call, done := context.WithTimeout(wait, workerInvocationReadTimeout)
		err := read(call)
		done()
		var unreachable *dex.WorkerInvocationError
		if err == nil {
			t.Logf("platform Dex invoked the new Worker %s after its replacement; %d earlier reads answered that it could not", time.Since(replaced).Round(100*time.Millisecond), refused)
			return
		}
		if !errors.As(err, &unreachable) {
			t.Fatalf("the read after the Worker replacement failed (%T)", err)
		}
		select {
		case <-wait.Done():
			t.Fatalf("platform Dex did not invoke the new Worker within %s", workerInvocationWait)
		case <-time.After(time.Second):
		}
	}
}

func nativeInstance(t testing.TB) string {
	t.Helper()
	if value := os.Getenv(nativeInstanceSetting); value != "" {
		return value
	}
	return defaultNativeInstance
}

// defaultNativeInstance is the Local Native instance that an unset
// SUPERVERSE_TEST_NATIVE_INSTANCE selects.
const defaultNativeInstance = "default"

// The bounds of a Worker replacement: the deletion of the server Pod, which
// includes its graceful shutdown, the wait until platform Dex invokes the new
// Worker, which covers its reconnection backoff, and one read in that wait.
const (
	podDeletionWait             = 3 * time.Minute
	workerInvocationWait        = 5 * time.Minute
	workerInvocationReadTimeout = 30 * time.Second
)

// repositoryRoot is the directory of go.mod above the test's directory.
func repositoryRoot(t testing.TB) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err = os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("the repository root is not above the test's directory")
		}
		directory = parent
	}
}
