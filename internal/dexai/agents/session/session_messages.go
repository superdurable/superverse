// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/flowdesign"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// A turn begins with user messages: a queued message, an answer to pending
// questions or steered messages that interrupt the running turn. It ends when
// the Agent waits for the user again. agentloop computes the new AgentState
// and the entries of a turn as values; beginUserTurn and beginSteeredTurn
// store them in the commit of the calling Step.

// beginUserTurn starts the turn of user messages, records them and returns
// the sequence of the last one. A per-message effort raised in the previous
// turn falls back to the segment's base.
func beginUserTurn(ctx dex.Context, messages ...conversation.UserMessage) conversation.Sequence {
	state, entries := agentloop.StartUserTurn(comm.Must(agentStateAttribute.Get(ctx)), messages)
	return beginUserEntries(ctx, state, entries, len(messages))
}

// beginUserEntries commits a new turn and its immutable entries.
func beginUserEntries(ctx dex.Context, state agentloop.AgentState, entries []conversation.Entry, count int) conversation.Sequence {
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	var last conversation.Sequence
	for index, entry := range entries {
		sequence := appendEntry(ctx, entry)
		if index < count {
			last = sequence
		}
	}
	if segment := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx)); segment != nil && segment.CurrentEffort != "" {
		appendEntry(ctx, conversation.EffortEntry(segment.Effort))
		restored := comm.Must(contextSegmentAttribute.Get(ctx))
		restored.CurrentEffort = ""
		comm.MustDo(contextSegmentAttribute.Set(ctx, restored))
	}
	return last
}

// beginSteeredTurn interrupts the running turn for steered messages: it
// abandons the question or approval the turn waits for, answers the pending
// tool calls as superseded and begins one turn with the messages.
func beginSteeredTurn(ctx dex.Context, messages []conversation.PendingUserMessage) {
	interruptions := agentloop.SteeringInterruptions(
		dexflow.OptionalValue(pendingUserInputAttribute.Get(ctx)),
		dexflow.OptionalValue(pendingApprovalAttribute.Get(ctx)),
	)
	comm.MustDo(pendingApprovalAttribute.Delete(ctx))
	comm.MustDo(pendingTimerAttribute.Delete(ctx))
	comm.MustDo(pendingUserInputAttribute.Delete(ctx))
	for _, event := range interruptions {
		writeActivity(ctx, event)
	}
	interruptPendingTools(ctx, comm.Must(agentStateAttribute.Get(ctx)).PendingToolIndex, agentloop.CancelledBySteering)
	values := make([]conversation.UserMessage, 0, len(messages))
	for _, message := range messages {
		values = append(values, message.Value)
	}
	state, entries := agentloop.StartUserTurn(comm.Must(agentStateAttribute.Get(ctx)), values)
	for index, message := range messages {
		entries[index].Meta.SteeredBy = message.SteeredBy
	}
	sequence := beginUserEntries(ctx, state, entries, len(values))
	writeInputConsumption(ctx, nil, messages, &sequence)
	writeActivity(ctx, agentloop.SteeringAppliedEvent(len(messages)))
}

type archivedMessagesRPCOutput struct {
	Page  conversation.HistoryPage `json:"page"`
	Found bool                     `json:"found"`
}

func pendingUserMessages(messages []dex.ChannelMessage[conversation.PendingUserMessage]) []conversation.PendingUserMessage {
	result := make([]conversation.PendingUserMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, message.Value)
	}
	return result
}

func findPendingUserMessage(
	messages []dex.ChannelMessage[conversation.PendingUserMessage],
	messageID conversation.MessageID,
) (dex.ChannelMessage[conversation.PendingUserMessage], bool) {
	for _, message := range messages {
		if message.Value.MessageID == messageID {
			return message, true
		}
	}
	return dex.ChannelMessage[conversation.PendingUserMessage]{}, false
}

// The entry log is a sequence of entries. The newest live in CurrentMessages
// keyed by sequence; once agentloop.CurrentMessageLimit accumulate, the oldest
// chunk of agentloop.ArchiveMessageChunkSize moves to one immutable
// ArchivedMessages instance keyed by its first sequence. Chunks before the
// current segment beyond the retention limit are dropped. AgentState holds
// the sequence bounds.

