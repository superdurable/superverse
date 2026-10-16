// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectorlibrary

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
)

// A release candidate publishes one contributed connector from its branch head as vX.Y.Z-rc.N, with the assets of a
// release, before maintainers review and release it. The library's Release Connector Candidate workflow publishes
// it on GitHub; in the local lanes the Sandbox builds the assets with the library's own connectorctl and the local
// Git host's copy keeps them, so no lane needs GitHub for it.

// ReleaseCandidate is one release candidate: its module directory, such as connectors/newsapi, its version, its tag
// and the commit the tag names.
type ReleaseCandidate struct {
	Directory string `json:"directory"`
	Version   string `json:"version"`
	Tag       string `json:"tag"`
	Commit    string `json:"commit"`
}

// The assets of a published release candidate; the complete marker is uploaded last.
const (
	ReleaseMetadataAsset       = "connector-release.json"
	releaseMetadataDigestAsset = "connector-release.json.sha256"
	releaseCompleteAsset       = "connector-release.complete"
)

// ErrReleaseCandidateConflict is a candidate whose tag already names another commit.
var ErrReleaseCandidateConflict = errors.New("the release candidate tag names another commit")

// PublicCloneURL is where the official library's releases are read without a
// credential, so a candidate is validated against its connector's latest release.
const PublicCloneURL = "https://github.com/" + publicRepository + ".git"

// releaseCandidateEvent is the repository dispatch that starts the library's Release Connector Candidate workflow.
const releaseCandidateEvent = "connector-release-candidate"

// ReleaseCandidateDownloadURL is where the release candidates of this server's
// contributions are downloaded from: the local Git host's copy in the local
// lanes, the GitHub releases of the upstream library, or of the connectors
// library on a server that names neither.
func (library *Library) ReleaseCandidateDownloadURL() string {
	switch {
	case library.mirror != nil:
		return library.mirror.ConnectorLibraryReleaseDownloadURL()
	case library.upstream == nil:
		return OfficialReleaseDownloadURL
	}
	return "https://github.com/" + library.upstream.FullName + "/releases/download"
}

// PublishLocalReleaseCandidate stores the assets of a candidate that the Sandbox built for the local Git host's copy.
// A repeated publication succeeds.
func (library *Library) PublishLocalReleaseCandidate(ctx context.Context, tag string, assets map[string][]byte) error {
	if library.mirror == nil {
		return ErrNotConfigured
	}
	return library.mirror.PublishConnectorLibraryRelease(ctx, tag, assets)
}

// RequestReleaseCandidate asks for the publication of candidate, whose branch is at location: the upstream library's
// workflow publishes it; the local Git host's copy already holds it. The workflow refuses a tag that names another
// commit and repeats a publication at the same commit, so a repeated request is harmless.
func (library *Library) RequestReleaseCandidate(ctx context.Context, candidate ReleaseCandidate, location Location) error {
	switch {
	case location == LocationLocalGitHost:
		return nil
	case location != LocationUpstream || library.upstream == nil:
		return ErrNotConfigured
	}
	return library.upstream.dispatchReleaseCandidate(ctx, candidate)
}

// IsReleaseCandidatePublished reports whether candidate's release holds its metadata, and on GitHub also the complete
// marker the workflow uploads last; a tag of another commit is ErrReleaseCandidateConflict.
func (library *Library) IsReleaseCandidatePublished(ctx context.Context, candidate ReleaseCandidate, location Location) (bool, error) {
	switch {
	case location == LocationLocalGitHost:
		return library.mirror.HasConnectorLibraryRelease(ctx, candidate.Tag, ReleaseMetadataAsset, releaseMetadataDigestAsset)
	case location != LocationUpstream || library.upstream == nil:
		return false, ErrNotConfigured
	}
	return library.upstream.isReleaseCandidatePublished(ctx, candidate)
}

func (upstream *Upstream) dispatchReleaseCandidate(ctx context.Context, candidate ReleaseCandidate) error {
	token, err := upstream.contentsToken(ctx)
	if err != nil {
		return err
	}
	input := map[string]any{"event_type": releaseCandidateEvent, "client_payload": map[string]string{
		"directory": candidate.Directory, "commit": candidate.Commit, "version": candidate.Version}}
	if err = upstream.App.CallWithToken(ctx, token, http.MethodPost, "/repos/"+upstream.FullName+"/dispatches", input, nil); err != nil {
		return fmt.Errorf("request the release candidate %s: %w", candidate.Tag, classify(err))
	}
	return nil
}

func (upstream *Upstream) isReleaseCandidatePublished(ctx context.Context, candidate ReleaseCandidate) (bool, error) {
	token, err := upstream.contentsToken(ctx)
	if err != nil {
		return false, err
	}
	var tag struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err = upstream.App.CallWithToken(ctx, token, http.MethodGet, "/repos/"+upstream.FullName+"/git/ref/tags/"+escapeTag(candidate.Tag), nil, &tag)
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read the tag %s: %w", candidate.Tag, classify(err))
	}
	if tag.Object.SHA != candidate.Commit {
		return false, fmt.Errorf("%w: %s names %s, not %s", ErrReleaseCandidateConflict, candidate.Tag, tag.Object.SHA, candidate.Commit)
	}
	var release struct {
		Prerelease bool `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	err = upstream.App.CallWithToken(ctx, token, http.MethodGet, "/repos/"+upstream.FullName+"/releases/tags/"+escapeTag(candidate.Tag), nil, &release)
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read the release %s: %w", candidate.Tag, classify(err))
	}
	names := make([]string, 0, len(release.Assets))
	for _, asset := range release.Assets {
		names = append(names, asset.Name)
	}
	return slices.Contains(names, ReleaseMetadataAsset) && slices.Contains(names, releaseCompleteAsset), nil
}

// escapeTag escapes each segment of a tag, whose slashes stay path separators.
func escapeTag(tag string) string {
	return (&url.URL{Path: tag}).EscapedPath()
}

func isNotFound(err error) bool {
	var response *githubapp.HTTPError
	return errors.As(err, &response) && response.Status == http.StatusNotFound
}
