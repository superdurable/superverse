// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package github is the GitHub implementation of repohost.Host: the managed
// repositories of Projects, their source reads and their credentials, all
// through the GitHub REST API as one installation of the GitHub App.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

// Client acts as one GitHub App installation. Owner is the organization that
// holds managed repositories, RepositoryPrefix starts the names of the
// repositories it creates and Environment is the deployment environment that
// their ownership markers carry; bootstrap validates every field at startup.
type Client struct {
	App              *githubapp.App
	Owner            string
	RepositoryPrefix deploymentnames.RepositoryPrefix
	Environment      deploymentnames.Environment
}

var _ repohost.Host = (*Client)(nil)

// Provider is GitHub.
func (*Client) Provider() repohost.Provider { return repohost.ProviderGitHub }

// InstallationID is the installation that owns the repositories.
func (c *Client) InstallationID() int64 { return c.App.InstallationID }

// RepositoryCredential mints a token restricted to the contents of one
// repository, for the coding tools and builds of that repository.
func (c *Client) RepositoryCredential(ctx context.Context, repositoryID int64) (repohost.Credential, error) {
	token, err := c.App.RepositoryToken(ctx, repositoryID)
	if err != nil {
		return repohost.Credential{}, translate(err)
	}
	return repohost.Credential{Token: token.Value, ExpiresAt: token.ExpiresAt}, nil
}

// repositoryRequest calls the endpoint suffix of one owner/name repository as
// the installation.
func (c *Client) repositoryRequest(ctx context.Context, method, fullName, suffix string, input, output any) error {
	path, err := repositoryPath(fullName)
	if err != nil {
		return err
	}
	return c.installationRequest(ctx, method, path+suffix, input, output)
}

func (c *Client) installationRequest(ctx context.Context, method, path string, input, output any) error {
	return translate(c.App.Call(ctx, method, path, input, output))
}

func repositoryPath(fullName string) (string, error) {
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("repository must be owner/name")
	}
	for _, part := range parts {
		if strings.Trim(part, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-") != "" || part == "." || part == ".." {
			return "", errors.New("invalid repository name")
		}
	}
	return "/repos/" + parts[0] + "/" + parts[1], nil
}

// translate gives an error of the GitHub App the host-independent type that
// callers classify, and keeps the GitHub error reachable behind it. A server
// error stays untyped: the caller's retry policy asks again.
func translate(err error) error {
	var response *githubapp.HTTPError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, githubapp.ErrOutcomeUnknown):
		return fmt.Errorf("%w: %w", repohost.ErrOutcomeUnknown, err)
	case !errors.As(err, &response):
		return err
	case response.Status == http.StatusNotFound:
		return fmt.Errorf("%w: %w", repohost.ErrNotFound, err)
	case response.Status == http.StatusUnauthorized || response.Status == http.StatusForbidden:
		return fmt.Errorf("%w: %w", repohost.ErrPermissionDenied, err)
	case response.Status >= http.StatusBadRequest && response.Status < http.StatusInternalServerError:
		return fmt.Errorf("%w: %w", repohost.ErrRejected, err)
	default:
		return err
	}
}
