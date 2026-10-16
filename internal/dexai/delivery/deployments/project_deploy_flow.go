// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

var (
	StartIdentity = dex.DefineAttribute[dexflow.StartIdentity]("StartIdentity", dex.SyncToAttributeStore())
	// dex:indexed-attribute attribute-key:ProjectID index-key:keyword2 index-type:keyword value-type:string description:"Owning project"
	ProjectID             = dex.DefineAttribute[string]("ProjectID", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword2"}))
	DeploymentID          = dex.DefineAttribute[string]("DeploymentID")
	ReleaseID             = dex.DefineAttribute[string]("ReleaseID", dex.SyncToAttributeStore())
	BuildRevisionID       = dex.DefineAttribute[string]("BuildRevisionID", dex.SyncToAttributeStore())
	ConfigurationRevision = dex.DefineAttribute[uint64]("ConfigurationRevision", dex.SyncToAttributeStore())
	ExpectedLiveRevision  = dex.DefineAttribute[uint64]("ExpectedLiveRevision")
	// dex:indexed-attribute attribute-key:DeploymentStatus index-key:keyword3 index-type:keyword value-type:string description:"Business lifecycle status"
	DeploymentStatus       = dex.DefineAttribute[string]("DeploymentStatus", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword3"}), dex.SyncToAttributeStore())
	ProjectDeploymentLease = dex.DefineAttribute[projects.DeploymentLease]("ProjectDeploymentLease", dex.SyncToAttributeStore())
	PinnedInputs           = dex.DefineAttribute[Inputs]("PinnedInputs", dex.SyncToAttributeStore())
	WorkloadGrantAttribute = dex.DefineAttribute[WorkloadGrant]("WorkloadGrant", dex.SyncToAttributeStore())
	// DeploymentResourcesAttribute is the deployment's copy of the project
	// references it runs on and its generation's Service.
	DeploymentResourcesAttribute = dex.DefineAttribute[DeploymentResources]("DeploymentResources", dex.SyncToAttributeStore())
	ProviderOperationRefs        = dex.DefineAttribute[ProviderRefs]("ProviderOperationRefs", dex.SyncToAttributeStore())
	// PhaseTimings measure each phase and preparation branch that ran, its
	// latest run only.
	PhaseTimings     = dex.DefineAttribute[[]dexflow.PhaseTiming]("PhaseTimings", dex.SyncToAttributeStore())
	ReadinessResult  = dex.DefineAttribute[Readiness]("ReadinessResult", dex.SyncToAttributeStore())
	DeploymentURL    = dex.DefineAttribute[string]("DeploymentURL", dex.SyncToAttributeStore())
	CleanupRequested = dex.DefineAttribute[bool]("CleanupRequested")
	// ReplacedLiveAttribute is the Live generation this deployment replaces; RetirementAttribute
	// the admitted request of the generation that replaced this one.
	ReplacedLiveAttribute = dex.DefineAttribute[ReplacedGeneration]("ReplacedLive")
	RetirementAttribute   = dex.DefineAttribute[Retirement]("Retirement")
	// CancellationAttribute is a person's admitted request to cancel the deployment, nil without one.
	CancellationAttribute = dex.DefineAttribute[*DeploymentCancellation]("Cancellation", dex.SyncToAttributeStore())
	FailureAttribute      = dex.DefineAttribute[*dexflow.Failure]("Failure", dex.SyncToAttributeStore())
	Revision              = dex.DefineAttribute[uint64]("Revision", dex.SyncToAttributeStore())
	CreatedAt             = dex.DefineAttribute[time.Time]("CreatedAt", dex.SyncToAttributeStore())
	UpdatedAt             = dex.DefineAttribute[time.Time]("UpdatedAt", dex.SyncToAttributeStore())
	// DeploymentRecoveryDecisions wakes AwaitDeploymentRecovery with the ID of the request
	// that admitted a retry or Project cleanup; the retained status decides what runs.
	DeploymentRecoveryDecisions = dex.DefineChannel[string]("DeploymentRecoveryDecisions")
	// LivePreparationResults carries each branch's one result of a preparation round to
	// AwaitLiveExecutionPreparation under the key prepare-<generation>-<round>.
	LivePreparationResults = dex.DefineChannelMap[LivePreparationResult]("LivePreparationResults")
	DeploymentProgress     = dex.DefineStream[dexflow.ProgressEvent]("DeploymentProgress", 10<<20)
)

// ProjectDeployFlow owns one explicit Live deployment attempt: pinned release and configuration,
// the generation's Project lease, provider effects and cleanup; the serving re-exports no infrastructure role.
type ProjectDeployFlow struct {
	dex.FlowDefaults
	projects      *projects.ProjectFlowClient
	releases      *releases.ProjectReleaseFlowClient
	objects       *objectstore.Store
	configuration *projectconfig.Configuration
	usages        *connections.Usages
	serving       Serving
	storage       infra.Storage
	execution     infra.Execution
	grants        infra.Grants
	redactor      infra.Redactor
	feedback      releases.DeliveryFeedback
	// deployments asks a replaced generation to retire, which then drains for
	// retirementDrain.
	deployments     *ProjectDeployFlowClient
	retirementDrain time.Duration
}

func NewProjectDeployFlow(projectClient *projects.ProjectFlowClient, releaseClient *releases.ProjectReleaseFlowClient, deploymentClient *ProjectDeployFlowClient, retirementDrain time.Duration, objects *objectstore.Store, configuration *projectconfig.Configuration, usages *connections.Usages, serving Serving, storage infra.Storage, execution infra.Execution, grants infra.Grants, redactor infra.Redactor, feedback releases.DeliveryFeedback) *ProjectDeployFlow {
	return &ProjectDeployFlow{projects: projectClient, releases: releaseClient, deployments: deploymentClient, retirementDrain: retirementDrain, objects: objects, configuration: configuration, usages: usages, serving: serving, storage: storage, execution: execution, grants: grants, redactor: redactor, feedback: feedback}
}

func (f *ProjectDeployFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(AdmitDeployment{flow: f}), dex.DefineStep(AcquireProjectDeployment{flow: f}),
		dex.DefineStep(VerifyReleaseAndConfiguration{flow: f}), dex.DefineStep(PublishDefinitions{flow: f}),
		dex.DefineStep(PrepareLiveExecution{flow: f}), dex.DefineStep(PrepareLiveTemporal{flow: f}),
		dex.DefineStep(PrepareLiveAccess{flow: f}), dex.DefineStep(RecordLivePreparationFailure{flow: f}),
		dex.DefineStep(AwaitLiveExecutionPreparation{flow: f}), dex.DefineStep(StartLiveDex{flow: f}),
		dex.DefineStep(ApplyApplication{flow: f}), dex.DefineStep(VerifyReadiness{flow: f}),
		dex.DefineStep(CommitCurrentLive{flow: f}), dex.DefineStep(RequestReplacedRetirement{flow: f}), dex.DefineStep(ReleaseProjectDeployment{flow: f}),
		dex.DefineStep(DrainReplacedGeneration{flow: f}), dex.DefineStep(RetireReplacedGeneration{flow: f}),
		dex.DefineStep(CleanupCancelledDeployment{flow: f}), dex.DefineStep(CommitDeploymentPhase{flow: f}),
		dex.DefineStep(RecordDeploymentFailure{flow: f}), dex.DefineStep(AwaitDeploymentRecovery{flow: f}),
		dex.DefineStep(NotifyDeploymentFailure{flow: f}), dex.DefineStep(AbandonDeploymentFailureNotification{flow: f}),
	}
}

func (f *ProjectDeployFlow) GetRPCs() []dex.RPCDef {
	read := dexflow.ShortRPC()
	write := dexflow.ShortRPC(dex.LockAttribute(Revision))
	return []dex.RPCDef{
		dex.DefineRPC(f.GetProjectDeployment, read), dex.DefineRPC(f.GetDexSummary, read), dex.DefineRPC(f.GetDexDisplay, read),
		dex.DefineRPC(f.RequestLiveDeployment, write), dex.DefineRPC(f.RevokeDeploymentGrant, write), dex.DefineRPC(f.RequestProjectCleanup, write),
		dex.DefineRPC(f.RequestLiveRetirement, write), dex.DefineRPC(f.RequestDeploymentCancellation, write),
	}
}

func (f *ProjectDeployFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{StartIdentity, ProjectID, DeploymentID, ReleaseID, BuildRevisionID, ConfigurationRevision, ExpectedLiveRevision, DeploymentStatus, ProjectDeploymentLease, PinnedInputs, WorkloadGrantAttribute, DeploymentResourcesAttribute, ProviderOperationRefs, PhaseTimings, ReadinessResult, DeploymentURL, CleanupRequested, ReplacedLiveAttribute, RetirementAttribute, CancellationAttribute, FailureAttribute, Revision, CreatedAt, UpdatedAt}, Channels: []dex.ChannelDef{DeploymentRecoveryDecisions, LivePreparationResults}, Streams: []dex.StreamDef{DeploymentProgress}}
}

