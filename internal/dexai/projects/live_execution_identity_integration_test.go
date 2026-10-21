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
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// TestLiveExecutionIdentityDexIntegration verifies immutable accepted Live identities
// through real Project RPCs, including rejection of the retired rotation transition.
func TestLiveExecutionIdentityDexIntegration(t *testing.T) {
	projectClient, _ := teamSpaceClients(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())
	lease, err := projectClient.AcquireDeployment(ctx, project.ProjectID, projects.DeploymentLease{DeploymentID: "identity-" + project.ProjectID})
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
		return infra.ExecutionRefs{TemporalNamespace: "identity", TemporalCredential: infra.ResourceRef{Boundary: boundary, Name: name, UID: uid, Generation: generation}}
	}
	record := func(expected, next infra.ExecutionRefs) error {
		return projectClient.RecordLiveExecution(ctx, project.ProjectID, projects.LiveExecutionUpdate{Lease: lease, Expected: expected, Execution: next})
	}
	first := credential("temporal", "uid-0", 0)
	if err = record(infra.ExecutionRefs{}, first); err != nil {
		t.Fatalf("record the first credential: %s", dexflowtest.ErrorCategory(err))
	}
	if err = record(infra.ExecutionRefs{}, first); err != nil {
		t.Fatalf("repeat first identity: %s", dexflowtest.ErrorCategory(err))
	}
	next := credential("temporal", "uid-1", 1)
	requireRejection(t, record(first, next), dexflow.RejectionFailedPrecondition, "a retired credential rotation")
	requireRejection(t, record(first, credential("temporal", "uid-other", 0)), dexflow.RejectionFailedPrecondition, "a same-generation replacement")
	other := first
	other.TemporalCredential.Boundary += "-other"
	requireRejection(t, record(first, other), dexflow.RejectionFailedPrecondition, "another boundary")
	// Repeating the accepted result succeeds even with an old Expected; a
	// different result must still reconcile the current accepted identity.
	if err = record(next, first); err != nil {
		t.Fatalf("repeat accepted identity with stale Expected: %s", dexflowtest.ErrorCategory(err))
	}
	requireRejection(t, record(infra.ExecutionRefs{}, next), dexflow.RejectionAborted, "a stale Expected")
	read, err := projectClient.Read(ctx, project.ProjectID)
	if err != nil || read.LiveExecution != first {
		t.Fatalf("the accepted Live execution is %+v: %v", read.LiveExecution, err)
	}
}
