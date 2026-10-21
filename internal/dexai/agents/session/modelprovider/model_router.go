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
// client of its model, the mock model's to the in-process mock Claude API and
// every other one to the Claude API. One Session may switch between them; a
// segment stays on one model, so a reply never mixes the two.
type ModelRouter struct {
	// claude is nil when the server has no Claude key.
	claude *claudeClient
	// mock is nil when the server configuration does not offer the mock model.
	mock *claudeClient
}

// NewModelRouter builds the clients the server offers over one owned HTTP
// client: Claude when claude has a key, the mock model when mock is set.
func NewModelRouter(httpClient *http.Client, claude ClaudeClientConfig, mock *ClaudeClientConfig, logger *slog.Logger) (*ModelRouter, error) {
	router := &ModelRouter{}
	var err error
	if claude.APIKey != "" {
		if router.claude, err = newClaudeClient(httpClient, claude, logger); err != nil {
			return nil, err
		}
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
// admits only the models the server offers; a Session that selected one before
// the server stopped offering it, such as a Claude model before the key was
// taken out, gets a rejection that ends its turn.
func (router *ModelRouter) Complete(ctx context.Context, request conversation.ModelRequest) (conversation.ModelReply, error) {
	if availability := ProfileOf(request.Model).Availability; !availability.Selectable() {
		return conversation.ModelReply{}, (&conversation.ModelRejectedError{Code: "model_unavailable", Hint: availability.Reason()})
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
