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
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// The original connection retries its original reports after two actual
// authorized Stops, with a genuinely new remote turn between those Stops.
func TestSessionRemoteTwoStopsOldGenerationHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	agentAPI := remoteStopAgentAPI(t)
	before := session.remoteModelCheckpoint(ctx)
	driver := newRemoteTurnControlDriver(ctx, t, pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID), "two-stops-old-generation")
	original := driver.active.Generation
	tool := bridgeapi.Report{Epoch: driver.active.Epoch, Generation: original, Sequence: 1, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolWrite), Arguments: json.RawMessage(`{"path":"raexec-two-stops.txt","content":"Original two-Stop proof\n"}`)}}
	driver.tool(ctx, conversation.ToolWrite, string(tool.ToolCall.Arguments), "Created raexec-two-stops.txt", false)
	public := bridgeapi.Report{Epoch: driver.active.Epoch, Generation: original, Sequence: 2, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: "The original remote turn remains open."}}}
	driver.report(ctx, bridgeapi.Report{Events: public.Events})
	session.await(ctx, "original public report consumed before the first Stop", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		if err != nil {
			return false
		}
		for _, entry := range snapshot.Agent.History.Messages {
			if entry.Message.Content == public.Events[0].Text {
				return true
			}
		}
		return false
	})
	session.awaitRemoteWait(ctx)
	first := remoteStopToolbar(ctx, t, session, agentAPI, caller)
	completion := bridgeapi.Report{Epoch: driver.active.Epoch, Generation: original, Sequence: 3, TurnCompleted: true}
	for _, report := range []bridgeapi.Report{tool, public, completion} {
		remoteStopReplay(ctx, t, driver, report)
	}
	driver.active.Generation = first.Description.Executor.Generation
	driver.report(ctx, bridgeapi.Report{Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: "Begin a genuinely new remote turn after the first Stop."}}})
	session.awaitRemoteWait(ctx)
	secondTurn, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || secondTurn.Agent.Description.Turn != first.Description.Turn+1 {
		t.Fatal("the second Stop lacks a genuinely new remote turn")
	}
	second := remoteStopToolbar(ctx, t, session, agentAPI, caller)
	if second.Description.Executor.Generation != original+2 || second.Description.Turn != first.Description.Turn+1 {
		t.Fatal("two actual Stops did not preserve the two-turn generation history")
	}
	// Replays of admitted identities answer duplicate; fresh identities must
	// fail before admission. Neither has any public or tool effect.
	stale := []bridgeapi.Report{tool, public, completion,
		{Epoch: driver.active.Epoch, Generation: original, Sequence: driver.sequence + 1, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolWrite), Arguments: json.RawMessage(`{"path":"raexec-two-stops.txt","content":"Forbidden old-generation mutation\n"}`)}},
		{Epoch: driver.active.Epoch, Generation: original, Sequence: driver.sequence + 1, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: "Forbidden old-generation public input"}}},
		{Epoch: driver.active.Epoch, Generation: original, Sequence: driver.sequence + 1, TurnCompleted: true},
	}
	for index, report := range stale {
		t.Run(fmt.Sprintf("stale_form_%d", index+1), func(t *testing.T) {
			remoteStopReplay(ctx, t, driver, report)
			unchanged, err := session.client.GetSnapshot(ctx, session.id)
			if err != nil || unchanged.Agent.Description.LastSequence != second.Description.LastSequence || unchanged.Agent.Description.Turn != second.Description.Turn || unchanged.Agent.Description.Status != agentloop.AgentStatusWaitingForMessage || unchanged.Agent.Description.Executor.Generation != original+2 || unchanged.Agent.Description.Executor.Remote.Reported != driver.sequence {
				t.Fatal("a report from two Stops ago changed canonical entries, turn or admission watermark")
			}
		})
	}
	driver.active.Generation = second.Description.Executor.Generation
	driver.tool(ctx, conversation.ToolRead, `{"path":"raexec-two-stops.txt"}`, "Original two-Stop proof", false)
	driver.complete(ctx, "Current-generation work continues after two Stops.")
	remoteStopAwaitCompletion(ctx, t, session, second.Description.Turn+1)
	requireRemoteTurnControlHistory(ctx, t, session, driver.active.Epoch, driver.tools, nil)
	_, messages := session.history(ctx)
	publicCount := 0
	for _, entry := range messages {
		if entry.Message.Content == public.Events[0].Text {
			publicCount++
		}
	}
	if publicCount != 1 {
		t.Fatal("an old original public/tool report was replayed")
	}
	requireRemoteModelInterval(t, session.remoteModelCheckpoint(ctx), before)
}

