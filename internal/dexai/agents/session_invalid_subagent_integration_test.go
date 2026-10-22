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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// Invalid starts run through RouteTool's generated validator, before admission.
func TestSessionMockSubagentInvalidStartDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "subagent-invalid-start", 1).Content)
	session.waitForSnapshot(ctx, "the reply after all three refused starts", func(agent agentloop.AgentSnapshot) bool {
		return len(replies(agent.History)) >= 4 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	agent, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 4 || assistant[3].Message.Content != "All three starts were refused as invalid_arguments. No subagent started." {
		t.Fatalf("expected all four mock replies, got %+v", assistant)
	}
	if tools := ofRole(messages, conversation.MessageRoleTool); len(tools) != 3 {
		t.Fatalf("expected three refused starts, got %d tool results", len(tools))
	}
	if len(agent.Description.Subagents) != 0 {
		t.Fatalf("a refused start wrote a record: %+v", agent.Description.Subagents)
	}
	workspace := session.workspace(ctx)
	t.Logf("workspace logical ID=%s state=%s physical instance UID=%s", workspace.InstanceID, workspace.Instance.String(), workspace.Resources.Instance.UID)
	objects := newSandboxObjects(t)
	for index, field := range []string{"description", "slug", "prompt"} {
		refused := toolResult(t, messages, conversation.ToolStartAgent, index)
		if refused.ResultCode != toolfailure.InvalidArguments.String() || refused.Outcome != conversation.ToolOutcomeKnownFailure {
			t.Fatalf("the invalid %s was not refused: %+v", field, refused)
		}
		if len(assistant[index].Message.ToolCalls) != 1 {
			t.Fatalf("expected one invalid start for %s", field)
		}
		call := assistant[index].Message.ToolCalls[0]
		identity := agenttasks.SubagentIdentity{ProjectID: session.identity.ProjectID, AgentID: flowid.AgentTaskID(string(session.id), string(call.ID))}
		var missing *dex.FlowNotFoundError
		if _, err := session.subagents.SnapshotForTest(ctx, identity); !errors.As(err, &missing) {
			t.Fatalf("an invalid %s created a subagent Flow (%T)", field, err)
		}

		// A Session may already have its own workspace; none of these calls may
		// create its normal subagent worktree or a connector worktree there.
		if workspace.Resources.Instance.UID != "" {
			command := fmt.Sprintf("cd /workspace && test ! -e .git/superverse/agents/%[1]s && test ! -e .git/superverse/worktrees/%[1]s && test ! -e .git/superverse/connectors/worktrees/%[1]s && test ! -e .git/superverse/connectors/contributions/%[1]s.json && ! git show-ref --quiet --verify refs/superverse/agents/%[1]s/base && echo absent", identity.AgentID)
			output := objects.runInstance(ctx, workspace.Resources, "sh", "-c", command)
			if !strings.Contains(output, "absent") {
				t.Fatalf("an invalid %s left a subagent worktree: %s", field, output)
			}
		}

	}
	t.Logf("FlowID=%s refused description, slug and prompt before any record, worktree or subagent Flow", session.id)
}
