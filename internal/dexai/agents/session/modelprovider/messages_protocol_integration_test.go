// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/superdurable/superversev2/internal/comm/modelendpoint"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
)

// This is model-wire evidence through real Dex; the Session/HTTP handoff case
// separately proves that the application produces this exact projected prefix.
func TestRemoteHandoffProjectionMessagesDexIntegration(t *testing.T) {
	baseURL, stop, err := mockmodel.StartForTest(t.Context(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	router, err := modelprovider.NewModelRouter(modelprovider.ClaudeClientConfig{}, &modelprovider.MockClientConfig{ClaudeClientConfig: modelprovider.ClaudeClientConfig{APIKey: mockmodel.APIKeyForTest, BaseURL: baseURL}}, slog.Default(), compatibleRuntime(t))
	if err != nil {
		t.Fatal(err)
	}
	harness := newMessagesWireHarness(t, nil, baseURL, router)
	request := messagesRequest(t, conversation.ModelMock, "remote-handoff")
	prelude, err := harness.complete(t, t.Context(), request, false)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(value any) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	quote := func(raw json.RawMessage) json.RawMessage {
		return encode(map[string]string{"type": "text", "text": conversation.RecordedToolHistoryPrefix + string(raw)})
	}
	call := mockmodel.RemoteReplyForTest("remote-handoff", 2, 1).ToolCalls[0]
	// Use the real producer's struct encoding; JSON member order differs from
	// the mock's map encoding but every identity and argument must match.
	nativeUse := remoteexec.ToolEntry([]conversation.ToolCall{call}, nil, "wire-fixture", time.Now()).Blocks[0]
	nativeResult := conversation.ToolResultBlock(call.ID, "Created raexec-handoff.txt", false)
	head := strings.Repeat("a", 40)
	now := time.Now().UTC()
	notice := (remoteexec.PlatformHandoff{From: now.Add(-time.Minute), To: now, Owners: []conversation.Actor{{Email: "fixture@example.test"}}, Reason: remoteexec.HandoffSwitched}).Notice(head)
	request.Messages = append(request.Messages,
		conversation.RequestMessage{Role: conversation.EntryRoleAssistant, Blocks: conversation.ModelReplyEntry(prelude, nil, now).WithoutProtocolHistory().Blocks, CacheAnchor: -1},
		conversation.RequestMessage{Role: conversation.EntryRoleUser, Blocks: conversation.UserMessageEntry(mockInput(t, "remote-handoff", 2).Content, nil).Blocks, CacheAnchor: -1},
		conversation.RequestMessage{Role: conversation.EntryRoleAssistant, Blocks: []json.RawMessage{quote(nativeUse)}, CacheAnchor: -1},
		conversation.RequestMessage{Role: conversation.EntryRoleUser, Blocks: []json.RawMessage{
			quote(nativeResult),
			conversation.UserMessageEntry(mockInput(t, "remote-handoff", 3).Content+" "+mockmodel.ArgumentMarkerForTest("head", head), nil).Blocks[0],
			conversation.NoticeEntry(conversation.EntryRoleUser, notice, "").Blocks[0],
		}, CacheAnchor: -1},
	)
	reply, err := harness.complete(t, t.Context(), request, false)
	if err != nil || len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Name != conversation.ToolRead {
		t.Fatalf("exact projected handoff rejected: %v", err)
	}
	t.Run("prior-platform-tool-unit", func(t *testing.T) {
		earlier := messagesRequest(t, conversation.ModelMock, "inspect-basic")
		original, err := harness.complete(t, t.Context(), earlier, false)
		if err != nil || len(original.ToolCalls) == 0 {
			t.Fatalf("original platform tool reply missing: %v", err)
		}
		results := []json.RawMessage{}
		for _, call := range original.ToolCalls {
			results = append(results, quote(conversation.ToolResultBlock(call.ID, "protocol result", false)))
		}
		prefix := append(conversation.UserMessageEntry("Retained public executor context.", nil).Blocks, earlier.Messages[0].Blocks...)
		changed := request
		changed.Messages = append([]conversation.RequestMessage{
			{Role: conversation.EntryRoleUser, Blocks: prefix, CacheAnchor: -1},
			{Role: conversation.EntryRoleAssistant, Blocks: conversation.ModelReplyEntry(original, nil, now).WithoutProtocolHistory().Blocks, CacheAnchor: -1},
			{Role: conversation.EntryRoleUser, Blocks: results, CacheAnchor: -1},
		}, request.Messages...)
		if _, err := harness.complete(t, t.Context(), changed, false); err != nil {
			t.Fatalf("original projected platform IDs changed with message grouping: %v", err)
		}
	})
	for name, mutate := range map[string]func(*conversation.ModelRequest){
		"missing-boundary": func(r *conversation.ModelRequest) { r.Messages[4].Blocks = r.Messages[4].Blocks[:2] },
		"private-history": func(r *conversation.ModelRequest) {
			r.Messages[1].Blocks = append(r.Messages[1].Blocks, prelude.Blocks[0])
		},
		"native-history": func(r *conversation.ModelRequest) { r.Messages[3].Blocks[0] = nativeUse },
		"changed-tool": func(r *conversation.ModelRequest) {
			r.Messages[3].Blocks[0] = quote(encode(map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": map[string]string{"path": "raexec-handoff.txt", "content": "corrupted"}}))
		},
		"duplicate-result": func(r *conversation.ModelRequest) {
			r.Messages[4].Blocks = append([]json.RawMessage{quote(nativeResult)}, r.Messages[4].Blocks...)
		},
		"missing-result": func(r *conversation.ModelRequest) { r.Messages[4].Blocks = r.Messages[4].Blocks[1:] },
	} {
		t.Run(name, func(t *testing.T) {
			changed := request
			changed.Messages = append([]conversation.RequestMessage(nil), request.Messages...)
			for index := range changed.Messages {
				changed.Messages[index].Blocks = append([]json.RawMessage(nil), changed.Messages[index].Blocks...)
			}
			mutate(&changed)
			_, err := harness.complete(t, t.Context(), changed, false)
			if rejected, ok := errors.AsType[*conversation.ModelRejectedError](err); !ok || rejected.Status != http.StatusBadRequest {
				t.Fatalf("corrupt projected handoff admitted: %v", err)
			}
		})
	}
	// The projection boundary cannot excuse a modified current signed reply.
	request.Messages = append(request.Messages,
		conversation.RequestMessage{Role: conversation.EntryRoleAssistant, Blocks: conversation.ModelReplyEntry(reply, nil, now).WithoutProtocolHistory().Blocks, CacheAnchor: -1},
		conversation.RequestMessage{Role: conversation.EntryRoleUser, Blocks: []json.RawMessage{conversation.ToolResultBlock(reply.ToolCalls[0].ID, "Remote handoff proof", false)}, CacheAnchor: -1},
	)
	if _, err := harness.complete(t, t.Context(), request, false); err != nil {
		if rejected, ok := errors.AsType[*conversation.ModelRejectedError](err); !ok || rejected.Status != http.StatusBadRequest {
			t.Fatalf("current replay corruption did not receive the exact rejection: %v", err)
		}
	} else {
		t.Fatal("current signed reply gained a handoff replay exemption")
	}
}

// Only the model is substituted. These are wire-protocol checks over its
// actual HTTP endpoint from a real Dex Worker. They do not claim Session/Sandbox or live-provider acceptance.
func TestCompatibleMessagesProtocolDexIntegration(t *testing.T) {
	requireMessagesKeyAdmission(t)
	baseURL, stop, err := mockmodel.StartForTest(t.Context(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	httpClient := modelprovider.NewProviderHTTPClientForTest()
	defer httpClient.CloseIdleConnections()
	client := modelprovider.NewMessagesClientForTest(httpClient)
	harness := newMessagesWireHarness(t, client, baseURL, nil)
	for _, model := range []conversation.Model{conversation.ModelDeepSeekFlash, conversation.ModelMiniMaxM3, conversation.ModelMuseWonder10} {
		t.Run(string(model), func(t *testing.T) {
			request := messagesRequest(t, model, "inspect-basic")
			for index := 0; index < 5; index++ {
				reply, err := harness.complete(t, t.Context(), request, false)
				if err != nil {
					t.Fatalf("reply %d: %v", index, err)
				}
				if index == 0 && len(reply.ToolCalls) != 2 {
					t.Fatal("parallel tool proposals lost")
				}
				if strings.Contains(string(joinBlocks(reply.Blocks)), "thinking\":") {
					t.Fatal("plaintext reasoning escaped")
				}
				if reply.Metrics.Model != model.ProviderModel() || reply.Metrics.InputTokens == nil {
					t.Fatal("model identity or usage lost")
				}
				if model == conversation.ModelMuseWonder10 && index == 0 {
					requireMessagesOpaqueReplay(t, harness, request, reply)
				}
				request.Messages = append(request.Messages, conversation.RequestMessage{Role: conversation.EntryRoleAssistant, Blocks: reply.Blocks, CacheAnchor: -1})
				if len(reply.ToolCalls) > 0 {
					blocks := []json.RawMessage{}
					for _, call := range reply.ToolCalls {
						blocks = append(blocks, conversation.ToolResultBlock(call.ID, "protocol result", false))
					}
					request.Messages = append(request.Messages, conversation.RequestMessage{Role: conversation.EntryRoleUser, Blocks: blocks, CacheAnchor: -1})
				}
			}
			request = messagesRequest(t, model, "model-failure")
			_, err = harness.complete(t, t.Context(), request, false)
			rejected, ok := errors.AsType[*conversation.ModelRejectedError](err)
			if !ok || rejected.Status != 400 || rejected.Detail != "" {
				t.Fatalf("unsafe 400 classification: %v", err)
			}
			if modelprovider.ProfileOf(model).Availability.Selectable() {
				t.Fatal("protocol checks enabled a candidate")
			}
		})
	}
}

// Mutations cross real Dex before admission rejects them; no malformed replay
// reaches the mock HTTP endpoint or becomes a proposed tool operation.
func requireMessagesOpaqueReplay(t *testing.T, harness *messagesWireHarness, request conversation.ModelRequest, reply conversation.ModelReply) {
	t.Helper()
	found := false
	for position, block := range reply.Blocks {
		if !conversation.IsOpaqueReplay(block) {
			continue
		}
		found = true
		replay, err := conversation.ReadOpaqueReplay(block, conversation.ProviderMeta, conversation.AnthropicMessages, position)
		if err != nil {
			t.Fatal("Meta reply did not use the shared canonical replay format")
		}
		for _, field := range []string{"provider", "wireMode", "position", "unknown"} {
			var fields map[string]any
			if err := json.Unmarshal(block, &fields); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "provider":
				fields[field] = conversation.ProviderOpenAI.String()
			case "wireMode":
				fields[field] = conversation.OpenAIResponses.String()
			case "position":
				fields[field] = position + 1
			default:
				fields[field] = "undocumented"
			}
			changed, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			blocks := append([]json.RawMessage(nil), reply.Blocks...)
			blocks[position] = changed
			invalid := request
			invalid.Messages = append(append([]conversation.RequestMessage(nil), request.Messages...), conversation.RequestMessage{Role: conversation.EntryRoleAssistant, Blocks: blocks, CacheAnchor: -1})
			if got, err := harness.complete(t, t.Context(), invalid, false); err == nil || len(got.ToolCalls) != 0 || len(got.Blocks) != 0 {
				t.Fatalf("invalid replay %s escaped admission", field)
			}
		}
		entry := conversation.ModelReplyEntry(reply, nil, time.Now())
		public, _ := entry.PublicMessage()
		encoded, err := json.Marshal(public)
		if err != nil || strings.Contains(string(encoded), replay.Ciphertext) {
			t.Fatal("encrypted replay escaped the public message projection")
		}
	}
	if !found {
		t.Fatal("Meta response omitted canonical opaque replay")
	}
}

func messagesRequest(t *testing.T, model conversation.Model, scenario string) conversation.ModelRequest {
	t.Helper()
	return messagesRequestInput(t, model, mockInput(t, scenario, 1).Content)
}

func messagesRequestInput(t *testing.T, model conversation.Model, input string) conversation.ModelRequest {
	t.Helper()
	// The mock validates that all Claude-only tool flags are removed.
	tools := []json.RawMessage{}
	for _, name := range []conversation.ToolName{conversation.ToolGrep, conversation.ToolGlob, conversation.ToolRead, conversation.ToolBash, conversation.ToolWriteTodos} {
		encoded, _ := json.Marshal(map[string]any{"name": name, "description": "Protocol tool", "input_schema": map[string]any{"type": "object"}, "strict": true, "eager_input_streaming": true})
		tools = append(tools, encoded)
	}
	return conversation.ModelRequest{Model: model, PrefixMismatch: conversation.PrefixMismatchError, Prompts: conversation.SegmentPrompts{System: "Protocol verification", Tools: tools},
		Messages: []conversation.RequestMessage{{Role: conversation.EntryRoleUser, Blocks: conversation.UserMessageEntry(input, nil).Blocks, CacheAnchor: 0}}}
}

func joinBlocks(blocks []json.RawMessage) []byte { value, _ := json.Marshal(blocks); return value }

func requireMessagesKeyAdmission(t *testing.T) {
	httpClient := modelprovider.NewProviderHTTPClientForTest()
	defer httpClient.CloseIdleConnections()
	client := modelprovider.NewMessagesClientForTest(httpClient)
	request := messagesRequest(t, conversation.ModelDeepSeekFlash, "inspect-basic")
	for _, key := range []string{"", "bad\nkey"} {
		_, err := modelprovider.CompleteMessagesKeyForTest(t.Context(), client, request, key)
		if _, ok := errors.AsType[*conversation.ModelRejectedError](err); !ok {
			t.Fatal("missing or malformed key admitted")
		}
	}
	request.Model = conversation.ModelMiniMaxM31Flash
	_, err := modelprovider.CompleteMessagesKeyForTest(t.Context(), client, request, mockmodel.APIKeyForTest)
	if _, ok := errors.AsType[*conversation.ModelRejectedError](err); !ok {
		t.Fatal("plaintext-required profile admitted")
	}
	// A missing mock key never falls back to the platform's Claude key.
	router, err := modelprovider.NewModelRouter(modelprovider.ClaudeClientConfig{}, nil, slog.Default(), compatibleRuntime(t))
	if err != nil {
		t.Fatal(err)
	}
	request.Model = conversation.ModelDeepSeekFlash
	_, err = router.Complete(t.Context(), request)
	if _, ok := errors.AsType[*conversation.ModelRejectedError](err); !ok {
		t.Fatal("candidate admission opened")
	}
}

func TestCompatibleMessagesStreamCancellationDexIntegration(t *testing.T) {
	baseURL, stop, err := mockmodel.StartForTest(t.Context(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	httpClient := modelprovider.NewProviderHTTPClientForTest()
	defer httpClient.CloseIdleConnections()
	client := modelprovider.NewMessagesClientForTest(httpClient)
	harness := newMessagesWireHarness(t, client, baseURL, nil)
	request := messagesRequest(t, conversation.ModelMiniMaxM3, "inspect-basic")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	reply, err := harness.complete(t, ctx, request, true)
	if !errors.Is(err, context.Canceled) || len(reply.ToolCalls) != 0 || len(reply.Blocks) != 0 {
		t.Fatalf("cancelled stream produced a committed reply: %v", err)
	}
}

func TestCompatibleMessagesProtocolFailuresDexIntegration(t *testing.T) {
	baseURL, stop, err := mockmodel.StartForTest(t.Context(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	httpClient := modelprovider.NewProviderHTTPClientForTest()
	defer httpClient.CloseIdleConnections()
	client := modelprovider.NewMessagesClientForTest(httpClient)
	harness := newMessagesWireHarness(t, client, baseURL, nil)
	for _, check := range []struct {
		input    mockmodel.Input
		model    conversation.Model
		rejected bool
		status   int
	}{
		{mockInput(t, "messages-plaintext", 1), conversation.ModelDeepSeekFlash, true, 0},
		{mockInput(t, "messages-plaintext", 1), conversation.ModelMiniMaxM3, true, 0},
		{mockInput(t, "messages-plaintext", 1), conversation.ModelMuseWonder10, true, 0},
		{mockInput(t, "messages-wrong-model", 1), conversation.ModelMuseWonder10, true, 0},
		{mockInput(t, "messages-missing-opaque", 1), conversation.ModelMuseWonder10, true, 0},
		{mockInput(t, "messages-duplicate-opaque", 1), conversation.ModelMuseWonder10, true, 0},
		{mockInput(t, "messages-oversize-opaque", 1), conversation.ModelMuseWonder10, true, 0},
		{mockInput(t, "messages-truncated", 1), conversation.ModelMuseWonder10, false, 0},
		{mockInput(t, "messages-credit-error", 1), conversation.ModelDeepSeekFlash, true, 402},
		{mockInput(t, "messages-billing-error", 1), conversation.ModelMiniMaxM3, true, 400},
		{mockInput(t, "messages-auth-error", 1), conversation.ModelDeepSeekFlash, true, 401},
		{mockInput(t, "messages-rate-limit", 1), conversation.ModelDeepSeekFlash, false, 429},
		{mockInput(t, "messages-server-error", 1), conversation.ModelMiniMaxM3, false, 503},
	} {
		t.Run(check.input.Label+"/"+string(check.model), func(t *testing.T) {
			request := messagesRequestInput(t, check.model, check.input.Content)
			reply, err := harness.complete(t, t.Context(), request, false)
			if err == nil || len(reply.Blocks) != 0 || len(reply.ToolCalls) != 0 {
				t.Fatal("invalid response escaped as a usable reply")
			}
			rejected, isRejected := errors.AsType[*conversation.ModelRejectedError](err)
			if isRejected != check.rejected {
				t.Fatalf("wrong error classification: %v", err)
			}
			if isRejected && (rejected.Hint == "" || rejected.Status != check.status) {
				t.Fatalf("wrong status: %v", err)
			}
			if isRejected && check.status == 402 && rejected.Code != conversation.ProviderCreditExhausted.Rejected(402).Code {
				t.Fatal("402 did not preserve the shared credit reason")
			}
			if isRejected && strings.Contains(check.input.Label, "billing") && rejected.Code != conversation.ProviderBillingProblem.Rejected(400).Code {
				t.Fatal("billing error did not preserve the shared billing reason")
			}
			if check.status == 429 || check.status == 503 {
				retry, ok := errors.AsType[*dex.RetryAfterError](err)
				if !ok || retry.After != 30*time.Second {
					t.Fatalf("provider backoff not delegated to Dex: %v", err)
				}
			}
			if strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("private provider data escaped")
			}
		})
	}
}

func TestCompatibleMessagesCompactionDexIntegration(t *testing.T) {
	baseURL, stop, err := mockmodel.StartForTest(t.Context(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	httpClient := modelprovider.NewProviderHTTPClientForTest()
	defer httpClient.CloseIdleConnections()
	client := modelprovider.NewMessagesClientForTest(httpClient)
	harness := newMessagesWireHarness(t, client, baseURL, nil)
	for _, model := range []conversation.Model{conversation.ModelDeepSeekFlash, conversation.ModelMiniMaxM3, conversation.ModelMuseWonder10} {
		t.Run(string(model), func(t *testing.T) {
			request := messagesRequest(t, model, "inspect-basic")
			reply, err := harness.complete(t, t.Context(), request, false)
			if err != nil {
				t.Fatal(err)
			}
			entry := conversation.ModelReplyEntry(reply, nil, time.Now())
			public := entry.WithoutOpaqueReplay()
			changed := len(public.Blocks) != len(entry.Blocks)
			if changed != (model == conversation.ModelMuseWonder10) {
				t.Fatal("wrong replay lifecycle policy")
			}
			if len(entry.Blocks) != len(reply.Blocks) {
				t.Fatal("compaction mutated the input reply")
			}
			request.Messages = append(request.Messages, conversation.RequestMessage{Role: conversation.EntryRoleAssistant, Blocks: public.Blocks, CacheAnchor: -1})
			results := []json.RawMessage{}
			for _, call := range reply.ToolCalls {
				results = append(results, conversation.ToolResultBlock(call.ID, "protocol result", false))
			}
			results = append(results, conversation.NoticeEntry(conversation.EntryRoleUser, conversation.CompactionInstruction, "").Blocks...)
			request.Messages = append(request.Messages, conversation.RequestMessage{Role: conversation.EntryRoleUser, Blocks: results, CacheAnchor: -1})
			summary, err := harness.complete(t, t.Context(), request, false)
			if err != nil || summary.Text == "" || len(summary.ToolCalls) != 0 {
				t.Fatalf("compaction failed: %v", err)
			}
			if strings.Contains(summary.Text, "synthetic") {
				t.Fatal("private replay entered summary")
			}
		})
	}
}

func TestMessagesLegacyClaudeReplayDexIntegration(t *testing.T) {
	baseURL, stop, err := mockmodel.StartForTest(t.Context(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	httpClient := modelprovider.NewProviderHTTPClientForTest()
	defer httpClient.CloseIdleConnections()
	router, err := modelprovider.NewModelRouter(modelprovider.ClaudeClientConfig{}, &modelprovider.MockClientConfig{ClaudeClientConfig: modelprovider.ClaudeClientConfig{APIKey: mockmodel.APIKeyForTest, BaseURL: baseURL}}, slog.Default(), compatibleRuntime(t))
	if err != nil {
		t.Fatal(err)
	}
	harness := newMessagesWireHarness(t, nil, baseURL, router)
	request := messagesRequest(t, conversation.ModelMock, "inspect-basic")
	reply, err := harness.complete(t, t.Context(), request, false)
	if err != nil {
		t.Fatal(err)
	}
	entry := conversation.ModelReplyEntry(reply, nil, time.Now())
	pruned := entry.WithoutOpaqueReplay()
	changed := len(pruned.Blocks) != len(entry.Blocks)
	if changed || string(joinBlocks(pruned.Blocks)) != string(joinBlocks(reply.Blocks)) {
		t.Fatal("legacy signed replay changed")
	}
	request.Messages = append(request.Messages, conversation.RequestMessage{Role: conversation.EntryRoleAssistant, Blocks: pruned.Blocks, CacheAnchor: -1})
	results := []json.RawMessage{}
	for _, call := range reply.ToolCalls {
		results = append(results, conversation.ToolResultBlock(call.ID, "protocol result", false))
	}
	request.Messages = append(request.Messages, conversation.RequestMessage{Role: conversation.EntryRoleUser, Blocks: results, CacheAnchor: -1})
	if _, err := harness.complete(t, t.Context(), request, false); err != nil {
		t.Fatalf("legacy replay rejected: %v", err)
	}
}

// An unavailable DNS zone must not prevent constructing the server's shared runtime.
func TestCompatibleProviderStartupDNSNetworkIntegration(t *testing.T) {
	runtime, err := modelprovider.NewCompatibleProviders(t.Context(), modelprovider.ProviderConfig{Enabled: []conversation.Provider{conversation.ProviderOpenAI}, MockEndpoint: true}, modelendpoint.Config{}, "https://provider.invalid", slog.Default(), "")
	if err != nil {
		t.Fatal("startup must admit syntax without DNS:", err)
	}
	defer runtime.Close()
	var dns *net.DNSError
	if err := runtime.ValidateCredential(t.Context(), conversation.ProviderOpenAI, mockmodel.APIKeyForTest); !errors.As(err, &dns) {
		t.Fatal("unresolvable provider call must fail at DNS dial:", err)
	}
}

func compatibleRuntime(t *testing.T) *modelprovider.CompatibleProviders {
	t.Helper()
	runtime, err := modelprovider.NewCompatibleProviders(t.Context(), modelprovider.ProviderConfig{}, modelendpoint.Config{AllowLocalLoopback: true}, "", slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	return runtime
}

// Both inference protocols validate the actual admission shape at their HTTP endpoint.
func TestCompatibleCredentialShapeNetworkIntegration(t *testing.T) {
	base, stop, err := mockmodel.StartForTest(t.Context(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	config := modelprovider.ProviderConfig{MockEndpoint: true}
	for _, model := range modelprovider.CompatibleModels() {
		config.Enabled = append(config.Enabled, model.Details().Provider)
	}
	runtime, err := modelprovider.NewCompatibleProviders(t.Context(), config, modelendpoint.Config{AllowLocalLoopback: true}, base, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	for _, model := range modelprovider.CompatibleModels() {
		if err := runtime.ValidateCredential(t.Context(), model.Details().Provider, mockmodel.APIKeyForTest); err != nil {
			t.Fatal(model, err)
		}
	}
	for _, body := range []string{
		`{"model":"gpt-6-astra","stream":false,"max_output_tokens":64,"input":"Reply OK."}`,
		`{"model":"gpt-6-astra","stream":false,"max_output_tokens":64,"input":"Reply OK.","store":true,"include":["reasoning.encrypted_content"]}`,
		`{"model":"gpt-6-astra","stream":false,"max_output_tokens":64,"input":"Reply OK.","store":false,"include":[]}`,
		`{"model":"gemini-4-argon","stream":false,"max_tokens":64,"messages":[]}`,
		`{"model":"gemini-4-argon","stream":false,"max_tokens":64,"messages":[{"role":"system","content":"Reply OK."}]}`,
	} {
		path := "/v1/responses"
		if strings.Contains(body, "gemini") {
			path = "/v1/chat/completions"
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+mockmodel.APIKeyForTest)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatal("malformed credential probe accepted", response.StatusCode)
		}
	}
}

// Hosted administrator mock routing grants no permission to custom endpoints.
func TestHostedBuiltinMockDexIntegration(t *testing.T) {
	mock, stop, err := mockmodel.StartBuiltin(t.Context(), slog.Default(), false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	runtime, err := modelprovider.NewCompatibleProviders(t.Context(), modelprovider.ProviderConfig{}, modelendpoint.Config{AllowLocalLoopback: false}, "", slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	router, err := modelprovider.NewModelRouter(modelprovider.ClaudeClientConfig{}, mock, slog.Default(), runtime)
	if err != nil {
		t.Fatal("hosted-style mock startup:", err)
	}
	harness := newMessagesWireHarness(t, nil, mock.BaseURL, router)
	if _, err := harness.complete(t, t.Context(), messagesRequest(t, conversation.ModelMock, "inspect-basic"), false); err != nil {
		t.Fatal("built-in mock:", err)
	}
	if _, err := runtime.CanonicalEndpoint(mock.BaseURL); err == nil {
		t.Fatal("custom URL inherited built-in loopback permission")
	}
	// The owned client is still authority-bound even when a request URL changes.
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:1/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := mock.HTTPClient.Do(request); err == nil {
		response.Body.Close()
		t.Fatal("built-in client admitted another origin")
	}
}
