// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package provision_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
)

// TestRetainedProvisionReadsDexIntegration reads an actual, already completed
// provisioning through its registered read RPC and its Flow client. It starts
// no Worker or Flow and invokes no mutation; the serving Worker must match the
// current read contracts. An operator supplies the identity of a provisioning
// a completed journey left: every provisioning completes once it reported its
// outcome.
func TestRetainedProvisionReadsDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_PROJECT_ID")
	requestID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_WORKSPACE_REQUEST_ID")
	flow := &provision.SandboxProvisionFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, flow)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	flowID := provision.FlowIDForTest(projectID, requestID)
	var snapshot provision.Snapshot
	dexflowtest.ReadRetainedForTest(ctx, t, client, flowID, flow.GetProvision, &snapshot)
	id := snapshot.Request.Identity
	if id.ProjectID != projectID || snapshot.Request.WorkspaceRequestID != requestID || snapshot.Outcome == nil || snapshot.Outcome.Reported == (provision.ReportAnswerForTest{}) {
		t.Fatal("completed provisioning identity or outcome mismatch")
	}
	t.Logf("FlowID=%s outcome=%s reported=%s", flowID, snapshot.Outcome.Status, snapshot.Outcome.Reported)
	retained, err := provision.NewSandboxProvisionFlowClientForTest(client, flow, dexflowtest.ExecutionReaderForTest(t, address), deploymentnames.MustFor(deploymentnames.EnvironmentTest)).Get(ctx, projectID, requestID)
	if err != nil {
		t.Fatalf("retained provisioning FlowClient read failed: %s", dexflowtest.ErrorCategory(err))
	}
	if !reflect.DeepEqual(retained, snapshot) {
		t.Fatal("retained provisioning FlowClient changed the authoritative snapshot")
	}
	dexflowtest.RequireCompletedForTest(ctx, t, client, flowID)
}
