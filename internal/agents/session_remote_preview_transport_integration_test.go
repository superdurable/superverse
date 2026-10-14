// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"io"
	"net"
	"os/exec"
	"strings"
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

// The Flow target is how platform Dex reaches the real lane Worker, not how
// this host reaches it. Validate the selected lane before translating it.
// Native's launcher binds the SAME effective port on loopback; Kind requires
// a private, bounded forward to the SAME Service, never another test Worker.
func remotePreviewWorkerDialTarget(ctx context.Context, t *testing.T, original string, cleanup *remotePreviewCleanup) string {
	t.Helper()
	if original != dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET") {
		t.Fatal("Preview relay original Worker target differs from the selected lane")
	}
	host, port, err := net.SplitHostPort(original)
	if err != nil {
		t.Fatal("Preview relay original Worker target is not host:port")
	}
	if dexflowtest.LocalLaneForTest(t) != dexflowtest.LaneKindForTest {
		if host != "host.docker.internal" {
			t.Fatal("Preview relay Native target differs from the effective launcher callback")
		}
		address := net.JoinHostPort("127.0.0.1", port)
		awaitRemotePreviewWorkerPort(ctx, t, address, nil)
		return address
	}
	parts := strings.Split(host, ".")
	if len(parts) != 5 || strings.Join(parts[2:], ".") != "svc.cluster.local" {
		t.Fatal("Preview relay Kind target does not name the selected Worker Service")
	}
	localPort := dexflowtest.FreePortForTest(ctx, t)
	address := net.JoinHostPort("127.0.0.1", localPort)
	// Survive body cancellation and ordinary fixture deletion when config
	// restoration is unconfirmed. The early owner always closes this forward
	// last, even when terminal confirmation fails, within this hard lifetime.
	deadline := time.Now().Add(previewSetupTimeout + 2*previewDeployTimeout + remotePreviewCleanupLifetime)
	if testDeadline, ok := t.Deadline(); ok && testDeadline.Add(remotePreviewCleanupLifetime).Before(deadline) {
		deadline = testDeadline.Add(remotePreviewCleanupLifetime)
	}
	forward, cancel := context.WithDeadline(context.Background(), deadline)
	command := exec.CommandContext(forward, "kubectl", "--kubeconfig", dexflowtest.Setting(t, "SUPERVERSE_TEST_KIND_KUBECONFIG"), //nolint:gosec // the validated lane's exact Worker Service
		"--context", "kind-superverse-v2", "--request-timeout=15s", "-n", parts[1], "port-forward", "--address=127.0.0.1", "service/"+parts[0], localPort+":"+port)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.WaitDelay = 5 * time.Second
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal("Preview relay exact Kind Worker forward could not start")
	}
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()
	cleanup.closeForward = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Preview relay owned Worker forward did not exit within5s")
		}
	}
	awaitRemotePreviewWorkerPort(ctx, t, address, done)
	return address
}

func awaitRemotePreviewWorkerPort(ctx context.Context, t *testing.T, address string, stopped <-chan struct{}) {
	t.Helper()
	ready, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stopped:
			t.Fatal("Preview relay exact Worker forward exited before readiness")
		default:
		}
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ready, "tcp", address)
		if err == nil {
			_ = connection.Close()
			return
		}
		select {
		case <-ready.Done():
			t.Fatal("Preview relay real Worker host transport was not reachable within10s")
		case <-stopped:
			t.Fatal("Preview relay exact Worker forward exited before readiness")
		case <-ticker.C:
		}
	}
}
