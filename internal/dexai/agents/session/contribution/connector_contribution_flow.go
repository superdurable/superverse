// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package contribution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

var (
	// The contribution: its identity and Project, its status, the latest
	// submission of a develop subagent and the record the Flow owns.
	identityAttribute = dex.DefineAttribute[Identity]("Identity")
	// dex:indexed-attribute attribute-key:ProjectID index-key:keyword2 index-type:keyword value-type:string description:"Project whose Session contributes"
	projectIDAttribute = dex.DefineAttribute[string]("ProjectID", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword2"}))
	// dex:indexed-attribute attribute-key:ContributionStatus index-key:keyword3 index-type:keyword value-type:string description:"Connector contribution status"
	statusAttribute     = dex.DefineAttribute[string]("ContributionStatus", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword3"}))
	submissionAttribute = dex.DefineAttribute[Submission]("Submission")
	recordAttribute     = dex.DefineAttribute[Record]("Contribution")
	// The close of a deleting Project's contribution.
	endChannel = dex.DefineChannel[contributionEnd]("ContributionEnd")
)

// ConnectorContributionFlow owns one connector contribution of a Project: its record, draft pull
// request and release candidates, reported through the Session Service.
type ConnectorContributionFlow struct {
	dex.FlowDefaults
	dependency        ConnectorContributionFlowDependency
	pullRequests      ConnectorPullRequests
	releaseCandidates ReleaseCandidates
}

// GetSteps registers the admission, the pull request, its record and the
// connector notification, beside the wait for the contribution's end.
func (flow *ConnectorContributionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(AdmitConnectorContribution{flow: flow}), dex.DefineStep(AwaitConnectorContributionEnd{}),
		dex.DefineStep(OpenConnectorPullRequest{flow: flow}), dex.DefineStep(RecordConnectorPullRequest{}),
		dex.DefineStep(ReportConnectorEvent{flow: flow}), dex.DefineStep(DropConnectorEvent{}),
		dex.DefineStep(PublishReleaseCandidate{flow: flow}), dex.DefineStep(RecordReleaseCandidate{}),
	}
}

// GetRPCs registers the later submissions, the close and the retained read.
func (flow *ConnectorContributionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.UpdateConnectorContribution, dexflow.ShortRPC(statusLock)),
		dex.DefineRPC(flow.CloseConnectorContribution, dexflow.ShortRPC(statusLock)),
		dex.DefineRPC(flow.GetConnectorContributionSnapshot, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexSummary, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexDisplay, &dex.RPCOptions{Timeout: readRPCTimeout}),
	}
}

// GetPersistenceSchema registers every durable value.
func (*ConnectorContributionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{identityAttribute, projectIDAttribute, statusAttribute, submissionAttribute, recordAttribute},
		Channels:   []dex.ChannelDef{endChannel},
	}
}

// Every writer of the record holds the status lock; the pull request Step
// polls without it and its record Step commits the result.
var (
	statusLock        = dex.LockAttribute(statusAttribute)
	readRPCTimeout    = 5 * time.Second
	recordStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteRetry: dexflow.StepRetry(2 * time.Minute), ExecuteLockAttributes: []dex.AttributeLock{statusLock}}
)

// pullRequestWaitLimit bounds the opening of the draft pull request, and
// releaseCandidateWaitLimit the library's publication of a release candidate.
const (
	pullRequestWaitLimit      = 5 * time.Minute
	releaseCandidateWaitLimit = 25 * time.Minute
)

