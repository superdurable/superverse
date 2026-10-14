// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package teamspaces

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
)

var (
	TeamSpaceStartIdentity    = dex.DefineAttribute[dexflow.StartIdentity]("StartIdentity")
	TeamSpaceName             = dex.DefineAttribute[string]("Name", dex.SyncToAttributeStore())
	TeamSpaceStatus           = dex.DefineAttribute[string]("TeamSpaceStatus", dex.SyncToAttributeStore())
	TeamSpaceCreatorAccountID = dex.DefineAttribute[string]("CreatorAccountID", dex.SyncToAttributeStore())
	// TeamSpaceRevision is bumped by name, member and status writes and locked by every write.
	TeamSpaceRevision      = dex.DefineAttribute[uint64]("Revision", dex.SyncToAttributeStore())
	TeamSpaceActiveCommand = dex.DefineAttribute[*Command]("ActiveCommand")
	TeamSpaceLastCommand   = dex.DefineAttribute[*Command]("LastCommand")
	TeamSpaceLastFailure   = dex.DefineAttribute[*dexflow.Failure]("LastFailure", dex.SyncToAttributeStore())
	TeamSpaceCreatedAt     = dex.DefineAttribute[time.Time]("CreatedAt", dex.SyncToAttributeStore())
	TeamSpaceUpdatedAt     = dex.DefineAttribute[time.Time]("UpdatedAt", dex.SyncToAttributeStore())
	// TeamSpaceMembers, by AccountID, syncs as the child table team_space_members.
	TeamSpaceMembers = dex.DefineAttributeMap[Member]("Members", dex.SyncToAttributeStore())
	// TeamSpaceProjects, by ProjectID, holds the Projects the TeamSpace's deletion deletes.
	TeamSpaceProjects = dex.DefineAttributeMap[RegisteredProject]("Projects")
	TeamSpaceCommands = dex.DefineChannel[Command]("TeamSpaceCommands")
)

// TeamSpaceFlow is the permanent owner of one TeamSpace (teamspace-<teamSpaceID>): its name,
// members and Project registry, and its deletion, which deletes every registered Project, then its connections.
type TeamSpaceFlow struct {
	dex.FlowDefaults
	Projects    ProjectCleanup
	Connections ConnectionCleanup
}

func (f *TeamSpaceFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(AwaitTeamSpaceCommands{flow: f}), dex.DefineStep(DeleteTeamSpaceResources{flow: f}),
		dex.DefineStep(CompleteTeamSpaceDeletion{flow: f}), dex.DefineStep(RecordTeamSpaceFailure{flow: f}),
	}
}

func (f *TeamSpaceFlow) GetRPCs() []dex.RPCDef {
	read := dexflow.ShortRPC()
	write := dexflow.ShortRPC(dex.LockAttribute(TeamSpaceRevision))
	members := dexflow.ShortRPC(dex.LockAttribute(TeamSpaceRevision))
	members.LoadAttributeMaps = []dex.AttributeDef{TeamSpaceMembers}
	return []dex.RPCDef{
		dex.DefineRPC(f.GetDexSummary, read), dex.DefineRPC(f.GetDexDisplay, read),
		dex.DefineRPC(f.AddTeamSpaceMember, members), dex.DefineRPC(f.SetTeamSpaceMemberRole, members), dex.DefineRPC(f.RemoveTeamSpaceMember, members),
		dex.DefineRPC(f.RenameTeamSpace, write), dex.DefineRPC(f.RequestTeamSpaceDeletion, write),
		dex.DefineRPC(f.RegisterProject, write), dex.DefineRPC(f.UnregisterProject, write),
	}
}

func (f *TeamSpaceFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{
			TeamSpaceStartIdentity, TeamSpaceName, TeamSpaceStatus, TeamSpaceCreatorAccountID, TeamSpaceRevision, TeamSpaceActiveCommand,
			TeamSpaceLastCommand, TeamSpaceLastFailure, TeamSpaceCreatedAt, TeamSpaceUpdatedAt, TeamSpaceMembers, TeamSpaceProjects,
		},
		Channels: []dex.ChannelDef{TeamSpaceCommands},
	}
}

