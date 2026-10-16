// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"cmp"
	"errors"
	"slices"
	"strings"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

// The values below are persisted in Attributes, map entries and Channel
// messages, so they must not change.

// Status is the business lifecycle of a Release, of its prepared source and of
// each build revision.
type Status = string

const (
	Pending    Status = "PENDING"
	Draft      Status = "DRAFT"
	Building   Status = "BUILDING"
	Ready      Status = "READY"
	Failed     Status = "FAILED"
	Recovering Status = "RECOVERING"
	Removing   Status = "REMOVING"
	Removed    Status = "REMOVED"
	// Cancelled is a build that a person cancelled, and a Release whose last
	// build it is.
	Cancelled Status = "CANCELLED"
)

// autoDeployStatus is the outcome of a build revision's automatic deployment,
// an alias of string as its readers store it; the constants declare the set.
type autoDeployStatus = string

const (
	autoDeployPending  autoDeployStatus = "PENDING"
	autoDeployAccepted autoDeployStatus = "ACCEPTED"
	autoDeployRejected autoDeployStatus = "REJECTED"
	autoDeployUnknown  autoDeployStatus = "UNKNOWN"
)

// Kinds of ReleaseBuildCommand.
const (
	commandPrepare = "PREPARE"
	commandBuild   = "BUILD"
	commandRemove  = "REMOVE"
)

// Phases of a build revision, persisted in buildOperation.Phase. Each names
// the Step that runs it; recovery resumes from it.
const (
	phaseFreeze  = "FreezeReleaseBuildConfiguration"
	phaseInspect = "InspectReleaseBuildRevision"
	phaseStart   = "StartReleaseBuildRevision"
	phaseAwait   = "AwaitReleaseBuildRevision"
	phasePublish = "PublishReleaseBuildRevision"
	phaseDeploy  = "DeployReleaseBuildRevision"
	// phaseCancel stops the provider build of an admitted cancellation.
	phaseCancel = "CancelReleaseBuildRevision"
)

// The build phases the cancellation tests wait for.
const (
	PhaseAwaitForTest   = phaseAwait
	PhasePublishForTest = phasePublish
)

// maxRequestReceipts bounds the request receipts kept per prepared source and
// per build revision.
const maxRequestReceipts = 128

// Phases of a build revision's Timings that the Flow measures. The
// provider's phases (build.Phase*) lie between them.
const (
	timingConfigurationFreeze = "CONFIGURATION_FREEZE"
	timingArtifactPublication = "ARTIFACT_PUBLICATION"
	// maxProviderTimings keeps a build revision's Timings within 16 entries.
	maxProviderTimings = 14
)

// Snapshot is a Release exactly as GetProjectRelease reads it. Its status,
// failure and recipe are the Release's own; a build revision keeps its own in
// ReleaseBuildRevision.
type Snapshot struct {
	Identity                                                                    dexflow.StartIdentity
	ProjectID, ReleaseID, SourceCommit, BuildProfileVersion, BuildProfileDigest string
	Repository                                                                  projects.RepositoryBindingRef
	Status                                                                      Status
	PreparedSource                                                              PreparedReleaseSource
	ActiveBuildRevisionID, ReadyBuildRevisionID                                 string
	CancellationRequested                                                       bool
	Failure                                                                     *dexflow.Failure
	Revision                                                                    uint64
	CreatedAt, UpdatedAt                                                        time.Time
}

// BuildSnapshot is a Release with one of its build revisions.
type BuildSnapshot struct {
	Release Snapshot
	Build   ReleaseBuildRevision
}

// releaseStart is the accepted input of PrepareRelease.
type releaseStart struct {
	identity                                                         dexflow.StartIdentity
	projectID, sourceCommit, buildProfileVersion, buildProfileDigest string
	repository                                                       projects.RepositoryBindingRef
}

// mutation is an RPC request guarded by the expected Release Revision.
type mutation struct {
	RequestID        string
	ExpectedRevision uint64
}

type mutationResult struct {
	Release   Snapshot
	Rejection *dexflow.Rejection
}

