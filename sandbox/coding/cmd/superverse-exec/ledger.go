// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/confinement"
	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

// The ledger lives in the Pod's /tmp, so a replaced Pod has none.
const (
	runsDirectory = confinement.CommandRunsRoot
	admissionLock = "/tmp/superverse-exec/admission.lock"
	stopGrace     = supervision.StopGracePeriod
)

// ledgerRun is the directory of one execution ID.
type ledgerRun struct {
	id        string
	directory string
}

func runOf(id string) ledgerRun {
	return ledgerRun{id: id, directory: filepath.Join(runsDirectory, id)}
}

func (r ledgerRun) admissionPath() string { return filepath.Join(r.directory, "admission.json") }
func (r ledgerRun) lockPath() string      { return filepath.Join(r.directory, "lock") }
func (r ledgerRun) outputPath() string    { return filepath.Join(r.directory, "output.log") }
func (r ledgerRun) exitPath() string      { return filepath.Join(r.directory, "exit.json") }

// admission is admission.json: the request and the processes that serve it.
// begin writes it once; the supervisor, background and stop rewrite it under
// the global admission lock.
type admission struct {
	ID               string    `json:"id"`
	Digest           string    `json:"digest"`
	Command          []string  `json:"command"`
	WorkingDirectory string    `json:"workingDirectory"`
	Background       bool      `json:"background"`
	AdmittedAt       time.Time `json:"admittedAt"`
	Deadline         time.Time `json:"deadline"`
	// Supervisor and Process are the supervisor and the command's group
	// leader, recorded by the supervisor once the command started.
	Supervisor *supervision.ProcessIdentity `json:"supervisor,omitempty"`
	Process    *supervision.ProcessIdentity `json:"process,omitempty"`
	// StopRequested is set by the first stop; the supervisor ends the command.
	StopRequested *stopRequest `json:"stopRequested,omitempty"`
}

type stopRequest struct {
	At    time.Time     `json:"at"`
	Grace time.Duration `json:"grace"`
}

// exitRecord is exit.json, written once and atomically when the command ended.
type exitRecord struct {
	StartedAt   time.Time `json:"startedAt"`
	EndedAt     time.Time `json:"endedAt"`
	ExitCode    *int      `json:"exitCode,omitempty"`
	Signal      string    `json:"signal,omitempty"`
	EndReason   string    `json:"endReason"`
	OutputBytes int64     `json:"outputBytes"`
}

