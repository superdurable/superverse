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
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
)

// TestMemberDAOIntegration runs the member queries against a migrated scratch
// database whose rows the test writes as Dex and the account code would: the
// Members maps of TeamSpaceFlow and ProjectFlow, the Projects' rows and the
// accounts. Project member rows stay after their Project is deleted, and an
// account removed from the TeamSpace keeps its Project rows.
func TestMemberDAOIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	dao := New(db)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	// The DAO interprets no status or role; these stand for any.
	const ready, deleted, owner, other = "READY", "DELETED", "OWNER", "OTHER"
	added := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, account := range []string{"alice", "bob", "carol", "dave"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO accounts (account_id, canonical_email, status, password_record, credential_revision,
			auth_epoch, registration_fingerprint, created_at, updated_at) VALUES ($1, $2, 'ACTIVE', '{}', 1, 1, $1, $3, $3)`,
			account, account+"@example.com", added); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO team_spaces (team_space_id, team_space_status) VALUES ('t001', $1), ('t002', $2)`, ready, deleted); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO team_space_members (team_space_id, account_id, role, added_at, added_by_account_id) VALUES
		('t001', 'alice', 'ADMIN', $1, 'alice'), ('t001', 'bob', 'MEMBER', $2, 'alice'), ('t001', 'carol', 'MEMBER', $3, 'alice'),
		('t002', 'dave', 'ADMIN', $1, 'dave')`, added, added.Add(time.Minute), added.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects (project_id, team_space_id, name, project_status, created_at) VALUES
		('p001', 't001', 'Shared', $1, $3), ('p002', 't001', 'Alone', $1, $3 + interval '1 minute'), ('p003', 't001', 'Gone', $2, $3)`,
		ready, deleted, added); err != nil {
		t.Fatal(err)
	}
	// p002's other owner, dave, is not a member of t001, so his role counts for nothing.
	if _, err := db.ExecContext(ctx, `INSERT INTO project_members (project_id, account_id, role, added_at, added_by_account_id) VALUES
		('p001', 'alice', $1, $3, 'alice'), ('p001', 'bob', $1, $4, 'alice'), ('p001', 'carol', $2, $5, 'alice'),
		('p002', 'alice', $1, $3, 'alice'), ('p002', 'dave', $1, $4, 'alice'), ('p003', 'alice', $1, $3, 'alice')`,
		owner, other, added, added.Add(time.Minute), added.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	members, err := dao.TeamSpaceMembers(ctx, "t001")
	if err != nil || len(members) != 3 || members[0].AccountID != "alice" || members[0].Email.V != "alice@example.com" ||
		members[0].Role.V != "ADMIN" || members[2].AccountID != "carol" || members[2].AddedByAccountID.V != "alice" {
		t.Fatalf("the members of t001: %+v: %v", members, err)
	}
	projectMembers, err := dao.ProjectMembers(ctx, "p002", deleted)
	if err != nil || len(projectMembers) != 2 || projectMembers[0].AccountID != "alice" || !projectMembers[0].TeamSpaceMember ||
		projectMembers[1].AccountID != "dave" || projectMembers[1].TeamSpaceMember || projectMembers[1].Email.V != "dave@example.com" {
		t.Fatalf("the members of p002: %+v: %v", projectMembers, err)
	}
	if gone, err := dao.ProjectMembers(ctx, "p003", deleted); err != nil || len(gone) != 0 {
		t.Fatalf("the members of the deleted p003 are listed: %+v: %v", gone, err)
	}
	accounts, err := dao.MemberAccounts(ctx, "t001", []string{"alice", "dave", "nobody"})
	if err != nil || len(accounts) != 2 || accounts["alice"] != (MemberAccount{Email: "alice@example.com", TeamSpaceMember: true}) ||
		accounts["dave"] != (MemberAccount{Email: "dave@example.com"}) {
		t.Fatalf("the member accounts of t001: %+v: %v", accounts, err)
	}

	if role, err := dao.TeamSpaceRole(ctx, "t001", "bob", deleted); err != nil || role != "MEMBER" {
		t.Fatalf("bob's role in t001 is %q: %v", role, err)
	}
	if role, err := dao.TeamSpaceRole(ctx, "t001", "dave", deleted); err != nil || role != "" {
		t.Fatalf("dave, no member of t001, has the role %q: %v", role, err)
	}
	for _, c := range []struct{ teamSpace, account string }{{"t002", "dave"}, {"t999", "alice"}} {
		if _, err = dao.TeamSpaceRole(ctx, c.teamSpace, c.account, deleted); !errors.Is(err, postgres.ErrNotFound) {
			t.Fatalf("%s in %s, a DELETED or unknown TeamSpace: %v", c.account, c.teamSpace, err)
		}
	}

	owned, err := dao.SoleOwnedProjects(ctx, "t001", "alice", owner, deleted, 10)
	if err != nil || !slices.EqualFunc(owned, []OwnedProject{{ProjectID: "p002"}}, func(a, b OwnedProject) bool { return a.ProjectID == b.ProjectID }) || owned[0].Name.V != "Alone" {
		t.Fatalf("alice solely owns %+v, want only p002: p001 has bob, p003 is deleted, and dave's role in p002 does not count: %v", owned, err)
	}
	if owned, err = dao.SoleOwnedProjects(ctx, "t001", "bob", owner, deleted, 10); err != nil || len(owned) != 0 {
		t.Fatalf("bob solely owns %+v: %v", owned, err)
	}

	for account, want := range map[string]int32{"alice": 2, "carol": 1, "dave": 1, "bob": 1} {
		if count, err := dao.RetainedProjectRoles(ctx, "t001", account, deleted); err != nil || count != want {
			t.Fatalf("%s holds roles in %d Projects of t001 that are not deleted, want %d: %v", account, count, want, err)
		}
	}
}
