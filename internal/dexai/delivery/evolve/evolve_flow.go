// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"golang.org/x/sync/semaphore"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

var (
	ProjectID               = dex.DefineAttribute[string]("ProjectID")
	LiveAttribute           = dex.DefineAttribute[LiveRelease]("Live")
	GoalsAttribute          = dex.DefineAttribute[Goals]("Goals")
	ScheduleAttribute       = dex.DefineAttribute[Schedule]("Schedule")
	ReviewSeq               = dex.DefineAttribute[int64]("ReviewSeq")
	ActiveReview            = dex.DefineAttribute[string]("Active")
	ManualRequestsAttribute = dex.DefineAttribute[ManualRequests]("ManualRequests")
	// Reviews holds the newest reviews by r-<n>; CommandLock serializes every write.
	Reviews     = dex.DefineAttributeMap[ReviewRecord]("Reviews")
	CommandLock = dex.DefineAttribute[bool]("CommandLock")
)

// EvolveFlow owns one Project's goal measurement (evolve-<projectID>): the goals of the Live build, the review
// schedule, each review's run export and metrics, and the review records. Project deletion completes it.
type EvolveFlow struct {
	dex.FlowDefaults
	settings   Settings
	projects   *projects.ProjectFlowClient
	host       repohost.Host
	objects    *objectstore.Store
	dependency EvolveFlowDependency
	exports    *semaphore.Weighted
}

func (f *EvolveFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{
		ProjectID, LiveAttribute, GoalsAttribute, ScheduleAttribute, ReviewSeq, ActiveReview, ManualRequestsAttribute, Reviews, CommandLock,
	}}
}

func (f *EvolveFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(ReadGoals{f: f}), dex.DefineStep(CommitGoals{f: f}), dex.DefineStep(AwaitSchedule{f: f}),
		dex.DefineStep(BeginReview{f: f}), dex.DefineStep(RecordReviewStart{f: f}), dex.DefineStep(ExportRuns{f: f}), dex.DefineStep(FinishExport{f: f}),
		dex.DefineStep(ComputeMetrics{f: f}), dex.DefineStep(GateReview{f: f}), dex.DefineStep(RecordReviewFailure{f: f}),
		dex.DefineStep(CompleteEvolve{}),
	}
}

func (f *EvolveFlow) GetRPCs() []dex.RPCDef {
	read := dexflow.ShortRPC()
	snapshot := dexflow.ShortRPC()
	snapshot.LoadAttributeMaps = []dex.AttributeDef{Reviews}
	write := dexflow.ShortRPC(dex.LockAttribute(CommandLock))
	review := dexflow.ShortRPC(dex.LockAttribute(CommandLock))
	review.LoadAttributeMaps = []dex.AttributeDef{Reviews}
	return []dex.RPCDef{
		dex.DefineRPC(f.GetDexSummary, read), dex.DefineRPC(f.GetDexDisplay, read), dex.DefineRPC(f.GetEvolve, snapshot),
		dex.DefineRPC(f.LiveChanged, write), dex.DefineRPC(f.StartReview, review), dex.DefineRPC(f.CompleteProjectEvolve, write),
	}
}

// initialAttributes describe the EvolveFlow of a Project whose first Live deployment is live.
func initialAttributes(projectID string, live LiveRelease) []dex.InitialAttributeDef {
	b := &dexflow.InitialAttributes{}
	dexflow.Add(b, ProjectID, projectID)
	dexflow.Add(b, LiveAttribute, live)
	dexflow.Add(b, GoalsAttribute, Goals{Status: GoalsNone})
	dexflow.Add(b, ScheduleAttribute, Schedule{AnchorHour: anchorHour(projectID)})
	dexflow.Add(b, ReviewSeq, int64(0))
	dexflow.Add(b, ActiveReview, "")
	dexflow.Add(b, ManualRequestsAttribute, ManualRequests{})
	dexflow.Add(b, CommandLock, false)
	return b.Values()
}

// lockedStateOptions serve the short Steps that write state, under the command lock, with every review loaded.
func lockedStateOptions() *dex.StepOptions {
	o := dexflow.StateStepOptions()
	o.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(CommandLock)}
	o.ExecuteLoadAttributeMaps = []dex.AttributeDef{Reviews}
	return o
}

