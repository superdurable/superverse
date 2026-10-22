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
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/projects/repobinding"
)

// This deletes a throwaway READY Project with the lane's existing Worker and
// requires the deletion to end its repository binding: once the repository is
// archived the RepositoryBindingFlow completes, its retained read shows the
// Project's released generation, and a later claim of the repository is refused
// without starting the FlowID again.
func TestProjectDeletionEndsRepositoryBindingDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")

	projectFlow, repositoryFlow := &projects.ProjectFlow{}, &repobinding.RepositoryBindingFlow{}
	// This client registry supplies definitions only; every handler runs in the existing Worker.
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Pull: true}, projectFlow, repositoryFlow)
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmails: []string{"admin@superverse.test"}}
	repositories := &repobinding.RepositoryBindingFlowClient{Dex: sdk, Flow: repositoryFlow}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())
	ref := project.RepositoryBinding
	bindingID := flowid.RepositoryBindingFlowID(ref.RepositoryID)
	binding, err := repositories.Find(ctx, ref.RepositoryID)
	if err != nil || binding == nil || binding.Status != repobinding.StatusBound || binding.ProjectID != project.ProjectID || binding.Generation != ref.Generation {
		t.Fatalf("READY Project %s does not hold its binding %s (%s)", project.ProjectID, bindingID, dexflowtest.ErrorCategory(err))
	}

	deleted := projecttest.DeleteForTest(ctx, t, projectClient, project.ProjectID)
	if deleted.LastFailure != nil {
		t.Fatalf("the deletion of Project %s failed in %s with %s", project.ProjectID, deleted.LastFailure.Phase, deleted.LastFailure.Code)
	}
	if _, err = sdk.WaitForFlow(ctx, bindingID, dex.WaitForFlowOptions{}); err != nil {
		t.Fatalf("binding %s did not complete after the deletion (%s)", bindingID, dexflowtest.ErrorCategory(err))
	}
	dexflowtest.RequireCompletedForTest(ctx, t, sdk, bindingID)
	binding, err = repositories.Find(ctx, ref.RepositoryID)
	if err != nil || binding == nil || binding.Status != repobinding.StatusReleased || binding.ProjectID != project.ProjectID || binding.Generation != ref.Generation {
		t.Fatalf("the completed binding %s does not retain the Project's release (%s)", bindingID, dexflowtest.ErrorCategory(err))
	}
	_, err = repositories.Claim(ctx, project.ProjectID, project.SetupFlowID, ref.RepositoryID)
	var rejected *dexflow.Rejection
	if !errors.As(err, &rejected) || !rejected.HasCode(dexflow.RejectionFailedPrecondition) {
		t.Fatalf("a claim of the archived repository was not refused (%s)", dexflowtest.ErrorCategory(err))
	}
	dexflowtest.RequireCompletedForTest(ctx, t, sdk, bindingID)
	t.Logf("Project %s DELETED; binding %s completed RELEASED at generation %d and refused a later claim", project.ProjectID, bindingID, ref.Generation)
}
