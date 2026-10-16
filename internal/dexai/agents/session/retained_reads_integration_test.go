// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
)

// TestMissingSessionReadsDexIntegration resolves reads of an absent Session
// through the actual Dex Server. No execution, Worker or provider resource is
// created by these reads.
func TestMissingSessionReadsDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	flow := &sessionagent.SessionAgentFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, flow)
	sessions := sessionagent.NewSessionAgentFlowClientForTest(client, flow, dexflowtest.ExecutionReaderForTest(t, address), "")
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	flowID := agentloop.SessionFlowID(flowid.RandomID(16))
	for _, test := range []struct {
		name string
		read func(context.Context) error
	}{
		{"SessionSummary", func(ctx context.Context) error { _, err := sessions.GetSessionSummary(ctx, flowID); return err }},
		{"SessionSnapshot", func(ctx context.Context) error { _, err := sessions.GetSnapshot(ctx, flowID); return err }},
		{"ArchivedMessages", func(ctx context.Context) error {
			_, err := sessions.GetArchivedMessageRange(ctx, flowID, 11, 10)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dexflowtest.RequireAbsentForTest(ctx, t, client, string(flowID))
			var missing *dex.FlowNotFoundError
			if err := test.read(ctx); !errors.As(err, &missing) {
				t.Fatalf("missing read classification was %s", dexflowtest.ErrorCategory(err))
			}
			dexflowtest.RequireAbsentForTest(ctx, t, client, string(flowID))
			t.Logf("FlowID=%s; missing read created no execution", flowID)
		})
	}
}

// TestRetainedSessionReadsDexIntegration reads the actual, already archived
// Session of a deleted Project through its registered read RPCs and its Flow
// client. It starts no Worker or Flow and invokes no mutation; the serving
// Worker must match the current read contracts. An operator supplies the
// Project of a completed journey.
func TestRetainedSessionReadsDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_PROJECT_ID")
	flow := &sessionagent.SessionAgentFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, flow)
	sessions := sessionagent.NewSessionAgentFlowClientForTest(client, flow, dexflowtest.ExecutionReaderForTest(t, address), "")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	flowID := flowid.SessionFlowID(projectID)
	var summary agentloop.SessionSummary
	t.Run("GetSessionSummary", func(t *testing.T) {
		dexflowtest.ReadRetainedForTest(ctx, t, client, flowID, flow.GetSessionSummary, &summary)
		if summary.Identity.ProjectID != projectID || summary.Status != agentloop.SessionStatusArchived {
			t.Fatal("terminal Agent Session summary identity/status mismatch")
		}
		t.Logf("FlowID=%s business=ARCHIVED revision=%d", flowID, summary.Revision)
		retained, err := sessions.GetSessionSummary(ctx, agentloop.FlowID(flowID))
		if err != nil {
			t.Fatalf("retained Session FlowClient read failed: %s", dexflowtest.ErrorCategory(err))
		}
		if retained.Identity != summary.Identity || retained.Status != summary.Status || retained.Revision != summary.Revision {
			t.Fatal("retained Session FlowClient changed authoritative summary")
		}
		dexflowtest.RequireCompletedForTest(ctx, t, client, flowID)
	})
	t.Run("GetSnapshot", func(t *testing.T) {
		var snapshot agentloop.SessionSnapshot
		dexflowtest.ReadRetainedForTest(ctx, t, client, flowID, flow.GetSnapshot, &snapshot)
		if snapshot.Session.Identity.ProjectID != projectID || snapshot.Session.Status != agentloop.SessionStatusArchived || snapshot.Agent == nil {
			t.Fatal("terminal combined Session/Agent snapshot does not match current read contract")
		}
		if summary.Revision != 0 && snapshot.Session.Revision != summary.Revision {
			t.Fatal("unchanged terminal summary and full snapshot revisions differ")
		}
		t.Logf("FlowID=%s business=ARCHIVED revision=%d retainedMessages=%d", flowID, snapshot.Session.Revision, len(snapshot.Agent.History.Messages))
		retained, err := sessions.GetSnapshot(ctx, agentloop.FlowID(flowID))
		if err != nil {
			t.Fatalf("retained Session snapshot FlowClient read failed: %s", dexflowtest.ErrorCategory(err))
		}
		if !reflect.DeepEqual(retained.Session, snapshot.Session) || retained.Agent == nil || len(retained.Agent.History.Messages) != len(snapshot.Agent.History.Messages) {
			t.Fatal("retained Session FlowClient changed authoritative snapshot")
		}
		dexflowtest.RequireCompletedForTest(ctx, t, client, flowID)
	})
}
