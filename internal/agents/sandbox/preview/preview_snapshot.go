// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"errors"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/repohost"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// Snapshot is the authoritative state of a Project's Preview, read from its
// Attributes. Identity, Generation and CreatedAt are fixed at start; Steps and
// RPCs change the rest through save, and Deployment through saveDeployment
// under CommandLock. WorkspaceRequestID names the Session's
// instance the Preview last acquired, and Resources are that instance's while
// the Preview holds it, without its Pod once the Preview released it;
// WorkspaceState and HeadCommit are what the last source preparation observed
// in that workspace. Deployment is the automatic deployment of pushed commits,
// and Report the notification of the last reportable outcome, which only the
// Steps that end a command write. FailureOwner is who must act on Failure.
type Snapshot struct {
	CreatedAt, UpdatedAt       time.Time
	Identity                   sandbox.Identity
	Generation, Revision       uint64
	WorkspaceRequestID         string
	Resources                  sandbox.Resources
	WorkspaceState, HeadCommit string
	Preview                    PreviewState
	Environment                PreviewEnvironmentState
	AppManifest                *pb.AppManifestSnapshot
	Configuration              PreviewConfigurationRef
	PreparedSource             PreparedPreviewSource
	ActiveCommand, LastCommand Command
	Failure                    *dexflow.Failure
	FailureOwner               conversation.DeliveryOwner
	Deployment                 PreviewDeployment
	Report                     *conversation.SessionNotification
	// Applied is the configuration version the last applying start froze,
	// kept across stops; every other start revalidates it.
	Applied AppliedConfiguration
	// RecentStarts are the newest starts and deployments, oldest first, at
	// most maximumRecentStarts.
	RecentStarts []StartRecord
}

// AppliedConfiguration is the configuration head version an applying start
// froze, by immutable metadata only: ConfigurationRevision is the frozen
// document's, and CommandID and ActorAccountID name the start and its sender,
// empty for the Agent's first deployment. The zero value is none.
type AppliedConfiguration struct {
	ObjectKey, ObjectVersion, Digest string
	ConfigurationRevision            uint64
	SourceCommit, CommandID          string
	ActorAccountID                   string `json:",omitempty"`
	AppliedAt                        time.Time
}

// Object is the frozen configuration object, the zero ref for none.
func (applied AppliedConfiguration) Object() objectstore.ObjectRef {
	if applied.ObjectKey == "" {
		return objectstore.ObjectRef{}
	}
	return objectstore.ObjectRef{Key: applied.ObjectKey, Version: applied.ObjectVersion, Digest: applied.Digest, MediaType: objectstore.MediaTypeJSON}
}

// applyConfiguration records what the active start validated: an applying
// start's frozen version becomes the applied one, and its record names the
// revision it runs with and whether it applied it.
func (s *Snapshot) applyConfiguration(configuration PreviewConfigurationRef, now time.Time) {
	applies := s.appliesConfiguration()
	if applies {
		s.Applied = AppliedConfiguration{
			ObjectKey: configuration.ObjectKey, ObjectVersion: configuration.ObjectVersion, Digest: configuration.Digest,
			ConfigurationRevision: configuration.ConfigurationRevision, SourceCommit: configuration.SourceCommit,
			CommandID: s.ActiveCommand.ID, ActorAccountID: s.ActiveCommand.ActorAccountID, AppliedAt: now,
		}
	}
	if record := s.startRecord(s.ActiveCommand.ID); record != nil {
		record.ConfigurationRevision, record.Applied = configuration.ConfigurationRevision, applies
	}
}

// appliesConfiguration reports whether the active start freezes the
// configuration head: it asks to, or nothing was applied yet.
func (s Snapshot) appliesConfiguration() bool {
	return s.ActiveCommand.ApplyConfiguration || s.Applied.ObjectKey == ""
}

// instance is the acquired instance as its provider sees it.
func (s Snapshot) instance() sandbox.Instance {
	return sandbox.Instance{Identity: s.Identity, WorkspaceRequestID: s.WorkspaceRequestID, Generation: s.Generation, Resources: s.Resources}
}

// binding is the workspace transport binding of the acquired instance.
func (s Snapshot) binding() workspace.Binding { return s.instance().Binding() }

