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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

// The engine's fixed local contract (coding profile v4). Everything listens
// on loopback; the Dex relay alone listens on the Pod's addresses.
const (
	realBinaries     = "/usr/local/libexec/superverse"
	temporalAddress  = "127.0.0.1:7233"
	dexPort          = 8801
	dexAddress       = "127.0.0.1:8801"
	engineNamespace  = "default"
	retention        = "720h"
	retentionTTL     = "2592000s"
	retentionDays    = 30
	minimumFreeBytes = 512 << 20
	// readinessBudget bounds how long Temporal or Dex may take to answer
	// its health check after it started.
	readinessBudget = 2 * time.Minute
	// A child that exits more than restartLimit times within restartWindow
	// fails the environment.
	restartLimit  = 3
	restartWindow = 10 * time.Minute
)

// environmentListeners must be free before the engine starts; a taken one
// fails the start instead of letting a process drift to another port.
var environmentListeners = []string{"127.0.0.1:7233", "127.0.0.1:7243", "127.0.0.1:7244", "127.0.0.1:8801", "127.0.0.1:8802"}

// environmentIdentity is what a ready environment verified: the engine store
// the volume holds, its read-back retention and the running versions.
type environmentIdentity struct {
	TemporalClusterID string `json:"temporalClusterId,omitempty"`
	RetentionDays     int    `json:"retentionDays,omitempty"`
	TemporalVersion   string `json:"temporalVersion,omitempty"`
	DexVersion        string `json:"dexVersion,omitempty"`
}

// environmentRecord is environment.json, which outlives every daemon. Its
// cluster ID binds the Dex blobs to the Temporal store that references them.
type environmentRecord struct {
	environmentIdentity
	StartedAt   time.Time  `json:"startedAt"`
	CleanStopAt *time.Time `json:"cleanStopAt,omitempty"`
}

// environment owns the Temporal and Dex process groups of the daemon and the
// relay that exposes Dex.
type environment struct {
	state stateDirectory
	owner *owner
	// retry wakes run after a refusal was reported, for a new attempt.
	retry chan struct{}

	mu sync.Mutex
	// current is the answer to ensure and status.
	current answer
	// reported is set once an ensure answered the current refusal; the next
	// ensure starts a new attempt.
	reported      bool
	temporal, dex *child
	relay         *dexRelay
	restarts      map[string][]time.Time
}

func newEnvironment(state stateDirectory, records *owner) *environment {
	return &environment{state: state, owner: records, retry: make(chan struct{}, 1), current: answer{State: stateStarting}, restarts: map[string][]time.Time{}}
}

func (e *environment) status() answer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.current
}

// ensure answers the environment's state. A refusal is answered once; the
// ensure after that starts a new attempt and answers starting. While the
// daemon stops, a new environment can start only after it exited.
func (e *environment) ensure() answer {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch e.current.State {
	case stateStopping:
		return answer{State: stateStarting, Reason: "the previous Preview environment is stopping"}
	case stateFull, stateInconsistent, stateFailed:
		if !e.reported {
			e.reported = true
			return e.current
		}
		e.current = answer{State: stateStarting}
		select {
		case e.retry <- struct{}{}:
		default:
		}
	}
	return e.current
}

// set records the state run reached. Once the daemon stops, it stays
// stopping: nothing starts again in this daemon.
func (e *environment) set(current answer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current.State != stateStopping {
		e.current, e.reported = current, false
	}
}

func (e *environment) markStopping() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.current = answer{State: stateStopping}
}

// run starts the engine, keeps it running and, after a refusal, waits for
// an ensure to try again, until ctx ends.
func (e *environment) run(ctx context.Context) {
	for {
		e.set(answer{State: stateStarting})
		identity, refusal := e.start(ctx)
		if refusal == nil {
			e.set(answer{State: stateReady, environmentIdentity: identity})
			log.Printf("the Preview environment is ready: %+v", identity)
			refusal = e.watch(ctx, identity)
		}
		if ctx.Err() != nil {
			return
		}
		log.Printf("the Preview environment is %s: %s", refusal.State, refusal.Reason)
		e.stopChildren()
		e.set(*refusal)
		select {
		case <-ctx.Done():
			return
		case <-e.retry:
		}
	}
}

