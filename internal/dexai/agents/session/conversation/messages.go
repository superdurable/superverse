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
	Via       *string     `json:"via,omitempty"`
	Imported  bool        `json:"imported,omitempty"`
	SteeredBy *Actor      `json:"steered_by,omitempty"`
	Actor     *Actor      `json:"actor,omitempty"`
	Role      MessageRole `json:"role"`
	Content   string      `json:"content"`
	// Attachments summarize the files a user message carries.
	Attachments         []AttachmentSummary   `json:"attachments,omitempty"`
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
	// Attachments are the files the message carries, at most
	// MaximumMessageAttachments, by reference: their content stays in object
	// storage and never enters Flow state.
	Attachments []AttachmentRef `json:"attachments,omitempty"`
}

// MaximumMessageAttachments bounds the attachments of one user message.
const MaximumMessageAttachments = 10

// MaximumAttachmentBriefBytes bounds the smallest block a document or JSON
// file renders: its brief, or the rendition of one without a brief, such as
// an export's summary.
const MaximumAttachmentBriefBytes = 8 << 10

// imageBlockOverhead bounds what the JSON of an image block adds to its
// base64 data.
const imageBlockOverhead = 128

// imageBlockBytes bounds the rendered block of an image of imageBytes: its
// base64 inside an image block.
func imageBlockBytes(imageBytes int64) int64 {
	return 4*((imageBytes+2)/3) + imageBlockOverhead
}

// ImageBlockBytesForTest is imageBlockBytes for the tests that check a real
// rendition against it.
func ImageBlockBytesForTest(imageBytes int64) int64 { return imageBlockBytes(imageBytes) }

// MessageAttachmentBytes bounds the rendered attachment blocks of one message
// of at most perMessage attachments, whose images have at most imageBytes and
// whose documents and JSON files show at most inline bytes, or their briefs
// when those alone take more. The configuration requires it to fit a fresh
// segment's budget, so that one message always shows every attachment once
// the context is compacted.
func MessageAttachmentBytes(perMessage int, imageBytes, inline int64) int64 {
	worst := int64(0)
	for images := 0; images <= perMessage; images++ {
		texts, bytes := int64(perMessage-images), int64(images)*imageBlockBytes(imageBytes)
		if texts > 0 {
			bytes += max(inline, texts*MaximumAttachmentBriefBytes)
		}
		worst = max(worst, bytes)
	}
	return worst
}

// AttachmentKind is what an attached file is, which decides how the model
// reads it: an image block, the framed text of a document, or a JSON file's
// text or summary. A reference stores it as a plain string, so the Session's
// start input stays a plain value; the API admits only these values.
type AttachmentKind string

const (
	AttachmentKindImage    AttachmentKind = "image"
	AttachmentKindDocument AttachmentKind = "document"
	AttachmentKindJSON     AttachmentKind = "json"
)

// AttachmentKinds are every kind.
var AttachmentKinds = []AttachmentKind{AttachmentKindImage, AttachmentKindDocument, AttachmentKindJSON}

// String is the persisted spelling.
func (kind AttachmentKind) String() string { return string(kind) }

// AttachmentJSONFormat is the automation tool a JSON attachment was exported
// from, or generic JSON; an export routes to the workflow-import subskill. A
// reference stores it as a plain string, as AttachmentKind.
type AttachmentJSONFormat string

const (
	AttachmentJSONFormatN8n     AttachmentJSONFormat = "n8n"
	AttachmentJSONFormatGeneric AttachmentJSONFormat = "generic"
)

// AttachmentObject is one staged object of an attachment, named by the
// SHA-256 digest of its bytes: the object store holds it at
// attachment-staging/<teamSpaceID>/<attachmentID>/<role>/<hex digest>.
type AttachmentObject struct {
	Digest string `json:"digest"`
	Bytes  int64  `json:"bytes"`
	// Version is the object's exact version, which the copy into the Project
	// copies and every read of the copy verifies by digest.
	Version string `json:"version"`
}

// AttachmentRendition is one prepared rendition, the exact Claude block the
// model reads, built once: its object and its estimated context tokens (an
// image's visual tokens, text's bytes divided by four).
type AttachmentRendition struct {
	AttachmentObject
	ContextTokens int64 `json:"context_tokens"`
}

