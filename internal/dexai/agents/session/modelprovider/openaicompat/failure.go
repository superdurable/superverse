// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// failureCode is the adapter-owned closed set of deterministic protocol rejections.
type failureCode string

const (
	failureDeltaAfterItemDone      failureCode = "delta_after_item_done"
	failureDuplicateOutputItem     failureCode = "duplicate_output_item"
	failureDuplicateSignature      failureCode = "duplicate_signature"
	failureDuplicateToolIdentity   failureCode = "duplicate_tool_identity"
	failureIncompleteResponse      failureCode = "incomplete_response"
	failureInconsistentResponse    failureCode = "inconsistent_response"
	failureInvalidChoiceOrder      failureCode = "invalid_choice_order"
	failureInvalidDeltaIdentity    failureCode = "invalid_delta_identity"
	failureInvalidDeltaType        failureCode = "invalid_delta_type"
	failureInvalidFinishReason     failureCode = "invalid_finish_reason"
	failureInvalidHistory          failureCode = "invalid_history"
	failureInvalidMockProgress     failureCode = "invalid_mock_progress"
	failureInvalidOutputIdentity   failureCode = "invalid_output_identity"
	failureInvalidOutputIndex      failureCode = "invalid_output_index"
	failureInvalidOutputOrder      failureCode = "invalid_output_order"
	failureInvalidReasoningReplay  failureCode = "invalid_reasoning_replay"
	failureInvalidReplay           failureCode = "invalid_replay"
	failureInvalidStream           failureCode = "invalid_stream"
	failureInvalidStreamEvent      failureCode = "invalid_stream_event"
	failureInvalidStreamOrder      failureCode = "invalid_stream_order"
	failureInvalidTextAggregation  failureCode = "invalid_text_aggregation"
	failureInvalidToolAggregation  failureCode = "invalid_tool_aggregation"
	failureInvalidToolArguments    failureCode = "invalid_tool_arguments"
	failureInvalidToolCall         failureCode = "invalid_tool_call"
	failureInvalidToolIdentity     failureCode = "invalid_tool_identity"
	failureInvalidToolIndex        failureCode = "invalid_tool_index"
	failureInvalidToolOrder        failureCode = "invalid_tool_order"
	failureInvalidToolResult       failureCode = "invalid_tool_result"
	failureInvalidToolSchema       failureCode = "invalid_tool_schema"
	failureInvalidToolType         failureCode = "invalid_tool_type"
	failureMissingReplay           failureCode = "missing_replay"
	failureModelChangedInStream    failureCode = "model_changed_in_stream"
	failureOpaqueReplayTooLarge    failureCode = "opaque_replay_too_large"
	failureResponseTooLarge        failureCode = "response_too_large"
	failureUnexpectedChoice        failureCode = "unexpected_choice"
	failureUnexpectedReplay        failureCode = "unexpected_replay"
	failureUnknownTool             failureCode = "unknown_tool"
	failureUnsupportedContent      failureCode = "unsupported_content"
	failureUnsupportedHistoryBlock failureCode = "unsupported_history_block"
	failureUnsupportedImage        failureCode = "unsupported_image"
	failureUnsupportedOutputItem   failureCode = "unsupported_output_item"
	failureUnsupportedStreamEvent  failureCode = "unsupported_stream_event"
)

// providerFailure keeps only known machine codes. Unknown text, including a
// provider's message and arbitrary code/type values, never enters an error or log.
func providerFailure(raw json.RawMessage, status int) error {
	var envelope struct {
		Error    json.RawMessage `json:"error"`
		Response struct {
			Error json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &envelope) == nil {
		if !emptyProtocolValue(envelope.Error) {
			raw = envelope.Error
		} else if !emptyProtocolValue(envelope.Response.Error) {
			raw = envelope.Response.Error
		}
	}
	var fields struct {
		Code string `json:"code"`
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &fields)
	code := ""
	var rejection *conversation.ModelRejectedError
	for _, value := range []string{fields.Code, fields.Type} {
		switch value {
		case "insufficient_quota", "credit_balance_too_low":
			code, rejection = value, conversation.ProviderCreditExhausted.Rejected(status)
		case "billing_hard_limit_reached":
			code, rejection = value, conversation.ProviderSpendLimitReached.Rejected(status)
		case "billing_error":
			code, rejection = value, conversation.ProviderBillingProblem.Rejected(status)
		case "context_length_exceeded", "invalid_prompt", "invalid_request_error", "invalid_api_key", "authentication_error", "permission_denied", "model_not_found", "unsupported_parameter", "invalid_value":
			code, rejection = value, conversation.ProviderRequestRejected.Rejected(status)
		case "rate_limit_exceeded", "rate_limit_error", "server_error", "overloaded_error":
			code = value
		}
		if rejection != nil {
			break
		}
	}
	if rejection == nil && status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests {
		rejection = conversation.ProviderRequestRejected.Rejected(status)
	}
	if rejection != nil {
		rejection.Detail = code
		if rejection.Code == conversation.ProviderRequestRejected.Rejected(status).Code {
			rejection.Hint += " Models that require readable reasoning to be sent back are unsupported; select a non-thinking mode or another model."
		}
		return rejection
	}
	return &retryableError{status: status, code: code}
}

// CredentialProbeFailure classifies bounded status metadata without retaining provider bodies.
func CredentialProbeFailure(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		rejected := conversation.InvalidProviderKey.Rejected()
		rejected.Status = status
		return rejected
	case http.StatusNotFound:
		return conversation.ProviderModelNotFound.Rejected(status)
	default:
		failure := providerFailure(nil, status)
		if _, retryable := failure.(*retryableError); retryable {
			return &CredentialProbeError{Reason: "provider_http_retryable", Status: status}
		}
		return failure
	}
}

// CredentialProbeError contains only a closed reason and bounded HTTP status.
// It never retains the request, endpoint, credential or provider response text.
type CredentialProbeError struct {
	Reason string
	Status int
}

func (failure *CredentialProbeError) Error() string { return failure.Reason }

// CredentialProbeTransportFailure classifies an I/O failure without retaining it.
func CredentialProbeTransportFailure(err error) error {
	reason := "provider_transport_failed"
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		reason = "provider_transport_timeout"
	}
	if errors.Is(err, context.Canceled) {
		reason = "provider_request_canceled"
	}
	return &CredentialProbeError{Reason: reason}
}
