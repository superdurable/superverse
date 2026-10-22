// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

var (
	StartIdentity = dex.DefineAttribute[dexflow.StartIdentity]("StartIdentity")
	// dex:indexed-attribute attribute-key:ProjectID index-key:keyword2 index-type:keyword value-type:string description:"Owning project"
	ProjectID           = dex.DefineAttribute[string]("ProjectID", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword2"}))
	ReleaseID           = dex.DefineAttribute[string]("ReleaseID")
	SourceCommit        = dex.DefineAttribute[string]("SourceCommit", dex.SyncToAttributeStore())
	BuildProfileVersion = dex.DefineAttribute[string]("BuildProfileVersion", dex.SyncToAttributeStore())
	BuildProfileDigest  = dex.DefineAttribute[string]("BuildProfileDigest")
	RepositoryBinding   = dex.DefineAttribute[projects.RepositoryBindingRef]("RepositoryBinding")
	// dex:indexed-attribute attribute-key:ReleaseStatus index-key:keyword3 index-type:keyword value-type:string description:"Business lifecycle status"
	ReleaseStatus         = dex.DefineAttribute[string]("ReleaseStatus", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword3"}), dex.SyncToAttributeStore())
	PreparedSource        = dex.DefineAttribute[PreparedReleaseSource]("PreparedSource", dex.SyncToAttributeStore())
	ActiveBuildRevisionID = dex.DefineAttribute[string]("ActiveBuildRevisionID", dex.SyncToAttributeStore())
	ReadyBuildRevisionID  = dex.DefineAttribute[string]("ReadyBuildRevisionID", dex.SyncToAttributeStore())
	ReleaseBuildRevisions = dex.DefineAttributeMap[ReleaseBuildRevision]("ReleaseBuildRevisions", dex.SyncToAttributeStore())
	CancellationRequested = dex.DefineAttribute[bool]("CancellationRequested")
	FailureAttribute      = dex.DefineAttribute[*dexflow.Failure]("Failure", dex.SyncToAttributeStore())
	Revision              = dex.DefineAttribute[uint64]("Revision", dex.SyncToAttributeStore())
	CreatedAt             = dex.DefineAttribute[time.Time]("CreatedAt", dex.SyncToAttributeStore())
	UpdatedAt             = dex.DefineAttribute[time.Time]("UpdatedAt", dex.SyncToAttributeStore())
	ReleaseBuildCommands  = dex.DefineChannel[ReleaseBuildCommand]("ReleaseBuildCommands")
	BuildProgress         = dex.DefineStream[dexflow.ProgressEvent]("BuildProgress", 10<<20)
)

// ProjectReleaseFlow owns one main-branch commit of a Project: its prepared
// source, its immutable build revisions and their cleanup.
type ProjectReleaseFlow struct {
	dex.FlowDefaults
	projects *projects.ProjectFlowClient
	host     repohost.Host
	// builder is nil in Local Kind without a build token, which leaves
	// Releases unavailable.
	builder     build.Builder
	profiles    *build.Profiles
	objects     *objectstore.Store
	environment deploymentnames.Environment
	publishing  ProjectReleaseFlowDependency
	feedback    DeliveryFeedback
}

// ProjectReleaseFlowDependency supplies the effects that configuration and Live deployment own,
// never handing credentials or mutable configuration values to the Flow.
type ProjectReleaseFlowDependency interface {
	// RegisterReleaseSource registers the prepared definitions with the configuration owner.
	RegisterReleaseSource(ctx context.Context, project projects.Project, source PreparedReleaseSource, definitions []appmanifest.FlowDefinition) (PreparedReleaseSource, error)
	// FreezeReleaseConfiguration freezes the build's accepted draft revision; a draft lacking
	// what the commit declares is refused with every gap, by name only, and no configuration.
	FreezeReleaseConfiguration(ctx context.Context, s Snapshot, build ReleaseBuildRevision) (ReadyConfiguration, []appmanifest.RequirementGap, error)
	DeployReleaseBuild(ctx context.Context, s Snapshot, build ReleaseBuildRevision) (string, error)
	// CheckReleaseDraft refuses a stale frozen copy of s's version, or one past configurationRevision;
	// gaps stay the freeze's (README, Build admission).
	CheckReleaseDraft(ctx context.Context, actor string, s Snapshot, configurationRevision uint64) error
}

// PreviewValidationReader reads the Preview starts of a Project for build admission. The Session Service
// implements it; the Release client never reads the Preview's Flow itself.
type PreviewValidationReader interface {
	// ReadPreviewValidation returns the VALIDATED record of the newest READY Preview start of commit in the
	// Preview's RecentStarts, or nil when none is recorded.
	ReadPreviewValidation(ctx context.Context, projectID, commit string) (*PreviewValidation, error)
}

func NewProjectReleaseFlow(projectClient *projects.ProjectFlowClient, host repohost.Host, builder build.Builder, profiles *build.Profiles, objects *objectstore.Store, environment deploymentnames.Environment, publishing ProjectReleaseFlowDependency, feedback DeliveryFeedback) *ProjectReleaseFlow {
	return &ProjectReleaseFlow{projects: projectClient, host: host, builder: builder, profiles: profiles, objects: objects, environment: environment, publishing: publishing, feedback: feedback}
}

func (f *ProjectReleaseFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(AdmitRelease{flow: f}), dex.DefineStep(RecordReleaseFailure{flow: f}),
		dex.DefineStep(PrepareReleasePublishingSource{flow: f}), dex.DefineStep(CommitReleasePublishingSource{flow: f}),
		dex.DefineStep(AwaitReleaseBuildCommands{flow: f}), dex.DefineStep(ResumeReleaseBuildRevision{flow: f}),
		dex.DefineStep(FreezeReleaseBuildConfiguration{flow: f}), dex.DefineStep(InspectReleaseBuildRevision{flow: f}),
		dex.DefineStep(StartReleaseBuildRevision{flow: f}), dex.DefineStep(AwaitReleaseBuildRevision{flow: f}),
		dex.DefineStep(PublishReleaseBuildRevision{flow: f}), dex.DefineStep(DeployReleaseBuildRevision{flow: f}),
		dex.DefineStep(CommitReleaseBuildRevision{flow: f}), dex.DefineStep(RecordReleaseBuildFailure{flow: f}),
		dex.DefineStep(CleanupReleasePublishing{flow: f}), dex.DefineStep(CompleteReleasePublishing{flow: f}),
		dex.DefineStep(RecordReleasePublishingCleanupFailure{flow: f}), dex.DefineStep(NotifyReleaseFailure{flow: f}),
		dex.DefineStep(AbandonReleaseFailureNotification{flow: f}), dex.DefineStep(CancelReleaseBuildRevision{flow: f}),
	}
}

func (f *ProjectReleaseFlow) GetRPCs() []dex.RPCDef {
	read := dexflow.ShortRPC()
	write := dexflow.ShortRPC(dex.LockAttribute(Revision))
	return []dex.RPCDef{
		dex.DefineRPC(f.GetProjectRelease, read), dex.DefineRPC(f.GetReleaseBuildRevision, read),
		dex.DefineRPC(f.GetDexSummary, read), dex.DefineRPC(f.GetDexDisplay, read),
		dex.DefineRPC(f.RequestReleaseSourcePreparation, write), dex.DefineRPC(f.RequestReleaseBuildRevision, write),
		dex.DefineRPC(f.RequestReleaseBuildRecovery, write), dex.DefineRPC(f.RequestReleaseRemoval, write),
		dex.DefineRPC(f.RequestReleaseBuildCancellation, write),
	}
}

func (f *ProjectReleaseFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{StartIdentity, ProjectID, ReleaseID, SourceCommit, BuildProfileVersion, BuildProfileDigest, RepositoryBinding, ReleaseStatus, PreparedSource, ActiveBuildRevisionID, ReadyBuildRevisionID, ReleaseBuildRevisions, CancellationRequested, FailureAttribute, Revision, CreatedAt, UpdatedAt}, Channels: []dex.ChannelDef{ReleaseBuildCommands}, Streams: []dex.StreamDef{BuildProgress}}
}