// AttachmentRef is what a message carries of one attachment its
// AttachmentFlow prepared: identity, description and its staged objects by
// digest, never content. The first model call that shows it copies the
// objects into the Project's attachment prefix.
type AttachmentRef struct {
	ID          string         `json:"id"`
	TeamSpaceID string         `json:"team_space_id"`
	Kind        AttachmentKind `json:"kind"`
	Name        string         `json:"name"`
	MediaType   string         `json:"media_type"`
	ByteSize    int64          `json:"byte_size"`
	// Original is the uploaded file, a JSON file's redacted copy instead.
	Original AttachmentObject `json:"original"`
	// Rendition is the block the model reads: a base64 image block, or framed
	// text. Brief, of a document or a large generic JSON file, is its outline
	// or summary in at most 8 KiB, which a message short of room shows
	// instead; Bundle is a document's sections, which read_attachment pages.
	Rendition AttachmentRendition  `json:"rendition"`
	Brief     *AttachmentRendition `json:"brief,omitempty"`
	Bundle    *AttachmentObject    `json:"bundle,omitempty"`
	Image     *AttachmentImage     `json:"image,omitempty"`
	JSON      *AttachmentJSON      `json:"json,omitempty"`
}

// AttachmentImage is the pixel size of a prepared image.
type AttachmentImage struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// AttachmentJSON describes a prepared JSON file: the format it was detected
// as and how many secret values preparation replaced with the marker.
type AttachmentJSON struct {
	Format     AttachmentJSONFormat `json:"format"`
	Redactions int                  `json:"redactions,omitempty"`
}

// AttachmentLabel names an attachment by ID, name and kind, as a sender
// envelope lists the attachments of its message and a segment_start the
// earlier ones of the Session.
type AttachmentLabel struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Kind AttachmentKind `json:"kind"`
}

// Label is the attachment's label.
func (attachment AttachmentRef) Label() AttachmentLabel {
	return AttachmentLabel{ID: attachment.ID, Name: attachment.Name, Kind: attachment.Kind}
}

// AttachmentSummary is what the public view shows of an attachment: never its
// objects or content.
type AttachmentSummary struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Kind      AttachmentKind `json:"kind"`
	MediaType string         `json:"media_type"`
	ByteSize  int64          `json:"byte_size"`
	Width     int            `json:"width,omitempty"`
	Height    int            `json:"height,omitempty"`
	// JSONFormat and Redactions describe a JSON file: its format and the
	// secret values preparation removed.
	JSONFormat AttachmentJSONFormat `json:"json_format,omitempty"`
	Redactions int                  `json:"redactions,omitempty"`
}

// Summary is the attachment's public summary, which the history and the
// queued and steered messages show.
func (attachment AttachmentRef) Summary() AttachmentSummary {
	summary := AttachmentSummary{ID: attachment.ID, Name: attachment.Name, Kind: attachment.Kind, MediaType: attachment.MediaType, ByteSize: attachment.ByteSize}
	if attachment.Image != nil {
		summary.Width, summary.Height = attachment.Image.Width, attachment.Image.Height
	}
	if attachment.JSON != nil {
		summary.JSONFormat, summary.Redactions = attachment.JSON.Format, attachment.JSON.Redactions
	}
	return summary
}

// AttachmentWorkspaceDirectory is where place_attachment writes attachments,
// relative to the workspace root: under /.local/, which the template's
// .gitignore ignores and commit_and_push refuses to stage.
const AttachmentWorkspaceDirectory = ".local/superverse-attachments"

// AttachmentFile is the file of an attachment its Project bound, which
// place_attachment writes into the workspace: a JSON file's redacted copy,
// with its digest and the number of secret values preparation replaced.
type AttachmentFile struct {
	Label      AttachmentLabel
	Data       []byte
	Digest     string
	Redactions int
}

// AttachmentWorkspacePath is the absolute path place_attachment writes
// attachment at in the Sandbox: /workspace/.local/superverse-attachments/<id>/<name>,
// with the name reduced to letters, digits, '.', '-' and '_'.
func AttachmentWorkspacePath(attachment AttachmentLabel) string {
	var name strings.Builder
	for _, character := range attachment.Name {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '.', character == '-', character == '_':
			name.WriteRune(character)
		default:
			name.WriteByte('_')
		}
	}
	safe := strings.TrimLeft(name.String(), ".")
	if safe == "" {
		safe = "attachment"
	}
	return "/workspace/" + AttachmentWorkspaceDirectory + "/" + attachment.ID + "/" + safe[:min(len(safe), 128)]
}

// IsWorkflowExport reports whether the attachment is an automation export the
// workflow-import subskill reads.
func (attachment AttachmentRef) IsWorkflowExport() bool {
	return attachment.Kind == AttachmentKindJSON && attachment.JSON != nil && attachment.JSON.Format == AttachmentJSONFormatN8n
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
