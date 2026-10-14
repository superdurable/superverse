// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/superdurable/superversev2/internal/platform/githubapp"
	"github.com/superdurable/superversev2/internal/platform/repohost"
)

// ManagedRepository returns the Project's active managed repository, creating
// it on first use. The create response carries the new repository, which a
// read right after the create may not see yet. A lost create response, or a
// name an earlier attempt already took, is reconciled by reading it.
func (c *Client) ManagedRepository(ctx context.Context, setup repohost.SetupIdentity) (repohost.Repository, error) {
	found, err := c.FindManagedRepository(ctx, setup)
	if err != nil {
		return repohost.Repository{}, err
	}
	if found == nil {
		name, marker := c.managedName(setup), repohost.ManagedMarker(c.Environment, setup.ProjectID, setup.SetupID)
		var created repositoryResponse
		createErr := c.installationRequest(ctx, http.MethodPost, "/orgs/"+url.PathEscape(c.Owner)+"/repos", map[string]any{"name": name, "private": true, "auto_init": false, "description": marker}, &created)
		if createErr == nil {
			if err = c.verifyManaged(created.repository(), setup); err != nil {
				return repohost.Repository{}, err
			}
			return created.repository(), nil
		}
		if found, err = c.FindManagedRepository(ctx, setup); err != nil || found == nil {
			return repohost.Repository{}, fmt.Errorf("managed repository %s/%s creation is unconfirmed: %w", c.Owner, name, errors.Join(createErr, err))
		}
	}
	if found.Archived {
		return repohost.Repository{}, fmt.Errorf("managed repository %s is archived", found.FullName)
	}
	return *found, nil
}

// FindManagedRepository returns the Project's managed repository by its
// deterministic name, or nil when it does not exist; it creates nothing, so
// cleanup can use it.
func (c *Client) FindManagedRepository(ctx context.Context, setup repohost.SetupIdentity) (*repohost.Repository, error) {
	name := c.managedName(setup)
	observed, err := c.ReadRepository(ctx, c.Owner+"/"+name)
	if errors.Is(err, repohost.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read managed repository %s/%s: %w", c.Owner, name, err)
	}
	if err = c.verifyManaged(observed, setup); err != nil {
		return nil, err
	}
	return &observed, nil
}

// managedName is the name this host gives the setup's repository.
func (c *Client) managedName(setup repohost.SetupIdentity) string {
	return repohost.ManagedName(c.RepositoryPrefix, setup.ProjectName, setup.ProjectID)
}

// verifyManaged checks that a repository is the setup's own private one,
// named and marked for it.
func (c *Client) verifyManaged(observed repohost.Repository, setup repohost.SetupIdentity) error {
	name := c.managedName(setup)
	if observed.FullName != c.Owner+"/"+name || !observed.Private || observed.Description != repohost.ManagedMarker(c.Environment, setup.ProjectID, setup.SetupID) {
		return fmt.Errorf("managed repository %s/%s ownership does not match", c.Owner, name)
	}
	return nil
}

// ArchiveManaged archives a repository only when this setup's marker owns it.
// A repository that no longer exists needs no archive. A server error, a
// conflict or a missing repository on the archive request itself leaves the
// archive unconfirmed: the caller reads again before it repeats the request.
func (c *Client) ArchiveManaged(ctx context.Context, projectID, setupID, fullName string) error {
	repository, err := c.ReadRepository(ctx, fullName)
	if errors.Is(err, repohost.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read managed repository %s before archiving: %w", fullName, archiveOutcome(err))
	}
	if repository.Description != repohost.ManagedMarker(c.Environment, projectID, setupID) {
		return fmt.Errorf("refusing to archive repository %s owned by another operation", fullName)
	}
	if repository.Archived {
		return nil
	}
	return archiveOutcome(c.repositoryRequest(ctx, http.MethodPatch, fullName, "", map[string]any{"archived": true}, nil))
}

// archiveOutcome marks the failures of an archive request whose effect is unknown
// as repohost.ErrOutcomeUnknown: a server error, a conflict, and the 404 that
// follows a repository the request may just have changed.
func archiveOutcome(err error) error {
	var response *githubapp.HTTPError
	if errors.As(err, &response) && (response.Status >= http.StatusInternalServerError || response.Status == http.StatusConflict ||
		response.Status == http.StatusNotFound && response.Method == http.MethodPatch) {
		return fmt.Errorf("%w: %w", repohost.ErrOutcomeUnknown, err)
	}
	return err
}