// initialAttributes describes the accepted per-commit Release. The caller has
// validated the commit and Project that form its FlowID.
func initialAttributes(in releaseStart) []dex.InitialAttributeDef {
	now := time.Now().UTC()
	b := &dexflow.InitialAttributes{}
	dexflow.Add(b, StartIdentity, in.identity)
	dexflow.Add(b, ProjectID, in.projectID)
	dexflow.Add(b, ReleaseID, in.sourceCommit)
	dexflow.Add(b, SourceCommit, in.sourceCommit)
	dexflow.Add(b, BuildProfileVersion, in.buildProfileVersion)
	dexflow.Add(b, BuildProfileDigest, in.buildProfileDigest)
	dexflow.Add(b, RepositoryBinding, in.repository)
	dexflow.Add(b, ReleaseStatus, Pending)
	dexflow.Add(b, PreparedSource, PreparedReleaseSource{Status: Pending, RequestID: in.identity.RequestID, SourceCommit: in.sourceCommit, Requests: []string{in.identity.RequestID}})
	dexflow.Add(b, ActiveBuildRevisionID, "")
	dexflow.Add(b, ReadyBuildRevisionID, "")
	dexflow.Add(b, CancellationRequested, false)
	dexflow.Add(b, FailureAttribute, nil)
	dexflow.Add(b, Revision, uint64(1))
	dexflow.Add(b, CreatedAt, now)
	dexflow.Add(b, UpdatedAt, now)
	return b.Values()
}

func readSnapshot(ctx dex.Context) Snapshot {
	return comm.Must((&ProjectReleaseFlow{}).GetProjectRelease(ctx, nil)).Output
}

// bumpRevision records a committed change for Revision CAS and readers and
// returns the Revision of the commit.
func bumpRevision(ctx dex.Context) uint64 {
	revision := comm.Must(Revision.Get(ctx)) + 1
	comm.MustDo(Revision.Set(ctx, revision))
	comm.MustDo(UpdatedAt.Set(ctx, time.Now().UTC()))
	return revision
}

// releaseBuildOptions run a build Step on its one map entry; failures reach RecordReleaseBuildFailure with it
// loaded and locked. Dex merges these over the Step's own options, which keep its timeouts and retries.
func releaseBuildOptions(f *ProjectReleaseFlow, id string) *dex.StepOptions {
	return &dex.StepOptions{
		ExecuteLoadAttributeMapInstances: []dex.AttributeMapLoad{ReleaseBuildRevisions.Load(id)},
		ExecuteFailure:                   dex.ProceedToOnExecuteFailure(RecordReleaseBuildFailure{flow: f}, releaseBuildCommitOptions(id)),
	}
}

// releaseBuildCommitOptions commit to one map entry under the Revision lock.
func releaseBuildCommitOptions(id string) *dex.StepOptions {
	return &dex.StepOptions{
		ExecuteLoadAttributeMapInstances: []dex.AttributeMapLoad{ReleaseBuildRevisions.Load(id)},
		ExecuteLockAttributes:            []dex.AttributeLock{dex.LockAttribute(Revision), dex.LockAttributeMap(ReleaseBuildRevisions, id)},
	}
}

// releaseCleanupLoad loads the active build, if any, into cleanup.
func releaseCleanupLoad(id string) *dex.StepOptions {
	if id == "" {
		return nil
	}
	return &dex.StepOptions{ExecuteLoadAttributeMapInstances: []dex.AttributeMapLoad{ReleaseBuildRevisions.Load(id)}}
}

func lockRevision(o *dex.StepOptions) *dex.StepOptions {
	o.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(Revision)}
	return o
}

func (*ProjectReleaseFlow) GetProjectRelease(ctx dex.Context, _ dex.None) (*dex.RPCResult[Snapshot], error) {
	s := Snapshot{
		Identity:              comm.Must(StartIdentity.Get(ctx)),
		ProjectID:             comm.Must(ProjectID.Get(ctx)),
		ReleaseID:             comm.Must(ReleaseID.Get(ctx)),
		SourceCommit:          comm.Must(SourceCommit.Get(ctx)),
		BuildProfileVersion:   comm.Must(BuildProfileVersion.Get(ctx)),
		BuildProfileDigest:    comm.Must(BuildProfileDigest.Get(ctx)),
		Repository:            comm.Must(RepositoryBinding.Get(ctx)),
		Status:                comm.Must(ReleaseStatus.Get(ctx)),
		PreparedSource:        comm.Must(PreparedSource.Get(ctx)),
		ActiveBuildRevisionID: comm.Must(ActiveBuildRevisionID.Get(ctx)),
		ReadyBuildRevisionID:  comm.Must(ReadyBuildRevisionID.Get(ctx)),
		CancellationRequested: comm.Must(CancellationRequested.Get(ctx)),
		Failure:               dexflow.Optional(FailureAttribute.Get(ctx)),
		Revision:              comm.Must(Revision.Get(ctx)),
		CreatedAt:             comm.Must(CreatedAt.Get(ctx)),
		UpdatedAt:             comm.Must(UpdatedAt.Get(ctx)),
	}
	return &dex.RPCResult[Snapshot]{Output: s}, nil
}

// GetReleaseBuildRevision reads one exact map entry; a zero value means absent.
func (*ProjectReleaseFlow) GetReleaseBuildRevision(ctx dex.Context, id string) (*dex.RPCResult[ReleaseBuildRevision], error) {
	record, err := ReleaseBuildRevisions.Get(ctx, id)
	if err != nil && !dexflow.IsAttributeNotFound(err) {
		return nil, err
	}
	return &dex.RPCResult[ReleaseBuildRevision]{Output: record}, nil
}

// dex:field attribute-key:ReleaseID value-type:string editable:false description:"Immutable lifecycle identity"
func (*ProjectReleaseFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ReleaseID": comm.Must(ReleaseID.Get(ctx))}}, nil
}

// dex:field attribute-key:ReadyBuildRevisionID value-type:string editable:false description:"Latest successful build revision"
func (*ProjectReleaseFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ReadyBuildRevisionID": comm.Must(ReadyBuildRevisionID.Get(ctx))}}, nil
}

func releaseResult(ctx dex.Context, rejection *dexflow.Rejection) *dex.RPCResult[mutationResult] {
	return &dex.RPCResult[mutationResult]{Output: mutationResult{Release: readSnapshot(ctx), Rejection: rejection}}
}