func (f *TeamSpaceFlow) InitialAttributes(in Start) []dex.InitialAttributeDef {
	b := &dexflow.InitialAttributes{}
	now := time.Now().UTC()
	dexflow.Add(b, TeamSpaceStartIdentity, in.Identity)
	dexflow.Add(b, TeamSpaceName, in.Name)
	dexflow.Add(b, TeamSpaceStatus, StatusReady)
	dexflow.Add(b, TeamSpaceCreatorAccountID, in.CreatorAccountID)
	dexflow.Add(b, TeamSpaceRevision, uint64(1))
	dexflow.Add(b, TeamSpaceActiveCommand, nil)
	dexflow.Add(b, TeamSpaceLastCommand, nil)
	dexflow.Add(b, TeamSpaceLastFailure, nil)
	dexflow.Add(b, TeamSpaceCreatedAt, now)
	dexflow.Add(b, TeamSpaceUpdatedAt, now)
	dexflow.AddInstance(b, TeamSpaceMembers, in.CreatorAccountID, Member{Role: RoleAdmin, AddedAt: now, AddedByAccountID: in.CreatorAccountID})
	return b.Values()
}

// dex:field attribute-key:Name value-type:string editable:false description:"TeamSpace name"
func (*TeamSpaceFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"Name": comm.Must(TeamSpaceName.Get(ctx))}}, nil
}

// dex:field attribute-key:Name value-type:string editable:false description:"TeamSpace name"
// dex:field attribute-key:TeamSpaceStatus value-type:string editable:false description:"TeamSpace lifecycle status"
func (*TeamSpaceFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"Name": comm.Must(TeamSpaceName.Get(ctx)), "TeamSpaceStatus": comm.Must(TeamSpaceStatus.Get(ctx))}}, nil
}

// AddTeamSpaceMember adds a member with a role; adding a member who already has that role
// changes nothing.
func (f *TeamSpaceFlow) AddTeamSpaceMember(ctx dex.Context, in MemberChange) (*dex.RPCResult[Update], error) {
	members, rejection := admitMemberWrite(ctx, in.Actor)
	if rejection != nil {
		return f.result(ctx, rejection)
	}
	if existing, found := members[in.AccountID]; found {
		if existing.Role != in.Role {
			return f.result(ctx, dexflow.Reject(dexflow.RejectionAlreadyExists, "the account is already a member with another role"))
		}
		return f.membersResult(ctx, members)
	}
	if len(members) >= maxMembers {
		return f.result(ctx, dexflow.Reject(dexflow.RejectionResourceExhausted, fmt.Sprintf("a TeamSpace holds at most %d members", maxMembers)))
	}
	members[in.AccountID] = Member{Role: in.Role, AddedAt: time.Now().UTC(), AddedByAccountID: in.Actor.AccountID}
	comm.MustDo(TeamSpaceMembers.Set(ctx, in.AccountID, members[in.AccountID]))
	touchTeamSpace(ctx)
	return f.membersResult(ctx, members)
}

// SetTeamSpaceMemberRole changes a member's role, keeping at least one admin.
func (f *TeamSpaceFlow) SetTeamSpaceMemberRole(ctx dex.Context, in MemberChange) (*dex.RPCResult[Update], error) {
	members, rejection := admitMemberWrite(ctx, in.Actor)
	if rejection != nil {
		return f.result(ctx, rejection)
	}
	member, found := members[in.AccountID]
	switch {
	case !found:
		return f.result(ctx, dexflow.Reject(dexflow.RejectionNotFound, "the account is not a member"))
	case member.Role == in.Role:
		return f.membersResult(ctx, members)
	case member.Role == RoleAdmin && admins(members) == 1:
		return f.result(ctx, dexflow.Reject(dexflow.RejectionFailedPrecondition, "a TeamSpace keeps at least one admin"))
	}
	member.Role = in.Role
	members[in.AccountID] = member
	comm.MustDo(TeamSpaceMembers.Set(ctx, in.AccountID, member))
	touchTeamSpace(ctx)
	return f.membersResult(ctx, members)
}

// RemoveTeamSpaceMember removes a member, keeping at least one admin; removing an absent
// member changes nothing. The member's Project roles stay and grant nothing without it.
func (f *TeamSpaceFlow) RemoveTeamSpaceMember(ctx dex.Context, in MemberChange) (*dex.RPCResult[Update], error) {
	members, rejection := admitMemberWrite(ctx, in.Actor)
	if rejection != nil {
		return f.result(ctx, rejection)
	}
	member, found := members[in.AccountID]
	if !found {
		return f.membersResult(ctx, members)
	}
	if member.Role == RoleAdmin && admins(members) == 1 {
		return f.result(ctx, dexflow.Reject(dexflow.RejectionFailedPrecondition, "a TeamSpace keeps at least one admin"))
	}
	delete(members, in.AccountID)
	comm.MustDo(TeamSpaceMembers.Delete(ctx, in.AccountID))
	touchTeamSpace(ctx)
	return f.membersResult(ctx, members)
}

