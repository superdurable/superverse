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
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"log/slog"
	"net/http"
)

// NewClaudeClientForTest is newClaudeClient for the tests.
func NewClaudeClientForTest(httpClient *http.Client, config ClaudeClientConfig, logger *slog.Logger) (*claudeClient, error) {
	return newClaudeClient(httpClient, config, logger)
}

func CompleteMessagesForTest(ctx context.Context, client *messagesClient, request conversation.ModelRequest, key, baseURL string) (conversation.ModelReply, error) {
	return client.complete(ctx, request, key, baseURL, true)
}

// MessagesClientForTest exposes the real adapter to external integration tests.
type MessagesClientForTest = messagesClient

func NewMessagesClientForTest(client *http.Client) *MessagesClientForTest {
	return newMessagesClient(client, slog.Default())
}
func CompleteMessagesKeyForTest(ctx context.Context, client *MessagesClientForTest, request conversation.ModelRequest, key string) (conversation.ModelReply, error) {
	return client.completeWithKey(ctx, request, key)
}
