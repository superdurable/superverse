// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package conversation

// ToolName identifies one provider-visible tool.
type ToolName string

// Sandbox tools run over the Session's accepted workspace lease.
const (
	ToolBash             ToolName = "bash"
	ToolRead             ToolName = "read"
	ToolEdit             ToolName = "edit"
	ToolWrite            ToolName = "write"
	ToolGrep             ToolName = "grep"
	ToolGlob             ToolName = "glob"
	ToolReadTaskOutput   ToolName = "read_task_output"
	ToolStopTask         ToolName = "stop_task"
	ToolConnectorCatalog ToolName = "connector_catalog"
	ToolCommitAndPush    ToolName = "commit_and_push"
)

// Delegation tools start the Session's subagents and merge what they changed
// in their worktrees into the workspace.
const (
	ToolStartAgent        ToolName = "start_agent"
	ToolMergeAgentChanges ToolName = "merge_agent_changes"
)

// Platform tools read the Session's own Project on the server, without the
// workspace.
const (
	ToolReadDeliveryStatus ToolName = "read_delivery_status"
	ToolReadPlatformFlow   ToolName = "read_platform_flow"
	ToolReadLiveAppRuns    ToolName = "read_live_app_runs"
)

// Built-in tools are handled by Steps of SessionAgentFlow.
const (
	// ToolWriteTodos replaces the Agent's progress list.
	ToolWriteTodos ToolName = "write_todos"
	// ToolStartTimer creates a Dex Timer wait.
	ToolStartTimer ToolName = "start_timer"
	// ToolRequestUserInput creates a durable user question.
	ToolRequestUserInput ToolName = "request_user_input"
	// ToolConfirmRequirements shows the requirements card the user confirms
	// before the Agent builds.
	ToolConfirmRequirements ToolName = "confirm_requirements"
	// ToolSetEffort raises the effort of the model calls until the next user
	// turn.
	ToolSetEffort ToolName = "set_effort"
)

// ToolSpec is what the model provider sees of one tool.
type ToolSpec struct {
	Name        ToolName
	Description string
	InputSchema JSONObject
}

// CallID uniquely identifies one model-requested tool call.
type CallID string

// ToolCall is one model-requested function call.
type ToolCall struct {
	ID        CallID     `json:"id"`
	Name      ToolName   `json:"name"`
	Arguments JSONObject `json:"arguments"`
}

// ToolOutcome records whether an external effect's result is known.
type ToolOutcome string

const (
	ToolOutcomeSucceeded    ToolOutcome = "succeeded"
	ToolOutcomeKnownFailure ToolOutcome = "known_failure"
	ToolOutcomeUnknown      ToolOutcome = "unknown"
)

// validate rejects unknown tool outcomes.
func (outcome ToolOutcome) validate() error {
	switch outcome {
	case ToolOutcomeSucceeded, ToolOutcomeKnownFailure, ToolOutcomeUnknown:
		return nil
	default:
		return NewEnumValidationError("ToolOutcome", string(outcome))
	}
}

// UnmarshalJSON decodes and validates a tool outcome.
func (outcome *ToolOutcome) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, outcome, ToolOutcome.validate)
}

// ToolExecutionMetrics accompanies the durable tool message across archival.
type ToolExecutionMetrics struct {
	Outcome      ToolOutcome `json:"outcome"`
	DurationMS   *int64      `json:"duration_ms,omitempty"`
	ExitCode     *int        `json:"exit_code,omitempty"`
	ResultBytes  int64       `json:"result_bytes"`
	OmittedBytes *int64      `json:"omitted_bytes,omitempty"`
	// ResultCode is what the UI shows of the result: the failure code of a
	// failed call, or for commit_and_push whether it pushed or recorded a
	// blocked checkpoint. Other successful calls have none.
	ResultCode string `json:"result_code,omitempty"`
}