func refuse(state, reason string) *answer { return &answer{State: state, Reason: reason} }

// start brings up Temporal and Dex on the state directory. A refusal is the
// definite answer of this attempt.
func (e *environment) start(ctx context.Context) (environmentIdentity, *answer) {
	var record environmentRecord
	err := supervision.ReadJSON(e.state.environmentRecord, &record)
	recorded := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return environmentIdentity{}, refuse(stateFailed, "read environment.json: "+err.Error())
	}
	for _, directory := range []string{filepath.Dir(e.state.temporalDatabase), e.state.blobs, e.state.connectors, e.state.sources, e.state.logs,
		filepath.Join(runtimeDirectory, "app-blobs"), filepath.Join(runtimeDirectory, "home")} {
		if err = os.MkdirAll(directory, 0o700); err != nil {
			return environmentIdentity{}, refuse(stateFailed, err.Error())
		}
	}
	if refusal := e.checkVolume(recorded); refusal != nil {
		return environmentIdentity{}, refusal
	}
	if err = checkListenersFree(ctx); err != nil {
		return environmentIdentity{}, refuse(stateFailed, err.Error())
	}
	if err = e.startTemporal(ctx); err != nil {
		return environmentIdentity{}, refuse(stateFailed, err.Error())
	}
	identity, err := describeTemporal(ctx)
	if err != nil {
		return environmentIdentity{}, refuse(stateFailed, err.Error())
	}
	if recorded && record.TemporalClusterID != identity.TemporalClusterID {
		return environmentIdentity{}, refuse(stateInconsistent, fmt.Sprintf(
			"the engine store is cluster %s, but environment.json and the Dex blobs belong to cluster %s", identity.TemporalClusterID, record.TemporalClusterID))
	}
	if identity.RetentionDays, err = applyRetention(ctx); err != nil {
		return environmentIdentity{}, refuse(stateFailed, err.Error())
	}
	if err = e.startDex(ctx); err != nil {
		return environmentIdentity{}, refuse(stateFailed, err.Error())
	}
	relay, err := startDexRelay(ctx)
	if err != nil {
		return environmentIdentity{}, refuse(stateFailed, err.Error())
	}
	e.mu.Lock()
	e.relay = relay
	e.mu.Unlock()
	if identity.DexVersion, err = dexVersion(ctx); err != nil {
		return environmentIdentity{}, refuse(stateFailed, err.Error())
	}
	if err = supervision.WriteJSON(e.state.environmentRecord, environmentRecord{environmentIdentity: identity, StartedAt: time.Now().UTC()}); err != nil {
		return environmentIdentity{}, refuse(stateFailed, "write environment.json: "+err.Error())
	}
	return identity, nil
}

// checkVolume refuses a state directory whose engine store is gone while
// Dex blobs or the environment record remain: a new store would make Dex
// delete every blob the old executions reference. It also refuses a volume
// without room for the engine.
func (e *environment) checkVolume(recorded bool) *answer {
	_, err := os.Stat(e.state.temporalDatabase)
	databaseMissing := errors.Is(err, os.ErrNotExist)
	blobs, _ := os.ReadDir(e.state.blobs)
	if databaseMissing && (recorded || len(blobs) > 0) {
		return refuse(stateInconsistent, "the engine store temporal/temporal.db is missing while environment.json or Dex blobs remain; refusing to start an engine that would discard them")
	}
	var volume syscall.Statfs_t
	if err = syscall.Statfs(e.state.root, &volume); err != nil {
		return refuse(stateFailed, "inspect the workspace volume: "+err.Error())
	}
	if free := uint64(volume.Bavail) * uint64(volume.Bsize); free < minimumFreeBytes { //nolint:gosec,unconvert // block counts and sizes are non-negative; their types differ by platform
		return refuse(stateFull, fmt.Sprintf("%d MiB are free on the workspace volume; the Preview engine needs at least %d MiB", free>>20, minimumFreeBytes>>20))
	}
	return nil
}

