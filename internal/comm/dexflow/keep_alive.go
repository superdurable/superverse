// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflow

import (
	"sync"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// keepAliveInterval keeps an attempt whose call reports nothing for a while
// well inside the one-minute heartbeat timeout.
const keepAliveInterval = 20 * time.Second

// KeepAlive records value as the Step heartbeat every 20 seconds until the
// returned stop is called, usually through defer: a long call bounded by its
// own timeout then outlasts the one-minute heartbeat timeout of its attempt,
// while a stalled Worker still stops heartbeating. stop ends and joins the
// owned goroutine, so no heartbeat follows the handler's return.
//
// The goroutine stops at the first failed heartbeat. A transport failure has
// already cancelled the attempt's context in the SDK, which ends the call, and
// value is a plain struct or nil that always encodes. The Step output emitter
// serializes these heartbeats with the handler's own frames.
func KeepAlive(ctx dex.Context, value any) (stop func()) {
	stopped := make(chan struct{})
	var heartbeats sync.WaitGroup
	heartbeats.Go(func() {
		ticker := time.NewTicker(keepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.RecordHeartbeat(value) != nil {
					return
				}
			}
		}
	})
	return func() {
		close(stopped)
		heartbeats.Wait()
	}
}