// PreparedReleaseSource is the exact source preparation of the commit: its
// manifest, immutable artifacts and definition revision. A source an earlier
// version prepared also holds the seed of its per-version draft, which
// decoding ignores.
type PreparedReleaseSource struct {
	Status                  Status
	RequestID, SourceCommit string
	AppManifest             *pb.AppManifestSnapshot
	Artifacts               ArtifactRefs
	DefinitionRevision      string
	Failure                 *dexflow.Failure
	// FailureOwner is who must act on Failure. The Step that decides the
	// failure sets it on its input, and it is committed only with the
	// failure, like Status.
	FailureOwner conversation.DeliveryOwner `json:",omitzero"`
	Requests     []string
	// Timings are the phases the provider measured while it prepared the
	// source.
	Timings []dexflow.PhaseTiming
	// Version is the commit's number on main and its subject, computed once
	// when the source was prepared; zero for a source prepared before it was
	// recorded.
	Version repohost.CommitVersion `json:",omitzero"`
}

type ArtifactRefs struct{ FlowDefinitions, ConnectorContract, EnvironmentContract, Manifest objectstore.ObjectRef }

type provenance struct{ Provider, BuildID, SourceCommit, BuildProfileDigest, ImageReference, ImageDigest string }

// buildOperation is the provider operation of a build revision.
type buildOperation struct {
	Requests  []BuildRequestReceipt
	ID, Phase string
	Attempt   uint64
}

// BuildRequestReceipt records one accepted build or recovery request.
type BuildRequestReceipt struct {
	RequestID      string
	FailedRevision uint64
}

// ReleaseBuildRevision is one accepted Save and Build, not a new Flow
// execution. Payloads and credentials stay outside this bounded map value.
type ReleaseBuildRevision struct {
	BuildRevisionID, ActorAccountID, Fingerprint string
	ConfigurationRevision                        uint64
	Configuration                                *ReadyConfiguration
	BuildProfileVersion, BuildProfileDigest      string
	Status                                       Status
	Revision                                     uint64
	Operation                                    buildOperation
	ImageReference, ImageDigest                  string
	Provenance                                   provenance
	Artifacts                                    ArtifactRefs
	Failure                                      *dexflow.Failure
	// ProviderSettled permits a new build only after this job cannot still act.
	ProviderSettled      bool
	AutoDeploy           bool
	ExpectedLiveRevision uint64
	AutoDeploymentID     string
	AutoDeployStatus     autoDeployStatus
	AutoDeployFailure    *dexflow.Failure
	// FailureOwner is who must act on Failure or AutoDeployFailure, set with
	// the failure.
	FailureOwner conversation.DeliveryOwner `json:",omitzero"`
	// Timings are the measured phases of the build: configuration freezing,
	// the provider's phases of its last observation, then artifact
	// publication.
	Timings              []dexflow.PhaseTiming
	CreatedAt, UpdatedAt time.Time
	// PreviewValidation is how the build met the publishing rule, decided at
	// admission and never changed; nil for a build admitted before it was
	// recorded.
	PreviewValidation *PreviewValidation `json:",omitempty"`
	// Cancellation is a person's admitted request to cancel the build; nil
	// when none was made.
	Cancellation *BuildCancellation `json:",omitempty"`
}

// BuildCancellation is a person's admitted request to cancel the build; it is
// set once. It holds no secret.
type BuildCancellation struct {
	RequestID, ActorAccountID string
	RequestedAt               time.Time
	// Phase is Operation.Phase when the request was admitted.
	Phase string
	// SettledAt is when the build ended CANCELLED; zero while it is being
	// cancelled.
	SettledAt time.Time `json:",omitzero"`
}

// PreviewValidationKind is how a build met the publishing rule. A struct that
// string literals cannot convert to: the values below are its only ones,
// persisted by their names.
type PreviewValidationKind struct{ name string }

var (
	// PreviewValidated is a build of a commit that a READY Preview start ran.
	PreviewValidated = PreviewValidationKind{"VALIDATED"}
	// PreviewSkipped is a build whose builder wrote why it skips the Preview.
	PreviewSkipped = PreviewValidationKind{"SKIPPED"}
	// PreviewLiveCode is a configuration-only build of the Release Live serves.
	PreviewLiveCode = PreviewValidationKind{"LIVE_CODE"}
)

func (kind PreviewValidationKind) String() string               { return kind.name }
func (kind PreviewValidationKind) MarshalText() ([]byte, error) { return []byte(kind.name), nil }
func (kind *PreviewValidationKind) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, kind, PreviewValidationKind.String, PreviewValidated, PreviewSkipped, PreviewLiveCode)
}

// PreviewValidation is how a build met the publishing rule: a READY Preview
// start of the commit, the builder's written reason for skipping it, or the
// code Live already runs. It holds no secret.
type PreviewValidation struct {
	Kind PreviewValidationKind
	// StartCommandID and StartedAt name the RecentStarts entry of a VALIDATED
	// build and when it ended.
	StartCommandID string    `json:",omitempty"`
	StartedAt      time.Time `json:",omitzero"`
	// SkipReason is a SKIPPED build's reason, 1 to 500 code points, trimmed.
	SkipReason     string `json:",omitempty"`
	ActorAccountID string
}

