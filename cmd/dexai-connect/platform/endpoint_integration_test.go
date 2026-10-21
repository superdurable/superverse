// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

// Both cases use the machine's real DNS resolver and HTTP transport. Reserved
// .invalid names are genuinely absent; no scripted resolver, transport or error.
func TestBridgeEndpointDNSIntegration(t *testing.T) {
	name := fmt.Sprintf("dexai-%d.invalid", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Run("endpoint-name-missing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		client, err := New("https://"+name, "unused-dns-test", bridgeapi.BridgeInfo{Version: bridgeapi.DevBridgeVersion}, logger)
		if err != nil {
			t.Fatal(err)
		}
		transport := &http.Transport{}
		defer transport.CloseIdleConnections()
		client.http.Transport = transport
		_, err = client.Sync(ctx, 1, 0, "", false)
		if !errors.Is(err, ErrEndpointGone) {
			t.Fatalf("real absent endpoint DNS: %v", err)
		}
	})
	t.Run("proxy-name-missing-retains-endpoint", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		client, err := New("http://127.0.0.1:1", "unused-dns-test", bridgeapi.BridgeInfo{Version: bridgeapi.DevBridgeVersion}, logger)
		if err != nil {
			t.Fatal(err)
		}
		proxy, err := url.Parse("http://proxy-" + name + ":8080")
		if err != nil {
			t.Fatal(err)
		}
		transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
		defer transport.CloseIdleConnections()
		client.http.Transport = transport
		_, err = client.Sync(ctx, 1, 0, "", false)
		if err == nil || errors.Is(err, ErrEndpointGone) {
			t.Fatalf("a missing real proxy must not forget the endpoint: %v", err)
		}
	})
}
