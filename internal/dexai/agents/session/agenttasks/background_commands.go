// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agenttasks

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// A background command is a command of the agent that runs on as a task in
// the Sandbox: started with bash run_in_background, or moved there when its
// foreground timeout passed. Its task ID is its command ledger ID,
// cmd-<12 characters> from the call's execution digest. The Session keeps one
// record per task in the AttributeMap BackgroundCommands under the task ID;
// the record is created with the tool result that started the task and,
// afterwards, written only when the task ends.

// The bounds of BackgroundCommands. The Sandbox runs at most four background
// commands; a record that ended stays readable until more than
// retainedEndedBackgroundCommands have ended.
const (
	maximumBackgroundCommands        = 64
	maximumRunningBackgroundCommands = 4
	retainedEndedBackgroundCommands  = 32
)

// BackgroundCommandStatus is the state of a background command.
type BackgroundCommandStatus string

const (
	BackgroundCommandRunning BackgroundCommandStatus = "running"
	// BackgroundCommandExited ended by itself or at a limit of the ledger.
	BackgroundCommandExited = BackgroundCommandStatus(conversation.TaskEndExited)
	// BackgroundCommandStopped was stopped by the user's Stop or by stop_task;
	// it sends no notification.
	BackgroundCommandStopped = BackgroundCommandStatus(conversation.TaskEndStopped)
	// BackgroundCommandLost ended without a recorded end, for example with its
	// Pod, or could not be followed.
	BackgroundCommandLost = BackgroundCommandStatus(conversation.TaskEndLost)
)

// validate rejects an unknown status.
func (status BackgroundCommandStatus) validate() error {
	switch status {
	case BackgroundCommandRunning, BackgroundCommandExited, BackgroundCommandStopped, BackgroundCommandLost:
		return nil
	default:
		return conversation.NewEnumValidationError("BackgroundCommandStatus", string(status))
	}
}

// UnmarshalJSON decodes and validates a status.
func (status *BackgroundCommandStatus) UnmarshalJSON(data []byte) error {
	return conversation.DecodeEnum(data, status, BackgroundCommandStatus.validate)
}

// BackgroundCommandEndReason is how an ended background command ended. Its
// spellings are persisted in the command's record, and LedgerEndReason maps
// the Sandbox's command ledger onto them.
type BackgroundCommandEndReason string

const (
	BackgroundCommandEndExit          BackgroundCommandEndReason = "exit"
	BackgroundCommandEndLifetimeLimit BackgroundCommandEndReason = "lifetime_limit"
	BackgroundCommandEndOutputLimit   BackgroundCommandEndReason = "output_limit"
	BackgroundCommandEndStopped       BackgroundCommandEndReason = "stopped"
	// BackgroundCommandEndLost ended without an end the ledger recorded.
	BackgroundCommandEndLost BackgroundCommandEndReason = "lost"
)

// LedgerEndReason is the end reason of a command whose run the ledger reports
// exited. The ledger's answer is not validated when it is decoded, so an end
// reason this server does not know is an error, never a record that could not
// be read back.
func LedgerEndReason(reason sandboxd.CommandEndReason) (BackgroundCommandEndReason, error) {
	switch reason {
	case sandboxd.CommandEndExit:
		return BackgroundCommandEndExit, nil
	case sandboxd.CommandEndLifetimeLimit:
		return BackgroundCommandEndLifetimeLimit, nil
	case sandboxd.CommandEndOutputLimit:
		return BackgroundCommandEndOutputLimit, nil
	case sandboxd.CommandEndStopped:
		return BackgroundCommandEndStopped, nil
	}
	return "", fmt.Errorf("the command ledger reported the unknown end reason %q", reason)
}

// validate rejects an unknown end reason.
func (reason BackgroundCommandEndReason) validate() error {
	switch reason {
	case BackgroundCommandEndExit, BackgroundCommandEndLifetimeLimit, BackgroundCommandEndOutputLimit, BackgroundCommandEndStopped, BackgroundCommandEndLost:
		return nil
	default:
		return conversation.NewEnumValidationError("BackgroundCommandEndReason", string(reason))
	}
}

// UnmarshalJSON decodes and validates an end reason.
func (reason *BackgroundCommandEndReason) UnmarshalJSON(data []byte) error {
	return conversation.DecodeEnum(data, reason, BackgroundCommandEndReason.validate)
}

// BackgroundCommand is the value of one BackgroundCommands instance, about 500
// bytes. It holds no command text or output: the description is what the user
// saw, and the output stays in the Sandbox's ledger.
type BackgroundCommand struct {
	TaskID string              `json:"task_id"`
	CallID conversation.CallID `json:"call_id"`
	// Description is the call's description, shown to the user.
	Description string `json:"description"`
	// WorkspaceRequestID is the Sandbox instance that runs the command.
	WorkspaceRequestID string                     `json:"workspace_request_id"`
	Status             BackgroundCommandStatus    `json:"status"`
	EndReason          BackgroundCommandEndReason `json:"end_reason,omitempty"`
	ExitCode           *int                       `json:"exit_code,omitempty"`
	OutputBytes        int64                      `json:"output_bytes,omitempty"`
	StartedAt          time.Time                  `json:"started_at"`
	// Deadline is the end of the command's lifetime in the ledger.
	Deadline time.Time `json:"deadline,omitzero"`
	EndedAt  time.Time `json:"ended_at,omitzero"`
}

