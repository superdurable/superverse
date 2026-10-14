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
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

const (
	stateDirectoryVariable = "SUPERVERSE_PREVIEW_STATE_DIR"
	podUIDVariable         = "SUPERVERSE_POD_UID"
	// lockAcquisition is how long a starting daemon retries the state lock: a
	// command that probes the lock holds it for an instant.
	lockAcquisition = time.Second
	// logLimit is the size at which a log is copied to <name>.1 and truncated.
	logLimit = 100 << 20
)

// stateDirectory is the Preview state on the workspace volume (profile
// subPath preview-state), which outlives every Pod of the volume.
type stateDirectory struct {
	root, lock, owner, environmentRecord, temporalDatabase, blobs, connectors, sources, sourceLock, logs string
}

func openStateDirectory() (stateDirectory, error) {
	root := os.Getenv(stateDirectoryVariable)
	if !filepath.IsAbs(root) {
		return stateDirectory{}, fmt.Errorf("%s must name the Preview state directory", stateDirectoryVariable)
	}
	return stateDirectory{
		root:              root,
		lock:              filepath.Join(root, "supervisor.lock"),
		owner:             filepath.Join(root, "owner.json"),
		environmentRecord: filepath.Join(root, "environment.json"),
		temporalDatabase:  filepath.Join(root, "temporal", "temporal.db"),
		blobs:             filepath.Join(root, "dex", "blobs"),
		connectors:        filepath.Join(root, "dex", "connectors"),
		sources:           filepath.Join(root, "source"),
		sourceLock:        filepath.Join(root, "source.lock"),
		logs:              filepath.Join(root, "logs"),
	}, nil
}

func (s stateDirectory) log(name string) string { return filepath.Join(s.logs, name) }

