package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	api "github.com/superdurable/dex-template-basic-process/internal/api"
	"github.com/superdurable/dex-template-basic-process/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	// Step, RPC and request failures log through the default logger.
	slog.SetDefault(logger)
	worker, err := worker.New(logger)
	if err != nil {
		return err
	}
	defer worker.Close()
	apiHandler, err := api.NewHandler(worker.Client)
	if err != nil {
		return fmt.Errorf("create OpenAPI handler: %w", err)
	}
	server := &http.Server{Addr: ":" + environment("PORT", "8080"), Handler: applicationHandler(apiHandler), ReadHeaderTimeout: 5 * time.Second}
	workerResult := worker.Start()
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.ListenAndServe() }()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-workerResult:
		if err != nil {
			return fmt.Errorf("run Dex Worker: %w", err)
		}
	case err := <-serverResult:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("run HTTP server: %w", err)
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

func applicationHandler(apiHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	mux.Handle("/", staticHandler("web/dist"))
	return logServerErrors(mux)
}

// logServerErrors logs every response with a 5xx status with its method and
// route, never a request or response body.
func logServerErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		if recorder.status >= http.StatusInternalServerError {
			slog.ErrorContext(request.Context(), "HTTP request failed", "method", request.Method, "route", request.URL.Path, "status", recorder.status)
		}
	})
}

// statusRecorder keeps the status a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func staticHandler(root string) http.Handler {
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		asset := filepath.Join(root, filepath.Clean(request.URL.Path))
		if request.URL.Path != "/" {
			if info, err := os.Stat(asset); err == nil && !info.IsDir() {
				files.ServeHTTP(w, request)
				return
			}
		}
		http.ServeFile(w, request, filepath.Join(root, "index.html"))
	})
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
