// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// A usage says that a Project scope uses a connection. It authorizes the
// scope's credential reads and keeps the connection from being deleted. A
// Preview's usages are the set its frozen configuration selects; a Live
// deployment adds its version's and never removes any, so a running version
// keeps reading until a release of the unused ones.

// ErrConnectionDeleted is a usage of a connection that is not ACTIVE, which a
// deployment cannot proceed with until its version selects another.
var ErrConnectionDeleted = errors.New("a selected connection was deleted")

// Usages records the connection usages and scope tokens of Projects.
type Usages struct {
	dao     *connectiondao.ConnectionDAO
	objects *objectstore.Store
}

// NewUsages records with dao and deletes TeamSpace objects from objects.
func NewUsages(dao *connectiondao.ConnectionDAO, objects *objectstore.Store) *Usages {
	return &Usages{dao: dao, objects: objects}
}

// addUsage records that a Project scope uses one more connection, as a
// selection does before the scope's configuration names it.
func (u *Usages) addUsage(ctx context.Context, teamSpaceID, connectionID, projectID string, scope infra.ScopeKind) error {
	return u.usages(ctx, teamSpaceID, []string{connectionID}, projectID, scope, false)
}

// ReplaceUsages makes the connections a scope's frozen configuration
// selects its usages, as a Preview start does. Every connection must belong
// to teamSpaceID, the Project's TeamSpace.
func (u *Usages) ReplaceUsages(ctx context.Context, teamSpaceID string, connectionIDs []string, projectID string, scope infra.ScopeKind) error {
	return u.usages(ctx, teamSpaceID, connectionIDs, projectID, scope, true)
}

// EnsureConnectionUsages records the Live usages of the connections a
// deployment's version selects, idempotently, before the deployment switches
// traffic. A deleted connection, or one of another TeamSpace than
// teamSpaceID, is ErrConnectionDeleted.
func (u *Usages) EnsureConnectionUsages(ctx context.Context, teamSpaceID, projectID string, connectionIDs []string) error {
	return u.usages(ctx, teamSpaceID, connectionIDs, projectID, infra.ScopeLive, false)
}

func (u *Usages) usages(ctx context.Context, teamSpaceID string, connectionIDs []string, projectID string, scope infra.ScopeKind, replace bool) error {
	err := u.dao.AddUsages(ctx, teamSpaceID, connectionIDs, projectID, scope.String(), replace)
	if errors.Is(err, connectiondao.ErrUnavailable) {
		return fmt.Errorf("%w (%w)", ErrConnectionDeleted, err)
	}
	return err
}

// RetainTriggerRegistrations keeps only the trigger registrations of a
// Project scope whose connection name still selects the registered
// connection in one of the selected configurations, which select them by
// connection name.
func (u *Usages) RetainTriggerRegistrations(ctx context.Context, projectID string, scope infra.ScopeKind, selected ...map[string]string) error {
	return u.dao.RetainTriggerRegistrations(ctx, projectID, scope.String(), selected...)
}

// ReleaseUnusedLiveUsages removes a Project's Live usages that the current
// Live version does not select and that were recorded before since, the
// database time read before the caller read the current Live version.
func (u *Usages) ReleaseUnusedLiveUsages(ctx context.Context, projectID string, keep []string, since time.Time) (int64, error) {
	return u.dao.ReleaseUnusedUsages(ctx, projectID, infra.ScopeLive.String(), keep, since)
}

// ProjectsUsing returns the Projects whose scope uses one of the given
// connections of a TeamSpace, each Project once.
func (u *Usages) ProjectsUsing(ctx context.Context, teamSpaceID string, connectionIDs []string, scope infra.ScopeKind) ([]string, error) {
	usages, err := u.dao.Usages(ctx, teamSpaceID, connectionIDs)
	if err != nil {
		return nil, err
	}
	var projectIDs []string
	for _, usage := range usages {
		if usage.Scope == scope.String() && !slices.Contains(projectIDs, usage.ProjectID) {
			projectIDs = append(projectIDs, usage.ProjectID)
		}
	}
	return projectIDs, nil
}

// Now is the database time that usages record.
func (u *Usages) Now(ctx context.Context) (time.Time, error) { return u.dao.Now(ctx) }

// ReleaseConnectionUsages removes a deleted Project's usages and scope
// tokens, so its applications can read no credential. It is idempotent.
func (u *Usages) ReleaseConnectionUsages(ctx context.Context, projectID string) error {
	return u.dao.DeleteProjectUsages(ctx, projectID)
}

// DeleteTeamSpaceConnections deletes the connections of a TeamSpace whose
// Projects are deleted: their usages and rows, then a bounded part of their
// objects. It is idempotent, never refused by a usage, and done once nothing
// is left.
func (u *Usages) DeleteTeamSpaceConnections(ctx context.Context, teamSpaceID string) (bool, error) {
	if err := u.dao.DeleteTeamSpaceConnections(ctx, teamSpaceID); err != nil {
		return false, err
	}
	return u.objects.DeletePrefix(ctx, teamSpacePrefix(teamSpaceID))
}

// ScopeToken returns the reference of the secret object that holds a scope's
// token, creating the token on its first use: write writes a new token's
// object and returns its reference. When another writer recorded the scope's
// token first, its reference is returned and the new object is unused.
func (u *Usages) ScopeToken(ctx context.Context, projectID string, scope infra.ScopeKind, write func(token string) (json.RawMessage, error)) (json.RawMessage, error) {
	stored, err := u.dao.ScopeToken(ctx, projectID, scope.String())
	if !errors.Is(err, connectiondao.ErrNotFound) {
		return stored, err
	}
	token, hash := newScopeToken()
	ref, err := write(token)
	if err != nil {
		return nil, err
	}
	return u.dao.RecordScopeToken(ctx, projectID, scope.String(), hash, ref)
}
