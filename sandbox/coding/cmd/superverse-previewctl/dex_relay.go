// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"time"
)

// dexRelay exposes Dex to the platform on the Pod's own addresses. dexcli
// v1.6.2 binds Dex and Dex Web to one address, and its local connector setup
// requires that address to be loopback, so Dex listens on 127.0.0.1:8801 and
// this relay listens on <Pod IP>:8801, the only port of the Sandbox Service.
// Dex Web stays on loopback. Linux allows a specific address beside loopback
// on one port, but not the wildcard address.
type dexRelay struct {
	listeners []net.Listener
}

func startDexRelay(ctx context.Context) (*dexRelay, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("list the Pod addresses: %w", err)
	}
	relay := &dexRelay{}
	for _, address := range addresses {
		network, ok := address.(*net.IPNet)
		if !ok || network.IP.IsLoopback() || network.IP.IsLinkLocalUnicast() {
			continue
		}
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(network.IP.String(), strconv.Itoa(dexPort)))
		if err != nil {
			relay.stop()
			return nil, fmt.Errorf("the Preview engine port %d on %s is in use: %w", dexPort, network.IP, err)
		}
		relay.listeners = append(relay.listeners, listener)
		go relay.serve(ctx, listener)
	}
	if len(relay.listeners) == 0 {
		return nil, errors.New("the Pod has no address on which to expose Dex")
	}
	return relay, nil
}

func (r *dexRelay) serve(ctx context.Context, listener net.Listener) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go relayConnection(ctx, connection)
	}
}

// relayConnection copies both directions until either side closes. A Dex
// that stops ends every relayed connection with it.
func relayConnection(ctx context.Context, connection net.Conn) {
	defer func() { _ = connection.Close() }()
	dial, cancel := context.WithTimeout(ctx, 5*time.Second)
	upstream, err := (&net.Dialer{}).DialContext(dial, "tcp", dexAddress)
	cancel()
	if err != nil {
		log.Printf("relay a platform connection to Dex: %v", err)
		return
	}
	defer func() { _ = upstream.Close() }()
	done := make(chan struct{}, 2)
	for _, pair := range [][2]net.Conn{{upstream, connection}, {connection, upstream}} {
		go func() {
			_, _ = io.Copy(pair[0], pair[1])
			done <- struct{}{}
		}()
	}
	<-done
}

// stop closes the listeners; connections in flight end when Dex stops.
func (r *dexRelay) stop() {
	for _, listener := range r.listeners {
		_ = listener.Close()
	}
}
