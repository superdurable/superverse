// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package localgit

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

// maximumCommits bounds ListCommits, as the GitHub host does.
const maximumCommits = 100

// maximumFileBytes bounds ReadFile.
const maximumFileBytes = 8 << 20

// errInvalidBranch reports a branch name that Git does not allow. No branch has
// such a name, so a read also reports it as repohost.ErrNotFound.
var errInvalidBranch = errors.New("the branch name is not valid")

// ReadFile reads one repository-relative file at an exact commit.
func (h *Host) ReadFile(_ context.Context, fullName, commit, path string) ([]byte, error) {
	var contents []byte
	err := h.withRepository(fullName, func(opened *git.Repository, found repository) error {
		resolved, err := found.resolveCommit(opened, commit)
		if err != nil {
			return err
		}
		file, err := resolved.File(path)
		if err != nil {
			return fmt.Errorf("file %q at commit %s: %w: %w", path, resolved.Hash, repohost.ErrNotFound, err)
		}
		// The size comes from the object's header: a file over the limit is
		// never loaded.
		if file.Size > maximumFileBytes {
			return fmt.Errorf("file %q is larger than %d bytes", path, maximumFileBytes)
		}
		reader, err := file.Reader()
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		contents, err = io.ReadAll(reader)
		return err
	})
	return contents, err
}

// ListCommits lists the recent commits of a branch, newest first, whoever
// authored them.
func (h *Host) ListCommits(_ context.Context, fullName, branch string) ([]repohost.Commit, error) {
	var commits []repohost.Commit
	err := h.withRepository(fullName, func(opened *git.Repository, found repository) error {
		head, err := found.branchCommit(opened, branch)
		if err != nil {
			return err
		}
		history, err := opened.Log(&git.LogOptions{From: head.Hash, Order: git.LogOrderCommitterTime})
		if err != nil {
			return err
		}
		defer history.Close()
		for len(commits) < maximumCommits {
			next, nextErr := history.Next()
			if errors.Is(nextErr, io.EOF) {
				break
			}
			if nextErr != nil {
				return nextErr
			}
			commits = append(commits, repohost.Commit{SHA: next.Hash.String(), Message: next.Message, CommittedAt: next.Committer.When})
		}
		return nil
	})
	return commits, err
}

// ValidateReleaseCommit accepts any commit reachable from branch, regardless of
// its author. A commit the repository does not have is ErrNotFound, as the
// GitHub host answers.
func (h *Host) ValidateReleaseCommit(_ context.Context, fullName, branch, commit string) error {
	return h.withRepository(fullName, func(opened *git.Repository, found repository) error {
		resolved, err := found.resolveCommit(opened, commit)
		if err != nil {
			return err
		}
		head, err := found.branchCommit(opened, branch)
		if err != nil {
			return err
		}
		reachable, err := resolved.IsAncestor(head)
		if err != nil {
			return err
		}
		if !reachable {
			return repohost.ErrCommitUnreachable
		}
		return nil
	})
}

// IsAncestor reports whether ancestor is descendant or one of its ancestors,
// as git merge-base --is-ancestor does. Both must be exact commit SHAs; a
// commit the repository does not have is ErrNotFound, as the GitHub host
// answers.
func (h *Host) IsAncestor(_ context.Context, fullName, ancestor, descendant string) (bool, error) {
	if !flowid.IsCommitSHA(ancestor) || !flowid.IsCommitSHA(descendant) {
		return false, fmt.Errorf("%w: ancestry compares exact commit SHAs", repohost.ErrRejected)
	}
	var reachable bool
	err := h.withRepository(fullName, func(opened *git.Repository, found repository) error {
		older, err := found.resolveCommit(opened, ancestor)
		if err != nil {
			return err
		}
		newer, err := found.resolveCommit(opened, descendant)
		if err != nil {
			return err
		}
		reachable, err = older.IsAncestor(newer)
		return err
	})
	return reachable, err
}

// DescribeCommit returns the version of a commit: the number of its
// ancestors, itself included, minus one, and its subject. Every ancestor is
// counted once, as git rev-list --count does.
func (h *Host) DescribeCommit(_ context.Context, fullName, commit string) (repohost.CommitVersion, error) {
	var version repohost.CommitVersion
	err := h.withRepository(fullName, func(opened *git.Repository, found repository) error {
		resolved, err := found.resolveCommit(opened, commit)
		if err != nil {
			return err
		}
		history, err := opened.Log(&git.LogOptions{From: resolved.Hash})
		if err != nil {
			return err
		}
		defer history.Close()
		ancestors := uint64(0)
		if err = history.ForEach(func(*object.Commit) error { ancestors++; return nil }); err != nil {
			return err
		}
		version = repohost.NewCommitVersion(ancestors, resolved.Message)
		return nil
	})
	return version, err
}

// branchHead returns the commit a branch points to, for the JSON interface. A
// missing branch is repohost.ErrNotFound, and a name Git does not allow is
// errInvalidBranch too.
func (h *Host) branchHead(fullName, branch string) (string, error) {
	var head string
	err := h.withRepository(fullName, func(opened *git.Repository, found repository) error {
		commit, err := found.branchCommit(opened, branch)
		if err != nil {
			return err
		}
		head = commit.Hash.String()
		return nil
	})
	return head, err
}

// withRepository opens the repository named fullName for reading and closes it.
func (h *Host) withRepository(fullName string, read func(*git.Repository, repository) error) error {
	found, err := h.store.find(fullName)
	if err != nil {
		return err
	}
	opened, storage, err := found.open()
	if err != nil {
		return err
	}
	defer func() { _ = storage.Close() }()
	return read(opened, found)
}

// branchCommit is the commit a branch points to; a missing branch, or one whose
// name Git does not allow, is not found, and a corrupt reference is an error.
func (r repository) branchCommit(opened *git.Repository, branch string) (*object.Commit, error) {
	name := plumbing.NewBranchReferenceName(branch)
	if name.Validate() != nil {
		return nil, fmt.Errorf("branch %q: %w: %w", branch, errInvalidBranch, repohost.ErrNotFound)
	}
	head, exists, err := r.reference(opened.Storer, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("branch %q: %w", branch, repohost.ErrNotFound)
	}
	return opened.CommitObject(head)
}

// resolveCommit finds a commit by its full SHA, or else by branch name. A commit
// the repository does not have is not found.
func (r repository) resolveCommit(opened *git.Repository, ref string) (*object.Commit, error) {
	if !flowid.IsCommitSHA(ref) {
		return r.branchCommit(opened, ref)
	}
	commit, err := opened.CommitObject(plumbing.NewHash(ref))
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, fmt.Errorf("commit %s: %w", ref, repohost.ErrNotFound)
	}
	return commit, err
}
