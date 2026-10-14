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
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
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
	registry, err := dex.NewRegistry([]dex.Flow{GenerationProbeFlow{}})
	if err != nil {
		return err
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: os.Getenv("DEX_BLOB_CACHE_DIR"), MaxBytes: 64 << 20, Logger: logger})
	if err != nil {
		return err
	}
	defer func() { _ = cache.Close() }()
	target := dex.WorkerTarget{Address: os.Getenv("DEX_WORKER_TARGET"), Headless: os.Getenv("DEX_WORKER_HEADLESS") == "true"}
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: os.Getenv("DEX_WORKER_BIND_ADDRESS"), WorkerTarget: target, FlowServiceAddress: address, Logger: logger})
	if err != nil {
		return err
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address, WorkerTarget: &target, Logger: logger})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	server := &http.Server{Addr: ":" + os.Getenv("PORT"), Handler: handler(client), ReadHeaderTimeout: 5 * time.Second}
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.ListenAndServe() }()
	select {
	case err = <-workerResult:
	case err = <-serverResult:
	}
	return err
}

// handler serves the health path and lets a test start, release and read a
// probe Flow by its ID.
func handler(client *dex.Client) http.Handler {
	mux := http.NewServeMux()
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