// requestDigest identifies a request: the command, its working directory and
// whether it runs in the background. Length prefixes keep fields apart.
func requestDigest(command []string, workingDirectory string, background bool) string {
	digest := sha256.New()
	for _, field := range append([]string{"superverse-exec-v1", workingDirectory, strconv.FormatBool(background)}, command...) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		digest.Write(length[:])
		digest.Write([]byte(field))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// tryLock takes the run's lock without waiting. With create it makes the run
// directory and the lock file; otherwise a missing lock file is not acquired.
// A lock whose file was replaced or removed meanwhile, by garbage collection
// or a refused begin, is released and reported as not acquired, so that the
// caller looks again.
func (r ledgerRun) tryLock(create bool) (*os.File, bool, error) {
	flags := os.O_RDWR
	if create {
		if err := os.MkdirAll(r.directory, 0o700); err != nil {
			return nil, false, err
		}
		flags |= os.O_CREATE
	}
	file, err := os.OpenFile(r.lockPath(), flags, 0o600)
	if errors.Is(err, fs.ErrNotExist) && !create {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	locked, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, false, err
	}
	if current, pathErr := os.Stat(r.lockPath()); pathErr != nil || !os.SameFile(locked, current) {
		_ = file.Close()
		if pathErr != nil && !errors.Is(pathErr, fs.ErrNotExist) {
			return nil, false, pathErr
		}
		return nil, false, nil
	}
	return file, true, nil
}

// lockHeld reports whether a process holds the run's lock: its supervisor, or
// a begin that is deciding the ID.
func (r ledgerRun) lockHeld() (bool, error) {
	file, err := os.OpenFile(r.lockPath(), os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}

// snapshot is what the ledger knows of one run.
type snapshot struct {
	state  string
	record *admission
	exit   *exitRecord
}

// observe reads the state of a run: exit.json present is exited, a held lock
// is running, neither is lost, and no admission is not found.
func (r ledgerRun) observe() (snapshot, error) {
	var record admission
	if err := supervision.ReadJSON(r.admissionPath(), &record); errors.Is(err, fs.ErrNotExist) {
		return snapshot{state: stateNotFound}, nil
	} else if err != nil {
		return snapshot{}, fmt.Errorf("read the admission of %s: %w", r.id, err)
	}
	for attempt := 0; ; attempt++ {
		var exit exitRecord
		if err := supervision.ReadJSON(r.exitPath(), &exit); err == nil {
			return snapshot{state: stateExited, record: &record, exit: &exit}, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return snapshot{}, fmt.Errorf("read the exit of %s: %w", r.id, err)
		}
		held, err := r.lockHeld()
		if err != nil {
			return snapshot{}, err
		}
		if held {
			return snapshot{state: stateRunning, record: &record}, nil
		}
		// The supervisor writes exit.json before it releases the lock, so a
		// free lock read after an absent exit.json is looked at once more.
		if attempt == 1 {
			return snapshot{state: stateLost, record: &record}, nil
		}
	}
}

// await observes the run until it is no longer running or wait passed.
func (r ledgerRun) await(wait time.Duration) (snapshot, error) {
	deadline := time.Now().Add(wait)
	for {
		current, err := r.observe()
		if err != nil || current.state != stateRunning || !time.Now().Before(deadline) {
			return current, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// answer renders a snapshot with the requested view of the output.
func (r ledgerRun) answer(current snapshot, view viewRequest) (answer, error) {
	reply := answer{ID: r.id, State: current.state}
	if current.record == nil {
		return reply, nil
	}
	reply.Background = current.record.Background
	deadline := current.record.Deadline
	reply.Deadline = &deadline
	if current.exit != nil {
		started, ended := current.exit.StartedAt, current.exit.EndedAt
		reply.StartedAt, reply.EndedAt = &started, &ended
		reply.ExitCode, reply.Signal, reply.EndReason = current.exit.ExitCode, current.exit.Signal, current.exit.EndReason
	} else {
		admitted := current.record.AdmittedAt
		reply.StartedAt = &admitted
	}
	output, err := view.read(r.outputPath())
	reply.Output = output
	return reply, err
}

// lockAdmission takes the global admission lock, which serializes every
// rewrite of an admission record and the background limit.
func lockAdmission() (*os.File, error) {
	for deadline := time.Now().Add(decisionLockAcquisition); ; time.Sleep(10 * time.Millisecond) {
		lock, acquired, err := supervision.TryLock(admissionLock)
		if err != nil || acquired {
			return lock, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("the ledger's admission lock stayed busy")
		}
	}
}

// updateAdmission rewrites a run's admission record under the admission lock.
func (r ledgerRun) updateAdmission(change func(*admission) error) error {
	lock, err := lockAdmission()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	var record admission
	if err = supervision.ReadJSON(r.admissionPath(), &record); err != nil {
		return err
	}
	if err = change(&record); err != nil {
		return err
	}
	return supervision.WriteJSON(r.admissionPath(), record)
}

// runningBackground counts the background runs that are running, all agents
// together. The caller holds the admission lock.
func runningBackground() (int, error) {
	entries, err := os.ReadDir(runsDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		var record admission
		if !entry.IsDir() || supervision.ReadJSON(runOf(entry.Name()).admissionPath(), &record) != nil || !record.Background {
			continue
		}
		current, err := runOf(entry.Name()).observe()
		if err != nil {
			return 0, err
		}
		if current.state == stateRunning {
			count++
		}
	}
	return count, nil
}
