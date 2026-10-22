// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package setup_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// TestPlatformPullLongStepsLeaveRPCCapacityIntegration holds twelve actual
// Setup Execute handlers on stopped MinIO services. The lane's single merged
// Worker runs every handler with real dependencies; no test Worker is started.
// A different Flow type must still answer its normal ten-second read RPC.
func TestPlatformPullLongStepsLeaveRPCCapacityIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	const count = 12
	held := make([]*storageScenario, 0, count)
	for index := range count {
		s := startStorageScenario(ctx, t, fmt.Sprintf("Pull capacity %d", index))
		s.awaitHeldWait(ctx, t)
		held = append(held, s)
	}
	// Each held service was created by ReconcileProjectStorage itself, proving
	// Execute reached the real provider rather than merely being engine-active.
	// No storage is released until the unrelated RPC and second state check.
	requireHeld := func() {
		t.Helper()
		for _, s := range held {
			active, failed := s.storageWait(ctx)
			if !active || failed {
				t.Fatalf("Setup %s no longer holds its original Execute attempt (active=%t failed=%t)", s.setupFlowID, active, failed)
			}
		}
	}
	requireHeld()
	settings := held[0].settings
	flow := &teamspaces.TeamSpaceFlow{}
	sdk := dexflowtest.ClientForTest(t, settings.dexAddress, &dex.WorkerTarget{Pull: true}, flow)
	project, err := held[0].projects.Read(ctx, held[0].projectID)
	if err != nil {
		t.Fatalf("read held Project: %s", dexflowtest.ErrorCategory(err))
	}
	// TeamSpace is independent of every held Setup. One attempt, no retries
	// and no enlarged timeout: the declaration uses dexflow.ShortRPC (10s).
	read, done := context.WithTimeout(ctx, 10*time.Second)
	started := time.Now()
	var display map[string]any
	err = sdk.InvokeRPC(read, flowid.TeamSpaceFlowID(project.TeamSpaceID), flow.GetDexDisplay, nil, &display)
	elapsed := time.Since(started)
	done()
	if err != nil || elapsed >= 10*time.Second || display["TeamSpaceStatus"] != teamspaces.StatusReady {
		t.Fatalf("unrelated TeamSpace RPC with %d held Steps: elapsed=%s error=%s", count, elapsed, dexflowtest.ErrorCategory(err))
	}
	requireHeld()
	t.Logf("%d concurrent real ReconcileProjectStorage handlers held MinIO; unrelated TeamSpace RPC answered in %s within its normal 10s bound", count, elapsed)
}