// RenameTeamSpace renames the TeamSpace at the revision the caller observed; the caller's
// Members instance is loaded exactly.
func (f *TeamSpaceFlow) RenameTeamSpace(ctx dex.Context, in Renaming) (*dex.RPCResult[Update], error) {
	t := readTeamSpace(ctx)
	switch {
	case t.Status != StatusReady:
		return f.result(ctx, dexflow.Reject(dexflow.RejectionFailedPrecondition, "the TeamSpace is being deleted"))
	case !in.Actor.mayAdminister(loadedMembers(ctx, in.Actor.AccountID)):
		return f.result(ctx, dexflow.Reject(dexflow.RejectionPermissionDenied, "only a TeamSpace admin renames it"))
	case t.Revision != in.ExpectedRevision:
		return f.result(ctx, dexflow.RejectStale("TeamSpace revision changed"))
	case t.Name == in.Name:
		return f.result(ctx, nil)
	}
	comm.MustDo(TeamSpaceName.Set(ctx, in.Name))
	touchTeamSpace(ctx)
	return f.result(ctx, nil)
}

// RequestTeamSpaceDeletion admits one deletion at a time, only from the platform admin. Its
// ID is the request's: a repeated ID answers with the current state.
func (f *TeamSpaceFlow) RequestTeamSpaceDeletion(ctx dex.Context, in DeletionRequest) (*dex.RPCResult[Update], error) {
	t := readTeamSpace(ctx)
	switch {
	case !in.Actor.PlatformAdmin:
		return f.result(ctx, dexflow.Reject(dexflow.RejectionPermissionDenied, "only the platform admin deletes a TeamSpace"))
	case t.ActiveCommand != nil && t.ActiveCommand.ID == in.ID:
		return f.result(ctx, nil)
	case t.ActiveCommand != nil:
		return f.result(ctx, dexflow.Reject(dexflow.RejectionFailedPrecondition, "the TeamSpace's deletion is in progress"))
	}
	command := Command{ID: in.ID, Kind: commandDeleteTeamSpace, ActorAccountID: in.Actor.AccountID}
	comm.MustDo(TeamSpaceStatus.Set(ctx, StatusDeleting))
	comm.MustDo(TeamSpaceActiveCommand.Set(ctx, &command))
	comm.MustDo(TeamSpaceCommands.Publish(ctx, command))
	touchTeamSpace(ctx)
	return f.result(ctx, nil)
}

// RegisterProject records a Project its creator starts, before any of the Project's effects.
// A registered Project is accepted again whatever the status, so a retry after a lost answer passes.
func (f *TeamSpaceFlow) RegisterProject(ctx dex.Context, in registration) (*dex.RPCResult[registrationResult], error) {
	if _, err := TeamSpaceProjects.Get(ctx, in.ProjectID); err == nil {
		return &dex.RPCResult[registrationResult]{}, nil
	} else if !dexflow.IsAttributeNotFound(err) {
		return nil, err
	}
	switch {
	case comm.Must(TeamSpaceStatus.Get(ctx)) != StatusReady:
		return registrationRejected(dexflow.RejectionFailedPrecondition, "the TeamSpace is being deleted")
	case !in.Actor.PlatformAdmin && !isLoadedMember(ctx, in.Actor.AccountID):
		return registrationRejected(dexflow.RejectionPermissionDenied, "only a member of the TeamSpace creates its Projects")
	}
	comm.MustDo(TeamSpaceProjects.Set(ctx, in.ProjectID, RegisteredProject{
		ProjectFlowID: flowid.ProjectFlowID(in.ProjectID), RegisteredAt: time.Now().UTC(), RegisteredByAccountID: in.Actor.AccountID,
	}))
	return &dex.RPCResult[registrationResult]{}, nil
}

// UnregisterProject removes a deleted Project from the registry, also while the TeamSpace is
// deleting; removing an unregistered Project changes nothing.
func (f *TeamSpaceFlow) UnregisterProject(ctx dex.Context, in registration) (*dex.RPCResult[registrationResult], error) {
	if _, err := TeamSpaceProjects.Get(ctx, in.ProjectID); dexflow.IsAttributeNotFound(err) {
		return &dex.RPCResult[registrationResult]{}, nil
	} else if err != nil {
		return nil, err
	}
	comm.MustDo(TeamSpaceProjects.Delete(ctx, in.ProjectID))
	return &dex.RPCResult[registrationResult]{}, nil
}

// dex:group group-id:teamspace group-label:"TeamSpace"
// dex:explanation text:"Wait for the platform admin's deletion command."
type AwaitTeamSpaceCommands struct {
	dex.StepDefaults
	flow *TeamSpaceFlow
}

