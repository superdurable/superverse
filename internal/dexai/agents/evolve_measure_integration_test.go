// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi"
	"github.com/superdurable/superversev2/internal/dexai/studio/runs"
)

const (
	// evolveTestTimeout bounds a throwaway Project's first Live deployment,
	// two pushes through the commit gate and a review of a few runs.
	evolveTestTimeout = 150 * time.Minute
	// evolveReviewTimeout bounds one review of a few runs: its export, its
	// manifest and its metrics.
	evolveReviewTimeout = 15 * time.Minute
	// liveSummaries is how many Live runs of the template's TextSummaryFlow a
	// review measures; the throwaway Project has no others.
	liveSummaries = 3
	// The goals of the evolve-goals scenario's valid file.
	goalRequests  = "requests"
	evolveGoalSum = 4
)

// TestEvolveMeasurementDexIntegration deploys the HEAD of a throwaway
// Project to Live (agents.NewEvolveProjectForTest) and checks its goal
// measurement on the lane's server Worker, the local Git host, the Live engine
// and storage:
//   - the deployment's announcement started EvolveFlow once, which read the
//     template's commit through the Git host and found no goals file; a
//     repeated and an older announcement change nothing, and Review now is
//     refused NO_GOALS;
//   - the announcement of a pushed commit with a valid goals file stores the
//     parsed goals and activates the weekly schedule at the Project's hour;
//   - two Review now requests at once start one review and refuse the other
//     REVIEW_RUNNING; the admitted request repeated answers its review;
//   - the review exports the Live runs into parts whose records hold each
//     run's Steps, path, release and allow-listed fields (AI usage where the
//     model ran), writes the manifest, whose source is the Live runs of this
//     review, and metrics.json, and ends measured with
//     the goal values; the next Review now is RATE_LIMITED;
//   - the announcement of a pushed commit with a refused goals file stores
//     the parser's errors, stops the schedule, and Review now is GOALS_INVALID.
//
// The pushed commits are announced through the client: measurement reads the
// goals of the announced build commit, and deploying each commit would only
// add two builds.
func TestEvolveMeasurementDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), evolveTestTimeout)
	defer cancel()
	project := agents.NewEvolveProjectForTest(t, ctx)
	connection, caller := projecttest.LoginForTest(ctx, t, project.Account)
	api := pb.NewEvolveServiceClient(connection)

	first := awaitEvolve(ctx, t, project, "the first Live announcement", func(s evolve.Snapshot) bool { return !s.Goals.ReadAt.IsZero() })
	if first.Live.Revision != project.Generation || first.Live.BuildCommit != project.ReleaseID || first.Live.CommittedAt.IsZero() ||
		first.Goals.Commit != project.ReleaseID || first.Goals.Status != evolve.GoalsNone || first.Schedule.Active || len(first.Reviews) != 0 {
		t.Fatalf("the first announcement of deployment %s did not measure the template's absent goals: %+v", project.DeploymentID, first)
	}
	older := evolve.LiveRelease{Revision: first.Live.Revision - 1, ReleaseID: project.ReleaseID, BuildCommit: project.ReleaseID, CommittedAt: time.Now().UTC()}
	for _, change := range []evolve.LiveRelease{first.Live, older} {
		if err := project.Evolve.LiveChanged(ctx, project.ProjectID, change); err != nil {
			t.Fatalf("announcing revision %d again failed (%T)", change.Revision, err)
		}
	}
	if again := readEvolve(ctx, t, project); again.Live != first.Live || !again.Goals.ReadAt.Equal(first.Goals.ReadAt) {
		t.Fatalf("a repeated or older announcement changed the measurement: %+v, then %+v", first, again)
	}
	if answer := startReview(ctx, t, api, caller, project.ProjectID, uuid.NewString()); answer.GetRefusal() != pb.EvolveReviewRefusal_EVOLVE_REVIEW_REFUSAL_NO_GOALS {
		t.Fatalf("Review now without goals answered %v", answer)
	}

	valid := project.PushGoalsForTest(ctx, mockInput(t, "evolve-goals", 1).Content)
	announce(ctx, t, project, first.Live.Revision+1, valid)
	measured := awaitEvolve(ctx, t, project, "the valid goals", func(s evolve.Snapshot) bool { return s.Goals.Commit == valid })
	now := time.Now()
	if schedule := measured.Schedule; measured.Goals.Status != evolve.GoalsValid || !schedule.Active || schedule.Period != goals.PeriodWeekly ||
		!schedule.NextAt.After(now) || schedule.NextAt.After(now.Add(7*24*time.Hour)) || schedule.AnchorHour < 0 || schedule.AnchorHour >= 7*24 {
		t.Fatalf("the valid goals of %s did not activate the weekly schedule: %+v", valid, measured)
	}
	view, err := api.GetEvolve(ctx, &pb.GetEvolveRequest{ProjectId: project.ProjectID, Caller: caller})
	if err != nil || view.GetEvolve().GetGoals().GetStatus() != pb.EvolveGoalsStatus_EVOLVE_GOALS_STATUS_VALID || len(view.GetEvolve().GetGoals().GetGoals()) != evolveGoalSum {
		t.Fatalf("GetEvolve does not show the valid goals of %s: %v (%T)", valid, view, err)
	}

	startLiveSummaries(ctx, t, project.LiveProjectForTest, liveSummaries)
	reviewID, admittedRequest := startConcurrentReviews(ctx, t, api, caller, project.ProjectID)
	if repeated := startReview(ctx, t, api, caller, project.ProjectID, admittedRequest); repeated.GetReviewId() != reviewID {
		t.Fatalf("the repeated Review now %s answered %v, not review %s", admittedRequest, repeated, reviewID)
	}
	ended := awaitReview(ctx, t, project, reviewID)
	requireMeasuredReview(t, ended, project.Account, valid)
	requireStoredExport(ctx, t, project.LiveProjectForTest, ended, valid)
	if answer := startReview(ctx, t, api, caller, project.ProjectID, uuid.NewString()); answer.GetRefusal() != pb.EvolveReviewRefusal_EVOLVE_REVIEW_REFUSAL_RATE_LIMITED {
		t.Fatalf("a second Review now within a day answered %v", answer)
	}

	invalid := project.PushGoalsForTest(ctx, mockInput(t, "evolve-goals", 2).Content)
	announce(ctx, t, project, first.Live.Revision+2, invalid)
	broken := awaitEvolve(ctx, t, project, "the refused goals", func(s evolve.Snapshot) bool { return s.Goals.Commit == invalid })
	if broken.Goals.Status != evolve.GoalsInvalid || len(broken.Goals.Errors) == 0 || broken.Goals.Spec != "" || broken.Schedule.Active {
		t.Fatalf("the refused goals of %s were not recorded with their errors: %+v", invalid, broken.Goals)
	}
	if answer := startReview(ctx, t, api, caller, project.ProjectID, uuid.NewString()); answer.GetRefusal() != pb.EvolveReviewRefusal_EVOLVE_REVIEW_REFUSAL_GOALS_INVALID {
		t.Fatalf("Review now with refused goals answered %v", answer)
	}
	view, err = api.GetEvolve(ctx, &pb.GetEvolveRequest{ProjectId: project.ProjectID, Caller: caller})
	if reviews := view.GetEvolve().GetReviews(); err != nil || len(reviews) == 0 || reviews[0].GetReviewId() != reviewID ||
		reviews[0].GetStatus() != pb.EvolveReviewStatus_EVOLVE_REVIEW_STATUS_MEASURED || len(reviews[0].GetValues()) != evolveGoalSum {
		t.Fatalf("GetEvolve does not show the measured review %s: %v (%T)", reviewID, view, err)
	}
	t.Logf("FlowID=%s measured review %s of %d Live runs; goals %s valid, %s refused with %v", flowid.EvolveFlowID(project.ProjectID), reviewID,
		ended.Export.Runs, valid, invalid, broken.Goals.Errors)
}

