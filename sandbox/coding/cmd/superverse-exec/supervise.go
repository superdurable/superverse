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
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

const (
	// supervisionTick is how often the supervisor looks at its limits and at
	// the admission record, which background and stop rewrite.
	supervisionTick = 250 * time.Millisecond
	// drainTimeout bounds the wait for output after the command's leader
	// exited, before and after the rest of its process group is killed.
	drainTimeout = time.Second
	// exitCodeNotStarted is a shell's exit code for a command that could not be
	// executed.
	exitCodeNotStarted = 127
)

// supervise is the detached supervisor of one run. It holds the run's lock,
// inherited as descriptor 3, until it wrote exit.json; runs the command as the
// leader of its own process group with stdin from /dev/null and stdout and
// stderr merged into output.log; and ends the group at its lifetime, at the
// output limit or on a stop request.
func supervise(id string) error {
	syscall.CloseOnExec(3)
	lock := os.NewFile(3, "lock")
	defer runtime.KeepAlive(lock)
	r := runOf(id)
	var record admission
	if err := supervision.ReadJSON(r.admissionPath(), &record); err != nil {
		return err
	}
	output, err := os.OpenFile(r.outputPath(), os.O_WRONLY|os.O_APPEND, 0) //nolint:gosec // the run's own output file
	if err != nil {
		return err
	}
	defer func() { _ = output.Close() }()
	command, started, err := startCommand(record)
	if err != nil {
		_, _ = fmt.Fprintf(output, "superverse-exec: %v\n", err)
		code := exitCodeNotStarted
		return r.finish(exitRecord{StartedAt: started, EndedAt: time.Now().UTC(), ExitCode: &code, EndReason: endExit, OutputBytes: outputSize(output)})
	}
	return (&supervisor{ledger: r, record: record, command: command, output: output, started: started}).run()
}

// startCommand starts the recorded command in its own process group. Its
// stdout and stderr share one pipe, so output.log keeps their order.
func startCommand(record admission) (*runningCommand, time.Time, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, time.Now().UTC(), err
	}
	command := exec.Command(record.Command[0], record.Command[1:]...) //nolint:gosec,noctx // the admitted command, bounded by its lifetime
	command.Dir = record.WorkingDirectory
	command.Env = os.Environ()
	command.Stdout, command.Stderr = writer, writer
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	started := time.Now().UTC()
	err = command.Start()
	_ = writer.Close()
	if err != nil {
		_ = reader.Close()
		return nil, started, fmt.Errorf("start %s: %w", record.Command[0], err)
	}
	return &runningCommand{command: command, output: reader, exited: make(chan struct{})}, started, nil
}

type runningCommand struct {
	command *exec.Cmd
	output  *os.File
	exited  chan struct{}
	// waitErr is the leader's exit, valid once exited is closed.
	waitErr error
}

type supervisor struct {
	ledger  ledgerRun
	record  admission
	command *runningCommand
	output  *os.File
	started time.Time
	// endReason is set by the first limit or stop that ends the command.
	endReason string
	// killAt is when the group is killed after a SIGTERM; zero when none was sent.
	killAt time.Time
}

func (s *supervisor) run() error {
	group := s.command.command.Process.Pid
	if err := s.recordProcesses(); err != nil {
		// The command runs anyway; the record only lets stop reach it later.
		_, _ = fmt.Fprintf(os.Stderr, "record the processes of %s: %v\n", s.ledger.id, err)
	}
	copied := make(chan int64, 1)
	limited := make(chan struct{}, 1)
	go copyOutput(s.command.output, s.output, copied, limited)
	go func() {
		s.command.waitErr = s.command.command.Wait()
		close(s.command.exited)
	}()
	wake := make(chan os.Signal, 1)
	signal.Notify(wake, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)
	ticker := time.NewTicker(supervisionTick)
	defer ticker.Stop()
	for running := true; running; {
		select {
		case <-s.command.exited:
			running = false
		case <-limited:
			s.end(endOutputLimit, 0)
		case received := <-wake:
			if received != syscall.SIGUSR1 {
				s.end(endStopped, stopGrace)
			}
			s.observeLimits()
		case <-ticker.C:
			s.observeLimits()
		}
		if !s.killAt.IsZero() && !time.Now().Before(s.killAt) {
			_ = syscall.Kill(-group, syscall.SIGKILL)
		}
	}
	// Members of the group that outlive its leader may hold the pipe open: they
	// get a moment to finish writing and are then killed.
	written, drained := s.drain(copied)
	if !drained {
		_ = syscall.Kill(-group, syscall.SIGKILL)
		written, _ = s.drain(copied)
	}
	_ = syscall.Kill(-group, syscall.SIGKILL)
	exit := exitRecord{StartedAt: s.started, EndedAt: time.Now().UTC(), EndReason: s.endReason, OutputBytes: written}
	if exit.EndReason == "" {
		exit.EndReason = endExit
	}
	if waitStatus, ok := s.command.command.ProcessState.Sys().(syscall.WaitStatus); ok && waitStatus.Signaled() {
		exit.Signal = signalName(waitStatus.Signal())
	} else {
		code := s.command.command.ProcessState.ExitCode()
		exit.ExitCode = &code
	}
	return s.ledger.finish(exit)
}