type PreviewState struct {
	Status                           Status
	URL                              string
	Revision                         uint64
	SourceCommit, DefinitionRevision string
	Definitions                      objectstore.ObjectRef
	// Timings are the phases of the current or last start: configuration,
	// environment, grant, application, route. An admitted start replaces them.
	Timings []dexflow.PhaseTiming
	// ConfigurationGaps are the gaps, at most maximumConfigurationGaps, that
	// refused the last start as PREVIEW_CONFIGURATION_INCOMPATIBLE, names
	// only; an admitted start clears them.
	ConfigurationGaps []appmanifest.RequirementGap `json:",omitempty"`
	// Version is the version of SourceCommit, which a preparation before
	// versions were recorded lacks.
	Version repohost.CommitVersion `json:",omitzero"`
	// StatusBeforeStart is the status the last admitted start-preview found,
	// which it restores when it ends before it pins its configuration.
	StatusBeforeStart Status `json:",omitempty"`
}

// Phases of PreviewState.Timings. The save that moves a start into a phase
// starts it, and the save that leaves it ends it; a failure record ends the
// phase it failed in.
const (
	phaseConfiguration = "configuration"
	phaseEnvironment   = "environment"
	phaseGrant         = "grant"
	phaseApplication   = "application"
	phaseRoute         = "route"
)

// PreparedPreviewSource is the source preparation of the acquired instance;
// it contains no configuration or credentials.
type PreparedPreviewSource struct {
	Status                                                          SourceStatus
	RequestID, WorkspaceRequestID, SourceCommit, DefinitionRevision string
	Generation                                                      uint64
	Definitions                                                     objectstore.ObjectRef
	Failure                                                         *dexflow.Failure
	// Version is the version of SourceCommit, computed once when it was
	// prepared.
	Version repohost.CommitVersion `json:",omitzero"`
}

// PreviewEnvironmentState is the Sandbox-local Preview engine of the acquired
// instance, as the Preview last ensured or stopped it. The Agent may also
// start the engine for its own Worker, which this state does not record.
type PreviewEnvironmentState struct {
	Status string
	// InstanceUID and ServiceUID are the instance's Sandbox and Service through
	// which a READY engine's Dex answered. InstanceUID is persisted as
	// SandboxUID.
	InstanceUID string `json:"SandboxUID"`
	ServiceUID  string
	// RetentionDays is the engine's retention of closed executions, read back.
	RetentionDays                                  int
	TemporalClusterID, DexVersion, TemporalVersion string
	// LastStop is how a reclamation stopped the engine, GRACEFUL or FORCED,
	// with the bounded cause of a forced stop.
	LastStop, LastStopCause string
}

// ready records the engine of answer as READY behind resources.
func (e *PreviewEnvironmentState) ready(answer workspace.PreviewAnswer, resources sandbox.Resources) {
	e.Status, e.InstanceUID, e.ServiceUID = EnvironmentReady, resources.Instance.UID, resources.ServiceUID
	e.RetentionDays, e.TemporalClusterID, e.DexVersion, e.TemporalVersion = answer.RetentionDays, answer.TemporalClusterID, answer.DexVersion, answer.TemporalVersion
}

// stopped records the end of the engine: how it stopped and why.
func (e *PreviewEnvironmentState) stopped(how, cause string) {
	e.Status, e.LastStop, e.LastStopCause = environmentStopped, how, dexflow.TruncateCause(cause)
}

// PreviewConfigurationRef pins the validated configuration of a Preview start
// by immutable metadata only, with the Live storage that holds it and the
// grants that serve it. Credential plaintext never crosses a Flow boundary.
type PreviewConfigurationRef struct {
	ObjectKey, ObjectVersion, Digest, ManifestDigest, ConnectorContractDigest, EnvironmentContractDigest string
	// SourceCommit is the prepared commit whose AppManifest the configuration
	// was validated against and whose application the start builds.
	SourceCommit                                           string
	ConfigurationRevision, AppManifestRevision, Generation uint64
	// StorageBoundaryUID and StorageBucket pin the Project's Live storage.
	// StorageBoundaryUID is persisted as StorageNamespaceUID.
	StorageBoundaryUID string `json:"StorageNamespaceUID"`
	StorageBucket      string
	// Principal is the Sandbox identity that every grant binds.
	Principal infra.Principal
	// Grant serves the configuration. SupersededGrant is the grant it replaced,
	// whose application may still serve until Grant's runs.
	Grant, SupersededGrant PreviewGrant
	// CommandID is the start or deployment that pinned the configuration.
	CommandID string `json:",omitempty"`
}

// PreviewGrant is one generation of the Preview's storage grant.
type PreviewGrant struct {
	Generation uint64
	Ref        string
}

