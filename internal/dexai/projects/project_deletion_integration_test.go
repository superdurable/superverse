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

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// This deletes a throwaway READY Project whose Session is idle, with its
// actual Worker. A Local Kind journey deleting a Project saw the Session's
// completion call outlast the cleanup pass's two-second lock wait while the
// Session's engine moved to a new run, and the deletion ended as
// OUTCOME_UNKNOWN. No Session Step or RPC holds the Session's lock that long,
// so a test cannot make the Worker's own call time out on demand: the scenario
// makes the completion call of the deleting Project itself, as the cleanup pass
// does but with a lock wait that ends before any answer. That transient failure
// must leave the Session pending under the rule of StopAndCleanChildren, the
// deletion must end DELETED without recording a failure, and the Session must
// complete with its graceful receipt.
func TestDeletionAfterInterruptedSessionCompletionDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	projectFlow, flow := &projects.ProjectFlow{}, &sessionagent.SessionAgentFlow{}
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, flow)
	// This client registry supplies definitions only. Real handlers and every
	// application dependency belong to the existing Worker selected above.
	sessions := sessionagent.NewSessionAgentFlowClientForTest(sdk, flow, dexflowtest.ExecutionReaderForTest(t, address), "")
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmail: "admin@superverse.test"}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	projectID := projecttest.CreateForTest(ctx, t, projectClient, t.Name()).ProjectID
	id := agentloop.SessionFlowID(projectID)
	await := func(what string, done func() bool) {
		t.Helper()
		for !done() {
			select {
			case <-ctx.Done():
				t.Fatalf("%s did not converge; reconcile Project %s", what, projectID)
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	await("the Session's initialization", func() bool {
		snapshot, readErr := sessions.GetSnapshot(ctx, id)
		return readErr == nil && snapshot.Agent != nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})

	project, err := projectClient.Read(ctx, projectID)
	if err != nil {
		t.Fatalf("Project read failed (%T)", err)
	}
	if _, err = projectClient.RequestDeletion(ctx, projectID, "delete-interrupted", project.Revision); err != nil {
		t.Fatalf("the deletion of Project %s was not admitted (%T)", projectID, err)
	}
	// The completion call of the deleting Project fails as the journey's did:
	// its lock wait ends before the Session answers.
	completeErr := sessions.CompleteProjectSession(ctx, projectID, time.Millisecond)
	if completeErr == nil {
		t.Fatalf("a completion call of %s that its lock wait cut off reported success", id)
	}
	if !projects.ChildCallPending(ctx, completeErr) {
		t.Fatalf("the cut-off completion call of %s does not leave the Session pending (%s)", id, dexflowtest.ErrorCategory(completeErr))
	}
	t.Logf("completion call of %s cut off by its lock wait: %s, pending", id, dexflowtest.ErrorCategory(completeErr))
	await("the deletion of the Project", func() bool {
		current, readErr := projectClient.Read(ctx, projectID)
		if readErr == nil && current.LastFailure != nil {
			t.Fatalf("the deletion of Project %s failed in %s with %s: %s", projectID, current.LastFailure.Phase, current.LastFailure.Code, current.LastFailure.Message)
		}
		return readErr == nil && current.ProjectStatus == projects.StatusDeleted
	})
	if done, err := sessions.ProjectSessionCompleted(ctx, projectID); !done || err != nil {
		t.Fatalf("Session %s has no matching graceful cleanup receipt (%s)", id, dexflowtest.ErrorCategory(err))
	}
	t.Logf("Project %s DELETED; its Session completed with its cleanup receipt", projectID)
}
