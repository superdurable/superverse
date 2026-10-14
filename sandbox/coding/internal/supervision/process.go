// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package supervision holds the process plumbing that the coding image's
// supervisors share: process identities read from procfs, process-group
// termination, detached starts, exclusive file locks and atomic JSON records.
// It uses only the standard library.
package supervision

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// StopGracePeriod is how long a process group has between SIGTERM and SIGKILL.
const StopGracePeriod = 10 * time.Second

// clockTicksPerSecond is USER_HZ, the unit of procfs start times. It is 100 on
// every Linux architecture the image is built for.
const clockTicksPerSecond = 100

// ProcessIdentity names one process beyond its PID, which the kernel reuses:
// a PID and its start time name one process per boot. The command line is
// recorded for people only. A process identified right after it started may
// show an empty one: execve closes the descriptor that tells os/exec the new
// program runs before the kernel publishes the new arguments, and a shell that
// execs its last command replaces it later.
type ProcessIdentity struct {
	PID       int    `json:"pid"`
	StartTime string `json:"startTime"`
	Command   string `json:"command"`
}

// Identify reads the identity of a live process from procfs.
func Identify(pid int) (ProcessIdentity, error) {
	fields, err := statFields(pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	command, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ProcessIdentity{}, err
	}
	return ProcessIdentity{PID: pid, StartTime: fields[19], Command: strings.TrimRight(strings.ReplaceAll(string(command), "\x00", " "), " ")}, nil
}

// Alive reports whether the process still runs: its PID names a process with
// the same start time, whatever program that process now executes.
func (identity ProcessIdentity) Alive() bool {
	fields, err := statFields(identity.PID)
	return err == nil && fields[19] == identity.StartTime
}

// StartTime is the wall-clock time at which a process started: the boot time
// plus the process's start in clock ticks after boot, to the second.
func StartTime(pid int) (time.Time, error) {
	fields, err := statFields(pid)
	if err != nil {
		return time.Time{}, err
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse the start time of process %d: %w", pid, err)
	}
	stat, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	for line := range strings.SplitSeq(string(stat), "\n") {
		if value, found := strings.CutPrefix(line, "btime "); found {
			boot, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return time.Time{}, fmt.Errorf("parse the boot time: %w", err)
			}
			return time.Unix(boot+ticks/clockTicksPerSecond, 0).UTC(), nil
		}
	}
	return time.Time{}, errors.New("/proc/stat names no boot time")
}

// statFields are the fields of /proc/<pid>/stat after the command name, which
// may contain spaces and parentheses: fields[0] is the state (field 3) and
// fields[19] the start time (field 22).
func statFields(pid int) ([]string, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	if len(fields) < 20 {
		return nil, errors.New("process stat is incomplete")
	}
	return fields, nil
}

// TerminateGroup stops the process group led by a process the caller did not
// start, or no longer waits for: SIGTERM, then SIGKILL after grace while the
// leader still runs. A leader that is gone, or whose PID now names another
// process, is left alone.
func TerminateGroup(identity ProcessIdentity, grace time.Duration) {
	if !identity.Alive() {
		return
	}
	_ = syscall.Kill(-identity.PID, syscall.SIGTERM)
	for deadline := time.Now().Add(grace); time.Now().Before(deadline) && identity.Alive(); {
		time.Sleep(100 * time.Millisecond)
	}
	if identity.Alive() {
		_ = syscall.Kill(-identity.PID, syscall.SIGKILL)
	}
}

// Detach starts command in a session of its own, so neither sandboxd's
// process-group cleanup of the calling command nor the caller's exit reaches
// it, and releases it: PID 1 reaps it once it exits. It returns the identity of
// the started process, which is zero when the process already exited; os/exec
// returns from Start only after the new program replaced the forked image, so
// procfs already shows its own command line.
func Detach(command *exec.Cmd) (ProcessIdentity, error) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return ProcessIdentity{}, err
	}
	identity, err := Identify(command.Process.Pid)
	if err != nil {
		identity = ProcessIdentity{}
	}
	return identity, command.Process.Release()
}
