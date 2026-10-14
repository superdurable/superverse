// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package platformissue

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/platform/githubissues"
)

var (
	IncidentAttribute = dex.DefineAttribute[Incident]("Incident")
	OutcomeAttribute  = dex.DefineAttribute[*Outcome]("Outcome")
)

// PlatformIssueFlow files the platform faults of one signature in one environment as one GitHub
// issue: one run finds or opens it, records the outcome and completes. GitHub owns the issue.
type PlatformIssueFlow struct {
	dex.FlowDefaults
	issues PlatformIssueFlowDependency
	logger *slog.Logger
}

func (f *PlatformIssueFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{IncidentAttribute, OutcomeAttribute}}
}

func (f *PlatformIssueFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(EnsurePlatformIssue{f: f}),
		dex.DefineStep(LabelPlatformIssue{f: f}),
		dex.DefineStep(RecordPlatformIssue{f: f}),
		dex.DefineStep(DropPlatformIssue{}),
	}
}

func (f *PlatformIssueFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(f.GetPlatformIssue, dexflow.ShortRPC()),
		dex.DefineRPC(f.GetDexSummary, dexflow.ShortRPC()),
		dex.DefineRPC(f.GetDexDisplay, dexflow.ShortRPC()),
	}
}

// GetPlatformIssue is unlocked and returns output only, so it also serves a completed run.
func (*PlatformIssueFlow) GetPlatformIssue(ctx dex.Context, _ dex.None) (*dex.RPCResult[snapshot], error) {
	return &dex.RPCResult[snapshot]{Output: snapshot{Incident: comm.Must(IncidentAttribute.Get(ctx)), Outcome: dexflow.Optional(OutcomeAttribute.Get(ctx))}}, nil
}

// GetDexSummary is the row of the Dex Web Flow list, whose FlowID names the signature.
func (*PlatformIssueFlow) GetDexSummary(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay is the header of the Dex Web Flow view.
//
// dex:field attribute-key:Incident value-type:json editable:false description:"Reported incident"
// dex:field attribute-key:Outcome value-type:json editable:false description:"Outcome and the issue"
func (*PlatformIssueFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"Incident": comm.Must(IncidentAttribute.Get(ctx)),
		"Outcome":  dexflow.Optional(OutcomeAttribute.Get(ctx)),
	}}, nil
}

// dex:group group-id:issue group-label:"Issue"
// dex:explanation text:"Find the GitHub issue that carries the signature's label, or open it, retrying only a transient failure; only log the incident where no issue repository is configured."
type EnsurePlatformIssue struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PlatformIssueFlow
}

func (step EnsurePlatformIssue) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(DropPlatformIssue{}, nil))
}

// Execute queries the label before it writes, which is the admission check of a run that reuses
// the FlowID: an earlier run's issue is found, never opened again.
func (step EnsurePlatformIssue) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	incident := comm.Must(IncidentAttribute.Get(ctx))
	filed, err := step.f.issues.EnsureIssue(ctx, incident.issue())
	switch {
	case errors.Is(err, githubissues.ErrNotConfigured):
		return dex.GoTo(RecordPlatformIssue{}, Outcome{Status: outcomeLogged}), nil
	case errors.Is(err, githubissues.ErrRejected):
		return dex.GoTo(RecordPlatformIssue{}, Outcome{Status: outcomeDropped, Failure: dexflow.TruncateCause(err.Error())}), nil
	case err != nil:
		logAttemptFailure(ctx, step.f.logger, incident, err)
		return nil, dex.ErrorWithStack(fmt.Errorf("file the issue of platform incident %s: %w", incident.signature(), err))
	case !filed.Labeled:
		// The decision commits the opened issue: a retried label call never opens another.
		return dex.GoTo(LabelPlatformIssue{}, filedOutcome(filed)), nil
	}
	return dex.GoTo(RecordPlatformIssue{}, filedOutcome(filed)), nil
}

// dex:group group-id:issue group-label:"Issue"
// dex:explanation text:"Attach the signature's label to the issue the report opened, which GitHub opened without it, retrying only a transient failure."
type LabelPlatformIssue struct {
	dex.StepDefaultsNoWaitFor[Outcome]
	f *PlatformIssueFlow
}

func (step LabelPlatformIssue) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordPlatformIssue{f: step.f}, nil))
}

func (step LabelPlatformIssue) Execute(ctx dex.Context, opened Outcome) (*dex.StepDecision, error) {
	incident := comm.Must(IncidentAttribute.Get(ctx))
	err := step.f.issues.LabelIssue(ctx, opened.IssueNumber, githubissues.IncidentLabel(incident.Environment, incident.signature()))
	switch {
	case errors.Is(err, githubissues.ErrRejected):
		opened.Failure = dexflow.TruncateCause(err.Error())
	case err != nil:
		logAttemptFailure(ctx, step.f.logger, incident, err)
		return nil, dex.ErrorWithStack(fmt.Errorf("label the issue of platform incident %s: %w", incident.signature(), err))
	}
	return dex.GoTo(RecordPlatformIssue{}, opened), nil
}

// dex:group group-id:issue group-label:"Issue"
// dex:explanation text:"Record the outcome of the report, with the final error of a label call whose retries ran out, log it and complete."
type RecordPlatformIssue struct {
	dex.StepDefaultsNoWaitFor[Outcome]
	f *PlatformIssueFlow
}

func (RecordPlatformIssue) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (step RecordPlatformIssue) Execute(ctx dex.Context, outcome Outcome) (*dex.StepDecision, error) {
	incident := comm.Must(IncidentAttribute.Get(ctx))
	if detail := dexflow.RecoveryDetail(ctx); detail != "" {
		outcome.Failure = detail
	}
	comm.MustDo(OutcomeAttribute.Set(ctx, &outcome))
	comm.MustDo(ctx.RecordEvent(outcomeEvent, outcomeRecord(incident, outcome)))
	logOutcome(ctx, step.f.logger, incident, outcome)
	return dex.GracefulComplete(receipt{Signature: incident.signature(), Status: outcome.Status}), nil
}

// dex:group group-id:issue group-label:"Issue"
// dex:explanation text:"Drop a report whose retries ran out, with the final error; each failed attempt was logged."
type DropPlatformIssue struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (DropPlatformIssue) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (DropPlatformIssue) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	return dex.GoTo(RecordPlatformIssue{}, Outcome{Status: outcomeDropped, Failure: dexflow.RecoveryDetail(ctx)}), nil
}