// TestEvolveProjectDeletionDexIntegration deletes a Project while its first
// review exports its Live runs: the deletion's child cleanup ends EvolveFlow,
// whose execution completes, removes the Project's evolve objects with its
// prefix, and a later announcement starts nothing.
func TestEvolveProjectDeletionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), evolveTestTimeout)
	defer cancel()
	project := agents.NewEvolveProjectForTest(t, ctx)
	connection, caller := projecttest.LoginForTest(ctx, t, project.Account)
	api := pb.NewEvolveServiceClient(connection)
	first := awaitEvolve(ctx, t, project, "the first Live announcement", func(s evolve.Snapshot) bool { return !s.Goals.ReadAt.IsZero() })
	valid := project.PushGoalsForTest(ctx, mockInput(t, "evolve-goals", 1).Content)
	announce(ctx, t, project, first.Live.Revision+1, valid)
	awaitEvolve(ctx, t, project, "the valid goals", func(s evolve.Snapshot) bool { return s.Goals.Status == evolve.GoalsValid })
	startLiveSummaries(ctx, t, project.LiveProjectForTest, liveSummaries)
	answer := startReview(ctx, t, api, caller, project.ProjectID, uuid.NewString())
	if answer.GetReviewId() == "" {
		t.Fatalf("Review now was not admitted: %v", answer)
	}

	if deleted := projecttest.DeleteForTest(ctx, t, project.Projects, project.ProjectID); deleted.ProjectID != "" && deleted.ProjectStatus != projects.StatusDeleted {
		t.Fatalf("Project %s did not reach DELETED: %s", project.ProjectID, deleted.ProjectStatus)
	}
	flowID := flowid.EvolveFlowID(project.ProjectID)
	summary, err := project.Execution.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil || summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_COMPLETED {
		t.Fatalf("%s did not complete with its Project: %s (%T)", flowID, summary.GetFlowStatus(), err)
	}
	ended := readEvolve(ctx, t, project)
	var manifest runexport.Manifest
	if _, err = project.Objects.GetCurrentJSON(ctx, exportKey(project.ProjectID, answer.GetReviewId(), "manifest.json"), &manifest); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("the manifest of review %s outlived its Project's deletion (%T)", answer.GetReviewId(), err)
	}
	later := evolve.LiveRelease{Revision: first.Live.Revision + 2, ReleaseID: valid, BuildCommit: valid, CommittedAt: time.Now().UTC()}
	if err = project.Evolve.LiveChanged(ctx, project.ProjectID, later); err != nil {
		t.Fatalf("an announcement after the deletion failed (%T)", err)
	}
	if after := readEvolve(ctx, t, project); after.Live != ended.Live {
		t.Fatalf("an announcement after the deletion changed the ended measurement: %+v", after.Live)
	}
	t.Logf("FlowID=%s completed with Project %s while review %s was %s", flowID, project.ProjectID, answer.GetReviewId(), reviewStatus(ended, answer.GetReviewId()))
}

