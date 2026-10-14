// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxtools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
)

// The bash tool runs a shell command in the agent root through the Sandbox's
// command ledger, under a ledger ID derived from the call identity: a retried
// attempt finds the command it started instead of starting it again. The
// Session begins the command, polls its status until it ends and, after a
// timeout or exhausted retries, settles it from the ledger, which moves a
// command that still runs to the background. A command started with
// run_in_background is a background task at once; its ledger ID is the task ID.
const (
	// defaultCommandTimeout applies when a call names no timeout_seconds.
	defaultCommandTimeout = 120 * time.Second
	// BackgroundCommandLifetime is how long a background command may run, from
	// its start or from its move to the background; the ledger then ends it.
	BackgroundCommandLifetime = 3300 * time.Second
	// CommandStatusWait is how long one status request waits for the end.
	CommandStatusWait = 20 * time.Second
	// commandBeginWait is how long begin waits, so that a short command
	// answers in one request.
	commandBeginWait = 5 * time.Second
	// commandLifetimeGrace is how long after its timeout the ledger itself
	// ends a foreground command whose Session stopped polling.
	commandLifetimeGrace = 30 * time.Second
	// commandStopGrace is the time between SIGTERM and SIGKILL of a stop.
	commandStopGrace = 10 * time.Second
	// ledgerRequestTimeout bounds one ledger request that does not wait.
	ledgerRequestTimeout = 30 * time.Second
	maximumCommandBytes  = 64 << 10
	// A failure's detail shows a stopped command's first 500 and last 1,000
	// bytes of output; the failure text holds at most 2 KiB of detail.
	detailHeadBytes = 500
	detailTailBytes = 1000
)

// commandOutputView is the output a bash result carries: the first 8,000 and
// the last 22,000 bytes, cut at line boundaries.
var commandOutputView = sandboxd.CommandOutputView{Head: 8000, Tail: 22000}

// subagentCommandPrefix runs a subagent's command at a lower priority and with
// bounded Go and Node parallelism and memory, because the main agent, the
// Preview and up to three subagents share the Sandbox's two CPUs and 4 GiB.
var subagentCommandPrefix = []string{"nice", "-n", "10", "env", "GOFLAGS=-p=2", "GOMAXPROCS=2", "NODE_OPTIONS=--max-old-space-size=1024"}

// CommandTimeout is the timeout of a bash call. Arguments that do not decode
// get the default; their call fails at once with invalid_arguments.
func CommandTimeout(arguments string) time.Duration {
	if input, err := toolinput.Bash.Decode(arguments); err == nil {
		return time.Duration(input.TimeoutSeconds.Value) * time.Second
	}
	return defaultCommandTimeout
}

// beginCommand starts the call's command once and waits briefly for its end.
// A command that still runs is a pending result, which the Session polls with
// CommandStatus.
func (execution *Execution) beginCommand(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.Bash.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	if err = validateNonBlankText("command", input.Command, maximumCommandBytes); err != nil {
		return Result{}, err
	}
	if err = validateNonBlankText("description", input.Description, 4*200); err != nil {
		return Result{}, err
	}
	timeout := time.Duration(input.TimeoutSeconds.Value) * time.Second
	command := []string{"bash", "-c", input.Command}
	if call.Subagent {
		if input.RunInBackground.Value {
			return Result{}, invalidArguments(errors.New("run_in_background: a subagent runs its commands in the foreground"))
		}
		command = append(slices.Clone(subagentCommandPrefix), command...)
	}
	request := sandboxd.CommandRequest{
		ID: commandID(call), WorkingDirectory: call.Root, Command: command,
		Lifetime: timeout + commandLifetimeGrace, NotBefore: call.NotBefore, Wait: min(commandBeginWait, timeout), Output: commandOutputView,
	}
	if input.RunInBackground.Value {
		request.Background, request.Lifetime, request.Wait, request.Output = true, BackgroundCommandLifetime, 0, sandboxd.CommandOutputView{}
	}
	ctx, cancel := context.WithTimeout(ctx, commandBeginWait+ledgerRequestTimeout)
	defer cancel()
	status, err := execution.executor.CommandLedger().Begin(ctx, request)
	switch {
	case err != nil:
		return Result{}, err
	case request.Background && status.Refusal == "" && status.State != sandboxd.CommandLost:
		// A retried begin may find the command already ended; its watcher
		// records the end at once.
		return backgroundResult(call, status, "The command runs in the background", ""), nil
	}
	return commandResult(status, timeout)
}