func (AwaitTeamSpaceCommands) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (AwaitTeamSpaceCommands) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	return dex.Until(TeamSpaceCommands.ForOne()), nil
}

func (s AwaitTeamSpaceCommands) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	command := comm.Must(TeamSpaceCommands.GetConditionResults(ctx))[0]
	return dex.GoTo(DeleteTeamSpaceResources{flow: s.flow}, command), nil
}

// dex:group group-id:teamspace group-label:"TeamSpace"
// dex:explanation text:"Delete every registered Project and wait until each is DELETED, then delete the TeamSpace's connections."
type DeleteTeamSpaceResources struct {
	dex.StepDefaultsNoWaitFor[Command]
	flow *TeamSpaceFlow
}

func (s DeleteTeamSpaceResources) GetStepOptions() *dex.StepOptions {
	options := dexflow.PollingStepOptions(deletionBudget, dex.ProceedToOnExecuteFailure(RecordTeamSpaceFailure{flow: s.flow}, nil))
	options.ExecuteLoadAttributeMaps = []dex.AttributeDef{TeamSpaceProjects}
	return options
}

func (s DeleteTeamSpaceResources) Execute(ctx dex.Context, command Command) (*dex.StepDecision, error) {
	if !readTeamSpace(ctx).runsCommand(command) {
		return dex.GoTo(AwaitTeamSpaceCommands{flow: s.flow}, nil), nil
	}
	// A deleting TeamSpace registers no Project, so the registry loaded here is complete.
	pending := slices.Sorted(slices.Values(TeamSpaceProjects.AllInstanceKeys(ctx)))
	teamSpaceID := strings.TrimPrefix(ctx.FlowID(), flowid.TeamSpaceFlowID(""))
	err := dexflow.PollUntil(ctx, deletionPollInterval, deletionRoundTimeout, func(callCtx context.Context) (bool, error) {
		if len(pending) > 0 {
			remaining, err := s.flow.deleteProjects(callCtx, pending, command.ID)
			if errors.Is(err, ErrProjectClosed) {
				command.Blocker = err.Error()
				return true, nil
			}
			if err != nil {
				return false, err
			}
			pending = remaining
			return false, nil
		}
		// With every Project DELETED no usage remains, so this is never refused.
		return s.flow.Connections.DeleteTeamSpaceConnections(callCtx, teamSpaceID)
	})
	if err != nil {
		return nil, err
	}
	if command.Blocker != "" {
		return dex.GoTo(RecordTeamSpaceFailure{flow: s.flow}, command), nil
	}
	return dex.GoTo(CompleteTeamSpaceDeletion{flow: s.flow}, command), nil
}

// dex:group group-id:teamspace group-label:"TeamSpace"
// dex:explanation text:"Record the TeamSpace DELETED; its members stay with the closed Flow."
type CompleteTeamSpaceDeletion struct {
	dex.StepDefaultsNoWaitFor[Command]
	flow *TeamSpaceFlow
}

func (CompleteTeamSpaceDeletion) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteDurability = dex.StepDurabilitySync
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(TeamSpaceRevision)}
	return options
}

func (s CompleteTeamSpaceDeletion) Execute(ctx dex.Context, command Command) (*dex.StepDecision, error) {
	if !readTeamSpace(ctx).runsCommand(command) {
		return dex.GoTo(AwaitTeamSpaceCommands{flow: s.flow}, nil), nil
	}
	comm.MustDo(TeamSpaceStatus.Set(ctx, StatusDeleted))
	comm.MustDo(TeamSpaceLastCommand.Set(ctx, &command))
	comm.MustDo(TeamSpaceActiveCommand.Set(ctx, nil))
	comm.MustDo(TeamSpaceLastFailure.Set(ctx, nil))
	touchTeamSpace(ctx)
	return dex.GracefulComplete(readTeamSpace(ctx)), nil
}

// dex:group group-id:teamspace group-label:"TeamSpace"
// dex:explanation text:"Record a deletion that did not converge; the TeamSpace stays DELETING until it is requested again."
type RecordTeamSpaceFailure struct {
	dex.StepDefaultsNoWaitFor[Command]
	flow *TeamSpaceFlow
}

func (RecordTeamSpaceFailure) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{dex.LockAttribute(TeamSpaceRevision)}
	return options
}

