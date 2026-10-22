package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"gopkg.in/yaml.v2"
)

type runMode string

const (
	modeWeb    runMode = "web"
	modeWorker runMode = "worker"
	modePull           = "pull"
	modePush           = "push"
)

// runtimeSettings is the platform-owned runtime.yaml, shared by both modes.
type runtimeSettings struct {
	Worker struct {
		Mode                string        `yaml:"mode"`
		BindAddress         string        `yaml:"bindAddress"`
		Target              string        `yaml:"target"`
		ReadinessAddress    string        `yaml:"readinessAddress"`
		MaxConcurrentTasks  int           `yaml:"maxConcurrentTasks"`
		IdleStreamsPerQueue int           `yaml:"idleStreamsPerQueue"`
		StopDrainTimeout    time.Duration `yaml:"stopDrainTimeout"`
	} `yaml:"worker"`
}

type lifecycle struct {
	mode     runMode
	settings runtimeSettings
	ready    atomic.Bool
	stopping atomic.Bool
}

func loadRuntime() (*lifecycle, error) {
	if len(os.Args) != 2 || (runMode(os.Args[1]) != modeWeb && runMode(os.Args[1]) != modeWorker) {
		return nil, errors.New("run the application with web or worker")
	}
	contents, err := os.ReadFile("/tmp/superverse-app/runtime.yaml")
	if err != nil {
		return nil, fmt.Errorf("read runtime.yaml: %w", err)
	}
	var settings runtimeSettings
	if err := yaml.UnmarshalStrict(contents, &settings); err != nil {
		return nil, fmt.Errorf("parse runtime.yaml: %w", err)
	}
	options := settings.Worker
	if options.ReadinessAddress == "" || options.StopDrainTimeout <= 0 || options.StopDrainTimeout > 10*time.Minute || options.MaxConcurrentTasks < 1 || options.MaxConcurrentTasks > 16 || options.IdleStreamsPerQueue < 1 || options.IdleStreamsPerQueue > 8 {
		return nil, errors.New("runtime.yaml requires Worker readiness and a drain timeout within ten minutes")
	}
	switch options.Mode {
	case modePull:
		if options.BindAddress != "" || options.Target != "" {
			return nil, errors.New("Live pull has no push address")
		}
	case modePush:
		if !loopbackAddress(options.BindAddress) || !loopbackAddress(options.Target) {
			return nil, errors.New("Preview push requires the loopback Worker address")
		}
	default:
		return nil, errors.New("runtime.yaml worker.mode must be pull or push")
	}
	return &lifecycle{mode: runMode(os.Args[1]), settings: settings}, nil
}

func (l *lifecycle) options(address string) dex.WorkerOptions {
	options := dex.WorkerOptions{FlowServiceAddress: address}
	if l.settings.Worker.Mode == modePull {
		options.Pull = &dex.PullWorkerOptions{
			MaxConcurrentTasks: l.settings.Worker.MaxConcurrentTasks, IdleStreamsPerQueue: l.settings.Worker.IdleStreamsPerQueue,
			StopDrainTimeout: l.settings.Worker.StopDrainTimeout, StopOnPermanentStreamRejection: true,
		}
	} else {
		options.BindAddress = l.settings.Worker.BindAddress
		options.WorkerTarget = dex.WorkerTarget{Address: l.settings.Worker.Target}
	}
	return options
}

func (worker *Worker) IsWorker() bool { return worker.lifecycle.mode == modeWorker }

func (worker *Worker) ReadinessAddress() string {
	return worker.lifecycle.settings.Worker.ReadinessAddress
}

// MarkStopping drops readiness before Stop begins its bounded handler drain.
func (worker *Worker) MarkStopping() { worker.lifecycle.stopping.Store(true) }

func (worker *Worker) ReadinessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ready := worker.lifecycle.ready.Load() && !worker.lifecycle.stopping.Load()
		if worker.lifecycle.settings.Worker.Mode == modePull {
			ready = ready && worker.dexWorker.PullRegistrationState().IsReady
		}
		if !ready {
			http.Error(w, "Worker is not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (worker *Worker) startDex() <-chan error {
	result := make(chan error, 2)
	go func() { result <- worker.dexWorker.Start() }()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if worker.lifecycle.settings.Worker.Mode == modePull {
			if err := worker.dexWorker.WaitPullRegistered(ctx); err != nil {
				result <- fmt.Errorf("register pull Worker: %w", err)
				return
			}
		} else {
			for {
				connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", worker.lifecycle.settings.Worker.BindAddress)
				if err == nil {
					_ = connection.Close()
					break
				}
				select {
				case <-ctx.Done():
					result <- fmt.Errorf("start Preview Worker: %w", ctx.Err())
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
		}
		worker.lifecycle.ready.Store(true)
	}()
	return result
}

func loopbackAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	number, err := strconv.Atoi(port)
	return ip != nil && ip.IsLoopback() && err == nil && number > 0 && number <= 65535
}