// moveTimedOutCommand settles a command whose timeout passed: the ledger moves
// one that still runs to the background, where it continues as a task; with
// four background commands already running it is stopped and timed out
// instead. A command that ended by itself meanwhile has its ordinary result.
func (execution *Execution) moveTimedOutCommand(ctx context.Context, call Call) (Result, error) {
	return execution.moveToBackground(ctx, call, fmt.Sprintf("The command ran past its timeout of %s and", CommandTimeout(call.Arguments)), (*Execution).stopTimedOutCommand)
}

// moveToBackground moves the call's running command to the background and
// answers with its output so far and its task ID, or with fallback's answer
// when the background limit refuses the move. A command that is not running
// has its settled result. A repeated move finds the command already moved.
func (execution *Execution) moveToBackground(ctx context.Context, call Call, reason string, fallback Tool) (Result, error) {
	moveCtx, cancel := context.WithTimeout(ctx, ledgerRequestTimeout)
	defer cancel()
	moved, err := execution.executor.CommandLedger().Background(moveCtx, commandID(call), BackgroundCommandLifetime)
	switch {
	case err != nil:
		return Result{}, err
	case moved.Refusal == sandboxd.CommandRefusalBackgroundLimit:
		return fallback(execution, ctx, call)
	}
	status, err := execution.executor.CommandLedger().Status(moveCtx, commandID(call), 0, commandOutputView)
	if err != nil {
		return Result{}, err
	}
	if status.State != sandboxd.CommandRunning || !status.Background {
		return commandResult(status, CommandTimeout(call.Arguments))
	}
	return backgroundResult(call, status, reason+" continues in the background", commandOutputText(status.Output)), nil
}

// backgroundResult is the successful result of a command that runs as a
// background task: its task ID, how the model follows it and, for a moved
// command, its output so far.
func backgroundResult(call Call, status sandboxd.CommandStatus, summary, outputSoFar string) Result {
	taskID := string(commandID(call))
	text := fmt.Sprintf("%s as task %s. You are notified when it ends; read its output with read_task_output and stop it with stop_task.", summary, taskID)
	if outputSoFar != "" {
		text = outputSoFar + "\n\n" + text
	}
	input, _ := toolinput.Bash.Decode(call.Arguments)
	result := measured(status)
	result.Content = text
	result.Background = &BackgroundStart{TaskID: taskID, Description: input.Description, StartedAt: result.StartedAt}
	if status.Deadline != nil {
		result.Background.Deadline = *status.Deadline
	}
	return result
}

// commandStatus waits up to CommandStatusWait for the end of the call's
// command; the caller bounds the request.
func (execution *Execution) commandStatus(ctx context.Context, call Call) (Result, error) {
	status, err := execution.executor.CommandLedger().Status(ctx, commandID(call), CommandStatusWait, commandOutputView)
	if err != nil {
		return Result{}, err
	}
	return commandResult(status, CommandTimeout(call.Arguments))
}

// stopTimedOutCommand stops a command whose timeout passed. Once the ledger
// confirms the stop, the call failed with command_timed_out and the output so
// far; a command that ended by itself meanwhile has its ordinary result.
func (execution *Execution) stopTimedOutCommand(ctx context.Context, call Call) (Result, error) {
	timeout := CommandTimeout(call.Arguments)
	status, err := execution.stopCommand(ctx, call)
	if err != nil {
		return Result{}, err
	}
	if status.State == sandboxd.CommandExited && status.EndReason == sandboxd.CommandEndStopped {
		return measured(status), sandboxd.NewToolFailure(toolfailure.CommandTimedOut,
			fmt.Errorf("the command exceeded its timeout of %s and was stopped with every process it started; its full output is in %s", timeout, status.Output.Path)).
			WithEffect(toolfailure.EffectPartial).WithDetail(outputDetail(status.Output))
	}
	return commandResult(status, timeout)
}

