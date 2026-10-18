// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/repobinding"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

var (
	ProjectStartIdentity = dex.DefineAttribute[dexflow.StartIdentity]("StartIdentity")
	ProjectRevision      = dex.DefineAttribute[uint64]("Revision", dex.SyncToAttributeStore())
	ProjectCreatedAt     = dex.DefineAttribute[time.Time]("CreatedAt", dex.SyncToAttributeStore())
	ProjectUpdatedAt     = dex.DefineAttribute[time.Time]("UpdatedAt", dex.SyncToAttributeStore())
)

var (
	// dex:indexed-attribute attribute-key:ProjectID index-key:keyword2 index-type:keyword value-type:string description:"Project identity"
	ProjectID               = dex.DefineAttribute[string]("ProjectID", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword2"}))
	ProjectCreatorAccountID = dex.DefineAttribute[string]("CreatorAccountID", dex.SyncToAttributeStore())
	ProjectName             = dex.DefineAttribute[string]("Name", dex.SyncToAttributeStore())
	ProjectTemplateRevision = dex.DefineAttribute[string]("TemplateRevision", dex.SyncToAttributeStore())
	ProjectSetupFlowID      = dex.DefineAttribute[string]("SetupFlowID", dex.SyncToAttributeStore())
	ProjectTeamSpaceID      = dex.DefineAttribute[string]("TeamSpaceID", dex.SyncToAttributeStore())
	// dex:indexed-attribute attribute-key:ProjectStatus index-key:keyword3 index-type:keyword value-type:string description:"Project lifecycle status"
	ProjectStatus            = dex.DefineAttribute[string]("ProjectStatus", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword3"}), dex.SyncToAttributeStore())
	ProjectRepositoryBinding = dex.DefineAttribute[RepositoryBindingRef]("RepositoryBinding", dex.SyncToAttributeStore())
	ProjectAppManifest       = dex.DefineAttribute[*pb.AppManifestSnapshot]("AppManifest", dex.SyncToAttributeStore())
	ProjectActiveDeployment  = dex.DefineAttribute[*DeploymentLease]("ActiveDeployment", dex.SyncToAttributeStore())
	ProjectCurrentLive       = dex.DefineAttribute[*LivePointer]("CurrentLive", dex.SyncToAttributeStore())
	ProjectStorage           = dex.DefineAttribute[infra.StorageRefs]("ProjectStorage", dex.SyncToAttributeStore())
	ProjectLiveExecution     = dex.DefineAttribute[infra.ExecutionRefs]("LiveExecution", dex.SyncToAttributeStore())
	ProjectActiveCommand     = dex.DefineAttribute[*ProjectCommand]("ActiveCommand", dex.SyncToAttributeStore())
	ProjectLastCommand       = dex.DefineAttribute[*ProjectCommand]("LastCommand")
	ProjectLastFailure       = dex.DefineAttribute[*dexflow.Failure]("LastFailure", dex.SyncToAttributeStore())
	// ProjectSessionStartFailure is SESSION_START_FAILED, kept apart from the command failure in
	// LastFailure: commands leave it, and only the Session's registration clears it.
	ProjectSessionStartFailure = dex.DefineAttribute[*dexflow.Failure]("SessionStartFailure", dex.SyncToAttributeStore())
	// ProjectInitialSessionRequest is the creator's Session model and first message until StartProjectSession
	// starts the Session; it is never synced, so the message never reaches SQL.
	ProjectInitialSessionRequest = dex.DefineAttribute[SessionStartRequest]("InitialSessionRequest")
	ChildFlows                   = dex.DefineAttributeMap[ChildFlowReference]("ChildFlows")
	// ProjectMembers, by AccountID, syncs as the child table project_members and stays after deletion.
	ProjectMembers = dex.DefineAttributeMap[ProjectMember]("Members", dex.SyncToAttributeStore())
	// ProjectAttachments, by attachment ID, are the files the Project's messages sent, which the totals count.
	ProjectAttachments      = dex.DefineAttributeMap[ProjectAttachment]("Attachments")
	ProjectAttachmentTotals = dex.DefineAttribute[AttachmentTotals]("AttachmentTotals")
	ProjectCommands         = dex.DefineChannel[ProjectCommand]("ProjectCommands")
	ProjectProgress         = dex.DefineStream[dexflow.ProgressEvent]("ProjectProgress", 10<<20)
)

// ProjectFlow is the permanent owner of one Project (project-<projectID>): it registers in its TeamSpace, starts
// the Setup and, once READY, the one Session, admits children and the Live lease, and runs removal and deletion.
type ProjectFlow struct {
	dex.FlowDefaults
	Client       *ProjectFlowClient
	TeamSpaces   *teamspaces.TeamSpaceFlowClient
	Setups       SetupLifecycle
	Repositories *repobinding.RepositoryBindingFlowClient
	Host         repohost.Host
	Sessions     SessionLifecycle
	Releases     ReleaseCleanup
	Deployments  DeploymentCleanup
	Objects      *objectstore.Store
	Storage      infra.Storage
	Cleanup      infra.Cleanup
	Connections  ConnectionUsages
	Attachments  AttachmentLimits
}

func (f *ProjectFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(RegisterProjectInTeamSpace{flow: f}), dex.DefineStep(RecordTeamSpaceAdmissionRejection{flow: f}),
		dex.DefineStep(StartProjectSetup{flow: f}), dex.DefineStep(RecordProjectSetupStartFailure{flow: f}),
		dex.DefineStep(StartProjectSession{flow: f}), dex.DefineStep(RecordProjectSessionStartFailure{flow: f}),
		dex.DefineStep(AwaitProjectCommands{flow: f}), dex.DefineStep(CommitProjectPhase{flow: f}), dex.DefineStep(RecordProjectFailure{flow: f}),
		dex.DefineStep(RemoveLiveServing{flow: f}), dex.DefineStep(RevokeDeploymentGrant{flow: f}), dex.DefineStep(CommitLiveAbsent{flow: f}),
		dex.DefineStep(StopAndCleanChildren{flow: f}), dex.DefineStep(DestroyLiveDataAndInfrastructure{flow: f}), dex.DefineStep(ReleaseConnectionUsages{flow: f}),
		dex.DefineStep(ArchiveManagedRepository{flow: f}),
		dex.DefineStep(ReleaseRepositoryBinding{flow: f}), dex.DefineStep(CompleteDeletion{flow: f}),
	}
}

func (f *ProjectFlow) GetRPCs() []dex.RPCDef {
	read := dexflow.ShortRPC()
	write := dexflow.ShortRPC(dex.LockAttribute(ProjectRevision))
	members := dexflow.ShortRPC(dex.LockAttribute(ProjectRevision))
	members.LoadAttributeMaps = []dex.AttributeDef{ProjectMembers}
	return []dex.RPCDef{
		dex.DefineRPC(f.GetDexSummary, read), dex.DefineRPC(f.GetDexDisplay, read), dex.DefineRPC(f.GetProject, read), dex.DefineRPC(f.GetChildFlow, read),
		dex.DefineRPC(f.CommitProjectSetup, write), dex.DefineRPC(f.RecordProjectStorage, write),
		dex.DefineRPC(f.RegisterChildFlow, write), dex.DefineRPC(f.AcknowledgeChildCleanup, write),
		dex.DefineRPC(f.RequestLiveRemoval, write), dex.DefineRPC(f.RequestProjectDeletion, write),
		dex.DefineRPC(f.AcquireDeployment, write), dex.DefineRPC(f.RecordLiveExecution, write), dex.DefineRPC(f.CommitLiveDeployment, write),
		dex.DefineRPC(f.ReleaseDeployment, write),
		dex.DefineRPC(f.AddProjectMember, members), dex.DefineRPC(f.SetProjectMemberRole, members), dex.DefineRPC(f.RemoveProjectMember, members),
		dex.DefineRPC(f.ReserveProjectAttachments, write), dex.DefineRPC(f.ReleaseProjectAttachments, write),
	}
}

func (f *ProjectFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{
			ProjectStartIdentity, ProjectRevision, ProjectCreatedAt, ProjectUpdatedAt, ProjectID, ProjectCreatorAccountID, ProjectName, ProjectTemplateRevision,
			ProjectSetupFlowID, ProjectTeamSpaceID, ProjectStatus, ProjectRepositoryBinding, ProjectAppManifest, ProjectActiveDeployment, ProjectCurrentLive, ProjectStorage,
			ProjectLiveExecution, ProjectActiveCommand, ProjectLastCommand, ProjectLastFailure, ProjectSessionStartFailure, ChildFlows, ProjectMembers,
			ProjectInitialSessionRequest, ProjectAttachments, ProjectAttachmentTotals,
		},
		Channels: []dex.ChannelDef{ProjectCommands},
		Streams:  []dex.StreamDef{ProjectProgress},
	}
}

