// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package setup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/repobinding"
)

var (
	SetupStartIdentity = dex.DefineAttribute[dexflow.StartIdentity]("StartIdentity")
	SetupRevision      = dex.DefineAttribute[uint64]("SetupRevision", dex.SyncToAttributeStore())
	SetupCreatedAt     = dex.DefineAttribute[time.Time]("CreatedAt")
	SetupUpdatedAt     = dex.DefineAttribute[time.Time]("UpdatedAt")
)

var (
	// dex:indexed-attribute attribute-key:ProjectID index-key:keyword2 index-type:keyword value-type:string description:"ProjectID"
	SetupProjectID         = dex.DefineAttribute[string]("ProjectID", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword2"}))
	SetupID                = dex.DefineAttribute[string]("SetupID")
	SetupSourceCommit      = dex.DefineAttribute[string]("SetupSourceCommit", dex.SyncToAttributeStore())
	SetupTemplateRevision  = dex.DefineAttribute[string]("TemplateRevision")
	SetupSourceRepository  = dex.DefineAttribute[projects.RepositoryBindingRef]("SourceRepository")
	SetupPhase             = dex.DefineAttribute[string]("SetupPhase", dex.SyncToAttributeStore())
	SetupAppManifestResult = dex.DefineAttribute[*pb.AppManifestSnapshot]("AppManifestResult")
	// dex:indexed-attribute attribute-key:SetupStatus index-key:keyword3 index-type:keyword value-type:string description:"SetupStatus"
	SetupStatus                   = dex.DefineAttribute[string]("SetupStatus", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword3"}), dex.SyncToAttributeStore())
	SetupProjectDeletionRequested = dex.DefineAttribute[bool]("ProjectDeletionRequested")
	SetupFailure                  = dex.DefineAttribute[*dexflow.Failure]("SetupFailure", dex.SyncToAttributeStore())
	SetupProgress                 = dex.DefineStream[dexflow.ProgressEvent]("SetupProgress", 10<<20)
)

// ProjectSetupFlow is one Setup attempt of a Project (setup-<projectID>-<setupID>): it provisions the
// repository, source and configuration storage and hands them to the Project; a deleting Project cancels it.
type ProjectSetupFlow struct {
	dex.FlowDefaults
	Client         *projects.ProjectFlowClient
	Repositories   *repobinding.RepositoryBindingFlowClient
	Host           repohost.Host
	Objects        *objectstore.Store
	Storage        infra.Storage
	Redactor       infra.Redactor
	Configurations AppManifestRegistrar
}

const (
	// storageWaitLimit is the wait limit of the configuration storage of one
	// Setup; the engine enforces it on ReconcileProjectStorage.
	storageWaitLimit = 10 * time.Minute
	// storagePollInterval spaces the rounds of ReconcileProjectStorage.
	storagePollInterval = 2 * time.Second
	// storageRoundTimeout bounds one round: a Project read, the storage
	// reconciliation and the resource handoff.
	storageRoundTimeout = 30 * time.Second
)

func (f *ProjectSetupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStep(RecordSetupFailure{flow: f}), dex.DefineStartStep(AdmitSetup{flow: f}),
		dex.DefineStep(CreateManagedRepository{flow: f}), dex.DefineStep(ClaimRepositoryBinding{flow: f}),
		dex.DefineStep(InitializeTemplate{flow: f}), dex.DefineStep(ExtractAppManifest{flow: f}),
		dex.DefineStep(ReconcileProjectStorage{flow: f}), dex.DefineStep(CommitProjectStorage{flow: f}), dex.DefineStep(RegisterAppManifest{flow: f}),
		dex.DefineStep(ConfigureRepository{flow: f}),
		dex.DefineStep(CommitProjectSetup{flow: f}), dex.DefineStep(CompleteProjectSetup{flow: f}), dex.DefineStep(CleanupSetup{flow: f}),
	}
}

func (f *ProjectSetupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(f.GetDexSummary, dexflow.ShortRPC()), dex.DefineRPC(f.GetDexDisplay, dexflow.ShortRPC()),
		dex.DefineRPC(f.GetProjectSetup, dexflow.ShortRPC()), dex.DefineRPC(f.RequestProjectSetupCleanup, dexflow.ShortRPC(dex.LockAttribute(SetupRevision))),
	}
}

