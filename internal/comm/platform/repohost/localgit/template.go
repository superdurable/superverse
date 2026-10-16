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
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

// templateSignature is the fixed author and committer of the initial commit. The
// commit has no parent, so every initialization of one template creates the same
// commit and the same SHA.
func templateSignature() object.Signature {
	return object.Signature{Name: "Superverse", Email: "bot@superdurable.io", When: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)}
}

// InitializeTemplate commits the application template embedded in this server
// build as the first commit of an empty managed repository. The revision the
// Setup Flow accepted must be this exact template: an older revision fails
// instead of silently initializing different content. A repository whose main
// branch already holds the template tree is left as it is and returns that
// commit. Waiting for a push to finish ends with ctx.
func (h *Host) InitializeTemplate(ctx context.Context, target repohost.Repository, revision string, template *repohost.Template) (string, error) {
	if revision != template.Revision {
		return "", fmt.Errorf("this server does not serve the accepted application template revision %s", revision)
	}
	found, err := h.store.find(target.FullName)
	if err != nil {
		return "", err
	}
	release, err := h.store.lock(ctx, found.ID)
	if err != nil {
		return "", fmt.Errorf("wait for the pushes of repository %s: %w", target.FullName, err)
	}
	defer release()
	if found, err = h.store.find(target.FullName); err != nil {
		return "", err
	}
	if found.Archived {
		return "", fmt.Errorf("managed repository %s is archived", found.FullName)
	}
	opened, storage, err := found.open()
	if err != nil {
		return "", err
	}
	defer func() { _ = storage.Close() }()

	main := plumbing.NewBranchReferenceName(defaultBranch)
	existing, exists, err := found.reference(storage, main)
	if err != nil {
		return "", err
	}
	if exists {
		head, headErr := opened.CommitObject(existing)
		if headErr != nil {
			return "", headErr
		}
		if head.TreeHash.String() == template.TreeID {
			return head.Hash.String(), nil
		}
		return "", errors.New("managed repository changed before template initialization")
	}

	tree, err := writeTree(storage, template.Files, "")
	if err != nil {
		return "", err
	}
	if tree.String() != template.TreeID {
		return "", fmt.Errorf("written template tree has identity %s instead of %s", tree, template.TreeID)
	}
	signature := templateSignature()
	commit := &object.Commit{Author: signature, Committer: signature, Message: "Initialize Superverse V2 project from " + revision + "\n", TreeHash: tree}
	encoded := storage.NewEncodedObject()
	if err = commit.Encode(encoded); err != nil {
		return "", err
	}
	hash, err := storage.SetEncodedObject(encoded)
	if err != nil {
		return "", err
	}
	if err = found.setReference(main, hash); err != nil {
		return "", err
	}
	return hash.String(), nil
}

// writeTree writes the Git tree of the template files below prefix, and the
// blobs and trees under it, and returns the tree's identity.
func writeTree(objects storer.EncodedObjectStorer, files []repohost.TemplateFile, prefix string) (plumbing.Hash, error) {
	var entries []object.TreeEntry
	directories := map[string][]repohost.TemplateFile{}
	for _, file := range files {
		name, _, nested := strings.Cut(strings.TrimPrefix(file.Path, prefix), "/")
		if nested {
			directories[name] = append(directories[name], file)
			continue
		}
		hash, err := writeBlob(objects, file)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		mode := filemode.Regular
		if file.Mode == "100755" {
			mode = filemode.Executable
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: mode, Hash: hash})
	}
	for name, nested := range directories {
		hash, err := writeTree(objects, nested, prefix+name+"/")
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: hash})
	}
	// Git sorts a directory as if its name ended in a slash.
	key := func(entry object.TreeEntry) string {
		if entry.Mode == filemode.Dir {
			return entry.Name + "/"
		}
		return entry.Name
	}
	sort.Slice(entries, func(i, j int) bool { return key(entries[i]) < key(entries[j]) })
	encoded := objects.NewEncodedObject()
	if err := (&object.Tree{Entries: entries}).Encode(encoded); err != nil {
		return plumbing.ZeroHash, err
	}
	return objects.SetEncodedObject(encoded)
}

// writeBlob stores one template file and checks the identity it gets.
func writeBlob(objects storer.EncodedObjectStorer, file repohost.TemplateFile) (plumbing.Hash, error) {
	encoded := objects.NewEncodedObject()
	encoded.SetType(plumbing.BlobObject)
	writer, err := encoded.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err = writer.Write(file.Contents); err != nil {
		return plumbing.ZeroHash, err
	}
	if err = writer.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	hash, err := objects.SetEncodedObject(encoded)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if hash.String() != file.BlobID {
		return plumbing.ZeroHash, fmt.Errorf("template blob %s has identity %s instead of %s", file.Path, hash, file.BlobID)
	}
	return hash, nil
}
