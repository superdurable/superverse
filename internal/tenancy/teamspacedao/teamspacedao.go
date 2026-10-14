// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package teamspacedao

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/superdurable/superversev2/internal/platform/postgres"
)

// TeamSpaceRow is one team_spaces row with the caller's role in it. Dex writes
// the row from TeamSpaceFlow's synced Attributes, so every column but the key
// may be NULL; CallerRole is NULL when the caller is not a member. LastFailure
// is the Attribute's JSON encoding, which the owning package decodes.
type TeamSpaceRow struct {
	TeamSpaceID                             string
	Name, TeamSpaceStatus, CreatorAccountID sql.Null[string]
	Revision                                sql.Null[int64]
	LastFailure                             json.RawMessage
	CreatedAt, UpdatedAt                    sql.Null[time.Time]
	CallerRole                              sql.Null[string]
}

// TeamSpaceDetail is a TeamSpace with the counts its Home shows: its members,
// and its Projects and those with Live, leaving out a Project status the
// caller excludes.
type TeamSpaceDetail struct {
	TeamSpaceRow
	MemberCount, ProjectCount, LiveProjectCount, ConnectionCount int32
}

// TeamSpaceDAO reads the team_spaces table, which only Dex writes.
type TeamSpaceDAO struct {
	db *sql.DB
}

// New returns the DAO of db.
func New(db *sql.DB) *TeamSpaceDAO { return &TeamSpaceDAO{db: db} }

const teamSpaceColumns = `s.team_space_id, s.name, s.team_space_status, s.creator_account_id, s.revision, s.last_failure,
	s.created_at, s.updated_at, m.role`

// ListTeamSpaces returns a page of at most limit TeamSpaces that TeamSpaceFlow
// has written, newest first, after the page key when one is given, and the key
// of the next page: those the account is a member of, or every one when all is
// set. It leaves out the excluded status and keeps only the given statuses
// unless there are none.
func (d *TeamSpaceDAO) ListTeamSpaces(ctx context.Context, accountID string, all bool, excludedStatus string, statuses []string, after *postgres.PageKey, limit int) ([]TeamSpaceRow, *postgres.PageKey, error) {
	afterCreatedAt, afterTeamSpaceID := postgres.AfterValues(after)
	rows, err := postgres.QueryRows(ctx, d.db, "list the TeamSpaces", scanTeamSpace, `SELECT `+teamSpaceColumns+` FROM team_spaces s
		LEFT JOIN team_space_members m ON m.team_space_id = s.team_space_id AND m.account_id = $1
		WHERE ($2 OR m.account_id IS NOT NULL)
			AND s.team_space_status IS NOT NULL AND s.team_space_status <> $3 AND s.created_at IS NOT NULL
			AND (coalesce(cardinality($4::text[]), 0) = 0 OR s.team_space_status = ANY($4))
			AND ($5::timestamptz IS NULL OR (s.created_at, s.team_space_id) < ($5, $6))
		ORDER BY s.created_at DESC, s.team_space_id DESC LIMIT $7`,
		accountID, all, excludedStatus, statuses, afterCreatedAt, afterTeamSpaceID, limit+1)
	if err != nil {
		return nil, nil, err
	}
	page, next := postgres.Page(rows, limit, func(s TeamSpaceRow) postgres.PageKey {
		return postgres.PageKey{CreatedAt: s.CreatedAt.V, ID: s.TeamSpaceID}
	})
	return page, next, nil
}

// TeamSpace returns a TeamSpace that TeamSpaceFlow has written and is not in
// the excluded status, with the account's role and the counts of its Home,
// which leave out Projects in the excluded Project status and count only
// connections in the active connection status. A missing or excluded row is
// postgres.ErrNotFound.
func (d *TeamSpaceDAO) TeamSpace(ctx context.Context, teamSpaceID, accountID, excludedStatus, excludedProjectStatus, activeConnectionStatus string) (TeamSpaceDetail, error) {
	var detail TeamSpaceDetail
	err := d.db.QueryRowContext(ctx, `SELECT `+teamSpaceColumns+`,
			(SELECT count(*) FROM team_space_members c WHERE c.team_space_id = s.team_space_id),
			(SELECT count(*) FROM projects p WHERE p.team_space_id = s.team_space_id
				AND p.project_status IS NOT NULL AND p.project_status <> $4),
			(SELECT count(*) FROM projects p WHERE p.team_space_id = s.team_space_id
				AND p.project_status IS NOT NULL AND p.project_status <> $4 AND p.current_live IS NOT NULL),
			(SELECT count(*) FROM team_space_connections c WHERE c.team_space_id = s.team_space_id AND c.connection_status = $5)
		FROM team_spaces s
		LEFT JOIN team_space_members m ON m.team_space_id = s.team_space_id AND m.account_id = $2
		WHERE s.team_space_id = $1 AND s.team_space_status IS NOT NULL AND s.team_space_status <> $3`,
		teamSpaceID, accountID, excludedStatus, excludedProjectStatus, activeConnectionStatus).Scan(
		append(detail.destinations(), &detail.MemberCount, &detail.ProjectCount, &detail.LiveProjectCount, &detail.ConnectionCount)...)
	if err = postgres.RowError("read the TeamSpace", err); err != nil {
		return TeamSpaceDetail{}, err
	}
	return detail, nil
}

// destinations are the scan targets of teamSpaceColumns.
func (s *TeamSpaceRow) destinations() []any {
	return []any{&s.TeamSpaceID, &s.Name, &s.TeamSpaceStatus, &s.CreatorAccountID, &s.Revision, postgres.JSONColumn(&s.LastFailure),
		&s.CreatedAt, &s.UpdatedAt, &s.CallerRole}
}

// scanTeamSpace reads the teamSpaceColumns of one row with scan, the Scan of
// a row or of a result set.
func scanTeamSpace(scan func(...any) error) (TeamSpaceRow, error) {
	var s TeamSpaceRow
	err := scan(s.destinations()...)
	return s, err
}