// appendEntry records an entry at the next sequence and archives the oldest
// chunk when the current window is full. What an entry adds to the current
// segment's request counts toward its compaction trigger; a model reply is
// counted by the usage its call reported. What a user message's attachments
// show is decided against the segment's attachment budget and frozen into the
// entry (AdmitAttachments); without a segment, the first one's segment_start
// shows them.
func appendEntry(ctx dex.Context, entry conversation.Entry) conversation.Sequence {
	segment := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx))
	if segment != nil && entry.Kind == conversation.EntryKindUserMessage {
		var admitted agentloop.ContextSegment
		admitted, entry = agentloop.AdmitAttachments(*segment, entry)
		segment = &admitted
	}
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
	if segment != nil && entry.Kind != conversation.EntryKindModelReply {
		segment.PendingBytes += entry.Bytes()
		comm.MustDo(contextSegmentAttribute.Set(ctx, *segment))
	}
	if currentExecutor(ctx).Mode == remoteexec.ModeRemote {
		trimRetainedEntries(ctx)
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

// publicMessages projects the public entries of an archive chunk.
func publicMessages(entries []conversation.SequencedEntry) []conversation.SequencedMessage {
	messages := []conversation.SequencedMessage{}
	for _, entry := range entries {
		if message, isPublic := entry.Entry.PublicMessage(); isPublic {
			messages = append(messages, conversation.SequencedMessage{Sequence: entry.Sequence, Message: message})
		}
	}
	return messages
}

// retainedEntries reads entries from the CurrentMessages and ArchivedMessages
// instances the calling Step loaded. Its callers load what they read, so a
// missing entry is corrupt history.
func retainedEntries(ctx dex.Context, state agentloop.AgentState) func(conversation.Sequence) conversation.Entry {
	return func(sequence conversation.Sequence) conversation.Entry {
		entry, found := loadedEntry(ctx, state, sequence)
		if !found {
			panic(fmt.Sprintf("entry %d is missing from the loaded history", sequence))
		}
		return entry
	}
}

// entryReader reads one entry of the log, or reports it not found.
type entryReader func(dex.Context, agentloop.AgentState, conversation.Sequence) (conversation.Entry, bool)

// currentEntry reads one entry of CurrentMessages, which every Step that
// records entries loads in full; an archived entry is not found.
func currentEntry(ctx dex.Context, state agentloop.AgentState, sequence conversation.Sequence) (conversation.Entry, bool) {
	if sequence < state.CurrentFirstSequence {
		return conversation.Entry{}, false
	}
	return comm.Must(currentMessagesAttribute.Get(ctx, agentloop.SequenceKey(sequence))), true
}

// loadedEntry reads one entry if the calling Step loaded it: CurrentMessages
// is loaded in full, an archive chunk only where a Step selected it. An entry
// before the retained history, and one in an archive chunk the Step did not
// load, is not found.
func loadedEntry(ctx dex.Context, state agentloop.AgentState, sequence conversation.Sequence) (conversation.Entry, bool) {
	if entry, found := currentEntry(ctx, state, sequence); found {
		return entry, true
	}
	if sequence < state.FirstRetainedSequence {
		return conversation.Entry{}, false
	}
	first := (sequence-1)/conversation.Sequence(agentloop.ArchiveMessageChunkSize)*conversation.Sequence(agentloop.ArchiveMessageChunkSize) + 1
	chunk, err := archivedMessagesAttribute.Get(ctx, agentloop.SequenceKey(first))
	if dexflow.IsAttributeNotFound(err) || dexflow.IsAttributeMapNotLoaded(err) {
		return conversation.Entry{}, false
	}
	index := sequence - first
	comm.MustDo(err)
	if index >= conversation.Sequence(len(chunk.Entries)) || chunk.Entries[index].Sequence != sequence {
		panic(fmt.Sprintf("archived entry %d is missing from chunk %d", sequence, first))
	}
	return chunk.Entries[index].Entry, true
}

// trimRetainedEntries drops the archive chunks before the current segment
// beyond the retention limit.
func trimRetainedEntries(ctx dex.Context) {
	state := comm.Must(agentStateAttribute.Get(ctx))
	segmentFirst := state.LastSequence + 1
	if currentExecutor(ctx).Mode != remoteexec.ModeRemote {
		if segment := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx)); segment != nil {
			segmentFirst = segment.FirstSequence
		}
	}
	first := agentloop.RetainedFirstSequence(state, segmentFirst)
	if first == state.FirstRetainedSequence {
		return
	}
	for chunk := state.FirstRetainedSequence; chunk < first; chunk += conversation.Sequence(agentloop.ArchiveMessageChunkSize) {
		comm.MustDo(archivedMessagesAttribute.Delete(ctx, agentloop.SequenceKey(chunk)))
	}
	state.FirstRetainedSequence = first
	comm.MustDo(agentStateAttribute.Set(ctx, state))
}

// recordModelReply records a completed model call by its stop reason, which
// is read before the content, and returns where the turn continues. A refusal
// or an unexpected stop ends the turn; a reply beyond the context window is
// discarded and forces a compaction.
func recordModelReply(ctx dex.Context, reply conversation.ModelReply, startedAt time.Time, admission remoteexec.Fence) agentloop.Continuation {
	state, segment := comm.Must(agentStateAttribute.Get(ctx)), comm.Must(contextSegmentAttribute.Get(ctx))
	segment.CacheAnchor = state.LastSequence
	if prompt, isKnown := reply.Metrics.PromptTokens(); isKnown {
		segment.PromptTokens = prompt
	}
	segment.OutputTokens, segment.PendingBytes = 0, 0
	if reply.Metrics.OutputTokens != nil {
		segment.OutputTokens = *reply.Metrics.OutputTokens
	}
	if len(reply.Blocks) == 0 && reply.StopReason != conversation.StopReasonRefusal && reply.StopReason != conversation.StopReasonModelContextWindowExceeded {
		// An empty assistant message cannot be replayed.
		reply.StopReason = "empty_reply"
	}
	switch reply.StopReason {
	case conversation.StopReasonModelContextWindowExceeded:
		segment.ForceCompaction = true
		comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
		return agentloop.ContinueCompactContext
	case conversation.StopReasonEndTurn, conversation.StopReasonToolUse, conversation.StopReasonMaxTokens:
	case conversation.StopReasonRefusal:
		comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
		recordTurnFailure(ctx, modelRefused.failure(reply.RefusalCategory))
		return agentloop.ContinueAwaitUser
	default:
		comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
		recordTurnFailure(ctx, modelUnexpectedStop.failure(string(reply.StopReason)))
		return agentloop.ContinueAwaitUser
	}
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	summaries := make([]conversation.ToolCallSummary, 0, len(reply.ToolCalls))
	for _, call := range reply.ToolCalls {
		summaries = append(summaries, agentloop.ToolSummary(call))
	}
	appendEntry(ctx, conversation.ModelReplyEntry(reply, summaries, startedAt))
	if reply.StopReason == conversation.StopReasonMaxTokens {
		limited, entries := agentloop.ContinueAfterOutputLimit(comm.Must(agentStateAttribute.Get(ctx)), reply)
		comm.MustDo(agentStateAttribute.Set(ctx, limited))
		for _, entry := range entries {
			appendEntry(ctx, entry)
		}
		return agentloop.ContinueCompactContext
	}
	state, next, notice := agentloop.ContinueAfterModelReply(comm.Must(agentStateAttribute.Get(ctx)), reply)
	if len(state.PendingToolCalls) > 0 {
		state.PendingToolsAdmission = admission
	}
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	if notice != nil {
		appendEntry(ctx, *notice)
	}
	return next
}

