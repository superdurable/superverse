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
	"errors"
	"strings"
	"testing"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd/sandboxdtest"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
)

// TestConnectorContributionHelperActualSandbox runs the actual contribution
// helper through the real sandboxd of a coding container, against a bare copy
// of the library in the container. A workspace that never held a contribution
// gets its first worktree, whose new connector the finish commits and pushes
// once; a remote that cannot be reached is transient; and a failure that
// asking again repeats, a workspace without a repository, is a platform
// tool_error at once.
func TestConnectorContributionHelperActualSandbox(t *testing.T) {
	client := sandboxdtest.StartCodingContainerForTest(t).Client()
	shell(t, client, `set -e; export GIT_AUTHOR_NAME=Seed GIT_AUTHOR_EMAIL=seed@example.com GIT_COMMITTER_NAME=Seed GIT_COMMITTER_EMAIL=seed@example.com
git init -q --bare -b main /tmp/library.git; git init -q -b main /tmp/seed; cd /tmp/seed; mkdir -p connectors/newsapi
printf 'directories:\n  - connectors/newsapi\n' > catalog.yaml
printf 'module github.com/superdurable/dex-connectors-library/connectors/newsapi\n' > connectors/newsapi/go.mod
printf 'metadata:\n  name: newsapi\n' > connectors/newsapi/connector.yaml
git add -A; git commit -qm seed; git push -q /tmp/library.git main:main
git init -q -b main /workspace; test ! -e /workspace/.git/superverse`)
	remote := connectorlibrary.Remote{CloneURL: "file:///tmp/library.git", Location: connectorlibrary.LocationSandbox}
	const branch = "superverse/localkind/ab12/mockfeed"
	create := map[string]any{"branch": branch, "slug": "mockfeed", "url": remote.CloneURL, "minimumFreeBytes": minimumFreeBytes}

	var worktree ContributionWorktree
	if err := runContribution(testContext(t), client, "create", "k3n8q5r1", remote, create, &worktree); err != nil {
		t.Fatalf("create the first contribution worktree of the workspace: %v", err)
	}
	if worktree.Branch != branch || !flowid.IsCommitSHA(worktree.Base) || worktree.Root != ContributionWorktreeRoot("k3n8q5r1") {
		t.Fatalf("the first worktree is not on the connector's branch: %+v", worktree)
	}
	shell(t, client, `set -e; cd `+worktree.Root+`; mkdir -p connectors/mockfeed
printf 'directories:\n  - connectors/newsapi\n  - connectors/mockfeed\n' > catalog.yaml
printf 'module github.com/superdurable/dex-connectors-library/connectors/mockfeed\n' > connectors/mockfeed/go.mod
printf 'metadata:\n  name: mockfeed\n' > connectors/mockfeed/connector.yaml`)
	finish := map[string]any{"branch": branch, "slug": "mockfeed", "contribution": "new", "subject": ContributionSubject("mockfeed", "add mockfeed"),
		"name": "Superverse", "email": "bot@superdurable.io", "push": true, "url": remote.CloneURL, "contribute": true}
	for attempt := range 2 {
		var finished contributionAnswer
		if err := runContribution(testContext(t), client, "finish", "k3n8q5r1", remote, finish, &finished); err != nil {
			t.Fatalf("finish attempt %d: %v", attempt, err)
		}
		if !finished.Pushed || finished.Module != "connectors/mockfeed" || finished.Base != worktree.Base || finished.Files != 3 {
			t.Fatalf("finish attempt %d did not contribute the new connector once: %+v", attempt, finished)
		}
		if pushed := strings.TrimSpace(shell(t, client, "git --git-dir=/tmp/library.git rev-parse refs/heads/"+branch)); pushed != finished.Commit {
			t.Fatalf("finish attempt %d: the library's branch is at %s, not %s", attempt, pushed, finished.Commit)
		}
	}

	var typed *sandboxd.ToolFailure
	unreachable := map[string]any{"branch": "superverse/localkind/ab12/gone", "slug": "gone", "url": "file:///tmp/nowhere.git", "minimumFreeBytes": minimumFreeBytes}
	err := runContribution(testContext(t), client, "create", "p7d2x9m4", remote, unreachable, &ContributionWorktree{})
	if err == nil || errors.As(err, &typed) || !sandboxd.IsTransient(err) {
		t.Fatalf("a remote that cannot be reached must be a transient fault: %v", err)
	}
	shell(t, client, "rm -rf /workspace/.git")
	err = runContribution(testContext(t), client, "create", "q1w2e3r4", remote, create, &ContributionWorktree{})
	if !errors.As(err, &typed) || typed.Code() != toolfailure.ToolError || sandboxd.IsTransient(err) {
		t.Fatalf("a workspace without a repository must be a platform tool_error at once: %v", err)
	}
}
