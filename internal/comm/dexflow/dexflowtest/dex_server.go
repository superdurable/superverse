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
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Setting returns a required setting of an integration test, failing the
// test when it is missing. A setting that holds an address of a local lane
// (the laneInputs) also fails the test unless the whole environment belongs
// to the lane that SUPERVERSE_TEST_LANE names.
func Setting(t testing.TB, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("requires %s", name)
	}
	for _, input := range laneInputs {
		if input.name == name {
			requireLane(t)
		}
	}
	return value
}

// ClientForTest returns a client of the Dex Server at address that knows flows. A
// nil target leaves Worker routing to the Server; a target is the server
// Worker of the lane, or a Worker of the test on a port FreePortForTest
// returned, on the lane's test Worker host. The client and its blob cache, in
// the test's temporary directory, close when the test ends. The test fails
// before it connects unless address and target belong to the lane that
// SUPERVERSE_TEST_LANE names.
func ClientForTest(t testing.TB, address string, target *dex.WorkerTarget, flows ...dex.Flow) *dex.Client {
	t.Helper()
	passed := []passedInput{{"the Dex address passed to Client", dexEndpoint, address}}
	if target != nil {
		passed = append(passed, workerTargetInput(target.Address))
	}
	requireLane(t, passed...)
	registry, cache := registryAndCache(t, flows)
	return client(t, registry, cache, address, target)
}

// ServerWorkerClient is ClientForTest for the lane's server Worker
// (SUPERVERSE_TEST_DEX_ADDRESS and SUPERVERSE_TEST_EXISTING_WORKER_TARGET),
// which runs the executions with every dependency of the server; the flows
// only supply the client's definitions. Test fixtures that act through a
// Flow of their own use it.
func ServerWorkerClient(t testing.TB, flows ...dex.Flow) *dex.Client {
	t.Helper()
	target := &dex.WorkerTarget{Address: Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")}
	return ClientForTest(t, Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), target, flows...)
}

// WorkerClientForTest serves flows from the test process: it starts a Worker for the
// Dex Server at address on an ephemeral port, which the Server reaches on
// workerHost, waits until the Worker listens and returns a client whose
// executions run on it. Everything stops when the test ends. The test fails
// before it starts the Worker unless address and workerHost belong to the
// lane that SUPERVERSE_TEST_LANE names.
func WorkerClientForTest(ctx context.Context, t testing.TB, address, workerHost string, flows ...dex.Flow) *dex.Client {
	t.Helper()
	return WorkerClientAtForTest(ctx, t, address, workerHost, FreePortForTest(ctx, t), flows...)
}

// FreePortForTest returns a loopback port that nothing listens on, which a test names
// as a Worker target before a Worker serves it.
func FreePortForTest(ctx context.Context, t testing.TB) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	closeOrReport(t, "port probe listener", listener.Close)
	testWorkerPorts.Store(port, struct{}{})
	return port
}

// testWorkerPorts are the ports FreePortForTest returned, on which a Worker of
// the test process serves or will serve.
var testWorkerPorts sync.Map

// workerTargetInput is the lane input that a Worker target passed to
// ClientForTest names: the lane's test Worker host when the port is one
// FreePortForTest returned, otherwise the lane's server Worker target. Another
// instance's server Worker may share the host, so only the test's own ports
// pass on the host alone.
func workerTargetInput(address string) passedInput {
	if host, port, err := net.SplitHostPort(address); err == nil {
		if _, own := testWorkerPorts.Load(port); own {
			return passedInput{"the host of the test Worker target passed to Client", workerHostEndpoint, host}
		}
	}
	return passedInput{"the Worker target passed to Client", workerTargetEndpoint, address}
}

// WorkerClientAtForTest is WorkerClientForTest on port, which FreePortForTest returned. Executions
// that earlier named the port as their Worker target meet the Worker once it
// serves. The lane check of WorkerClient applies.
func WorkerClientAtForTest(ctx context.Context, t testing.TB, address, workerHost, port string, flows ...dex.Flow) *dex.Client {
	t.Helper()
	client, _ := workerClientAt(ctx, t, address, workerHost, port, flows...)
	return client
}

// WorkerClientWithReplacementForTest serves only the test's flows, with a
// single replacement on the same owned port using a fresh registry/blob cache.
// The caller terminates every disposable execution before final test cleanup;
// this recreates a test Worker, not the merged platform server process.
func WorkerClientWithReplacementForTest(ctx context.Context, t testing.TB, address, workerHost string, flows ...dex.Flow) (*dex.Client, func() *dex.Client) {
	t.Helper()
	port := FreePortForTest(ctx, t)
	current, stop := workerClientAt(ctx, t, address, workerHost, port, flows...)
	replaced := false
	return current, func() *dex.Client {
		t.Helper()
		if replaced {
			t.Fatal("test Worker replacement is bounded to one")
		}
		replaced = true
		if err := stop(); err != nil {
			t.Fatalf("test Worker did not stop before replacement: %T", err)
		}
		fresh, _ := workerClientAt(ctx, t, address, workerHost, port, flows...)
		return fresh
	}
}