func (f *ProjectFlow) InitialAttributes(in ProjectStart) []dex.InitialAttributeDef {
	b := &dexflow.InitialAttributes{}
	now := time.Now().UTC()
	dexflow.Add(b, ProjectStartIdentity, in.Identity)
	dexflow.Add(b, ProjectRevision, uint64(1))
	dexflow.Add(b, ProjectCreatedAt, now)
	dexflow.Add(b, ProjectUpdatedAt, now)
	dexflow.Add(b, ProjectID, in.ProjectID)
	dexflow.Add(b, ProjectCreatorAccountID, in.CreatorAccountID)
	dexflow.Add(b, ProjectName, in.Name)
	dexflow.Add(b, ProjectTemplateRevision, in.TemplateRevision)
	dexflow.Add(b, ProjectSetupFlowID, in.SetupFlowID)
	dexflow.Add(b, ProjectTeamSpaceID, in.TeamSpaceID)
	dexflow.Add(b, ProjectStatus, StatusCreating)
	dexflow.Add(b, ProjectRepositoryBinding, RepositoryBindingRef{})
	dexflow.Add(b, ProjectAppManifest, nil)
	dexflow.Add(b, ProjectActiveDeployment, nil)
	dexflow.Add(b, ProjectCurrentLive, nil)
	dexflow.Add(b, ProjectStorage, infra.StorageRefs{})
	dexflow.Add(b, ProjectLiveExecution, infra.ExecutionRefs{})
	dexflow.Add(b, ProjectActiveCommand, nil)
	dexflow.Add(b, ProjectLastCommand, nil)
	dexflow.Add(b, ProjectLastFailure, nil)
	dexflow.Add(b, ProjectSessionStartFailure, nil)
	dexflow.AddInstance(b, ProjectMembers, in.CreatorAccountID, ProjectMember{Role: RoleOwner, AddedAt: now, AddedByAccountID: in.CreatorAccountID})
	totals := AttachmentTotals{}
	if in.InitialSession != nil {
		dexflow.Add(b, ProjectInitialSessionRequest, *in.InitialSession)
		for _, attached := range in.InitialSession.Attachments {
			dexflow.AddInstance(b, ProjectAttachments, attached.File.ID, projectAttachment(attached.File, in.InitialSession.MessageID, "", now))
			totals.Count, totals.Bytes = totals.Count+1, totals.Bytes+attached.File.Bytes
		}
	}
	dexflow.Add(b, ProjectAttachmentTotals, totals)
	return b.Values()
}

// providerStepOptions is the base of a Step with provider effects: its writes
// are durable before the next Step, and exhausted retries move to onFailure.
func providerStepOptions(onFailure *dex.ExecuteFailure) *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteDurability = dex.StepDurabilitySync
	options.ExecuteFailure = onFailure
	return options
}

func (f *ProjectFlow) GetProject(ctx dex.Context, _ dex.None) (*dex.RPCResult[Project], error) {
	return &dex.RPCResult[Project]{Output: Project{
		Identity:            comm.Must(ProjectStartIdentity.Get(ctx)),
		ProjectID:           comm.Must(ProjectID.Get(ctx)),
		CreatorAccountID:    comm.Must(ProjectCreatorAccountID.Get(ctx)),
		Name:                comm.Must(ProjectName.Get(ctx)),
		TemplateRevision:    comm.Must(ProjectTemplateRevision.Get(ctx)),
		SetupFlowID:         comm.Must(ProjectSetupFlowID.Get(ctx)),
		TeamSpaceID:         comm.Must(ProjectTeamSpaceID.Get(ctx)),
		ProjectStatus:       comm.Must(ProjectStatus.Get(ctx)),
		RepositoryBinding:   comm.Must(ProjectRepositoryBinding.Get(ctx)),
		AppManifest:         dexflow.Optional(ProjectAppManifest.Get(ctx)),
		ActiveDeployment:    dexflow.Optional(ProjectActiveDeployment.Get(ctx)),
		CurrentLive:         dexflow.Optional(ProjectCurrentLive.Get(ctx)),
		Storage:             comm.Must(ProjectStorage.Get(ctx)),
		LiveExecution:       comm.Must(ProjectLiveExecution.Get(ctx)),
		ActiveCommand:       dexflow.Optional(ProjectActiveCommand.Get(ctx)),
		LastCommand:         dexflow.Optional(ProjectLastCommand.Get(ctx)),
		LastFailure:         dexflow.Optional(ProjectLastFailure.Get(ctx)),
		SessionStartFailure: dexflow.Optional(ProjectSessionStartFailure.Get(ctx)),
		AttachmentTotals:    dexflow.ValueOrZero(ProjectAttachmentTotals.Get(ctx)),
		Revision:            comm.Must(ProjectRevision.Get(ctx)),
		CreatedAt:           comm.Must(ProjectCreatedAt.Get(ctx)),
		UpdatedAt:           comm.Must(ProjectUpdatedAt.Get(ctx)),
	}}, nil
}

