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
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
)

// TestTeamSpaceDAOIntegration runs the team_spaces queries against a migrated
// scratch database whose rows the test writes as Dex would: TeamSpaceFlow's
// row and Members map, and ProjectFlow's rows of the TeamSpace's Projects.
func TestTeamSpaceDAOIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	dao := New(db)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	// The DAO interprets no status or role; these stand for any listed, any
	// excluded and any other status.
	const listed, excluded, other = "LISTED", "EXCLUDED", "OTHER"
	created := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		id, status string
		at         time.Duration
	}{{"t001", listed, 0}, {"t002", listed, time.Minute}, {"t003", excluded, 2 * time.Minute}, {"t004", other, 3 * time.Minute}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO team_spaces (team_space_id, name, team_space_status, creator_account_id, revision, created_at, updated_at)
			VALUES ($1, $2, $3, 'alice', 3, $4, $4)`, s.id, "TeamSpace "+s.id, s.status, created.Add(s.at)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO team_space_members (team_space_id, account_id, role, added_at, added_by_account_id) VALUES
		('t001', 'alice', 'ADMIN', $1, 'alice'), ('t001', 'carol', 'MEMBER', $1, 'alice'), ('t002', 'alice', 'MEMBER', $1, 'bob'),
		('t003', 'alice', 'ADMIN', $1, 'alice'), ('t004', 'bob', 'ADMIN', $1, 'bob')`, created); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects (project_id, team_space_id, project_status, current_live, created_at) VALUES
		('p001', 't001', $1, '{"DeploymentID": "d1"}', $3), ('p002', 't001', $1, NULL, $3), ('p003', 't001', $2, '{"DeploymentID": "d2"}', $3),
		('p004', 't002', $1, NULL, $3)`, listed, excluded, created); err != nil {
		t.Fatal(err)
	}

	// One ACTIVE and one DELETED connection of t001, which the connection table's checks spell out.
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (account_id, canonical_email, status, password_record, credential_revision, auth_epoch,
		registration_fingerprint, created_at, updated_at) VALUES ('alice', 'alice@example.com', 'ACTIVE', '{}', 1, 1, 'alice', $1, $1)`, created); err != nil {
		t.Fatal(err)
	}
	for i, status := range []string{"ACTIVE", "DELETED"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO team_space_connections (team_space_id, connection_id, creation_fingerprint, name, connector_id,
			module_path, version_line, manifest_version, auth_method_id, oauth_client, settings, revision, credential_status, connection_status,
			creator_account_id, updated_by_account_id, created_at, updated_at)
			VALUES ('t001', $1, $1, $1, 'openai', 'openai', 'v0', 'v0.21.1', '', 'user', '{}', 1, 'READY', $2, 'alice', 'alice', $3, $3)`,
			"c"+string(rune('a'+i)), status, created); err != nil {
			t.Fatal(err)
		}
	}

	list := func(account string, all bool, statuses []string) []string {
		t.Helper()
		var ids []string
		var after *postgres.PageKey
		for {
			page, next, err := dao.ListTeamSpaces(ctx, account, all, excluded, statuses, after, 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range page {
				ids = append(ids, s.TeamSpaceID+":"+s.CallerRole.V)
			}
			if next == nil {
				return ids
			}
			after = next
		}
	}
	for _, c := range []struct {
		account  string
		all      bool
		statuses []string
		want     []string
	}{
		{"alice", false, nil, []string{"t002:MEMBER", "t001:ADMIN"}},
		{"admin", true, nil, []string{"t004:", "t002:", "t001:"}},
		{"bob", true, []string{other}, []string{"t004:ADMIN"}},
		{"dave", false, nil, nil},
	} {
		if got := list(c.account, c.all, c.statuses); !slices.Equal(got, c.want) {
			t.Fatalf("%s (all %t, statuses %v) lists %v, want %v: newest first, own memberships unless all, the excluded status left out", c.account, c.all, c.statuses, got, c.want)
		}
	}

	detail, err := dao.TeamSpace(ctx, "t001", "alice", excluded, excluded, "ACTIVE")
	if err != nil || detail.Name.V != "TeamSpace t001" || detail.CallerRole.V != "ADMIN" || detail.Revision.V != 3 ||
		detail.MemberCount != 2 || detail.ProjectCount != 2 || detail.LiveProjectCount != 1 || detail.ConnectionCount != 1 {
		t.Fatalf("TeamSpace t001 for alice: %+v: %v", detail, err)
	}
	if outsider, err := dao.TeamSpace(ctx, "t001", "dave", excluded, excluded, "ACTIVE"); err != nil || outsider.CallerRole.Valid {
		t.Fatalf("TeamSpace t001 for a non-member reads the role %v: %v", outsider.CallerRole, err)
	}
	for _, missing := range []string{"t003", "t999"} {
		if _, err = dao.TeamSpace(ctx, missing, "alice", excluded, excluded, "ACTIVE"); !errors.Is(err, postgres.ErrNotFound) {
			t.Fatalf("TeamSpace %s, excluded or never written: %v", missing, err)
		}
	}
}
