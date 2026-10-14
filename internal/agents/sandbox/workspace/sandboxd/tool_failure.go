// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxd

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/superdurable/superversev2/internal/agents/toolfailure"
)

// ToolFailure is a tool failure whose code and effect are known and safe to
// return to the model; the code names its owner. Transport loss, cancellation
// and ambiguous side effects stay ordinary errors that the caller classifies
// with IsTransient.
//
//nolint:errname // it names the failure the model reads, which is also an error
type ToolFailure struct {
	code   toolfailure.Code
	effect toolfailure.Effect
	cause  error
	detail string
}

// NewToolFailure is a failure with the effect none; WithEffect names another.
func NewToolFailure(code toolfailure.Code, cause error) *ToolFailure {
	return &ToolFailure{code: code, effect: toolfailure.EffectNone, cause: cause}
}

// WithEffect returns the failure with another effect.
func (failure *ToolFailure) WithEffect(effect toolfailure.Effect) *ToolFailure {
	changed := *failure
	changed.effect = effect
	return &changed
}

// WithDetail returns the failure with supplementary, secret-free text for the
// model, such as the end of a command's output.
func (failure *ToolFailure) WithDetail(detail string) *ToolFailure {
	changed := *failure
	changed.detail = detail
	return &changed
}

// Rewrap returns the failure with another cause and the same code, effect and
// detail.
func (failure *ToolFailure) Rewrap(cause error) *ToolFailure {
	changed := *failure
	changed.cause = cause
	return &changed
}

func (failure *ToolFailure) Error() string              { return failure.cause.Error() }
func (failure *ToolFailure) Unwrap() error              { return failure.cause }
func (failure *ToolFailure) Code() toolfailure.Code     { return failure.code }
func (failure *ToolFailure) Effect() toolfailure.Effect { return failure.effect }
func (failure *ToolFailure) Detail() string             { return failure.detail }

// transientError marks an error that its source knows a repeat can outlive.
type transientError struct{ cause error }

func (marked *transientError) Error() string { return marked.cause.Error() }
func (marked *transientError) Unwrap() error { return marked.cause }

// MarkTransient returns err as a fault that IsTransient accepts. A caller marks
// the failure of an operation whose cause it cannot tell apart by type, such as
// a remote Git command that exited nonzero after the network failed; a typed
// failure that wraps the marked error is still a typed failure.
func MarkTransient(err error) error {
	return &transientError{cause: err}
}

// IsTransient reports whether err is a transport or capacity fault that a
// repeat of a repeat-safe call can outlive. It looks at error types only, never
// at message text.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	var marked *transientError
	if errors.As(err, &marked) {
		return true
	}
	// A platform command that outlived its timeout is as transient as a deadline.
	// A model command's timeout never gets here: the tool turns it into the call
	// failure command_timed_out.
	for _, transient := range []error{errStreamWithoutExit, errProcessTimeout, io.ErrUnexpectedEOF, context.DeadlineExceeded, syscall.ECONNRESET, syscall.ECONNREFUSED} {
		if errors.Is(err, transient) {
			return true
		}
	}
	var network net.Error
	if errors.As(err, &network) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted, codes.Aborted, codes.DeadlineExceeded:
		return true
	default:
	}
	var rejected *filesystemStatusError
	if errors.As(err, &rejected) {
		switch rejected.statusCode {
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
		}
	}
	return false
}
