// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package git

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
)

type CheckoutResult struct {
	BaseCommitSHA string // remote default-branch HEAD
	HeadCommitSHA string // HEAD of the checkout
	HeadPushed    bool   // the remote default branch contains HeadCommitSHA
}

// Checkout prepares the checkout of one Session in a new Sandbox instance. A
// fresh volume is cloned at the remote default-branch HEAD. A retained volume
// belongs to the same Session and is adopted as it is, clean or dirty, on
// whatever commit it holds; it is only fast-forwarded when it is clean and
// strictly behind the remote branch, so local work is never discarded or rejected.
func (operations *GitOperations) Checkout(ctx context.Context, client *sandboxd.SandboxClient, repository Repository) (CheckoutResult, error) {
	if err := validateRepository(repository); err != nil {
		return CheckoutResult{}, err
	}
	g, err := operations.repositoryGit(ctx, client, repository)
	if err != nil {
		return CheckoutResult{}, err
	}
	remote := repository.CloneURL
	inside, err := g.git(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil && inside.exitCode != 128 {
		// Only Git's own answer, exit 128, says the workspace is no repository.
		// A failed probe must not send an existing checkout down the clone path.
		return CheckoutResult{}, commandError("inspect workspace repository", inside, err)
	}
	if err != nil || strings.TrimSpace(inside.stdout) != "true" {
		return g.clone(ctx, remote, repository.DefaultBranch)
	}

	origin, err := g.git(ctx, "remote", "get-url", "origin")
	if err != nil && origin.exitCode == 0 {
		// Git never answered, so nothing is known about the origin.
		return CheckoutResult{}, commandError("read workspace origin", origin, err)
	}
	if err != nil || !sameRemote(strings.TrimSpace(origin.stdout), remote) {
		return CheckoutResult{}, sandboxd.NewToolFailure(toolfailure.WorkspaceOriginMismatch, errors.New("workspace origin does not match the admitted repository"))
	}
	if err = g.remoteStep(ctx, "fetch repository", "fetch", "--prune", "origin"); err != nil {
		return CheckoutResult{}, err
	}
	base, err := g.existingRemoteHead(ctx, repository.DefaultBranch)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("resolve default branch: %w", err)
	}
	local, err := g.localHead(ctx)
	if err != nil {
		return CheckoutResult{}, err
	}
	// A commit the remote branch contains can be served by GitHub. An unknown
	// answer reads as not contained, which only keeps the checkout untouched.
	pushed, _ := g.isAncestor(ctx, local, base)
	if pushed && local != base {
		status, statusErr := g.git(ctx, "status", "--porcelain=v1")
		branch, branchErr := g.git(ctx, "branch", "--show-current")
		if statusErr == nil && branchErr == nil && strings.TrimSpace(status.stdout) == "" &&
			strings.TrimSpace(branch.stdout) == repository.DefaultBranch {
			if err = g.step(ctx, "fast-forward default branch", "merge", "--ff-only", base); err != nil {
				return CheckoutResult{}, err
			}
			local = base
		}
	}
	return CheckoutResult{BaseCommitSHA: base, HeadCommitSHA: local, HeadPushed: pushed}, nil
}

// clone fills an empty volume with the remote default branch.
func (g workspaceGit) clone(ctx context.Context, remote, branch string) (CheckoutResult, error) {
	if err := g.prepareCloneDestination(ctx); err != nil {
		return CheckoutResult{}, err
	}
	if err := g.remoteStep(ctx, "clone repository", "clone", remote, "."); err != nil {
		return CheckoutResult{}, err
	}
	base, err := g.existingRemoteHead(ctx, branch)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("resolve default branch: %w", err)
	}
	if err = g.step(ctx, "checkout default branch", "checkout", "-B", branch, base); err != nil {
		return CheckoutResult{}, err
	}
	return CheckoutResult{BaseCommitSHA: base, HeadCommitSHA: base, HeadPushed: true}, nil
}

// prepareCloneDestination accepts only an empty volume, or one holding just the
// filesystem's lost+found, which it removes.
func (g workspaceGit) prepareCloneDestination(ctx context.Context) error {
	listed, err := g.run(ctx, sandboxd.GitProcessEnvironment(), gitCommandTimeout, "ls", "-A")
	if err != nil {
		return commandError("inspect clone destination", listed, err)
	}
	entries := strings.Fields(listed.stdout)
	if len(entries) == 0 {
		return nil
	}
	if len(entries) != 1 || entries[0] != "lost+found" {
		return sandboxd.NewToolFailure(toolfailure.WorkspaceNotEmpty, errors.New("workspace contains files but is not a Git repository"))
	}
	removed, err := g.run(ctx, sandboxd.GitProcessEnvironment(), gitCommandTimeout, "rmdir", "lost+found")
	switch {
	case err == nil:
		return nil
	case removed.exitCode != 0:
		// rmdir answered: lost+found holds files, so the volume is not empty.
		return sandboxd.NewToolFailure(toolfailure.WorkspaceNotEmpty, commandError("remove empty filesystem initializer", removed, err))
	default:
		// The command never answered; asking again may succeed.
		return commandError("remove empty filesystem initializer", removed, err)
	}
}

// existingRemoteHead reads the GitHub head of the default branch, whose absence
// is the application failure git_remote_conflict.
func (g workspaceGit) existingRemoteHead(ctx context.Context, branch string) (string, error) {
	sha, found, err := g.remoteHead(ctx, branch)
	if err != nil {
		return "", err
	}
	if !found {
		return "", sandboxd.NewToolFailure(toolfailure.GitRemoteConflict, errors.New("default branch is missing on GitHub")).WithEffect(toolfailure.EffectPartial)
	}
	return sha, nil
}

// Inspect reports whether the workspace holds uncommitted changes, and its
// HEAD, without changing anything. It reads only the local checkout: no
// reclaim or Preview decision depends on GitHub being reachable.
func (operations *GitOperations) Inspect(ctx context.Context, client *sandboxd.SandboxClient) (dirty bool, head string, err error) {
	g := workspaceGit{client: client}
	status, err := g.git(ctx, "status", "--porcelain=v1")
	if err != nil {
		return false, "", commandError("inspect workspace status", status, err)
	}
	head, err = g.localHead(ctx)
	if err != nil {
		return false, "", err
	}
	return strings.TrimSpace(status.stdout) != "", head, nil
}

func sameRemote(actual, expected string) bool {
	return actual == expected || actual == strings.TrimSuffix(expected, ".git")
}
