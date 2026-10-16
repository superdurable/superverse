// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

var (
	IdentityAttribute              = dex.DefineAttribute[sandbox.Identity]("Identity")
	GenerationAttribute            = dex.DefineAttribute[uint64]("Generation")
	WorkspaceRequestIDAttribute    = dex.DefineAttribute[string]("WorkspaceRequestID")
	ResourcesAttribute             = dex.DefineAttribute[sandbox.Resources]("Resources")
	WorkspaceStateAttribute        = dex.DefineAttribute[string]("WorkspaceState")
	HeadCommitAttribute            = dex.DefineAttribute[string]("HeadCommit")
	AppManifestAttribute           = dex.DefineAttribute[*pb.AppManifestSnapshot]("AppManifest")
	PreparedPreviewSourceAttribute = dex.DefineAttribute[PreparedPreviewSource]("PreparedPreviewSource")
	PreviewConfigurationAttribute  = dex.DefineAttribute[PreviewConfigurationRef]("PreviewConfigurationRef")
	PreviewAttribute               = dex.DefineAttribute[PreviewState]("PreviewState")
	PreviewEnvironmentAttribute    = dex.DefineAttribute[PreviewEnvironmentState]("PreviewEnvironment")
	ActiveCommandAttribute         = dex.DefineAttribute[Command]("ActiveCommand")
	LastCommandAttribute           = dex.DefineAttribute[Command]("LastCommand")
	FailureAttribute               = dex.DefineAttribute[*dexflow.Failure]("Failure")
	FailureOwnerAttribute          = dex.DefineAttribute[conversation.DeliveryOwner]("FailureOwner")
	DeploymentAttribute            = dex.DefineAttribute[PreviewDeployment]("PreviewDeployment")
	ReportAttribute                = dex.DefineAttribute[*conversation.SessionNotification]("OutcomeReport")
	AppliedConfigurationAttribute  = dex.DefineAttribute[AppliedConfiguration]("AppliedConfiguration")
	RecentStartsAttribute          = dex.DefineAttribute[[]StartRecord]("RecentStarts")
	// dex:indexed-attribute attribute-key:ProjectID index-key:keyword2 index-type:keyword value-type:string description:"Owning project"
	ProjectID = dex.DefineAttribute[string]("ProjectID", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword2"}))
	Revision  = dex.DefineAttribute[uint64]("Revision")
	CreatedAt = dex.DefineAttribute[time.Time]("CreatedAt")
	UpdatedAt = dex.DefineAttribute[time.Time]("UpdatedAt")
	// CommandLock serializes command admission: the command RPCs and the Steps that write
	// state while no command may be active. Only its holders write PreviewDeployment.
	CommandLock     = dex.DefineAttribute[bool]("CommandLock")
	PreviewCommands = dex.DefineChannel[Command]("PreviewCommands")
	// WorkspaceSettled carries the Session's announcement that the workspace
	// request a command waits for settled.
	WorkspaceSettled = dex.DefineChannel[WorkspaceSettlement]("WorkspaceSettled")
)

// PreviewFlow is the Preview of a Project until its deletion, on the Session's current instance, which it
// acquires through the Session; the SessionWorkspaceFlow owns the instance and has the Preview release it.
type PreviewFlow struct {
	dex.FlowDefaults
	// Provider exposes and stops the Preview of an instance and checks its
	// engine.
	Provider sandbox.InstanceProvider
	// Workspace reaches the acquired instance through its transport (inspection, source preparation,
	// Preview engine); Names admits the Project's Sandbox scope and Routes is the Preview's public address.
	Workspace  *workspace.Capabilities
	Names      deploymentnames.Names
	Routes     infra.PublicRoutes
	Dependency PreviewFlowDependency
	Preview    *PreviewConfiguration
}

func (f *PreviewFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{
			ProjectID, IdentityAttribute, GenerationAttribute, WorkspaceRequestIDAttribute, ResourcesAttribute,
			WorkspaceStateAttribute, HeadCommitAttribute, PreviewAttribute, PreviewEnvironmentAttribute,
			AppManifestAttribute, PreparedPreviewSourceAttribute, PreviewConfigurationAttribute,
			ActiveCommandAttribute, LastCommandAttribute, FailureAttribute, FailureOwnerAttribute, DeploymentAttribute, ReportAttribute, Revision, CreatedAt, UpdatedAt, CommandLock,
			AppliedConfigurationAttribute, RecentStartsAttribute,
		},
		Channels: []dex.ChannelDef{PreviewCommands, WorkspaceSettled},
	}
}

func (f *PreviewFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(AwaitPreviewCommands{f: f}),
		dex.DefineStep(RoutePreviewCommand{f: f}),
		dex.DefineStep(AcquireSessionWorkspace{f: f}),
		dex.DefineStep(AwaitWorkspaceSettled{f: f}),
		dex.DefineStep(CompletePreviewCommand{f: f}),
		dex.DefineStep(RecordPreviewFailure{f: f}),
		dex.DefineStep(RecordPreviewRefusal{f: f}),
		dex.DefineStep(ReadPreviewSourceVersion{f: f}),
		dex.DefineStep(PreparePreviewSource{f: f}),
		dex.DefineStep(VerifyPreviewSource{f: f}),
		dex.DefineStep(PreparePreviewConfiguration{f: f}),
		dex.DefineStep(EnsurePreviewEnvironment{f: f}),
		dex.DefineStep(StartPreview{f: f}),
		dex.DefineStep(ProbeAndExposePreview{f: f}),
		dex.DefineStep(NotifyPreviewOutcome{f: f}),
		dex.DefineStep(BeginPreviewStop{f: f}),
		dex.DefineStep(StopPreview{f: f}),
		dex.DefineStep(StopPreviewEnvironment{f: f}),
		dex.DefineStep(RecordForcedEnvironmentStop{f: f}),
		dex.DefineStep(CompletePreview{f: f}),
	}
}

