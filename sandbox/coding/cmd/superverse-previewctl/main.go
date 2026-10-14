// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command superverse-previewctl owns the Preview environment of one coding
// Sandbox (coding profile v4): a local Temporal and Dex engine whose state
// lives on the workspace volume, isolated source trees of prepared commits
// and the Preview application built from them. One detached daemon per Pod,
// `supervise`, owns every process; the other commands ask it over a Pod-local
// socket. Every command prints one JSON answer on stdout and exits 0 with a
// definite state, or 1 with state "error", except apptest run, which runs
// a command against a fresh application test engine. README.md is the
// contract.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Answer states.
const (
	stateStarting      = "starting"
	stateReady         = "ready"
	stateFull          = "full"
	stateInconsistent  = "inconsistent"
	stateFailed        = "failed"
	stateStopping      = "stopping"
	stateStopped       = "stopped"
	stateBuilding      = "building"
	stateRunning       = "running"
	stateSourceMissing = "source_missing"
	stateError         = "error"
)

// answer is the one JSON object every command prints.
type answer struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	environmentIdentity
	applicationIdentity
	ApplicationPort int    `json:"applicationPort,omitempty"`
	WorkerListening bool   `json:"workerListening,omitempty"`
	Path            string `json:"path,omitempty"`
}

const usage = "usage: superverse-previewctl environment ensure|status|stop | source ensure --commit SHA | begin --commit SHA | status | stop | shutdown [--timeout DURATION] | supervise | apptest run [--run-id ID] [--timeout 6m] [--report-dir DIR] -- COMMAND [ARGUMENT]"

// The commands of the command line, as README.md spells them.
const (
	runEnvironmentEnsure = "environment ensure"
	runEnvironmentStatus = "environment status"
	runEnvironmentStop   = "environment stop"
	runSourceEnsure      = "source ensure"
	runBegin             = "begin"
	runStatus            = "status"
	runStop              = "stop"
	runShutdown          = "shutdown"
	runSupervise         = "supervise"
)

// launchWait is how long ensure waits for a daemon it launched to answer.
const launchWait = 3 * time.Second

var commitPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

func main() {
	// apptest run streams its command's output and exits with its code.
	if len(os.Args) > 1 && os.Args[1] == "apptest" {
		os.Exit(runApplicationTests(os.Args[2:]))
	}
	reply, err := run(os.Args[1:])
	if err != nil {
		reply = answer{State: stateError, Reason: err.Error()}
	}
	if err = json.NewEncoder(os.Stdout).Encode(reply); err != nil || reply.State == stateError {
		os.Exit(1)
	}
}

func run(arguments []string) (answer, error) {
	words := 1
	if len(arguments) > 0 && (arguments[0] == "environment" || arguments[0] == "source") {
		words = 2
	}
	if len(arguments) < words {
		return answer{}, errors.New(usage)
	}
	command := strings.Join(arguments[:words], " ")
	set := flag.NewFlagSet(command, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var commit string
	timeout := 30 * time.Second
	switch command {
	case runSourceEnsure, runBegin:
		set.StringVar(&commit, "commit", "", "full SHA of the prepared commit")
	case runShutdown:
		set.DurationVar(&timeout, "timeout", timeout, "how long to wait for the stop")
	}
	if err := set.Parse(arguments[words:]); err != nil || set.NArg() != 0 {
		return answer{}, errors.New(usage)
	}
	if (command == runSourceEnsure || command == runBegin) && !commitPattern.MatchString(commit) {
		return answer{}, errors.New("--commit must be a full lowercase commit SHA")
	}
	switch command {
	case runEnvironmentEnsure:
		return ensureEnvironment()
	case runEnvironmentStatus:
		return environmentStatus()
	case runEnvironmentStop:
		return stopEnvironment()
	case runSourceEnsure:
		return ensureSourceTree(commit)
	case runBegin:
		return beginApplication(commit)
	case runStatus:
		return askOr(controlRequest{Command: commandStatus}, stateStopped)
	case runStop:
		return askOr(controlRequest{Command: commandStop}, stateStopped)
	case runShutdown:
		return shutdown(timeout)
	case runSupervise:
		return supervise()
	}
	return answer{}, errors.New(usage)
}

// ensureEnvironment answers the daemon's environment state and launches the
// daemon when none runs. Concurrent callers may each launch one; all but one
// lose the state lock and exit.
func ensureEnvironment() (answer, error) {
	if reply, err := ask(controlRequest{Command: commandEnsure}); err == nil {
		return reply, nil
	}
	state, err := openStateDirectory()
	if err != nil {
		return answer{}, err
	}
	running, err := supervisorRunning(state)
	if err != nil {
		return answer{}, err
	}
	if running {
		// A daemon is starting or stopping; it answers once its socket is up.
		return answer{State: stateStarting}, nil
	}
	if err = launchSupervisor(state); err != nil {
		return answer{}, err
	}
	for deadline := time.Now().Add(launchWait); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if reply, err := ask(controlRequest{Command: commandEnsure}); err == nil {
			return reply, nil
		}
	}
	return answer{State: stateStarting}, nil
}

