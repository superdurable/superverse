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
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

var (
	// ErrForbidden denies a caller who does not hold the capability it asks
	// for in the Project, and any access to a deleted Project.
	ErrForbidden = errors.New("project access denied")
	// ErrChildAdmissionClosed reports a Project that is deleting, deleted or
	// absent and therefore admits no new child.
	ErrChildAdmissionClosed = errors.New("project no longer admits children")
)

// existenceReadTimeout bounds the read of ProjectFlow that tells a Project
// whose row Dex has not written yet from one that does not exist.
const existenceReadTimeout = 10 * time.Second

// memberLockWait bounds a member write, including its wait for another
// write's Revision lock and its Attribute Store sync, which a loaded lane slows
// to several seconds.
const memberLockWait = 30 * time.Second

// ProjectFlowClient fronts ProjectFlow: its RPCs and exact reads through Dex,
// and its read model rows, which Dex syncs into PostgreSQL, through
// Projections; Members reads the members of a Project from the read model.
type ProjectFlowClient struct {
	Dex                *dex.Client
	Flow               *ProjectFlow
	Projections        *projectdao.ProjectDAO
	Members            *memberdao.MemberDAO
	ProjectIDLength    int
	PlatformAdminEmail string
}

// ProjectProjection is a Project as the read model holds it, seconds behind
// ProjectFlow and its Setup. Identity and LastCommand are not synced and stay
// zero. It serves reads only: no write admission or Flow input takes one.
// CallerRole is the effective role of the account that an authorization or a
// list read it for.
type ProjectProjection struct {
	Project    Project
	Setup      SetupSummary
	CallerRole Role
	// LiveVersion is the version of the Release Live serves, zero without
	// Live or for a Release prepared before versions were recorded.
	LiveVersion repohost.CommitVersion
}

// SetupSummary is what a Project shows of its Setup: in a projection, the
// Setup's columns of the Project's row, whose Status is empty until the
// Setup's first write reaches it.
type SetupSummary struct {
	Status, Phase, SourceCommit string
	Failure                     *dexflow.Failure
	Revision                    uint64
}

// Read returns the Project of a caller-supplied ID from ProjectFlow, also
// after its deletion completed.
func (c *ProjectFlowClient) Read(ctx context.Context, projectID string) (Project, error) {
	if err := flowid.ValidateProjectID(projectID, c.ProjectIDLength); err != nil {
		return Project{}, err
	}
	var project Project
	err := dexflow.InvokeRetainedRead(ctx, c.Dex, flowid.ProjectFlowID(projectID), c.Flow.GetProject, nil, &project)
	return project, err
}

// Authorize returns the read model's Project when accountID holds the
// capability in it (access.allows). A deleted Project is forbidden to
// everyone. The row of a Project created a moment ago is waited for
// (postgres.AwaitRow); a Project without a ProjectFlow is
// postgres.ErrNotFound.
func (c *ProjectFlowClient) Authorize(ctx context.Context, accountID, projectID string, capability Capability) (ProjectProjection, error) {
	projection, err := c.ReadAuthorized(ctx, accountID, projectID, capability)
	if err == nil && projection.Project.ProjectStatus == StatusDeleted {
		return ProjectProjection{}, ErrForbidden
	}
	return projection, err
}

// ReadAuthorized is Authorize that keeps a deleted Project readable, for the
// terminal snapshot of a Watch.
func (c *ProjectFlowClient) ReadAuthorized(ctx context.Context, accountID, projectID string, capability Capability) (ProjectProjection, error) {
	if err := flowid.ValidateProjectID(projectID, c.ProjectIDLength); err != nil {
		return ProjectProjection{}, err
	}
	if accountID == "" {
		return ProjectProjection{}, ErrForbidden
	}
	row, err := postgres.AwaitRow(ctx, func(ctx context.Context) (projectdao.ProjectAccess, error) {
		return c.Projections.AuthorizeProject(ctx, projectID, accountID, c.PlatformAdminEmail, accountdao.StatusActive)
	}, func(ctx context.Context) (bool, error) { return c.exists(ctx, projectID) })
	if err != nil {
		return ProjectProjection{}, err
	}
	held := access{platformAdmin: row.PlatformAdmin, projectRole: row.ProjectRole.V, teamSpaceRole: row.TeamSpaceRole.V}
	if !held.allows(capability) {
		return ProjectProjection{}, fmt.Errorf("%w: %s", ErrForbidden, capability.name)
	}
	projection, err := projectionOf(row.Project)
	projection.CallerRole = held.effectiveRole()
	return projection, err
}