func (f *PreviewFlow) GetRPCs() []dex.RPCDef {
	command := dexflow.ShortRPC(dex.LockAttribute(CommandLock))
	settled := dexflow.ShortRPC(dex.LockAttribute(CommandLock))
	settled.LoadChannels = []dex.ChannelDef{WorkspaceSettled}
	return []dex.RPCDef{
		dex.DefineRPC(f.GetPreview, dexflow.ShortRPC()),
		dex.DefineRPC(f.GetDexSummary, dexflow.ShortRPC()),
		dex.DefineRPC(f.GetDexDisplay, dexflow.ShortRPC()),
		dex.DefineRPC(f.RequestPreviewCommand, command),
		dex.DefineRPC(f.RequestPreviewDeployment, command),
		dex.DefineRPC(f.NotifyWorkspaceSettled, settled),
		dex.DefineRPC(f.EndPreview, command),
	}
}

// stateStepOptions serve short state transitions. They lock CommandLock like
// the command RPCs, because some of them admit a command while none is active.
func stateStepOptions() *dex.StepOptions {
	o := dexflow.StateStepOptions()
	o.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(CommandLock)}
	return o
}

// commandEndStepOptions serve the Steps that end a command, which discard its pending settlement.
func commandEndStepOptions() *dex.StepOptions {
	o := stateStepOptions()
	o.ExecuteLoadChannels = []dex.ChannelDef{WorkspaceSettled}
	return o
}

// A Step that calls a provider or the Session holds no lock: while its command is active the RPCs write
// only PreviewDeployment, which save never writes, so the Step is the single writer of what it saves.

// GetPreview is unlocked and returns output only, so it also serves the retained snapshot
// of a completed Preview; its reads are the graph's read edges.
func (*PreviewFlow) GetPreview(ctx dex.Context, _ dex.None) (*dex.RPCResult[Snapshot], error) {
	// ProjectID mirrors Identity for the index and CommandLock holds no state;
	// reading both records them in the graph with the Preview state.
	_, _ = comm.Must(ProjectID.Get(ctx)), comm.Must(CommandLock.Get(ctx))
	return &dex.RPCResult[Snapshot]{Output: Snapshot{
		Identity:           comm.Must(IdentityAttribute.Get(ctx)),
		Generation:         comm.Must(GenerationAttribute.Get(ctx)),
		CreatedAt:          comm.Must(CreatedAt.Get(ctx)),
		Revision:           comm.Must(Revision.Get(ctx)),
		UpdatedAt:          comm.Must(UpdatedAt.Get(ctx)),
		WorkspaceRequestID: comm.Must(WorkspaceRequestIDAttribute.Get(ctx)),
		Resources:          comm.Must(ResourcesAttribute.Get(ctx)),
		WorkspaceState:     comm.Must(WorkspaceStateAttribute.Get(ctx)),
		HeadCommit:         comm.Must(HeadCommitAttribute.Get(ctx)),
		Preview:            comm.Must(PreviewAttribute.Get(ctx)),
		Environment:        comm.Must(PreviewEnvironmentAttribute.Get(ctx)),
		// AppManifest and Failure are nil until set, and nil reads back as missing.
		AppManifest:    dexflow.Optional(AppManifestAttribute.Get(ctx)),
		PreparedSource: comm.Must(PreparedPreviewSourceAttribute.Get(ctx)),
		Configuration:  comm.Must(PreviewConfigurationAttribute.Get(ctx)),
		ActiveCommand:  comm.Must(ActiveCommandAttribute.Get(ctx)),
		LastCommand:    comm.Must(LastCommandAttribute.Get(ctx)),
		Failure:        dexflow.Optional(FailureAttribute.Get(ctx)),
		// An execution started before FailureOwner existed reads it as none.
		FailureOwner: dexflow.ValueOrZero(FailureOwnerAttribute.Get(ctx)),
		Deployment:   comm.Must(DeploymentAttribute.Get(ctx)),
		Report:       dexflow.Optional(ReportAttribute.Get(ctx)),
		// An execution started before these were recorded has neither.
		Applied:      dexflow.ValueOrZero(AppliedConfigurationAttribute.Get(ctx)),
		RecentStarts: dexflow.ValueOrZero(RecentStartsAttribute.Get(ctx)),
	}}, nil
}

// readSnapshot reads the Preview for a Step through GetPreview's reads.
func readSnapshot(ctx dex.Context) Snapshot {
	return comm.Must((*PreviewFlow).GetPreview(nil, ctx, nil)).Output
}

// GetDexSummary is the Preview row of the Dex Web Flow list, which shows the
// indexed Project already.
//
// dex:field attribute-key:WorkspaceRequestID value-type:string editable:false description:"Sandbox instance of the Preview"
func (*PreviewFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"WorkspaceRequestID": comm.Must(WorkspaceRequestIDAttribute.Get(ctx))}}, nil
}

