// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/projects"
)

// TestMissingReleaseReadDexIntegration resolves the read of an absent Release
// through the actual Dex Server and creates no execution.
func TestMissingReleaseReadDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectFlow, releaseFlow := &projects.ProjectFlow{}, &releases.ProjectReleaseFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, projectFlow, releaseFlow)
	releaseClient := releases.NewProjectReleaseFlowClient(releaseFlow, &projects.ProjectFlowClient{Dex: client, Flow: projectFlow, ProjectIDLength: 4, PlatformAdminEmail: "admin@superverse.test"}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	projectID, releaseID := flowid.RandomID(4), strings.Repeat("0", 40)
	flowID, err := releases.FlowID(projectID, releaseID)
	if err != nil {
		t.Fatal(err)
	}
	dexflowtest.RequireAbsentForTest(ctx, t, client, flowID)
	var missing *dex.FlowNotFoundError
	if _, err = releaseClient.ReadSnapshot(ctx, projectID, releaseID); !errors.As(err, &missing) {
		t.Fatalf("missing read classification was %s", dexflowtest.ErrorCategory(err))
	}
	dexflowtest.RequireAbsentForTest(ctx, t, client, flowID)
	t.Logf("FlowID=%s; missing read created no execution", flowID)
}

// TestRetainedReleaseReadsDexIntegration reads an actual, already removed
// Release through its registered read RPC and its Flow client. It starts no
// Worker or Flow and invokes no mutation; the serving Worker must match the
// current read contracts. An operator supplies the retained identity from a
// completed journey.
func TestRetainedReleaseReadsDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_RELEASE_PROJECT_ID")
	releaseID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_RELEASE_ID")
	projectFlow, releaseFlow := &projects.ProjectFlow{}, &releases.ProjectReleaseFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, projectFlow, releaseFlow)
	releaseClient := releases.NewProjectReleaseFlowClient(releaseFlow, &projects.ProjectFlowClient{Dex: client, Flow: projectFlow, ProjectIDLength: len(projectID), PlatformAdminEmail: "admin@superverse.test"}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	flowID, err := releases.FlowID(projectID, releaseID)
	if err != nil {
		t.Fatal("invalid exact Release identity")
	}
	var snapshot releases.Snapshot
	dexflowtest.ReadRetainedForTest(ctx, t, client, flowID, releaseFlow.GetProjectRelease, &snapshot)
	if snapshot.ProjectID != projectID || snapshot.ReleaseID != releaseID || snapshot.Status != releases.Removed {
		t.Fatal("terminal Release identity/status mismatch")
	}
	t.Logf("FlowID=%s business=REMOVED revision=%d", flowID, snapshot.Revision)
	retained, err := releaseClient.ReadSnapshot(ctx, projectID, releaseID)
	if err != nil {
		t.Fatalf("retained Release FlowClient read failed: %s", dexflowtest.ErrorCategory(err))
	}
	if retained.ProjectID != snapshot.ProjectID || retained.ReleaseID != snapshot.ReleaseID || retained.Status != snapshot.Status || retained.Revision != snapshot.Revision {
		t.Fatal("retained Release FlowClient changed authoritative snapshot")
	}
	dexflowtest.RequireCompletedForTest(ctx, t, client, flowID)
}