// drain waits up to drainTimeout for the output copy to reach the end of the
// pipe and returns the bytes written to output.log.
func (s *supervisor) drain(copied chan int64) (int64, bool) {
	select {
	case written := <-copied:
		copied <- written
		return written, true
	case <-time.After(drainTimeout):
		return outputSize(s.output), false
	}
}

// observeLimits applies a stop request and the lifetime, both read from the
// admission record, which background and stop rewrite.
func (s *supervisor) observeLimits() {
	var record admission
	if err := supervision.ReadJSON(s.ledger.admissionPath(), &record); err == nil {
		s.record = record
	}
	if s.record.StopRequested != nil {
		s.end(endStopped, s.record.StopRequested.Grace)
	}
	if !time.Now().Before(s.record.Deadline) {
		s.end(endLifetimeLimit, stopGrace)
	}
}

// end ends the process group once: SIGTERM and SIGKILL after grace, or SIGKILL
// at once without grace.
func (s *supervisor) end(reason string, grace time.Duration) {
	if s.endReason != "" {
		return
	}
	s.endReason = reason
	group := s.command.command.Process.Pid
	if grace == 0 {
		_ = syscall.Kill(-group, syscall.SIGKILL)
		return
	}
	_ = syscall.Kill(-group, syscall.SIGTERM)
	s.killAt = time.Now().Add(grace)
}

// recordProcesses records the supervisor and the command's group leader in
// the admission record, so that stop can reach them.
func (s *supervisor) recordProcesses() error {
	self, err := supervision.Identify(os.Getpid())
	if err != nil {
		return err
	}
	leader, err := supervision.Identify(s.command.command.Process.Pid)
	if err != nil {
		return err
	}
	return s.ledger.updateAdmission(func(record *admission) error {
		record.Supervisor, record.Process = &self, &leader
		return nil
	})
}

// copyOutput copies the merged output into output.log up to the output limit
// and signals limited when the command passes it; the rest is read and
// dropped so that the command never blocks on a full pipe until it is killed.
// It sends the bytes written once the pipe is closed.
func copyOutput(pipe, log *os.File, copied chan<- int64, limited chan<- struct{}) {
	var written int64
	buffer := make([]byte, 64<<10)
	for {
		read, err := pipe.Read(buffer)
		if keep := min(int64(read), maximumOutputBytes-written); keep > 0 {
			stored, _ := log.Write(buffer[:keep])
			written += int64(stored)
		}
		if int64(read) > 0 && written >= maximumOutputBytes {
			select {
			case limited <- struct{}{}:
			default:
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				_, _ = fmt.Fprintf(os.Stderr, "read the command output: %v\n", err)
			}
			_ = pipe.Close()
			copied <- written
			return
		}
	}
}

func outputSize(output *os.File) int64 {
	info, err := output.Stat()
	if err != nil {
		return 0
	}
	return info.Size()
}

// finish writes exit.json once, atomically; the lock is released when the
// supervisor exits right after.
func (r ledgerRun) finish(exit exitRecord) error {
	return supervision.WriteJSON(r.exitPath(), exit)
}

func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGPIPE:
		return "SIGPIPE"
	case syscall.SIGSEGV:
		return "SIGSEGV"
	case syscall.SIGABRT:
		return "SIGABRT"
	default:
		return fmt.Sprintf("signal %d", int(sig))
	}
}
