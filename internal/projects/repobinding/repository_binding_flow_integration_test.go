// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package repobinding

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// Exercise the real SDK/Server absence-vs-inactive boundary and binding lifecycle:
// the release of the archived repository completes the execution, which stays
// readable, refuses every later claim and confirms a repeated release. No
// GitHub repository or provider resource is created by this state-only Flow.
func TestRepositoryBindingDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	host := dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST")
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	flow := &RepositoryBindingFlow{}
	client := dexflowtest.WorkerClientForTest(ctx, t, address, host, flow)
	repositories := &RepositoryBindingFlowClient{Dex: client, Flow: flow}
	newRepositoryID := func() int64 {
		random, err := rand.Int(rand.Reader, big.NewInt(1<<50))
		if err != nil {
			t.Fatal(err)
		}
		return int64(8_000_000_000_000_000) + random.Int64()
	}
	// Every execution on the test's own Worker port is closed before the test ends.
	stopOnExit := func(id string) {
		t.Cleanup(func() {
			cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			var inactive *dex.FlowNotActiveOrNotFoundError
			if stopErr := client.StopFlow(cleanup, id, dex.StopOptions{Type: dex.CancelFlow, Reason: "Real repository-state integration cleanup; no provider resources"}); stopErr != nil && !errors.As(stopErr, &inactive) {
				t.Error(stopErr)
			}
		})
	}
	awaitCompleted := func(id string) {
		t.Helper()
		if _, err := client.WaitForFlow(ctx, id, dex.WaitForFlowOptions{}); err != nil {
			t.Fatalf("the released binding %s did not complete: %s", id, dexflowtest.ErrorCategory(err))
		}
		dexflowtest.RequireCompletedForTest(ctx, t, client, id)
	}
	requireRejected := func(what string, err error, code dexflow.RejectionCode) {
		t.Helper()
		var rejected *dexflow.Rejection
		if !errors.As(err, &rejected) || !rejected.HasCode(code) {
			t.Fatalf("%s: want %s, got %s", what, code, dexflowtest.ErrorCategory(err))
		}
	}

	repositoryID := newRepositoryID()
	id := flowid.RepositoryBindingFlowID(repositoryID)
	binding, err := repositories.Find(ctx, repositoryID)
	if err != nil || binding != nil {
		t.Fatalf("never-created binding must be available: binding present=%t error=%v", binding != nil, err)
	}
	stopOnExit(id)
	generation, err := repositories.Claim(ctx, "a7k2", "setup-a7k2-integration", repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	binding, err = repositories.Find(ctx, repositoryID)
	if err != nil || binding == nil || binding.Status != StatusBound || binding.ProjectID != "a7k2" || binding.Generation != generation {
		t.Fatalf("authoritative binding did not preserve claim: %v", err)
	}
	if _, err = repositories.Claim(ctx, "b8l3", "setup-b8l3-conflict", repositoryID); err == nil {
		t.Fatal("a different owner must not claim the bound repository")
	}
	requireRejected("release by another Project", repositories.Release(ctx, "b8l3", repositoryID, generation), dexflow.RejectionAborted)
	if err = repositories.Release(ctx, "a7k2", repositoryID, generation); err != nil {
		t.Fatal(err)
	}
	awaitCompleted(id)
	binding, err = repositories.Find(ctx, repositoryID)
	if err != nil || binding == nil || binding.Status != StatusReleased || binding.ProjectID != "a7k2" || binding.Generation != generation || binding.RepositoryID != repositoryID {
		t.Fatalf("the released binding must stay readable: %s", dexflowtest.ErrorCategory(err))
	}
	// The closed execution answers every later mutation from its retained state.
	if err = repositories.Release(ctx, "a7k2", repositoryID, generation); err != nil {
		t.Fatalf("a repeated release must confirm the retained release: %s", dexflowtest.ErrorCategory(err))
	}
	requireRejected("closed release by another Project", repositories.Release(ctx, "b8l3", repositoryID, generation), dexflow.RejectionAborted)
	requireRejected("closed release of another generation", repositories.Release(ctx, "a7k2", repositoryID, generation+1), dexflow.RejectionAborted)
	_, err = repositories.Claim(ctx, "a7k2", "setup-a7k2-integration", repositoryID)
	requireRejected("claim of the released repository", err, dexflow.RejectionFailedPrecondition)
	dexflowtest.RequireCompletedForTest(ctx, t, client, id)
	t.Logf("released binding completed and refused a later claim: FlowID=%s", id)

	// A binding whose start committed and whose claim never did is released at generation 0.
	unclaimedID := newRepositoryID()
	unclaimed := flowid.RepositoryBindingFlowID(unclaimedID)
	stopOnExit(unclaimed)
	request := flowid.Fingerprint(unclaimedID)
	if _, err = client.StartFlow(ctx, flow, unclaimed, nil, dex.StartFlowOptions{
		IDReusePolicy: dex.IDReuseDisallow, RequestID: &request,
		Attributes: flow.InitialAttributes(unclaimedID, dexflow.StartIdentity{RequestID: request, Fingerprint: request}),
	}); err != nil {
		t.Fatal(err)
	}
	if err = repositories.Release(ctx, "c9m4", unclaimedID, 0); err != nil {
		t.Fatal(err)
	}
	awaitCompleted(unclaimed)
	binding, err = repositories.Find(ctx, unclaimedID)
	if err != nil || binding == nil || binding.Status != StatusReleased || binding.ProjectID != "c9m4" || binding.Generation != 0 {
		t.Fatalf("the released unclaimed binding must stay readable: %s", dexflowtest.ErrorCategory(err))
	}
	t.Logf("unclaimed binding released at generation 0 and completed: FlowID=%s", unclaimed)
}
