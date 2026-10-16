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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
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
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestProjectReadModelGRPCDexIntegration drives ProjectService through the
// actual gRPC boundary against the lane's Dex and PostgreSQL, whose projects
// rows platform Dex writes from the Flows of two Projects this test creates
// through CreateProject:
//   - a Project that never existed, which has no Flow, is NotFound at once to
//     GetProject, WatchProject and RemoveLive;
//   - while the test holds a SHARE lock on projects, which stops Dex's writes
//     and none of the server's reads, a Watch of the new Project waits for its
//     row and answers NotFound after 10 seconds, and the same read, Watch and
//     write, when the row appears during their wait because the lock ends,
//     are served;
//   - GetProject, ListProjects and the Watch show ProjectFlow's and its
//     Setup's state; the creator lists both Projects in pages of one, another
//     account lists neither and may not read them;
//   - a deletion fenced with an older revision is refused, the reread
//     revision admits it, and the Watch ends on the DELETED snapshot, which
//     GetProject hides.
func TestProjectReadModelGRPCDexIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("CreateProject runs a Project's Setup, which creates its repository: requires SUPERVERSE_TEST_GITHUB_EFFECTS=true")
	}
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	projectFlow, setupFlow := &projects.ProjectFlow{}, &setup.ProjectSetupFlow{}
	// This client registry supplies definitions only; the existing Worker
	// runs both Flows with every dependency of its server.
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, setupFlow)
	db := postgrestest.OpenForTest(t)
	idLength := projecttest.IDLengthForTest(t)
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), ProjectIDLength: idLength, PlatformAdminEmail: "admin@superverse.test"}
	signingKey := []byte(strings.Repeat("r", 32))
	service := &accounts.Accounts{DAO: accountdao.New(db, "admin@superverse.test"), Signer: comm.Must(accounts.NewTokenSigner(signingKey))}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	setupClient := &setup.ProjectSetupFlowClient{Dex: sdk, Flow: setupFlow}
	connection := serveLoopback(ctx, t, &Server{
		Boundary:        &boundary.Boundary{Logger: slog.Default(), Accounts: service, Dex: sdk, Cursors: watch.NewCursorCodec(signingKey)},
		ProjectIDLength: idLength, Projects: projectClient, Members: memberdao.New(db), Setups: setupClient,
	})
	// flows shows a Project as a write does: ProjectFlow's state with its Setup's.
	flows := &projectServer{projects: projectClient, setups: setupClient}
	api := pb.NewProjectServiceClient(connection)
	owner, other := registerCaller(ctx, t, service), registerCaller(ctx, t, service)
	// The owner's TeamSpace is deleted last, after its Projects.
	teamSpaceID := projecttest.TeamSpaceForTest(ctx, t, owner.GetAccountId())

	// A Project that never existed has no Flow: its reads and writes are
	// NotFound at once, without waiting for a row.
	missing, _ := flowid.ProjectCreationIDs(flowid.RandomID(12), flowid.RandomID(24), idLength)
	started := time.Now()
	for name, call := range projectCalls(api, owner, missing) {
		if err := call(ctx); status.Code(err) != codes.NotFound || time.Since(started) > time.Second {
			t.Fatalf("%s of the Project %s that never existed answered %v after %s, not NotFound at once", name, missing, status.Code(err), time.Since(started).Round(time.Millisecond))
		}
		started = time.Now()
	}

	// Dex cannot write the new Project's row while the lock is held. The Flow's
	// RPCs then wait about 7 seconds for its blocked sync (superdurable/dex#623),
	// so the existence read before the wait is that slow; the bounds allow it.
	release := postgrestest.LockTableForTest(ctx, t, "projects")
	first := createProject(ctx, t, api, owner, teamSpaceID, projectClient)
	started = time.Now()
	if _, err := firstWatchSnapshot(ctx, api, owner, first); status.Code(err) != codes.NotFound || time.Since(started) < 9*time.Second {
		t.Fatalf("the Watch of Project %s without a row answered %v after %s, not NotFound after its 10-second wait", first, status.Code(err), time.Since(started).Round(time.Second))
	}
	answers := make(chan error)
	calls := projectCalls(api, owner, first)
	for name, call := range calls {
		go func() {
			if err := call(ctx); err != nil {
				answers <- fmt.Errorf("%s: %w", name, err)
				return
			}
			answers <- nil
		}()
	}
	time.Sleep(2 * time.Second)
	release()
	for range calls {
		if err := <-answers; err != nil {
			t.Fatalf("Project %s, whose row appeared while the request waited: %v", first, err)
		}
	}
	t.Logf("Project %s: requests waited 10 seconds for its row while Dex could not write it, and those that outlasted the lock were served; a Project that never existed was NotFound at once", first)

	second := createProject(ctx, t, api, owner, teamSpaceID, projectClient)
	for _, projectID := range []string{first, second} {
		projecttest.AwaitReadyForTest(ctx, t, projectClient, projectID)
		awaitFlowSnapshot(ctx, t, api, owner, flows, teamSpaceID, projectID)
	}
	expectPages(ctx, t, api, owner, teamSpaceID, [][]string{{second}, {first}})
	if _, err := api.ListProjects(ctx, &pb.ListProjectsRequest{Caller: other, TeamSpaceId: teamSpaceID}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another account listed the TeamSpace's Projects: %v", status.Code(err))
	}
	for _, projectID := range []string{first, second} {
		if _, err := api.GetProject(ctx, &pb.GetProjectRequest{Caller: other, ProjectId: projectID}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("another account read Project %s: %v", projectID, status.Code(err))
		}
		if _, err := firstWatchSnapshot(ctx, api, other, projectID); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("another account watched Project %s: %v", projectID, status.Code(err))
		}
	}

	ready, err := api.GetProject(ctx, &pb.GetProjectRequest{Caller: owner, ProjectId: second})
	if err != nil {
		t.Fatal(err)
	}
	stale := ready.GetProject().GetRevision() - 1
	if _, err = api.DeleteProject(ctx, &pb.DeleteProjectRequest{Caller: owner, ProjectId: second, RequestId: flowid.RandomID(24), ExpectedRevision: stale}); status.Code(err) != codes.Aborted {
		t.Fatalf("a deletion of Project %s fenced at the older revision %d answered %v, not Aborted", second, stale, status.Code(err))
	}
	deleting, err := api.DeleteProject(ctx, &pb.DeleteProjectRequest{Caller: owner, ProjectId: second, RequestId: flowid.RandomID(24), ExpectedRevision: ready.GetProject().GetRevision()})
	if err != nil || deleting.GetProject().GetStatus() != pb.ProjectStatus_PROJECT_STATUS_DELETING {
		t.Fatalf("the deletion of Project %s at the reread revision: %v", second, err)
	}
	awaitDeletedSnapshot(ctx, t, api, owner, second)
	if _, err = api.GetProject(ctx, &pb.GetProjectRequest{Caller: owner, ProjectId: second}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("GetProject served the DELETED Project %s: %v", second, status.Code(err))
	}
	t.Logf("Projects %s and %s: reads equal the Flows, pages hold one each, another account sees none, an older fence is refused and the Watch ended on DELETED", first, second)
}

