// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package orca

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/relay"
	"github.com/superdurable/superversev2/cmd/superverse-bridge/state"
)

const (
	// maxBlocksBytes is Orca's limit on a send's blocks.
	maxBlocksBytes = 256 << 10
	// ownerRetryLimit bounds the resends while Orca's Codex owner recovers.
	ownerRetryLimit = 2 * time.Minute
)

// Method-level refusal codes the Bridge tells apart.
const (
	refusalOperationUnknown   = "agent_session_operation_unknown"
	refusalOperationExpired   = "agent_session_operation_expired"
	refusalOperationConflict  = "agent_session_operation_conflict"
	refusalOwnerReconciling   = "execution_owner_reconciling"
	refusalOwnerRestartFailed = "agent_session_owner_restart_failed"
)

// Withdrawal dispositions of agentSession.queuedMessageDelete.
const (
	dispositionDispatched = "dispatched"
)

// paramEnvelope is the params key of a mutation's envelope.
const paramEnvelope = "envelope"

// envelope is the strict mutation envelope; the fence is null on create.
type envelope struct {
	SessionID            string `json:"sessionId"`
	ClientOperationID    string `json:"clientOperationId"`
	ExpectedRuntimeFence *int64 `json:"expectedRuntimeFence"`
	PayloadFingerprint   string `json:"payloadFingerprint"`
}

func mutationEnvelope(sessionID, operationID string, fence *int64, fingerprint string) envelope {
	return envelope{SessionID: sessionID, ClientOperationID: operationID, ExpectedRuntimeFence: fence, PayloadFingerprint: fingerprint}
}

