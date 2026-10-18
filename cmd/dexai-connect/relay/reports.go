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
	"sync"
	"time"

	"github.com/superdurable/superversev2/cmd/dexai-connect/mcpserver"
	"github.com/superdurable/superversev2/cmd/dexai-connect/platform"
	"github.com/superdurable/superversev2/cmd/dexai-connect/state"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// mcpToolTimeoutSeconds is Codex's timeout for a Superverse tool call, which
// the Bridge writes to Codex's configuration as tool_timeout_sec.
const mcpToolTimeoutSeconds = 1800

// toolWaitBudget bounds how long the Bridge holds an MCP tool call open, for
// every tool, so it answers before Codex's timeout. Expiry leaves the outcome
// pending: the Session records the result, which arrives later as a message.
const toolWaitBudget = (mcpToolTimeoutSeconds - 30) * time.Second

const (
	// turnWait is how long a tool call waits for its turn's start event.
	turnWait = 5 * time.Second
	// protocolRetry is the pause after an answer the Bridge does not understand.
	protocolRetry = 5 * time.Second
)

// pendingCall is an MCP tool call waiting for its result.
type pendingCall struct {
	callID       string
	turnID       string
	result       chan mcpserver.CallResult
	accepted     chan struct{}
	acceptedOnce sync.Once
	timing       mcpserver.CallTiming
}

func newPendingCall(turnID string) *pendingCall {
	return &pendingCall{turnID: turnID, result: make(chan mcpserver.CallResult, 1), accepted: make(chan struct{})}
}

func (call *pendingCall) accept() { call.acceptedOnce.Do(func() { close(call.accepted) }) }

// finish delivers the call's result once; a later result is dropped, and
// its delivery obligation counts as undelivered.
func (call *pendingCall) finish(result mcpserver.CallResult) {
	result.Timing = call.timing
	select {
	case call.result <- result:
	default:
		if result.Delivered != nil {
			result.Delivered(false)
		}
	}
}

// resultAck is the delivery obligation of a result or answer handed to a
// live MCP call: settled once, by the MCP process's acknowledgement that it
// wrote the response to Codex, or as undelivered. Late is the message that
// tells Codex instead when it was not delivered.
type resultAck struct {
	once      sync.Once
	done      chan struct{}
	delivered bool
	late      string
}

func newResultAck(late string) *resultAck {
	return &resultAck{done: make(chan struct{}), late: late}
}

func (ack *resultAck) settle(delivered bool) {
	ack.once.Do(func() {
		ack.delivered = delivered
		close(ack.done)
	})
}

// call reports one MCP tool call of Codex and waits for its tool_result
// entry, an answer to it, a refusal, or the end of the wait budget.
func (r *relay) call(ctx context.Context, name string, arguments json.RawMessage) (mcpserver.CallResult, error) {
	if refused := r.readyResult(); refused != nil {
		return *refused, nil
	}
	if !r.offered(name) {
		return mcpserver.CallResult{Text: fmt.Sprintf("the tool %q is not offered by Superverse", name), IsError: true}, nil
	}
	turnID, generation := r.callTurn(ctx)
	r.mu.Lock()
	current := r.progress.Generation
	r.mu.Unlock()
	if generation == 0 || generation != current {
		return interruptedResult, nil
	}
	if refused := r.confirmConversation(ctx); refused != nil {
		return *refused, nil
	}
	call := newPendingCall(turnID)
	report := bridgeapi.Report{Generation: generation, ProviderTurn: turnID, ToolCall: &bridgeapi.ToolCall{Name: name, Arguments: arguments}}
	r.mu.Lock()
	if r.runningTurn != turnID {
		r.mu.Unlock()
		return mcpserver.CallResult{Text: "the Codex turn ended before Superverse accepted this call; retry in a new turn", IsError: true}, nil
	}
	err := r.storeLocked(r.progress, []bridgeapi.Report{report}, call)
	r.mu.Unlock()
	if errors.Is(err, state.ErrStoreFull) {
		return mcpserver.CallResult{Text: err.Error(), IsError: true}, nil
	}
	if err != nil {
		return mcpserver.CallResult{}, err
	}
	timer := time.NewTimer(r.toolWait)
	defer timer.Stop()
	select {
	case result := <-call.result:
		return result, nil
	case <-accepted(name, call):
		return r.abandonWith(call, "its connector checks started; its result will arrive as a message naming that call. Do not run it again."), nil
	case <-timer.C:
		return r.abandon(call), nil
	case <-ctx.Done():
		if result := r.abandon(call); result.Delivered != nil {
			result.Delivered(false)
		}
		return mcpserver.CallResult{}, ctx.Err()
	}
}

// abandon stops waiting for a call whose outcome is still pending; it never
// reports the effect as failed. Its result, or an unanswered question's
// answers, arrive later as an ordinary message. A result that arrived as the
// wait ended is returned, so its delivery obligation is settled.
func (r *relay) abandon(call *pendingCall) mcpserver.CallResult {
	return r.abandonWith(call, "")
}

