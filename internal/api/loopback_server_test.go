// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// serveLoopback serves s, with the production interceptors, on an owned
// ephemeral loopback listener and returns a connection to it, on which a test
// creates the clients of the services it calls. Both stop when the test ends.
func serveLoopback(ctx context.Context, t *testing.T, s *Server) *grpc.ClientConn {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := NewGRPCServer(s)
	if path := os.Getenv("SUPERVERSE_TEST_RPC_COVERAGE_FILE"); path != "" {
		recorder, err := NewRPCCoverage(path)
		if err != nil {
			t.Fatal(err)
		}
		s.RPCCoverage = recorder
		server = NewGRPCServer(s)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if stopped := <-served; stopped != nil && !errors.Is(stopped, grpc.ErrServerStopped) {
			t.Errorf("owned API listener shutdown failed (%T)", stopped)
		}
	})
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := connection.Close(); closeErr != nil {
			t.Errorf("close API connection: %v", closeErr)
		}
	})
	return connection
}