// mutationResult is a mutation's method-level answer.
type mutationResult[V any] struct {
	OK      bool `json:"ok"`
	Refusal *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"refusal"`
	Fence int64 `json:"fence"`
	Value V     `json:"value"`
}

func (result mutationResult[V]) refused() error {
	if result.OK {
		return nil
	}
	if result.Refusal == nil {
		return errors.New("orca refused the call without a reason")
	}
	return fmt.Errorf("orca refused the call: %s: %s", result.Refusal.Code, result.Refusal.Message)
}

type submission struct {
	DispatchState         string `json:"dispatchState"`
	Reason                string `json:"reason"`
	ProviderItemID        string `json:"providerItemId"`
	KeptAsQueuedMessageID string `json:"keptAsQueuedMessageId"`
}

// Send submits one user message, or asks about an earlier submission with
// the same operation ID: the params are rebuilt identically, and Orca's
// ledger answers a repeat from its journal. retryUnknown and delivery are
// never sent.
func (client *Client) Send(ctx context.Context, request relay.SendRequest) (relay.SendResult, error) {
	if !utf8.ValidString(request.Text) {
		return relay.SendResult{State: relay.SubmissionRejected, Reason: "the message is not valid UTF-8"}, nil
	}
	blocks := []any{object{"type": "text", "text": request.Text}}
	if len(canonical(blocks)) > maxBlocksBytes {
		return relay.SendResult{State: relay.SubmissionRejected, Reason: "the message exceeds Orca's 256 KiB limit"}, nil
	}
	body := object{"kind": "message", "role": "user", "blocks": blocks}
	fingerprint := payloadFingerprint(methodSend, request.SessionID, object{"body": body})
	started := time.Now()
	backoff := time.Second
	for {
		// Orca's send has no provider-thread precondition: the thread is read
		// just before and after each write (README, known gaps).
		if _, err := client.boundTail(ctx, request.SessionID, request.ProviderThreadID); err != nil {
			return relay.SendResult{}, err
		}
		fence := client.fence(request.SessionID)
		params := object{paramEnvelope: mutationEnvelope(request.SessionID, request.ClientOperationID, &fence, fingerprint), "body": body}
		var result mutationResult[struct {
			Submission submission `json:"submission"`
		}]
		_, err := client.transport.callAt(ctx, request.RuntimeID, methodSend, params, &result)
		if _, threadErr := client.boundTail(ctx, request.SessionID, request.ProviderThreadID); errors.Is(threadErr, relay.ErrConversationChanged) {
			return relay.SendResult{}, threadErr
		}
		if errors.Is(err, errRuntimeChanged) {
			return relay.SendResult{State: relay.SubmissionUnknown, Reason: "Orca restarted after the first submission"}, nil
		}
		if err != nil {
			return client.sendFailure(ctx, err)
		}
		if result.OK {
			client.noteFence(request.SessionID, result.Fence)
			return client.settled(ctx, request.SessionID, result.Value.Submission)
		}
		code, message := "", ""
		if result.Refusal != nil {
			code, message = result.Refusal.Code, result.Refusal.Message
		}
		switch code {
		case refusalOperationUnknown, refusalOperationExpired:
			return relay.SendResult{State: relay.SubmissionUnknown, Reason: message}, nil
		case refusalOwnerReconciling, refusalOwnerRestartFailed:
			if time.Since(started) < ownerRetryLimit {
				if err := pause(ctx, backoff); err != nil {
					return relay.SendResult{}, err
				}
				backoff = min(2*backoff, 15*time.Second)
				continue
			}
		case refusalOperationConflict:
			client.logger.ErrorContext(ctx, "orca refused a delivery's operation ID or fingerprint; this is a Bridge defect", "reason", message)
		}
		// Admission refused before Orca recorded anything.
		return relay.SendResult{State: relay.SubmissionRejected, Reason: fmt.Sprintf("%s: %s", code, message)}, nil
	}
}

// sendFailure maps an RPC-level failure of a send. A refusal without
// recorded data proves nothing was written; any other failure is uncertain
// and the relay asks again with the same operation.
func (client *Client) sendFailure(ctx context.Context, err error) (relay.SendResult, error) {
	var refused *rpcError
	if errors.As(err, &refused) {
		switch {
		case refused.Refusal == refusalOperationExpired:
			return relay.SendResult{State: relay.SubmissionUnknown, Reason: refused.Message}, nil
		case refused.Refusal == "" && (refused.Code == codeInvalidArgument || refused.Code == codeMethodNotFound || refused.Code == codeMethodNotSupported):
			client.logger.ErrorContext(ctx, "orca refused a delivery as incompatible", "code", refused.Code)
			return relay.SendResult{State: relay.SubmissionRejected, Reason: "Orca refused the message as incompatible: " + refused.Code}, nil
		}
	}
	return relay.SendResult{}, err
}

// settled maps a submission's dispatch state. A rejection that left a paused
// queued copy withdraws it, so a later resume cannot deliver it.
func (client *Client) settled(ctx context.Context, sessionID string, sub submission) (relay.SendResult, error) {
	switch relay.ParseSubmissionState(sub.DispatchState) {
	case relay.SubmissionAccepted:
		return relay.SendResult{State: relay.SubmissionAccepted, ProviderItemID: sub.ProviderItemID}, nil
	case relay.SubmissionPending:
		return relay.SendResult{State: relay.SubmissionPending}, nil
	case relay.SubmissionRejected:
		if sub.KeptAsQueuedMessageID == "" {
			return relay.SendResult{State: relay.SubmissionRejected, Reason: sub.Reason}, nil
		}
		disposition, err := client.withdraw(ctx, sessionID, sub.KeptAsQueuedMessageID)
		switch {
		case err != nil:
			client.logger.WarnContext(ctx, "withdraw Orca's queued copy of a rejected delivery", "error", err)
			return relay.SendResult{State: relay.SubmissionUnknown, Reason: sub.Reason}, nil
		case disposition == dispositionDispatched:
			// The copy was sent under its own message ID, which a send cannot
			// ask about: asking with the original only repeats the rejection.
			return relay.SendResult{State: relay.SubmissionUnknown, Reason: "Orca dispatched the queued copy of a rejected delivery"}, nil
		default:
			return relay.SendResult{State: relay.SubmissionRejected, Reason: sub.Reason}, nil
		}
	default:
		return relay.SendResult{State: relay.SubmissionUnknown, Reason: sub.Reason}, nil
	}
}

// withdraw deletes a queued message; it returns the disposition, empty when
// the message was deleted.
func (client *Client) withdraw(ctx context.Context, sessionID, messageID string) (string, error) {
	fence := client.fence(sessionID)
	fields := object{"messageId": messageID}
	params := object{
		paramEnvelope: mutationEnvelope(sessionID, relay.NewOperationID(time.Now()), &fence, payloadFingerprint(methodQueuedMessageDelete, sessionID, fields)),
		"messageId":   messageID,
	}
	var result mutationResult[struct {
		Deleted     bool   `json:"deleted"`
		Disposition string `json:"disposition"`
	}]
	if err := client.mutate(ctx, methodQueuedMessageDelete, params, &result); err != nil {
		return "", err
	}
	if err := result.refused(); err != nil {
		return "", err
	}
	if result.Value.Deleted {
		return "", nil
	}
	return result.Value.Disposition, nil
}

// Cancel stops the session's latest turn unless it is positively over (an
// unverifiable turn is cancelled too) and returns the event position once it
// is. The cancel names that turn's ID, so Orca cancels no other turn (another
// thread's turn included); the thread is confirmed before it and while
// waiting. An idle session is not cancelled, because a cancel would start a
// Codex process for it.
func (client *Client) Cancel(ctx context.Context, sessionID, providerThreadID string) (state.Position, error) {
	page, err := client.boundTail(ctx, sessionID, providerThreadID)
	if err != nil {
		return state.Position{}, err
	}
	if page.Page.idle() {
		return page.Page.LiveCursor.position(), nil
	}
	_, turnID, _ := page.Page.latestTurnIdentity()
	if turnID == "" {
		return state.Position{}, fmt.Errorf("orca session %s names no turn ID to cancel", sessionID)
	}
	fence := client.fence(sessionID)
	fields := object{"turnId": turnID}
	params := object{paramEnvelope: mutationEnvelope(sessionID, relay.NewOperationID(time.Now()), &fence, payloadFingerprint(methodCancel, sessionID, fields)),
		"turnId": turnID}
	var result mutationResult[struct {
		Cancelled bool `json:"cancelled"`
	}]
	if err := client.mutate(ctx, methodCancel, params, &result); err != nil {
		return state.Position{}, err
	}
	if err := result.refused(); err != nil {
		return state.Position{}, err
	}
	return client.waitIdle(ctx, sessionID, providerThreadID)
}

// waitIdle polls the session, for at most maxIdleWait, until its latest turn
// is positively over (completed, interrupted or none) and returns its
// position. An unverifiable or unknown turn is not over: it keeps waiting and
// fails at the deadline, never reporting success. A providerThreadID other
// than empty must be named by the page that proves the turn over: another
// thread fails with relay.ErrConversationChanged, and a page naming none
// proves nothing, so it keeps waiting and fails at the deadline.
func (client *Client) waitIdle(ctx context.Context, sessionID, providerThreadID string) (state.Position, error) {
	ctx, stop := context.WithTimeout(ctx, maxIdleWait)
	defer stop()
	for {
		page, err := client.tail(ctx, sessionID)
		if err != nil {
			return state.Position{}, err
		}
		confirmed := true
		if providerThreadID != "" {
			if err := sameThread(page, providerThreadID); err != nil {
				return state.Position{}, err
			}
			confirmed = page.ProviderSession != nil && page.ProviderSession.ID != ""
		}
		if confirmed && page.Page.idle() {
			return page.Page.LiveCursor.position(), nil
		}
		if err := pause(ctx, idlePoll); err != nil {
			if !confirmed {
				return state.Position{}, fmt.Errorf("orca session %s names no Codex thread to confirm the stop: %w", sessionID, err)
			}
			return state.Position{}, fmt.Errorf("wait for the Codex turn to stop: %w", err)
		}
	}
}

func pause(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(duration):
		return nil
	}
}