func accepted(name string, call *pendingCall) <-chan struct{} {
	if name == string(conversation.ToolSubmitConnectorContribution) {
		return call.accepted
	}
	return nil
}

func (r *relay) abandonWith(call *pendingCall, acceptedText string) mcpserver.CallResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case result := <-call.result:
		return result
	default:
	}
	for slot, pending := range r.slotCalls {
		if pending == call {
			delete(r.slotCalls, slot)
		}
	}
	if call.callID != "" {
		delete(r.calls, call.callID)
	}
	if call.callID != "" && call.callID == r.session.question {
		return mcpserver.CallResult{Text: "the question is still open; the answers will arrive as a message"}
	}
	if call.callID != "" {
		if acceptedText != "" {
			return mcpserver.CallResult{Text: fmt.Sprintf("Superverse call %s was accepted and %s", call.callID, acceptedText)}
		}
		return mcpserver.CallResult{Text: fmt.Sprintf("Superverse call %s is still running after %s; its result will arrive "+
			"as a message naming that call. Do not run it again.", call.callID, r.toolWait)}
	}
	return mcpserver.CallResult{Text: fmt.Sprintf("the call is still running after %s; its result will arrive as a "+
		"message. Do not run it again.", r.toolWait)}
}

// confirmConversation admits a call only while the Orca session holds the
// bound provider thread, read just before the call is stored; another thread
// stops the relay. A change after this read is the gap the README records.
func (r *relay) confirmConversation(ctx context.Context) *mcpserver.CallResult {
	conversation, err := r.orca.Conversation(ctx, r.sessionID)
	switch {
	case err != nil:
		return &mcpserver.CallResult{Text: "the Bridge could not confirm the Orca conversation: " + err.Error(), IsError: true}
	case conversation.ProviderThreadID == "":
		return &mcpserver.CallResult{Text: "the Bridge could not confirm the Orca conversation's Codex thread", IsError: true}
	case conversation.ProviderThreadID != r.binding.Record.ProviderThreadID:
		r.mu.Lock()
		select {
		case <-r.lost:
		default:
			close(r.lost)
		}
		r.mu.Unlock()
		return &mcpserver.CallResult{Text: ErrConversationChanged.Error(), IsError: true}
	}
	return nil
}

const turnEndedResult = "this Codex turn ended before Superverse answered the call; its result will arrive as a message. Do not run it again."

// retireTurnCallsLocked leaves results of a completed Codex turn for the
// ordinary delivery path. An MCP response to that turn can no longer inform
// the agent, even when its transport remains open.
func (r *relay) retireTurnCallsLocked(turnID string) {
	for slot, call := range r.slotCalls {
		if call.turnID == turnID {
			delete(r.slotCalls, slot)
			call.finish(mcpserver.CallResult{Text: turnEndedResult})
		}
	}
	for id, call := range r.calls {
		if call.turnID == turnID {
			delete(r.calls, id)
			call.finish(mcpserver.CallResult{Text: turnEndedResult})
		}
	}
}

// callTurn returns the running Codex turn and its generation, waiting
// briefly for a turn whose start event has not arrived yet.
func (r *relay) callTurn(ctx context.Context) (string, uint64) {
	deadline := time.NewTimer(turnWait)
	defer deadline.Stop()
	for {
		r.mu.Lock()
		turnID, started := r.runningTurn, r.turnStarted
		generation, _ := r.turnGenerationLocked(turnID)
		r.mu.Unlock()
		if turnID != "" {
			return turnID, generation
		}
		select {
		case <-started:
		case <-deadline.C:
			return "", 0
		case <-ctx.Done():
			return "", 0
		}
	}
}

func (r *relay) offered(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.ContainsFunc(r.tools, func(tool bridgeapi.Tool) bool { return tool.Name == name })
}

// storeLocked stores reports and makes progress the relay's, in one atomic
// write, so the reports and the progress that accounts for them are durable
// together. A report of a generation that is not current would be refused, so
// it is dropped; call, the MCP call of a single tool call report, is then
// answered interrupted. A failure leaves the relay's progress unchanged. The
// caller holds mu.
func (r *relay) storeLocked(progress state.Progress, reports []bridgeapi.Report, call *pendingCall) error {
	current := slices.DeleteFunc(slices.Clone(reports), func(report bridgeapi.Report) bool {
		return report.Generation == 0 || report.Generation != progress.Generation
	})
	if call != nil && len(current) == 0 {
		call.finish(interruptedResult)
		return nil
	}
	slots, err := r.binding.StoreReports(progress, current...)
	if err != nil {
		return err
	}
	r.progress = progress
	if call != nil {
		r.slotCalls[slots[0]] = call
	}
	if len(slots) > 0 {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
	return nil
}

// sendLoop sends the stored reports one at a time, in order. Only the head
// report carries a sequence, Acknowledged+1; a resend after a lost answer
// repeats it, and a refused report frees it for the next.
func (r *relay) sendLoop(ctx context.Context) error {
	for {
		reports, err := r.binding.StoredReports()
		if err != nil {
			return err
		}
		if len(reports) == 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-r.wake:
				continue
			}
		}
		if err := r.sendHead(ctx, reports[0]); err != nil {
			return err
		}
	}
}

