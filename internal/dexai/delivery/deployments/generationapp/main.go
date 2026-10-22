// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command generationapp is a Live application that the deployment integration
// tests build in several versions and run as consecutive Live generations. Its
// one Flow waits for a release and records which version ran each Step, so a
// test observes which generation's Worker the project Dex dispatches to. The
// build sets version, and removeAwait leaves AwaitRelease unregistered, as a
// version that removed a Step that open Flows still wait in.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Set by the build with -ldflags -X.
var (
	version     = "1"
	removeAwait = "false"
)

// releaseRetry bounds the attempts of AwaitRelease, so a version that no
// longer registers it reaches the failure route within the test.
var releaseRetry = &dex.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 5 * time.Second, TotalDuration: 45 * time.Second}

var (
	startedBy  = dex.DefineAttribute[string]("started-by")
	releasedBy = dex.DefineAttribute[string]("released-by")
	outcome    = dex.DefineAttribute[string]("outcome")
	release    = dex.DefineChannel[string]("release")
)

// GenerationProbeFlow waits for one release and records the version of the
// Worker that ran each Step.
type GenerationProbeFlow struct{ dex.FlowDefaults }

func (GenerationProbeFlow) GetSteps() []dex.StepDef {
	steps := []dex.StepDef{dex.DefineStartStep(RecordStart{}), dex.DefineStep(RecordFailureRoute{})}
	if removeAwait != "true" {
		steps = append(steps, dex.DefineStep(AwaitRelease{}))
	}
	return steps
}

func (f GenerationProbeFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(f.GetProbe, nil), dex.DefineRPC(f.Release, nil)}
}

func (GenerationProbeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{startedBy, releasedBy, outcome}, Channels: []dex.ChannelDef{release}}
}

// probeState is what GetProbe reads: the version of each Step's Worker and the
// outcome.
type probeState struct {
	StartedBy  string `json:"startedBy"`
	ReleasedBy string `json:"releasedBy"`
	Outcome    string `json:"outcome"`
	// ReadBy is the version of the Worker that served the read.
	ReadBy string `json:"readBy"`
}

func (GenerationProbeFlow) GetProbe(ctx dex.Context, _ dex.None) (*dex.RPCResult[probeState], error) {
	probe := probeState{ReadBy: version}
	var err error
	if probe.StartedBy, err = startedBy.Get(ctx); err != nil {
		return nil, err
	}
	if probe.ReleasedBy, err = releasedBy.Get(ctx); err != nil {
		return nil, err
	}
	probe.Outcome, err = outcome.Get(ctx)
	return &dex.RPCResult[probeState]{Output: probe}, err
}

// Release publishes the release the Flow waits for.
func (GenerationProbeFlow) Release(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	return &dex.RPCResult[dex.None]{}, release.Publish(ctx, version)
}

// RecordStart records the version that started the Flow.
type RecordStart struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (RecordStart) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if err := errors.Join(startedBy.Set(ctx, version), releasedBy.Set(ctx, ""), outcome.Set(ctx, "waiting")); err != nil {
		return nil, err
	}
	return dex.GoTo(AwaitRelease{}, nil), nil
}

// AwaitRelease waits for the release and records the version that ran it.
type AwaitRelease struct{ dex.StepDefaults }

func (AwaitRelease) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteRetry: releaseRetry, ExecuteFailure: dex.ProceedToOnExecuteFailure(RecordFailureRoute{}, nil)}
}

func (AwaitRelease) WaitFor(dex.Context, dex.None) (*dex.Wait, error) {
	return dex.Until(release.ForOne()), nil
}

func (AwaitRelease) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if err := errors.Join(releasedBy.Set(ctx, version), outcome.Set(ctx, "completed")); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(nil), nil
}