// openLog opens one of the daemon's logs for appending.
func (s stateDirectory) openLog(name string) (*os.File, error) {
	if err := os.MkdirAll(s.logs, 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(s.log(name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // fixed log names in the state directory
}

// supervisorRunning reports whether a daemon holds the state lock.
func supervisorRunning(state stateDirectory) (bool, error) {
	lock, acquired, err := supervision.TryLock(state.lock)
	if err != nil {
		return false, err
	}
	if !acquired {
		return true, nil
	}
	return false, lock.Close()
}

// launchSupervisor starts the daemon detached in its own session, so neither
// sandboxd's process-group cleanup of the calling command nor the caller's
// exit reaches it. Its HOME is Pod-local and never the repository.
func launchSupervisor(state stateDirectory) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	output, err := state.openLog("supervisor.log")
	if err != nil {
		return err
	}
	defer func() { _ = output.Close() }()
	daemon := exec.Command(executable, runSupervise) //nolint:gosec,noctx // this executable's own daemon, detached on purpose
	daemon.Dir = state.root
	daemon.Env = append(os.Environ(), "HOME="+filepath.Join(runtimeDirectory, "home"))
	daemon.Stdout = output
	daemon.Stderr = output
	if _, err = supervision.Detach(daemon); err != nil {
		return fmt.Errorf("launch the Preview supervisor: %w", err)
	}
	return nil
}

// ownerRecord is owner.json: the Pod and processes that own the state
// directory, so that the next daemon can stop what a killed one left behind.
type ownerRecord struct {
	PodUID   string                                 `json:"podUid"`
	Daemon   supervision.ProcessIdentity            `json:"daemon"`
	Children map[string]supervision.ProcessIdentity `json:"children"`
}

type owner struct {
	mu     sync.Mutex
	path   string
	record ownerRecord
}

// set records the named child, or forgets it when process is nil.
func (o *owner) set(name string, process *child) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if process == nil {
		delete(o.record.Children, name)
	} else {
		o.record.Children[name] = process.identity
	}
	if err := supervision.WriteJSON(o.path, o.record); err != nil {
		log.Printf("record the owner of the Preview state: %v", err)
	}
}

// daemon is `superverse-previewctl supervise`: the one owner of the Preview
// environment and application of this Pod.
type daemon struct {
	environment *environment
	application *application
	stopOnce    sync.Once
	stopped     chan struct{}
}

func (d *daemon) requestStop() { d.stopOnce.Do(func() { close(d.stopped) }) }

// supervise runs the daemon until a stop request or SIGTERM, then stops the
// application, Dex and Temporal in that order.
func supervise() (answer, error) {
	state, err := openStateDirectory()
	if err != nil {
		return answer{}, err
	}
	podUID := os.Getenv(podUIDVariable)
	if podUID == "" {
		return answer{}, fmt.Errorf("%s must name this Pod", podUIDVariable)
	}
	lock, err := acquireStateLock(state)
	if err != nil {
		return answer{}, err
	}
	// The lock is released when the daemon exits, however it exits.
	defer func() { _ = lock.Close() }()
	self, err := supervision.Identify(os.Getpid())
	if err != nil {
		return answer{}, err
	}
	stopRecordedChildren(state, podUID)
	records := &owner{path: state.owner, record: ownerRecord{PodUID: podUID, Daemon: self, Children: map[string]supervision.ProcessIdentity{}}}
	if err = supervision.WriteJSON(records.path, records.record); err != nil {
		return answer{}, err
	}
	rotateLogs(state.logs)
	listener, err := listenControl()
	if err != nil {
		return answer{}, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	d := &daemon{
		environment: newEnvironment(state, records),
		application: newApplication(state, records),
		stopped:     make(chan struct{}),
	}
	environmentContext, stopEnvironment := context.WithCancel(ctx)
	environmentDone := make(chan struct{})
	go func() {
		defer close(environmentDone)
		d.environment.run(environmentContext)
	}()
	go d.serve(listener)
	go rotateLogsPeriodically(ctx, state.logs)
	log.Printf("Preview supervisor %d owns %s for Pod %s", self.PID, state.root, podUID) //nolint:gosec // the Pod's own downward-API identity

	select {
	case <-ctx.Done():
	case <-d.stopped:
	}
	log.Print("stopping the Preview environment")
	d.environment.markStopping()
	stopEnvironment()
	<-environmentDone
	d.application.shutdown()
	d.environment.stopChildren()
	recordCleanStop(state)
	_ = listener.Close()
	_ = os.Remove(controlSocket)
	log.Print("the Preview environment stopped")
	return answer{State: stateStopped}, nil
}

// acquireStateLock takes the state lock for the daemon's lifetime. Its file
// descriptor is close-on-exec, so no child inherits the lock.
func acquireStateLock(state stateDirectory) (*os.File, error) {
	for deadline := time.Now().Add(lockAcquisition); ; time.Sleep(50 * time.Millisecond) {
		lock, acquired, err := supervision.TryLock(state.lock)
		if err != nil || acquired {
			return lock, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("another Preview supervisor owns the state directory")
		}
	}
}

// stopRecordedChildren stops the process groups a killed daemon of this Pod
// left behind, each only after its identity still matches. A record of
// another Pod names processes that ended with that Pod.
func stopRecordedChildren(state stateDirectory, podUID string) {
	var previous ownerRecord
	if err := supervision.ReadJSON(state.owner, &previous); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("read the previous owner of the Preview state: %v", err)
		}
		return
	}
	if previous.PodUID != podUID {
		log.Printf("taking over the Preview state from Pod %s", previous.PodUID)
		return
	}
	for name, identity := range previous.Children {
		if identity.Alive() {
			log.Printf("stopping the orphaned %s process group %d", name, identity.PID)
			supervision.TerminateGroup(identity, supervision.StopGracePeriod)
		}
	}
}

func rotateLogsPeriodically(ctx context.Context, directory string) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rotateLogs(directory)
		}
	}
}

// rotateLogs copies every log above the limit to <name>.1 and truncates it.
// Writers append, so they continue at the new end.
func rotateLogs(directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() < logLimit || strings.HasSuffix(entry.Name(), ".1") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if err = copyFile(path, path+".1"); err == nil {
			err = os.Truncate(path, 0)
		}
		if err != nil {
			log.Printf("rotate %s: %v", path, err)
		}
	}
}

func copyFile(source, destination string) error {
	input, err := os.Open(source) //nolint:gosec // a log of the state directory
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // the rotated copy beside it
	if err != nil {
		return err
	}
	if _, err = io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
