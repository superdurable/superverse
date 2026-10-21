// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspacedao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTeamSpaceServicesDexIntegration drives TeamSpaceService,
// TeamSpaceMemberService and the TeamSpace guards of ProjectService and
// ProjectMemberService through the actual gRPC API, with TeamSpaceFlow and
// ProjectFlow on the lane's server Worker and the read model of the lane's
// PostgreSQL. It covers the creation's replays, reads only for members,
// changes only from an admin, the stale rename's REVISION_CHANGED, the
// creation of a Project only by a member, the refused removal of a Project's
// only owner, and the count of the roles that a re-added member regains.
//
// A member creates a Project, whose Setup creates its repository, so the
// test requires SUPERVERSE_TEST_GITHUB_EFFECTS=true. When it ends, the Project
// and then the TeamSpace are deleted.
func TestTeamSpaceServicesDexIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("a Project's Setup creates its repository: requires SUPERVERSE_TEST_GITHUB_EFFECTS=true")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	key := []byte(strings.Repeat("x", 32))
	accountDAO := accountdao.New(db, []string{"admin@superverse.test"})
	teamSpaceFlow, projectFlow := &teamspaces.TeamSpaceFlow{}, &projects.ProjectFlow{}
	sdk := dexflowtest.ServerWorkerClient(t, teamSpaceFlow, projectFlow)
	teamSpaceClient := &teamspaces.TeamSpaceFlowClient{Dex: sdk, Flow: teamSpaceFlow}
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), Members: memberdao.New(db), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmails: []string{"admin@superverse.test"}}
	b := &boundary.Boundary{Logger: slog.Default(), Accounts: &accounts.Accounts{DAO: accountDAO, Signer: comm.Must(accounts.NewTokenSigner(key))}, Cursors: watch.NewCursorCodec(key)}
	connection := serveLoopback(ctx, t, &Server{Boundary: b, ProjectIDLength: projectClient.ProjectIDLength, Projects: projectClient,
		TeamSpaces: teamSpaceClient, TeamSpaceReads: teamspacedao.New(db), Members: memberdao.New(db)})
	teamSpaceAPI, memberAPI := pb.NewTeamSpaceServiceClient(connection), pb.NewTeamSpaceMemberServiceClient(connection)
	projectAPI, projectMemberAPI := pb.NewProjectServiceClient(connection), pb.NewProjectMemberServiceClient(connection)
	accountAPI := pb.NewAccountServiceClient(connection)

	id := flowid.RandomID(12)
	alice, aliceEmail := registerNamedCaller(ctx, t, accountAPI, "alice-"+id)
	bob, bobEmail := registerNamedCaller(ctx, t, accountAPI, "bob-"+id)
	carol, _ := registerNamedCaller(ctx, t, accountAPI, "carol-"+id)

	create := &pb.CreateTeamSpaceRequest{RequestId: "team-" + id, Name: "Team " + id, Caller: alice}
	created, err := teamSpaceAPI.CreateTeamSpace(ctx, create)
	if err != nil {
		t.Fatalf("create the TeamSpace: %v", err)
	}
	teamSpaceID := created.GetTeamSpaceId()
	t.Cleanup(func() { deleteTeamSpace(ctx, t, teamSpaceClient, teamSpaceID) })
	if again, err := teamSpaceAPI.CreateTeamSpace(ctx, create); err != nil || again.GetTeamSpaceId() != teamSpaceID {
		t.Fatalf("a replay of the creation answered %v: %v", again, err)
	}
	_, err = teamSpaceAPI.CreateTeamSpace(ctx, &pb.CreateTeamSpaceRequest{RequestId: create.GetRequestId(), Name: "Another name", Caller: alice})
	requireCode(t, err, codes.AlreadyExists, "the creation's request ID with another name")

	// The reads right after the creation wait for the TeamSpace's row.
	if _, err = projectAPI.ListProjects(ctx, &pb.ListProjectsRequest{TeamSpaceId: teamSpaceID, Caller: alice}); err != nil {
		t.Fatalf("alice lists the new TeamSpace's Projects: %v", err)
	}
	got, err := teamSpaceAPI.GetTeamSpace(ctx, &pb.GetTeamSpaceRequest{TeamSpaceId: teamSpaceID, Caller: alice})
	if err != nil {
		t.Fatalf("alice reads the new TeamSpace: %v", err)
	}
	team := got.GetTeamSpace()
	if team.GetCallerRole() != pb.TeamSpaceRole_TEAM_SPACE_ROLE_ADMIN || team.GetStatus() != pb.TeamSpaceStatus_TEAM_SPACE_STATUS_READY || team.GetName() != create.GetName() {
		t.Fatalf("alice reads %v", team)
	}
	listed, err := teamSpaceAPI.ListTeamSpaces(ctx, &pb.ListTeamSpacesRequest{Caller: alice})
	if err != nil || len(listed.GetTeamSpaces()) != 1 || listed.GetTeamSpaces()[0].GetTeamSpaceId() != teamSpaceID {
		t.Fatalf("alice lists %v: %v", listed, err)
	}
	if listed, err = teamSpaceAPI.ListTeamSpaces(ctx, &pb.ListTeamSpacesRequest{Caller: bob}); err != nil || len(listed.GetTeamSpaces()) != 0 {
		t.Fatalf("bob, a member of none, lists %v: %v", listed, err)
	}
	_, err = teamSpaceAPI.GetTeamSpace(ctx, &pb.GetTeamSpaceRequest{TeamSpaceId: teamSpaceID, Caller: bob})
	requireCode(t, err, codes.PermissionDenied, "a non-member's read of the TeamSpace")
	_, err = projectAPI.CreateProject(ctx, &pb.CreateProjectRequest{RequestId: "carol-" + id, Name: "By carol", ManagedRepository: &pb.ManagedRepositorySource{}, TeamSpaceId: teamSpaceID, Caller: carol})
	requireCode(t, err, codes.PermissionDenied, "a non-member's Project creation")

	_, err = memberAPI.AddTeamSpaceMember(ctx, &pb.AddTeamSpaceMemberRequest{TeamSpaceId: teamSpaceID, Email: "nobody-" + id + "@example.com", Role: pb.TeamSpaceRole_TEAM_SPACE_ROLE_MEMBER, Caller: alice})
	requireCode(t, err, codes.FailedPrecondition, "adding an unregistered email")
	added, err := memberAPI.AddTeamSpaceMember(ctx, &pb.AddTeamSpaceMemberRequest{TeamSpaceId: teamSpaceID, Email: strings.ToUpper(bobEmail), Role: pb.TeamSpaceRole_TEAM_SPACE_ROLE_MEMBER, Caller: alice})
	if err != nil || len(added.GetMembers()) != 2 || added.GetMembers()[1].GetEmail() != bobEmail || added.GetRegainedProjectCount() != 0 {
		t.Fatalf("alice adds bob: %v: %v", added, err)
	}
	awaitTrue(ctx, t, "bob's list of the members", func() bool {
		members, err := memberAPI.ListTeamSpaceMembers(ctx, &pb.ListTeamSpaceMembersRequest{TeamSpaceId: teamSpaceID, Caller: bob})
		return err == nil && len(members.GetMembers()) == 2 && members.GetMembers()[0].GetEmail() == aliceEmail
	})
	_, err = memberAPI.AddTeamSpaceMember(ctx, &pb.AddTeamSpaceMemberRequest{TeamSpaceId: teamSpaceID, Email: aliceEmail, Role: pb.TeamSpaceRole_TEAM_SPACE_ROLE_MEMBER, Caller: bob})
	requireCode(t, err, codes.PermissionDenied, "a member who is not an admin changes the members")

	_, err = teamSpaceAPI.RenameTeamSpace(ctx, &pb.RenameTeamSpaceRequest{TeamSpaceId: teamSpaceID, ExpectedRevision: added.GetRevision(), Name: "By bob", Caller: bob})
	requireCode(t, err, codes.PermissionDenied, "a member who is not an admin renames")
	_, err = teamSpaceAPI.RenameTeamSpace(ctx, &pb.RenameTeamSpaceRequest{TeamSpaceId: teamSpaceID, ExpectedRevision: added.GetRevision() - 1, Name: "Stale", Caller: alice})
	requireRevisionChanged(t, err)
	renamed, err := teamSpaceAPI.RenameTeamSpace(ctx, &pb.RenameTeamSpaceRequest{TeamSpaceId: teamSpaceID, ExpectedRevision: added.GetRevision(), Name: "Renamed " + id, Caller: alice})
	if err != nil || renamed.GetTeamSpace().GetName() != "Renamed "+id || renamed.GetTeamSpace().GetRevision() <= added.GetRevision() {
		t.Fatalf("alice renames at the current revision: %v: %v", renamed, err)
	}
	_, err = teamSpaceAPI.DeleteTeamSpace(ctx, &pb.DeleteTeamSpaceRequest{TeamSpaceId: teamSpaceID, RequestId: "delete-" + id, Caller: alice})
	requireCode(t, err, codes.PermissionDenied, "a TeamSpace admin's deletion of the TeamSpace")

	// Bob creates a Project and is its only owner, so he cannot leave until alice owns it too.
	project, err := projectAPI.CreateProject(ctx, &pb.CreateProjectRequest{RequestId: "bob-" + id, Name: "Owned by bob", ManagedRepository: &pb.ManagedRepositorySource{}, TeamSpaceId: teamSpaceID, Caller: bob})
	if err != nil {
		t.Fatalf("bob creates a Project: %v", err)
	}
	projectID := project.GetProjectId()
	t.Cleanup(func() { projecttest.DeleteForTest(context.WithoutCancel(ctx), t, projectClient, projectID) })
	awaitRows(ctx, t, db, projectID, []string{bob.GetAccountId() + ":" + projects.RoleOwner})
	// The guard also reads the Project's row, which Dex writes in its own batch.
	awaitTrue(ctx, t, "the Project's row in the TeamSpace", func() bool {
		return len(postgrestest.StringsForTest(ctx, t, db, `SELECT project_status FROM projects
			WHERE project_id = $1 AND team_space_id = $2 AND project_status IS NOT NULL`, projectID, teamSpaceID)) == 1
	})
	_, err = memberAPI.RemoveTeamSpaceMember(ctx, &pb.RemoveTeamSpaceMemberRequest{TeamSpaceId: teamSpaceID, AccountId: bob.GetAccountId(), Caller: alice})
	requireCode(t, err, codes.FailedPrecondition, "removing the only owner of a Project")
	if !strings.Contains(status.Convert(err).Message(), "Owned by bob") {
		t.Fatalf("the refused removal does not name the Project: %v", err)
	}
	if _, err = projectMemberAPI.AddProjectMember(ctx, &pb.AddProjectMemberRequest{ProjectId: projectID, AccountId: alice.GetAccountId(), Role: pb.ProjectRole_PROJECT_ROLE_OWNER, Caller: bob}); err != nil {
		t.Fatalf("bob makes alice an owner: %v", err)
	}
	owners := []string{alice.GetAccountId() + ":" + projects.RoleOwner, bob.GetAccountId() + ":" + projects.RoleOwner}
	slices.Sort(owners)
	awaitRows(ctx, t, db, projectID, owners)
	removed, err := memberAPI.RemoveTeamSpaceMember(ctx, &pb.RemoveTeamSpaceMemberRequest{TeamSpaceId: teamSpaceID, AccountId: bob.GetAccountId(), Caller: alice})
	if err != nil || len(removed.GetMembers()) != 1 {
		t.Fatalf("alice removes bob, no longer the only owner: %v: %v", removed, err)
	}
	awaitTrue(ctx, t, "bob's refused read after his removal", func() bool {
		_, err := teamSpaceAPI.GetTeamSpace(ctx, &pb.GetTeamSpaceRequest{TeamSpaceId: teamSpaceID, Caller: bob})
		return status.Code(err) == codes.PermissionDenied
	})
	readded, err := memberAPI.AddTeamSpaceMember(ctx, &pb.AddTeamSpaceMemberRequest{TeamSpaceId: teamSpaceID, Email: bobEmail, Role: pb.TeamSpaceRole_TEAM_SPACE_ROLE_MEMBER, Caller: alice})
	if err != nil || readded.GetRegainedProjectCount() != 1 {
		t.Fatalf("bob, re-added, regains the roles of %d Projects, want 1: %v", readded.GetRegainedProjectCount(), err)
	}
	t.Logf("TeamSpace %s and Project %s: replays, member-only reads and creation, admin-only changes, REVISION_CHANGED, the only owner kept, regained roles", teamSpaceID, projectID)
}