// GetChildFlow exact-reads one admission. A child that is not registered reads as
// the zero reference, which callers compare; only a failed read is an error.
func (f *ProjectFlow) GetChildFlow(ctx dex.Context, flowID string) (*dex.RPCResult[ChildFlowReference], error) {
	child, err := ChildFlows.Get(ctx, flowID)
	if err != nil && !dexflow.IsAttributeNotFound(err) {
		return nil, err
	}
	return &dex.RPCResult[ChildFlowReference]{Output: child}, nil
}

// dex:field attribute-key:Name value-type:string editable:false description:"Project name"
func (*ProjectFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"Name": comm.Must(ProjectName.Get(ctx))}}, nil
}

// dex:field attribute-key:Name value-type:string editable:false description:"Project name"
// dex:field attribute-key:ProjectID value-type:string editable:false description:"Project identity"
func (*ProjectFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"Name": comm.Must(ProjectName.Get(ctx)), "ProjectID": comm.Must(ProjectID.Get(ctx))}}, nil
}

// CommitProjectSetup accepts Setup's binding and manifest and makes the Project READY, which starts
// its Session (StartProjectSession); only its own Setup commits, once: a repeat starts nothing.
func (f *ProjectFlow) CommitProjectSetup(ctx dex.Context, in SetupCommit) (*dex.RPCResult[projectResult], error) {
	p := f.readProject(ctx)
	if !p.AdmitsChildren() || p.SetupFlowID != in.SetupFlowID {
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "setup no longer owns this project")
	}
	if !f.Storage.Complete(p.Storage) {
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "project configuration storage is incomplete")
	}
	// The input is Setup's persisted state; the binding rule is an ownership check.
	if in.Repository.Generation == 0 {
		return f.reject(ctx, dexflow.RejectionInvalidArgument, "setup repository binding does not belong to the project")
	}
	if p.ProjectStatus == StatusReady {
		if p.RepositoryBinding != in.Repository {
			return f.reject(ctx, dexflow.RejectionAlreadyExists, "setup result conflicts")
		}
		return f.result(ctx, nil)
	}
	comm.MustDo(ProjectRepositoryBinding.Set(ctx, in.Repository))
	comm.MustDo(ProjectAppManifest.Set(ctx, in.AppManifest))
	comm.MustDo(ProjectStatus.Set(ctx, StatusReady))
	// StartFlow may have been accepted even when every start response was lost.
	// Only Setup's authoritative successful handoff clears that start failure.
	if p.LastFailure != nil && p.LastFailure.Phase == failureSetupStartPhase {
		comm.MustDo(ProjectLastFailure.Set(ctx, nil))
	}
	touchProject(ctx)
	return &dex.RPCResult[projectResult]{Output: projectResult{Project: f.readProject(ctx)}, NextSteps: []dex.StepMovement{dex.MovementOf(StartProjectSession{}, nil)}}, nil
}

// RecordProjectStorage transfers the storage identities Setup discovered to the permanent owner;
// it stays available during deletion so an in-flight Setup keeps its cleanup responsibility.
func (f *ProjectFlow) RecordProjectStorage(ctx dex.Context, in ProjectStorageUpdate) (*dex.RPCResult[projectResult], error) {
	p := f.readProject(ctx)
	if p.ProjectStatus == StatusDeleted || p.SetupFlowID != in.SetupFlowID {
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "setup no longer owns resource admission")
	}
	if p.Storage == in.Storage {
		return f.result(ctx, nil)
	}
	if p.Storage != in.Expected {
		return f.reject(ctx, dexflow.RejectionAborted, "project storage identity changed")
	}
	if !storageExtends(p.Storage, in.Storage) {
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "owned resource identity cannot be replaced")
	}
	comm.MustDo(ProjectStorage.Set(ctx, in.Storage))
	touchProject(ctx)
	return f.result(ctx, nil)
}

// RegisterChildFlow admits a child before any effect and owns which FlowIDs belong to the Project;
// admission ends when deletion begins, and the Session's admission clears a recorded start failure.
func (f *ProjectFlow) RegisterChildFlow(ctx dex.Context, in ChildFlowReference) (*dex.RPCResult[childAdmissionResult], error) {
	projectID := comm.Must(ProjectID.Get(ctx))
	status := comm.Must(ProjectStatus.Get(ctx))
	if status == StatusDeleting || status == StatusDeleted {
		return childRejected(dexflow.RejectionFailedPrecondition, "project admission is frozen")
	}
	child, known := childFlows[in.FlowType]
	if !known || !child.belongs(projectID, in.FlowID) {
		return childRejected(dexflow.RejectionInvalidArgument, "child identity does not belong to project")
	}
	existing, err := ChildFlows.Get(ctx, in.FlowID)
	if err == nil {
		if existing != in {
			return childRejected(dexflow.RejectionAlreadyExists, "child admission conflicts")
		}
		return &dex.RPCResult[childAdmissionResult]{}, nil
	}
	if !dexflow.IsAttributeNotFound(err) {
		return nil, err
	}
	comm.MustDo(ChildFlows.Set(ctx, in.FlowID, in))
	if child == ChildSession && dexflow.Optional(ProjectSessionStartFailure.Get(ctx)) != nil {
		comm.MustDo(ProjectSessionStartFailure.Set(ctx, nil))
	}
	touchProject(ctx)
	return &dex.RPCResult[childAdmissionResult]{}, nil
}

// AcknowledgeChildCleanup removes a child whose cleanup completed. A repeated
// acknowledgement finds nothing to remove.
func (f *ProjectFlow) AcknowledgeChildCleanup(ctx dex.Context, in ChildFlowReference) (*dex.RPCResult[childAdmissionResult], error) {
	existing, err := ChildFlows.Get(ctx, in.FlowID)
	if dexflow.IsAttributeNotFound(err) {
		return &dex.RPCResult[childAdmissionResult]{}, nil
	}
	if err != nil {
		return nil, err
	}
	if existing != in {
		return childRejected(dexflow.RejectionAborted, "child admission changed")
	}
	comm.MustDo(ChildFlows.Delete(ctx, in.FlowID))
	touchProject(ctx)
	return &dex.RPCResult[childAdmissionResult]{}, nil
}

