// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projecttest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// deletionWait bounds a Project's deletion: its cleanup completes the Session,
// ends the Preview and releases the Sandbox and the project's storage.
const deletionWait = 10 * time.Minute

// IDLengthForTest is the Project ID length of the server under test,
// SUPERVERSE_TEST_PROJECT_ID_LENGTH, with which a ProjectFlow client derives
// the IDs of the Projects it creates.
func IDLengthForTest(t testing.TB) int {
	t.Helper()
	length, err := strconv.Atoi(dexflowtest.Setting(t, "SUPERVERSE_TEST_PROJECT_ID_LENGTH"))
	if err != nil || length < 1 || length > 32 {
		t.Fatal("SUPERVERSE_TEST_PROJECT_ID_LENGTH must be the server's Project ID length, 1 to 32")
	}
	return length
}

// CreateForTest starts a Project as StartForTest does and returns it once it is READY,
// which starts its Session.
func CreateForTest(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, name string) projects.Project {
	t.Helper()
	return AwaitReadyForTest(ctx, t, client, StartForTest(ctx, t, client, name))
}

// StartForTest registers an ordinary account through the merged business API at
// SUPERVERSE_TEST_API_ADDRESS, starts a managed Project of that account
// through client's ProjectFlow exactly as CreateProject does and returns the
// Project's ID while its Setup runs. The Setup runs on the existing Worker and
// creates the Project's GitHub repository and storage, so StartForTest requires
// SUPERVERSE_TEST_GITHUB_EFFECTS=true. When the test ends the Project is
// deleted, and the cleanup waits for its DELETED snapshot, so the Session,
// its Sandbox and the Preview end with the test.
func StartForTest(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, name string) string {
	t.Helper()
	return StartAfterForTest(ctx, t, client, name, nil)
}

// StartAfterForTest starts a Project as StartForTest does, after before, unless nil, ran with
// the ID the Project gets, so that a test can stage what the Project's Flows
// meet before the Project exists.
func StartAfterForTest(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, name string, before func(projectID string)) string {
	t.Helper()
	owner := registerAccount(ctx, t)
	return startProject(ctx, t, client, name, owner, TeamSpaceForTest(ctx, t, owner), before)
}

// CreateInTeamSpaceForTest creates a READY Project of owner in owner's
// TeamSpace teamSpaceID as CreateForTest does, for a test that needs two
// Projects of one TeamSpace.
func CreateInTeamSpaceForTest(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, name, owner, teamSpaceID string) projects.Project {
	t.Helper()
	return AwaitReadyForTest(ctx, t, client, startProject(ctx, t, client, name, owner, teamSpaceID, nil))
}

// CreateMatchedForTest creates a READY Project of owner in owner's TeamSpace
// teamSpaceID from the example exampleID of this build, recording what a
// CreateProject that matched it to a first message records, the facets and
// the score, but no message: its Session starts from the example as matched
// and waits for a message, for a test that reads that Session before any turn.
func CreateMatchedForTest(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, name, owner, teamSpaceID, exampleID string, matched projects.MatchedExample) projects.Project {
	t.Helper()
	template, err := apptemplate.ExampleTemplate(exampleID)
	if err != nil {
		t.Fatal(err)
	}
	return AwaitReadyForTest(ctx, t, client, startProjectAs(ctx, t, client, name, owner, teamSpaceID, nil, func(start *projects.ProjectStart) {
		start.TemplateRevision, start.InitialSession = template.Revision, &projects.SessionStartRequest{MatchedExample: &matched}
	}))
}

// CreateAtRevisionForTest creates a READY Project of owner in owner's
// TeamSpace teamSpaceID whose ProjectFlow records the template revision
// revision, such as a tree of an earlier build that this build keeps, with no
// first message.
func CreateAtRevisionForTest(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, name, owner, teamSpaceID, revision string) projects.Project {
	t.Helper()
	return AwaitReadyForTest(ctx, t, client, startProjectAs(ctx, t, client, name, owner, teamSpaceID, nil, func(start *projects.ProjectStart) {
		start.TemplateRevision = revision
	}))
}

// RegisterAccountForTest registers an ordinary account as the Projects of
// StartForTest do and returns its ID, for a test that needs a second person.
func RegisterAccountForTest(ctx context.Context, t testing.TB) string {
	t.Helper()
	return registerAccount(ctx, t)
}

// startProject starts a Project of owner in teamSpaceID, after before, unless
// nil, ran with its ID, and owns its deletion.
func startProject(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, name, owner, teamSpaceID string, before func(projectID string)) string {
	t.Helper()
	return startProjectAs(ctx, t, client, name, owner, teamSpaceID, before, func(*projects.ProjectStart) {})
}

