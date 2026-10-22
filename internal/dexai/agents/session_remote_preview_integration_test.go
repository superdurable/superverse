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
	"strings"
	"testing"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Only the model is substituted. Reports enter the real HTTP endpoint, edits
// and commits use the Sandbox/Git gate, and Preview uses its actual local engine.
func TestSessionRemoteImmediatePreviewHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+2*previewDeployTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	session.send(ctx, mockInput(t, "inspect-basic", 1).Content)
	session.finishedTurn(ctx)
	session.preparePreview(ctx)
	session.configurePreview(ctx)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "immediate-preview", HostLabel: "integration host"}}, &active, http.StatusOK)
	sequence := uint64(0)
	call := func(name conversation.ToolName, arguments any, duplicate bool) conversation.CallID {
		t.Helper()
		sequence++
		encoded, err := json.Marshal(arguments)
		if err != nil {
			t.Fatal(err)
		}
		report := bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence, ToolCall: &bridgeapi.ToolCall{Name: string(name), Arguments: encoded}}
		var accepted bridgeapi.ReportResponse
		remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
		if accepted.Reported != sequence || accepted.Duplicate {
			t.Fatal("tool report did not have one original admission")
		}
		if duplicate {
			remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
			if !accepted.Duplicate || accepted.Reported != sequence {
				t.Fatal("duplicate tool report had another admission")
			}
		}
		id := conversation.CallID(fmt.Sprintf("remote-%d-%d", active.Epoch, sequence))
		remote.awaitResult(ctx, active.Epoch, id, "")
		return id
	}
	call(conversation.ToolWrite, map[string]any{"path": "raexec-preview-note.md", "content": "before edit\n"}, true)
	edit := call(conversation.ToolEdit, map[string]any{"path": "raexec-preview-note.md", "old_string": "before edit", "new_string": "first pushed note"}, true)
	call(conversation.ToolBash, map[string]any{"command": "make bootstrap", "description": "Restore application dependencies", "timeout_seconds": 600}, false)
	firstID := call(conversation.ToolCommitAndPush, map[string]any{"summary": "Add first remote Preview note", "verification": []string{"raexec-preview-note.md holds first pushed note"}}, true)
	first := remotePushedRequest(ctx, t, session, firstID)
	// There is deliberately no TurnCompleted report before either deployment.
	firstDeployment := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == first.Commit })
	if firstDeployment.Deployment.Last.Status != preview.DeploymentReady || firstDeployment.Preview.SourceCommit != first.Commit || firstDeployment.WorkspaceRequestID != first.WorkspaceRequestID {
		t.Fatal("the first verified push did not deploy immediately on its accepted READY workspace")
	}
	call(conversation.ToolWrite, map[string]any{"path": "raexec-preview-note.md", "content": "second pushed note\n"}, false)
	secondID := call(conversation.ToolCommitAndPush, map[string]any{"summary": "Add second remote Preview note", "verification": []string{"raexec-preview-note.md holds second pushed note"}}, true)
	second := remotePushedRequest(ctx, t, session, secondID)
	if second.Commit == first.Commit || second.Sequence <= first.Sequence || second.WorkspaceRequestID != first.WorkspaceRequestID {
		t.Fatal("two pushes did not have distinct immutable ordered identities")
	}
	// Exercise the actual Preview admission after it has admitted the newer
	// push. This covers delayed old-request admission, not a delayed Step branch.
	session.await(ctx, "second Preview admission", func() bool {
		state, err := session.previews.Get(ctx, session.identity.ProjectID)
		if err != nil {
			t.Fatalf("Preview admission read failed (%T)", err)
		}
		return state.Deployment.HandledSequence >= second.Sequence || state.Deployment.Queued != nil && state.Deployment.Queued.Sequence >= second.Sequence
	})
	for retry := 0; retry < 2; retry++ {
		answer, err := session.previews.RequestDeployment(ctx, session.identity.ProjectID, first)
		if err != nil || answer != preview.AnswerDuplicate {
			t.Fatalf("old immutable Preview request was not duplicate (%T)", err)
		}
	}
	deployed := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Commit == second.Commit })
	if deployed.Deployment.Last.Status != preview.DeploymentReady || deployed.Preview.SourceCommit != second.Commit || deployed.PreparedSource.SourceCommit != second.Commit || deployed.Deployment.HandledSequence < second.Sequence {
		t.Fatal("a delayed old request replaced the newer admitted push")
	}
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Agent.Description.Executor.Mode != "remote" || snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage {
		t.Fatal("Preview waited for remote turn completion or changed executor")
	}
	for _, id := range []conversation.CallID{edit, firstID, secondID} {
		uses, results := 0, 0
		for _, item := range snapshot.Agent.History.Messages {
			for _, use := range item.Message.ToolCalls {
				if use.ID == id {
					uses++
				}
			}
			if item.Message.ToolCallID != nil && *item.Message.ToolCallID == id {
				results++
			}
		}
		if uses != 1 || results != 1 {
			t.Fatal("duplicate edit/commit reports changed their exact-once canonical tool unit")
		}
	}
	actual := remoteSandboxOperator(ctx, t, session.workspace(ctx).Resources, "cd /workspace && git rev-parse HEAD && cat raexec-preview-note.md && git log --format=%s")
	if !strings.Contains(actual, second.Commit) || !strings.Contains(actual, "second pushed note") || strings.Count(actual, "Add first remote Preview note") != 1 || strings.Count(actual, "Add second remote Preview note") != 1 {
		t.Fatal("canonical push differs from the real Git workspace")
	}
	var accepted bridgeapi.ReportResponse
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence + 1, TurnCompleted: true}, &accepted, http.StatusOK)
	session.stopPreview(ctx)
}

func remotePushedRequest(ctx context.Context, t *testing.T, session *mockSession, id conversation.CallID) preview.DeploymentRequest {
	t.Helper()
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	var sequence conversation.Sequence
	for _, item := range snapshot.Agent.History.Messages {
		if item.Message.ToolCallID != nil && *item.Message.ToolCallID == id {
			if item.Message.ToolExecution == nil || item.Message.ToolExecution.Outcome != conversation.ToolOutcomeSucceeded || item.Message.ToolExecution.ResultCode != agentloop.CommitPushedForTest {
				t.Fatal("remote commit was not verified, pushed and unblocked")
			}
			sequence = item.Sequence
		}
	}
	if sequence == 0 {
		t.Fatal("remote pushed result lacks its canonical sequence")
	}
	return preview.DeploymentRequest{Commit: snapshot.Session.Identity.HeadCommit, Sequence: int64(sequence), WorkspaceRequestID: session.workspace(ctx).InstanceID}
}
