// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// maximumTurnRequests bounds the user messages a turn quotes as its pending
// request after the message that started it: the answers given since.
const maximumTurnRequests = 8

// outputLimitResult answers a tool call that a reply cut off at the output
// limit never ran.
const outputLimitResult = "Not executed: your reply reached the output limit before this call was complete."

// StartUserTurn starts the turn of one or more user messages and returns the
// entries that record them. A message is appended at the next sequence. An
// answer to a question continues the request the turn works on; any other
// message replaces it.
func StartUserTurn(state AgentState, messages []conversation.UserMessage) (AgentState, []conversation.Entry) {
	state = startTurn(state)
	// The user is back: notifications may open turns again, and subagents start
	// within a fresh budget.
	state.AutomaticTaskTurns, state.AutomaticPlatformTurns, state.AutomaticTurnsPaused = 0, 0, false
	state.SubagentsStartedWithoutUser, state.AwaitsFirstMessage = 0, false
	entries := make([]conversation.Entry, 0, len(messages)+1)
	for _, message := range messages {
		sequence := state.LastSequence + conversation.Sequence(len(entries)) + 1
		if message.AnsweredInputCallID == nil {
			state.TurnRequestSequences = nil
		}
		state = AddTurnRequest(state, sequence)
		entries = append(entries, message.Entry())
	}
	return state, entries
}

// startTurn is the state of every turn start: the next turn number, fresh
// handoff tracking and no pending call.
func startTurn(state AgentState) AgentState {
	state.Turn++
	state.HandoffContinuations, state.SourceChangedSinceHandoff = 0, false
	state.Status = AgentStatusCallingModel
	state.PendingToolCalls, state.PendingToolIndex = []conversation.ToolCall{}, 0
	state.PendingToolsAdmission = remoteexec.Fence{}
	return state
}

// AddTurnRequest adds the entry at sequence to the request the turn works on,
// keeping the message that started the work and the latest ones.
func AddTurnRequest(state AgentState, sequence conversation.Sequence) AgentState {
	state.TurnRequestSequences = append(state.TurnRequestSequences, sequence)
	if overflow := len(state.TurnRequestSequences) - (maximumTurnRequests + 1); overflow > 0 {
		state.TurnRequestSequences = append(state.TurnRequestSequences[:1], state.TurnRequestSequences[1+overflow:]...)
	}
	return state
}

// Session notifications reach a turn at its model-continuation boundaries and
// the idle Agent in AwaitUser; both take at most NotificationBatchSize at a
// time. The Channel admits fewer than maximumPendingNotifications, and the
// IDs of the last maximumNotificationReceipts admitted ones deduplicate a
// producer's retries.
const (
	NotificationBatchSize       = 16
	maximumPendingNotifications = 32
	maximumNotificationReceipts = 128
)

// AdmitNotification decides whether a notification enters the Channel: a known
// ID is a duplicate, which is answered as admitted and publishes nothing; a
// full Channel refuses it. It returns the receipts to store with a publish.
func AdmitNotification(receipts []string, pending int, id string) (isNew, isAdmitted bool, next []string) {
	switch {
	case slices.Contains(receipts, id):
		return false, true, receipts
	case pending >= maximumPendingNotifications:
		return false, false, receipts
	}
	next = append(slices.Clone(receipts), id)
	return true, true, next[max(0, len(next)-maximumNotificationReceipts):]
}

// TakesNotifications reports whether CheckSteered delivers pending
// notifications at its boundary: before a model continuation inside a running
// turn (after tool results), or with steering, ahead of the steered text.
// None is taken while a Stop of the turn is pending.
func TakesNotifications(next Continuation, isSteered, isStopped bool) bool {
	return !isStopped && (isSteered || next == ContinueCallModel || next == ContinueCompactContext)
}

// IdleNotifications is what a batch of notifications consumed while the Agent
// waits for the user does: OpensTurn starts an automatic turn, and Paused
// records once that automatic continuation paused. isStopped reports a Stop
// of the last turn that no turn end has applied yet.
type IdleNotifications struct {
	OpensTurn bool
	Paused    bool
}

// AutomaticTurnBounds bound the consecutive automatic turns: Task those that
// task and connector notifications open, Platform those that
// application-owned delivery failures of HEAD open. Zero opens none.
type AutomaticTurnBounds struct {
	Task, Platform int
}

