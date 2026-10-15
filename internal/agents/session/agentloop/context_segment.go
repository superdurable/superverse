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
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/comm"
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
	// Summary is the bounded previous-platform summary retained through remote work.
	Summary       string                `json:"summary,omitempty"`
	Number        int64                 `json:"number"`
	FirstSequence conversation.Sequence `json:"first_sequence"`
	Reason        SegmentReason         `json:"reason"`
	Model         conversation.Model    `json:"model"`
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
	return segment.ForceCompaction || segment.estimatedTokens() >= modelprovider.ProfileOf(segment.Model).HardLimit
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
}

// SessionPrompt is what the current binary sends a segment of one model: the
// frozen system prompt and tools, their digest and the tool names.
type SessionPrompt struct {
	Prompts   conversation.SegmentPrompts
	Digest    string
	ToolNames []conversation.ToolName
}

// promptModels are the models a prompt is built for.
var promptModels = []conversation.Model{conversation.ModelOpus55, conversation.ModelSonnet55, conversation.ModelHaiku45, conversation.ModelMock}

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
	return NewSegmentFor(previous, first, reason, model, SessionPromptFor(model), modelprovider.ProfileOf(model).BaseEffort(settings.Effort), startBytes)
}

// NewSegmentFor is the segment that opens at first with reason and the given
// segment_start bytes, on model with prompt and the base effort, as a subagent
// opens its own.
func NewSegmentFor(previous *ContextSegment, first conversation.Sequence, reason SegmentReason, model conversation.Model, prompt SessionPrompt, effort conversation.Effort, startBytes int64) ContextSegment {
	number := int64(1)
	if previous != nil {
		number = previous.Number + 1
	}
	promptBytes := int64(len(prompt.Prompts.System))
	for _, tool := range prompt.Prompts.Tools {
		promptBytes += int64(len(tool))
	}
	return ContextSegment{Number: number, FirstSequence: first, Reason: reason, Model: model,
		Effort: effort, PromptDigest: prompt.Digest, ToolNames: prompt.ToolNames, BaseBytes: promptBytes + startBytes}
}

// SegmentChange decides at a model-call boundary whether the current segment
// is renewed before the call. At a turn start a different prompt digest,
// selected model or base effort rolls it over. Otherwise a compaction is due
// when the context reached its threshold or the API reported it exceeded the
// window. A turn whose compaction already failed skips it below the hard
// limit. "" keeps the segment.
func SegmentChange(segment ContextSegment, selected conversation.Model, settings ModelSettings, turn int64, isTurnStart bool) SegmentReason {
	profile := modelprovider.ProfileOf(segment.Model)
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

// SegmentStart is the opening message of a segment.
type SegmentStart struct {
	Summary               string
	RepositoryContext     string
	ConfirmedRequirements *ConfirmedRequirements
	TodoList              *TodoList
	PendingRequest        string
}

// Text renders the segment_start text: the summary, the repository context,
// the confirmed requirements, the todo list and the pending request, each in
// its own tag.
func (start SegmentStart) Text() string {
	text := taggedSection("context_summary", start.Summary) +
		taggedSection(repositoryContextTag, start.RepositoryContext) +
		taggedSection("confirmed_requirements", confirmedRequirementsText(start.ConfirmedRequirements)) +
		taggedSection("current_plan", todoListText(start.TodoList)) +
		taggedSection("pending_request", start.PendingRequest)
	if text == "" {
		return "<context_summary>\nThe conversation starts here.\n</context_summary>"
	}
	return strings.TrimSuffix(text, "\n")
}

const repositoryContextTag = "repository_context"

// maximumRemoteAgentContextBytes bounds RemoteAgentContext: the prompts are
// about 30 KiB and the repository context at most 8 KiB.
const maximumRemoteAgentContextBytes = 64 << 10

// RemoteAgentContext is what a connected remote agent is told instead of the
// Session's system prompt and segment_start: the remote agent prompt, the
// shared Session rules and the repository context in its own tag.
func RemoteAgentContext(repositoryContext string) string {
	text := conversation.RemoteAgentPrompt + "\n\n" + conversation.RemoteSessionSystemPrompt +
		"\n\n" + taggedSection(repositoryContextTag, repositoryContext)
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
	for chunk := archiveChunkFirst(max(first, state.FirstRetainedSequence)); chunk < state.CurrentFirstSequence; chunk += ArchiveMessageChunkSize {
		keys = append(keys, SequenceKey(chunk))
	}
	return keys
}

// archiveChunkFirst is the first sequence of the archive chunk holding sequence.
func archiveChunkFirst(sequence conversation.Sequence) conversation.Sequence {
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

// ExecutorContextStart selects one chronological suffix of complete units.
// It stops at an incomplete assistant/tool-result unit and never replays a
// result without its assistant call. The chosen suffix begins with user text.
func ExecutorContextStart(entries []conversation.SequencedEntry, compactionTokens int64) conversation.Sequence {
	if len(entries) == 0 {
		return 0
	}
	budget, used := max(int64(0), compactionTokens*2), int64(0)
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
		unitBytes := int64(0)
		for index := start; index < end; index++ {
			unitBytes += entries[index].Entry.Bytes()
		}
		if used+unitBytes > budget {
			break
		}
		used += unitBytes
		if entries[start].Entry.Role == conversation.EntryRoleUser && entries[start].Entry.Kind != conversation.EntryKindToolResult {
			first = entries[start].Sequence
		}
		end = start
	}
	return first
}
