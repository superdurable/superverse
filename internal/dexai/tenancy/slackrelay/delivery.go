// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package slackrelay

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
)

// deliver POSTs one queued event to the trigger path of its scope's public
// route, signed with the scope's token, and drops it on any answer but 200
// or after the delivery timeout; nothing is retried.
func (r *Relay) deliver(ctx context.Context, d delivery) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, r.config.DeliveryTimeout)
	defer cancel()
	token, err := r.scopeToken(ctx, d.projectID, d.scope)
	if err != nil {
		r.record(d, FateTokenUnavailable, 0, time.Since(started))
		r.drop(ctx, dropTokenUnavailable, append(d.attributes(), slog.String("error", err.Error()))...)
		return
	}
	base, host := r.routes.PublicRoute(d.projectID, d.scope)
	target := strings.TrimRight(base, "/") + triggerPath(d.connectionName)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(d.body))
	if err != nil {
		panic(err)
	}
	request.Host = host
	signedAt := time.Now()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(projectconfig.RelayTimestampHeader, strconv.FormatInt(signedAt.Unix(), 10))
	request.Header.Set(projectconfig.RelaySignatureHeader, projectconfig.SignRelayedRequest(token, signedAt, d.body))
	response, err := r.client.Do(request)
	if err != nil {
		fate := FateNotRunning
		if errors.Is(err, context.DeadlineExceeded) {
			fate = FateTimedOut
		}
		r.record(d, fate, 0, time.Since(started))
		// The URL holds no secret; the scope's route and the transport's reason
		// say why the application did not answer.
		r.drop(ctx, dropDeliveryFailed, append(d.attributes(), slog.String("error", comm.TailUTF8(err.Error(), 512)))...)
		return
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	r.record(d, fateOfStatus(response.StatusCode), response.StatusCode, time.Since(started))
	if response.StatusCode != http.StatusOK {
		// A refused signature may follow a token rotation, so the next
		// delivery rereads the token.
		if response.StatusCode == http.StatusBadRequest {
			r.cache.forget(scopeKey(d.projectID, d.scope))
		}
		r.drop(ctx, dropDeliveryFailed, append(d.attributes(), slog.Int("status", response.StatusCode))...)
	}
}

// fateOfStatus is the fate of an event the application, or the gateway in
// front of it, answered with status.
func fateOfStatus(status int) EventFate {
	switch status {
	case http.StatusOK:
		return FateRecorded
	case http.StatusBadGateway, http.StatusGatewayTimeout:
		return FateNotRunning
	case http.StatusServiceUnavailable:
		return FateApplicationUnavailable
	}
	return FateRefused
}

// triggerPath is the path of the Slack trigger receiver of connectionName on
// a scope's route: the prefix and the name in unpadded base64url.
func triggerPath(connectionName string) string {
	return triggerPathPrefix + base64.RawURLEncoding.EncodeToString([]byte(connectionName))
}

// scopeToken returns the scope's token, read from its secret object and then
// kept in memory for tokenTTL.
func (r *Relay) scopeToken(ctx context.Context, projectID, scope string) (string, error) {
	key := scopeKey(projectID, scope)
	if token, found := r.cache.get(key, time.Now()); found {
		return token, nil
	}
	ref, err := r.dao.ScopeToken(ctx, projectID, scope)
	if err != nil {
		return "", err
	}
	token, err := r.tokens.ReadScopeToken(ctx, projectID, scope, ref)
	if err != nil {
		return "", err
	}
	r.cache.put(key, token, time.Now())
	return token, nil
}

func scopeKey(projectID, scope string) string { return projectID + "/" + scope }

// The scope token cache's bounds: a token is reread after tokenTTL, and at
// most tokenCacheSize scopes are kept, the oldest read dropped first.
const (
	tokenTTL       = 10 * time.Minute
	tokenCacheSize = 1024
)

// tokenCache keeps recently read scope tokens of this process, bounded in
// age and count; losing it costs only a reread.
type tokenCache struct {
	mutex   sync.Mutex
	entries map[string]cachedToken
}

type cachedToken struct {
	token  string
	readAt time.Time
}

func newTokenCache() *tokenCache { return &tokenCache{entries: map[string]cachedToken{}} }

func (c *tokenCache) get(key string, now time.Time) (string, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	entry, found := c.entries[key]
	if !found || now.Sub(entry.readAt) >= tokenTTL {
		delete(c.entries, key)
		return "", false
	}
	return entry.token, true
}

func (c *tokenCache) put(key, token string, now time.Time) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if _, found := c.entries[key]; !found && len(c.entries) >= tokenCacheSize {
		oldest := ""
		for candidate, entry := range c.entries {
			if oldest == "" || entry.readAt.Before(c.entries[oldest].readAt) {
				oldest = candidate
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = cachedToken{token: token, readAt: now}
}

func (c *tokenCache) forget(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.entries, key)
}
