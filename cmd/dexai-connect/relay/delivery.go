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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/cmd/dexai-connect/state"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

const (
	// pendingRetry is how often a pending submission is asked about again.
	pendingRetry = 2 * time.Second
	// historyProofTimeout bounds one positive check after a one-write send.
	historyProofTimeout = 10 * time.Second
	// journalLimit is how long Orca's submission journal keeps a submission.
	journalLimit = 24 * time.Hour
	// briefEntries is how many recent entries the brief reads.
	briefEntries = 20
	// maxBriefMessageBytes bounds each message the brief quotes.
	maxBriefMessageBytes = 2 << 10
	briefKey             = "brief"
	contextFollowupKey   = "repository-context"
	briefContextWait     = 3 * time.Minute
	contextFollowupRetry = 15 * time.Second
	contextFollowupLimit = 30 * time.Minute
)

// sessionState is the state sync last returned. A changed digest replaces
// every value, so a field absent from that answer clears; an unchanged digest
// keeps them all.
type sessionState struct {
	digest       string
	question     string
	requirements string
	todos        []bridgeapi.Todo
}

func (session *sessionState) apply(answer bridgeapi.SyncResponse) {
	if answer.StateDigest == session.digest {
		return
	}
	*session = sessionState{digest: answer.StateDigest, requirements: answer.Requirements, todos: answer.Todos}
	if answer.PendingQuestion != nil {
		session.question = answer.PendingQuestion.CallID
	}
}

// delivery is one message the Bridge sends to the bound session, or the
// account of an entry's results handed to live MCP calls (acks): one whose
// MCP response was not written is told as a late-result message instead.
type delivery struct {
	key           string
	entrySequence uint64
	text          string
	acks          []*resultAck
}

// deliverEntry settles a queued delivery: it waits until each result handed
// to a live MCP call is acknowledged or not, then sends the text with the
// undelivered results, if any is left to send.
func (r *relay) deliverEntry(ctx context.Context, message delivery) error {
	parts := []string{message.text}
	for _, ack := range message.acks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ack.done:
		}
		if !ack.delivered {
			parts = append(parts, ack.late)
		}
	}
	message.text = strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), "\n\n")
	if message.text == "" {
		return nil
	}
	return r.deliver(ctx, message)
}

// deliver persists identity before a provider write. A journal provider may
// query the same operation again; a one-write provider may only read history.
func (r *relay) deliver(ctx context.Context, message delivery) error {
	r.mu.Lock()
	sending := r.progress.Sending
	r.mu.Unlock()
	if sending != nil && sending.Key != message.key {
		// The record names another message, which this one follows.
		if err := r.finishDelivery(*sending, SendResult{State: SubmissionUnknown}); err != nil {
			return err
		}
		sending = nil
	}
	if sending == nil {
		mode := r.provider.SubmissionMode()
		if mode != SubmissionJournal && mode != SubmissionOneWrite {
			panic("provider declared an unknown submission mode")
		}
		now := time.Now()
		var runtimeID, marker string
		if mode == SubmissionJournal {
			runtime, err := r.provider.Runtime(ctx)
			if err != nil {
				return fmt.Errorf("read the %s runtime fence: %w", r.provider.ProviderName(), err)
			}
			runtimeID = runtime.InstanceID
		} else {
			if _, ok := r.provider.(OneWriteProvider); !ok {
				panic("one-write provider cannot prove a submitted marker")
			}
			marker = NewOperationID(now)
		}
		operationID := NewOperationID(now)
		messageID := r.provider.SubmittedMessageID(operationID, marker)
		if messageID == "" {
			panic("provider returned an empty submitted-message ID")
		}
		r.mu.Lock()
		sending = &state.Sending{
			Key: message.key, EntrySequence: message.entrySequence, Generation: r.progress.Generation,
			OperationID: operationID, Fingerprint: fingerprint(message.text), InstanceID: runtimeID,
			Marker: marker, OneWrite: mode == SubmissionOneWrite, FirstSentAt: now,
		}
		r.progress.Sending = sending
		rememberDelivery(&r.progress, messageID, sending.Generation)
		err := r.saveLocked()
		r.mu.Unlock()
		if err != nil {
			return err
		}
	}
	if sending.Fingerprint != fingerprint(message.text) {
		return r.finishDelivery(*sending, SendResult{State: SubmissionUnknown})
	}
	result, err := r.settle(ctx, *sending, message.text)
	if err != nil {
		return err
	}
	return r.finishDelivery(*sending, result)
}