// UpdateConnectorContribution applies a later head of the connector's branch; a repeated one
// changes nothing, and one whose pull request failed opens it again.
func (flow *ConnectorContributionFlow) UpdateConnectorContribution(ctx dex.Context, submission Submission) (*dex.RPCResult[bool], error) {
	status := statusOf(comm.Must(statusAttribute.Get(ctx)))
	if status.Closed() || submission.Identity != comm.Must(identityAttribute.Get(ctx)) {
		return &dex.RPCResult[bool]{Output: false}, nil
	}
	comm.MustDo(submissionAttribute.Set(ctx, submission))
	record := dexflow.OptionalValue(recordAttribute.Get(ctx))
	if record == nil || record.HeadCommit == submission.Contribution.HeadCommit {
		// The admission records the latest submission.
		return &dex.RPCResult[bool]{Output: true}, nil
	}
	updated, reopens := record.update(submission, flow.pullRequests.OpensPullRequests(), time.Now().UTC())
	comm.MustDo(recordAttribute.Set(ctx, updated))
	comm.MustDo(statusAttribute.Set(ctx, updated.Status.String()))
	var next []dex.StepMovement
	if reopens {
		next = append(next, dex.MovementOf(OpenConnectorPullRequest{}, pullRequestAttempt{HeadCommit: updated.HeadCommit}))
	}
	// A candidate the library refused at once is recorded; another is published.
	switch attempt := candidateAttemptOf(submission.Contribution); {
	case attempt != nil && attempt.Failure != "":
		next = append(next, dex.MovementOf(RecordReleaseCandidate{}, *attempt))
	case attempt != nil:
		next = append(next, dex.MovementOf(PublishReleaseCandidate{}, *attempt))
	}
	return &dex.RPCResult[bool]{Output: true, NextSteps: next}, nil
}

// CloseConnectorContribution ends the contribution of a deleting Project and
// answers whether it has ended; the pull request and the branch stay.
func (*ConnectorContributionFlow) CloseConnectorContribution(ctx dex.Context, _ dex.None) (*dex.RPCResult[bool], error) {
	if statusOf(comm.Must(statusAttribute.Get(ctx))).Closed() {
		return &dex.RPCResult[bool]{Output: true}, nil
	}
	comm.MustDo(endChannel.Publish(ctx, contributionEnd{}))
	return &dex.RPCResult[bool]{Output: false}, nil
}

// GetConnectorContributionSnapshot reads the contribution without a lock, so
// it also serves a completed execution for the engine's retention.
func (*ConnectorContributionFlow) GetConnectorContributionSnapshot(ctx dex.Context, _ dex.None) (*dex.RPCResult[Snapshot], error) {
	snapshot := Snapshot{Identity: comm.Must(identityAttribute.Get(ctx)), Record: dexflow.ValueOrZero(recordAttribute.Get(ctx))}
	if comm.Must(projectIDAttribute.Get(ctx)) != snapshot.Identity.ProjectID {
		panic("a contribution's ProjectID differs from its identity")
	}
	snapshot.Record.Status = statusOf(comm.Must(statusAttribute.Get(ctx)))
	return &dex.RPCResult[Snapshot]{Output: snapshot}, nil
}

// GetDexSummary is the contribution row of the Dex Web Flow list, whose FlowID
// names the Project and the connector and whose indexed status Dex shows.
func (*ConnectorContributionFlow) GetDexSummary(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay is the contribution header of the Dex Web Flow view.
//
// dex:field attribute-key:ContributionStatus value-type:string editable:false description:"Connector contribution status"
// dex:field attribute-key:Contribution value-type:json editable:false description:"The contribution's branch, head commit and pull request"
func (*ConnectorContributionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"ContributionStatus": comm.Must(statusAttribute.Get(ctx)), "Contribution": dexflow.OptionalValue(recordAttribute.Get(ctx)),
	}}, nil
}

// dex:group group-id:contribution group-label:"Contribution"
// dex:explanation text:"Ask the Session to admit the contribution before any effect, then record it and open its pull request beside the wait for its end."
type AdmitConnectorContribution struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ConnectorContributionFlow
}

// GetStepOptions: one Session read under the status lock, which a later
// submission waits for.
func (AdmitConnectorContribution) GetStepOptions() *dex.StepOptions {
	options := dexflow.ProviderStepOptions(nil)
	options.ExecuteLockAttributes = []dex.AttributeLock{statusLock}
	return options
}

