// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package connectorlibrary is the connectors library that Sessions contribute
// to: where a Sandbox fetches it and pushes a contribution branch, the
// identity that commits there, and the draft pull requests of the upstream
// library on GitHub, all with tokens minted per operation.
package connectorlibrary

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

const (
	// publicRepository is the official library, a public repository, which a
	// server without an upstream setting fetches from without a credential.
	publicRepository = "superdurable/dex-connectors-library"
	// ModulePath is the Go module path of the library's root; each connector
	// is the module ModulePath/connectors/<directory>.
	ModulePath = "github.com/superdurable/dex-connectors-library"
	// defaultBranch is the protected branch every contribution starts from and
	// every pull request targets.
	defaultBranch = "main"
)

// Location is where a contribution branch was pushed: the upstream library on
// GitHub, the local Git host's copy of the library in the local lanes, or
// nowhere beyond the Sandbox's own clone, when the server names no upstream
// library and has no local Git host. A struct that string literals cannot
// convert to; its values below are persisted by their names.
type Location struct{ name, description string }

var (
	LocationUpstream     = Location{"upstream", "pushed to the upstream connectors library"}
	LocationLocalGitHost = Location{"local_git_host", "pushed to the local Git host's copy of the connectors library"}
	LocationSandbox      = Location{"sandbox", "kept in the Sandbox's clone only, because this server names no upstream connectors library"}
)

func (location Location) String() string { return location.name }

// Description says in words where the branch is.
func (location Location) Description() string { return location.description }

func (location Location) MarshalText() ([]byte, error) { return []byte(location.name), nil }
func (location *Location) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, location, Location.String, LocationUpstream, LocationLocalGitHost, LocationSandbox)
}

// Remote is where one Git operation fetches the library or pushes a branch:
// the clone URL and, for a host that needs one, a token for exactly that
// repository, minted for this operation. Location is where a push lands; a
// remote of LocationSandbox takes no push.
type Remote struct {
	CloneURL string
	Location Location
	token    string
}

// Token is the secret of the remote, for the trusted Git process only; it is
// empty for an anonymous fetch.
func (remote Remote) Token() string { return remote.token }

// Identity is who authors and commits a contribution: the GitHub App's bot.
type Identity struct {
	Name, Email string
}

// Mirror is the local Git host's copy of the library in the local lanes.
type Mirror interface {
	// ConnectorLibraryMirror creates the copy when it is missing, refreshes its
	// main branch from source, a public GitHub clone URL, fast-forward only,
	// unless source is empty, and returns it.
	ConnectorLibraryMirror(ctx context.Context, source string) (repohost.Repository, error)
	// RepositoryCredential mints the credential of one repository of the host.
	RepositoryCredential(ctx context.Context, repositoryID int64) (repohost.Credential, error)
	// PushConnectorBranch publishes a branch of the copy to target with token,
	// without force.
	PushConnectorBranch(ctx context.Context, branch, target, token string) error
	// PublishConnectorLibraryRelease stores the assets of the copy's release
	// tag; a repeated publication succeeds.
	PublishConnectorLibraryRelease(ctx context.Context, tag string, assets map[string][]byte) error
	// HasConnectorLibraryRelease reports whether the copy's release tag holds
	// every named asset.
	HasConnectorLibraryRelease(ctx context.Context, tag string, names ...string) (bool, error)
	// ConnectorLibraryReleaseDownloadURL is where the server downloads the
	// copy's release assets.
	ConnectorLibraryReleaseDownloadURL() string
}

// ErrNotConfigured is the answer of an operation that needs the upstream
// library on GitHub of a server that names none (github.connectors).
var ErrNotConfigured = errors.New("this server names no upstream connectors library (github.connectors)")

// localIdentity commits a contribution of a server without an upstream
// library, whose branch never reaches GitHub.
var localIdentity = Identity{Name: "Superverse", Email: "bot@superdurable.io"}

// Library is the connectors library of this server: the upstream library on
// GitHub when github.connectors names one, and the local Git host's copy in
// the local lanes, which then takes the pushes. lane is the server's
// repository prefix, which every contribution branch names.
type Library struct {
	upstream *Upstream
	mirror   Mirror
	lane     deploymentnames.RepositoryPrefix
}

// New is the library of a server; upstream and mirror may each be nil.
func New(upstream *Upstream, mirror Mirror, lane deploymentnames.RepositoryPrefix) *Library {
	return &Library{upstream: upstream, mirror: mirror, lane: lane}
}

// Branch is the branch on which the Session of projectID contributes the
// connector slug: superverse/<lane>/<projectID>/<slug>, such as
// superverse/test/a7k2/newsapi. The lanes that share the test environment
// have their own prefixes (localkind, localnative), so they never share one.
func (library *Library) Branch(projectID, slug string) string {
	return "superverse/" + library.lane.String() + "/" + projectID + "/" + slug
}

