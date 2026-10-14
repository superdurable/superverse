// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command superverse-exec is the command ledger of one coding Sandbox: it starts
// each command under its own detached supervisor, records it under a stable
// execution ID in the Pod's /tmp and answers what became of it, so a command
// survives the sandboxd call that started it and a retried call can ask
// instead of guessing. Every subcommand prints one JSON answer on stdout and
// exits 0 with a definite answer, or 1 with a helper error that says nothing
// about the command. README.md is the contract.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"
)

// Limits of the ledger. The supervisor enforces them itself.
const (
	maximumLifetime         = 6900 * time.Second
	defaultBackgroundLimit  = 4
	maximumWait             = 25 * time.Second
	maximumOutputBytes      = 16 << 20
	maximumAnswerOutput     = 1 << 20
	maximumStopGrace        = time.Minute
	finishedRunRetention    = 2 * time.Hour
	maximumLedgerBytes      = 256 << 20
	decisionLockAcquisition = 10 * time.Second
)

// Run states.
const (
	stateNotFound = "not_found"
	stateRunning  = "running"
	stateExited   = "exited"
	stateLost     = "lost"
)

// Refusals of begin and background: definite answers that started nothing.
const (
	refusalExecutionConflict      = "execution_conflict"
	refusalInstanceReplaced       = "instance_replaced"
	refusalBackgroundLimit        = "background_limit"
	refusalWorkingDirectoryAbsent = "working_directory_unavailable"
)

// End reasons of an exited run.
const (
	endExit          = "exit"
	endLifetimeLimit = "lifetime_limit"
	endOutputLimit   = "output_limit"
	endStopped       = "stopped"
)

// answer is the one JSON object every subcommand prints.
type answer struct {
	ID         string      `json:"id,omitempty"`
	State      string      `json:"state,omitempty"`
	Refusal    string      `json:"refusal,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Background bool        `json:"background,omitempty"`
	StartedAt  *time.Time  `json:"startedAt,omitempty"`
	Deadline   *time.Time  `json:"deadline,omitempty"`
	EndedAt    *time.Time  `json:"endedAt,omitempty"`
	ExitCode   *int        `json:"exitCode,omitempty"`
	Signal     string      `json:"signal,omitempty"`
	EndReason  string      `json:"endReason,omitempty"`
	Output     *outputView `json:"output,omitempty"`
	// Error is set only on exit code 1.
	Error string `json:"error,omitempty"`
}

// The subcommands, as README.md and the server's ledger client spell them.
const (
	subcommandBegin      = "begin"
	subcommandStatus     = "status"
	subcommandBackground = "background"
	subcommandStop       = "stop"
	subcommandSupervise  = "supervise"
)

const usage = "usage: superverse-exec begin --id ID --cwd DIR --lifetime DURATION [--not-before TIME] [--background] [--wait DURATION] [--head N --tail N] -- ARGV... | " +
	"status --id ID [--wait DURATION] [--from OFFSET --max BYTES] [--head N --tail N] | background --id ID --lifetime DURATION | stop --id ID [--grace DURATION] [--head N --tail N]"

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func main() {
	reply, err := run(os.Args[1:])
	if err != nil {
		reply = answer{Error: err.Error()}
	}
	if err = json.NewEncoder(os.Stdout).Encode(reply); err != nil || reply.Error != "" {
		os.Exit(1)
	}
}

// request is the parsed command line of one subcommand.
type request struct {
	id, workingDirectory    string
	lifetime, wait, grace   time.Duration
	notBefore               *time.Time
	background              bool
	view                    viewRequest
	command                 []string
	subcommand, notBeforeIn string
}

func run(arguments []string) (answer, error) {
	if len(arguments) == 0 {
		return answer{}, errors.New(usage)
	}
	parsed, err := parse(arguments[0], arguments[1:])
	if err != nil {
		return answer{}, err
	}
	switch parsed.subcommand {
	case subcommandBegin:
		return begin(parsed)
	case subcommandStatus:
		return status(parsed)
	case subcommandBackground:
		return background(parsed)
	case subcommandStop:
		return stop(parsed)
	case subcommandSupervise:
		return answer{}, supervise(parsed.id)
	}
	return answer{}, errors.New(usage)
}

func parse(subcommand string, arguments []string) (request, error) {
	parsed := request{subcommand: subcommand, grace: stopGrace}
	set := flag.NewFlagSet(subcommand, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&parsed.id, "id", "", "execution ID")
	switch subcommand {
	case subcommandBegin:
		set.StringVar(&parsed.workingDirectory, "cwd", "", "absolute working directory")
		set.DurationVar(&parsed.lifetime, "lifetime", 0, "lifetime of the command")
		set.StringVar(&parsed.notBeforeIn, "not-before", "", "time of the first attempt, RFC 3339")
		set.BoolVar(&parsed.background, "background", false, "start a background command")
		set.DurationVar(&parsed.wait, "wait", 0, "how long to wait for the end")
		parsed.view.register(set, false)
	case subcommandStatus:
		set.DurationVar(&parsed.wait, "wait", 0, "how long to wait for the end")
		parsed.view.register(set, true)
	case subcommandBackground:
		set.DurationVar(&parsed.lifetime, "lifetime", 0, "lifetime from the start")
	case subcommandStop:
		set.DurationVar(&parsed.grace, "grace", stopGrace, "time between SIGTERM and SIGKILL")
		parsed.view.register(set, false)
	case subcommandSupervise:
	default:
		return request{}, errors.New(usage)
	}
	if err := set.Parse(arguments); err != nil {
		return request{}, fmt.Errorf("%w; %s", err, usage)
	}
	parsed.command = set.Args()
	return parsed, parsed.validate()
}

func (parsed *request) validate() error {
	switch {
	case !idPattern.MatchString(parsed.id):
		return errors.New("--id must be 1 to 128 letters, digits, '_' or '-'")
	case parsed.subcommand != subcommandBegin && len(parsed.command) > 0:
		return errors.New(usage)
	case parsed.wait < 0 || parsed.wait > maximumWait:
		return fmt.Errorf("--wait must be between 0 and %s", maximumWait)
	case parsed.grace < 0 || parsed.grace > maximumStopGrace:
		return fmt.Errorf("--grace must be between 0 and %s", maximumStopGrace)
	case (parsed.subcommand == subcommandBegin || parsed.subcommand == subcommandBackground) && (parsed.lifetime <= 0 || parsed.lifetime > maximumLifetime):
		return fmt.Errorf("--lifetime must be positive and at most %s", maximumLifetime)
	}
	if err := parsed.view.validate(); err != nil {
		return err
	}
	if parsed.subcommand != subcommandBegin {
		return nil
	}
	switch {
	case len(parsed.command) == 0 || parsed.command[0] == "":
		return fmt.Errorf("begin needs the command after --; %s", usage)
	case parsed.workingDirectory == "" || parsed.workingDirectory[0] != '/':
		return errors.New("--cwd must be an absolute directory")
	}
	if parsed.notBeforeIn != "" {
		notBefore, err := time.Parse(time.RFC3339Nano, parsed.notBeforeIn)
		if err != nil {
			return fmt.Errorf("--not-before must be an RFC 3339 time: %w", err)
		}
		parsed.notBefore = &notBefore
	}
	return nil
}
