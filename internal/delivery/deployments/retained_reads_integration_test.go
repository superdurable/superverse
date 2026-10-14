// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/projects"
)

// TestMissingDeploymentReadDexIntegration resolves the read of an absent
// Deployment through the actual Dex Server and creates no execution.
func TestMissingDeploymentReadDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectFlow, deploymentFlow := &projects.ProjectFlow{}, &deployments.ProjectDeployFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, projectFlow, deploymentFlow)
	deploymentClient := deployments.NewProjectDeployFlowClient(&projects.ProjectFlowClient{Dex: client, Flow: projectFlow, ProjectIDLength: 4, PlatformAdminEmail: "admin@superverse.test"}, nil, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	projectID, deploymentID := flowid.RandomID(4), flowid.RandomID(16)
	flowID, err := deployments.FlowID(projectID, deploymentID)
	if err != nil {
		t.Fatal(err)
	}
	dexflowtest.RequireAbsentForTest(ctx, t, client, flowID)
	var missing *dex.FlowNotFoundError
	if _, err = deploymentClient.ReadSnapshotForTest(ctx, projectID, deploymentID); !errors.As(err, &missing) {
		t.Fatalf("missing read classification was %s", dexflowtest.ErrorCategory(err))
	}
	dexflowtest.RequireAbsentForTest(ctx, t, client, flowID)
	t.Logf("FlowID=%s; missing read created no execution", flowID)
}

// TestRetainedDeploymentReadsDexIntegration reads an actual, already removed
// Deployment through its registered read RPC and its Flow client. It starts
// no Worker or Flow and invokes no mutation; the serving Worker must match the
// current read contracts. An operator supplies the retained identity from a
// completed journey.
func TestRetainedDeploymentReadsDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_RELEASE_PROJECT_ID")
	releaseID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_RELEASE_ID")
	deploymentID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_DEPLOYMENT_ID")
	projectFlow, deploymentFlow := &projects.ProjectFlow{}, &deployments.ProjectDeployFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, projectFlow, deploymentFlow)
	deploymentClient := deployments.NewProjectDeployFlowClient(&projects.ProjectFlowClient{Dex: client, Flow: projectFlow, ProjectIDLength: len(projectID), PlatformAdminEmail: "admin@superverse.test"}, nil, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	flowID, err := deployments.FlowID(projectID, deploymentID)
	if err != nil {
		t.Fatal("invalid exact Deployment identity")
	}
	var snapshot deployments.Snapshot
	dexflowtest.ReadRetainedForTest(ctx, t, client, flowID, deploymentFlow.GetProjectDeployment, &snapshot)
	if snapshot.ProjectID != projectID || snapshot.DeploymentID != deploymentID || snapshot.ReleaseID != releaseID || snapshot.Status != deployments.Removed {
		t.Fatal("terminal Deployment identity/status mismatch")
	}
	t.Logf("FlowID=%s business=REMOVED revision=%d", flowID, snapshot.Revision)
	retained, err := deploymentClient.ReadSnapshotForTest(ctx, projectID, deploymentID)
	if err != nil {
		t.Fatalf("retained Deployment FlowClient read failed: %s", dexflowtest.ErrorCategory(err))
	}
	if retained.ProjectID != snapshot.ProjectID || retained.DeploymentID != snapshot.DeploymentID || retained.Status != snapshot.Status || retained.Revision != snapshot.Revision {
		t.Fatal("retained Deployment FlowClient changed authoritative snapshot")
	}
	dexflowtest.RequireCompletedForTest(ctx, t, client, flowID)
}