// Operator reads only observe the real command's process markers. The Stop
// uses the business API; no takeover, scripted handler or state injection runs.
func TestSessionRemoteForegroundToolbarStopHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	agentAPI := remoteStopAgentAPI(t)
	before := session.remoteModelCheckpoint(ctx)
	driver := newRemoteTurnControlDriver(ctx, t, pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID), "foreground-toolbar-stop")
	driver.tool(ctx, conversation.ToolWrite, `{"path":"raexec-toolbar-stop.txt","content":""}`, "Created raexec-toolbar-stop.txt", false)
	workspace := session.workspace(ctx)
	proof := func() string {
		return remoteSandboxOperator(ctx, t, workspace.Resources, `if test -f /workspace/raexec-toolbar-stop.txt; then cat /workspace/raexec-toolbar-stop.txt; fi`)
	}
	command := `trap 'printf "stopped\n" >> raexec-toolbar-stop.txt; exit 143' TERM; printf 'started\n' >> raexec-toolbar-stop.txt; while :; do sleep 1; done`
	arguments, err := json.Marshal(map[string]any{"command": command, "description": "Observe one real cancellable command during same-connection toolbar Stop", "timeout_seconds": 120})
	if err != nil {
		t.Fatal(err)
	}
	originalGeneration := driver.active.Generation
	driver.report(ctx, bridgeapi.Report{ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolBash), Arguments: arguments}})
	id := conversation.CallID(fmt.Sprintf("remote-%d-%d", driver.active.Epoch, driver.sequence))
	driver.tools = append(driver.tools, id)
	session.await(ctx, "actual cancellable foreground command started", func() bool { return proof() == "started" })
	stopped := remoteStopToolbar(ctx, t, session, agentAPI, caller)
	session.await(ctx, "original foreground command ended once", func() bool { return proof() == "started\nstopped" })
	// The live command cannot complete normally before Stop. Its mutating
	// effect is unknown, so the canonical result must retain that policy.
	driver.remote.awaitToolResult(ctx, driver.active.Epoch, id, "interrupted", true)
	_, history := session.history(ctx)
	calls, results := 0, 0
	for _, entry := range history {
		for _, call := range entry.Message.ToolCalls {
			if call.ID == id {
				calls++
			}
		}
		if entry.Message.ToolCallID != nil && *entry.Message.ToolCallID == id {
			results++
			if entry.Message.ToolExecution == nil || entry.Message.ToolExecution.Outcome != conversation.ToolOutcomeUnknown || entry.Message.ToolExecution.ResultCode != toolfailure.InterruptedByUser.String() {
				t.Fatal("Stop lost the original in-flight command's unknown interrupted effect")
			}
		}
	}
	if calls != 1 || results != 1 {
		t.Fatal("stopped foreground call did not have exactly one canonical call and result")
	}
	for _, report := range []bridgeapi.Report{
		{Epoch: driver.active.Epoch, Generation: originalGeneration, Sequence: driver.sequence + 1, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolBash), Arguments: json.RawMessage(`{"command":"printf forbidden >> raexec-toolbar-stop.txt","description":"Old generation must not execute","timeout_seconds":30}`)}},
		{Epoch: driver.active.Epoch, Generation: originalGeneration, Sequence: driver.sequence + 1, Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: "Forbidden old command-generation input"}}},
		{Epoch: driver.active.Epoch, Generation: originalGeneration, Sequence: driver.sequence + 1, TurnCompleted: true},
	} {
		remoteStopReplay(ctx, t, driver, report)
	}
	driver.active.Generation = stopped.Description.Executor.Generation
	driver.tool(ctx, conversation.ToolBash, `{"command":"test \"$(grep -c '^started$' raexec-toolbar-stop.txt)\" -eq 1 && test \"$(grep -c '^stopped$' raexec-toolbar-stop.txt)\" -eq 1 && ! grep -q forbidden raexec-toolbar-stop.txt && printf 'current generation completed\\n'","description":"Verify one original process and current-generation continuation","timeout_seconds":30}`, "current generation completed", false)
	driver.complete(ctx, "The same connection resumed after its toolbar Stop.")
	remoteStopAwaitCompletion(ctx, t, session, stopped.Description.Turn+1)
	_, history = session.history(ctx)
	calls, results = 0, 0
	for _, entry := range history {
		for _, call := range entry.Message.ToolCalls {
			if call.ID == id {
				calls++
			}
		}
		if entry.Message.ToolCallID != nil && *entry.Message.ToolCallID == id {
			results++
		}
	}
	if calls != 1 || results != 1 || proof() != "started\nstopped" {
		t.Fatal("current-generation continuation replayed or failed to end the original command exactly once")
	}
	requireRemoteModelInterval(t, session.remoteModelCheckpoint(ctx), before)
}