// projectCalls are a read, a Watch and a write of a Project by ID, each of
// which authorizes the caller from the Project's row: GetProject, WatchProject
// and RemoveLive, which returns a Project without Live unchanged.
func projectCalls(api pb.ProjectServiceClient, caller *pb.CallerContext, projectID string) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"GetProject": func(ctx context.Context) error {
			_, err := api.GetProject(ctx, &pb.GetProjectRequest{Caller: caller, ProjectId: projectID})
			return err
		},
		"WatchProject": func(ctx context.Context) error {
			_, err := firstWatchSnapshot(ctx, api, caller, projectID)
			return err
		},
		"RemoveLive": func(ctx context.Context) error {
			_, err := api.RemoveLive(ctx, &pb.RemoveLiveRequest{Caller: caller, ProjectId: projectID, RequestId: flowid.RandomID(24)})
			return err
		},
	}
}

// registerCaller registers an ordinary account and returns its explicit
// caller identity, which is revoked when the test ends.
func registerCaller(ctx context.Context, t *testing.T, service *accounts.Accounts) *pb.CallerContext {
	t.Helper()
	request := flowid.RandomID(24)
	login, err := service.Register(ctx, "read-model-"+request+"@example.com", request, request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if logoutErr := service.Logout(context.WithoutCancel(ctx), login.Token); logoutErr != nil {
			t.Errorf("revoke the test login (%T)", logoutErr)
		}
	})
	return &pb.CallerContext{AccountId: login.Account.AccountID, CanonicalEmail: login.Account.CanonicalEmail, SessionToken: login.Token}
}

// createProject creates a managed Project of the TeamSpace through the API
// and deletes it when the test ends.
func createProject(ctx context.Context, t *testing.T, api pb.ProjectServiceClient, caller *pb.CallerContext, teamSpaceID string, client *projects.ProjectFlowClient) string {
	t.Helper()
	created, err := api.CreateProject(ctx, &pb.CreateProjectRequest{Caller: caller, RequestId: flowid.RandomID(24), Name: t.Name(), ManagedRepository: &pb.ManagedRepositorySource{},
		TeamSpaceId: teamSpaceID})
	if err != nil {
		t.Fatal(err)
	}
	projectID := created.GetProjectId()
	t.Cleanup(func() { projecttest.DeleteForTest(context.WithoutCancel(ctx), t, client, projectID) })
	return projectID
}

