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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/sandbox/coding/internal/supervision"
)

// The application test engine's directories. One lock admits one engine at a
// time; each run works in its own directory, so queued runs never overwrite
// each other.
const (
	apptestRoot         = "/tmp/superverse-apptest"
	apptestLock         = apptestRoot + "/lock"
	apptestRuns         = apptestRoot + "/runs"
	apptestEngineRecord = apptestRoot + "/engine.json"
	// keptFailedRuns is how many failed run directories stay for the agent.
	keptFailedRuns = 3
	// staleRunAge is the age after which a directory without a summary, of a
	// run that was killed before it finished, is removed.
	staleRunAge = 2 * time.Hour
	// passThroughBytes bounds the command output copied to stdout; the
	// whole output stays in the run's output.log.
	passThroughBytes   = 512 << 10
	defaultTestTimeout = 6 * time.Minute
	maximumTestTimeout = 30 * time.Minute
	// commandStopGrace is how long the command and the engine have between
	// SIGTERM and SIGKILL.
	commandStopGrace = 5 * time.Second
)

// Exit codes of apptest run besides the command's own: a run that passed its
// timeout, while queued or running, an engine that did not start twice and a
// stopped run.
const (
	exitUsage             = 2
	exitTimedOut          = 124
	exitEngineUnavailable = 125
	// exitStopped: a SIGTERM or SIGINT stopped the run.
	exitStopped = 143
)

// testRunPhase is where a run ended.
type testRunPhase string

const (
	// phaseQueued: the deadline passed while another run held the engine.
	phaseQueued testRunPhase = "queued"
	// phaseEngine: the engine did not start twice; the command never ran.
	phaseEngine testRunPhase = "engine"
	// phaseCommand: the command ran to its exit or its timeout.
	phaseCommand testRunPhase = "command"
)

var runIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

const apptestUsage = "usage: superverse-previewctl apptest run [--run-id ID] [--timeout 6m] [--report-dir DIR] -- COMMAND [ARGUMENT]"

// testRun is one apptest run: its identity, its directories and its deadline.
type testRun struct {
	id, directory, reportDirectory string
	started, deadline              time.Time
}

func (r testRun) path(name string) string { return filepath.Join(r.directory, name) }

// runApplicationTests is apptest run: it waits for the engine lock, starts a
// fresh engine, runs the command against it, summarizes the engine and the
// reports, tears the engine down and exits with the command's code. It runs
// in the caller's process group, so the caller's kill of that group also ends
// the engine; a SIGTERM tears the engine down first.
func runApplicationTests(arguments []string) int {
	run, command, err := parseTestRun(arguments)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	summary := testRunSummary{RunID: run.id, Directory: run.directory}
	code := run.execute(ctx, command, &summary)
	summary.ExitCode = code
	summary.DurationMS = time.Since(run.started).Milliseconds()
	summary.Kept = code != 0
	writeSummary(run, &summary)
	if !summary.Kept {
		_ = os.RemoveAll(run.directory)
	}
	return code
}