// RequestReleaseSourcePreparation admits an explicit retry of failed source
// preparation under a new request identity and the current Revision.
func (*ProjectReleaseFlow) RequestReleaseSourcePreparation(ctx dex.Context, in mutation) (*dex.RPCResult[mutationResult], error) {
	s := readSnapshot(ctx)
	source := s.PreparedSource
	if source.Status == Ready || slices.Contains(source.Requests, in.RequestID) {
		return releaseResult(ctx, nil), nil
	}
	if source.Status != Failed || in.ExpectedRevision != s.Revision || s.CancellationRequested {
		return releaseResult(ctx, dexflow.RejectStale("source preparation revision changed")), nil
	}
	if len(source.Requests) >= maxRequestReceipts {
		return releaseResult(ctx, dexflow.Reject(dexflow.RejectionResourceExhausted, "source preparation receipt capacity is unavailable")), nil
	}
	source.Requests = append(source.Requests, in.RequestID)
	source.Status, source.Failure, source.FailureOwner = Pending, nil, conversation.DeliveryOwner{}
	comm.MustDo(PreparedSource.Set(ctx, source))
	comm.MustDo(ReleaseBuildCommands.Publish(ctx, ReleaseBuildCommand{Kind: commandPrepare, RequestID: in.RequestID}))
	bumpRevision(ctx)
	return releaseResult(ctx, nil), nil
}

func rejectBuildRevision(id string, code dexflow.RejectionCode, message string) *dex.RPCResult[buildRevisionResult] {
	return &dex.RPCResult[buildRevisionResult]{Output: buildRevisionResult{BuildRevisionID: id, Rejection: dexflow.Reject(code, message)}}
}

func staleBuildRevision(id, message string) *dex.RPCResult[buildRevisionResult] {
	return &dex.RPCResult[buildRevisionResult]{Output: buildRevisionResult{BuildRevisionID: id, Rejection: dexflow.RejectStale(message)}}
}

func acceptBuildRevision(id string) *dex.RPCResult[buildRevisionResult] {
	return &dex.RPCResult[buildRevisionResult]{Output: buildRevisionResult{BuildRevisionID: id}}
}

// buildFingerprint hashes the complete input of one Save and Build in this order, so accepted fingerprints stay
// comparable; an empty skip reason hashes as before it existed, and the computed validation is not input.
func buildFingerprint(s Snapshot, in BuildRevisionRequest, profile build.Profile) string {
	return flowid.Fingerprint(struct {
		ProjectID, ReleaseID, SourceCommit, RequestID, BuildRevisionID, ActorAccountID string
		ExpectedRevision, ConfigurationRevision                                        uint64
		BuildProfileVersion, BuildProfileDigest                                        string
		AutoDeploy                                                                     bool
		ExpectedLiveRevision                                                           uint64
		PreviewSkipReason                                                              string `json:",omitempty"`
	}{s.ProjectID, s.ReleaseID, s.SourceCommit, in.RequestID, in.BuildRevisionID, in.ActorAccountID, in.ExpectedRevision, in.ConfigurationRevision, profile.Version, profile.Digest, in.AutoDeploy, in.ExpectedLiveRevision, in.PreviewSkipReason})
}

// RequestReleaseBuildRevision admits one Save and Build of the prepared source; a replay returns the original
// acceptance, a new build needs the current Revision and a settled failure of the active build, if any.
func (f *ProjectReleaseFlow) RequestReleaseBuildRevision(ctx dex.Context, in BuildRevisionRequest) (*dex.RPCResult[buildRevisionResult], error) {
	s := readSnapshot(ctx)
	previous, err := ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID)
	if err == nil {
		// A replay keeps the recipe it was accepted with.
		recipe := build.Profile{Version: previous.BuildProfileVersion, Digest: previous.BuildProfileDigest}
		if previous.Fingerprint != buildFingerprint(s, in, recipe) {
			return rejectBuildRevision(in.BuildRevisionID, dexflow.RejectionAlreadyExists, "build identity conflicts with accepted inputs"), nil
		}
		return acceptBuildRevision(in.BuildRevisionID), nil
	}
	if !dexflow.IsAttributeNotFound(err) {
		return nil, err
	}
	switch {
	case s.CancellationRequested || s.Status == Removing || s.Status == Removed:
		return rejectBuildRevision(in.BuildRevisionID, dexflow.RejectionFailedPrecondition, "release is being removed"), nil
	case in.ExpectedRevision != s.Revision:
		return staleBuildRevision(in.BuildRevisionID, "release revision changed"), nil
	case s.PreparedSource.Status != Ready:
		return rejectBuildRevision(in.BuildRevisionID, dexflow.RejectionFailedPrecondition, "release source is not prepared"), nil
	case s.ActiveBuildRevisionID != in.PreviousBuildRevisionID:
		return staleBuildRevision(in.BuildRevisionID, "active build changed"), nil
	}
	if s.ActiveBuildRevisionID != "" {
		if active := comm.Must(ReleaseBuildRevisions.Get(ctx, s.ActiveBuildRevisionID)); !active.supersedable() {
			return rejectBuildRevision(in.BuildRevisionID, dexflow.RejectionFailedPrecondition, "the current build must settle before another build starts"), nil
		}
	}
	if in.PreviewValidation == nil {
		panic("releases: build " + in.BuildRevisionID + " reached admission without its Preview validation")
	}
	profile := f.profiles.Default()
	now := time.Now().UTC()
	comm.MustDo(ReleaseBuildRevisions.Set(ctx, in.BuildRevisionID, ReleaseBuildRevision{
		BuildRevisionID: in.BuildRevisionID, ActorAccountID: in.ActorAccountID, Fingerprint: buildFingerprint(s, in, profile),
		ConfigurationRevision: in.ConfigurationRevision, BuildProfileVersion: profile.Version, BuildProfileDigest: profile.Digest,
		Status: Pending, Revision: 1, Operation: buildOperation{Phase: phaseFreeze, Attempt: 1, Requests: []BuildRequestReceipt{{RequestID: in.RequestID}}},
		AutoDeploy: in.AutoDeploy, ExpectedLiveRevision: in.ExpectedLiveRevision, CreatedAt: now, UpdatedAt: now, PreviewValidation: in.PreviewValidation,
	}))
	comm.MustDo(ActiveBuildRevisionID.Set(ctx, in.BuildRevisionID))
	comm.MustDo(ReleaseStatus.Set(ctx, Building))
	comm.MustDo(FailureAttribute.Set(ctx, nil))
	comm.MustDo(ReleaseBuildCommands.Publish(ctx, ReleaseBuildCommand{Kind: commandBuild, RequestID: in.RequestID, BuildRevisionID: in.BuildRevisionID}))
	bumpRevision(ctx)
	return acceptBuildRevision(in.BuildRevisionID), nil
}