// Execute closes a contribution whose Session is no longer open without an effect; any other is
// recorded, and opens its pull request where the server names an upstream library.
func (step AdmitConnectorContribution) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	identity, submission := comm.Must(identityAttribute.Get(ctx)), comm.Must(submissionAttribute.Get(ctx))
	admitted, err := step.flow.dependency.AdmitConnectorContribution(ctx, identity)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("admit the contribution %s with its Session: %w", identity.Slug, err))
	}
	now := time.Now().UTC()
	if !admitted {
		comm.MustDo(recordAttribute.Set(ctx, submission.record(StatusClosed, now)))
		comm.MustDo(statusAttribute.Set(ctx, StatusClosed.String()))
		return dex.GracefulComplete(receipt{Identity: identity, Status: StatusClosed}), nil
	}
	status := StatusPullRequestPending
	if !step.flow.pullRequests.OpensPullRequests() || submission.Contribution.Location == connectorlibrary.LocationSandbox {
		status = StatusUnpublished
	}
	record := submission.record(status, now)
	comm.MustDo(recordAttribute.Set(ctx, record))
	comm.MustDo(statusAttribute.Set(ctx, status.String()))
	next := []dex.StepMovement{dex.MovementOf(AwaitConnectorContributionEnd{}, nil)}
	if status != StatusUnpublished {
		next = append(next, dex.MovementOf(OpenConnectorPullRequest{}, pullRequestAttempt{HeadCommit: record.HeadCommit}))
	}
	// A candidate the library refused at once is recorded; another is published.
	switch attempt := candidateAttemptOf(submission.Contribution); {
	case attempt != nil && attempt.Failure != "":
		next = append(next, dex.MovementOf(RecordReleaseCandidate{}, *attempt))
	case attempt != nil:
		next = append(next, dex.MovementOf(PublishReleaseCandidate{}, *attempt))
	}
	return dex.GoToMany(next...), nil
}

// dex:group group-id:contribution group-label:"Contribution"
// dex:explanation text:"Wait for the close of a deleting Project's contribution, then end it, cancelling a pull request still being opened."
type AwaitConnectorContributionEnd struct {
	dex.StepDefaults
}

func (AwaitConnectorContributionEnd) GetStepOptions() *dex.StepOptions { return recordStepOptions }

func (AwaitConnectorContributionEnd) WaitFor(dex.Context, dex.None) (*dex.Wait, error) {
	return dex.Until(endChannel.ForOne()), nil
}

// Execute records the end; the pull request and the branch stay on GitHub.
func (AwaitConnectorContributionEnd) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	identity, record := comm.Must(identityAttribute.Get(ctx)), comm.Must(recordAttribute.Get(ctx))
	record.Status, record.UpdatedAt = StatusClosed, time.Now().UTC()
	comm.MustDo(recordAttribute.Set(ctx, record))
	comm.MustDo(statusAttribute.Set(ctx, StatusClosed.String()))
	return dex.GracefulComplete(receipt{Identity: identity, Status: StatusClosed}).CancelSteps(OpenConnectorPullRequest{}, RecordConnectorPullRequest{},
		ReportConnectorEvent{}, DropConnectorEvent{}, PublishReleaseCandidate{}, RecordReleaseCandidate{}), nil
}

// dex:group group-id:pull-request group-label:"Pull request"
// dex:explanation text:"Find the pull request of the branch, or push the branch from the local Git host and open it as a draft, polling without a lock."
type OpenConnectorPullRequest struct {
	dex.StepDefaultsNoWaitFor[pullRequestAttempt]
	flow *ConnectorContributionFlow
}

// GetStepOptions: a polling Step whose passed limit records the failure.
func (OpenConnectorPullRequest) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(pullRequestWaitLimit, dex.ProceedToOnExecuteFailure(RecordConnectorPullRequest{}, nil))
}

