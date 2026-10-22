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
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// policy is deliberately not yet exported: custom endpoint admission is a later slice.
// Its local exception must come from the validated server configuration when wired.
type policy struct {
	allowLocalLoopback bool
	resolver           *net.Resolver
}

var errEndpointDenied = errors.New("model endpoint is not permitted")
var errEndpointConnect = errors.New("model endpoint connection failed")

var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func (p policy) allowedAddress(ip netip.Addr) bool {
	if ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return p.allowLocalLoopback
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func (p policy) validate(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errEndpointDenied
	}
	if err := p.validateAuthority(u); err != nil {
		return nil, err
	}
	return u, nil
}

func (p policy) validateAuthority(u *url.URL) error {
	if u.Opaque != "" || u.Host == "" || u.User != nil || strings.Contains(u.Hostname(), "%") {
		return errEndpointDenied
	}

	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || strings.ContainsAny(host, "\\\x00\r\n") {
		return errEndpointDenied
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errEndpointDenied
		}
	}
	for _, name := range []string{"metadata", "metadata.google.internal", "metadata.azure.internal", "instance-data.ec2.internal"} {
		if host == name || strings.HasSuffix(host, "."+name) {
			return errEndpointDenied
		}
	}
	localName := host == "localhost" || strings.HasSuffix(host, ".localhost")
	ip, literal := netip.ParseAddr(host)
	isLoopback := literal == nil && ip.Unmap().IsLoopback()
	if localName && !p.allowLocalLoopback {
		return errEndpointDenied
	}
	if literal == nil && !p.allowedAddress(ip) {
		return errEndpointDenied
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && p.allowLocalLoopback && (localName || isLoopback)) {
		return errEndpointDenied
	}
	return nil
}

// dial resolves anew at connection time and connects only to the checked IP.
func (p policy) dial(ctx context.Context, network, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errEndpointDenied
	}
	ips, err := p.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %w", errEndpointConnect, ctx.Err())
		}
	}
	return nil, fmt.Errorf("%w: %w", errEndpointConnect, lastErr)
}

func (p policy) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resolver := p.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errEndpointConnect, err)
	}
	if len(ips) == 0 || len(ips) > 16 {
		return nil, fmt.Errorf("%w: %w", errEndpointConnect, errors.New("model endpoint DNS answer count is out of bounds"))
	}
	for _, ip := range ips {
		if !p.allowedAddress(ip) {
			return nil, errEndpointDenied
		}
	}
	return ips, nil
}

const maximumConnections = 128

var errEndpointCapacity = errors.New("model endpoint connection capacity is exhausted")

type pool struct {
	policy policy
	http   *http.Transport
	slots  chan struct{}
}

type connection struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *connection) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

func (p *pool) dial(ctx context.Context, network, address string) (net.Conn, error) {
	select {
	case p.slots <- struct{}{}:
	default:
		return nil, errEndpointCapacity
	}
	conn, err := p.policy.dial(ctx, network, address)
	if err != nil {
		<-p.slots
		return nil, err
	}
	return &connection{Conn: conn, release: func() { <-p.slots }}, nil
}

type transport struct {
	base *url.URL
	pool *pool
}

func (t *transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := t.pool.policy.validateAuthority(request.URL); err != nil {
		return nil, err
	}
	if request.URL.Fragment != "" || request.URL.Scheme != t.base.Scheme || endpointAuthority(request.URL) != endpointAuthority(t.base) {
		return nil, errEndpointDenied
	}
	return t.pool.http.RoundTrip(request)
}
func (t *transport) CloseIdleConnections() { t.pool.http.CloseIdleConnections() }

func endpointAuthority(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	return net.JoinHostPort(strings.ToLower(strings.TrimSuffix(u.Hostname(), ".")), port)
}

func (p policy) newPool() *pool {
	result := &pool{policy: p, slots: make(chan struct{}, maximumConnections)}
	result.http = &http.Transport{
		DialContext: result.dial, ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns: 100, MaxIdleConnsPerHost: 20, MaxConnsPerHost: 64,
		IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		ResponseHeaderTimeout: 2 * time.Minute, ExpectContinueTimeout: time.Second, MaxResponseHeaderBytes: 64 << 10,
	}
	return result
}

// client binds one admitted origin without allocating another connection pool.
func (p *pool) client(ctx context.Context, baseURL string) (*http.Client, error) {
	base, err := p.policy.validate(baseURL)
	if err != nil {
		return nil, err
	}
	if _, err := p.policy.resolve(ctx, base.Hostname()); err != nil {
		return nil, err
	}
	return &http.Client{Transport: &transport{base: base, pool: p}, CheckRedirect: func(*http.Request, []*http.Request) error { return errEndpointDenied }}, nil
}

func (p policy) newClient(ctx context.Context, baseURL string) (*http.Client, error) {
	return p.newPool().client(ctx, baseURL)
}
