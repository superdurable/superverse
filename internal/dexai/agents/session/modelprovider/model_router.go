// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

// ModelRouter is the Flow's model client: it sends each request to the
// reviewed adapter for its admitted model. Claude uses the Project key and mock
// uses the in-process mock Claude API. A segment stays on one selected model.
type ModelRouter struct {
	// claude uses the key of each admitted Session request.
	claude   *claudeClient
	messages *messagesClient
	// mock is nil when the server configuration does not offer the mock model.
	mock       map[conversation.APIStyle]conversation.ModelClient
	compatible *CompatibleProviders
}

// MockClientConfig keeps internal wire fixtures confined to the local lanes.
// Hosted administrator mock Sessions retain only the published Anthropic mock.
type MockClientConfig struct {
	ClaudeClientConfig
	CompatibleTransports bool
	// HTTPClient is supplied only by the owner of the built-in mock listener.
	// It is authority-bound to that listener and never serves custom models.
	HTTPClient *http.Client `yaml:"-"`
}

// NewModelRouter builds the clients the server offers over one owned HTTP
// client: official Claude with per-request authentication, and the configured mock.
func NewModelRouter(claude ClaudeClientConfig, mock *MockClientConfig, logger *slog.Logger, compatible *CompatibleProviders) (*ModelRouter, error) {
	router := &ModelRouter{compatible: compatible}
	base := claude.BaseURL
	if base == "" {
		base = conversation.ProviderAnthropic.BaseURL()
	}
	httpClient, err := compatible.pool.Client(context.Background(), base)
	if err != nil {
		return nil, err
	}
	router.messages = newMessagesClient(httpClient, logger)
	if router.claude, err = newClaudeClient(httpClient, ClaudeClientConfig{BaseURL: claude.BaseURL}, logger); err != nil {
		return nil, err
	}
	if mock != nil {
		mockHTTP := mock.HTTPClient
		if mockHTTP == nil {
			mockHTTP, err = compatible.pool.Client(context.Background(), mock.BaseURL)
			if err != nil {
				return nil, err
			}
		}
		messages, err := newClaudeClient(mockHTTP, mock.ClaudeClientConfig, logger)
		if err != nil {
			return nil, err
		}
		messages.readsMockProgress = true
		router.mock = map[conversation.APIStyle]conversation.ModelClient{conversation.AnthropicMessages: messages}
		if !mock.CompatibleTransports {
			return router, nil
		}
		for _, model := range []conversation.Model{conversation.ModelMockResponses, conversation.ModelMockChat} {
			profile := ProfileOf(model)
			policy := openaicompat.Policy{Provider: model.Details().Provider, WireMode: model.Details().Provider.APIStyle(), MaxTokens: profile.maxTokens,
				Images: profile.Capabilities.SupportsImages, Strict: profile.Capabilities.StrictSchema == strictEnabled,
				Effort: profile.Effort, Replay: profile.Capabilities.Replay, ChatThinking: profile.Capabilities.chatThinking}
			client, err := openaicompat.New(mockHTTP, strings.TrimRight(mock.BaseURL, "/")+"/v1", policy, true, logger)
			if err != nil {
				return nil, err
			}
			router.mock[policy.WireMode] = client
		}
	}
	return router, nil
}

// MockProgressHeader carries the conversation.MockProgress of a reply as
// JSON: the mock model sets it on the response of every reply of a scenario.
const MockProgressHeader = "Superverse-Mock-Progress"

// mockProgress is the progress the mock model reported with a reply, or nil
// when it reported none, as for a conversation that started no scenario.
func mockProgress(response *http.Response) *conversation.MockProgress {
	value := response.Header.Get(MockProgressHeader)
	if value == "" {
		return nil
	}
	var progress conversation.MockProgress
	// The in-process mock model wrote it; any other value is a defect.
	comm.MustDo(json.Unmarshal([]byte(value), &progress))
	return &progress
}

// Complete streams the reply of the request's model from its client. The API
// admits only available catalog models. A stored unavailable model or a Claude
// request without its Project key gets a rejection that ends its turn.
func (router *ModelRouter) Complete(ctx context.Context, request conversation.ModelRequest) (conversation.ModelReply, error) {
	if availability := router.compatible.availability(request.Model); !availability.Selectable() {
		rejection := conversation.ModelUnavailable.Rejected()
		rejection.Hint = availability.Reason()
		return conversation.ModelReply{}, rejection
	}
	if request.Model.IsCustom() {
		return router.compatible.Complete(ctx, request)
	}
	style := request.Model.Details().Provider.APIStyle()
	if request.Model.IsMock() {
		if client := router.mock[style]; client != nil {
			return client.Complete(ctx, request)
		}
		return conversation.ModelReply{}, conversation.MockModelUnavailable.Rejected()
	}
	switch style {
	case conversation.AnthropicMessages:
		if request.APIKey == "" {
			return conversation.ModelReply{}, conversation.NoProviderKey.Rejected()
		}
		if request.Model.Details().Provider == conversation.ProviderAnthropic && router.claude != nil {
			return router.claude.Complete(ctx, request)
		}
		// BYOK will supply a request-local key here; no platform key is borrowed.
		return router.messages.completeWithKey(ctx, request, "")
	case conversation.OpenAIResponses, conversation.OpenAIChatCompletions:
		return router.compatible.Complete(ctx, request)
	default:
		return conversation.ModelReply{}, conversation.ModelUnavailable.Rejected()
	}
}
