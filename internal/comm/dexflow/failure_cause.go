// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflow

import (
	"strings"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
)

// maximumCauseBytes bounds the cause a Failure or failure record carries.
const maximumCauseBytes = 2 << 10

// TruncateCause bounds cause to 2 KiB of valid UTF-8; a cut cause ends with
// "...". A cause is error text, which provider clients build without
// credentials: those travel only in request headers.
func TruncateCause(cause string) string {
	cause = strings.ToValidUTF8(cause, "\uFFFD")
	if len(cause) <= maximumCauseBytes {
		return cause
	}
	return comm.TruncateUTF8(cause, maximumCauseBytes-len("...")) + "..."
}

// RecoveryDetail is the final error that moved a Step to its failure-record
// Step, with engine diagnostics replaced by business text and other causes
// bounded by TruncateCause; empty when a decision reached the Step.
func RecoveryDetail(ctx dex.Context) string {
	if failure := ctx.RecoveryError(); failure != nil {
		if RecoveryTimedOut(ctx) {
			return "The operation did not finish within its time limit."
		}
		if engineFailureDetail(failure.Detail) {
			return "The operation could not be completed. Please try again."
		}
		return TruncateCause(failure.Detail)
	}
	return ""
}

// RecoveryErrorType is the type of the final error that moved a Step to its
// failure-record Step, ctx.RecoveryError().ErrorType: the Worker error's Go
// type, or the engine's failure type such as StartToClose; empty when a
// decision reached the Step.
func RecoveryErrorType(ctx dex.Context) string {
	if failure := ctx.RecoveryError(); failure != nil {
		return failure.ErrorType
	}
	return ""
}

// executeTimeoutTypes are the RecoveryError types of an Execute that reached a
// wait limit of its StepOptions, as the Dex Server reports them:
// "StartToClose" for ExecuteMethodTimeout and "ScheduleToClose" for
// ExecuteRetry.TotalDuration. A heartbeat timeout is a stalled Worker, not a
// wait limit.
var executeTimeoutTypes = map[string]bool{"StartToClose": true, "ScheduleToClose": true}

// RecoveryTimedOut reports whether the engine moved the Step to its
// failure-record Step because the failed Execute reached ExecuteMethodTimeout
// or ExecuteRetry.TotalDuration. It is false for a decision, exhausted retries
// of an error and a stalled Worker.
func RecoveryTimedOut(ctx dex.Context) bool {
	failure := ctx.RecoveryError()
	return failure != nil && executeTimeoutTypes[failure.ErrorType]
}

// RecoveryStep is the registered type of the Step whose Execute failure moved
// the Step here, from its execution ID "<Step>-<n>", or empty when a decision
// reached the Step.
func RecoveryStep(ctx dex.Context) string {
	if ctx.RecoveryError() == nil {
		return ""
	}
	step, _, _ := strings.Cut(ctx.FromStepExecutionID(), "-")
	return step
}

// FailureMessage appends a bounded business cause to the authored message,
// omitting engine diagnostics that belong only in execution history.
func FailureMessage(message, cause string) string {
	if cause == "" || engineFailureDetail(cause) {
		return message
	}
	return message + " Cause: " + TruncateCause(cause)
}

// Engine diagnostics belong in execution history, never in business failures.
// Match case-insensitively because SDK and server wrappers differ in casing.
func engineFailureDetail(detail string) bool {
	detail = strings.ToLower(detail)
	for _, marker := range []string{"temporal", "scheduletoclose", "starttoclose", "schedule-to-close", "start-to-close", "activity schedule", "activity error", "activity timeout", "workflow execution", "heartbeat timeout", "not enough time to schedule next retry"} {
		if strings.Contains(detail, marker) {
			return true
		}
	}
	return false
}
