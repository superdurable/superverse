package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/superdurable/dex-template-basic-process/internal/connectorconfiguration"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Worker owns the Dex Worker, the Dex Client used by the API, and their blob cache.
type Worker struct {
	Client    *dex.Client
	dexWorker *dex.Worker
	cache     *blobcache.Cache
}

func New(logger *slog.Logger) (*Worker, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	configuration, err := connectorconfiguration.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load connector configuration: %w", err)
	}
	connections, err := OpenConnections(configuration)
	if err != nil {
		return nil, err
	}
	registry, err := dex.NewRegistry(Application(connections))
	if err != nil {
		return nil, fmt.Errorf("register Flows: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: environment("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "application-blobs")), MaxBytes: 256 << 20, Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("create Dex blob cache: %w", err)
	}
	flowServiceAddress := environment("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	// Live sets a headless target that Dex resolves itself, so the Flows follow
	// it to each new version of the application.
	workerTarget := dex.WorkerTarget{Address: environment("DEX_WORKER_TARGET", "127.0.0.1:8811"), Headless: os.Getenv("DEX_WORKER_HEADLESS") == "true"}
	dexWorker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: environment("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8811"), WorkerTarget: workerTarget, FlowServiceAddress: flowServiceAddress, Logger: logger})
	if err != nil {
		_ = cache.Close()
		return nil, fmt.Errorf("create Dex Worker: %w", err)
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: flowServiceAddress, WorkerTarget: &workerTarget, Logger: logger})
	if err != nil {
		_ = dexWorker.Stop(context.Background())
		_ = cache.Close()
		return nil, fmt.Errorf("create Dex Client: %w", err)
	}
	return &Worker{Client: client, dexWorker: dexWorker, cache: cache}, nil
}

func (worker *Worker) Start() <-chan error {
	result := make(chan error, 1)
	go func() { result <- worker.dexWorker.Start() }()
	return result
}

func (worker *Worker) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(worker.dexWorker.Stop(ctx), worker.Client.Close(), worker.cache.Close())
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
