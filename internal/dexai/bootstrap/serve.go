// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// serve runs the Dex Worker, the business gRPC server, the health endpoints,
// the credential read endpoint and the Slack event relay until ctx ends or one of them stops, then
// drains the Worker before stopping the listeners.
func serve(ctx context.Context, config serverConfig, server *composition) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", config.grpcAddress)
	if err != nil {
		return err
	}
	var stopping atomic.Bool
	health := &http.Server{Addr: config.healthAddress, Handler: healthHandler(server.dex, server.worker, &stopping, config.nativeRunID), ReadHeaderTimeout: 5 * time.Second}
	// A read may wait 35 seconds for another read's renewal of a credential.
	credentials := &http.Server{Addr: config.credentialRead, Handler: server.credentials, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 2 * time.Minute}
	remoteAgent := server.remoteAgent.Server(config.remoteAgent.Listen)
	stopped := make(chan error, 8)
	go func() { stopped <- server.worker.Start() }()
	// Registration is bounded independently of the lifetime of the serving process.
	registration, cancelRegistration := context.WithTimeout(ctx, 2*time.Minute)
	err = server.worker.WaitPullRegistered(registration)
	cancelRegistration()
	if err != nil {
		drain, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), config.dexWorker.Pull.StopDrainTimeout+10*time.Second)
		defer cancelDrain()
		_ = server.worker.Stop(drain)
		_ = listener.Close()
		return fmt.Errorf("register platform pull Worker: %w", err)
	}

	go func() { stopped <- server.business.Serve(listener) }()
	go func() { stopped <- health.ListenAndServe() }()
	go func() { stopped <- credentials.ListenAndServe() }()
	go func() { stopped <- remoteAgent.ListenAndServe() }()
	// Slack's events answer at once; deliveries run until ctx ends, and the
	// listener serves only with the platform Slack application's secret.
	var slack *http.Server
	relaying, stopRelay := context.WithCancel(ctx)
	defer stopRelay()
	relayed := make(chan struct{})
	if len(config.slackSigningSecret) > 0 {
		slack = &http.Server{Addr: config.slackRelay.ListenAddress, Handler: http.HandlerFunc(server.slack.ServeEvents), ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
		go func() { stopped <- slack.ListenAndServe() }()
		go func() { server.slack.Run(relaying); close(relayed) }()
	} else {
		close(relayed)
	}
	// The local Git host serves clones and pushes that take as long as they take, so only
	// its header read and idle connections are bounded.
	var git *http.Server
	if config.localGit != nil {
		git = &http.Server{Addr: config.gitAddress, Handler: config.localGit, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
		go func() { stopped <- git.ListenAndServe() }()
	}
	// The Native gateway polls its routes from this listener on the loopback.
	var routes *http.Server
	if server.routes != nil {
		routes = &http.Server{Addr: config.routesListen, Handler: server.routes, ReadHeaderTimeout: 5 * time.Second}
		go func() { stopped <- routes.ListenAndServe() }()
	}
	select {
	case <-ctx.Done():
	case err = <-stopped:
	}

	stopping.Store(true)
	var rejection *dex.PullRegistrationError
	if errors.As(err, &rejection) {
		slog.ErrorContext(context.WithoutCancel(ctx), "Platform pull Worker registration rejected", "queue", rejection.Queue, "code", status.Code(rejection.Err))
	}
	drain, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), config.dexWorker.Pull.StopDrainTimeout+10*time.Second)
	drainErr := server.worker.Stop(drain)
	expectedCancellation := errors.Is(drainErr, context.DeadlineExceeded) && drain.Err() == nil
	cancelDrain()
	if expectedCancellation {
		slog.InfoContext(context.WithoutCancel(ctx), "Platform pull Worker drain elapsed; handlers cancelled")
	}
	if drainErr != nil && !expectedCancellation {
		err = errors.Join(err, fmt.Errorf("stop platform pull Worker: %w", drainErr))
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	server.remoteAgent.Close()
	_ = health.Shutdown(shutdown)
	_ = credentials.Shutdown(shutdown)
	if err := remoteAgent.Shutdown(shutdown); err != nil {
		_ = remoteAgent.Close()
	}
	if slack != nil {
		_ = slack.Shutdown(shutdown)
	}
	// Queued events are dropped: the relay is best effort.
	stopRelay()
	<-relayed
	if git != nil {
		if gitErr := git.Shutdown(shutdown); gitErr != nil {
			_ = git.Close()
		}
		// Host.Close retains the process owner lock until every cancelled HTTP handler returns.
	}
	if routes != nil {
		_ = routes.Shutdown(shutdown)
	}
	drained := make(chan struct{})
	go func() { server.business.GracefulStop(); close(drained) }()
	select {
	case <-drained:
	case <-shutdown.Done():
		server.business.Stop()
	}
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}
	return err
}

// healthHandler serves liveness and Dex readiness. A Local Native server
// labels both with its run ID, because its launcher accepts readiness only
// from the run it started.
func healthHandler(client *dex.Client, worker *dex.Worker, stopping *atomic.Bool, nativeRunID string) http.Handler {
	labeled := func(handler http.HandlerFunc) http.HandlerFunc {
		if nativeRunID == "" {
			return handler
		}
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Superverse-Native-Run-ID", nativeRunID)
			handler(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", labeled(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	mux.HandleFunc("/readyz", labeled(func(w http.ResponseWriter, r *http.Request) {
		if stopping.Load() || !worker.PullRegistrationState().IsReady {
			http.Error(w, "Worker not ready", http.StatusServiceUnavailable)
			return
		}
		check, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if _, err := client.HealthCheck(check); err != nil {
			http.Error(w, "Dex unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	return mux
}
