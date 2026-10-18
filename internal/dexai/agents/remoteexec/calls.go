// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteexec

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Call binds both sandboxd effects and the returned tool_result to report identity.
func (report RemoteReport) Call() conversation.ToolCall {
	if report.ToolCall == nil {
		panic("a remote tool call needs a tool report")
	}
	return conversation.ToolCall{ID: conversation.CallID(fmt.Sprintf("remote-%d-%d", report.Epoch, report.Sequence)), Name: report.ToolCall.Name, Arguments: report.ToolCall.Arguments}
}

// ToolEntry records a batch just like a model's assistant tool-use reply,
// without model metrics or hidden reasoning.
func ToolEntry(calls []conversation.ToolCall, summaries []conversation.ToolCallSummary, via string, at time.Time) conversation.Entry {
	blocks := make([]json.RawMessage, 0, len(calls))
	for _, call := range calls {
		block, err := json.Marshal(struct {
			Type  string                  `json:"type"`
			ID    conversation.CallID     `json:"id"`
			Name  conversation.ToolName   `json:"name"`
			Input conversation.JSONObject `json:"input"`
		}{conversation.BlockTypeToolUse.String(), call.ID, call.Name, call.Arguments})
		if err != nil {
			panic(err)
		}
		blocks = append(blocks, block)
	}
	return conversation.Entry{Role: conversation.EntryRoleAssistant, Kind: conversation.EntryKindModelReply, Display: conversation.EntryDisplayPublic, Blocks: blocks,
		Meta: conversation.EntryMeta{ToolCalls: summaries, Via: &via}, CreatedAt: at.UTC()}
}

// Entry freezes a public remote message with its authenticated connection
// owner. Delivery diagnostics become durable visible notices but do not open a
// platform turn; activity remains best-effort.
func (event RemoteEvent) Entry(owner conversation.Actor, connectionID string, at time.Time) (conversation.Entry, bool) {
	switch event.Kind {
	case EventDeliveryFailed:
		return conversation.DeliveryNoticeEntry("Message delivery failed: the local agent did not receive this message. Send it again."), true
	case EventDeliveryUncertain:
		return conversation.DeliveryNoticeEntry("Message delivery is uncertain: the local agent may not have received this message. Send it again if needed."), true
	case EventDeliveryDelayed:
		return conversation.DeliveryNoticeEntry("Message delivery is delayed: the local agent has not accepted it yet. It will keep retrying."), true
	}
	role := conversation.EntryRoleAssistant
	switch event.Kind {
	case EventUserMessage:
		role = conversation.EntryRoleUser
	case EventImportedMessage:
		role = event.Role
	case EventAssistantMessage:
	default:
		return conversation.Entry{}, false
	}
	var entry conversation.Entry
	if role == conversation.EntryRoleUser {
		entry = (conversation.UserMessage{Content: event.Text, Actor: &owner, Via: &connectionID}).Entry()
	} else {
		entry = conversation.NoticeEntry(conversation.EntryRoleAssistant, event.Text, "")
		entry.Kind, entry.Display = conversation.EntryKindModelReply, conversation.EntryDisplayPublic
		entry.Meta.Actor, entry.Meta.Via = &owner, &connectionID
	}
	entry.Meta.Imported = event.Kind == EventImportedMessage
	entry.CreatedAt = at.UTC()
	return entry, true
}