// initialAttributes describes an accepted deployment the client validated;
// receiptRequestID is the caller's request, recorded as the first request receipt.
func initialAttributes(in Start, identity dexflow.StartIdentity, receiptRequestID string) []dex.InitialAttributeDef {
	now := time.Now().UTC()
	b := &dexflow.InitialAttributes{}
	dexflow.Add(b, StartIdentity, identity)
	dexflow.Add(b, ProjectID, in.ProjectID)
	dexflow.Add(b, DeploymentID, in.DeploymentID)
	dexflow.Add(b, ReleaseID, in.ReleaseID)
	dexflow.Add(b, BuildRevisionID, in.BuildRevisionID)
	dexflow.Add(b, ConfigurationRevision, in.ConfigurationRevision)
	dexflow.Add(b, ExpectedLiveRevision, in.ExpectedLiveRevision)
	dexflow.Add(b, DeploymentStatus, Pending)
	dexflow.Add(b, ProjectDeploymentLease, projects.DeploymentLease{})
	dexflow.Add(b, PinnedInputs, Inputs{})
	dexflow.Add(b, WorkloadGrantAttribute, WorkloadGrant{})
	dexflow.Add(b, DeploymentResourcesAttribute, DeploymentResources{})
	dexflow.Add(b, ProviderOperationRefs, ProviderRefs{Phase: phaseAdmit, Requests: []DeploymentRequestReceipt{{RequestID: receiptRequestID}}})
	dexflow.Add(b, PhaseTimings, nil)
	dexflow.Add(b, ReadinessResult, Readiness{})
	dexflow.Add(b, DeploymentURL, "")
	dexflow.Add(b, CleanupRequested, false)
	dexflow.Add(b, ReplacedLiveAttribute, ReplacedGeneration{})
	dexflow.Add(b, RetirementAttribute, Retirement{})
	dexflow.Add(b, CancellationAttribute, nil)
	dexflow.Add(b, FailureAttribute, nil)
	dexflow.Add(b, Revision, uint64(1))
	dexflow.Add(b, CreatedAt, now)
	dexflow.Add(b, UpdatedAt, now)
	return b.Values()
}

func readSnapshot(ctx dex.Context) Snapshot {
	return comm.Must((&ProjectDeployFlow{}).GetProjectDeployment(ctx, nil)).Output
}

// bumpRevision records a committed change for Revision CAS and readers and
// returns the Revision of the commit.
func bumpRevision(ctx dex.Context) uint64 {
	revision := comm.Must(Revision.Get(ctx)) + 1
	comm.MustDo(Revision.Set(ctx, revision))
	comm.MustDo(UpdatedAt.Set(ctx, time.Now().UTC()))
	return revision
}

func lockRevision(o *dex.StepOptions) *dex.StepOptions {
	o.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(Revision)}
	return o
}

// failureDeployment is the code of every deployment failure.
const failureDeployment = "DEPLOYMENT_FAILED"

// deploymentFailure is the failure of a provider phase, which StartDeployment
// recovery or Project cleanup resumes.
func deploymentFailure(phase, reason string) *dexflow.Failure {
	return &dexflow.Failure{Code: failureDeployment, Phase: phase, Message: reason, Recoverable: true, Remedy: ownerRemedies[failureOwner(phase)]}
}

