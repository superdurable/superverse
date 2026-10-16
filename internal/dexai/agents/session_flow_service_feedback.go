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
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
)

// Delivery feedback runs both ways between a Session and its Preview: the
// Session hands each successful pushed commit to the Preview with its result
// (RequestPreviewDeployment), and the Preview reports how its deployments,
// starts and source preparations ended (ReportPreviewOutcome). The Live
// release and deployment Flows report their failures the same way
// (ReportDeliveryFailure).

// RequestPreviewDeployment asks the Project's Preview to deploy the pushed
// commit of handoff on the Session's accepted instance requestID, and starts the
// Preview with the Project's first deployment. The Session records nothing
// from the answer, which only the log keeps: the outcome returns as a
// notification. A Preview that ended with its Project deploys nothing.
func (s *SessionFlowService) RequestPreviewDeployment(ctx context.Context, id agentloop.SessionIdentity, requestID string, handoff agentloop.PreviewHandoff) error {
	s.logColdPath(ctx, "RequestPreviewDeployment", id.ProjectID)
	if err := s.Previews.Start(ctx, sandbox.Identity{ProjectID: id.ProjectID}); err != nil {
		return fmt.Errorf("start the Preview of Project %s: %w", id.ProjectID, err)
	}
	request := preview.DeploymentRequest{WorkspaceRequestID: requestID, Commit: handoff.Commit, Sequence: int64(handoff.Sequence)}
	answer, err := s.Previews.RequestDeployment(ctx, id.ProjectID, request)
	if errors.Is(err, preview.ErrClosed) {
		s.Logger.InfoContext(ctx, "preview deployment", "project_id", id.ProjectID, "sequence", request.Sequence, "answer", "preview_closed")
		return nil
	}
	if err != nil {
		return fmt.Errorf("request the Preview deployment of %s: %w", handoff.Commit, err)
	}
	s.Logger.InfoContext(ctx, "preview deployment", "project_id", id.ProjectID, "sequence", request.Sequence, "answer", answer.String())
	return nil
}

// UpdatePreviewAutoDeploy turns the Preview's deployment of each pushed commit
// on or off for an account the Project admits, through the Session RPC of the
// same name. A Session that is not ACTIVE rejects it
// (sessionagent.ErrCommandRejected), and one whose engine execution stopped
// refuses it with its *sessionagent.SessionExecutionStoppedError.
func (s *SessionFlowService) UpdatePreviewAutoDeploy(ctx context.Context, account accounts.AccountSummary, project string, isEnabled bool) error {
	id, err := s.AuthorizeSession(ctx, account, project)
	if err != nil {
		return err
	}
	return s.Agent.UpdatePreviewAutoDeploy(ctx, id.FlowID, agentloop.PreviewAutoDeployRequest{IsEnabled: isEnabled, Actor: &id.Actor})
}

// ReportPreviewOutcome tells the Session the outcome of one of its Preview's
// commands, best effort, with one admission (deliverNotification).
func (s *SessionFlowService) ReportPreviewOutcome(ctx context.Context, id sandbox.Identity, notification conversation.SessionNotification) error {
	return s.deliverNotification(ctx, id.ProjectID, notification)
}

// ReportDeliveryFailure tells the Project's Session that a Live release or
// deployment failed, best effort, with one admission (deliverNotification).
// The notification comes from the failure alone (deliveryNotification),
// never from a classification of its code.
func (s *SessionFlowService) ReportDeliveryFailure(ctx context.Context, failure releases.DeliveryFailure) error {
	return s.deliverNotification(ctx, failure.ProjectID, deliveryNotification(failure))
}

// deliverNotification makes the one admission of a delivery notification. A
// Session that does not admit it (not ACTIVE, its queue full) or that is
// missing or closed drops it, which is logged with a fixed category: its
// source keeps the outcome. An error is a delivery that may be repeated; the
// notification's ID makes a repeat a no-op once admitted.
func (s *SessionFlowService) deliverNotification(ctx context.Context, projectID string, notification conversation.SessionNotification) error {
	admitted, err := s.reportSessionNotification(ctx, projectID, notification)
	switch {
	case errors.Is(err, sessionagent.ErrSessionInactive):
		s.Logger.InfoContext(ctx, "delivery notification dropped", "project_id", projectID, "category", "session_inactive")
	case err != nil:
		return err
	case !admitted:
		s.Logger.InfoContext(ctx, "delivery notification dropped", "project_id", projectID, "category", "not_admitted")
	}
	return nil
}

// maximumSummaryBytes bounds the summary of a delivery notification, which
// Studio shows.
const maximumSummaryBytes = 512

// deliveryNotification is the delivery notification of a Live failure: a
// failure when the application owns it and a notice otherwise, with the
// first conversation.MaximumNotificationGaps gaps and their count. The
// message is cut until the notification fits its bound.
func deliveryNotification(failure releases.DeliveryFailure) conversation.SessionNotification {
	what := "The Live " + failure.Source.String()
	notification := conversation.SessionNotification{
		ID: failure.ID, Kind: conversation.NotificationKindDelivery, Source: deliverySource(failure.Source), OccurredAt: failure.OccurredAt,
		Outcome: conversation.DeliveryNotice, Owner: failure.Owner, Code: failure.Failure.Code, Phase: failure.Failure.Phase, Remedy: failure.Failure.Remedy,
		Commit: failure.Commit, ResourceID: failure.ResourceID, Message: failure.Failure.Message, GapCount: len(failure.Gaps),
		Gaps:    appmanifest.RequirementGapNames(failure.Gaps[:min(len(failure.Gaps), conversation.MaximumNotificationGaps)]),
		Summary: what + " failed.",
	}
	if failure.Owner == conversation.DeliveryOwnerApplication {
		notification.Outcome = conversation.DeliveryFailure
	}
	if len(failure.Gaps) > 0 {
		notification.Summary = comm.TruncateUTF8(what+" needs configuration: "+strings.Join(notification.Gaps, ", ")+".", maximumSummaryBytes)
	}
	for len(notification.Text()) > conversation.MaximumNotificationBytes && notification.Message != "" {
		notification.Message = comm.TruncateUTF8(notification.Message, len(notification.Message)/2)
	}
	return notification
}

// deliverySource is the notification source of a delivery Flow.
func deliverySource(source releases.DeliverySource) conversation.NotificationSource {
	switch source {
	case releases.SourceRelease:
		return conversation.NotificationSourceRelease
	case releases.SourceDeployment:
		return conversation.NotificationSourceDeployment
	}
	panic("agents: unknown delivery source " + source.String())
}