// MaximumSkipReason bounds a skip reason, in code points; the API refuses a
// longer one.
const MaximumSkipReason = 500

// advance records the result of one build phase and returns the phase to run
// next, or "" when the build waits for a command.
func (r *ReleaseBuildRevision) advance(in releaseBuildPhaseResult, now time.Time) (string, error) {
	next := ""
	switch {
	case in.Phase == phaseCancel:
		r.settleCancellation(in, now)
	case in.Configuration != nil:
		r.Configuration = in.Configuration
		r.Timings = dexflow.EndPhase([]dexflow.PhaseTiming{{Phase: timingConfigurationFreeze, StartedAt: in.StartedAt}}, now)
		next = phaseInspect
	case in.Observation != nil:
		var err error
		if next, err = r.observe(*in.Observation); err != nil {
			return "", err
		}
	case in.Artifacts != nil:
		r.Artifacts = *in.Artifacts
		r.Timings = dexflow.EndPhase(append(r.Timings, dexflow.PhaseTiming{Phase: timingArtifactPublication, StartedAt: in.StartedAt}), now)
		r.Status = Ready
		r.Failure, r.FailureOwner = nil, conversation.DeliveryOwner{}
		r.ProviderSettled = true
		if r.AutoDeploy {
			r.AutoDeployStatus = autoDeployPending
			next = phaseDeploy
		}
	case in.Phase == phaseDeploy:
		r.Status = Ready
		r.AutoDeploymentID = in.AutoDeploymentID
		r.AutoDeployFailure = in.AutoDeployFailure
		r.AutoDeployStatus = autoDeployAccepted
		if in.AutoDeployFailure != nil {
			r.AutoDeployStatus, r.FailureOwner = autoDeployRejected, conversation.DeliveryOwnerPlatform
		}
	}
	if next != "" {
		r.Operation.Phase = next
		if r.Status != Ready {
			r.Status = Building
		}
	}
	r.Revision++
	r.UpdatedAt = now
	return next, nil
}

// settleCancellation ends the build CANCELLED: settled unless the provider
// could not confirm that it stopped, with the provider's last phases.
func (r *ReleaseBuildRevision) settleCancellation(in releaseBuildPhaseResult, now time.Time) {
	r.Status, r.ProviderSettled = Cancelled, in.Unconfirmed == ""
	r.Failure, r.FailureOwner = nil, conversation.DeliveryOwner{}
	if in.Unconfirmed != "" {
		r.Failure, r.FailureOwner = failureCancelUnconfirmed.failure(phaseCancel, in.Unconfirmed), failureCancelUnconfirmed.owner
	}
	if in.Observation != nil {
		r.Timings = slices.DeleteFunc(r.Timings, func(t dexflow.PhaseTiming) bool { return t.Phase != timingConfigurationFreeze })
		r.Timings = append(r.Timings, in.Observation.Timings...)
	}
	r.Cancellation.SettledAt = now
}

// observe records a provider observation of the build and returns the next
// phase. A failed or unknown build stops for an explicit recovery.
func (r *ReleaseBuildRevision) observe(o buildObservation) (string, error) {
	r.Operation.ID = o.Operation.ID
	// The observation's phases replace the provider's earlier ones after the
	// configuration freezing.
	r.Timings = slices.DeleteFunc(r.Timings, func(t dexflow.PhaseTiming) bool { return t.Phase != timingConfigurationFreeze })
	r.Timings = append(r.Timings, o.Timings...)
	switch o.State {
	case build.BuildMissing:
		r.ProviderSettled = false
		return phaseStart, nil
	case build.BuildRunning:
		r.ProviderSettled = false
		return phaseAwait, nil
	case build.BuildReady:
		if o.ImageDigest == "" || o.ImageReference == "" {
			return "", errors.New("ready release build has no immutable image")
		}
		r.ImageReference = o.ImageReference
		r.ImageDigest = o.ImageDigest
		r.Provenance = o.Provenance
		r.ProviderSettled = true
		return phasePublish, nil
	case build.BuildFailed, build.BuildUnknown:
		r.Status = Failed
		r.ProviderSettled = o.State == build.BuildFailed
		kind, message := failureBuildUnknown, "The build provider cannot confirm the outcome of the build; inspect it before a retry."
		if o.State == build.BuildFailed {
			kind, message = failureBuildFailed, "The build failed: "+cmp.Or(o.Failure, "the build provider gave no reason")+"."
			if o.FailureCategory.Class() == build.FailureClassPlatform {
				kind = failureBuildPlatform
			}
		}
		r.Failure, r.FailureOwner = kind.failure(r.Operation.Phase, message), kind.owner
		return "", nil
	default:
		return "", errors.New("invalid release build observation")
	}
}

