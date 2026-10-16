// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package apptemplate

import (
	"bytes"
	"crypto/sha1" // #nosec G505 -- Git object IDs are SHA-1 by definition.
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

// Git blob and tree object IDs are computed locally. Callers use them to verify
// objects created through the GitHub API and to reconcile a lost response
// before uploading again.

// The Git object types of a tree entry.
const (
	objectTypeTree = "tree"
)

// treeID returns the root tree ID for blob entries with slash-separated paths.
// Git tree hashes include raw child hashes and byte-sorted file/directory names.
func treeID(entries []repohost.TreeEntry) (string, error) {
	type node struct {
		entries  map[string]repohost.TreeEntry
		children map[string]*node
	}
	root := &node{entries: map[string]repohost.TreeEntry{}, children: map[string]*node{}}
	for _, entry := range entries {
		parts := strings.Split(entry.Path, "/")
		current := root
		for _, part := range parts {
			if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\x00") {
				return "", errors.New("invalid Git tree path")
			}
		}
		for _, part := range parts[:len(parts)-1] {
			if _, ok := current.entries[part]; ok {
				return "", errors.New("conflicting Git tree path")
			}
			child := current.children[part]
			if child == nil {
				child = &node{entries: map[string]repohost.TreeEntry{}, children: map[string]*node{}}
				current.children[part] = child
			}
			current = child
		}
		name := parts[len(parts)-1]
		if _, ok := current.entries[name]; ok {
			return "", errors.New("duplicate Git tree path")
		}
		if current.children[name] != nil {
			return "", errors.New("conflicting Git tree path")
		}
		entry.Path = name
		current.entries[name] = entry
	}
	var digest func(*node) (string, error)
	digest = func(current *node) (string, error) {
		entries := make([]repohost.TreeEntry, 0, len(current.entries)+len(current.children))
		for _, entry := range current.entries {
			entries = append(entries, entry)
		}
		for name, child := range current.children {
			sha, err := digest(child)
			if err != nil {
				return "", err
			}
			entries = append(entries, repohost.TreeEntry{Path: name, Mode: "40000", Type: objectTypeTree, SHA: sha})
		}
		sort.Slice(entries, func(i, j int) bool {
			a, b := entries[i].Path, entries[j].Path
			if entries[i].Type == objectTypeTree {
				a += "/"
			}
			if entries[j].Type == objectTypeTree {
				b += "/"
			}
			return a < b
		})
		var data bytes.Buffer
		for _, entry := range entries {
			sha, err := hex.DecodeString(entry.SHA)
			if err != nil || len(sha) != 20 {
				return "", errors.New("invalid Git object identity")
			}
			fmt.Fprintf(&data, "%s %s\x00", entry.Mode, entry.Path)
			data.Write(sha)
		}
		sum := sha1.Sum(append([]byte(fmt.Sprintf("tree %d\x00", data.Len())), data.Bytes()...)) // #nosec G401 -- Git object identity.
		return hex.EncodeToString(sum[:]), nil
	}
	return digest(root)
}

// The tests name these constants.
const (
	ObjectTypeTreeForTest = objectTypeTree
)
