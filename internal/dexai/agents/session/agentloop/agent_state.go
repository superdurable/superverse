// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"slices"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Every Session shares these bounds; they are not part of its configuration.
const (
	// messageRetentionLimit bounds the retained entries before the current
	// segment. It is a multiple of the archive chunk size.
	messageRetentionLimit = 1_000
	// maximumParallelToolCalls bounds one read-only tool wave.
	// conversation/prompts/v1/session.md states it and must change with it.
	maximumParallelToolCalls = 4
)

// StartRequest is an accepted Session start; Sandbox owns repository identity.
type StartRequest struct {
	// SessionStartIdentity fingerprints trusted embedding input independently of mutable server defaults or repository HEAD.
	SessionStartIdentity string          `json:"session_start_identity,omitempty"`
	Session              SessionIdentity `json:"session"`
	// Model is the model the Project's creator chose, or the server's default
	// when the Session started; the first accepted start keeps it on an exact retry.
	Model conversation.Model `json:"model"`
	// FirstMessage is the creator's first message, which the start Step admits
	// once into QueuedUserMessages; nil without one.
	FirstMessage *conversation.PendingUserMessage `json:"first_message,omitempty"`
}

// SessionStartInput is the input of the Session's start Step: the first
// message it admits, or nil. A Session started before it existed has none.
type SessionStartInput struct {
	FirstMessage *conversation.PendingUserMessage `json:"first_message,omitempty"`
}

// AgentStatus describes the active durable state-machine boundary.
type AgentStatus string

const (
	AgentStatusInitializing           AgentStatus = "initializing"
	AgentStatusWaitingForMessage      AgentStatus = "waiting_for_message"
	AgentStatusCompactingContext      AgentStatus = "compacting_context"
	AgentStatusCallingModel           AgentStatus = "calling_model"
	AgentStatusRoutingTool            AgentStatus = "routing_tool"
	AgentStatusWaitingForToolApproval AgentStatus = "waiting_for_tool_approval"
	AgentStatusExecutingTool          AgentStatus = "executing_tool"
	AgentStatusWaitingForTimer        AgentStatus = "waiting_for_timer"
)

// validate rejects unknown Agent statuses.
func (status AgentStatus) validate() error {
	switch status {
	case AgentStatusInitializing,
		AgentStatusWaitingForMessage,
		AgentStatusCompactingContext,
		AgentStatusCallingModel,
		AgentStatusRoutingTool,
		AgentStatusWaitingForToolApproval,
		AgentStatusExecutingTool,
		AgentStatusWaitingForTimer:
		return nil
	default:
		return conversation.NewEnumValidationError("AgentStatus", string(status))
	}
}

// UnmarshalJSON decodes and validates an Agent status.
func (status *AgentStatus) UnmarshalJSON(data []byte) error {
	return conversation.DecodeEnum(data, status, AgentStatus.validate)
}

// WaitingInputRound is the monotonic browser reconciliation watermark.
type WaitingInputRound int64

// maximumWaitingInputRound is the largest round exactly representable by JavaScript.
const maximumWaitingInputRound WaitingInputRound = 9_007_199_254_740_991

// NextWaitingInputRound advances the watermark by one durable wait. It starts
// at zero, so the JavaScript limit is unreachable in practice.
func NextWaitingInputRound(round WaitingInputRound) WaitingInputRound {
	if round >= maximumWaitingInputRound {
		panic("waiting input round reached the JavaScript safe integer limit")
	}
	return round + 1
}