func (f *ProjectFlow) RequestLiveRemoval(ctx dex.Context, in ProjectCommand) (*dex.RPCResult[projectResult], error) {
	in.Kind = CommandRemoveLive
	return f.requestCommand(ctx, in)
}

func (f *ProjectFlow) RequestProjectDeletion(ctx dex.Context, in ProjectCommand) (*dex.RPCResult[projectResult], error) {
	in.Kind = CommandDeleteProject
	return f.requestCommand(ctx, in)
}

// ReserveProjectAttachments counts a READY Project's message attachments against its lifetime quota under the
// admission's hold; one reserved already is held again, uncounted, and only the client's named instances load.
func (f *ProjectFlow) ReserveProjectAttachments(ctx dex.Context, in AttachmentReservation) (*dex.RPCResult[attachmentResult], error) {
	if comm.Must(ProjectStatus.Get(ctx)) != StatusReady {
		return attachmentRejected(dexflow.RejectionFailedPrecondition, "the Project accepts no attachments: it is not READY")
	}
	totals, now := dexflow.ValueOrZero(ProjectAttachmentTotals.Get(ctx)), time.Now().UTC()
	ids, changed := []string{}, map[string]ProjectAttachment{}
	for _, file := range in.Files {
		reserved, err := ProjectAttachments.Get(ctx, file.ID)
		switch {
		case err == nil:
			if held, isChanged := reserved.held(in.AdmissionID); isChanged {
				ids, changed[file.ID] = append(ids, file.ID), held
			}
		case dexflow.IsAttributeNotFound(err):
			ids, changed[file.ID] = append(ids, file.ID), projectAttachment(file, in.MessageID, in.AdmissionID, now)
			totals.Count, totals.Bytes = totals.Count+1, totals.Bytes+file.Bytes
		default:
			return nil, err
		}
	}
	if totals.Count > f.Attachments.Files || totals.Bytes > f.Attachments.Bytes {
		return attachmentRejected(dexflow.RejectionResourceExhausted, fmt.Sprintf("a Project's messages send at most %d attachments and %d bytes over its lifetime", f.Attachments.Files, f.Attachments.Bytes))
	}
	for _, id := range ids {
		comm.MustDo(ProjectAttachments.Set(ctx, id, changed[id]))
	}
	comm.MustDo(ProjectAttachmentTotals.Set(ctx, totals))
	return &dex.RPCResult[attachmentResult]{}, nil
}

// ReleaseProjectAttachments drops, after a definite refusal of a message admission, that admission's holds; an
// attachment no admission holds any more and that is not kept is freed, and a repeat finds nothing to drop.
func (f *ProjectFlow) ReleaseProjectAttachments(ctx dex.Context, in attachmentRelease) (*dex.RPCResult[attachmentResult], error) {
	totals := dexflow.ValueOrZero(ProjectAttachmentTotals.Get(ctx))
	for _, id := range in.AttachmentIDs {
		reserved, err := ProjectAttachments.Get(ctx, id)
		if dexflow.IsAttributeNotFound(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		switch remaining, isChanged, isFree := reserved.released(in.AdmissionID); {
		case isFree:
			comm.MustDo(ProjectAttachments.Delete(ctx, id))
			totals.Count, totals.Bytes = max(totals.Count-1, 0), max(totals.Bytes-reserved.Bytes, 0)
		case isChanged:
			comm.MustDo(ProjectAttachments.Set(ctx, id, remaining))
		}
	}
	comm.MustDo(ProjectAttachmentTotals.Set(ctx, totals))
	return &dex.RPCResult[attachmentResult]{}, nil
}

func attachmentRejected(code dexflow.RejectionCode, message string) (*dex.RPCResult[attachmentResult], error) {
	return &dex.RPCResult[attachmentResult]{Output: attachmentResult{Rejection: dexflow.Reject(code, message)}}, nil
}

// AcquireDeployment leases Live to one deployment at a time; the lease names the Live revision it
// replaces, so a deployment admitted against an older Live cannot take it.
func (f *ProjectFlow) AcquireDeployment(ctx dex.Context, in DeploymentLease) (*dex.RPCResult[projectResult], error) {
	p := f.readProject(ctx)
	if p.ProjectStatus != StatusReady {
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "project is not ready")
	}
	if p.ActiveCommand != nil && p.ActiveCommand.Kind == CommandRemoveLive {
		return f.reject(ctx, dexflow.RejectionAborted, "Live removal is in progress")
	}
	if p.ActiveDeployment != nil {
		if p.ActiveDeployment.DeploymentID == in.DeploymentID && p.ActiveDeployment.ExpectedLiveRevision == in.ExpectedLiveRevision {
			return f.result(ctx, nil)
		}
		return f.reject(ctx, dexflow.RejectionAborted, "deployment is in progress")
	}
	live := uint64(0)
	if p.CurrentLive != nil {
		live = p.CurrentLive.Revision
	}
	if in.ExpectedLiveRevision != live {
		return f.result(ctx, dexflow.RejectStale("Live generation changed"))
	}
	in.Generation = p.Revision + 1
	comm.MustDo(ProjectActiveDeployment.Set(ctx, &in))
	touchProject(ctx)
	return f.result(ctx, nil)
}

// RecordLiveExecution transfers the Live execution identities the lease holder discovered; it stays
// available during deletion, and Live removal leaves them for the next deployment.
func (f *ProjectFlow) RecordLiveExecution(ctx dex.Context, in LiveExecutionUpdate) (*dex.RPCResult[projectResult], error) {
	p := f.readProject(ctx)
	if (p.ProjectStatus != StatusReady && p.ProjectStatus != StatusDeleting) || !f.Storage.Complete(p.Storage) {
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "project admits no Live execution environment")
	}
	if p.ActiveDeployment == nil || *p.ActiveDeployment != in.Lease {
		return f.reject(ctx, dexflow.RejectionAborted, "deployment lease changed")
	}
	if p.LiveExecution == in.Execution {
		return f.result(ctx, nil)
	}
	if p.LiveExecution != in.Expected {
		return f.reject(ctx, dexflow.RejectionAborted, "Live execution identity changed")
	}
	if !executionExtends(p.LiveExecution, in.Execution) {
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "owned resource identity cannot be replaced")
	}
	comm.MustDo(ProjectLiveExecution.Set(ctx, in.Execution))
	touchProject(ctx)
	return f.result(ctx, nil)
}