func modelCallID(ctx dex.Context, kind agentloop.ModelCallKind) conversation.CallID {
	encoded, err := json.Marshal(struct {
		FlowID          string                  `json:"flow_id"`
		StepExecutionID string                  `json:"step_execution_id"`
		Kind            agentloop.ModelCallKind `json:"kind"`
	}{ctx.FlowID(), ctx.StepExecutionID(), kind})
	if err != nil {
		panic(fmt.Sprintf("encode model call identity: %v", err))
	}
	return conversation.CallID(uuid.NewSHA1(uuid.NameSpaceOID, encoded).String())
}

// replaceTodoList stores the progress list a write_todos call writes and
// returns its revision.
func replaceTodoList(ctx dex.Context, items []agentloop.TodoItem) int64 {
	list := agentloop.NextTodoList(dexflow.OptionalValue(todoListAttribute.Get(ctx)), items)
	comm.MustDo(todoListAttribute.Set(ctx, list))
	writeSnapshotRequired(ctx)
	return list.Revision
}

// raiseEffort records the per-message effort a set_effort call asks for,
// clamped to the model's ceiling, unless the segment already runs at it.
func raiseEffort(ctx dex.Context, effort conversation.Effort) conversation.Effort {
	segment := comm.Must(contextSegmentAttribute.Get(ctx))
	if effort = effort.AtMost(modelprovider.ProfileOf(segment.Model).MaxEffort); effort == segment.EffectiveEffort() {
		return effort
	}
	appendEntry(ctx, conversation.EffortEntry(effort))
	segment = comm.Must(contextSegmentAttribute.Get(ctx))
	segment.CurrentEffort = effort
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	return effort
}

// Beside the turn: the user's Stop and the notifications from outside it.

// agentInterruption is the input of InterruptAgent: the stopped turn, whether
// the Stop cancelled its work in flight and so ends it at once, and that work.
type foregroundStop struct {
	CallID conversation.CallID `json:"call_id"`
	Lease  *workspace.Lease    `json:"lease,omitempty"`
}

type agentInterruption struct {
	Foreground         *foregroundStop        `json:"foreground,omitempty"`
	PreserveBackground bool                   `json:"preserve_background,omitempty"`
	RequestedAt        time.Time              `json:"requested_at,omitempty"`
	Actor              *conversation.Actor    `json:"actor,omitempty"`
	Turn               int64                  `json:"turn"`
	EndsTurn           bool                   `json:"ends_turn,omitempty"`
	Running            *agentloop.RunningWork `json:"running,omitempty"`
}

// Capture only the original command identity and accepted credential-free
// lease, never arguments. Retries cannot borrow a replacement command's lease.
func captureForegroundStop(ctx dex.Context, interruption agentInterruption) agentInterruption {
	if running := interruption.Running; !interruption.EndsTurn || running == nil || running.Kind != agentloop.RunningTool || running.ToolName != conversation.ToolBash {
		return interruption
	}
	state := comm.Must(agentStateAttribute.Get(ctx))
	if state.PendingToolIndex < len(state.PendingToolCalls) {
		interruption.Foreground = &foregroundStop{CallID: state.PendingToolCalls[state.PendingToolIndex].ID,
			Lease: agentloop.AdmittedLease(dexflow.ValueOrZero(workspaceUseAttribute.Get(ctx)), dexflow.ValueOrZero(workspaceAttribute.Get(ctx)))}
	}
	return interruption
}

func originalForegroundStop(ctx dex.Context, interruption agentInterruption) (*foregroundStop, error) {
	if interruption.Foreground == nil {
		// Legacy payloads have no captured identity. Only the exact pending Stop
		// of the still-running stopped turn permits its snapshot-derived fallback.
		if matchingInterrupt(ctx, interruption) == nil {
			return nil, errors.New("foreground Stop identity is no longer provable; original command settlement remains required")
		}
		interruption = captureForegroundStop(ctx, interruption)
	}
	stopped := interruption.Foreground
	if stopped == nil || stopped.CallID == "" || stopped.Lease == nil {
		return nil, errors.New("foreground Stop has no proven original command lease; command settlement remains required")
	}
	if err := stopped.Lease.Validate(); err != nil {
		return nil, errors.New("foreground Stop original command lease is invalid; command settlement remains required")
	}
	return stopped, nil
}

// setRunningWork records the interruptible work the conversation schedules, or
// nil in the commit of the Step that ends it.
func setRunningWork(ctx dex.Context, work *agentloop.RunningWork) {
	if state := comm.Must(agentStateAttribute.Get(ctx)); state.Running != nil || work != nil {
		state.Running = work
		comm.MustDo(agentStateAttribute.Set(ctx, state))
	}
}

// takeAgentInterrupt deletes a recorded Stop and reports whether it stops the
// current turn; a Stop of an earlier turn is stale. AwaitUser writes the marker
// before consumption; the caller holds MessageWindowLock and interruptLock.
func takeAgentInterrupt(ctx dex.Context, writeMarker bool) *agentloop.AgentInterrupt {
	interrupt := dexflow.OptionalValue(agentInterruptAttribute.Get(ctx))
	if interrupt == nil {
		return nil
	}
	if interrupt.Turn != comm.Must(agentStateAttribute.Get(ctx)).Turn {
		comm.MustDo(agentInterruptAttribute.Delete(ctx))
		return nil
	}
	if writeMarker {
		writeStopMarker(ctx, interrupt)
	}
	comm.MustDo(agentInterruptAttribute.Delete(ctx))
	return interrupt
}

