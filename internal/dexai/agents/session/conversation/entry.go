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
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// The entry log is what the model saw, append-only: an entry's Blocks are the
// exact Claude content blocks and never change once appended. Meta is never
// sent to the model; the UI and metrics read it.

// EntryRole is the Claude role of an entry.
type EntryRole string

const (
	EntryRoleUser      EntryRole = "user"
	EntryRoleAssistant EntryRole = "assistant"
	EntryRoleSystem    EntryRole = "system"
)

// Validate rejects an unknown role.
func (role EntryRole) Validate() error {
	switch role {
	case EntryRoleUser, EntryRoleAssistant, EntryRoleSystem:
		return nil
	default:
		return NewEnumValidationError("EntryRole", string(role))
	}
}

// UnmarshalJSON decodes and validates a role.
func (role *EntryRole) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, role, EntryRole.Validate)
}

// EntryKind is what an entry is.
type EntryKind string

const (
	EntryKindUserMessage  EntryKind = "user_message"
	EntryKindModelReply   EntryKind = "model_reply"
	EntryKindToolResult   EntryKind = "tool_result"
	EntryKindNotice       EntryKind = "notice"
	EntryKindSegmentStart EntryKind = "segment_start"
	// EntryKindNotification is a Session notification (NotificationEntry).
	EntryKindNotification EntryKind = "notification"
)

// validate rejects an unknown kind.
func (kind EntryKind) validate() error {
	switch kind {
	case EntryKindUserMessage, EntryKindModelReply, EntryKindToolResult, EntryKindNotice, EntryKindSegmentStart, EntryKindNotification:
		return nil
	default:
		return NewEnumValidationError("EntryKind", string(kind))
	}
}

// UnmarshalJSON decodes and validates a kind.
func (kind *EntryKind) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, kind, EntryKind.validate)
}

// EntryDisplay is how the public API projects an entry.
type EntryDisplay string

const (
	// EntryDisplayPublic projects the entry as conversation.
	EntryDisplayPublic EntryDisplay = "public"
	// EntryDisplayMarker projects only Meta.Marker, as a divider.
	EntryDisplayMarker EntryDisplay = "marker"
	// EntryDisplayHidden projects nothing.
	EntryDisplayHidden EntryDisplay = "hidden"
)

// validate rejects an unknown display.
func (display EntryDisplay) validate() error {
	switch display {
	case EntryDisplayPublic, EntryDisplayMarker, EntryDisplayHidden:
		return nil
	default:
		return NewEnumValidationError("EntryDisplay", string(display))
	}
}

// UnmarshalJSON decodes and validates a display.
func (display *EntryDisplay) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, display, EntryDisplay.validate)
}

