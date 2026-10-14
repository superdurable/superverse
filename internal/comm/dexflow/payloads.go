// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflow

import (
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
)

// The payloads below are persisted in Attributes, RPC results and Streams of
// several Flows. Their field names and order are their JSON encoding, so they
// carry no tags and must not be renamed or reordered.

// StartIdentity records the request that started a Flow execution.
type StartIdentity struct {
	RequestID      string
	Fingerprint    string
	ActorAccountID string
}

// Rejection is a business refusal returned inside an RPC result. Code is the
// persisted spelling of a RejectionCode; Message is safe to show to the caller.
// Stale marks a revision fence the caller observed before the resource
// changed, so the caller rereads instead of reporting a rule. A Flow client
// returns it as its error, which callers inspect with errors.As.
//
//nolint:errname // the RPC result field and the error are one type, named for the payload
type Rejection struct {
	Code    string
	Message string
	Stale   bool
}

// RejectionCode is why a Flow refused a request: the name of a gRPC status code
// below.
type RejectionCode struct{ name string }

var (
	RejectionAborted            = RejectionCode{name: "ABORTED"}
	RejectionAlreadyExists      = RejectionCode{name: "ALREADY_EXISTS"}
	RejectionFailedPrecondition = RejectionCode{name: "FAILED_PRECONDITION"}
	RejectionInvalidArgument    = RejectionCode{name: "INVALID_ARGUMENT"}
	RejectionNotFound           = RejectionCode{name: "NOT_FOUND"}
	RejectionPermissionDenied   = RejectionCode{name: "PERMISSION_DENIED"}
	RejectionResourceExhausted  = RejectionCode{name: "RESOURCE_EXHAUSTED"}
	RejectionUnauthenticated    = RejectionCode{name: "UNAUTHENTICATED"}
)

// String is the persisted spelling of the code.
func (code RejectionCode) String() string { return code.name }

// Reject is the refusal of code with a message that is safe to show.
func Reject(code RejectionCode, message string) *Rejection {
	return &Rejection{Code: code.String(), Message: message}
}

// RejectStale is the ABORTED refusal of a revision fence that no longer holds:
// the resource changed after the caller read the revision it sent.
func RejectStale(message string) *Rejection {
	return &Rejection{Code: RejectionAborted.String(), Message: message, Stale: true}
}

// HasCode reports whether the refusal is of code.
func (r *Rejection) HasCode(code RejectionCode) bool { return r.Code == code.String() }

func (r *Rejection) Error() string { return r.Code + ": " + r.Message }

// Err returns r as an error. A nil r yields a nil error, which returning a nil
// *Rejection through an error result would not.
func (r *Rejection) Err() error {
	if r == nil {
		return nil
	}
	return r
}

// Failure is the durable, user-visible record of a failed lifecycle phase.
// Remedy is what the reader can do about it, declared with the code.
type Failure struct {
	Code, Phase, Message string
	Recoverable          bool
	Remedy               FailureRemedy
}

// FailureRemedy is what the reader of a failure can do about it, declared
// with each failure code where its message is. It is a struct that string
// literals cannot convert to: the values below are its only ones, persisted
// by their names, and the zero FailureRemedy is none, which Studio reads as
// a platform problem.
type FailureRemedy struct{ name string }

var (
	// RemedyOpenSetup asks to complete or fix Setup items.
	RemedyOpenSetup = FailureRemedy{"open_setup"}
	// RemedyApplyAgain asks to start the Preview again or Apply.
	RemedyApplyAgain = FailureRemedy{"apply_again"}
	// RemedyAskAgent asks the Agent to change the application's source.
	RemedyAskAgent = FailureRemedy{"ask_agent"}
	// RemedyReauthorize asks to authorize a connection again.
	RemedyReauthorize = FailureRemedy{"reauthorize"}
	// RemedyFreeWorkspace asks to free space on the workspace volume.
	RemedyFreeWorkspace = FailureRemedy{"free_workspace"}
	// RemedyPublishAgain asks to retry the build or deployment.
	RemedyPublishAgain = FailureRemedy{"publish_again"}
	// RemedyContactOperator names a platform defect or a terminal state.
	RemedyContactOperator = FailureRemedy{"contact_operator"}
	// RemedyCancelDeployment asks to cancel the deployment a build started
	// instead of the build.
	RemedyCancelDeployment = FailureRemedy{"cancel_deployment"}
)

func (remedy FailureRemedy) String() string               { return remedy.name }
func (remedy FailureRemedy) MarshalText() ([]byte, error) { return []byte(remedy.name), nil }
func (remedy *FailureRemedy) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, remedy, FailureRemedy.String, RemedyOpenSetup, RemedyApplyAgain, RemedyAskAgent,
		RemedyReauthorize, RemedyFreeWorkspace, RemedyPublishAgain, RemedyContactOperator, RemedyCancelDeployment)
}

// PhaseTiming is one phase of a lifecycle that a Flow measures durably: when
// it started and how long it took. A zero Duration marks the phase in
// progress. Phase names are code constants of the owning Flow.
type PhaseTiming struct {
	Phase     string
	StartedAt time.Time
	Duration  time.Duration
}

// BeginPhase ends the phase in progress at now and starts phase, unless phase
// is already in progress, so a repeated Step attempt measures it once.
func BeginPhase(timings []PhaseTiming, phase string, now time.Time) []PhaseTiming {
	if last := len(timings) - 1; last >= 0 && timings[last].Phase == phase && timings[last].Duration == 0 {
		return timings
	}
	return append(EndPhase(timings, now), PhaseTiming{Phase: phase, StartedAt: now})
}

// EndPhase ends the phase in progress, if any, at now. An ended phase lasts at
// least a millisecond, so it never reads as in progress, even when the Workers
// that started and ended it disagree about the time.
func EndPhase(timings []PhaseTiming, now time.Time) []PhaseTiming {
	if last := len(timings) - 1; last >= 0 && timings[last].Duration == 0 {
		timings[last].Duration = max(now.Sub(timings[last].StartedAt), time.Millisecond)
	}
	return timings
}

// ProgressEvent is one best-effort progress Stream entry. Readers recover from
// a lost entry through the owning Flow's snapshot, never from the Stream.
// Phase is the Step name only. Detail is the free-text diagnostic of a waiting
// Step, such as a provider state or pending reason; it can name internal
// resources, so the API never maps it.
type ProgressEvent struct {
	FlowID, Phase string
	Revision      uint64
	At            time.Time
	Detail        string
}

// Progress is the progress entry of phase at a Flow revision, stamped now.
func Progress(ctx dex.Context, phase string, revision uint64) ProgressEvent {
	return ProgressEvent{FlowID: ctx.FlowID(), Phase: phase, Revision: revision, At: time.Now().UTC()}
}

// WithDetail is e carrying the diagnostic detail of its waiting Step.
func (e ProgressEvent) WithDetail(detail string) ProgressEvent {
	e.Detail = detail
	return e
}

// StreamWritten takes the result of a write to a best-effort Stream. A write
// fails in operation only once the handler's context ended: after a Stop or
// another cancellation of the Step, at its timeout, or when its connection to
// Dex broke, which the SDK turns into the end of that context. The write then
// loses only its live entry, which readers recover from the owning Flow's
// durable state, and the handler goes on; the engine records how the attempt
// ended. With the context alive a write fails only through a bug, such as an
// unregistered Stream, a write from an RPC handler or a value that does not
// encode, which panics.
func StreamWritten(ctx dex.Context, err error) {
	if ctx.Err() == nil {
		comm.MustDo(err)
	}
}