// reviewStepOptions serve a review's long Steps, which hold no lock and fail into RecordReviewFailure.
func (f *EvolveFlow) reviewStepOptions(attempt, total time.Duration) *dex.StepOptions {
	o := dexflow.StateStepOptions()
	o.ExecuteDurability = dex.StepDurabilitySync
	o.ExecuteMethodTimeout = attempt
	o.ExecuteRetry = dexflow.StepRetry(total)
	o.ExecuteLoadAttributeMaps = []dex.AttributeDef{Reviews}
	o.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordReviewFailure{f: f}, nil)
	return o
}

// Wait limits of the review Steps, which the engine enforces.
const (
	beginReviewAttempt    = 2 * time.Minute
	beginReviewLimit      = 10 * time.Minute
	exportPartAttempt     = 30 * time.Minute
	exportPartLimit       = 4 * time.Hour
	computeMetricsAttempt = 5 * time.Minute
	computeMetricsLimit   = 15 * time.Minute
	readGoalsLimit        = 10 * time.Minute
)

func (*EvolveFlow) GetDexSummary(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// dex:field attribute-key:Goals value-type:json editable:false description:"Goals of the Live build"
// dex:field attribute-key:Schedule value-type:json editable:false description:"Next scheduled review"
// dex:field attribute-key:Active value-type:string editable:false description:"Review in progress"
func (*EvolveFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"Goals": comm.Must(GoalsAttribute.Get(ctx)), "Schedule": comm.Must(ScheduleAttribute.Get(ctx)), "Active": comm.Must(ActiveReview.Get(ctx)),
	}}, nil
}

// GetEvolve reads the state and every kept review without a lock, also of a completed execution.
func (*EvolveFlow) GetEvolve(ctx dex.Context, _ dex.None) (*dex.RPCResult[Snapshot], error) {
	// CommandLock holds no state; reading it records it in the graph with the state it guards.
	_ = comm.Must(CommandLock.Get(ctx))
	return &dex.RPCResult[Snapshot]{Output: readSnapshot(ctx)}, nil
}

// LiveChanged records a newer Live deployment and reads its goals; an older or repeated one changes nothing.
func (f *EvolveFlow) LiveChanged(ctx dex.Context, in LiveRelease) (*dex.RPCResult[bool], error) {
	if in.Revision <= comm.Must(LiveAttribute.Get(ctx)).Revision {
		return &dex.RPCResult[bool]{Output: false}, nil
	}
	comm.MustDo(LiveAttribute.Set(ctx, in))
	read := dex.MovementOf(ReadGoals{f: f}, goalsReading{Revision: in.Revision, Commit: in.BuildCommit})
	return &dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{read}}, nil
}

// StartReview admits a person's Review now; a known RequestID answers the review it started.
func (f *EvolveFlow) StartReview(ctx dex.Context, in ReviewRequest) (*dex.RPCResult[ReviewAdmission], error) {
	manual := comm.Must(ManualRequestsAttribute.Get(ctx))
	if slices.Contains(manual.RequestIDs, in.RequestID) {
		return &dex.RPCResult[ReviewAdmission]{Output: ReviewAdmission{ReviewID: requestedReview(ctx, in.RequestID)}}, nil
	}
	now := time.Now().UTC()
	if refusal := manualRefusal(comm.Must(GoalsAttribute.Get(ctx)), comm.Must(ActiveReview.Get(ctx)), manual, now); refusal != "" {
		return &dex.RPCResult[ReviewAdmission]{Output: ReviewAdmission{Refusal: refusal}}, nil
	}
	manual.RequestIDs = append(manual.RequestIDs, in.RequestID)
	manual.RequestIDs, manual.LastAt = manual.RequestIDs[max(0, len(manual.RequestIDs)-keptManualRequests):], now
	comm.MustDo(ManualRequestsAttribute.Set(ctx, manual))
	record := allocateReview(ctx, ReviewRecord{Trigger: TriggerManual, RequestID: in.RequestID, Actor: in.Actor, RequestedAt: now, Status: ReviewExporting})
	begin := dex.MovementOf(BeginReview{f: f}, reviewInput{ReviewID: record.ID})
	return &dex.RPCResult[ReviewAdmission]{Output: ReviewAdmission{ReviewID: record.ID}, NextSteps: []dex.StepMovement{begin}}, nil
}