func (*ProjectSetupFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{
			SetupStartIdentity, SetupRevision, SetupCreatedAt, SetupUpdatedAt, SetupProjectID, SetupID, SetupSourceCommit, SetupTemplateRevision,
			SetupSourceRepository, SetupPhase, SetupAppManifestResult, SetupStatus, SetupProjectDeletionRequested, SetupFailure,
		},
		Streams: []dex.StreamDef{SetupProgress},
	}
}

func (f *ProjectSetupFlow) InitialAttributes(in setupStart) []dex.InitialAttributeDef {
	b := &dexflow.InitialAttributes{}
	now := time.Now().UTC()
	dexflow.Add(b, SetupStartIdentity, in.Identity)
	dexflow.Add(b, SetupRevision, uint64(1))
	dexflow.Add(b, SetupCreatedAt, now)
	dexflow.Add(b, SetupUpdatedAt, now)
	dexflow.Add(b, SetupProjectID, in.ProjectID)
	dexflow.Add(b, SetupID, in.SetupID)
	dexflow.Add(b, SetupSourceCommit, "")
	dexflow.Add(b, SetupTemplateRevision, in.TemplateRevision)
	dexflow.Add(b, SetupSourceRepository, projects.RepositoryBindingRef{})
	dexflow.Add(b, SetupPhase, "AdmitSetup")
	dexflow.Add(b, SetupAppManifestResult, nil)
	dexflow.Add(b, SetupStatus, statusPending)
	dexflow.Add(b, SetupProjectDeletionRequested, false)
	dexflow.Add(b, SetupFailure, nil)
	return b.Values()
}

// setupStepOptions is the base of every Setup Step: the Revision lock, durable writes
// before the next Step, and exhausted retries moving to onFailure.
func setupStepOptions(onFailure *dex.ExecuteFailure) *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(SetupRevision)}
	options.ExecuteDurability = dex.StepDurabilitySync
	options.ExecuteFailure = onFailure
	return options
}

func (f *ProjectSetupFlow) GetProjectSetup(ctx dex.Context, _ dex.None) (*dex.RPCResult[Setup], error) {
	return &dex.RPCResult[Setup]{Output: Setup{
		Identity:                 comm.Must(SetupStartIdentity.Get(ctx)),
		CreatedAt:                comm.Must(SetupCreatedAt.Get(ctx)),
		UpdatedAt:                comm.Must(SetupUpdatedAt.Get(ctx)),
		ProjectID:                comm.Must(SetupProjectID.Get(ctx)),
		SetupID:                  comm.Must(SetupID.Get(ctx)),
		SourceCommit:             comm.Must(SetupSourceCommit.Get(ctx)),
		TemplateRevision:         comm.Must(SetupTemplateRevision.Get(ctx)),
		Status:                   comm.Must(SetupStatus.Get(ctx)),
		Repository:               comm.Must(SetupSourceRepository.Get(ctx)),
		Phase:                    comm.Must(SetupPhase.Get(ctx)),
		AppManifest:              dexflow.Optional(SetupAppManifestResult.Get(ctx)),
		ProjectDeletionRequested: comm.Must(SetupProjectDeletionRequested.Get(ctx)),
		Failure:                  dexflow.Optional(SetupFailure.Get(ctx)),
		Revision:                 comm.Must(SetupRevision.Get(ctx)),
	}}, nil
}

// dex:field attribute-key:SetupID value-type:string editable:false description:"Setup identity"
func (*ProjectSetupFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"SetupID": comm.Must(SetupID.Get(ctx))}}, nil
}

// dex:field attribute-key:SetupSourceCommit value-type:string editable:false description:"Accepted source commit"
func (*ProjectSetupFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"SetupSourceCommit": comm.Must(SetupSourceCommit.Get(ctx))}}, nil
}

