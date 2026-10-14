// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/projects/repobinding"
)

// Reads the actual Project that the real Studio/BFF/gRPC journey deleted.
// Completion is proved by the owner and its complete authoritative Map, not
// by visibility, a Stream event or an engine status alone.
func TestDeletedProjectCleanupDexIntegration(t *testing.T) {
	address, projectID := os.Getenv("SUPERVERSE_TEST_DEX_ADDRESS"), os.Getenv("SUPERVERSE_TEST_DELETED_PROJECT_ID")
	if address == "" || projectID == "" {
		t.Fatal("requires real Dex and the owned business journey's deleted Project")
	}
	flow, repositoryFlow := &ProjectFlow{}, &repobinding.RepositoryBindingFlow{}
	sdk := dexflowtest.ClientForTest(t, address, nil, flow, repositoryFlow)
	client := &ProjectFlowClient{Dex: sdk, Flow: flow, ProjectIDLength: 4, PlatformAdminEmail: "admin@superverse.test"}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	project, err := client.Read(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if project.ProjectID != projectID || project.ProjectStatus != StatusDeleted || project.CurrentLive != nil {
		t.Fatal("Project has not completed its admitted deletion")
	}
	// The terminal owner retains its admitted command as the completion evidence.
	// CompleteDeletion writes DELETED only after proving ChildFlows empty under
	// the membership lock, so DELETED also proves no cleanup member remains.
	if project.ActiveCommand == nil || project.ActiveCommand.Kind != CommandDeleteProject || project.ActiveCommand.Phase != phaseCompleteDeletion {
		t.Fatal("Project lacks its matching final deletion command")
	}
	ref := project.RepositoryBinding
	if ref.RepositoryID == 0 || ref.Generation == 0 || ref.FullName == "" {
		t.Fatal("requires the business journey's actual managed repository identity")
	}
	repositories := &repobinding.RepositoryBindingFlowClient{Dex: sdk, Flow: repositoryFlow}
	binding, err := repositories.Find(ctx, ref.RepositoryID)
	if err != nil {
		t.Fatalf("actual repository binding read failed (%T)", err)
	}
	if binding == nil || binding.RepositoryID != ref.RepositoryID || binding.Generation != ref.Generation || binding.Status != repobinding.StatusReleased || binding.ProjectID != projectID {
		t.Fatal("the retained repository owner has not released the exact project generation")
	}
	dexflowtest.RequireCompletedForTest(ctx, t, sdk, flowid.RepositoryBindingFlowID(ref.RepositoryID))
	// The real GitHub API proves the managed repository effect independently of
	// the Project's durable completion. A missing credential is an explicit failure.
	data, err := exec.CommandContext(ctx, "gh", "api", "repos/"+ref.FullName, "--jq", "{id, archived}").Output()
	if err != nil {
		t.Fatalf("actual managed repository read failed (%T)", err)
	}
	var repository struct {
		ID       int64 `json:"id"`
		Archived bool  `json:"archived"`
	}
	if err = json.Unmarshal(data, &repository); err != nil {
		t.Fatal("actual managed repository response is invalid")
	}
	if repository.ID != ref.RepositoryID || !repository.Archived {
		t.Fatal("the exact managed repository has not been archived")
	}
	t.Logf("retained repository owner released generation=%d and completed; GitHub repository=%s id=%d archived", ref.Generation, ref.FullName, ref.RepositoryID)
}