// registerNamedCaller registers an account through AccountService and returns its
// caller and email; its login is revoked when the test ends.
func registerNamedCaller(ctx context.Context, t *testing.T, accountAPI pb.AccountServiceClient, name string) (*pb.CallerContext, string) {
	t.Helper()
	email := name + "@example.com"
	registered, err := accountAPI.RegisterAccount(ctx, &pb.RegisterAccountRequest{Email: email, Password: "password-" + name, RequestId: "register-" + name})
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	login := registered.GetLogin()
	caller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
	t.Cleanup(func() {
		if _, err := accountAPI.Logout(context.WithoutCancel(ctx), &pb.LogoutRequest{Caller: caller}); err != nil {
			t.Errorf("revoke the login of %s: %v", name, err)
		}
	})
	return caller, email
}

// deleteTeamSpace deletes the TeamSpace as the platform admin through its Flow
// and waits until the Flow completes.
func deleteTeamSpace(ctx context.Context, t *testing.T, client *teamspaces.TeamSpaceFlowClient, teamSpaceID string) {
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer done()
	admin := teamspaces.Actor{AccountID: "api-test", PlatformAdmin: true}
	_, err := client.RequestDeletion(cleanup, teamSpaceID, teamspaces.DeletionRequest{Actor: admin, ID: "api-test-cleanup"})
	if _, inactive := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); err != nil && !inactive {
		t.Errorf("the deletion of TeamSpace %s was not admitted (%s)", teamSpaceID, dexflowtest.ErrorCategory(err))
		return
	}
	if _, err = client.Dex.WaitForFlow(cleanup, flowid.TeamSpaceFlowID(teamSpaceID), dex.WaitForFlowOptions{}); err != nil {
		t.Errorf("TeamSpace %s did not complete its deletion (%s)", teamSpaceID, dexflowtest.ErrorCategory(err))
	}
}

