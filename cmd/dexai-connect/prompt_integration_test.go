// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestBridgePromptsInPTY(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "dexai-connect")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Bridge CLI: %v\n%s", err, output)
	}
	command := exec.CommandContext(ctx, "python3", "prompt_pty_integration_test.py")
	command.Env = append(os.Environ(), "DEXAI_CONNECT_BINARY="+binary)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("exercise real CLI and Orca in a PTY: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}
