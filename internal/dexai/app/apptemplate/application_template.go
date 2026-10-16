// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package apptemplate

import (
	"archive/tar"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"sync"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

//go:generate go run ../../../../cmd/tools/app-template-archive

//go:embed app-template.tar
var archive []byte

// revisionPrefix starts every template revision; the rest is the Git tree ID.
const revisionPrefix = "app-template@"

var current = sync.OnceValue(func() *repohost.Template { return comm.Must(Decode(archive)) })

// Current returns the template embedded in this server build. An embedded
// archive that does not decode is a broken build, so the first call panics.
// The archive is decoded on first use, not at package initialization, so that
// cmd/tools/app-template-archive can still import this package and regenerate
// a broken archive.
func Current() *repohost.Template { return current() }

// Decode reads a template archive written by cmd/tools/app-template-archive.
func Decode(data []byte) (*repohost.Template, error) {
	reader := tar.NewReader(bytes.NewReader(data))
	template := &repohost.Template{}
	entries := []repohost.TreeEntry{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read application template archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || path.Clean(header.Name) != header.Name || path.IsAbs(header.Name) {
			return nil, fmt.Errorf("application template archive has unsupported entry %q", header.Name)
		}
		mode := "100644"
		switch header.Mode {
		case 0o644:
		case 0o755:
			mode = "100755"
		default:
			return nil, fmt.Errorf("application template file %q has unsupported mode %o", header.Name, header.Mode)
		}
		contents, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("read application template file %q: %w", header.Name, err)
		}
		file := repohost.TemplateFile{Path: header.Name, Mode: mode, BlobID: repohost.BlobID(contents), Contents: contents}
		template.Files = append(template.Files, file)
		entries = append(entries, file.TreeEntry())
	}
	if len(template.Files) == 0 {
		return nil, errors.New("application template archive is empty")
	}
	sort.Slice(template.Files, func(i, j int) bool { return template.Files[i].Path < template.Files[j].Path })
	tree, err := treeID(entries)
	if err != nil {
		return nil, fmt.Errorf("application template tree: %w", err)
	}
	template.TreeID = tree
	template.Revision = revisionPrefix + tree
	return template, nil
}

// The tests name these constants.
const (
	RevisionPrefixForTest = revisionPrefix
)