func (*ProjectDeployFlow) GetProjectDeployment(ctx dex.Context, _ dex.None) (*dex.RPCResult[Snapshot], error) {
	return &dex.RPCResult[Snapshot]{Output: Snapshot{
		Identity:              comm.Must(StartIdentity.Get(ctx)),
		ProjectID:             comm.Must(ProjectID.Get(ctx)),
		DeploymentID:          comm.Must(DeploymentID.Get(ctx)),
		ReleaseID:             comm.Must(ReleaseID.Get(ctx)),
		BuildRevisionID:       comm.Must(BuildRevisionID.Get(ctx)),
		ConfigurationRevision: comm.Must(ConfigurationRevision.Get(ctx)),
		ExpectedLiveRevision:  comm.Must(ExpectedLiveRevision.Get(ctx)),
		Status:                comm.Must(DeploymentStatus.Get(ctx)),
		Lease:                 comm.Must(ProjectDeploymentLease.Get(ctx)),
		Inputs:                comm.Must(PinnedInputs.Get(ctx)),
		Grant:                 comm.Must(WorkloadGrantAttribute.Get(ctx)),
		Resources:             comm.Must(DeploymentResourcesAttribute.Get(ctx)),
		Providers:             comm.Must(ProviderOperationRefs.Get(ctx)),
		Timings:               dexflow.ValueOrZero(PhaseTimings.Get(ctx)),
		Readiness:             comm.Must(ReadinessResult.Get(ctx)),
		URL:                   comm.Must(DeploymentURL.Get(ctx)),
		CleanupRequested:      comm.Must(CleanupRequested.Get(ctx)),
		ReplacedLive:          dexflow.ValueOrZero(ReplacedLiveAttribute.Get(ctx)),
		Retirement:            dexflow.ValueOrZero(RetirementAttribute.Get(ctx)),
		Cancellation:          dexflow.Optional(CancellationAttribute.Get(ctx)),
		Failure:               dexflow.Optional(FailureAttribute.Get(ctx)),
		Revision:              comm.Must(Revision.Get(ctx)),
		CreatedAt:             comm.Must(CreatedAt.Get(ctx)),
		UpdatedAt:             comm.Must(UpdatedAt.Get(ctx)),
	}}, nil
}

// dex:field attribute-key:DeploymentID value-type:string editable:false description:"Immutable lifecycle identity"
func (*ProjectDeployFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"DeploymentID": comm.Must(DeploymentID.Get(ctx))}}, nil
}

// dex:field attribute-key:DeploymentURL value-type:string editable:false description:"Public Live URL once activated"
func (*ProjectDeployFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"DeploymentURL": comm.Must(DeploymentURL.Get(ctx))}}, nil
}

func deploymentResult(ctx dex.Context, rejection *dexflow.Rejection) *dex.RPCResult[mutationResult] {
	return &dex.RPCResult[mutationResult]{Output: mutationResult{Deployment: readSnapshot(ctx), Rejection: rejection}}
}

// RequestLiveDeployment admits a StartDeployment request without changing the lease or pinned
// inputs; a request for a failed deployment at its current Revision resumes the failed phase.
func (*ProjectDeployFlow) RequestLiveDeployment(ctx dex.Context, in mutation) (*dex.RPCResult[mutationResult], error) {
	s := readSnapshot(ctx)
	if in.RequestID == "" {
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionInvalidArgument, "deployment request identity is required")), nil
	}
	if s.Status == Ready || slices.ContainsFunc(s.Providers.Requests, func(r DeploymentRequestReceipt) bool { return r.RequestID == in.RequestID }) {
		return deploymentResult(ctx, nil), nil
	}
	if s.cancelling() || s.Status == Removed || s.Status == Retiring || s.Status == Retired || s.Status == Cancelled {
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionFailedPrecondition, "deployment does not admit serving requests")), nil
	}
	if len(s.Providers.Requests) == 0 || len(s.Providers.Requests) >= 128 {
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionResourceExhausted, "deployment request receipt capacity is unavailable")), nil
	}
	receipt := DeploymentRequestReceipt{RequestID: in.RequestID}
	if s.Status == Failed {
		if in.ExpectedRevision != s.Revision {
			return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionAborted, "deployment failure revision changed")), nil
		}
		receipt.FailedRevision = s.Revision
		comm.MustDo(DeploymentStatus.Set(ctx, Recovering))
		comm.MustDo(DeploymentRecoveryDecisions.Publish(ctx, in.RequestID))
	}
	s.Providers.Requests = append(s.Providers.Requests, receipt)
	comm.MustDo(ProviderOperationRefs.Set(ctx, s.Providers))
	bumpRevision(ctx)
	return deploymentResult(ctx, nil), nil
}

// RevokeDeploymentGrant fences workload admission before the Project clears
// its Live pointer.
func (*ProjectDeployFlow) RevokeDeploymentGrant(ctx dex.Context, in mutation) (*dex.RPCResult[mutationResult], error) {
	s := readSnapshot(ctx)
	if s.Grant.Revoked {
		return deploymentResult(ctx, nil), nil
	}
	if in.RequestID == "" || in.ExpectedRevision != s.Revision {
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionAborted, "grant revision changed")), nil
	}
	s.Grant.Revoked = true
	comm.MustDo(WorkloadGrantAttribute.Set(ctx, s.Grant))
	if s.Status == Ready {
		comm.MustDo(DeploymentStatus.Set(ctx, Removed))
	}
	bumpRevision(ctx)
	return deploymentResult(ctx, nil), nil
}

// RequestProjectCleanup is admitted only by the deleting Project owner. A
// failed cleanup is admitted again at its current Revision.
func (*ProjectDeployFlow) RequestProjectCleanup(ctx dex.Context, in mutation) (*dex.RPCResult[mutationResult], error) {
	s := readSnapshot(ctx)
	if s.CleanupRequested && s.Status != Failed {
		return deploymentResult(ctx, nil), nil
	}
	if in.RequestID == "" || in.ExpectedRevision != s.Revision {
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionAborted, "deployment cleanup revision changed")), nil
	}
	comm.MustDo(CleanupRequested.Set(ctx, true))
	comm.MustDo(DeploymentStatus.Set(ctx, Cancelling))
	comm.MustDo(DeploymentRecoveryDecisions.Publish(ctx, in.RequestID))
	bumpRevision(ctx)
	return deploymentResult(ctx, nil), nil
}