// exists reports whether the Project has a ProjectFlow, for a missing row: a
// bounded read of the Flow, which a just-created Project answers.
func (c *ProjectFlowClient) exists(ctx context.Context, projectID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, existenceReadTimeout)
	defer cancel()
	_, err := c.Read(ctx, projectID)
	if _, missing := errors.AsType[*dex.FlowNotFoundError](err); missing {
		return false, nil
	}
	return err == nil, err
}

// AuthorizeCurrent authorizes accountID for the capability as Authorize does
// and returns ProjectFlow's current Project, which the admission of a write
// requires: the read model can lag the Flow (decision 27).
func (c *ProjectFlowClient) AuthorizeCurrent(ctx context.Context, accountID, projectID string, capability Capability) (Project, error) {
	p, _, err := c.authorizeCurrent(ctx, accountID, projectID, capability)
	return p, err
}

// authorizeCurrent is AuthorizeCurrent that also returns the caller's
// effective role.
func (c *ProjectFlowClient) authorizeCurrent(ctx context.Context, accountID, projectID string, capability Capability) (Project, Role, error) {
	projection, err := c.Authorize(ctx, accountID, projectID, capability)
	if err != nil {
		return Project{}, "", err
	}
	p, err := c.Read(ctx, projectID)
	if err != nil {
		return Project{}, "", err
	}
	if p.ProjectStatus == StatusDeleted {
		return Project{}, "", ErrForbidden
	}
	return p, projection.CallerRole, nil
}

// ReadProjection returns the read model's Project of a scope that its caller
// already holds, such as a Session's, without authorizing an account.
func (c *ProjectFlowClient) ReadProjection(ctx context.Context, projectID string) (ProjectProjection, error) {
	row, err := postgres.AwaitRow(ctx, func(ctx context.Context) (projectdao.ProjectRow, error) {
		return c.Projections.Project(ctx, projectID)
	}, func(ctx context.Context) (bool, error) { return c.exists(ctx, projectID) })
	if err != nil {
		return ProjectProjection{}, err
	}
	return projectionOf(row)
}

// ProjectList selects the Projects of one TeamSpace that ListProjections
// lists for an account; Statuses, when given, select those.
type ProjectList struct {
	AccountID     string
	PlatformAdmin bool
	TeamSpaceID   string
	Statuses      []string
}

// ListProjections returns at most limit Projects of the read model that are
// not deleted and that the list selects, newest first, after the page key
// when one is given, each with the account's effective role, and the key of
// the page after them. The list is for the TeamSpace's members and the
// platform admin, whom the caller checked.
func (c *ProjectFlowClient) ListProjections(ctx context.Context, list ProjectList, after *postgres.PageKey, limit int) ([]ProjectProjection, *postgres.PageKey, error) {
	filter := projectdao.ProjectFilter{AccountID: list.AccountID, TeamSpaceID: list.TeamSpaceID, Statuses: list.Statuses, ExcludedStatus: StatusDeleted}
	rows, next, err := c.Projections.ListProjects(ctx, filter, after, limit)
	if err != nil {
		return nil, nil, err
	}
	projections := make([]ProjectProjection, 0, len(rows))
	for _, row := range rows {
		projection, decodeErr := projectionOf(row.Project)
		if decodeErr != nil {
			return nil, nil, decodeErr
		}
		projection.CallerRole = access{platformAdmin: list.PlatformAdmin, projectRole: row.ProjectRole.V, teamSpaceRole: row.TeamSpaceRole.V}.effectiveRole()
		projections = append(projections, projection)
	}
	return projections, next, nil
}

