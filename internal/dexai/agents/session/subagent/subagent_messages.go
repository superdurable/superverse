// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package subagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
)

// A subagent keeps its message log and its context segments like the Session:
// the newest entries in CurrentMessages keyed by sequence, older chunks in
// ArchivedMessages, and the frozen prompts of the current segment in
// ContextSegmentPrompts. Its one task is one turn, so AgentState's turn
// counters count its model calls. The functions below read and write the
// subagent's own primitives in the commit of the calling Step; agentloop and
// conversation decide over values.

// appendEntry records an entry at the next sequence and archives the oldest
// chunk when the current window is full. What it adds to the current
// segment's request counts toward the segment's compaction.
func appendEntry(ctx dex.Context, entry conversation.Entry) conversation.Sequence {
	state := comm.Must(agentStateAttribute.Get(ctx))
	state.LastSequence++
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.Blocks == nil {
		entry.Blocks = []json.RawMessage{}
	}
	comm.MustDo(currentMessagesAttribute.Set(ctx, agentloop.SequenceKey(state.LastSequence), entry))
	if state.LastSequence-state.CurrentFirstSequence+1 >= conversation.Sequence(agentloop.CurrentMessageLimit) {
		archiveEntryChunk(ctx, state.CurrentFirstSequence)
		state.CurrentFirstSequence += conversation.Sequence(agentloop.ArchiveMessageChunkSize)
	}
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	if segment := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx)); segment != nil && entry.Kind != conversation.EntryKindModelReply {
		segment.PendingBytes += entry.Bytes()
		comm.MustDo(contextSegmentAttribute.Set(ctx, *segment))
	}
	return state.LastSequence
}

// archiveEntryChunk moves the chunk that starts at first from CurrentMessages
// to ArchivedMessages.
func archiveEntryChunk(ctx dex.Context, first conversation.Sequence) {
	chunk := conversation.ArchivedEntryChunk{Entries: make([]conversation.SequencedEntry, 0, agentloop.ArchiveMessageChunkSize)}
	for sequence := first; sequence < first+conversation.Sequence(agentloop.ArchiveMessageChunkSize); sequence++ {
		chunk.Entries = append(chunk.Entries, conversation.SequencedEntry{Sequence: sequence, Entry: comm.Must(currentMessagesAttribute.Get(ctx, agentloop.SequenceKey(sequence)))})
		comm.MustDo(currentMessagesAttribute.Delete(ctx, agentloop.SequenceKey(sequence)))
	}
	comm.MustDo(archivedMessagesAttribute.Set(ctx, agentloop.SequenceKey(first), chunk))
}

// archivedEntry is the entry at sequence of the archive chunk that holds it.
// A missing entry is corrupt history.
func archivedEntry(chunk conversation.ArchivedEntryChunk, sequence conversation.Sequence) conversation.Entry {
	index := sequence - agentloop.ArchiveChunkFirst(sequence)
	if index >= conversation.Sequence(len(chunk.Entries)) || chunk.Entries[index].Sequence != sequence {
		panic(fmt.Sprintf("archived entry %d is missing from its chunk", sequence))
	}
	return chunk.Entries[index].Entry
}

// modelCallPlan is the movement to the next model call: a compaction first
// when the segment reached its threshold, and the loads of the segment's
// archive chunks and prompts that the model Step reads.
type modelCallPlan struct {
	compacts bool
	load     *dex.StepOptions
}

// planModelCall decides how the loop continues with the model.
func (flow *SubagentFlow) planModelCall(ctx dex.Context) modelCallPlan {
	state, segment := comm.Must(agentStateAttribute.Get(ctx)), comm.Must(contextSegmentAttribute.Get(ctx))
	loads := []dex.AttributeMapLoad{contextSegmentPromptsAttribute.Load(segment.PromptsKey())}
	for _, key := range agentloop.ArchiveChunksFrom(state, segment.FirstSequence) {
		loads = append(loads, archivedMessagesAttribute.Load(key))
	}
	isDue := agentloop.SegmentChange(segment, segment.Model, flow.settings, state.Turn, false) != ""
	return modelCallPlan{compacts: isDue, load: &dex.StepOptions{ExecuteLoadAttributeMapInstances: loads}}
}

