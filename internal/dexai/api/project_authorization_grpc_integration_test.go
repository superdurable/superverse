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
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue/memory"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deliverydao"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/slackrelay"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspacedao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestProjectAuthorizationMatrixDexIntegration checks the Project
// capabilities through the actual gRPC API for every kind of caller of one
// Project: its owner, a builder, an operator, a TeamSpace member without a
// role, the TeamSpace's admin without a role, an outsider, and a builder
// removed from the TeamSpace, whose role no longer counts. Both Flows run on
// the lane's server Worker and every check reads the lane's read model.
//
//   - View (GetProject, ListProjectMembers, GetRelease) is an operator's,
//     Build (ListReleases, GetReleaseBuildLog, and CancelReleaseBuild and
//     CancelDeployment, which need the READY Project) a builder's, ManageMembers (RemoveProjectMember) an
//     owner's, and Delete (DeleteProject) an owner's or the TeamSpace admin's;
//     every other caller is PERMISSION_DENIED. Raising a capability's least
//     role, or dropping the TeamSpace membership from the effective role,
//     fails a row.
//   - ListRecentTriggerEvents needs the builder capability for Preview and the
//     operator capability for Live.
//   - ListProjects of the TeamSpace serves its members only, and reduces the
//     entry of a member without a role; a list without a TeamSpace is
//     INVALID_ARGUMENT.
//   - DeleteProject at a stale revision is ABORTED with REVISION_CHANGED.
//
// The platform admin is not a caller here: its password is no test input.
// The owner's Project's Setup creates a repository, so the test requires
// SUPERVERSE_TEST_GITHUB_EFFECTS=true. The TeamSpace admin deletes the
// Project at the end; the TeamSpace is deleted after it.
func TestProjectAuthorizationMatrixDexIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("a Project's Setup creates its repository: requires SUPERVERSE_TEST_GITHUB_EFFECTS=true")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	key := []byte(strings.Repeat("m", 32))
	teamSpaceFlow, projectFlow, setupFlow, releaseFlow := &teamspaces.TeamSpaceFlow{}, &projects.ProjectFlow{}, &setup.ProjectSetupFlow{}, &releases.ProjectReleaseFlow{}
	deployFlow, previewFlow := &deployments.ProjectDeployFlow{}, &preview.PreviewFlow{}
	sdk := dexflowtest.ServerWorkerClient(t, teamSpaceFlow, projectFlow, setupFlow, releaseFlow, deployFlow, previewFlow)
	teamSpaceClient := &teamspaces.TeamSpaceFlowClient{Dex: sdk, Flow: teamSpaceFlow}
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), Members: memberdao.New(db), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmails: []string{"admin@superverse.test"}}
	releaseClient := releases.NewProjectReleaseFlowClient(releaseFlow, projectClient, nil)
	releaseClient.Rows = deliverydao.New(db)
	deployClient := deployments.NewProjectDeployFlowClient(projectClient, nil, nil)
	deployClient.Rows, deployClient.Releases = deliverydao.New(db), releaseClient
	b := &boundary.Boundary{Logger: slog.Default(), Accounts: &accounts.Accounts{DAO: accountdao.New(db, []string{"admin@superverse.test"}), Signer: comm.Must(accounts.NewTokenSigner(key))}, Cursors: watch.NewCursorCodec(key)}
	connection := serveLoopback(ctx, t, &Server{Boundary: b, ProjectIDLength: projectClient.ProjectIDLength, Projects: projectClient, TeamSpaces: teamSpaceClient,
		TeamSpaceReads: teamspacedao.New(db), Members: memberdao.New(db), Setups: &setup.ProjectSetupFlowClient{Dex: sdk, Flow: setupFlow}, Releases: releaseClient,
		Deployments: deployClient, Publishing: deployments.NewPublishingFlowService(projectClient, deployClient, nil, nil, deployments.BuildLogReads{PerSecond: 2}), Sessions: &agents.SessionFlowService{Previews: &preview.PreviewFlowClient{Dex: sdk, Flow: previewFlow}},
		RecentEvents: slackrelay.New(slackrelay.Config{}, connectiondao.New(db), memory.New(eventqueue.Limits{MaxItems: 1, MaxBytes: 1, MaxItemBytes: 1, MaxKeyItems: 1, Concurrency: 1}, slog.Default()), nil, nil, slog.Default())})

	teamSpaceAPI, memberAPI := pb.NewTeamSpaceServiceClient(connection), pb.NewTeamSpaceMemberServiceClient(connection)
	projectAPI, projectMemberAPI := pb.NewProjectServiceClient(connection), pb.NewProjectMemberServiceClient(connection)
	releaseAPI, accountAPI := pb.NewReleaseServiceClient(connection), pb.NewAccountServiceClient(connection)
	deploymentAPI := pb.NewDeploymentServiceClient(connection)

	id := flowid.RandomID(10)
	callers := map[string]*pb.CallerContext{}
	emails := map[string]string{}
	names := []string{"admin", "owner", "builder", "operator", "member", "outsider", "removed"}
	for _, name := range names {
		callers[name], emails[name] = registerNamedCaller(ctx, t, accountAPI, name+"-"+id)
	}
	created, err := teamSpaceAPI.CreateTeamSpace(ctx, &pb.CreateTeamSpaceRequest{RequestId: "matrix-" + id, Name: "Matrix " + id, Caller: callers["admin"]})
	if err != nil {
		t.Fatalf("create the TeamSpace: %v", err)
	}
	teamSpaceID := created.GetTeamSpaceId()
	t.Cleanup(func() { deleteTeamSpace(ctx, t, teamSpaceClient, teamSpaceID) })
	awaitTrue(ctx, t, "the TeamSpace's row", func() bool {
		_, err := teamSpaceAPI.GetTeamSpace(ctx, &pb.GetTeamSpaceRequest{TeamSpaceId: teamSpaceID, Caller: callers["admin"]})
		return err == nil
	})
	for _, name := range []string{"owner", "builder", "operator", "member", "removed"} {
		if _, err = memberAPI.AddTeamSpaceMember(ctx, &pb.AddTeamSpaceMemberRequest{TeamSpaceId: teamSpaceID, Email: emails[name], Role: pb.TeamSpaceRole_TEAM_SPACE_ROLE_MEMBER, Caller: callers["admin"]}); err != nil {
			t.Fatalf("add %s to the TeamSpace: %v", name, err)
		}
		awaitTrue(ctx, t, name+"'s member row", func() bool {
			_, err := teamSpaceAPI.GetTeamSpace(ctx, &pb.GetTeamSpaceRequest{TeamSpaceId: teamSpaceID, Caller: callers[name]})
			return err == nil
		})
	}

	project, err := projectAPI.CreateProject(ctx, &pb.CreateProjectRequest{RequestId: "matrix-" + id, Name: "Matrix " + id, ManagedRepository: &pb.ManagedRepositorySource{},
		TeamSpaceId: teamSpaceID, Caller: callers["owner"]})
	if err != nil {
		t.Fatalf("the owner creates a Project: %v", err)
	}
	projectID := project.GetProjectId()
	t.Cleanup(func() { projecttest.DeleteForTest(context.WithoutCancel(ctx), t, projectClient, projectID) })
	awaitTrue(ctx, t, "the owner's view of the Project", func() bool {
		got, err := projectAPI.GetProject(ctx, &pb.GetProjectRequest{ProjectId: projectID, Caller: callers["owner"]})
		return err == nil && got.GetProject().GetCallerRole() == pb.ProjectRole_PROJECT_ROLE_OWNER
	})
	for name, role := range map[string]pb.ProjectRole{"builder": pb.ProjectRole_PROJECT_ROLE_BUILDER, "operator": pb.ProjectRole_PROJECT_ROLE_OPERATOR, "removed": pb.ProjectRole_PROJECT_ROLE_BUILDER} {
		if _, err = projectMemberAPI.AddProjectMember(ctx, &pb.AddProjectMemberRequest{ProjectId: projectID, AccountId: callers[name].GetAccountId(), Role: role, Caller: callers["owner"]}); err != nil {
			t.Fatalf("the owner adds %s: %v", name, err)
		}
	}
	rows := []string{callers["owner"].GetAccountId() + ":" + projects.RoleOwner, callers["builder"].GetAccountId() + ":" + projects.RoleBuilder,
		callers["operator"].GetAccountId() + ":" + projects.RoleOperator, callers["removed"].GetAccountId() + ":" + projects.RoleBuilder}
	slices.Sort(rows)
	awaitRows(ctx, t, db, projectID, rows)
	// Exercise role changes through the authenticated boundary, not only initial membership.
	for _, role := range []pb.ProjectRole{pb.ProjectRole_PROJECT_ROLE_OPERATOR, pb.ProjectRole_PROJECT_ROLE_BUILDER} {
		changed, err := projectMemberAPI.SetProjectMemberRole(ctx, &pb.SetProjectMemberRoleRequest{ProjectId: projectID, AccountId: callers["builder"].GetAccountId(), Role: role, Caller: callers["owner"]})
		if err != nil {
			t.Fatalf("the owner changes the builder's role: %v", err)
		}
		if !slices.ContainsFunc(changed.GetMembers(), func(member *pb.ProjectMemberSnapshot) bool {
			return member.GetAccountId() == callers["builder"].GetAccountId() && member.GetRole() == role
		}) {
			t.Fatal("the role change did not acknowledge the accepted role")
		}
	}
	awaitRows(ctx, t, db, projectID, rows)
	if _, err = memberAPI.RemoveTeamSpaceMember(ctx, &pb.RemoveTeamSpaceMemberRequest{TeamSpaceId: teamSpaceID, AccountId: callers["removed"].GetAccountId(), Caller: callers["admin"]}); err != nil {
		t.Fatalf("remove the builder from the TeamSpace: %v", err)
	}
	awaitTrue(ctx, t, "the removed builder's refusal", func() bool {
		_, err := projectAPI.GetProject(ctx, &pb.GetProjectRequest{ProjectId: projectID, Caller: callers["removed"]})
		return status.Code(err) == codes.PermissionDenied
	})

	// The cancellations need the READY Project.
	projecttest.AwaitReadyForTest(ctx, t, projectClient, projectID)
	type call func(*pb.CallerContext) error
	calls := []struct {
		name    string
		allowed []string
		// served is the code an allowed caller gets.
		served codes.Code
		call   call
	}{
		{"GetProject", []string{"owner", "builder", "operator"}, codes.OK, func(c *pb.CallerContext) error {
			_, err := projectAPI.GetProject(ctx, &pb.GetProjectRequest{ProjectId: projectID, Caller: c})
			return err
		}},
		{"ListProjectMembers", []string{"owner", "builder", "operator"}, codes.OK, func(c *pb.CallerContext) error {
			_, err := projectMemberAPI.ListProjectMembers(ctx, &pb.ListProjectMembersRequest{ProjectId: projectID, Caller: c})
			return err
		}},
		{"GetRelease of a missing Release", []string{"owner", "builder", "operator"}, codes.NotFound, func(c *pb.CallerContext) error {
			_, err := releaseAPI.GetRelease(ctx, &pb.GetReleaseRequest{ProjectId: projectID, ReleaseId: strings.Repeat("a", 40), Caller: c})
			return err
		}},
		{"GetReleaseBuildLog of a missing Release", []string{"owner", "builder"}, codes.NotFound, func(c *pb.CallerContext) error {
			_, err := releaseAPI.GetReleaseBuildLog(ctx, &pb.GetReleaseBuildLogRequest{ProjectId: projectID, ReleaseId: strings.Repeat("a", 40), BuildRevisionId: "build-" + uuid.NewString(), Caller: c})
			return err
		}},
		{"CancelReleaseBuild of a missing Release", []string{"owner", "builder"}, codes.NotFound, func(c *pb.CallerContext) error {
			_, err := releaseAPI.CancelReleaseBuild(ctx, &pb.CancelReleaseBuildRequest{ProjectId: projectID, RequestId: "cancel-" + flowid.RandomID(12), ReleaseId: strings.Repeat("a", 40),
				BuildRevisionId: "build-" + uuid.NewString(), Caller: c})
			return err
		}},
		{"CancelDeployment of a missing deployment", []string{"owner", "builder"}, codes.NotFound, func(c *pb.CallerContext) error {
			_, err := deploymentAPI.CancelDeployment(ctx, &pb.CancelDeploymentRequest{ProjectId: projectID, DeploymentId: uuid.NewString(), RequestId: "cancel-" + flowid.RandomID(12), Caller: c})
			return err
		}},
		{"ListReleases", []string{"owner", "builder"}, codes.OK, func(c *pb.CallerContext) error {
			_, err := releaseAPI.ListReleases(ctx, &pb.ListReleasesRequest{ProjectId: projectID, Caller: c})
			return err
		}},
		// A Project that never ran a Preview command has no starts; the empty history is still served.
		{"ListEnvironmentEvents of Preview", []string{"owner", "builder"}, codes.OK, func(c *pb.CallerContext) error {
			_, err := projectAPI.ListEnvironmentEvents(ctx, &pb.ListEnvironmentEventsRequest{ProjectId: projectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW, Caller: c})
			return err
		}},
		{"ListEnvironmentEvents of Live", []string{"owner", "builder", "operator"}, codes.OK, func(c *pb.CallerContext) error {
			_, err := projectAPI.ListEnvironmentEvents(ctx, &pb.ListEnvironmentEventsRequest{ProjectId: projectID, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, Caller: c})
			return err
		}},
		{"ListRecentTriggerEvents of Preview", []string{"owner", "builder"}, codes.OK, func(c *pb.CallerContext) error {
			_, err := projectAPI.ListRecentTriggerEvents(ctx, &pb.ListRecentTriggerEventsRequest{ProjectId: projectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW, Caller: c})
			return err
		}},
		{"ListRecentTriggerEvents of Live", []string{"owner", "builder", "operator"}, codes.OK, func(c *pb.CallerContext) error {
			_, err := projectAPI.ListRecentTriggerEvents(ctx, &pb.ListRecentTriggerEventsRequest{ProjectId: projectID, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, Caller: c})
			return err
		}},
		{"RemoveProjectMember of an absent account", []string{"owner"}, codes.OK, func(c *pb.CallerContext) error {
			_, err := projectMemberAPI.RemoveProjectMember(ctx, &pb.RemoveProjectMemberRequest{ProjectId: projectID, AccountId: "absent-" + id, Caller: c})
			return err
		}},
		{"DeleteProject at a stale revision", []string{"owner", "admin"}, codes.Aborted, func(c *pb.CallerContext) error {
			_, err := projectAPI.DeleteProject(ctx, &pb.DeleteProjectRequest{ProjectId: projectID, RequestId: "stale-" + flowid.RandomID(12), ExpectedRevision: 1 << 40, Caller: c})
			return err
		}},
		{"ListProjects of the TeamSpace", []string{"owner", "builder", "operator", "member", "admin"}, codes.OK, func(c *pb.CallerContext) error {
			_, err := projectAPI.ListProjects(ctx, &pb.ListProjectsRequest{TeamSpaceId: teamSpaceID, Caller: c})
			return err
		}},
	}
	for _, c := range calls {
		for _, name := range names {
			want := codes.PermissionDenied
			if slices.Contains(c.allowed, name) {
				want = c.served
			}
			if err := c.call(callers[name]); status.Code(err) != want {
				t.Errorf("%s by %s answered %v, want %s", c.name, name, err, want)
			}
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	_, err = projectAPI.DeleteProject(ctx, &pb.DeleteProjectRequest{ProjectId: projectID, RequestId: "stale-" + id, ExpectedRevision: 1 << 40, Caller: callers["owner"]})
	requireRevisionChanged(t, err)

	// A member without a role sees the Project reduced; one with a role in full.
	entries := func(caller *pb.CallerContext, teamSpaceID string) map[string]*pb.ProjectSnapshot {
		listed, err := projectAPI.ListProjects(ctx, &pb.ListProjectsRequest{TeamSpaceId: teamSpaceID, Caller: caller})
		if err != nil {
			t.Fatalf("ListProjects (TeamSpace %q): %v", teamSpaceID, err)
		}
		byID := map[string]*pb.ProjectSnapshot{}
		for _, entry := range listed.GetProjects() {
			byID[entry.GetProjectId()] = entry
		}
		return byID
	}
	reduced, full := entries(callers["member"], teamSpaceID)[projectID], entries(callers["builder"], teamSpaceID)[projectID]
	if reduced == nil || reduced.GetCallerRole() != pb.ProjectRole_PROJECT_ROLE_UNSPECIFIED || reduced.GetName() == "" || reduced.GetTeamSpaceId() != teamSpaceID ||
		reduced.GetRepository() != nil || reduced.GetCreatorAccountId() != "" || reduced.GetRevision() != 0 {
		t.Fatalf("the member without a role lists %v, want the reduced entry", reduced)
	}
	if full == nil || full.GetCallerRole() != pb.ProjectRole_PROJECT_ROLE_BUILDER || full.GetRepository() == nil || full.GetCreatorAccountId() != callers["owner"].GetAccountId() {
		t.Fatalf("the builder lists %v, want the full entry", full)
	}
	_, err = projectAPI.ListProjects(ctx, &pb.ListProjectsRequest{Caller: callers["owner"]})
	requireCode(t, err, codes.InvalidArgument, "ListProjects without a TeamSpace")

	// The TeamSpace admin deletes the Project without a role in it, at the
	// revision it reads again while the Setup still moves it.
	awaitTrue(ctx, t, "the TeamSpace admin's deletion", func() bool {
		current, err := projectClient.Read(ctx, projectID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = projectAPI.DeleteProject(ctx, &pb.DeleteProjectRequest{ProjectId: projectID, RequestId: "admin-" + id, ExpectedRevision: current.Revision, Caller: callers["admin"]})
		if code := status.Code(err); code != codes.OK && code != codes.Aborted {
			t.Fatalf("the TeamSpace admin deletes the Project: %v", err)
		}
		return err == nil
	})
	t.Logf("TeamSpace %s, Project %s: capabilities by role, the removed member refused, reduced list entries, REVISION_CHANGED, the TeamSpace admin's deletion", teamSpaceID, projectID)
}
