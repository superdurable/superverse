// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxdtest

import (
	"os"
	"testing"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ClientForTest connects to the owned, disposable real sandboxd of a test, whose
// process and file APIs SUPERVERSE_TEST_SANDBOX_GRPC and
// SUPERVERSE_TEST_SANDBOX_HTTP name. Missing endpoints fail the test; the
// connection closes with it.
func ClientForTest(t testing.TB) *sandboxd.SandboxClient {
	t.Helper()
	address, filesystem := os.Getenv("SUPERVERSE_TEST_SANDBOX_GRPC"), os.Getenv("SUPERVERSE_TEST_SANDBOX_HTTP")
	if address == "" || filesystem == "" {
		t.Fatal("actual owned sandboxd gRPC and HTTP addresses are required")
	}
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	return sandboxd.NewSandboxClient(sandboxd.SandboxClientConfig{FilesystemBaseURL: filesystem, ProcessConnection: connection})
}
