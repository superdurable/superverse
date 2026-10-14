// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/projects"
)

// logColdPath records every cross-Flow workspace operation. Tool calls use the
// accepted lease and the sandboxd transport only, so a tool call that reached
// this package would show up here. Only identities are logged.
func (s *SessionFlowService) logColdPath(ctx context.Context, operation, projectID string) {
	s.Logger.InfoContext(ctx, "session workspace cold path", "operation", operation, "project_id", projectID)
}

// ProvisionWorkspace sends the Project's SessionWorkspaceFlow, which it starts
// on the first command, the command that provisions the instance of requestID
// on the Session HEAD. A Project that no longer admits a workspace, and a
// workspace Flow that closed, reject it with
// sessionagent.ErrWorkspaceCommandRejected; false refuses the command of a
// replaced request.
func (s *SessionFlowService) ProvisionWorkspace(ctx context.Context, id agentloop.SessionIdentity, requestID, previewHolds string) (bool, error) {
	s.logColdPath(ctx, "ProvisionWorkspace", id.ProjectID)
	project, err := s.Projects.ReadSessionProject(ctx, id.ProjectID)
	if err != nil {
		return false, fmt.Errorf("read Project %s of the Session: %w", id.ProjectID, err)
	}
	if project.ProjectStatus != projects.StatusReady {
		return false, fmt.Errorf("%w: project status %s does not admit a session workspace", sessionagent.ErrWorkspaceCommandRejected, project.ProjectStatus)
	}
	return s.submitWorkspaceCommand(ctx, id.ProjectID, sessionworkspace.NewCommand(sessionworkspace.CommandProvision, requestID, id.HeadCommit, previewHolds))
}

// ReclaimWorkspace sends the command that reclaims the instance of the
// revoked request requestID; it answers like ProvisionWorkspace.
func (s *SessionFlowService) ReclaimWorkspace(ctx context.Context, id agentloop.SessionIdentity, requestID, previewHolds string) (bool, error) {
	s.logColdPath(ctx, "ReclaimWorkspace", id.ProjectID)
	return s.submitWorkspaceCommand(ctx, id.ProjectID, sessionworkspace.NewCommand(sessionworkspace.CommandReclaim, requestID, "", previewHolds))
}

// submitWorkspaceCommand submits command, classifying a closed workspace Flow
// as a rejection.
func (s *SessionFlowService) submitWorkspaceCommand(ctx context.Context, projectID string, command sessionworkspace.WorkspaceCommand) (bool, error) {
	accepted, err := s.Workspaces.Submit(ctx, projectID, command)
	if errors.Is(err, sessionworkspace.ErrWorkspaceClosed) {
		return false, fmt.Errorf("%w: %w", sessionagent.ErrWorkspaceCommandRejected, err)
	}
	if err != nil {
		return false, fmt.Errorf("submit the workspace command %s: %w", command.ID, err)
	}
	return accepted, nil
}

// ObserveWorkspace reads the outcome of requestID from the Project's
// SessionWorkspaceFlow, which owns it, for a Session whose own wait ended:
// nil while the request has not settled.
func (s *SessionFlowService) ObserveWorkspace(ctx context.Context, id agentloop.SessionIdentity, requestID string) (*agentloop.WorkspaceGrant, error) {
	s.logColdPath(ctx, "ObserveWorkspace", id.ProjectID)
	state, err := s.Workspaces.Get(ctx, id.ProjectID)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Session workspace of %s: %w", id.ProjectID, err)
	}
	if outcome := state.SettledOutcome(requestID); outcome != nil {
		return workspaceGrant(*outcome), nil
	}
	return nil, nil
}

// workspaceGrant is the Session's view of a ready or failed outcome.
func workspaceGrant(outcome sessionworkspace.Outcome) *agentloop.WorkspaceGrant {
	grant := &agentloop.WorkspaceGrant{RequestID: outcome.WorkspaceRequestID}
	if outcome.Lease != nil {
		grant.Lease = *outcome.Lease
	}
	if outcome.Failure != nil {
		grant.Failure, grant.FailureDetail = outcome.Failure.Code, outcome.Failure.Detail
	}
	return grant
}

