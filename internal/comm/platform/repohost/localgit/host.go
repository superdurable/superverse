// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package localgit is a Git host inside the server process for the local lanes:
// bare repositories on disk, served over the standard smart HTTP protocol and
// read through a small JSON interface. It is pure Go, because the server image
// has no git, and it stands in for GitHub as MinIO stands in for S3.
package localgit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost/localgit/releases"
)

// credentialLifetime is how long a repository credential works, as long as a
// GitHub installation token.
const credentialLifetime = time.Hour

// Config is what the local Git host needs. Bootstrap validates it.
type Config struct {
	// Dir holds the bare repositories and is shared by the local lanes.
	Dir string
	// CloneBaseURL is the origin, without a path, that the Sandbox clones from.
	CloneBaseURL string
	// ServerBaseURL is the origin, without a path, at which the server itself
	// reaches the host, such as http://127.0.0.1:8085.
	ServerBaseURL string
	// SigningKey signs the repository credentials. It is a secret.
	SigningKey []byte
	// RepositoryPrefix starts the names of the managed repositories of Projects.
	RepositoryPrefix deploymentnames.RepositoryPrefix
	// Environment is the deployment environment that the ownership markers of
	// managed repositories carry.
	Environment deploymentnames.Environment
}

// Host is the local Git host. It implements repohost.Host and, as an
// http.Handler, serves the Git protocol and the JSON interface.
type Host struct {
	httpMu     sync.Mutex
	closing    bool
	requests   sync.WaitGroup
	closeOnce  sync.Once
	closeError error
	config     Config
	store      *store
	releases   *releases.Store
	signer     tokenSigner
	logger     *slog.Logger
	// pushing is the one slot for the objects of a push: the host stores and
	// checks the objects of one push at a time.
	pushing chan struct{}
}

var _ repohost.Host = (*Host)(nil)

// New opens the repository directory of config, creating it when it is new.
func New(config Config, logger *slog.Logger) (*Host, error) {
	repositories, err := newStore(config.Dir)
	if err != nil {
		return nil, err
	}
	assets, err := releases.New(filepath.Join(config.Dir, releasesDir))
	if err != nil {
		_ = repositories.locks.Close()
		return nil, err
	}
	config.CloneBaseURL = strings.TrimRight(config.CloneBaseURL, "/")
	config.ServerBaseURL = strings.TrimRight(config.ServerBaseURL, "/")
	return &Host{config: config, store: repositories, releases: assets, signer: tokenSigner{key: config.SigningKey}, logger: logger,
		pushing: make(chan struct{}, 1)}, nil
}

// Close releases this host's temporary-file owner after all listeners and operations stop.
func (h *Host) Close() error {
	h.closeOnce.Do(func() {
		h.httpMu.Lock()
		h.closing = true
		h.httpMu.Unlock()
		h.requests.Wait()
		h.closeError = h.store.locks.Close()
	})
	return h.closeError
}

// Provider is the local Git host.
func (*Host) Provider() repohost.Provider { return repohost.ProviderLocalGit }

// InstallationID is the fixed identity of the local host's credentials.
func (h *Host) InstallationID() int64 { return installationID }

// cloneURL is the address a Sandbox clones the repository from.
func (h *Host) cloneURL(fullName string) string {
	return h.config.CloneBaseURL + "/" + fullName + repositorySuffix
}

func (h *Host) publish(r repository) repohost.Repository {
	return repohost.Repository{
		ID: r.ID, FullName: r.FullName, CloneURL: h.cloneURL(r.FullName), DefaultBranch: defaultBranch,
		Description: r.Description, Private: r.Private, Archived: r.Archived,
	}
}

// ManagedRepository returns the Project's active managed repository, creating
// it on first use. The host answers from its own disk, so there is no lost
// response to reconcile.
func (h *Host) ManagedRepository(ctx context.Context, setup repohost.SetupIdentity) (repohost.Repository, error) {
	release, err := h.store.locks.Lock(ctx, "creation")
	if err != nil {
		return repohost.Repository{}, err
	}
	defer release()
	found, err := h.findManaged(setup)
	if err != nil {
		return repohost.Repository{}, err
	}
	if found == nil {
		created, createErr := h.store.create(h.managedName(setup), h.managedMarker(setup), true)
		if createErr != nil {
			return repohost.Repository{}, createErr
		}
		return h.publish(created), nil
	}
	if found.Archived {
		return repohost.Repository{}, fmt.Errorf("managed repository %s is archived", found.FullName)
	}
	return h.publish(*found), nil
}

