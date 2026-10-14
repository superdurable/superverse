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
	"errors"
	"os"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects/projectdao"
	"github.com/superdurable/superversev2/internal/tenancy/accountdao"
)

// Read-only access decisions use the actual accounts table and the projects
// rows that Dex writes from ProjectFlow. This narrower authority check does not
// replace authenticated browser acceptance.
func TestAdministratorProjectAccessDexIntegration(t *testing.T) {
	address := os.Getenv("SUPERVERSE_TEST_DEX_ADDRESS")
	firstID := os.Getenv("SUPERVERSE_TEST_ADMIN_FIRST_PROJECT_ID")
	secondID := os.Getenv("SUPERVERSE_TEST_ADMIN_SECOND_PROJECT_ID")
	deletedID := os.Getenv("SUPERVERSE_TEST_ADMIN_DELETED_PROJECT_ID")
	if address == "" || firstID == "" || secondID == "" || deletedID == "" || firstID == secondID {
		t.Fatal("requires actual Dex and two retained Projects with different owners, plus an actually deleted Project")
	}
	projectFlow := &ProjectFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, projectFlow)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	admin, err := accountdao.New(db, "admin@superverse.test").AccountByEmail(ctx, "admin@superverse.test")
	if err != nil || admin.Status != accountdao.StatusActive {
		t.Fatal("requires the actual active ordinary administrator account")
	}
	boundary := &ProjectFlowClient{Dex: client, Flow: projectFlow, Projections: projectdao.New(db), ProjectIDLength: 4, PlatformAdminEmail: "admin@superverse.test"}
	first, err := boundary.Read(ctx, firstID)
	if err != nil || first.ProjectStatus == StatusDeleted {
		t.Fatal("first actual Project is unavailable")
	}
	second, err := boundary.Read(ctx, secondID)
	if err != nil || second.ProjectStatus == StatusDeleted || first.CreatorAccountID == second.CreatorAccountID || first.CreatorAccountID == admin.AccountID || second.CreatorAccountID == admin.AccountID {
		t.Fatal("requires two actual Projects owned by different ordinary users")
	}
	for _, project := range []Project{first, second} {
		if _, err = boundary.Authorize(ctx, admin.AccountID, project.ProjectID, CapabilityView); err != nil {
			t.Fatalf("administrator access rejected for Project %s (%T)", project.ProjectID, err)
		}
		if _, err = boundary.Authorize(ctx, project.CreatorAccountID, project.ProjectID, CapabilityView); err != nil {
			t.Fatalf("owner access rejected for Project %s (%T)", project.ProjectID, err)
		}
		other := first.CreatorAccountID
		if project.ProjectID == first.ProjectID {
			other = second.CreatorAccountID
		}
		for _, actor := range []string{other, "", "unregistered-admin-claim"} {
			if _, err = boundary.Authorize(ctx, actor, project.ProjectID, CapabilityView); !errors.Is(err, ErrForbidden) {
				t.Fatalf("ordinary/empty/unregistered actor was not rejected (%T)", err)
			}
		}
	}
	deleted, err := boundary.Read(ctx, deletedID)
	if err != nil || deleted.ProjectStatus != StatusDeleted {
		t.Fatal("requires an actual terminal DELETED Project snapshot")
	}
	if _, err = boundary.Authorize(ctx, admin.AccountID, deletedID, CapabilityView); !errors.Is(err, ErrForbidden) {
		t.Fatal("ordinary deleted-resource access was not rejected")
	}
	if _, err = boundary.ReadAuthorized(ctx, admin.AccountID, deletedID, CapabilityView); err != nil {
		t.Fatalf("administrator terminal snapshot access was rejected (%T)", err)
	}
	t.Logf("actual authority: administrator/owners accepted; cross-owner and unregistered actors rejected; terminal cleanup snapshot readable; Projects=%s,%s deleted=%s", firstID, secondID, deletedID)
}