// segmentRequest is the model request that replays the current segment with
// its frozen prompts, followed by the given transient entries, which are
// never stored.
func (flow *SubagentFlow) segmentRequest(ctx dex.Context, kind agentloop.ModelCallKind, prompts conversation.SegmentPrompts, read func(conversation.Sequence) conversation.Entry,
	transient ...conversation.Entry) conversation.ModelRequest {
	state, segment := comm.Must(agentStateAttribute.Get(ctx)), comm.Must(contextSegmentAttribute.Get(ctx))
	entries := make([]conversation.SequencedEntry, 0, int(state.LastSequence-segment.FirstSequence+1)+len(transient))
	for sequence := segment.FirstSequence; sequence <= state.LastSequence; sequence++ {
		entry := read(sequence)
		if kind == agentloop.ModelCallKindCompaction && modelprovider.ProfileOf(segment.Model).Capabilities.Replay == conversation.ReplayEncryptedOpaque {
			entry = entry.WithoutProtocolHistory()
		}
		entries = append(entries, conversation.SequencedEntry{Sequence: sequence, Entry: entry})
	}
	for index, entry := range transient {
		entries = append(entries, conversation.SequencedEntry{Sequence: state.LastSequence + conversation.Sequence(index) + 1, Entry: entry})
	}
	profile := modelprovider.ProfileOf(segment.Model)
	return conversation.ModelRequest{
		Endpoint: segment.Endpoint, Model: segment.Model, Effort: segment.EffectiveEffort(), Prompts: prompts,
		Messages:       conversation.AssembleRequest(entries, segment.CacheAnchor, profile.RequestShape()),
		PrefixMismatch: flow.settings.PrefixMismatch, CallID: modelCallID(ctx, kind), ToolTarget: agentloop.ToolTarget,
		WriteAssistant: discardText, WriteReasoning: discardText, WriteActivity: func(conversation.AgentEvent) error { return nil },
	}
}

func discardText(string) error { return nil }

// modelCallID is the stable identity of the model call of the calling Step.
func modelCallID(ctx dex.Context, kind agentloop.ModelCallKind) conversation.CallID {
	encoded := comm.Must(json.Marshal(struct {
		FlowID          string                  `json:"flow_id"`
		StepExecutionID string                  `json:"step_execution_id"`
		Kind            agentloop.ModelCallKind `json:"kind"`
	}{ctx.FlowID(), ctx.StepExecutionID(), kind}))
	return conversation.CallID(uuid.NewSHA1(uuid.NameSpaceOID, encoded).String())
}

// openFirstSegment opens the subagent's first segment on its frozen prompt:
// the segment_start with the repository context, then the task as the first
// user message.
func openFirstSegment(ctx dex.Context, config Config, repositoryContext, task string) {
	entry := conversation.SegmentStartEntry(agentloop.SegmentStart{RepositoryContext: repositoryContext}.Text(), "")
	first := appendEntry(ctx, entry)
	prompt := agentloop.SubagentPromptFor(config.Kind, config.Model)
	segment := agentloop.NewSegmentFor(nil, first, agentloop.SegmentReasonSessionStart, config.Model, prompt, config.Effort, entry.Bytes())
	segment.Endpoint = config.Endpoint
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	comm.MustDo(contextSegmentPromptsAttribute.Set(ctx, segment.PromptsKey(), prompt.Prompts))
	appendEntry(ctx, conversation.UserMessageEntry(task, nil))
}

// openNextSegment opens the segment after a compaction on the same frozen
// prompts: its segment_start carries the summary, the repository context and
// the task, so the task survives every compaction.
func openNextSegment(ctx dex.Context, summary, repositoryContext, task string) agentloop.ContextSegment {
	previous := comm.Must(contextSegmentAttribute.Get(ctx))
	purgeClosedReplay(ctx)
	prompts := comm.Must(contextSegmentPromptsAttribute.Get(ctx, previous.PromptsKey()))
	start := agentloop.SegmentStart{Summary: summary, RepositoryContext: repositoryContext, PendingRequest: comm.TruncateUTF8(task, conversation.MaximumUserMessageContentBytes)}
	entry := conversation.SegmentStartEntry(start.Text(), agentloop.SegmentMarker(agentloop.SegmentReasonCompaction))
	first := appendEntry(ctx, entry)
	prompt := agentloop.SessionPrompt{Prompts: prompts, Digest: previous.PromptDigest, ToolNames: previous.ToolNames}
	segment := agentloop.NewSegmentFor(&previous, first, agentloop.SegmentReasonCompaction, previous.Model, prompt, previous.Effort, entry.Bytes())
	segment.Endpoint = comm.Must(configAttribute.Get(ctx)).Endpoint
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	comm.MustDo(contextSegmentPromptsAttribute.Set(ctx, segment.PromptsKey(), prompts))
	comm.MustDo(contextSegmentPromptsAttribute.Delete(ctx, previous.PromptsKey()))
	return segment
}

// replyNext is where the loop goes after a recorded reply: routing its tool
// calls, another model call, a compaction, or the subagent's end.
type replyNext struct {
	routes, callsAgain, compacts bool
	finish                       *subagentFinish
}

