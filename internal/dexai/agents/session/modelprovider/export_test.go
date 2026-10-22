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
	"errors"
	"github.com/superdurable/superversev2/internal/comm/modelendpoint"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"log/slog"
	"net"
	"net/http"
	"time"
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

// NewProviderHTTPClientForTest is the model provider transport. Redirects are refused
// so a credential never reaches another origin. It sets no overall timeout: a
// streamed reply can run for many minutes, and the Step's context bounds it.
func NewProviderHTTPClientForTest() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			MaxConnsPerHost:       64,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 2 * time.Minute,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("model provider redirects are disabled")
		},
	}
}

// ProviderPoolForTest exposes only transport occupancy to real-listener recovery tests.
func ProviderPoolForTest(providers *CompatibleProviders) *modelendpoint.Pool { return providers.pool }
