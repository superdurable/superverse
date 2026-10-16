// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"context"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
)

// Reads genuine retained Session executions created by the owned business E2E
// journey. It does not start a fixture Worker or manufacture Session outputs.
// Resource reconciliation itself is validated separately through Project E2E.
func TestProjectSessionTerminationDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	flow := &SessionAgentFlow{}
	client := newSessionAgentFlowClient(dexflowtest.ClientForTest(t, address, nil, flow), flow, dexflowtest.ExecutionReaderForTest(t, address), "")
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	for _, tc := range []struct {
		name     string
		abnormal bool
		status   dexpb.FlowStatus
	}{
		{"FAILED", true, dexpb.FlowStatus_FLOW_STATUS_FAILED},
		{"CANCELED", true, dexpb.FlowStatus_FLOW_STATUS_CANCELED},
		{"TERMINATED", true, dexpb.FlowStatus_FLOW_STATUS_TERMINATED},
		{"COMPLETED", false, dexpb.FlowStatus_FLOW_STATUS_COMPLETED},
		{"ARCHIVED_RUNNING", false, dexpb.FlowStatus_FLOW_STATUS_RUNNING},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := agentloop.FlowID(dexflowtest.Setting(t, "SUPERVERSE_TEST_CLEANUP_"+tc.name+"_SESSION_FLOW_ID"))
			summary, e := client.execution.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: string(id)})
			if e != nil || summary.GetFlowStatus() != tc.status {
				t.Fatalf("requires actual %s Session execution (%T)", tc.name, e)
			}
			session, e := client.GetSessionSummary(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			project := session.Identity.ProjectID
			if id != agentloop.SessionFlowID(project) {
				t.Fatalf("Session %s is not the one Session of its Project %s", id, project)
			}
			if tc.name == "ARCHIVED_RUNNING" && session.Status != agentloop.SessionStatusArchived {
				t.Fatal("requires archived but running Session")
			}
			abnormal, e := client.HasAbnormalProjectSessionTermination(ctx, id)
			if e != nil || abnormal != tc.abnormal {
				t.Fatalf("actual terminal classification mismatch (%T)", e)
			}
			if tc.name == "COMPLETED" {
				// A completion call to an ended Session asks nothing, and the
				// read confirms its receipt.
				if e = client.CompleteProjectSession(ctx, project, commandRPCTimeout); e != nil {
					t.Fatalf("a completed Session was asked again (%T)", e)
				}
				done, e := client.ProjectSessionCompleted(ctx, project)
				if e != nil || !done {
					t.Fatalf("requires genuine matching Project cleanup receipt (%T)", e)
				}
			}
			if tc.status == dexpb.FlowStatus_FLOW_STATUS_RUNNING {
				if done, e := client.ProjectSessionCompleted(ctx, project); e != nil || done {
					t.Fatalf("a running Session read as completed (%T)", e)
				}
			}
		})
	}
	// The Session of a Project that never existed has no engine to read.
	absent := flowid.RandomID(12)
	if abnormal, e := client.HasAbnormalProjectSessionTermination(ctx, agentloop.SessionFlowID(absent)); e == nil || abnormal {
		t.Fatal("missing engine was treated as cleaned")
	}
	if done, e := client.ProjectSessionCompleted(ctx, absent); e == nil || done {
		t.Fatal("the Session of a missing Project read as completed")
	}
}