// RequestReleaseBuildRecovery admits an explicit recovery of the active build at its current revision;
// the request identity is bound to actor and revision, so a replay is accepted once.
func (*ProjectReleaseFlow) RequestReleaseBuildRecovery(ctx dex.Context, in BuildRevisionRecovery) (*dex.RPCResult[buildRevisionResult], error) {
	s := readSnapshot(ctx)
	record, err := ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID)
	if dexflow.IsAttributeNotFound(err) {
		return rejectBuildRevision(in.BuildRevisionID, dexflow.RejectionNotFound, "release build revision was not found"), nil
	}
	if err != nil {
		return nil, err
	}
	recoveryID := flowid.Fingerprint(struct {
		Actor, Request, Build string
		Revision              uint64
	}{in.ActorAccountID, in.RequestID, in.BuildRevisionID, in.ExpectedRevision})
	if slices.ContainsFunc(record.Operation.Requests, func(r BuildRequestReceipt) bool { return r.RequestID == recoveryID }) {
		return acceptBuildRevision(in.BuildRevisionID), nil
	}
	switch {
	case s.CancellationRequested || s.ActiveBuildRevisionID != in.BuildRevisionID || !record.recoverable():
		return rejectBuildRevision(in.BuildRevisionID, dexflow.RejectionFailedPrecondition, "build is not the recoverable active revision"), nil
	case record.Revision != in.ExpectedRevision:
		return staleBuildRevision(in.BuildRevisionID, "build revision changed"), nil
	case len(record.Operation.Requests) >= maxRequestReceipts:
		return rejectBuildRevision(in.BuildRevisionID, dexflow.RejectionResourceExhausted, "build recovery receipt capacity is exhausted"), nil
	}
	record.admitRecovery(recoveryID, time.Now().UTC())
	comm.MustDo(ReleaseBuildRevisions.Set(ctx, in.BuildRevisionID, record))
	comm.MustDo(ReleaseStatus.Set(ctx, Building))
	comm.MustDo(ReleaseBuildCommands.Publish(ctx, ReleaseBuildCommand{Kind: commandBuild, RequestID: recoveryID, BuildRevisionID: in.BuildRevisionID}))
	bumpRevision(ctx)
	return acceptBuildRevision(in.BuildRevisionID), nil
}

// RequestReleaseRemoval is called only by Project cleanup after it revoked serving references and
// workload grants; a removal whose cleanup failed is admitted again at the current Revision.
func (*ProjectReleaseFlow) RequestReleaseRemoval(ctx dex.Context, in mutation) (*dex.RPCResult[mutationResult], error) {
	s := readSnapshot(ctx)
	if s.Status == Removed || s.Status == Removing && s.Failure == nil {
		return releaseResult(ctx, nil), nil
	}
	if in.ExpectedRevision != s.Revision {
		return releaseResult(ctx, dexflow.Reject(dexflow.RejectionAborted, "release revision changed")), nil
	}
	if s.Status == Removing {
		comm.MustDo(FailureAttribute.Set(ctx, nil))
	} else {
		comm.MustDo(CancellationRequested.Set(ctx, true))
		comm.MustDo(ReleaseStatus.Set(ctx, Removing))
	}
	comm.MustDo(ReleaseBuildCommands.Publish(ctx, ReleaseBuildCommand{Kind: commandRemove, RequestID: in.RequestID}))
	bumpRevision(ctx)
	return releaseResult(ctx, nil), nil
}

// RequestReleaseBuildCancellation admits a person's cancellation of the active build until its artifacts commit,
// once; it preempts every Step the build's chain can be in and schedules CancelReleaseBuildRevision.
func (f *ProjectReleaseFlow) RequestReleaseBuildCancellation(ctx dex.Context, in BuildRevisionCancellation) (*dex.RPCResult[buildRevisionResult], error) {
	s := readSnapshot(ctx)
	record, err := ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID)
	if dexflow.IsAttributeNotFound(err) {
		return rejectBuildRevision(in.BuildRevisionID, dexflow.RejectionNotFound, "release build revision was not found"), nil
	}
	if err != nil {
		return nil, err
	}
	if refused := cancellationRefused(s, record); refused != nil {
		return &dex.RPCResult[buildRevisionResult]{Output: *refused}, nil
	}
	if record.Cancellation != nil {
		return acceptBuildRevision(in.BuildRevisionID), nil
	}
	now := time.Now().UTC()
	record.Cancellation = &BuildCancellation{RequestID: in.RequestID, ActorAccountID: in.ActorAccountID, RequestedAt: now, Phase: record.Operation.Phase}
	record.Operation.Phase = phaseCancel
	record.Revision++
	record.UpdatedAt = now
	comm.MustDo(ReleaseBuildRevisions.Set(ctx, in.BuildRevisionID, record))
	bumpRevision(ctx)
	cancel := dex.MovementOf(CancelReleaseBuildRevision{flow: f}, releaseBuildStepInput{BuildRevisionID: in.BuildRevisionID}, dex.WithStepOptions(releaseBuildOptions(f, in.BuildRevisionID)))
	result := &dex.RPCResult[buildRevisionResult]{Output: buildRevisionResult{BuildRevisionID: in.BuildRevisionID}, NextSteps: []dex.StepMovement{cancel}}
	return result.CancelSteps(ResumeReleaseBuildRevision{}, FreezeReleaseBuildConfiguration{}, InspectReleaseBuildRevision{}, StartReleaseBuildRevision{}, AwaitReleaseBuildRevision{},
		PublishReleaseBuildRevision{}, CommitReleaseBuildRevision{}, RecordReleaseBuildFailure{}, NotifyReleaseFailure{}, AbandonReleaseFailureNotification{}, AwaitReleaseBuildCommands{}), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Register exact Project membership before any external build effect."
type AdmitRelease struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectReleaseFlow
}

func (s AdmitRelease) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordReleaseFailure{flow: s.flow}, nil))
}

func (s AdmitRelease) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if s.flow.builder == nil {
		return dex.GoTo(CommitReleasePublishingSource{flow: s.flow}, failedSource(failureSourcePlatform, errBuildsUnavailable.Error())), nil
	}
	project := comm.Must(ProjectID.Get(ctx))
	err := s.flow.admitRelease(ctx, project, comm.Must(ReleaseID.Get(ctx)))
	if errors.Is(err, projects.ErrChildAdmissionClosed) {
		// A Project that is already being deleted never owned this Release.
		return dex.GoTo(CompleteReleasePublishing{flow: s.flow}, nil), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("register the Release with Project %s: %w", project, err))
	}
	return dex.GoTo(PrepareReleasePublishingSource{flow: s.flow}, nil), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Turn the cause of failed admission or source preparation into the failure to commit, which the platform owns."
type RecordReleaseFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectReleaseFlow
}

func (RecordReleaseFailure) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

// Execute records platform as the owner: every exhausted retry and every wait
// limit is a platform symptom.
func (s RecordReleaseFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	reason := dexflow.RecoveryDetail(ctx)
	if dexflow.RecoveryStep(ctx) == "PrepareReleasePublishingSource" && dexflow.RecoveryTimedOut(ctx) {
		reason = fmt.Sprintf("Source preparation did not finish within %s.", sourceWaitLimit)
	}
	return dex.GoTo(CommitReleasePublishingSource{flow: s.flow}, failedSource(failureSourcePlatform, reason)), nil
}

// failedSource is the failure of source preparation of kind for the reason
// given, which an explicit retry recovers, and its owner.
func failedSource(kind releaseFailureKind, reason string) PreparedReleaseSource {
	return PreparedReleaseSource{Failure: kind.failure("PrepareReleasePublishingSource", reason), FailureOwner: kind.owner}
}

// rejectedSource is the failure of a source preparation the same request meets again (errSourceRejected):
// the application owns a rejection of the commit's source (errSourceInvalid), the platform every other.
func rejectedSource(err error) PreparedReleaseSource {
	if errors.Is(err, errSourceInvalid) {
		return failedSource(failureSourceInvalid, err.Error())
	}
	return failedSource(failureSourcePlatform, err.Error())
}

const (
	// sourcePollInterval spaces observations of source preparation, which a
	// user is waiting for.
	sourcePollInterval = 2 * time.Second
	// sourceRoundTimeout bounds the one provider inspection of a round.
	sourceRoundTimeout = 10 * time.Second
)

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Prepare the exact main commit manifest and strict FDG before configuration, polling the provider without locks."
type PrepareReleasePublishingSource struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectReleaseFlow
}