func environmentStatus() (answer, error) {
	reply, err := ask(controlRequest{Command: commandEnvironmentStatus})
	if !errors.Is(err, errNoSupervisor) {
		return reply, err
	}
	return lockState(stateStarting)
}

// stopEnvironment requests the daemon's stop; it answers stopping until the
// daemon exited and released the state lock.
func stopEnvironment() (answer, error) {
	reply, err := ask(controlRequest{Command: commandEnvironmentStop})
	if !errors.Is(err, errNoSupervisor) {
		return reply, err
	}
	return lockState(stateStopping)
}

// lockState answers whenHeld while a daemon without a control socket holds
// the state lock, and stopped once none does.
func lockState(whenHeld string) (answer, error) {
	state, err := openStateDirectory()
	if err != nil {
		return answer{}, err
	}
	running, err := supervisorRunning(state)
	if err != nil {
		return answer{}, err
	}
	if running {
		return answer{State: whenHeld}, nil
	}
	return answer{State: stateStopped}, nil
}

// shutdown is the preStop hook: it stops the environment and waits until the
// daemon released the state lock or the timeout passed.
func shutdown(timeout time.Duration) (answer, error) {
	state, err := openStateDirectory()
	if err != nil {
		return answer{}, err
	}
	for deadline := time.Now().Add(timeout); ; time.Sleep(250 * time.Millisecond) {
		running, err := supervisorRunning(state)
		if err != nil {
			return answer{}, err
		}
		if !running {
			return answer{State: stateStopped}, nil
		}
		if time.Now().After(deadline) {
			return answer{State: stateStopping, Reason: fmt.Sprintf("the Preview supervisor did not stop within %s", timeout)}, nil
		}
		// A daemon that is still starting opens its socket later; asking
		// again is idempotent.
		_, _ = ask(controlRequest{Command: commandEnvironmentStop})
	}
}

func ensureSourceTree(commit string) (answer, error) {
	state, err := openStateDirectory()
	if err != nil {
		return answer{}, err
	}
	tree, err := ensureSource(context.Background(), state, commit, false)
	if errors.Is(err, errSourceMissing) {
		return answer{State: stateSourceMissing, Reason: err.Error(), applicationIdentity: applicationIdentity{Commit: commit}}, nil
	}
	if err != nil {
		return answer{}, err
	}
	return answer{State: stateReady, applicationIdentity: applicationIdentity{Commit: commit}, Path: tree.path}, nil
}

// beginApplication asks the daemon to start the application of commit with
// the configuration digest and grant generation of this process's
// environment, which the build and the application inherit.
func beginApplication(commit string) (answer, error) {
	digest := os.Getenv("DEX_PROJECT_CONFIG_DIGEST")
	generation, err := strconv.ParseUint(os.Getenv("SUPERVERSE_PREVIEW_GRANT_GENERATION"), 10, 64)
	if digest == "" || err != nil || generation == 0 {
		return answer{}, errors.New("begin requires DEX_PROJECT_CONFIG_DIGEST and a positive SUPERVERSE_PREVIEW_GRANT_GENERATION")
	}
	reply, err := ask(controlRequest{
		Command:     commandBegin,
		Application: applicationIdentity{Commit: commit, ConfigurationDigest: digest, GrantGeneration: generation},
		Environment: os.Environ(),
	})
	if errors.Is(err, errNoSupervisor) {
		return answer{}, errors.New("the Preview environment is not running; start it with superverse-previewctl environment ensure")
	}
	return reply, err
}

// askOr answers absent when no daemon runs.
func askOr(request controlRequest, absent string) (answer, error) {
	reply, err := ask(request)
	if errors.Is(err, errNoSupervisor) {
		return answer{State: absent}, nil
	}
	return reply, err
}

// runCommand runs one bounded command and returns its combined output. A nil
// environment inherits this process's.
func runCommand(ctx context.Context, environment []string, name string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, name, arguments...) //nolint:gosec // the real engine CLIs and git with fixed subcommands
	command.Env = environment
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("%s %s: %w: %s", filepath.Base(name), strings.Join(arguments, " "), err, text)
	}
	return text, nil
}