// readEvolve reads the Project's measurement, also an ended one.
func readEvolve(ctx context.Context, t *testing.T, project agents.EvolveProjectForTest) evolve.Snapshot {
	t.Helper()
	snapshot, err := project.Evolve.Get(ctx, project.ProjectID)
	if err != nil {
		t.Fatalf("reading the measurement of %s failed (%T)", project.ProjectID, err)
	}
	return snapshot
}

// awaitEvolve waits until the Project's measurement satisfies done and returns it.
func awaitEvolve(ctx context.Context, t *testing.T, project agents.EvolveProjectForTest, what string, done func(evolve.Snapshot) bool) evolve.Snapshot {
	t.Helper()
	for {
		snapshot, err := project.Evolve.Get(ctx, project.ProjectID)
		if err == nil && done(snapshot) {
			return snapshot
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for %s of %s: %+v (%T)", what, project.ProjectID, snapshot, err)
		case <-time.After(2 * time.Second):
		}
	}
}

// awaitReview waits until review reviewID ended and returns it.
func awaitReview(ctx context.Context, t *testing.T, project agents.EvolveProjectForTest, reviewID string) evolve.ReviewRecord {
	t.Helper()
	wait, cancel := context.WithTimeout(ctx, evolveReviewTimeout)
	defer cancel()
	snapshot := awaitEvolve(wait, t, project, "review "+reviewID, func(s evolve.Snapshot) bool {
		status := reviewStatus(s, reviewID)
		return status != "" && status != evolve.ReviewExporting && s.Active == ""
	})
	for _, review := range snapshot.Reviews {
		if review.ID == reviewID {
			return review
		}
	}
	panic("awaitEvolve returned without review " + reviewID)
}