// ReportWorkspaceOutcome tells the Project's Session the outcome of a command
// of its SessionWorkspaceFlow. False means the Session waits for no such
// request: it replaced the request, or it is missing or closed, which the
// workspace Flow only records.
func (s *SessionFlowService) ReportWorkspaceOutcome(ctx context.Context, projectID string, outcome sessionworkspace.Outcome) (bool, error) {
	s.logColdPath(ctx, "ReportWorkspaceOutcome", projectID)
	report := agentloop.WorkspaceReport{WorkspaceRequestID: outcome.WorkspaceRequestID, Reclaimed: outcome.Kind == sessionworkspace.OutcomeReclaimed}
	if !report.Reclaimed {
		report.Grant = workspaceGrant(outcome)
	}
	admitted, err := s.Agent.ReportWorkspaceOutcome(ctx, agentloop.SessionFlowID(projectID), report)
	return s.workspaceReportAnswer(ctx, projectID, fmt.Sprintf("%s outcome of %s", outcome.Kind, outcome.WorkspaceRequestID), admitted, err)
}

// StartProvisioning idempotently starts the SandboxProvisionFlow of the
// instance the Project's SessionWorkspaceFlow provisions. A Project that
// cannot host the workspace is a rejection wrapping
// sessionworkspace.ErrWorkspaceStartRejected.
func (s *SessionFlowService) StartProvisioning(ctx context.Context, projectID string, request sessionworkspace.ProvisionRequest) error {
	s.logColdPath(ctx, "StartProvisioning", projectID)
	project, err := s.Projects.ReadSessionProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("read Project %s of the Session: %w", projectID, err)
	}
	if project.ProjectStatus != projects.StatusReady {
		return fmt.Errorf("%w: project status %s does not admit a session workspace", sessionworkspace.ErrWorkspaceStartRejected, project.ProjectStatus)
	}
	identity, err := sandboxIdentity(project, agentloop.SessionIdentity{ProjectID: projectID, HeadCommit: request.HeadCommit})
	if err != nil {
		return fmt.Errorf("%w: %w", sessionworkspace.ErrWorkspaceStartRejected, err)
	}
	if err = s.Provisions.Start(ctx, provision.Request{Identity: identity, WorkspaceRequestID: request.WorkspaceRequestID, Retained: request.Retained}); err != nil {
		return fmt.Errorf("start the provisioning of %s: %w", request.WorkspaceRequestID, err)
	}
	return nil
}

// ObserveProvisioning reads the outcome of the provisioning of requestID,
// which owns it: nil while it runs, the READY lease with the instance's
// resources, or the failure with the resources it recorded, each with the
// phases it measured. A provisioning that never started reads as failed
// WORKSPACE_NOT_PROVISIONED, and one whose engine execution stopped as failed
// WORKSPACE_EXECUTION_STOPPED.
func (s *SessionFlowService) ObserveProvisioning(ctx context.Context, projectID, requestID string) (*sessionworkspace.Provisioning, error) {
	state, err := s.Provisions.Get(ctx, projectID, requestID)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return &sessionworkspace.Provisioning{Failure: &sessionworkspace.Failure{Code: sandbox.WorkspaceFailureNotProvisioned}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the provisioning of %s: %w", requestID, err)
	}
	outcome := state.Outcome
	if outcome == nil {
		return nil, nil
	}
	observed := &sessionworkspace.Provisioning{Resources: outcome.Resources, VolumeState: outcome.Checkout.WorkspaceState, Timings: state.WorkspaceTimings}
	if outcome.Status == provision.OutcomeReady {
		observed.Lease = state.Lease()
		return observed, nil
	}
	observed.Failure = &sessionworkspace.Failure{Code: outcome.Failure.Code, Detail: outcome.Failure.Message}
	return observed, nil
}

// ReportProvisionOutcome tells the Project's SessionWorkspaceFlow, which
// started the provisioning, that its provisioning decided the outcome, which
// the workspace Flow then reads from the provisioning. False means the
// workspace Flow waits for no such instance, or ended, which the provisioning
// only records.
func (s *SessionFlowService) ReportProvisionOutcome(ctx context.Context, id sandbox.Identity, requestID string, status provision.OutcomeStatus) (bool, error) {
	s.logColdPath(ctx, "ReportProvisionOutcome", id.ProjectID)
	admitted, err := s.Workspaces.ReportProvisionOutcome(ctx, id.ProjectID, requestID)
	if err != nil {
		return false, fmt.Errorf("report the %s outcome of %s to the Session workspace: %w", status, requestID, err)
	}
	return admitted, nil
}

// workspaceReportAnswer answers false for a missing or closed Session, whose
// refusal the reporter only records; the SDK cause is logged here.
func (s *SessionFlowService) workspaceReportAnswer(ctx context.Context, projectID, report string, admitted bool, err error) (bool, error) {
	if errors.Is(err, sessionagent.ErrSessionInactive) {
		s.Logger.InfoContext(ctx, "workspace report declined", "project_id", projectID, "report", report, "cause", err.Error())
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("report the %s to the Session: %w", report, err)
	}
	return admitted, nil
}

