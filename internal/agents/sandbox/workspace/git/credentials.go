// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/platform/repohost"
)

type tokenRequest struct {
	installationID int64
	repositoryID   int64
}

const maximumTokenBytes = 4096

// repositoryToken is a validated, short-lived repository token of the Git host. It
// never shows the token through formatting.
type repositoryToken struct {
	// token is the secret, only for a trusted Git subprocess adapter.
	token string
	// expiresAt is the provider expiry that schedules a refresh.
	expiresAt time.Time
}

// newRepositoryToken validates token material from the Git host; an expired token is refused.
func newRepositoryToken(token string, expiresAt, now time.Time) (repositoryToken, error) {
	if token == "" || len(token) > maximumTokenBytes || !utf8.ValidString(token) || strings.TrimSpace(token) != token {
		return repositoryToken{}, errors.New("repository credential token is invalid")
	}
	for _, character := range token {
		if character < 0x21 || character > 0x7e {
			return repositoryToken{}, errors.New("repository credential token must contain visible ASCII only")
		}
	}
	if !expiresAt.After(now) {
		return repositoryToken{}, errors.New("repository credential is expired")
	}
	return repositoryToken{token: token, expiresAt: expiresAt.UTC()}, nil
}

func (repositoryToken) String() string   { return "RepositoryCredential[REDACTED]" }
func (repositoryToken) GoString() string { return "RepositoryCredential[REDACTED]" }

const (
	// credentialRefreshMargin retires a cached token before the host does, so a Git
	// subprocess never starts with a token about to expire.
	credentialRefreshMargin = 5 * time.Minute
	// maximumCachedCredentials bounds the process-local cache.
	maximumCachedCredentials = 256
)

// HostCredentials mints repository-scoped tokens from the repository host alone.
// It reads no Flow: the lease a Session accepted already names the admitted
// installation and repository, and the host restricts each token to that one
// repository.
type HostCredentials struct {
	Host repohost.Host

	mu     sync.Mutex
	cached map[int64]repositoryToken
}

// repositoryCredential returns a token for exactly the requested repository of
// the configured installation. Tokens stay in memory only and are never logged.
func (c *HostCredentials) repositoryCredential(ctx context.Context, request tokenRequest) (repositoryToken, error) {
	// The lease is persisted data: it must name a repository of this host's
	// installation. Another host's repository is repohost.ErrOtherHost, a
	// refusal that names the host to run the server with.
	if request.repositoryID <= 0 {
		return repositoryToken{}, errors.New("repository scope mismatch")
	}
	if err := repohost.CheckInstallation(c.Host, request.installationID); err != nil {
		return repositoryToken{}, fmt.Errorf("repository %d: %w", request.repositoryID, err)
	}
	now := time.Now()
	c.mu.Lock()
	credential, found := c.cached[request.repositoryID]
	c.mu.Unlock()
	if found && credential.expiresAt.After(now.Add(credentialRefreshMargin)) {
		return credential, nil
	}
	// Mint outside the lock: one slow host request must not block other
	// repositories. Concurrent mints for one repository are both valid tokens.
	minted, err := c.Host.RepositoryCredential(ctx, request.repositoryID)
	if err != nil {
		return repositoryToken{}, err
	}
	credential, err = newRepositoryToken(minted.Token, minted.ExpiresAt, now)
	if err != nil {
		return repositoryToken{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached == nil {
		c.cached = make(map[int64]repositoryToken)
	}
	if _, replacing := c.cached[request.repositoryID]; !replacing && len(c.cached) >= maximumCachedCredentials {
		c.evict(now)
	}
	c.cached[request.repositoryID] = credential
	return credential, nil
}

// evict drops every token that is no longer reusable and, when the cache is still
// full, the one expiring first. The caller holds the lock.
func (c *HostCredentials) evict(now time.Time) {
	var oldest int64
	var oldestExpiry time.Time
	for repositoryID, credential := range c.cached {
		expiry := credential.expiresAt
		if !expiry.After(now.Add(credentialRefreshMargin)) {
			delete(c.cached, repositoryID)
			continue
		}
		if oldestExpiry.IsZero() || expiry.Before(oldestExpiry) {
			oldest, oldestExpiry = repositoryID, expiry
		}
	}
	if len(c.cached) >= maximumCachedCredentials {
		delete(c.cached, oldest)
	}
}
