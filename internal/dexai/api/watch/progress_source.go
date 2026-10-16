// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package watch

import (
	"context"
	"errors"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
)

// DexStreamSource reads one Flow's Dex Stream as a Watch progress source
// whose identity is <FlowID>/<Stream name>.
func DexStreamSource[T any](client *dex.Client, flowID string, stream dex.Stream[T]) Source[T] {
	return Source[T]{
		Identity: flowID + "/" + stream.StreamName(),
		Tail: func(ctx context.Context) (string, error) {
			var page dex.StreamMessagesPage[T]
			if err := client.ListStreamMessages(ctx, flowID, stream, 1, "", &page); err != nil {
				return "", err
			}
			if len(page.Messages) == 0 {
				return "", nil
			}
			return page.Messages[0].ResumeToken, nil
		},
		Read: func(ctx context.Context, cursor string) (T, string, error) {
			return ReadUntilMessage(ctx, func(ctx context.Context) (T, string, error) {
				var value T
				message, err := client.ReadStream(ctx, flowID, stream, cursor, &value)
				return value, message.ResumeToken, err
			})
		},
	}
}

// ReadUntilMessage repeats one long poll until it yields a message. An idle
// poll ends with *dex.LongPollTimeoutError at the Dex server's cap, or with
// comm.ErrLongPollWindowEnded at a per-poll window; neither ends the Watch
// while ctx is live.
func ReadUntilMessage[T any](ctx context.Context, poll func(context.Context) (T, string, error)) (T, string, error) {
	for {
		value, cursor, err := poll(ctx)
		if ctx.Err() != nil {
			var zero T
			return zero, "", ctx.Err()
		}
		var idle *dex.LongPollTimeoutError
		if err == nil || !errors.As(err, &idle) && !errors.Is(err, comm.ErrLongPollWindowEnded) {
			return value, cursor, err
		}
	}
}