// CompleteProjectEvolve ends the EvolveFlow of a deleting Project: it cancels every Step and completes.
func (f *EvolveFlow) CompleteProjectEvolve(ctx dex.Context, _ dex.None) (*dex.RPCResult[bool], error) {
	return (&dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{dex.MovementOf(CompleteEvolve{}, nil)}}).CancelSteps(
		ReadGoals{}, CommitGoals{}, AwaitSchedule{}, BeginReview{}, RecordReviewStart{}, ExportRuns{}, FinishExport{}, ComputeMetrics{}, GateReview{}, RecordReviewFailure{}), nil
}

// dex:group group-id:goals group-label:"Goals"
// dex:explanation text:"Read .superverse/goals.md at the Live build's commit through the repository host and parse it; an absent file means no goals. A superseded revision begins again the review that waited."
type ReadGoals struct {
	dex.StepDefaultsNoWaitFor[goalsReading]
	f *EvolveFlow
}

func (s ReadGoals) GetStepOptions() *dex.StepOptions {
	o := dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(CommitGoals{f: s.f}, nil))
	o.ExecuteRetry = dexflow.StepRetry(readGoalsLimit)
	return o
}

func (s ReadGoals) Execute(ctx dex.Context, in goalsReading) (*dex.StepDecision, error) {
	if comm.Must(LiveAttribute.Get(ctx)).Revision != in.Revision {
		if in.ReviewID != "" {
			return dex.GoTo(BeginReview{f: s.f}, reviewInput{ReviewID: in.ReviewID}), nil
		}
		return dex.DeadEnd(), nil
	}
	file, found, err := s.f.readGoalsFile(ctx, comm.Must(ProjectID.Get(ctx)), in.Commit)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("read the goals of %s: %w", in.Commit, err))
	}
	read := readGoals(in.Commit, file, found, time.Now().UTC())
	in.Goals = &read
	return dex.GoTo(CommitGoals{f: s.f}, in), nil
}

// dex:group group-id:goals group-label:"Goals"
// dex:explanation text:"Store the goals of the newest Live revision, or the read failure, restart the schedule they set, and begin again the review that waited for them."
type CommitGoals struct {
	dex.StepDefaultsNoWaitFor[goalsReading]
	f *EvolveFlow
}

func (CommitGoals) GetStepOptions() *dex.StepOptions { return lockedStateOptions() }

func (s CommitGoals) Execute(ctx dex.Context, in goalsReading) (*dex.StepDecision, error) {
	if comm.Must(LiveAttribute.Get(ctx)).Revision != in.Revision {
		if in.ReviewID != "" {
			return dex.GoTo(BeginReview{f: s.f}, reviewInput{ReviewID: in.ReviewID}), nil
		}
		return dex.DeadEnd(), nil
	}
	read := goalsReadFailure(in.Commit, dexflow.RecoveryDetail(ctx), time.Now().UTC())
	if in.Goals != nil {
		read = *in.Goals
	}
	comm.MustDo(GoalsAttribute.Set(ctx, read))
	schedule := nextSchedule(comm.Must(ScheduleAttribute.Get(ctx)), read, time.Now().UTC())
	comm.MustDo(ScheduleAttribute.Set(ctx, schedule))
	var next []dex.StepMovement
	if in.ReviewID != "" {
		next = append(next, dex.MovementOf(BeginReview{f: s.f}, reviewInput{ReviewID: in.ReviewID}))
	}
	if schedule.Active {
		next = append(next, dex.MovementOf(AwaitSchedule{f: s.f}, nil))
	}
	if len(next) == 0 {
		return dex.DeadEnd().CancelSteps(AwaitSchedule{}), nil
	}
	return dex.GoToMany(next...).CancelSteps(AwaitSchedule{}), nil
}

// dex:group group-id:schedule group-label:"Schedule"
// dex:explanation text:"Wait for the next scheduled review, then start it, or record it skipped while another review runs."
type AwaitSchedule struct {
	dex.StepDefaults
	f *EvolveFlow
}

func (AwaitSchedule) GetStepOptions() *dex.StepOptions { return lockedStateOptions() }

func (AwaitSchedule) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	nextAt := comm.Must(ScheduleAttribute.Get(ctx)).NextAt
	return dex.Until(dex.Timer(max(nextAt.Sub(ctx.FirstAttemptAt()), time.Second))), nil
}

