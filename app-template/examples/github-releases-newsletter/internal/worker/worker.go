package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-template-basic-process/internal/connectorconfiguration"
	"github.com/superdurable/dex-template-basic-process/internal/process"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

type Worker struct {
	Client    *dex.Client
	dexWorker *dex.Worker
	trigger   *slack.MessageTriggerRunner
	cancel    context.CancelFunc
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
	clients := &process.Clients{}
	registry, err := dex.NewRegistry(Application(connections, clients))
	if err != nil {
		return nil, fmt.Errorf("register Flows: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: environment("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "application-blobs")), MaxBytes: 256 << 20, Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("create Dex blob cache: %w", err)
	}
	flowServiceAddress := environment("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	workerTarget := dex.WorkerTarget{Address: environment("DEX_WORKER_TARGET", "127.0.0.1:8811"), Headless: os.Getenv("DEX_WORKER_HEADLESS") == "true"}
	dexWorker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:  environment("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8811"),
		WorkerTarget: workerTarget, FlowServiceAddress: flowServiceAddress, Logger: logger,
	})
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
	clients.Dex = client

	var triggerConfiguration slack.ChannelThreadCreatedTriggerConfiguration
	err = configuration.Project().Configuration.DecodeTriggerConfiguration(
		slack.ConnectorID, process.SlackConnectionName,
		slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName,
		process.NewsletterTriggerBinding, &triggerConfiguration,
	)
	if err != nil {
		_ = client.Close()
		_ = dexWorker.Stop(context.Background())
		_ = cache.Close()
		return nil, fmt.Errorf("configure newsletter trigger: %w", err)
	}
	filter, err := process.NewNewsletterTriggerFilter(triggerConfiguration)
	if err != nil {
		_ = client.Close()
		_ = dexWorker.Stop(context.Background())
		_ = cache.Close()
		return nil, err
	}
	trigger, err := slack.NewProjectMessageTriggerRunner(configuration.Project(), process.SlackConnectionName,
		slack.ProjectMessageTriggerRunnerConfig{ChannelThreadCreatedRoutes: []slack.ProjectChannelThreadCreatedTriggerRoute{{
			BindingName: process.NewsletterTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(client, NewsletterFlow(connections, clients), filter,
				process.ResolveNewsletterFlowID, process.MapNewsletterTrigger),
		}}}, slack.WithLogger(logger))
	if err != nil {
		_ = client.Close()
		_ = dexWorker.Stop(context.Background())
		_ = cache.Close()
		return nil, fmt.Errorf("start newsletter trigger: %w", err)
	}
	return &Worker{Client: client, dexWorker: dexWorker, trigger: trigger, cache: cache}, nil
}

func (worker *Worker) Start() <-chan error {
	result := make(chan error, 2)
	ctx, cancel := context.WithCancel(context.Background())
	worker.cancel = cancel
	go func() { result <- worker.dexWorker.Start() }()
	go func() { result <- worker.trigger.Run(ctx) }()
	return result
}

func (worker *Worker) TriggerHandler() http.Handler { return worker.trigger }

func (worker *Worker) Close() error {
	if worker.cancel != nil {
		worker.cancel()
	}
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