// One attempt starts or joins the provider work and polls it to its end;
// retries are safe as the provider dispatches only what inspection finds missing.
func (s PrepareReleasePublishingSource) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(sourceWaitLimit, dex.ProceedToOnExecuteFailure(RecordReleaseFailure{flow: s.flow}, nil))
}

func (s PrepareReleasePublishingSource) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, releaseBuildStepInput{}), nil
	}
	// The work before and after the polling takes up to sourceCallTimeout each.
	defer dexflow.KeepAlive(ctx, nil)()
	preparation, err := s.flow.startSource(ctx, state)
	if errors.Is(err, errSourceRejected) {
		return dex.GoTo(CommitReleasePublishingSource{flow: s.flow}, rejectedSource(err)), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	var observed build.BuildResult
	err = dexflow.PollUntil(ctx, sourcePollInterval, sourceRoundTimeout, func(call context.Context) (bool, error) {
		current, inspectErr := s.flow.builder.Inspect(call, preparation.request)
		if inspectErr != nil {
			return false, fmt.Errorf("inspect source preparation: %w", inspectErr)
		}
		if current.State != observed.State {
			dexflow.StreamWritten(ctx, BuildProgress.Write(ctx, dexflow.Progress(ctx, "PrepareReleasePublishingSource", state.Revision).WithDetail(string(current.State))))
		}
		observed = current
		return observed.State != build.BuildRunning && observed.State != build.BuildUnknown, nil
	})
	if err != nil {
		return nil, err
	}
	prepared, err := s.flow.finishSource(ctx, state, preparation, observed)
	if errors.Is(err, errSourceRejected) {
		return dex.GoTo(CommitReleasePublishingSource{flow: s.flow}, rejectedSource(err)), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	return dex.GoTo(CommitReleasePublishingSource{flow: s.flow}, prepared), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Commit the prepared source as DRAFT, or its failure with its owner for an explicit retry and tell the Session of the failure, before exposing configuration."
type CommitReleasePublishingSource struct {
	dex.StepDefaultsNoWaitFor[PreparedReleaseSource]
	flow *ProjectReleaseFlow
}

func (CommitReleasePublishingSource) GetStepOptions() *dex.StepOptions {
	return lockRevision(dexflow.StateStepOptions())
}

// Execute commits the prepared source, or only its Failure with the owner its producing Step decided, keeping the
// accepted source and receipts; a non-cancellation failure goes to NotifyReleaseFailure (hook H1).
func (s CommitReleasePublishingSource) Execute(ctx dex.Context, in PreparedReleaseSource) (*dex.StepDecision, error) {
	source, status := comm.Must(PreparedSource.Get(ctx)), Draft
	if in.Failure == nil {
		in.RequestID, in.Requests, in.Status = source.RequestID, source.Requests, Ready
		source = in
	} else {
		source.Status, source.Failure, source.FailureOwner = Failed, in.Failure, in.FailureOwner
		status = Failed
	}
	comm.MustDo(PreparedSource.Set(ctx, source))
	comm.MustDo(FailureAttribute.Set(ctx, source.Failure))
	cancelled := comm.Must(CancellationRequested.Get(ctx))
	if !cancelled {
		comm.MustDo(ReleaseStatus.Set(ctx, status))
	}
	revision := bumpRevision(ctx)
	switch {
	case cancelled:
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, releaseBuildStepInput{}), nil
	case in.Failure != nil:
		return dex.GoTo(NotifyReleaseFailure{flow: s.flow}, releaseFailure(readSnapshot(ctx), "", revision, *in.Failure, in.FailureOwner, time.Now().UTC())), nil
	}
	return dex.GoTo(AwaitReleaseBuildCommands{flow: s.flow}, nil), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Wait for an admitted build, source retry or Project cleanup command."
type AwaitReleaseBuildCommands struct {
	dex.StepDefaults
	flow *ProjectReleaseFlow
}

func (AwaitReleaseBuildCommands) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (AwaitReleaseBuildCommands) WaitFor(dex.Context, dex.None) (*dex.Wait, error) {
	return dex.Until(ReleaseBuildCommands.ForOne()), nil
}

func (s AwaitReleaseBuildCommands) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	command := comm.Must(ReleaseBuildCommands.GetConditionResults(ctx))[0]
	active := comm.Must(ActiveBuildRevisionID.Get(ctx))
	if comm.Must(CancellationRequested.Get(ctx)) {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, releaseBuildStepInput{BuildRevisionID: active}, dex.WithStepOptions(releaseCleanupLoad(active))), nil
	}
	switch command.Kind {
	case commandPrepare:
		if comm.Must(PreparedSource.Get(ctx)).Status == Pending {
			return dex.GoTo(AdmitRelease{flow: s.flow}, nil), nil
		}
	case commandBuild:
		if command.BuildRevisionID == active {
			return dex.GoTo(ResumeReleaseBuildRevision{flow: s.flow}, releaseBuildStepInput{BuildRevisionID: active}, dex.WithStepOptions(releaseBuildOptions(s.flow, active))), nil
		}
	}
	return dex.GoTo(AwaitReleaseBuildCommands{flow: s.flow}, nil), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Resume the exact admitted build phase without changing its identity."
type ResumeReleaseBuildRevision struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

func (ResumeReleaseBuildRevision) GetStepOptions() *dex.StepOptions {
	return dexflow.StateStepOptions()
}

func (s ResumeReleaseBuildRevision) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	state, record := readSnapshot(ctx), comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID))
	if state.ActiveBuildRevisionID != in.BuildRevisionID {
		return nil, admissionChanged(state.ActiveBuildRevisionID, in.BuildRevisionID)
	}
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, in, dex.WithStepOptions(releaseCleanupLoad(in.BuildRevisionID))), nil
	}
	if record.Status != Pending && record.Status != Recovering {
		return dex.GoTo(AwaitReleaseBuildCommands{flow: s.flow}, nil), nil
	}
	options := dex.WithStepOptions(releaseBuildOptions(s.flow, in.BuildRevisionID))
	switch record.Operation.Phase {
	case phaseFreeze:
		return dex.GoTo(FreezeReleaseBuildConfiguration{flow: s.flow}, in, options), nil
	case phaseInspect:
		return dex.GoTo(InspectReleaseBuildRevision{flow: s.flow}, in, options), nil
	case phaseDeploy:
		return dex.GoTo(DeployReleaseBuildRevision{flow: s.flow}, in, options), nil
	case phaseCancel:
		return dex.GoTo(CancelReleaseBuildRevision{flow: s.flow}, in, options), nil
	}
	panic("releases: build " + in.BuildRevisionID + " has the unknown recovery phase " + record.Operation.Phase)
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Freeze the accepted draft revision before starting a build; a draft that lacks what the commit declares refuses the build with its gaps."
type FreezeReleaseBuildConfiguration struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

func (s FreezeReleaseBuildConfiguration) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordReleaseBuildFailure{flow: s.flow}, nil))
}

