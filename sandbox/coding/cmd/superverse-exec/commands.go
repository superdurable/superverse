// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

// begin admits and starts a command once per execution ID, or answers the run
// that already has the ID. It decides under the run's lock, which the started
// supervisor inherits, so the lock is held from the decision until the
// command ended: concurrent begins of one ID start one command.
func begin(request request) (answer, error) {
	r := runOf(request.id)
	digest := requestDigest(request.command, request.workingDirectory, request.background)
	for deadline := time.Now().Add(decisionLockAcquisition); ; time.Sleep(20 * time.Millisecond) {
		lock, acquired, err := r.tryLock(true)
		if err != nil {
			return answer{}, err
		}
		if acquired {
			reply, err := r.decide(lock, request, digest)
			collectGarbage(r.id)
			return reply, err
		}
		// A supervisor runs the ID, or another begin is deciding it and has
		// not yet written its admission.
		var record admission
		if supervision.ReadJSON(r.admissionPath(), &record) == nil {
			return r.existing(record, request, digest)
		}
		if time.Now().After(deadline) {
			return answer{}, fmt.Errorf("another begin of %s is still deciding", r.id)
		}
	}
}

// decide runs under the run's lock: it answers an existing record, refuses a
// request it cannot start, or starts the supervisor and hands it the lock.
func (r ledgerRun) decide(lock *os.File, request request, digest string) (answer, error) {
	defer func() { _ = lock.Close() }()
	var record admission
	if err := supervision.ReadJSON(r.admissionPath(), &record); err == nil {
		return r.existing(record, request, digest)
	}
	if request.notBefore != nil {
		started, err := supervision.StartTime(1)
		if err != nil {
			return answer{}, fmt.Errorf("read the start of this instance: %w", err)
		}
		if started.After(*request.notBefore) {
			return r.refuse(refusalInstanceReplaced, fmt.Sprintf("this Sandbox instance started at %s, after the first attempt; an earlier instance may have run the command", started.Format(time.RFC3339)))
		}
	}
	if info, err := os.Stat(request.workingDirectory); err != nil || !info.IsDir() {
		return r.refuse(refusalWorkingDirectoryAbsent, fmt.Sprintf("the working directory %s does not exist or is not a directory", request.workingDirectory))
	}
	if refusal, err := r.admit(request, digest); err != nil || refusal != "" {
		if err != nil {
			return answer{}, err
		}
		return r.refuse(refusalBackgroundLimit, refusal)
	}
	executable, err := os.Executable()
	if err != nil {
		return answer{}, err
	}
	supervisor := exec.Command(executable, subcommandSupervise, "--id", r.id) //nolint:gosec,noctx // this executable's own supervisor, detached on purpose
	supervisor.Dir = r.directory
	supervisor.Env = os.Environ()
	// The supervisor inherits the run's lock as descriptor 3 and holds it until
	// the command ended.
	supervisor.ExtraFiles = []*os.File{lock}
	if _, err = supervision.Detach(supervisor); err != nil {
		return answer{}, fmt.Errorf("start the supervisor of %s: %w", r.id, err)
	}
	// From here on only the supervisor holds the lock.
	_ = lock.Close()
	current, err := r.await(request.wait)
	if err != nil {
		return answer{}, err
	}
	return r.answer(current, request.view)
}

// admit writes the admission record under the admission lock, so that the
// background limit counts every admitted background run. It returns the
// reason of a refusal.
func (r ledgerRun) admit(request request, digest string) (string, error) {
	lock, err := lockAdmission()
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Close() }()
	if request.background {
		running, countErr := runningBackground()
		if countErr != nil {
			return "", countErr
		}
		if running >= defaultBackgroundLimit {
			return fmt.Sprintf("%d background commands are already running in this Sandbox", running), nil
		}
	}
	output, err := os.OpenFile(r.outputPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // the run's own output file
	if err != nil {
		return "", err
	}
	if err = output.Close(); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	return "", supervision.WriteJSON(r.admissionPath(), admission{
		ID: r.id, Digest: digest, Command: request.command, WorkingDirectory: request.workingDirectory,
		Background: request.background, AdmittedAt: now, Deadline: now.Add(request.lifetime),
	})
}