// recordReply records a completed model call by its stop reason, which is
// read before the content. A reply without tool calls is the report; a refusal
// or an unexpected stop fails the subagent; a reply beyond the context window
// is discarded and forces a compaction.
func recordReply(ctx dex.Context, reply conversation.ModelReply, startedAt time.Time) replyNext {
	state, segment := comm.Must(agentStateAttribute.Get(ctx)), comm.Must(contextSegmentAttribute.Get(ctx))
	// The run's platform model usage, which its end reports for an agent review's billable record.
	state.ModelUsage = conversation.AddModelUsage(state.ModelUsage, reply.Metrics)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	segment.CacheAnchor = state.LastSequence
	if prompt, isKnown := reply.Metrics.PromptTokens(); isKnown {
		segment.PromptTokens = prompt
	}
	segment.OutputTokens, segment.PendingBytes = 0, 0
	if reply.Metrics.OutputTokens != nil {
		segment.OutputTokens = *reply.Metrics.OutputTokens
	}
	if reply.StopReason == conversation.StopReasonModelContextWindowExceeded {
		segment.ForceCompaction = true
		comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
		return replyNext{compacts: true}
	}
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	switch {
	case reply.StopReason == conversation.StopReasonRefusal:
		return replyNext{finish: &subagentFinish{Status: agenttasks.SubagentFailed, Failure: "the model declined the task (" + reply.RefusalCategory + ")"}}
	case len(reply.Blocks) == 0:
		return replyNext{finish: &subagentFinish{Status: agenttasks.SubagentFailed, Failure: "the model's reply was empty"}}
	case reply.StopReason != conversation.StopReasonEndTurn && reply.StopReason != conversation.StopReasonToolUse && reply.StopReason != conversation.StopReasonMaxTokens:
		return replyNext{finish: &subagentFinish{Status: agenttasks.SubagentFailed, Failure: "the model's reply stopped for the unexpected reason " + string(reply.StopReason)}}
	}
	summaries := make([]conversation.ToolCallSummary, 0, len(reply.ToolCalls))
	for _, call := range reply.ToolCalls {
		summaries = append(summaries, agentloop.ToolSummary(call))
	}
	appendEntry(ctx, conversation.ModelReplyEntry(reply, summaries, startedAt))
	state = comm.Must(agentStateAttribute.Get(ctx))
	if reply.StopReason == conversation.StopReasonMaxTokens {
		// A cut reply's tool calls never run; the loop continues with a fresh
		// call.
		limited, entries := agentloop.ContinueAfterOutputLimit(state, reply)
		comm.MustDo(agentStateAttribute.Set(ctx, limited))
		for _, entry := range entries {
			appendEntry(ctx, entry)
		}
		return replyNext{callsAgain: true}
	}
	if len(reply.ToolCalls) == 0 {
		comm.MustDo(agentStateAttribute.Set(ctx, state))
		return replyNext{finish: &subagentFinish{Status: agenttasks.SubagentCompleted, Report: comm.TruncateUTF8(reply.Text, maximumReportBytes)}}
	}
	state.Status, state.PendingToolCalls, state.PendingToolIndex = agentloop.AgentStatusRoutingTool, reply.ToolCalls, 0
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	return replyNext{routes: true}
}

// summarize asks the segment's model for the summary of a compaction
// request; a reply that calls a tool is asked again with tool_choice none, and
// a reply without a summary is a rejection a retry cannot change.
func (flow *SubagentFlow) summarize(ctx dex.Context, request conversation.ModelRequest) (string, error) {
	key, keyErr := subagentModelCredential(ctx, request.Model)
	if keyErr != nil {
		return "", keyErr
	}
	request.APIKey = key
	defer func() { request.APIKey = "" }()
	reply, err := flow.modelClient.Complete(ctx, request)
	if err == nil && (len(reply.ToolCalls) > 0 || reply.StopReason == conversation.StopReasonToolUse) {
		request.ToolChoiceNone = true
		reply, err = flow.modelClient.Complete(ctx, request)
	}
	if err != nil {
		return "", err
	}
	summary := agentloop.ExtractSummary(reply.Text)
	if summary == "" || reply.StopReason == conversation.StopReasonRefusal {
		return "", &conversation.ModelRejectedError{Code: "summary_unavailable"}
	}
	return summary, nil
}

// modelFailure is the bounded failure of a model call a retry cannot change:
// the rejection's class and status, never provider text.
func modelFailure(err error) string {
	var rejected *conversation.ModelRejectedError
	if !errors.As(err, &rejected) {
		return "the model API did not answer before the retries ran out"
	}
	if rejected.Status != 0 {
		return fmt.Sprintf("the model API rejected the request: %s, HTTP %d", rejected.Code, rejected.Status)
	}
	return "the model API rejected the request: " + rejected.Code
}

