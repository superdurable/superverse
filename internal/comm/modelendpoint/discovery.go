// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelendpoint

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// DiscoveryResult is the closed outcome of a bounded model list request.
type DiscoveryResult string

const (
	DiscoverySupported   DiscoveryResult = "supported"
	DiscoveryUnsupported DiscoveryResult = "unsupported"
	DiscoveryEmpty       DiscoveryResult = "empty"
)

func (result DiscoveryResult) String() string { return string(result) }

const maximumModelIDBytes = 256
const maximumDiscoveryBytes = 1 << 20

var discoverySlots = make(chan struct{}, 32)
var errDiscoveryCapacity = errors.New("model discovery capacity is exhausted")

var errDiscovery = errors.New("model discovery did not complete")
var ErrDiscoveryBounds = errors.New("model discovery exceeded its limits")

// ValidModelID bounds an opaque JSON field; IDs never become paths or labels.
func ValidModelID(value string) bool {
	if value == "" || len(value) > maximumModelIDBytes || strings.TrimSpace(value) != value {
		return false
	}
	for _, c := range value {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

// Discover reads IDs only, never pagination URLs or provider error bodies.
func (p *Pool) Discover(ctx context.Context, base, key string, messages bool) ([]string, DiscoveryResult, error) {
	ids, result, _, err := p.DiscoverWithStatus(ctx, base, key, messages)
	return ids, result, err
}

// DiscoveredModel contains only a bounded identifier and an advisory numeric capacity.
type DiscoveredModel struct {
	ID            string
	ContextWindow int64
}

// DiscoverWithStatus preserves the identifier-only API for existing consumers.
func (p *Pool) DiscoverWithStatus(ctx context.Context, base, key string, messages bool) ([]string, DiscoveryResult, int, error) {
	models, result, status, err := p.DiscoverModelsWithStatus(ctx, base, key, messages)
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids, result, status, err
}

// DiscoverModelsWithStatus admits bounded advisory hints without returning provider objects.
func (p *Pool) DiscoverModelsWithStatus(ctx context.Context, base, key string, messages bool) ([]DiscoveredModel, DiscoveryResult, int, error) {
	httpStatus := 0
	select {
	case discoverySlots <- struct{}{}:
		defer func() { <-discoverySlots }()
	default:
		return nil, "", httpStatus, errDiscoveryCapacity
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	canonical, err := p.CanonicalBaseURL(base)
	if err != nil {
		return nil, "", httpStatus, err
	}
	client, err := p.Client(ctx, canonical)
	if err != nil {
		return nil, "", httpStatus, err
	}
	call, err := http.NewRequestWithContext(ctx, http.MethodGet, canonical+"/models", nil)
	if err != nil {
		return nil, "", httpStatus, errDiscovery
	}
	call.Header.Set("Authorization", "Bearer "+key)
	if messages {
		call.Header.Set("x-api-key", key)
		call.Header.Set("anthropic-version", "2023-06-01")
	}
	response, err := client.Do(call)
	if err != nil {
		return nil, "", httpStatus, discoveryTransportError(err)
	}
	defer response.Body.Close()
	httpStatus = response.StatusCode
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented {
		return nil, DiscoveryUnsupported, httpStatus, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", httpStatus, errDiscovery
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumDiscoveryBytes+1))
	if err != nil {
		return nil, "", httpStatus, discoveryTransportError(err)
	}
	if len(raw) > maximumDiscoveryBytes {
		return nil, "", httpStatus, ErrDiscoveryBounds
	}
	var body struct {
		Data []struct {
			ID            string          `json:"id"`
			ContextLength json.RawMessage `json:"context_length"`
			ContextWindow json.RawMessage `json:"context_window"`
			MaxModelLen   json.RawMessage `json:"max_model_len"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil, "", httpStatus, errDiscovery
	}
	if len(body.Data) > 100 || body.HasMore {
		return nil, "", httpStatus, ErrDiscoveryBounds
	}
	models := make([]DiscoveredModel, 0, len(body.Data))
	seen := make(map[string]int, len(body.Data))
	for _, model := range body.Data {
		if !ValidModelID(model.ID) || strings.Contains(model.ID, key) || strings.Contains(model.ID, canonical) {
			return nil, "", httpStatus, ErrDiscoveryBounds
		}
		hint := discoveryContextHint(model.ContextLength, model.ContextWindow, model.MaxModelLen)
		if index, exists := seen[model.ID]; exists {
			if models[index].ContextWindow != hint {
				models[index].ContextWindow = 0
			}
		} else {
			seen[model.ID] = len(models)
			models = append(models, DiscoveredModel{ID: model.ID, ContextWindow: hint})
		}
	}
	if len(models) == 0 {
		return models, DiscoveryEmpty, httpStatus, nil
	}
	return models, DiscoverySupported, httpStatus, nil
}

// Keep transport classification without retaining request URLs or credential-bearing errors.
func discoveryTransportError(err error) error {
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return errDiscovery
}

// Hints are advisory: malformed values never invalidate an otherwise bounded ID.
func discoveryContextHint(values ...json.RawMessage) int64 {
	var hint int64
	for _, raw := range values {
		var value int64
		if json.Unmarshal(raw, &value) != nil || value < 32768 || value > 1_000_000 {
			continue
		}
		if hint != 0 && hint != value {
			return 0
		}
		hint = value
	}
	return hint
}
