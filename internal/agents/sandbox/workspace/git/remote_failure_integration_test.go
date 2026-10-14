// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package git

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd/sandboxdtest"
)

// The actual Git of the owned sandboxd lists a remote on a loopback port that
// nothing listens on: it exits nonzero, which is a transient fault of a remote
// command that Dex repeats, not a typed failure. A local command that fails is
// not transient.
func TestRemoteGitFailureIsTransientActualSandbox(t *testing.T) {
	g := workspaceGit{client: sandboxdtest.ClientForTest(t)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var typed *sandboxd.ToolFailure
	err := g.remoteStep(ctx, "list the unreachable remote", "ls-remote", "http://127.0.0.1:1/none.git")
	if err == nil || errors.As(err, &typed) || !sandboxd.IsTransient(err) {
		t.Fatalf("a failed remote Git command must be a transient, untyped error: %v", err)
	}
	err = g.step(ctx, "resolve a revision that does not exist", "rev-parse", "--verify", "refs/heads/no-such-branch")
	if err == nil || errors.As(err, &typed) || sandboxd.IsTransient(err) {
		t.Fatalf("a failed local Git command must stay an ordinary error: %v", err)
	}
}
