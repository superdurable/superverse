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
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/comm"
)

// MaximumUserMessageContentBytes bounds one user-message body.
const MaximumUserMessageContentBytes = 256 << 10

// PendingRequestText quotes a bounded first request and the newest whole later
// entries that fit, without changing any stored entry or its authenticated actor.
func PendingRequestText(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	const separator = "\n\n"
	texts := make([]string, len(entries))
	total := (len(entries) - 1) * len(separator)
	for index, entry := range entries {
		texts[index] = entry.Text()
		total += len(texts[index])
	}
	if total <= MaximumUserMessageContentBytes {
		return strings.Join(texts, separator)
	}
	omittedLine := func(count int) string { return fmt.Sprintf("[%d pending request entries omitted]", count) }
	reserve := 0
	if len(entries) > 1 {
		reserve = len(separator) + len(omittedLine(len(entries)-1))
	}
	first := pendingRequestPart(entries[0], MaximumUserMessageContentBytes-reserve)
	remaining := MaximumUserMessageContentBytes - len(first) - reserve
	kept := make([]string, len(entries))
	omitted := len(entries) - 1
	for index := len(entries) - 1; index > 0; index-- {
		part := texts[index]
		if len(separator)+len(part) <= remaining {
			kept[index] = part
			remaining -= len(separator) + len(part)
			omitted--
		}
	}
	parts := []string{first}
	if omitted > 0 {
		parts = append(parts, omittedLine(omitted))
	}
	for _, part := range kept[1:] {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, separator)
}

// pendingRequestPart shortens only the decoded body of a known attributed
// entry; authorless historical text is never interpreted as an envelope.
func pendingRequestPart(entry Entry, maximum int) string {
	text := entry.Text()
	if len(text) <= maximum {
		return text
	}
	const suffix = " [truncated]"
	if entry.Meta.Actor == nil {
		return comm.TruncateUTF8(text, maximum-len(suffix)) + suffix
	}
	encoded, found := strings.CutPrefix(text, SenderEnvelopePrefix)
	if !found {
		panic("an attributed pending request requires its stored sender envelope")
	}
	var envelope senderEnvelope
	comm.MustDo(json.Unmarshal([]byte(encoded), &envelope))
	if envelope.Sender != entry.Meta.Actor.Email || (envelope.Via == nil) != (entry.Meta.Via == nil) ||
		(envelope.Via != nil && *envelope.Via != *entry.Meta.Via) {
		panic("a pending request cannot change its authenticated actor or via")
	}
	body := envelope.Text
	envelope.Text = suffix
	budget := maximum - len(encodeSenderEnvelope(envelope))
	if budget < 0 {
		panic("a pending request budget must retain its sender envelope and truncation suffix")
	}
	end := 0
	for offset, character := range body {
		cost := utf8.RuneLen(character)
		switch character {
		case '"', '\\', '\b', '\f', '\n', '\r', '\t':
			cost = 2
		case '\u2028', '\u2029':
			cost = 6
		default:
			if character < 0x20 {
				cost = 6
			}
		}
		if cost > budget {
			break
		}
		budget -= cost
		end = offset + utf8.RuneLen(character)
	}
	envelope.Text = body[:end] + suffix
	return encodeSenderEnvelope(envelope)
}

// Actor is the frozen authenticated author of a Session action.
type Actor struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
}

// MessageID uniquely identifies one pending application message across Channel moves.
type MessageID string

// Sequence orders durable application-history messages.
type Sequence int64

// MessageRole identifies a provider-neutral message kind.
type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
	// MessageRoleNotification is a Session notification: an automated report,
	// never a message of the user.
	MessageRoleNotification MessageRole = "notification"
)

// validate rejects unknown message roles.
func (role MessageRole) validate() error {
	switch role {
	case MessageRoleSystem, MessageRoleUser, MessageRoleAssistant, MessageRoleTool, MessageRoleNotification:
		return nil
	default:
		return NewEnumValidationError("MessageRole", string(role))
	}
}

// UnmarshalJSON decodes and validates a message role.
func (role *MessageRole) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, role, MessageRole.validate)
}

// AgentMessage is the public view of one entry (Entry.PublicMessage): user
// text, assistant text with its tool call identities, a tool result's status,
// a notification or a marker. It never carries thinking, tool arguments, tool
// results or a notification's report.
type AgentMessage struct {
	Via                 *string               `json:"via,omitempty"`
	Imported            bool                  `json:"imported,omitempty"`
	SteeredBy           *Actor                `json:"steered_by,omitempty"`
	Actor               *Actor                `json:"actor,omitempty"`
	Role                MessageRole           `json:"role"`
	Content             string                `json:"content"`
	ToolCalls           []ToolCallSummary     `json:"tool_calls"`
	ToolCallID          *CallID               `json:"tool_call_id,omitempty"`
	ToolName            *ToolName             `json:"tool_name,omitempty"`
	AnsweredInputCallID *CallID               `json:"answered_input_call_id,omitempty"`
	StartedAt           *time.Time            `json:"started_at,omitempty"`
	CreatedAt           time.Time             `json:"created_at"`
	ModelCall           *ModelCallMetrics     `json:"model_call,omitempty"`
	ToolExecution       *ToolExecutionMetrics `json:"tool_execution,omitempty"`
	Notification        *SessionNotification  `json:"notification,omitempty"`
	// MockProgress is where a mock reply left its scenario.
	MockProgress *MockProgress `json:"mock_progress,omitempty"`
}

// SequencedMessage pairs one application message with its durable ordering key.
type SequencedMessage struct {
	Sequence Sequence     `json:"sequence"`
	Message  AgentMessage `json:"message"`
}

// HistoryPage is one ascending page of retained application history.
type HistoryPage struct {
	Messages           []SequencedMessage `json:"messages"`
	NextBeforeSequence *Sequence          `json:"next_before_sequence,omitempty"`
}

// PendingUserMessage carries one stable application message ID through durable queues.
type PendingUserMessage struct {
	SteeredBy *Actor      `json:"steered_by,omitempty"`
	MessageID MessageID   `json:"message_id"`
	Value     UserMessage `json:"value"`
}

// UserMessage is a queued or steered user request.
type UserMessage struct {
	Actor               *Actor  `json:"actor,omitempty"`
	Via                 *string `json:"via,omitempty"`
	Content             string  `json:"content"`
	AnsweredInputCallID *CallID `json:"answered_input_call_id,omitempty"`
}

// SteerMessageRequest atomically moves one queued message into steering.
type SteerMessageRequest struct {
	Actor     *Actor    `json:"actor,omitempty"`
	MessageID MessageID `json:"message_id"`
}

// DeleteQueuedMessageRequest admits deletion against the author under the queue lock.
type DeleteQueuedMessageRequest struct {
	MessageID MessageID `json:"message_id"`
	Actor     Actor     `json:"actor"`
	IsOwner   bool      `json:"is_owner"`
}

// ArchivedMessagesNotFoundError reports a history chunk lost to retention or a stale view.
type ArchivedMessagesNotFoundError struct {
	BeforeSequence Sequence
}

var _ error = (*ArchivedMessagesNotFoundError)(nil)

// Error describes the missing history boundary.
func (err *ArchivedMessagesNotFoundError) Error() string {
	return fmt.Sprintf("archived messages before %d are no longer retained", err.BeforeSequence)
}