// AgentState records state-machine and message-sequence progress.
type AgentState struct {
	FirstRetainedSequence conversation.Sequence   `json:"first_retained_sequence"`
	CurrentFirstSequence  conversation.Sequence   `json:"current_first_sequence"`
	LastSequence          conversation.Sequence   `json:"last_sequence"`
	Status                AgentStatus             `json:"status"`
	PendingToolCalls      []conversation.ToolCall `json:"pending_tool_calls"`
	PendingToolIndex      int                     `json:"pending_tool_index"`
	PendingToolsAdmission remoteexec.Fence        `json:"pending_tools_admission"`
	// Turn counts the turns; each turn start increments it.
	Turn int64 `json:"turn,omitempty"`
	// TurnRequestSequences are the user messages the current turn works on:
	// the message that started it and the answers given since. A new
	// segment quotes them as the pending request.
	TurnRequestSequences []conversation.Sequence `json:"turn_request_sequences,omitempty"`
	// SourceChangedSinceHandoff records a source change of the current turn
	// after its last unblocked commit_and_push.
	SourceChangedSinceHandoff bool `json:"source_changed_since_handoff,omitempty"`
	// HandoffContinuations counts automatic continuations of the current turn
	// after it stopped with changed source and no unblocked commit.
	HandoffContinuations int `json:"handoff_continuations,omitempty"`
	// Running is the interruptible work in flight: the Step that schedules
	// it sets it, and the Step that ends it clears it in its own commit, so
	// InterruptAgent never cancels work that has already committed.
	Running *RunningWork `json:"running,omitempty"`
	// AutomaticTaskTurns counts the consecutive turns that task notifications
	// opened, and AutomaticPlatformTurns those that application-owned delivery
	// failures of HEAD opened; AutomaticTurnsPaused stops both, after Stop or
	// at a bound, until the user starts a turn.
	AutomaticTaskTurns     int  `json:"automatic_task_turns,omitempty"`
	AutomaticPlatformTurns int  `json:"automatic_platform_turns,omitempty"`
	AutomaticTurnsPaused   bool `json:"automatic_turns_paused,omitempty"`
	// SubagentsStartedWithoutUser counts the subagents admitted since the
	// user last started a turn; start_agent admits at most
	// agenttasks.MaximumSubagentsWithoutUser.
	SubagentsStartedWithoutUser int `json:"subagents_started_without_user,omitempty"`
	// ConnectorSlugs are the distinct connectors that admitted
	// connector_develop calls named, at most
	// agenttasks.MaximumConnectorContributions, kept for the Session's life;
	// the contributions themselves are ConnectorContributionFlows.
	ConnectorSlugs []string `json:"connector_slugs,omitempty"`
}

// ConnectorContributionAttempt is the Session's remote worktree attempt. The
// ConnectorContributionFlow owns the submitted branch, pull request and
// release candidate; this record only gates tools and recovers their effects.
type ConnectorContributionAttempt struct {
	Contribution conversation.ContributionKind       `json:"contribution"`
	WorktreePath string                              `json:"worktree_path"`
	WorktreeID   string                              `json:"worktree_id"`
	BaseCommit   string                              `json:"base_commit,omitempty"`
	Status       ConnectorContributionAttemptStatus  `json:"status"`
	StartCallID  conversation.CallID                 `json:"start_call_id"`
	Attempt      uint64                              `json:"attempt"`
	SubmitCallID conversation.CallID                 `json:"submit_call_id,omitempty"`
	Summary      string                              `json:"summary,omitempty"`
	Result       *conversation.ConnectorContribution `json:"result,omitempty"`
	SubmittedAt  *time.Time                          `json:"submitted_at,omitempty"`
}

// ConnectorContributionAttemptStatus is the closed state of a remote
// contribution worktree, separate from the contribution Flow's PR status.
type ConnectorContributionAttemptStatus string

const (
	ConnectorContributionOpen      ConnectorContributionAttemptStatus = "OPEN"
	ConnectorContributionSubmitted ConnectorContributionAttemptStatus = "SUBMITTED"
	ConnectorContributionFailed    ConnectorContributionAttemptStatus = "FAILED"
)

// TaskWorktree is one Codex-owned checkout of the Session workspace. The
// Session owns admission and the Git helper owns the snapshots and receipts.
type TaskWorktree struct {
	Name         string              `json:"name"`
	WorktreeID   string              `json:"worktree_id"`
	WorktreePath string              `json:"worktree_path"`
	BaseCommit   string              `json:"base_commit,omitempty"`
	Attempt      uint64              `json:"attempt"`
	CleanupID    string              `json:"cleanup_id,omitempty"`
	Status       TaskWorktreeStatus  `json:"status"`
	OpenCallID   conversation.CallID `json:"open_call_id"`
	MergeCallID  conversation.CallID `json:"merge_call_id,omitempty"`
	Receipt      *git.MergeReceipt   `json:"receipt,omitempty"`
	ClosedAt     *time.Time          `json:"closed_at,omitempty"`
}

// TaskWorktreeStatus is the checkout and merge state, independent of a turn.
type TaskWorktreeStatus string

const (
	TaskWorktreeOpen      TaskWorktreeStatus = "OPEN"
	TaskWorktreeSaved     TaskWorktreeStatus = "SAVED"
	TaskWorktreeMerged    TaskWorktreeStatus = "MERGED"
	TaskWorktreeDiscarded TaskWorktreeStatus = "DISCARDED"
)