// CommitLiveDeployment makes the leased, verified deployment the Project's
// Live, which runs on the complete Live execution environment.
func (f *ProjectFlow) CommitLiveDeployment(ctx dex.Context, in DeploymentCommit) (*dex.RPCResult[projectResult], error) {
	p := f.readProject(ctx)
	if p.ProjectStatus != StatusReady || p.ActiveDeployment == nil || *p.ActiveDeployment != in.Lease {
		return f.reject(ctx, dexflow.RejectionAborted, "deployment lease changed")
	}
	if !in.Pointer.Ready || in.Pointer.DeploymentID != in.Lease.DeploymentID || in.Pointer.Revision != in.Lease.Generation {
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "deployment is not verified ready")
	}
	if !p.LiveExecution.Complete() {
		return f.reject(ctx, dexflow.RejectionFailedPrecondition, "the Live execution environment is incomplete")
	}
	comm.MustDo(ProjectCurrentLive.Set(ctx, &in.Pointer))
	touchProject(ctx)
	return f.result(ctx, nil)
}

// ReleaseDeployment ends exactly the given lease; another lease is ABORTED.
func (f *ProjectFlow) ReleaseDeployment(ctx dex.Context, in DeploymentLease) (*dex.RPCResult[projectResult], error) {
	p := f.readProject(ctx)
	if p.ActiveDeployment == nil {
		return f.result(ctx, nil)
	}
	if *p.ActiveDeployment != in {
		return f.reject(ctx, dexflow.RejectionAborted, "deployment lease changed")
	}
	comm.MustDo(ProjectActiveDeployment.Set(ctx, nil))
	touchProject(ctx)
	return f.result(ctx, nil)
}

// AddProjectMember adds a member as an owner or the platform admin; the API checked that the
// account belongs to the Project's TeamSpace.
func (f *ProjectFlow) AddProjectMember(ctx dex.Context, in MemberChange) (*dex.RPCResult[MemberUpdate], error) {
	now := time.Now().UTC()
	return f.changeMembers(ctx, in, func(members map[string]ProjectMember) memberOutcome { return addMember(members, in, now) })
}

// SetProjectMemberRole changes a member's role, keeping at least one owner.
func (f *ProjectFlow) SetProjectMemberRole(ctx dex.Context, in MemberChange) (*dex.RPCResult[MemberUpdate], error) {
	return f.changeMembers(ctx, in, func(members map[string]ProjectMember) memberOutcome { return setMemberRole(members, in) })
}

// RemoveProjectMember removes a member, keeping at least one owner.
func (f *ProjectFlow) RemoveProjectMember(ctx dex.Context, in MemberChange) (*dex.RPCResult[MemberUpdate], error) {
	return f.changeMembers(ctx, in, func(members map[string]ProjectMember) memberOutcome { return removeMember(members, in) })
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Register the Project in its TeamSpace before any effect, as its creator."
type RegisterProjectInTeamSpace struct {
	dex.StepDefaultsNoWaitFor[teamSpaceAdmission]
	flow *ProjectFlow
}

func (s RegisterProjectInTeamSpace) GetStepOptions() *dex.StepOptions {
	return providerStepOptions(dex.ProceedToOnExecuteFailure(RecordTeamSpaceAdmissionRejection{flow: s.flow}, nil))
}

// Execute registers a CREATING Project; one whose deletion began meanwhile goes on to that
// deletion, whose last phase removes any registration.
func (s RegisterProjectInTeamSpace) Execute(ctx dex.Context, in teamSpaceAdmission) (*dex.StepDecision, error) {
	if project := s.flow.readProject(ctx); project.ProjectStatus == StatusCreating {
		creator := teamspaces.Actor{AccountID: project.CreatorAccountID, PlatformAdmin: in.CreatorIsPlatformAdmin}
		err := s.flow.TeamSpaces.RegisterProject(ctx, project.TeamSpaceID, project.ProjectID, creator)
		if rejected, isRejected := errors.AsType[*dexflow.Rejection](err); isRejected {
			in.Rejection = rejected
			return dex.GoTo(RecordTeamSpaceAdmissionRejection{flow: s.flow}, in), nil
		}
		if err != nil {
			return nil, dex.ErrorWithStack(fmt.Errorf("register Project %s in TeamSpace %s: %w", project.ProjectID, project.TeamSpaceID, err))
		}
	}
	return dex.GoTo(StartProjectSetup{flow: s.flow}, nil), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Record the TeamSpace's refusal or an unconfirmed registration, and end the Project, which created nothing yet."
type RecordTeamSpaceAdmissionRejection struct {
	dex.StepDefaultsNoWaitFor[teamSpaceAdmission]
	flow *ProjectFlow
}

func (RecordTeamSpaceAdmissionRejection) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(ProjectRevision)}
	return options
}

// Execute ends the Project DELETED; a deletion admitted during the registration completes with it,
// and AwaitProjectCommands drains its Channel message before the Flow completes.
func (s RecordTeamSpaceAdmissionRejection) Execute(ctx dex.Context, in teamSpaceAdmission) (*dex.StepDecision, error) {
	if project := s.flow.readProject(ctx); project.ProjectStatus == StatusCreating || project.ProjectStatus == StatusDeleting {
		comm.MustDo(ProjectLastFailure.Set(ctx, teamSpaceAdmissionFailure(ctx, in.Rejection)))
		comm.MustDo(ProjectStatus.Set(ctx, StatusDeleted))
		if command := project.ActiveCommand; command != nil {
			command.Phase = phaseComplete
			comm.MustDo(ProjectLastCommand.Set(ctx, command))
			comm.MustDo(ProjectActiveCommand.Set(ctx, nil))
		}
		touchProject(ctx)
	}
	return dex.ForceCompleteIfChannelsEmpty(s.flow.readProject(ctx), []dex.ChannelDef{ProjectCommands}, dex.MovementOf(AwaitProjectCommands{flow: s.flow}, nil)), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Start Setup from accepted Project identity before entering command waits."
type StartProjectSetup struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectFlow
}

func (s StartProjectSetup) GetStepOptions() *dex.StepOptions {
	return providerStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectSetupStartFailure{flow: s.flow}, nil))
}

func (s StartProjectSetup) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if project := s.flow.readProject(ctx); project.ProjectStatus == StatusCreating {
		if err := s.flow.Setups.StartForProject(ctx, project); err != nil {
			return nil, dex.ErrorWithStack(fmt.Errorf("start Setup %s: %w", project.SetupFlowID, err))
		}
	}
	return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Retain an uncertain Setup start without preventing Project deletion."
type RecordProjectSetupStartFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectFlow
}

func (RecordProjectSetupStartFailure) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(ProjectRevision)}
	return options
}

func (s RecordProjectSetupStartFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if comm.Must(ProjectStatus.Get(ctx)) == StatusCreating {
		comm.MustDo(ProjectStatus.Set(ctx, statusSetupFailed))
		message := dexflow.FailureMessage("Setup start could not be confirmed. Delete the project to reconcile its resources.", dexflow.RecoveryDetail(ctx))
		comm.MustDo(ProjectLastFailure.Set(ctx, &dexflow.Failure{Code: failureSetupStart, Phase: failureSetupStartPhase, Message: message, Remedy: dexflow.RemedyContactOperator}))
		touchProject(ctx)
	}
	return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Start the READY Project's one Session; a retried or replayed start joins its execution."
type StartProjectSession struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectFlow
}

func (s StartProjectSession) GetStepOptions() *dex.StepOptions {
	return providerStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectSessionStartFailure{flow: s.flow}, nil))
}

// Execute makes the Session's one StartFlow, idempotent by FlowID and RequestID, with the creator's request,
// which this commit clears; a Project no longer READY because its deletion began starts none.
func (s StartProjectSession) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if project := s.flow.readProject(ctx); project.ProjectStatus == StatusReady {
		initial := dexflow.OptionalValue(ProjectInitialSessionRequest.Get(ctx))
		if err := s.flow.Sessions.StartForProject(ctx, project, initial); err != nil {
			return nil, dex.ErrorWithStack(fmt.Errorf("start the Session of Project %s: %w", project.ProjectID, err))
		}
		if initial != nil {
			comm.MustDo(ProjectInitialSessionRequest.Delete(ctx))
		}
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Record a Session start whose retries ran out; the Project stays READY, and the Session's admission clears the failure."
type RecordProjectSessionStartFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *ProjectFlow
}

func (RecordProjectSessionStartFailure) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(ProjectRevision)}
	options.ExecuteLoadAttributeMaps = []dex.AttributeDef{ChildFlows}
	return options
}

// Execute records SessionStartFailure (not the command-owned LastFailure) on a READY Project whose
// Session has not admitted itself; a start whose responses were all lost registers it and records none.
func (s RecordProjectSessionStartFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	project := s.flow.readProject(ctx)
	session, err := ChildFlows.Get(ctx, flowid.SessionFlowID(project.ProjectID))
	if err != nil && !dexflow.IsAttributeNotFound(err) {
		return nil, dex.ErrorWithStack(fmt.Errorf("read the Session membership of Project %s: %w", project.ProjectID, err))
	}
	if project.ProjectStatus == StatusReady && session.FlowID == "" {
		message := dexflow.FailureMessage("The Project's agent Session could not be started. An operator must start it again.", dexflow.RecoveryDetail(ctx))
		comm.MustDo(ProjectSessionStartFailure.Set(ctx, &dexflow.Failure{Code: failureSessionStart, Phase: failureSessionStartPhase, Message: message, Remedy: dexflow.RemedyContactOperator}))
		touchProject(ctx)
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Wait for an admitted Live removal or deletion command; both start by removing Live serving."
type AwaitProjectCommands struct {
	dex.StepDefaults
	flow *ProjectFlow
}

func (AwaitProjectCommands) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (AwaitProjectCommands) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	return dex.Until(ProjectCommands.ForOne()), nil
}

func (s AwaitProjectCommands) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	command := comm.Must(ProjectCommands.GetConditionResults(ctx))[0]
	// A Project the TeamSpace refused is DELETED already; the command admitted meanwhile ends with it.
	if project := s.flow.readProject(ctx); project.ProjectStatus == StatusDeleted {
		return dex.ForceCompleteIfChannelsEmpty(project, []dex.ChannelDef{ProjectCommands}, dex.MovementOf(AwaitProjectCommands{flow: s.flow}, nil)), nil
	}
	return dex.GoTo(RemoveLiveServing{flow: s.flow}, projectPhaseInput{Command: command}), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Advance the active command to its next phase, or complete it."
type CommitProjectPhase struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (CommitProjectPhase) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(ProjectRevision)}
	return options
}

func (s CommitProjectPhase) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	if !s.flow.readProject(ctx).runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	phases := projectPhases[in.Command.Kind]
	in.Phase++
	command := in.Command
	if in.Phase == len(phases) {
		command.Phase = phaseComplete
		comm.MustDo(ProjectLastCommand.Set(ctx, &command))
		comm.MustDo(ProjectActiveCommand.Set(ctx, nil))
		touchProject(ctx)
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	command.Phase = phases[in.Phase]
	comm.MustDo(ProjectActiveCommand.Set(ctx, &command))
	touchProject(ctx)
	switch command.Phase {
	case phaseRevokeDeploymentGrant:
		return dex.GoTo(RevokeDeploymentGrant{flow: s.flow}, in), nil
	case phaseCommitLiveAbsent:
		return dex.GoTo(CommitLiveAbsent{flow: s.flow}, in), nil
	case phaseStopAndCleanChildren:
		return dex.GoTo(StopAndCleanChildren{flow: s.flow}, in), nil
	case phaseDestroyLiveDataAndInfrastructure:
		return dex.GoTo(DestroyLiveDataAndInfrastructure{flow: s.flow}, in), nil
	case phaseReleaseConnectionUsages:
		return dex.GoTo(ReleaseConnectionUsages{flow: s.flow}, in), nil
	case phaseReleaseRepositoryBinding:
		return dex.GoTo(ReleaseRepositoryBinding{flow: s.flow}, in), nil
	case phaseArchiveManagedRepository:
		return dex.GoTo(ArchiveManagedRepository{flow: s.flow}, in), nil
	case phaseCompleteDeletion:
		return dex.GoTo(CompleteDeletion{flow: s.flow}, in), nil
	}
	panic("project phase " + command.Phase + " has no Step")
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Retain a failed command phase and its safe Failure until the command is retried."
type RecordProjectFailure struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (RecordProjectFailure) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(ProjectRevision)}
	return options
}

func (s RecordProjectFailure) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	if !s.flow.readProject(ctx).runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	// A classified failure wins; an Execute failure arrives without one.
	phase := projectPhases[in.Command.Kind][in.Phase]
	failure := executeFailure(ctx, phase)
	if in.Failure != nil {
		failure = *in.Failure
	}
	failure.Phase = phase
	command := in.Command
	command.Phase = phaseFailed
	comm.MustDo(ProjectLastFailure.Set(ctx, &failure))
	comm.MustDo(ProjectActiveCommand.Set(ctx, &command))
	touchProject(ctx)
	return dex.DeadEnd(), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Remove the Live serving resources and wait until they are gone."