// RequestProjectSetupCleanup is Project deletion asking its Setup to stop: a running one cleans up
// at its next Step, a failed one now, a completed or cancelled one has nothing left.
func (f *ProjectSetupFlow) RequestProjectSetupCleanup(ctx dex.Context, _ dex.None) (*dex.RPCResult[setupCleanupResult], error) {
	state := f.readSetup(ctx)
	project, err := f.Client.Read(ctx, state.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read Project %s: %w", state.ProjectID, err)
	}
	if project.SetupFlowID != state.FlowID() || project.ProjectStatus != projects.StatusDeleting {
		return &dex.RPCResult[setupCleanupResult]{Output: setupCleanupResult{Rejection: dexflow.Reject(dexflow.RejectionFailedPrecondition, "owning Project must be deleting")}}, nil
	}
	result := &dex.RPCResult[setupCleanupResult]{}
	if state.Status == StatusComplete || state.Status == StatusCancelled {
		return result, nil
	}
	if !state.ProjectDeletionRequested {
		comm.MustDo(SetupProjectDeletionRequested.Set(ctx, true))
		touchSetup(ctx)
	}
	if state.Status == StatusSetupFailed {
		comm.MustDo(SetupStatus.Set(ctx, statusCleaning))
		result.NextSteps = []dex.StepMovement{dex.MovementOf(CleanupSetup{flow: f}, nil)}
	}
	return result, nil
}

// failureSetup is the code of every failure a Setup records.
const failureSetup = "SETUP_FAILED"

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Retain a terminal setup failure until Project deletion cleans its effects."
type RecordSetupFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (RecordSetupFailure) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(SetupRevision)}
	return options
}