// OfficialReleaseDownloadURL is where the connectors library publishes its GitHub releases.
const OfficialReleaseDownloadURL = "https://github.com/superdurable/dex-connectors-library/releases/download"

// PublishedCatalogURL is the official catalog of released connectors: the
// latest release of each, never a release candidate.
const PublishedCatalogURL = "https://superdurable.github.io/dex-connectors-library/catalog.yaml"

// PublishedSourceURL serves the library's files at a release tag:
// <PublishedSourceURL>/<directory>/<version>/<path in the repository>.
const PublishedSourceURL = "https://raw.githubusercontent.com/superdurable/dex-connectors-library/refs/tags"

// OpensPullRequests reports whether the server names an upstream library,
// where contribution branches become pull requests.
func (library *Library) OpensPullRequests() bool { return library.upstream != nil }

// OpenDraftPullRequest returns the pull request of draft.Branch in the
// upstream library, opening it as a draft when there is none. A branch that is
// on the local Git host's copy is first pushed to the upstream library without
// force. A server without an upstream library answers ErrNotConfigured.
func (library *Library) OpenDraftPullRequest(ctx context.Context, draft PullRequestDraft, location Location) (PullRequest, error) {
	if library.upstream == nil {
		return PullRequest{}, ErrNotConfigured
	}
	if location == LocationLocalGitHost {
		token, err := library.upstream.contentsToken(ctx)
		if err != nil {
			return PullRequest{}, err
		}
		if err = library.mirror.PushConnectorBranch(ctx, draft.Branch, library.upstream.cloneURL(), token.Value); err != nil {
			return PullRequest{}, fmt.Errorf("publish %s to %s: %w", draft.Branch, library.upstream.FullName, err)
		}
	}
	return library.upstream.openDraftPullRequest(ctx, draft)
}

// FetchRemote is where a Sandbox fetches the library before it creates a
// branch. In the local lanes the local Git host's copy is refreshed from
// GitHub first; otherwise the upstream library, or the public one, is fetched
// anonymously.
func (library *Library) FetchRemote(ctx context.Context) (Remote, error) {
	if library.mirror != nil {
		return library.mirrorRemote(ctx, true)
	}
	return Remote{CloneURL: library.gitHubCloneURL(), Location: LocationUpstream}, nil
}

// PushRemote is where a contribution branch is pushed: the local Git host's
// copy in the local lanes, the upstream library with a token of contents
// write, or a remote of LocationSandbox, which takes no push, for a server
// with neither.
func (library *Library) PushRemote(ctx context.Context) (Remote, error) {
	switch {
	case library.mirror != nil:
		return library.mirrorRemote(ctx, false)
	case library.upstream == nil:
		return Remote{Location: LocationSandbox}, nil
	}
	token, err := library.upstream.contentsToken(ctx)
	if err != nil {
		return Remote{}, err
	}
	return Remote{CloneURL: library.upstream.cloneURL(), Location: LocationUpstream, token: token.Value}, nil
}

// Committer is the identity that commits a contribution: the GitHub App's bot
// when the server names the upstream library, which only it may push to.
func (library *Library) Committer(ctx context.Context) (Identity, error) {
	if library.upstream == nil {
		return localIdentity, nil
	}
	return library.upstream.botIdentity(ctx)
}

// gitHubCloneURL is the clone URL of the upstream library, or of the public
// library without one.
func (library *Library) gitHubCloneURL() string {
	if library.upstream != nil {
		return library.upstream.cloneURL()
	}
	return gitHubCloneURL(publicRepository)
}

// mirrorRemote is the local Git host's copy, refreshed from GitHub when
// refresh, with a credential of that one repository.
func (library *Library) mirrorRemote(ctx context.Context, refresh bool) (Remote, error) {
	source := ""
	if refresh {
		source = library.gitHubCloneURL()
	}
	mirror, err := library.mirror.ConnectorLibraryMirror(ctx, source)
	if err != nil {
		return Remote{}, err
	}
	credential, err := library.mirror.RepositoryCredential(ctx, mirror.ID)
	if err != nil {
		return Remote{}, err
	}
	if !credential.ExpiresAt.After(time.Now()) {
		return Remote{}, errors.New("the local Git host minted an expired credential")
	}
	return Remote{CloneURL: mirror.CloneURL, Location: LocationLocalGitHost, token: credential.Token}, nil
}

func gitHubCloneURL(fullName string) string { return "https://github.com/" + fullName + ".git" }

// The tests name these constants.
const (
	DefaultBranchForTest = defaultBranch
)