// settle chooses the persisted delivery rule, including after restart.
func (r *relay) settle(ctx context.Context, sending state.Sending, text string) (SendResult, error) {
	if sending.OneWrite {
		return r.settleOneWrite(ctx, sending, text)
	}
	return r.settleJournal(ctx, sending, text)
}

// settleJournal preserves Orca's retryable journal behavior.
func (r *relay) settleJournal(ctx context.Context, sending state.Sending, text string) (SendResult, error) {
	backoff := pendingRetry
	for {
		if time.Since(sending.FirstSentAt) > journalLimit {
			return SendResult{State: SubmissionUnknown}, nil
		}
		result, sendErr := r.provider.Send(ctx, SendRequest{SessionID: r.sessionID, ProviderThreadID: r.binding.Record.ProviderThreadID,
			Text: text, ClientOperationID: sending.OperationID, InstanceID: sending.InstanceID})
		if ctx.Err() != nil {
			return SendResult{}, ctx.Err()
		}
		if isSessionChanged(sendErr) {
			return SendResult{}, sendErr
		}
		if errors.Is(sendErr, ErrUnsupportedProtocol) {
			return SendResult{}, sendErr
		}
		if sendErr == nil && (result.State == SubmissionAccepted || result.State == SubmissionRejected) {
			return result, nil
		}
		runtime, err := r.provider.Runtime(ctx)
		if err == nil && runtime.InstanceID != sending.InstanceID {
			return SendResult{State: SubmissionUnknown}, nil
		}
		if sendErr == nil && result.State == SubmissionUnknown {
			return result, nil
		}
		if sendErr != nil {
			r.logger.WarnContext(ctx, "Orca did not answer a delivery; asking again", "project", r.projectName, "after", backoff, "error", sendErr)
			backoff = min(2*backoff, 30*time.Second)
		}
		if err := sleep(ctx, backoff); err != nil {
			return SendResult{}, err
		}
		if sendErr == nil {
			backoff = pendingRetry
		}
	}
}

// settleOneWrite marks the only provider write as attempted before calling it.
// A crash before the call sacrifices delivery certainty rather than risking a
// duplicate. Only an accepted response or exact marker proof can confirm it.
func (r *relay) settleOneWrite(ctx context.Context, sending state.Sending, text string) (SendResult, error) {
	provider, ok := r.provider.(OneWriteProvider)
	if !ok {
		panic("one-write delivery resumed with a provider lacking history proof")
	}
	if !sending.Attempted {
		r.mu.Lock()
		r.progress.Sending.Attempted = true
		err := r.saveLocked()
		r.mu.Unlock()
		if err != nil {
			return SendResult{}, err
		}
		sending.Attempted = true
		result, sendErr := provider.Send(ctx, SendRequest{
			SessionID: r.sessionID, ProviderThreadID: r.binding.Record.ProviderThreadID,
			Text: text, ClientOperationID: sending.OperationID, Marker: sending.Marker,
		})
		if ctx.Err() != nil {
			return SendResult{}, ctx.Err()
		}
		if isSessionChanged(sendErr) {
			return SendResult{}, sendErr
		}
		if errors.Is(sendErr, ErrUnsupportedProtocol) {
			return SendResult{}, sendErr
		}
		if sendErr == nil {
			if result.State == SubmissionRejected {
				return result, nil
			}
			if result.State == SubmissionAccepted && result.ProviderItemID != "" {
				return result, nil
			}
		}
	}
	proofCtx, cancel := context.WithTimeout(ctx, historyProofTimeout)
	defer cancel()
	itemID, err := provider.FindSubmittedMessage(proofCtx, r.sessionID, r.binding.Record.ProviderThreadID, sending.Marker)
	if ctx.Err() != nil {
		return SendResult{}, ctx.Err()
	}
	if isSessionChanged(err) {
		return SendResult{}, err
	}
	if errors.Is(err, ErrUnsupportedProtocol) {
		return SendResult{}, err
	}
	if err != nil {
		r.logger.WarnContext(ctx, "provider history could not prove delivery", "project", r.projectName, "error", err)
	}
	if err != nil || itemID == "" {
		return SendResult{State: SubmissionUnknown}, nil
	}
	return SendResult{State: SubmissionAccepted, ProviderItemID: itemID}, nil
}

