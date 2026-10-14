// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package hosted

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/platform"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
)

const maxHostedSource = 128 << 20
const maxHostedExpanded = 512 << 20

var errHostedPreparationRejected = errors.New("hosted source preparation was rejected before upload")
var errHostedSourceMissing = errors.New("hosted source upload outcome is unconfirmed")

func (b *Builder) inspectSource(ctx context.Context, in build.BuildRequest) (objectstore.ObjectRef, error) {
	key := b.attemptPrefix(in) + "source.zip"
	data, _, version, err := b.getObject(ctx, key, "", maxHostedSource)
	if platform.HasAWSErrorCode(err, "NoSuchKey", "NotFound") {
		return objectstore.ObjectRef{}, fmt.Errorf("%w: %w", errHostedSourceMissing, err)
	}
	if err != nil {
		return objectstore.ObjectRef{}, fmt.Errorf("read the source archive: %w", err)
	}
	return objectstore.ObjectRef{Key: key, Version: version, Digest: objectstore.Digest(data), MediaType: objectstore.MediaTypeZip}, nil
}

// prepareSource downloads only the admitted GitHub repository commit. The token
// stays in this request boundary, never a ZIP, Flow payload, build environment,
// log, process argument, source URL, or CodeBuild source credential.
func (b *Builder) prepareSource(ctx context.Context, in build.BuildRequest, token string) (objectstore.ObjectRef, error) {
	key := b.attemptPrefix(in) + "source.zip"
	if existing, _, version, err := b.getObject(ctx, key, "", maxHostedSource); err == nil {
		return objectstore.ObjectRef{Key: key, Version: version, Digest: objectstore.Digest(existing), MediaType: objectstore.MediaTypeZip}, nil
	} else if !platform.HasAWSErrorCode(err, "NoSuchKey", "NotFound") {
		return objectstore.ObjectRef{}, fmt.Errorf("%w: read the source archive: %w", errHostedPreparationRejected, err)
	}
	client := &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) > 2 || request.URL.Scheme != "https" || request.URL.User != nil || (request.URL.Host != "api.github.com" && request.URL.Host != "codeload.github.com") {
			return errors.New("source redirect rejected")
		}
		request.Header.Del("Authorization")
		return nil
	}}
	// The repository and commit were validated with the request, so the URL
	// always parses.
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+in.RepositoryFullName+"/zipball/"+in.SourceCommit, nil))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := client.Do(request)
	if err != nil {
		return objectstore.ObjectRef{}, fmt.Errorf("%w: download the source archive: %w", errHostedPreparationRejected, withoutURL(err))
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return objectstore.ObjectRef{}, fmt.Errorf("%w: download the source archive: GitHub answered %s", errHostedPreparationRejected, response.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxHostedSource+1))
	if err != nil {
		return objectstore.ObjectRef{}, fmt.Errorf("%w: download the source archive: %w", errHostedPreparationRejected, withoutURL(err))
	}
	if len(raw) > maxHostedSource {
		return objectstore.ObjectRef{}, fmt.Errorf("%w: the source archive exceeds %d bytes", errHostedPreparationRejected, maxHostedSource)
	}
	archive, err := normalizeHostedArchive(raw)
	if err != nil {
		return objectstore.ObjectRef{}, fmt.Errorf("%w: %w", errHostedPreparationRejected, err)
	}
	ref, err := b.putObject(ctx, key, objectstore.MediaTypeZip, archive, "")
	if err != nil {
		// Immutable source upload may have succeeded despite a lost response. Joining
		// it is safe; dispatch still requires its separate admission CAS below.
		observed, _, version, readErr := b.getObject(ctx, key, "", maxHostedSource)
		if readErr == nil && bytes.Equal(observed, archive) {
			return objectstore.ObjectRef{Key: key, Version: version, Digest: objectstore.Digest(archive), MediaType: objectstore.MediaTypeZip}, nil
		}
	}
	return ref, err
}

// normalizeHostedArchive rewrites GitHub's archive of one commit as a ZIP of
// its regular files under their repository paths.
func normalizeHostedArchive(raw []byte) ([]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("read the source archive: %w", err)
	}
	if len(archive.File) == 0 || len(archive.File) > 20000 {
		return nil, fmt.Errorf("source archive: its %d entries are outside 1..20000", len(archive.File))
	}
	var root string
	seen := map[string]bool{}
	files := map[string]*zip.File{}
	var expanded uint64
	for _, entry := range archive.File {
		name := entry.Name
		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 || parts[0] == "" || strings.ContainsAny(name, "\\\x00\r\n") || strings.HasPrefix(name, "/") {
			return nil, sourceEntryError(name, "is not below one archive root")
		}
		if root == "" {
			root = parts[0]
		}
		if root != parts[0] {
			return nil, sourceEntryError(name, "has a second archive root")
		}
		relative := strings.TrimSuffix(parts[1], "/")
		if relative == "" && entry.FileInfo().IsDir() {
			continue
		}
		if relative == "" || relative != path.Clean(relative) || relative == ".." || strings.HasPrefix(relative, "../") || relative == ".git" || strings.HasPrefix(relative, ".git/") || seen[relative] {
			return nil, sourceEntryError(name, "is not a unique clean repository path")
		}
		seen[relative] = true
		if entry.FileInfo().IsDir() {
			continue
		}
		if !entry.Mode().IsRegular() {
			return nil, sourceEntryError(name, "is not a regular file")
		}
		if entry.UncompressedSize64 > maxHostedSource {
			return nil, sourceEntryError(name, fmt.Sprintf("exceeds %d bytes", maxHostedSource))
		}
		expanded += entry.UncompressedSize64
		if expanded > maxHostedExpanded {
			return nil, fmt.Errorf("source archive: its files exceed %d bytes", maxHostedExpanded)
		}
		files[relative] = entry
	}
	for _, name := range []string{"go.mod", "dex-app.yaml"} {
		if files[name] == nil {
			return nil, fmt.Errorf("source archive: the required %s is absent", name)
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range names {
		entry := files[name]
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(entry.Mode().Perm() & 0755)
		// The writer only buffers into memory.
		destination := comm.Must(writer.CreateHeader(header))
		source, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("open source entry %s: %w", name, err)
		}
		size := entry.FileInfo().Size()
		n, err := io.Copy(destination, io.LimitReader(source, size+1))
		if closeErr := source.Close(); err == nil {
			err = closeErr
		}
		if err == nil && n != size {
			err = fmt.Errorf("read %d of %d bytes", n, size)
		}
		if err != nil {
			return nil, fmt.Errorf("copy source entry %s: %w", name, err)
		}
	}
	comm.MustDo(writer.Close())
	if output.Len() > maxHostedSource {
		return nil, fmt.Errorf("source archive: the normalized archive exceeds %d bytes", maxHostedSource)
	}
	return output.Bytes(), nil
}

// sourceEntryError rejects one archive entry. Its name is repository content,
// never a credential, quoted with a bounded length.
func sourceEntryError(name, problem string) error {
	if len(name) > 256 {
		name = strings.ToValidUTF8(name[:256], "") + "..."
	}
	return fmt.Errorf("source archive: entry %q %s", name, problem)
}

// withoutURL drops the URL of an HTTP client error, because GitHub redirects
// an archive download to a URL whose query carries a temporary access token.
func withoutURL(err error) error {
	var request *url.Error
	if errors.As(err, &request) {
		return fmt.Errorf("%s request: %w", request.Op, request.Err)
	}
	return err
}
