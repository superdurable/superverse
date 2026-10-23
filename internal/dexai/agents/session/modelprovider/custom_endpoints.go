// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superdurable/superversev2/internal/comm/modelendpoint"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

// CustomModel is the only custom identity admitted for this credential namespace.
func customModel(provider conversation.Provider) (conversation.Model, error) {
	for _, model := range conversation.Models() {
		if model.IsCustom() && model.Details().Provider == provider {
			return model, nil
		}
	}
	return "", conversation.ModelUnavailable.Rejected()
}

// ResolveCustom admits canonical metadata without persisting transient credentials.
func (p *CompatibleProviders) ResolveCustom(ctx context.Context, provider conversation.Provider, base, id, key string) (conversation.EndpointSelection, error) {
	if key != "" && (strings.Contains(base, key) || strings.Contains(id, key)) {
		return conversation.EndpointSelection{}, errors.New("custom endpoint metadata must not contain authentication material")
	}
	model, err := customModel(provider)
	if err != nil || !p.availability(model).Selectable() {
		return conversation.EndpointSelection{}, conversation.ModelUnavailable.Rejected()
	}
	base, err = p.pool.CanonicalBaseURL(base)
	if err != nil {
		return conversation.EndpointSelection{}, err
	}
	effective := id
	if id == "" {
		ids, _, err := p.pool.Discover(ctx, base, key, provider.APIStyle() == conversation.AnthropicMessages)
		if err != nil {
			return conversation.EndpointSelection{}, err
		}
		if len(ids) != 1 {
			return conversation.EndpointSelection{}, errors.New("select or enter a model ID")
		}
		effective = ids[0]
	}
	if !modelendpoint.ValidModelID(effective) {
		return conversation.EndpointSelection{}, errors.New("model ID is invalid")
	}
	return conversation.EndpointSelection{BaseURL: base, CustomModelID: id, EffectiveModelID: effective, CustomProfileVersion: 1}, nil
}

// DiscoverForTest exercises identifier-only discovery through the configured runtime.
func (p *CompatibleProviders) DiscoverForTest(ctx context.Context, provider conversation.Provider, base, key string) ([]string, modelendpoint.DiscoveryResult, error) {
	model, err := customModel(provider)
	if err != nil || !p.availability(model).Selectable() {
		return nil, "", conversation.ModelUnavailable.Rejected()
	}
	return p.pool.Discover(ctx, base, key, provider.APIStyle() == conversation.AnthropicMessages)
}

// DiscoverModels includes only validated advisory capacities alongside model IDs.
func (p *CompatibleProviders) DiscoverModels(ctx context.Context, provider conversation.Provider, base, key string) ([]modelendpoint.DiscoveredModel, modelendpoint.DiscoveryResult, error) {
	model, err := customModel(provider)
	if err != nil || !p.availability(model).Selectable() {
		return nil, "", conversation.ModelUnavailable.Rejected()
	}
	models, result, _, err := p.pool.DiscoverModelsWithStatus(ctx, base, key, provider.APIStyle() == conversation.AnthropicMessages)
	return models, result, err
}

// CanonicalEndpoint validates input before any stored-key endpoint comparison.
func (p *CompatibleProviders) CanonicalEndpoint(base string) (string, error) {
	return p.pool.CanonicalBaseURL(base)
}

func (p *CompatibleProviders) completeCustom(ctx context.Context, request conversation.ModelRequest) (conversation.ModelReply, error) {
	if request.Endpoint.CustomProfileVersion != 1 || request.Endpoint.BaseURL == "" || !modelendpoint.ValidModelID(request.Endpoint.EffectiveModelID) {
		return conversation.ModelReply{}, conversation.ModelUnavailable.Rejected()
	}
	if _, err := ProfileFor(request.Model, request.Endpoint); err != nil {
		return conversation.ModelReply{}, conversation.ModelUnavailable.Rejected()
	}
	httpClient, err := p.pool.Client(ctx, request.Endpoint.BaseURL)
	if err != nil {
		return conversation.ModelReply{}, err
	}
	provider := request.Model.Details().Provider
	if provider.APIStyle() == conversation.AnthropicMessages {
		messages := *p.messages
		messages.httpClient = httpClient
		reply, err := messages.complete(ctx, request, request.APIKey, request.Endpoint.BaseURL, p.config.MockEndpoint)
		return customReply(reply, err)
	}
	profile, err := ProfileFor(request.Model, request.Endpoint)
	if err != nil {
		return conversation.ModelReply{}, conversation.ModelUnavailable.Rejected()
	}
	client, err := openaicompat.New(httpClient, request.Endpoint.BaseURL, openaicompat.Policy{Provider: provider, WireMode: provider.APIStyle(), MaxTokens: profile.maxTokens, Replay: conversation.ReplayNonThinking}, p.config.MockEndpoint, p.logger)
	if err != nil {
		return conversation.ModelReply{}, err
	}
	reply, err := client.Complete(ctx, request)
	return customReply(reply, err)
}

func customReply(reply conversation.ModelReply, err error) (conversation.ModelReply, error) {
	if err == nil && len(reply.ToolCalls) > 1 {
		return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
	}
	return reply, err
}

var customProbes = make(chan struct{}, 32)
var probeSequence atomic.Uint64