func checkListenersFree(ctx context.Context) error {
	for _, address := range environmentListeners {
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
		if err != nil {
			return fmt.Errorf("the Preview engine port %s is in use: %w", address, err)
		}
		_ = listener.Close()
	}
	return nil
}

func (e *environment) startTemporal(ctx context.Context) error {
	process, err := e.startEngineProcess("temporal", "temporal.log", filepath.Join(realBinaries, "temporal"), "server", "start-dev",
		"--ip", "127.0.0.1", "--port", "7233", "--http-port", "7243", "--metrics-port", "7244", "--headless",
		"--db-filename", e.state.temporalDatabase, "--log-level", "warn")
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.temporal = process
	e.mu.Unlock()
	return e.waitReady(ctx, process, "Temporal", "temporal.log", func(ctx context.Context) error {
		output, err := runTool(ctx, "temporal", "operator", "cluster", "health", "--address", temporalAddress)
		if err == nil && !strings.Contains(output, "SERVING") {
			err = errors.New(output)
		}
		return err
	})
}

func (e *environment) startDex(ctx context.Context) error {
	process, err := e.startEngineProcess("dex", "dexcli.log", filepath.Join(realBinaries, "dexcli"), "dev",
		"--bind-address", "127.0.0.1", "--dex-port", "8801", "--web-port", "8802",
		"--external-temporal-address", temporalAddress, "--external-temporal-namespace", engineNamespace,
		"--blob-store-dir", e.state.blobs, "--server-log-folder", e.state.logs, "--connector-config-dir", e.state.connectors, "--open=false")
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.dex = process
	e.mu.Unlock()
	return e.waitReady(ctx, process, "Dex", "dexcli.log", func(ctx context.Context) error {
		_, err := runTool(ctx, "dexcli", "health", "--server", dexAddress, "--timeout", "5s")
		return err
	})
}

func (e *environment) startEngineProcess(name, logName string, command ...string) (*child, error) {
	output, err := e.state.openLog(logName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = output.Close() }()
	process, err := startChild(e.state.root, command, os.Environ(), output)
	if err != nil {
		return nil, err
	}
	e.owner.set(name, process)
	return process, nil
}

