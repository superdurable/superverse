// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflow

import (
	"context"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// pollingHeartbeatTimeout is the HeartbeatTimeout of PollingStepOptions.
	pollingHeartbeatTimeout = time.Minute
	// heartbeatMargin keeps one polling round safely inside the heartbeat
	// timeout despite scheduling and transport delays.
	heartbeatMargin = 10 * time.Second
)

// PollUntil waits inside one Execute attempt for an external system: each
// round calls check under its own roundTimeout, heartbeats and sleeps every,
// until check reports done or fails. The Step must use PollingStepOptions, and
// every plus roundTimeout must stay heartbeatMargin inside its one-minute
// heartbeat timeout. A round of one API call takes ten seconds; a round of
// several calls takes up to 40 seconds with a short interval.
//
// check is an idempotent query; "not ready yet" is done=false, never an error.
// A check error is returned with a stack, so the Step's retry policy runs a new
// attempt, which restarts the loop. A round that roundTimeout cuts off while
// the attempt goes on is pending like "not ready yet": the provider was slow,
// and the next round settles what it left, so check returns such a round's
// error unclassified.
//
// PollUntil has no deadline of its own: the engine ends the wait at the Step's
// ExecuteMethodTimeout or ExecuteRetry.TotalDuration and moves the Step to its
// Execute failure route. A business condition that is not a fixed duration,
// such as a lease the Flow does not own, is a check that reports done.
//
// The heartbeat clears heartbeat details, so a polling Step keeps no
// checkpoint: a new attempt simply queries again. A Step that reports progress
// writes its Stream frame inside check, a closure defined in the Step method of
// the Flow file, and only when the observed phase changes, because the Stream
// budget is shared by every execution of the Flow type.
func PollUntil(ctx dex.Context, every, roundTimeout time.Duration, check func(context.Context) (done bool, err error)) error {
	if every+roundTimeout > pollingHeartbeatTimeout-heartbeatMargin {
		panic(fmt.Sprintf("dexflow: polling every %s with a %s round timeout misses the %s heartbeat timeout", every, roundTimeout, pollingHeartbeatTimeout))
	}
	for {
		round, cancel := context.WithTimeout(ctx, roundTimeout)
		done, err := check(round)
		cutOff := err != nil && round.Err() != nil && ctx.Err() == nil
		cancel()
		switch {
		case cutOff:
			// Pending: the next round asks again.
		case err != nil:
			return dex.ErrorWithStack(err)
		case done:
			return nil
		}
		if err := ctx.RecordHeartbeat(nil); err != nil {
			return dex.ErrorWithStack(fmt.Errorf("record polling heartbeat: %w", err))
		}
		time.Sleep(every)
	}
}
