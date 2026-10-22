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
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

func TestCompatibleOutputLimitSessionDexIntegration(t *testing.T) {
	for _, model := range []conversation.Model{conversation.ModelMockResponses, conversation.ModelMockChat} {
		t.Run(string(model), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
			defer cancel()
			session := newSession(t, ctx, model, mockInput(t, "compatible-output-limit", 1).Content)
			first := session.finishedTurn(ctx)
			requireNoFailure(t, first.History)
			entries := compatibleCanonicalEntries(ctx, t, session)
			var callID conversation.CallID
			for _, item := range entries {
				if item.Entry.Meta.ModelStopReason != conversation.StopReasonMaxTokens {
					continue
				}
				for _, raw := range item.Entry.Blocks {
					var block struct {
						Type  string              `json:"type"`
						ID    conversation.CallID `json:"id"`
						Input json.RawMessage     `json:"input"`
					}
					if json.Unmarshal(raw, &block) == nil && block.Type == conversation.BlockTypeToolUse.String() {
						if string(block.Input) != "{}" {
							t.Fatal("truncated call did not retain its empty canonical argument object")
						}
						callID = block.ID
					}
				}
			}
			answered := false
			for _, item := range entries {
				if item.Entry.Meta.ToolCall == nil || item.Entry.Meta.ToolCall.ID != callID {
					continue
				}
				for _, raw := range item.Entry.Blocks {
					var result struct {
						Type    string `json:"type"`
						IsError bool   `json:"is_error"`
					}
					if json.Unmarshal(raw, &result) == nil && result.Type == conversation.BlockTypeToolResult.String() && result.IsError && strings.Contains(string(raw), "output_limit_exceeded") {
						answered = true
					}
				}
			}
			if callID == "" || !answered || !strings.Contains(compatibleRecoveryJSON(t, first), "The output-limit continuation was accepted.") {
				t.Fatal("output limit lost canonical proposal/result or failed to continue")
			}
			session.send(ctx, mockInput(t, "compatible-output-limit", 2).Content)
			second := session.waitForSnapshot(ctx, "the next output-limit turn", func(snapshot agentloop.AgentSnapshot) bool {
				return endsWithReplyAfter(snapshot, first.Description.LastSequence)
			})
			requireNoFailure(t, second.History)
			if !strings.Contains(compatibleRecoveryJSON(t, second), "The next turn accepted the projected history.") {
				t.Fatal("next turn could not replay the output-limit history")
			}
		})
	}
}

func TestCompatibleInvalidArgumentsSessionDexIntegration(t *testing.T) {
	for _, model := range []conversation.Model{conversation.ModelMockResponses, conversation.ModelMockChat} {
		t.Run(string(model), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
			defer cancel()
			session := newSession(t, ctx, model, mockInput(t, "compatible-invalid-arguments", 1).Content)
			result := session.finishedTurn(ctx)
			requireNoFailure(t, result.History)
			if !strings.Contains(compatibleRecoveryJSON(t, result), "The invalid tool argument became a correctable result.") {
				t.Fatal("schema mistake did not continue through a correctable tool result")
			}
			found := false
			for _, entry := range compatibleCanonicalEntries(ctx, t, session) {
				found = found || entry.Entry.Meta.ToolCall != nil && strings.Contains(compatibleRecoveryJSON(t, entry), toolfailure.InvalidDurationSeconds.String())
			}
			if !found {
				t.Fatal("correctable schema result is absent from canonical history")
			}
		})
	}
}

func compatibleRecoveryJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