// ConsumeIdleNotifications decides a batch consumed in AwaitUser, as the
// Session recorded it. A task or connector notification, or a delivery
// failure that the application owns, may open one automatic turn, which
// counts against the bound of the kind that qualified it, the platform bound
// when both did; at that bound, or after a Stop, automatic turns pause and the
// batch is only recorded. requests are the sequences of the recorded
// notifications, which the opened turn works on.
func ConsumeIdleNotifications(state AgentState, notifications []conversation.SessionNotification, requests []conversation.Sequence, bounds AutomaticTurnBounds, isStopped bool) (AgentState, IdleNotifications) {
	var decision IdleNotifications
	if isStopped {
		state, decision.Paused = pauseAutomaticTurns(state)
	}
	platform := slices.ContainsFunc(notifications, conversation.SessionNotification.OpensPlatformTurn)
	if !platform && !slices.ContainsFunc(notifications, opensTaskTurn) || state.AutomaticTurnsPaused {
		return state, decision
	}
	if platform && state.AutomaticPlatformTurns >= bounds.Platform || !platform && state.AutomaticTaskTurns >= bounds.Task {
		state, decision.Paused = pauseAutomaticTurns(state)
		return state, decision
	}
	state = startTurn(state)
	if platform {
		state.AutomaticPlatformTurns++
	} else {
		state.AutomaticTaskTurns++
	}
	for _, sequence := range requests {
		state = AddTurnRequest(state, sequence)
	}
	decision.OpensTurn = true
	return state, decision
}

// opensTaskTurn reports whether a notification qualifies for the task bound.
func opensTaskTurn(notification conversation.SessionNotification) bool {
	return notification.Kind == conversation.NotificationKindTask || notification.Kind == conversation.NotificationKindConnector
}

// pauseAutomaticTurns pauses automatic turns and reports whether they ran
// until now, which records the pause once.
func pauseAutomaticTurns(state AgentState) (AgentState, bool) {
	isNew := !state.AutomaticTurnsPaused
	state.AutomaticTurnsPaused = true
	return state, isNew
}

// The Stop button: InterruptAgent records an AgentInterrupt for the turn the
// user saw, stops the running tasks and ends that turn, at once when its work
// in flight can be cancelled and otherwise at its next boundary.

// AgentInterrupt is a Stop of turn Turn that no turn end has applied yet;
// EndsTurn when it cancelled the turn's work and InterruptAgent ends it.
// MarkerWritten owns the one marker even while an idle Stop stays pending.
type AgentInterrupt struct {
	PreserveBackground bool                `json:"preserve_background,omitempty"`
	Turn               int64               `json:"turn"`
	RequestedAt        time.Time           `json:"requested_at"`
	EndsTurn           bool                `json:"ends_turn,omitempty"`
	Actor              *conversation.Actor `json:"actor,omitempty"`
	MarkerWritten      bool                `json:"marker_written,omitempty"`
}

// InterruptAgentRequest is a Stop of the turn the user saw in the Snapshot.
type InterruptAgentRequest struct {
	Actor *conversation.Actor `json:"actor,omitempty"`
	Turn  int64               `json:"turn"`
}

// turnRuns reports whether a turn runs: the Agent is not waiting for the user.
func (state AgentState) turnRuns() bool {
	return state.Status != AgentStatusWaitingForMessage && state.Status != AgentStatusInitializing
}

// InterruptPlan is what a Stop does: whether it is admitted, whether it joins
// the Stop already pending for the turn, and whether it cancels the work in
// flight and ends the turn at once.
type InterruptPlan struct {
	Admitted bool
	Joined   bool
	EndsTurn bool
}

// PlanInterrupt admits a Stop of the running turn, or of the running tasks
// while no turn runs, that names the current turn: a Stop the user pressed
// for an earlier turn is stale, and one with nothing running does nothing. A
// repeated Stop joins any pending Stop of the turn, including an uninterruptible
// tool or an idle task-only Stop, preserving the first accepted actor.
func PlanInterrupt(state AgentState, request InterruptAgentRequest, hasRunningTasks bool, pending *AgentInterrupt) InterruptPlan {
	switch {
	case request.Turn != state.Turn || !state.turnRuns() && !hasRunningTasks:
		return InterruptPlan{}
	case pending != nil && pending.Turn == state.Turn:
		return InterruptPlan{Admitted: true, Joined: true}
	}
	return InterruptPlan{Admitted: true, EndsTurn: state.turnRuns() && state.Running != nil && state.Running.interruptible()}
}