// endStoppedTurn ends the turn the user stopped in the commit of the Step that
// applies the Stop: a result a stopped wave already published is kept, every
// other pending call is answered interrupted_by_user, nothing stays in flight
// or waits, automatic turns pause and the Stop leaves its marker.
func endStoppedTurn(ctx dex.Context, published []parallelToolResult, interrupt *agentloop.AgentInterrupt) {
	state := comm.Must(agentStateAttribute.Get(ctx))
	inFlight := 0
	switch running := state.Running; {
	case running == nil:
	case running.Kind == agentloop.RunningWave:
		inFlight = running.BatchSize
	case running.Kind != agentloop.RunningModel:
		inFlight = 1
	}
	state, calls, isNewPause := agentloop.StopTurn(state)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	for index, call := range calls {
		result := agentloop.InterruptedToolResult(call, index < inFlight)
		timing := agentloop.ToolMessageTiming{}
		for _, branch := range published {
			if branch.Call.ID == call.ID && !branch.WorkspaceLost {
				result, timing = branch.Result, branch.Timing
			}
		}
		appendToolResult(ctx, call, result, timing)
		writeActivity(ctx, agentloop.ToolTerminalEvent(call, result))
	}
	comm.MustDo(pendingApprovalAttribute.Delete(ctx))
	comm.MustDo(pendingTimerAttribute.Delete(ctx))
	endToolWork(ctx)
	comm.MustDo(pendingUserInputAttribute.Delete(ctx))
	writeStopMarker(ctx, interrupt)
	if isNewPause && !interrupt.PreserveBackground {
		appendEntry(ctx, agentloop.PausedEntry())
	}
	// The running subagents are stopped by InterruptAgent; the ones not
	// admitted yet stop here.
	stopStartingSubagents(ctx)
	if interrupt.PreserveBackground {
		writeSnapshotRequired(ctx)
	} else {
		writeActivity(ctx, interruptActivity(ctx, "Stopped by the user."))
	}
}

// publishPreviewHandoff publishes each successful push handoff in the
// commit of the Step that records its tool result, and reports whether it builds:
// while automatic deployment is off it is never deployed, also not later.
func publishPreviewHandoff(ctx dex.Context, handoff *agentloop.PreviewHandoff) bool {
	handoff.PublishedSequence = handoff.Sequence
	comm.MustDo(previewHandoffAttribute.Set(ctx, *handoff))
	return dexflow.ValueOrZero(previewAutoDeployAttribute.Get(ctx))
}

// publishedWaveResults reads and clears the results the branches of a stopped
// wave published before the Stop; the FinishInterrupt movement loaded them.
func publishedWaveResults(ctx dex.Context, running *agentloop.RunningWork) []parallelToolResult {
	if running == nil || running.Kind != agentloop.RunningWave {
		return nil
	}
	var published []parallelToolResult
	for _, message := range comm.Must(parallelToolResultsChannel.PendingMessages(ctx, running.BatchID)) {
		published = append(published, message.Value)
		comm.MustDo(parallelToolResultsChannel.Delete(ctx, running.BatchID, message.MessageID))
	}
	return published
}

// publishNotification admits one notification of an ACTIVE Session under the
// receipts lock and reports whether it is admitted: a new ID is published and
// recorded in the receipts, which deduplicate a repeated one; a full Channel
// refuses it, and its source keeps it readable.
func publishNotification(ctx dex.Context, notification conversation.SessionNotification, receipts []string) bool {
	isNew, isAdmitted, receipts := agentloop.AdmitNotification(receipts, sessionNotificationsChannel.Size(ctx), notification.ID)
	if isNew {
		comm.MustDo(sessionNotificationsChannel.Publish(ctx, notification))
		comm.MustDo(notificationReceiptsAttribute.Set(ctx, receipts))
	}
	return isAdmitted
}

// appendNotifications records the consumed notifications as user-role
// entries, as the Session holds them at its HEAD: a delivery failure of
// another commit is a notice (AtHead). It returns the recorded ones with their
// sequences. An ordinary ready Preview enters no entry: its report already
// held the instance it serves on (ReportSessionNotification). A ready Preview
// that resolves an earlier notice enters the conversation. A task's end holds
// the Sandbox for another Sandbox hold. The caller holds the Workspace lock.
func (flow *SessionAgentFlow) appendNotifications(ctx dex.Context, notifications []conversation.SessionNotification) ([]conversation.SessionNotification, []conversation.Sequence) {
	head := comm.Must(headCommitAttribute.Get(ctx))
	recorded, sequences := make([]conversation.SessionNotification, 0, len(notifications)), make([]conversation.Sequence, 0, len(notifications))
	for _, notification := range notifications {
		switch {
		case notification.IsSilentPreviewReady():
			continue
		case notification.Kind == conversation.NotificationKindTask:
			holdSandbox(ctx, "", flow.sandboxHold)
		}
		notification = notification.AtHead(head)
		recorded, sequences = append(recorded, notification), append(sequences, appendEntry(ctx, conversation.NotificationEntry(notification)))
	}
	if len(notifications) > 0 {
		writeSnapshotRequired(ctx)
	}
	return recorded, sequences
}

// consumeIdleNotifications records the notifications AwaitUser consumed and
// reports whether they open an automatic turn; at a bound, or after a Stop,
// automatic turns pause, which is recorded once.
func (flow *SessionAgentFlow) consumeIdleNotifications(ctx dex.Context, notifications []conversation.SessionNotification, isStopped bool) bool {
	recorded, sequences := flow.appendNotifications(ctx, notifications)
	state, decision := agentloop.ConsumeIdleNotifications(comm.Must(agentStateAttribute.Get(ctx)), recorded, sequences, flow.automaticTurns, isStopped)
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	if decision.Paused {
		appendEntry(ctx, agentloop.PausedEntry())
	}
	return decision.OpensTurn
}