func (s RecordSetupFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	// A deterministic failure is recorded and decided here; exhausted retries and an expired wait
	// limit arrive with their final error, whose internal resource names are redacted.
	failure := dexflow.Optional(SetupFailure.Get(ctx))
	if ctx.RecoveryError() != nil || failure == nil {
		message := "Project setup failed. Delete this project before creating another."
		if dexflow.RecoveryStep(ctx) == "ReconcileProjectStorage" && dexflow.RecoveryTimedOut(ctx) {
			message = fmt.Sprintf("Project configuration storage did not become ready within %d minutes. Delete this project before creating another.", int(storageWaitLimit/time.Minute))
		}
		detail := s.flow.Redactor.Redact(comm.Must(SetupProjectID.Get(ctx)), dexflow.RecoveryDetail(ctx))
		failure = &dexflow.Failure{Code: failureSetup, Phase: comm.Must(SetupPhase.Get(ctx)), Message: dexflow.FailureMessage(message, detail), Remedy: dexflow.RemedyContactOperator}
	}
	comm.MustDo(SetupStatus.Set(ctx, StatusSetupFailed))
	comm.MustDo(SetupFailure.Set(ctx, failure))
	touchSetup(ctx)
	return dex.DeadEnd(), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Register the Setup as a Project child before any provider effect, or cancel when the Project no longer admits it."
type AdmitSetup struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s AdmitSetup) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s AdmitSetup) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	if state.ProjectDeletionRequested {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "AdmitSetup", state.Revision)))
	err := s.flow.admitSetup(ctx, state)
	if errors.Is(err, projects.ErrChildAdmissionClosed) {
		comm.MustDo(SetupStatus.Set(ctx, StatusCancelled))
		touchSetup(ctx)
		return dex.GracefulComplete(s.flow.readSetup(ctx)), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("admit the Setup as a Project child: %w", err))
	}
	comm.MustDo(SetupStatus.Set(ctx, statusRunning))
	advanceSetup(ctx, "CreateManagedRepository")
	return dex.GoTo(CreateManagedRepository{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Create or recover the owned managed repository and record its identity."
type CreateManagedRepository struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s CreateManagedRepository) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s CreateManagedRepository) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	if state.ProjectDeletionRequested {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "CreateManagedRepository", state.Revision)))
	repository, err := s.flow.createManagedRepository(ctx, state)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("create managed repository: %w", err))
	}
	comm.MustDo(SetupSourceRepository.Set(ctx, repository))
	advanceSetup(ctx, "ClaimRepositoryBinding")
	return dex.GoTo(ClaimRepositoryBinding{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Claim exclusive managed repository ownership."
type ClaimRepositoryBinding struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s ClaimRepositoryBinding) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s ClaimRepositoryBinding) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	if state.ProjectDeletionRequested {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "ClaimRepositoryBinding", state.Revision)))
	generation, err := s.flow.Repositories.Claim(ctx, state.ProjectID, state.FlowID(), state.Repository.RepositoryID)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("claim repository %d: %w", state.Repository.RepositoryID, err))
	}
	state.Repository.Generation = generation
	comm.MustDo(SetupSourceRepository.Set(ctx, state.Repository))
	advanceSetup(ctx, "InitializeTemplate")
	return dex.GoTo(InitializeTemplate{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Push the accepted app template to the owned managed repository."
type InitializeTemplate struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s InitializeTemplate) GetStepOptions() *dex.StepOptions {
	// Execute heartbeats every 20 s while its GitHub requests (30 s each) run, so the default
	// one-minute heartbeat timeout detects a lost Worker.
	options := setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
	options.ExecuteMethodTimeout = 5 * time.Minute
	options.ExecuteRetry = dexflow.StepRetry(15 * time.Minute)
	return options
}

func (s InitializeTemplate) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	if state.ProjectDeletionRequested {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "InitializeTemplate", state.Revision)))
	defer dexflow.KeepAlive(ctx, nil)()
	commit, err := s.flow.initializeTemplate(ctx, state)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("initialize the template: %w", err))
	}
	comm.MustDo(SetupSourceCommit.Set(ctx, commit))
	advanceSetup(ctx, "ExtractAppManifest")
	return dex.GoTo(ExtractAppManifest{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Publish the source commit's app manifest and contracts as immutable objects."
type ExtractAppManifest struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s ExtractAppManifest) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s ExtractAppManifest) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	if state.ProjectDeletionRequested {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "ExtractAppManifest", state.Revision)))
	manifest, err := projects.PublishAppManifest(ctx, s.flow.Host, s.flow.Objects, state.ProjectID, state.Repository.FullName, state.SourceCommit)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("publish the app manifest of %s: %w", state.SourceCommit, err))
	}
	comm.MustDo(SetupAppManifestResult.Set(ctx, manifest))
	advanceSetup(ctx, "ReconcileProjectStorage")
	return dex.GoTo(ReconcileProjectStorage{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Reconcile the project's configuration storage until it is ready, handing every discovered identity to the Project; holds no Setup lock."
type ReconcileProjectStorage struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

// GetStepOptions makes the wait one long Execute without the Revision lock,
// which RequestProjectSetupCleanup needs while the storage starts.
func (s ReconcileProjectStorage) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(storageWaitLimit, dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s ReconcileProjectStorage) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	if state.ProjectDeletionRequested {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "ReconcileProjectStorage", state.Revision)))
	// A Project that stops admitting effects before its cleanup RPC arrives cancels the poll
	// instead of recording a failure; provider errors and the wait limit follow the failure route.
	var reason string
	var cancelled bool
	err := dexflow.PollUntil(ctx, storagePollInterval, storageRoundTimeout, func(callCtx context.Context) (bool, error) {
		err := s.flow.reconcileProjectStorage(callCtx, state)
		if errors.Is(err, projects.ErrChildAdmissionClosed) {
			cancelled = true
			return true, nil
		}
		if !errors.Is(err, infra.ErrPending) {
			return true, err
		}
		if err.Error() != reason {
			reason = err.Error()
			dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "ReconcileProjectStorage", state.Revision).WithDetail(reason)))
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	if cancelled {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	return dex.GoTo(CommitProjectStorage{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Advance to manifest registration under the Setup lock once the configuration storage is ready."
type CommitProjectStorage struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s CommitProjectStorage) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s CommitProjectStorage) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if comm.Must(SetupProjectDeletionRequested.Get(ctx)) {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	advanceSetup(ctx, "RegisterAppManifest")
	return dex.GoTo(RegisterAppManifest{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Register the exact source manifest in the project's configuration storage before setup completion."
type RegisterAppManifest struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s RegisterAppManifest) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s RegisterAppManifest) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	if state.ProjectDeletionRequested {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "RegisterAppManifest", state.Revision)))
	if err := s.flow.registerAppManifest(ctx, state); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("register the app manifest: %w", err))
	}
	advanceSetup(ctx, "ConfigureRepository")
	return dex.GoTo(ConfigureRepository{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Recheck the managed repository identity and default branch right before the handoff."
type ConfigureRepository struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s ConfigureRepository) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s ConfigureRepository) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	if state.ProjectDeletionRequested {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "ConfigureRepository", state.Revision)))
	if err := s.flow.configureRepository(ctx, state); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("recheck the repository: %w", err))
	}
	advanceSetup(ctx, "CommitProjectSetup")
	return dex.GoTo(CommitProjectSetup{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Hand the repository binding and manifest to the Project and end the Setup's cleanup membership."
type CommitProjectSetup struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s CommitProjectSetup) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s CommitProjectSetup) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	if state.ProjectDeletionRequested {
		return dex.GoTo(CleanupSetup{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "CommitProjectSetup", state.Revision)))
	if err := s.flow.commitProjectSetup(ctx, state); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("hand the Setup result to the Project: %w", err))
	}
	advanceSetup(ctx, "Complete")
	return dex.GoTo(CompleteProjectSetup{flow: s.flow}, nil), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Complete the Setup once the Project accepted its result."
