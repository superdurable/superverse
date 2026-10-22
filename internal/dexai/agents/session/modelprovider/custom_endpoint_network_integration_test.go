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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/modelendpoint"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

// Only the approved mock model is substituted, over its actual HTTP listener.
func TestCustomEndpointDiscoveryNetworkIntegration(t *testing.T) {
	for _, test := range []struct {
		listen      string
		local, mock bool
	}{{"127.0.0.1:18083", false, true}, {"127.0.0.1:18083", true, false}, {"0.0.0.0:18083", true, true}, {"localhost:18083", true, true}, {"[::1]:18083", true, true}, {"127.0.0.1:0", true, true}} {
		if (modelprovider.ProviderConfig{MockListen: test.listen}).Validate(test.local, test.mock) == nil {
			t.Fatal("mock listener configuration escaped its local IPv4 loopback contract")
		}
	}
	if err := (modelprovider.ProviderConfig{MockListen: "127.0.0.1:18083"}).Validate(true, true); err != nil {
		t.Fatal("valid fixed local mock listener was refused")
	}
	base, _, stop, err := mockmodel.StartCustomEndpointProbeForTest(t.Context(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	config := modelprovider.ProviderConfig{Enabled: []conversation.Provider{conversation.ProviderAnthropic, conversation.ProviderOpenAI, conversation.ProviderXAI, conversation.ProviderGemini, conversation.ProviderQwen}, MockEndpoint: true, CustomEndpoints: true}
	if err := config.Validate(true, true); err != nil {
		t.Fatal(err)
	}
	runtime, err := modelprovider.NewCompatibleProviders(t.Context(), config, modelendpoint.Config{AllowLocalLoopback: true}, base, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	for _, test := range []struct {
		fixture string
		count   int
		result  modelendpoint.DiscoveryResult
		bounded bool
	}{
		{"empty", 0, modelendpoint.DiscoveryEmpty, false}, {"one", 1, modelendpoint.DiscoverySupported, false}, {"many", 2, modelendpoint.DiscoverySupported, false}, {"unsupported", 0, modelendpoint.DiscoveryUnsupported, false}, {"max-count", 100, modelendpoint.DiscoverySupported, false}, {"max-id", 1, modelendpoint.DiscoverySupported, false}, {"key", 0, "", true}, {"pagination", 0, "", true}, {"count", 0, "", true}, {"id", 0, "", true}, {"bytes", 0, "", true},
	} {
		t.Run(test.fixture, func(t *testing.T) {
			ids, result, err := runtime.Discover(t.Context(), conversation.ProviderQwen, base+"/custom/"+test.fixture+"/v1", mockmodel.APIKeyForTest)
			if test.bounded {
				if !errors.Is(err, modelendpoint.ErrDiscoveryBounds) {
					t.Fatalf("bounds were not refused: %T", err)
				}
				return
			}
			if err != nil || len(ids) != test.count || result != test.result {
				t.Fatalf("discovery result: count=%d result=%s error=%T", len(ids), result, err)
			}
		})
	}
	for _, code := range []int{401, 403, 404, 429, 500, 503} {
		endpoint, err := runtime.ResolveCustom(t.Context(), conversation.ProviderQwen, fmt.Sprintf("%s/custom/probe-%d/v1", base, code), mockmodel.CustomManualModelIDForTest, mockmodel.APIKeyForTest)
		if err != nil {
			t.Fatal(err)
		}
		err = runtime.ValidateCustom(t.Context(), conversation.ProviderQwen, endpoint, mockmodel.APIKeyForTest)
		var rejected *conversation.ModelRejectedError
		if code == 401 || code == 403 || code == 404 {
			want := conversation.InvalidProviderKey.Rejected().Code
			if code == 404 {
				want = conversation.ProviderModelNotFound.Rejected(code).Code
			}
			if !errors.As(err, &rejected) || rejected.Code != want || rejected.Status != code {
				t.Fatalf("incorrect probe rejection for %d: %T", code, err)
			}
		} else {
			probe, ok := errors.AsType[*openaicompat.CredentialProbeError](err)
			if !ok || probe.Reason != "provider_http_retryable" || probe.Status != code {
				t.Fatalf("probe status %d must remain typed and retryable: %T", code, err)
			}
		}
	}
	endpoint, err := runtime.ResolveCustom(t.Context(), conversation.ProviderQwen, base+"/custom/probe-timeout/v1", mockmodel.CustomManualModelIDForTest, mockmodel.APIKeyForTest)
	if err != nil {
		t.Fatal(err)
	}
	probeCtx, cancelProbe := context.WithTimeout(t.Context(), 100*time.Millisecond)
	probeErr := runtime.ValidateCustom(probeCtx, conversation.ProviderQwen, endpoint, mockmodel.APIKeyForTest)
	cancelProbe()
	probe, ok := errors.AsType[*openaicompat.CredentialProbeError](probeErr)
	if !ok || probe.Reason != "provider_transport_timeout" {
		t.Fatalf("probe timeout must retain only its closed reason: %T", probeErr)
	}
	for _, model := range []conversation.Model{conversation.ModelAnthropicCustom, conversation.ModelOpenAICustom, conversation.ModelXAICustom, conversation.ModelGeminiCustom, conversation.ModelQwenCustom} {
		t.Run(string(model), func(t *testing.T) {
			provider := model.Details().Provider
			endpoint, err := runtime.ResolveCustom(t.Context(), provider, base+"/custom/one/v1", "", mockmodel.APIKeyForTest)
			if err != nil || endpoint.EffectiveModelID != mockmodel.CustomDiscoveredModelIDForTest || endpoint.CustomModelID != "" || endpoint.CustomProfileVersion != 1 {
				t.Fatalf("single-model resolution: %T", err)
			}
			if err := runtime.ValidateCustom(t.Context(), provider, endpoint, mockmodel.APIKeyForTest); err != nil {
				t.Fatalf("custom probe failed: %T", err)
			}
			request := messagesRequest(t, model, "subagent-investigate")
			request.ToolTarget = func(conversation.ToolCall) string { return "" }
			request.WriteAssistant = func(string) error { return nil }
			request.WriteReasoning = func(string) error { t.Fatal("custom readable reasoning"); return nil }
			request.WriteActivity = func(conversation.AgentEvent) error { return nil }
			request.Prompts = agentloop.SessionPromptFor(model).Prompts
			request.APIKey, request.Endpoint = mockmodel.APIKeyForTest, endpoint
			reply, err := runtime.Complete(t.Context(), request)
			if err != nil || len(reply.ToolCalls) != 1 {
				t.Fatalf("custom inference: calls=%d error=%v", len(reply.ToolCalls), err)
			}
		})
	}
	for _, model := range []conversation.Model{conversation.ModelAnthropicCustom, conversation.ModelOpenAICustom, conversation.ModelQwenCustom} {
		faultBase, _, stop, err := mockmodel.StartCustomEndpointProbeForTest(t.Context(), slog.Default(), mockmodel.WireFaultPlaintextForTest)
		if err != nil {
			t.Fatal(err)
		}
		endpoint, err := runtime.ResolveCustom(t.Context(), model.Details().Provider, faultBase+"/custom/one/v1", mockmodel.CustomDiscoveredModelIDForTest, mockmodel.APIKeyForTest)
		if err != nil {
			t.Fatal(err)
		}
		request := messagesRequest(t, model, "subagent-investigate")
		request.Prompts = agentloop.SessionPromptFor(model).Prompts
		request.APIKey, request.Endpoint = mockmodel.APIKeyForTest, endpoint
		request.WriteAssistant = func(string) error { return nil }
		request.WriteReasoning = func(string) error { t.Fatal("custom endpoint exposed readable reasoning"); return nil }
		request.WriteActivity = func(conversation.AgentEvent) error { return nil }
		request.ToolTarget = func(conversation.ToolCall) string { return "" }
		reply, err := runtime.Complete(t.Context(), request)
		stop()
		if model == conversation.ModelAnthropicCustom {
			if err == nil || len(reply.ToolCalls) != 0 || len(reply.Blocks) != 0 {
				t.Fatal("malformed Messages reply was admitted")
			}
		} else if err != nil || len(reply.ToolCalls) != 1 || strings.Contains(reply.Text, "synthetic forbidden reasoning") {
			t.Fatalf("discarding custom reasoning lost valid output on %s: %v", model, err)
		}
	}
	for _, fixture := range []string{"empty", "many", "unsupported"} {
		if _, err := runtime.ResolveCustom(t.Context(), conversation.ProviderQwen, base+"/custom/"+fixture+"/v1", "", mockmodel.APIKeyForTest); err == nil {
			t.Fatal("ambiguous or absent discovery was silently selected")
		}
	}
	manual, err := runtime.ResolveCustom(t.Context(), conversation.ProviderQwen, base+"/custom/unsupported/v1", mockmodel.CustomManualModelIDForTest, mockmodel.APIKeyForTest)
	if err != nil || manual.EffectiveModelID != mockmodel.CustomManualModelIDForTest {
		t.Fatal("manual model ID requires discovery")
	}
	if err := runtime.ValidateCustom(t.Context(), conversation.ProviderQwen, manual, mockmodel.APIKeyForTest); err != nil {
		t.Fatal("manual custom endpoint probe failed")
	}
	for _, input := range []struct{ base, id string }{{base + "/custom/one/v1", mockmodel.APIKeyForTest}, {base + "/custom/" + mockmodel.APIKeyForTest + "/v1", mockmodel.CustomManualModelIDForTest}} {
		if _, err := runtime.ResolveCustom(t.Context(), conversation.ProviderQwen, input.base, input.id, mockmodel.APIKeyForTest); err == nil {
			t.Fatal("authentication material entered endpoint metadata")
		}
	}
	started := time.Now()
	if _, _, err := runtime.Discover(t.Context(), conversation.ProviderQwen, base+"/custom/timeout/v1", mockmodel.APIKeyForTest); err == nil || time.Since(started) > 12*time.Second {
		t.Fatal("discovery did not enforce its ten-second deadline")
	}
	for _, target := range []string{"https://127.0.0.1", "https://10.0.0.1/v1", "https://169.254.169.254/latest", "https://metadata.google.internal/v1"} {
		policy := modelendpoint.NewPool(modelendpoint.Config{})
		if _, err := policy.CanonicalBaseURL(target); err == nil {
			t.Fatal("prohibited target admitted")
		}
		policy.Close()
	}
}

// A single shared pool survives more requests than both admission and connection
// budgets, including failures and cancellation, while another stream stays open.
func TestCustomEndpointAdmissionRecoveryNetworkIntegration(t *testing.T) {
	base, _, stop, err := mockmodel.StartCustomEndpointProbeForTest(t.Context(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	config := modelprovider.ProviderConfig{Enabled: []conversation.Provider{conversation.ProviderQwen}, MockEndpoint: true, CustomEndpoints: true}
	runtime, err := modelprovider.NewCompatibleProviders(t.Context(), config, modelendpoint.Config{AllowLocalLoopback: true}, base, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	pool := modelprovider.ProviderPoolForTest(runtime)
	client, err := pool.Client(t.Context(), base)
	if err != nil {
		t.Fatal(err)
	}
	hold := func() (context.CancelFunc, *http.Response) {
		ctx, cancel := context.WithCancel(t.Context())
		call, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/custom/held-stream/v1/responses", nil)
		if err != nil {
			t.Fatal(err)
		}
		call.Header.Set("Authorization", "Bearer "+mockmodel.APIKeyForTest)
		response, err := client.Do(call)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			cancel()
			response.Body.Close()
			t.Fatal("held stream failed")
		}
		return cancel, response
	}
	cancelLong, long := hold()
	defer cancelLong()
	defer long.Body.Close()
	endpoint, err := runtime.ResolveCustom(t.Context(), conversation.ProviderQwen, base+"/custom/one/v1", mockmodel.CustomDiscoveredModelIDForTest, mockmodel.APIKeyForTest)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 160; i++ {
		ids, _, err := runtime.Discover(t.Context(), conversation.ProviderQwen, endpoint.BaseURL, mockmodel.APIKeyForTest)
		if err != nil || len(ids) != 1 {
			t.Fatalf("discovery %d failed: %T", i, err)
		}
		if _, _, err := runtime.Discover(t.Context(), conversation.ProviderQwen, base+"/custom/count/v1", mockmodel.APIKeyForTest); err == nil {
			t.Fatal("bounded discovery succeeded")
		}
		rejected := endpoint
		rejected.BaseURL = base + "/custom/probe-429/v1"
		if err := runtime.ValidateCustom(t.Context(), conversation.ProviderQwen, rejected, mockmodel.APIKeyForTest); err == nil {
			t.Fatal("failed probe succeeded")
		}
		if err := runtime.ValidateCustom(t.Context(), conversation.ProviderQwen, endpoint, mockmodel.APIKeyForTest); err != nil {
			t.Fatalf("probe %d failed: %T", i, err)
		}
		request := messagesRequest(t, conversation.ModelQwenCustom, "subagent-investigate")
		request.Prompts = agentloop.SessionPromptFor(request.Model).Prompts
		request.APIKey, request.Endpoint = mockmodel.APIKeyForTest, endpoint
		if _, err := runtime.Complete(t.Context(), request); err != nil {
			t.Fatalf("inference %d failed: %T", i, err)
		}
		cancel, response := hold()
		cancel()
		response.Body.Close()
	}
	if count := pool.OpenConnections(); count > 64 {
		t.Fatalf("pool accumulated %d connections", count)
	}
	if err := runtime.ValidateCustom(t.Context(), conversation.ProviderQwen, endpoint, mockmodel.APIKeyForTest); err != nil {
		t.Fatalf("final probe failed: %T", err)
	}
	t.Logf("160 mixed cycles, a held stream, cancellations and final probe passed; open connections=%d", pool.OpenConnections())
}
