// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package modelprovider

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// These tests call the real Claude API with ANTHROPIC_API_KEY; a missing key
// fails them. They send the request shapes the Session Agent sends, built from
// entries through the same request assembly, so a broken append-only rule, a
// rejected parameter or a wrong profile fails here.

var liveTool = json.RawMessage(`{"name":"lookup_city","description":"Look up the country and population of one city.","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"],"additionalProperties":false}}`)

func liveClient(t *testing.T, key string) *claudeClient {
	t.Helper()
	if key == "" {
		t.Fatal("ANTHROPIC_API_KEY is required for the live Claude API tests")
	}
	client, err := newClaudeClient(NewProviderHTTPClient(), ClaudeClientConfig{APIKey: key}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// liveConversation is an append-only entry log replayed as one segment.
type liveConversation struct {
	model   conversation.Model
	effort  conversation.Effort
	entries []conversation.SequencedEntry
	anchor  conversation.Sequence
}

func newLiveConversation(model conversation.Model, request string) *liveConversation {
	live := &liveConversation{model: model, effort: ProfileOf(model).BaseEffort(conversation.EffortMedium)}
	live.append(conversation.SegmentStartEntry("<pending_request>\n"+request+"\n</pending_request>", ""))
	return live
}

func (live *liveConversation) append(entry conversation.Entry) {
	live.entries = append(live.entries, conversation.SequencedEntry{Sequence: conversation.Sequence(len(live.entries) + 1), Entry: entry})
}

func (live *liveConversation) request(transient ...conversation.Entry) conversation.ModelRequest {
	entries := live.entries
	for _, entry := range transient {
		entries = append(entries, conversation.SequencedEntry{Sequence: conversation.Sequence(len(entries) + 1), Entry: entry})
	}
	ignore := func(string) error { return nil }
	return conversation.ModelRequest{Model: live.model, Effort: live.effort,
		Prompts:        conversation.SegmentPrompts{System: conversation.SessionSystemPrompt, Tools: []json.RawMessage{liveTool}},
		Messages:       conversation.AssembleRequest(entries, live.anchor, ProfileOf(live.model).RequestShape()),
		PrefixMismatch: conversation.PrefixMismatchError, WriteAssistant: ignore, WriteReasoning: ignore,
		WriteActivity: func(conversation.AgentEvent) error { return nil }, ToolTarget: func(conversation.ToolCall) string { return "" }}
}

// record appends a reply and answers its tool calls, as the Flow does.
func (live *liveConversation) record(reply conversation.ModelReply) {
	live.anchor = conversation.Sequence(len(live.entries))
	live.append(conversation.ModelReplyEntry(reply, nil, time.Now()))
	for _, call := range reply.ToolCalls {
		var input struct {
			City string `json:"city"`
		}
		_ = json.Unmarshal([]byte(call.Arguments), &input)
		summary := conversation.ToolCallSummary{ID: call.ID, Name: call.Name}
		live.append(conversation.Entry{Role: conversation.EntryRoleUser, Kind: conversation.EntryKindToolResult, Display: conversation.EntryDisplayPublic,
			Blocks: []json.RawMessage{conversation.ToolResultBlock(call.ID, `{"city":"`+input.City+`","country":"known","population":1000000}`, false)},
			Meta:   conversation.EntryMeta{ToolCall: &summary}})
	}
}

func complete(t *testing.T, client *claudeClient, request conversation.ModelRequest) conversation.ModelReply {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	reply, err := client.Complete(ctx, request)
	if err != nil {
		t.Fatalf("%s rejected the request: %v", request.Model, err)
	}
	if transformations := reply.Metrics.InputTransformations; len(transformations) > 0 || reply.Metrics.DroppedThinkingBlocks > 0 {
		t.Fatalf("%s transformed the replayed input: %+v", request.Model, transformations)
	}
	return reply
}

// Every profile's request shape is accepted: system and effort entries where
// the profile sends them, thinking, fallbacks and the cache breakpoints.
func TestLiveClaudeModelProfiles(t *testing.T) {
	client := liveClient(t, os.Getenv("ANTHROPIC_API_KEY"))
	for _, model := range []conversation.Model{conversation.ModelOpus55, conversation.ModelSonnet55, conversation.ModelHaiku45} {
		live := newLiveConversation(model, "Reply with the single word OK.")
		live.append(conversation.UserMessageEntry("Reply with the single word OK.", nil))
		live.append(conversation.NoticeEntry(conversation.EntryRoleSystem, "Answer in English.", ""))
		if ProfileOf(model).Effort {
			live.append(conversation.EffortEntry(conversation.EffortHigh))
		}
		reply := complete(t, client, live.request())
		if reply.StopReason != conversation.StopReasonEndTurn || !strings.Contains(reply.Text, "OK") {
			t.Fatalf("%s answered %q with %s", model, reply.Text, reply.StopReason)
		}
		t.Logf("%s served by %s: %d blocks", model, reply.Metrics.ServedModel, len(reply.Blocks))
	}
}

// A streamed reply forwards its summarized reasoning and its text live, and
// the stored text is the streamed text.
func TestLiveClaudeStreamsSummarizedThinking(t *testing.T) {
	client := liveClient(t, os.Getenv("ANTHROPIC_API_KEY"))
	live := newLiveConversation(conversation.ModelOpus55, "Which is larger, 17^5 or 5^17? Reason it through, then answer in one sentence.")
	live.effort = conversation.EffortHigh
	live.append(conversation.UserMessageEntry("Which is larger, 17^5 or 5^17? Reason it through, then answer in one sentence.", nil))
	request := live.request()
	var text, reasoning strings.Builder
	request.WriteAssistant = func(chunk string) error { text.WriteString(chunk); return nil }
	request.WriteReasoning = func(chunk string) error { reasoning.WriteString(chunk); return nil }
	reply := complete(t, client, request)
	if reasoning.Len() == 0 || text.String() != reply.Text || reply.Metrics.ThinkingTokens == nil {
		t.Fatalf("reasoning %d bytes, streamed text %q, stored text %q", reasoning.Len(), text.String(), reply.Text)
	}
}

// A multi-turn tool loop replays every stored block unchanged with
// prefix_mismatch_behavior error: parallel tool calls, a system notice and a
// per-message effort between them, then a second user turn. No request is
// rejected or transformed, cache reads grow and count_tokens accepts the
// stored conversation.
func TestLiveClaudeAppendOnlyToolLoop(t *testing.T) {
	client := liveClient(t, os.Getenv("ANTHROPIC_API_KEY"))
	live := newLiveConversation(conversation.ModelOpus55, "Look up Paris and Tokyo.")
	live.append(conversation.UserMessageEntry("Use lookup_city for Paris and for Tokyo, both in one reply, then compare them in one sentence.", nil))
	var cacheRead int64
	calls := 0
	for turn, message := range []string{"", "Now look up Berlin the same way and add it to the comparison."} {
		if message != "" {
			live.append(conversation.UserMessageEntry(message, nil))
		}
		for {
			reply := complete(t, client, live.request())
			calls++
			if calls > 1 && (reply.Metrics.CacheReadInputTokens == nil || *reply.Metrics.CacheReadInputTokens < cacheRead || *reply.Metrics.CacheReadInputTokens == 0) {
				t.Fatalf("call %d read %v cached tokens after %d", calls, reply.Metrics.CacheReadInputTokens, cacheRead)
			}
			if reply.Metrics.CacheReadInputTokens != nil {
				cacheRead = *reply.Metrics.CacheReadInputTokens
			}
			live.record(reply)
			if len(reply.ToolCalls) == 0 {
				break
			}
			if turn == 0 && calls == 1 {
				live.append(conversation.NoticeEntry(conversation.EntryRoleSystem, "Keep the comparison under 40 words.", ""))
				live.append(conversation.EffortEntry(conversation.EffortHigh))
			}
			if calls > 6 {
				t.Fatal("the tool loop did not finish within six calls")
			}
		}
	}
	live.append(conversation.UserMessageEntry("Thanks.", nil))
	request := live.request()
	params := messageParams(ProfileOf(request.Model), request)
	countParams := anthropic.BetaMessageCountTokensParams{Model: params.Model, Messages: params.Messages, Thinking: params.Thinking,
		OutputConfig: params.OutputConfig, System: anthropic.BetaMessageCountTokensParamsSystemUnion{OfBetaTextBlockArray: params.System}, Betas: params.Betas}
	for _, tool := range request.Prompts.Tools {
		countParams.Tools = append(countParams.Tools, param.Override[anthropic.BetaMessageCountTokensParamsToolUnion](tool))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	count, err := client.client.Beta.Messages.CountTokens(ctx, countParams)
	if err != nil || count.InputTokens == 0 {
		t.Fatalf("count_tokens rejected the stored conversation: %v", err)
	}
	t.Logf("%d calls, last cache read %d tokens, replay counts %d input tokens", calls, cacheRead, count.InputTokens)
}

// A compaction call replays the segment with the compaction instruction and
// returns a summary inside <summary> tags; a tool call is retried with
// tool_choice none. As in the Flow, the compaction is decided at a boundary
// whose trailing gap holds a user entry: here the start of the next turn.
func TestLiveClaudeCompaction(t *testing.T) {
	client := liveClient(t, os.Getenv("ANTHROPIC_API_KEY"))
	live := newLiveConversation(conversation.ModelOpus55, "Look up Paris.")
	live.append(conversation.UserMessageEntry("Use lookup_city for Paris, then tell me its country.", nil))
	for range 3 {
		reply := complete(t, client, live.request())
		live.record(reply)
		if len(reply.ToolCalls) == 0 {
			break
		}
	}
	live.append(conversation.UserMessageEntry("Now look up Rome the same way.", nil))
	request := live.request(conversation.NoticeEntry(conversation.EntryRoleSystem, conversation.CompactionInstruction, ""))
	reply := complete(t, client, request)
	if len(reply.ToolCalls) > 0 {
		request.ToolChoiceNone = true
		reply = complete(t, client, request)
	}
	if !strings.Contains(reply.Text, "<summary>") || !strings.Contains(reply.Text, "Paris") {
		t.Fatalf("the compaction reply has no summary: %q", reply.Text)
	}
}

// A reply cut off at max_tokens is stored unchanged and the conversation
// continues after the continuation notice.
func TestLiveClaudeMaxTokensContinuation(t *testing.T) {
	client := liveClient(t, os.Getenv("ANTHROPIC_API_KEY"))
	live := newLiveConversation(conversation.ModelSonnet55, "Write a long story.")
	live.effort = conversation.EffortLow
	live.append(conversation.UserMessageEntry("Write a 3000-word story about a lighthouse keeper.", nil))
	request := live.request()
	params := messageParams(ProfileOf(request.Model), request)
	params.MaxTokens = 2048
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	reply, err := client.complete(ctx, params, request)
	if err != nil || reply.StopReason != conversation.StopReasonMaxTokens {
		t.Fatalf("the limited reply stopped with %q: %v", reply.StopReason, err)
	}
	live.record(reply)
	live.append(conversation.NoticeEntry(conversation.EntryRoleUser, conversation.OutputLimitContinuation, ""))
	params = messageParams(ProfileOf(request.Model), live.request())
	params.MaxTokens = 2048
	if _, err = client.complete(ctx, params, request); err != nil {
		t.Fatalf("the continuation after max_tokens was rejected: %v", err)
	}
}

// Requests the API refuses for good are rejections, not retryable errors: an
// unknown model (404) and an invalid key (401).
func TestLiveClaudeRejections(t *testing.T) {
	client := liveClient(t, os.Getenv("ANTHROPIC_API_KEY"))
	live := newLiveConversation(conversation.ModelHaiku45, "Reply OK.")
	live.append(conversation.UserMessageEntry("Reply OK.", nil))
	request := live.request()
	params := messageParams(ProfileOf(request.Model), request)
	params.Model = "claude-model-that-does-not-exist"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := client.complete(ctx, params, request)
	var rejected *conversation.ModelRejectedError
	if !errors.As(err, &rejected) || rejected.Status != 404 {
		t.Fatalf("an unknown model is not a 404 rejection: %v", err)
	}
	unauthorized := liveClient(t, "sk-ant-invalid-key")
	if _, err = unauthorized.Complete(ctx, request); !errors.As(err, &rejected) || rejected.Code != "authentication_error" || rejected.Status != 401 {
		t.Fatalf("an invalid key is not a 401 rejection: %v", err)
	}
}
