// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// ValidateCredential tests inference admission with at most 64 output tokens.
// No provider body or key is retained in a diagnostic or persisted as history.
func (client *Client) ValidateCredential(ctx context.Context, key string, model conversation.Model) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if model.Details().Provider != client.policy.Provider || key == "" {
		return conversation.InvalidProviderKey.Rejected()
	}
	for _, character := range key {
		if character < 0x21 || character > 0x7e {
			return conversation.InvalidProviderKey.Rejected()
		}
	}
	body := map[string]any{"model": model.ProviderModel(), "stream": false}
	if client.policy.WireMode == conversation.OpenAIResponses {
		body["input"] = "Reply OK."
		body["max_output_tokens"] = 64
		body["store"] = false
		body["include"] = []string{"reasoning.encrypted_content"}
	} else {
		body["messages"] = []any{map[string]string{"role": "user", "content": "Reply OK."}}
		body["max_tokens"] = 64
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return errors.New("encode credential admission request")
	}
	call, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+client.endpointPath(), bytes.NewReader(raw))
	if err != nil {
		return errors.New("construct credential admission request")
	}
	call.Header.Set("Content-Type", "application/json")
	call.Header.Set("Authorization", "Bearer "+key)
	if client.localMock {
		call.Header.Set(MockProviderHeader, client.policy.Provider.String())
	}
	select {
	case compatibleCalls <- struct{}{}:
		defer func() { <-compatibleCalls }()
	default:
		return ErrModelCapacity
	}
	response, err := client.http.Do(call)
	if err != nil {
		return &transportError{cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return httpFailure(response)
	}
	if size, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, (1<<20)+1)); readErr != nil {
		return &transportError{cause: readErr}
	} else if size > 1<<20 {
		return errors.New("credential admission response exceeded its limit")
	}
	return nil
}
