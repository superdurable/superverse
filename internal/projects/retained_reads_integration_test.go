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
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/repobinding"
	"github.com/superdurable/superversev2/internal/projects/setup"
)

// TestMissingProjectReadsDexIntegration resolves reads of absent Project,
// Setup and repository binding executions through the actual Dex Server. No
// execution, Worker or provider resource is created by these reads.
func TestMissingProjectReadsDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectFlow, setupFlow, repositoryFlow := &projects.ProjectFlow{}, &setup.ProjectSetupFlow{}, &repobinding.RepositoryBindingFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, projectFlow, setupFlow, repositoryFlow)
	projectClient := &projects.ProjectFlowClient{Dex: client, Flow: projectFlow, ProjectIDLength: 4, PlatformAdminEmail: "admin@superverse.test"}
	setupClient := &setup.ProjectSetupFlowClient{Dex: client, Flow: setupFlow}
	repositoryClient := &repobinding.RepositoryBindingFlowClient{Dex: client, Flow: repositoryFlow}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	projectID, childID := flowid.RandomID(4), flowid.RandomID(16)
	setupFlowID := flowid.SetupFlowID(projectID, childID)
	for _, test := range []struct {
		name, flowID string
		read         func(context.Context) error
	}{
		{"Project", flowid.ProjectFlowID(projectID), func(ctx context.Context) error { _, err := projectClient.Read(ctx, projectID); return err }},
		{"ProjectChild", flowid.ProjectFlowID(projectID), func(ctx context.Context) error {
			_, err := projectClient.ReadChildForTest(ctx, projectID, flowid.SessionFlowID(projectID))
			return err
		}},
		{"Setup", setupFlowID, func(ctx context.Context) error { _, err := setupClient.Read(ctx, setupFlowID); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dexflowtest.RequireAbsentForTest(ctx, t, client, test.flowID)
			var missing *dex.FlowNotFoundError
			if err := test.read(ctx); !errors.As(err, &missing) {
				t.Fatalf("missing read classification was %s", dexflowtest.ErrorCategory(err))
			}
			dexflowtest.RequireAbsentForTest(ctx, t, client, test.flowID)
			t.Logf("FlowID=%s; missing read created no execution", test.flowID)
		})
	}
	t.Run("RepositoryBinding", func(t *testing.T) {
		repositoryID := time.Now().UnixNano()
		flowID := flowid.RepositoryBindingFlowID(repositoryID)
		dexflowtest.RequireAbsentForTest(ctx, t, client, flowID)
		binding, err := repositoryClient.Find(ctx, repositoryID)
		if err != nil || binding != nil {
			t.Fatalf("missing binding lookup returned binding=%t error=%s", binding != nil, dexflowtest.ErrorCategory(err))
		}
		dexflowtest.RequireAbsentForTest(ctx, t, client, flowID)
		t.Logf("FlowID=%s; missing lookup created no execution", flowID)
	})
}

// TestRetainedProjectReadsDexIntegration reads an actual, already deleted
// Project and its Setup through their registered read RPCs and Flow clients.
// It starts no Worker or Flow and invokes no mutation; the serving Worker must
// match the current read contracts. An operator supplies the retained
// identity from a completed journey.
func TestRetainedProjectReadsDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_PROJECT_ID")
	projectFlow, setupFlow := &projects.ProjectFlow{}, &setup.ProjectSetupFlow{}
	// The Flows provide the client registry only; every handler runs in the
	// existing Worker.
	client := dexflowtest.ClientForTest(t, address, nil, projectFlow, setupFlow)
	projectClient := &projects.ProjectFlowClient{Dex: client, Flow: projectFlow, ProjectIDLength: len(projectID), PlatformAdminEmail: "admin@superverse.test"}
	setupClient := &setup.ProjectSetupFlowClient{Dex: client, Flow: setupFlow}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	projectFlowID := flowid.ProjectFlowID(projectID)
	var project projects.Project
	t.Run("GetProject", func(t *testing.T) {
		dexflowtest.ReadRetainedForTest(ctx, t, client, projectFlowID, projectFlow.GetProject, &project)
		if project.ProjectID != projectID || project.ProjectStatus != projects.StatusDeleted || project.SetupFlowID == "" {
			t.Fatal("terminal Project identity/status/setup reference mismatch")
		}
		t.Logf("FlowID=%s business=DELETED revision=%d", projectFlowID, project.Revision)
		retained, err := projectClient.Read(ctx, projectID)
		if err != nil {
			t.Fatalf("retained Project FlowClient read failed: %s", dexflowtest.ErrorCategory(err))
		}
		if retained.ProjectID != project.ProjectID || retained.ProjectStatus != project.ProjectStatus || retained.Revision != project.Revision {
			t.Fatal("retained Project FlowClient changed authoritative snapshot")
		}
		dexflowtest.RequireCompletedForTest(ctx, t, client, projectFlowID)
	})
	t.Run("GetProjectSetup", func(t *testing.T) {
		if !strings.HasPrefix(project.SetupFlowID, flowid.SetupFlowID(projectID, "")) {
			t.Fatal("requires the authoritative SetupFlowID from GetProject")
		}
		var retainedSetup setup.Setup
		dexflowtest.ReadRetainedForTest(ctx, t, client, project.SetupFlowID, setupFlow.GetProjectSetup, &retainedSetup)
		if retainedSetup.ProjectID != projectID || project.SetupFlowID != flowid.SetupFlowID(retainedSetup.ProjectID, retainedSetup.SetupID) || retainedSetup.Status == "" {
			t.Fatal("terminal Setup identity/status mismatch")
		}
		t.Logf("FlowID=%s business=%s revision=%d", project.SetupFlowID, retainedSetup.Status, retainedSetup.Revision)
		retained, err := setupClient.Read(ctx, project.SetupFlowID)
		if err != nil {
			t.Fatalf("retained Setup FlowClient read failed: %s", dexflowtest.ErrorCategory(err))
		}
		if retained.ProjectID != retainedSetup.ProjectID || retained.SetupID != retainedSetup.SetupID || retained.Revision != retainedSetup.Revision || retained.Status != retainedSetup.Status {
			t.Fatal("retained Setup FlowClient changed authoritative snapshot")
		}
		dexflowtest.RequireCompletedForTest(ctx, t, client, project.SetupFlowID)
	})
}
