// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// commandLedgerProgram is the coding image's command ledger; its README is the
// contract this file speaks.
const commandLedgerProgram = "superverse-exec"

// maximumLedgerAnswerBytes bounds one ledger answer: an output window of at
// most 1 MiB, JSON-escaped, and the run's fields.
const maximumLedgerAnswerBytes = 8 << 20

// CommandState is the state of a ledger run.
type CommandState string

const (
	CommandNotFound CommandState = "not_found"
	CommandRunning  CommandState = "running"
	CommandExited   CommandState = "exited"
	CommandLost     CommandState = "lost"
)

// CommandRefusal says why the ledger started or changed nothing.
type CommandRefusal string

const (
	CommandRefusalExecutionConflict CommandRefusal = "execution_conflict"
	CommandRefusalInstanceReplaced  CommandRefusal = "instance_replaced"
	CommandRefusalBackgroundLimit   CommandRefusal = "background_limit"
	CommandRefusalNoWorkingDir      CommandRefusal = "working_directory_unavailable"
)

// CommandEndReason says how an exited run ended.
type CommandEndReason string

const (
	CommandEndExit          CommandEndReason = "exit"
	CommandEndLifetimeLimit CommandEndReason = "lifetime_limit"
	CommandEndOutputLimit   CommandEndReason = "output_limit"
	CommandEndStopped       CommandEndReason = "stopped"
)

// CommandOutputView selects the output an answer carries: Head and Tail bytes
// of the beginning and end cut at line boundaries, and a window of Max bytes
// from the byte offset From. Zero values select nothing.
type CommandOutputView struct {
	Head, Tail int
	From       int64
	Max        int
}

// CommandRequest admits one command. The command runs in WorkingDirectory,
// the agent root, with the tool environment of that root.
type CommandRequest struct {
	ID               ExecutionID
	WorkingDirectory string
	Command          []string
	Lifetime         time.Duration
	// NotBefore is the time of the call's first attempt, passed from the second
	// attempt on, so that a replaced instance is told apart from one the first
	// attempt never reached.
	NotBefore  *time.Time
	Background bool
	// Wait holds the answer until the command ends or Wait passes, at most 25 s.
	Wait   time.Duration
	Output CommandOutputView
}

// CommandStatus is the ledger's answer about one run.
type CommandStatus struct {
	ID         string           `json:"id"`
	State      CommandState     `json:"state"`
	Refusal    CommandRefusal   `json:"refusal,omitempty"`
	Reason     string           `json:"reason,omitempty"`
	Background bool             `json:"background,omitempty"`
	StartedAt  *time.Time       `json:"startedAt,omitempty"`
	Deadline   *time.Time       `json:"deadline,omitempty"`
	EndedAt    *time.Time       `json:"endedAt,omitempty"`
	ExitCode   *int             `json:"exitCode,omitempty"`
	Signal     string           `json:"signal,omitempty"`
	EndReason  CommandEndReason `json:"endReason,omitempty"`
	Output     CommandOutput    `json:"output"`
}

// CommandOutput is the merged output file of a run and the requested parts.
type CommandOutput struct {
	Path       string             `json:"path"`
	Bytes      int64              `json:"bytes"`
	Projection *CommandProjection `json:"projection,omitempty"`
	Window     *CommandWindow     `json:"window,omitempty"`
}

// CommandProjection is the head and tail of the output; when the output fits,
// Head holds all of it and nothing is omitted.
type CommandProjection struct {
	Head         string `json:"head"`
	Tail         string `json:"tail"`
	OmittedBytes int64  `json:"omittedBytes"`
}

// CommandWindow is the output from From to Next.
type CommandWindow struct {
	From int64  `json:"from"`
	Next int64  `json:"next"`
	Data string `json:"data"`
}

// CommandLedger reaches the command ledger of one Sandbox through its process
// API: each operation is one bounded helper process with a JSON answer.
type CommandLedger struct {
	client *SandboxClient
}

