// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projectdao

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
)

// ProjectRow is one projects row. Dex writes it from the synced Attributes of
// ProjectFlow and of its ProjectSetupFlow, so every column but the key may be
// NULL. A JSON column holds its Attribute's JSON encoding, nil for NULL,
// which the package that owns the Attribute's type decodes.
type ProjectRow struct {
	ProjectID                                                                         string
	CreatorAccountID, TeamSpaceID, Name, ProjectStatus, TemplateRevision, SetupFlowID sql.Null[string]
	Revision                                                                          sql.Null[int64]
	RepositoryBinding, AppManifest, ActiveDeployment, CurrentLive                     json.RawMessage
	ActiveCommand, LastFailure, SessionStartFailure                                   json.RawMessage
	ProjectStorage, LiveExecution                                                     json.RawMessage
	CreatedAt, UpdatedAt                                                              sql.Null[time.Time]
	SetupStatus, SetupPhase, SetupSourceCommit                                        sql.Null[string]
	SetupFailure                                                                      json.RawMessage
	SetupRevision                                                                     sql.Null[int64]
	// LiveVersion is the version of the Release the Live pointer names, from
	// that Release's prepared source; NULL without Live or before versions
	// were recorded.
	LiveVersion json.RawMessage
}

// ProjectAccess is the row of a Project with what the account that asks for
// it holds: whether it is the platform admin, its role in the Project and its
// role in the Project's TeamSpace, each NULL when it holds none.
type ProjectAccess struct {
	Project                    ProjectRow
	PlatformAdmin              bool
	ProjectRole, TeamSpaceRole sql.Null[string]
}

// ListedProject is a Project of a list with the roles of the account that
// lists it, each NULL when it holds none.
type ListedProject struct {
	Project                    ProjectRow
	ProjectRole, TeamSpaceRole sql.Null[string]
}

// ProjectFilter selects the Projects of one TeamSpace for AccountID, whose
// roles each listed Project carries. Every Project in ExcludedStatus is left
// out.
type ProjectFilter struct {
	AccountID, TeamSpaceID string
	// Statuses selects the Projects in one of them; empty selects every
	// status.
	Statuses       []string
	ExcludedStatus string
}

// ProjectDAO reads the projects table, which only Dex writes.
type ProjectDAO struct {
	db *sql.DB
}

// New returns the DAO of db.
func New(db *sql.DB) *ProjectDAO { return &ProjectDAO{db: db} }

const projectColumns = `p.project_id, p.creator_account_id, p.team_space_id, p.name, p.project_status, p.template_revision, p.setup_flow_id, p.revision,
	p.repository_binding, p.app_manifest, p.active_deployment, p.current_live, p.active_command, p.last_failure, p.session_start_failure,
	p.project_storage, p.live_execution, p.created_at, p.updated_at,
	p.setup_status, p.setup_phase, p.setup_source_commit, p.setup_failure, p.setup_revision, lr.prepared_source->'Version'`

// liveRelease joins the Release a Project's Live pointer names, for its
// version.
const liveRelease = ` LEFT JOIN releases lr ON lr.project_id = p.project_id AND lr.release_id = p.current_live->>'ReleaseID'`