// Entry is one durable conversation item: the value of the CurrentMessages
// and ArchivedMessages AttributeMaps.
type Entry struct {
	Role      EntryRole         `json:"role"`
	Kind      EntryKind         `json:"kind"`
	Display   EntryDisplay      `json:"display"`
	Blocks    []json.RawMessage `json:"blocks"`
	Meta      EntryMeta         `json:"meta"`
	StartedAt *time.Time        `json:"started_at,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// EntryMeta is what the UI and metrics read of an entry; the model never sees it.
type EntryMeta struct {
	Actor     *Actor  `json:"actor,omitempty"`
	Via       *string `json:"via,omitempty"`
	Imported  bool    `json:"imported,omitempty"`
	SteeredBy *Actor  `json:"steered_by,omitempty"`
	// AnsweredInputCallID is the question batch a user message answers.
	AnsweredInputCallID *CallID `json:"answered_input_call_id,omitempty"`
	// ToolCalls summarizes the tool_use blocks of a model reply.
	ToolCalls []ToolCallSummary `json:"tool_calls,omitempty"`
	// ToolCall identifies the call a tool result answers.
	ToolCall      *ToolCallSummary      `json:"tool_call,omitempty"`
	ToolExecution *ToolExecutionMetrics `json:"tool_execution,omitempty"`
	ModelCall     *ModelCallMetrics     `json:"model_call,omitempty"`
	// Effort is the per-message effort a system notice sets.
	Effort Effort `json:"effort,omitempty"`
	// Marker is the divider text a marker entry shows.
	Marker string `json:"marker,omitempty"`
	// Notification is what the UI shows of a notification: the notification
	// without its report.
	Notification *SessionNotification `json:"notification,omitempty"`
	// MockProgress is what the mock model reported with a reply.
	MockProgress *MockProgress `json:"mock_progress,omitempty"`
}

// ToolCallSummary is what the UI sees of a tool call: never its arguments.
type ToolCallSummary struct {
	ID     CallID   `json:"id"`
	Name   ToolName `json:"name"`
	Target string   `json:"target,omitempty"`
}

// SequencedEntry pairs an entry with its durable sequence.
type SequencedEntry struct {
	Sequence Sequence `json:"sequence"`
	Entry    Entry    `json:"entry"`
}

// ArchivedEntryChunk stores one immutable ten-entry archive page.
type ArchivedEntryChunk struct {
	Entries []SequencedEntry `json:"entries"`
}

// Bytes is the size the entry adds to a request.
func (entry Entry) Bytes() int64 {
	var total int64
	for _, block := range entry.Blocks {
		total += int64(len(block))
	}
	return total
}

// BlockType is the type of a Claude content block, as the Messages API spells
// it: every type the stored blocks, the stream reader and the mock model
// tell apart.
type BlockType struct{ name string }

var (
	BlockTypeText             = BlockType{name: "text"}
	BlockTypeThinking         = BlockType{name: "thinking"}
	BlockTypeRedactedThinking = BlockType{name: "redacted_thinking"}
	BlockTypeToolUse          = BlockType{name: "tool_use"}
	BlockTypeServerToolUse    = BlockType{name: "server_tool_use"}
	BlockTypeToolResult       = BlockType{name: "tool_result"}
	BlockTypeFallback         = BlockType{name: "fallback"}
)

// String is the spelling of the Messages API.
func (blockType BlockType) String() string { return blockType.name }

// textBlock is a Claude text block. Claude rejects empty text blocks, so the
// caller passes non-empty text.
func textBlock(text string) json.RawMessage {
	if text == "" {
		panic("a Claude text block needs text")
	}
	return comm.Must(json.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{BlockTypeText.String(), text}))
}

// ToolResultBlock is a Claude tool_result block. An empty result says so,
// because a block without content would read as a successful nothing.
func ToolResultBlock(callID CallID, content string, isError bool) json.RawMessage {
	if strings.TrimSpace(content) == "" {
		content = "(no output)"
	}
	return comm.Must(json.Marshal(struct {
		Type      string `json:"type"`
		ToolUseID CallID `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error,omitempty"`
	}{BlockTypeToolResult.String(), callID, content, isError}))
}

// UserMessageEntry records an unattributed message, including trusted subagent tasks.
func UserMessageEntry(text string, answeredInputCallID *CallID) Entry {
	return UserMessage{Content: text, AnsweredInputCallID: answeredInputCallID}.Entry()
}

// Entry freezes the sender envelope and public projection when a user message is appended.
func (message UserMessage) Entry() Entry {
	text := message.Content
	meta := EntryMeta{AnsweredInputCallID: message.AnsweredInputCallID, Actor: message.Actor, Via: message.Via}
	if message.Actor != nil {
		text = encodeSenderEnvelope(senderEnvelope{message.Actor.Email, message.Via, message.Content})
	}
	return Entry{Role: EntryRoleUser, Kind: EntryKindUserMessage, Display: EntryDisplayPublic,
		Blocks: []json.RawMessage{textBlock(text)}, Meta: meta}
}

// SenderEnvelopePrefix introduces the single JSON object of an attributed user text block.
const SenderEnvelopePrefix = "Superverse message (JSON): "

type senderEnvelope struct {
	Sender string  `json:"sender"`
	Via    *string `json:"via"`
	Text   string  `json:"text"`
}

func encodeSenderEnvelope(envelope senderEnvelope) string {
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	comm.MustDo(encoder.Encode(envelope))
	return SenderEnvelopePrefix + strings.TrimSuffix(encoded.String(), "\n")
}

// NoticeEntry records an instruction the Session appends: role user for a
// continuation the model must act on, role system for an operator notice.
// A marker notice also shows marker in the UI.
func NoticeEntry(role EntryRole, text, marker string) Entry {
	display := EntryDisplayHidden
	if marker != "" {
		display = EntryDisplayMarker
	}
	return Entry{Role: role, Kind: EntryKindNotice, Display: display, Blocks: []json.RawMessage{textBlock(text)}, Meta: EntryMeta{Marker: marker}}
}