// projectionOf decodes a projects row into the types of the Attributes Dex
// wrote it from.
func projectionOf(row projectdao.ProjectRow) (ProjectProjection, error) {
	p := Project{
		ProjectID: row.ProjectID, CreatorAccountID: row.CreatorAccountID.V, TeamSpaceID: row.TeamSpaceID.V, Name: row.Name.V, TemplateRevision: row.TemplateRevision.V,
		SetupFlowID: row.SetupFlowID.V, ProjectStatus: row.ProjectStatus.V, Revision: postgres.Unsigned(row.Revision),
		CreatedAt: row.CreatedAt.V, UpdatedAt: row.UpdatedAt.V,
	}
	setup := SetupSummary{Status: row.SetupStatus.V, Phase: row.SetupPhase.V, SourceCommit: row.SetupSourceCommit.V, Revision: postgres.Unsigned(row.SetupRevision)}
	err := errors.Join(
		postgres.DecodeJSON("repository_binding", row.RepositoryBinding, &p.RepositoryBinding),
		postgres.DecodeJSON("app_manifest", row.AppManifest, &p.AppManifest),
		postgres.DecodeJSON("active_deployment", row.ActiveDeployment, &p.ActiveDeployment),
		postgres.DecodeJSON("current_live", row.CurrentLive, &p.CurrentLive),
		postgres.DecodeJSON("active_command", row.ActiveCommand, &p.ActiveCommand),
		postgres.DecodeJSON("last_failure", row.LastFailure, &p.LastFailure),
		postgres.DecodeJSON("session_start_failure", row.SessionStartFailure, &p.SessionStartFailure),
		postgres.DecodeJSON("project_storage", row.ProjectStorage, &p.Storage),
		postgres.DecodeJSON("live_execution", row.LiveExecution, &p.LiveExecution),
		postgres.DecodeJSON("setup_failure", row.SetupFailure, &setup.Failure),
	)
	var liveVersion repohost.CommitVersion
	err = errors.Join(err, postgres.DecodeJSON("live release version", row.LiveVersion, &liveVersion))
	if err != nil {
		return ProjectProjection{}, fmt.Errorf("project %s: %w", row.ProjectID, err)
	}
	return ProjectProjection{Project: p, Setup: setup, LiveVersion: liveVersion}, nil
}

// Create starts the ProjectFlow of an accepted creation, whose first Step
// registers it in its TeamSpace. A retry of the same request joins the
// execution it started; Read owns its state.
func (c *ProjectFlowClient) Create(ctx context.Context, in ProjectStart) error {
	_, err := c.Dex.StartFlow(ctx, c.Flow, flowid.ProjectFlowID(in.ProjectID), startInput(in), dex.StartFlowOptions{
		IDReusePolicy: dex.IDReuseDisallow, RequestID: &in.Identity.RequestID,
		AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: c.Flow.InitialAttributes(in),
		ConfigOverride: &dex.FlowConfig{AttributeStoreNames: postgres.ProjectStore.Names()},
	})
	return err
}

// startInput is the input of the start Step, RegisterProjectInTeamSpace.
func startInput(in ProjectStart) teamSpaceAdmission {
	return teamSpaceAdmission{CreatorIsPlatformAdmin: in.CreatorIsPlatformAdmin}
}

// StartInputForTest is startInput for the tests that start a ProjectFlow with
// their own StartFlow options.
func StartInputForTest(in ProjectStart) any { return startInput(in) }

// DeleteForTeamSpace asks the Project to delete itself for its TeamSpace's
// deletion, under the TeamSpace's command ID, and reports whether it is
// DELETED. A Project that is deleting joins with the request; one whose
// revision, lock or other command keeps it from admitting the deletion now is
// asked again in a later round; a FAILED deletion is admitted again at its
// current revision. The retained read still answers for an execution that
// closed undeleted, but the request does not: that Project is
// teamspaces.ErrProjectClosed.
func (c *ProjectFlowClient) DeleteForTeamSpace(ctx context.Context, projectID, commandID string) (bool, error) {
	p, err := c.Read(ctx, projectID)
	if _, missing := errors.AsType[*dex.FlowNotFoundError](err); missing {
		return true, nil
	}
	switch {
	case err != nil:
		return false, err
	case p.ProjectStatus == StatusDeleted:
		return true, nil
	}
	_, err = c.RequestDeletion(ctx, projectID, commandID, p.Revision)
	if rejected, isRejected := errors.AsType[*dexflow.Rejection](err); isRejected && (rejected.HasCode(dexflow.RejectionAborted) || rejected.HasCode(dexflow.RejectionFailedPrecondition)) {
		return false, nil
	}
	if _, busy := errors.AsType[*dex.RPCLockConflictError](err); busy {
		return false, nil
	}
	if _, closed := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); closed {
		return false, fmt.Errorf("%w: Project %s, last %s", teamspaces.ErrProjectClosed, projectID, p.ProjectStatus)
	}
	return false, err
}

