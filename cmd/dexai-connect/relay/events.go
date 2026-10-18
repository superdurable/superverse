// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/superdurable/superversev2/cmd/dexai-connect/state"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

const (
	// maxBatchEventBytes bounds the encoded events of one report, leaving room
	// for the report's other fields within its body limit.
	maxBatchEventBytes = bridgeapi.MaxReportRequestBytes - 16<<10
	// History import bounds.
	maxImportedMessages = 200
	maxImportedBytes    = 512 << 10
	// Bounds of the remembered turns and delivered messages.
	maxRememberedTurns      = 16
	maxRememberedDeliveries = 64
	subscribeRetry          = 5 * time.Second
)

// reportBatch is the reports made from one page of events, in their order:
// consecutive events of one generation and provider turn share an events report
// of at most 32 events and maxBatchEventBytes encoded; a turn completion is a
// report of its own, after its turn's events.
type reportBatch struct {
	reports []bridgeapi.Report
	// bytes is the encoded size of the last report's events.
	bytes int
	// completed is the turns whose completion the page holds.
	completed []string
}

func (batch *reportBatch) addEvent(generation uint64, turnID string, event bridgeapi.Event) {
	if event.Text == "" {
		return
	}
	event.Text = comm.TruncateUTF8(event.Text, bridgeapi.MaxEventTextBytes)
	// The encoded size counts JSON escaping, which can make text several
	// times longer; one event of the largest text always fits.
	size := len(comm.Must(json.Marshal(event))) + 1
	last := len(batch.reports) - 1
	if last < 0 || len(batch.reports[last].Events) == 0 || batch.reports[last].Generation != generation ||
		batch.reports[last].ProviderTurn != turnID || len(batch.reports[last].Events) == bridgeapi.MaxReportEvents ||
		batch.bytes+size > maxBatchEventBytes {
		batch.reports = append(batch.reports, bridgeapi.Report{Generation: generation, ProviderTurn: turnID})
		last, batch.bytes = last+1, 0
	}
	batch.reports[last].Events = append(batch.reports[last].Events, event)
	batch.bytes += size
}