type CompleteProjectSetup struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s CompleteProjectSetup) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s CompleteProjectSetup) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "Complete", s.flow.readSetup(ctx).Revision)))
	comm.MustDo(SetupStatus.Set(ctx, StatusComplete))
	touchSetup(ctx)
	return dex.GracefulComplete(s.flow.readSetup(ctx)), nil
}

// dex:group group-id:setup group-label:"Project setup"
// dex:explanation text:"Release and archive the managed repository this Setup created but never handed off, then cancel."
type CleanupSetup struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectSetupFlow
}

func (s CleanupSetup) GetStepOptions() *dex.StepOptions {
	return setupStepOptions(dex.ProceedToOnExecuteFailure(RecordSetupFailure{flow: s.flow}, nil))
}

func (s CleanupSetup) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	state := s.flow.readSetup(ctx)
	dexflow.StreamWritten(ctx, SetupProgress.Write(ctx, dexflow.Progress(ctx, "CleanupSetup", state.Revision)))
	err := s.flow.cleanupSetup(ctx, state)
	if errors.Is(err, repohost.ErrOtherHost) {
		// This server cannot reach the repository to archive it; a retry cannot
		// change that.
		detail := s.flow.Redactor.Redact(state.ProjectID, err.Error())
		message := dexflow.FailureMessage("The Setup's repository belongs to another repository host than this server's. Run the server with the repository host named below, then delete the Project again.", detail)
		comm.MustDo(SetupFailure.Set(ctx, &dexflow.Failure{Code: failureSetup, Phase: state.Phase, Message: message, Remedy: dexflow.RemedyContactOperator}))
		return dex.GoTo(RecordSetupFailure{flow: s.flow}, nil), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("clean up the Setup repository: %w", err))
	}
	comm.MustDo(SetupStatus.Set(ctx, StatusCancelled))
	touchSetup(ctx)
	return dex.GracefulComplete(s.flow.readSetup(ctx)), nil
}

// readSetup reads the snapshot of the invoking execution.
func (f *ProjectSetupFlow) readSetup(ctx dex.Context) Setup {
	return comm.Must(f.GetProjectSetup(ctx, nil)).Output
}

// advanceSetup records next as the Setup phase in progress.
func advanceSetup(ctx dex.Context, next string) {
	comm.MustDo(SetupPhase.Set(ctx, next))
	touchSetup(ctx)
}

func touchSetup(ctx dex.Context) {
	comm.MustDo(SetupRevision.Set(ctx, comm.Must(SetupRevision.Get(ctx))+1))
	comm.MustDo(SetupUpdatedAt.Set(ctx, time.Now().UTC()))
}
