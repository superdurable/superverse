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
)

// ShortRPC is the preset for an RPC that only reads or writes Attributes,
// holding locks for the given Attributes.
func ShortRPC(locks ...dex.AttributeLock) *dex.RPCOptions {
	return &dex.RPCOptions{Timeout: 10 * time.Second, LockAttributes: locks}
}

// StepRetry retries a Step handler for up to total, one second apart at first,
// doubling up to thirty seconds, without an attempt cap. The engine counts an
// attempt that cannot reach the Worker, so an attempt cap with the engine's
// one-second default backoff ends within seconds of a Worker restart or Pod
// replacement; a bound in time does not.
func StepRetry(total time.Duration) *dex.RetryPolicy {
	return &dex.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 30 * time.Second, TotalDuration: total}
}

// CappedStepRetry retries a Step handler whose attempts are expensive, such as
// a model call or a long build, at most attempts times within total. The
// retries start thirty seconds apart and double up to two minutes, so even the
// second attempt comes after a Worker restart instead of failing against it.
func CappedStepRetry(attempts int32, total time.Duration) *dex.RetryPolicy {
	return &dex.RetryPolicy{InitialInterval: 30 * time.Second, BackoffCoefficient: 2, MaximumInterval: 2 * time.Minute, MaximumAttempts: attempts, TotalDuration: total}
}

// StateStepOptions is the preset for a Step that only reads and writes
// Attributes: two minutes of retries and asynchronous durability.
func StateStepOptions() *dex.StepOptions {
	return &dex.StepOptions{
		WaitForMethodTimeout: 30 * time.Second,
		ExecuteMethodTimeout: 30 * time.Second,
		WaitForRetry:         StepRetry(2 * time.Minute),
		ExecuteRetry:         StepRetry(2 * time.Minute),
		WaitForDurability:    dex.StepDurabilityAsync,
		ExecuteDurability:    dex.StepDurabilityAsync,
	}
}

// ProviderStepOptions is the preset for a Step that makes short external
// provider calls: synchronous durability, a 45-second attempt and three
// minutes of retries. The attempt ends before the default
// one-minute heartbeat timeout, so the Step needs no heartbeat. Exhausted
// retries move to failure; nil fails the Flow.
func ProviderStepOptions(failure *dex.ExecuteFailure) *dex.StepOptions {
	o := StateStepOptions()
	o.ExecuteDurability = dex.StepDurabilitySync
	o.ExecuteMethodTimeout = 45 * time.Second
	o.ExecuteRetry = StepRetry(3 * time.Minute)
	o.ExecuteFailure = failure
	return o
}

// PollingStepOptions is the preset for a Step whose Execute waits up to
// maxWait for an external system through PollUntil. The engine owns the wait
// limits: ExecuteMethodTimeout bounds one attempt and ExecuteRetry.TotalDuration,
// measured from the first attempt, bounds every attempt together; both are maxWait, and Step code never repeats them.
// The one-minute heartbeat timeout detects a stalled Worker, and a few retries
// with backoff cover Worker loss and transient errors within maxWait. An
// expired limit or exhausted retries move to failure, whose Step records the
// business failure from ctx.RecoveryError(); nil fails the Flow. The Step must
// not lock Execute Attributes, which would block their RPCs for the whole
// wait; a short following Step commits a result that needs a lock.
func PollingStepOptions(maxWait time.Duration, failure *dex.ExecuteFailure) *dex.StepOptions {
	o := StateStepOptions()
	o.ExecuteDurability = dex.StepDurabilitySync
	o.ExecuteMethodTimeout = maxWait
	o.HeartbeatTimeout = pollingHeartbeatTimeout
	o.ExecuteRetry = &dex.RetryPolicy{
		InitialInterval: 5 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute,
		MaximumAttempts: 5, TotalDuration: maxWait,
	}
	o.ExecuteFailure = failure
	return o
}