// identity is the identity of the Preview application the configuration starts.
func (c PreviewConfigurationRef) identity() workspace.PreviewIdentity {
	return workspace.PreviewIdentity{Commit: c.SourceCommit, ConfigurationDigest: c.Digest, GrantGeneration: c.Grant.Generation}
}

// grantFor keeps current's grant for next while the configuration revision and
// the principal are unchanged, so a restart issues no new grant. Otherwise next
// gets a new generation above every one the Preview used, which its revision
// bounds, and the replaced grant becomes superseded. While a superseded grant
// is still recorded, the start that issued current's grant ended before its
// application ran, and the superseded application may still serve: that one
// stays superseded, and current's grant is dropped and returned, to be revoked
// at once.
func (current PreviewConfigurationRef) grantFor(next *PreviewConfigurationRef, previewRevision uint64, workload string) (dropped PreviewGrant) {
	next.Grant, next.SupersededGrant = current.Grant, current.SupersededGrant
	if current.Grant.Ref != "" && current.ConfigurationRevision == next.ConfigurationRevision && current.Principal == next.Principal {
		return PreviewGrant{}
	}
	generation := max(previewRevision, current.Grant.Generation) + 1
	next.Grant = PreviewGrant{Generation: generation, Ref: infra.WorkloadName(workload, generation)}
	if current.SupersededGrant.Ref == "" {
		next.SupersededGrant = current.Grant
		return PreviewGrant{}
	}
	return current.Grant
}

// serve records that the Preview of source is READY at url.
func (preview *PreviewState) serve(url string, source PreparedPreviewSource) {
	preview.Status, preview.URL = PreviewReady, url
	preview.SourceCommit, preview.DefinitionRevision, preview.Definitions, preview.Version = source.SourceCommit, source.DefinitionRevision, source.Definitions, source.Version
	preview.Revision++
	preview.Timings = dexflow.EndPhase(preview.Timings, time.Now().UTC())
}

// acquire binds the Preview to the Session's instance requestID, with its
// resources once it is READY. A new instance runs no Preview engine yet.
func (s *Snapshot) acquire(requestID string, resources sandbox.Resources) {
	if s.WorkspaceRequestID != requestID || s.Resources.Instance.UID != resources.Instance.UID {
		s.Environment = PreviewEnvironmentState{Status: environmentNotStarted}
	}
	s.WorkspaceRequestID, s.Resources = requestID, resources
	if s.ActiveCommand.Kind == CommandPreparePreviewSource || s.ActiveCommand.Kind == CommandDeployPreview || s.PreparedSource.Status == previewSourceReady {
		// READY definitions are immutable commit artifacts. A replacement may
		// start the same pinned commit without regenerating those processes.
		s.PreparedSource.WorkspaceRequestID = requestID
	}
}

// sourcePrepared reports whether the acquired instance holds a READY source
// preparation, which a Preview start deploys.
func (s Snapshot) sourcePrepared() bool {
	source := s.PreparedSource
	return source.Status == previewSourceReady && source.WorkspaceRequestID == s.WorkspaceRequestID && source.Generation == s.Generation
}

// PreparedForForTest reports whether the acquired instance prepared exactly commit
// for Preview.
func (s Snapshot) PreparedForForTest(commit string) bool {
	return s.sourcePrepared() && s.PreparedSource.SourceCommit == commit
}

// observeHead records the HEAD the workspace really has and reports whether
// the workspace is clean.
func (s *Snapshot) observeHead(dirty bool, head string) bool {
	s.HeadCommit, s.WorkspaceState = head, sandbox.WorkspaceClean
	if dirty {
		s.WorkspaceState = sandbox.WorkspaceDirty
	}
	return !dirty
}

// receipt is the completion output of a Preview, which ends only with its
// Project.
type receipt struct {
	ProjectID string
}

