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
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
)

// The DAO never interprets a status, an email or an account status; these
// stand for any listed, excluded and filtered one, and for the admin rule's
// parameters.
const (
	listed, excluded, other = "LISTED", "EXCLUDED", "OTHER"
	adminEmail, active      = "admin@example.com", "ACTIVE"
	// a001 is the Project whose row both stores wrote; a004 another creator's.
	a001, a004 = "a001", "a004"
)

// TestProjectDAOIntegration runs the projects queries against a migrated
// scratch database whose rows the test writes as Dex would: ProjectFlow's
// columns, the Setup's columns of the same row, and a row that only a Setup
// batch has written so far.
func TestProjectDAOIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	dao := New(db)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	created := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	live := json.RawMessage(`{"DeploymentID": "d1", "ReleaseID": "r1", "Revision": 2, "Ready": true}`)
	for _, p := range []struct {
		id, creator, teamSpace, status string
		createdAt                      time.Time
	}{
		{a001, "c1", "t1", listed, created}, {"a002", "c1", "t1", listed, created}, {"a003", "c1", "t1", excluded, created.Add(time.Minute)},
		{a004, "c2", "t2", listed, created.Add(2 * time.Minute)}, {"a006", "c1", "t2", other, created.Add(3 * time.Minute)},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO projects (project_id, creator_account_id, team_space_id, name, project_status, revision, current_live, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, 7, $6, $7, $7)`, p.id, p.creator, p.teamSpace, "Project "+p.id, p.status, live, p.createdAt); err != nil {
			t.Fatal(err)
		}
	}
	// c1 holds roles in a001, a002 and a006 but belongs only to a001's and a002's TeamSpace.
	if _, err := db.ExecContext(ctx, `INSERT INTO project_members (project_id, account_id, role) VALUES
		('a001', 'c1', 'OWNER'), ('a002', 'c1', 'BUILDER'), ('a004', 'c2', 'OWNER'), ('a006', 'c1', 'OPERATOR');
		INSERT INTO team_space_members (team_space_id, account_id, role) VALUES ('t1', 'c1', 'ADMIN'), ('t2', 'c2', 'ADMIN')`); err != nil {
		t.Fatal(err)
	}
	// The Setup's store upserts only its own columns, of the Project's row or of a row the Project has not written yet.
	if _, err := db.ExecContext(ctx, `INSERT INTO projects (project_id, setup_status, setup_phase, setup_source_commit, setup_revision)
		VALUES ('a001', 'DONE', 'Commit', 'abc', 4), ('a005', 'RUNNING', 'Admit', NULL, 1)
		ON CONFLICT (project_id) DO UPDATE SET setup_status = EXCLUDED.setup_status, setup_phase = EXCLUDED.setup_phase,
			setup_source_commit = EXCLUDED.setup_source_commit, setup_revision = EXCLUDED.setup_revision`); err != nil {
		t.Fatal(err)
	}

	p, err := dao.Project(ctx, a001)
	if err != nil || p.Name.V != "Project a001" || p.ProjectStatus.V != listed || p.Revision.V != 7 || !p.CreatedAt.V.Equal(created) ||
		p.SetupStatus.V != "DONE" || p.SetupSourceCommit.V != "abc" || p.SetupRevision.V != 4 || p.CreatorAccountID.V != "c1" || p.TeamSpaceID.V != "t1" || p.SetupFailure != nil {
		t.Fatalf("project a001: %+v: %v", p, err)
	}
	var pointer map[string]any
	if err = json.Unmarshal(p.CurrentLive, &pointer); err != nil || pointer["DeploymentID"] != "d1" {
		t.Fatalf("the Live pointer of a001 is %s: %v", p.CurrentLive, err)
	}
	for _, missing := range []string{"a005", "a999"} {
		if _, err = dao.Project(ctx, missing); !errors.Is(err, postgres.ErrNotFound) {
			t.Fatalf("project %s, which ProjectFlow has not written: %v", missing, err)
		}
	}

	for _, c := range []struct {
		name   string
		filter ProjectFilter
		want   [][]string
	}{
		{"a member of t1", ProjectFilter{AccountID: "c1", TeamSpaceID: "t1", ExcludedStatus: excluded}, [][]string{{"a002:BUILDER:ADMIN", a001 + ":OWNER:ADMIN"}}},
		{"a member of t2", ProjectFilter{AccountID: "c2", TeamSpaceID: "t2", ExcludedStatus: excluded}, [][]string{{"a006::ADMIN", a004 + ":OWNER:ADMIN"}}},
		{"a role outside the TeamSpace", ProjectFilter{AccountID: "c1", TeamSpaceID: "t2", ExcludedStatus: excluded}, [][]string{{"a006:OPERATOR:", a004 + "::"}}},
		{"one status", ProjectFilter{AccountID: "c2", TeamSpaceID: "t2", Statuses: []string{listed}, ExcludedStatus: excluded}, [][]string{{a004 + ":OWNER:ADMIN"}}},
		{"an excluded status", ProjectFilter{AccountID: "c1", TeamSpaceID: "t1", Statuses: []string{excluded}, ExcludedStatus: excluded}, nil},
	} {
		if pages := listPages(ctx, t, dao, c.filter); !slices.EqualFunc(pages, c.want, slices.Equal) {
			t.Fatalf("%s: the pages are %v, want %v as ID:Project role:TeamSpace role: newest first, ties by ID, the excluded status and the Setup-only row left out", c.name, pages, c.want)
		}
	}

	if _, err = db.ExecContext(ctx, `INSERT INTO accounts (account_id, canonical_email, status, password_record, credential_revision, auth_epoch,
		registration_fingerprint, created_at, updated_at) VALUES ('admin', $1, $2, '{}', 1, 1, 'f1', now(), now()), ('c2', 'c2@example.com', $2, '{}', 1, 1, 'f2', now(), now()), ('admin2', 'admin2@example.com', $2, '{}', 1, 1, 'f3', now(), now())`,
		adminEmail, active); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		account string
		admin   bool
		roles   string
	}{{"admin", true, ":"}, {"admin2", true, ":"}, {"c2", false, ":"}, {"c1", false, "OWNER:ADMIN"}, {"unregistered", false, ":"}} {
		access, authorizeErr := dao.AuthorizeProject(ctx, a001, c.account, []string{adminEmail, "admin2@example.com"}, active)
		if authorizeErr != nil || access.PlatformAdmin != c.admin || access.ProjectRole.V+":"+access.TeamSpaceRole.V != c.roles ||
			access.Project.ProjectID != a001 || access.Project.CreatorAccountID.V != "c1" || access.Project.SetupStatus.V != "DONE" {
			t.Fatalf("authorize %s on a001: %+v: %v", c.account, access, authorizeErr)
		}
	}
	if _, err = dao.AuthorizeProject(ctx, "a005", "admin", []string{adminEmail, "admin2@example.com"}, active); !errors.Is(err, postgres.ErrNotFound) {
		t.Fatalf("authorize the admin on a005, which ProjectFlow has not written: %v", err)
	}
}

// listPages pages through ListProjects two rows at a time.
func listPages(ctx context.Context, t *testing.T, dao *ProjectDAO, filter ProjectFilter) [][]string {
	t.Helper()
	var pages [][]string
	var after *postgres.PageKey
	for {
		page, next, err := dao.ListProjects(ctx, filter, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, p := range page {
			ids = append(ids, p.Project.ProjectID+":"+p.ProjectRole.V+":"+p.TeamSpaceRole.V)
		}
		if ids != nil {
			pages = append(pages, ids)
		}
		if next == nil {
			return pages
		}
		if len(page) != 2 {
			t.Fatalf("a page of %v is short but names a next page", ids)
		}
		after = next
	}
}