// RequestLiveRetirement admits the retirement of a Live generation that a newer one replaced; a
// generation that cannot retire, or already does, is answered unchanged and its snapshot says why.
func (*ProjectDeployFlow) RequestLiveRetirement(ctx dex.Context, in retirementRequest) (*dex.RPCResult[mutationResult], error) {
	s := readSnapshot(ctx)
	if in.RequestID == "" || in.ReplacedBy.DeploymentID == "" || in.ReplacedBy.DeploymentID == s.DeploymentID || in.ReplacedBy.Generation <= s.Lease.Generation {
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionInvalidArgument, "retirement requires the newer generation that replaced this one")), nil
	}
	if !retirementEligible(s) {
		return deploymentResult(ctx, nil), nil
	}
	if s.Status != Ready {
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionFailedPrecondition, "only a READY generation retires")), nil
	}
	comm.MustDo(RetirementAttribute.Set(ctx, Retirement{RequestID: in.RequestID, ReplacedBy: in.ReplacedBy, RequestedAt: time.Now().UTC()}))
	s.Providers.Phase = phaseRetire
	comm.MustDo(ProviderOperationRefs.Set(ctx, s.Providers))
	comm.MustDo(DeploymentStatus.Set(ctx, Retiring))
	comm.MustDo(DeploymentRecoveryDecisions.Publish(ctx, in.RequestID))
	bumpRevision(ctx)
	return deploymentResult(ctx, nil), nil
}

// RequestDeploymentCancellation admits a person's cancellation before the Flow chooses to commit the deployment as
// Live, once; a FAILED one, or one polling its provider, is preempted into CleanupCancelledDeployment.
func (f *ProjectDeployFlow) RequestDeploymentCancellation(ctx dex.Context, in cancellationRequest) (*dex.RPCResult[mutationResult], error) {
	s := readSnapshot(ctx)
	switch {
	case in.RequestID == "":
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionInvalidArgument, "cancellation request identity is required")), nil
	case s.Cancellation != nil && (s.Cancellation.RequestID == in.RequestID || s.Status != Failed):
		return deploymentResult(ctx, nil), nil
	case s.CleanupRequested:
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionFailedPrecondition, "the Project is being deleted, and its deletion removes the deployment")), nil
	case s.Cancellation == nil && !s.cancellable():
		return deploymentResult(ctx, dexflow.Reject(dexflow.RejectionFailedPrecondition, "the deployment is live or going live, so it can no longer be cancelled")), nil
	}
	cancellation := DeploymentCancellation{RequestID: in.RequestID, ActorAccountID: in.ActorAccountID, RequestedAt: time.Now().UTC(), Phase: s.Providers.Phase,
		Preempted: s.Status != Failed && s.Providers.Phase == phaseApply}
	if s.Cancellation != nil {
		// A failed cleanup is admitted again under the new request.
		cancellation = *s.Cancellation
		cancellation.RequestID = in.RequestID
	}
	comm.MustDo(CancellationAttribute.Set(ctx, &cancellation))
	comm.MustDo(DeploymentStatus.Set(ctx, Cancelling))
	result := &dex.RPCResult[mutationResult]{}
	if s.preemptsPolling() {
		next := dex.MovementOf(ReleaseProjectDeployment{flow: f}, nil)
		if s.Providers.Phase != phaseReleaseLease {
			s.Providers.Phase = phaseCleanup
			comm.MustDo(ProviderOperationRefs.Set(ctx, s.Providers))
			next = dex.MovementOf(CleanupCancelledDeployment{flow: f}, nil)
		}
		result.NextSteps = []dex.StepMovement{next}
		result.CancelSteps(StartLiveDex{}, ApplyApplication{}, VerifyReadiness{}, RecordDeploymentFailure{}, CommitDeploymentPhase{}, NotifyDeploymentFailure{},
			AbandonDeploymentFailureNotification{}, AwaitDeploymentRecovery{})
	}
	bumpRevision(ctx)
	result.Output = mutationResult{Deployment: readSnapshot(ctx)}
	return result, nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Register exact Project membership before any provider effect."
type AdmitDeployment struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s AdmitDeployment) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s AdmitDeployment) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.CleanupRequested {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultAdmissionRejected}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseAdmit, state.Revision)))
	err := s.flow.admitDeployment(ctx, state)
	if errors.Is(err, projects.ErrChildAdmissionClosed) {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultAdmissionRejected}), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseAdmit, StartedAt: ctx.FirstAttemptAt().UTC()}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Acquire the Project deployment lease, which assigns this attempt's generation."
type AcquireProjectDeployment struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s AcquireProjectDeployment) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s AcquireProjectDeployment) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	// A later attempt may follow an acquire whose answer was lost, so it completes the idempotent acquire.
	if state.CleanupRequested || state.Cancellation != nil && ctx.Attempt() == 1 {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseAcquire, state.Revision)))
	lease, err := s.flow.projects.AcquireDeployment(ctx, state.ProjectID, projects.DeploymentLease{DeploymentID: state.DeploymentID, ExpectedLiveRevision: state.ExpectedLiveRevision})
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("acquire the deployment lease of Project %s: %w", state.ProjectID, err))
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseAcquire, StartedAt: ctx.FirstAttemptAt().UTC(), Lease: &lease}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Pin the exact ready build, its frozen configuration and the Project's storage."
type VerifyReleaseAndConfiguration struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s VerifyReleaseAndConfiguration) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s VerifyReleaseAndConfiguration) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.cancelling() {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseVerify, state.Revision)))
	inputs, resources, replaced, err := s.flow.verifyReleaseAndConfiguration(ctx, state)
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseVerify, StartedAt: ctx.FirstAttemptAt().UTC(), Inputs: &inputs, Resources: &resources, Replaced: &replaced}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Publish the build's Flow Definitions to the project's storage without activating them."
type PublishDefinitions struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s PublishDefinitions) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s PublishDefinitions) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.cancelling() {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phasePublish, state.Revision)))
	if err := s.flow.publishDefinitions(ctx, state); err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phasePublish, StartedAt: ctx.FirstAttemptAt().UTC()}), nil
}