// settleCommand settles a command whose attempts are exhausted without a
// timeout: an exited command has its result; one that still runs moves to the
// background like a timed-out one, and with four background commands running
// it is stopped and interrupted; a lost or unknown one may have run.
func (execution *Execution) settleCommand(ctx context.Context, call Call) (Result, error) {
	status, err := execution.executor.CommandLedger().Status(ctx, commandID(call), 0, commandOutputView)
	if err != nil {
		return Result{}, err
	}
	if status.State != sandboxd.CommandRunning {
		return commandResult(status, CommandTimeout(call.Arguments))
	}
	return execution.moveToBackground(ctx, call, "The platform could not follow the command to its end; it", (*Execution).stopInterruptedCommand)
}

// settleForegroundCommand settles a subagent's command whose attempts are
// exhausted: an exited command has its result, and one that still runs is
// stopped and interrupted, because a subagent has no background commands.
func (execution *Execution) settleForegroundCommand(ctx context.Context, call Call) (Result, error) {
	status, err := execution.executor.CommandLedger().Status(ctx, commandID(call), 0, commandOutputView)
	if err != nil {
		return Result{}, err
	}
	if status.State != sandboxd.CommandRunning {
		return commandResult(status, CommandTimeout(call.Arguments))
	}
	return execution.stopInterruptedCommand(ctx, call)
}

// stopInterruptedCommand stops a running command the platform could not
// follow and could not move to the background: it was interrupted.
func (execution *Execution) stopInterruptedCommand(ctx context.Context, call Call) (Result, error) {
	status, err := execution.stopCommand(ctx, call)
	if err != nil {
		return Result{}, err
	}
	if status.State != sandboxd.CommandExited || status.EndReason != sandboxd.CommandEndStopped {
		return commandResult(status, CommandTimeout(call.Arguments))
	}
	return measured(status), sandboxd.NewToolFailure(toolfailure.CommandInterrupted,
		fmt.Errorf("the platform could not follow the command to its end and stopped it; its output so far is in %s", status.Output.Path)).
		WithEffect(toolfailure.EffectPartial).WithDetail(outputDetail(status.Output))
}

// stopCommand stops the call's command and answers its final state. A stop
// the ledger could not confirm leaves the outcome unknown.
func (execution *Execution) stopCommand(ctx context.Context, call Call) (sandboxd.CommandStatus, error) {
	return execution.stopRun(ctx, commandID(call), commandOutputView)
}

// stopRun stops one ledger run and answers its final state with the output
// view.
func (execution *Execution) stopRun(ctx context.Context, id sandboxd.ExecutionID, view sandboxd.CommandOutputView) (sandboxd.CommandStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, commandStopGrace+ledgerRequestTimeout)
	defer cancel()
	status, err := execution.executor.CommandLedger().Stop(ctx, id, commandStopGrace, view)
	if err == nil && status.State == sandboxd.CommandRunning {
		return sandboxd.CommandStatus{}, interrupted("the command did not stop within its grace period")
	}
	return status, err
}