// firstWatchSnapshot opens a Project Watch and returns its first snapshot.
func firstWatchSnapshot(ctx context.Context, api pb.ProjectServiceClient, caller *pb.CallerContext, projectID string) (*pb.ProjectSnapshot, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := api.WatchProject(ctx, &pb.WatchProjectRequest{Caller: caller, ProjectId: projectID})
	if err != nil {
		return nil, err
	}
	frame, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	if frame.GetSnapshot() == nil {
		return nil, errors.New("the Watch began without a snapshot")
	}
	return frame.GetSnapshot(), nil
}

// awaitFlowSnapshot waits until GetProject, the caller's listing and a Watch
// show exactly what ProjectFlow and its Setup hold, which flows reads, with
// timestamps to the microsecond that PostgreSQL keeps.
func awaitFlowSnapshot(ctx context.Context, t *testing.T, api pb.ProjectServiceClient, caller *pb.CallerContext, flows *projectServer, teamSpaceID, projectID string) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		current, err := flows.projects.Read(ctx, projectID)
		if err != nil {
			t.Fatal(err)
		}
		want, err := flows.projectResponse(ctx, current, projects.RoleOwner)
		if err != nil {
			t.Fatal(err)
		}
		got, err := api.GetProject(ctx, &pb.GetProjectRequest{Caller: caller, ProjectId: projectID})
		if err != nil {
			t.Fatal(err)
		}
		listed, err := api.ListProjects(ctx, &pb.ListProjectsRequest{Caller: caller, TeamSpaceId: teamSpaceID})
		if err != nil {
			t.Fatal(err)
		}
		watched, err := firstWatchSnapshot(ctx, api, caller, projectID)
		if err != nil {
			t.Fatal(err)
		}
		agree := sameSnapshot(got.GetProject(), want) && sameSnapshot(watched, want)
		for _, entry := range listed.GetProjects() {
			if entry.GetProjectId() == projectID {
				agree = agree && sameSnapshot(entry, want)
			}
		}
		if agree {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Project %s: the API shows %v, ProjectFlow and its Setup %v", projectID, got.GetProject(), want)
		}
		time.Sleep(time.Second)
	}
}

// sameSnapshot compares two Project snapshots with their times to the
// microsecond.
func sameSnapshot(left, right *pb.ProjectSnapshot) bool {
	left, right = proto.CloneOf(left), proto.CloneOf(right)
	for _, snapshot := range []*pb.ProjectSnapshot{left, right} {
		for _, at := range []**timestamppb.Timestamp{&snapshot.CreatedAt, &snapshot.UpdatedAt} {
			*at = timestamppb.New((*at).AsTime().Truncate(time.Microsecond))
		}
	}
	return proto.Equal(left, right)
}

// expectPages lists the TeamSpace's Projects one a page and requires the pages
// to name exactly the wanted Projects, newest first, with no token after the
// last.
func expectPages(ctx context.Context, t *testing.T, api pb.ProjectServiceClient, caller *pb.CallerContext, teamSpaceID string, want [][]string) {
	t.Helper()
	var pages [][]string
	token := ""
	for {
		page, err := api.ListProjects(ctx, &pb.ListProjectsRequest{Caller: caller, TeamSpaceId: teamSpaceID, PageSize: 1, PageToken: token})
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, project := range page.GetProjects() {
			ids = append(ids, project.GetProjectId())
		}
		if ids != nil {
			pages = append(pages, ids)
		}
		if token = page.GetNextPageToken(); token == "" {
			break
		}
	}
	if len(pages) != len(want) {
		t.Fatalf("the listing of %s pages as %v, not %v", caller.GetAccountId(), pages, want)
	}
	for index := range want {
		if strings.Join(pages[index], ",") != strings.Join(want[index], ",") {
			t.Fatalf("the listing of %s pages as %v, not %v", caller.GetAccountId(), pages, want)
		}
	}
}

// awaitDeletedSnapshot follows a Project Watch until its DELETED snapshot
// ends it.
func awaitDeletedSnapshot(ctx context.Context, t *testing.T, api pb.ProjectServiceClient, caller *pb.CallerContext, projectID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	stream, err := api.WatchProject(ctx, &pb.WatchProjectRequest{Caller: caller, ProjectId: projectID})
	if err != nil {
		t.Fatal(err)
	}
	last := pb.ProjectStatus_PROJECT_STATUS_UNSPECIFIED
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) && last == pb.ProjectStatus_PROJECT_STATUS_DELETED {
			return
		}
		if err != nil {
			t.Fatalf("the Watch of Project %s ended at %s: %v", projectID, last, err)
		}
		if snapshot := frame.GetSnapshot(); snapshot != nil {
			last = snapshot.GetStatus()
		}
	}
}
