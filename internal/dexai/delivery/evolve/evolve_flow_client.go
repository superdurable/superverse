// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

// lockWait bounds the retries of a lock conflict of a locked RPC; the review
// Steps that hold the lock make short provider calls.
const lockWait = 10 * time.Second

// ErrNotStarted reports a Project whose EvolveFlow does not run: it never had
// a Live deployment, or it is being deleted.
var ErrNotStarted = errors.New("the Project has no goal measurement")

// EvolveFlowClient is the typed boundary of EvolveFlow.
type EvolveFlowClient struct {
	dex      *dex.Client
	flow     *EvolveFlow
	projects *projects.ProjectFlowClient
}

// Bind connects the client to the server's one Dex client.
func (c *EvolveFlowClient) Bind(sdk *dex.Client) { c.dex = sdk }

// LiveChanged tells the Project's EvolveFlow that a Live deployment
// committed. It first admits the Flow as a child of the Project, which a
// deleting Project refuses, then starts it once and reports the change; a
// repeated or older change changes nothing.
func (c *EvolveFlowClient) LiveChanged(ctx context.Context, projectID string, change LiveRelease) error {
	flowID := flowid.EvolveFlowID(projectID)
	err := c.projects.AdmitChild(ctx, projectID, projects.ChildEvolve, flowID)
	if errors.Is(err, projects.ErrChildAdmissionClosed) {
		return nil
	}
	if err != nil {
		return err
	}
	request := flowid.Fingerprint(struct{ ProjectID string }{projectID})
	if _, err = c.dex.StartFlow(ctx, c.flow, flowID, goalsReading{Revision: change.Revision, Commit: change.BuildCommit}, dex.StartFlowOptions{
		RequestID: &request, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true},
		Attributes: initialAttributes(projectID, change),
	}); err != nil {
		return fmt.Errorf("start %s: %w", flowID, err)
	}
	_, err = dexflow.InvokeLocked[bool](ctx, c.dex, lockWait, flowID, c.flow.LiveChanged, change)
	var closed *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &closed) {
		// The Flow completed with its deleting Project.
		return nil
	}
	return err
}

// Get reads the Project's goal measurement, also a completed one; a Project
// that never had a Live deployment has none: ErrNotStarted.
func (c *EvolveFlowClient) Get(ctx context.Context, projectID string) (Snapshot, error) {
	var snapshot Snapshot
	err := dexflow.InvokeRetainedRead(ctx, c.dex, flowid.EvolveFlowID(projectID), c.flow.GetEvolve, nil, &snapshot)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrNotStarted, err)
	}
	return snapshot, err
}

// StartReview asks for a review now; the admission names the review it
// started, or why it started none. A Project without a running EvolveFlow is
// ErrNotStarted.
func (c *EvolveFlowClient) StartReview(ctx context.Context, projectID string, request ReviewRequest) (ReviewAdmission, error) {
	admission, err := dexflow.InvokeLocked[ReviewAdmission](ctx, c.dex, lockWait, flowid.EvolveFlowID(projectID), c.flow.StartReview, request)
	var closed *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &closed) {
		return ReviewAdmission{}, fmt.Errorf("%w: %w", ErrNotStarted, err)
	}
	return admission, err
}

// ReportReview reports the Session's report on a review to the Project's
// EvolveFlow. A reported proposal comes with the files of its change: the
// client decodes the backtest and the open-run check, and stores the proposal
// document under a key of its content, so a repeat stores nothing new and a
// refused report never blocks a corrected one; files that cannot back the
// proposal are a defect the Flow answers after its stale check, and the Flow
// checks the binding and the gates. A refusal is an answer. A refused report
// may leave its document, which Project deletion removes. A Project without a
// running EvolveFlow is ErrNotStarted.
func (c *EvolveFlowClient) ReportReview(ctx context.Context, projectID string, report ReviewReport, files *ReviewFiles) (ReportAnswer, error) {
	if (report.Proposal != nil) != (files != nil) {
		return ReportAnswer{}, fmt.Errorf("%w: a proposal, and only one, comes with its files", ErrInvalidReport)
	}
	if report.Proposal != nil {
		var err error
		if report.Defect, err = c.readProposalFiles(ctx, projectID, report.ReviewID, report.Proposal, *files); err != nil {
			return ReportAnswer{}, err
		}
	}
	if err := report.validate(); err != nil {
		return ReportAnswer{}, err
	}
	answer, err := dexflow.InvokeLocked[ReportAnswer](ctx, c.dex, lockWait, flowid.EvolveFlowID(projectID), c.flow.ReportReview, report)
	var closed *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &closed) {
		return ReportAnswer{}, fmt.Errorf("%w: %w", ErrNotStarted, err)
	}
	return answer, err
}

