// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

// The test engine's fixed loopback contract (coding profile v4), apart from the
// Preview engine's ports.
const (
	testTemporalAddress = "127.0.0.1:7235"
	testDexAddress      = "127.0.0.1:8805"
	testWorkerAddress   = "127.0.0.1:18813"
	// The engine must answer within these budgets after it started.
	testTemporalReadiness = 15 * time.Second
	testDexReadiness      = 20 * time.Second
)

// testEngineListeners must be free before the engine starts.
var testEngineListeners = []string{testTemporalAddress, "127.0.0.1:7245", "127.0.0.1:7246", testDexAddress, "127.0.0.1:8806", testWorkerAddress}

// testEngine is the in-memory Temporal and the dexcli dev of one run. Both
// stay in the caller's process group, so a kill of that group ends them.
type testEngine struct {
	temporal, dex *exec.Cmd
	exited        map[*exec.Cmd]chan struct{}
}

// engineRecord is engine.json: the processes of the engine that holds the
// lock, which the next holder stops if their run died without a teardown.
type engineRecord struct {
	Processes []supervision.ProcessIdentity `json:"processes"`
}

// startTestEngine starts Temporal, then Dex, and waits until both answer. A
// failed start stops what it started.
func startTestEngine(ctx context.Context, run testRun) (*testEngine, error) {
	if err := checkTestListenersFree(ctx); err != nil {
		return nil, err
	}
	logs := run.path("logs")
	for _, directory := range []string{logs, run.path("engine/blobs"), run.path("engine/connectors"), run.path("blob-cache")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, err
		}
	}
	engine := &testEngine{exited: map[*exec.Cmd]chan struct{}{}}
	var err error
	engine.temporal, err = engine.start(filepath.Join(logs, "temporal.log"), filepath.Join(realBinaries, "temporal"), "server", "start-dev", "--headless",
		"--ip", "127.0.0.1", "--port", "7235", "--http-port", "7245", "--metrics-port", "7246", "--log-level", "warn")
	if err == nil {
		err = engine.awaitReady(ctx, engine.temporal, "Temporal", testTemporalReadiness, filepath.Join(logs, "temporal.log"), func(ctx context.Context) error {
			output, healthErr := runTool(ctx, "temporal", "operator", "cluster", "health", "--address", testTemporalAddress)
			if healthErr == nil && !strings.Contains(output, "SERVING") {
				healthErr = errors.New(output)
			}
			return healthErr
		})
	}
	if err == nil {
		engine.dex, err = engine.start(filepath.Join(logs, "dexcli.log"), filepath.Join(realBinaries, "dexcli"), "dev",
			"--bind-address", "127.0.0.1", "--dex-port", "8805", "--web-port", "8806",
			"--external-temporal-address", testTemporalAddress, "--external-temporal-namespace", engineNamespace,
			"--blob-store-dir", run.path("engine/blobs"), "--server-log-folder", logs, "--connector-config-dir", run.path("engine/connectors"), "--open=false")
	}
	if err == nil {
		err = engine.awaitReady(ctx, engine.dex, "Dex", testDexReadiness, filepath.Join(logs, "dexcli.log"), func(ctx context.Context) error {
			_, healthErr := runTool(ctx, "dexcli", "health", "--server", testDexAddress, "--timeout", "5s")
			return healthErr
		})
	}
	if err != nil {
		engine.stop()
		return nil, err
	}
	engine.record()
	return engine, nil
}

func checkTestListenersFree(ctx context.Context) error {
	for _, address := range testEngineListeners {
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
		if err != nil {
			return fmt.Errorf("the test engine port %s is in use: %w", address, err)
		}
		_ = listener.Close()
	}
	return nil
}

// start runs one engine process with its output in logPath.
func (e *testEngine) start(logPath string, command ...string) (*exec.Cmd, error) {
	output, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // the run's own log
	if err != nil {
		return nil, err
	}
	defer func() { _ = output.Close() }()
	process := exec.Command(command[0], command[1:]...) //nolint:gosec,noctx // the engine's fixed commands; the run stops their trees explicitly
	process.Stdout, process.Stderr = output, output
	if err = process.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", filepath.Base(command[0]), err)
	}
	exited := make(chan struct{})
	e.exited[process] = exited
	go func() {
		_ = process.Wait()
		close(exited)
	}()
	return process, nil
}

// awaitReady polls check until it passes, the process exits or budget passes.
func (e *testEngine) awaitReady(ctx context.Context, process *exec.Cmd, name string, budget time.Duration, logPath string, check func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	for {
		round, cancelRound := context.WithTimeout(ctx, 5*time.Second)
		err := check(round)
		cancelRound()
		if err == nil {
			return nil
		}
		select {
		case <-e.exited[process]:
			return fmt.Errorf("%s exited before it was ready (%s): %s", name, process.ProcessState, tail(logPath, 0))
		case <-ctx.Done():
			return fmt.Errorf("%s was not ready within %s: %w: %s", name, budget, err, tail(logPath, 0))
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// record writes engine.json with the engine's processes and their
// descendants, such as the Dex Server that dexcli dev starts.
func (e *testEngine) record() {
	var record engineRecord
	for _, process := range []*exec.Cmd{e.temporal, e.dex} {
		record.Processes = append(record.Processes, identityOf(process.Process.Pid)...)
		record.Processes = append(record.Processes, descendants(process.Process.Pid)...)
	}
	if err := supervision.WriteJSON(apptestEngineRecord, record); err != nil {
		fmt.Fprintf(os.Stderr, "superverse-previewctl: record the test engine: %v\n", err)
	}
}

// stop ends Dex, then Temporal: SIGTERM, then SIGKILL after the grace period.
func (e *testEngine) stop() {
	for _, process := range []*exec.Cmd{e.dex, e.temporal} {
		if process != nil {
			stopTree(process.Process.Pid, e.exited[process])
		}
	}
	_ = os.Remove(apptestEngineRecord)
}

// stopLeftoverEngine kills the processes that a run which died without its
// teardown left behind, after their identity check.
func stopLeftoverEngine() {
	var record engineRecord
	if err := supervision.ReadJSON(apptestEngineRecord, &record); err != nil {
		return
	}
	for _, process := range record.Processes {
		if process.Alive() {
			_ = syscall.Kill(process.PID, syscall.SIGKILL)
		}
	}
	_ = os.Remove(apptestEngineRecord)
}