// finishDelivery reports an outcome the provider could not confirm, advances the
// cursor and clears the sending record, all in one write.
func (r *relay) finishDelivery(sending state.Sending, result SendResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	progress := r.progress
	var reports []bridgeapi.Report
	if sending.EntrySequence != 0 && result.State != SubmissionAccepted {
		event := bridgeapi.Event{Kind: bridgeapi.EventKindDeliveryUncertain, EntrySequence: sending.EntrySequence}
		if result.State == SubmissionRejected {
			event = bridgeapi.Event{Kind: bridgeapi.EventKindDeliveryFailed, Text: comm.TruncateUTF8(result.Reason, 1<<10), EntrySequence: sending.EntrySequence}
		}
		reports = append(reports, bridgeapi.Report{Generation: progress.Generation, Events: []bridgeapi.Event{event}})
	}
	if result.State == SubmissionAccepted {
		rememberDelivery(&progress, result.ProviderItemID, sending.Generation)
	}
	if sending.Key == briefKey {
		progress.BriefSent = true
		progress.BriefAccepted = result.State == SubmissionAccepted
		progress.BriefText = ""
	}
	if sending.Key == contextFollowupKey {
		progress.ContextFollowupSent = true
		progress.ContextFollowupText = ""
	}
	progress.Cursor = max(progress.Cursor, sending.EntrySequence)
	progress.Sending = nil
	return r.storeLocked(progress, reports, nil)
}

// sendBrief sends Codex its first message once: the platform's agent context,
// the requirements, HEAD, the todo list and the recent public messages.
func (r *relay) sendBrief(ctx context.Context) error {
	r.mu.Lock()
	sent, epoch, cursor, sending, savedText := r.progress.BriefSent, r.progress.Epoch, r.progress.BriefActivationCursor, r.progress.Sending, r.progress.BriefText
	r.mu.Unlock()
	if sent {
		return nil
	}
	if sending != nil && sending.Key != briefKey {
		return nil
	}
	if savedText != "" {
		return r.deliver(ctx, delivery{key: briefKey, text: savedText})
	}
	r.mu.Lock()
	if r.progress.BriefWaitStartedAt.IsZero() {
		r.progress.BriefWaitStartedAt = time.Now().UTC()
		if err := r.saveLocked(); err != nil {
			r.mu.Unlock()
			return err
		}
	}
	started := r.progress.BriefWaitStartedAt
	r.mu.Unlock()
	for {
		answer, err := r.client.Sync(ctx, epoch, cursor-min(cursor, briefEntries), "", true)
		if err != nil {
			return err
		}
		if answer.AgentContextState != bridgeapi.AgentContextPending || time.Since(started) >= briefContextWait {
			r.noteAgentContext(answer)
			brief := composeBrief(r.projectName, answer, cursor)
			r.mu.Lock()
			r.progress.BriefWithoutContext = answer.AgentContextState == bridgeapi.AgentContextPending
			r.progress.BriefText = brief
			err = r.saveLocked()
			r.mu.Unlock()
			if err != nil {
				return err
			}
			return r.deliver(ctx, delivery{key: briefKey, text: brief})
		}
		if err := sleep(ctx, pendingRetry); err != nil {
			return err
		}
	}
}