// initialAttributes admits the Preview of a Project and returns its initial
// Attributes. The Project must fit the deployment names of its Sandbox.
func initialAttributes(id sandbox.Identity, names deploymentnames.Names) ([]dex.InitialAttributeDef, error) {
	if err := flowid.ValidateSessionScope(names, id.ProjectID); err != nil {
		return nil, err
	}
	if id.Repository != (sandbox.Repository{}) {
		return nil, errors.New("a Preview identity names no repository; it acquires the Session's workspace")
	}
	now := time.Now().UTC()
	s := Snapshot{
		Identity: id, Generation: sandbox.InstanceGeneration, Revision: 1, CreatedAt: now, UpdatedAt: now,
		WorkspaceState: sandbox.WorkspaceUnknown,
		PreparedSource: PreparedPreviewSource{Status: PreviewSourceNotPrepared},
		// A revoked Preview grant identity stays a fence for a while, so grant
		// generations never repeat for the Project: the revision is seeded from
		// the start time, and each later advance takes at least one durable
		// second.
		Preview:     PreviewState{Status: PreviewNotStarted, Revision: uint64(now.Unix())}, //nolint:gosec // a time after 1970
		Environment: PreviewEnvironmentState{Status: environmentNotStarted},
	}
	attributes := &dexflow.InitialAttributes{}
	dexflow.Add(attributes, ProjectID, id.ProjectID)
	dexflow.Add(attributes, IdentityAttribute, s.Identity)
	dexflow.Add(attributes, GenerationAttribute, s.Generation)
	dexflow.Add(attributes, CreatedAt, now)
	dexflow.Add(attributes, CommandLock, false)
	dexflow.Add(attributes, DeploymentAttribute, s.Deployment)
	for _, attribute := range s.mutableAttributes() {
		attribute.initial(attributes)
	}
	return attributes.Values(), nil
}

// save advances the revision and writes every Attribute a Step may change
// but PreviewDeployment. Each write is unconditional and comes from the
// Snapshot the Step or RPC read when it began, and a provider Step holds no
// lock: RequestPreviewDeployment may queue a request while it runs, which
// its save would undo. So only CommandLock holders write PreviewDeployment,
// with saveDeployment.
func save(ctx dex.Context, s *Snapshot) {
	s.Revision++
	s.UpdatedAt = time.Now().UTC()
	for _, attribute := range s.mutableAttributes() {
		attribute.set(ctx)
	}
}

// saveDeployment writes PreviewDeployment alone. Only a CommandLock holder
// calls it: the Steps that end a command or the Preview, after save, and
// RequestPreviewDeployment, after save when it accepts a request, and alone,
// without a new revision, when it queues one while a provider Step may run.
func saveDeployment(ctx dex.Context, s Snapshot) {
	comm.MustDo(DeploymentAttribute.Set(ctx, s.Deployment))
}

// mutableAttributes binds every Attribute that save writes to its field.
func (s *Snapshot) mutableAttributes() []boundAttribute {
	return []boundAttribute{
		bind(Revision, s.Revision),
		bind(UpdatedAt, s.UpdatedAt),
		bind(WorkspaceRequestIDAttribute, s.WorkspaceRequestID),
		bind(ResourcesAttribute, s.Resources),
		bind(WorkspaceStateAttribute, s.WorkspaceState),
		bind(HeadCommitAttribute, s.HeadCommit),
		bind(PreviewAttribute, s.Preview),
		bind(PreviewEnvironmentAttribute, s.Environment),
		bind(AppManifestAttribute, s.AppManifest),
		bind(PreparedPreviewSourceAttribute, s.PreparedSource),
		bind(PreviewConfigurationAttribute, s.Configuration),
		bind(ActiveCommandAttribute, s.ActiveCommand),
		bind(LastCommandAttribute, s.LastCommand),
		bind(FailureAttribute, s.Failure),
		bind(FailureOwnerAttribute, s.FailureOwner),
		bind(ReportAttribute, s.Report),
		bind(AppliedConfigurationAttribute, s.Applied),
		bind(RecentStartsAttribute, s.RecentStarts),
	}
}

// boundAttribute is one Attribute with the value a Snapshot holds for it.
type boundAttribute struct {
	set     func(ctx dex.Context)
	initial func(attributes *dexflow.InitialAttributes)
}

func bind[T any](attribute dex.Attribute[T], value T) boundAttribute {
	b := bound[T]{attribute, value}
	return boundAttribute{set: b.set, initial: b.initial}
}

type bound[T any] struct {
	attribute dex.Attribute[T]
	value     T
}

func (b bound[T]) set(ctx dex.Context) { comm.MustDo(b.attribute.Set(ctx, b.value)) }
func (b bound[T]) initial(attributes *dexflow.InitialAttributes) {
	dexflow.Add(attributes, b.attribute, b.value)
}

// The tests name these constants.
const (
	PhaseConfigurationForTest = phaseConfiguration
	PhaseEnvironmentForTest   = phaseEnvironment
	PhaseGrantForTest         = phaseGrant
	PhaseApplicationForTest   = phaseApplication
	PhaseRouteForTest         = phaseRoute
)
