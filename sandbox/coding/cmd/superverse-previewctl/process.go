// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

// child is one process group the daemon started and reaps. Its leader's PID
// is the group ID.
type child struct {
	identity supervision.ProcessIdentity
	exited   chan struct{}
	// result is the leader's exit error, valid once exited is closed.
	result error
}

// startChild starts command as the leader of its own process group, with
// its output appended to log, and reaps it in the background.
func startChild(directory string, command, environment []string, log *os.File) (*child, error) {
	process := exec.Command(command[0], command[1:]...) //nolint:gosec,noctx // the daemon's fixed commands; the process group outlives any request context
	process.Dir = directory
	process.Env = environment
	process.Stdout = log
	process.Stderr = log
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := process.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command[0], err)
	}
	// os/exec returns from Start only after the new program replaced the
	// forked image, so procfs already shows its own command line.
	identity, err := supervision.Identify(process.Process.Pid)
	if err != nil {
		_ = syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
		_ = process.Wait()
		return nil, fmt.Errorf("identify %s: %w", command[0], err)
	}
	started := &child{identity: identity, exited: make(chan struct{})}
	go func() {
		started.result = process.Wait()
		close(started.exited)
	}()
	return started, nil
}

// exitStatus describes how the leader exited, once exited is closed.
func (c *child) exitStatus() string {
	if c.result == nil {
		return "exit status 0"
	}
	return c.result.Error()
}

func (c *child) running() bool {
	select {
	case <-c.exited:
		return false
	default:
		return true
	}
}

// stop ends the whole process group: SIGTERM, then SIGKILL once the leader
// exited or the grace period passed, for members the leader left behind.
func (c *child) stop() {
	_ = syscall.Kill(-c.identity.PID, syscall.SIGTERM)
	select {
	case <-c.exited:
	case <-time.After(supervision.StopGracePeriod):
	}
	_ = syscall.Kill(-c.identity.PID, syscall.SIGKILL)
	<-c.exited
}