// sendContextFollowup delivers the platform's section after a timed-out brief.
// A positive delay is the next bounded check; zero means no follow-up timer.
func (r *relay) sendContextFollowup(ctx context.Context) (time.Duration, error) {
	r.mu.Lock()
	needed := r.progress.BriefSent && r.progress.BriefWithoutContext && !r.progress.ContextFollowupSent && !r.progress.ContextFollowupAbandoned
	epoch, cursor, savedText, sending := r.progress.Epoch, r.progress.Cursor, r.progress.ContextFollowupText, r.progress.Sending
	r.mu.Unlock()
	if !needed {
		return 0, nil
	}
	if sending != nil && sending.Key != contextFollowupKey {
		return contextFollowupRetry, nil
	}
	if savedText != "" {
		return 0, r.deliver(ctx, delivery{key: contextFollowupKey, text: savedText})
	}
	now := time.Now().UTC()
	r.mu.Lock()
	if r.progress.ContextFollowupStartedAt.IsZero() {
		r.progress.ContextFollowupStartedAt = now
	}
	deadline := r.progress.ContextFollowupStartedAt.Add(contextFollowupLimit)
	if !now.Before(deadline) {
		r.progress.ContextFollowupAbandoned = true
		err := r.saveLocked()
		r.mu.Unlock()
		if err == nil {
			r.logger.InfoContext(ctx, "repository context follow-up abandoned after timeout", "project", r.projectName)
		}
		return 0, err
	}
	if next := r.progress.ContextFollowupCheckedAt.Add(contextFollowupRetry); now.Before(next) {
		r.mu.Unlock()
		return min(time.Until(next), time.Until(deadline)), nil
	}
	r.progress.ContextFollowupCheckedAt = now
	err := r.saveLocked()
	r.mu.Unlock()
	if err != nil {
		return 0, err
	}
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	answer, err := r.client.Sync(pollCtx, epoch, cursor, "", true)
	cancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return 0, r.abandonContextFollowup(ctx, "repository context follow-up abandoned after timeout")
		}
		return 0, err
	}
	if answer.AgentContextState == bridgeapi.AgentContextPending {
		return min(contextFollowupRetry, time.Until(deadline)), nil
	}
	if answer.AgentContextState == bridgeapi.AgentContextUnavailable {
		return 0, r.abandonContextFollowup(ctx, "repository context follow-up unavailable")
	}
	const open, close = "<repository_context>\n", "\n</repository_context>"
	start := strings.Index(answer.AgentContext, open)
	if start < 0 {
		return 0, r.abandonContextFollowup(ctx, "repository context follow-up ready without section")
	}
	end := strings.LastIndex(answer.AgentContext[start+len(open):], close)
	if end < 0 {
		return 0, r.abandonContextFollowup(ctx, "repository context follow-up ready without closing tag")
	}
	section := answer.AgentContext[start : start+len(open)+end+len(close)]
	r.mu.Lock()
	r.progress.ContextFollowupText = section
	err = r.saveLocked()
	r.mu.Unlock()
	if err != nil {
		return 0, err
	}
	return 0, r.deliver(ctx, delivery{key: contextFollowupKey, text: section})
}

func (r *relay) abandonContextFollowup(ctx context.Context, reason string) error {
	r.mu.Lock()
	r.progress.ContextFollowupAbandoned = true
	err := r.saveLocked()
	r.mu.Unlock()
	if err == nil {
		r.logger.InfoContext(ctx, reason, "project", r.projectName)
	}
	return err
}

// composeBrief frames the platform's data: its agent context, then the
// Project's name, requirements, HEAD, todo list and recent messages. The
// Bridge adds no instructions of its own.
func composeBrief(projectName string, answer bridgeapi.SyncResponse, activationCursor uint64) string {
	var brief strings.Builder
	if answer.AgentContext != "" {
		fmt.Fprintf(&brief, "%s\n\n", answer.AgentContext)
	}
	fmt.Fprintf(&brief, "Superverse Project: %s\n", projectName)
	if answer.Requirements != "" {
		fmt.Fprintf(&brief, "\nCurrent requirements:\n%s\n", answer.Requirements)
	}
	if answer.Head != nil {
		fmt.Fprintf(&brief, "\nHEAD: %s %s\n", answer.Head.Commit, answer.Head.Branch)
	}
	if len(answer.Todos) > 0 {
		brief.WriteString("\nTodo list:\n")
		for _, todo := range answer.Todos {
			fmt.Fprintf(&brief, "- [%s] %s\n", todo.Status, todo.Content)
		}
	}
	var recent []string
	for _, entry := range answer.Entries {
		if entry.Sequence <= activationCursor && entry.Text != "" && entry.Kind == bridgeapi.EntryKindUserMessage {
			recent = append(recent, fmt.Sprintf("%s: %s", entry.Role, comm.TruncateUTF8(entry.Text, maxBriefMessageBytes)))
		}
	}
	if len(recent) > 0 {
		fmt.Fprintf(&brief, "\nRecent messages (oldest first):\n%s\n", strings.Join(recent, "\n"))
	}
	return strings.TrimSuffix(brief.String(), "\n")
}

// NewOperationID is a new Orca client operation ID: a 13-digit millisecond
// timestamp, a hyphen and 32 random hex digits.
func NewOperationID(now time.Time) string {
	random := make([]byte, 16)
	comm.Must(rand.Read(random))
	return fmt.Sprintf("%013d-%s", now.UnixMilli(), hex.EncodeToString(random))
}

func fingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