// writeStopMarker records a Stop once. Its caller holds MessageWindowLock and
// interruptLock and owns the pending Stop, or consumes it in this same commit.
// Consuming paths may set MarkerWritten then delete the Attribute in that atomic commit.
func writeStopMarker(ctx dex.Context, interrupt *agentloop.AgentInterrupt) {
	if interrupt.MarkerWritten {
		return
	}
	if !interrupt.PreserveBackground {
		appendEntry(ctx, agentloop.StoppedEntry(interrupt.Actor))
	}
	interrupt.MarkerWritten = true
	comm.MustDo(agentInterruptAttribute.Set(ctx, *interrupt))
}

// matchingInterrupt fences settlement by the exact admitted Stop identity;
// a later Stop of the same turn belongs to its own completion, never this one.
func matchingInterrupt(ctx dex.Context, interruption agentInterruption) *agentloop.AgentInterrupt {
	pending := dexflow.OptionalValue(agentInterruptAttribute.Get(ctx))
	if pending == nil || pending.Turn != interruption.Turn || pending.Turn != comm.Must(agentStateAttribute.Get(ctx)).Turn {
		return nil
	}
	if interruption.RequestedAt.IsZero() {
		// A legacy ending Step can only own the one still-unwritten ending Stop.
		// A later Stop of this ended turn is task-only; a running turn increments Turn.
		if !interruption.EndsTurn || !pending.EndsTurn || pending.MarkerWritten {
			return nil
		}
	} else if !pending.RequestedAt.Equal(interruption.RequestedAt) {
		return nil
	}
	return pending
}

// finishLegacyTaskInterrupt preserves only the old idempotent starting-task
// cleanup. It never changes marker ownership, consumes a Stop or uses input.Actor.
func finishLegacyTaskInterrupt(ctx dex.Context, interruption agentInterruption) {
	pending := dexflow.OptionalValue(agentInterruptAttribute.Get(ctx))
	if pending != nil && !pending.EndsTurn && pending.Turn == interruption.Turn && pending.Turn == comm.Must(agentStateAttribute.Get(ctx)).Turn {
		stopStartingSubagents(ctx)
	}
}

// admitInterrupt records the user's Stop of the turn the user saw, with what
// it stops: the running turn, or the running tasks while no turn runs. A Stop
// of an earlier turn, one with nothing running and one to a Session that is
// not ACTIVE are refused; one repeated while any Stop of the turn is pending
// joins it and records nothing. The caller holds the AgentInterrupt lock.
func admitInterrupt(ctx dex.Context, request agentloop.InterruptAgentRequest) (agentInterruption, agentloop.InterruptPlan) {
	state := comm.Must(agentStateAttribute.Get(ctx))
	pending := dexflow.OptionalValue(agentInterruptAttribute.Get(ctx))
	// A takeover is not a user Stop: an explicit Stop may replace its pending
	// interruption and stop background commands under its own stable identity.
	if pending != nil && pending.PreserveBackground {
		pending = nil
	}
	hasRunningTasks := len(agenttasks.RunningBackgroundCommands(loadedBackgroundCommands(ctx))) > 0 || len(agenttasks.UnfinishedSubagents(loadedSubagents(ctx), time.Now())) > 0
	plan := agentloop.PlanInterrupt(state, request, hasRunningTasks, pending)
	if currentExecutor(ctx).Mode == remoteexec.ModeRemote && request.Turn == state.Turn && !plan.Admitted {
		plan.Admitted = true
		plan.Joined = pending != nil && pending.Turn == state.Turn
	}
	switch {
	case !acceptsUserCommand(ctx):
		return agentInterruption{}, agentloop.InterruptPlan{}
	case !plan.Admitted || plan.Joined:
		return agentInterruption{}, plan
	}
	requestedAt := time.Now().UTC()
	comm.MustDo(agentInterruptAttribute.Set(ctx, agentloop.AgentInterrupt{Turn: state.Turn, RequestedAt: requestedAt, EndsTurn: plan.EndsTurn, Actor: request.Actor}))
	return captureForegroundStop(ctx, agentInterruption{RequestedAt: requestedAt, Actor: request.Actor, Turn: state.Turn, EndsTurn: plan.EndsTurn, Running: state.Running}), plan
}

// stopStartingSubagentsOf stops, in the commit of the FinishInterrupt of a Stop
// that does not end its turn at once, the subagents not admitted yet while that
// Stop is still pending. Once the turn's next boundary applied the Stop, which
// stopped the turn's starting subagents itself, a later turn may have admitted
// new ones, and they are none of this Stop's: a slow InterruptAgent must not
// deny a later start that the model reads as started.
func stopStartingSubagentsOf(ctx dex.Context, interruption agentInterruption) {
	if matchingInterrupt(ctx, interruption) != nil {
		stopStartingSubagents(ctx)
	}
}

// interruptStepOptions are the options of one InterruptAgent: a few idempotent
// Sandbox stops under a heartbeat, without a lock, and its failure route.
func interruptStepOptions(failure *dex.ExecuteFailure) *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: 3 * time.Minute, HeartbeatTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteRetry: dexflow.StepRetry(6 * time.Minute), ExecuteLoadAttributeMaps: taskMaps, ExecuteFailure: failure}
}

// interruptMovementOptions are the options of the InterruptAgent of one Stop,
// whose failure route to FinishInterrupt carries that Stop's loads.
func interruptMovementOptions(interruption agentInterruption) *dex.StepOptions {
	return interruptStepOptions(dex.ProceedToOnExecuteFailure(FinishInterrupt{}, finishInterruptOptions(interruption)))
}