const (
	MaximumOpenTaskWorktrees = 3
	maximumTaskWorktreeNames = 16
)

// AdmitTaskWorktree selects one stable attempt and, when full, the oldest
// closed record to evict. The new attempt's CleanupID keeps that old ref
// reachable until ExecuteTool deletes it, so the map and refs stay bounded.
// CallID participates in the derived ID because an evicted name has no
// retained attempt counter; the attempt field is informational while present.
func AdmitTaskWorktree(records map[string]TaskWorktree, flowID, name string, callID conversation.CallID) (TaskWorktree, string, bool) {
	current, exists := records[name]
	if exists && (current.Status == TaskWorktreeOpen || current.Status == TaskWorktreeSaved || current.OpenCallID == callID) {
		return current, "", true
	}
	open := 0
	for _, record := range records {
		if record.Status == TaskWorktreeOpen {
			open++
		}
	}
	if open >= MaximumOpenTaskWorktrees {
		return TaskWorktree{}, "", false
	}
	var evicted, cleanupID string
	if exists {
		cleanupID = current.WorktreeID
	} else if len(records) >= maximumTaskWorktreeNames {
		closed := make([]TaskWorktree, 0, len(records))
		for _, record := range records {
			if record.Status == TaskWorktreeMerged || record.Status == TaskWorktreeDiscarded {
				closed = append(closed, record)
			}
		}
		if len(closed) == 0 {
			return TaskWorktree{}, "", false
		}
		slices.SortFunc(closed, func(left, right TaskWorktree) int {
			return left.ClosedAt.Compare(*right.ClosedAt)
		})
		evicted, cleanupID = closed[0].Name, closed[0].WorktreeID
	}
	attempt := current.Attempt + 1
	if !exists {
		attempt = 1
	}
	id := flowid.AgentTaskID(flowID, "task-worktree:"+name+":"+string(callID))
	return TaskWorktree{Name: name, WorktreeID: id, WorktreePath: git.AgentWorktreeRoot(id), Attempt: attempt,
		CleanupID: cleanupID, Status: TaskWorktreeOpen, OpenCallID: callID}, evicted, true
}

// RunningKind is the kind of work a turn has in flight.
type RunningKind string

const (
	// RunningModel is a model call or a compaction.
	RunningModel RunningKind = "model"
	// RunningTool is a serial tool call.
	RunningTool RunningKind = "tool"
	// RunningWave is a parallel tool wave.
	RunningWave RunningKind = "wave"
	// RunningWait is a durable timer or a tool approval the turn waits for.
	RunningWait RunningKind = "wait"
	// RunningRemote is a remote turn waiting for public reports.
	RunningRemote RunningKind = "remote"
)

// validate rejects an unknown kind.
func (kind RunningKind) validate() error {
	switch kind {
	case RunningModel, RunningTool, RunningWave, RunningWait, RunningRemote:
		return nil
	default:
		return conversation.NewEnumValidationError("RunningKind", string(kind))
	}
}

// UnmarshalJSON decodes and validates a kind.
func (kind *RunningKind) UnmarshalJSON(data []byte) error {
	return conversation.DecodeEnum(data, kind, RunningKind.validate)
}

// RunningWork names the work in flight: the tool of a serial call, and the
// result instance of a wave and its number of calls from the cursor.
type RunningWork struct {
	Kind      RunningKind           `json:"kind"`
	ToolName  conversation.ToolName `json:"tool_name,omitempty"`
	BatchID   string                `json:"batch_id,omitempty"`
	BatchSize int                   `json:"batch_size,omitempty"`
}

// interruptible reports whether Stop cancels the work at once. A tool that
// must finish its own effect keeps running, and the turn ends at the next
// boundary.
func (work RunningWork) interruptible() bool {
	return work.Kind != RunningTool || !CatalogTool(work.ToolName).uninterruptible
}

// NewAgentState returns the state a Session starts with.
func NewAgentState() AgentState {
	return AgentState{
		FirstRetainedSequence: 1,
		CurrentFirstSequence:  1,
		Status:                AgentStatusInitializing,
		PendingToolCalls:      []conversation.ToolCall{},
	}
}

// Continuation names where the conversation goes after a Step boundary.
type Continuation string

