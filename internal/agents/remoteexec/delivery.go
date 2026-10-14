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
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

const (
	maximumDeliveryEntries = 64
	deliverySoftBytes      = 1 << 20
)

// ReadRequest names a connection-bound cursor held by the Bridge. ArchiveKeys
// are only the previous typed read's server-derived selection, never HTTP input.
type ReadRequest struct {
	ProjectID    string                `json:"project_id"`
	ConnectionID string                `json:"connection_id"`
	Epoch        uint64                `json:"epoch"`
	After        conversation.Sequence `json:"after"`
	CurrentKeys  []string              `json:"current_keys,omitempty"`
	ArchiveKeys  []string              `json:"archive_keys,omitempty"`
	// RepositoryContext asks for the Session's repository context, which only
	// the read that builds a remote agent's context needs.
	RepositoryContext bool `json:"repository_context,omitempty"`
}

// Delivery is a read-only projection. Through includes examined filtered
// entries, never a budget-excluded eligible one. LastSequence is independent.
type Delivery struct {
	Entries               []conversation.SequencedEntry `json:"entries"`
	FirstRetainedSequence conversation.Sequence         `json:"first_retained_sequence"`
	LastSequence          conversation.Sequence         `json:"last_sequence"`
	Through               conversation.Sequence         `json:"through"`
	Truncated             bool                          `json:"truncated"`
	More                  bool                          `json:"more"`
}

func (executor Executor) CheckReader(request ReadRequest) error {
	if executor.Mode != ModeRemote || executor.Remote == nil || executor.Remote.ConnectionID != request.ConnectionID || executor.Epoch != request.Epoch {
		return &executorChangedError{}
	}
	return nil
}

// IsCallID admits only canonical stable call identities of the exact epoch.
func IsCallID(epoch uint64, id conversation.CallID) bool {
	_, ok := callSequence(epoch, id)
	return ok
}

// callSequence is the report sequence of a canonical call ID of epoch.
func callSequence(epoch uint64, id conversation.CallID) (uint64, bool) {
	sequence, found := strings.CutPrefix(string(id), fmt.Sprintf("remote-%d-", epoch))
	if !found {
		return 0, false
	}
	number, err := strconv.ParseUint(sequence, 10, 64)
	return number, err == nil && number > 0 && strconv.FormatUint(number, 10) == sequence
}

// projectEntry keeps canonical user input and this connection's results. It
// cannot expose assistant blocks, thinking, system text or opaque tool input.
func projectEntry(request ReadRequest, item conversation.SequencedEntry) (conversation.SequencedEntry, bool, error) {
	entry := item.Entry
	if entry.Role != conversation.EntryRoleUser {
		return item, false, nil
	}
	via := ""
	if entry.Meta.Via != nil {
		via = *entry.Meta.Via
	}
	switch entry.Kind {
	case conversation.EntryKindUserMessage, conversation.EntryKindNotice, conversation.EntryKindNotification:
		if via == request.ConnectionID {
			return item, false, nil
		}
		entry = conversation.NoticeEntry(conversation.EntryRoleUser, entry.Text(), "")
		entry.Kind = item.Entry.Kind
		entry.Meta.Via = item.Entry.Meta.Via
		if id := item.Entry.Meta.AnsweredInputCallID; id != nil && len(*id) <= 48 && IsCallID(request.Epoch, *id) {
			entry.Meta.AnsweredInputCallID = id
		}
	case conversation.EntryKindToolResult:
		if via != request.ConnectionID || entry.Meta.ToolCall == nil || !IsCallID(request.Epoch, entry.Meta.ToolCall.ID) {
			return item, false, nil
		}
		var blocks []json.RawMessage
		for _, raw := range entry.Blocks {
			var result struct {
				Type      string              `json:"type"`
				ToolUseID conversation.CallID `json:"tool_use_id"`
				Content   string              `json:"content"`
				IsError   bool                `json:"is_error"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				return item, false, err
			}
			if result.Type == conversation.BlockTypeToolResult.String() && result.ToolUseID == entry.Meta.ToolCall.ID {
				blocks = append(blocks, conversation.ToolResultBlock(result.ToolUseID, result.Content, result.IsError))
			}
		}
		if len(blocks) != 1 {
			return item, false, errors.New("a remote result must have exactly one matching block")
		}
		entry = conversation.Entry{Role: entry.Role, Kind: entry.Kind, Display: conversation.EntryDisplayPublic, Blocks: blocks,
			Meta: conversation.EntryMeta{Via: entry.Meta.Via, ToolCall: entry.Meta.ToolCall}}
	default:
		return item, false, nil
	}
	item.Entry = entry
	return item, true, nil
}

// BoundedDelivery includes the first eligible entry whole. The endpoint
// additionally measures its final wire response, including capped state.
func BoundedDelivery(request ReadRequest, first, last conversation.Sequence, entries []conversation.SequencedEntry) (Delivery, error) {
	if request.After < 0 || request.After > last {
		return Delivery{}, errors.New("remote delivery cursor is outside retained history")
	}
	first = max(1, first)
	delivery := Delivery{Entries: []conversation.SequencedEntry{}, FirstRetainedSequence: first, LastSequence: last,
		Through: max(request.After, first-1), Truncated: request.After < first-1}
	bytes := 2
	for _, item := range entries {
		if item.Sequence <= delivery.Through {
			continue
		}
		if item.Sequence != delivery.Through+1 {
			return Delivery{}, fmt.Errorf("remote delivery has a gap at sequence %d", delivery.Through+1)
		}
		projected, eligible, err := projectEntry(request, item)
		if err != nil {
			return Delivery{}, err
		}
		if eligible {
			encoded, err := json.Marshal(projected)
			if err != nil {
				return Delivery{}, err
			}
			if len(delivery.Entries) >= maximumDeliveryEntries || len(delivery.Entries) > 0 && bytes+len(encoded)+1 > deliverySoftBytes {
				break
			}
			bytes += len(encoded) + 1
			delivery.Entries = append(delivery.Entries, projected)
		}
		delivery.Through = item.Sequence
	}
	delivery.More = delivery.Through < last
	return delivery, nil
}