// CommandLedger is the ledger of this client's Sandbox.
func (client *SandboxClient) CommandLedger() CommandLedger {
	return CommandLedger{client: client}
}

// Begin starts the command once per execution ID, or answers the run that
// already has the ID. The helper and, through it, the command run in the tool
// environment of the working directory.
func (ledger CommandLedger) Begin(ctx context.Context, request CommandRequest) (CommandStatus, error) {
	arguments := ledgerArguments("begin", request.ID, "--cwd", request.WorkingDirectory, "--lifetime", duration(request.Lifetime), "--wait", duration(request.Wait))
	if request.NotBefore != nil {
		arguments = append(arguments, "--not-before", request.NotBefore.UTC().Format(time.RFC3339Nano))
	}
	if request.Background {
		arguments = append(arguments, "--background")
	}
	arguments = append(append(arguments, request.Output.flags(false)...), "--")
	return ledger.ask(ctx, append(arguments, request.Command...), ToolProcessEnvironment(request.WorkingDirectory, ledger.client.redirects...))
}

// Status answers the state of a run, holding the answer while it runs for at
// most wait.
func (ledger CommandLedger) Status(ctx context.Context, id ExecutionID, wait time.Duration, view CommandOutputView) (CommandStatus, error) {
	arguments := append(ledgerArguments("status", id, "--wait", duration(wait)), view.flags(true)...)
	return ledger.ask(ctx, arguments, GitProcessEnvironment())
}

// Background turns a running foreground command into a background command
// whose lifetime ends lifetime from now.
func (ledger CommandLedger) Background(ctx context.Context, id ExecutionID, lifetime time.Duration) (CommandStatus, error) {
	return ledger.ask(ctx, ledgerArguments("background", id, "--lifetime", duration(lifetime)), GitProcessEnvironment())
}

// Stop ends the run's process group, SIGTERM and SIGKILL after grace, and
// answers its final state. It is idempotent.
func (ledger CommandLedger) Stop(ctx context.Context, id ExecutionID, grace time.Duration, view CommandOutputView) (CommandStatus, error) {
	arguments := append(ledgerArguments("stop", id, "--grace", duration(grace)), view.flags(false)...)
	return ledger.ask(ctx, arguments, GitProcessEnvironment())
}

// ledgerArguments is the command line of a ledger subcommand about the run id,
// with its other flags.
func ledgerArguments(subcommand string, id ExecutionID, flags ...string) []string {
	return append([]string{commandLedgerProgram, subcommand, "--id", string(id)}, flags...)
}

// ask runs one ledger subcommand. An answer that does not follow the
// contract means the image's helper is incompatible, a platform fault.
func (ledger CommandLedger) ask(ctx context.Context, arguments []string, environment []EnvironmentVariable) (CommandStatus, error) {
	answer, err := ledger.client.runHelperJSON(ctx, arguments, environment, nil, maximumLedgerAnswerBytes)
	if err != nil {
		return CommandStatus{}, err
	}
	var status CommandStatus
	if err = json.Unmarshal(answer, &status); err != nil {
		return CommandStatus{}, NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("%s answered %s with an unreadable answer: %w", commandLedgerProgram, arguments[1], err))
	}
	switch status.State {
	case CommandNotFound, CommandRunning, CommandExited, CommandLost:
		return status, nil
	default:
		return CommandStatus{}, NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("%s answered %s with the unknown state %q", commandLedgerProgram, arguments[1], status.State))
	}
}

func (view CommandOutputView) flags(window bool) []string {
	var flags []string
	if view.Head > 0 || view.Tail > 0 {
		flags = append(flags, "--head", strconv.Itoa(view.Head), "--tail", strconv.Itoa(view.Tail))
	}
	if window && view.Max > 0 {
		flags = append(flags, "--from", strconv.FormatInt(view.From, 10), "--max", strconv.Itoa(view.Max))
	}
	return flags
}

// duration renders a duration in whole milliseconds, which the ledger's Go
// duration flags read exactly.
func duration(value time.Duration) string {
	return strconv.FormatInt(value.Milliseconds(), 10) + "ms"
}