// dex:field attribute-key:ProjectID value-type:string editable:false description:"Owning project"
// dex:field attribute-key:WorkspaceRequestID value-type:string editable:false description:"Sandbox instance of the Preview"
func (*PreviewFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"ProjectID":          comm.Must(ProjectID.Get(ctx)),
		"WorkspaceRequestID": comm.Must(WorkspaceRequestIDAttribute.Get(ctx)),
	}}, nil
}

// RequestPreviewCommand admits one command and publishes it.
func (f *PreviewFlow) RequestPreviewCommand(ctx dex.Context, c Command) (*dex.RPCResult[commandResult], error) {
	s := readSnapshot(ctx)
	if rejection, repeat := s.admit(c); rejection != "" || repeat {
		return commandAnswer(s, rejection), nil
	}
	s.accept(c)
	save(ctx, &s)
	comm.MustDo(PreviewCommands.Publish(ctx, c))
	return commandAnswer(s, ""), nil
}

// RequestPreviewDeployment is the only admission of a deploy-preview: DUPLICATE
// for a request not newer, ACCEPTED when idle, else QUEUED (newest wins); a queued request writes only PreviewDeployment.
func (f *PreviewFlow) RequestPreviewDeployment(ctx dex.Context, in DeploymentRequest) (*dex.RPCResult[DeploymentAnswer], error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	s := readSnapshot(ctx)
	answer, accepted, changed := s.requestDeployment(in, time.Now().UTC())
	if accepted != nil {
		save(ctx, &s)
		comm.MustDo(PreviewCommands.Publish(ctx, *accepted))
	}
	if changed {
		saveDeployment(ctx, s)
	}
	return &dex.RPCResult[DeploymentAnswer]{Output: answer}, nil
}

// NotifyWorkspaceSettled admits the settlement only for the active command it names; a repeat, or a stale
// one for an ended command (a retried Session Step), answers true without a message.
func (f *PreviewFlow) NotifyWorkspaceSettled(ctx dex.Context, in WorkspaceSettlement) (*dex.RPCResult[bool], error) {
	if active := comm.Must(ActiveCommandAttribute.Get(ctx)).ID; active != "" && in.CommandID == active && WorkspaceSettled.Size(ctx) == 0 {
		comm.MustDo(WorkspaceSettled.Publish(ctx, in))
	}
	return &dex.RPCResult[bool]{Output: true}, nil
}

// EndPreview ends the Preview of a deleting Project: it cancels every branch and completes,
// and the Project releases the Preview's resources by label.
func (f *PreviewFlow) EndPreview(ctx dex.Context, _ dex.None) (*dex.RPCResult[bool], error) {
	return (&dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{dex.MovementOf(CompletePreview{}, nil)}}).CancelSteps(
		AwaitPreviewCommands{}, RoutePreviewCommand{}, AcquireSessionWorkspace{}, AwaitWorkspaceSettled{}, CompletePreviewCommand{}, RecordPreviewFailure{},
		RecordPreviewRefusal{}, ReadPreviewSourceVersion{}, PreparePreviewSource{}, VerifyPreviewSource{}, PreparePreviewConfiguration{}, EnsurePreviewEnvironment{}, StartPreview{},
		ProbeAndExposePreview{}, NotifyPreviewOutcome{}, BeginPreviewStop{}, StopPreview{}, StopPreviewEnvironment{}, RecordForcedEnvironmentStop{}), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Wait for one admitted command."
type AwaitPreviewCommands struct {
	dex.StepDefaults
	f *PreviewFlow
}

func (AwaitPreviewCommands) GetStepOptions() *dex.StepOptions { return stateStepOptions() }
func (AwaitPreviewCommands) WaitFor(dex.Context, dex.None) (*dex.Wait, error) {
	return dex.Until(PreviewCommands.ForOne()), nil
}

// Execute routes the one message the wait consumed, which the RPC that
// admitted it already made the active command.
func (step AwaitPreviewCommands) Execute(dex.Context, dex.None) (*dex.StepDecision, error) {
	return dex.GoTo(RoutePreviewCommand{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Route the admitted command into its durable lifecycle."
type RoutePreviewCommand struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (RoutePreviewCommand) GetStepOptions() *dex.StepOptions { return stateStepOptions() }
func (step RoutePreviewCommand) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	switch kind := s.ActiveCommand.Kind; {
	case s.ActiveCommand.ID == "":
		return dex.GoTo(AwaitPreviewCommands{f: step.f}, nil), nil
	case kind.acquiresWorkspace():
		return dex.GoTo(AcquireSessionWorkspace{f: step.f}, nil), nil
	case kind.stops():
		return dex.GoTo(BeginPreviewStop{f: step.f}, nil), nil
	default:
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refusal(failureInvalidCommand, fmt.Errorf("unknown command kind %q", kind))), nil
	}
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Ask the Session for its current instance: a READY one is acquired; otherwise the command releases the instance it holds, then waits until the Session announces the settlement of its request; a deployment resumes a reclaimed instance so its pinned processes are built."
type AcquireSessionWorkspace struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (step AcquireSessionWorkspace) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordPreviewFailure{f: step.f}, nil))
}

