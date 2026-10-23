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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// TestPlatformPullLongStepsLeaveRPCCapacityIntegration holds twelve actual
// Setup Execute handlers on stopped MinIO services. The lane's single merged
// Worker runs every handler with real dependencies; no test Worker is started.
// A different Flow type must still answer its normal ten-second read RPC.
// SUPERVERSE_TEST_PULL_CAPACITY_CONFIG names the running server profile for
// a small-M saturation run; its M-R Steps must fit the twelve-Project budget.
func TestPlatformPullLongStepsLeaveRPCCapacityIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	count := 12
	if profile := os.Getenv("SUPERVERSE_TEST_PULL_CAPACITY_CONFIG"); profile != "" {
		settings, err := serverconfig.Load(strings.Split(profile, ",")...)
		if err != nil {
			t.Fatalf("load capacity profile: %v", err)
		}
		pull := settings.Dex.Pull
		count = pull.MaxConcurrentTasks - pull.ReservedRPCTasks
		if settings.Dex.ServerAddress != requireSetupSettings(t).dexAddress || pull.ReservedRPCTasks < 1 || count < 1 || count > 12 {
			t.Fatal("capacity profile must name this lane Dex, reserve RPC capacity, and allow 1..12 concurrent Steps")
		}
		t.Logf("saturating running profile %s: M=%d R=%d, holding all %d Step slots", profile, pull.MaxConcurrentTasks, pull.ReservedRPCTasks, count)
	}
	var extra *storageScenario
	var extraInput projects.ProjectStart
	if os.Getenv("SUPERVERSE_TEST_PULL_CAPACITY_CONFIG") != "" {
		extra, extraInput = prepareStorageScenario(ctx, t, "Reserved capacity overflow")
	}
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
	if extra != nil {
		if err := extra.projects.Create(ctx, extraInput); err != nil {
			t.Fatalf("start overflow Project: %v", err)
		}
		project, err := extra.projects.Read(ctx, extra.projectID)
		if err != nil {
			t.Fatalf("read overflow Project initial state: %v", err)
		}
		// Start the same admitted Setup identity the Project will also start.
		// Its owner still gates all effects; both starts join one execution.
		if err := extra.setups.StartForProject(ctx, project); err != nil {
			t.Fatalf("start overflow Setup: %v", err)
		}
		t.Logf("overflow Setup %s accepted at %s while all Step slots are held", extra.setupFlowID, time.Now().UTC().Format(time.RFC3339Nano))
		// Observe a bounded no-progress window shorter than the dispatch wait.
		// The SDK counter belongs to the separate lane server process.
		until := time.Now().Add(3 * time.Second)
		for time.Now().Before(until) {
			snapshot, err := extra.setups.Read(ctx, extra.setupFlowID)
			if err != nil || snapshot.Phase != "AdmitSetup" || snapshot.Revision != 1 {
				t.Fatalf("overflow Setup ran despite the Step cap: snapshot=%+v error=%v", snapshot, err)
			}
			requireHeld()
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
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
	if extra != nil {
		releasedAt := time.Now().UTC()
		if err := held[0].releaseStorage(ctx); err != nil {
			t.Fatalf("release one Step slot: %v", err)
		}
		extra.awaitHeldWait(ctx, t)
		// Diagnostic evidence only: the first handler stamps this frame before
		// admission. Business completion is still proved by the typed snapshot.
		var progress dex.StreamMessagesPage[dexflow.ProgressEvent]
		if err := extra.setups.Dex.ListStreamMessages(ctx, extra.setupFlowID, setup.SetupProgress, 100, "", &progress); err != nil {
			t.Fatalf("read overflow handler-start evidence: %v", err)
		}
		var admittedAt time.Time
		for _, message := range progress.Messages {
			if message.Value.Phase == "AdmitSetup" && (admittedAt.IsZero() || message.Value.At.Before(admittedAt)) {
				admittedAt = message.Value.At
			}
		}
		if admittedAt.IsZero() || admittedAt.Before(releasedAt) {
			t.Fatalf("overflow handler start must follow capacity release: start=%s release=%s", admittedAt, releasedAt)
		}
		t.Logf("overflow first handler timestamp=%s, capacity released=%s", admittedAt.Format(time.RFC3339Nano), releasedAt.Format(time.RFC3339Nano))
		t.Logf("overflow Setup %s entered its real storage handler at %s after one slot was released", extra.setupFlowID, time.Now().UTC().Format(time.RFC3339Nano))
		if err := extra.releaseStorage(ctx); err != nil {
			t.Fatalf("release overflow storage: %v", err)
		}
		await(ctx, t, "overflow Setup completion", func() bool {
			snapshot, err := extra.setups.Read(ctx, extra.setupFlowID)
			if err != nil || snapshot.Status == setup.StatusSetupFailed || snapshot.Status == setup.StatusCancelled {
				t.Fatalf("overflow Setup failed after capacity returned: status=%s error=%v", snapshot.Status, err)
			}
			return snapshot.Status == setup.StatusComplete
		})
	}
	t.Logf("%d concurrent real ReconcileProjectStorage handlers held MinIO; unrelated TeamSpace RPC answered in %s within its normal 10s bound", count, elapsed)
}