// waitReady polls check until it passes, the process exits or the readiness
// budget is spent.
func (e *environment) waitReady(ctx context.Context, process *child, name, logName string, check func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, readinessBudget)
	defer cancel()
	for {
		round, cancelRound := context.WithTimeout(ctx, 5*time.Second)
		err := check(round)
		cancelRound()
		if err == nil {
			return nil
		}
		select {
		case <-process.exited:
			return fmt.Errorf("%s exited before it was ready (%s): %s", name, process.exitStatus(), tail(e.state.log(logName), 0))
		case <-ctx.Done():
			return fmt.Errorf("%s was not ready within %s: %w: %s", name, readinessBudget, err, tail(e.state.log(logName), 0))
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// describeTemporal reads the identity of the engine store.
func describeTemporal(ctx context.Context) (environmentIdentity, error) {
	output, err := runTool(ctx, "temporal", "operator", "cluster", "describe", "--address", temporalAddress, "--output", "json")
	if err != nil {
		return environmentIdentity{}, fmt.Errorf("describe the Temporal cluster: %w", err)
	}
	var cluster struct {
		ClusterID     string `json:"clusterId"`
		ServerVersion string `json:"serverVersion"`
	}
	if err = json.Unmarshal([]byte(output), &cluster); err != nil || cluster.ClusterID == "" {
		return environmentIdentity{}, fmt.Errorf("the Temporal cluster description has no cluster ID: %s", output)
	}
	return environmentIdentity{TemporalClusterID: cluster.ClusterID, TemporalVersion: cluster.ServerVersion}, nil
}

// applyRetention sets the namespace retention and reads it back: closed
// executions and the Dex blobs they reference stay for 30 days, which bounds
// the volume's growth.
func applyRetention(ctx context.Context) (int, error) {
	if _, err := runTool(ctx, "temporal", "operator", "namespace", "update", "--address", temporalAddress, "--namespace", engineNamespace, "--retention", retention); err != nil {
		return 0, fmt.Errorf("set the engine retention: %w", err)
	}
	output, err := runTool(ctx, "temporal", "operator", "namespace", "describe", "--address", temporalAddress, "--namespace", engineNamespace, "--output", "json")
	if err != nil {
		return 0, fmt.Errorf("read back the engine retention: %w", err)
	}
	var namespace struct {
		Config struct {
			WorkflowExecutionRetentionTTL string `json:"workflowExecutionRetentionTtl"`
		} `json:"config"`
	}
	if err = json.Unmarshal([]byte(output), &namespace); err != nil || namespace.Config.WorkflowExecutionRetentionTTL != retentionTTL {
		return 0, fmt.Errorf("the engine retention reads back as %q, not %s", namespace.Config.WorkflowExecutionRetentionTTL, retentionTTL)
	}
	return retentionDays, nil
}

func dexVersion(ctx context.Context) (string, error) {
	output, err := runTool(ctx, "dexcli", "version")
	if err != nil {
		return "", fmt.Errorf("read the dexcli version: %w", err)
	}
	fields := strings.Fields(output)
	if len(fields) < 2 {
		return "", fmt.Errorf("unexpected dexcli version output: %s", output)
	}
	return fields[1], nil
}

// runTool runs one bounded command of the real Temporal or Dex CLI.
func runTool(ctx context.Context, name string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return runCommand(ctx, nil, filepath.Join(realBinaries, name), arguments...)
}

// watch keeps a ready engine running: a child that exits is restarted while
// the restart budget allows, else the environment fails.
func (e *environment) watch(ctx context.Context, identity environmentIdentity) *answer {
	for {
		e.mu.Lock()
		temporal, dex := e.temporal, e.dex
		e.mu.Unlock()
		var name string
		var process *child
		select {
		case <-ctx.Done():
			return nil
		case <-temporal.exited:
			name, process = "Temporal", temporal
		case <-dex.exited:
			name, process = "Dex", dex
		}
		log.Printf("%s exited: %s", name, process.exitStatus())
		if !e.allowRestart(name) {
			return refuse(stateFailed, fmt.Sprintf("%s exited %d times within %s; last exit: %s", name, restartLimit+1, restartWindow, process.exitStatus()))
		}
		e.set(answer{State: stateStarting, Reason: name + " is restarting"})
		var err error
		if process == temporal {
			err = e.startTemporal(ctx)
		} else {
			err = e.startDex(ctx)
		}
		if err != nil {
			return refuse(stateFailed, "restart "+name+": "+err.Error())
		}
		e.set(answer{State: stateReady, environmentIdentity: identity})
	}
}

func (e *environment) allowRestart(name string) bool {
	now := time.Now()
	recent := []time.Time{now}
	for _, at := range e.restarts[name] {
		if now.Sub(at) < restartWindow {
			recent = append(recent, at)
		}
	}
	e.restarts[name] = recent
	return len(recent) <= restartLimit
}

// stopChildren stops the relay, Dex, then Temporal. run has returned or is
// waiting for a retry, so no child starts meanwhile.
func (e *environment) stopChildren() {
	e.mu.Lock()
	temporal, dex, relay := e.temporal, e.dex, e.relay
	e.temporal, e.dex, e.relay = nil, nil, nil
	e.mu.Unlock()
	if relay != nil {
		relay.stop()
	}
	for _, stopped := range []struct {
		name    string
		process *child
	}{{"dex", dex}, {"temporal", temporal}} {
		if stopped.process != nil {
			stopped.process.stop()
			e.owner.set(stopped.name, nil)
		}
	}
}

// recordCleanStop marks environment.json after a graceful stop, for
// diagnostics of the next start.
func recordCleanStop(state stateDirectory) {
	var record environmentRecord
	if err := supervision.ReadJSON(state.environmentRecord, &record); err != nil {
		return
	}
	now := time.Now().UTC()
	record.CleanStopAt = &now
	if err := supervision.WriteJSON(state.environmentRecord, record); err != nil {
		log.Printf("record the clean stop: %v", err)
	}
}
