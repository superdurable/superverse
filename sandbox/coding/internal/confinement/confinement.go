// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package confinement applies the path rules of the coding image's helpers: a
// path must resolve, after its symbolic links, inside the agent root, where it
// may be read and written, or inside a read-only root, where it may only be
// read; and no path may pass through a .git directory of its root. The rules
// isolate agents and keep them correct; they are no security boundary, because
// the helpers and the commands they run share one UID.
package confinement

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrOutsideRoots refuses a path that leaves every root, writes into a
// read-only root or passes through .git.
var ErrOutsideRoots = errors.New("path is outside the agent root and the read-only roots")

// Read-only roots of every agent besides the module cache named by GOMODCACHE.
const (
	SkillsRoot      = "/opt/superverse/dex-skills"
	GoSourceRoot    = "/usr/local/go/src"
	PreviewLogsRoot = "/var/lib/superverse/preview/logs"
	CommandRunsRoot = "/tmp/superverse-exec/runs"
)

// Access is what a caller does with a path.
type Access int

const (
	Read Access = iota
	Write
)

// Roots are the agent root and the read-only roots, each resolved once.
type Roots struct {
	agent    string
	readOnly []string
}

// Path is a confined path: the resolved absolute path, the root it lies in
// and its slash-separated path relative to that root ("." for the root).
type Path struct {
	Absolute string
	Root     string
	Relative string
	ReadOnly bool
}

// New resolves the agent root, which must be an absolute directory, and the
// read-only roots of this process's environment. A read-only root that does
// not exist yet keeps its literal path.
func New(agentRoot string) (Roots, error) {
	if !filepath.IsAbs(agentRoot) {
		return Roots{}, fmt.Errorf("the agent root %q must be absolute", agentRoot)
	}
	agent, err := filepath.EvalSymlinks(filepath.Clean(agentRoot))
	if err != nil {
		return Roots{}, fmt.Errorf("resolve the agent root: %w", err)
	}
	if info, err := os.Stat(agent); err != nil || !info.IsDir() {
		return Roots{}, fmt.Errorf("the agent root %q is not a directory", agentRoot)
	}
	roots := Roots{agent: agent}
	for _, root := range []string{SkillsRoot, os.Getenv("GOMODCACHE"), GoSourceRoot, PreviewLogsRoot, CommandRunsRoot} {
		if !filepath.IsAbs(root) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil {
			resolved = filepath.Clean(root)
		}
		roots.readOnly = append(roots.readOnly, resolved)
	}
	return roots, nil
}

// Agent is the resolved agent root.
func (roots Roots) Agent() string { return roots.agent }

// Resolve confines path, which is absolute or relative to the agent root, for
// access. A path that does not exist yet resolves through its nearest existing
// ancestor, so a new file cannot be placed outside a root through a linked
// parent directory. A path that a directory without search permission hides
// resolves the same way, through its nearest ancestor that resolves: the roots
// decide whether it may be named at all, and the operation on it then meets
// the permission error.
func (roots Roots) Resolve(path string, access Access) (Path, error) {
	lexical := filepath.Clean(path)
	if !filepath.IsAbs(lexical) {
		lexical = filepath.Join(roots.agent, lexical)
	}
	resolved, err := resolveLinks(lexical)
	if err != nil {
		return Path{}, err
	}
	if relative, inside := within(roots.agent, resolved); inside {
		if lexicalRelative, lexicalInside := within(roots.agent, lexical); throughGit(relative) || lexicalInside && throughGit(lexicalRelative) {
			return Path{}, fmt.Errorf("%w: %s passes through .git", ErrOutsideRoots, path)
		}
		return Path{Absolute: resolved, Root: roots.agent, Relative: relative}, nil
	}
	for _, root := range roots.readOnly {
		relative, inside := within(root, resolved)
		if !inside {
			continue
		}
		if access == Write {
			return Path{}, fmt.Errorf("%w: %s is in the read-only root %s", ErrOutsideRoots, path, root)
		}
		if throughGit(relative) {
			return Path{}, fmt.Errorf("%w: %s passes through .git", ErrOutsideRoots, path)
		}
		return Path{Absolute: resolved, Root: root, Relative: relative, ReadOnly: true}, nil
	}
	return Path{}, fmt.Errorf("%w: %s", ErrOutsideRoots, path)
}

// resolveLinks evaluates the symbolic links of an absolute, clean path. For a
// path that does not exist or that permissions hide, it evaluates the nearest
// ancestor that resolves and appends the rest lexically.
func resolveLinks(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil || !unresolvable(err) {
		return resolved, err
	}
	ancestor, suffix := path, []string{}
	for {
		suffix = append(suffix, filepath.Base(ancestor))
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("%w: %s has no existing ancestor", ErrOutsideRoots, path)
		}
		ancestor = parent
		resolved, err = filepath.EvalSymlinks(ancestor)
		if err == nil {
			slices.Reverse(suffix)
			return filepath.Join(append([]string{resolved}, suffix...)...), nil
		}
		if !unresolvable(err) {
			return "", err
		}
	}
}

// unresolvable reports an error of a path that its nearest resolvable ancestor
// stands in for: the path does not exist, or a directory on it denies search.
func unresolvable(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission)
}

// within reports whether path is root or below it, with the slash-separated
// path relative to root. A sibling sharing a prefix, such as /workspace-old
// for /workspace, is not within.
func within(root, path string) (string, bool) {
	if path == root {
		return ".", true
	}
	relative, found := strings.CutPrefix(path, root+string(filepath.Separator))
	return filepath.ToSlash(relative), found
}

func throughGit(relative string) bool {
	return slices.Contains(strings.Split(relative, "/"), ".git")
}
