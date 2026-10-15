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
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/mcpserver"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/comm"
)

// The in-memory delivery queue's bounds.
const (
	maxQueuedDeliveries = 64
	maxQueuedBytes      = 8 << 20
	// maxScanResolved bounds the marked entries; the retention window is
	// 1,000 entries. Past it the oldest mark goes, and its answer or result,
	// read again, is delivered as a message: a repeat, never a loss.
	maxScanResolved = 1024
)

// resolvedCall marks a call an entry's answer or tool result resolved, with
// the obligation to account for its delivery to Codex.
type resolvedCall struct {
	callID string
	ack    *resultAck
}

// The cursors. The durable delivery cursor (Progress.Cursor) moves only past
// settled deliveries, in order, so a restart re-reads from the first unsettled
// one. In memory, queuedThrough is how far every input is queued, and the
// read cursor how far sync has read: equal while the queue has room. While it
// cannot take the next input, the read cursor scans on to resolve tool results
// and answers but queues nothing; once a delivery settles, reading rewinds to
// queuedThrough. A rewind follows only a settled delivery, never the queue's
// size alone, so an input refused by the byte bound is not re-read in a loop.

// syncLoop reads the Session after the read cursor: it refreshes the tool
// set, adopts a new generation, answers calls at once and queues input.
func (r *relay) syncLoop(ctx context.Context) error {
	for {
		r.mu.Lock()
		if r.scanning && r.settled != r.scanSettled {
			r.readCursor, r.scanning = r.queuedThrough, false
		}
		epoch, after, digest, scanning, withContext := r.progress.Epoch, r.readCursor, r.session.digest, r.scanning, !r.contextRead
		r.mu.Unlock()
		// A drain ends only on an answer to a sync that started after its
		// reports settled.
		drainReported, err := r.drainReported()
		if err != nil {
			return err
		}
		pollCtx, stop := ctx, context.CancelFunc(func() {})
		if scanning {
			pollCtx, stop = r.untilDequeued(ctx)
		}
		answer, err := r.client.Sync(pollCtx, epoch, after, digest, withContext)
		interrupted := pollCtx.Err() != nil
		stop()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if interrupted {
			continue
		}
		if err != nil {
			var ended *detachment
			if errors.As(detachOn(err), &ended) {
				return ended
			}
			r.logger.WarnContext(ctx, "sync failed; retrying", "project", r.projectName, "error", err)
			r.setProblem(err)
			if err := sleep(ctx, protocolRetry); err != nil {
				return err
			}
			continue
		}
		if withContext {
			r.noteAgentContext(answer)
		}
		if err := r.applySync(ctx, answer); err != nil {
			return err
		}
		if drainReported {
			if err := r.finishDrain(ctx, answer); err != nil {
				return err
			}
		}
	}
}

func (r *relay) applySync(ctx context.Context, answer bridgeapi.SyncResponse) error {
	r.mu.Lock()
	r.lastSync, r.problem = time.Now(), ""
	r.interrupt = answer.Interrupt
	r.session.apply(answer)
	digest, generation := r.digest, r.progress.Generation
	r.mu.Unlock()
	if answer.ToolSetDigest != digest {
		if err := r.refreshTools(ctx); err != nil {
			return err
		}
	}
	if answer.Generation > generation {
		if err := r.adopt(ctx, answer.Generation); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queueAnswerLocked(answer)
	if len(r.queue) > 0 {
		return nil
	}
	r.progress.Cursor = max(r.progress.Cursor, r.queuedThrough)
	return r.saveLocked()
}

// untilDequeued is ctx, ended early when a delivery settles, so that a scan
// waiting in a long poll rewinds at once.
func (r *relay) untilDequeued(ctx context.Context) (context.Context, context.CancelFunc) {
	pollCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-r.dequeued:
			cancel()
		case <-pollCtx.Done():
		}
	}()
	return pollCtx, cancel
}

