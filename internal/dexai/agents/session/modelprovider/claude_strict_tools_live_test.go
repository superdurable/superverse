// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package modelprovider_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
)

// The frozen tool set of the Session Agent, strict tools with the schemas the
// API takes and the eager input streaming of write, edit and confirm_requirements, is accepted by
// every supported model, and a model that is asked for a tool call answers with
// an input that the tool's own decoder validates. The key is
// ANTHROPIC_API_KEY; a missing key fails the test.
func TestLiveClaudeStrictToolSet(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Fatal("ANTHROPIC_API_KEY is required for the live Claude API tests")
	}
	key = modelprovider.PersistLiveCredentialForTest(t, key)
	transport := modelprovider.NewProviderHTTPClient()
	t.Cleanup(transport.CloseIdleConnections)
	client, err := modelprovider.NewClaudeClientForTest(transport, modelprovider.ClaudeClientConfig{APIKey: key}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	const task = "Call the write tool once, with the path notes/strict-check.txt and the content hello. Reply with that tool call only."
	for _, model := range []conversation.Model{conversation.ModelOpus55, conversation.ModelSonnet55, conversation.ModelHaiku45} {
		profile := modelprovider.ProfileOf(model)
		prompt := agentloop.SessionPromptFor(model)
		entries := []conversation.SequencedEntry{
			{Sequence: 1, Entry: conversation.SegmentStartEntry("<pending_request>\n"+task+"\n</pending_request>", "")},
			{Sequence: 2, Entry: conversation.UserMessageEntry(task, nil)},
		}
		ignore := func(string) error { return nil }
		request := conversation.ModelRequest{Model: model, Effort: profile.BaseEffort(conversation.EffortMedium), Prompts: prompt.Prompts,
			Messages:       conversation.AssembleRequest(entries, 0, profile.RequestShape()),
			PrefixMismatch: conversation.PrefixMismatchError, WriteAssistant: ignore, WriteReasoning: ignore,
			WriteActivity: func(conversation.AgentEvent) error { return nil }, ToolTarget: agentloop.ToolTarget}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		reply, err := client.Complete(ctx, request)
		cancel()
		if err != nil {
			t.Fatalf("%s rejected the strict tool set of %d tools: %v", model, len(prompt.ToolNames), err)
		}
		var calls []string
		for _, call := range reply.ToolCalls {
			calls = append(calls, string(call.Name))
			if call.Name != conversation.ToolWrite {
				continue
			}
			if err = toolinput.DecodeInput(call.Name, call.Arguments.String()); err != nil {
				t.Fatalf("%s answered the write tool with an input that its decoder rejects: %v: %s", model, err, call.Arguments)
			}
		}
		if reply.StopReason != conversation.StopReasonToolUse || !strings.Contains(strings.Join(calls, " "), string(conversation.ToolWrite)) {
			t.Fatalf("%s did not call the write tool: stop %s, calls %v, text %q", model, reply.StopReason, calls, reply.Text)
		}
		t.Logf("%s accepted the %d strict and non-strict tools (%d bytes of tool definitions) and called %v", model, len(prompt.ToolNames), toolBytes(prompt.Prompts), calls)
	}
}

func toolBytes(prompts conversation.SegmentPrompts) int {
	total := 0
	for _, tool := range prompts.Tools {
		total += len(tool)
	}
	return total
}
