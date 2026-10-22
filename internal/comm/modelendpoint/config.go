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
	"errors"
	"net/http"
)

// Config is non-secret endpoint policy; the server validates its local capability.
type Config struct {
	AllowLocalLoopback bool `yaml:"allowLocalLoopback"`
}

// Validate refuses the loopback capability outside the local lanes.
func (c Config) Validate(local bool) error {
	if c.AllowLocalLoopback && !local {
		return errors.New("agent.modelEndpoints.allowLocalLoopback requires a local lane")
	}
	return nil
}

// Pool owns one bounded transport shared by every admitted model origin.
type Pool struct{ owned *pool }

// NewPool consumes the server's validated configuration; it contains no keys.
func NewPool(config Config) *Pool {
	return &Pool{owned: (policy{allowLocalLoopback: config.AllowLocalLoopback}).newPool()}
}

// Client admits URL syntax and authority without DNS; every connection resolves and checks DNS.
func (p *Pool) Client(ctx context.Context, baseURL string) (*http.Client, error) {
	return p.owned.client(ctx, baseURL)
}

// Close releases idle connections; callers separately bound in-flight requests.
func (p *Pool) Close() error { p.owned.http.CloseIdleConnections(); return nil }