// reviewStatus is the status of review reviewID, empty when the snapshot does not keep it.
func reviewStatus(s evolve.Snapshot, reviewID string) evolve.ReviewStatus {
	for _, review := range s.Reviews {
		if review.ID == reviewID {
			return review.Status
		}
	}
	return ""
}

// announce reports commit as Live revision revision, as ProjectDeployFlow does once a deployment commits.
func announce(ctx context.Context, t *testing.T, project agents.EvolveProjectForTest, revision uint64, commit string) {
	t.Helper()
	if err := project.Evolve.LiveChanged(ctx, project.ProjectID, evolve.LiveRelease{Revision: revision, ReleaseID: commit, BuildCommit: commit, CommittedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("announcing %s as Live revision %d failed (%T)", commit, revision, err)
	}
}

// startReview asks for Review now through the business API.
func startReview(ctx context.Context, t *testing.T, api pb.EvolveServiceClient, caller *pb.CallerContext, projectID, requestID string) *pb.StartEvolveReviewResponse {
	t.Helper()
	answer, err := api.StartEvolveReview(ctx, &pb.StartEvolveReviewRequest{ProjectId: projectID, RequestId: requestID, Caller: caller})
	if err != nil {
		t.Fatalf("StartEvolveReview %s failed (%T)", requestID, err)
	}
	return answer
}

// startConcurrentReviews sends two Review now requests at once and returns
// the review the admitted one started with its request ID; the other must be
// refused REVIEW_RUNNING.
func startConcurrentReviews(ctx context.Context, t *testing.T, api pb.EvolveServiceClient, caller *pb.CallerContext, projectID string) (string, string) {
	t.Helper()
	requests := []string{uuid.NewString(), uuid.NewString()}
	answers := make([]*pb.StartEvolveReviewResponse, len(requests))
	failures := make([]error, len(requests))
	var started sync.WaitGroup
	for index, request := range requests {
		started.Go(func() {
			answers[index], failures[index] = api.StartEvolveReview(ctx, &pb.StartEvolveReviewRequest{ProjectId: projectID, RequestId: request, Caller: caller})
		})
	}
	started.Wait()
	if err := errors.Join(failures...); err != nil {
		t.Fatalf("concurrent Review now failed (%T)", err)
	}
	admitted := 0
	if answers[1].GetReviewId() != "" {
		admitted = 1
	}
	other := answers[1-admitted]
	if answers[admitted].GetReviewId() == "" || other.GetReviewId() != "" || other.GetRefusal() != pb.EvolveReviewRefusal_EVOLVE_REVIEW_REFUSAL_REVIEW_RUNNING {
		t.Fatalf("two Review now requests at once answered %v and %v, not one review and one REVIEW_RUNNING", answers[0], answers[1])
	}
	return answers[admitted].GetReviewId(), requests[admitted]
}

// requireMeasuredReview checks a measured manual review of the Live runs.
func requireMeasuredReview(t *testing.T, review evolve.ReviewRecord, actor, goalsCommit string) {
	t.Helper()
	if review.Status != evolve.ReviewMeasured || review.Trigger != evolve.TriggerManual || review.Actor != actor || review.GoalsCommit != goalsCommit ||
		review.GoalsSpec != "" || review.StartedAt.IsZero() || review.FinishedAt.Before(review.StartedAt) || !review.Window.To.Equal(review.StartedAt) ||
		review.Export.Runs != liveSummaries || review.Export.Parts == 0 || review.Export.Truncated || review.Metrics == nil || review.Metrics.Runs != liveSummaries {
		t.Fatalf("review %s did not measure the %d Live runs: %+v", review.ID, liveSummaries, review)
	}
	for _, goal := range review.Metrics.Goals {
		if goal.ID == goalRequests && (goal.Value == nil || *goal.Value != liveSummaries || goal.Runs != liveSummaries || !goal.FewRuns) {
			t.Fatalf("goal %s of review %s is %+v, not the %d runs", goalRequests, review.ID, goal, liveSummaries)
		}
	}
	if len(review.Metrics.Goals) != evolveGoalSum {
		t.Fatalf("review %s keeps %d goal values, not %d", review.ID, len(review.Metrics.Goals), evolveGoalSum)
	}
}

// requireStoredExport reads the review's manifest, metrics and parts from
// storage and checks the records of the Live runs.
func requireStoredExport(ctx context.Context, t *testing.T, project agents.LiveProjectForTest, review evolve.ReviewRecord, goalsCommit string) {
	t.Helper()
	var manifest runexport.Manifest
	if _, err := project.Objects.GetCurrentJSON(ctx, exportKey(project.ProjectID, review.ID, "manifest.json"), &manifest); err != nil {
		t.Fatalf("the manifest of review %s was not read (%T)", review.ID, err)
	}
	if manifest.Source == nil || manifest.Source.Kind != runexport.SourceLive || manifest.Source.Dex != "" || manifest.CheckLive(project.ProjectID, review.ID) != nil {
		t.Fatalf("the manifest of review %s does not name the Live runs as its source: %+v", review.ID, manifest)
	}
	if manifest.ReviewID != review.ID || manifest.ProjectID != project.ProjectID || manifest.Runs != liveSummaries || !manifest.Complete || manifest.Truncated ||
		manifest.GoalsCommit != goalsCommit || len(manifest.Parts) != review.Export.Parts || !manifest.GeneratedAt.Equal(review.Window.To) || len(manifest.Timeline) == 0 ||
		manifest.Timeline[len(manifest.Timeline)-1].Release != project.ReleaseID {
		t.Fatalf("the manifest of review %s does not describe its export: %+v", review.ID, manifest)
	}
	var metrics goals.Metrics
	if _, err := project.Objects.GetCurrentJSON(ctx, exportKey(project.ProjectID, review.ID, "metrics.json"), &metrics); err != nil || metrics.Runs != liveSummaries || len(metrics.Goals) != evolveGoalSum {
		t.Fatalf("the metrics of review %s are %+v (%T)", review.ID, metrics, err)
	}
	records := 0
	for _, part := range manifest.Parts {
		for _, record := range readPart(ctx, t, project, review.ID, part) {
			records++
			if record.FlowType != "TextSummaryFlow" || record.Steps["PrepareSummary"] != 1 || len(record.Path) == 0 || record.Path[0].Step != "PrepareSummary" ||
				record.ReleaseAtStart != project.ReleaseID || record.StartedAt.Before(review.Window.From) {
				t.Fatalf("the record of %s in %s does not hold the run: %+v", record.FlowID, part.Name, record)
			}
			for field := range record.Fields {
				if field != "summary-status" {
					t.Fatalf("the record of %s carries %s, which the goals do not list", record.FlowID, field)
				}
			}
			if record.Steps["SaveSummaryResult"] > 0 && (record.AI.Calls == 0 || record.AI.InputTokens == 0) {
				t.Fatalf("the record of %s saved a summary without the model's usage: %+v", record.FlowID, record.AI)
			}
		}
	}
	if records != liveSummaries {
		t.Fatalf("the parts of review %s hold %d records, not %d", review.ID, records, liveSummaries)
	}
}

// readPart reads one part from the lane's artifact bucket by its manifest
// name, checks its SHA-256 and decodes its records.
func readPart(ctx context.Context, t *testing.T, project agents.LiveProjectForTest, reviewID string, part runexport.Part) []runexport.Record {
	t.Helper()
	endpoint, bucket := dexflowtest.Setting(t, "SUPERVERSE_S3_ENDPOINT"), dexflowtest.Setting(t, "SUPERVERSE_ARTIFACT_BUCKET")
	config, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client := s3.NewFromConfig(config, func(o *s3.Options) { o.BaseEndpoint = &endpoint; o.UsePathStyle = true })
	key := project.Objects.FullKey(exportKey(project.ProjectID, reviewID, part.Name))
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		t.Fatalf("part %s was not read (%T)", key, err)
	}
	defer func() { _ = object.Body.Close() }()
	compressed, err := io.ReadAll(object.Body)
	if sum := sha256.Sum256(compressed); err != nil || hex.EncodeToString(sum[:]) != part.SHA256 {
		t.Fatalf("part %s does not have the manifest's SHA-256 (%T)", key, err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("part %s is not gzip (%T)", key, err)
	}
	records, err := runexport.Read(reader)
	if err != nil {
		t.Fatalf("part %s does not decode: %v", key, err)
	}
	return records
}

// mockInput is input number of the mock scenario, which the Session's mock model answers.
func mockInput(t *testing.T, scenario string, number int) mockmodel.Input {
	t.Helper()
	for _, candidate := range mockmodel.Catalog() {
		if candidate.Name == scenario && number >= 1 && number <= len(candidate.Inputs) {
			return candidate.Inputs[number-1]
		}
	}
	t.Fatalf("the mock catalog has no input %d of scenario %s", number, scenario)
	return mockmodel.Input{}
}

// exportKey is the key of a file of a review's export below the store's prefix.
func exportKey(projectID, reviewID, name string) string {
	return projectID + "/evolve/exports/" + reviewID + "/" + name
}

// startLiveSummaries starts count runs of the template's TextSummaryFlow on
// the Project's Live deployment through Studio Runs, as a person does from
// the Runs page, and returns their FlowIDs.
func startLiveSummaries(ctx context.Context, t *testing.T, project agents.LiveProjectForTest, count int) []string {
	t.Helper()
	flowRuns := &runs.Runs{Projects: project.Projects, Deployments: project.Deployments, Objects: project.Objects, Connections: project.Connections,
		Names: deploymentnames.MustFor(deploymentnames.EnvironmentTest), Logger: slog.Default(), DexConnections: 4}
	t.Cleanup(func() { _ = flowRuns.Close() })
	target := &pb.FlowApiContext{Target: &pb.FlowApiTarget{ProjectId: project.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE,
		ReleaseId: project.ReleaseID, DeploymentId: project.DeploymentID, Generation: project.Generation}}
	var catalog struct {
		DefinitionRevision string `json:"definitionRevision"`
		StartFlow          struct {
			TargetRevision string `json:"targetRevision"`
		} `json:"startFlow"`
	}
	document, err := flowRuns.Invoke(ctx, project.Account, target, runs.Read, func(_ context.Context, flows *flowapi.FlowAPI) flowapi.Document { return flows.Catalog() })
	if err != nil || document.StatusCode != http.StatusOK || json.Unmarshal(document.JSON, &catalog) != nil {
		t.Fatalf("the Live catalog of deployment %s was not read: %d (%T)", project.DeploymentID, document.StatusCode, err)
	}
	target.DefinitionRevision = catalog.DefinitionRevision
	flowIDs := make([]string, 0, count)
	for range count {
		requestID := uuid.NewString()
		flowID := "text-summary-" + requestID
		document, err = flowRuns.Invoke(ctx, project.Account, target, runs.PinnedWrite, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
			return flows.Start(ctx, flowapi.StartRequest{FlowType: "TextSummaryFlow", FlowID: flowID, RequestID: requestID,
				TargetRevision: catalog.StartFlow.TargetRevision, Input: json.RawMessage(`{"text":"Goal measurement reads the runs of the Live build and evaluates its goals."}`)})
		})
		if err != nil || document.StatusCode != http.StatusOK {
			t.Fatalf("the Live start of %s failed: %d %s (%T)", flowID, document.StatusCode, document.JSON, err)
		}
		flowIDs = append(flowIDs, flowID)
	}
	return flowIDs
}
