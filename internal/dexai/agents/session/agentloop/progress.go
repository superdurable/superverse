// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"encoding/json"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// HeartbeatPhase names what a Step was doing when it recorded a heartbeat.
type HeartbeatPhase string

const (
	HeartbeatPhaseCompacting HeartbeatPhase = "compacting"
	HeartbeatPhaseModelCall  HeartbeatPhase = "model_call"
	HeartbeatPhaseToolCall   HeartbeatPhase = "tool_call"
)

// CompactionHeartbeat is the heartbeat of a silent compaction model call.
type CompactionHeartbeat struct {
	Phase   HeartbeatPhase `json:"phase"`
	Segment int64          `json:"segment"`
}

// ModelHeartbeat is the heartbeat of a streamed model reply.
type ModelHeartbeat struct {
	Phase HeartbeatPhase `json:"phase"`
}

// ToolHeartbeat is the heartbeat of a running tool call.
type ToolHeartbeat struct {
	Phase    HeartbeatPhase        `json:"phase"`
	ToolName conversation.ToolName `json:"tool_name"`
}

// CondenseActivityMessage collapses whitespace and bounds an activity message
// to 200 runes.
func CondenseActivityMessage(message string) string {
	const maximumRunes = 200
	condensed := strings.Join(strings.Fields(message), " ")
	runes := []rune(condensed)
	if len(runes) <= maximumRunes {
		return condensed
	}
	return string(runes[:maximumRunes-1]) + "…"
}

// InputConsumptionEvent reports the exact queued and steered messages a Step
// consumed; false when it consumed none.
func InputConsumptionEvent(queued, steered []conversation.PendingUserMessage, messageSequence *conversation.Sequence) (conversation.AgentEvent, bool) {
	if len(queued) == 0 && len(steered) == 0 {
		return conversation.AgentEvent{}, false
	}
	descriptions := make([]string, 0, 2)
	if len(queued) > 0 {
		descriptions = append(descriptions, consumedMessagesDescription(len(queued), "queued"))
	}
	if len(steered) > 0 {
		descriptions = append(descriptions, consumedMessagesDescription(len(steered), "steered"))
	}
	return conversation.AgentEvent{
		Kind:            conversation.EventKindInputConsumed,
		Message:         strings.Join(descriptions, " "),
		MessageSequence: messageSequence,
		InputConsumption: &conversation.InputConsumption{
			QueuedMessageIDs:  pendingMessageIDs(queued),
			SteeredMessageIDs: pendingMessageIDs(steered),
		},
	}, true
}

// ToolActivity is an activity event about one tool call, with its target.
func ToolActivity(kind conversation.EventKind, message string, call conversation.ToolCall) conversation.AgentEvent {
	return conversation.AgentEvent{Kind: kind, Message: message, CallID: &call.ID, ToolName: &call.Name, Target: ToolTarget(call)}
}

// ToolSummary is what the UI sees of a call: its identity and target, never
// its arguments.
func ToolSummary(call conversation.ToolCall) conversation.ToolCallSummary {
	return conversation.ToolCallSummary{ID: call.ID, Name: call.Name, Target: ToolTarget(call)}
}

// ToolTarget says what a call works on, in at most 200 characters: the
// description of a command or a subagent, the path, the query, the task, the
// agent or the Flow. Malformed arguments have no target.
func ToolTarget(call conversation.ToolCall) string {
	var arguments struct {
		Description string `json:"description"`
		Path        string `json:"path"`
		Pattern     string `json:"pattern"`
		Query       string `json:"query"`
		Summary     string `json:"summary"`
		FlowID      string `json:"flow_id"`
		TaskID      string `json:"task_id"`
		AgentID     string `json:"agent_id"`
		Name        string `json:"name"`
		FlowType    string `json:"flow_type"`
		Operation   string `json:"operation"`
		Release     string `json:"release_id"`
		Deploy      string `json:"deployment_id"`
		Reason      string `json:"reason"`
		Level       string `json:"level"`
		Title       string `json:"title"`
		Questions   []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	if json.Unmarshal([]byte(call.Arguments), &arguments) != nil {
		return ""
	}
	target := ""
	switch call.Name {
	case conversation.ToolBash, conversation.ToolStartAgent:
		target = arguments.Description
	case conversation.ToolMergeAgentChanges:
		target = arguments.AgentID
	case conversation.ToolOpenTaskWorktree, conversation.ToolMergeTaskWorktree:
		target = arguments.Name
	case conversation.ToolRead, conversation.ToolEdit, conversation.ToolWrite:
		target = arguments.Path
	case conversation.ToolGrep, conversation.ToolGlob:
		target = arguments.Pattern
	case conversation.ToolConnectorCatalog:
		target = arguments.Query
	case conversation.ToolReadTaskOutput, conversation.ToolStopTask:
		target = arguments.TaskID
	case conversation.ToolCommitAndPush:
		target, _, _ = strings.Cut(arguments.Summary, "\n")
	case conversation.ToolReadPlatformFlow:
		target = arguments.FlowID
	case conversation.ToolReadLiveAppRuns:
		target = strings.TrimSpace(arguments.Operation + " " + arguments.FlowType + " " + arguments.FlowID)
	case conversation.ToolReadDeliveryStatus:
		target = strings.TrimSpace(arguments.Release + " " + arguments.Deploy)
	case conversation.ToolStartTimer:
		target = arguments.Reason
	case conversation.ToolSetEffort:
		target = arguments.Level
	case conversation.ToolRequestUserInput:
		if len(arguments.Questions) > 0 {
			target = arguments.Questions[0].Question
		}
	case conversation.ToolConfirmRequirements:
		target = arguments.Title
	case conversation.ToolWriteTodos:
	}
	return CondenseActivityMessage(target)
}
