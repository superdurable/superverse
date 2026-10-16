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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// teamSpaceClients are clients of the lane's server Worker for ProjectFlow and
// TeamSpaceFlow; their registries supply definitions only.
func teamSpaceClients(t *testing.T) (*projects.ProjectFlowClient, *teamspaces.TeamSpaceFlowClient) {
	t.Helper()
	projectFlow, teamSpaceFlow := &projects.ProjectFlow{}, &teamspaces.TeamSpaceFlow{}
	sdk := dexflowtest.ServerWorkerClient(t, projectFlow, teamSpaceFlow)
	db := postgrestest.OpenForTest(t)
	return &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), Members: memberdao.New(db), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmail: "admin@superverse.test"},
		&teamspaces.TeamSpaceFlowClient{Dex: sdk, Flow: teamSpaceFlow}
}

// TestTeamSpaceAdmissionDexIntegration starts Projects that their TeamSpace
// refuses, on the server Worker: one whose creator is not a member, and one
// in a TeamSpace that does not exist. Each ends DELETED with the refusal and
// completes before its Setup starts, so it created nothing.
func TestTeamSpaceAdmissionDexIntegration(t *testing.T) {
	projectClient, teamSpaces := teamSpaceClients(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	teamSpaceID := flowid.RandomID(8)
	owner, outsider := "owner-"+teamSpaceID, "outsider-"+teamSpaceID
	start := teamspaces.Start{Name: "Admission", CreatorAccountID: owner}
	start.Identity = dexflow.StartIdentity{RequestID: flowid.Fingerprint(start), ActorAccountID: owner}
	if err := teamSpaces.Create(ctx, teamSpaceID, start); err != nil {
		t.Fatalf("create TeamSpace %s: %s", teamSpaceID, dexflowtest.ErrorCategory(err))
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer done()
		admin := teamspaces.Actor{AccountID: "admin-" + teamSpaceID, PlatformAdmin: true}
		if _, err := teamSpaces.RequestDeletion(cleanup, teamSpaceID, teamspaces.DeletionRequest{Actor: admin, ID: "cleanup"}); err != nil {
			t.Errorf("delete TeamSpace %s: %s", teamSpaceID, dexflowtest.ErrorCategory(err))
		}
	})
	for _, c := range []struct{ name, teamSpaceID, creator string }{
		{"a creator who is not a member", teamSpaceID, outsider},
		{"a TeamSpace that does not exist", flowid.RandomID(8), owner},
	} {
		requestID := flowid.RandomID(24)
		projectID, setupID := flowid.ProjectCreationIDs(c.creator, requestID, projectClient.ProjectIDLength)
		in := projects.ProjectStart{ProjectID: projectID, CreatorAccountID: c.creator, Name: "Refused", TeamSpaceID: c.teamSpaceID,
			SetupFlowID: flowid.SetupFlowID(projectID, setupID), TemplateRevision: apptemplate.Current().Revision}
		in.Identity = dexflow.StartIdentity{RequestID: requestID, Fingerprint: requestID, ActorAccountID: c.creator}
		if err := projectClient.Create(ctx, in); err != nil {
			t.Fatalf("start the Project of %s: %s", c.name, dexflowtest.ErrorCategory(err))
		}
		if _, err := projectClient.Dex.WaitForFlow(ctx, flowid.ProjectFlowID(projectID), dex.WaitForFlowOptions{}); err != nil {
			t.Fatalf("the Project of %s did not complete: %s", c.name, dexflowtest.ErrorCategory(err))
		}
		refused, err := projectClient.Read(ctx, projectID)
		if err != nil || refused.ProjectStatus != projects.StatusDeleted || refused.LastFailure == nil ||
			refused.LastFailure.Code != projects.FailureTeamSpaceAdmissionForTest || refused.Storage != (projects.Project{}).Storage {
			t.Fatalf("the Project of %s is %+v: %v", c.name, refused, err)
		}
		dexflowtest.RequireAbsentForTest(ctx, t, projectClient.Dex, in.SetupFlowID)
		t.Logf("Project %s of %s: DELETED with %s, no Setup", projectID, c.name, refused.LastFailure.Message)
	}
}