// startProjectAs is startProject with the start that shape changes from the
// default template's.
func startProjectAs(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, name, owner, teamSpaceID string, before func(projectID string), shape func(*projects.ProjectStart)) string {
	t.Helper()
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("a Project's Setup creates its GitHub repository: requires SUPERVERSE_TEST_GITHUB_EFFECTS=true")
	}
	requestID := flowid.RandomID(24)
	projectID, setupID := flowid.ProjectCreationIDs(owner, requestID, client.ProjectIDLength)
	// CreateProject admits names of at most 120 bytes; a test's name may be longer.
	start := projects.ProjectStart{ProjectID: projectID, CreatorAccountID: owner, Name: name[:min(len(name), 120)], SetupFlowID: flowid.SetupFlowID(projectID, setupID),
		TemplateRevision: apptemplate.Current().Revision, TeamSpaceID: teamSpaceID}
	shape(&start)
	fingerprint := flowid.Fingerprint(start)
	start.Identity = dexflow.StartIdentity{RequestID: fingerprint, Fingerprint: fingerprint, ActorAccountID: owner}
	// The test owns the deletion before the start, also when the start's
	// answer is lost.
	t.Cleanup(func() {
		if _, err := deleteProject(context.WithoutCancel(ctx), client, projectID, requestID+"-cleanup"); err != nil {
			t.Errorf("%v; reconcile its exact identity", err)
		}
	})
	if before != nil {
		before(projectID)
	}
	if err := client.Create(ctx, start); err != nil {
		t.Fatalf("the start of Project %s requires reconciliation (%s)", projectID, dexflowtest.ErrorCategory(err))
	}
	t.Logf("Project %s of a test account, with the Session %s", projectID, flowid.SessionFlowID(projectID))
	return projectID
}

// AwaitReadyForTest waits until the Project's Setup made it READY, in
// ProjectFlow and in the read model row that client's Projections read, which
// the API authorizes from, and returns ProjectFlow's Project; a Project that
// leaves CREATING otherwise fails the test.
func AwaitReadyForTest(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, projectID string) projects.Project {
	t.Helper()
	for {
		project, err := client.Read(ctx, projectID)
		switch {
		case err == nil && project.ProjectStatus == projects.StatusReady:
			var row projectdao.ProjectRow
			if row, err = client.Projections.Project(ctx, projectID); err == nil && row.ProjectStatus.V == projects.StatusReady {
				return project
			}
		case err == nil && project.ProjectStatus != projects.StatusCreating:
			t.Fatalf("Project %s reached %s instead of READY", projectID, project.ProjectStatus)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Project %s did not become READY in ProjectFlow and its row (%s)", projectID, dexflowtest.ErrorCategory(err))
		case <-time.After(2 * time.Second):
		}
	}
}

// DeleteForTest requests the deletion of a Project that Create returned, as
// DeleteProject does, and returns its DELETED snapshot.
func DeleteForTest(ctx context.Context, t testing.TB, client *projects.ProjectFlowClient, projectID string) projects.Project {
	t.Helper()
	deleted, err := deleteProject(ctx, client, projectID, flowid.RandomID(24))
	if err != nil {
		t.Fatal(err)
	}
	return deleted
}

// deleteProject requests the Project's deletion at the revision it reads
// until the Project is DELETING, and then waits for its DELETED snapshot. It
// joins a deletion that is already running.
func deleteProject(ctx context.Context, client *projects.ProjectFlowClient, projectID, requestID string) (projects.Project, error) {
	wait, done := context.WithTimeout(ctx, deletionWait)
	defer done()
	var err error
	for wait.Err() == nil {
		var project projects.Project
		project, err = client.Read(wait, projectID)
		var absent *dex.FlowNotFoundError
		switch {
		case errors.As(err, &absent):
			return projects.Project{}, nil
		case err == nil && project.ProjectStatus == projects.StatusDeleted:
			return project, nil
		case err == nil && project.ProjectStatus != projects.StatusDeleting:
			// A rejection whose revision fence moved reads again.
			var rejected *dexflow.Rejection
			if _, err = client.RequestDeletion(wait, projectID, requestID, project.Revision); errors.As(err, &rejected) && rejected.HasCode(dexflow.RejectionAborted) {
				err = nil
			}
		}
		// Any other error is the Worker restarting: the next read decides.
		select {
		case <-wait.Done():
		case <-time.After(time.Second):
		}
	}
	return projects.Project{}, fmt.Errorf("project %s did not reach its DELETED snapshot within %s (%s)", projectID, deletionWait, dexflowtest.ErrorCategory(err))
}