// RecordFailureRoute records that AwaitRelease exhausted its retries, with the
// version that ran the failure route.
type RecordFailureRoute struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (RecordFailureRoute) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if err := outcome.Set(ctx, "failure-route:"+version); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(nil), nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("generationapp stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if len(os.Args) != 2 {
		return errors.New("explicit process mode required")
	}
	flows := []dex.Flow{GenerationProbeFlow{}, OverlapProbeFlow{}}
	if strings.HasPrefix(version, "drain-") || version == "rejected" {
		flows = append(flows, DrainProbeFlow{})
	}
	registry, err := dex.NewRegistry(flows)
	if err != nil {
		return err
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(os.Getenv("DEX_BLOB_CACHE_DIR"), os.Args[1]), MaxBytes: 64 << 20, Logger: logger})
	if err != nil {
		return err
	}
	defer func() { _ = cache.Close() }()
	if len(os.Args) != 2 || (os.Args[1] != "web" && os.Args[1] != "worker") {
		return errors.New("explicit web or worker mode required")
	}
	target := dex.WorkerTarget{Pull: true}
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")

	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address, WorkerTarget: &target, Logger: logger})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if os.Args[1] == "web" {
		server := &http.Server{Addr: ":" + os.Getenv("PORT"), Handler: handler(client), ReadHeaderTimeout: 5 * time.Second}
		return server.ListenAndServe()
	}
	var settings struct {
		Worker struct {
			Mode                string        `yaml:"mode"`
			ReadinessAddress    string        `yaml:"readinessAddress"`
			MaxConcurrentTasks  int           `yaml:"maxConcurrentTasks"`
			IdleStreamsPerQueue int           `yaml:"idleStreamsPerQueue"`
			StopDrainTimeout    time.Duration `yaml:"stopDrainTimeout"`
		} `yaml:"worker"`
	}
	data, err := os.ReadFile("/tmp/superverse-app/runtime.yaml")
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return err
	}
	if settings.Worker.Mode != "pull" {
		return errors.New("fixture requires pull")
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address, Logger: logger,
		Pull: &dex.PullWorkerOptions{MaxConcurrentTasks: settings.Worker.MaxConcurrentTasks, IdleStreamsPerQueue: settings.Worker.IdleStreamsPerQueue,
			StopDrainTimeout: settings.Worker.StopDrainTimeout, StopOnPermanentStreamRejection: true}})
	if err != nil {
		return err
	}
	result := make(chan error, 2)
	go func() { result <- worker.Start() }()
	var ready atomic.Bool
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := worker.WaitPullRegistered(ctx); err != nil {
			result <- err
			return
		}
		ready.Store(true)
	}()
	server := &http.Server{Addr: settings.Worker.ReadinessAddress, ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if !ready.Load() || !worker.PullRegistrationState().IsReady {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		})}
	go func() { result <- server.ListenAndServe() }()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	select {
	case <-ctx.Done():
	case err = <-result:
	}
	ready.Store(false)
	if version == "blocked-drain" {
		// This real fixture intentionally ignores termination; the provider must enforce its eleven-minute grace.
		select {}
	}
	shutdown, stop := context.WithTimeout(context.Background(), 11*time.Minute)
	defer stop()
	return errors.Join(err, worker.Stop(shutdown), server.Shutdown(shutdown))

}

// handler serves the health path and lets a test start, release and read a
// probe Flow by its ID.
func handler(client *dex.Client) http.Handler {
	mux := http.NewServeMux()
	overlapHandlers(mux, client)
	drainHandlers(mux, client)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /probes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		_, err := client.StartFlow(r.Context(), GenerationProbeFlow{}, id, nil, dex.StartFlowOptions{IDReusePolicy: dex.IDReuseDisallow, RequestID: &id, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}})
		respond(w, nil, err)
	})
	mux.HandleFunc("POST /probes/{id}/release", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, client.InvokeRPC(r.Context(), r.PathValue("id"), GenerationProbeFlow{}.Release, nil, nil))
	})
	mux.HandleFunc("GET /probes/{id}", func(w http.ResponseWriter, r *http.Request) {
		var probe probeState
		err := client.InvokeRPC(r.Context(), r.PathValue("id"), GenerationProbeFlow{}.GetProbe, nil, &probe)
		respond(w, probe, err)
	})
	return mux
}

func respond(w http.ResponseWriter, body any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