// Execute looks the branch's pull request up first in every round, so a lost
// answer opens no second one; a refusal of GitHub ends the attempt at once.
func (step OpenConnectorPullRequest) Execute(ctx dex.Context, attempt pullRequestAttempt) (*dex.StepDecision, error) {
	identity, record := comm.Must(identityAttribute.Get(ctx)), comm.Must(recordAttribute.Get(ctx))
	err := dexflow.PollUntil(ctx, 10*time.Second, 40*time.Second, func(round context.Context) (bool, error) {
		opened, err := step.flow.pullRequests.OpenDraftPullRequest(round, record.draft(identity.Slug), record.Location)
		switch {
		case errors.Is(err, connectorlibrary.ErrRejected), errors.Is(err, connectorlibrary.ErrNotConfigured):
			attempt.Failure = dexflow.TruncateCause(err.Error())
			return true, nil
		case err != nil:
			return false, fmt.Errorf("open the pull request of %s: %w", record.Branch, err)
		}
		attempt.PullRequest = &PullRequest{Number: opened.Number, URL: opened.URL, NodeID: opened.NodeID, State: opened.State}
		return true, nil
	})
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	return dex.GoTo(RecordConnectorPullRequest{}, attempt), nil
}

// dex:group group-id:pull-request group-label:"Pull request"
// dex:explanation text:"Record the draft pull request, or the failure of a refused or timed-out opening, and report a failure to the Session."
type RecordConnectorPullRequest struct {
	dex.StepDefaultsNoWaitFor[pullRequestAttempt]
}

func (RecordConnectorPullRequest) GetStepOptions() *dex.StepOptions { return recordStepOptions }

// Execute keeps a pull request once recorded: a failure never replaces it,
// and a closed contribution records nothing.
func (RecordConnectorPullRequest) Execute(ctx dex.Context, attempt pullRequestAttempt) (*dex.StepDecision, error) {
	identity, record := comm.Must(identityAttribute.Get(ctx)), comm.Must(recordAttribute.Get(ctx))
	if record.Status.Closed() || record.Status.HasPullRequest() {
		return dex.DeadEnd(), nil
	}
	record.UpdatedAt = time.Now().UTC()
	if attempt.PullRequest != nil {
		record.PullRequest, record.Status, record.Failure = attempt.PullRequest, StatusDraft, ""
		comm.MustDo(recordAttribute.Set(ctx, record))
		comm.MustDo(statusAttribute.Set(ctx, record.Status.String()))
		return dex.DeadEnd(), nil
	}
	record.Status, record.Failure = StatusPullRequestFailed, attempt.Failure
	if record.Failure == "" {
		record.Failure = fmt.Sprintf("GitHub did not open it within %s: %s", pullRequestWaitLimit, dexflow.RecoveryDetail(ctx))
	}
	comm.MustDo(recordAttribute.Set(ctx, record))
	comm.MustDo(statusAttribute.Set(ctx, record.Status.String()))
	return dex.GoTo(ReportConnectorEvent{}, failureNotification(identity, record, record.UpdatedAt)), nil
}

// dex:group group-id:pull-request group-label:"Pull request"
// dex:explanation text:"Deliver the connector notification of a failed pull request to the Session through the Session Service, once per head commit."
type ReportConnectorEvent struct {
	dex.StepDefaultsNoWaitFor[conversation.SessionNotification]
	flow *ConnectorContributionFlow
}

// GetStepOptions: one Service call; after exhausted retries the event is
// dropped, and the record keeps the failure.
func (ReportConnectorEvent) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(DropConnectorEvent{}, nil))
}

// Execute delivers the notification, whose ID makes a repeat harmless; a
// Session that is busy or closed does not take it, and the record keeps it.
func (step ReportConnectorEvent) Execute(ctx dex.Context, notification conversation.SessionNotification) (*dex.StepDecision, error) {
	if _, err := step.flow.dependency.ReportConnectorEvent(ctx, comm.Must(identityAttribute.Get(ctx)), notification); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("report %s to the Session: %w", notification.ID, err))
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:pull-request group-label:"Pull request"
// dex:explanation text:"Drop a connector notification whose delivery exhausted its retries; the record keeps the failure."
type DropConnectorEvent struct {
	dex.StepDefaultsNoWaitFor[conversation.SessionNotification]
}

func (DropConnectorEvent) Execute(dex.Context, conversation.SessionNotification) (*dex.StepDecision, error) {
	return dex.DeadEnd(), nil
}