// TestTeamSpaceDeletionCascadeDexIntegration deletes the TeamSpace of a READY
// Project as the platform admin: while the TeamSpace is DELETING it refuses a
// new Project, the Project's own deletion unregisters it, and the TeamSpace
// completes DELETED after the Project is DELETED.
func TestTeamSpaceDeletionCascadeDexIntegration(t *testing.T) {
	projectClient, teamSpaces := teamSpaceClients(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())
	admin := teamspaces.Actor{AccountID: "admin-" + project.ProjectID, PlatformAdmin: true}
	deleting, err := teamSpaces.RequestDeletion(ctx, project.TeamSpaceID, teamspaces.DeletionRequest{Actor: admin, ID: "cascade-" + project.ProjectID})
	if err != nil || deleting.Status != teamspaces.StatusDeleting {
		t.Fatalf("the deletion of TeamSpace %s was not admitted: %+v: %v", project.TeamSpaceID, deleting, err)
	}
	err = teamSpaces.RegisterProject(ctx, project.TeamSpaceID, flowid.RandomID(4), admin)
	if rejected, isRejected := errors.AsType[*dexflow.Rejection](err); !isRejected || !rejected.HasCode(dexflow.RejectionFailedPrecondition) {
		t.Fatalf("a deleting TeamSpace admitted a new Project: %v", err)
	}
	result, err := teamSpaces.Dex.WaitForFlow(ctx, flowid.TeamSpaceFlowID(project.TeamSpaceID), dex.WaitForFlowOptions{NeedsResults: true})
	var deleted teamspaces.TeamSpace
	if err == nil {
		err = result.DecodeSingleOutput(&deleted)
	}
	if err != nil || deleted.Status != teamspaces.StatusDeleted || deleted.LastFailure != nil {
		t.Fatalf("TeamSpace %s did not complete DELETED: %+v: %v", project.TeamSpaceID, deleted, err)
	}
	if current, err := projectClient.Read(ctx, project.ProjectID); err != nil || current.ProjectStatus != projects.StatusDeleted || current.LastFailure != nil {
		t.Fatalf("Project %s of the deleted TeamSpace is %s: %v", project.ProjectID, current.ProjectStatus, err)
	}
	t.Logf("TeamSpace %s DELETED after its Project %s unregistered in its own deletion", project.TeamSpaceID, project.ProjectID)
}

// TestTeamSpaceDeletionWorkerRestartIntegration replaces the server, and with
// it the Worker, while the TeamSpace of a READY Project deletes its Projects:
// DeleteTeamSpaceResources picks up where it was, the Project completes its
// deletion and the TeamSpace completes DELETED. It restarts the lane's server,
// so it runs alone.
func TestTeamSpaceDeletionWorkerRestartIntegration(t *testing.T) {
	projectClient, teamSpaces := teamSpaceClients(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())
	admin := teamspaces.Actor{AccountID: "admin-" + project.ProjectID, PlatformAdmin: true}
	if _, err := teamSpaces.RequestDeletion(ctx, project.TeamSpaceID, teamspaces.DeletionRequest{Actor: admin, ID: "restart-" + project.ProjectID}); err != nil {
		t.Fatalf("the deletion of TeamSpace %s was not admitted: %v", project.TeamSpaceID, err)
	}
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, err := projectClient.Read(ctx, project.ProjectID)
		return err
	})
	result, err := teamSpaces.Dex.WaitForFlow(ctx, flowid.TeamSpaceFlowID(project.TeamSpaceID), dex.WaitForFlowOptions{NeedsResults: true})
	var deleted teamspaces.TeamSpace
	if err == nil {
		err = result.DecodeSingleOutput(&deleted)
	}
	if err != nil || deleted.Status != teamspaces.StatusDeleted || deleted.LastFailure != nil {
		t.Fatalf("TeamSpace %s did not complete DELETED after the restart: %+v: %v", project.TeamSpaceID, deleted, err)
	}
	if current, err := projectClient.Read(ctx, project.ProjectID); err != nil || current.ProjectStatus != projects.StatusDeleted {
		t.Fatalf("Project %s of the deleted TeamSpace is %s: %v", project.ProjectID, current.ProjectStatus, err)
	}
	t.Logf("TeamSpace %s and its Project %s DELETED across a server restart during the deletion", project.TeamSpaceID, project.ProjectID)
}