// finishInterruptOptions load, for a Stop that ends a wave, the results its
// branches published before the Stop; FinishInterrupt otherwise needs only its
// registered options.
func finishInterruptOptions(interruption agentInterruption) *dex.StepOptions {
	running := interruption.Running
	if !interruption.EndsTurn || running == nil || running.Kind != agentloop.RunningWave {
		return nil
	}
	return &dex.StepOptions{ExecuteLoadChannelMapInstances: []dex.ChannelMapLoad{parallelToolResultsChannel.LoadMessages(running.BatchID)}}
}

// interruptActivity is the activity of a Stop; after InterruptAgent exhausted
// its retries it adds why running tasks may not have stopped.
func interruptActivity(ctx dex.Context, message string) conversation.AgentEvent {
	if detail := dexflow.RecoveryDetail(ctx); detail != "" {
		message += " Stopping the running tasks failed: " + detail
	}
	return conversation.AgentEvent{Kind: conversation.EventKindAgentInterrupted, Message: message}
}

func remoteIdleStepOptions() *dex.StepOptions {
	options := *awaitUserStepOptions
	options.WaitForLoadChannels = []dex.ChannelDef{remoteInboxChannel}
	options.ExecuteLoadChannels = []dex.ChannelDef{remoteInboxChannel}
	options.WaitForLockAttributes = []dex.AttributeLock{executorLock, interruptLock}
	return &options
}

func remoteStepOptions() *dex.StepOptions {
	options := *checkSteeredStepOptions
	options.WaitForLoadChannels = []dex.ChannelDef{remoteInboxChannel}
	options.ExecuteLoadChannels = []dex.ChannelDef{remoteInboxChannel}
	options.WaitForLockAttributes = []dex.AttributeLock{executorLock, interruptLock}
	return &options
}

func remoteHeadCount(ctx dex.Context) int {
	messages := comm.Must(remoteInboxChannel.PendingMessages(ctx))
	count := 1
	if len(messages) > 0 && messages[0].Value.Report != nil && messages[0].Value.Report.ToolCall != nil {
		for count < min(4, len(messages)) && messages[count].Value.Report != nil && messages[count].Value.Report.ToolCall != nil {
			count++
		}
	}
	return count
}

func (flow *SessionAgentFlow) executeRemote(ctx dex.Context) agentloop.Continuation {
	reconcileExecutorWake(ctx)
	reports := comm.Must(remoteInboxChannel.GetConditionResults(ctx))
	if interrupt := takeAgentInterrupt(ctx, false); interrupt != nil {
		endStoppedTurn(ctx, nil, interrupt)
		if len(reports) > 0 {
			return flow.consumeRemote(ctx, reports, true)
		}
		return ""
	}
	flow.appendNotifications(ctx, comm.Must(sessionNotificationsChannel.GetConditionResults(ctx)))
	if steered := comm.Must(steeredUserMessagesChannel.GetConditionResults(ctx)); len(steered) > 0 {
		beginSteeredTurn(ctx, steered)
	}
	return flow.consumeRemote(ctx, reports, false)
}

func (flow *SessionAgentFlow) consumeRemote(ctx dex.Context, items []remoteexec.InboxItem, idle bool) agentloop.Continuation {
	executor := currentExecutor(ctx)
	var calls []conversation.ToolCall
	var summaries []conversation.ToolCallSummary
	var admission remoteexec.Fence
	for _, item := range items {
		if item.Report == nil {
			continue
		}
		report := *item.Report
		consumeReport(ctx, report)
		if !executor.MatchesReport(report) {
			if report.ToolCall != nil {
				call := report.Call()
				appendEntry(ctx, remoteexec.ToolEntry([]conversation.ToolCall{call}, []conversation.ToolCallSummary{agentloop.ToolSummary(call)}, report.ConnectionID, time.Now()))
				appendToolResult(ctx, call, agentloop.InterruptedToolResult(call, false), agentloop.ToolMessageTiming{})
			}
			continue
		}
		opensTurn := report.ToolCall != nil
		for _, event := range report.Events {
			switch event.Kind {
			case remoteexec.EventAssistantMessage, remoteexec.EventUserMessage, remoteexec.EventImportedMessage:
				opensTurn = true
			}
		}
		if idle && opensTurn {
			state, _ := agentloop.StartUserTurn(comm.Must(agentStateAttribute.Get(ctx)), nil)
			state.TurnRequestSequences = nil
			comm.MustDo(agentStateAttribute.Set(ctx, state))
			idle = false
		}
		for _, event := range report.Events {
			if entry, public := event.Entry(report.Owner, report.ConnectionID, time.Now()); public {
				sequence := appendEntry(ctx, entry)
				if entry.Role == conversation.EntryRoleUser {
					state := comm.Must(agentStateAttribute.Get(ctx))
					state = agentloop.AddTurnRequest(state, sequence)
					comm.MustDo(agentStateAttribute.Set(ctx, state))
				}
			} else {
				activity := conversation.AgentEvent{Kind: conversation.EventKindSnapshotRequired, Message: agentloop.CondenseActivityMessage(event.Text)}
				if event.EntrySequence > 0 {
					sequence := event.EntrySequence
					activity.MessageSequence = &sequence
				}
				writeActivity(ctx, activity)
			}
		}
		if report.ToolCall != nil {
			call := report.Call()
			calls, summaries = append(calls, call), append(summaries, agentloop.ToolSummary(call))
			admission = remoteexec.Fence{Epoch: report.Epoch, Generation: report.Generation}
		}
		if report.TurnCompleted && !idle {
			setRunningWork(ctx, nil)
			return agentloop.ContinueAwaitUser
		}
	}
	if len(calls) > 0 {
		appendEntry(ctx, remoteexec.ToolEntry(calls, summaries, executor.Remote.ConnectionID, time.Now()))
		state := comm.Must(agentStateAttribute.Get(ctx))
		state.PendingToolCalls, state.PendingToolIndex = calls, 0
		state.PendingToolsAdmission = admission
		state.Status, state.Running = agentloop.AgentStatusRoutingTool, nil
		comm.MustDo(agentStateAttribute.Set(ctx, state))
		return agentloop.ContinueRouteTool
	}
	// Public events keep an opened turn waiting on its remote executor;
	// stale reports and delivery notices alone leave an idle Session idle.
	if !idle {
		return agentloop.ContinueStartTurn
	}
	return ""
}