func (s AwaitSchedule) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	schedule, now := comm.Must(ScheduleAttribute.Get(ctx)), time.Now().UTC()
	switch {
	case !schedule.Active:
		return dex.DeadEnd(), nil
	case now.Before(schedule.NextAt):
		return dex.GoTo(AwaitSchedule{f: s.f}, nil), nil
	}
	schedule.NextAt = nextAnchor(schedule.Period, schedule.AnchorHour, now)
	comm.MustDo(ScheduleAttribute.Set(ctx, schedule))
	if comm.Must(ActiveReview.Get(ctx)) != "" {
		allocateReview(ctx, ReviewRecord{Trigger: TriggerSchedule, RequestedAt: now, FinishedAt: now, Status: ReviewSkipped, Reason: ReasonPreviousReviewRunning})
		return dex.GoTo(AwaitSchedule{f: s.f}, nil), nil
	}
	record := allocateReview(ctx, ReviewRecord{Trigger: TriggerSchedule, RequestedAt: now, Status: ReviewExporting})
	return dex.GoToMany(dex.MovementOf(BeginReview{f: s.f}, reviewInput{ReviewID: record.ID}), dex.MovementOf(AwaitSchedule{f: s.f}, nil)), nil
}

// dex:group group-id:review group-label:"Review"
// dex:explanation text:"Read the Project's current Live deployment, and with valid goals of its build delete the exports of earlier reviews beyond the kept ones."
type BeginReview struct {
	dex.StepDefaultsNoWaitFor[reviewInput]
	f *EvolveFlow
}

func (s BeginReview) GetStepOptions() *dex.StepOptions {
	return s.f.reviewStepOptions(beginReviewAttempt, beginReviewLimit)
}

func (s BeginReview) Execute(ctx dex.Context, in reviewInput) (*dex.StepDecision, error) {
	defer dexflow.KeepAlive(ctx, nil)()
	projectID, current := comm.Must(ProjectID.Get(ctx)), comm.Must(GoalsAttribute.Get(ctx))
	live, reason, err := s.f.currentLive(ctx, projectID)
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	in.Live, in.Skip = live, reason
	if reason == "" && goalsSkip(current) == "" && current.Commit == measuredLive(comm.Must(LiveAttribute.Get(ctx)), live).BuildCommit {
		if in.DeletedExports, err = s.f.deleteOldExports(ctx, projectID, loadedReviews(ctx), in.ReviewID); err != nil {
			return nil, dex.ErrorWithStack(err)
		}
	}
	return dex.GoTo(RecordReviewStart{f: s.f}, in), nil
}

// dex:group group-id:review group-label:"Review"
// dex:explanation text:"Mark the deleted exports; adopt a newer current Live and read its goals first; record the review skipped, or copy the goals and the window into it and start the export."
type RecordReviewStart struct {
	dex.StepDefaultsNoWaitFor[reviewInput]
	f *EvolveFlow
}

func (RecordReviewStart) GetStepOptions() *dex.StepOptions { return lockedStateOptions() }

func (s RecordReviewStart) Execute(ctx dex.Context, in reviewInput) (*dex.StepDecision, error) {
	record, current, now := comm.Must(Reviews.Get(ctx, in.ReviewID)), comm.Must(GoalsAttribute.Get(ctx)), time.Now().UTC()
	for _, id := range in.DeletedExports {
		old, err := Reviews.Get(ctx, id)
		if dexflow.IsAttributeNotFound(err) {
			continue
		}
		old = comm.Must(old, err)
		old.Export.Deleted = true
		comm.MustDo(Reviews.Set(ctx, id, old))
	}
	announced := comm.Must(LiveAttribute.Get(ctx))
	live := measuredLive(announced, in.Live)
	if live.Revision != announced.Revision {
		comm.MustDo(LiveAttribute.Set(ctx, live))
	}
	if in.Skip == "" && current.Commit != live.BuildCommit {
		// The goals of the measured Live build are not stored yet: read them, then begin the review again.
		return dex.GoTo(ReadGoals{f: s.f}, goalsReading{Revision: live.Revision, Commit: live.BuildCommit, ReviewID: in.ReviewID}), nil
	}
	if reason := cmp.Or(in.Skip, goalsSkip(current)); reason != "" {
		record.Status, record.Reason, record.FinishedAt = ReviewSkipped, reason, now
		endReview(ctx, record)
		return dex.DeadEnd(), nil
	}
	record.StartedAt, record.GoalsCommit, record.GoalsSpec = now, current.Commit, current.Spec
	record.Window.From, record.Window.To = now.AddDate(0, 0, -s.f.settings.RetentionDays), now
	comm.MustDo(Reviews.Set(ctx, in.ReviewID, record))
	return dex.GoTo(ExportRuns{f: s.f}, reviewInput{ReviewID: in.ReviewID, Export: exportCursor{Part: 1}}), nil
}