// commandResult maps the ledger's answer to the call's result: a running
// command is pending; an exit is the output with a status line when it was
// not successful; a limit, a stop and a lost or replaced instance are typed
// failures.
func commandResult(status sandboxd.CommandStatus, timeout time.Duration) (Result, error) {
	switch status.Refusal {
	case "":
	case sandboxd.CommandRefusalExecutionConflict:
		return Result{}, sandboxd.NewToolFailure(toolfailure.ExecutionConflict, errors.New("the Sandbox's command ledger holds another command under this call's identity"))
	case sandboxd.CommandRefusalInstanceReplaced:
		return Result{}, interrupted("the Sandbox instance was replaced after an earlier attempt of the call, which may have run the command: " + status.Reason)
	case sandboxd.CommandRefusalNoWorkingDir:
		return Result{}, sandboxd.NewToolFailure(toolfailure.CommandNotStarted, errors.New("nothing ran: "+status.Reason))
	default:
		return Result{}, sandboxd.NewToolFailure(toolfailure.BackgroundLimitReached, errors.New(status.Reason))
	}
	switch status.State {
	case sandboxd.CommandRunning:
		return Result{Pending: true}, nil
	case sandboxd.CommandLost:
		return measured(status), interrupted("the command's supervisor ended before it recorded the command's end, for example because the Sandbox container restarted")
	case sandboxd.CommandNotFound:
		return Result{}, interrupted("the Sandbox instance has no record of the command; it was replaced while the command ran")
	case sandboxd.CommandExited:
	}
	switch status.EndReason {
	case sandboxd.CommandEndOutputLimit:
		return measured(status), sandboxd.NewToolFailure(toolfailure.CommandOutputLimitExceeded,
			fmt.Errorf("the command printed more than 16 MiB and was stopped with every process it started; the first 16 MiB are in %s", status.Output.Path)).
			WithEffect(toolfailure.EffectPartial).WithDetail(outputDetail(status.Output))
	case sandboxd.CommandEndLifetimeLimit:
		return measured(status), sandboxd.NewToolFailure(toolfailure.CommandTimedOut,
			fmt.Errorf("the command exceeded its timeout of %s and was stopped with every process it started; its full output is in %s", timeout, status.Output.Path)).
			WithEffect(toolfailure.EffectPartial).WithDetail(outputDetail(status.Output))
	case sandboxd.CommandEndStopped:
		return measured(status), sandboxd.NewToolFailure(toolfailure.CommandInterrupted,
			fmt.Errorf("the command was stopped before it finished; its output so far is in %s", status.Output.Path)).
			WithEffect(toolfailure.EffectPartial).WithDetail(outputDetail(status.Output))
	case sandboxd.CommandEndExit:
	}
	text := commandOutputText(status.Output)
	failed := status.Signal != "" || status.ExitCode == nil || *status.ExitCode != 0
	switch {
	case status.Signal != "":
		text += "\nterminated by signal: " + status.Signal
	case failed:
		text += fmt.Sprintf("\nexit code: %d", *status.ExitCode)
	}
	exitCode := -1
	if status.ExitCode != nil {
		exitCode = *status.ExitCode
	}
	var omitted int64
	if status.Output.Projection != nil {
		omitted = status.Output.Projection.OmittedBytes
	}
	result := measured(status)
	result.Content, result.KnownFailure, result.ExitCode, result.OmittedBytes = text, failed, &exitCode, &omitted
	return result, nil
}

// measured is a result with the ledger's times of the run, which a settled
// command records as its duration.
func measured(status sandboxd.CommandStatus) Result {
	var result Result
	if status.StartedAt != nil {
		result.StartedAt = *status.StartedAt
	}
	if status.EndedAt != nil {
		result.EndedAt = *status.EndedAt
	}
	return result
}

// interrupted is the failure of a command whose outcome the platform cannot
// tell: it may have run, in full or in part.
func interrupted(message string) error {
	return sandboxd.NewToolFailure(toolfailure.CommandInterrupted, errors.New(message)).WithEffect(toolfailure.EffectUnknown)
}

// commandOutputText is the merged output the model reads: the head, a marker
// that names the omitted bytes and the full output file, and the tail.
func commandOutputText(output sandboxd.CommandOutput) string {
	projection := output.Projection
	if projection == nil || projection.Head == "" && projection.Tail == "" {
		return "(no output)"
	}
	text := strings.TrimSuffix(projection.Head, "\n")
	if projection.OmittedBytes > 0 {
		text += fmt.Sprintf("\n[… %d bytes omitted · full output: %s]\n", projection.OmittedBytes, output.Path) + strings.TrimSuffix(projection.Tail, "\n")
	}
	return text
}

// outputDetail is the start and the end of a failed command's output for the
// failure's detail line; the message names the file with all of it.
func outputDetail(output sandboxd.CommandOutput) string {
	text := commandOutputText(output)
	if len(text) <= detailHeadBytes+detailTailBytes {
		return "output: " + text
	}
	return "output (start and end):\n" + comm.TruncateUTF8(text, detailHeadBytes) + "\n[…]\n" + comm.TailUTF8(text, detailTailBytes)
}

// commandID is the ledger ID of the call's command, which is also its task ID
// once it runs in the background.
func commandID(call Call) sandboxd.ExecutionID {
	return sandboxd.CommandTaskID(call.Identity)
}
