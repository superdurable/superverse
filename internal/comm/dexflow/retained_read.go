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

	"github.com/superdurable/dex/sdk-go/dex"
)

// InvokeRetainedRead invokes a query-only RPC and reports a missing execution
// as *dex.FlowNotFoundError.
//
// An RPC without locks, transactionality, durable effects or a Server policy
// that runs every RPC transactionally also reads a retained, completed execution. For such an RPC the
// SDK's *dex.FlowNotActiveOrNotFoundError can only mean that no execution
// exists. Mutations and active-only RPCs must not use this helper: for them
// that error does not distinguish a missing Flow from a closed one.
func InvokeRetainedRead(ctx context.Context, client *dex.Client, flowID string, rpc, input, outputPtr any, loads ...dex.AttributeMapLoad) error {
	err := client.InvokeRPCWithOptions(ctx, flowID, rpc, input, outputPtr, dex.RPCInvokeOptions{LoadAttributeMapInstances: loads})
	var missing *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &missing) {
		return &dex.FlowNotFoundError{ServiceError: missing.ServiceError}
	}
	return err
}