// queueAnswerLocked queues the answer's input behind the deliveries already
// queued. When the queue fills, it switches to scanning: the read cursor
// moves on and answers still reach their calls, but nothing is queued
// beyond queuedThrough.
func (r *relay) queueAnswerLocked(answer bridgeapi.SyncResponse) {
	if dropped := answer.Dropped; dropped != nil {
		// Dropped entries are never read again, so their marks go.
		for sequence := range r.scanResolved {
			if sequence <= dropped.Through {
				delete(r.scanResolved, sequence)
			}
		}
	}
	if dropped := answer.Dropped; dropped != nil && !r.scanning {
		notice := delivery{
			key: fmt.Sprintf("%s/dropped/%d-%d", r.connectionID, dropped.From, dropped.Through),
			text: fmt.Sprintf("Superverse messages %d-%d were dropped by retention while this agent was disconnected; "+
				"the repository and its commits hold the work.", dropped.From, dropped.Through),
		}
		if !r.enqueueDeliveryLocked(notice) {
			r.startScanLocked()
		}
	}
	for _, entry := range answer.Entries {
		if entry.Sequence <= r.readCursor {
			continue
		}
		r.readCursor = entry.Sequence
		// Every answer and tool result reaches its waiting call as soon as it
		// is read, whatever delivery waits; the entry's delivery accounts for
		// it, so the cursor passes it only once its MCP response was written.
		answered := r.answeredLocked(entry)
		acks, late := r.lateResultsLocked(entry)
		if answered != nil {
			acks = append(acks, answered)
		}
		if r.scanning {
			continue
		}
		input := delivery{key: r.connectionID + "/" + strconv.FormatUint(entry.Sequence, 10), entrySequence: entry.Sequence, acks: acks}
		send := late != "" || answered == nil && entry.Delivered(r.connectionID)
		if send {
			input.text = entry.Text
		}
		if late != "" {
			input.text = late
		}
		if (send || len(acks) > 0) && !r.enqueueDeliveryLocked(input) {
			r.startScanLocked()
			continue
		}
		r.queuedThrough = entry.Sequence
		delete(r.scanResolved, entry.Sequence)
	}
	r.readCursor = max(r.readCursor, answer.ExaminedThrough)
	if !r.scanning {
		r.queuedThrough = r.readCursor
	}
}

func (r *relay) startScanLocked() {
	r.scanning, r.scanSettled = true, r.settled
}

// answeredLocked resolves the waiting question an entry answers: that is
// the call's result, not a message, unless its MCP response is not written.
func (r *relay) answeredLocked(entry bridgeapi.Entry) *resultAck {
	if entry.AnsweredInputCallID == "" {
		return nil
	}
	return r.resolveEntryLocked(entry.Sequence, entry.AnsweredInputCallID, mcpserver.CallResult{Text: entry.Text}, entry.Text)
}

// lateResultsLocked resolves the waiting calls of a tool_result entry and
// returns their delivery obligations and, as a message for Codex, the
// results no call waits for any more: the wait expired, the MCP call ended
// or the Bridge restarted.
func (r *relay) lateResultsLocked(entry bridgeapi.Entry) ([]*resultAck, string) {
	var acks []*resultAck
	var late []string
	for _, result := range entry.ToolResults {
		if !bridgeapi.IsRemoteCallID(result.CallID) {
			continue
		}
		// A question's placeholder is not its result: the waiting call stays
		// open for the answer entry (answeredLocked) or its wait's end.
		if _, waiting := r.calls[result.CallID]; waiting && result.Status == bridgeapi.ToolResultStatusWaitingForUser {
			continue
		}
		outcome := "Result"
		if result.IsError {
			outcome = "Error result"
		}
		text := fmt.Sprintf("%s of Superverse call %s (it finished after the call was answered): %s",
			outcome, result.CallID, comm.TruncateUTF8(result.Content, bridgeapi.MaxEventTextBytes))
		if ack := r.resolveEntryLocked(entry.Sequence, result.CallID, mcpserver.CallResult{Text: result.Content, IsError: result.IsError}, text); ack != nil {
			acks = append(acks, ack)
			continue
		}
		late = append(late, text)
	}
	return acks, strings.Join(late, "\n\n")
}

