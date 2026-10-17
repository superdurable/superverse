//go:build apptest

// Package apptest runs API tests of the application against the engine that
// make test-app starts: a real Dex and Temporal that exist only for the run.
// Connector mocks live here only; the commit gate rejects them elsewhere.
package apptest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	api "github.com/superdurable/dex-template-basic-process/internal/api"
	"github.com/superdurable/dex-template-basic-process/internal/api/generated"
	"github.com/superdurable/dex-template-basic-process/internal/process"
	"github.com/superdurable/dex-template-basic-process/internal/worker"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// engineWait bounds how long a test waits for the engine before it fails.
const engineWait = 30 * time.Second

// Env is the application of one test: its HTTP API, served by the real
// handler, and the Dex Client behind it.
type Env struct {
	// API is the generated client of openapi/openapi.yaml. Tests drive the
	// application through it, as the browser does.
	API *generated.Client
	// Dex is for what Studio does: an Inbox Action, a Display read or a
	// Start Flow. Never use it to bypass the API.
	Dex *dex.Client
}

// Start runs the Flows of worker.Application with connections on a Worker of
// the test engine and serves the API on a loopback server. Everything stops
// when the test ends. A missing or unreachable engine fails the test within
// 30 seconds; it never skips it.
func Start(t *testing.T, connections worker.Connections) *Env {
	t.Helper()
	recordOutcome(t)
	flowService, bindAddress, workerAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS"), os.Getenv("DEX_WORKER_BIND_ADDRESS"), os.Getenv("DEX_WORKER_TARGET")
	if flowService == "" || bindAddress == "" || workerAddress == "" {
		t.Fatal("the test engine is not configured: run the tests with make test-app")
	}
	clients := &process.Clients{}
	registry, err := dex.NewRegistry(worker.Application(connections, clients))
	if err != nil {
		t.Fatalf("register the Flows: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cache, err := blobcache.New(&blobcache.Config{Dir: t.TempDir(), MaxBytes: 64 << 20, Logger: logger})
	if err != nil {
		t.Fatalf("create the blob cache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	target := dex.WorkerTarget{Address: workerAddress}
	dexWorker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: bindAddress, WorkerTarget: target,
		FlowServiceAddress: flowService, AttributeIndexSyncTimeout: engineWait, Logger: logger})
	if err != nil {
		t.Fatalf("create the Worker: %v", err)
	}
	served := make(chan error, 1)
	go func() { served <- dexWorker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = dexWorker.Stop(ctx)
	})
	awaitListener(t, bindAddress, served)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: flowService, WorkerTarget: &target, Logger: logger})
	if err != nil {
		t.Fatalf("create the Dex Client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	clients.Dex = client
	handler, err := api.NewHandler(client)
	if err != nil {
		t.Fatalf("create the API handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	apiClient, err := generated.NewClient(server.URL)
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}
	return &Env{API: apiClient, Dex: client}
}

// awaitListener waits until the Worker listens, and fails the test when it
// stopped or did not listen in time.
func awaitListener(t *testing.T, address string, served <-chan error) {
	t.Helper()
	for deadline := time.Now().Add(engineWait); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		select {
		case err := <-served:
			t.Fatalf("the Worker did not start; does the test engine run (make test-app)? %v", err)
		default:
		}
		if connection, err := net.DialTimeout("tcp", address, time.Second); err == nil {
			_ = connection.Close()
			return
		}
	}
	t.Fatalf("the Worker did not listen on %s within %s", address, engineWait)
}

// Eventually polls condition every 100 milliseconds until it holds, and fails
// the test when it does not within timeout (at most 30 seconds). Use it for
// every wait; never sleep.
func (env *Env) Eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	for deadline := time.Now().Add(min(timeout, engineWait)); !condition(); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the condition did not hold within %s", min(timeout, engineWait))
		}
	}
}

// Context is the context of one call, bounded to 30 seconds and ended with
// the test.
func (env *Env) Context(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), engineWait)
	t.Cleanup(cancel)
	return ctx
}

// testOutcome is one test that ran through Start, as tests.json records it for
// the commit gate's requirement trace.
type testOutcome struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
}

var report struct {
	mu    sync.Mutex
	tests []testOutcome
}

// recordOutcome writes the outcome of t to tests.json in the run's report
// directory once t ends. It is the first cleanup, so it runs last and sees the
// failures of every other cleanup.
func recordOutcome(t *testing.T) {
	directory := os.Getenv("SUPERVERSE_APPTEST_REPORT_DIR")
	if directory == "" {
		return
	}
	t.Cleanup(func() {
		outcome := "passed"
		switch {
		case t.Failed():
			outcome = "failed"
		case t.Skipped():
			outcome = "skipped"
		}
		report.mu.Lock()
		defer report.mu.Unlock()
		report.tests = append(report.tests, testOutcome{Name: t.Name(), Outcome: outcome})
		data, err := json.Marshal(map[string]any{"tests": report.tests})
		if err == nil {
			path := filepath.Join(directory, "tests.json")
			err = errors.Join(os.WriteFile(path+".new", data, 0o600), os.Rename(path+".new", path))
		}
		if err != nil {
			t.Errorf("record the test outcome: %v", err)
		}
	})
}
