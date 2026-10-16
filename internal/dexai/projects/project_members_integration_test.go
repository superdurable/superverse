// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// TestProjectMembersDexIntegration changes the members of a throwaway Project
// on the server Worker: only an owner or the platform admin changes them, a
// repeat changes nothing, four owners racing to be removed or demoted leave
// exactly one, and the project_members rows follow the Flow and stay after
// the Project is deleted.
func TestProjectMembersDexIntegration(t *testing.T) {
	projectClient, _ := teamSpaceClients(t)
	db := postgrestest.OpenForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	projectID := projecttest.StartForTest(ctx, t, projectClient, t.Name())
	project, err := projectClient.Read(ctx, projectID)
	if err != nil {
		t.Fatalf("read Project %s: %s", projectID, dexflowtest.ErrorCategory(err))
	}
	owner := project.CreatorAccountID
	account := func(name string) string { return name + "-" + projectID }
	platformAdmin := func(accountID string, role projects.Role) projects.MemberChange {
		return projects.MemberChange{ActorAccountID: account("admin"), ActorIsPlatformAdmin: true, AccountID: accountID, Role: role}
	}

	_, err = projectClient.AddMember(ctx, projectID, projects.MemberChange{ActorAccountID: account("outsider"), AccountID: account("bob"), Role: projects.RoleBuilder})
	requireRejection(t, err, dexflow.RejectionPermissionDenied, "an account without a role adds a member")
	update, err := projectClient.AddMember(ctx, projectID, projects.MemberChange{ActorAccountID: owner, AccountID: account("bob"), Role: projects.RoleBuilder})
	if err != nil || len(update.Members) != 2 || update.Members[1].AccountID != account("bob") || update.Members[1].AddedByAccountID != owner {
		t.Fatalf("the owner adds bob: %+v: %v", update.Members, err)
	}
	if again, err := projectClient.AddMember(ctx, projectID, projects.MemberChange{ActorAccountID: owner, AccountID: account("bob"), Role: projects.RoleBuilder}); err != nil || again.Project.Revision != update.Project.Revision {
		t.Fatalf("adding bob again with the same role changed the Project or failed: %v", err)
	}
	_, err = projectClient.AddMember(ctx, projectID, projects.MemberChange{ActorAccountID: account("bob"), AccountID: account("carol"), Role: projects.RoleOperator})
	requireRejection(t, err, dexflow.RejectionPermissionDenied, "a builder adds a member")
	_, err = projectClient.SetMemberRole(ctx, projectID, projects.MemberChange{ActorAccountID: owner, AccountID: owner, Role: projects.RoleBuilder})
	requireRejection(t, err, dexflow.RejectionFailedPrecondition, "the only owner demotes itself")

	// Four owners race to be removed or demoted; exactly one owner must remain.
	owners := []string{owner}
	for _, name := range []string{"o1", "o2", "o3"} {
		owners = append(owners, account(name))
		if _, err = projectClient.AddMember(ctx, projectID, platformAdmin(account(name), projects.RoleOwner)); err != nil {
			t.Fatalf("add owner %s: %v", name, err)
		}
	}
	var wait sync.WaitGroup
	var mutex sync.Mutex
	var accepted, kept int
	for i, target := range owners {
		wait.Go(func() {
			write := projectClient.SetMemberRole
			if i%2 == 0 {
				write = projectClient.RemoveMember
			}
			_, err := write(ctx, projectID, platformAdmin(target, projects.RoleOperator))
			var rejected *dexflow.Rejection
			mutex.Lock()
			defer mutex.Unlock()
			switch {
			case err == nil:
				accepted++
			case errors.As(err, &rejected) && rejected.HasCode(dexflow.RejectionFailedPrecondition):
				kept++
			default:
				t.Errorf("a racing change of owner %s failed: %v", target, err)
			}
		})
	}
	wait.Wait()
	update, err = projectClient.AddMember(ctx, projectID, platformAdmin(account("bob"), projects.RoleBuilder))
	remaining := slices.DeleteFunc(slices.Clone(update.Members), func(m projects.MemberEntry) bool { return m.Role != projects.RoleOwner })
	if err != nil || accepted != len(owners)-1 || kept != 1 || len(remaining) != 1 {
		t.Fatalf("after the race %d changes passed and %d were refused, leaving the owners %+v: %v", accepted, kept, remaining, err)
	}
	if _, err = projectClient.RemoveMember(ctx, projectID, platformAdmin(account("nobody"), "")); err != nil {
		t.Fatalf("removing an absent member failed: %v", err)
	}

	rows := func() []string {
		var want []string
		for _, member := range update.Members {
			want = append(want, member.AccountID+":"+member.Role)
		}
		slices.Sort(want)
		return want
	}()
	awaitMemberRows(ctx, t, db, projectID, rows)
	projecttest.DeleteForTest(ctx, t, projectClient, projectID)
	awaitMemberRows(ctx, t, db, projectID, rows)
	t.Logf("Project %s: members changed by owners only, one owner kept, rows kept after deletion", projectID)
}

// awaitMemberRows waits until the project_members rows of the Project hold
// exactly want, as account:role.
func awaitMemberRows(ctx context.Context, t *testing.T, db *sql.DB, projectID string, want []string) {
	t.Helper()
	for {
		rows := postgrestest.StringsForTest(ctx, t, db, `SELECT account_id || ':' || role FROM project_members
			WHERE project_id = $1 ORDER BY account_id COLLATE "C"`, projectID)
		if slices.Equal(rows, want) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the project_members rows of %s are %v, want %v", projectID, rows, want)
		case <-time.After(2 * time.Second):
		}
	}
}

func requireRejection(t *testing.T, err error, code dexflow.RejectionCode, what string) {
	t.Helper()
	var rejected *dexflow.Rejection
	if !errors.As(err, &rejected) || !rejected.HasCode(code) {
		t.Fatalf("%s was not refused with %s: %v", what, code, err)
	}
}