// dex:group group-id:preparation group-label:"Live preparation"
// dex:explanation text:"Start a new round that prepares the Live Temporal and the Live access in parallel, and its join."
type PrepareLiveExecution struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

// GetStepOptions locks the Revision, so the round number and the cleanup fence
// are read and written with every RPC that changes them.
func (s PrepareLiveExecution) GetStepOptions() *dex.StepOptions {
	o := lockRevision(dexflow.StateStepOptions())
	o.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil)
	return o
}

func (s PrepareLiveExecution) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.cancelling() {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phasePrepare, state.Revision)))
	// A round starts only after the join consumed both results of the
	// previous one, so its key never holds a stale result.
	state.Providers.PreparationRound++
	comm.MustDo(ProviderOperationRefs.Set(ctx, state.Providers))
	bumpRevision(ctx)
	round := livePreparationInput{Round: state.Providers.PreparationRound, Key: preparationKey(state.Lease.Generation, state.Providers.PreparationRound), StartedAt: ctx.FirstAttemptAt().UTC()}
	return dex.GoToMany(
		dex.MovementOf(AwaitLiveExecutionPreparation{flow: s.flow}, round),
		dex.MovementOf(PrepareLiveTemporal{flow: s.flow}, round.branch(branchTemporal)),
		dex.MovementOf(PrepareLiveAccess{flow: s.flow}, round.branch(branchAccess)),
	), nil
}

// dex:group group-id:preparation group-label:"Live preparation"
// dex:explanation text:"Prepare or reuse the project's Live Temporal namespace and credential until its data plane accepts the credential, handing new identities to the Project; holds no lock."
type PrepareLiveTemporal struct {
	dex.StepDefaultsNoWaitFor[livePreparationInput]
	flow *ProjectDeployFlow
}

func (s PrepareLiveTemporal) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(prepareTemporalWaitLimit, dex.ProceedToOnExecuteFailure(RecordLivePreparationFailure{flow: s.flow}, nil))
}

func (s PrepareLiveTemporal) Execute(ctx dex.Context, in livePreparationInput) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, in.Branch, state.Revision)))
	result, err := s.flow.prepareBranch(ctx, state, in, func(call context.Context) (LivePreparationResult, error) {
		return s.flow.prepareLiveTemporal(call, state)
	}, func(reason string) {
		dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, in.Branch, state.Revision).WithDetail(reason)))
	})
	if err != nil {
		return nil, err
	}
	comm.MustDo(LivePreparationResults.Publish(ctx, in.Key, result))
	return dex.DeadEnd(), nil
}

// dex:group group-id:preparation group-label:"Live preparation"
// dex:explanation text:"Record the Live usages of the TeamSpace connections the version selects, then prepare or reuse the access of the Live Dex and applications, the generation's Service and its workload grant; holds no lock."
type PrepareLiveAccess struct {
	dex.StepDefaultsNoWaitFor[livePreparationInput]
	flow *ProjectDeployFlow
}

func (s PrepareLiveAccess) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(prepareAccessWaitLimit, dex.ProceedToOnExecuteFailure(RecordLivePreparationFailure{flow: s.flow}, nil))
}

func (s PrepareLiveAccess) Execute(ctx dex.Context, in livePreparationInput) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, in.Branch, state.Revision)))
	result, err := s.flow.prepareBranch(ctx, state, in, func(call context.Context) (LivePreparationResult, error) {
		return s.flow.prepareLiveAccess(call, state)
	}, func(reason string) {
		dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, in.Branch, state.Revision).WithDetail(reason)))
	})
	if err != nil {
		return nil, err
	}
	comm.MustDo(LivePreparationResults.Publish(ctx, in.Key, result))
	return dex.DeadEnd(), nil
}

// dex:group group-id:preparation group-label:"Live preparation"
// dex:explanation text:"Publish the failed result of a preparation branch whose attempts failed or passed its wait limit, with its classified cause."
type RecordLivePreparationFailure struct {
	dex.StepDefaultsNoWaitFor[livePreparationInput]
	flow *ProjectDeployFlow
}

// GetStepOptions fits one attempt to the bounded data-plane check. The
// classification never fails, so only a defect or a lost Worker retries.
func (RecordLivePreparationFailure) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(nil)
}

func (s RecordLivePreparationFailure) Execute(ctx dex.Context, in livePreparationInput) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	message := in.Branch + " failed."
	if dexflow.RecoveryTimedOut(ctx) {
		message = in.Branch + " did not finish within its wait limit of " + minutes(branchWaitLimit[in.Branch]) + "."
		if in.Branch == branchTemporal {
			message += " " + s.flow.temporalWaitCause(ctx, state)
		}
	}
	failure := dexflow.FailureMessage(message, s.flow.redactor.Redact(state.ProjectID, dexflow.RecoveryDetail(ctx)))
	// The branch committed nothing, so this is the round's one result of it.
	comm.MustDo(LivePreparationResults.Publish(ctx, in.Key, LivePreparationResult{Branch: in.Branch, Round: in.Round, Timing: measure(in.Branch, in.StartedAt), Failure: failure}))
	return dex.DeadEnd(), nil
}

// dex:group group-id:preparation group-label:"Live preparation"
// dex:explanation text:"Join the one result of each preparation branch and commit what they prepared, or the failure that ends the round."
type AwaitLiveExecutionPreparation struct {
	dex.StepDefaults
	flow *ProjectDeployFlow
}

// GetStepOptions declares no failure route: the join only merges committed
// results, so its failure is a defect.
func (AwaitLiveExecutionPreparation) GetStepOptions() *dex.StepOptions {
	return dexflow.StateStepOptions()
}

func (AwaitLiveExecutionPreparation) WaitFor(_ dex.Context, in livePreparationInput) (*dex.Wait, error) {
	return dex.Until(LivePreparationResults.ForN(in.Key, 2)), nil
}

