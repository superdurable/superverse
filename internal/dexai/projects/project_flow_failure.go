// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

import (
	"context"
	"errors"
	"slices"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrCleanupPending reports cleanup that is still converging: the phase polls
// it again until the engine ends the phase at its wait limit.
var ErrCleanupPending = errors.New("project cleanup is pending")

// Typed causes that Project provider Steps record as safe Failures. The phase
// runner and the Session cleanup owner return these, so classification never
// depends on error text.
var (
	errCleanupDeadline         = errors.New("project cleanup deadline exceeded")
	ErrCleanupOwnerMissing     = errors.New("registered project cleanup owner is missing")
	ErrCleanupReceiptMismatch  = errors.New("project child cleanup receipt does not match")
	errCleanupPermissionDenied = errors.New("project cleanup access was denied")
	errCleanupOutcomeUnknown   = errors.New("project cleanup outcome is unconfirmed")
)

// The codes of the failures a Project records.
const (
	failureCleanupTimeout      = "CLEANUP_TIMEOUT"
	failureProvider            = "PROVIDER_FAILURE"
	failureOwnershipConflict   = "OWNERSHIP_CONFLICT"
	failureCleanupOwnerMissing = "CLEANUP_OWNER_MISSING"
	failureOutcomeUnknown      = "OUTCOME_UNKNOWN"
	failurePermissionDenied    = "PERMISSION_DENIED"
	failureRepositoryHost      = "REPOSITORY_HOST_MISMATCH"
	// failureSetupStart is the failure of StartProjectSetup, its phase.
	failureSetupStart      = "SETUP_START_FAILED"
	failureSetupStartPhase = "StartProjectSetup"
	// failureSessionStart is the failure of StartProjectSession, its phase.
	failureSessionStart      = "SESSION_START_FAILED"
	failureSessionStartPhase = "StartProjectSession"
	// The failures of RegisterProjectInTeamSpace, its phase: the TeamSpace
	// refused the Project, or the registration could not be confirmed.
	failureTeamSpaceAdmission    = "TEAM_SPACE_ADMISSION_REJECTED"
	failureTeamSpaceRegistration = "TEAM_SPACE_REGISTRATION_FAILED"
	failureTeamSpacePhase        = "RegisterProjectInTeamSpace"
)

// teamSpaceAdmissionFailure is the Failure of a Project that its TeamSpace
// refused, with the refusal's message, or whose registration could not be
// confirmed, with the final error.
func teamSpaceAdmissionFailure(ctx dex.Context, rejection *dexflow.Rejection) *dexflow.Failure {
	if rejection != nil {
		return &dexflow.Failure{Code: failureTeamSpaceAdmission, Phase: failureTeamSpacePhase, Message: rejection.Message, Remedy: dexflow.RemedyContactOperator}
	}
	message := dexflow.FailureMessage("The Project's registration in its TeamSpace could not be confirmed. Create the Project again.", dexflow.RecoveryDetail(ctx))
	return &dexflow.Failure{Code: failureTeamSpaceRegistration, Phase: failureTeamSpacePhase, Message: message, Remedy: dexflow.RemedyContactOperator}
}

// cleanupTimeoutMessage is the authored message of CLEANUP_TIMEOUT.
const cleanupTimeoutMessage = "Cleanup did not converge within its wait limit. Inspect retained resources before retrying."

// executeFailure is the Failure of the command phase whose Execute failure
// moved the command to RecordProjectFailure: CLEANUP_TIMEOUT when the engine
// ended a converging cleanup phase at its wait limit, else PROVIDER_FAILURE for
// exhausted retries. Both carry the final error.
func executeFailure(ctx dex.Context, phase string) dexflow.Failure {
	detail := dexflow.RecoveryDetail(ctx)
	if slices.Contains(cleanupPhases, phase) && dexflow.RecoveryTimedOut(ctx) {
		return dexflow.Failure{Code: failureCleanupTimeout, Message: dexflow.FailureMessage(cleanupTimeoutMessage, detail), Remedy: dexflow.RemedyContactOperator}
	}
	return dexflow.Failure{Code: failureProvider, Message: dexflow.FailureMessage("The provider outcome could not be confirmed. Reconcile retained resources before retrying.", detail), Remedy: dexflow.RemedyContactOperator}
}

// unansweredCall reports whether err is a call that ended without an answer
// while the round that made it goes on: its own bounded wait passed, it was
// canceled or its server was unavailable. Every cleanup effect reads its exact
// identities before it mutates, and every child call is idempotent and
// confirmed by the child's state, so the next round settles whatever such a
// call did: it is pending, never an unknown outcome. A round that its own
// timeout cut off is left to PollUntil.
func unansweredCall(round context.Context, err error) bool {
	return round.Err() == nil && (platform.IsCallInterrupted(err) || status.Code(err) == codes.Unavailable)
}

// classifyProjectFailure records a typed cause as a Failure: an authored code
// and message that say what to reconcile, followed by the error itself. Unknown
// errors return nil and keep the SDK retry policy. OUTCOME_UNKNOWN is only for
// an outcome that cannot be confirmed, such as a Sandbox cleanup that recorded
// one. An unanswered call is never classified: a polling phase repeats it, and
// ReleaseRepositoryBinding leaves its idempotent release to the retry policy.
func classifyProjectFailure(err error) *dexflow.Failure {
	var missing *dex.FlowNotFoundError
	var code, message string
	switch {
	case errors.Is(err, infra.ErrOwnershipConflict), errors.Is(err, ErrCleanupReceiptMismatch):
		code, message = failureOwnershipConflict, "Cleanup resource ownership changed. Resolve the scope or identity conflict before retrying."
	case errors.Is(err, ErrCleanupOwnerMissing), errors.As(err, &missing):
		code, message = failureCleanupOwnerMissing, "A registered cleanup execution is missing. Reconcile its resources and ownership before retrying."
	case errors.Is(err, errCleanupDeadline):
		code, message = failureCleanupTimeout, cleanupTimeoutMessage
	case errors.Is(err, errCleanupOutcomeUnknown), errors.Is(err, repohost.ErrOutcomeUnknown):
		code, message = failureOutcomeUnknown, "A provider outcome could not be confirmed. Reconcile its original resource identity before retrying."
	case errors.Is(err, errCleanupPermissionDenied), errors.Is(err, repohost.ErrPermissionDenied), platform.IsAccessDenied(err):
		code, message = failurePermissionDenied, "The provider denied cleanup access. Restore the required authorization before retrying."
	case errors.Is(err, repohost.ErrOtherHost):
		code, message = failureRepositoryHost, "The Project's repository belongs to another repository host than this server's. Run the server with the repository host named below, then retry."
	case errors.Is(err, repohost.ErrRejected), errors.Is(err, repohost.ErrNotFound):
		code, message = failureProvider, "The repository host rejected the cleanup request. Resolve the provider rejection before retrying."
	default:
		return nil
	}
	return &dexflow.Failure{Code: code, Message: dexflow.FailureMessage(message, err.Error()), Remedy: dexflow.RemedyContactOperator}
}
