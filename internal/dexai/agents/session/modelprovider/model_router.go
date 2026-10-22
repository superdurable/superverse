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

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// ModelRouter is the Flow's model client: it sends each request to the
// reviewed adapter for its admitted model. Claude uses the Project key and mock
// uses the in-process mock Claude API. A segment stays on one selected model.
type ModelRouter struct {
	// claude uses the key of each admitted Session request.
	claude *claudeClient
	// mock is nil when the server configuration does not offer the mock model.
	mock *claudeClient
}

// NewModelRouter builds the clients the server offers over one owned HTTP
// client: official Claude with per-request authentication, and the configured mock.
func NewModelRouter(httpClient *http.Client, claude ClaudeClientConfig, mock *ClaudeClientConfig, logger *slog.Logger) (*ModelRouter, error) {
	router := &ModelRouter{}
	var err error
	if router.claude, err = newClaudeClient(httpClient, ClaudeClientConfig{BaseURL: claude.BaseURL}, logger); err != nil {
		return nil, err
	}
	if mock != nil {
		if router.mock, err = newClaudeClient(httpClient, *mock, logger); err != nil {
			return nil, err
		}
		router.mock.readsMockProgress = true
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
	if availability := ProfileOf(request.Model).Availability; !availability.Selectable() {
		return conversation.ModelReply{}, (&conversation.ModelRejectedError{Code: "model_unavailable", Hint: availability.Reason()})
	}
	if request.Model != conversation.ModelMock && request.APIKey == "" {
		return conversation.ModelReply{}, conversation.NoClaudeKey.Rejected()
	}
	client := router.claude
	if request.Model == conversation.ModelMock {
		client = router.mock
	}
	switch {
	case client != nil:
		return client.Complete(ctx, request)
	case request.Model == conversation.ModelMock:
		return conversation.ModelReply{}, conversation.MockModelUnavailable.Rejected()
	default:
		return conversation.ModelReply{}, conversation.NoClaudeKey.Rejected()
	}
}
