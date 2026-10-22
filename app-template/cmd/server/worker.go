package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/superdurable/dex-template-basic-process/internal/worker"
)

// runWorker exposes registration readiness while the separate process executes Flows.
func runWorker(application *worker.Worker) error {
	mux := http.NewServeMux()
	mux.Handle("/readyz", application.ReadinessHandler())
	server := &http.Server{Addr: application.ReadinessAddress(), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	listening := make(chan error, 1)
	go func() { listening <- server.ListenAndServe() }()
	running := application.Start()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	var failure error
	select {
	case <-ctx.Done():
	case err := <-running:
		if err != nil {
			failure = fmt.Errorf("run Dex Worker: %w", err)
		}
	case err := <-listening:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			failure = fmt.Errorf("serve Worker readiness: %w", err)
		}
	}
	application.MarkStopping()
	stopped := application.Close()
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	return errors.Join(failure, stopped, server.Shutdown(shutdown))
}
