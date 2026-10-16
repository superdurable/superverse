// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package orca

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestOrcaVersionAndTabsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := client.Runtime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.OrcaVersion != "1.4.225" || !strings.HasPrefix(runtime.CodexVersion, "0.162.") {
		t.Fatalf("unexpected Orca/Codex versions: %s / %s", runtime.OrcaVersion, runtime.CodexVersion)
	}
	conversations, err := client.OpenConversations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) == 0 {
		t.Fatal("Orca returned no open Codex conversation")
	}
	worktrees, err := client.OpenWorktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(worktrees) == 0 {
		t.Fatal("Orca returned no open worktree")
	}
	for _, worktree := range worktrees {
		if resolved, err := client.ResolveWorktree(ctx, "path:"+worktree.Path); err != nil || resolved != worktree.Selector {
			t.Fatalf("resolve a worktree from the real tabs snapshot: %v", err)
		}
	}
	t.Logf("Orca %s, Codex %s: %d conversations and %d resolvable worktrees",
		runtime.OrcaVersion, runtime.CodexVersion, len(conversations), len(worktrees))
}