type RemoveLiveServing struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (s RemoveLiveServing) GetStepOptions() *dex.StepOptions {
	return cleanupStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectFailure{flow: s.flow}, nil))
}

func (s RemoveLiveServing) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	project := s.flow.readProject(ctx)
	if !project.runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, ProjectProgress.Write(ctx, dexflow.Progress(ctx, phaseRemoveLiveServing, project.Revision)))
	failure, err := awaitPhase(ctx, "remove Live serving", func(callCtx context.Context) error {
		return s.flow.removeLiveServing(callCtx, project)
	})
	if err != nil {
		return nil, err
	}
	if failure != nil {
		in.Failure = failure
		return dex.GoTo(RecordProjectFailure{flow: s.flow}, in), nil
	}
	return dex.GoTo(CommitProjectPhase{flow: s.flow}, in), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Revoke the Live deployment's workload grant and wait until revocation completes."
type RevokeDeploymentGrant struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (s RevokeDeploymentGrant) GetStepOptions() *dex.StepOptions {
	return cleanupStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectFailure{flow: s.flow}, nil))
}

func (s RevokeDeploymentGrant) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	project := s.flow.readProject(ctx)
	if !project.runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, ProjectProgress.Write(ctx, dexflow.Progress(ctx, phaseRevokeDeploymentGrant, project.Revision)))
	failure, err := awaitPhase(ctx, "revoke the Live deployment grant", func(callCtx context.Context) error {
		return s.flow.revokeDeploymentGrant(callCtx, project)
	})
	if err != nil {
		return nil, err
	}
	if failure != nil {
		in.Failure = failure
		return dex.GoTo(RecordProjectFailure{flow: s.flow}, in), nil
	}
	return dex.GoTo(CommitProjectPhase{flow: s.flow}, in), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Clear the Project's Live pointer once Live serving and its grant are gone."
type CommitLiveAbsent struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (s CommitLiveAbsent) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(ProjectRevision)}
	options.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordProjectFailure{flow: s.flow}, nil)
	return options
}

func (s CommitLiveAbsent) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	if !s.flow.readProject(ctx).runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	comm.MustDo(ProjectCurrentLive.Set(ctx, nil))
	touchProject(ctx)
	dexflow.StreamWritten(ctx, ProjectProgress.Write(ctx, dexflow.Progress(ctx, phaseCommitLiveAbsent, s.flow.readProject(ctx).Revision)))
	return dex.GoTo(CommitProjectPhase{flow: s.flow}, in), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Clean up and acknowledge registered children in rotating batches until membership is empty."
type StopAndCleanChildren struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (s StopAndCleanChildren) GetStepOptions() *dex.StepOptions {
	options := cleanupStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectFailure{flow: s.flow}, nil))
	options.ExecuteLoadAttributeMaps = []dex.AttributeDef{ChildFlows}
	return options
}

func (s StopAndCleanChildren) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	project := s.flow.readProject(ctx)
	if !project.runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	// A deleting Project admits no child, so the membership loaded here holds
	// every child left to clean up; each round drops the acknowledged ones.
	children := make([]ChildFlowReference, 0, len(ChildFlows.AllInstanceKeys(ctx)))
	for _, key := range ChildFlows.AllInstanceKeys(ctx) {
		children = append(children, comm.Must(ChildFlows.Get(ctx, key)))
	}
	sort.Slice(children, func(i, j int) bool { return children[i].FlowID < children[j].FlowID })
	dexflow.StreamWritten(ctx, ProjectProgress.Write(ctx, dexflow.Progress(ctx, phaseStopAndCleanChildren, project.Revision)))
	failure, err := awaitPhase(ctx, "stop and clean up children", func(callCtx context.Context) error {
		// A failed round, including one its timeout cut off, keeps the
		// membership it started with for the next round.
		remaining, err := s.flow.stopAndCleanChildren(callCtx, project, children)
		if err != nil {
			return err
		}
		children = remaining
		if len(children) > 0 {
			return fmt.Errorf("%w: %d children remain, first %s", ErrCleanupPending, len(children), children[0].FlowID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if failure != nil {
		in.Failure = failure
		return dex.GoTo(RecordProjectFailure{flow: s.flow}, in), nil
	}
	return dex.GoTo(CommitProjectPhase{flow: s.flow}, in), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Destroy the Live project infrastructure and delete the Project's stored objects, waiting until both are gone."
type DestroyLiveDataAndInfrastructure struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (s DestroyLiveDataAndInfrastructure) GetStepOptions() *dex.StepOptions {
	return cleanupStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectFailure{flow: s.flow}, nil))
}

func (s DestroyLiveDataAndInfrastructure) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	project := s.flow.readProject(ctx)
	if !project.runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, ProjectProgress.Write(ctx, dexflow.Progress(ctx, phaseDestroyLiveDataAndInfrastructure, project.Revision)))
	failure, err := awaitPhase(ctx, "destroy Live data and infrastructure", func(callCtx context.Context) error {
		return s.flow.destroyLiveDataAndInfrastructure(callCtx, project)
	})
	if err != nil {
		return nil, err
	}
	if failure != nil {
		in.Failure = failure
		return dex.GoTo(RecordProjectFailure{flow: s.flow}, in), nil
	}
	return dex.GoTo(CommitProjectPhase{flow: s.flow}, in), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Delete the Project's connection usages and scope tokens, so its applications read no credential."
type ReleaseConnectionUsages struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (s ReleaseConnectionUsages) GetStepOptions() *dex.StepOptions {
	return providerStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectFailure{flow: s.flow}, nil))
}

func (s ReleaseConnectionUsages) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	project := s.flow.readProject(ctx)
	if !project.runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, ProjectProgress.Write(ctx, dexflow.Progress(ctx, phaseReleaseConnectionUsages, project.Revision)))
	if err := s.flow.Connections.ReleaseConnectionUsages(ctx, project.ProjectID); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("release the connection usages of Project %s: %w", project.ProjectID, err))
	}
	return dex.GoTo(CommitProjectPhase{flow: s.flow}, in), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Archive the owned managed repository."
type ArchiveManagedRepository struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (s ArchiveManagedRepository) GetStepOptions() *dex.StepOptions {
	return cleanupStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectFailure{flow: s.flow}, nil))
}