// ListMembers lists the members of a Project that is not DELETED, in the order
// they were added, from the read model.
func (c *ProjectFlowClient) ListMembers(ctx context.Context, projectID string) ([]MemberView, error) {
	rows, err := c.Members.ProjectMembers(ctx, projectID, StatusDeleted)
	if err != nil {
		return nil, err
	}
	members := make([]MemberView, 0, len(rows))
	for _, row := range rows {
		members = append(members, MemberView{AccountID: row.AccountID, Email: row.Email.V, Role: row.Role.V, AddedAt: row.AddedAt.V,
			AddedByAccountID: row.AddedByAccountID.V, TeamSpaceMember: row.TeamSpaceMember})
	}
	return members, nil
}

// AddMember, SetMemberRole and RemoveMember change the Project's members as
// the actor, and return the Project and its members after the change, or the
// change's rejection as the error.
func (c *ProjectFlowClient) AddMember(ctx context.Context, projectID string, in MemberChange) (MemberUpdate, error) {
	return c.invokeMembers(ctx, projectID, c.Flow.AddProjectMember, in)
}

func (c *ProjectFlowClient) SetMemberRole(ctx context.Context, projectID string, in MemberChange) (MemberUpdate, error) {
	return c.invokeMembers(ctx, projectID, c.Flow.SetProjectMemberRole, in)
}

func (c *ProjectFlowClient) RemoveMember(ctx context.Context, projectID string, in MemberChange) (MemberUpdate, error) {
	return c.invokeMembers(ctx, projectID, c.Flow.RemoveProjectMember, in)
}

// invokeMembers calls a member write, whose RPC loads the whole Members map.
func (c *ProjectFlowClient) invokeMembers(ctx context.Context, projectID string, rpc any, in MemberChange) (MemberUpdate, error) {
	result, err := dexflow.InvokeLocked[MemberUpdate](ctx, c.Dex, memberLockWait, flowid.ProjectFlowID(projectID), rpc, in)
	if err != nil {
		return MemberUpdate{}, err
	}
	return result, result.Rejection.Err()
}

// CommitProjectSetup crosses the Setup-to-Project durable ownership boundary.
func (c *ProjectFlowClient) CommitProjectSetup(ctx context.Context, projectID string, in SetupCommit) error {
	_, err := c.invoke(ctx, projectID, c.Flow.CommitProjectSetup, in)
	return err
}

// RecordProjectStorage retains Setup's exact storage effects before it advances.
func (c *ProjectFlowClient) RecordProjectStorage(ctx context.Context, projectID string, in ProjectStorageUpdate) error {
	_, err := c.invoke(ctx, projectID, c.Flow.RecordProjectStorage, in)
	return err
}

// RequestDeletion admits the deletion command requestID at the Project
// revision the caller observed.
func (c *ProjectFlowClient) RequestDeletion(ctx context.Context, projectID, requestID string, expectedRevision uint64) (Project, error) {
	return c.invoke(ctx, projectID, c.Flow.RequestProjectDeletion, ProjectCommand{ID: requestID, ExpectedRevision: expectedRevision})
}

// RemoveLive admits the Live removal command requestID for the Live pointer
// the caller observed: its deployment and revision, the fence StartDeployment
// and StartReleaseBuild also use. The command is admitted at the Project
// revision this call read, for a builder; it also returns the caller's
// effective role. A Project without Live is returned unchanged.
func (c *ProjectFlowClient) RemoveLive(ctx context.Context, accountID, projectID, requestID, deploymentID string, expectedLiveRevision uint64) (Project, Role, error) {
	p, role, err := c.authorizeCurrent(ctx, accountID, projectID, CapabilityBuild)
	if err != nil || p.CurrentLive == nil {
		return p, role, err
	}
	if p.CurrentLive.DeploymentID != deploymentID || p.CurrentLive.Revision != expectedLiveRevision {
		return Project{}, "", dexflow.RejectStale("Live deployment changed")
	}
	p, err = c.invoke(ctx, projectID, c.Flow.RequestLiveRemoval, ProjectCommand{ID: requestID, ExpectedRevision: p.Revision})
	return p, role, err
}

// AcquireDeployment leases Live to the deployment and returns the lease with
// the generation the Project assigned.
func (c *ProjectFlowClient) AcquireDeployment(ctx context.Context, projectID string, in DeploymentLease) (DeploymentLease, error) {
	project, err := c.invoke(ctx, projectID, c.Flow.AcquireDeployment, in)
	if err != nil {
		return DeploymentLease{}, err
	}
	return *project.ActiveDeployment, nil
}