func workerClientAt(ctx context.Context, t testing.TB, address, workerHost, port string, flows ...dex.Flow) (*dex.Client, func() error) {
	t.Helper()
	requireLane(t, passedInput{"the Dex address passed to WorkerClient", dexEndpoint, address},
		passedInput{"the Worker host passed to WorkerClient", workerHostEndpoint, workerHost})
	registry, cache := registryAndCache(t, flows)
	target := dex.WorkerTarget{Address: net.JoinHostPort(workerHost, port), Headless: true}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: net.JoinHostPort("0.0.0.0", port), WorkerTarget: target, FlowServiceAddress: address})
	if err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- worker.Start() }()
	var stopped sync.Once
	var stopError error
	stop := func() error {
		stopped.Do(func() {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			stopError = worker.Stop(stopCtx)
		})
		return stopError
	}
	t.Cleanup(func() { closeOrReport(t, "Worker", stop) })
	// Probe the Worker's listener; a fixed sleep cannot tell when it serves.
	for {
		probe, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
		if err == nil {
			closeOrReport(t, "Worker probe", probe.Close)
			return client(t, registry, cache, address, &target), stop
		}
		select {
		case err := <-exited:
			t.Fatalf("the Worker exited: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func registryAndCache(t testing.TB, flows []dex.Flow) (*dex.Registry, *blobcache.Cache) {
	t.Helper()
	registry, err := dex.NewRegistry(flows)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: t.TempDir(), MaxBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeOrReport(t, "blob cache", cache.Close) })
	return registry, cache
}

func client(t testing.TB, registry *dex.Registry, cache *blobcache.Cache, address string, target *dex.WorkerTarget) *dex.Client {
	t.Helper()
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address, WorkerTarget: target})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeOrReport(t, "Dex client", client.Close) })
	return client
}

// ExecutionReaderForTest reads engine state from the Dex Server at address. Its
// connection closes when the test ends. The test fails before it connects
// unless address belongs to the lane that SUPERVERSE_TEST_LANE names.
func ExecutionReaderForTest(t testing.TB, address string) dexpb.FlowServiceClient {
	t.Helper()
	requireLane(t, passedInput{"the Dex address passed to ExecutionReader", dexEndpoint, address})
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeOrReport(t, "execution reader", connection.Close) })
	return dexpb.NewFlowServiceClient(connection)
}

// RequireAbsentForTest fails the test unless no execution of flowID exists.
func RequireAbsentForTest(ctx context.Context, t testing.TB, client *dex.Client, flowID string) {
	t.Helper()
	_, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	var missing *dex.FlowNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("requires a genuinely absent execution of %s (%s)", flowID, ErrorCategory(err))
	}
}

// RequireCompletedForTest fails the test unless the execution of flowID completed.
func RequireCompletedForTest(ctx context.Context, t testing.TB, client *dex.Client, flowID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: false})
	if err != nil {
		t.Fatalf("status read for %s failed: %s", flowID, ErrorCategory(err))
	}
	if result.Status != dex.FlowCompleted {
		t.Fatalf("requires a genuinely completed resource: %s status=%s", flowID, result.Status)
	}
}

// ReadRetainedForTest invokes the query-only rpc of the completed execution of
// flowID into output and fails unless the execution stays completed.
func ReadRetainedForTest(ctx context.Context, t testing.TB, client *dex.Client, flowID string, rpc, output any) {
	t.Helper()
	RequireCompletedForTest(ctx, t, client, flowID)
	readCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := client.InvokeRPC(readCtx, flowID, rpc, nil, output); err != nil {
		t.Fatalf("retained RPC for %s failed: %s", flowID, ErrorCategory(err))
	}
	RequireCompletedForTest(ctx, t, client, flowID)
}

// ErrorCategory names the typed category of a Dex error. A Worker's message
// can carry application data, so integration logs show only the category.
func ErrorCategory(err error) string {
	var missing *dex.FlowNotFoundError
	var inactive *dex.FlowNotActiveOrNotFoundError
	var service *dex.ServiceError
	switch {
	case err == nil:
		return "no error"
	case errors.As(err, &missing):
		return "FlowNotFoundError"
	case errors.As(err, &inactive):
		return "FlowNotActiveOrNotFoundError"
	case errors.As(err, &service):
		return fmt.Sprintf("DexServiceError(code=%s,substatus=%d)", service.Code, service.SubStatus)
	case errors.Is(err, context.DeadlineExceeded):
		return "DeadlineExceeded"
	default:
		return fmt.Sprintf("%T", err)
	}
}

func closeOrReport(t testing.TB, name string, closeResource func() error) {
	if err := closeResource(); err != nil {
		t.Errorf("close %s: %v", name, err)
	}
}
