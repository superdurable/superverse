// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package memberdao

import (
	"context"
	"database/sql"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
)

// MemberRow is one team_space_members row with the member's email from
// accounts. Dex writes the row from a Members map instance, so every column
// but the key may be NULL.
type MemberRow struct {
	AccountID        string
	Email, Role      sql.Null[string]
	AddedAt          sql.Null[time.Time]
	AddedByAccountID sql.Null[string]
}

// ProjectMemberRow is a Project member and whether the account is still a
// member of the Project's TeamSpace, without which its role grants nothing.
type ProjectMemberRow struct {
	MemberRow
	TeamSpaceMember bool
}

// MemberAccount is an account's email and whether it is a member of a
// TeamSpace.
type MemberAccount struct {
	Email           string
	TeamSpaceMember bool
}

// OwnedProject is a Project in which an account holds the only owner role
// that counts.
type OwnedProject struct {
	ProjectID string
	Name      sql.Null[string]
}

// MemberDAO reads the member child tables, which only Dex writes.
type MemberDAO struct {
	db *sql.DB
}

// New returns the DAO of db.
func New(db *sql.DB) *MemberDAO { return &MemberDAO{db: db} }

// TeamSpaceMembers returns the members of a TeamSpace in the order they were
// added.
func (d *MemberDAO) TeamSpaceMembers(ctx context.Context, teamSpaceID string) ([]MemberRow, error) {
	return postgres.QueryRows(ctx, d.db, "list the TeamSpace members", func(scan func(...any) error) (MemberRow, error) {
		var m MemberRow
		err := scan(m.destinations()...)
		return m, err
	}, `SELECT m.account_id, a.canonical_email, m.role, m.added_at, m.added_by_account_id
		FROM team_space_members m LEFT JOIN accounts a ON a.account_id = m.account_id
		WHERE m.team_space_id = $1 ORDER BY m.added_at, m.account_id`, teamSpaceID)
}

// ProjectMembers returns the members of a Project outside the excluded
// status in the order they were added, each with whether it is still a member
// of the Project's TeamSpace.
func (d *MemberDAO) ProjectMembers(ctx context.Context, projectID, excludedProjectStatus string) ([]ProjectMemberRow, error) {
	return postgres.QueryRows(ctx, d.db, "list the Project members", func(scan func(...any) error) (ProjectMemberRow, error) {
		var m ProjectMemberRow
		err := scan(append(m.destinations(), &m.TeamSpaceMember)...)
		return m, err
	}, `SELECT m.account_id, a.canonical_email, m.role, m.added_at, m.added_by_account_id, t.account_id IS NOT NULL
		FROM project_members m
		JOIN projects p ON p.project_id = m.project_id
		LEFT JOIN accounts a ON a.account_id = m.account_id
		LEFT JOIN team_space_members t ON t.team_space_id = p.team_space_id AND t.account_id = m.account_id
		WHERE m.project_id = $1 AND p.project_status IS NOT NULL AND p.project_status <> $2
		ORDER BY m.added_at, m.account_id`, projectID, excludedProjectStatus)
}

// MemberAccounts returns, by AccountID, the email of each of the accounts
// that exists and whether it is a member of the TeamSpace.
func (d *MemberDAO) MemberAccounts(ctx context.Context, teamSpaceID string, accountIDs []string) (map[string]MemberAccount, error) {
	type accountRow struct {
		accountID string
		MemberAccount
	}
	rows, err := postgres.QueryRows(ctx, d.db, "read the member accounts", func(scan func(...any) error) (accountRow, error) {
		var row accountRow
		err := scan(&row.accountID, &row.Email, &row.TeamSpaceMember)
		return row, err
	}, `SELECT a.account_id, a.canonical_email, t.account_id IS NOT NULL FROM accounts a
		LEFT JOIN team_space_members t ON t.team_space_id = $1 AND t.account_id = a.account_id
		WHERE a.account_id = ANY($2)`, teamSpaceID, accountIDs)
	if err != nil {
		return nil, err
	}
	accounts := make(map[string]MemberAccount, len(rows))
	for _, row := range rows {
		accounts[row.accountID] = row.MemberAccount
	}
	return accounts, nil
}

// TeamSpaceRole returns the account's role in a TeamSpace outside the
// excluded status, or an empty role when the account is not a member.
// postgres.ErrNotFound is a TeamSpace the read model does not hold yet or excludes.
func (d *MemberDAO) TeamSpaceRole(ctx context.Context, teamSpaceID, accountID, excludedStatus string) (string, error) {
	var role sql.Null[string]
	err := d.db.QueryRowContext(ctx, `SELECT m.role FROM team_spaces s
		LEFT JOIN team_space_members m ON m.team_space_id = s.team_space_id AND m.account_id = $2
		WHERE s.team_space_id = $1 AND s.team_space_status IS NOT NULL AND s.team_space_status <> $3`,
		teamSpaceID, accountID, excludedStatus).Scan(&role)
	return role.V, postgres.RowError("read the TeamSpace role", err)
}

// SoleOwnedProjects returns at most limit Projects of the TeamSpace, outside
// the excluded status, in which the account holds ownerRole and no other
// member of the TeamSpace does: removing the account from the TeamSpace would
// leave them without an owner whose role counts.
func (d *MemberDAO) SoleOwnedProjects(ctx context.Context, teamSpaceID, accountID, ownerRole, excludedProjectStatus string, limit int) ([]OwnedProject, error) {
	return postgres.QueryRows(ctx, d.db, "find the Projects the member solely owns", func(scan func(...any) error) (OwnedProject, error) {
		var p OwnedProject
		err := scan(&p.ProjectID, &p.Name)
		return p, err
	}, `SELECT p.project_id, p.name FROM projects p
		JOIN project_members me ON me.project_id = p.project_id AND me.account_id = $2 AND me.role = $3
		WHERE p.team_space_id = $1 AND p.project_status IS NOT NULL AND p.project_status <> $4
			AND NOT EXISTS (SELECT FROM project_members o
				JOIN team_space_members t ON t.team_space_id = p.team_space_id AND t.account_id = o.account_id
				WHERE o.project_id = p.project_id AND o.role = $3 AND o.account_id <> $2)
		ORDER BY p.created_at DESC, p.project_id DESC LIMIT $5`, teamSpaceID, accountID, ownerRole, excludedProjectStatus, limit)
}

// RetainedProjectRoles counts the Projects of the TeamSpace, outside the
// excluded status, in which the account holds a role: the roles that count
// again when the account rejoins the TeamSpace.
func (d *MemberDAO) RetainedProjectRoles(ctx context.Context, teamSpaceID, accountID, excludedProjectStatus string) (int32, error) {
	var count int32
	err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM project_members m
		JOIN projects p ON p.project_id = m.project_id
		WHERE p.team_space_id = $1 AND m.account_id = $2 AND p.project_status IS NOT NULL AND p.project_status <> $3`,
		teamSpaceID, accountID, excludedProjectStatus).Scan(&count)
	return count, postgres.RowError("count the retained Project roles", err)
}

// destinations are the scan targets of a member row's first five columns.
func (m *MemberRow) destinations() []any {
	return []any{&m.AccountID, &m.Email, &m.Role, &m.AddedAt, &m.AddedByAccountID}
}