// Project returns the row of a Project that ProjectFlow has written; a row
// that holds only its Setup's columns is postgres.ErrNotFound.
func (d *ProjectDAO) Project(ctx context.Context, projectID string) (ProjectRow, error) {
	var p ProjectRow
	err := d.db.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects p`+liveRelease+`
		WHERE p.project_id = $1 AND p.project_status IS NOT NULL`, projectID).Scan(p.destinations()...)
	return p, postgres.RowError("read the project", err)
}

// AuthorizeProject returns the row of a Project that ProjectFlow has written
// with what accountID holds: whether it is the platform admin, the account
// with platformAdminEmail in activeStatus, and its roles in the Project and in
// the Project's TeamSpace. An unknown account holds nothing.
func (d *ProjectDAO) AuthorizeProject(ctx context.Context, projectID, accountID, platformAdminEmail, activeStatus string) (ProjectAccess, error) {
	var access ProjectAccess
	err := d.db.QueryRowContext(ctx, `SELECT `+projectColumns+`, COALESCE(a.canonical_email = $3 AND a.status = $4, false), pm.role, tm.role
		FROM projects p`+liveRelease+` LEFT JOIN accounts a ON a.account_id = $2
		LEFT JOIN project_members pm ON pm.project_id = p.project_id AND pm.account_id = $2
		LEFT JOIN team_space_members tm ON tm.team_space_id = p.team_space_id AND tm.account_id = $2
		WHERE p.project_id = $1 AND p.project_status IS NOT NULL`, projectID, accountID, platformAdminEmail, activeStatus).
		Scan(append(access.Project.destinations(), &access.PlatformAdmin, &access.ProjectRole, &access.TeamSpaceRole)...)
	return access, postgres.RowError("authorize the project", err)
}

// ListProjects returns at most limit Projects that ProjectFlow has written
// and filter selects, newest first, after the page key when one is given, and
// the key of the page after them.
func (d *ProjectDAO) ListProjects(ctx context.Context, filter ProjectFilter, after *postgres.PageKey, limit int) ([]ListedProject, *postgres.PageKey, error) {
	afterCreatedAt, afterProjectID := postgres.AfterValues(after)
	rows, err := postgres.QueryRows(ctx, d.db, "list the projects", func(scan func(...any) error) (ListedProject, error) {
		var listed ListedProject
		err := scan(append(listed.Project.destinations(), &listed.ProjectRole, &listed.TeamSpaceRole)...)
		return listed, err
	}, `SELECT `+projectColumns+`, pm.role, tm.role FROM projects p`+liveRelease+`
		LEFT JOIN project_members pm ON pm.project_id = p.project_id AND pm.account_id = $2
		LEFT JOIN team_space_members tm ON tm.team_space_id = p.team_space_id AND tm.account_id = $2
		WHERE p.project_status IS NOT NULL AND p.project_status <> $1 AND p.created_at IS NOT NULL
			AND p.team_space_id = $3
			AND (COALESCE(cardinality($4::text[]), 0) = 0 OR p.project_status = ANY($4))
			AND ($5::timestamptz IS NULL OR (p.created_at, p.project_id) < ($5, $6))
		ORDER BY p.created_at DESC, p.project_id DESC LIMIT $7`,
		filter.ExcludedStatus, filter.AccountID, filter.TeamSpaceID, filter.Statuses, afterCreatedAt, afterProjectID, limit+1)
	if err != nil {
		return nil, nil, err
	}
	page, next := postgres.Page(rows, limit, func(p ListedProject) postgres.PageKey {
		return postgres.PageKey{CreatedAt: p.Project.CreatedAt.V, ID: p.Project.ProjectID}
	})
	return page, next, nil
}

func (p *ProjectRow) destinations() []any {
	return []any{&p.ProjectID, &p.CreatorAccountID, &p.TeamSpaceID, &p.Name, &p.ProjectStatus, &p.TemplateRevision, &p.SetupFlowID, &p.Revision,
		postgres.JSONColumn(&p.RepositoryBinding), postgres.JSONColumn(&p.AppManifest), postgres.JSONColumn(&p.ActiveDeployment), postgres.JSONColumn(&p.CurrentLive),
		postgres.JSONColumn(&p.ActiveCommand), postgres.JSONColumn(&p.LastFailure), postgres.JSONColumn(&p.SessionStartFailure),
		postgres.JSONColumn(&p.ProjectStorage), postgres.JSONColumn(&p.LiveExecution), &p.CreatedAt, &p.UpdatedAt,
		&p.SetupStatus, &p.SetupPhase, &p.SetupSourceCommit, postgres.JSONColumn(&p.SetupFailure), &p.SetupRevision, postgres.JSONColumn(&p.LiveVersion)}
}
