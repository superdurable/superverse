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
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd/sandboxdtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// TestConnectorContributionHelperActualSandbox runs the actual contribution
// helper through the real sandboxd of a coding container, against a bare copy
// of the official library in the container. A Stripe example with two Flows
// fails the library's own gate without a push, then a repaired worktree passes
// and pushes once. A remote that cannot be reached is transient; and a failure that
// asking again repeats, a workspace without a repository, is a platform
// tool_error at once.
func TestConnectorContributionHelperActualSandbox(t *testing.T) {
	client := sandboxdtest.StartCodingContainerForTest(t).Client()
	shell(t, client, `set -e; git clone -q --bare https://github.com/superdurable/dex-connectors-library.git /tmp/library.git
git init -q -b main /workspace; test ! -e /workspace/.git/superverse`)
	remote := connectorlibrary.Remote{CloneURL: "file:///tmp/library.git", Location: connectorlibrary.LocationSandbox}
	const branch = "superverse/localkind/ab12/stripe"
	create := map[string]any{"branch": branch, "slug": "stripe", "url": remote.CloneURL, "minimumFreeBytes": minimumFreeBytes}

	var worktree ContributionWorktree
	if err := runContribution(testContext(t), client, "create", "k3n8q5r1", remote, create, &worktree); err != nil {
		t.Fatalf("create the first contribution worktree of the workspace: %v", err)
	}
	if worktree.Branch != branch || !flowid.IsCommitSHA(worktree.Base) || worktree.Root != ContributionWorktreeRoot("k3n8q5r1") {
		t.Fatalf("the first worktree is not on the connector's branch: %+v", worktree)
	}
	shell(t, client, `set -e; cd `+worktree.Root+`; cat > connectors/stripe/examples/create-checkout-session/flow/card_flow.go <<'EOF'
package createcheckoutsession
import "github.com/superdurable/dex/sdk-go/dex"
type CardFlow struct { dex.FlowDefaults }
func (*CardFlow) GetSteps() []dex.StepDef { return nil }
EOF
printf '\nThe Stripe checkout example remains the compatibility fixture.\n' >> connectors/stripe/README.md`)
	finish := map[string]any{"branch": branch, "slug": "stripe", "contribution": "fix", "subject": ContributionSubject("stripe", "repair checkout example"),
		"name": "Superverse", "email": "bot@superdurable.io", "push": true, "url": remote.CloneURL,
		"publicURL": remote.CloneURL, "contribute": true}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Minute)
	defer cancel()
	var failed contributionAnswer
	coldStart := time.Now()
	if err := runContribution(ctx, client, "finish", "k3n8q5r1", remote, finish, &failed); err != nil {
		t.Fatalf("check the invalid Stripe example: %v", err)
	}
	coldDuration := time.Since(coldStart)
	if !strings.Contains(failed.CheckFailed, "expected one Flow source with GetSteps") || failed.Pushed || failed.Commit != "" {
		t.Fatalf("the two-Flow example was not refused before push: %+v", failed)
	}
	shell(t, client, `set -e; test -e `+worktree.Root+`/connectors/stripe/examples/create-checkout-session/flow/card_flow.go
test -z "$(git --git-dir=/tmp/library.git for-each-ref refs/heads/`+branch+`)"
rm `+worktree.Root+`/connectors/stripe/examples/create-checkout-session/flow/card_flow.go`)
	var firstHead string
	warmStart := time.Now()
	for attempt := range 2 {
		var finished contributionAnswer
		if err := runContribution(ctx, client, "finish", "k3n8q5r1", remote, finish, &finished); err != nil {
			t.Fatalf("finish attempt %d: %v", attempt, err)
		}
		if !finished.Pushed || finished.Module != "connectors/stripe" || finished.Base != worktree.Base || finished.Files != 1 ||
			finished.Unchanged || finished.Missing || finished.Rejected != "" {
			t.Fatalf("finish attempt %d did not contribute the new connector once: %+v", attempt, finished)
		}
		if attempt == 0 {
			firstHead = finished.Commit
		} else if finished.Commit != firstHead {
			t.Fatalf("finish replay changed the committed head from %s to %s", firstHead, finished.Commit)
		}
		if pushed := strings.TrimSpace(shell(t, client, "git --git-dir=/tmp/library.git rev-parse refs/heads/"+branch)); pushed != finished.Commit {
			t.Fatalf("finish attempt %d: the library's branch is at %s, not %s", attempt, pushed, finished.Commit)
		}
	}
	t.Logf("Stripe library gate in Sandbox: cold failure=%s, warm repaired success=%s", coldDuration, time.Since(warmStart))

	var typed *sandboxd.ToolFailure
	unreachable := map[string]any{"branch": "superverse/localkind/ab12/gone", "slug": "gone", "url": "file:///tmp/nowhere.git", "minimumFreeBytes": minimumFreeBytes}
	err := runContribution(testContext(t), client, "create", "p7d2x9m4", remote, unreachable, &ContributionWorktree{})
	if err == nil || errors.As(err, &typed) || !sandboxd.IsTransient(err) {
		t.Fatalf("a remote that cannot be reached must be a transient fault: %v", err)
	}
	// Go module caches are deliberately read-only. Make the retained gate cache
	// writable before removing the test repository for the missing-repository case.
	shell(t, client, "chmod -R u+w /workspace/.git/superverse/connectors/check-cache; rm -rf /workspace/.git")
	err = runContribution(testContext(t), client, "create", "q1w2e3r4", remote, create, &ContributionWorktree{})
	if !errors.As(err, &typed) || typed.Code() != toolfailure.ToolError || sandboxd.IsTransient(err) {
		t.Fatalf("a workspace without a repository must be a platform tool_error at once: %v", err)
	}
}