// dex:group group-id:release-candidate group-label:"Release candidate"
// dex:explanation text:"Request the head's release candidate of the connectors library and poll until its release holds its assets, without a lock."
type PublishReleaseCandidate struct {
	dex.StepDefaultsNoWaitFor[candidateAttempt]
	flow *ConnectorContributionFlow
}

// GetStepOptions: a polling Step whose passed limit records the failure.
func (PublishReleaseCandidate) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(releaseCandidateWaitLimit, dex.ProceedToOnExecuteFailure(RecordReleaseCandidate{}, nil))
}

// Execute looks the release up first in every round, so a lost answer or a
// restart requests nothing twice; a refusal of the library ends the attempt.
func (step PublishReleaseCandidate) Execute(ctx dex.Context, attempt candidateAttempt) (*dex.StepDecision, error) {
	requested := false
	err := dexflow.PollUntil(ctx, 10*time.Second, 40*time.Second, func(round context.Context) (bool, error) {
		published, err := step.flow.releaseCandidates.IsReleaseCandidatePublished(round, attempt.Candidate, attempt.Location)
		switch {
		case isCandidateRefusal(err):
			attempt.Failure = dexflow.TruncateCause(err.Error())
			return true, nil
		case err != nil:
			return false, fmt.Errorf("read the release candidate %s: %w", attempt.Candidate.Tag, err)
		case published:
			attempt.Published = true
			return true, nil
		case requested:
			return false, nil
		}
		if err = step.flow.releaseCandidates.RequestReleaseCandidate(round, attempt.Candidate, attempt.Location); isCandidateRefusal(err) {
			attempt.Failure = dexflow.TruncateCause(err.Error())
			return true, nil
		} else if err != nil {
			return false, fmt.Errorf("request the release candidate %s: %w", attempt.Candidate.Tag, err)
		}
		requested = true
		return false, nil
	})
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	return dex.GoTo(RecordReleaseCandidate{}, attempt), nil
}

// isCandidateRefusal reports an answer of the library that asking again cannot change.
func isCandidateRefusal(err error) bool {
	return errors.Is(err, connectorlibrary.ErrRejected) || errors.Is(err, connectorlibrary.ErrNotConfigured) ||
		errors.Is(err, connectorlibrary.ErrReleaseCandidateConflict)
}

// dex:group group-id:release-candidate group-label:"Release candidate"
// dex:explanation text:"Record the head's release candidate, published or failed, and report it to the Session; a candidate of an older head is dropped."
type RecordReleaseCandidate struct {
	dex.StepDefaultsNoWaitFor[candidateAttempt]
}

func (RecordReleaseCandidate) GetStepOptions() *dex.StepOptions { return recordStepOptions }

// Execute records the candidate of the current head only: a later head brings
// its own candidate, and a closed contribution records nothing.
func (RecordReleaseCandidate) Execute(ctx dex.Context, attempt candidateAttempt) (*dex.StepDecision, error) {
	identity, record := comm.Must(identityAttribute.Get(ctx)), comm.Must(recordAttribute.Get(ctx))
	if record.Status.Closed() || record.HeadCommit != attempt.Candidate.Commit {
		return dex.DeadEnd(), nil
	}
	candidate := ReleaseCandidate{ReleaseCandidate: attempt.Candidate, Status: ReleaseCandidatePublished, UpdatedAt: time.Now().UTC()}
	if !attempt.Published {
		candidate.Status, candidate.Failure = ReleaseCandidateFailed, attempt.Failure
		if candidate.Failure == "" {
			candidate.Failure = fmt.Sprintf("the connectors library did not publish it within %s: %s", releaseCandidateWaitLimit, dexflow.RecoveryDetail(ctx))
		}
	}
	record.ReleaseCandidate, record.UpdatedAt = &candidate, candidate.UpdatedAt
	comm.MustDo(recordAttribute.Set(ctx, record))
	if attempt.Reported == candidate.Status {
		// The subagent's task notification already told the Session.
		return dex.DeadEnd(), nil
	}
	return dex.GoTo(ReportConnectorEvent{}, candidateNotification(identity, record, candidate)), nil
}
