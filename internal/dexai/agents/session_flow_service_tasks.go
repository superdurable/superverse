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

	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
)

// reportSessionNotification delivers one notification from a source outside
// the Session, such as a delivery outcome, to the Session of the Project:
// true when the Session admitted it or already holds its ID, false when the
// Session is not ACTIVE or its queue is full. The notification's ID is
// derived from the source's durable facts, so a producer that retries after
// an error or a false answer repeats it safely. A missing or closed Session
// returns an error wrapping sessionagent.ErrSessionInactive. A subagent's
// report takes ReportSubagentOutcome, which also records its end.
func (s *SessionFlowService) reportSessionNotification(ctx context.Context, projectID string, notification conversation.SessionNotification) (bool, error) {
	admitted, err := s.Agent.ReportSessionNotification(ctx, agentloop.SessionFlowID(projectID), notification)
	s.logNotification(ctx, projectID, notification, admitted, err)
	if err != nil {
		return false, fmt.Errorf("report notification %s to the Session of Project %s: %w", notification.ID, projectID, err)
	}
	return admitted, nil
}

func (s *SessionFlowService) logNotification(ctx context.Context, projectID string, notification conversation.SessionNotification, admitted bool, err error) {
	s.Logger.InfoContext(ctx, "session notification", "operation", "report", "project_id", projectID, "kind", string(notification.Kind),
		"source", string(notification.Source), "admitted", admitted, "failed", err != nil)
}

// The Session's subagents: the Session starts and stops them through this
// Service (agenttasks.AgentTaskLauncher), and each SubagentFlow reaches its
// Session only through it (subagent.SubagentFlowDependency), so neither Flow
// package imports the other.

// StartSubagent idempotently starts the SubagentFlow of a subagent the
// Session admitted; a repeated start joins its execution, also a closed one.
func (s *SessionFlowService) StartSubagent(ctx context.Context, start agenttasks.SubagentStart, key modelcredential.Envelope, attempt int32) error {
	s.logSubagent(ctx, "start", start.Identity)
	return s.ModelCredentials.startSubagent(ctx, start, key, attempt)
}

// StopSubagent asks a subagent to stop and answers whether it has ended.
func (s *SessionFlowService) StopSubagent(ctx context.Context, id agenttasks.SubagentIdentity) (bool, error) {
	s.logSubagent(ctx, "stop", id)
	return s.Subagents.Stop(ctx, id)
}

// AdmitSubagent asks the subagent's Session whether it may run. A Session that
// no longer runs denies it.
func (s *SessionFlowService) AdmitSubagent(ctx context.Context, id agenttasks.SubagentIdentity, admission agenttasks.SubagentAdmission) (agenttasks.SubagentAdmissionAnswer, error) {
	s.logSubagent(ctx, "admit", id)
	answer, err := s.Agent.AdmitSubagent(ctx, agentloop.SessionFlowID(id.ProjectID), admission)
	if errors.Is(err, sessionagent.ErrSessionInactive) {
		return agenttasks.SubagentAdmissionAnswer{}, nil
	}
	return answer, err
}

// RefreshSubagentWorkspace asks the subagent's Session for the lease of its
// instance after the subagent's transport proved the lease of request lost
// unreachable. A Session that is no longer ACTIVE, whose execution ended, or
// whose record of the subagent no longer runs, lends it nothing any more.
func (s *SessionFlowService) RefreshSubagentWorkspace(ctx context.Context, id agenttasks.SubagentIdentity, lost string) (subagent.WorkspaceRefresh, error) {
	s.logSubagent(ctx, "refresh-workspace", id)
	lease, err := s.Agent.RequestSubagentWorkspace(ctx, agentloop.SessionFlowID(id.ProjectID), id.AgentID, lost)
	var stopped *sessionagent.SessionExecutionStoppedError
	switch {
	case errors.Is(err, sessionagent.ErrSessionNotActive), errors.Is(err, sessionagent.ErrSessionInactive), errors.Is(err, sessionagent.ErrCommandRejected), errors.As(err, &stopped):
		return subagent.WorkspaceRefresh{Closed: true}, nil
	case err != nil:
		return subagent.WorkspaceRefresh{}, err
	}
	return subagent.WorkspaceRefresh{Lease: lease}, nil
}