func (s ArchiveManagedRepository) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	project := s.flow.readProject(ctx)
	if !project.runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, ProjectProgress.Write(ctx, dexflow.Progress(ctx, phaseArchiveManagedRepository, project.Revision)))
	failure, err := awaitRepositoryArchive(ctx, func(callCtx context.Context) error {
		return s.flow.archiveManagedRepository(callCtx, project)
	})
	if err != nil {
		return nil, err
	}
	if failure != nil {
		in.Failure = failure
		return dex.GoTo(RecordProjectFailure{flow: s.flow}, in), nil
	}
	return dex.GoTo(CommitProjectPhase{flow: s.flow}, in), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"End the archived managed repository's exclusive binding, which completes the binding's Flow."
type ReleaseRepositoryBinding struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (s ReleaseRepositoryBinding) GetStepOptions() *dex.StepOptions {
	return providerStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectFailure{flow: s.flow}, nil))
}

func (s ReleaseRepositoryBinding) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	project := s.flow.readProject(ctx)
	if !project.runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	dexflow.StreamWritten(ctx, ProjectProgress.Write(ctx, dexflow.Progress(ctx, phaseReleaseRepositoryBinding, project.Revision)))
	failure, err := classifyPhaseError("release the repository binding", s.flow.releaseRepositoryBinding(ctx, project))
	if err != nil {
		return nil, err
	}
	if failure != nil {
		in.Failure = failure
		return dex.GoTo(RecordProjectFailure{flow: s.flow}, in), nil
	}
	return dex.GoTo(CommitProjectPhase{flow: s.flow}, in), nil
}

// dex:group group-id:project group-label:"Project"
// dex:explanation text:"Prove cleanup membership empty and atomically complete project deletion."
type CompleteDeletion struct {
	dex.StepDefaultsNoWaitFor[projectPhaseInput]
	flow *ProjectFlow
}

func (s CompleteDeletion) GetStepOptions() *dex.StepOptions {
	options := providerStepOptions(dex.ProceedToOnExecuteFailure(RecordProjectFailure{flow: s.flow}, nil))
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(ProjectRevision)}
	options.ExecuteLoadAttributeMaps = []dex.AttributeDef{ChildFlows}
	return options
}

func (s CompleteDeletion) Execute(ctx dex.Context, in projectPhaseInput) (*dex.StepDecision, error) {
	project := s.flow.readProject(ctx)
	dexflow.StreamWritten(ctx, ProjectProgress.Write(ctx, dexflow.Progress(ctx, phaseCompleteDeletion, project.Revision)))
	if !project.runsCommand(in.Command) {
		return dex.GoTo(AwaitProjectCommands{flow: s.flow}, nil), nil
	}
	// Deletion is irreversible: the persisted command must be exactly at its final phase.
	if project.ProjectStatus != StatusDeleting || project.ActiveCommand.Kind != CommandDeleteProject || project.ActiveCommand.Phase != phaseCompleteDeletion ||
		in.Command.Kind != CommandDeleteProject || in.Phase != len(projectPhases[CommandDeleteProject])-1 {
		return nil, dex.ErrorWithStack(fmt.Errorf("final deletion of project %s is not admitted in status %s at phase %s", project.ProjectID, project.ProjectStatus, project.ActiveCommand.Phase))
	}
	if remaining := len(ChildFlows.AllInstanceKeys(ctx)); remaining != 0 {
		return nil, dex.ErrorWithStack(fmt.Errorf("%d project children have not acknowledged their cleanup", remaining))
	}
	if err := s.flow.TeamSpaces.UnregisterProject(ctx, project.TeamSpaceID, project.ProjectID); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("unregister Project %s from TeamSpace %s: %w", project.ProjectID, project.TeamSpaceID, err))
	}
	comm.MustDo(ProjectStatus.Set(ctx, StatusDeleted))
	touchProject(ctx)
	return dex.GracefulComplete(s.flow.readProject(ctx)), nil
}

// changeMembers admits a member write on the whole loaded Members map, applies change and
// writes the changed instance; it answers with the members after the write.
func (f *ProjectFlow) changeMembers(ctx dex.Context, in MemberChange, change func(map[string]ProjectMember) memberOutcome) (*dex.RPCResult[MemberUpdate], error) {
	members := make(map[string]ProjectMember, ProjectMembers.MapSize(ctx))
	for _, accountID := range ProjectMembers.AllInstanceKeys(ctx) {
		members[accountID] = comm.Must(ProjectMembers.Get(ctx, accountID))
	}
	outcome := memberOutcome{rejection: admitMemberChange(f.readProject(ctx), members, in)}
	if outcome.rejection == nil {
		outcome = change(members)
	}
	if outcome.rejection != nil {
		return &dex.RPCResult[MemberUpdate]{Output: MemberUpdate{Project: f.readProject(ctx), Rejection: outcome.rejection}}, nil
	}
	if member, kept := outcome.members[in.AccountID]; outcome.changed && kept {
		comm.MustDo(ProjectMembers.Set(ctx, in.AccountID, member))
	} else if outcome.changed {
		comm.MustDo(ProjectMembers.Delete(ctx, in.AccountID))
	}
	if outcome.changed {
		touchProject(ctx)
	}
	return &dex.RPCResult[MemberUpdate]{Output: MemberUpdate{Project: f.readProject(ctx), Members: memberEntries(outcome.members)}}, nil
}

// readProject reads the snapshot of the invoking execution.
func (f *ProjectFlow) readProject(ctx dex.Context) Project {
	return comm.Must(f.GetProject(ctx, nil)).Output
}

// result answers a Project RPC with the current snapshot and rejection.
func (f *ProjectFlow) result(ctx dex.Context, rejection *dexflow.Rejection) (*dex.RPCResult[projectResult], error) {
	return &dex.RPCResult[projectResult]{Output: projectResult{Project: f.readProject(ctx), Rejection: rejection}}, nil
}

func (f *ProjectFlow) reject(ctx dex.Context, code dexflow.RejectionCode, message string) (*dex.RPCResult[projectResult], error) {
	return f.result(ctx, dexflow.Reject(code, message))
}

func childRejected(code dexflow.RejectionCode, message string) (*dex.RPCResult[childAdmissionResult], error) {
	return &dex.RPCResult[childAdmissionResult]{Output: childAdmissionResult{Rejection: dexflow.Reject(code, message)}}, nil
}

func touchProject(ctx dex.Context) {
	comm.MustDo(ProjectRevision.Set(ctx, comm.Must(ProjectRevision.Get(ctx))+1))
	comm.MustDo(ProjectUpdatedAt.Set(ctx, time.Now().UTC()))
}