// StopTurn ends a stopped turn: nothing stays in flight, the Agent waits for
// the user and automatic turns pause. It returns the pending calls from the
// cursor, which the caller answers, and whether the pause is new.
func StopTurn(state AgentState) (AgentState, []conversation.ToolCall, bool) {
	calls := slices.Clone(state.PendingToolCalls[state.PendingToolIndex:])
	state.PendingToolCalls, state.PendingToolIndex, state.Running = []conversation.ToolCall{}, 0, nil
	state.PendingToolsAdmission = remoteexec.Fence{}
	state.Status = AgentStatusWaitingForMessage
	state, isNewPause := pauseAutomaticTurns(state)
	return state, calls, isNewPause
}

// ExampleStartedNotice opens the conversation of a Session created from an
// example: the model reads its text, and Studio shows its marker, the first
// divider of an example Project's conversation, as the agent's greeting.
func ExampleStartedNotice(brief ExampleBrief) conversation.Entry {
	text := fmt.Sprintf("This Project starts from the example %s (%s, Git tree %s): its first commit is the example's finished source, "+
		"its processes are built and its requirements are recorded as version 1. Wait for the person's message, then build only the change "+
		"they ask for, and keep the processes and requirements they still want.", brief.Title, brief.ExampleID, brief.Tree)
	marker := fmt.Sprintf("This project starts from the %s example. Its processes are built and drawn on the Flow tab, and Try it gets ready "+
		"in a few minutes. Tell me what you want to change.", brief.Title)
	return conversation.NoticeEntry(conversation.EntryRoleSystem, text, marker)
}

// ExampleBriefRejectedNotice opens the conversation of a Session whose
// example has no card it can record: the processes are built, and no
// requirements are recorded.
func ExampleBriefRejectedNotice(brief ExampleBrief, problem string) conversation.Entry {
	text := fmt.Sprintf("This Project starts from the example %s (%s, Git tree %s): its first commit is the example's finished source, "+
		"but its requirements card could not be recorded (%s). Wait for the person's message; confirm requirements before a large change.",
		brief.Title, brief.ExampleID, brief.Tree, comm.TruncateUTF8(problem, 2<<10))
	marker := fmt.Sprintf("This project starts from the %s example. Its processes are built; its brief could not be recorded. Tell me what you want to change.", brief.Title)
	return conversation.NoticeEntry(conversation.EntryRoleSystem, text, marker)
}

// stoppedMarker is the divider a Stop leaves in the conversation.
const stoppedMarker = "Stopped by user"

// stoppedNotice is what the model reads of a Stop.
const stoppedNotice = "The user pressed Stop: the turn ended and every running background task was stopped. Wait for the user's next message before you continue the work."

// StoppedEntry records a Stop in the conversation.
func StoppedEntry(actor *conversation.Actor) conversation.Entry {
	text, marker := stoppedNotice, stoppedMarker
	if actor != nil {
		text += " Stopped by " + actor.Email + "."
		marker = "Stopped by " + actor.Email
	}
	entry := conversation.NoticeEntry(conversation.EntryRoleSystem, text, marker)
	entry.Meta.Actor = actor
	return entry
}

// pausedMarker is the divider the pause of automatic turns leaves once.
const pausedMarker = "Automatic continuation paused; reply to continue"

// pausedNotice is what the model reads of the pause.
const pausedNotice = "Automatic continuation is paused: notifications are recorded but start no turn until the user replies."

// PausedEntry records the pause of automatic turns.
func PausedEntry() conversation.Entry {
	return conversation.NoticeEntry(conversation.EntryRoleSystem, pausedNotice, pausedMarker)
}

// SteeringInterruptions are the activity events of the waits a steered turn
// abandons: a question batch and a tool approval.
func SteeringInterruptions(input *PendingUserInput, approval *PendingApproval) []conversation.AgentEvent {
	var events []conversation.AgentEvent
	if input != nil {
		events = append(events, conversation.AgentEvent{Kind: conversation.EventKindUserInputCancelled, Message: "Pending user input was cancelled by steering.", CallID: &input.CallID})
	}
	if approval != nil {
		events = append(events, ToolActivity(conversation.EventKindToolApprovalResolved, "Tool approval was interrupted by steering.", conversation.ToolCall{ID: approval.CallID, Name: approval.ToolName}))
	}
	return events
}

// SteeringAppliedEvent reports how many steered messages a turn applied.
func SteeringAppliedEvent(count int) conversation.AgentEvent {
	return conversation.AgentEvent{Kind: conversation.EventKindSteeringApplied, Message: fmt.Sprintf("Applied %d steered user message(s).", count)}
}

