// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
)

// This bounded engine read proves the selected durable wait, not business chat.
func (session *mockSession) awaitRemoteWait(ctx context.Context) {
	session.t.Helper()
	session.await(ctx, "AwaitRemoteAgent durable wait", func() bool {
		state, err := session.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(session.id)})
		if err != nil {
			session.t.Fatalf("remote wait diagnostic read failed (%T)", err)
		}
		for _, step := range state.GetActiveStepExecutions() {
			if step.GetStepType() == dex.GetFinalStepType(sessionagent.AwaitRemoteAgent{}) && step.GetWaitingCondition() != nil {
				return true
			}
		}
		return false
	})
}

// No Bridge call renews the workspace during this actual sandbox.hold wait.
func TestSessionRemoteOfflineSandboxReclaimHTTPDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout+hold+sandboxReclaimTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "offline-hold", ProviderThreadID: "offline-hold", HostLabel: "integration host"}}, &active, http.StatusOK)
	var accepted bridgeapi.ReportResponse
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolWrite), Arguments: json.RawMessage(`{"path":"raexec-offline.txt","content":"Kept across offline reclamation\n"}`)}}, &accepted, http.StatusOK)
	remote.awaitResult(ctx, active.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-1", active.Epoch)), "Created raexec-offline.txt")
	session.awaitRemoteWait(ctx)
	ready := session.workspace(ctx)
	reclaimed := session.awaitReclaim(ctx, ready)
	if reclaimed.Resources.Volume.UID == "" || reclaimed.Resources.Volume.UID != ready.Resources.Volume.UID {
		t.Fatal("offline reclamation lost the authoritative workspace volume")
	}
	session.awaitRemoteWait(ctx)
	executor, err := session.client.ReadExecutor(ctx, session.identity.ProjectID)
	if err != nil || executor.Mode != remoteexec.ModeRemote || executor.Epoch != active.Epoch || executor.Generation != active.Generation || executor.Remote.Reported != 1 {
		t.Fatal("offline reclamation changed executor authority or fell back to platform")
	}
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 2, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolRead), Arguments: json.RawMessage(`{"path":"raexec-offline.txt"}`)}}, &accepted, http.StatusOK)
	remote.awaitResult(ctx, active.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-2", active.Epoch)), "Kept across offline reclamation")
	replaced := session.workspace(ctx)
	if replaced.InstanceID == ready.InstanceID || replaced.Instance != sessionworkspace.InstanceReady || replaced.Resources.Volume.UID != ready.Resources.Volume.UID {
		t.Fatal("reconnect did not provision a new real instance on the same volume")
	}
	if snapshot, err := session.client.GetSnapshot(ctx, session.id); err != nil || snapshot.Agent.Description.ContextModel != "" {
		t.Fatal("offline remote recovery opened platform context")
	}
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 3, TurnCompleted: true}, &accepted, http.StatusOK)
	session.requireNoPreview(ctx)
}

// Replace the actual lane Worker both at its remote wait and inside a command.
// Operator markers hold the real Sandbox command until the replacement serves.
func TestSessionRemoteWorkerRestartHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "worker-restart", ProviderThreadID: "worker-restart", HostLabel: "integration host"}}, &active, http.StatusOK)
	var accepted bridgeapi.ReportResponse
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolWrite), Arguments: json.RawMessage(`{"path":"raexec-foreground.txt","content":""}`)}}, &accepted, http.StatusOK)
	remote.awaitResult(ctx, active.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-1", active.Epoch)), "")
	session.awaitRemoteWait(ctx)
	restart := func() {
		dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
			_, err := session.client.ReadExecutor(ctx, session.identity.ProjectID)
			return err
		})
	}
	restart()
	session.awaitRemoteWait(ctx)
	workspace := session.workspace(ctx)
	command := `printf 'started\n' >> raexec-foreground.txt; while ! test -f raexec-restart-release; do sleep 1; done; printf 'completed\n' >> raexec-foreground.txt; printf 'restart command completed\n'`
	arguments, err := json.Marshal(map[string]any{"command": command, "description": "Hold the real command during Worker replacement", "timeout_seconds": 600})
	if err != nil {
		t.Fatal(err)
	}
	report := bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 2, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolBash), Arguments: arguments}}
	remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
	session.await(ctx, "actual command started before Worker restart", func() bool { return remoteForegroundProof(ctx, t, workspace.Resources) == "started" })
	restart()
	remoteSandboxOperator(ctx, t, workspace.Resources, `touch -- "$1"`, "/workspace/raexec-restart-release")
	call := conversation.CallID(fmt.Sprintf("remote-%d-2", active.Epoch))
	remote.awaitResult(ctx, active.Epoch, call, "restart command completed")
	if proof := remoteForegroundProof(ctx, t, workspace.Resources); proof != "started\ncompleted" {
		t.Fatal("Worker restart replayed the actual command instead of reconciling its ledger")
	}
	remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
	if !accepted.Duplicate || accepted.Reported != 2 {
		t.Fatal("report retry after Worker restart created another admission")
	}
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || snapshot.Agent.Description.Executor.Mode != remoteexec.ModeRemote || snapshot.Agent.Description.Executor.Generation != active.Generation {
		t.Fatal("Worker restart lost canonical executor authority")
	}
	results := 0
	for _, entry := range snapshot.Agent.History.Messages {
		if entry.Message.ToolCallID != nil && *entry.Message.ToolCallID == call {
			results++
		}
	}
	if results != 1 || snapshot.Agent.Description.ContextModel != "" || snapshot.Session.Status != agentloop.SessionStatusActive {
		t.Fatal("restart duplicated the tool result, called a platform model or closed the Session")
	}
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 3, TurnCompleted: true}, &accepted, http.StatusOK)
}
