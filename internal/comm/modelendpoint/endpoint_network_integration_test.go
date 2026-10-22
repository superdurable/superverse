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
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// These actual transport HTTP/TLS/DNS calls establish no model compatibility.
func TestEndpointTransportNetworkIntegration(t *testing.T) {
	if (Config{AllowLocalLoopback: true}).Validate(false) == nil {
		t.Fatal("hosted Validate admitted loopback policy")
	}
	if err := (Config{AllowLocalLoopback: true}).Validate(true); err != nil {
		t.Fatal("local Validate refused its explicit policy")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "probe"), []byte("endpoint transport"), 0600); err != nil {
		t.Fatal(err)
	}
	files := http.FileServer(http.Dir(root))
	secured := httptest.NewTLSServer(files)
	defer secured.Close()
	plain := httptest.NewServer(files)
	defer plain.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	for _, base := range []string{secured.URL, plain.URL} {
		owned := NewPool(Config{AllowLocalLoopback: true})
		defer owned.Close()
		local, err := owned.Client(ctx, base)
		if err != nil {
			t.Fatal(err)
		}
		defer local.CloseIdleConnections()
		roots := x509.NewCertPool()
		roots.AddCert(secured.Certificate())
		local.Transport.(*transport).pool.http.TLSClientConfig.RootCAs = roots
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/probe?beta=true", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := local.Do(request)
		if err != nil {
			t.Fatal("explicit local transport:", err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || string(body) != "endpoint transport" {
			t.Fatal("actual local response did not match")
		}
		for _, foreign := range []string{strings.Replace(base, "//", "//user:secret@", 1), strings.Replace(base, "127.0.0.1", "localhost", 1)} {
			forbidden, err := http.NewRequestWithContext(ctx, http.MethodGet, foreign+"/probe?beta=true", nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := local.Do(forbidden)
			if result != nil {
				result.Body.Close()
			}
			if !errors.Is(err, errEndpointDenied) {
				t.Fatal("admitted origin did not fence request authority")
			}
		}
	}

	for _, target := range []string{plain.URL, secured.URL, strings.Replace(secured.URL, "127.0.0.1", "localhost", 1),
		"http://example.com", "https://user:secret@example.com", "https://example.com/?key=secret", "https://example.com/#secret",
		"https://10.0.0.1", "https://172.16.0.1", "https://192.168.0.1", "https://169.254.169.254",
		"https://100.100.100.200", "https://metadata.google.internal", "https://metadata.azure.internal",
		"https://[::1]", "https://[::ffff:127.0.0.1]", "https://[fd00:ec2::254]", "https://[fe80::1%25lo0]",
		"https://[64:ff9b::a9fe:a9fe]", "https://0.0.0.0", "https://192.0.2.1", "https://[2001:db8::1]", "https://[3fff::1]"} {
		t.Run(target, func(t *testing.T) {
			client, err := (policy{}).newClient(ctx, target)
			if client != nil {
				client.CloseIdleConnections()
			}
			if !errors.Is(err, errEndpointDenied) {
				t.Fatalf("expected base admission denial, got %T", err)
			}
		})
	}
	_, port, err := net.SplitHostPort(strings.TrimPrefix(plain.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	connection, err := (policy{}).dial(ctx, "tcp", net.JoinHostPort("localhost", port))
	if connection != nil {
		connection.Close()
	}
	if !errors.Is(err, errEndpointDenied) {
		t.Fatalf("connect-time system DNS was not denied: %T", err)
	}
}

func TestEndpointRedirectNetworkIntegration(t *testing.T) {
	var received atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.RedirectHandler(destination.URL, http.StatusTemporaryRedirect))
	defer redirect.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, err := policy{allowLocalLoopback: true}.newClient(ctx, redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, redirect.URL, strings.NewReader("credential probe"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer synthetic-transport-credential")
	response, err := client.Do(request)
	if response != nil {
		response.Body.Close()
	}
	if !errors.Is(err, errEndpointDenied) || received.Load() != 0 {
		t.Fatal("transport followed a credential-bearing redirect")
	}
}

func TestEndpointConnectionCauseNetworkIntegration(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := (policy{allowLocalLoopback: true}).newClient(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if response != nil {
		response.Body.Close()
	}
	var operation *net.OpError
	if !errors.Is(err, errEndpointConnect) || !errors.As(err, &operation) {
		t.Fatal("connection failure lost its typed cause")
	}
}

func TestEndpointConnectionBudgetNetworkIntegration(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	pool := (policy{allowLocalLoopback: true}).newPool()
	defer pool.http.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var connections []net.Conn
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()
	// Two actual listener authorities share one total connection budget.
	for i := 0; i < maximumConnections; i++ {
		target := first.Addr().String()
		if i%2 != 0 {
			target = second.Addr().String()
		}
		conn, err := pool.dial(ctx, "tcp", target)
		if err != nil {
			t.Fatal("fill real connection pool:", err)
		}
		connections = append(connections, conn)
	}
	if _, err := pool.dial(ctx, "tcp", second.Addr().String()); !errors.Is(err, errEndpointCapacity) {
		t.Fatal("total connection budget was not enforced")
	}
	if err := connections[0].Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := pool.dial(ctx, "tcp", second.Addr().String())
	if err != nil {
		t.Fatal("closed connection did not release capacity:", err)
	}
	restored.Close()
}