// FindManagedRepository returns the Project's managed repository by its
// deterministic name, or nil when it does not exist; it creates nothing, so
// cleanup can use it.
func (h *Host) FindManagedRepository(ctx context.Context, setup repohost.SetupIdentity) (*repohost.Repository, error) {
	found, err := h.findManaged(setup)
	if err != nil || found == nil {
		return nil, err
	}
	published := h.publish(*found)
	return &published, nil
}

// managedName is the name this host gives the setup's repository.
func (h *Host) managedName(setup repohost.SetupIdentity) string {
	return repohost.ManagedName(h.config.RepositoryPrefix, setup.ProjectName, setup.ProjectID)
}

// managedMarker is the ownership marker of the setup's repository.
func (h *Host) managedMarker(setup repohost.SetupIdentity) string {
	return repohost.ManagedMarker(h.config.Environment, setup.ProjectID, setup.SetupID)
}

// findManaged reads the setup's repository and checks that the setup owns it.
func (h *Host) findManaged(setup repohost.SetupIdentity) (*repository, error) {
	fullName := repositoryOwner + "/" + h.managedName(setup)
	found, err := h.store.find(fullName)
	if errors.Is(err, repohost.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if found.Description != h.managedMarker(setup) || !found.Private {
		return nil, fmt.Errorf("managed repository %s ownership does not match", fullName)
	}
	return &found, nil
}

// ArchiveManaged archives a repository only when this setup's marker owns it. A
// repository that no longer exists needs no archive. An archived repository
// refuses pushes and stays readable. Waiting for a push to finish ends with
// ctx.
func (h *Host) ArchiveManaged(ctx context.Context, projectID, setupID, fullName string) error {
	found, err := h.store.find(fullName)
	if errors.Is(err, repohost.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read managed repository %s before archiving: %w", fullName, err)
	}
	if found.Description != repohost.ManagedMarker(h.config.Environment, projectID, setupID) {
		return fmt.Errorf("refusing to archive repository %s owned by another operation", fullName)
	}
	// A push in flight finishes first, and none starts after the record changes.
	release, err := h.store.lock(ctx, found.ID)
	if err != nil {
		return fmt.Errorf("wait for the pushes of repository %s: %w", fullName, err)
	}
	defer release()
	if found, err = h.store.find(fullName); err != nil {
		return err
	}
	if found.Archived {
		return nil
	}
	found.Archived = true
	return found.save(found.path)
}

// ReadRepository returns the repository named owner/name.
func (h *Host) ReadRepository(_ context.Context, fullName string) (repohost.Repository, error) {
	found, err := h.store.find(fullName)
	if err != nil {
		return repohost.Repository{}, err
	}
	return h.publish(found), nil
}

// connectorLibraryName is the local Git host's copy of the connectors library,
// which the develop subagents of the local lanes push their branches to.
const connectorLibraryName = "dex-connectors-library"

// connectorLibraryMarker is the description of the copy, which no setup owns.
const connectorLibraryMarker = "superverse:connectors-library-copy"

// ConnectorLibraryMirror returns the host's copy of the connectors library,
// creating it on first use, after it fetched the main branch of source, a
// public clone URL, fast-forward only, unless source is empty. The fetch
// holds the copy's lock, so it never interleaves with a push; it ends with
// ctx.
func (h *Host) ConnectorLibraryMirror(ctx context.Context, source string) (repohost.Repository, error) {
	found, err := h.connectorLibrary(ctx)
	if err != nil || source == "" {
		return h.publish(found), err
	}
	release, err := h.store.lock(ctx, found.ID)
	if err != nil {
		return repohost.Repository{}, fmt.Errorf("wait for the pushes of repository %s: %w", found.FullName, err)
	}
	defer release()
	_, storage, err := found.open()
	if err != nil {
		return repohost.Repository{}, err
	}
	defer func() { _ = storage.Close() }()
	remote := git.NewRemote(storage, &config.RemoteConfig{Name: "upstream", URLs: []string{source}})
	main := config.RefSpec("refs/heads/" + defaultBranch + ":refs/heads/" + defaultBranch)
	if err = remote.FetchContext(ctx, &git.FetchOptions{RefSpecs: []config.RefSpec{main}, Tags: git.NoTags}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return repohost.Repository{}, fmt.Errorf("refresh %s from %s: %w", found.FullName, source, err)
	}
	return h.publish(found), nil
}

// releasesDir holds the release assets, beside the owner directory of the
// repositories.
const releasesDir = ".releases"

// connectorLibraryRelease is the release store's name of the copy.
const connectorLibraryRelease = repositoryOwner + "/" + connectorLibraryName

// PublishConnectorLibraryRelease stores the assets of the copy's release tag:
// a release candidate of a contribution of the local lanes. A repeated
// publication succeeds; an asset never changes.
func (h *Host) PublishConnectorLibraryRelease(_ context.Context, tag string, assets map[string][]byte) error {
	return h.releases.Publish(connectorLibraryRelease, tag, assets)
}

// HasConnectorLibraryRelease reports whether the copy's release tag holds
// every named asset.
func (h *Host) HasConnectorLibraryRelease(_ context.Context, tag string, names ...string) (bool, error) {
	return h.releases.Has(connectorLibraryRelease, tag, names...)
}

// ConnectorLibraryReleaseDownloadURL is where the server downloads the copy's
// release assets, GitHub's path below the host's own address.
func (h *Host) ConnectorLibraryReleaseDownloadURL() string {
	return connectorLibraryReleaseDownloadURL(h.config.ServerBaseURL)
}

// connectorLibraryReleaseDownloadURL is the download path of the copy's
// releases below the host's address base.
func connectorLibraryReleaseDownloadURL(base string) string {
	return base + "/" + connectorLibraryRelease + "/releases/download"
}

// ConnectorLibraryReleaseDownloadURLForTest is where a test process reads the
// copy's releases from a host at base, as the server does.
var ConnectorLibraryReleaseDownloadURLForTest = connectorLibraryReleaseDownloadURL

// ConnectorLibraryCloneURL is where a Sandbox reads the copy of the connectors
// library, which serves its reads without a credential.
func (h *Host) ConnectorLibraryCloneURL() string {
	return h.cloneURL(repositoryOwner + "/" + connectorLibraryName)
}

// PushConnectorBranch publishes branch of the copy of the connectors library
// to target, the upstream library on GitHub, with token as its credential,
// without force: a target branch that already holds it is published, and one
// that moved elsewhere refuses the push. The token is used for this push only.
func (h *Host) PushConnectorBranch(ctx context.Context, branch, target, token string) error {
	found, err := h.connectorLibrary(ctx)
	if err != nil {
		return err
	}
	_, storage, err := found.open()
	if err != nil {
		return err
	}
	defer func() { _ = storage.Close() }()
	remote := git.NewRemote(storage, &config.RemoteConfig{Name: "upstream", URLs: []string{target}})
	reference := config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)
	err = remote.PushContext(ctx, &git.PushOptions{RefSpecs: []config.RefSpec{reference}, Auth: &githttp.BasicAuth{Username: gitUsername, Password: token}})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		// go-git reports the remote's answer without the credential.
		return fmt.Errorf("push %s of %s to %s: %w", branch, found.FullName, target, err)
	}
	return nil
}

// connectorLibrary is the copy of the connectors library, created when it is
// missing.
func (h *Host) connectorLibrary(ctx context.Context) (repository, error) {
	release, err := h.store.locks.Lock(ctx, "creation")
	if err != nil {
		return repository{}, err
	}
	defer release()
	found, err := h.store.find(repositoryOwner + "/" + connectorLibraryName)
	switch {
	case errors.Is(err, repohost.ErrNotFound):
		return h.store.create(connectorLibraryName, connectorLibraryMarker, false)
	case err == nil && found.Description != connectorLibraryMarker:
		return repository{}, fmt.Errorf("repository %s is not the copy of the connectors library", found.FullName)
	}
	return found, err
}

// RepositoryCredential mints a credential that reads and pushes exactly one
// repository for an hour.
func (h *Host) RepositoryCredential(_ context.Context, repositoryID int64) (repohost.Credential, error) {
	expiresAt := time.Now().Add(credentialLifetime).Unix()
	return repohost.Credential{
		Token:     h.signer.mint(tokenClaims{Scope: scopeRepository, RepositoryID: repositoryID, ExpiresAt: expiresAt}),
		ExpiresAt: time.Unix(expiresAt, 0),
	}, nil
}
