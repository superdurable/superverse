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
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/projecttest"
)

// TestLiveCredentialRotationDexIntegration records Live execution identities
// of a throwaway READY Project on the server Worker under a deployment lease:
// the next generation of the Temporal credential in the same boundary replaces
// the accepted one, as a rotated API key does, while a skipped generation, a
// same-generation replacement, another boundary and a stale Expected are
// refused and leave the accepted credential.
func TestLiveCredentialRotationDexIntegration(t *testing.T) {
	projectClient, _ := teamSpaceClients(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())
	lease, err := projectClient.AcquireDeployment(ctx, project.ProjectID, projects.DeploymentLease{DeploymentID: "rotation-" + project.ProjectID})
	if err != nil {
		t.Fatalf("acquire the deployment lease: %s", dexflowtest.ErrorCategory(err))
	}
	t.Cleanup(func() {
		release, done := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer done()
		if err := projectClient.ReleaseDeployment(release, project.ProjectID, lease); err != nil {
			t.Errorf("release the deployment lease: %s", dexflowtest.ErrorCategory(err))
		}
	})
	boundary := project.Storage.Boundary.Name
	credential := func(name, uid string, generation uint64) infra.ExecutionRefs {
		return infra.ExecutionRefs{TemporalNamespace: "rotation", TemporalCredential: infra.ResourceRef{Boundary: boundary, Name: name, UID: uid, Generation: generation}}
	}
	record := func(expected, next infra.ExecutionRefs) error {
		return projectClient.RecordLiveExecution(ctx, project.ProjectID, projects.LiveExecutionUpdate{Lease: lease, Expected: expected, Execution: next})
	}
	first := credential("temporal-credential", "uid-0", 0)
	if err = record(infra.ExecutionRefs{}, first); err != nil {
		t.Fatalf("record the first credential: %s", dexflowtest.ErrorCategory(err))
	}
	rotated := credential("temporal-credential-1", "uid-1", 1)
	if err = record(first, rotated); err != nil {
		t.Fatalf("record the rotated credential: %s", dexflowtest.ErrorCategory(err))
	}
	if err = record(first, rotated); err != nil {
		t.Fatalf("repeat the rotated credential: %s", dexflowtest.ErrorCategory(err))
	}
	requireRejection(t, record(rotated, credential("temporal-credential-3", "uid-3", 3)), dexflow.RejectionFailedPrecondition, "a skipped credential generation")
	requireRejection(t, record(rotated, credential("temporal-credential-1", "uid-other", 1)), dexflow.RejectionFailedPrecondition, "a same-generation replacement")
	other := credential("temporal-credential-2", "uid-2", 2)
	other.TemporalCredential.Boundary += "-other"
	requireRejection(t, record(rotated, other), dexflow.RejectionFailedPrecondition, "a credential in another boundary")
	requireRejection(t, record(first, credential("temporal-credential-2", "uid-2", 2)), dexflow.RejectionAborted, "a stale Expected")
	read, err := projectClient.Read(ctx, project.ProjectID)
	if err != nil || read.LiveExecution != rotated {
		t.Fatalf("the accepted Live execution is %+v: %v", read.LiveExecution, err)
	}
}