// committedFailure is the failure that the commit of phase result in recorded
// for the Session, with its owner, or nil: a build the provider reported
// FAILED, which the application owns; an UNKNOWN build and a rejected
// automatic deployment, which leaves the build READY, which the platform owns.
func (r ReleaseBuildRevision) committedFailure(in releaseBuildPhaseResult) (*dexflow.Failure, conversation.DeliveryOwner) {
	switch {
	case in.Phase == phaseCancel:
		// A person cancelled the build; Publishing shows how it ended.
		return nil, conversation.DeliveryOwner{}
	case in.Observation != nil && (in.Observation.State == build.BuildFailed || in.Observation.State == build.BuildUnknown):
		return r.Failure, r.FailureOwner
	case in.AutoDeployFailure != nil:
		return r.AutoDeployFailure, r.FailureOwner
	}
	return nil, conversation.DeliveryOwner{}
}

// fail records a phase that ended without a result, for the reason given. A
// deployment failure keeps the build READY with an unknown deployment outcome.
func (r *ReleaseBuildRevision) fail(kind releaseFailureKind, reason string, now time.Time) *dexflow.Failure {
	failure := kind.failure(r.Operation.Phase, reason)
	r.FailureOwner = kind.owner
	if r.Operation.Phase == phaseDeploy {
		r.Status = Ready
		r.AutoDeployStatus = autoDeployUnknown
		r.AutoDeployFailure = failure
	} else {
		r.Status = Failed
		r.Failure = failure
	}
	// Nothing reached the provider before the configuration was frozen.
	if r.Configuration == nil {
		r.ProviderSettled = true
	}
	r.Revision++
	r.UpdatedAt = now
	return failure
}

// recoverable reports whether an explicit recovery may resume this build: it
// failed, its automatic deployment outcome is unknown, or the provider did
// not confirm that its cancellation stopped it.
func (r *ReleaseBuildRevision) recoverable() bool {
	return r.Status == Failed || r.Status == Ready && r.AutoDeployStatus == autoDeployUnknown || r.Status == Cancelled && !r.ProviderSettled
}

// supersedable reports whether a new build may replace this active one: it
// ended FAILED or CANCELLED and its provider job cannot still act.
func (r *ReleaseBuildRevision) supersedable() bool {
	return (r.Status == Failed || r.Status == Cancelled) && r.ProviderSettled
}

// admitRecovery resumes the build from the phase that can safely run again:
// configuration freezing, inspection of the provider build with a new
// attempt, or the idempotent automatic deployment.
func (r *ReleaseBuildRevision) admitRecovery(requestID string, now time.Time) {
	r.Operation.Requests = append(r.Operation.Requests, BuildRequestReceipt{RequestID: requestID, FailedRevision: r.Revision})
	switch {
	case r.Status == Cancelled:
		// The provider is asked again under the same attempt.
		r.Operation.Phase = phaseCancel
	case r.Configuration == nil:
		r.Operation.Phase = phaseFreeze
	case r.Operation.Phase != phaseDeploy:
		r.Operation.Phase = phaseInspect
		r.Operation.Attempt++
	}
	r.Status = Recovering
	r.Failure, r.FailureOwner = nil, conversation.DeliveryOwner{}
	r.Revision++
	r.UpdatedAt = now
}

// cancellationRefused is the refusal of a cancellation of record, in the
// admission order of RequestReleaseBuildCancellation, or nil when it is
// admitted or was admitted before.
func cancellationRefused(s Snapshot, r ReleaseBuildRevision) *buildRevisionResult {
	refuse := func(refusal cancellationRefusal, deploymentID, message string) *buildRevisionResult {
		return &buildRevisionResult{BuildRevisionID: r.BuildRevisionID, Rejection: dexflow.Reject(dexflow.RejectionFailedPrecondition, message), Refusal: refusal, DeploymentID: deploymentID}
	}
	switch {
	case r.Cancellation != nil:
		return nil
	case s.CancellationRequested:
		return refuse("", "", "the Project is being deleted, and its cleanup cancels the build")
	case r.AutoDeploy && r.Status == Ready || r.Operation.Phase == phaseDeploy:
		deployment := strings.TrimPrefix(r.BuildRevisionID, "build-")
		return refuse(refusalDeploying, deployment, "the build is published and starts its deployment "+deployment+"; cancel that deployment instead")
	case s.ActiveBuildRevisionID != r.BuildRevisionID || r.Status != Pending && r.Status != Building && r.Status != Recovering:
		return refuse(refusalNotCancellable, "", "the build is not in progress")
	}
	return nil
}

