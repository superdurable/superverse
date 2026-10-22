// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package conversation

import (
	"encoding/json"
	"fmt"
	"slices"
)

// RequestMessage is one message of a model request: the exact blocks of the
// entries it renders, in order. A system message may carry a per-message
// effort and no blocks.
type RequestMessage struct {
	ModelStopReason StopReason
	Role            EntryRole
	Blocks          []json.RawMessage
	Effort          Effort
	// CacheAnchor is the index of the block that carries the explicit cache
	// breakpoint, or -1.
	CacheAnchor int
}

// RequestShape is what the model profile allows in a request.
type RequestShape struct {
	// ExecutorContext admits a coherent retained user-first range after a remote handoff.
	ExecutorContext bool
	// SystemMessages renders system entries as role "system" messages. A model
	// without them reads their text as reminders at the end of the user
	// message and never gets a per-message effort.
	SystemMessages bool
}

// AssembleRequest renders the entries of one segment, its opening user entry first, as
// request messages. The entries are cut into gaps at each assistant entry.
// Each gap is one user message with the tool results first and the other user
// blocks after them, followed by one system message when the gap holds system
// entries. A model reply renders as an assistant message with its blocks as
// stored. anchor is the last sequence of the previous request: the last block
// of the last user entry at or before it carries the one explicit cache
// breakpoint, so a request uses at most three of the four the API allows
// (with the system prompt's and the automatic one).
//
// A violated invariant is a programming error and panics: the first entry is
// segment_start (or an executor's retained user-first entry), every gap holds a user entry, each tool_use is answered
// exactly once in the gap after it, the trailing gap is not empty and only
// user entries hold attachment references, which keep their positions until
// the model call replaces each 1:1.
func AssembleRequest(entries []SequencedEntry, anchor Sequence, shape RequestShape) []RequestMessage {
	if len(entries) == 0 || (entries[0].Entry.Kind != EntryKindSegmentStart &&
		(!shape.ExecutorContext || entries[0].Entry.Role != EntryRoleUser || entries[0].Entry.Kind == EntryKindToolResult)) {
		panic("a request starts with its segment_start entry")
	}
	anchored := Sequence(0)
	for _, entry := range entries {
		if entry.Sequence <= anchor && entry.Entry.Role == EntryRoleUser && len(entry.Entry.Blocks) > 0 {
			anchored = entry.Sequence
		}
	}
	var messages []RequestMessage
	var gap []SequencedEntry
	var toolUses []CallID
	for _, entry := range entries {
		if entry.Entry.Role != EntryRoleAssistant {
			gap = append(gap, entry)
			continue
		}
		if len(gap) == 0 && shape.ExecutorContext && len(toolUses) == 0 && len(messages) > 0 && messages[len(messages)-1].Role == EntryRoleAssistant {
			messages[len(messages)-1].Blocks = append(messages[len(messages)-1].Blocks, entry.Entry.Blocks...)
			toolUses = toolUseIDs(entry.Entry)
			continue
		}
		if len(gap) == 0 {
			panic(fmt.Sprintf("model reply %d follows another reply without a user entry", entry.Sequence))
		}
		messages = append(messages, renderGap(gap, toolUses, anchored, shape)...)
		gap, toolUses = nil, toolUseIDs(entry.Entry)
		messages = append(messages, RequestMessage{Role: EntryRoleAssistant, Blocks: entry.Entry.Blocks, ModelStopReason: entry.Entry.Meta.ModelStopReason, CacheAnchor: -1})
	}
	if len(gap) == 0 {
		panic("a model call needs a non-empty trailing gap")
	}
	return append(messages, renderGap(gap, toolUses, anchored, shape)...)
}

// renderGap checks one gap against the tool_use blocks of the reply before it
// and renders it: the user message, then the system message. The last block
// of the entry at sequence anchored carries the cache breakpoint.
func renderGap(gap []SequencedEntry, toolUses []CallID, anchored Sequence, shape RequestShape) []RequestMessage {
	var results, others, system []json.RawMessage
	var effort Effort
	answered := map[CallID]int{}
	anchorInResults, anchorIndex := false, -1
	for _, entry := range gap {
		blocks := entry.Entry.Blocks
		switch {
		case entry.Entry.Role == EntryRoleSystem:
			if entry.Entry.Meta.Effort != "" {
				effort = entry.Entry.Meta.Effort
			}
			if slices.ContainsFunc(blocks, IsAttachmentReference) {
				panic(fmt.Sprintf("system entry %d holds an attachment reference", entry.Sequence))
			}
			system = append(system, blocks...)
			continue
		case entry.Entry.Role != EntryRoleUser:
			panic(fmt.Sprintf("entry %d of role %s cannot be in a gap", entry.Sequence, entry.Entry.Role))
		case entry.Entry.Kind == EntryKindToolResult:
			answered[entry.Entry.Meta.ToolCall.ID]++
			results = append(results, blocks...)
		default:
			others = append(others, blocks...)
		}
		if entry.Sequence == anchored {
			anchorInResults = entry.Entry.Kind == EntryKindToolResult
			anchorIndex = len(others) - 1
			if anchorInResults {
				anchorIndex = len(results) - 1
			}
		}
	}
	if len(results)+len(others) == 0 {
		panic(fmt.Sprintf("the gap at entry %d has no user entry", gap[0].Sequence))
	}
	for _, id := range toolUses {
		if answered[id] != 1 {
			panic(fmt.Sprintf("tool_use %s is answered %d times in the gap after it", id, answered[id]))
		}
		delete(answered, id)
	}
	for id := range answered {
		panic(fmt.Sprintf("tool_result %s answers no tool_use of the reply before it", id))
	}
	if anchorIndex >= 0 && !anchorInResults {
		anchorIndex += len(results)
	}
	user := RequestMessage{Role: EntryRoleUser, Blocks: append(results, others...), CacheAnchor: anchorIndex}
	if !shape.SystemMessages {
		for _, block := range system {
			user.Blocks = append(user.Blocks, textBlock("<system-reminder>\n"+readBlock(block).Text+"\n</system-reminder>"))
		}
		return []RequestMessage{user}
	}
	if len(system) == 0 && effort == "" {
		return []RequestMessage{user}
	}
	return []RequestMessage{user, {Role: EntryRoleSystem, Blocks: system, Effort: effort, CacheAnchor: -1}}
}

// toolUseIDs lists the tool_use blocks of a model reply.
func toolUseIDs(entry Entry) []CallID {
	var ids []CallID
	for _, block := range entry.Blocks {
		if header := readBlock(block); header.Type == BlockTypeToolUse.String() {
			ids = append(ids, header.ID)
		}
	}
	return ids
}

// HasEffort reports whether a request sets a per-message effort.
func HasEffort(messages []RequestMessage) bool {
	return slices.ContainsFunc(messages, func(message RequestMessage) bool { return message.Effort != "" })
}