func (s AwaitLiveExecutionPreparation) Execute(ctx dex.Context, in livePreparationInput) (*dex.StepDecision, error) {
	results := comm.Must(LivePreparationResults.GetConditionResults(ctx, in.Key))
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, joinLivePreparation(readSnapshot(ctx), in, results)), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Start the project's Live Dex on the prepared Temporal, handing its Service to the Project, polling without locks until it is ready."
type StartLiveDex struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s StartLiveDex) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(startDexWaitLimit, dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s StartLiveDex) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.cancelling() {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseStartDex, state.Revision)))
	resources, err := converge(ctx, func(call context.Context) (DeploymentResources, error) {
		return s.flow.startLiveDex(call, state)
	}, func(reason string) {
		dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseStartDex, state.Revision).WithDetail(reason)))
	})
	if err != nil {
		return nil, err
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseStartDex, StartedAt: ctx.FirstAttemptAt().UTC(), Resources: &resources}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Run the generation's application from its immutable image once its grant and project Dex are usable, polling without locks."
type ApplyApplication struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s ApplyApplication) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(applicationWaitLimit, dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s ApplyApplication) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.cancelling() {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseApply, state.Revision)))
	application, err := converge(ctx, func(call context.Context) (infra.ResourceRef, error) {
		return s.flow.applyApplication(call, state)
	}, func(reason string) {
		dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseApply, state.Revision).WithDetail(reason)))
	})
	if errors.Is(err, errLeaseLost) {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, s.flow.leaseLostFailure(state, phaseApply, err)), nil
	}
	if err != nil {
		return nil, err
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseApply, StartedAt: ctx.FirstAttemptAt().UTC(), Application: &application}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Observe the application, polling without locks until it serves the pinned image and definitions."
type VerifyReadiness struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s VerifyReadiness) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(readinessWaitLimit, dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s VerifyReadiness) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.cancelling() {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseReadiness, state.Revision)))
	readiness, err := converge(ctx, func(call context.Context) (Readiness, error) {
		return s.flow.verifyReadiness(call, state)
	}, func(reason string) {
		dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseReadiness, state.Revision).WithDetail(reason)))
	})
	if err != nil {
		return nil, err
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseReadiness, StartedAt: ctx.FirstAttemptAt().UTC(), Readiness: &readiness}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Activate the ready generation and commit it as the Project's Live pointer, polling without locks until the public route serves it."
type CommitCurrentLive struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s CommitCurrentLive) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(readinessWaitLimit, dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s CommitCurrentLive) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.CleanupRequested {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseCommitLive, state.Revision)))
	activation, err := converge(ctx, func(call context.Context) (Activation, error) {
		return s.flow.commitCurrentLive(call, state)
	}, func(reason string) {
		dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseCommitLive, state.Revision).WithDetail(reason)))
	})
	if err != nil {
		return nil, err
	}
	resources := state.Resources
	resources.Worker = activation.Worker
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseCommitLive, StartedAt: ctx.FirstAttemptAt().UTC(), URL: activation.URL, Resources: &resources}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Ask the Live generation this one replaced to retire, while this one still holds the Project lease."
type RequestReplacedRetirement struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s RequestReplacedRetirement) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s RequestReplacedRetirement) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.CleanupRequested {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseRequestRetirement, state.Revision)))
	if state.ReplacedLive != (ReplacedGeneration{}) {
		if err := s.flow.deployments.requestRetirement(ctx, state); err != nil {
			return nil, dex.ErrorWithStack(err)
		}
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseRequestRetirement, StartedAt: ctx.FirstAttemptAt().UTC()}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Release the Project deployment lease of this exact generation."
type ReleaseProjectDeployment struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s ReleaseProjectDeployment) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s ReleaseProjectDeployment) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseReleaseLease, state.Revision)))
	if err := s.flow.projects.ReleaseDeployment(ctx, state.ProjectID, state.Lease); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("release the deployment lease of Project %s: %w", state.ProjectID, err))
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseReleaseLease, StartedAt: ctx.FirstAttemptAt().UTC()}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Revoke and remove the generation of a deleting Project or a cancelled deployment, polling without locks until it is gone or the wait limit passes."
type CleanupCancelledDeployment struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s CleanupCancelledDeployment) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(cleanupWaitLimit, dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s CleanupCancelledDeployment) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseCleanup, state.Revision)))
	lease, err := converge(ctx, func(call context.Context) (*projects.DeploymentLease, error) {
		return s.flow.cleanupCancelledDeployment(call, state)
	}, func(reason string) {
		dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseCleanup, state.Revision).WithDetail(reason)))
	})
	if err != nil {
		return nil, err
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseCleanup, StartedAt: ctx.FirstAttemptAt().UTC(), Lease: lease}), nil
}

// dex:group group-id:retirement group-label:"Retirement"
// dex:explanation text:"Keep the replaced generation running for the drain window, so in-flight requests and reads finish; Project cleanup ends the wait."
type DrainReplacedGeneration struct {
	dex.StepDefaults
	flow *ProjectDeployFlow
}

func (DrainReplacedGeneration) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

// WaitFor measures the drain from the admitted request, so a Worker restart never extends it.
func (s DrainReplacedGeneration) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	remaining := max(s.flow.drainRemaining(readSnapshot(ctx).Retirement), time.Second)
	return dex.AnyOf(dex.Timer(remaining), DeploymentRecoveryDecisions.ForOne()), nil
}

func (s DrainReplacedGeneration) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	switch {
	case state.CleanupRequested:
		return dex.GoTo(CleanupCancelledDeployment{flow: s.flow}, nil), nil
	case s.flow.drainRemaining(state.Retirement) > 0:
		return dex.GoTo(DrainReplacedGeneration{flow: s.flow}, nil), nil
	}
	return dex.GoTo(RetireReplacedGeneration{flow: s.flow}, nil), nil
}

// dex:group group-id:retirement group-label:"Retirement"
// dex:explanation text:"Revoke the replaced generation's workload grant, then remove its objects, polling without locks until they are gone."
type RetireReplacedGeneration struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (s RetireReplacedGeneration) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(cleanupWaitLimit, dex.ProceedToOnExecuteFailure(RecordDeploymentFailure{flow: s.flow}, nil))
}

