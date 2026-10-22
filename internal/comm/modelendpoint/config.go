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
	"net"
	"net/http"
	"strings"
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

// CanonicalBaseURL validates user input without performing an outbound request.
func (p *Pool) CanonicalBaseURL(raw string) (string, error) {
	base, err := p.owned.policy.validate(raw)
	if err != nil {
		return "", err
	}
	base.Scheme = strings.ToLower(base.Scheme)
	host := strings.ToLower(strings.TrimSuffix(base.Hostname(), "."))
	port := base.Port()
	if port == "443" && base.Scheme == "https" || port == "80" && base.Scheme == "http" {
		port = ""
	}
	if port != "" {
		base.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		base.Host = "[" + host + "]"
	} else {
		base.Host = host
	}
	return strings.TrimRight(base.String(), "/"), nil
}

// OpenConnections is bounded per-replica transport occupancy, including idle connections.
func (p *Pool) OpenConnections() int { return len(p.owned.slots) }