// BuildRevisionRequest admits one Save and Build.
type BuildRevisionRequest struct {
	RequestID, BuildRevisionID              string
	ActorAccountID                          string
	ExpectedRevision, ConfigurationRevision uint64
	AutoDeploy                              bool
	ExpectedLiveRevision                    uint64
	// PreviousBuildRevisionID selects the one active record needed for admission.
	PreviousBuildRevisionID string
	// PreviewSkipReason is the builder's trimmed reason to skip the Preview
	// validation, empty when none was given; it is part of the fingerprint.
	PreviewSkipReason string
	// PreviewValidation is what admission decided for a new build; a replay
	// leaves it nil and keeps the accepted one.
	PreviewValidation *PreviewValidation
}

type BuildRevisionRecovery struct {
	BuildRevisionID, RequestID, ActorAccountID string
	ExpectedRevision                           uint64
}

// BuildRevisionCancellation asks to cancel one build revision.
type BuildRevisionCancellation struct {
	BuildRevisionID, RequestID, ActorAccountID string
}

type buildRevisionResult struct {
	BuildRevisionID string
	Rejection       *dexflow.Rejection
	// Refusal is why a cancellation was refused, for the API's reasons, and
	// DeploymentID the deployment a deploying build started; empty otherwise.
	Refusal      cancellationRefusal `json:",omitempty"`
	DeploymentID string              `json:",omitempty"`
}

// cancellationRefusal is why RequestReleaseBuildCancellation refused a build
// that exists, outside Project deletion; an alias of string as the RPC result
// stores it, the constants declare the set.
type cancellationRefusal = string

const (
	// refusalNotCancellable is a build that is not active or already ended.
	refusalNotCancellable cancellationRefusal = "NOT_CANCELLABLE"
	// refusalDeploying is a build whose artifacts committed, so its automatic
	// deployment is or may be admitted.
	refusalDeploying cancellationRefusal = "DEPLOYING"
)

// ReleaseBuildCommand wakes AwaitReleaseBuildCommands for state already
// accepted under the Revision lock; it is not a deduplication record.
type ReleaseBuildCommand struct {
	Kind, RequestID, BuildRevisionID string
}

// releaseBuildStepInput addresses the build revision a build Step acts on;
// cleanup leaves it empty when no build is active. Gaps are set only by
// FreezeReleaseBuildConfiguration when it refuses a draft that lacks what the
// commit declares, for RecordReleaseBuildFailure.
type releaseBuildStepInput struct {
	BuildRevisionID string
	Gaps            []appmanifest.RequirementGap `json:",omitempty"`
}

// releaseBuildPhaseResult carries the outcome of one build phase to
// CommitReleaseBuildRevision. StartedAt is when the Step that produced it
// first started, which times configuration freezing and artifact
// publication; it is a measurement, never a deadline. Unconfirmed is why the
// provider could not confirm that a cancelled build stopped, for phaseCancel.
type releaseBuildPhaseResult struct {
	Input             releaseBuildStepInput
	Phase             string
	StartedAt         time.Time
	Observation       *buildObservation
	Configuration     *ReadyConfiguration
	Artifacts         *ArtifactRefs
	AutoDeploymentID  string
	AutoDeployFailure *dexflow.Failure
	Unconfirmed       string `json:",omitempty"`
}

// buildObservation is what one provider call observed of a build. Failure is
// the provider's bounded reason for a FAILED build; Timings are the phases
// the provider measured.
type buildObservation struct {
	State                       build.BuildState
	Operation                   buildOperation
	ImageReference, ImageDigest string
	Provenance                  provenance
	Failure                     string
	FailureCategory             build.FailureCategory
	Timings                     []dexflow.PhaseTiming
}

// ReadyConfiguration is the frozen Live configuration a Release build pins and
// its Deployment serves.
type ReadyConfiguration struct {
	Object                                                             objectstore.ObjectRef
	ReleaseID                                                          string
	ConfigurationRevision                                              uint64
	AppManifestRevision                                                uint64
	ManifestDigest, ConnectorContractDigest, EnvironmentContractDigest string
}
