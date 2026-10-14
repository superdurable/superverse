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
	"errors"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// InvokeLocked invokes an RPC that locks Attributes and retries explicit lock
// contention, with backoff, within timeout. When the lock stays held for the
// whole timeout it returns that *dex.RPCLockConflictError, which the caller
// may retry later; only the end of ctx itself returns ctx's error. Every other
// outcome, including a business rejection carried in the output, is returned
// unchanged.
func InvokeLocked[T any](ctx context.Context, sdk *dex.Client, timeout time.Duration, flowID string, rpc, input any) (T, error) {
	return InvokeLockedWith[T](ctx, sdk, timeout, flowID, rpc, input, dex.RPCInvokeOptions{})
}

// InvokeLockedWith is InvokeLocked with the AttributeMap instances the
// invocation locks and loads, such as the one record a read-modify-write RPC
// changes.
func InvokeLockedWith[T any](ctx context.Context, sdk *dex.Client, timeout time.Duration, flowID string, rpc, input any, options dex.RPCInvokeOptions) (T, error) {
	const initialRetryDelay, maximumRetryDelay = 5 * time.Millisecond, 100 * time.Millisecond
	budget, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for retryDelay := initialRetryDelay; ; retryDelay = min(retryDelay*2, maximumRetryDelay) {
		var output T
		err := sdk.InvokeRPCWithOptions(budget, flowID, rpc, input, &output, options)
		var conflict *dex.RPCLockConflictError
		if !errors.As(err, &conflict) {
			return output, err
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-budget.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return output, ctx.Err()
			}
			return output, err
		case <-timer.C:
		}
	}
}
