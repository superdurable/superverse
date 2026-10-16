// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package backgroundcommand

import (
	"context"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
)

// watchSlack is how much longer than its lifetime a background command is
// watched, so that the ledger's own end is observed.
const watchSlack = 5 * time.Minute

// The ledger is asked every second; one status request waits up to
// CommandStatusWait for the end.
const (
	pollInterval = time.Second
	pollRound    = sandboxtools.CommandStatusWait + 10*time.Second
)

// BackgroundCommandWatch is the input of BackgroundCommandFlow, which the
// Session's AwaitBackgroundCommand Step passes: the task, the Session whose
// ledger identity it has and the accepted lease of the instance that runs it.
// The lease names provider identities only and holds no secret.
type BackgroundCommandWatch struct {
	TaskID        string          `json:"task_id"`
	SessionFlowID string          `json:"session_flow_id"`
	Lease         workspace.Lease `json:"lease"`
}

// watchCommand polls the command ledger of the watched instance until the
// command ends. A task the instance does not know, such as on a replacement
// instance, is lost. An unreachable instance is an error for a new attempt;
// once the attempts run out, RecordBackgroundCommandLost records it lost. It
// reads no Flow.
func (flow *BackgroundCommandFlow) watchCommand(ctx dex.Context, watch BackgroundCommandWatch) (agenttasks.BackgroundCommandEnd, error) {
	call := sandboxtools.Call{Tool: string(conversation.ToolStopTask), Arguments: sandboxtools.TaskArguments(watch.TaskID), Lease: &watch.Lease, Root: sandboxd.WorkspaceRoot,
		Identity: sandboxd.InvocationIdentity{FlowID: watch.SessionFlowID, CallID: watch.TaskID}, Progress: func(string) {}}
	var state *sandboxtools.TaskState
	err := dexflow.PollUntil(ctx, pollInterval, pollRound, func(round context.Context) (bool, error) {
		result, err := flow.sandbox.Run(round, sandboxtools.CommandTaskStatus, call)
		switch {
		case err != nil:
			return false, fmt.Errorf("read background command %s: %w", watch.TaskID, err)
		case result.Failure != nil:
			return false, fmt.Errorf("read background command %s: %s", watch.TaskID, result.Failure.Text())
		}
		state = result.Task
		return !state.Running, nil
	})
	if err != nil {
		return agenttasks.BackgroundCommandEnd{}, err
	}
	end := agenttasks.BackgroundCommandEnd{EndReason: agenttasks.BackgroundCommandEndLost, ExitCode: state.ExitCode, OutputBytes: state.OutputBytes, EndedAt: state.EndedAt}
	if !state.Lost {
		if end.EndReason, err = agenttasks.LedgerEndReason(state.EndReason); err != nil {
			return agenttasks.BackgroundCommandEnd{}, dex.ErrorWithStack(fmt.Errorf("read background command %s: %w", watch.TaskID, err))
		}
	}
	if end.EndedAt.IsZero() {
		end.EndedAt = time.Now().UTC()
	}
	return end, nil
}

// WatchedEnd is, in the Execute of the Step that waited for the
// BackgroundCommandFlow, how the command ended: its completion output, or lost
// when the SubFlow ended otherwise, for example stopped from Dex Web, or the
// Step's WaitFor failed.
func WatchedEnd(ctx dex.Context) agenttasks.BackgroundCommandEnd {
	lost := agenttasks.BackgroundCommandEnd{EndReason: agenttasks.BackgroundCommandEndLost, EndedAt: time.Now().UTC()}
	if ctx.WaitForMethodFailed() {
		return lost
	}
	result := comm.Must(dex.SubFlowResult(ctx))
	if result.Status != dex.FlowCompleted {
		return lost
	}
	var end agenttasks.BackgroundCommandEnd
	comm.MustDo(result.DecodeSingleOutput(&end))
	return end
}
