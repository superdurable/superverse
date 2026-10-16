// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package repohost

import (
	"crypto/sha1" // #nosec G505 -- Git object IDs are SHA-1 by definition.
	"encoding/hex"
	"fmt"
)

// TemplateFile is one file of a Git template and its verified blob identity.
type TemplateFile struct {
	Path     string
	Mode     string
	BlobID   string
	Contents []byte
}

// TreeEntry is one Git tree entry in the GitHub Git Data API shape.
type TreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// TreeEntry returns the Git tree entry for a template file.
func (f TemplateFile) TreeEntry() TreeEntry {
	return TreeEntry{Path: f.Path, Mode: f.Mode, Type: "blob", SHA: f.BlobID}
}

// Template is a prepared, immutable application template.
type Template struct {
	Revision string
	TreeID   string
	Files    []TemplateFile
}

// BlobID hashes the Git object header and bytes, not the contents alone.
func BlobID(contents []byte) string {
	checksum := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(contents))), contents...)) // #nosec G401 -- Git object identity.
	return hex.EncodeToString(checksum[:])
}