// readProposalFiles decodes a proposal's backtest and open-run check into
// offer and stores its document; it returns the defect of files that cannot
// back it, nil when they can.
func (c *EvolveFlowClient) readProposalFiles(ctx context.Context, projectID, reviewID string, offer *ProposalOffer, files ReviewFiles) (*ReportDefect, error) {
	if files.Unreadable != nil {
		return files.Unreadable, nil
	}
	if err := validateDocument(files.Proposal); err != nil {
		return &ReportDefect{Refusal: ReportTooLarge, Detail: err.Error()}, nil
	}
	backtest, err := goals.ParseBacktest(files.Backtest)
	if err != nil {
		return &ReportDefect{Refusal: ReportInvalidBacktest, Detail: comm.TruncateUTF8(err.Error(), maximumDetailBytes)}, nil
	}
	openRuns, err := goals.ParseOpenRuns(files.OpenRuns)
	if err != nil {
		return &ReportDefect{Refusal: ReportInvalidBacktest, Detail: comm.TruncateUTF8(err.Error(), maximumDetailBytes)}, nil
	}
	if kept := comm.Must(json.Marshal(backtest.Kept())); len(kept) > maximumBacktestBytes {
		return &ReportDefect{Refusal: ReportTooLarge, Detail: fmt.Sprintf("the backtest's summary takes %d bytes; at most %d are kept", len(kept), maximumBacktestBytes)}, nil
	}
	stored, err := c.flow.objects.Put(ctx, proposalKey(projectID, reviewID, files.Proposal), objectstore.MediaTypeMarkdown, files.Proposal)
	if err != nil {
		return nil, fmt.Errorf("store the proposal of %s: %w", reviewID, err)
	}
	offer.Backtest, offer.OpenRuns, offer.Object = &backtest, &openRuns, stored
	return nil, nil
}

// SkipWaitingReview ends the Project's review that waits for the agent as
// skipped for reason, a Session that can no longer take it; nothing waits,
// or the review has moved on, is nil. A Project without goal measurement is
// ErrNotStarted.
func (c *EvolveFlowClient) SkipWaitingReview(ctx context.Context, projectID string, reason ReviewReason) error {
	snapshot, err := c.Get(ctx, projectID)
	if err != nil {
		return err
	}
	waiting := slices.ContainsFunc(snapshot.Reviews, func(r ReviewRecord) bool { return r.ID == snapshot.Active && r.Status == ReviewWaitingAgent })
	if !waiting {
		return nil
	}
	_, err = c.ReportReview(ctx, projectID, ReviewReport{ReviewID: snapshot.Active, Phase: ReportSkipped, Reason: reason}, nil)
	return err
}

// DecideProposal records a person's decision on the proposal of a review at
// the revision they saw; a stale revision or status is a refusal in the
// answer. A Project without a running EvolveFlow is ErrNotStarted.
func (c *EvolveFlowClient) DecideProposal(ctx context.Context, projectID string, request DecisionRequest) (DecisionAnswer, error) {
	answer, err := dexflow.InvokeLockedWith[DecisionAnswer](ctx, c.dex, lockWait, flowid.EvolveFlowID(projectID), c.flow.DecideProposal, request,
		dex.RPCInvokeOptions{LoadAttributeMapInstances: []dex.AttributeMapLoad{Proposals.Load(request.ReviewID)}})
	var closed *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &closed) {
		return DecisionAnswer{}, fmt.Errorf("%w: %w", ErrNotStarted, err)
	}
	return answer, err
}

// ReadProposal reads the stored document of a review's proposal, also of a
// completed EvolveFlow. A review without a kept proposal is ErrNoProposal; a
// Project that never had a Live deployment is ErrNotStarted.
func (c *EvolveFlowClient) ReadProposal(ctx context.Context, projectID, reviewID string) ([]byte, error) {
	var proposal ProposalRecord
	err := dexflow.InvokeRetainedRead(ctx, c.dex, flowid.EvolveFlowID(projectID), c.flow.GetProposal, reviewID, &proposal, Proposals.Load(reviewID))
	var missing *dex.FlowNotFoundError
	switch {
	case errors.As(err, &missing):
		return nil, fmt.Errorf("%w: %w", ErrNotStarted, err)
	case err != nil:
		return nil, err
	case proposal.ReviewID == "":
		return nil, fmt.Errorf("%w: %s", ErrNoProposal, reviewID)
	}
	document, err := c.flow.objects.Get(ctx, proposal.Object)
	if err != nil {
		return nil, fmt.Errorf("read the proposal of %s: %w", reviewID, err)
	}
	return document, nil
}