// awaitRows waits until the project_members rows of the Project hold exactly
// want, as account:role in byte order.
func awaitRows(ctx context.Context, t *testing.T, db *sql.DB, projectID string, want []string) {
	t.Helper()
	var rows []string
	awaitTrue(ctx, t, "the project_members rows "+strings.Join(want, ", "), func() bool {
		rows = postgrestest.StringsForTest(ctx, t, db, `SELECT account_id || ':' || role FROM project_members
			WHERE project_id = $1 ORDER BY account_id COLLATE "C"`, projectID)
		return slices.Equal(rows, want)
	})
}

// awaitTrue polls done every second until it holds or the test's deadline
// passes.
func awaitTrue(ctx context.Context, t *testing.T, what string, done func() bool) {
	t.Helper()
	for !done() {
		select {
		case <-ctx.Done():
			t.Fatalf("%s did not happen", what)
		case <-time.After(time.Second):
		}
	}
}

func requireCode(t *testing.T, err error, code codes.Code, what string) {
	t.Helper()
	if status.Code(err) != code {
		t.Fatalf("%s answered %v, want %s", what, err, code)
	}
}

// requireRevisionChanged requires ABORTED with the ErrorInfo REVISION_CHANGED,
// on which Studio reads again.
func requireRevisionChanged(t *testing.T, err error) {
	t.Helper()
	if failure := status.Convert(err); failure.Code() != codes.Aborted {
		t.Fatalf("a stale rename answered %v, want ABORTED", err)
	}
	requireRemedy(t, err, boundary.ReasonRevisionChangedForTest, pb.FailureRemedy_FAILURE_REMEDY_APPLY_AGAIN)
}
