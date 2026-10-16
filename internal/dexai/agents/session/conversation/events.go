// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package conversation

// EventKind identifies one structured activity event.
type EventKind string

const (
	EventKindInputConsumed         EventKind = "input_consumed"
	EventKindUserInputAnswered     EventKind = "user_input_answered"
	EventKindUserInputCancelled    EventKind = "user_input_cancelled"
	EventKindSnapshotRequired      EventKind = "snapshot_required"
	EventKindSteeringApplied       EventKind = "steering_applied"
	EventKindCompactionFailed      EventKind = "compaction_failed"
	EventKindCompacted             EventKind = "compacted"
	EventKindModelStarted          EventKind = "model_started"
	EventKindModelFailed           EventKind = "model_failed"
	EventKindModelCompleted        EventKind = "model_completed"
	EventKindModelToolCall         EventKind = "model_tool_call"
	EventKindModelFallback         EventKind = "model_fallback"
	EventKindUserInputRequested    EventKind = "user_input_requested"
	EventKindToolApprovalRequested EventKind = "tool_approval_requested"
	EventKindToolApprovalResolved  EventKind = "tool_approval_resolved"
	EventKindTimerStarted          EventKind = "timer_started"
	EventKindTimerResolved         EventKind = "timer_resolved"
	EventKindToolProgress          EventKind = "tool_progress"
	EventKindToolFailed            EventKind = "tool_failed"
	EventKindToolCompleted         EventKind = "tool_completed"
	// EventKindAgentInterrupted reports that the user's Stop ended the turn
	// or stopped the running tasks; text from earlier model calls is stale.
	EventKindAgentInterrupted EventKind = "agent_interrupted"
)

// validate rejects unknown event kinds.
func (kind EventKind) validate() error {
	switch kind {
	case EventKindInputConsumed,
		EventKindUserInputAnswered,
		EventKindUserInputCancelled,
		EventKindSnapshotRequired,
		EventKindSteeringApplied,
		EventKindCompactionFailed,
		EventKindCompacted,
		EventKindModelStarted,
		EventKindModelFailed,
		EventKindModelCompleted,
		EventKindModelToolCall,
		EventKindModelFallback,
		EventKindUserInputRequested,
		EventKindToolApprovalRequested,
		EventKindToolApprovalResolved,
		EventKindTimerStarted,
		EventKindTimerResolved,
		EventKindToolProgress,
		EventKindToolFailed,
		EventKindToolCompleted,
		EventKindAgentInterrupted:
		return nil
	default:
		return NewEnumValidationError("EventKind", string(kind))
	}
}

// UnmarshalJSON decodes and validates an event kind.
func (kind *EventKind) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, kind, EventKind.validate)
}

// AgentEvent is emitted to the best-effort activity Stream.
type AgentEvent struct {
	Kind     EventKind `json:"kind"`
	Message  string    `json:"message"`
	CallID   *CallID   `json:"call_id,omitempty"`
	ToolName *ToolName `json:"tool_name,omitempty"`
	// Target says what a tool call works on, at most 200 characters.
	Target           string            `json:"target,omitempty"`
	MessageSequence  *Sequence         `json:"message_sequence,omitempty"`
	InputConsumption *InputConsumption `json:"input_consumption,omitempty"`
	Attempt          *int32            `json:"attempt,omitempty"`
}

// InputConsumption identifies exact durable inputs consumed at a Step boundary.
type InputConsumption struct {
	QueuedMessageIDs  []MessageID `json:"queued_message_ids"`
	SteeredMessageIDs []MessageID `json:"steered_message_ids"`
}

// TextWriter receives one provider text delta.
type TextWriter func(string) error

// ActivityWriter receives one structured lifecycle event.
type ActivityWriter func(AgentEvent) error

// ToolInputWriter receives the input of a tool call the model is still
// writing, which it streams for a requirements card.
type ToolInputWriter func(ToolInputProgress)

// ToolInputProgress is the input a streamed tool call holds so far: the JSON
// text written up to now, an object the model may have cut anywhere, and
// whether its block is complete.
type ToolInputProgress struct {
	CallID   CallID
	Name     ToolName
	Input    string
	Complete bool
}

// TaskStatus describes one item of the Agent's todo list.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
)

// validate rejects unknown task statuses.
func (status TaskStatus) validate() error {
	switch status {
	case TaskStatusPending, TaskStatusInProgress, TaskStatusCompleted:
		return nil
	default:
		return NewEnumValidationError("TaskStatus", string(status))
	}
}

// UnmarshalJSON decodes and validates a task status.
func (status *TaskStatus) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, status, TaskStatus.validate)
}
