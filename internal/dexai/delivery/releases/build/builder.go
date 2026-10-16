// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package build

import (
	"context"
	"errors"
)

// Builder is the external build boundary shared by the Local Kind fixture and
// CodeBuild. An unknown provider outcome never grants permission to repeat a
// build.
type Builder interface {
	Inspect(context.Context, BuildRequest) (BuildResult, error)
	// Start dispatches the build once, cloning the source with the
	// repository-scoped token.
	Start(ctx context.Context, request BuildRequest, repositoryToken string) (BuildResult, error)
	// Cancel stops the build and returns ErrPending until its termination is
	// observed, or ErrCancelUnconfirmed when the provider can never confirm it.
	Cancel(context.Context, BuildRequest) error
	// CleanupRelease fences and reclaims every source and build namespace of
	// one Release, returning ErrPending while work remains. Durable provider
	// receipts outlive cleanup to reject replay.
	CleanupRelease(context.Context, BuildRequest) error
	// ReadLog reads one page of the build's log as read asks. It is read on
	// demand and never stored. The log is build output, so the caller redacts
	// it before anyone sees it. A malformed cursor is ErrInvalidLogCursor and
	// a read the provider's rate limit refused ErrLogThrottled.
	ReadLog(ctx context.Context, request BuildRequest, read LogRead) (LogPage, error)
}

// LogRead is one read of a build's log: the page after Cursor, the Next of an
// earlier page ("" is the start of the log), or with Tail its last bytes, at
// most MaximumBytes either way.
type LogRead struct {
	Cursor       string
	Tail         bool
	MaximumBytes int
}

// LogPage is one page of a build's log: whole lines of build output, at most
// the read's MaximumBytes, of which only a single longer line is cut; the
// cursor after them; and Complete once the provider build no longer runs and
// its log holds nothing after Next. A build the provider never ran has an
// empty page that is complete.
type LogPage struct {
	Text     string
	Next     string
	Complete bool
}

// ErrPending means the provider accepted the work but has not finished it: a
// cancelled build that has not yet terminated, or a bounded cleanup with work
// left. The caller still owns the work and calls again.
var ErrPending = errors.New("release build provider work is still in progress")

// ErrCancelUnconfirmed means the provider cannot confirm that a cancelled
// build ended, which waiting does not change: the local fixture lost the job
// that ran it or could not stop its exact child processes, or CodeBuild no
// longer finds the execution its admission names.
var ErrCancelUnconfirmed = errors.New("the build provider cannot confirm that the cancelled build stopped")

// ErrInvalidLogCursor refuses a log cursor that no page of this provider
// returned.
var ErrInvalidLogCursor = errors.New("invalid build log cursor")

// ErrLogThrottled means the provider refused a log read for its rate limit;
// the caller reads again later. The log is best effort, so it is not a failure.
var ErrLogThrottled = errors.New("the build provider throttled the log read")