// eventLoop follows the session's events from the saved cursor,
// resubscribing after a failure.
func (r *relay) eventLoop(ctx context.Context) error {
	for {
		r.mu.Lock()
		after := r.progress.EventCursor
		r.mu.Unlock()
		err := r.provider.Subscribe(ctx, r.sessionID, r.binding.Record.ProviderThreadID, after, func(events []Event, next state.EventPosition) error {
			return r.handleEvents(events, next)
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if isSessionChanged(err) {
			return err
		}
		if errors.Is(err, ErrUnsupportedProtocol) {
			return err
		}
		r.logger.WarnContext(ctx, fmt.Sprintf("the %s session subscription ended; resubscribing", r.provider.ProviderName()), "project", r.projectName, "error", err)
		if err := sleep(ctx, subscribeRetry); err != nil {
			return err
		}
	}
}

// handleEvents maps a page of provider events to reports and stores them with
// the cursor after the page in one atomic write, so no event is lost and none
// is reported twice. A failure, such as a full store, keeps the saved cursor:
// the subscription ends and resumes from it. Echoes of delivered messages and
// turns that started before the generation's boundary are not reported; the
// provider adapter never passes hidden reasoning or raw tool output.
func (r *relay) handleEvents(events []Event, next state.EventPosition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var batch reportBatch
	for _, event := range events {
		r.handleEventLocked(event, &batch)
	}
	progress := r.progress
	progress.EventCursor = next
	// A completion marks its turn only in the write that stores it.
	progress.Turns = slices.Clone(progress.Turns)
	for index := range progress.Turns {
		if slices.Contains(batch.completed, progress.Turns[index].TurnID) {
			progress.Turns[index].Completed = true
		}
	}
	reports := completeDrain(&progress, batch.reports)
	return r.storeLocked(progress, reports, nil)
}

func (r *relay) handleEventLocked(event Event, batch *reportBatch) {
	generation := r.bindTurnLocked(event)
	switch event.Kind {
	case EventTurnStarted:
		r.runningTurn = event.TurnID
		close(r.turnStarted)
		r.turnStarted = make(chan struct{})
	case EventTurnCompleted:
		if r.runningTurn == event.TurnID {
			r.runningTurn = ""
		}
		r.retireTurnCallsLocked(event.TurnID)
		batch.completed = append(batch.completed, event.TurnID)
		batch.reports = append(batch.reports, bridgeapi.Report{Generation: generation, ProviderTurn: event.TurnID, TurnCompleted: true})
	case EventUserMessage:
		if _, delivered := r.deliveredLocked(event.MessageID); !delivered {
			batch.addEvent(generation, event.TurnID, bridgeapi.Event{Kind: bridgeapi.EventKindUserMessage, Text: event.Text})
		}
	case EventAssistantMessage:
		batch.addEvent(generation, event.TurnID, bridgeapi.Event{Kind: bridgeapi.EventKindAssistantMessage, Text: event.Text})
	case EventActivity:
		batch.addEvent(generation, event.TurnID, bridgeapi.Event{Kind: bridgeapi.EventKindActivity, Text: event.Text})
	}
}

// bindTurnLocked returns the generation of an event's turn, binding a turn
// seen for the first time by its start: a turn a delivery opened belongs to
// that delivery's generation; any other belongs to the current generation
// when it started after the boundary, and to none (0, refused) before it.
func (r *relay) bindTurnLocked(event Event) uint64 {
	if event.TurnID == "" {
		return 0
	}
	if generation, known := r.turnGenerationLocked(event.TurnID); known {
		return generation
	}
	generation, delivered := r.deliveredLocked(event.MessageID)
	if event.Kind != EventTurnStarted || !delivered {
		generation = 0
		if r.provider.EventAfter(event.Position, r.progress.Boundary) {
			generation = r.progress.Generation
		}
	}
	r.rememberTurnLocked(event.TurnID, generation)
	return generation
}

// deliveredLocked returns the generation of a message the Bridge delivered.
func (r *relay) deliveredLocked(messageID string) (uint64, bool) {
	if messageID == "" {
		return 0, false
	}
	for _, message := range r.progress.Delivered {
		if message.MessageID == messageID {
			return message.Generation, true
		}
	}
	return 0, false
}

// rememberDelivery records a delivered message in progress, bounded.
func rememberDelivery(progress *state.Progress, messageID string, generation uint64) {
	if messageID == "" || slices.ContainsFunc(progress.Delivered, func(message state.DeliveredMessage) bool { return message.MessageID == messageID }) {
		return
	}
	progress.Delivered = append(progress.Delivered, state.DeliveredMessage{MessageID: messageID, Generation: generation})
	if extra := len(progress.Delivered) - maxRememberedDeliveries; extra > 0 {
		progress.Delivered = progress.Delivered[extra:]
	}
}

// turnGenerationLocked returns the generation a turn was bound to, which is
// 0 for a turn that started before the boundary.
func (r *relay) turnGenerationLocked(turnID string) (uint64, bool) {
	turn, found := r.turnLocked(turnID)
	return turn.Generation, found
}

// turnLocked returns a remembered turn.
func (r *relay) turnLocked(turnID string) (state.TurnGeneration, bool) {
	for _, turn := range r.progress.Turns {
		if turn.TurnID == turnID {
			return turn, true
		}
	}
	return state.TurnGeneration{}, false
}

func (r *relay) rememberTurnLocked(turnID string, generation uint64) {
	r.progress.Turns = append(r.progress.Turns, state.TurnGeneration{TurnID: turnID, Generation: generation})
	if extra := len(r.progress.Turns) - maxRememberedTurns; extra > 0 {
		r.progress.Turns = r.progress.Turns[extra:]
	}
}

// importHistory reports the conversation's public messages once, newest
// last, at most 200 messages or 512 KiB, after the last one imported.
func (r *relay) importHistory(ctx context.Context) error {
	r.mu.Lock()
	imported, lastID := r.progress.HistoryImported, r.progress.LastImportedMessageID
	r.mu.Unlock()
	if imported {
		return nil
	}
	messages, err := r.provider.History(ctx, r.sessionID, maxImportedMessages)
	if err != nil {
		return err
	}
	if index := slices.IndexFunc(messages, func(message HistoryMessage) bool { return message.ID == lastID }); lastID != "" && index >= 0 {
		messages = messages[index+1:]
	}
	budget, first := maxImportedBytes, len(messages)
	for first > 0 && len(messages)-first < maxImportedMessages {
		size := min(len(messages[first-1].Text), bridgeapi.MaxEventTextBytes)
		if size > budget {
			break
		}
		budget -= size
		first--
	}
	messages = messages[first:]
	r.mu.Lock()
	defer r.mu.Unlock()
	// The imported events and the import's completion commit together.
	progress := r.progress
	var batch reportBatch
	for _, message := range messages {
		role := bridgeapi.EntryRoleUser
		if message.Role == HistoryRoleAssistant {
			role = bridgeapi.EntryRoleAssistant
		}
		batch.addEvent(progress.Generation, "", bridgeapi.Event{Kind: bridgeapi.EventKindImportedMessage, Role: role, Text: message.Text})
		progress.LastImportedMessageID = message.ID
	}
	progress.HistoryImported = true
	return r.storeLocked(progress, batch.reports, nil)
}