const (
	ContinueAwaitUser   Continuation = "await_user"
	ContinueAwaitRemote Continuation = "await_remote"
	// ContinueStartTurn is the model call that starts a turn: a rollover is
	// decided before it.
	ContinueStartTurn         Continuation = "start_turn"
	ContinueCallModel         Continuation = "call_model"
	ContinueCompactContext    Continuation = "compact_context"
	ContinueRouteTool         Continuation = "route_tool"
	ContinueAwaitToolApproval Continuation = "await_tool_approval"
	ContinueExecuteTool       Continuation = "execute_tool"
	ContinueAwaitTimer        Continuation = "await_timer"
)

// AgentDescription is the durable application state needed to render a conversation.
type AgentDescription struct {
	Executor                   remoteexec.Executor `json:"executor"`
	ModelChangedBy             *conversation.Actor `json:"model_changed_by,omitempty"`
	PreviewAutoDeployChangedBy *conversation.Actor `json:"preview_auto_deploy_changed_by,omitempty"`
	Status                     AgentStatus         `json:"status"`
	WaitingInputRound          WaitingInputRound   `json:"waiting_input_round"`
	// Model and ModelRevision are the user's selection; ContextModel and
	// Effort are what the current segment runs on, empty before it opens.
	Model                 conversation.Model     `json:"model"`
	ModelRevision         uint64                 `json:"model_revision"`
	ContextModel          conversation.Model     `json:"context_model,omitempty"`
	Effort                conversation.Effort    `json:"effort,omitempty"`
	FirstRetainedSequence conversation.Sequence  `json:"first_retained_sequence"`
	LastSequence          conversation.Sequence  `json:"last_sequence"`
	PendingApproval       *PendingApproval       `json:"pending_approval,omitempty"`
	PendingTimer          *PendingTimer          `json:"pending_timer,omitempty"`
	PendingUserInput      *PendingUserInput      `json:"pending_user_input,omitempty"`
	TodoList              *TodoList              `json:"todo_list,omitempty"`
	ConfirmedRequirements *ConfirmedRequirements `json:"confirmed_requirements,omitempty"`
	// ConfirmedDesignBuilt says whether a pushed, unblocked commit_and_push
	// built the Flow design the confirmed requirements carry.
	ConfirmedDesignBuilt bool `json:"confirmed_design_built,omitempty"`
	// ConfirmedDesignBuiltCommit is the commit that push verified, set only
	// while ConfirmedDesignBuilt.
	ConfirmedDesignBuiltCommit string `json:"confirmed_design_built_commit,omitempty"`
	PendingQueuedMessageCount  int    `json:"pending_queued_message_count"`
	PendingSteeredMessageCount int    `json:"pending_steered_message_count"`

	AvailableTools []conversation.ToolName `json:"available_tools"`
	// Turn is the current turn, which a Stop names.
	Turn int64 `json:"turn"`
	// BackgroundCommands are the task records of the Session's background
	// commands, in start order.
	BackgroundCommands []agenttasks.BackgroundCommand `json:"background_commands"`
	// Subagents are the records of the Session's subagents, in start order.
	Subagents []agenttasks.SubagentRecord `json:"subagents"`
	// MockProgress is where the newest mock reply left its scenario; nil
	// before one.
	MockProgress *conversation.MockProgress `json:"mock_progress,omitempty"`
	// IsPreviewAutoDeployEnabled is whether the Preview deploys each pushed
	// commit after its turn.
	IsPreviewAutoDeployEnabled bool `json:"is_preview_auto_deploy_enabled"`
	// ConnectorSlugs is the union of platform and remote connector slugs whose
	// ConnectorContributionFlows the Snapshot reads.
	ConnectorSlugs []string `json:"connector_slugs,omitempty"`
}

// NewestMockProgress is the progress of the newest mock reply of a history
// page, or nil. The Snapshot's window always holds the newest reply.
func NewestMockProgress(history conversation.HistoryPage) *conversation.MockProgress {
	for index := len(history.Messages) - 1; index >= 0; index-- {
		if progress := history.Messages[index].Message.MockProgress; progress != nil {
			return progress
		}
	}
	return nil
}

// AgentSnapshot is one atomic durable application view.
type AgentSnapshot struct {
	History     conversation.HistoryPage          `json:"history"`
	Description AgentDescription                  `json:"description"`
	Queued      []conversation.PendingUserMessage `json:"queued"`
	Steered     []conversation.PendingUserMessage `json:"steered"`
}

// PendingTimer describes a durable model-requested wait.
type PendingTimer struct {
	CallID          conversation.CallID `json:"call_id"`
	DurationSeconds int64               `json:"duration_seconds"`
	Reason          string              `json:"reason"`
	StartedAt       *time.Time          `json:"started_at,omitempty"`
}
