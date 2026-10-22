package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-template-basic-process/internal/connectorconfiguration"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Worker owns the Dex Worker, the Dex Client used by the API and the Slack
// Trigger runner, and their blob cache.
type Worker struct {
	lifecycle *lifecycle
	Client    *dex.Client
	// Triggers receives the relayed Slack events at slack.RelayedTriggerPath;
	// run it with Run beside the Worker.
	Triggers  *slack.MessageTriggerRunner
	dexWorker *dex.Worker
	cache     *blobcache.Cache
}

func New(logger *slog.Logger) (*Worker, error) {
	lifecycle, err := loadRuntime()
	if err != nil {
		return nil, err
	}
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
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(environment("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "application-blobs")), string(lifecycle.mode)), MaxBytes: 256 << 20, Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("create Dex blob cache: %w", err)
	}
	flowServiceAddress := environment("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	options := lifecycle.options(flowServiceAddress)
	options.Logger = logger
	dexWorker, err := dex.NewWorker(registry, cache, options)
	if err != nil {
		_ = cache.Close()
		return nil, fmt.Errorf("create Dex Worker: %w", err)
	}
	workerTarget := dexWorker.WorkerTarget()
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: flowServiceAddress, WorkerTarget: workerTarget, Logger: logger})
	if err != nil {
		_ = dexWorker.Stop(context.Background())
		_ = cache.Close()
		return nil, fmt.Errorf("create Dex Client: %w", err)
	}
	var triggers *slack.MessageTriggerRunner
	if lifecycle.mode == modeWeb {
		triggers, err = OpenTriggers(configuration, connections, client, logger)
		if err != nil {
			_ = client.Close()
			_ = dexWorker.Stop(context.Background())
			_ = cache.Close()
			return nil, err
		}
	}
	return &Worker{lifecycle: lifecycle, Client: client, Triggers: triggers, dexWorker: dexWorker, cache: cache}, nil
}

func (worker *Worker) Start() <-chan error {
	if worker.IsWorker() {
		return worker.startDex()
	}
	return make(chan error)
}

func (worker *Worker) Close() error {
	worker.MarkStopping()
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
	defer cancel()
	return errors.Join(worker.dexWorker.Stop(ctx), worker.Client.Close(), worker.cache.Close())
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