// ContinueAfterModelReply returns the state after a recorded model reply,
// where the turn goes next and the notice to append first, if any. Tool calls
// are routed. Without them the turn ends unless it changed source after its
// last unblocked commit_and_push: such a turn has not handed off the requested
// implementation and is continued a bounded number of times, each with an
// appended reminder.
func ContinueAfterModelReply(state AgentState, reply conversation.ModelReply) (AgentState, Continuation, *conversation.Entry) {
	if len(reply.ToolCalls) > 0 {
		state.Status = AgentStatusRoutingTool
		state.PendingToolCalls, state.PendingToolIndex = reply.ToolCalls, 0
		return state, ContinueRouteTool, nil
	}
	if state.SourceChangedSinceHandoff && state.HandoffContinuations < maximumAutomaticHandoffContinuations {
		state.HandoffContinuations++
		notice := conversation.NoticeEntry(conversation.EntryRoleUser, conversation.UnfinishedSourceHandoffInstruction, "")
		return state, ContinueCallModel, &notice
	}
	return state, ContinueAwaitUser, nil
}

// ContinueAfterOutputLimit returns the state after a recorded reply that
// stopped at the output limit and the entries to append: an error result for
// each of its tool calls, which never run, or else a continuation notice.
func ContinueAfterOutputLimit(state AgentState, reply conversation.ModelReply) (AgentState, []conversation.Entry) {
	if len(reply.ToolCalls) == 0 {
		return state, []conversation.Entry{conversation.NoticeEntry(conversation.EntryRoleUser, conversation.OutputLimitContinuation, "")}
	}
	entries := make([]conversation.Entry, 0, len(reply.ToolCalls))
	for _, call := range reply.ToolCalls {
		entries = append(entries, ToolResultEntry(call, SessionFailureResult(toolfailure.OutputLimitExceeded, outputLimitResult), ToolMessageTiming{}))
	}
	return state, entries
}

// ModelCallKind separates the stable identities of response and compaction effects.
type ModelCallKind string

const (
	ModelCallKindResponse   ModelCallKind = "response"
	ModelCallKindCompaction ModelCallKind = "compaction"
)

// ModelReplyActivityMessage describes the completed response without model content.
func ModelReplyActivityMessage(reply conversation.ModelReply) string {
	if len(reply.ToolCalls) == 0 {
		return "Model response completed."
	}
	names := make([]string, 0, len(reply.ToolCalls))
	for _, call := range reply.ToolCalls {
		names = append(names, string(call.Name))
	}
	return "Model requested: " + strings.Join(names, ", ")
}

// The message window: CurrentMessages holds fewer than CurrentMessageLimit
// entries and archives the oldest in immutable chunks of ArchiveMessageChunkSize.
const (
	ArchiveMessageChunkSize = 10
	CurrentMessageLimit     = 2 * ArchiveMessageChunkSize
)

// SequenceKey is the CurrentMessages and ArchivedMessages instance key of a sequence.
func SequenceKey(sequence conversation.Sequence) string {
	return strconv.FormatInt(int64(sequence), 10)
}

// ArchivedMessageChunkFirst is the first sequence of the archive chunk that
// ends before before; false for a value that is no chunk boundary.
func ArchivedMessageChunkFirst(before conversation.Sequence) (conversation.Sequence, bool) {
	if before <= conversation.Sequence(ArchiveMessageChunkSize) || (before-1)%conversation.Sequence(ArchiveMessageChunkSize) != 0 {
		return 0, false
	}
	return before - conversation.Sequence(ArchiveMessageChunkSize), true
}

// AllPendingChannelMessages makes a Channel condition take every pending message.
const AllPendingChannelMessages = 2_147_483_647

func pendingMessageIDs(messages []conversation.PendingUserMessage) []conversation.MessageID {
	ids := make([]conversation.MessageID, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.MessageID)
	}
	return ids
}

func consumedMessagesDescription(count int, queue string) string {
	noun := "messages"
	if count == 1 {
		noun = "message"
	}
	return fmt.Sprintf("Consumed %d %s user %s.", count, queue, noun)
}

// The tests name these constants.
const (
	StoppedMarkerForTest               = stoppedMarker
	PausedMarkerForTest                = pausedMarker
	MaximumPendingNotificationsForTest = maximumPendingNotifications
)