// lastReplyText is the text of the subagent's newest model reply in the
// loaded window, which a subagent that ends without its report passes on;
// "" when it has none.
func lastReplyText(ctx dex.Context) string {
	state := comm.Must(agentStateAttribute.Get(ctx))
	for sequence := state.LastSequence; sequence >= state.CurrentFirstSequence && sequence > 0; sequence-- {
		if entry := comm.Must(currentMessagesAttribute.Get(ctx, agentloop.SequenceKey(sequence))); entry.Kind == conversation.EntryKindModelReply {
			return entry.Text()
		}
	}
	return ""
}

// The best-effort live output of a subagent: its Session's Watch relays the
// AgentActivity of a running subagent as the subagent's one-line status.

// writeActivity writes an activity event; a failed write, such as after a
// Stop cancelled the Step, loses only the live event.
func writeActivity(ctx dex.Context, event conversation.AgentEvent) {
	event.Message = agentloop.CondenseActivityMessage(event.Message)
	dexflow.StreamWritten(ctx, agentActivityStream.Write(ctx, event))
}

// writeToolProgress reports the attempt of a tool call that starts running.
func writeToolProgress(ctx dex.Context, call conversation.ToolCall) {
	event := agentloop.ToolActivity(conversation.EventKindToolProgress, "Running "+string(call.Name)+".", call)
	attempt := ctx.Attempt()
	event.Attempt = &attempt
	writeActivity(ctx, event)
}

// modelProgress writes the best-effort activity of one model reply.
type modelProgress struct {
	ctx             dex.Context
	messageSequence conversation.Sequence
}

func (progress modelProgress) write(event conversation.AgentEvent) {
	event.MessageSequence = &progress.messageSequence
	writeActivity(progress.ctx, event)
}

func (progress modelProgress) writeActivity(event conversation.AgentEvent) error {
	progress.write(event)
	return nil
}

// subagentModelCredential fails closed until the direct copy of the frozen revision exists.
func subagentModelCredential(ctx dex.Context, model conversation.Model) (string, error) {
	if model.IsMock() {
		return "", nil
	}
	revision := dexflow.ValueOrZero(credentialRevisionAttribute.Get(ctx))
	envelope := dexflow.ValueOrZero(modelKeyAttribute.Get(ctx))
	if revision == 0 || envelope.Revision != revision || envelope.Key == "" || dexflow.ValueOrZero(credentialStopAttribute.Get(ctx)) {
		return "", modelcredential.ErrUnavailable
	}
	return envelope.Key, nil
}

// closedReplayOptions loads exact chunks of the last segment for terminal cleanup.
// CurrentMessages stays a whole-window load; archive instances are never scanned.
func closedReplayOptions(ctx dex.Context) *dex.StepOptions {
	options := *loopStepOptions
	if segment := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx)); segment != nil && modelprovider.ProfileOf(segment.Model).Capabilities.Replay == conversation.ReplayEncryptedOpaque {
		state := comm.Must(agentStateAttribute.Get(ctx))
		size := conversation.Sequence(agentloop.ArchiveMessageChunkSize)
		for first := agentloop.ArchiveChunkFirst(segment.FirstSequence); first <= state.LastSequence; first += size {
			options.ExecuteLoadAttributeMapInstances = append(options.ExecuteLoadAttributeMapInstances, archivedMessagesAttribute.Load(agentloop.SequenceKey(first)))
		}
	}
	return &options
}

// purgeClosedReplay runs under MessageWindowLock after a segment ends. The two
// existing message maps remain the sole owners of its canonical history.
func purgeClosedReplay(ctx dex.Context) {
	segment := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx))
	if segment == nil || modelprovider.ProfileOf(segment.Model).Capabilities.Replay != conversation.ReplayEncryptedOpaque {
		return
	}
	state := comm.Must(agentStateAttribute.Get(ctx))
	agentloop.PurgeModelReplay(state, segment.FirstSequence, state.LastSequence, agentloop.ModelReplayHistory{
		ReadCurrent:  func(key string) conversation.Entry { return comm.Must(currentMessagesAttribute.Get(ctx, key)) },
		WriteCurrent: func(key string, entry conversation.Entry) { comm.MustDo(currentMessagesAttribute.Set(ctx, key, entry)) },
		ReadArchive: func(key string) conversation.ArchivedEntryChunk {
			return comm.Must(archivedMessagesAttribute.Get(ctx, key))
		},
		WriteArchive: func(key string, chunk conversation.ArchivedEntryChunk) {
			comm.MustDo(archivedMessagesAttribute.Set(ctx, key, chunk))
		},
	})
}
