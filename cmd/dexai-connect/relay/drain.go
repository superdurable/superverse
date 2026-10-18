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
	"fmt"
	"slices"

	"github.com/superdurable/superversev2/cmd/dexai-connect/mcpserver"
	"github.com/superdurable/superversev2/cmd/dexai-connect/state"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

// The answers to an MCP call while the relay does not accept new work.
var (
	drainingResult = mcpserver.CallResult{Text: "the Bridge restarted and accepts new work once the calls sent before the restart settle; " +
		"this call was not sent. Send a new message to continue.", IsError: true}
	notReadyResult = mcpserver.CallResult{Text: "the Bridge is not ready; run dexai-connect status", IsError: true}
)

// beginDrain fails closed at start: work the Bridge that ran before reported
// may still execute, so a draining record fences new work until the Session
// settled every report admitted or stored before it (finishDrain). Only a
// latest turn this relay tracks (its loops restarted), or a new latest turn
// with nothing acknowledged and nothing stored, starts at once. When the latest
// turn's events were already read (it is in Progress.Turns), the record names
// it: it is cancelled unless positively over, and its completion is awaited.
// A latest turn never read, or none, is new: the record names no turn, stops
// nothing and awaits no completion, since every older turn is over. The
// record survives restarts. No generation change is needed: reports reach the
// Session in the one ordered stream; Stop and SetExecutor stay the only
// generation changes.
func (r *relay) beginDrain(ctx context.Context, conversation Conversation, tracked string) error {
	stored, err := r.binding.StoredReports()
	if err != nil {
		return err
	}
	r.mu.Lock()
	draining := r.progress.Draining
	turn, read := r.turnLocked(conversation.TurnID)
	read = read && conversation.TurnID != ""
	nothingSent := !read && r.progress.Acknowledged == 0 && len(stored) == 0
	r.mu.Unlock()
	if draining == nil && ((conversation.TurnID != "" && conversation.TurnID == tracked) || nothingSent) {
		r.release()
		return nil
	}
	if draining == nil {
		r.mu.Lock()
		progress := r.progress
		// A completion the event loop already stored never comes again, and
		// a report acknowledged before this record is gone from the store.
		record := state.Draining{Admitted: progress.Acknowledged}
		if read {
			record.TurnID, record.CompletionStored = conversation.TurnID, turn.Completed
		}
		if len(stored) > 0 {
			record.Slot = stored[len(stored)-1].Slot
		}
		progress.Draining = &record
		err := r.storeLocked(progress, nil, nil)
		r.mu.Unlock()
		if err != nil {
			return err
		}
		draining = progress.Draining
		r.logger.WarnContext(ctx, "the Bridge that ran before left work; settling it before new work", "project", r.projectName)
	}
	// Only the named turn is cancelled: a later latest turn is the owner's new
	// turn and proves the named one is over.
	if draining.TurnID != "" && conversation.TurnID == draining.TurnID && conversation.Turn != TurnIdle {
		if _, err := r.provider.Cancel(ctx, r.sessionID, r.binding.Record.ProviderThreadID); err != nil {
			return fmt.Errorf("stop the %s turn that ran at the restart: %w", r.provider.AgentLabel(), err)
		}
	}
	return r.interruptUnsent(*draining)
}

// drains reports whether a stored report belongs to the drain: stored before
// its record, or of its turn.
func drains(draining state.Draining, stored state.StoredReport) bool {
	return stored.Slot <= draining.Slot || (draining.TurnID != "" && stored.Report.ProviderTurn == draining.TurnID)
}

// interruptUnsent settles the drain's tool calls that were never sent: they
// are removed and their MCP calls answered interrupted, so none is sent after
// the restart. A call with a sequence may have been admitted, so it is sent
// as usual and its result arrives through sync.
func (r *relay) interruptUnsent(draining state.Draining) error {
	reports, err := r.binding.StoredReports()
	if err != nil {
		return err
	}
	for _, stored := range reports {
		if stored.Report.Sequence == 0 && stored.Report.ToolCall != nil && drains(draining, stored) {
			if err := r.settleHead(stored, &interruptedResult); err != nil {
				return err
			}
		}
	}
	return nil
}