func remoteStopAgentAPI(t *testing.T) pb.AgentServiceClient {
	t.Helper()
	connection, err := grpc.NewClient(dexflowtest.Setting(t, "SUPERVERSE_TEST_API_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return pb.NewAgentServiceClient(connection)
}

func remoteStopToolbar(ctx context.Context, t *testing.T, session *mockSession, api pb.AgentServiceClient, caller *pb.CallerContext) agentloop.AgentSnapshot {
	t.Helper()
	before, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || before.Agent.Description.Executor.Remote == nil {
		t.Fatal("toolbar Stop has no canonical remote authority")
	}
	stop, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = api.InterruptAgent(stop, &pb.InterruptAgentRequest{ProjectId: session.identity.ProjectID, Turn: before.Agent.Description.Turn, Caller: caller})
	if err != nil {
		t.Fatalf("authorized business API toolbar Stop failed (%T)", err)
	}
	executor, err := session.client.ReadExecutor(stop, session.identity.ProjectID)
	if err != nil || executor.Generation != before.Agent.Description.Executor.Generation+1 || executor.Epoch != before.Agent.Description.Executor.Epoch || executor.Remote == nil || executor.Remote.ConnectionID != before.Agent.Description.Executor.Remote.ConnectionID {
		t.Fatal("Stop did not advance exactly one generation on the same connection and epoch")
	}
	return session.waitForSnapshot(ctx, "actual toolbar Stop settlement", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Turn == before.Agent.Description.Turn && agent.Description.Executor.Generation == executor.Generation && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
}

// remoteStopReplay sends an original-generation report after Stop: an admitted
// sequence answers duplicate, so only a never-admitted one is stale_generation.
func remoteStopReplay(ctx context.Context, t *testing.T, driver *remoteTurnControlDriver, report bridgeapi.Report) {
	t.Helper()
	if report.Sequence <= driver.sequence {
		var answer bridgeapi.ReportResponse
		driver.remote.post(ctx, bridgeapi.PathReport, report, &answer, http.StatusOK)
		if !answer.Duplicate || answer.Reported != driver.sequence {
			t.Fatal("an admitted original-generation report was not answered as a duplicate")
		}
		return
	}
	var refusal bridgeapi.ErrorBody
	driver.remote.post(ctx, bridgeapi.PathReport, report, &refusal, http.StatusConflict)
	if refusal.Code != bridgeapi.ErrorStaleGeneration {
		t.Fatal("an original-generation report was not refused as stale_generation")
	}
}

func remoteStopAwaitCompletion(ctx context.Context, t *testing.T, session *mockSession, turn int64) {
	t.Helper()
	session.await(ctx, "current-generation remote turn completed", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && snapshot.Agent.Description.Turn == turn && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
}