// dex:group group-id:review group-label:"Review"
// dex:explanation text:"Export one part: page the Live engine's search, read and compact up to 500 runs at a paced rate, write the part and continue with the next."
type ExportRuns struct {
	dex.StepDefaultsNoWaitFor[reviewInput]
	f *EvolveFlow
}

func (s ExportRuns) GetStepOptions() *dex.StepOptions {
	return s.f.reviewStepOptions(exportPartAttempt, exportPartLimit)
}

func (s ExportRuns) Execute(ctx dex.Context, in reviewInput) (*dex.StepDecision, error) {
	defer dexflow.KeepAlive(ctx, nil)()
	if err := s.f.exports.Acquire(ctx, 1); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("wait for an export slot: %w", err))
	}
	defer s.f.exports.Release(1)
	record := comm.Must(Reviews.Get(ctx, in.ReviewID))
	cursor, done, err := s.f.exportPart(ctx, comm.Must(ProjectID.Get(ctx)), record, in.Export, ctx.Attempt())
	var tooLarge *partTooLargeError
	if errors.As(err, &tooLarge) {
		in.Failure = &reviewFailure{Code: ReasonExportTooLarge, Detail: tooLarge.Error()}
		return dex.GoTo(RecordReviewFailure{f: s.f}, in), nil
	} else if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	in.Export = cursor
	if done {
		return dex.GoTo(FinishExport{f: s.f}, in), nil
	}
	return dex.GoTo(ExportRuns{f: s.f}, in), nil
}

// dex:group group-id:review group-label:"Review"
// dex:explanation text:"Write the export's manifest once: its window, parts, run count, truncation and the Live release timeline up to the review's start."
type FinishExport struct {
	dex.StepDefaultsNoWaitFor[reviewInput]
	f *EvolveFlow
}

func (s FinishExport) GetStepOptions() *dex.StepOptions {
	o := dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordReviewFailure{f: s.f}, nil))
	o.ExecuteLoadAttributeMaps = []dex.AttributeDef{Reviews}
	return o
}

func (s FinishExport) Execute(ctx dex.Context, in reviewInput) (*dex.StepDecision, error) {
	ref, err := s.f.writeManifest(ctx, comm.Must(ProjectID.Get(ctx)), comm.Must(Reviews.Get(ctx, in.ReviewID)), in.Export)
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	in.Manifest = &ref
	return dex.GoTo(ComputeMetrics{f: s.f}, in), nil
}

// dex:group group-id:review group-label:"Review"
// dex:explanation text:"Refuse a manifest that is not this review's export of the Live runs, evaluate the copied goals over the export's runs, write metrics.json beside it and keep the goal values for the review."
type ComputeMetrics struct {
	dex.StepDefaultsNoWaitFor[reviewInput]
	f *EvolveFlow
}

func (s ComputeMetrics) GetStepOptions() *dex.StepOptions {
	return s.f.reviewStepOptions(computeMetricsAttempt, computeMetricsLimit)
}

func (s ComputeMetrics) Execute(ctx dex.Context, in reviewInput) (*dex.StepDecision, error) {
	defer dexflow.KeepAlive(ctx, nil)()
	summary, err := s.f.computeMetrics(ctx, comm.Must(ProjectID.Get(ctx)), comm.Must(Reviews.Get(ctx, in.ReviewID)), in.Export, *in.Manifest)
	if failure := metricsFailure(err); failure != nil {
		in.Failure = failure
		return dex.GoTo(RecordReviewFailure{f: s.f}, in), nil
	} else if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	in.Metrics = &summary
	return dex.GoTo(GateReview{f: s.f}, in), nil
}

// dex:group group-id:review group-label:"Review"
// dex:explanation text:"Record the review measured with its export and goal values and clear the active review; agent reviews are not part of measurement."
type GateReview struct {
	dex.StepDefaultsNoWaitFor[reviewInput]
	f *EvolveFlow
}

func (GateReview) GetStepOptions() *dex.StepOptions { return lockedStateOptions() }

func (GateReview) Execute(ctx dex.Context, in reviewInput) (*dex.StepDecision, error) {
	record := comm.Must(Reviews.Get(ctx, in.ReviewID))
	record.Status, record.FinishedAt, record.Metrics = ReviewMeasured, time.Now().UTC(), in.Metrics
	record.Export = ExportSummary{Parts: len(in.Export.Parts), Runs: in.Export.Runs, Bytes: in.Export.Bytes, Truncated: in.Export.Truncated}
	endReview(ctx, record)
	return dex.DeadEnd(), nil
}

