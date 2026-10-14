// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bridgeapi

import (
	"fmt"
	"net/http"
)

// ErrorBody is the JSON body of every refused request. ExpectedSequence is set
// for sequence_gap; Epoch and Generation are the Session's current values when
// the refusal concerns them.
type ErrorBody struct {
	Code             ErrorCode `json:"code"`
	Message          string    `json:"message"`
	ExpectedSequence uint64    `json:"expectedSequence,omitempty"`
	Epoch            uint64    `json:"epoch,omitempty"`
	Generation       uint64    `json:"generation,omitempty"`
}

// MaxErrorBodyBytes bounds an error body.
const MaxErrorBodyBytes = 8 << 10

// ErrorCode is why the endpoint refused a request.
type ErrorCode string

const (
	// ErrorMalformed is a body that does not decode or validate.
	ErrorMalformed ErrorCode = "malformed"
	// ErrorUnauthorized is a bad, revoked or expired token, or a login session
	// that ended: the Bridge stops and asks for reconnection from Studio.
	ErrorUnauthorized ErrorCode = "unauthorized"
	// ErrorPairingCodeInvalid is an unknown, expired or used pairing code.
	ErrorPairingCodeInvalid ErrorCode = "pairing_code_invalid"
	// ErrorBridgeOutdated is a Bridge older than the server's minimum version.
	ErrorBridgeOutdated ErrorCode = "bridge_outdated"
	// ErrorForbidden is a Builder who lost the Project's build capability.
	ErrorForbidden ErrorCode = "forbidden"
	// ErrorNotFound is an unknown path, or a connection whose Project no longer
	// exists or is being deleted: the Bridge stops, as for unauthorized.
	ErrorNotFound ErrorCode = "not_found"
	// ErrorExecutorChanged is a connection that is no longer the executor, or
	// an activation whose basis epoch is stale.
	ErrorExecutorChanged ErrorCode = "executor_changed"
	// ErrorStaleGeneration is a report of a stopped Codex turn.
	ErrorStaleGeneration ErrorCode = "stale_generation"
	// ErrorSequenceGap is a report after a missing sequence; the body names the
	// expected one.
	ErrorSequenceGap ErrorCode = "sequence_gap"
	// ErrorTooLarge is a body over its path's limit.
	ErrorTooLarge ErrorCode = "too_large"
	// ErrorRateLimited is a request over a per-replica or per-connection limit.
	ErrorRateLimited ErrorCode = "rate_limited"
	// ErrorReportsPending is a report refused because the Session holds 8
	// unconsumed reports or 2 MiB of them; the Bridge resends the same report,
	// with the same sequence, after a backoff.
	ErrorReportsPending ErrorCode = "reports_pending"
	// ErrorUnavailable is a Session the endpoint cannot reach now.
	ErrorUnavailable ErrorCode = "unavailable"
)

// Status is the HTTP status the endpoint answers the code with.
func (code ErrorCode) Status() int {
	switch code {
	case ErrorMalformed:
		return http.StatusBadRequest
	case ErrorUnauthorized, ErrorPairingCodeInvalid:
		return http.StatusUnauthorized
	case ErrorForbidden, ErrorBridgeOutdated:
		return http.StatusForbidden
	case ErrorNotFound:
		return http.StatusNotFound
	case ErrorExecutorChanged, ErrorStaleGeneration, ErrorSequenceGap:
		return http.StatusConflict
	case ErrorTooLarge:
		return http.StatusRequestEntityTooLarge
	case ErrorRateLimited, ErrorReportsPending:
		return http.StatusTooManyRequests
	case ErrorUnavailable:
		return http.StatusServiceUnavailable
	default:
		panic(fmt.Sprintf("undeclared remote agent error code %q", string(code)))
	}
}

// UnmarshalText accepts only a declared code, so Status never meets an
// unknown one.
func (code *ErrorCode) UnmarshalText(text []byte) error {
	switch candidate := ErrorCode(text); candidate {
	case ErrorMalformed, ErrorUnauthorized, ErrorPairingCodeInvalid, ErrorBridgeOutdated, ErrorForbidden, ErrorNotFound,
		ErrorExecutorChanged, ErrorStaleGeneration, ErrorSequenceGap, ErrorTooLarge, ErrorRateLimited, ErrorReportsPending, ErrorUnavailable:
		*code = candidate
		return nil
	default:
		return fmt.Errorf("unknown remote agent error code %q", text)
	}
}

// Transient reports whether the Bridge retries the request unchanged after a
// backoff; every other refusal is final for that request.
func (code ErrorCode) Transient() bool {
	return code == ErrorRateLimited || code == ErrorReportsPending || code == ErrorUnavailable
}