func parseTestRun(arguments []string) (testRun, []string, error) {
	if len(arguments) == 0 || arguments[0] != "run" {
		return testRun{}, nil, errors.New(apptestUsage)
	}
	set := flag.NewFlagSet("apptest run", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	id := set.String("run-id", "", "the run's directory name")
	timeout := set.Duration("timeout", defaultTestTimeout, "the deadline of the run, queue wait included")
	reportDirectory := set.String("report-dir", "", "where the tests and mocks write their reports")
	if err := set.Parse(arguments[1:]); err != nil || set.NArg() == 0 || arguments[len(arguments)-set.NArg()-1] != "--" {
		return testRun{}, nil, errors.New(apptestUsage)
	}
	if *id == "" {
		*id = "run-" + time.Now().UTC().Format("20060102-150405") + "-" + randomSuffix()
	}
	if !runIDPattern.MatchString(*id) || *timeout <= 0 || *timeout > maximumTestTimeout {
		return testRun{}, nil, fmt.Errorf("--run-id must match %s and --timeout must be positive and at most %s", runIDPattern, maximumTestTimeout)
	}
	started := time.Now()
	run := testRun{id: *id, directory: filepath.Join(apptestRuns, *id), started: started, deadline: started.Add(*timeout)}
	if err := os.MkdirAll(apptestRuns, 0o700); err != nil {
		return testRun{}, nil, err
	}
	if err := os.Mkdir(run.directory, 0o700); err != nil {
		return testRun{}, nil, fmt.Errorf("create the run directory: %w", err)
	}
	run.reportDirectory = *reportDirectory
	if run.reportDirectory == "" {
		run.reportDirectory = run.path("report")
	}
	if err := os.MkdirAll(run.reportDirectory, 0o700); err != nil {
		return testRun{}, nil, err
	}
	return run, set.Args(), nil
}

func randomSuffix() string {
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	return hex.EncodeToString(suffix)
}

// execute runs the phases of the run and returns its exit code.
func (r testRun) execute(ctx context.Context, command []string, summary *testRunSummary) int {
	lock, err := r.acquireEngine(ctx)
	if err != nil {
		summary.Phase, summary.EngineError = phaseQueued, err.Error()
		if ctx.Err() != nil {
			return exitStopped
		}
		return exitTimedOut
	}
	defer func() { _ = lock.Close() }()
	stopLeftoverEngine()
	pruneRunDirectories(r.id)
	engine, err := startTestEngine(ctx, r)
	if err == nil {
		defer engine.stop()
	} else {
		fmt.Fprintf(os.Stderr, "superverse-previewctl: the test engine did not start, trying once more: %v\n", err)
		if engine, err = startTestEngine(ctx, r); err != nil {
			summary.Phase, summary.EngineError = phaseEngine, err.Error()
			return exitEngineUnavailable
		}
		defer engine.stop()
	}
	summary.Phase = phaseCommand
	code, timedOut := r.runCommand(ctx, command)
	if ctx.Err() != nil {
		return exitStopped
	}
	summary.TimedOut = timedOut
	collectRunSummary(ctx, r, summary)
	if timedOut {
		return exitTimedOut
	}
	return code
}

// acquireEngine waits for the engine lock until the run's deadline. The
// holder keeps it until it exits; a holder that dies releases it.
func (r testRun) acquireEngine(ctx context.Context) (*os.File, error) {
	announced := false
	for {
		lock, held, err := supervision.TryLock(apptestLock)
		if err != nil {
			return nil, fmt.Errorf("take the test engine lock: %w", err)
		}
		if held {
			return lock, nil
		}
		if !announced {
			fmt.Fprintln(os.Stderr, "superverse-previewctl: another application test run holds the engine; waiting")
			announced = true
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("the run was stopped while it waited for the test engine")
		case <-time.After(250 * time.Millisecond):
		}
		if time.Now().After(r.deadline) {
			return nil, fmt.Errorf("other application test runs held the engine until the %s deadline passed", r.deadline.Sub(r.started).Round(time.Second))
		}
	}
}

// runCommand runs command in the run's environment until it exits or the
// deadline passes, copying its output to output.log and, bounded, to stdout.
func (r testRun) runCommand(ctx context.Context, command []string) (int, bool) {
	output, err := os.OpenFile(r.path("output.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // the run's own log
	if err != nil {
		fmt.Fprintf(os.Stderr, "superverse-previewctl: open the run's output log: %v\n", err)
		return 1, false
	}
	defer func() { _ = output.Close() }()
	writer := &boundedTee{log: output, terminal: os.Stdout, remaining: passThroughBytes, path: r.path("output.log")}
	process := exec.Command(command[0], command[1:]...) //nolint:gosec,noctx // the caller's own test command; its tree is stopped explicitly
	process.Env = r.commandEnvironment()
	process.Stdout, process.Stderr = writer, writer
	// An orphan the command left may hold its output open; Wait stops waiting
	// for that output once the command itself exited.
	process.WaitDelay = commandStopGrace
	if err = process.Start(); err != nil {
		_, _ = fmt.Fprintf(writer, "superverse-previewctl: start %s: %v\n", command[0], err)
		return 127, false
	}
	exited := make(chan struct{})
	go func() {
		_ = process.Wait()
		close(exited)
	}()
	timer := time.NewTimer(time.Until(r.deadline))
	defer timer.Stop()
	timedOut := false
	select {
	case <-exited:
	case <-timer.C:
		timedOut = true
		_, _ = fmt.Fprintf(writer, "\nsuperverse-previewctl: the run passed its %s deadline; stopping the command\n", r.deadline.Sub(r.started).Round(time.Second))
		stopTree(process.Process.Pid, exited)
	case <-ctx.Done():
		stopTree(process.Process.Pid, exited)
	}
	stopOrphans()
	return process.ProcessState.ExitCode(), timedOut
}

// stopOrphans ends the processes the command left behind: members of this
// process group that PID 1 adopted, such as a server a test started in the
// background. The engine, this process and its ancestors are not orphans.
func stopOrphans() {
	group := syscall.Getpgrp()
	var orphans []supervision.ProcessIdentity
	for _, process := range listProcesses() {
		if process.group == group && process.parent == 1 && process.pid != os.Getpid() {
			orphans = append(orphans, identityOf(process.pid)...)
		}
	}
	for _, orphan := range orphans {
		_ = syscall.Kill(orphan.PID, syscall.SIGTERM)
	}
	for deadline := time.Now().Add(commandStopGrace); time.Now().Before(deadline) && anyAlive(orphans); {
		time.Sleep(100 * time.Millisecond)
	}
	for _, orphan := range orphans {
		if orphan.Alive() {
			_ = syscall.Kill(orphan.PID, syscall.SIGKILL)
		}
	}
}

// commandEnvironment is the caller's environment without project
// configuration, pointed at the run's engine and directories.
func (r testRun) commandEnvironment() []string {
	fixed := map[string]string{
		"DEX_FLOW_SERVICE_ADDRESS":      testDexAddress,
		"DEX_WORKER_BIND_ADDRESS":       testWorkerAddress,
		"DEX_WORKER_TARGET":             testWorkerAddress,
		"DEX_BLOB_CACHE_DIR":            r.path("blob-cache"),
		"DEX_CONNECTOR_MOCK_REPORT_DIR": r.reportDirectory,
		"SUPERVERSE_APPTEST_REPORT_DIR": r.reportDirectory,
		"SUPERVERSE_APPTEST_RUN":        r.directory,
	}
	environment := slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		_, isFixed := fixed[name]
		return isFixed || strings.HasPrefix(name, "DEX_PROJECT_")
	})
	for name, value := range fixed {
		environment = append(environment, name+"="+value)
	}
	return environment
}

// boundedTee writes everything to log and the first bytes to terminal, then
// one line that names the log.
type boundedTee struct {
	log, terminal io.Writer
	remaining     int
	path          string
}

func (t *boundedTee) Write(data []byte) (int, error) {
	if _, err := t.log.Write(data); err != nil {
		return 0, err
	}
	switch {
	case t.remaining >= len(data):
		_, _ = t.terminal.Write(data)
		t.remaining -= len(data)
	case t.remaining > 0:
		_, _ = t.terminal.Write(data[:t.remaining])
		_, _ = fmt.Fprintf(t.terminal, "\n[the output continues in %s]\n", t.path)
		t.remaining = 0
	}
	return len(data), nil
}

// stopTree ends a process and every descendant it started: SIGTERM, then
// SIGKILL after the grace period to whatever of them still runs, including
// descendants the leader's exit orphaned.
func stopTree(pid int, exited <-chan struct{}) {
	tree := append(descendants(pid), identityOf(pid)...)
	for _, member := range tree {
		_ = syscall.Kill(member.PID, syscall.SIGTERM)
	}
	select {
	case <-exited:
	case <-time.After(commandStopGrace):
	}
	for deadline := time.Now().Add(commandStopGrace); time.Now().Before(deadline) && anyAlive(tree); {
		time.Sleep(100 * time.Millisecond)
	}
	for _, member := range tree {
		if member.Alive() {
			_ = syscall.Kill(member.PID, syscall.SIGKILL)
		}
	}
	<-exited
}

func identityOf(pid int) []supervision.ProcessIdentity {
	identity, err := supervision.Identify(pid)
	if err != nil {
		return nil
	}
	return []supervision.ProcessIdentity{identity}
}

func anyAlive(processes []supervision.ProcessIdentity) bool {
	return slices.ContainsFunc(processes, supervision.ProcessIdentity.Alive)
}

// procfsProcess is one process as /proc/<pid>/stat shows it.
type procfsProcess struct {
	pid, parent, group int
}

// listProcesses reads every process with its parent and process group.
func listProcesses() []procfsProcess {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var processes []procfsProcess
	for _, entry := range entries {
		var process procfsProcess
		if _, err := fmt.Sscan(entry.Name(), &process.pid); err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// The fields after the command name: state, parent, process group.
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		if len(fields) < 3 {
			continue
		}
		_, parentErr := fmt.Sscan(fields[1], &process.parent)
		_, groupErr := fmt.Sscan(fields[2], &process.group)
		if parentErr == nil && groupErr == nil {
			processes = append(processes, process)
		}
	}
	return processes
}

// descendants are the live descendants of pid, read from procfs.
func descendants(pid int) []supervision.ProcessIdentity {
	children := map[int][]int{}
	for _, process := range listProcesses() {
		children[process.parent] = append(children[process.parent], process.pid)
	}
	var found []supervision.ProcessIdentity
	pending := slices.Clone(children[pid])
	for len(pending) > 0 {
		next := pending[0]
		pending = append(pending[1:], children[next]...)
		found = append(found, identityOf(next)...)
	}
	return found
}

// pruneRunDirectories keeps the newest failed run directories and removes
// stale ones of killed runs. A directory without a summary may belong to a
// queued run, so it stays until it is stale.
func pruneRunDirectories(current string) {
	entries, err := os.ReadDir(apptestRuns)
	if err != nil {
		return
	}
	type finished struct {
		path string
		at   time.Time
	}
	var failed []finished
	for _, entry := range entries {
		path := filepath.Join(apptestRuns, entry.Name())
		info, err := entry.Info()
		if entry.Name() == current || err != nil || !entry.IsDir() {
			continue
		}
		summary, err := os.Stat(filepath.Join(path, "summary.json"))
		switch {
		case err == nil:
			failed = append(failed, finished{path: path, at: summary.ModTime()})
		case time.Since(info.ModTime()) > staleRunAge:
			_ = os.RemoveAll(path)
		}
	}
	slices.SortFunc(failed, func(a, b finished) int { return b.at.Compare(a.at) })
	// The current run may fail too, so it takes one of the kept places.
	for index, run := range failed {
		if index >= keptFailedRuns-1 {
			_ = os.RemoveAll(run.path)
		}
	}
}