// TeamSpaceForTest starts a TeamSpace whose one admin is owner, on the lane's
// server Worker, and returns its ID. When the test ends, after the Project's
// deletion, the platform admin deletes the TeamSpace and the cleanup waits
// until its Flow completes.
func TeamSpaceForTest(ctx context.Context, t testing.TB, owner string) string {
	t.Helper()
	flow := &teamspaces.TeamSpaceFlow{}
	teamSpaces := &teamspaces.TeamSpaceFlowClient{Dex: dexflowtest.ServerWorkerClient(t, flow), Flow: flow}
	teamSpaceID := flowid.RandomID(8)
	start := teamspaces.Start{Name: "Test TeamSpace of " + owner, CreatorAccountID: owner}
	start.Identity = dexflow.StartIdentity{RequestID: flowid.Fingerprint(start), ActorAccountID: owner}
	start.Identity.Fingerprint = start.Identity.RequestID
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), deletionWait)
		defer done()
		admin := teamspaces.Actor{AccountID: "projecttest", PlatformAdmin: true}
		_, err := teamSpaces.RequestDeletion(cleanup, teamSpaceID, teamspaces.DeletionRequest{Actor: admin, ID: "projecttest-cleanup"})
		if _, inactive := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); err != nil && !inactive {
			t.Errorf("the deletion of TeamSpace %s was not admitted (%s)", teamSpaceID, dexflowtest.ErrorCategory(err))
			return
		}
		if _, err = teamSpaces.Dex.WaitForFlow(cleanup, flowid.TeamSpaceFlowID(teamSpaceID), dex.WaitForFlowOptions{}); err != nil {
			t.Errorf("TeamSpace %s did not complete its deletion (%s)", teamSpaceID, dexflowtest.ErrorCategory(err))
		}
	})
	if err := teamSpaces.Create(ctx, teamSpaceID, start); err != nil {
		t.Fatalf("the start of TeamSpace %s requires reconciliation (%s)", teamSpaceID, dexflowtest.ErrorCategory(err))
	}
	return teamSpaceID
}

// registeredAccounts holds the sign-in of each account this process
// registered, by account ID, so that a test may sign it in again
// (LoginForTest).
var registeredAccounts sync.Map

// LoginForTest signs in an account that this process registered through the
// merged business API at SUPERVERSE_TEST_API_ADDRESS and returns a connection
// to that API with the login's caller, as the Studio BFF would send it. The
// login ends and the connection closes when the test does.
func LoginForTest(ctx context.Context, t testing.TB, accountID string) (*grpc.ClientConn, *pb.CallerContext) {
	t.Helper()
	request, found := registeredAccounts.Load(accountID)
	if !found {
		t.Fatalf("account %s was not registered by this process", accountID)
	}
	connection, err := grpc.NewClient(dexflowtest.Setting(t, "SUPERVERSE_TEST_API_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("the business API connection failed: %v", err)
	}
	accounts := pb.NewAccountServiceClient(connection)
	signedIn, err := accounts.Login(ctx, request.(*pb.LoginRequest))
	login := signedIn.GetLogin()
	if err != nil || login.GetAccount().GetAccountId() != accountID {
		t.Fatalf("the sign-in of %s failed (%s)", accountID, status.Code(err))
	}
	caller := &pb.CallerContext{AccountId: accountID, CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
	t.Cleanup(func() {
		_, _ = accounts.Logout(context.WithoutCancel(ctx), &pb.LogoutRequest{Caller: caller})
		_ = connection.Close()
	})
	return connection, caller
}

// registerAccount registers an ordinary account through the merged business
// API and returns its ID. The registration's login is revoked at once: the
// test acts through the Flows.
func registerAccount(ctx context.Context, t testing.TB) string {
	t.Helper()
	connection, err := grpc.NewClient(dexflowtest.Setting(t, "SUPERVERSE_TEST_API_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("the business API connection failed: %v", err)
	}
	defer func() { _ = connection.Close() }()
	accounts := pb.NewAccountServiceClient(connection)
	request := flowid.RandomID(24)
	registered, err := accounts.RegisterAccount(ctx, &pb.RegisterAccountRequest{RequestId: request, Email: "project-test-" + request + "@example.com", Password: request})
	login := registered.GetLogin()
	if err != nil || login.GetAccount().GetAccountId() == "" {
		t.Fatalf("the account registration failed (%s)", status.Code(err))
	}
	caller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
	if _, err = accounts.Logout(ctx, &pb.LogoutRequest{Caller: caller}); err != nil {
		t.Fatalf("the registration's login was not revoked (%s)", status.Code(err))
	}
	registeredAccounts.Store(caller.GetAccountId(), &pb.LoginRequest{Email: "project-test-" + request + "@example.com", Password: request})
	return caller.GetAccountId()
}