// consumeReport advances Executor.Consumed in the commit that consumes report;
// its tool call, if any, is settled by its tool_result (SettledThrough).
func consumeReport(ctx dex.Context, report remoteexec.RemoteReport) {
	if executor, changed := currentExecutor(ctx).Consume(report); changed {
		comm.MustDo(executorAttribute.Set(ctx, executor))
	}
}

// A pre-RAEXEC null serial input proves no original identity. This fresh
// failure boundary preserves pending work and never fabricates a tool result.
type ambiguousToolIdentityError struct{}

func (ambiguousToolIdentityError) Error() string {
	return "A previous tool invocation has no original admission identity. Its effect is unknown and automatic replay was refused. Reconcile the operation through Project diagnostics before continuing."
}

func ambiguousToolInvocation(ctx dex.Context) agentloop.PlatformIncident {
	unknown := ambiguousToolIdentityError{}
	appendEntry(ctx, conversation.NoticeEntry(conversation.EntryRoleSystem, unknown.Error(), "Tool identity requires reconciliation"))
	writeSnapshotRequired(ctx)
	incident := agentloop.PlatformIncident{Code: toolfailure.ToolIdentityUnavailable.String(), Owner: toolfailure.ToolIdentityUnavailable.Owner().String(), Effect: toolfailure.EffectUnknown.String(), ErrorType: fmt.Sprintf("%T", unknown), At: time.Now().UTC()}
	return incident
}

// Original identity is matched against the atomically settled cursor, not history.
func currentToolInvocation(ctx dex.Context, input toolInvocation) (toolInvocation, bool) {
	state := comm.Must(agentStateAttribute.Get(ctx))
	if input.Call.ID == "" || input.Index != state.PendingToolIndex || input.Index < 0 || input.Index >= len(state.PendingToolCalls) || input.Call != state.PendingToolCalls[input.Index] {
		return input, false
	}
	legacy := input.Turn == 0 && input.Admission == (remoteexec.Fence{})
	return input, legacy || input.Turn == state.Turn && input.Admission == state.PendingToolsAdmission
}

// Legacy waves retain their call and slot even when Turn and Admission are absent.
func currentParallelInvocation(ctx dex.Context, input parallelToolExecutionInput) bool {
	state := comm.Must(agentStateAttribute.Get(ctx))
	if input.Call.ID == "" || input.Index < state.PendingToolIndex || input.Index >= len(state.PendingToolCalls) || input.Call != state.PendingToolCalls[input.Index] {
		return false
	}
	running := state.Running
	if running == nil || running.Kind != agentloop.RunningWave || running.BatchID != input.ResultInstance || input.Index >= state.PendingToolIndex+running.BatchSize {
		return false
	}
	legacy := input.Turn == 0 && input.Admission == (remoteexec.Fence{})
	return legacy || input.Turn == state.Turn && input.Admission == state.PendingToolsAdmission
}

func currentParallelBatch(ctx dex.Context, batch agentloop.ToolBatchState) bool {
	state := comm.Must(agentStateAttribute.Get(ctx))
	running := state.Running
	if running == nil || running.Kind != agentloop.RunningWave || running.BatchID != batch.BatchID || running.BatchSize != len(batch.Records) || state.PendingToolIndex != batch.FirstIndex {
		return false
	}
	for _, record := range batch.Records {
		if record.Index < batch.FirstIndex || record.Index >= len(state.PendingToolCalls) || record.Call != state.PendingToolCalls[record.Index] {
			return false
		}
	}
	return true
}

// sendMessage owns the conversation data operation of SendMessage: it queues
// the message unless questions wait for their answers. It admits at most
// conversation.MaximumMessageAttachments attachments and blank text only with
// attachments. A message with attachments is refused while a local agent
// executes the Session, since the remote delivery carries text only; one
// queued before a switch to it is delivered with a line naming what it left
// out (remoteexec). A Session that
// still waits at a requirements card from before cards stopped waiting
// records that card as the next version, as a card is recorded now when the
// Agent shows it, and takes the message as the card's reply, which AwaitUser
// consumes; remove that branch once no Session holds such a card.
func sendMessage(ctx dex.Context, input conversation.PendingUserMessage) (*dex.RPCResult[messageAdmission], error) {
	if !acceptsUserCommand(ctx) {
		return &dex.RPCResult[messageAdmission]{}, nil
	}
	switch attachments := len(input.Value.Attachments); {
	case attachments > conversation.MaximumMessageAttachments, attachments == 0 && strings.TrimSpace(input.Value.Content) == "":
		return &dex.RPCResult[messageAdmission]{}, nil
	case attachments > 0 && currentExecutor(ctx).Mode == remoteexec.ModeRemote:
		return &dex.RPCResult[messageAdmission]{Output: messageAdmission{AttachmentsRefused: true}}, nil
	}
	accepted := &dex.RPCResult[messageAdmission]{Output: messageAdmission{Accepted: true}}
	if pending := dexflow.OptionalValue(pendingUserInputAttribute.Get(ctx)); pending != nil {
		if pending.Kind != agentloop.PendingUserInputKindRequirements {
			return &dex.RPCResult[messageAdmission]{}, nil
		}
		version := recordWaitingCard(ctx, *pending.Requirements)
		comm.MustDo(pendingUserInputAttribute.Delete(ctx))
		comm.MustDo(answeredUserInputsChannel.Publish(ctx, agentloop.AnswerRequirementsCard(*pending, input.Value, version)))
		return accepted, nil
	}
	comm.MustDo(queuedUserMessagesChannel.Publish(ctx, input))
	return accepted, nil
}