// Execute freezes the draft, or moves one lacking what the commit declares straight to
// RecordReleaseBuildFailure with its gaps, as the same draft meets the same refusal.
func (s FreezeReleaseBuildConfiguration) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	state, record := readSnapshot(ctx), comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID))
	if state.ActiveBuildRevisionID != in.BuildRevisionID {
		return nil, admissionChanged(state.ActiveBuildRevisionID, in.BuildRevisionID)
	}
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, in, dex.WithStepOptions(releaseCleanupLoad(in.BuildRevisionID))), nil
	}
	if record.Cancellation != nil {
		return dex.GoTo(CancelReleaseBuildRevision{flow: s.flow}, in, dex.WithStepOptions(releaseBuildOptions(s.flow, in.BuildRevisionID))), nil
	}
	configuration, gaps, err := s.flow.publishing.FreezeReleaseConfiguration(ctx, state, record)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("freeze the configuration of build %s: %w", in.BuildRevisionID, err))
	}
	if len(gaps) > 0 {
		refused := releaseBuildStepInput{BuildRevisionID: in.BuildRevisionID, Gaps: gaps}
		return dex.GoTo(RecordReleaseBuildFailure{flow: s.flow}, refused, dex.WithStepOptions(releaseBuildCommitOptions(in.BuildRevisionID))), nil
	}
	return dex.GoTo(CommitReleaseBuildRevision{flow: s.flow}, releaseBuildPhaseResult{Input: in, Phase: phaseFreeze, StartedAt: ctx.FirstAttemptAt().UTC(), Configuration: &configuration}, dex.WithStepOptions(releaseBuildCommitOptions(in.BuildRevisionID))), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Reconcile the exact provider build before admitting another effect."
type InspectReleaseBuildRevision struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

func (s InspectReleaseBuildRevision) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordReleaseBuildFailure{flow: s.flow}, nil))
}

func (s InspectReleaseBuildRevision) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	state, record := readSnapshot(ctx), comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID))
	if state.ActiveBuildRevisionID != in.BuildRevisionID {
		return nil, admissionChanged(state.ActiveBuildRevisionID, in.BuildRevisionID)
	}
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, in, dex.WithStepOptions(releaseCleanupLoad(in.BuildRevisionID))), nil
	}
	if record.Cancellation != nil {
		return dex.GoTo(CancelReleaseBuildRevision{flow: s.flow}, in, dex.WithStepOptions(releaseBuildOptions(s.flow, in.BuildRevisionID))), nil
	}
	observation, err := s.flow.inspectBuild(ctx, state, record)
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	return dex.GoTo(CommitReleaseBuildRevision{flow: s.flow}, releaseBuildPhaseResult{Input: in, Phase: phaseInspect, Observation: &observation}, dex.WithStepOptions(releaseBuildCommitOptions(in.BuildRevisionID))), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Start the immutable configuration and source build once."
type StartReleaseBuildRevision struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

func (s StartReleaseBuildRevision) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordReleaseBuildFailure{flow: s.flow}, nil))
}

func (s StartReleaseBuildRevision) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	state, record := readSnapshot(ctx), comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID))
	if state.ActiveBuildRevisionID != in.BuildRevisionID {
		return nil, admissionChanged(state.ActiveBuildRevisionID, in.BuildRevisionID)
	}
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, in, dex.WithStepOptions(releaseCleanupLoad(in.BuildRevisionID))), nil
	}
	if record.Cancellation != nil {
		return dex.GoTo(CancelReleaseBuildRevision{flow: s.flow}, in, dex.WithStepOptions(releaseBuildOptions(s.flow, in.BuildRevisionID))), nil
	}
	observation, err := s.flow.startBuild(ctx, state, record)
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	return dex.GoTo(CommitReleaseBuildRevision{flow: s.flow}, releaseBuildPhaseResult{Input: in, Phase: phaseStart, Observation: &observation}, dex.WithStepOptions(releaseBuildCommitOptions(in.BuildRevisionID))), nil
}

const (
	// buildAwaitBudget is the wait limit for one provider build; it outlasts the hosted provider's own limits
	// (120-minute queue, 45-minute build; infra-as-code/terraform/cloud/variables.tf), so the provider settles a build first.
	buildAwaitBudget = 180 * time.Minute
	// buildPollInterval spaces the observations of a running build.
	buildPollInterval = 10 * time.Second
	// buildRoundTimeout bounds one observation: a Project read and a provider
	// inspection.
	buildRoundTimeout = 20 * time.Second
)

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Poll the asynchronous build until it ends or its wait limit passes, without locks."
type AwaitReleaseBuildRevision struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

// One attempt polls for the whole build without locks; Commit records the
// final observation once, under the Revision lock.
func (s AwaitReleaseBuildRevision) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(buildAwaitBudget, dex.ProceedToOnExecuteFailure(RecordReleaseBuildFailure{flow: s.flow}, nil))
}

func (s AwaitReleaseBuildRevision) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	state, record := readSnapshot(ctx), comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID))
	if state.ActiveBuildRevisionID != in.BuildRevisionID {
		return nil, admissionChanged(state.ActiveBuildRevisionID, in.BuildRevisionID)
	}
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, in, dex.WithStepOptions(releaseCleanupLoad(in.BuildRevisionID))), nil
	}
	if record.Cancellation != nil {
		return dex.GoTo(CancelReleaseBuildRevision{flow: s.flow}, in, dex.WithStepOptions(releaseBuildOptions(s.flow, in.BuildRevisionID))), nil
	}
	var observation buildObservation
	var deleting bool
	err := dexflow.PollUntil(ctx, buildPollInterval, buildRoundTimeout, func(call context.Context) (bool, error) {
		current, isDeleting, err := s.flow.observeBuild(call, state, record)
		if err != nil || isDeleting {
			deleting = isDeleting
			return deleting, err
		}
		if current.State != observation.State {
			dexflow.StreamWritten(ctx, BuildProgress.Write(ctx, dexflow.Progress(ctx, phaseAwait, state.Revision).WithDetail(string(current.State))))
		}
		observation = current
		return observation.State != build.BuildRunning, nil
	})
	if err != nil {
		return nil, err
	}
	if deleting {
		// The deleting Project removes this Release through a command, whose
		// cleanup cancels the build.
		return dex.GoTo(AwaitReleaseBuildCommands{flow: s.flow}, nil), nil
	}
	return dex.GoTo(CommitReleaseBuildRevision{flow: s.flow}, releaseBuildPhaseResult{Input: in, Phase: phaseAwait, Observation: &observation}, dex.WithStepOptions(releaseBuildCommitOptions(in.BuildRevisionID))), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Validate the built manifest and strict Flow Definitions, then publish them as immutable artifacts."
type PublishReleaseBuildRevision struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

func (s PublishReleaseBuildRevision) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordReleaseBuildFailure{flow: s.flow}, nil))
}

func (s PublishReleaseBuildRevision) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	state, record := readSnapshot(ctx), comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID))
	if state.ActiveBuildRevisionID != in.BuildRevisionID {
		return nil, admissionChanged(state.ActiveBuildRevisionID, in.BuildRevisionID)
	}
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, in, dex.WithStepOptions(releaseCleanupLoad(in.BuildRevisionID))), nil
	}
	if record.Cancellation != nil {
		return dex.GoTo(CancelReleaseBuildRevision{flow: s.flow}, in, dex.WithStepOptions(releaseBuildOptions(s.flow, in.BuildRevisionID))), nil
	}
	artifacts, err := s.flow.publishBuild(ctx, state, record)
	if err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	return dex.GoTo(CommitReleaseBuildRevision{flow: s.flow}, releaseBuildPhaseResult{Input: in, Phase: phasePublish, StartedAt: ctx.FirstAttemptAt().UTC(), Artifacts: &artifacts}, dex.WithStepOptions(releaseBuildCommitOptions(in.BuildRevisionID))), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Admit optional deployment of this exact build using the Live revision fence."
