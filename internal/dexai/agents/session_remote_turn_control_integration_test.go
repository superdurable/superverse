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
	"slices"
	"strings"
	"testing"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// The platform prelude really opens a segment offering start_agent. Remote
// routing must refuse that name despite the existing segment's frozen catalog.
func TestSessionRemoteExistingPlatformCatalogHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	session.send(ctx, mockInput(t, "remote-handoff", 1).Content)
	initial := session.finishedTurn(ctx)
	requireNoFailure(t, initial.History)
	segment := longHandoffSegment(ctx, t, session)
	if segment.Number == 0 || !slices.Contains(segment.ToolNames, conversation.ToolStartAgent) {
		t.Fatal("real platform segment did not offer start_agent before remote activation")
	}
	before := session.remoteModelCheckpoint(ctx)
	requireModelPositiveControl(t, before.events)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	driver := newRemoteTurnControlDriver(ctx, t, remote, "existing-platform-catalog")
	for _, tool := range driver.active.Tools {
		if conversation.ToolName(tool.Name) == conversation.ToolStartAgent {
			t.Fatal("remote activation offered the prior platform catalog's start_agent")
		}
	}
	id := driver.tool(ctx, conversation.ToolStartAgent, `{"description":"Forbidden remote investigation","prompt":"Inspect the repository without changing it.","kind":"investigate"}`, "unknown or not enabled", true)
	driver.complete(ctx, "The earlier platform catalog grants no remote subagent authority.")
	session.await(ctx, "existing-segment refusal completed", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && snapshot.Agent.Description.LastSequence > before.sequence && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	requireRemoteTurnControlHistory(ctx, t, session, driver.active.Epoch, driver.tools, map[conversation.CallID]string{id: toolfailure.UnknownOrDisabledTool.String()})
	after := session.remoteModelCheckpoint(ctx)
	requireRemoteModelInterval(t, after, before)
	requireRemoteExecutorCommits(t, after, before, driver.active.Epoch, driver.active.Generation, false)
	retained := longHandoffSegment(ctx, t, session)
	if retained.Number != segment.Number || retained.FirstSequence != segment.FirstSequence || !slices.Equal(retained.ToolNames, segment.ToolNames) {
		t.Fatal("remote catalog refusal replaced the existing platform segment or its catalog")
	}
}

type remoteTurnControlDriver struct {
	t        *testing.T
	remote   remoteHTTPClient
	active   bridgeapi.ActivateResponse
	sequence uint64
	tools    []conversation.CallID
}

func newRemoteTurnControlDriver(ctx context.Context, t *testing.T, remote remoteHTTPClient, bindingID string) *remoteTurnControlDriver {
	t.Helper()
	driver := &remoteTurnControlDriver{t: t, remote: remote}
	driver.remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: bindingID, ProviderThreadID: bindingID, HostLabel: "integration host"}}, &driver.active, http.StatusOK)
	return driver
}

func (driver *remoteTurnControlDriver) report(ctx context.Context, report bridgeapi.Report) {
	driver.t.Helper()
	driver.sequence++
	report.Epoch, report.Generation, report.Sequence = driver.active.Epoch, driver.active.Generation, driver.sequence
	var accepted bridgeapi.ReportResponse
	driver.remote.post(ctx, bridgeapi.PathReport, report, &accepted, http.StatusOK)
	if accepted.Duplicate || accepted.Reported != driver.sequence {
		driver.t.Fatal("turn-control report lacks its exact original admission")
	}
}

func (driver *remoteTurnControlDriver) tool(ctx context.Context, name conversation.ToolName, arguments, content string, isError bool) conversation.CallID {
	driver.t.Helper()
	driver.report(ctx, bridgeapi.Report{ToolCall: &bridgeapi.ToolCall{Name: string(name), Arguments: json.RawMessage(arguments)}})
	id := conversation.CallID(fmt.Sprintf("remote-%d-%d", driver.active.Epoch, driver.sequence))
	driver.tools = append(driver.tools, id)
	driver.remote.awaitToolResult(ctx, driver.active.Epoch, id, content, isError)
	return id
}

func (driver *remoteTurnControlDriver) complete(ctx context.Context, text string) {
	driver.report(ctx, bridgeapi.Report{Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindAssistantMessage, Text: text}}})
	driver.report(ctx, bridgeapi.Report{TurnCompleted: true})
}

func requireRemoteTurnControlHistory(ctx context.Context, t *testing.T, session *mockSession, epoch uint64, ids []conversation.CallID, failures map[conversation.CallID]string) {
	t.Helper()
	agent, messages := session.history(ctx)
	uses, results := map[conversation.CallID]int{}, map[conversation.CallID]int{}
	for _, entry := range messages {
		message := entry.Message
		for _, call := range message.ToolCalls {
			if !strings.HasPrefix(string(call.ID), fmt.Sprintf("remote-%d-", epoch)) {
				continue
			}
			if len(message.ToolCalls) != 1 || !slices.Contains(ids, call.ID) {
				t.Fatal("remote allowance calls coalesced into a batch or acquired an unexpected identity")
			}
			uses[call.ID]++
		}
		if message.ToolCallID != nil && slices.Contains(ids, *message.ToolCallID) {
			id := *message.ToolCallID
			results[id]++
			code, refused := failures[id]
			want := conversation.ToolOutcomeSucceeded
			if refused {
				want = conversation.ToolOutcomeKnownFailure
			}
			if message.ToolExecution == nil || message.ToolExecution.Outcome != want || message.ToolExecution.ResultCode != code {
				t.Fatalf("canonical result %s has the wrong outcome or refusal code", id)
			}
		}
	}
	for _, id := range ids {
		if uses[id] != 1 || results[id] != 1 {
			t.Fatalf("remote tool %s has %d canonical calls and %d results", id, uses[id], results[id])
		}
	}
	if len(agent.Description.Subagents) != 0 {
		t.Fatal("remote turn control admitted a platform subagent")
	}
}
