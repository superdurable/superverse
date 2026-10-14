// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	// workspaceRoot is the repository checkout (profile subPath repository).
	workspaceRoot = "/workspace"
	// sourceMarker holds the commit of a complete tree. A daemon that uses a
	// tree holds a shared lock on it, which keeps the tree from deletion.
	sourceMarker = ".superverse-source"
)

// errSourceMissing means the commit is not in the workspace repository.
var errSourceMissing = errors.New("the commit is not in the workspace repository")

// sourceTree is $S/source/<commit>, an export of one commit that no edit in
// the workspace reaches.
type sourceTree struct {
	commit, path string
	// lock is the shared lock of a tree in use, nil otherwise.
	lock *os.File
}

func (t sourceTree) release() {
	if t.lock != nil {
		_ = t.lock.Close()
	}
}

// ensureSource exports commit into its tree unless the tree is complete,
// after deleting every other tree that is not in use: at most the running
// tree and the candidate remain. A commit missing from the repository
// changes nothing. With hold, the tree stays in use until release.
func ensureSource(ctx context.Context, state stateDirectory, commit string, hold bool) (sourceTree, error) {
	unlock, err := lockSources(state)
	if err != nil {
		return sourceTree{}, err
	}
	defer unlock()
	tree := sourceTree{commit: commit, path: filepath.Join(state.sources, commit)}
	recorded, _ := os.ReadFile(filepath.Join(tree.path, sourceMarker)) //nolint:gosec // the marker of a tree under the state directory
	exported := string(recorded) == commit
	if !exported {
		if err = requireCommit(ctx, commit); err != nil {
			return sourceTree{}, err
		}
	}
	entries, err := os.ReadDir(state.sources)
	if err != nil {
		return sourceTree{}, err
	}
	for _, entry := range entries {
		if entry.Name() != commit {
			removeUnusedTree(filepath.Join(state.sources, entry.Name()))
		}
	}
	if !exported {
		if err = exportTree(ctx, tree); err != nil {
			return sourceTree{}, err
		}
	}
	if hold {
		if tree.lock, err = os.Open(filepath.Join(tree.path, sourceMarker)); err != nil { //nolint:gosec // the marker just verified
			return sourceTree{}, err
		}
		if err = syscall.Flock(int(tree.lock.Fd()), syscall.LOCK_SH); err != nil {
			tree.release()
			return sourceTree{}, err
		}
	}
	return tree, nil
}

// removeSourceTree deletes the tree of commit unless it is in use.
func removeSourceTree(state stateDirectory, commit string) {
	unlock, err := lockSources(state)
	if err != nil {
		return
	}
	defer unlock()
	removeUnusedTree(filepath.Join(state.sources, commit))
}

// lockSources serializes every export and deletion of trees. Taking a tree
// into use happens under it too, so a deletion never races a new user.
func lockSources(state stateDirectory) (func(), error) {
	if err := os.MkdirAll(state.sources, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(state.sourceLock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return func() { _ = lock.Close() }, nil
}

// removeUnusedTree deletes path unless a daemon holds its marker's shared
// lock. A partial export has no lock holder.
func removeUnusedTree(path string) {
	if marker, err := os.Open(filepath.Join(path, sourceMarker)); err == nil { //nolint:gosec // a tree under the state directory
		defer func() { _ = marker.Close() }()
		if syscall.Flock(int(marker.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			return
		}
	}
	_ = os.RemoveAll(path)
}

// exportTree extracts `git archive <commit>` into <tree>.partial, records
// the commit and renames it into place, so a tree is either complete or
// absent.
func exportTree(ctx context.Context, tree sourceTree) error {
	partial := tree.path + ".partial"
	if err := os.RemoveAll(partial); err != nil {
		return err
	}
	if err := os.MkdirAll(partial, 0o700); err != nil {
		return err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	var diagnostics bytes.Buffer
	archive := exec.CommandContext(ctx, "git", gitArguments("archive", "--format=tar", tree.commit)...) //nolint:gosec // a validated commit SHA
	archive.Env = gitEnvironment()
	archive.Stdout, archive.Stderr = writer, &diagnostics
	extract := exec.CommandContext(ctx, "tar", "-x", "-f", "-", "-C", partial) //nolint:gosec // a directory under the state directory
	extract.Stdin, extract.Stderr = reader, &diagnostics
	err = errors.Join(archive.Start(), extract.Start())
	_, _ = reader.Close(), writer.Close()
	if err == nil {
		err = errors.Join(archive.Wait(), extract.Wait())
	}
	if err != nil {
		return fmt.Errorf("export commit %s: %w: %s", tree.commit, err, strings.TrimSpace(diagnostics.String()))
	}
	if err = os.WriteFile(filepath.Join(partial, sourceMarker), []byte(tree.commit), 0o600); err != nil {
		return err
	}
	if err = os.RemoveAll(tree.path); err != nil {
		return err
	}
	return os.Rename(partial, tree.path)
}

// requireCommit answers errSourceMissing unless the workspace repository
// holds commit.
func requireCommit(ctx context.Context, commit string) error {
	_, err := runCommand(ctx, gitEnvironment(), "git", gitArguments("cat-file", "-e", commit+"^{commit}")...)
	if errors.As(err, new(*exec.ExitError)) {
		return errSourceMissing
	}
	return err
}

// gitArguments reads the workspace repository without its global or system
// configuration; the volume may belong to another UID.
func gitArguments(arguments ...string) []string {
	return append([]string{"-c", "safe.directory=" + workspaceRoot, "-C", workspaceRoot}, arguments...)
}

func gitEnvironment() []string {
	return append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
}
