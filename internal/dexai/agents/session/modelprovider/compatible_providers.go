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
	"log/slog"
	"slices"
	"strings"

	"github.com/superdurable/superversev2/internal/comm/modelendpoint"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

// CompatibleProviders is one replica-owned runtime shared by Session, child and
// credential admission. It holds bounded connections and no keys or Flow state.
type CompatibleProviders struct {
	config  ProviderConfig
	pool    *modelendpoint.Pool
	clients map[conversation.Provider]*openaicompat.Client
}

// NewCompatibleProviders consumes validated server policy and the owned local
// scenario endpoint. Every official or local origin uses the same DNS-safe pool.
func NewCompatibleProviders(ctx context.Context, config ProviderConfig, endpoints modelendpoint.Config, mockURL string, logger *slog.Logger) (*CompatibleProviders, error) {
	config.Enabled = slices.Clone(config.Enabled)
	result := &CompatibleProviders{config: config, pool: modelendpoint.NewPool(endpoints), clients: make(map[conversation.Provider]*openaicompat.Client, len(config.Enabled))}
	for _, model := range CompatibleModels() {
		if !config.Availability(model).Selectable() {
			continue
		}
		provider := model.Details().Provider
		baseURL := provider.BaseURL()
		if config.MockEndpoint {
			baseURL = strings.TrimRight(mockURL, "/") + "/v1"
		}
		client, err := result.pool.Client(ctx, baseURL)
		if err != nil {
			result.Close()
			return nil, err
		}
		profile := ProfileOf(model)
		adapter, err := openaicompat.New(client, baseURL, openaicompat.Policy{Provider: provider, WireMode: provider.APIStyle(), MaxTokens: profile.maxTokens, Images: profile.Capabilities.SupportsImages, Strict: profile.Capabilities.StrictSchema == strictEnabled, Effort: profile.Effort, Replay: profile.Capabilities.Replay, ChatThinking: profile.Capabilities.chatThinking}, config.MockEndpoint, logger)
		if err != nil {
			result.Close()
			return nil, err
		}
		result.clients[provider] = adapter
	}
	return result, nil
}

// availability is this replica's admission without changing catalog profile facts.
func (providers *CompatibleProviders) availability(model conversation.Model) ModelAvailability {
	return providers.config.Availability(model)
}

// Complete uses the Session's transient key and frozen provider selection.
func (providers *CompatibleProviders) Complete(ctx context.Context, request conversation.ModelRequest) (conversation.ModelReply, error) {
	if providers.availability(request.Model).Selectable() {
		if client := providers.clients[request.Model.Details().Provider]; client != nil {
			return client.Complete(ctx, request)
		}
	}
	return conversation.ModelReply{}, conversation.ModelUnavailable.Rejected()
}

// ValidateCredential admits a provider's reviewed default model before key storage.
func (providers *CompatibleProviders) ValidateCredential(ctx context.Context, provider conversation.Provider, key string) error {
	model, err := defaultModel(provider)
	if err != nil {
		return conversation.ModelUnavailable.Rejected()
	}
	if client := providers.clients[provider]; client != nil && providers.availability(model).Selectable() {
		return client.ValidateCredential(ctx, key, model)
	}
	return conversation.ModelUnavailable.Rejected()
}

// Close releases the one shared pool after all its Flow and API users stop.
func (providers *CompatibleProviders) Close() error { return providers.pool.Close() }