// RecordLiveExecution retains a deployment's exact Live execution effects
// under its lease before it advances.
func (c *ProjectFlowClient) RecordLiveExecution(ctx context.Context, projectID string, in LiveExecutionUpdate) error {
	_, err := c.invoke(ctx, projectID, c.Flow.RecordLiveExecution, in)
	return err
}

// CommitLiveDeployment fences the exact lease and pointer in the owning Project RPC.
func (c *ProjectFlowClient) CommitLiveDeployment(ctx context.Context, projectID string, in DeploymentCommit) error {
	_, err := c.invoke(ctx, projectID, c.Flow.CommitLiveDeployment, in)
	return err
}

// ReleaseDeployment ends the deployment's lease. A lease that is gone or held
// by a later deployment needs no release; a lost response of an earlier
// release must not end that later lease.
func (c *ProjectFlowClient) ReleaseDeployment(ctx context.Context, projectID string, lease DeploymentLease) error {
	if lease.Generation == 0 {
		return nil
	}
	_, err := c.invoke(ctx, projectID, c.Flow.ReleaseDeployment, lease)
	var rejected *dexflow.Rejection
	if errors.As(err, &rejected) && rejected.HasCode(dexflow.RejectionAborted) {
		return nil
	}
	return err
}

// AdmitChild registers flowID as a cleanup child of the Project before the
// child's first effect. A Project that is deleting, deleted or absent admits
// nothing: that is ErrChildAdmissionClosed, also when deletion began while the
// registration was in flight.
func (c *ProjectFlowClient) AdmitChild(ctx context.Context, projectID string, flow ChildFlow, flowID string) error {
	child := ChildFlowReference{FlowID: flowID, FlowType: flow.FlowType()}
	err := c.invokeChild(ctx, projectID, c.Flow.RegisterChildFlow, child)
	if err == nil {
		return nil
	}
	project, readErr := c.Read(ctx, projectID)
	var missing *dex.FlowNotFoundError
	if errors.As(readErr, &missing) || readErr == nil && !project.AdmitsChildren() {
		return fmt.Errorf("%w: %w", ErrChildAdmissionClosed, err)
	}
	return fmt.Errorf("register child %s of project %s: %w", flowID, projectID, errors.Join(err, readErr))
}

// AcknowledgeChild removes a child whose cleanup completed.
func (c *ProjectFlowClient) AcknowledgeChild(ctx context.Context, projectID string, child ChildFlowReference) error {
	return c.invokeChild(ctx, projectID, c.Flow.AcknowledgeChildCleanup, child)
}

// readChild exact-loads one admission; search results are never permission
// evidence. A child that is not registered reads as the zero reference.
func (c *ProjectFlowClient) readChild(ctx context.Context, projectID, flowID string) (ChildFlowReference, error) {
	var child ChildFlowReference
	err := dexflow.InvokeRetainedRead(ctx, c.Dex, flowid.ProjectFlowID(projectID), c.Flow.GetChildFlow, flowID, &child, ChildFlows.Load(flowID))
	return child, err
}

// invoke calls a ProjectFlow RPC that answers with a projectResult. It returns
// the Project after the call, or the call's rejection as the error.
func (c *ProjectFlowClient) invoke(ctx context.Context, projectID string, rpc, in any) (Project, error) {
	var result projectResult
	if err := c.Dex.InvokeRPC(ctx, flowid.ProjectFlowID(projectID), rpc, in, &result); err != nil {
		return Project{}, err
	}
	return result.Project, result.Rejection.Err()
}

// invokeChild calls a membership RPC under the exact ChildFlows entry it changes.
func (c *ProjectFlowClient) invokeChild(ctx context.Context, projectID string, rpc any, child ChildFlowReference) error {
	var result childAdmissionResult
	err := c.Dex.InvokeRPCWithOptions(ctx, flowid.ProjectFlowID(projectID), rpc, child, &result, dex.RPCInvokeOptions{
		LoadAttributeMapInstances: []dex.AttributeMapLoad{ChildFlows.Load(child.FlowID)},
		LockAttributeMapInstances: []dex.AttributeLock{dex.LockAttributeMap(ChildFlows, child.FlowID)},
	})
	if err != nil {
		return err
	}
	return result.Rejection.Err()
}

// ReadChildForTest is readChild for the tests.
func (r *ProjectFlowClient) ReadChildForTest(ctx context.Context, projectID string, flowID string) (ChildFlowReference, error) {
	return r.readChild(ctx, projectID, flowID)
}