// refuse answers a request that started nothing and removes the run's
// directory, which holds only the lock; a waiting begin then looks again.
func (r ledgerRun) refuse(refusal, reason string) (answer, error) {
	if err := os.RemoveAll(r.directory); err != nil {
		return answer{}, err
	}
	return answer{ID: r.id, State: stateNotFound, Refusal: refusal, Reason: reason}, nil
}

// existing answers a begin whose ID already has a record: the same request
// gets the run's state, never a second start; another request is a conflict.
func (r ledgerRun) existing(record admission, request request, digest string) (answer, error) {
	wait := request.wait
	if record.Digest != digest {
		wait = 0
	}
	current, err := r.await(wait)
	if err != nil {
		return answer{}, err
	}
	reply, err := r.answer(current, request.view)
	if err == nil && record.Digest != digest {
		reply.Refusal, reply.Reason = refusalExecutionConflict, "the execution ID was admitted for another command, working directory or mode"
	}
	return reply, err
}

func status(request request) (answer, error) {
	r := runOf(request.id)
	current, err := r.await(request.wait)
	if err != nil {
		return answer{}, err
	}
	return r.answer(current, request.view)
}

// background turns a running foreground command into a background command
// whose lifetime ends request.lifetime from now, and never later than the
// longest lifetime after its admission.
func background(request request) (answer, error) {
	r := runOf(request.id)
	lock, err := lockAdmission()
	if err != nil {
		return answer{}, err
	}
	current, err := r.observe()
	if err != nil || current.state != stateRunning || current.record.Background {
		_ = lock.Close()
		if err != nil {
			return answer{}, err
		}
		return r.answer(current, viewRequest{})
	}
	refusal := ""
	running, err := runningBackground()
	if err == nil && running >= defaultBackgroundLimit {
		refusal = fmt.Sprintf("%d background commands are already running in this Sandbox", running)
	} else if err == nil {
		record := *current.record
		record.Background = true
		record.Deadline = time.Now().UTC().Add(request.lifetime)
		if longest := record.AdmittedAt.Add(maximumLifetime); record.Deadline.After(longest) {
			record.Deadline = longest
		}
		err = supervision.WriteJSON(r.admissionPath(), record)
	}
	if err = errors.Join(err, lock.Close()); err != nil {
		return answer{}, err
	}
	if current, err = r.observe(); err != nil {
		return answer{}, err
	}
	reply, err := r.answer(current, viewRequest{})
	if refusal != "" {
		reply.Refusal, reply.Reason = refusalBackgroundLimit, refusal
	}
	return reply, err
}

// stop asks the supervisor to end the command's process group: SIGTERM, and
// SIGKILL after the grace period. It waits for the end and is idempotent. A
// run whose supervisor is gone has its recorded process group stopped
// directly and stays lost.
func stop(request request) (answer, error) {
	r := runOf(request.id)
	current, err := r.observe()
	if err != nil {
		return answer{}, err
	}
	switch current.state {
	case stateRunning:
		err = r.updateAdmission(func(record *admission) error {
			if record.StopRequested == nil {
				record.StopRequested = &stopRequest{At: time.Now().UTC(), Grace: request.grace}
			}
			return nil
		})
		if err != nil {
			return answer{}, err
		}
		if current, err = r.observe(); err == nil && current.state == stateRunning && current.record.Supervisor != nil && current.record.Supervisor.Alive() {
			// The signal only wakes the supervisor; the record says what to do.
			_ = syscall.Kill(current.record.Supervisor.PID, syscall.SIGUSR1)
		}
		if err == nil {
			current, err = r.await(min(request.grace+10*time.Second, maximumWait))
		}
	case stateLost:
		if current.record.Process != nil {
			supervision.TerminateGroup(*current.record.Process, request.grace)
		}
	}
	if err != nil {
		return answer{}, err
	}
	return r.answer(current, request.view)
}
