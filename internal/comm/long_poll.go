// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package comm

import (
	"context"
	"errors"
	"time"
)

// ErrLongPollWindowEnded is the cause of a context whose long poll window
// ended.
var ErrLongPollWindowEnded = errors.New("long poll window ended")

// WithLongPollWindow returns a context that ends after window on this
// process's clock, with cause ErrLongPollWindowEnded, and keeps parent's
// deadline. A long poll whose idle end is a normal result uses it instead of
// context.WithTimeout: gRPC sends a deadline to the server, whose own timer
// resets the call when the server's clock reaches it, and a server whose clock
// runs ahead of this process's, such as one in a container VM, does so before
// the deadline passes here, which the caller reads as Canceled.
func WithLongPollWindow(parent context.Context, window time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	timer := time.AfterFunc(window, func() { cancel(ErrLongPollWindowEnded) })
	return ctx, func() {
		timer.Stop()
		cancel(context.Canceled)
	}
}

// LongPollWindowEnded reports whether the window of a WithLongPollWindow
// context ended it.
func LongPollWindowEnded(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), ErrLongPollWindowEnded)
}