// completeDrain lets the drained turn's completion through once: the report
// and CompletionStored commit in one write, so a restart never stores a
// second one. Orca reports a completion only once the turn is positively over.
func completeDrain(progress *state.Progress, reports []bridgeapi.Report) []bridgeapi.Report {
	return slices.DeleteFunc(reports, func(report bridgeapi.Report) bool {
		draining := progress.Draining
		if draining == nil || draining.TurnID == "" || !report.TurnCompleted || report.ProviderTurn != draining.TurnID {
			return false
		}
		if draining.CompletionStored {
			return true
		}
		completed := *draining
		completed.CompletionStored = true
		progress.Draining = &completed
		return false
	})
}

// admitDrainLocked raises the drain's Admitted to an acknowledged report of
// the drain (an admission or a duplicate, never a refusal), in the write that
// records the acknowledgement. The caller holds mu.
func (r *relay) admitDrainLocked(stored state.StoredReport, report bridgeapi.Report) {
	if draining := r.progress.Draining; draining != nil && drains(*draining, stored) && report.Sequence > draining.Admitted {
		admitted := *draining
		admitted.Admitted = report.Sequence
		r.progress.Draining = &admitted
	}
}

// drainReported reports a drain whose turn completion, if it names a turn, is
// stored and whose every report was acknowledged or refused: none is stored.
func (r *relay) drainReported() (bool, error) {
	r.mu.Lock()
	draining := r.progress.Draining
	r.mu.Unlock()
	if draining == nil || (draining.TurnID != "" && !draining.CompletionStored) {
		return false, nil
	}
	reports, err := r.binding.StoredReports()
	if err != nil {
		return false, err
	}
	return !slices.ContainsFunc(reports, func(stored state.StoredReport) bool { return drains(*draining, stored) }), nil
}

// finishDrain ends the drain on an answer to a sync that started after
// drainReported, once that answer shows the Session's settledThrough at
// least the drain's Admitted sequence with every entry read, so every result
// of the turn was read: it reached its call, is queued, or (read by a scan)
// lies after the durable cursor, which the rewind reads again. An
// acknowledgement proves only admission; settledThrough proves the outcome is
// recorded. Orca must also show a named turn positively over; otherwise the
// next sync asks again. Orca idle alone never ends a drain.
func (r *relay) finishDrain(ctx context.Context, answer bridgeapi.SyncResponse) error {
	r.mu.Lock()
	draining := r.progress.Draining
	r.mu.Unlock()
	if draining == nil || answer.SettledThrough < draining.Admitted || answer.ExaminedThrough < answer.LastSequence {
		return nil
	}
	conversation, err := r.provider.Conversation(ctx, r.sessionID)
	if err != nil {
		r.logger.WarnContext(ctx, "read the "+r.provider.ProviderName()+" conversation to end a drain", "project", r.projectName, "error", err)
		return nil
	}
	// A page that names no thread leaves the binding unconfirmed: wait for the
	// next sync. Only another named thread proves the conversation changed.
	if conversation.ProviderThreadID == "" {
		return nil
	}
	if conversation.ProviderThreadID != r.binding.Record.ProviderThreadID {
		return r.provider.SessionChangedError()
	}
	if draining.TurnID != "" && conversation.TurnID == draining.TurnID && conversation.Turn != TurnIdle {
		return nil
	}
	r.mu.Lock()
	progress := r.progress
	progress.Draining = nil
	err = r.storeLocked(progress, nil, nil)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	r.logger.InfoContext(ctx, "the work of the Bridge that ran before settled; accepting new work", "project", r.projectName)
	r.release()
	return nil
}

// release makes the relay accept new work.
func (r *relay) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.ready:
	default:
		close(r.ready)
	}
}

// readyResult is nil when the relay accepts new work, else the answer to an
// MCP call.
func (r *relay) readyResult() *mcpserver.CallResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.ready:
		return nil
	default:
	}
	if r.progress.Draining != nil {
		return &drainingResult
	}
	return &notReadyResult
}

// holdChanged keeps a relay whose conversation changed listed with the
// reason, serving nothing, until serve stops or the Builder disconnects it,
// which removes the binding without touching the Orca session.
func (r *relay) holdChanged(ctx context.Context, err error) {
	r.logger.ErrorContext(ctx, "The "+r.provider.ProviderName()+" conversation changed; reconnect from Studio", "project", r.projectName)
	r.setProblem(err)
	<-ctx.Done()
	r.mu.Lock()
	stopping := r.stopping
	r.mu.Unlock()
	if stopping != nil {
		r.finishDetach(ctx, stopping, false)
	}
}
