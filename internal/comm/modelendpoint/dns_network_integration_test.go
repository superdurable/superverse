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
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// The real UDP DNS service changes its zone after admission, before connection.
func TestEndpointDNSChangeNetworkIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var private atomic.Bool
	var queries atomic.Int32
	resolver := endpointDNS(t, &private, &queries)
	var received atomic.Int32
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer endpoint.Close()
	base := strings.Replace(endpoint.URL, "127.0.0.1", "model.endpoint.test", 1)
	client, err := (policy{allowLocalLoopback: true, resolver: resolver}).newClient(ctx, base)
	if err != nil {
		t.Fatal("admit original real DNS answer:", err)
	}
	defer client.CloseIdleConnections()
	admitted := queries.Load()
	if admitted != 0 {
		t.Fatal("client construction must not depend on DNS")
	}
	// Observe the original zone through the real resolver, then prove the dial
	// resolves again rather than trusting that earlier public/allowed answer.
	if _, err := (policy{allowLocalLoopback: true, resolver: resolver}).resolve(ctx, "model.endpoint.test"); err != nil {
		t.Fatal("original DNS answer:", err)
	}
	admitted = queries.Load()
	private.Store(true)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models?limit=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if response != nil {
		response.Body.Close()
	}
	if !errors.Is(err, errEndpointDenied) || queries.Load() <= admitted || received.Load() != 0 {
		t.Fatal("changed DNS answer reached an endpoint or was not rechecked")
	}
}

func endpointDNS(t *testing.T, private *atomic.Bool, queries *atomic.Int32) *net.Resolver {
	t.Helper()
	socket, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	faults := make(chan error, 1)
	go func() {
		defer close(stopped)
		buffer := make([]byte, 4096)
		for {
			size, peer, err := socket.ReadFrom(buffer)
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if err != nil {
				faults <- err
				return
			}
			var parser dnsmessage.Parser
			header, err := parser.Start(buffer[:size])
			if err != nil {
				faults <- err
				return
			}
			question, err := parser.Question()
			if err != nil {
				faults <- err
				return
			}
			queries.Add(1)
			builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: header.ID, Response: true, Authoritative: true})
			if err = builder.StartQuestions(); err == nil {
				err = builder.Question(question)
			}
			if err == nil {
				err = builder.StartAnswers()
			}
			if err == nil && question.Type == dnsmessage.TypeA {
				ip := [4]byte{127, 0, 0, 1}
				if private.Load() {
					ip = [4]byte{10, 0, 0, 1}
				}
				err = builder.AResource(dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 0}, dnsmessage.AResource{A: ip})
			}
			var reply []byte
			if err == nil {
				reply, err = builder.Finish()
			}
			if err == nil {
				_, err = socket.WriteTo(reply, peer)
			}
			if err != nil {
				faults <- err
				return
			}
		}
	}()
	t.Cleanup(func() {
		socket.Close()
		<-stopped
		select {
		case err := <-faults:
			t.Error("actual DNS service:", err)
		default:
		}
	})
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", socket.LocalAddr().String())
	}}
}
