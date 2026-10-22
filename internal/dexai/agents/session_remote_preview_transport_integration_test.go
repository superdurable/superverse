// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// projecttest owns two ordinary cleanup waits (Project then TeamSpace), each
// bounded to10min. Leave one additional minute for config/terminal readback;
// this is solely the lifetime of the test's owned local forwarding process.
const remotePreviewCleanupLifetime = 21 * time.Minute

type remotePreviewCleanup struct {
	reader                   dexpb.FlowServiceClient
	gate                     *remotePreviewGate
	restored                 bool
	closeRelay, closeForward func()
	closeOnce                sync.Once
}

// Register BEFORE fixture creation. Ordinary Project deletion is the sole
// deletion owner and runs before this finalizer. Its read-only connection is
// independently owned, so fixture clients may close before terminal readback.
func newRemotePreviewCleanup(t *testing.T) *remotePreviewCleanup {
	t.Helper()
	connection, err := grpc.NewClient(dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal("Preview relay independent cleanup reader connection failed")
	}
	cleanup := &remotePreviewCleanup{reader: dexpb.NewFlowServiceClient(connection)}
	t.Cleanup(func() {
		defer func() {
			if err := connection.Close(); err != nil {
				t.Error("Preview relay independent cleanup reader close failed")
			}
		}()
		defer cleanup.close()
		if cleanup.gate == nil || cleanup.restored {
			return
		}
		cleanup.gate.open()
		terminal, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if !cleanup.awaitTerminal(terminal) {
			t.Errorf("Preview relay residual config/terminal state unconfirmed for FlowID=%s RunID=%s; transport shutdown is bounded, resource cleanup is NOT proven", cleanup.gate.flowID, cleanup.gate.runID)
		}
	})
	return cleanup
}

func (cleanup *remotePreviewCleanup) close() {
	cleanup.closeOnce.Do(func() {
		if cleanup.closeRelay != nil {
			cleanup.closeRelay()
		}
		if cleanup.closeForward != nil {
			cleanup.closeForward()
		}
	})
}

func (cleanup *remotePreviewCleanup) awaitTerminal(ctx context.Context) bool {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		read, cancel := context.WithTimeout(ctx, 3*time.Second)
		summary, err := cleanup.reader.GetFlowSummary(read, &dexpb.GetFlowSummaryRequest{FlowId: cleanup.gate.flowID, RunId: cleanup.gate.runID})
		cancel()
		if err == nil && summary.GetFlowExecutionId().GetFlowId() == cleanup.gate.flowID && summary.GetFlowExecutionId().GetRunId() == cleanup.gate.runID && summary.GetFlowStatus() == dexpb.FlowStatus_FLOW_STATUS_COMPLETED {
			// An old completed Run alone cannot prove the Session is terminal:
			// reject a newer current Run instead of shutting down its callback.
			read, cancel := context.WithTimeout(ctx, 3*time.Second)
			current, err := cleanup.reader.GetFlowSummary(read, &dexpb.GetFlowSummaryRequest{FlowId: cleanup.gate.flowID})
			cancel()
			if err == nil {
				return current.GetFlowExecutionId().GetFlowId() == cleanup.gate.flowID && current.GetFlowExecutionId().GetRunId() == cleanup.gate.runID && current.GetFlowStatus() == dexpb.FlowStatus_FLOW_STATUS_COMPLETED
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// The gated Session uses a push Worker in a child process with the production
// assembly. Native host composition uses the effective lane endpoints directly.
func remotePreviewWorkerDialTarget(ctx context.Context, t *testing.T, cleanup *remotePreviewCleanup) string {
	t.Helper()
	if dexflowtest.LocalLaneForTest(t) == dexflowtest.LaneKindForTest {
		t.Fatal("held Preview race requires Native: production host composition cannot reach Kind-only Sandbox addresses")
	}
	if os.Getenv("SUPERVERSE_CONFIG_FILE") == "" {
		t.Fatal("held Preview race requires the selected Native run's effective SUPERVERSE_CONFIG_FILE and secret environment")
	}
	binary := filepath.Join(t.TempDir(), "platform-worker.test")
	build := exec.CommandContext(ctx, "go", "test", "-c", "-o", binary, "../bootstrap")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compile real platform Worker fixture: %v %s", err, output)
	}
	port := dexflowtest.FreePortForTest(ctx, t)
	deadline := time.Now().Add(previewSetupTimeout + 2*previewDeployTimeout + remotePreviewCleanupLifetime)
	if testDeadline, ok := t.Deadline(); ok && testDeadline.Add(remotePreviewCleanupLifetime).Before(deadline) {
		deadline = testDeadline.Add(remotePreviewCleanupLifetime)
	}
	lifetime, cancelLifetime := context.WithDeadline(context.Background(), deadline)
	child := exec.CommandContext(lifetime, binary, "-test.run=^TestPlatformPushWorkerProcess$", "-test.timeout=35m")
	child.Env = append(os.Environ(), "SUPERVERSE_TEST_PUSH_WORKER_PORT="+port)
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		found := false
		for scanner.Scan() {
			if scanner.Text() == "PULLPLATFORM_PUSH_WORKER_READY" && !found {
				ready <- true
				found = true
			}
		}
		if !found {
			ready <- false
		}
	}()
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	cleanup.closeForward = func() {
		defer cancelLifetime()
		_ = input.Close()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = child.Process.Kill()
			<-done
		}
	}
	select {
	case ok := <-ready:
		if !ok {
			cleanup.closeForward()
			t.Fatal("real platform Worker child exited before readiness")
		}
	case <-ctx.Done():
		cleanup.closeForward()
		t.Fatal(ctx.Err())
	}
	return net.JoinHostPort("127.0.0.1", port)
}
