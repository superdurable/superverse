// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// TestEvolveRemoteSessionSkipDexIntegration ends agent reviews skipped
// (remote_session) when the Project's Session runs a local agent, which never
// runs the platform model or its tools, on the lane's server Worker, Live
// engine, Sandbox, local Git host, storage, remote agent endpoint and the mock
// model's evolve-goals scenario. Review now admits one review a day, so each
// case has its own Live Project with the scenario's valid goals and Live runs:
//   - a local agent activated before Review now: EvolveFlow's RequestReview
//     asks the Session, whose answer skips the review;
//   - a review that waits for the agent when the local agent is activated,
//     after the Session's own turn of the review: the activation's
//     ReportEvolveRemote skips the waiting review.
func TestEvolveRemoteSessionSkipDexIntegration(t *testing.T) {
	t.Run("activated before the review", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(t.Context(), evolveTestTimeout)
		defer cancel()
		project, api, caller := measuredProject(ctx, t)
		project.ActivateLocalAgentForTest(ctx)
		reviewID := startReview(ctx, t, api, caller, project.ProjectID, uuid.NewString()).GetReviewId()
		requireRemoteSkip(ctx, t, project, reviewID)
	})
	t.Run("activated while the review waits", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(t.Context(), evolveTestTimeout)
		defer cancel()
		project, api, caller := measuredProject(ctx, t)
		reviewID := startReview(ctx, t, api, caller, project.ProjectID, uuid.NewString()).GetReviewId()
		if waiting := awaitReview(ctx, t, project, reviewID); waiting.Status != evolve.ReviewWaitingAgent {
			t.Fatalf("review %s does not wait for the agent: %+v", reviewID, waiting)
		}
		project.AwaitReviewTurnForTest(ctx, reviewID)
		project.ActivateLocalAgentForTest(ctx)
		requireRemoteSkip(ctx, t, project, reviewID)
	})
}

// measuredProject is a Live Project of the default template whose goals are
// the evolve-goals scenario's valid ones, announced as the Live build's, with
// Live runs that wait for their approval, and its EvolveService client.
func measuredProject(ctx context.Context, t *testing.T) (agents.EvolveProjectForTest, pb.EvolveServiceClient, *pb.CallerContext) {
	t.Helper()
	project := agents.NewEvolveProjectForTest(t, ctx, "")
	connection, caller := projecttest.LoginForTest(ctx, t, project.Account)
	first := awaitEvolve(ctx, t, project, "the first Live announcement", func(s evolve.Snapshot) bool { return !s.Goals.ReadAt.IsZero() })
	valid := project.PushGoalsForTest(ctx, mockInput(t, "evolve-goals", 1).Content)
	announce(ctx, t, project, first.Live.Revision+1, valid)
	awaitEvolve(ctx, t, project, "the valid goals", func(s evolve.Snapshot) bool { return s.Goals.Commit == valid && s.Goals.Status == evolve.GoalsValid })
	startLiveSummaries(ctx, t, project.LiveProjectForTest, liveSummaries)
	return project, pb.NewEvolveServiceClient(connection), caller
}

// requireRemoteSkip waits until review reviewID ended skipped because the
// Session runs a local agent, with no proposal.
func requireRemoteSkip(ctx context.Context, t *testing.T, project agents.EvolveProjectForTest, reviewID string) {
	t.Helper()
	if reviewID == "" {
		t.Fatal("Review now was not admitted")
	}
	wait, cancel := context.WithTimeout(ctx, evolveReviewTimeout)
	defer cancel()
	ended := awaitEvolve(wait, t, project, "the skip of review "+reviewID, func(s evolve.Snapshot) bool {
		status := reviewStatus(s, reviewID)
		return status != "" && status != evolve.ReviewExporting && status != evolve.ReviewWaitingAgent
	})
	for _, review := range ended.Reviews {
		if review.ID == reviewID && (review.Status != evolve.ReviewSkipped || review.Reason != evolve.ReasonRemoteSession || proposalOf(ended, reviewID) != nil) {
			t.Fatalf("review %s of a Session on a local agent ended %s (%s): %+v", reviewID, review.Status, review.Reason, review)
		}
	}
	t.Logf("FlowID=%s skipped review %s: remote_session", flowid.EvolveFlowID(project.ProjectID), reviewID)
}