// RenewWorkspace keeps the Session's READY workspace running for
// agentloop.KeepRunningHold more, Studio's "Keep running for 7 days", and
// reports whether it did: false while the Session is not ACTIVE or its
// workspace not READY, which leaves the deadline unchanged. The account must
// be admitted to the Session, which a stopped engine execution refuses.
func (s *SessionFlowService) RenewWorkspace(ctx context.Context, account, project string) (bool, error) {
	_, err := s.readAuthorizedSession(ctx, account, project)
	if err != nil {
		return false, err
	}
	return s.Agent.RenewSessionWorkspace(ctx, agentloop.SessionFlowID(project))
}

// AcquireSessionWorkspace asks the Project's Session for its instance on
// behalf of the Project's Preview: a READY instance is lent with its
// resources, which the SessionWorkspaceFlow records, and otherwise the
// Session requests one unless a request is in flight, and announces the
// settlement; a ready-only demand, a deployment's, wakes nothing. A Session
// that is not ACTIVE, or whose engine execution stopped, lends nothing.
func (s *SessionFlowService) AcquireSessionWorkspace(ctx context.Context, id sandbox.Identity, commandID string) (preview.SessionWorkspace, error) {
	s.logColdPath(ctx, "AcquireSessionWorkspace", id.ProjectID)
	demand, err := s.Agent.RequestSessionWorkspace(ctx, agentloop.SessionFlowID(id.ProjectID), agentloop.WorkspaceDemandRequest{PreviewCommand: commandID})
	_, stopped := errors.AsType[*sessionagent.SessionExecutionStoppedError](err)
	if stopped || errors.Is(err, sessionagent.ErrSessionNotActive) || errors.Is(err, sessionagent.ErrCommandRejected) {
		return preview.SessionWorkspace{}, fmt.Errorf("%w: %w", preview.ErrSessionClosed, err)
	}
	if err != nil {
		return preview.SessionWorkspace{}, err
	}
	if !demand.Lent {
		return preview.SessionWorkspace{}, nil
	}
	lent := demand.Summary.RequestID
	state, err := s.Workspaces.Get(ctx, id.ProjectID)
	if err != nil {
		return preview.SessionWorkspace{}, fmt.Errorf("read the instance %s lent to the Preview: %w", lent, err)
	}
	if instance := state.Instance; instance.State != sessionworkspace.InstanceReady || instance.InstanceID != lent {
		// The Session revoked the lease since it lent it; the Preview asks again.
		return preview.SessionWorkspace{}, fmt.Errorf("the instance %s lent to the Preview is not the Session workspace's ready instance %s", lent, instance.InstanceID)
	}
	return preview.SessionWorkspace{WorkspaceRequestID: lent, Ready: true, Resources: state.Instance.Resources}, nil
}

// NotifyPreviewWorkspace announces to the Project's Preview command commandID
// that the workspace request it waits for settled.
func (s *SessionFlowService) NotifyPreviewWorkspace(ctx context.Context, id agentloop.SessionIdentity, commandID, requestID, failure string) error {
	s.logColdPath(ctx, "NotifyPreviewWorkspace", id.ProjectID)
	return s.Previews.NotifyWorkspaceSettled(ctx, id.ProjectID, preview.WorkspaceSettlement{CommandID: commandID, WorkspaceRequestID: requestID, Failure: failure})
}

// ReleasePreviewInstance asks the Project's Preview to stop using instance
// requestID, which the SessionWorkspaceFlow is about to reclaim, and reports
// whether it no longer uses it: a Preview that never started, ended or uses
// another instance, or whose reclaim command ended. A reclaim the Preview
// could not finish is logged; the Pod deletion that follows stops what still
// runs.
func (s *SessionFlowService) ReleasePreviewInstance(ctx context.Context, projectID, requestID string) (bool, error) {
	s.logColdPath(ctx, "ReleasePreviewInstance", projectID)
	command := preview.ReclaimCommand(requestID)
	state, err := s.Previews.Command(ctx, projectID, command)
	switch {
	case errors.Is(err, preview.ErrNotHeld), errors.Is(err, preview.ErrClosed):
		return true, nil
	case errors.Is(err, preview.ErrBusy):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("send %s to the Preview: %w", command.ID, err)
	case state.LastCommand.ID != command.ID:
		return false, nil
	}
	if state.Failure != nil {
		s.Logger.WarnContext(ctx, "the Preview did not finish its release", "project_id", projectID, "instance", requestID, "failure", state.Failure.Code)
	}
	return true, nil
}
