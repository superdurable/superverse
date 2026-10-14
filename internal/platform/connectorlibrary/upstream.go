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
	"strconv"
	"strings"
	"sync"

	"github.com/superdurable/superversev2/internal/platform/githubapp"
)

// Upstream is the upstream connectors library on GitHub, reached as the
// GitHub App's installation on it, the second client beside the repository
// host's. Every operation mints its own token, restricted to this one
// repository and to the permission it needs: contents write for a push, pull
// requests write for the pull request calls. No token is cached, logged,
// stored or returned in an error.
type Upstream struct {
	App *githubapp.App
	// FullName is owner/name.
	FullName string

	mu       sync.Mutex
	identity *Identity
}

// cloneURL is where Git fetches and pushes the library.
func (upstream *Upstream) cloneURL() string { return gitHubCloneURL(upstream.FullName) }

// The errors of the upstream library, classified as the GitHub repository
// host classifies its own.
var (
	// errOutcomeUnknown marks a request whose answer was lost, so it may have
	// taken effect; the caller reads before it repeats a write.
	errOutcomeUnknown = errors.New("GitHub connectors library request outcome is uncertain")
	// ErrRejected marks a request GitHub refused in a way that asking again
	// cannot change: a missing permission or installation, a repository that
	// does not exist, or an invalid request.
	ErrRejected = errors.New("GitHub refused the connectors library request")
)

// tokenPermissions are the permissions of a token: the one it writes with, if
// any, and the metadata read every installation token holds.
func tokenPermissions(write string) map[string]string {
	permissions := map[string]string{"metadata": "read"}
	if write != "" {
		permissions[write] = "write"
	}
	return permissions
}

// contentsToken is a token that pushes to the library alone.
func (upstream *Upstream) contentsToken(ctx context.Context) (githubapp.Token, error) {
	return upstream.token(ctx, tokenPermissions("contents"))
}

// pullRequestToken is a token that reads and opens the library's pull
// requests alone.
func (upstream *Upstream) pullRequestToken(ctx context.Context) (githubapp.Token, error) {
	return upstream.token(ctx, tokenPermissions("pull_requests"))
}

func (upstream *Upstream) token(ctx context.Context, permissions map[string]string) (githubapp.Token, error) {
	_, name, _ := strings.Cut(upstream.FullName, "/")
	token, err := upstream.App.OperationToken(ctx, []string{name}, permissions)
	if err != nil {
		return githubapp.Token{}, fmt.Errorf("mint a token of %s: %w", upstream.FullName, classify(err))
	}
	return token, nil
}

// botIdentity is the GitHub App's bot as GitHub attributes its commits:
// <slug>[bot] with the noreply address of the bot's user ID. It is read once
// per process.
func (upstream *Upstream) botIdentity(ctx context.Context) (Identity, error) {
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	if upstream.identity != nil {
		return *upstream.identity, nil
	}
	var app struct {
		Slug string `json:"slug"`
	}
	if err := upstream.App.CallAsApp(ctx, http.MethodGet, "/app", &app); err != nil {
		return Identity{}, fmt.Errorf("read the GitHub App: %w", classify(err))
	}
	login := app.Slug + "[bot]"
	var user struct {
		ID int64 `json:"id"`
	}
	// The bot's user is public: a token of no repository reads it.
	token, err := upstream.App.OperationToken(ctx, nil, tokenPermissions(""))
	if err != nil {
		return Identity{}, fmt.Errorf("mint a token of the installation: %w", classify(err))
	}
	if err = upstream.App.CallWithToken(ctx, token, http.MethodGet, "/users/"+url.PathEscape(login), nil, &user); err != nil {
		return Identity{}, fmt.Errorf("read the user of %s: %w", login, classify(err))
	}
	if app.Slug == "" || user.ID <= 0 {
		return Identity{}, fmt.Errorf("GitHub described the App's bot %q without an identity", login)
	}
	upstream.identity = &Identity{Name: login, Email: strconv.FormatInt(user.ID, 10) + "+" + login + "@users.noreply.github.com"}
	return *upstream.identity, nil
}

// PullRequest is a pull request of the library: its number, page and GraphQL
// node ID, whether it is a draft, and its state, open or closed.
type PullRequest struct {
	Number int64  `json:"number"`
	URL    string `json:"html_url"`
	NodeID string `json:"node_id"`
	Draft  bool   `json:"draft"`
	State  string `json:"state"`
}

// PullRequestDraft is the draft pull request of a contribution branch.
type PullRequestDraft struct {
	Branch, Title, Body string
}

// findPullRequest returns the newest pull request, open or closed, whose head
// is branch of the library itself; false when there is none. Every opening
// asks it first, so a lost answer never opens a second pull request.
func (upstream *Upstream) findPullRequest(ctx context.Context, branch string) (PullRequest, bool, error) {
	token, err := upstream.pullRequestToken(ctx)
	if err != nil {
		return PullRequest{}, false, err
	}
	owner, _, _ := strings.Cut(upstream.FullName, "/")
	var listed []PullRequest
	query := "?state=all&per_page=10&head=" + url.QueryEscape(owner+":"+branch)
	if err = upstream.App.CallWithToken(ctx, token, http.MethodGet, "/repos/"+upstream.FullName+"/pulls"+query, nil, &listed); err != nil {
		return PullRequest{}, false, fmt.Errorf("find the pull request of %s: %w", branch, classify(err))
	}
	if len(listed) == 0 {
		return PullRequest{}, false, nil
	}
	return listed[0], true, nil
}

// openDraftPullRequest returns the pull request of draft.Branch, opening it as
// a draft against main when there is none. It looks the branch up first, so a
// repeat after a lost answer finds the pull request the first call opened.
func (upstream *Upstream) openDraftPullRequest(ctx context.Context, draft PullRequestDraft) (PullRequest, error) {
	if found, isFound, err := upstream.findPullRequest(ctx, draft.Branch); err != nil || isFound {
		return found, err
	}
	token, err := upstream.pullRequestToken(ctx)
	if err != nil {
		return PullRequest{}, err
	}
	var created PullRequest
	input := map[string]any{"title": draft.Title, "head": draft.Branch, "base": defaultBranch, "body": draft.Body, "draft": true}
	if err = upstream.App.CallWithToken(ctx, token, http.MethodPost, "/repos/"+upstream.FullName+"/pulls", input, &created); err != nil {
		return PullRequest{}, fmt.Errorf("open the draft pull request of %s: %w", draft.Branch, classify(err))
	}
	return created, nil
}

// classify gives an error of the GitHub App the type callers classify and
// keeps the GitHub error behind it: a lost answer is ErrOutcomeUnknown, a 4xx
// other than 408 and 429 is ErrRejected, and any other error, such as a server
// error, stays untyped and may succeed when asked again.
func classify(err error) error {
	var response *githubapp.HTTPError
	switch {
	case errors.Is(err, githubapp.ErrOutcomeUnknown):
		return fmt.Errorf("%w: %w", errOutcomeUnknown, err)
	case !errors.As(err, &response):
		return err
	case response.Status >= http.StatusBadRequest && response.Status < http.StatusInternalServerError &&
		response.Status != http.StatusRequestTimeout && response.Status != http.StatusTooManyRequests:
		return fmt.Errorf("%w: %w", ErrRejected, err)
	default:
		return err
	}
}

// BotIdentityForTest is botIdentity for the tests.
func (r *Upstream) BotIdentityForTest(ctx context.Context) (Identity, error) {
	return r.botIdentity(ctx)
}
