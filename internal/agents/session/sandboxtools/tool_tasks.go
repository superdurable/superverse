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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
)

// The task tools follow the agent's background commands through the command
// ledger alone: a task ID is its command's ledger ID, so no Flow is read.
// read_task_output pages a task's output and stop_task stops it. The Session
// watches a task with CommandTaskStatus and stops the running ones with
// StopTask when the user presses Stop.

// taskOutputBytes bounds the output one read_task_output call returns.
const taskOutputBytes = 30_000

// TaskArguments are the arguments of a task tool for taskID, with which the
// Session watches and stops a task it recorded.
func TaskArguments(taskID string) string {
	return string(comm.Must(json.Marshal(struct {
		TaskID string `json:"task_id"`
	}{taskID})))
}

// readTaskOutput answers a task's state and its output from the offset.
func (execution *Execution) readTaskOutput(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.ReadTaskOutput.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	taskID, err := commandTask(input.TaskID)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, ledgerRequestTimeout)
	defer cancel()
	view := sandboxd.CommandOutputView{From: int64(input.Offset.Value), Max: taskOutputBytes}
	status, err := execution.executor.CommandLedger().Status(ctx, taskID, 0, view)
	switch {
	case err != nil:
		return Result{}, err
	case status.State == sandboxd.CommandNotFound:
		return Result{}, taskNotFound(input.TaskID)
	}
	return Result{Content: taskOutputText(taskID, status, view.From)}, nil
}

// stopTask stops a task's command, idempotently: SIGTERM to its process group
// and SIGKILL after the grace period. A task that already ended answers its
// state.
func (execution *Execution) stopTask(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.StopTask.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	taskID, err := commandTask(input.TaskID)
	if err != nil {
		return Result{}, err
	}
	status, err := execution.stopRun(ctx, taskID, sandboxd.CommandOutputView{})
	switch {
	case err != nil:
		return Result{}, err
	case status.State == sandboxd.CommandNotFound:
		return Result{}, taskNotFound(input.TaskID)
	}
	return Result{Content: fmt.Sprintf("Task %s: %s.", taskID, taskStateText(status))}, nil
}

// commandTaskStatus waits up to CommandStatusWait for the end of a task and
// answers its state; a task the instance does not know is lost. The caller
// bounds the request.
func (execution *Execution) commandTaskStatus(ctx context.Context, call Call) (Result, error) {
	input, err := toolinput.StopTask.Decode(call.Arguments)
	if err != nil {
		return Result{}, invalidArguments(err)
	}
	taskID, err := commandTask(input.TaskID)
	if err != nil {
		return Result{}, err
	}
	status, err := execution.executor.CommandLedger().Status(ctx, taskID, CommandStatusWait, sandboxd.CommandOutputView{})
	if err != nil {
		return Result{}, err
	}
	state := &TaskState{Running: status.State == sandboxd.CommandRunning, Lost: status.State == sandboxd.CommandLost || status.State == sandboxd.CommandNotFound,
		EndReason: status.EndReason, ExitCode: status.ExitCode, OutputBytes: status.Output.Bytes}
	if status.EndedAt != nil {
		state.EndedAt = *status.EndedAt
	}
	return Result{Task: state}, nil
}

// commandTask is the ledger ID of a command task ID. Any other ID names no
// task of this Sandbox.
func commandTask(taskID string) (sandboxd.ExecutionID, error) {
	taskID = strings.TrimSpace(taskID)
	if !sandboxd.IsCommandTaskID(taskID) {
		return "", taskNotFound(taskID)
	}
	return sandboxd.ExecutionID(taskID), nil
}

func taskNotFound(taskID string) error {
	return sandboxd.NewToolFailure(toolfailure.TaskNotFound, fmt.Errorf("no task %q is known in this Sandbox; a background command's task ID looks like cmd-qjo5x7m2k4ab", taskID))
}

// taskOutputText is a task's state, the window of its output from offset and
// where to read on.
func taskOutputText(taskID sandboxd.ExecutionID, status sandboxd.CommandStatus, offset int64) string {
	text := fmt.Sprintf("Task %s: %s.\n", taskID, taskStateText(status))
	window, total := status.Output.Window, status.Output.Bytes
	switch {
	case window == nil || window.Next <= window.From:
		return text + fmt.Sprintf("No output from offset %d; the output has %d bytes so far.", offset, total)
	case window.Next < total:
		text += fmt.Sprintf("Output bytes %d to %d of %d; read on with offset %d.", window.From, window.Next, total, window.Next)
	case status.State == sandboxd.CommandRunning:
		text += fmt.Sprintf("Output bytes %d to %d, all it has printed so far; read on with offset %d.", window.From, window.Next, window.Next)
	default:
		text += fmt.Sprintf("Output bytes %d to %d, the end of the output.", window.From, window.Next)
	}
	return text + "\n" + window.Data
}

// taskStateText describes a task's state in a few words.
func taskStateText(status sandboxd.CommandStatus) string {
	switch status.State {
	case sandboxd.CommandRunning:
		if status.StartedAt == nil {
			return "running"
		}
		return "running since " + status.StartedAt.UTC().Format(time.RFC3339)
	case sandboxd.CommandLost:
		return "lost: its supervisor ended without recording the end, for example because the Sandbox restarted"
	case sandboxd.CommandNotFound, sandboxd.CommandExited:
	}
	switch status.EndReason {
	case sandboxd.CommandEndLifetimeLimit:
		return "stopped at the end of its lifetime"
	case sandboxd.CommandEndOutputLimit:
		return "stopped after printing 16 MiB"
	case sandboxd.CommandEndStopped:
		return "stopped"
	case sandboxd.CommandEndExit:
	}
	switch {
	case status.Signal != "":
		return "ended by signal " + status.Signal
	case status.ExitCode != nil:
		return fmt.Sprintf("exited with code %d", *status.ExitCode)
	default:
		return "exited"
	}
}