func (s RecordTeamSpaceFailure) Execute(ctx dex.Context, command Command) (*dex.StepDecision, error) {
	if readTeamSpace(ctx).runsCommand(command) {
		code, message, detail := failureDeletionFailed, "The TeamSpace's Projects were not all deleted. Request the deletion again.", dexflow.RecoveryDetail(ctx)
		switch {
		case command.Blocker != "":
			code, message, detail = failureProjectClosed, "A Project's execution closed before it was DELETED. Reconcile the Project, then request the deletion again.", command.Blocker
		case dexflow.RecoveryTimedOut(ctx):
			code, message = failureDeletionTimeout, "The TeamSpace's Projects were not all deleted within the wait limit. Inspect them, then request the deletion again."
		}
		comm.MustDo(TeamSpaceLastFailure.Set(ctx, &dexflow.Failure{Code: code, Phase: failureDeletionPhase, Message: dexflow.FailureMessage(message, detail), Recoverable: true, Remedy: dexflow.RemedyContactOperator}))
		comm.MustDo(TeamSpaceActiveCommand.Set(ctx, nil))
		touchTeamSpace(ctx)
	}
	return dex.GoTo(AwaitTeamSpaceCommands{flow: s.flow}, nil), nil
}

// readTeamSpace reads the snapshot of the invoking execution.
func readTeamSpace(ctx dex.Context) TeamSpace {
	return TeamSpace{
		Identity:         comm.Must(TeamSpaceStartIdentity.Get(ctx)),
		Name:             comm.Must(TeamSpaceName.Get(ctx)),
		CreatorAccountID: comm.Must(TeamSpaceCreatorAccountID.Get(ctx)),
		Status:           comm.Must(TeamSpaceStatus.Get(ctx)),
		ActiveCommand:    dexflow.Optional(TeamSpaceActiveCommand.Get(ctx)),
		LastCommand:      dexflow.Optional(TeamSpaceLastCommand.Get(ctx)),
		LastFailure:      dexflow.Optional(TeamSpaceLastFailure.Get(ctx)),
		Revision:         comm.Must(TeamSpaceRevision.Get(ctx)),
		CreatedAt:        comm.Must(TeamSpaceCreatedAt.Get(ctx)),
		UpdatedAt:        comm.Must(TeamSpaceUpdatedAt.Get(ctx)),
	}
}

// admitMemberWrite returns the whole Members map of a member write that the
// TeamSpace's status and the actor admit, or the write's rejection.
func admitMemberWrite(ctx dex.Context, a Actor) (map[string]Member, *dexflow.Rejection) {
	if comm.Must(TeamSpaceStatus.Get(ctx)) != StatusReady {
		return nil, dexflow.Reject(dexflow.RejectionFailedPrecondition, "the TeamSpace is being deleted")
	}
	members := loadedMembers(ctx, TeamSpaceMembers.AllInstanceKeys(ctx)...)
	if !a.mayAdminister(members) {
		return nil, dexflow.Reject(dexflow.RejectionPermissionDenied, "only a TeamSpace admin changes its members")
	}
	return members, nil
}

// loadedMembers reads the given Members instances that the invocation loaded
// and that exist.
func loadedMembers(ctx dex.Context, accountIDs ...string) map[string]Member {
	members := make(map[string]Member, len(accountIDs))
	for _, accountID := range accountIDs {
		member, err := TeamSpaceMembers.Get(ctx, accountID)
		if dexflow.IsAttributeNotFound(err) {
			continue
		}
		members[accountID] = comm.Must(member, err)
	}
	return members
}

// isLoadedMember reports whether the account, whose Members instance the
// invocation loaded, is a member.
func isLoadedMember(ctx dex.Context, accountID string) bool {
	_, found := loadedMembers(ctx, accountID)[accountID]
	return found
}

func (f *TeamSpaceFlow) result(ctx dex.Context, rejection *dexflow.Rejection) (*dex.RPCResult[Update], error) {
	return &dex.RPCResult[Update]{Output: Update{TeamSpace: readTeamSpace(ctx), Rejection: rejection}}, nil
}

func (f *TeamSpaceFlow) membersResult(ctx dex.Context, members map[string]Member) (*dex.RPCResult[Update], error) {
	return &dex.RPCResult[Update]{Output: Update{TeamSpace: readTeamSpace(ctx), Members: memberEntries(members)}}, nil
}

func registrationRejected(code dexflow.RejectionCode, message string) (*dex.RPCResult[registrationResult], error) {
	return &dex.RPCResult[registrationResult]{Output: registrationResult{Rejection: dexflow.Reject(code, message)}}, nil
}

func touchTeamSpace(ctx dex.Context) {
	comm.MustDo(TeamSpaceRevision.Set(ctx, comm.Must(TeamSpaceRevision.Get(ctx))+1))
	comm.MustDo(TeamSpaceUpdatedAt.Set(ctx, time.Now().UTC()))
}
