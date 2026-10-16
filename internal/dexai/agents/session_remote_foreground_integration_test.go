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

	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Operator reads prove that the actual Sandbox process began before takeover
// and received Stop; they do not replace the production command ledger.
func remoteForegroundProof(ctx context.Context, t *testing.T, resources sandbox.Resources) string {
	t.Helper()
	script := `if test -f "$1"; then cat -- "$1"; fi`
	path := sandboxd.WorkspaceRoot + "/raexec-foreground.txt"
	return remoteSandboxOperator(ctx, t, resources, script, path)
}

func remoteSandboxOperator(ctx context.Context, t *testing.T, resources sandbox.Resources, script string, arguments ...string) string {
	t.Helper()
	command := append([]string{"bash", "-c", script, "bash"}, arguments...)
	if dexflowtest.LocalLaneForTest(t) == dexflowtest.LaneKindForTest {
		return operatorCommand(t, ctx, "kubectl", append([]string{"--kubeconfig", dexflowtest.Setting(t, "SUPERVERSE_TEST_KIND_KUBECONFIG"), "--context", "kind-superverse-v2", "--request-timeout=15s", "-n", resources.Boundary.Name, "exec", resources.Instance.Name, "--"}, command...)...)
	}
	return operatorCommand(t, ctx, "docker", append([]string{"exec", resources.Instance.Name}, command...)...)
}

func TestSessionRemoteForegroundTakeoverHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	binding := bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "foreground-takeover", ProviderThreadID: "foreground-takeover", HostLabel: "integration host"}}
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, binding, &active, http.StatusOK)
	next := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var accepted bridgeapi.ReportResponse
	report := bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolWrite), Arguments: json.RawMessage(`{"path":"raexec-foreground.txt","content":""}`)}}
	remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
	remote.awaitResult(ctx, active.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-1", active.Epoch)), "")
	workspace := session.workspace(ctx)
	command := `printf 'started\n' >> raexec-foreground.txt; trap 'printf "stopped\n" >> raexec-foreground.txt; exit 143' TERM; while :; do sleep 1; done`
	arguments, err := json.Marshal(map[string]any{"command": command, "description": "Observe the real original foreground command during executor takeover", "timeout_seconds": 120})
	if err != nil {
		t.Fatal(err)
	}
	report.Sequence, report.ToolCall = 2, &bridgeapi.ToolCall{Name: string(conversation.ToolBash), Arguments: arguments}
	remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
	session.await(ctx, "original Sandbox command began", func() bool { return remoteForegroundProof(ctx, t, workspace.Resources) == "started" })
	var takeover bridgeapi.ActivateResponse
	next.post(ctx, bridgeapi.PathActivate, binding, &takeover, http.StatusOK)
	if takeover.Epoch != active.Epoch+1 || takeover.Generation <= active.Generation {
		t.Fatal("foreground takeover did not advance authority")
	}
	session.await(ctx, "captured original command stopped", func() bool { return remoteForegroundProof(ctx, t, workspace.Resources) == "started\nstopped" })
	oldCall := conversation.CallID(fmt.Sprintf("remote-%d-2", active.Epoch))
	session.await(ctx, "one canonical uncertain result for stopped command", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		if err != nil {
			return false
		}
		count := 0
		for _, entry := range snapshot.Agent.History.Messages {
			if entry.Message.ToolCallID != nil && *entry.Message.ToolCallID == oldCall {
				count++
				if entry.Message.ToolExecution == nil || entry.Message.ToolExecution.Outcome != conversation.ToolOutcomeUnknown {
					t.Fatal("Stop claimed an uncertain foreground effect succeeded")
				}
			}
		}
		return count == 1
	})
	var refused bridgeapi.ErrorBody
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 3, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolBash), Arguments: json.RawMessage(`{"command":"printf forbidden >> raexec-foreground.txt","description":"This stale old executor must never dispatch","timeout_seconds":30}`)}}, &refused, http.StatusConflict)
	if refused.Code != bridgeapi.ErrorExecutorChanged {
		t.Fatal("old connection was not refused")
	}
	command = `test "$(grep -c '^started$' raexec-foreground.txt)" -eq 1 && test "$(grep -c '^stopped$' raexec-foreground.txt)" -eq 1 && ! grep -q forbidden raexec-foreground.txt && printf 'replacement completed\n'`
	arguments, err = json.Marshal(map[string]any{"command": command, "description": "Verify the replacement is not stopped and the original was not replayed", "timeout_seconds": 30})
	if err != nil {
		t.Fatal(err)
	}
	next.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: takeover.Epoch, Generation: takeover.Generation, Sequence: 1, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolBash), Arguments: arguments}}, &accepted, http.StatusOK)
	next.awaitResult(ctx, takeover.Epoch, conversation.CallID(fmt.Sprintf("remote-%d-1", takeover.Epoch)), "replacement completed")
	if proof := remoteForegroundProof(ctx, t, workspace.Resources); strings.Count(proof, "started") != 1 || strings.Count(proof, "stopped") != 1 || strings.Contains(proof, "forbidden") {
		t.Fatal("foreground ledger identity was replayed or a stale report ran")
	}
}