// Execute acquires or resumes the Session workspace for a pinned build. A pushed
// commit is not silently skipped when reclamation raced its asynchronous handoff.
func (step AcquireSessionWorkspace) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	acquired, err := step.f.Dependency.AcquireSessionWorkspace(ctx, s.Identity, s.ActiveCommand.ID)
	if errors.Is(err, ErrSessionClosed) {
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refusal(failureSessionClosed, err)), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("acquire the Session's workspace: %w", err))
	}
	switch {
	case !acquired.Ready && s.Resources.Instance.UID != "":
		// The Session lends no instance while it replaces the held one and asks the Preview to release it
		// first, so the command releases it before waiting and a reclamation never waits for a command.
		return dex.GoTo(BeginPreviewStop{f: step.f}, nil), nil
	case !acquired.Ready:
		return dex.GoTo(AwaitWorkspaceSettled{f: step.f}, nil), nil
	}
	s.acquire(acquired.WorkspaceRequestID, acquired.Resources)
	save(ctx, &s)
	if s.ActiveCommand.Kind == CommandStartPreview {
		return dex.GoTo(VerifyPreviewSource{f: step.f}, nil), nil
	}
	return dex.GoTo(ReadPreviewSourceVersion{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Wait, bounded, until the Session announces that the workspace request the command waits for settled, then acquire the workspace again."
type AwaitWorkspaceSettled struct {
	dex.StepDefaults
	f *PreviewFlow
}

func (AwaitWorkspaceSettled) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }
func (AwaitWorkspaceSettled) WaitFor(_ dex.Context, _ dex.None) (*dex.Wait, error) {
	return dex.AnyOf(WorkspaceSettled.ForOne(), dex.Timer(workspaceWaitBudget)), nil
}

// Execute refuses the command once the wait passed or the request failed;
// after a READY settlement it asks the Session again, which lends the instance.
func (step AwaitWorkspaceSettled) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if ctx.HasTimerFired() {
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refusal(failureWorkspaceUnavailable, fmt.Errorf("the Session's workspace request did not settle within %s", workspaceWaitBudget))), nil
	}
	if settled := comm.Must(WorkspaceSettled.GetConditionResults(ctx))[0]; settled.Failure != "" {
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refusal(failureWorkspaceFailed, fmt.Errorf("the Session's workspace request %s failed with %s", settled.WorkspaceRequestID, settled.Failure))), nil
	}
	return dex.GoTo(AcquireSessionWorkspace{f: step.f}, nil), nil
}

// The Steps that end a command (CompletePreviewCommand, RecordPreviewFailure, RecordPreviewRefusal) record its
// outcome, admit the queued deployment and, if never READY, release what it started (endCommand).

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Commit command completion, admit the queued deployment or the release of what a start or deployment began without serving, and tell the Session a READY start or wait for the next command."
type CompletePreviewCommand struct {
	dex.StepDefaultsNoWaitFor[completion]
	f *PreviewFlow
}