// dex:group group-id:review group-label:"Review"
// dex:explanation text:"Record a review whose Step failed or ran past its wait limit as failed, with its code and the redacted cause, and clear the active review."
type RecordReviewFailure struct {
	dex.StepDefaultsNoWaitFor[reviewInput]
	f *EvolveFlow
}

func (RecordReviewFailure) GetStepOptions() *dex.StepOptions { return lockedStateOptions() }

func (RecordReviewFailure) Execute(ctx dex.Context, in reviewInput) (*dex.StepDecision, error) {
	record := comm.Must(Reviews.Get(ctx, in.ReviewID))
	failure := recoveredFailure(dexflow.RecoveryStep(ctx), dexflow.RecoveryTimedOut(ctx), dexflow.RecoveryDetail(ctx))
	if in.Failure != nil {
		failure = *in.Failure
	}
	record.Status, record.Reason, record.Detail, record.FinishedAt = ReviewFailed, failure.Code, dexflow.TruncateCause(failure.Detail), time.Now().UTC()
	record.Export = ExportSummary{Parts: len(in.Export.Parts), Runs: in.Export.Runs, Bytes: in.Export.Bytes, Truncated: in.Export.Truncated}
	endReview(ctx, record)
	return dex.DeadEnd(), nil
}

// dex:group group-id:end group-label:"End"
// dex:explanation text:"Complete the Flow of a deleting Project after its Steps were cancelled; the Project's deletion removes the evolve objects."
type CompleteEvolve struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (CompleteEvolve) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (CompleteEvolve) Execute(dex.Context, dex.None) (*dex.StepDecision, error) {
	return dex.GracefulComplete(true), nil
}

// readSnapshot reads the state and the loaded reviews, newest first.
func readSnapshot(ctx dex.Context) Snapshot {
	reviews := loadedReviews(ctx)
	newestFirst(reviews)
	for i := range reviews {
		reviews[i].GoalsSpec = ""
	}
	return Snapshot{
		ProjectID: comm.Must(ProjectID.Get(ctx)), Live: comm.Must(LiveAttribute.Get(ctx)), Goals: comm.Must(GoalsAttribute.Get(ctx)),
		Schedule: comm.Must(ScheduleAttribute.Get(ctx)), ReviewSeq: comm.Must(ReviewSeq.Get(ctx)), Active: comm.Must(ActiveReview.Get(ctx)),
		LastManualAt: comm.Must(ManualRequestsAttribute.Get(ctx)).LastAt, Reviews: reviews,
	}
}

// loadedReviews are the reviews the invocation loaded, which is every kept one.
func loadedReviews(ctx dex.Context) []ReviewRecord {
	keys := Reviews.AllInstanceKeys(ctx)
	records := make([]ReviewRecord, 0, len(keys))
	for _, key := range keys {
		records = append(records, comm.Must(Reviews.Get(ctx, key)))
	}
	return records
}

// allocateReview numbers record as the next review, stores it and prunes; a record that is not skipped becomes Active.
func allocateReview(ctx dex.Context, record ReviewRecord) ReviewRecord {
	seq := comm.Must(ReviewSeq.Get(ctx)) + 1
	comm.MustDo(ReviewSeq.Set(ctx, seq))
	record.ID = reviewKey(seq)
	comm.MustDo(Reviews.Set(ctx, record.ID, record))
	if !record.ended() {
		comm.MustDo(ActiveReview.Set(ctx, record.ID))
	}
	for _, key := range prunedReviews(loadedReviews(ctx)) {
		comm.MustDo(Reviews.Delete(ctx, key))
	}
	return record
}

// endReview stores the review's final record without its goals copy and clears the active review.
func endReview(ctx dex.Context, record ReviewRecord) {
	record.GoalsSpec = ""
	comm.MustDo(Reviews.Set(ctx, record.ID, record))
	if comm.Must(ActiveReview.Get(ctx)) == record.ID {
		comm.MustDo(ActiveReview.Set(ctx, ""))
	}
}

// requestedReview is the kept review that a Review now request started, empty once pruned.
func requestedReview(ctx dex.Context, requestID string) string {
	for _, record := range loadedReviews(ctx) {
		if record.RequestID == requestID {
			return record.ID
		}
	}
	return ""
}
