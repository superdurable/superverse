// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// errAssetConflict is a publication of an asset that already exists with other contents: an asset never changes.
var errAssetConflict = errors.New("a release asset already exists with other contents")

// maximumAssetBytes bounds one asset, a Connector release's metadata or Studio UI archive.
const maximumAssetBytes = 16 << 20

var (
	// repositoryPattern is owner/name of a repository of the local Git host.
	repositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9._-]*$`)
	// tagPattern is a tag that names a directory and a version, such as connectors/newsapi/v0.1.0-rc.1.
	tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(?:/[a-z0-9][a-z0-9-]*)*/v[0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[1-9][0-9]*)?$`)
	// assetPattern is a release asset's file name, such as connector-release.json.sha256.
	assetPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
)

// Store keeps the release assets of the local Git host's repositories as files below its directory and serves them
// read-only at GitHub's download path, /<owner>/<name>/releases/download/<tag>/<asset>. They are public data, as the
// assets of a public GitHub release are.
type Store struct {
	dir string
}

// New opens the store in dir, creating it when it is new.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: the lanes share this directory with the server Pod's user
		return nil, fmt.Errorf("create the release asset directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Publish stores the assets of repository's release tag. An asset that already exists with the same contents is
// kept, so a repeated publication succeeds; one with other contents is errAssetConflict.
func (s *Store) Publish(repository, tag string, assets map[string][]byte) error {
	release, err := s.releaseDir(repository, tag)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(release, 0o755); err != nil { //nolint:gosec // G301: see New
		return fmt.Errorf("create the release %s of %s: %w", tag, repository, err)
	}
	for name, contents := range assets {
		if !assetPattern.MatchString(name) || len(contents) > maximumAssetBytes {
			return fmt.Errorf("release asset %q of %s is not a valid asset", name, tag)
		}
		path := filepath.Join(release, name)
		existing, readErr := os.ReadFile(path) //nolint:gosec // a validated asset of this store
		switch {
		case readErr == nil && bytes.Equal(existing, contents):
			continue
		case readErr == nil:
			return fmt.Errorf("%w: %s of %s", errAssetConflict, name, tag)
		case !errors.Is(readErr, os.ErrNotExist):
			return fmt.Errorf("read release asset %s of %s: %w", name, tag, readErr)
		}
		if err = writeOnce(path, contents); err != nil {
			return fmt.Errorf("write release asset %s of %s: %w", name, tag, err)
		}
	}
	return nil
}

// Has reports whether repository's release tag holds every named asset.
func (s *Store) Has(repository, tag string, names ...string) (bool, error) {
	release, err := s.releaseDir(repository, tag)
	if err != nil {
		return false, err
	}
	for _, name := range names {
		if _, err = os.Stat(filepath.Join(release, name)); errors.Is(err, os.ErrNotExist) {
			return false, nil
		} else if err != nil {
			return false, err
		}
	}
	return true, nil
}

// ServeHTTP serves GET /<owner>/<name>/releases/download/<tag>/<asset>; any other request is not found.
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repository, tag, asset, ok := parseDownloadPath(r.URL.Path)
	if !ok || r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	release, err := s.releaseDir(repository, tag)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(release, asset)) //nolint:gosec // a validated asset of this store
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "the release asset could not be read", http.StatusInternalServerError)
		return
	}
	defer func() { _ = file.Close() }()
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, asset, fileModTime(file), file)
}

// IsDownloadPath reports whether path is a release download of the store.
func IsDownloadPath(path string) bool {
	_, _, _, ok := parseDownloadPath(path)
	return ok
}

// parseDownloadPath splits /<owner>/<name>/releases/download/<tag>/<asset>, whose tag has slashes of its own.
func parseDownloadPath(path string) (repository, tag, asset string, ok bool) {
	prefix, rest, found := strings.Cut(strings.TrimPrefix(path, "/"), "/releases/download/")
	separator := strings.LastIndex(rest, "/")
	if !found || separator < 0 {
		return "", "", "", false
	}
	repository, tag, asset = prefix, rest[:separator], rest[separator+1:]
	return repository, tag, asset, repositoryPattern.MatchString(repository) && tagPattern.MatchString(tag) && assetPattern.MatchString(asset)
}

// releaseDir is the directory of repository's release tag; the patterns admit no path element that leaves the store.
func (s *Store) releaseDir(repository, tag string) (string, error) {
	if !repositoryPattern.MatchString(repository) || !tagPattern.MatchString(tag) {
		return "", fmt.Errorf("release %q of %q is not a valid release", tag, repository)
	}
	return filepath.Join(s.dir, filepath.FromSlash(repository), filepath.FromSlash(tag)), nil
}

// writeOnce writes contents to path through a temporary file and a link, so a reader never sees a partial asset
// and two writers never both create it.
func writeOnce(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".asset-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if _, err = io.Copy(temporary, bytes.NewReader(contents)); err != nil {
		_ = temporary.Close() // the copy's error is the one reported
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = os.Link(temporary.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	existing, err := os.ReadFile(path) //nolint:gosec // the asset just linked
	if err != nil {
		return err
	}
	if !bytes.Equal(existing, contents) {
		return errAssetConflict
	}
	return nil
}

func fileModTime(file *os.File) (modified time.Time) {
	if info, err := file.Stat(); err == nil {
		modified = info.ModTime()
	}
	return modified
}