func (s RetireReplacedGeneration) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.CleanupRequested {
		return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: resultCleanupRequested}), nil
	}
	dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseRetire, state.Revision)))
	if _, err := converge(ctx, func(call context.Context) (struct{}, error) {
		return struct{}{}, s.flow.retireGeneration(call, state)
	}, func(reason string) {
		dexflow.StreamWritten(ctx, DeploymentProgress.Write(ctx, dexflow.Progress(ctx, phaseRetire, state.Revision).WithDetail(reason)))
	}); err != nil {
		return nil, err
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phaseRetire, StartedAt: ctx.FirstAttemptAt().UTC()}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Commit provider observations and timings before advancing the durable lifecycle; tell the Session of a failure outside Project cleanup."
type CommitDeploymentPhase struct {
	dex.StepDefaultsNoWaitFor[phaseResult]
	flow *ProjectDeployFlow
}

func (CommitDeploymentPhase) GetStepOptions() *dex.StepOptions {
	return lockRevision(dexflow.StateStepOptions())
}

func (s CommitDeploymentPhase) Execute(ctx dex.Context, in phaseResult) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if in.Phase == resultAdmissionRejected {
		comm.MustDo(DeploymentStatus.Set(ctx, Removed))
		comm.MustDo(CleanupRequested.Set(ctx, true))
		bumpRevision(ctx)
		return dex.GracefulComplete(readSnapshot(ctx)), nil
	}
	if in.Lease != nil {
		// The lease comes from the Project Flow; it must be this deployment's.
		if in.Lease.Generation == 0 || in.Lease.DeploymentID != state.DeploymentID {
			return nil, dex.ErrorWithStack(fmt.Errorf("the Project returned the lease %+v, which is not this deployment's", *in.Lease))
		}
		comm.MustDo(ProjectDeploymentLease.Set(ctx, *in.Lease))
	}
	if in.Inputs != nil {
		comm.MustDo(PinnedInputs.Set(ctx, *in.Inputs))
	}
	if in.Replaced != nil {
		comm.MustDo(ReplacedLiveAttribute.Set(ctx, *in.Replaced))
	}
	if in.Resources != nil {
		comm.MustDo(DeploymentResourcesAttribute.Set(ctx, *in.Resources))
	}
	if in.Grant != nil {
		comm.MustDo(WorkloadGrantAttribute.Set(ctx, *in.Grant))
	}
	if in.Readiness != nil {
		comm.MustDo(ReadinessResult.Set(ctx, *in.Readiness))
	}
	if in.URL != "" {
		comm.MustDo(DeploymentURL.Set(ctx, in.URL))
	}
	timings := in.Timings
	if !in.StartedAt.IsZero() {
		timings = append(timings, measure(in.Phase, in.StartedAt))
	}
	if len(timings) > 0 {
		comm.MustDo(PhaseTimings.Set(ctx, recordTimings(state.Timings, timings...)))
	}
	if in.Failure != nil && state.Cancellation != nil && !state.CleanupRequested && in.Phase != phaseCleanup && in.Phase != phaseReleaseLease {
		// A forward phase that failed after a cancellation is moot: its cleanup follows.
		in.Failure = nil
	}
	if in.Failure != nil {
		// Every FAILED deployment is committed here, keeping what the phase
		// recorded. The failed phase stays current, so recovery resumes it.
		comm.MustDo(FailureAttribute.Set(ctx, in.Failure))
		comm.MustDo(DeploymentStatus.Set(ctx, Failed))
		revision := bumpRevision(ctx)
		if state.cancelling() {
			// The cleanup of a deleting Project or a cancellation failed; nobody is told.
			return dex.GoTo(AwaitDeploymentRecovery{flow: s.flow}, nil), nil
		}
		return dex.GoTo(NotifyDeploymentFailure{flow: s.flow}, deliveryFailure(state, revision, *in.Failure, time.Now().UTC())), nil
	}
	if in.Phase == phaseReleaseLease {
		// Releasing the lease ends serving work: READY, REMOVED for Project cleanup, CANCELLED for a person's.
		status := Ready
		switch {
		case state.CleanupRequested:
			status = Removed
		case state.Cancellation != nil:
			status = Cancelled
		}
		comm.MustDo(DeploymentStatus.Set(ctx, status))
		comm.MustDo(FailureAttribute.Set(ctx, nil))
		bumpRevision(ctx)
		if state.cancelling() {
			return dex.GracefulComplete(readSnapshot(ctx)), nil
		}
		return dex.GoTo(AwaitDeploymentRecovery{flow: s.flow}, nil), nil
	}
	if in.Phase == phaseCleanup || in.Phase == phaseRetire {
		// Cleanup and retirement revoked even a grant whose issuance left no receipt.
		state.Grant.Revoked = true
		comm.MustDo(WorkloadGrantAttribute.Set(ctx, state.Grant))
	}
	if in.Phase == phaseRetire && !state.CleanupRequested {
		state.Retirement.RetiredAt = time.Now().UTC()
		comm.MustDo(RetirementAttribute.Set(ctx, state.Retirement))
		comm.MustDo(DeploymentStatus.Set(ctx, Retired))
		comm.MustDo(FailureAttribute.Set(ctx, nil))
		bumpRevision(ctx)
		return dex.GracefulComplete(readSnapshot(ctx)), nil
	}
	next := state.Providers.advance(in, state.cancelling())
	comm.MustDo(ProviderOperationRefs.Set(ctx, state.Providers))
	if !state.cancelling() {
		comm.MustDo(DeploymentStatus.Set(ctx, Deploying))
	}
	bumpRevision(ctx)
	switch next {
	case phaseAcquire:
		return dex.GoTo(AcquireProjectDeployment{flow: s.flow}, nil), nil
	case phaseVerify:
		return dex.GoTo(VerifyReleaseAndConfiguration{flow: s.flow}, nil), nil
	case phasePublish:
		return dex.GoTo(PublishDefinitions{flow: s.flow}, nil), nil
	case phasePrepare:
		return dex.GoTo(PrepareLiveExecution{flow: s.flow}, nil), nil
	case phaseStartDex:
		return dex.GoTo(StartLiveDex{flow: s.flow}, nil), nil
	case phaseApply:
		return dex.GoTo(ApplyApplication{flow: s.flow}, nil), nil
	case phaseReadiness:
		return dex.GoTo(VerifyReadiness{flow: s.flow}, nil), nil
	case phaseCommitLive:
		return dex.GoTo(CommitCurrentLive{flow: s.flow}, nil), nil
	case phaseRequestRetirement:
		return dex.GoTo(RequestReplacedRetirement{flow: s.flow}, nil), nil
	case phaseReleaseLease:
		return dex.GoTo(ReleaseProjectDeployment{flow: s.flow}, nil), nil
	case phaseCleanup:
		return dex.GoTo(CleanupCancelledDeployment{flow: s.flow}, nil), nil
	}
	panic("deployments: phase " + in.Phase + " has no next phase")
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Turn the redacted cause of a failed phase into the failure to commit for query-first recovery."
type RecordDeploymentFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectDeployFlow
}

