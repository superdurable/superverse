// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
)

// A context segment is one replayable conversation: a segment_start entry and
// the entries after it, with a fixed system prompt, tool set, model and base
// effort. Renewing the context opens the next segment; nothing sent is ever
// edited. The functions below decide over values; the Steps apply them.

// SegmentReason is why a segment opened.
type SegmentReason string

const (
	SegmentReasonSessionStart SegmentReason = "session_start"
	SegmentReasonCompaction   SegmentReason = "compaction"
	SegmentReasonRollover     SegmentReason = "rollover"
	SegmentReasonUnsummarized SegmentReason = "unsummarized"
	SegmentReasonExecutor     SegmentReason = "executor"
)

// validate rejects an unknown reason.
func (reason SegmentReason) validate() error {
	switch reason {
	case SegmentReasonSessionStart, SegmentReasonCompaction, SegmentReasonRollover, SegmentReasonUnsummarized, SegmentReasonExecutor:
		return nil
	default:
		return conversation.NewEnumValidationError("SegmentReason", string(reason))
	}
}

// UnmarshalJSON decodes and validates a reason.
func (reason *SegmentReason) UnmarshalJSON(data []byte) error {
	return conversation.DecodeEnum(data, reason, SegmentReason.validate)
}

// ContextSegment is the current segment and its token accounting.
type ContextSegment struct {
	Endpoint           conversation.EndpointSelection `json:"endpoint,omitempty"`
	CredentialRevision uint64                         `json:"credential_revision,omitempty"`
	// Summary is the bounded previous-platform summary retained through remote work.
	Summary       string                `json:"summary,omitempty"`
	Number        int64                 `json:"number"`
	FirstSequence conversation.Sequence `json:"first_sequence"`
	// ProtocolStartSequence fences history imported from another executor: older
	// entries are textual context, never unsigned calls on the new wire.
	ProtocolStartSequence conversation.Sequence `json:"protocol_start_sequence,omitempty"`
	Reason                SegmentReason         `json:"reason"`
	Model                 conversation.Model    `json:"model"`
	// Effort is the segment's base effort; CurrentEffort the latest
	// per-message effort, empty while the base applies.
	Effort        conversation.Effort     `json:"effort,omitempty"`
	CurrentEffort conversation.Effort     `json:"current_effort,omitempty"`
	PromptDigest  string                  `json:"prompt_digest"`
	ToolNames     []conversation.ToolName `json:"tool_names"`
	// BaseBytes estimates the frozen prompt, tools and segment_start until
	// the segment has a usage figure.
	BaseBytes int64 `json:"base_bytes"`
	// PromptTokens and OutputTokens are the usage of the last response;
	// PendingBytes counts what was appended after it.
	PromptTokens int64 `json:"prompt_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
	PendingBytes int64 `json:"pending_bytes,omitempty"`
	// CacheAnchor is the last sequence of the previous request.
	CacheAnchor          conversation.Sequence `json:"cache_anchor,omitempty"`
	ForceCompaction      bool                  `json:"force_compaction,omitempty"`
	CompactionFailedTurn int64                 `json:"compaction_failed_turn,omitempty"`
	// AwaitsRepositoryContext marks a segment that opened without the
	// repository context: the first model-call boundary after the workspace
	// read it appends it once as a notice (RepositoryContextNotice) and clears
	// the mark. A segment of an earlier binary decodes false.
	AwaitsRepositoryContext bool `json:"awaits_repository_context,omitempty"`
	// AttachmentBudget bounds the attachments the segment shows, frozen when
	// it opened; AttachmentBytes, AttachmentImages and AttachmentTokens count
	// what they render: block bytes, images and visual tokens. A segment of an
	// earlier binary has no budget, so its first attachment compacts it.
	AttachmentBudget AttachmentBudget `json:"attachment_budget"`
	AttachmentBytes  int64            `json:"attachment_bytes,omitempty"`
	AttachmentImages int              `json:"attachment_images,omitempty"`
	AttachmentTokens int64            `json:"attachment_tokens,omitempty"`
	// Attachments are the newest attachments the Session's messages held up
	// to the segment, newest last, at most maximumListedAttachments, which the
	// next segment_start lists.
	Attachments []RememberedAttachment `json:"attachments,omitempty"`
}

// RememberedAttachment is an attachment a segment remembers for the next
// segment_start's list: its label, and whether no model call showed its
// content yet. Such an attachment was only ever a placeholder, so the Project
// never bound it and its tools cannot read it.
type RememberedAttachment struct {
	conversation.AttachmentLabel
	Unshown bool `json:"unshown,omitempty"`
}

// PromptsKey is the ContextSegmentPrompts instance key of the segment, its
// number, as in "3".
func (segment ContextSegment) PromptsKey() string {
	return strconv.FormatInt(segment.Number, 10)
}

// EffectiveEffort is the effort the segment's calls are sent with.
func (segment ContextSegment) EffectiveEffort() conversation.Effort {
	if segment.CurrentEffort != "" {
		return segment.CurrentEffort
	}
	return segment.Effort
}

// estimatedTokens is the trigger's context size: the last usage figure, or
// the base estimate before there is one, plus what was appended since.
func (segment ContextSegment) estimatedTokens() int64 {
	return max(segment.PromptTokens+segment.OutputTokens, segment.BaseBytes/4) + segment.PendingBytes/4
}

// AtHardLimit reports whether the context reached the size at which a failed
// compaction opens an unsummarized segment.
func (segment ContextSegment) AtHardLimit() bool {
	return segment.ForceCompaction || segment.estimatedTokens() >= comm.Must(modelprovider.ProfileFor(segment.Model, segment.Endpoint)).HardLimit
}

// SessionModel is the model a user selected for the Session and its revision.
// A change takes effect at the next turn through a rollover.
type SessionModel struct {
	ChangedBy *conversation.Actor `json:"changed_by,omitempty"`
	Model     conversation.Model  `json:"model"`
	Revision  uint64              `json:"revision"`
}

// UpdateSessionModelRequest selects a model at the revision the caller observed.
type UpdateSessionModelRequest struct {
	Actor            *conversation.Actor `json:"actor,omitempty"`
	Model            conversation.Model  `json:"model"`
	ExpectedRevision uint64              `json:"expected_revision"`
}

// ChangeSessionModel applies a selection at the exact observed revision. The
// exact next revision with the same model acknowledges a request whose
// response was lost; any other revision is rejected, including a later change
// back. The resource API rejects the largest revision, so ExpectedRevision+1
// cannot wrap around. It returns the selection and whether it is accepted.
func ChangeSessionModel(current SessionModel, request UpdateSessionModelRequest) (SessionModel, bool) {
	if current.Revision == request.ExpectedRevision+1 && current.Model == request.Model {
		return current, true
	}
	if current.Revision != request.ExpectedRevision {
		return current, false
	}
	return SessionModel{Model: request.Model, Revision: current.Revision + 1, ChangedBy: request.Actor}, true
}

// ModelSettings are the server's model settings for a Session's segments.
type ModelSettings struct {
	// Effort is the configured base effort, clamped per model.
	Effort conversation.Effort
	// CompactionTokens is the configured compaction threshold, capped per model.
	CompactionTokens int64
	PrefixMismatch   conversation.PrefixMismatchBehavior
	// Attachments is the attachment budget each new segment freezes.
	Attachments AttachmentBudget
}

// SessionPrompt is what the current binary sends a segment of one model: the
// frozen system prompt and tools, their digest and the tool names.
type SessionPrompt struct {
	Prompts   conversation.SegmentPrompts
	Digest    string
	ToolNames []conversation.ToolName
}

// promptModels are the models a prompt is built for.
var promptModels = []conversation.Model{conversation.ModelOpus55, conversation.ModelSonnet55, conversation.ModelHaiku45, conversation.ModelMock, conversation.ModelMockResponses, conversation.ModelMockChat, conversation.ModelDeepSeekFlash, conversation.ModelMiniMaxM3, conversation.ModelMuseWonder10, conversation.ModelGPT6Astra, conversation.ModelGrok47, conversation.ModelGemini4Argon, conversation.ModelAnthropicCustom, conversation.ModelOpenAICustom, conversation.ModelXAICustom, conversation.ModelGeminiCustom, conversation.ModelQwenCustom}

// sessionPrompts holds the prompt of each model; the catalog and the prompts
// are compiled in, so they are built once.
var sessionPrompts = func() map[conversation.Model]SessionPrompt {
	prompts := map[conversation.Model]SessionPrompt{}
	for _, model := range promptModels {
		prompts[model] = newPrompt(conversation.SessionSystemPrompt, SessionTools(model))
	}
	return prompts
}()

// subagentPrompt names the prompt of one subagent kind on one model.
type subagentPrompt struct {
	kind  string
	model conversation.Model
}

// subagentPrompts holds the prompt of each subagent kind on each model.
var subagentPrompts = func() map[subagentPrompt]SessionPrompt {
	prompts := map[subagentPrompt]SessionPrompt{}
	for _, kind := range agenttasks.SubagentKinds {
		for _, model := range promptModels {
			prompts[subagentPrompt{kind.String(), model}] = newPrompt(kind.SystemPrompt(), SubagentTools(kind, model))
		}
	}
	return prompts
}()

// newPrompt freezes a system prompt and tool set with their digest.
func newPrompt(system string, definitions []ToolDefinition) SessionPrompt {
	prompt := SessionPrompt{Prompts: conversation.SegmentPrompts{System: system}}
	for _, definition := range definitions {
		prompt.Prompts.Tools = append(prompt.Prompts.Tools, providerTool(definition))
		prompt.ToolNames = append(prompt.ToolNames, definition.Name)
	}
	digest := sha256.Sum256(comm.Must(json.Marshal(prompt.Prompts)))
	prompt.Digest = hex.EncodeToString(digest[:])
	return prompt
}

// SubagentPromptFor returns the current binary's prompt of a subagent of kind
// on model, which the subagent freezes when it starts.
func SubagentPromptFor(kind agenttasks.SubagentKind, model conversation.Model) SessionPrompt {
	return subagentPrompts[subagentPrompt{kind.String(), model}]
}

// providerTool is the tool as the request frozen into a segment carries it: a
// strict tool with the schema the provider takes, and eager input streaming
// where the catalog asks for it.
func providerTool(definition ToolDefinition) json.RawMessage {
	schema := definition.InputSchema
	if definition.strict {
		schema = modelprovider.StrictInputSchema(schema)
	}
	return comm.Must(json.Marshal(struct {
		Name                conversation.ToolName   `json:"name"`
		Description         string                  `json:"description"`
		InputSchema         conversation.JSONObject `json:"input_schema"`
		Strict              bool                    `json:"strict,omitempty"`
		EagerInputStreaming bool                    `json:"eager_input_streaming,omitempty"`
	}{definition.Name, definition.Description, schema, definition.strict, definition.eagerInputStreaming}))
}

// SessionPromptFor returns the current binary's prompt for model.
func SessionPromptFor(model conversation.Model) SessionPrompt {
	return sessionPrompts[model]
}

// NewSegment is the segment that opens at first with reason and the given
// segment_start bytes, on model with the configured settings.
func NewSegment(previous *ContextSegment, first conversation.Sequence, reason SegmentReason, model conversation.Model, settings ModelSettings, startBytes int64) ContextSegment {
	segment := NewSegmentFor(previous, first, reason, model, SessionPromptFor(model), modelprovider.ProfileOf(model).BaseEffort(settings.Effort), startBytes)
	segment.AttachmentBudget = settings.Attachments
	return segment
}

// NewSegmentFor is the segment that opens at first with reason and the given
// segment_start bytes, on model with prompt and the base effort, as a subagent
// opens its own.
func NewSegmentFor(previous *ContextSegment, first conversation.Sequence, reason SegmentReason, model conversation.Model, prompt SessionPrompt, effort conversation.Effort, startBytes int64) ContextSegment {
	number := int64(1)
	var attachments []RememberedAttachment
	if previous != nil {
		number, attachments = previous.Number+1, previous.Attachments
	}
	promptBytes := int64(len(prompt.Prompts.System))
	for _, tool := range prompt.Prompts.Tools {
		promptBytes += int64(len(tool))
	}
	return ContextSegment{Number: number, FirstSequence: first, Reason: reason, Model: model,
		Effort: effort, PromptDigest: prompt.Digest, ToolNames: prompt.ToolNames, BaseBytes: promptBytes + startBytes, Attachments: attachments}
}

// SegmentChange decides at a model-call boundary whether the current segment
// is renewed before the call. At a turn start a different prompt digest,
// selected model or base effort rolls it over. Otherwise a compaction is due
// when the context reached its threshold or the API reported it exceeded the
// window. A turn whose compaction already failed skips it below the hard
// limit. "" keeps the segment.
func SegmentChange(segment ContextSegment, selected conversation.Model, settings ModelSettings, turn int64, isTurnStart bool) SegmentReason {
	profile := comm.Must(modelprovider.ProfileFor(segment.Model, segment.Endpoint))
	if isTurnStart && (segment.Model != selected || segment.PromptDigest != SessionPromptFor(segment.Model).Digest ||
		segment.Effort != modelprovider.ProfileOf(selected).BaseEffort(settings.Effort)) {
		return SegmentReasonRollover
	}
	isDue := segment.ForceCompaction || segment.estimatedTokens() >= profile.CompactionThreshold(settings.CompactionTokens)
	if !isDue || segment.CompactionFailedTurn == turn && !segment.AtHardLimit() {
		return ""
	}
	return SegmentReasonCompaction
}

// SegmentStart is the opening message of a segment. CatalogMatches, the
// released connectors the first request names, only opens the Session's
// first segment. StartingExample describes the example the Project started
// from: a picked one in the Session's first segment, a matched one in every
// segment until the Session's first build. NoCardDesign says that the cards
// the agent records carry no Flow design, which every segment of a Project
// that started from an example or holds a pushed commit_and_push of the
// agent's says, so that the agent never tells it from a compacted memory.
type SegmentStart struct {
	Summary               string
	RepositoryContext     string
	ConfirmedRequirements *ConfirmedRequirements
	NoCardDesign          bool
	TodoList              *TodoList
	StartingExample       string
	CatalogMatches        string
	// Attachments are the attachments of earlier messages
	// (SegmentAttachments), and WorkflowExports the automation exports of the
	// pending request, which the workflow-import subskill reads.
	Attachments     []RememberedAttachment
	WorkflowExports []conversation.AttachmentRef
	PendingRequest  string
}

// Text renders the segment_start text: the summary, the repository context,
// the current requirements, the card design rule, the todo list, the starting
// example, the catalog matches, the attachments of earlier messages, the
// workflow import of the pending request's exports and the pending request,
// each in its own tag; the pending request stays last.
func (start SegmentStart) Text() string {
	text := taggedSection("context_summary", start.Summary) +
		taggedSection(repositoryContextTag, start.RepositoryContext) +
		taggedSection("confirmed_requirements", confirmedRequirementsText(start.ConfirmedRequirements)) +
		taggedSection(cardDesignTag, cardDesignText(start.NoCardDesign)) +
		taggedSection("current_plan", todoListText(start.TodoList)) +
		taggedSection(startingExampleTag, start.StartingExample) +
		taggedSection("catalog_matches", start.CatalogMatches) +
		taggedSection("attachments", attachmentsText(start.Attachments)) +
		taggedSection("workflow_import", workflowImportText(start.WorkflowExports)) +
		taggedSection("pending_request", start.PendingRequest)
	if text == "" {
		return "<context_summary>\nThe conversation starts here.\n</context_summary>"
	}
	return strings.TrimSuffix(text, "\n")
}

const repositoryContextTag = "repository_context"

// cardDesignTag is the tag of the card design section.
const cardDesignTag = "card_design"

// cardDesignText is the card design section of a Project whose cards carry no
// Flow design, or "" before a default-template Project's first build, where
// the prompt asks for one.
func cardDesignText(noCardDesign bool) string {
	if !noCardDesign {
		return ""
	}
	return "The cards you record for this Project carry no Flow design, since it started from an example or a commit_and_push of yours has pushed: Studio draws its real Flows and marks what the next build adds or changes."
}

// startingExampleTag is the tag of the starting example section.
const startingExampleTag = "starting_example"

// MaximumStartingExampleBytes bounds the starting example section.
const MaximumStartingExampleBytes = 16 << 10

// RepositoryContextNotice is the user notice that brings the repository
// context to a segment that opened without it, in the tag segment_start uses.
func RepositoryContextNotice(repositoryContext string) conversation.Entry {
	return conversation.NoticeEntry(conversation.EntryRoleUser, strings.TrimSuffix(taggedSection(repositoryContextTag, repositoryContext), "\n"), "")
}

// HoldsRepositoryContext reports whether text, a segment_start or a notice,
// holds a repository context section.
func HoldsRepositoryContext(text string) bool {
	opening := "<" + repositoryContextTag + ">\n"
	return strings.HasPrefix(text, opening) || strings.Contains(text, "\n"+opening)
}

// maximumRemoteAgentContextBytes bounds RemoteAgentContext: the prompts are
// about 30 KiB, the repository context at most 8 KiB and the starting example
// at most MaximumStartingExampleBytes.
const maximumRemoteAgentContextBytes = 80 << 10

// RemoteAgentContext is what a connected remote agent is told instead of the
// Session's system prompt and segment_start: the remote agent prompt, the
// shared Session rules, the repository context and, while a segment would
// carry them, the starting example and the card design rule, each in its own
// tag.
func RemoteAgentContext(repositoryContext, startingExample string, noCardDesign bool) string {
	text := conversation.RemoteAgentPrompt + "\n\n" + conversation.RemoteSessionSystemPrompt +
		"\n\n" + taggedSection(repositoryContextTag, repositoryContext) + taggedSection(startingExampleTag, startingExample) +
		taggedSection(cardDesignTag, cardDesignText(noCardDesign))
	return comm.TruncateUTF8(strings.TrimSpace(text), maximumRemoteAgentContextBytes)
}

// taggedSection is content in its tag, or empty when there is none.
func taggedSection(tag, content string) string {
	if content = strings.TrimSpace(content); content == "" {
		return ""
	}
	return fmt.Sprintf("<%s>\n%s\n</%s>\n", tag, content, tag)
}

// SegmentMarker is the divider the UI shows for a segment that opened for
// reason; the first segment of a Session shows none.
func SegmentMarker(reason SegmentReason) string {
	switch reason {
	case SegmentReasonCompaction:
		return "Context compacted"
	case SegmentReasonRollover:
		return "Context refreshed after a platform or model change"
	case SegmentReasonUnsummarized:
		return "Earlier context could not be summarized"
	default:
		return ""
	}
}

// ExtractSummary reads the summary of a compaction reply: the text inside
// <summary> tags, or the whole text when the model left them out.
func ExtractSummary(text string) string {
	if _, rest, found := strings.Cut(text, "<summary>"); found {
		summary, _, _ := strings.Cut(rest, "</summary>")
		return strings.TrimSpace(summary)
	}
	return strings.TrimSpace(text)
}

// ArchiveChunksFrom are the ArchivedMessages instance keys that hold the
// archived entries from first on.
func ArchiveChunksFrom(state AgentState, first conversation.Sequence) []string {
	var keys []string
	for chunk := ArchiveChunkFirst(max(first, state.FirstRetainedSequence)); chunk < state.CurrentFirstSequence; chunk += ArchiveMessageChunkSize {
		keys = append(keys, SequenceKey(chunk))
	}
	return keys
}

// ArchiveChunkFirst is the first sequence of the archive chunk holding sequence.
func ArchiveChunkFirst(sequence conversation.Sequence) conversation.Sequence {
	return (sequence-1)/ArchiveMessageChunkSize*ArchiveMessageChunkSize + 1
}

// RetainedFirstSequence is the first sequence history keeps once the archive
// chunks beyond the retention limit are dropped. A chunk is dropped only when
// it lies entirely before the current segment, so the segment can always be
// replayed.
func RetainedFirstSequence(state AgentState, segmentFirst conversation.Sequence) conversation.Sequence {
	retained := max(conversation.Sequence(0), state.LastSequence-state.FirstRetainedSequence+1)
	first := state.FirstRetainedSequence
	for retained > messageRetentionLimit && first < state.CurrentFirstSequence && first+ArchiveMessageChunkSize-1 < segmentFirst {
		first += ArchiveMessageChunkSize
		retained -= ArchiveMessageChunkSize
	}
	return first
}

// ExecutorContextStart selects one chronological suffix of complete units
// within the explicit replay byte budget and the attachment budget. It stops at
// an incomplete assistant/tool-result unit and never replays a result without
// its assistant call. The chosen suffix begins with user text.
func ExecutorContextStart(entries []conversation.SequencedEntry, replayBytes int64, attachments AttachmentBudget) conversation.Sequence {
	if len(entries) == 0 {
		return 0
	}
	budget, used := max(int64(0), replayBytes), int64(0)
	shown := ContextSegment{AttachmentBudget: attachments}
	first := conversation.Sequence(0)
	for end := len(entries); end > 0; {
		start := end - 1
		entry := entries[start].Entry
		if entry.Kind == conversation.EntryKindToolResult {
			results := map[conversation.CallID]bool{}
			for start >= 0 && entries[start].Entry.Kind == conversation.EntryKindToolResult {
				call := entries[start].Entry.Meta.ToolCall
				if call == nil || results[call.ID] {
					return first
				}
				results[call.ID] = true
				start--
			}
			if start < 0 || entries[start].Entry.Role != conversation.EntryRoleAssistant {
				break
			}
			calls := entries[start].Entry.Meta.ToolCalls
			if len(calls) != len(results) {
				break
			}
			complete := true
			for _, call := range calls {
				if !results[call.ID] {
					complete = false
					break
				}
			}
			if !complete {
				break
			}
		} else if len(entry.Meta.ToolCalls) > 0 {
			break
		}
		unitBytes, unit := int64(0), shown
		for index := start; index < end; index++ {
			unitBytes += entries[index].Entry.Bytes()
			unit = unit.withAttachments(entries[index].Entry.AttachmentReferences())
		}
		if used+unitBytes > budget || !unit.withinAttachmentBudget() {
			break
		}
		used, shown = used+unitBytes, unit
		if entries[start].Entry.Role == conversation.EntryRoleUser && entries[start].Entry.Kind != conversation.EntryKindToolResult {
			first = entries[start].Sequence
		}
		end = start
	}
	return first
}

// AttachmentBudget bounds what the attachments a segment shows add to its
// requests: the rendered bytes of their blocks (attachments.segmentBytes) and
// the images (attachments.segmentImages), within the API's request-size and
// image limits, since every block is resent on each call of the segment, and
// the text renditions of one message (attachments.messageInlineText).
type AttachmentBudget struct {
	SegmentBytes      int64 `json:"segment_bytes"`
	SegmentImages     int   `json:"segment_images"`
	MessageInlineText int64 `json:"message_inline_text"`
}

// maximumListedAttachments bounds the attachments a segment_start lists.
const maximumListedAttachments = 50

// withAttachments is the segment with the references it shows counted.
func (segment ContextSegment) withAttachments(references []conversation.AttachmentReference) ContextSegment {
	for _, reference := range references {
		if reference.Variant == conversation.AttachmentVariantPlaceholder {
			continue
		}
		segment.AttachmentBytes += reference.RenderedBytes()
		segment.AttachmentTokens += reference.ContextTokens()
		if reference.Attachment.Kind == conversation.AttachmentKindImage {
			segment.AttachmentImages++
		}
	}
	return segment
}

// withinAttachmentBudget reports whether the counted attachments stay within
// the segment's budget.
func (segment ContextSegment) withinAttachmentBudget() bool {
	return segment.AttachmentBytes <= segment.AttachmentBudget.SegmentBytes && segment.AttachmentImages <= segment.AttachmentBudget.SegmentImages
}

// messageVariants decides, within the message's inline-text bound, what the
// text attachments of one message show: in attachment order, a document or
// generic JSON file shows its full rendition when the full renditions chosen
// so far plus the briefs of the rest fit, otherwise its brief, which is at
// most 8 KiB; one without a brief, such as an export's summary, an image or a
// small generic file, always shows its full rendition.
func messageVariants(attachments []conversation.AttachmentRef, inline int64) []conversation.AttachmentReference {
	smallest := func(attachment conversation.AttachmentRef) int64 {
		switch {
		case attachment.Kind == conversation.AttachmentKindImage:
			return 0
		case attachment.Brief != nil:
			return attachment.Brief.Bytes
		default:
			return attachment.Rendition.Bytes
		}
	}
	rest := int64(0)
	for _, attachment := range attachments {
		rest += smallest(attachment)
	}
	chosen := int64(0)
	references := make([]conversation.AttachmentReference, 0, len(attachments))
	for _, attachment := range attachments {
		rest -= smallest(attachment)
		reference := conversation.AttachmentReference{Attachment: attachment, Variant: conversation.AttachmentVariantFull}
		if attachment.Brief != nil && attachment.Kind != conversation.AttachmentKindImage && chosen+attachment.Rendition.Bytes+rest > inline {
			reference.Variant = conversation.AttachmentVariantBrief
		}
		if attachment.Kind != conversation.AttachmentKindImage {
			chosen += reference.RenderedBytes()
		}
		references = append(references, reference)
	}
	return references
}

// fitting keeps what each reference shows while the segment's budget holds,
// visiting them in order; one that would pass it, or that follows
// conversation.MaximumMessageAttachments others, shows the placeholder. It
// returns the segment with the shown references counted and whether any is a
// placeholder.
func (segment ContextSegment) fitting(references []conversation.AttachmentReference, order []int) (ContextSegment, bool) {
	placed, shown := false, 0
	for _, index := range order {
		counted := segment.withAttachments(references[index : index+1])
		if shown == conversation.MaximumMessageAttachments || !counted.withinAttachmentBudget() {
			references[index].Variant, placed = conversation.AttachmentVariantPlaceholder, true
			continue
		}
		segment, shown = counted, shown+1
	}
	return segment, placed
}

// AdmitAttachments decides, as a user message's entry is appended to segment,
// what each of its references shows, frozen into its blocks: the variants of
// the message's inline-text bound (messageVariants), then, in attachment
// order, the placeholder for each that does not fit the segment's budget,
// which forces a compaction whose next segment shows the pending request's
// attachments again. It returns the segment with the shown attachments
// counted and every one remembered, and the entry.
func AdmitAttachments(segment ContextSegment, entry conversation.Entry) (ContextSegment, conversation.Entry) {
	var attachments []conversation.AttachmentRef
	var positions []int
	for position, block := range entry.Blocks {
		if reference, isReference := conversation.ReadAttachmentReference(block); isReference {
			attachments, positions = append(attachments, reference.Attachment), append(positions, position)
		}
	}
	if len(attachments) == 0 {
		return segment, entry
	}
	references := messageVariants(attachments, segment.AttachmentBudget.MessageInlineText)
	inOrder := make([]int, len(references))
	for index := range inOrder {
		inOrder[index] = index
	}
	segment, placed := segment.fitting(references, inOrder)
	segment.ForceCompaction = segment.ForceCompaction || placed
	segment.Attachments = rememberAttachments(segment.Attachments, references)
	blocks := slices.Clone(entry.Blocks)
	for index, position := range positions {
		blocks[position] = references[index].Block()
	}
	entry.Blocks = blocks
	return segment, entry
}

// SegmentAttachments chooses what the segment_start of a segment opening with
// budget shows of the attachments of its pending request, given in request
// order, each once: the variants of the inline-text bound, then, newest
// first, the placeholder for each that does not fit the fresh budget; a
// segment_start never forces a compaction. It returns one reference per
// attachment in request order, and the labels segment_start lists: the
// Session's earlier attachments (newest last in earlier), newest first, at
// most maximumListedAttachments, without the pending ones.
func SegmentAttachments(pending []conversation.AttachmentRef, earlier []RememberedAttachment, budget AttachmentBudget) ([]conversation.AttachmentReference, []RememberedAttachment) {
	references := messageVariants(pending, budget.MessageInlineText)
	newestFirst := make([]int, 0, len(references))
	for index := len(references) - 1; index >= 0; index-- {
		newestFirst = append(newestFirst, index)
	}
	ContextSegment{AttachmentBudget: budget}.fitting(references, newestFirst)
	isPending := map[string]bool{}
	for _, attachment := range pending {
		isPending[attachment.ID] = true
	}
	var listed []RememberedAttachment
	for _, attachment := range slices.Backward(earlier) {
		if !isPending[attachment.ID] && len(listed) < maximumListedAttachments {
			listed = append(listed, attachment)
		}
	}
	return references, listed
}

// OpenedWithAttachments is a segment that opened with the references its
// segment_start shows, which it counts, remembering the attachments of the
// pending request as those references show them.
func (segment ContextSegment) OpenedWithAttachments(shown []conversation.AttachmentReference, pending []conversation.AttachmentRef) ContextSegment {
	segment = segment.withAttachments(shown)
	isPending := map[string]bool{}
	for _, attachment := range pending {
		isPending[attachment.ID] = true
	}
	var requested []conversation.AttachmentReference
	for _, reference := range shown {
		if isPending[reference.Attachment.ID] {
			requested = append(requested, reference)
		}
	}
	segment.Attachments = rememberAttachments(segment.Attachments, requested)
	return segment
}

// rememberAttachments appends the attachments of references to the
// newest-last list, moving one listed already to the end, and keeps the
// newest maximumListedAttachments. An attachment stays unshown while every
// reference to it was a placeholder.
func rememberAttachments(listed []RememberedAttachment, references []conversation.AttachmentReference) []RememberedAttachment {
	if len(references) == 0 {
		return listed
	}
	remembered := slices.Clone(listed)
	for _, reference := range references {
		unshown := reference.Variant == conversation.AttachmentVariantPlaceholder
		if index := slices.IndexFunc(remembered, func(earlier RememberedAttachment) bool { return earlier.ID == reference.Attachment.ID }); index >= 0 {
			unshown = unshown && remembered[index].Unshown
			remembered = slices.Delete(remembered, index, index+1)
		}
		remembered = append(remembered, RememberedAttachment{AttachmentLabel: reference.Attachment.Label(), Unshown: unshown})
	}
	return remembered[max(0, len(remembered)-maximumListedAttachments):]
}

// attachmentUse says how the model can use an earlier attachment now.
var attachmentUse = map[conversation.AttachmentKind]string{
	conversation.AttachmentKindImage:    "as you saw it before the summary",
	conversation.AttachmentKindDocument: "read it with read_attachment",
	conversation.AttachmentKindJSON:     "place it in the workspace with place_attachment",
}

func init() {
	for _, kind := range conversation.AttachmentKinds {
		if attachmentUse[kind] == "" {
			panic("attachmentUse has no use of attachment kind " + kind.String())
		}
	}
}

// unshownAttachmentUse says what the model can do with an earlier attachment
// that no model call showed: its message came when the context had no room.
const unshownAttachmentUse = "never shown to you, since the context had no room for it when it was sent; ask the person to attach it again if you need it"

// attachmentsText is the attachments section: the attachments of earlier
// messages, newest first, by ID, name and kind with how to use each now.
func attachmentsText(listed []RememberedAttachment) string {
	if len(listed) == 0 {
		return ""
	}
	var text strings.Builder
	text.WriteString("Files the user attached to earlier messages, newest first. Their content is not in this context; your summary records what each contributed.\n")
	for _, attachment := range listed {
		use := attachmentUse[attachment.Kind]
		if attachment.Unshown {
			use = unshownAttachmentUse
		}
		fmt.Fprintf(&text, "- %s %s (%s): %s\n", attachment.ID, comm.Must(json.Marshal(attachment.Name)), attachment.Kind, use)
	}
	return text.String()
}

// The workflow-import subskill and its inventory script, as the coding image
// installs the in-repository dex-skills.
const (
	workflowImportReference = "/opt/superverse/dex-skills/dex-app-builder/references/workflow-import.md"
	workflowInventoryScript = "/opt/superverse/dex-skills/dex-app-builder/scripts/n8n_inventory.py"
)

// workflowImportText is the workflow_import section of a pending request that
// holds automation exports: the subskill, the place_attachment call of each
// export and the inventory command that reads the placed file.
func workflowImportText(exports []conversation.AttachmentRef) string {
	if len(exports) == 0 {
		return ""
	}
	var text strings.Builder
	fmt.Fprintf(&text, "The pending request holds a workflow export. Follow %s: the export, not an example, is the behavior authority, and you never summarize it by hand. "+
		"For each export, call place_attachment, then run the inventory on the placed file:\n", workflowImportReference)
	for _, export := range exports {
		placed := conversation.AttachmentWorkspacePath(export.Label())
		fmt.Fprintf(&text, "- %s, an %s export with %d secret values removed: place_attachment with attachment_id %q writes %s; then python3 %s inventory %s --out /workspace/docs/import/<workflow>\n",
			comm.Must(json.Marshal(export.Name)), export.JSON.Format, export.JSON.Redactions, export.ID, placed, workflowInventoryScript, placed)
	}
	return text.String()
}

// ModelReplayHistory reads and mutates the canonical message maps under the
// caller's MessageWindowLock, with exact archive instances already loaded.
type ModelReplayHistory struct {
	ReadCurrent  func(string) conversation.Entry
	WriteCurrent func(string, conversation.Entry)
	ReadArchive  func(string) conversation.ArchivedEntryChunk
	WriteArchive func(string, conversation.ArchivedEntryChunk)
}

// PurgeModelReplay removes only opaque payloads within an inclusive retained range.
// Map ownership, selective loads and the enclosing commit stay with the caller.
func PurgeModelReplay(state AgentState, first, last conversation.Sequence, history ModelReplayHistory) {
	if first <= 0 {
		return
	}
	first, last = max(first, state.FirstRetainedSequence), min(last, state.LastSequence)
	if first > last {
		return
	}
	for sequence := max(first, state.CurrentFirstSequence); sequence <= last; sequence++ {
		key := SequenceKey(sequence)
		entry := history.ReadCurrent(key)
		clean := entry.WithoutOpaqueReplay()
		if len(clean.Blocks) != len(entry.Blocks) {
			history.WriteCurrent(key, clean)
		}
	}
	archiveState := state
	archiveState.CurrentFirstSequence = min(state.CurrentFirstSequence, last+1)
	for _, key := range ArchiveChunksFrom(archiveState, first) {
		chunk := history.ReadArchive(key)
		changed := false
		for index, item := range chunk.Entries {
			if item.Sequence < first || item.Sequence > last {
				continue
			}
			clean := item.Entry.WithoutOpaqueReplay()
			if len(clean.Blocks) != len(item.Entry.Blocks) {
				chunk.Entries[index].Entry, changed = clean, true
			}
		}
		if changed {
			history.WriteArchive(key, chunk)
		}
		if len(chunk.Entries) > 0 && chunk.Entries[len(chunk.Entries)-1].Sequence >= last {
			break
		}
	}
}
