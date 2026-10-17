// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxtools

import (
	"encoding/json"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Result carries one known Sandbox result; errors retain uncertain external effects.
type Result struct {
	Content string
	// Failure is set when the call failed with a known owner and effect; Content
	// is then its text for the model.
	Failure *conversation.ToolFailure
	// KnownFailure marks a result that reports a failure of its own, such as a
	// command that exited with a nonzero code.
	KnownFailure bool
	// Pending marks a command that still runs; the caller asks again.
	Pending    bool
	HeadCommit string
	// DefinedSteps are the Steps of the Flows the commit gate's definitions
	// stage rendered of the tree a pushed commit_and_push committed; empty
	// for a blocked call and when the gate skipped that stage.
	DefinedSteps []git.DefinedStep
	// ExitCode and OmittedBytes measure a command result: its exit code, -1 for
	// a command a signal ended, and the output bytes its projection omitted.
	ExitCode     *int
	OmittedBytes *int64
	// StartedAt and EndedAt are the command ledger's times of a command: its
	// start, and its end once it ended; zero when the ledger has none. A typed
	// failure of the command keeps them.
	StartedAt, EndedAt time.Time
	// Background is set when the call's command runs as a background task,
	// started so or moved there after its timeout; the Session records and
	// watches the task.
	Background *BackgroundStart
	// Task is the ledger's state of a command task, for the Session that
	// watches it (CommandTaskStatus).
	Task *TaskState
	// Worktree is the worktree CreateAgentWorktree created for a subagent,
	// Finished the result FinishAgentWorktree saved, and Merge the receipt of
	// MergeAgentChanges, which the Session records.
	Worktree *git.AgentWorktree
	Finished *git.AgentResult
	Merge    *git.MergeReceipt
	// Contribution is the worktree of the connectors library that
	// CreateContributionWorktree created for a develop subagent, and
	// Contributed what FinishContribution committed and pushed.
	Contribution *git.ContributionWorktree
	Contributed  *git.ContributionResult
}

// BackgroundStart is a command that runs in the background as a task. Its
// field names are persisted keys of tool results.
type BackgroundStart struct {
	TaskID string `json:"task_id"`
	// Description is the call's description of the command, for the user.
	Description string    `json:"description"`
	StartedAt   time.Time `json:"started_at,omitzero"`
	// Deadline is the end of the command's lifetime, after which the ledger
	// ends it; zero when the ledger named none.
	Deadline time.Time `json:"deadline,omitzero"`
}

// TaskState is the ledger's state of a command task: running, exited or lost.
type TaskState struct {
	Running bool
	// Lost marks a command whose supervisor ended without recording its end,
	// or that the Sandbox instance no longer knows.
	Lost bool
	// EndReason is how an exited command ended.
	EndReason   sandboxd.CommandEndReason
	ExitCode    *int
	OutputBytes int64
	EndedAt     time.Time
}

// jsonResult encodes a handler payload. Payloads hold only plain values, so
// encoding fails only through a programming error.
func jsonResult(payload any, knownFailure bool) Result {
	return Result{Content: string(comm.Must(json.Marshal(payload))), KnownFailure: knownFailure}
}

// failureResult is the result of a call that failed with a known owner and
// effect.
func failureResult(failure conversation.ToolFailure) Result {
	return Result{Content: failure.Text(), Failure: &failure}
}