func (RecordDeploymentFailure) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (s RecordDeploymentFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	phase := comm.Must(ProviderOperationRefs.Get(ctx)).Phase
	if step := dexflow.RecoveryStep(ctx); step == phaseCleanup || step == phaseReleaseLease {
		// Cleanup may run from a wakeup in the phase it was cancelled in.
		phase = step
	}
	reason := s.flow.redactor.Redact(comm.Must(ProjectID.Get(ctx)), dexflow.RecoveryDetail(ctx))
	if dexflow.RecoveryTimedOut(ctx) {
		reason = dexflow.FailureMessage(phase+" did not finish within its wait limit.", reason)
	}
	return dex.GoTo(CommitDeploymentPhase{flow: s.flow}, phaseResult{Phase: phase, Failure: deploymentFailure(phase, reason)}), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Tell the Project's Session, best effort, of the failure the commit recorded, then wait for a retry or Project cleanup."
type NotifyDeploymentFailure struct {
	dex.StepDefaultsNoWaitFor[releases.DeliveryFailure]
	flow *ProjectDeployFlow
}

// GetStepOptions: one call of the port without a lock; a failure that cannot
// be delivered is dropped, because the deployment keeps it.
func (s NotifyDeploymentFailure) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(AbandonDeploymentFailureNotification{flow: s.flow}, nil))
}

// Execute delivers the failure the commit recorded and saves nothing; the input names the FAILED
// Revision, so every retry repeats one notification, which the Session's receipts admit once.
func (s NotifyDeploymentFailure) Execute(ctx dex.Context, in releases.DeliveryFailure) (*dex.StepDecision, error) {
	if err := s.flow.feedback.ReportDeliveryFailure(ctx, in); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("report %s to the Session: %w", in.ID, err))
	}
	return dex.GoTo(AwaitDeploymentRecovery{flow: s.flow}, nil), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Drop a failure notification whose delivery exhausted its retries; the deployment keeps the failure."
type AbandonDeploymentFailureNotification struct {
	dex.StepDefaultsNoWaitFor[releases.DeliveryFailure]
	flow *ProjectDeployFlow
}

func (AbandonDeploymentFailureNotification) GetStepOptions() *dex.StepOptions {
	return dexflow.StateStepOptions()
}

// Execute keeps nothing: the engine history holds the cause.
func (s AbandonDeploymentFailureNotification) Execute(dex.Context, releases.DeliveryFailure) (*dex.StepDecision, error) {
	return dex.GoTo(AwaitDeploymentRecovery{flow: s.flow}, nil), nil
}

// dex:group group-id:deployments group-label:"Live deployment"
// dex:explanation text:"Wait for an admitted retry or Project cleanup request."
type AwaitDeploymentRecovery struct {
	dex.StepDefaults
	flow *ProjectDeployFlow
}

func (AwaitDeploymentRecovery) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (AwaitDeploymentRecovery) WaitFor(dex.Context, dex.None) (*dex.Wait, error) {
	return dex.Until(DeploymentRecoveryDecisions.ForOne()), nil
}

func (s AwaitDeploymentRecovery) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	switch {
	case state.Status == Removed:
		return dex.GoTo(AwaitDeploymentRecovery{flow: s.flow}, nil), nil
	case state.CleanupRequested, state.Cancellation != nil && state.Status == Cancelling:
		return dex.GoTo(CleanupCancelledDeployment{flow: s.flow}, nil), nil
	case state.Status == Retiring:
		return dex.GoTo(DrainReplacedGeneration{flow: s.flow}, nil), nil
	case state.Status != Recovering:
		return dex.GoTo(AwaitDeploymentRecovery{flow: s.flow}, nil), nil
	}
	switch state.Providers.Phase {
	case phaseAdmit:
		return dex.GoTo(AdmitDeployment{flow: s.flow}, nil), nil
	case phaseAcquire:
		return dex.GoTo(AcquireProjectDeployment{flow: s.flow}, nil), nil
	case phaseVerify:
		return dex.GoTo(VerifyReleaseAndConfiguration{flow: s.flow}, nil), nil
	case phasePublish:
		return dex.GoTo(PublishDefinitions{flow: s.flow}, nil), nil
	case phasePrepare:
		// A new round reuses what the failed one accepted.
		return dex.GoTo(PrepareLiveExecution{flow: s.flow}, nil), nil
	case phaseStartDex:
		return dex.GoTo(StartLiveDex{flow: s.flow}, nil), nil
	case phaseApply:
		return dex.GoTo(ApplyApplication{flow: s.flow}, nil), nil
	case phaseReadiness:
		return dex.GoTo(VerifyReadiness{flow: s.flow}, nil), nil
	case phaseCommitLive:
		return dex.GoTo(CommitCurrentLive{flow: s.flow}, nil), nil
	case phaseRequestRetirement:
		return dex.GoTo(RequestReplacedRetirement{flow: s.flow}, nil), nil
	case phaseReleaseLease:
		return dex.GoTo(ReleaseProjectDeployment{flow: s.flow}, nil), nil
	case phaseCleanup:
		return dex.GoTo(CleanupCancelledDeployment{flow: s.flow}, nil), nil
	case phaseRetire:
		return dex.GoTo(RetireReplacedGeneration{flow: s.flow}, nil), nil
	}
	panic("deployments: unknown recovery phase " + state.Providers.Phase)
}
