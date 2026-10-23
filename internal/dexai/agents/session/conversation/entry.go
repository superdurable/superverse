// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package conversation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// The entry log keeps canonical text/tool blocks and private provider replay.
// Existing Claude blocks retain their exact shape; opaque replay has an explicit cleanup projection.
// Except for replay cleanup and superverse_attachment reference blocks,
// which the model call replaces 1:1 by the immutable block their rendition
// object holds, blocks never change once appended. Meta is never sent to the
// model; the UI and metrics read it.

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
	// NotificationContinuesTurn records consumption during an already active turn.
	NotificationContinuesTurn bool `json:"notification_continues_turn,omitempty"`
	// ExecutorChanged marks a visible executor boundary, never a model input.
	ExecutorChanged bool `json:"executor_changed,omitempty"`
	// ModelStopReason records only output limits for safe compatible history projection.
	ModelStopReason StopReason `json:"model_stop_reason,omitempty"`
	Actor           *Actor     `json:"actor,omitempty"`
	Via             *string    `json:"via,omitempty"`
	Imported        bool       `json:"imported,omitempty"`
	SteeredBy       *Actor     `json:"steered_by,omitempty"`
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

// Bytes is the size the entry adds to a request's estimate: its blocks, a
// reference block by what it renders (AttachmentReference.contextBytes).
func (entry Entry) Bytes() int64 {
	var total int64
	for _, block := range entry.Blocks {
		if reference, isReference := ReadAttachmentReference(block); isReference {
			total += reference.contextBytes()
			continue
		}
		total += int64(len(block))
	}
	return total
}

// BlockType is the type of a Claude content block, as the Messages API spells
// it: every type the stored blocks, the stream reader and the mock model
// tell apart.
type BlockType struct{ name string }