// ReportSubagentOutcome delivers the task notification of a subagent's end to
// its Session, whose record takes the end: admitted, also as a repeat of an
// admitted ID; busy while the Session's notification queue is full; closed
// once the Session no longer runs, so the report never arrives.
func (s *SessionFlowService) ReportSubagentOutcome(ctx context.Context, id agenttasks.SubagentIdentity, notification conversation.SessionNotification) (subagent.ReportAnswer, error) {
	sessionFlowID := agentloop.SessionFlowID(id.ProjectID)
	admitted, err := s.Agent.ReportSubagentNotification(ctx, sessionFlowID, id.AgentID, notification)
	s.logNotification(ctx, id.ProjectID, notification, admitted, err)
	switch {
	case errors.Is(err, sessionagent.ErrSessionInactive):
		return subagent.ReportClosed, nil
	case err != nil:
		return subagent.ReportAnswer{}, fmt.Errorf("report notification %s to the Session of Project %s: %w", notification.ID, id.ProjectID, err)
	case admitted:
		// A completed develop subagent's contribution; a repeat submits it again harmlessly.
		if err = s.submitContribution(ctx, id, notification); err != nil {
			return subagent.ReportAnswer{}, err
		}
		return subagent.ReportAdmitted, nil
	}
	summary, err := s.Agent.GetSessionSummary(ctx, sessionFlowID)
	if err != nil {
		return subagent.ReportAnswer{}, fmt.Errorf("read the Session of Project %s: %w", id.ProjectID, err)
	}
	if summary.Status == agentloop.SessionStatusActive {
		return subagent.ReportBusy, nil
	}
	return subagent.ReportClosed, nil
}

// stopSessionSubagents asks every subagent of a deleting Project's Session
// that has not ended to stop, and reports whether all ended. Subagents are not
// Project children, so they are found by their Flow type, ProjectID and
// status; the search is eventually consistent, and one it misses is denied by
// the closed Session at its admission or fails at its next lease refresh.
func (s *SessionFlowService) stopSessionSubagents(ctx context.Context, projectID string) (bool, error) {
	subagents, err := s.Subagents.SessionSubagents(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("find the subagents of the Session of Project %s: %w", projectID, err)
	}
	ended := true
	for _, id := range subagents {
		stopped, err := s.StopSubagent(ctx, id)
		if err != nil {
			return false, fmt.Errorf("stop subagent %s: %w", id.AgentID, err)
		}
		ended = ended && stopped
	}
	return ended, nil
}

func (s *SessionFlowService) logSubagent(ctx context.Context, operation string, id agenttasks.SubagentIdentity) {
	s.Logger.InfoContext(ctx, "subagent", "operation", operation, "project_id", id.ProjectID, "agent_id", id.AgentID)
}

// StopBackgroundCommandWatch stops the BackgroundCommandFlow that watches a
// background command of a closing Session; the Session's AwaitBackgroundCommand
// calls it once the close published the stop of the watch.
func (s *SessionFlowService) StopBackgroundCommandWatch(ctx context.Context, flowID string) error {
	return s.BackgroundCommands.Stop(ctx, flowID)
}

// ReconcileSubagentCredentials releases only exact-confirmed absent copies of ended children.
func (s *SessionFlowService) ReconcileSubagentCredentials(ctx context.Context, project string, holders []modelcredential.Holder, operation string) ([]string, error) {
	var cleared []string
	for _, holder := range holders {
		id := agenttasks.SubagentIdentity{ProjectID: project, AgentID: holder.AgentID}
		running, err := s.Subagents.CredentialHolderRunning(ctx, id)
		if err != nil {
			return nil, err
		}
		if running {
			continue
		}
		if err := s.ModelCredentials.clearSubagentCredential(ctx, id, operation+"/reconcile-holder/"+holder.CallID); err != nil {
			return nil, err
		}
		cleared = append(cleared, holder.AgentID)
	}
	return cleared, nil
}
