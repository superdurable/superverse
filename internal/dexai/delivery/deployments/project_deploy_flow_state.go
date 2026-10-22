// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments

import (
	"fmt"
	"slices"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// The values below are persisted in Attributes and Step inputs, so they must
// not change.

// Status is the business lifecycle of a deployment.
type Status = string

const (
	Pending    Status = "PENDING"
	Deploying  Status = "DEPLOYING"
	Ready      Status = "READY"
	Failed     Status = "FAILED"
	Recovering Status = "RECOVERING"
	// Cancelling is a deployment being cancelled: a person cancelled it, and it
	// ends CANCELLED, or its Project is being deleted, and it ends REMOVED.
	Cancelling Status = "CANCELLING"
	Removed    Status = "REMOVED"
	// Cancelled is a deployment a person cancelled before it went live; its
	// generation is removed and its lease released.
	Cancelled Status = "CANCELLED"
	// Retiring is a generation that a newer one replaced as Live; it drains
	// and ends RETIRED once its grant is revoked and its objects are gone.
	Retiring Status = "RETIRING"
	Retired  Status = "RETIRED"
)

// Phases of a deployment, persisted in ProviderRefs.Phase. Each names the
// Step that runs it; recovery resumes from it.
const (
	phaseAdmit             = "AdmitDeployment"
	phaseAcquire           = "AcquireProjectDeployment"
	phaseVerify            = "VerifyReleaseAndConfiguration"
	phasePublish           = "PublishDefinitions"
	phasePrepare           = "PrepareLiveExecution"
	phaseStartDex          = "StartLiveDex"
	phaseApply             = "ApplyApplication"
	phaseReadiness         = "VerifyReadiness"
	phaseCommitLive        = "CommitCurrentLive"
	phaseStopWorker        = "StopReplacedWorker"
	phaseStopCandidate     = "StopFailedCandidateWorker"
	phaseRequestRetirement = "RequestReplacedRetirement"
	phaseReleaseLease      = "ReleaseProjectDeployment"
	phaseCleanup           = "CleanupCancelledDeployment"
	// phaseRetire retires a generation that a newer one replaced, after its
	// drain.
	phaseRetire = "RetireReplacedGeneration"
)

// The branches of phasePrepare, which run in parallel. Each names the Step
// that runs it.
const (
	branchTemporal = "PrepareLiveTemporal"
	branchAccess   = "PrepareLiveAccess"
)

// DeployPhasesForTest are the phases and branches that DeploymentProgress frames may
// carry, each the name of the Step that runs it.
var DeployPhasesForTest = []string{
	phaseAdmit, phaseAcquire, phaseVerify, phasePublish, phasePrepare, branchTemporal, branchAccess, phaseStartDex, phaseApply,
	phaseReadiness, phaseCommitLive, phaseStopCandidate, phaseStopWorker, phaseRequestRetirement, phaseReleaseLease, phaseCleanup, phaseRetire,
}

// PollingPhasesForTest are the phases whose polling a cancellation preempts.
var PollingPhasesForTest = []string{phaseStartDex, phaseApply, phaseReadiness}

// The phase and the branch that a failed Live preparation reports, and the
// code of every deployment failure, which a deployment's Snapshot carries.
const (
	PhasePrepareForTest      = phasePrepare
	BranchAccessForTest      = branchAccess
	FailureDeploymentForTest = failureDeployment
)

// Phase results that end a phase without running its effect.
const (
	// resultAdmissionRejected: the Project was already being deleted.
	resultAdmissionRejected = "AdmissionRejected"
	// resultCleanupRequested: Project cleanup was admitted before the effect.
	resultCleanupRequested = "CancellationBoundary"
)

// nextPhase is the forward order of the provider phases.
var nextPhase = map[string]string{
	phaseAdmit: phaseAcquire, phaseAcquire: phaseVerify, phaseVerify: phasePublish,
	phasePublish: phasePrepare, phasePrepare: phaseStartDex, phaseStartDex: phaseApply,
	phaseApply: phaseReadiness, phaseReadiness: phaseCommitLive, phaseCommitLive: phaseStopWorker, phaseStopWorker: phaseRequestRetirement,
	phaseRequestRetirement: phaseReleaseLease,
}

// Wait limits of the Steps that wait for their provider, which the engine
// enforces through PollingStepOptions.
const (
	// prepareTemporalWaitLimit bounds namespace readiness and data-plane
	// convergence during execution provisioning.
	prepareTemporalWaitLimit = 20 * time.Minute
	prepareAccessWaitLimit   = 15 * time.Minute
	startDexWaitLimit        = 10 * time.Minute
	applicationWaitLimit     = 15 * time.Minute
	readinessWaitLimit       = 10 * time.Minute
	stopWorkerWaitLimit      = 12 * time.Minute
	// cleanupWaitLimit matches the cleanup wait limit of the deleting Project,
	// which admits cleanup again after a failure.
	cleanupWaitLimit = 30 * time.Minute
)

// branchWaitLimit is the wait limit of each branch of phasePrepare.
var branchWaitLimit = map[string]time.Duration{branchTemporal: prepareTemporalWaitLimit, branchAccess: prepareAccessWaitLimit}

// failureOwner is who must act on a failure of phase: the application, whose
// image does not run or become ready; the configuration, whose frozen
// revision no longer verifies; and the platform for every other phase.
func failureOwner(phase string) conversation.DeliveryOwner {
	switch phase {
	case phaseApply, phaseReadiness:
		return conversation.DeliveryOwnerApplication
	case phaseVerify:
		return conversation.DeliveryOwnerConfiguration
	}
	return conversation.DeliveryOwnerPlatform
}

// ownerRemedies is what the user can do about a deployment failure by its
// owner: ask the Agent to fix the application, fix Setup · Live, or deploy
// again.
var ownerRemedies = map[conversation.DeliveryOwner]dexflow.FailureRemedy{
	conversation.DeliveryOwnerApplication:   dexflow.RemedyAskAgent,
	conversation.DeliveryOwnerConfiguration: dexflow.RemedyOpenSetup,
	conversation.DeliveryOwnerPlatform:      dexflow.RemedyPublishAgain,
}

// FailureOwner is who must act on the deployment's failure, none without one.
func (s Snapshot) FailureOwner() conversation.DeliveryOwner {
	if s.Failure == nil {
		return conversation.DeliveryOwner{}
	}
	return failureOwner(s.Failure.Phase)
}

// deliveryFailure is the failure the deployment s committed at revision, as
// its Session is told it. Its ID names the generation of the Project lease
// the deployment holds, 0 before it acquired one, and that revision.
func deliveryFailure(s Snapshot, revision uint64, failure dexflow.Failure, at time.Time) releases.DeliveryFailure {
	return releases.DeliveryFailure{
		ID: fmt.Sprintf("deployment/%s/g%d/r%d", s.DeploymentID, s.Lease.Generation, revision), ProjectID: s.ProjectID, Source: releases.SourceDeployment,
		Owner: failureOwner(failure.Phase), Commit: s.ReleaseID, ResourceID: s.DeploymentID, Failure: failure, OccurredAt: at,
	}
}

// Snapshot is a deployment as read through GetProjectDeployment.
type Snapshot struct {
	BuildRevisionID                             string
	Identity                                    dexflow.StartIdentity
	ProjectID, DeploymentID, ReleaseID          string
	ConfigurationRevision, ExpectedLiveRevision uint64
	Status                                      Status
	Lease                                       projects.DeploymentLease
	Inputs                                      Inputs
	Grant                                       WorkloadGrant
	Resources                                   DeploymentResources
	Providers                                   ProviderRefs
	// Timings measure each phase that ran, its latest run only.
	Timings   []dexflow.PhaseTiming
	Readiness Readiness
	URL       string
	// CleanupRequested is the Project cleanup fence; it is never cleared.
	CleanupRequested bool
	// ReplacedLive is the Live generation this deployment replaces, which it
	// asks to retire once it committed; zero for the first Live.
	ReplacedLive ReplacedGeneration
	// Retirement is the request of the generation that replaced this one.
	Retirement Retirement
	// Cancellation is a person's admitted request to cancel the deployment;
	// nil when none was made.
	Cancellation         *DeploymentCancellation
	Failure              *dexflow.Failure
	Revision             uint64
	CreatedAt, UpdatedAt time.Time
}

// Start is the requested deployment of StartDeployment and automatic deployment.
type Start struct {
	BuildRevisionID                             string
	ProjectID, DeploymentID, ReleaseID          string
	ConfigurationRevision, ExpectedLiveRevision uint64
}

// ReplacedGeneration names one Live generation of a Project by its
// deployment and generation.
type ReplacedGeneration struct {
	DeploymentID string
	Generation   uint64
}

// Retirement is the admitted request to retire a replaced generation: the
// request, the generation that replaced it, when the request was admitted,
// which starts the drain, and when the generation was retired.
type Retirement struct {
	RequestID              string
	ReplacedBy             ReplacedGeneration
	RequestedAt, RetiredAt time.Time
}

// DeploymentCancellation is a person's admitted request to cancel the
// deployment. It holds no secret.
type DeploymentCancellation struct {
	// RequestID is the latest admitted request, which a failed cleanup admits
	// again.
	RequestID, ActorAccountID string
	RequestedAt               time.Time
	// Phase is ProviderRefs.Phase at the first admission.
	Phase string
	// Preempted is a cancellation that ended a running ApplyApplication, whose
	// last round may still act until preemptedRoundBound after RequestedAt.
	Preempted bool `json:",omitempty"`
}

// preemptedRoundBound bounds how long a preempted polling Step can still act:
// it stops at its next heartbeat, which the engine answers within its
// heartbeat timeout, after the round in progress (one interval and one round).
const preemptedRoundBound = time.Minute + convergenceInterval + convergenceRoundTimeout

// cancellationRequest asks to cancel a deployment.
type cancellationRequest struct {
	RequestID, ActorAccountID string
}

// cancelling reports a deployment whose Project's deletion or a person
// cancels it: every committed phase continues with its cleanup.
func (s Snapshot) cancelling() bool { return s.CleanupRequested || s.Cancellation != nil }

// cancellable reports a deployment that a person may cancel: in progress, or
// FAILED, in a phase before the Flow chooses to commit it as Live.
func (s Snapshot) cancellable() bool {
	switch s.Status {
	case Pending, Deploying, Recovering, Failed:
	default:
		return false
	}
	switch s.Providers.Phase {
	case phaseAdmit, phaseAcquire, phaseVerify, phasePublish, phasePrepare, phaseStartDex, phaseApply, phaseReadiness:
		return true
	}
	return false
}

// preemptsPolling reports a cancellation that ends the running Steps at once:
// a FAILED deployment waits, and the three phases that poll their provider
// act only on what cleanup removes.
func (s Snapshot) preemptsPolling() bool {
	return s.Status == Failed || s.Providers.Phase == phaseStartDex || s.Providers.Phase == phaseApply || s.Providers.Phase == phaseReadiness
}

// retirementRequest asks a replaced generation to retire.
type retirementRequest struct {
	RequestID  string
	ReplacedBy ReplacedGeneration
}

// mutation is an RPC request guarded by the expected deployment Revision.
type mutation struct {
	RequestID        string
	ExpectedRevision uint64
}

type mutationResult struct {
	Deployment Snapshot
	Rejection  *dexflow.Rejection
}

// Inputs are the immutable release and configuration a deployment pinned.
type Inputs struct {
	BuildRevisionID                           string
	ApplicationPort                           int
	HealthPath                                string
	ImageReference, ImageDigest, BundleDigest string
	Configuration                             releases.ReadyConfiguration
	Definitions, Manifest                     objectstore.ObjectRef
}

// WorkloadGrant identifies the generation's storage grant; it never holds the
// credential.
type WorkloadGrant struct {
	ID, ProjectID, DeploymentID       string
	Generation, ConfigurationRevision uint64
	Configuration                     objectstore.ObjectRef
	Revoked                           bool
}

// DeploymentResources are the project references a deployment runs on and its
// generation's Service. VerifyReleaseAndConfiguration pins the Project's
// storage, and its Live execution environment once complete; preparation and
// StartLiveDex record the execution references they observed, and activation
// the Live Worker endpoint it moved to the generation. The Project owns storage
// and execution; the deployment owns its Service.
type DeploymentResources struct {
	Storage   infra.StorageRefs
	Execution infra.ExecutionRefs
	Service   infra.ResourceRef
	Worker    infra.ResourceRef
}

// Readiness is the observed evidence that the generation serves the pinned
// image and definitions.
type Readiness struct {
	Ready                     bool
	ImageDigest, BundleDigest string
	CheckedAt                 time.Time
}

// DeploymentRequestReceipt records one accepted StartDeployment request; a
// FailedRevision of zero is the initial start or a join.
type DeploymentRequestReceipt struct {
	RequestID      string
	FailedRevision uint64
}

// ProviderRefs are the provider phase, its accepted application identity, the
// accepted request receipts and the number of the latest preparation round.
// The engine enforces the wait limit of each phase that waits for its
// provider, so no deadline is recorded.
type ProviderRefs struct {
	Requests         []DeploymentRequestReceipt
	Phase            string
	Application      infra.ResourceRef
	PreparationRound uint64
}

// advance records the provider references of a committed phase and returns
// the phase to run next. Once Project cleanup is requested, every phase
// continues with cleanup.
func (p *ProviderRefs) advance(in phaseResult, cleanupRequested bool) string {
	if in.Application != nil {
		p.Application = *in.Application
	}
	next := nextPhase[in.Phase]
	if in.Phase == phaseCleanup {
		next = phaseReleaseLease
	} else if cleanupRequested {
		next = phaseCleanup
	}
	p.Phase = next
	return next
}

// phaseResult carries the outcome of one phase to CommitDeploymentPhase. A
// Failure ends the phase, keeping what it recorded, for recovery.
type phaseResult struct {
	Phase string
	// StartedAt is when the phase began, a measurement that the commit ends.
	StartedAt   time.Time
	Lease       *projects.DeploymentLease
	Inputs      *Inputs
	Resources   *DeploymentResources
	Grant       *WorkloadGrant
	Application *infra.ResourceRef
	Readiness   *Readiness
	URL         string
	// Replaced is the Live generation that VerifyReleaseAndConfiguration found
	// this deployment replaces.
	Replaced *ReplacedGeneration
	// Timings are the measured branches of phasePrepare.
	Timings []dexflow.PhaseTiming
	Failure *dexflow.Failure
}

// recordTimings replaces each phase's entry with its newest measurement, so a
// phase that runs again is measured once.
func recordTimings(timings []dexflow.PhaseTiming, measured ...dexflow.PhaseTiming) []dexflow.PhaseTiming {
	for _, timing := range measured {
		timings = append(slices.DeleteFunc(timings, func(t dexflow.PhaseTiming) bool { return t.Phase == timing.Phase }), timing)
	}
	return timings
}

// measure is phase from startedAt until now.
func measure(phase string, startedAt time.Time) dexflow.PhaseTiming {
	return dexflow.EndPhase([]dexflow.PhaseTiming{{Phase: phase, StartedAt: startedAt}}, time.Now().UTC())[0]
}

// livePreparationInput is one round of phasePrepare. PrepareLiveExecution
// starts both branches and the join with it; Branch names the branch Step and
// is empty for the join. A branch's failure route receives the same input, so
// every branch publishes exactly one result under Key.
type livePreparationInput struct {
	Branch string
	Round  uint64
	// Key is the round's LivePreparationResults instance,
	// prepare-<generation>-<round>.
	Key       string
	StartedAt time.Time
}

// branch is the input of the round's branch Step name.
func (in livePreparationInput) branch(name string) livePreparationInput {
	in.Branch = name
	return in
}

// preparationKey is the LivePreparationResults instance of one round of a
// deployment generation.
func preparationKey(generation, round uint64) string {
	return fmt.Sprintf("prepare-%d-%d", generation, round)
}

// LivePreparationResult is one branch's outcome of a preparation round: the
// references it observed, also when it failed, Cancelled when the deployment
// lost the Project's lease, or Failure, the message of a failure that ends the
// round. Timing measures the branch.
type LivePreparationResult struct {
	Branch string
	Round  uint64
	// Execution is the Temporal branch's; Service and Grant are the access
	// branch's.
	Execution infra.ExecutionRefs
	Service   infra.ResourceRef
	Grant     WorkloadGrant
	Timing    dexflow.PhaseTiming
	Cancelled bool
	Failure   string
}

// MutationForTest and its result retain the production RPC wire types in integration fixtures.
type MutationForTest = mutation
type MutationResultForTest = mutationResult

// RetirementRequestForTest is the production retirement request in integration fixtures.
type RetirementRequestForTest = retirementRequest