func (CompletePreviewCommand) GetStepOptions() *dex.StepOptions { return commandEndStepOptions() }
func (step CompletePreviewCommand) Execute(ctx dex.Context, in completion) (*dex.StepDecision, error) {
	reports, admitted := endCommand(ctx, readSnapshot(ctx), commandEnd{skipped: in.Skipped})
	if admitted != nil {
		comm.MustDo(PreviewCommands.Publish(ctx, *admitted))
	}
	if reports {
		return dex.GoTo(NotifyPreviewOutcome{f: step.f}, nil), nil
	}
	return dex.GoTo(AwaitPreviewCommands{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Record the failure of a provider Step whose retries ran out or whose wait limit passed, with the final error, end its command and admit the queued deployment or the release of what a failed start or deployment began without serving."
type RecordPreviewFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (RecordPreviewFailure) GetStepOptions() *dex.StepOptions { return commandEndStepOptions() }

// Execute records a platform failure: every exhausted retry and every wait
// limit is a symptom of the platform, never of the application.
func (step RecordPreviewFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	if dexflow.RecoveryStep(ctx) == providerStep(EnsurePreviewEnvironment{}) {
		s.Environment.Status = environmentFailed
	}
	failure := newFailure(executeFailureCode(ctx), s.ActiveCommand.Kind, dexflow.RecoveryDetail(ctx))
	reports, admitted := endCommand(ctx, s, commandEnd{failure: failure, owner: conversation.DeliveryOwnerPlatform})
	if admitted != nil {
		comm.MustDo(PreviewCommands.Publish(ctx, *admitted))
	}
	if reports {
		return dex.GoTo(NotifyPreviewOutcome{f: step.f}, nil), nil
	}
	return dex.GoTo(AwaitPreviewCommands{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Record a definite refusal a Step decided, with its cause and owner, end its command and admit the queued deployment or the release of what a refused start or deployment began without serving."
type RecordPreviewRefusal struct {
	dex.StepDefaultsNoWaitFor[previewRefusal]
	f *PreviewFlow
}

func (RecordPreviewRefusal) GetStepOptions() *dex.StepOptions { return commandEndStepOptions() }
func (step RecordPreviewRefusal) Execute(ctx dex.Context, in previewRefusal) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	failure := newFailure(failureCodeOf(in.Code), s.ActiveCommand.Kind, in.Cause)
	reports, admitted := endCommand(ctx, s, commandEnd{failure: failure, owner: in.Owner, gaps: in.Gaps, refused: true})
	if admitted != nil {
		comm.MustDo(PreviewCommands.Publish(ctx, *admitted))
	}
	if reports {
		return dex.GoTo(NotifyPreviewOutcome{f: step.f}, nil), nil
	}
	return dex.GoTo(AwaitPreviewCommands{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Read the Git-owned version of the pinned push or clean HEAD and persist its name before compiling processes."
type ReadPreviewSourceVersion struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (step ReadPreviewSourceVersion) GetStepOptions() *dex.StepOptions {
	o := dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordPreviewFailure{f: step.f}, nil))
	o.ExecuteMethodTimeout = time.Minute
	o.ExecuteRetry = dexflow.CappedStepRetry(2, 3*time.Minute)
	return o
}

func (step ReadPreviewSourceVersion) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	defer dexflow.KeepAlive(ctx, nil)()
	s := readSnapshot(ctx)
	commit := s.ActiveCommand.Commit
	if s.ActiveCommand.Kind == CommandPreparePreviewSource {
		dirty, head, err := step.f.Workspace.Inspect(ctx, s.binding())
		if err != nil {
			return nil, dex.ErrorWithStack(fmt.Errorf("inspect the workspace: %w", err))
		}
		commit, s.PreparedSource.SourceCommit = head, head
		if !s.observeHead(dirty, head) {
			save(ctx, &s)
			return dex.GoTo(RecordPreviewRefusal{f: step.f}, refusal(failurePreviewDirtyHead, fmt.Errorf("the workspace has uncommitted changes on %s", head))), nil
		}
	}
	version, err := step.f.Preview.readPreviewSourceVersion(ctx, s.Identity, commit)
	if err != nil {
		save(ctx, &s)
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, manifestRefusal(err)), nil
	}
	s.PreparedSource.Version = version
	save(ctx, &s)
	return dex.GoTo(PreparePreviewSource{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Compile and render processes from the pinned commit's exported tree for a deployment or the clean pushed HEAD otherwise: register its manifest and generate immutable FDG from an isolated archive."
type PreparePreviewSource struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

// GetStepOptions: one heartbeating attempt spans the generator's timeout and its surrounding calls, a second
// covers a lost Worker; every effect is keyed by request and commit, so it repeats safely.
func (step PreparePreviewSource) GetStepOptions() *dex.StepOptions {
	o := dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordPreviewFailure{f: step.f}, nil))
	o.ExecuteMethodTimeout = previewSourceTimeout
	o.ExecuteRetry = dexflow.CappedStepRetry(2, 2*previewSourceTimeout+time.Minute)
	return o
}

// Execute prepares a deployment's pinned commit (preparePinnedSource) or the workspace's clean HEAD; the
// AppManifest is saved only with the READY preparation of its commit, never paired with another commit.
func (step PreparePreviewSource) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	defer dexflow.KeepAlive(ctx, nil)()
	s := readSnapshot(ctx)
	if s.ActiveCommand.Kind == CommandDeployPreview {
		refused, err := step.f.preparePinnedSource(ctx, &s)
		if err != nil {
			return nil, dex.ErrorWithStack(err)
		}
		if refused != nil {
			return dex.GoTo(RecordPreviewRefusal{f: step.f}, *refused), nil
		}
		save(ctx, &s)
		return dex.GoTo(VerifyPreviewSource{f: step.f}, nil), nil
	}
	dirty, head, err := step.f.Workspace.Inspect(ctx, s.binding())
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("inspect the workspace: %w", err))
	}
	namedCommit := s.PreparedSource.SourceCommit
	s.PreparedSource.SourceCommit = head
	if !s.observeHead(dirty, head) {
		save(ctx, &s)
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refusal(failurePreviewDirtyHead, fmt.Errorf("the workspace has uncommitted changes on %s", head))), nil
	}
	if namedCommit == "" {
		// A preparation scheduled before the metadata Step was introduced.
		save(ctx, &s)
		return dex.GoTo(ReadPreviewSourceVersion{f: step.f}, nil), nil
	}
	if namedCommit != head {
		s.PreparedSource.Version = repohost.CommitVersion{}
		save(ctx, &s)
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refusal(failurePreviewSourceNotPrepared, errors.New("the pushed HEAD changed while identifying processes; build the current push again"))), nil
	}
	manifest, version, err := step.f.Preview.registerPreviewAppManifest(ctx, s, head)
	if err != nil {
		save(ctx, &s)
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, manifestRefusal(err)), nil
	}
	prepared, err := step.f.Preview.preparePreviewDefinitions(ctx, s, manifest, step.f.Workspace)
	if refused, isRefused := sourceRefusal(err); isRefused {
		save(ctx, &s)
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refused), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("prepare the Preview definitions of %s: %w", head, err))
	}
	prepared.Version = version
	s.AppManifest, s.PreparedSource = manifest, prepared
	save(ctx, &s)
	return dex.GoTo(CompletePreviewCommand{f: step.f}, completion{}), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Require the acquired instance's READY source preparation, which the start deploys whatever the workspace holds now."
type VerifyPreviewSource struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (VerifyPreviewSource) GetStepOptions() *dex.StepOptions { return stateStepOptions() }

// Execute calls no provider: the Project's one Preview holds no route lease,
// so a start needs only the preparation of the instance it acquired.
func (step VerifyPreviewSource) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	if !s.sourcePrepared() {
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refusal(failurePreviewSourceNotPrepared, fmt.Errorf("the preparation of instance %s is %s", s.WorkspaceRequestID, s.PreparedSource.Status))), nil
	}
	// The start's phases begin here; a deployment keeps the phases of the
	// Preview's last start until now.
	s.Preview.Timings = dexflow.BeginPhase(nil, phaseConfiguration, time.Now().UTC())
	if record := s.startRecord(s.ActiveCommand.ID); record != nil {
		record.Commit, record.Version = s.PreparedSource.SourceCommit, s.PreparedSource.Version
	}
	save(ctx, &s)
	return dex.GoTo(PreparePreviewConfiguration{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Validate the configuration of the prepared commit and pin it, its Live storage, the Sandbox principal and the grant generation by immutable references; a configuration that lacks what the source declares is refused with every gap, and the running Preview is left untouched."
type PreparePreviewConfiguration struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (step PreparePreviewConfiguration) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordPreviewFailure{f: step.f}, nil))
}