// EffortEntry records a per-message effort: a system message with no content.
func EffortEntry(effort Effort) Entry {
	return Entry{Role: EntryRoleSystem, Kind: EntryKindNotice, Display: EntryDisplayHidden, Blocks: []json.RawMessage{}, Meta: EntryMeta{Effort: effort}}
}

// SegmentStartEntry opens a context segment. The first segment of a Session
// is hidden; a later one shows marker.
func SegmentStartEntry(text, marker string) Entry {
	display := EntryDisplayHidden
	if marker != "" {
		display = EntryDisplayMarker
	}
	return Entry{Role: EntryRoleUser, Kind: EntryKindSegmentStart, Display: display, Blocks: []json.RawMessage{textBlock(text)}, Meta: EntryMeta{Marker: marker}}
}

// ModelReplyEntry records a model reply exactly as it will be replayed.
func ModelReplyEntry(reply ModelReply, toolCalls []ToolCallSummary, startedAt time.Time) Entry {
	metrics := reply.Metrics
	return Entry{Role: EntryRoleAssistant, Kind: EntryKindModelReply, Display: EntryDisplayPublic, Blocks: reply.Blocks,
		Meta: EntryMeta{ToolCalls: toolCalls, ModelCall: &metrics, MockProgress: reply.MockProgress}, StartedAt: &startedAt}
}

// blockHeader reads the fields of a stored block that assembly and projection need.
type blockHeader struct {
	Type      string     `json:"type"`
	ID        CallID     `json:"id"`
	ToolUseID CallID     `json:"tool_use_id"`
	Text      string     `json:"text"`
	Name      ToolName   `json:"name"`
	Input     JSONObject `json:"input"`
}

func readBlock(block json.RawMessage) blockHeader {
	var header blockHeader
	// Stored blocks are Claude blocks this binary wrote; any other block is
	// corrupt history.
	comm.MustDo(json.Unmarshal(block, &header))
	return header
}

// PublicMessage is the projection of an entry for the public API, or false
// when the entry is hidden. It carries user text, assistant text, tool call
// identities and a notification's summary; never thinking, tool arguments,
// tool results or a notification's report.
func (entry Entry) PublicMessage() (AgentMessage, bool) {
	message := AgentMessage{Actor: entry.Meta.Actor, Via: entry.Meta.Via, Imported: entry.Meta.Imported, SteeredBy: entry.Meta.SteeredBy, CreatedAt: entry.CreatedAt, StartedAt: entry.StartedAt, ToolCalls: []ToolCallSummary{}}
	switch {
	case entry.Display == EntryDisplayHidden:
		return AgentMessage{}, false
	case entry.Display == EntryDisplayMarker:
		message.Role, message.Content = MessageRoleSystem, entry.Meta.Marker
	case entry.Kind == EntryKindUserMessage:
		message.Role, message.Content, message.AnsweredInputCallID = MessageRoleUser, entry.Text(), entry.Meta.AnsweredInputCallID
		if entry.Meta.Actor != nil {
			var envelope struct {
				Text string `json:"text"`
			}
			encoded, found := strings.CutPrefix(message.Content, SenderEnvelopePrefix)
			if !found {
				panic("an attributed user entry requires its sender envelope")
			}
			comm.MustDo(json.Unmarshal([]byte(encoded), &envelope))
			message.Content = envelope.Text
		}
	case entry.Kind == EntryKindModelReply:
		message.Role, message.Content, message.ModelCall = MessageRoleAssistant, entry.Text(), entry.Meta.ModelCall
		message.ToolCalls, message.MockProgress = append(message.ToolCalls, entry.Meta.ToolCalls...), entry.Meta.MockProgress
	case entry.Kind == EntryKindToolResult:
		call := *entry.Meta.ToolCall
		message.Role, message.ToolCallID, message.ToolName, message.ToolExecution = MessageRoleTool, &call.ID, &call.Name, entry.Meta.ToolExecution
	case entry.Kind == EntryKindNotification:
		notification := *entry.Meta.Notification
		message.Role, message.Content, message.Notification = MessageRoleNotification, notification.Summary, &notification
	default:
		panic("a public entry is a user message, a model reply, a tool result or a notification: " + string(entry.Kind))
	}
	return message, true
}

// Text joins the entry's text blocks.
func (entry Entry) Text() string {
	var parts []string
	for _, block := range entry.Blocks {
		if header := readBlock(block); header.Type == BlockTypeText.String() {
			parts = append(parts, header.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}
