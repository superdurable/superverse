// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package backgroundcommand

import (
	"context"
	"errors"

	"github.com/superdurable/dex/sdk-go/dex"
)

// BackgroundCommandFlowClient fronts BackgroundCommandFlow, which only the
// Session starts, as a SubFlow: the one call is the stop of a watch whose
// Session closes.
type BackgroundCommandFlowClient struct {
	sdk *dex.Client
}

// Stop cancels the running BackgroundCommandFlow flowID, the SubFlow ID Dex
// gave the watch. One that no longer runs, or never started, is stopped
// already; the call is idempotent.
func (client *BackgroundCommandFlowClient) Stop(ctx context.Context, flowID string) error {
	err := client.sdk.StopFlow(ctx, flowID, dex.StopOptions{Type: dex.CancelFlow, Reason: "the Session of the background command closed"})
	if _, isNotActive := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); isNotActive {
		return nil
	}
	return err
}