// Execute asks for the gap report once validation refuses the configuration: gaps refuse the start as
// PREVIEW_CONFIGURATION_INCOMPATIBLE, none keeps _FAILED; a head past the applied revision is _CHANGED.
func (step PreparePreviewConfiguration) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	configuration, err := step.f.Preview.preparePreview(ctx, s)
	if errors.Is(err, projectconfig.ErrRevisionChanged) {
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, configurationRefusal(failurePreviewConfigurationChanged, err)), nil
	}
	if errors.Is(err, projectconfig.ErrNotReady) || errors.Is(err, projectconfig.ErrConflict) {
		gaps, gapsErr := step.f.Preview.checkRequirements(ctx, s)
		if gapsErr != nil {
			return nil, dex.ErrorWithStack(fmt.Errorf("list the configuration gaps of %s: %w", s.PreparedSource.SourceCommit, gapsErr))
		}
		if len(gaps) > 0 {
			return dex.GoTo(RecordPreviewRefusal{f: step.f}, incompatibleRefusal(gaps)), nil
		}
	}
	if errors.Is(err, projectconfig.ErrInvalid) || errors.Is(err, projectconfig.ErrNotReady) || errors.Is(err, projectconfig.ErrConflict) {
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refusal(failurePreviewConfiguration, err)), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("prepare the Preview configuration: %w", err))
	}
	s.applyConfiguration(configuration, time.Now().UTC())
	s.startsPreview(configuration)
	s.Preview.Timings = dexflow.BeginPhase(s.Preview.Timings, phaseEnvironment, time.Now().UTC())
	save(ctx, &s)
	return dex.GoTo(EnsurePreviewEnvironment{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Start the Sandbox-local Preview engine, or find it running, until its Dex answers through the instance's own Sandbox Service."
type EnsurePreviewEnvironment struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (step EnsurePreviewEnvironment) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(environmentBudget, dex.ProceedToOnExecuteFailure(RecordPreviewFailure{f: step.f}, nil))
}
func (step EnsurePreviewEnvironment) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	wait := &providerWait{}
	var engine workspace.PreviewAnswer
	err := dexflow.PollUntil(ctx, providerInterval, providerRoundTimeout, func(call context.Context) (bool, error) {
		answer, err := step.f.Provider.EnsurePreviewEnvironment(call, s.instance())
		if errors.Is(err, infra.ErrPending) {
			// The Preview Dex does not answer yet.
			return false, nil
		}
		if err != nil {
			return wait.refuseConflict(failurePreviewOwnershipConflict, fmt.Errorf("ensure the Preview environment: %w", err))
		}
		if code := environmentRefusalCode(answer.State); code != noFailure {
			// Only the supervisor's own refusal fails the engine; an ownership conflict ends this
			// start but leaves the engine as the supervisor last reported it.
			s.Environment.Status = environmentFailed
			wait.refuse(code, fmt.Errorf("the Preview environment is %s: %s", answer.State, answer.Reason))
			return true, nil
		}
		engine = answer
		return answer.State == workspace.PreviewStateReady, nil
	})
	if err != nil {
		return nil, err
	}
	if refused, failed := wait.failure(); failed {
		save(ctx, &s)
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refused), nil
	}
	s.Environment.ready(engine, s.Resources)
	s.Preview.Timings = dexflow.BeginPhase(s.Preview.Timings, phaseGrant, time.Now().UTC())
	save(ctx, &s)
	return dex.GoTo(StartPreview{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Issue or reuse the storage grant, start the prepared commit's application through the supervisor until it runs, then revoke the grant it superseded."
type StartPreview struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (step StartPreview) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(operationBudget, dex.ProceedToOnExecuteFailure(RecordPreviewFailure{f: step.f}, nil))
}
func (step StartPreview) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	wait := &providerWait{}
	running := false
	err := dexflow.PollUntil(ctx, providerInterval, providerRoundTimeout, func(call context.Context) (bool, error) {
		if !running {
			environment, granted, err := step.f.Preview.materializePreview(call, s)
			if err != nil {
				return wait.refuseGrant(failurePreviewOwnershipConflict, fmt.Errorf("materialize the Preview storage grant: %w", err))
			}
			if !granted {
				// The storage grant is still reconciling.
				return false, nil
			}
			s.Preview.Timings = dexflow.BeginPhase(s.Preview.Timings, phaseApplication, time.Now().UTC())
			answer, err := step.f.startPreview(call, s, environment)
			switch {
			case errors.Is(err, workspace.ErrPreviewSupervisor):
				// The build failed, or the application exited or missed its
				// health probe: the application's own failure.
				wait.refuseApplication(failurePreview, err)
				return true, nil
			case err != nil:
				return false, fmt.Errorf("start the Preview: %w", err)
			case answer.State == workspace.PreviewStateSourceMissing:
				wait.refuse(failurePreviewSourceNotPrepared, fmt.Errorf("the prepared commit %s is not in the workspace repository", s.Configuration.SourceCommit))
				return true, nil
			}
			running = answer.State == workspace.PreviewStateRunning
		}
		if !running {
			return false, nil
		}
		s.Preview.Timings = dexflow.BeginPhase(s.Preview.Timings, phaseRoute, time.Now().UTC())
		// The supervisor stopped the superseded grant's application when it
		// started this one.
		revoked, err := step.f.Preview.revokeSupersededGrant(call, s)
		if err != nil {
			return wait.refuseConflict(failurePreviewOwnershipConflict, fmt.Errorf("revoke the superseded Preview grant: %w", err))
		}
		return revoked, nil
	})
	if err != nil {
		return nil, err
	}
	// The phases the start reached are kept, also when it was refused.
	refused, failed := wait.failure()
	if !failed {
		s.Configuration.SupersededGrant = PreviewGrant{}
	}
	save(ctx, &s)
	if failed {
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refused), nil
	}
	return dex.GoTo(ProbeAndExposePreview{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Reconcile the Project's route to the instance under its stable host and probe through the real gateway until it answers, then READY."
type ProbeAndExposePreview struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (step ProbeAndExposePreview) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(operationBudget, dex.ProceedToOnExecuteFailure(RecordPreviewFailure{f: step.f}, nil))
}
func (step ProbeAndExposePreview) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	wait := &providerWait{}
	url := ""
	err := dexflow.PollUntil(ctx, providerInterval, providerRoundTimeout, func(call context.Context) (bool, error) {
		// Until the gateway answers, the URL stays empty.
		exposed, _, err := step.f.Provider.ExposePreview(call, s.instance())
		if err != nil {
			return wait.refuseConflict(failurePreviewOwnershipConflict, fmt.Errorf("publish the Preview route: %w", err))
		}
		url = exposed
		return url != "", nil
	})
	if err != nil {
		return nil, err
	}
	if refused, failed := wait.failure(); failed {
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refused), nil
	}
	s.Preview.serve(url, s.PreparedSource)
	s.endStart(s.ActiveCommand.ID, StartReady, nil, time.Now().UTC())
	save(ctx, &s)
	return dex.GoTo(CompletePreviewCommand{f: step.f}, completion{}), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Tell the Session, best effort, the outcome of a deployment, a start or a source preparation as one delivery notification, then wait for the next command."