type DeployReleaseBuildRevision struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

func (s DeployReleaseBuildRevision) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordReleaseBuildFailure{flow: s.flow}, nil))
}

func (s DeployReleaseBuildRevision) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	state, record := readSnapshot(ctx), comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID))
	if state.ActiveBuildRevisionID != in.BuildRevisionID {
		return nil, admissionChanged(state.ActiveBuildRevisionID, in.BuildRevisionID)
	}
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, in, dex.WithStepOptions(releaseCleanupLoad(in.BuildRevisionID))), nil
	}
	if record.Cancellation != nil {
		return dex.GoTo(CancelReleaseBuildRevision{flow: s.flow}, in, dex.WithStepOptions(releaseBuildOptions(s.flow, in.BuildRevisionID))), nil
	}
	deploymentID, err := s.flow.publishing.DeployReleaseBuild(ctx, state, record)
	result := releaseBuildPhaseResult{Input: in, Phase: phaseDeploy, AutoDeploymentID: deploymentID}
	var rejected *dexflow.Rejection
	if errors.As(err, &rejected) {
		result.AutoDeployFailure = rejectedAutoDeployFailure(rejected)
	} else if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("deploy build %s: %w", in.BuildRevisionID, err))
	}
	return dex.GoTo(CommitReleaseBuildRevision{flow: s.flow}, result, dex.WithStepOptions(releaseBuildCommitOptions(in.BuildRevisionID))), nil
}

const (
	// buildCancelWaitLimit is the wait limit for a cancelled provider build to
	// settle.
	buildCancelWaitLimit = 15 * time.Minute
	// buildCancelInterval spaces the cancellation rounds.
	buildCancelInterval = 5 * time.Second
	// buildCancelRoundTimeout bounds one round: the provider's reads and stop.
	buildCancelRoundTimeout = 40 * time.Second
)

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Stop the provider build of an admitted cancellation and wait until it settles, without locks."
type CancelReleaseBuildRevision struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

func (s CancelReleaseBuildRevision) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(buildCancelWaitLimit, dex.ProceedToOnExecuteFailure(RecordReleaseBuildFailure{flow: s.flow}, nil))
}

func (s CancelReleaseBuildRevision) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	state, record := readSnapshot(ctx), comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID))
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, in, dex.WithStepOptions(releaseCleanupLoad(in.BuildRevisionID))), nil
	}
	dexflow.StreamWritten(ctx, BuildProgress.Write(ctx, dexflow.Progress(ctx, phaseCancel, state.Revision)))
	result := releaseBuildPhaseResult{Input: in, Phase: phaseCancel}
	if record.Configuration != nil {
		var err error
		if result.Observation, result.Unconfirmed, err = s.flow.cancelBuild(ctx, state, record, func(stage string) {
			dexflow.StreamWritten(ctx, BuildProgress.Write(ctx, dexflow.Progress(ctx, phaseCancel, state.Revision).WithDetail(stage)))
		}); err != nil {
			return nil, err
		}
	}
	return dex.GoTo(CommitReleaseBuildRevision{flow: s.flow}, result, dex.WithStepOptions(releaseBuildCommitOptions(in.BuildRevisionID))), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Commit provider observations and the latest successful build pointer; tell the Session of a failed build or a rejected automatic deployment."
type CommitReleaseBuildRevision struct {
	dex.StepDefaultsNoWaitFor[releaseBuildPhaseResult]
	flow *ProjectReleaseFlow
}

func (CommitReleaseBuildRevision) GetStepOptions() *dex.StepOptions {
	return dexflow.StateStepOptions()
}

func (s CommitReleaseBuildRevision) Execute(ctx dex.Context, in releaseBuildPhaseResult) (*dex.StepDecision, error) {
	state, record := readSnapshot(ctx), comm.Must(ReleaseBuildRevisions.Get(ctx, in.Input.BuildRevisionID))
	if state.ActiveBuildRevisionID != in.Input.BuildRevisionID {
		return nil, admissionChanged(state.ActiveBuildRevisionID, in.Input.BuildRevisionID)
	}
	if record.Cancellation != nil && in.Phase != phaseCancel {
		// The phase lost to an admitted cancellation, which records nothing of it.
		return dex.GoTo(CancelReleaseBuildRevision{flow: s.flow}, in.Input, dex.WithStepOptions(releaseBuildOptions(s.flow, in.Input.BuildRevisionID))), nil
	}
	now := time.Now().UTC()
	next, err := record.advance(in, now)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("commit the %s result of build %s: %w", in.Phase, record.BuildRevisionID, err))
	}
	comm.MustDo(ReleaseBuildRevisions.Set(ctx, record.BuildRevisionID, record))
	if in.Artifacts != nil {
		comm.MustDo(ReadyBuildRevisionID.Set(ctx, record.BuildRevisionID))
	}
	if !state.CancellationRequested {
		comm.MustDo(ReleaseStatus.Set(ctx, record.Status))
		comm.MustDo(FailureAttribute.Set(ctx, record.Failure))
	}
	if next == "" && record.Status == Ready {
		comm.MustDo(ActiveBuildRevisionID.Set(ctx, ""))
	}
	revision := bumpRevision(ctx)
	dexflow.StreamWritten(ctx, BuildProgress.Write(ctx, dexflow.Progress(ctx, in.Phase, revision)))
	if state.CancellationRequested {
		return dex.GoTo(CleanupReleasePublishing{flow: s.flow}, in.Input, dex.WithStepOptions(releaseCleanupLoad(in.Input.BuildRevisionID))), nil
	}
	if failure, owner := record.committedFailure(in); failure != nil {
		// Hook H2: the build waits for an explicit recovery, or stays READY
		// after its rejected automatic deployment.
		return dex.GoTo(NotifyReleaseFailure{flow: s.flow}, releaseFailure(state, record.BuildRevisionID, revision, *failure, owner, now)), nil
	}
	options := dex.WithStepOptions(releaseBuildOptions(s.flow, record.BuildRevisionID))
	switch next {
	case phaseInspect:
		return dex.GoTo(InspectReleaseBuildRevision{flow: s.flow}, in.Input, options), nil
	case phaseStart:
		return dex.GoTo(StartReleaseBuildRevision{flow: s.flow}, in.Input, options), nil
	case phaseAwait:
		return dex.GoTo(AwaitReleaseBuildRevision{flow: s.flow}, in.Input, options), nil
	case phasePublish:
		return dex.GoTo(PublishReleaseBuildRevision{flow: s.flow}, in.Input, options), nil
	case phaseDeploy:
		return dex.GoTo(DeployReleaseBuildRevision{flow: s.flow}, in.Input, options), nil
	default:
		return dex.GoTo(AwaitReleaseBuildCommands{flow: s.flow}, nil), nil
	}
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Retain failed or uncertain work for explicit recovery and tell the Session of it."
type RecordReleaseBuildFailure struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

func (RecordReleaseBuildFailure) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