// recordWaitingCard records the card an old Session waits at over the current
// version, with the Flow design it left in the pending slot, and returns the
// version it recorded.
func recordWaitingCard(ctx dex.Context, card agentloop.Requirements) int64 {
	stored := dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx))
	recorded := agentloop.RecordRequirements(stored, card, dexflow.ValueOrZero(builtFlowDesignAttribute.Get(ctx)), time.Now().UTC())
	var design *flowdesign.Design
	if card.Design != nil {
		shown := comm.Must(flowDesignsAttribute.Get(ctx, string(flowdesign.SlotPending)))
		if shown.Digest() != card.Design.Digest {
			panic("the pending flow design does not match the card's reference")
		}
		design = &shown
	}
	recordFlowDesign(ctx, stored, recorded.Spec.Design, design)
	comm.MustDo(confirmedRequirementsAttribute.Set(ctx, recorded))
	return recorded.Revision
}

// steerMessage owns the conversation data operation of SteerMessage.
func steerMessage(ctx dex.Context, input conversation.SteerMessageRequest) (*dex.RPCResult[bool], error) {
	message, found := findPendingUserMessage(comm.Must(queuedUserMessagesChannel.PendingMessages(ctx)), input.MessageID)
	if !acceptsUserCommand(ctx) || !found {
		return rpcAccepted(false), nil
	}
	comm.MustDo(queuedUserMessagesChannel.Delete(ctx, message.MessageID))
	message.Value.SteeredBy = input.Actor
	comm.MustDo(steeredUserMessagesChannel.Publish(ctx, message.Value))
	return rpcAccepted(true), nil
}

// deleteQueuedMessage owns the conversation data operation of DeleteQueuedMessage.
func deleteQueuedMessage(ctx dex.Context, input conversation.DeleteQueuedMessageRequest) (*dex.RPCResult[queuedMessageDeletion], error) {
	message, found := findPendingUserMessage(comm.Must(queuedUserMessagesChannel.PendingMessages(ctx)), input.MessageID)
	result := queuedMessageDeletion{}
	if acceptsUserCommand(ctx) && found {
		author := message.Value.Value.Actor
		result.NotAuthor = !input.IsOwner && (input.Actor.AccountID == "" || author == nil || author.AccountID != input.Actor.AccountID)
		if !result.NotAuthor {
			comm.MustDo(queuedUserMessagesChannel.Delete(ctx, message.MessageID))
			result.Deleted = true
		}
	}
	return &dex.RPCResult[queuedMessageDeletion]{Output: result}, nil
}

// getSessionSnapshot owns the conversation data operation of GetSnapshot.
func (flow *SessionAgentFlow) getSessionSnapshot(ctx dex.Context) (*dex.RPCResult[agentloop.SessionSnapshot], error) {
	snapshot := agentloop.SessionSnapshot{Session: comm.Must(flow.GetSessionSummary(ctx, nil)).Output}
	state, err := agentStateAttribute.Get(ctx)
	if dexflow.IsAttributeNotFound(err) {
		return &dex.RPCResult[agentloop.SessionSnapshot]{Output: snapshot}, nil
	}
	read := func(sequence conversation.Sequence) conversation.Entry {
		return comm.Must(currentMessagesAttribute.Get(ctx, agentloop.SequenceKey(sequence)))
	}
	snapshot.Agent = agentSnapshot(ctx, comm.Must(state, err), read, dexflow.OptionalValue(todoListAttribute.Get(ctx)))
	snapshot.Agent.Description.ConnectorSlugs = connectorContributionSlugs(ctx, snapshot.Agent.Description.ConnectorSlugs)
	return &dex.RPCResult[agentloop.SessionSnapshot]{Output: snapshot}, nil
}

// getArchivedMessages owns the conversation data operation of GetArchivedMessages.
func getArchivedMessages(ctx dex.Context, before conversation.Sequence) (*dex.RPCResult[archivedMessagesRPCOutput], error) {
	// The resource API admits only a chunk boundary; any other value finds nothing.
	first, _ := agentloop.ArchivedMessageChunkFirst(before)
	state, err := agentStateAttribute.Get(ctx)
	if dexflow.IsAttributeNotFound(err) || err == nil && first < state.FirstRetainedSequence {
		return &dex.RPCResult[archivedMessagesRPCOutput]{}, nil
	}
	comm.MustDo(err)
	chunk, err := archivedMessagesAttribute.Get(ctx, agentloop.SequenceKey(first))
	if dexflow.IsAttributeNotFound(err) {
		return &dex.RPCResult[archivedMessagesRPCOutput]{}, nil
	}
	page := conversation.HistoryPage{Messages: publicMessages(comm.Must(chunk, err).Entries)}
	if first > state.FirstRetainedSequence {
		page.NextBeforeSequence = &first
	}
	return &dex.RPCResult[archivedMessagesRPCOutput]{Output: archivedMessagesRPCOutput{Page: page, Found: true}}, nil
}

// prepareSessionNotification validates the notification and records any subagent completion.
func prepareSessionNotification(ctx dex.Context, notification conversation.SessionNotification) error {
	if err := notification.Validate(); err != nil {
		return fmt.Errorf("invalid notification %q: %w", notification.ID, err)
	}
	return recordSubagentEnd(ctx, notification)
}