// ValidateCustom probes the exact effective selection with conservative common fields.
func (p *CompatibleProviders) ValidateCustom(ctx context.Context, provider conversation.Provider, endpoint conversation.EndpointSelection, key string) error {
	select {
	case customProbes <- struct{}{}:
		defer func() { <-customProbes }()
	default:
		return openaicompat.ErrModelCapacity
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	probeID := strconv.FormatUint(probeSequence.Add(1), 10)
	started := time.Now()
	if p.config.MockEndpoint {
		p.logger.InfoContext(ctx, "model credential probe started", "probe_id", probeID, "open_connections", p.pool.OpenConnections(), "active_probes", len(customProbes))
	}
	defer func() {
		if p.config.MockEndpoint {
			p.logger.InfoContext(ctx, "model credential probe finished", "probe_id", probeID, "elapsed_ms", time.Since(started).Milliseconds(), "open_connections", p.pool.OpenConnections())
		}
	}()
	err := p.validateCustomOnce(ctx, provider, endpoint, key, probeID)
	if probe, ok := errors.AsType[*openaicompat.CredentialProbeError](err); ok && probe.Status == http.StatusTooManyRequests {
		remaining, _ := ctx.Deadline()
		wait := probe.RetryAfter
		// Honor Retry-After rather than retrying early; no wait may exceed five seconds.
		if wait <= 5*time.Second && wait < time.Until(remaining) {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-timer.C:
				return p.validateCustomOnce(ctx, provider, endpoint, key, probeID)
			case <-ctx.Done():
				return openaicompat.CredentialProbeTransportFailure(ctx.Err())
			}
		}
	}
	return err
}

func (p *CompatibleProviders) validateCustomOnce(ctx context.Context, provider conversation.Provider, endpoint conversation.EndpointSelection, key, probeID string) error {
	if provider.APIStyle() == conversation.OpenAIChatCompletions {
		ids, result, status, err := p.pool.DiscoverWithStatus(ctx, endpoint.BaseURL, key, false)
		if status != 0 && status != http.StatusOK && status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			failure := openaicompat.CredentialProbeFailure(status)
			if limited, ok := errors.AsType[*modelendpoint.ProbeRateLimit](err); ok {
				if probe, ok := errors.AsType[*openaicompat.CredentialProbeError](failure); ok {
					probe.RetryAfter = limited.RetryAfter
				}
			}
			return failure
		}
		if err != nil {
			if errors.Is(err, modelendpoint.ErrDiscoveryBounds) {
				return conversation.ProviderProtocolRejected.Rejected(status)
			}
			return openaicompat.CredentialProbeTransportFailure(err)
		}
		if result != modelendpoint.DiscoveryUnsupported {
			for _, id := range ids {
				if id == endpoint.EffectiveModelID {
					return nil
				}
			}
			return conversation.ProviderModelNotFound.Rejected(status)
		}
	}
	client, err := p.pool.Client(ctx, endpoint.BaseURL)
	if err != nil {
		return err
	}
	body := map[string]any{"model": endpoint.EffectiveModelID, "stream": false, "max_tokens": 64}
	path := "/chat/completions"
	switch provider.APIStyle() {
	case conversation.AnthropicMessages:
		path = "/messages"
		body["messages"] = []any{map[string]string{"role": "user", "content": "Reply OK."}}
	case conversation.OpenAIResponses:
		path = "/responses"
		delete(body, "max_tokens")
		body["max_output_tokens"], body["store"], body["input"] = 64, false, "Reply OK."
	default:
		body["messages"] = []any{map[string]string{"role": "user", "content": "Reply OK."}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	call, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return errors.New("construct model credential probe")
	}
	call.Header.Set("Content-Type", "application/json")
	call.Header.Set("Authorization", "Bearer "+key)
	if provider.APIStyle() == conversation.AnthropicMessages {
		call.Header.Set("x-api-key", key)
		call.Header.Set("anthropic-version", "2023-06-01")
	}
	if p.config.MockEndpoint {
		call.Header.Set(openaicompat.MockProviderHeader, provider.String())
		call.Header.Set("Superverse-Mock-Probe", probeID)
	}
	response, err := client.Do(call)
	if err != nil {
		failure := openaicompat.CredentialProbeTransportFailure(err)
		if p.config.MockEndpoint {
			p.logger.WarnContext(ctx, "model credential probe failed", "probe_id", probeID, "reason", failure.Error(), "open_connections", p.pool.OpenConnections(), "active_probes", len(customProbes))
		}
		return failure
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		failure := openaicompat.CredentialProbeFailure(response.StatusCode)
		if probe, ok := errors.AsType[*openaicompat.CredentialProbeError](failure); ok {
			probe.RetryAfter = modelendpoint.ProbeRetryAfter(response.Header.Get("Retry-After"))
		}
		return failure
	}
	size, err := io.Copy(io.Discard, io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		failure := openaicompat.CredentialProbeTransportFailure(err)
		if p.config.MockEndpoint {
			p.logger.WarnContext(ctx, "model credential probe failed", "probe_id", probeID, "reason", failure.Error(), "open_connections", p.pool.OpenConnections(), "active_probes", len(customProbes))
		}
		return failure
	}
	if size > 1<<20 {
		return conversation.ProviderProtocolRejected.Rejected(response.StatusCode)
	}
	return nil
}