// IsRunning reports whether the command has no recorded end.
func (command BackgroundCommand) IsRunning() bool {
	return command.Status == BackgroundCommandRunning
}

// BackgroundCommandEnd is how a watched command ended, as the Session
// observed it.
type BackgroundCommandEnd struct {
	EndReason   BackgroundCommandEndReason `json:"end_reason"`
	ExitCode    *int                       `json:"exit_code,omitempty"`
	OutputBytes int64                      `json:"output_bytes,omitempty"`
	EndedAt     time.Time                  `json:"ended_at"`
}

// End records the end of a running command: stopped for a stop, lost for a
// command without a recorded end, exited otherwise.
func (command BackgroundCommand) End(end BackgroundCommandEnd) BackgroundCommand {
	command.EndReason, command.ExitCode, command.OutputBytes, command.EndedAt = end.EndReason, end.ExitCode, end.OutputBytes, end.EndedAt
	switch end.EndReason {
	case BackgroundCommandEndStopped:
		command.Status = BackgroundCommandStopped
	case BackgroundCommandEndLost:
		command.Status = BackgroundCommandLost
	default:
		command.Status = BackgroundCommandExited
	}
	return command
}

// ExpiredBackgroundCommands are the records to delete before a new one is
// added to commands: the oldest ended ones beyond retainedEndedBackgroundCommands.
// Running records are never deleted.
func ExpiredBackgroundCommands(commands map[string]BackgroundCommand) []string {
	var ended []BackgroundCommand
	for _, command := range commands {
		if !command.IsRunning() {
			ended = append(ended, command)
		}
	}
	slices.SortFunc(ended, func(left, right BackgroundCommand) int { return left.EndedAt.Compare(right.EndedAt) })
	var expired []string
	for len(ended)-len(expired) > retainedEndedBackgroundCommands-1 {
		expired = append(expired, ended[len(expired)].TaskID)
	}
	return expired
}

// SortedBackgroundCommands lists the records in start order.
func SortedBackgroundCommands(commands map[string]BackgroundCommand) []BackgroundCommand {
	return slices.SortedFunc(maps.Values(commands), func(left, right BackgroundCommand) int {
		if order := left.StartedAt.Compare(right.StartedAt); order != 0 {
			return order
		}
		return strings.Compare(left.TaskID, right.TaskID)
	})
}

// RunningBackgroundCommands lists the running records in start order.
func RunningBackgroundCommands(commands map[string]BackgroundCommand) []BackgroundCommand {
	var running []BackgroundCommand
	for _, command := range SortedBackgroundCommands(commands) {
		if command.IsRunning() {
			running = append(running, command)
		}
	}
	return running
}

// CommandNotification is the task notification of an ended command; a
// stopped command sends none. Its ID repeats on every retry of its recording.
func CommandNotification(command BackgroundCommand) (conversation.SessionNotification, bool) {
	if command.Status == BackgroundCommandStopped || command.IsRunning() {
		return conversation.SessionNotification{}, false
	}
	return conversation.SessionNotification{
		ID: fmt.Sprintf("task/%s/%s", command.TaskID, command.Status), Kind: conversation.NotificationKindTask, Source: conversation.NotificationSourceCommand,
		OccurredAt: command.EndedAt, TaskID: command.TaskID, TaskStatus: string(command.Status),
		Summary: commandSummary(command), Report: commandReport(command),
	}, true
}

// maximumSummaryDescriptionBytes keeps a summary well inside a notification.
const maximumSummaryDescriptionBytes = 400

// commandSummary is the line the user sees of an ended command.
func commandSummary(command BackgroundCommand) string {
	description := comm.TruncateUTF8(command.Description, maximumSummaryDescriptionBytes)
	switch command.EndReason {
	case BackgroundCommandEndLost:
		return fmt.Sprintf("Background command %q was lost before it ended.", description)
	case BackgroundCommandEndLifetimeLimit:
		return fmt.Sprintf("Background command %q was stopped at the end of its lifetime.", description)
	case BackgroundCommandEndOutputLimit:
		return fmt.Sprintf("Background command %q was stopped after printing 16 MiB.", description)
	case BackgroundCommandEndStopped:
		return fmt.Sprintf("Background command %q was stopped.", description)
	case BackgroundCommandEndExit:
	}
	if command.ExitCode == nil {
		return fmt.Sprintf("Background command %q ended by a signal.", description)
	}
	return fmt.Sprintf("Background command %q exited with code %d.", description, *command.ExitCode)
}

// commandReport is what the model reads of an ended command: its end and the
// size of its output, which it reads with read_task_output.
func commandReport(command BackgroundCommand) string {
	exitCode := "none"
	if command.ExitCode != nil {
		exitCode = strconv.Itoa(*command.ExitCode)
	}
	return fmt.Sprintf("exit code: %s; end reason: %s; output: %d bytes, read with read_task_output", exitCode, command.EndReason, command.OutputBytes)
}