type NotifyPreviewOutcome struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

// GetStepOptions: a notification that cannot be delivered is dropped; the
// Preview keeps the outcome.
func (step NotifyPreviewOutcome) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(AwaitPreviewCommands{f: step.f}, nil))
}

// Execute delivers the report the ending Step kept and saves nothing; only those Steps write
// it, so every retry delivers one notification, which the Session's receipts admit once.
func (step NotifyPreviewOutcome) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	report := comm.Must(ReportAttribute.Get(ctx))
	if err := step.f.Dependency.ReportPreviewOutcome(ctx, comm.Must(IdentityAttribute.Get(ctx)), *report); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("report %s to the Session: %w", report.ID, err))
	}
	return dex.GoTo(AwaitPreviewCommands{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Revoke broker admission durably before provider cleanup begins; a start that releases the instance it held stays STARTING."
type BeginPreviewStop struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (BeginPreviewStop) GetStepOptions() *dex.StepOptions { return stateStepOptions() }
func (step BeginPreviewStop) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	if s.ActiveCommand.Kind != CommandStartPreview {
		// An admitted start revoked broker admission already.
		s.Preview.Status = previewStopping
	}
	save(ctx, &s)
	return dex.GoTo(StopPreview{f: step.f}, nil), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Remove the Project's route, stop the application and revoke the Preview grants; giving up the instance then stops the engine, while an explicit stop and the release of a deployment keep it."
type StopPreview struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (step StopPreview) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(operationBudget, dex.ProceedToOnExecuteFailure(RecordPreviewFailure{f: step.f}, nil))
}
func (step StopPreview) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	wait := &providerWait{}
	releasing := s.ActiveCommand.Kind.releasesInstance()
	stopped, revoked := false, false
	err := dexflow.PollUntil(ctx, providerInterval, providerRoundTimeout, func(call context.Context) (bool, error) {
		if !stopped {
			var err error
			stopped, err = step.f.Provider.StopPreview(call, s.instance(), s.ActiveCommand.ID)
			if releasing && errors.Is(err, workspace.ErrUnreachable) {
				// The Session deletes the Pod next, which ends the Preview process even if the
				// supervisor is unreachable; an explicit stop keeps the Pod and needs its answer.
				stopped, err = true, nil
			}
			if err != nil {
				return wait.refuseIf(previewCleanupRefusalCode(err), fmt.Errorf("stop the Preview process: %w", err))
			}
		}
		if stopped && !revoked {
			var err error
			if revoked, err = step.f.Preview.revokePreview(call, s); err != nil {
				return wait.refuseIf(previewCleanupRefusalCode(err), fmt.Errorf("revoke the Preview storage grant: %w", err))
			}
		}
		return revoked, nil
	})
	if err != nil {
		return nil, err
	}
	if refused, failed := wait.failure(); failed {
		return dex.GoTo(RecordPreviewRefusal{f: step.f}, refused), nil
	}
	status := previewStopped
	if s.ActiveCommand.Kind == CommandStartPreview {
		status = previewStarting
	}
	// The configuration gaps of the last refused start stay until a start is
	// admitted.
	s.Preview, s.Configuration = PreviewState{Status: status, Revision: s.Preview.Revision + 1, Timings: s.Preview.Timings,
		ConfigurationGaps: s.Preview.ConfigurationGaps}, PreviewConfigurationRef{}
	if releasing && s.Resources.ServiceName != "" {
		s.Environment.Status = environmentStopping
		save(ctx, &s)
		return dex.GoTo(StopPreviewEnvironment{f: step.f}, nil), nil
	}
	// An explicit stop and the release of a deployment keep the engine, and an
	// instance that never published its Service ran none.
	if s.ActiveCommand.Kind.acquiresWorkspace() {
		s.releaseInstance()
		save(ctx, &s)
		return dex.GoTo(AcquireSessionWorkspace{f: step.f}, nil), nil
	}
	save(ctx, &s)
	return dex.GoTo(CompletePreviewCommand{f: step.f}, completion{}), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Stop the Sandbox-local Preview engine before the Session deletes its Pod; an engine that does not stop gracefully is stopped by the Pod deletion."
type StopPreviewEnvironment struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (step StopPreviewEnvironment) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(environmentStopBudget, dex.ProceedToOnExecuteFailure(RecordForcedEnvironmentStop{f: step.f}, nil))
}
func (step StopPreviewEnvironment) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	forced := ""
	err := dexflow.PollUntil(ctx, providerInterval, providerRoundTimeout, func(call context.Context) (bool, error) {
		answer, err := step.f.Workspace.StopPreviewEnvironment(call, s.binding(), "environment/"+s.WorkspaceRequestID+"/stop")
		if errors.Is(err, workspace.ErrUnreachable) {
			// No supervisor can be asked, and the Pod deletion that follows
			// stops what still runs in it.
			forced = err.Error()
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("stop the Preview environment: %w", err)
		}
		return answer.State == workspace.PreviewStateStopped, nil
	})
	if err != nil {
		return nil, err
	}
	if forced != "" {
		s.Environment.stopped(environmentStopForced, forced)
	} else {
		s.Environment.stopped(environmentStopGraceful, "")
	}
	if s.ActiveCommand.Kind.acquiresWorkspace() {
		s.releaseInstance()
		save(ctx, &s)
		return dex.GoTo(AcquireSessionWorkspace{f: step.f}, nil), nil
	}
	save(ctx, &s)
	return dex.GoTo(CompletePreviewCommand{f: step.f}, completion{}), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"Record that the Pod deletion stops a Preview engine whose graceful stop reached its wait limit or ran out of retries, with the cause, and complete the release."
