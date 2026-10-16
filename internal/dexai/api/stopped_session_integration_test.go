// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
)

// TestStoppedSessionSnapshotDexIntegration uses the actual stopped Session of a
// Project and its existing application Worker. No Flow is started or mutated, no dependencies
// are substituted, and no message bodies or credentials are logged.
func TestStoppedSessionSnapshotDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_STOPPED_PROJECT_ID")
	flowID := agentloop.SessionFlowID(projectID)
	flow := &sessionagent.SessionAgentFlow{}
	sdk := dexflowtest.ClientForTest(t, address, nil, flow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	client := sessionagent.NewSessionAgentFlowClientForTest(sdk, flow, reader, "")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	before, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: string(flowID)})
	if err != nil {
		t.Fatalf("real engine read failed (%T)", err)
	}
	if before.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_CANCELED {
		t.Fatal("requires an actually canceled Session; no mutation issued")
	}
	snapshot, err := client.GetSnapshot(ctx, flowID)
	if err != nil {
		t.Fatalf("retained typed snapshot failed (%T)", err)
	}
	if snapshot.Session.Identity.ProjectID != projectID || snapshot.Session.Status != agentloop.SessionStatusActive || snapshot.Agent == nil {
		t.Fatal("requires the observed retained ACTIVE conversation after engine cancellation")
	}
	if snapshot.ExecutionFailure != agentloop.ExecutionCanceled {
		t.Fatal("canceled execution was presented as active")
	}
	output := sessionSnapshot(agents.Snapshot{Session: snapshot.Session, Agent: snapshot.Agent, ExecutionFailure: snapshot.ExecutionFailure})
	if output.Status != pb.SessionStatus_SESSION_STATUS_FAILED || output.Failure == nil || output.Failure.Code != "SESSION_EXECUTION_CANCELED" || output.Agent == nil || output.Revision != snapshot.Session.Revision {
		t.Fatal("resource presentation lost history/revision or advertised active work")
	}
	var retained agentloop.SessionSnapshot
	if err = sdk.InvokeRPC(ctx, string(flowID), flow.GetSnapshot, nil, &retained); err != nil {
		t.Fatalf("direct retained read failed (%T)", err)
	}
	if !reflect.DeepEqual(retained.Session, snapshot.Session) || retained.Agent == nil || len(retained.Agent.History.Messages) != len(snapshot.Agent.History.Messages) {
		t.Fatal("liveness read changed the durable Session snapshot")
	}
	// Read only: compare the actual retained summary with the conversation
	// read. Revocation and drain mutation coverage belongs to a separate owned
	// Session, never to the original dirty workspace.
	retainedSummary, err := client.GetSessionSummary(ctx, flowID)
	if err != nil {
		t.Fatalf("retained lifecycle read failed (%T)", err)
	}
	if !reflect.DeepEqual(retainedSummary, snapshot.Session) {
		t.Fatal("retained lifecycle and conversation reads disagree")
	}
	after, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: string(flowID)})
	if err != nil {
		t.Fatal("final bounded engine read failed")
	}
	if after.GetFlowStatus() != before.GetFlowStatus() || after.GetFlowExecutionId().GetRunId() != before.GetFlowExecutionId().GetRunId() {
		t.Fatal("read-only verification changed or raced the stopped execution")
	}
	t.Logf("FlowID=%s engine=CANCELED retained=ACTIVE resource=FAILED sequence=%d messages=%d; original execution unchanged", flowID, snapshot.Agent.Description.LastSequence, len(snapshot.Agent.History.Messages))
}