var (
	BlockTypeOpaqueReplay     = BlockType{name: "opaque_replay"}
	BlockTypeText             = BlockType{name: "text"}
	BlockTypeThinking         = BlockType{name: "thinking"}
	BlockTypeRedactedThinking = BlockType{name: "redacted_thinking"}
	BlockTypeToolUse          = BlockType{name: "tool_use"}
	BlockTypeServerToolUse    = BlockType{name: "server_tool_use"}
	BlockTypeToolResult       = BlockType{name: "tool_result"}
	BlockTypeFallback         = BlockType{name: "fallback"}
	BlockTypeImage            = BlockType{name: "image"}
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

// Entry freezes the sender envelope and public projection when a user message
// is appended: one reference block per attachment, in the order they were
// attached, showing its full rendition until the append decides otherwise,
// then the text, whose sender envelope lists the attachments. An attributed
// message always has its envelope, also with blank text.
func (message UserMessage) Entry() Entry {
	text := message.Content
	meta := EntryMeta{AnsweredInputCallID: message.AnsweredInputCallID, Actor: message.Actor, Via: message.Via}
	blocks := make([]json.RawMessage, 0, len(message.Attachments)+1)
	var labels []AttachmentLabel
	for _, attachment := range message.Attachments {
		blocks = append(blocks, AttachmentReference{Attachment: attachment, Variant: AttachmentVariantFull}.Block())
		labels = append(labels, attachment.Label())
	}
	if message.Actor != nil {
		text = encodeSenderEnvelope(senderEnvelope{Sender: message.Actor.Email, Via: message.Via, Text: message.Content, Attachments: labels})
	}
	return Entry{Role: EntryRoleUser, Kind: EntryKindUserMessage, Display: EntryDisplayPublic, Blocks: append(blocks, textBlock(text)), Meta: meta}
}

// SenderEnvelopePrefix introduces the single JSON object of an attributed user text block.
const SenderEnvelopePrefix = "Project message (JSON): "

// legacySenderEnvelopePrefix keeps persisted attributed messages readable.
const legacySenderEnvelopePrefix = "Superverse message (JSON): "

// senderEnvelope is the attributed text of a user message; it lists the
// message's attachments, which come before it, only when there are any, so
// every envelope without them keeps its bytes.
type senderEnvelope struct {
	Sender      string            `json:"sender"`
	Via         *string           `json:"via"`
	Text        string            `json:"text"`
	Attachments []AttachmentLabel `json:"attachments,omitempty"`
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

// ExecutorChangeEntry records a presentation boundary without adding model instructions.
func ExecutorChangeEntry(remote bool) Entry {
	text := "Switched to the Platform agent"
	if remote {
		text = "Switched to the local agent"
	}
	return Entry{Role: EntryRoleSystem, Kind: EntryKindNotice, Display: EntryDisplayMarker,
		Blocks: []json.RawMessage{}, Meta: EntryMeta{Marker: text, ExecutorChanged: true}}
}

// DeliveryNoticeEntry is a platform-authored visible record that a remote
// agent did not definitely receive an already-recorded user message. It uses
// the ordinary public text projection without claiming it was a model call.
func DeliveryNoticeEntry(text string) Entry {
	return Entry{Role: EntryRoleAssistant, Kind: EntryKindModelReply, Display: EntryDisplayPublic, Blocks: []json.RawMessage{textBlock(text)}}
}

// EffortEntry records a per-message effort: a system message with no content.
func EffortEntry(effort Effort) Entry {
	return Entry{Role: EntryRoleSystem, Kind: EntryKindNotice, Display: EntryDisplayHidden, Blocks: []json.RawMessage{}, Meta: EntryMeta{Effort: effort}}
}

// SegmentStartEntry opens a context segment: its text, which stays the first
// block, then the references of the pending request's attachments. The first
// segment of a Session is hidden; a later one shows marker.
func SegmentStartEntry(text, marker string, attachments ...AttachmentReference) Entry {
	display := EntryDisplayHidden
	if marker != "" {
		display = EntryDisplayMarker
	}
	blocks := []json.RawMessage{textBlock(text)}
	for _, reference := range attachments {
		blocks = append(blocks, reference.Block())
	}
	return Entry{Role: EntryRoleUser, Kind: EntryKindSegmentStart, Display: display, Blocks: blocks, Meta: EntryMeta{Marker: marker}}
}

// ModelReplyEntry records a model reply exactly as it will be replayed.
func ModelReplyEntry(reply ModelReply, toolCalls []ToolCallSummary, startedAt time.Time) Entry {
	metrics := reply.Metrics
	var stopReason StopReason
	if reply.StopReason == StopReasonMaxTokens {
		stopReason = reply.StopReason
	}
	return Entry{Role: EntryRoleAssistant, Kind: EntryKindModelReply, Display: EntryDisplayPublic, Blocks: reply.Blocks,
		Meta: EntryMeta{ModelStopReason: stopReason, ToolCalls: toolCalls, ModelCall: &metrics, MockProgress: reply.MockProgress}, StartedAt: &startedAt}
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
// when the entry is hidden. It carries user text with the summaries of its
// attachments, assistant text, tool call identities and a notification's
// summary; never thinking, tool arguments, tool results, attachment objects or
// a notification's report.
func (entry Entry) PublicMessage() (AgentMessage, bool) {
	message := AgentMessage{NotificationContinuesTurn: entry.Meta.NotificationContinuesTurn, ExecutorChanged: entry.Meta.ExecutorChanged, Actor: entry.Meta.Actor, Via: entry.Meta.Via, Imported: entry.Meta.Imported, SteeredBy: entry.Meta.SteeredBy, CreatedAt: entry.CreatedAt, StartedAt: entry.StartedAt, ToolCalls: []ToolCallSummary{}}
	switch {
	case entry.Display == EntryDisplayHidden:
		return AgentMessage{}, false
	case entry.Display == EntryDisplayMarker:
		message.Role, message.Content = MessageRoleSystem, entry.Meta.Marker
	case entry.Kind == EntryKindUserMessage:
		message.Role, message.Content, message.AnsweredInputCallID = MessageRoleUser, entry.Text(), entry.Meta.AnsweredInputCallID
		for _, reference := range entry.AttachmentReferences() {
			message.Attachments = append(message.Attachments, reference.Attachment.Summary())
		}
		if entry.Meta.Actor != nil {
			var envelope struct {
				Text string `json:"text"`
			}
			encoded, found := cutSenderEnvelope(message.Content)
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

// AttachmentVariant is what a reference block shows of its attachment, frozen
// when the entry is appended or its segment opens: the full rendition, the
// brief of a document or large generic JSON file, or the placeholder that says
// the attachment is not shown in this part of the conversation.
type AttachmentVariant struct{ name string }

var (
	AttachmentVariantFull        = AttachmentVariant{name: "full"}
	AttachmentVariantBrief       = AttachmentVariant{name: "brief"}
	AttachmentVariantPlaceholder = AttachmentVariant{name: "placeholder"}
	attachmentVariants           = []AttachmentVariant{AttachmentVariantFull, AttachmentVariantBrief, AttachmentVariantPlaceholder}
)

// String is the persisted spelling.
func (variant AttachmentVariant) String() string { return variant.name }

// MarshalText writes the persisted spelling.
func (variant AttachmentVariant) MarshalText() ([]byte, error) { return []byte(variant.name), nil }

// UnmarshalText reads a persisted spelling.
func (variant *AttachmentVariant) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, variant, AttachmentVariant.String, attachmentVariants...)
}

// attachmentReferenceType is the type of a reference block. It is no Claude
// block type: a model call replaces every reference block 1:1 before its
// request leaves the Session, and the provider adapter refuses one that is
// left (IsAttachmentReference).
const attachmentReferenceType = "superverse_attachment"

// attachmentReferencePrefix opens every reference block, whose type is
// encoded first, so that a reader tells one apart without decoding a block.
var attachmentReferencePrefix = []byte(`{"type":"` + attachmentReferenceType + `",`)

// AttachmentReference is one reference block of an entry: the attachment and
// the variant it shows. A user message's references precede its envelope; a
// segment_start's follow its text. The block holds no content, about 0.5 KiB.
type AttachmentReference struct {
	Attachment AttachmentRef
	Variant    AttachmentVariant
}

// referenceBlock is the stored form of a reference: its type first, the
// variant, then the attachment's fields.
type referenceBlock struct {
	Type    string            `json:"type"`
	Variant AttachmentVariant `json:"variant"`
	AttachmentRef
}

// Block is the stored reference block.
func (reference AttachmentReference) Block() json.RawMessage {
	return comm.Must(json.Marshal(referenceBlock{Type: attachmentReferenceType, Variant: reference.Variant, AttachmentRef: reference.Attachment}))
}

// IsAttachmentReference reports whether block is a reference block.
func IsAttachmentReference(block json.RawMessage) bool {
	return bytes.HasPrefix(block, attachmentReferencePrefix)
}

// ReadAttachmentReference decodes a reference block; false for any other
// block.
func ReadAttachmentReference(block json.RawMessage) (AttachmentReference, bool) {
	if !IsAttachmentReference(block) {
		return AttachmentReference{}, false
	}
	var stored referenceBlock
	// A reference is a block this binary wrote; any other is corrupt history.
	comm.MustDo(json.Unmarshal(block, &stored))
	return AttachmentReference{Attachment: stored.AttachmentRef, Variant: stored.Variant}, true
}

// AttachmentReferences are the reference blocks of the entry, in order.
func (entry Entry) AttachmentReferences() []AttachmentReference {
	var references []AttachmentReference
	for _, block := range entry.Blocks {
		if reference, isReference := ReadAttachmentReference(block); isReference {
			references = append(references, reference)
		}
	}
	return references
}

// Rendition is the prepared rendition the reference shows; a placeholder
// shows none.
func (reference AttachmentReference) Rendition() (AttachmentRendition, bool) {
	switch reference.Variant {
	case AttachmentVariantFull:
		return reference.Attachment.Rendition, true
	case AttachmentVariantBrief:
		return *reference.Attachment.Brief, true
	default:
		return AttachmentRendition{}, false
	}
}

// RenderedBytes is the size of the block the reference renders, which the
// attachment byte budget of a segment counts; a placeholder counts none.
func (reference AttachmentReference) RenderedBytes() int64 {
	rendition, isShown := reference.Rendition()
	if !isShown {
		return 0
	}
	return rendition.Bytes
}

// ContextTokens is the reference's estimated share of the context: its
// rendition's, or for a placeholder its block at four bytes per token.
func (reference AttachmentReference) ContextTokens() int64 {
	rendition, isShown := reference.Rendition()
	if !isShown {
		return int64(len(PlaceholderAttachmentBlock(reference.Attachment))) / 4
	}
	return rendition.ContextTokens
}

// contextBytes is what the reference adds to a request's size estimate, which
// the compaction trigger divides by four: four times its context tokens. A
// rendered image's base64 bytes are no measure of the context it takes, and
// only the segment's attachment byte budget counts them.
func (reference AttachmentReference) contextBytes() int64 {
	return 4 * reference.ContextTokens()
}

// The text of every text rendition and placeholder is framed in the tag that
// names its attachment, <attachment id="…" name="…" kind="…">, which a
// placeholder marks with a status, then the text and </attachment>.

// attachmentFrame opens framed attachment text; status is empty for content.
func attachmentFrame(attachment AttachmentRef, status string) string {
	quoted := func(value string) string { return string(comm.Must(json.Marshal(value))) }
	opening := "<attachment id=" + quoted(attachment.ID) + " name=" + quoted(attachment.Name) + " kind=" + quoted(attachment.Kind.String())
	if status != "" {
		opening += " status=" + quoted(status)
	}
	return opening + ">"
}

// attachmentFrameOpening reads the opening tag of framed attachment text.
var attachmentFrameOpening = regexp.MustCompile(`^<attachment id="[a-z0-9]*" name="((?:[^"\\]|\\.)*)"( kind="[a-z_]+")?( format="[a-z0-9]+")?( status="[a-z]+")?>\n`)

// attachmentPlaceholder is the frozen text block that stands for attachment.
func attachmentPlaceholder(attachment AttachmentRef, status, text string) json.RawMessage {
	return textBlock(attachmentFrame(attachment, status) + "\n" + text + "\n</attachment>")
}

// attachmentFrameTag finds a tag that would open or close the frame of
// attachment text or a part inside it: a section, an excerpt, an outline, the
// start of a document or the content of a JSON file.
var attachmentFrameTag = regexp.MustCompile(`(?i)<\s*(/?)\s*(attachment|section|excerpt|outline|start|content)\b`)

// EscapeAttachmentText is untrusted text of an attachment, such as its
// content, a heading or a name, made safe to frame: every tag that would open
// or close a part of the frame starts with "&lt;" instead of "<", so no file
// ends the frame that marks it as the user's material, never instructions.
// Every rendition builder and read_attachment apply it to what the file holds
// before they frame it.
func EscapeAttachmentText(text string) string {
	return attachmentFrameTag.ReplaceAllString(text, "&lt;$1$2")
}

// FrameAttachmentText frames text of the attachment label names, as a text
// rendition is framed, for a tool result that reads the attachment. The
// caller escapes what the file holds (EscapeAttachmentText).
func FrameAttachmentText(label AttachmentLabel, text string) string {
	return attachmentFrame(AttachmentRef{ID: label.ID, Name: label.Name, Kind: label.Kind}, "") + "\n" + text + "\n</attachment>"
}

// PlaceholderAttachmentBlock is the frozen text block of a placeholder
// reference: the attachment is not shown in this part of the conversation.
func PlaceholderAttachmentBlock(attachment AttachmentRef) json.RawMessage {
	return attachmentPlaceholder(attachment, "placeholder", "This attachment is not shown in this part of the conversation: the context's attachment budget is used up. The context is compacted next, and the new context shows the attachment again while the request it belongs to is pending.")
}

// ExpiredAttachmentBlock is the frozen text block of an attachment whose
// staged and Project objects were both gone when a model call first showed
// it, which stays so: nothing restores them.
func ExpiredAttachmentBlock(attachment AttachmentRef) json.RawMessage {
	return attachmentPlaceholder(attachment, "expired", "This attachment expired or was removed before it was read; ask the person to attach it again if you need it.")
}

// FramedAttachmentName is the name of the attachment whose content a framed
// text shows; false for a placeholder and any other text.
func FramedAttachmentName(text string) (string, bool) {
	match := attachmentFrameOpening.FindStringSubmatch(text)
	if match == nil || match[4] != "" {
		return "", false
	}
	var name string
	if json.Unmarshal([]byte(`"`+match[1]+`"`), &name) != nil {
		return "", false
	}
	return name, true
}

// EnvelopeAttachments are the attachments an attributed text block lists in
// its sender envelope; nil for any other text.
func EnvelopeAttachments(text string) []AttachmentLabel {
	encoded, found := cutSenderEnvelope(text)
	var envelope senderEnvelope
	if !found || json.Unmarshal([]byte(encoded), &envelope) != nil {
		return nil
	}
	return envelope.Attachments
}

// UndeliveredAttachmentsNote is the line that tells the local agent which
// attachments of entry it did not receive, or "" when the entry holds none:
// only the Superverse agent reads attachments.
func UndeliveredAttachmentsNote(entry Entry) string {
	references := entry.AttachmentReferences()
	if len(references) == 0 {
		return ""
	}
	names := make([]string, 0, len(references))
	for _, reference := range references {
		names = append(names, reference.Attachment.Name)
	}
	count := fmt.Sprintf("%d attachments", len(names))
	if len(names) == 1 {
		count = "1 attachment"
	}
	return "Not delivered to the connected agent: " + count + " (" + strings.Join(names, ", ") + "). Attachments work with the Platform agent."
}

// MaximumOpaqueReplayBytes bounds the encoded private replay blocks of one reply.
const MaximumOpaqueReplayBytes = 256 << 10

// ProviderReplyStatus is the retained execution state of a provider output item.
type ProviderReplyStatus string

const (
	ProviderReplyCompleted  ProviderReplyStatus = "completed"
	ProviderReplyInProgress ProviderReplyStatus = "in_progress"
	ProviderReplyIncomplete ProviderReplyStatus = "incomplete"
)

// OpaqueReplay stores only encrypted protocol material and its binding metadata.
// It is private to the frozen segment; no public projection copies it.
type OpaqueReplay struct {
	Type       string              `json:"type"`
	Provider   string              `json:"provider"`
	WireMode   string              `json:"wireMode"`
	Position   int                 `json:"position"`
	CallID     CallID              `json:"callID,omitempty"`
	ItemID     string              `json:"itemID,omitempty"`
	Status     ProviderReplyStatus `json:"status,omitempty"`
	Ciphertext string              `json:"ciphertext"`
}

// Block validates and encodes replay material without retaining a raw provider object.
func (replay OpaqueReplay) Block() (json.RawMessage, error) {
	replay.Type = BlockTypeOpaqueReplay.String()
	if err := replay.validate(); err != nil {
		return nil, err
	}
	block, err := json.Marshal(replay)
	if err != nil {
		return nil, err
	}
	if len(block) > MaximumOpaqueReplayBytes {
		return nil, fmt.Errorf("opaque replay exceeds the reply budget")
	}
	return block, nil
}

func (replay OpaqueReplay) validate() error {
	if replay.Type != BlockTypeOpaqueReplay.String() || replay.Position < 0 || replay.Ciphertext == "" || len(replay.Ciphertext) > MaximumOpaqueReplayBytes {
		return fmt.Errorf("invalid opaque replay metadata")
	}
	switch {
	case (replay.Provider == ProviderOpenAI.String() || replay.Provider == ProviderXAI.String()) && replay.WireMode == OpenAIResponses.String():
		if replay.CallID != "" || replay.ItemID == "" || replay.Status != ProviderReplyCompleted {
			return fmt.Errorf("invalid Responses replay binding")
		}
	case replay.Provider == ProviderGemini.String() && replay.WireMode == OpenAIChatCompletions.String():
		if replay.CallID == "" || replay.ItemID != "" || replay.Status != "" {
			return fmt.Errorf("invalid Gemini replay binding")
		}
	case replay.Provider == ProviderMeta.String() && replay.WireMode == AnthropicMessages.String():
		if replay.CallID != "" || replay.ItemID != "" || replay.Status != "" {
			return fmt.Errorf("invalid Messages replay binding")
		}
	default:
		return fmt.Errorf("unsupported opaque replay protocol")
	}
	return nil
}

// IsOpaqueReplay recognizes private replay regardless of JSON field order.
func IsOpaqueReplay(block json.RawMessage) bool {
	var header struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(block, &header) == nil && header.Type == BlockTypeOpaqueReplay.String()
}

// ReadOpaqueReplay rejects unknown fields, wrong providers, modes and positions.
// Errors never include encrypted content or provider payloads.
func ReadOpaqueReplay(block json.RawMessage, provider Provider, mode APIStyle, position int) (OpaqueReplay, error) {
	var replay OpaqueReplay
	decoder := json.NewDecoder(bytes.NewReader(block))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&replay) != nil || !json.Valid(block) || len(block) > MaximumOpaqueReplayBytes {
		return OpaqueReplay{}, fmt.Errorf("invalid opaque replay encoding")
	}
	if err := replay.validate(); err != nil {
		return OpaqueReplay{}, err
	}
	if replay.Provider != provider.String() || replay.WireMode != mode.String() || replay.Position != position {
		return OpaqueReplay{}, fmt.Errorf("opaque replay does not match the frozen segment")
	}
	return replay, nil
}

// WithoutOpaqueReplay removes private protocol material when a segment closes
// and when compaction builds its allowlisted input. Original block slices stay immutable.
func (entry Entry) WithoutOpaqueReplay() Entry {
	blocks := make([]json.RawMessage, 0, len(entry.Blocks))
	for _, block := range entry.Blocks {
		if !IsOpaqueReplay(block) {
			blocks = append(blocks, block)
		}
	}
	entry.Blocks = blocks
	return entry
}

// WithoutProtocolHistory projects canonical content for compaction or a new
// executor's context. Tools become quoted history rather than unsigned native
// calls; only text, images and attachment references remain protocol blocks.
// The persisted entry and its original block slice remain unchanged.
func (entry Entry) WithoutProtocolHistory() Entry {
	blocks := make([]json.RawMessage, 0, len(entry.Blocks))
	for _, block := range entry.Blocks {
		header := readBlock(block)
		switch header.Type {
		case BlockTypeText.String(), BlockTypeImage.String():
			blocks = append(blocks, block)
		case BlockTypeToolUse.String(), BlockTypeToolResult.String():
			blocks = append(blocks, textBlock(RecordedToolHistoryPrefix+string(block)))
		default:
			if IsAttachmentReference(block) {
				blocks = append(blocks, block)
			}
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, textBlock("(No visible message content.)"))
	}
	entry.Blocks = blocks
	if entry.Kind == EntryKindToolResult {
		entry.Kind = EntryKindNotice
	}
	return entry
}

// RecordedToolHistoryPrefix labels tool JSON as quoted context rather than a native call.
const RecordedToolHistoryPrefix = "Recorded tool history (JSON):\n"

// cutSenderEnvelope accepts both retained history and the current envelope.
func cutSenderEnvelope(text string) (string, bool) {
	if body, ok := strings.CutPrefix(text, SenderEnvelopePrefix); ok {
		return body, true
	}
	return strings.CutPrefix(text, legacySenderEnvelopePrefix)
}