func (r *relay) sendHead(ctx context.Context, head state.StoredReport) error {
	report := head.Report
	r.mu.Lock()
	acknowledged, epoch, generation := r.progress.Acknowledged, r.progress.Epoch, r.progress.Generation
	r.mu.Unlock()
	switch {
	case report.Sequence != 0 && report.Sequence <= acknowledged:
		// Acknowledged before a restart: the drain may not have seen it.
		r.mu.Lock()
		var err error
		if r.progress.Draining != nil {
			r.admitDrainLocked(head, report)
			err = r.saveLocked()
		}
		r.mu.Unlock()
		if err != nil {
			return err
		}
		return r.settleHead(head, nil)
	case report.Sequence == 0 && report.Generation != generation:
		// Never sent, and it would be refused. A sent report is always resent,
		// because the Session may have admitted it.
		return r.settleHead(head, &interruptedResult)
	case report.Sequence == 0:
		report.Sequence, report.Epoch = acknowledged+1, epoch
		if err := r.binding.UpdateReport(head.Slot, report); err != nil {
			return err
		}
	}
	r.bindCall(head.Slot, report)
	if report.ToolCall != nil {
		r.mu.Lock()
		if call := r.slotCalls[head.Slot]; call != nil && call.timing.ReportedAt == 0 {
			call.timing.ReportedAt = time.Now().UnixMilli()
		}
		r.mu.Unlock()
	}
	answer, err := r.client.Report(ctx, report)
	if report.ToolCall != nil {
		r.mu.Lock()
		if call := r.slotCalls[head.Slot]; call != nil {
			call.timing.ReportAnswerAt = time.Now().UnixMilli()
		}
		r.mu.Unlock()
	}
	if err == nil {
		r.mu.Lock()
		r.progress.Acknowledged = max(r.progress.Acknowledged, answer.Reported, report.Sequence)
		r.admitDrainLocked(head, report)
		err = r.saveLocked()
		if err == nil {
			if call := r.slotCalls[head.Slot]; call != nil {
				call.accept()
			}
		}
		r.mu.Unlock()
		if err != nil {
			return err
		}
		return r.settleHead(head, nil)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	code, refused := platform.RefusalCode(err)
	var refusal *platform.RefusalError
	errors.As(err, &refusal)
	switch {
	case errors.Is(err, platform.ErrInvalidRequest):
		// Refused before it was sent, and it would be refused again: the
		// Session never saw it, so its sequence is free for the next report.
		r.logger.ErrorContext(ctx, "a report is invalid and was not sent; dropping it", "project", r.projectName,
			"sequence", report.Sequence, "error", err)
		return r.settleHead(head, &mcpserver.CallResult{Text: "the Bridge could not send the call: " + err.Error(), IsError: true})
	case !refused:
		r.logger.WarnContext(ctx, "report not answered; retrying", "project", r.projectName, "sequence", report.Sequence, "error", err)
		return sleep(ctx, protocolRetry)
	case code == bridgeapi.ErrorStaleGeneration:
		return r.settleHead(head, &interruptedResult)
	case code == bridgeapi.ErrorSequenceGap:
		// The Session's sequence is the truth: resend the head with it.
		report.Sequence = 0
		if updateErr := r.binding.UpdateReport(head.Slot, report); updateErr != nil {
			return updateErr
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.progress.Acknowledged = max(refusal.Body.ExpectedSequence, 1) - 1
		return r.saveLocked()
	case code == bridgeapi.ErrorMalformed || code == bridgeapi.ErrorTooLarge:
		r.logger.ErrorContext(ctx, "the platform refused a report", "project", r.projectName, "sequence", report.Sequence, "error", err)
		return r.settleHead(head, &mcpserver.CallResult{Text: "Superverse refused the call: " + refusal.Body.Message, IsError: true})
	default:
		return detachOn(err)
	}
}

// bindCall gives a stored call its call ID, remote-<epoch>-<sequence>, the
// ID of the tool_result entry that answers it.
func (r *relay) bindCall(slot uint64, report bridgeapi.Report) {
	r.mu.Lock()
	defer r.mu.Unlock()
	call, ok := r.slotCalls[slot]
	if !ok || report.ToolCall == nil {
		return
	}
	call.callID = bridgeapi.RemoteCallID(report.Epoch, report.Sequence)
	call.timing.CallID = call.callID
	r.calls[call.callID] = call
}

// settleHead removes the head report; a refused call gets result.
func (r *relay) settleHead(head state.StoredReport, result *mcpserver.CallResult) error {
	r.mu.Lock()
	call, ok := r.slotCalls[head.Slot]
	delete(r.slotCalls, head.Slot)
	if ok && result != nil {
		delete(r.calls, call.callID)
		call.finish(*result)
	}
	r.mu.Unlock()
	return r.binding.RemoveReport(head.Slot)
}

func sleep(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
	case <-time.After(duration):
	}
	return nil
}