// TestTeamSpaceDeletionClosedProjectDexIntegration deletes a TeamSpace with a
// registered Project whose ProjectFlow failed before it was DELETED: the
// deletion fails at once with PROJECT_CLOSED naming the Project, leaves the
// TeamSpace DELETING without an active command, and completes once the
// Project is unregistered and the deletion is requested again. The Project
// runs on a Worker of this test, which fails it before its first Step, so it
// creates nothing; the test keeps that Worker until the TeamSpace completes,
// since a closed execution's read still runs there.
func TestTeamSpaceDeletionClosedProjectDexIntegration(t *testing.T) {
	_, teamSpaces := teamSpaceClients(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	teamSpaceID := flowid.RandomID(8)
	owner := "owner-" + teamSpaceID
	start := teamspaces.Start{Name: "Closed Project", CreatorAccountID: owner}
	start.Identity = dexflow.StartIdentity{RequestID: flowid.Fingerprint(start), ActorAccountID: owner}
	if err := teamSpaces.Create(ctx, teamSpaceID, start); err != nil {
		t.Fatalf("create TeamSpace %s: %s", teamSpaceID, dexflowtest.ErrorCategory(err))
	}
	projectFlow := &projects.ProjectFlow{TeamSpaces: teamSpaces}
	ownWorker := dexflowtest.WorkerClientForTest(ctx, t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), projectFlow)
	projectClient := &projects.ProjectFlowClient{Dex: ownWorker, Flow: projectFlow, ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmail: "admin@superverse.test"}
	requestID := flowid.RandomID(24)
	projectID, setupID := flowid.ProjectCreationIDs(owner, requestID, projectClient.ProjectIDLength)
	admin := teamspaces.Actor{AccountID: "admin-" + teamSpaceID, PlatformAdmin: true}
	// Registered after the Worker, this cleanup runs before it stops: a TeamSpace the test left
	// unfinished completes without the Project while the Project's reads still have their Worker.
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer done()
		if err := teamSpaces.UnregisterProject(cleanup, teamSpaceID, projectID); err != nil {
			t.Errorf("unregister Project %s: %v", projectID, err)
		}
		_, err := teamSpaces.RequestDeletion(cleanup, teamSpaceID, teamspaces.DeletionRequest{Actor: admin, ID: "cleanup"})
		if _, inactive := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); err != nil && !inactive {
			t.Errorf("delete TeamSpace %s: %v", teamSpaceID, err)
		}
		if _, err = teamSpaces.Dex.WaitForFlow(cleanup, flowid.TeamSpaceFlowID(teamSpaceID), dex.WaitForFlowOptions{}); err != nil {
			t.Errorf("TeamSpace %s did not complete: %s", teamSpaceID, dexflowtest.ErrorCategory(err))
		}
	})
	if err := teamSpaces.RegisterProject(ctx, teamSpaceID, projectID, teamspaces.Actor{AccountID: owner}); err != nil {
		t.Fatalf("register Project %s: %v", projectID, err)
	}
	in := projects.ProjectStart{ProjectID: projectID, CreatorAccountID: owner, Name: "Closed", TeamSpaceID: teamSpaceID,
		SetupFlowID: flowid.SetupFlowID(projectID, setupID), TemplateRevision: apptemplate.Current().Revision}
	in.Identity = dexflow.StartIdentity{RequestID: requestID, Fingerprint: requestID, ActorAccountID: owner}
	if err := projectClient.Create(ctx, in); err != nil {
		t.Fatalf("start Project %s: %s", projectID, dexflowtest.ErrorCategory(err))
	}
	if err := ownWorker.StopFlow(ctx, flowid.ProjectFlowID(projectID), dex.StopOptions{Type: dex.FailFlow, Reason: "a Project that failed before its deletion"}); err != nil {
		t.Fatalf("fail Project %s: %s", projectID, dexflowtest.ErrorCategory(err))
	}
	// WaitForFlow returns once the execution closed, whatever its outcome.
	if _, err := ownWorker.WaitForFlow(ctx, flowid.ProjectFlowID(projectID), dex.WaitForFlowOptions{}); ctx.Err() != nil {
		t.Fatalf("Project %s did not close: %s", projectID, dexflowtest.ErrorCategory(err))
	}

	if _, err := teamSpaces.RequestDeletion(ctx, teamSpaceID, teamspaces.DeletionRequest{Actor: admin, ID: "first"}); err != nil {
		t.Fatalf("request the deletion of TeamSpace %s: %v", teamSpaceID, err)
	}
	for {
		row := postgrestest.StringsForTest(ctx, t, db, `SELECT team_space_status || ':' || coalesce(last_failure->>'Code', '') || ':' || coalesce(last_failure->>'Message', '')
			FROM team_spaces WHERE team_space_id = $1`, teamSpaceID)
		if len(row) == 1 && strings.HasPrefix(row[0], teamspaces.StatusDeleting+":PROJECT_CLOSED:") && strings.Contains(row[0], projectID) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("TeamSpace %s did not record the closed Project: %v", teamSpaceID, row)
		case <-time.After(time.Second):
		}
	}
	// The failure cleared the active command, so a new deletion request is admitted.
	if err := teamSpaces.UnregisterProject(ctx, teamSpaceID, projectID); err != nil {
		t.Fatalf("unregister Project %s: %v", projectID, err)
	}
	if _, err := teamSpaces.RequestDeletion(ctx, teamSpaceID, teamspaces.DeletionRequest{Actor: admin, ID: "second"}); err != nil {
		t.Fatalf("request the deletion of TeamSpace %s again: %v", teamSpaceID, err)
	}
	if _, err := teamSpaces.Dex.WaitForFlow(ctx, flowid.TeamSpaceFlowID(teamSpaceID), dex.WaitForFlowOptions{}); err != nil {
		t.Fatalf("TeamSpace %s did not complete: %s", teamSpaceID, dexflowtest.ErrorCategory(err))
	}
	t.Logf("TeamSpace %s failed fast on its closed Project %s and completed once the Project was unregistered", teamSpaceID, projectID)
}