// ReviewExport is a review's export as place_evolve_export places it: the
// stored manifest with the digest of runs.jsonl, the export's runs.jsonl, one
// run per FlowID in canonical order (runexport.Canonical), as a stream of
// RunsBytes bytes holding RunsCount runs, the stored metrics.json and goals.md
// at the manifest's goals commit, the Live build's. Runs fails at its end
// unless what it streamed is the runs the review evaluated.
type ReviewExport struct {
	Manifest  runexport.Manifest
	Runs      io.Reader
	RunsBytes int64
	RunsCount int
	Metrics   []byte
	Goals     []byte
}

// ErrNoExport reports a review whose export is not stored: it never
// finished, or a later review deleted it.
var ErrNoExport = errors.New("the review has no stored export")

// ReviewExport reads the export of review reviewID of project projectID for
// its Session, from storage and the repository host only, never through the
// Flow: the manifest, refused unless it labels this review's export of the
// Live runs (runexport.ErrNotLive); the index of the runs ComputeMetrics
// evaluated and stored, whose objects Runs streams within ctx; metrics.json;
// and goals.md at the goals commit from the Project's repository fullName
// under installationID, as ReadGoals reads it. An export that is not stored
// is ErrNoExport.
func (c *EvolveFlowClient) ReviewExport(ctx context.Context, projectID, reviewID, fullName string, installationID int64) (ReviewExport, error) {
	prefix := exportPrefix(projectID, reviewID)
	var export ReviewExport
	if _, err := c.flow.objects.GetCurrentJSON(ctx, prefix+"manifest.json", &export.Manifest); errors.Is(err, objectstore.ErrNotFound) {
		return ReviewExport{}, fmt.Errorf("%w: %s", ErrNoExport, reviewID)
	} else if err != nil {
		return ReviewExport{}, fmt.Errorf("read the manifest of %s: %w", reviewID, err)
	}
	if err := export.Manifest.CheckPlaced(projectID, reviewID); err != nil {
		return ReviewExport{}, err
	}
	var index runsIndex
	if _, err := c.flow.objects.GetCurrentJSON(ctx, prefix+runsIndexObject, &index); errors.Is(err, objectstore.ErrNotFound) {
		return ReviewExport{}, fmt.Errorf("%w: %s has no evaluated runs", ErrNoExport, reviewID)
	} else if err != nil {
		return ReviewExport{}, fmt.Errorf("read the runs of %s: %w", reviewID, err)
	}
	export.Manifest.RunsSHA256, export.RunsBytes, export.RunsCount = index.RunsSHA256, index.Bytes, index.Runs
	export.Runs = &storedRuns{ctx: ctx, objects: c.flow.objects, prefix: prefix, index: index, digest: sha256.New()}
	var err error
	if export.Metrics, _, err = c.flow.objects.GetCurrent(ctx, prefix+"metrics.json"); errors.Is(err, objectstore.ErrNotFound) {
		return ReviewExport{}, fmt.Errorf("%w: %s has no metrics", ErrNoExport, reviewID)
	} else if err != nil {
		return ReviewExport{}, fmt.Errorf("read the metrics of %s: %w", reviewID, err)
	}
	if !flowid.IsCommitSHA(export.Manifest.GoalsCommit) {
		return ReviewExport{}, fmt.Errorf("%w: it names the goals commit %q", runexport.ErrNotLive, export.Manifest.GoalsCommit)
	}
	if err = repohost.CheckInstallation(c.flow.host, installationID); err != nil {
		return ReviewExport{}, err
	}
	if export.Goals, err = c.flow.host.ReadFile(ctx, fullName, export.Manifest.GoalsCommit, goals.FilePath); err != nil {
		return ReviewExport{}, fmt.Errorf("read the goals of %s at %s: %w", reviewID, export.Manifest.GoalsCommit, err)
	}
	return export, nil
}

// CleanupProjectEvolve ends the EvolveFlow of a deleting Project and reports
// whether it has ended: the first pass cancels its Steps and a later pass
// finds it completed. A Project whose Flow never started has nothing to end.
func (c *EvolveFlowClient) CleanupProjectEvolve(ctx context.Context, projectID string) (bool, error) {
	_, err := dexflow.InvokeLocked[bool](ctx, c.dex, lockWait, flowid.EvolveFlowID(projectID), c.flow.CompleteProjectEvolve, nil)
	var closed *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &closed) {
		return true, nil
	}
	return false, err
}
