// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
)

// ResumeToken resumes a best-effort event subscription.
type ResumeToken string

// EventStream selects one best-effort live Stream.
type EventStream string

const (
	EventStreamReasoning EventStream = "reasoning"
	EventStreamAssistant EventStream = "assistant"
	EventStreamActivity  EventStream = "activity"
)

// StreamEventKind discriminates the three live event payloads, which the API
// sends as the reasoning_summary, assistant_text and activity members of the
// SessionProgress content oneof.
type StreamEventKind string

const (
	StreamEventKindReasoning StreamEventKind = "reasoning_summary"
	StreamEventKindAssistant StreamEventKind = "assistant_text"
	StreamEventKindActivity  StreamEventKind = "activity"
)

// definition returns the Stream of stream and the kind of its events.
func (stream EventStream) definition() (dex.StreamDef, StreamEventKind) {
	switch stream {
	case EventStreamReasoning:
		return reasoningSummaryStream, StreamEventKindReasoning
	case EventStreamAssistant:
		return assistantTextStream, StreamEventKindAssistant
	case EventStreamActivity:
		return agentActivityStream, StreamEventKindActivity
	default:
		panic(fmt.Sprintf("unknown event Stream %q", stream))
	}
}

// StreamEvent is one typed best-effort Stream message.
// Text is populated for assistant and reasoning events; Activity is populated for activity events.
type StreamEvent struct {
	Kind        StreamEventKind
	Text        string
	Activity    conversation.AgentEvent
	ResumeToken ResumeToken
	CreatedAt   time.Time
	Source      string
	// TaskID names the subagent whose activity a Session's Watch relays;
	// empty for the Session's own Streams.
	TaskID string
}

// writeActivity writes an activity event to the best-effort Stream; a failed
// write, such as after a Stop cancelled the Step, loses only the live event.
func writeActivity(ctx dex.Context, event conversation.AgentEvent) {
	event.Message = agentloop.CondenseActivityMessage(event.Message)
	dexflow.StreamWritten(ctx, agentActivityStream.Write(ctx, event))
}

// writeSnapshotRequired tells live readers to read the Snapshot again.
func writeSnapshotRequired(ctx dex.Context) {
	writeActivity(ctx, conversation.AgentEvent{Kind: conversation.EventKindSnapshotRequired, Message: "Durable interaction state changed."})
}

// writeInputConsumption reports the inputs a Step consumed, if any.
func writeInputConsumption(ctx dex.Context, queued, steered []conversation.PendingUserMessage, messageSequence *conversation.Sequence) {
	if event, isConsumed := agentloop.InputConsumptionEvent(queued, steered, messageSequence); isConsumed {
		writeActivity(ctx, event)
	}
}

// modelProgress writes the best-effort activity of one model reply.
type modelProgress struct {
	ctx             dex.Context
	messageSequence conversation.Sequence
}

func (progress modelProgress) write(event conversation.AgentEvent) {
	event.Message, event.MessageSequence = agentloop.CondenseActivityMessage(event.Message), &progress.messageSequence
	dexflow.StreamWritten(progress.ctx, agentActivityStream.Write(progress.ctx, event))
}

// writeActivity is the conversation.ActivityWriter of the model call.
func (progress modelProgress) writeActivity(event conversation.AgentEvent) error {
	progress.write(event)
	return nil
}

// writeToolProgress reports the attempt of a tool call that starts running.
// Progress carries no command output; dexflow.KeepAlive, not progress, keeps
// the attempt alive.
func writeToolProgress(ctx dex.Context, call conversation.ToolCall) {
	event := agentloop.ToolActivity(conversation.EventKindToolProgress, "Running "+string(call.Name)+".", call)
	attempt := ctx.Attempt()
	event.Attempt = &attempt
	dexflow.StreamWritten(ctx, agentActivityStream.Write(ctx, event))
}