type RecordForcedEnvironmentStop struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (RecordForcedEnvironmentStop) GetStepOptions() *dex.StepOptions { return stateStepOptions() }

// Execute is the failure route of StopPreviewEnvironment, so it takes that
// Step's input; the cause is the failed attempt's recovery detail.
func (step RecordForcedEnvironmentStop) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	// The preStop hook still asks the supervisor for a graceful stop within
	// the Pod's termination grace period.
	s.Environment.stopped(environmentStopForced, dexflow.RecoveryDetail(ctx))
	if s.ActiveCommand.Kind.acquiresWorkspace() {
		s.releaseInstance()
		save(ctx, &s)
		return dex.GoTo(AcquireSessionWorkspace{f: step.f}, nil), nil
	}
	save(ctx, &s)
	return dex.GoTo(CompletePreviewCommand{f: step.f}, completion{}), nil
}

// dex:group group-id:preview group-label:"Preview"
// dex:explanation text:"End the Preview of a deleting Project: discard pending messages, cancel the queued deployment and complete."
type CompletePreview struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *PreviewFlow
}

func (CompletePreview) GetStepOptions() *dex.StepOptions {
	o := stateStepOptions()
	o.ExecuteLoadChannels = []dex.ChannelDef{PreviewCommands, WorkspaceSettled}
	return o
}
func (step CompletePreview) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	for _, message := range comm.Must(PreviewCommands.PendingMessages(ctx)) {
		comm.MustDo(PreviewCommands.Delete(ctx, message.MessageID))
	}
	discardSettlements(ctx)
	// Nobody is told: the Session completes with its Project.
	now := time.Now().UTC()
	s.cancelQueued(now)
	if s.ActiveCommand.ID != "" {
		s.finishCommand(commandEnd{failure: newFailure(failureSessionClosed, s.ActiveCommand.Kind, "the Project is being deleted"), owner: conversation.DeliveryOwnerPlatform, cancelled: true}, now)
	}
	save(ctx, &s)
	saveDeployment(ctx, s)
	return dex.GracefulComplete(receipt{ProjectID: s.Identity.ProjectID}), nil
}

// startPreview asks the supervisor to start the pinned commit's application with the
// materialized environment plus the route address, and returns its answer.
func (f *PreviewFlow) startPreview(ctx context.Context, s Snapshot, environment map[string]string) (workspace.PreviewAnswer, error) {
	environment["PUBLIC_BASE_URL"] = f.Routes.URL(infra.ScopePreview, s.Identity.ProjectID)
	identity := s.Configuration.identity()
	commandID := fmt.Sprintf("start/%s/%d/%s", s.WorkspaceRequestID, identity.GrantGeneration, identity.Commit)
	return f.Workspace.BeginPreview(ctx, s.binding(), commandID, workspace.PreviewConfiguration{PreviewIdentity: identity, Environment: environment})
}