// resolveEntryLocked answers callID's waiting call with the result entry
// carries and returns the delivery obligation, now or from an earlier read,
// or nil when no call waits: late is then what Codex gets instead. The call
// is marked on the entry, because a rewind reads the entry again after its
// call is gone; the mark lasts until queuedThrough passes the entry or
// retention drops it.
func (r *relay) resolveEntryLocked(entry uint64, callID string, result mcpserver.CallResult, late string) *resultAck {
	if index := slices.IndexFunc(r.scanResolved[entry], func(resolved resolvedCall) bool { return resolved.callID == callID }); index >= 0 {
		return r.scanResolved[entry][index].ack
	}
	call, ok := r.calls[callID]
	if !ok {
		return nil
	}
	delete(r.calls, callID)
	call.timing.SyncResultAt = time.Now().UnixMilli()
	ack := newResultAck(late)
	result.Delivered = ack.settle
	call.finish(result)
	if _, marked := r.scanResolved[entry]; !marked && len(r.scanResolved) >= maxScanResolved {
		delete(r.scanResolved, slices.Min(slices.Collect(maps.Keys(r.scanResolved))))
	}
	r.scanResolved[entry] = append(r.scanResolved[entry], resolvedCall{callID: callID, ack: ack})
	return ack
}

// enqueueDeliveryLocked queues a delivery once, or reports a full queue.
func (r *relay) enqueueDeliveryLocked(message delivery) bool {
	if slices.ContainsFunc(r.queue, func(queued delivery) bool { return queued.key == message.key }) {
		return true
	}
	if r.queueFullLocked() || r.queueBytes+len(message.text) > maxQueuedBytes && len(r.queue) > 0 {
		return false
	}
	r.queue = append(r.queue, message)
	r.queueBytes += len(message.text)
	select {
	case r.queued <- struct{}{}:
	default:
	}
	return true
}

func (r *relay) queueFullLocked() bool {
	return len(r.queue) >= maxQueuedDeliveries || r.queueBytes >= maxQueuedBytes
}

// deliveryLoop sends the queued deliveries one at a time, in order. Once the
// queue is empty, every queued input is settled, so the durable cursor joins
// queuedThrough.
func (r *relay) deliveryLoop(ctx context.Context) error {
	r.mu.Lock()
	ready := r.ready
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ready:
	}
	if err := r.sendBrief(ctx); err != nil {
		return err
	}
	for {
		r.mu.Lock()
		if len(r.queue) == 0 {
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-r.queued:
				continue
			}
		}
		head := r.queue[0]
		r.mu.Unlock()
		if err := r.deliverEntry(ctx, head); err != nil {
			return err
		}
		r.mu.Lock()
		r.queue = r.queue[1:]
		r.queueBytes -= len(head.text)
		r.settled++
		var err error
		if len(r.queue) == 0 {
			r.progress.Cursor = max(r.progress.Cursor, r.queuedThrough)
			err = r.saveLocked()
		}
		r.mu.Unlock()
		select {
		case r.dequeued <- struct{}{}:
		default:
		}
		if err != nil {
			return err
		}
	}
}

// resetQueueLocked starts reading again from the durable cursor, as after a
// restart: deliveries settle again in order, and tool results are found again
// by call ID.
func (r *relay) resetQueueLocked() {
	r.readCursor, r.queuedThrough, r.scanning = r.progress.Cursor, r.progress.Cursor, false
	r.queue, r.queueBytes, r.scanResolved = nil, 0, map[uint64][]resolvedCall{}
}

// refreshTools repeats the activation, which returns the current tool set.
func (r *relay) refreshTools(ctx context.Context) error {
	record := r.binding.Record
	activation, err := r.client.Activate(ctx, bridgeapi.Binding{
		OrcaVersion: record.OrcaVersion, CodexVersion: record.CodexVersion, OrcaSessionID: record.OrcaSessionID,
		ProviderThreadID: record.ProviderThreadID, HostLabel: record.HostLabel,
	})
	if err != nil {
		return detachOn(err)
	}
	r.setTools(activation.Tools, activation.ToolSetDigest)
	return nil
}