// Execute records a build that ended without a result (hook H3): the configuration owns a draft refused
// with its gaps, the platform every exhausted retry, wait limit and unknown automatic deployment.
func (s RecordReleaseBuildFailure) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	record, reason, kind := comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID)), dexflow.RecoveryDetail(ctx), failureBuildInterrupted
	switch {
	case dexflow.RecoveryStep(ctx) == phaseCancel:
		// The cancellation settles unconfirmed, and nobody is told: a person asked for it.
		reason = dexflow.FailureMessage(fmt.Sprintf("The build provider did not confirm within %s that the build stopped.", buildCancelWaitLimit), reason)
		return dex.GoTo(CommitReleaseBuildRevision{flow: s.flow}, releaseBuildPhaseResult{Input: in, Phase: phaseCancel, Unconfirmed: reason}, dex.WithStepOptions(releaseBuildCommitOptions(in.BuildRevisionID))), nil
	case record.Cancellation != nil:
		return dex.GoTo(CancelReleaseBuildRevision{flow: s.flow}, in, dex.WithStepOptions(releaseBuildOptions(s.flow, in.BuildRevisionID))), nil
	}
	switch {
	case len(in.Gaps) > 0:
		reason, kind = "Configure "+strings.Join(appmanifest.RequirementGapNames(in.Gaps), ", ")+" in Setup · Live, then publish again.", failureBuildIncomplete
	case dexflow.RecoveryStep(ctx) == "AwaitReleaseBuildRevision" && dexflow.RecoveryTimedOut(ctx):
		reason = dexflow.FailureMessage(fmt.Sprintf("The build did not finish within the %s wait for it.", buildAwaitBudget), reason)
	}
	now := time.Now().UTC()
	failure := record.fail(kind, reason, now)
	comm.MustDo(ReleaseBuildRevisions.Set(ctx, record.BuildRevisionID, record))
	cancelled := comm.Must(CancellationRequested.Get(ctx))
	if !cancelled {
		comm.MustDo(ReleaseStatus.Set(ctx, record.Status))
	}
	comm.MustDo(FailureAttribute.Set(ctx, failure))
	revision := bumpRevision(ctx)
	if cancelled {
		return dex.GoTo(AwaitReleaseBuildCommands{flow: s.flow}, nil), nil
	}
	notice := releaseFailure(readSnapshot(ctx), record.BuildRevisionID, revision, *failure, kind.owner, now)
	notice.Gaps = in.Gaps
	return dex.GoTo(NotifyReleaseFailure{flow: s.flow}, notice), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Tell the Project's Session, best effort, of the failure the previous Step committed, then wait for the next command."
type NotifyReleaseFailure struct {
	dex.StepDefaultsNoWaitFor[DeliveryFailure]
	flow *ProjectReleaseFlow
}

// GetStepOptions: one call of the port without a lock; a failure that cannot
// be delivered is dropped, because the Release keeps it.
func (s NotifyReleaseFailure) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(AbandonReleaseFailureNotification{flow: s.flow}, nil))
}

// Execute delivers the failure its hook committed and saves nothing; the input names the FAILED
// Revision, so every retry repeats one notification, which the Session's receipts admit once.
func (s NotifyReleaseFailure) Execute(ctx dex.Context, in DeliveryFailure) (*dex.StepDecision, error) {
	if err := s.flow.feedback.ReportDeliveryFailure(ctx, in); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("report %s to the Session: %w", in.ID, err))
	}
	return dex.GoTo(AwaitReleaseBuildCommands{flow: s.flow}, nil), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Drop a failure notification whose delivery exhausted its retries; the Release keeps the failure."
type AbandonReleaseFailureNotification struct {
	dex.StepDefaultsNoWaitFor[DeliveryFailure]
	flow *ProjectReleaseFlow
}

func (AbandonReleaseFailureNotification) GetStepOptions() *dex.StepOptions {
	return dexflow.StateStepOptions()
}

// Execute keeps nothing: the engine history holds the cause.
func (s AbandonReleaseFailureNotification) Execute(dex.Context, DeliveryFailure) (*dex.StepDecision, error) {
	return dex.GoTo(AwaitReleaseBuildCommands{flow: s.flow}, nil), nil
}

const (
	// releaseCleanupBudget is the wait limit of Release cleanup, the cleanup wait limit of
	// its deleting Project, which admits the removal again after a failure.
	releaseCleanupBudget = 30 * time.Minute
	// releaseCleanupInterval spaces the bounded rounds of Release cleanup.
	releaseCleanupInterval = 2 * time.Second
	// releaseCleanupRoundTimeout bounds one cleanup round: build cancellation,
	// provider cleanup and object deletion.
	releaseCleanupRoundTimeout = 40 * time.Second
)

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Reconcile and clean every artifact owned by this commit in bounded rounds without locks before completion."
type CleanupReleasePublishing struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

// One attempt repeats bounded cleanup rounds until nothing remains; every round is idempotent,
// so a retry resumes where the provider and the store stand.
func (s CleanupReleasePublishing) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(releaseCleanupBudget, dex.ProceedToOnExecuteFailure(RecordReleasePublishingCleanupFailure{flow: s.flow}, nil))
}

func (s CleanupReleasePublishing) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	state := readSnapshot(ctx)
	if !state.CancellationRequested {
		panic("releases: cleanup of " + ctx.FlowID() + " runs without its owner's admission")
	}
	var active *ReleaseBuildRevision
	if in.BuildRevisionID != "" {
		record := comm.Must(ReleaseBuildRevisions.Get(ctx, in.BuildRevisionID))
		active = &record
	}
	// waiting is the stage of the last pending round.
	var waiting string
	err := dexflow.PollUntil(ctx, releaseCleanupInterval, releaseCleanupRoundTimeout, func(call context.Context) (bool, error) {
		stage, err := s.flow.cleanupRelease(call, state, active)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, build.ErrPending) {
			return false, err
		}
		if stage != waiting {
			dexflow.StreamWritten(ctx, BuildProgress.Write(ctx, dexflow.Progress(ctx, "CleanupReleasePublishing", state.Revision).WithDetail(stage)))
		}
		waiting = stage
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return dex.GoTo(CompleteReleasePublishing{flow: s.flow}, nil), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Complete only after Project-owned cleanup is confirmed."
type CompleteReleasePublishing struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectReleaseFlow
}

func (CompleteReleasePublishing) GetStepOptions() *dex.StepOptions {
	return lockRevision(dexflow.StateStepOptions())
}

func (s CompleteReleasePublishing) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	comm.MustDo(ReleaseStatus.Set(ctx, Removed))
	bumpRevision(ctx)
	return dex.GracefulComplete(readSnapshot(ctx)), nil
}

// dex:group group-id:publishing group-label:"Release publishing"
// dex:explanation text:"Retain cleanup responsibility until Project retries it."
type RecordReleasePublishingCleanupFailure struct {
	dex.StepDefaultsNoWaitFor[releaseBuildStepInput]
	flow *ProjectReleaseFlow
}

func (RecordReleasePublishingCleanupFailure) GetStepOptions() *dex.StepOptions {
	return lockRevision(dexflow.StateStepOptions())
}

func (s RecordReleasePublishingCleanupFailure) Execute(ctx dex.Context, in releaseBuildStepInput) (*dex.StepDecision, error) {
	reason := dexflow.RecoveryDetail(ctx)
	if dexflow.RecoveryTimedOut(ctx) {
		reason = dexflow.FailureMessage(fmt.Sprintf("Release cleanup did not finish within %s.", releaseCleanupBudget), reason)
	}
	comm.MustDo(FailureAttribute.Set(ctx, failureCleanup.failure("CleanupReleasePublishing", reason)))
	bumpRevision(ctx)
	return dex.GoTo(AwaitReleaseBuildCommands{flow: s.flow}, nil), nil
}
