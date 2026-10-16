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

	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// The Session's connector contributions: each connector a completed
// connector_develop subagent contributed is one ConnectorContributionFlow,
// which this Service starts or updates when the subagent's report reaches
// the Session, which it admits and lets report connector notifications, and
// which it closes for a deleting Project.

// submitContribution submits the contribution that a completed develop
// subagent's report carries, after its Session admitted the report: a new
// connector starts its contribution, a later head of the same one updates it.
// A report of another subagent submits nothing; a contribution that ended,
// such as a deleting Project's, is only logged.
func (s *SessionFlowService) submitContribution(ctx context.Context, id agenttasks.SubagentIdentity, notification conversation.SessionNotification) error {
	if notification.Contribution == nil || notification.TaskStatus != agenttasks.SubagentCompleted.String() {
		return nil
	}
	submission := contribution.Submission{Identity: contribution.Identity{ProjectID: id.ProjectID, Slug: notification.Contribution.Slug},
		Contribution: *notification.Contribution, Report: notification.Report}
	err := s.Contributions.Submit(ctx, submission)
	s.Logger.InfoContext(ctx, "connector contribution", "operation", "submit", "project_id", id.ProjectID, "agent_id", id.AgentID,
		"slug", submission.Identity.Slug, "head_commit", submission.Contribution.HeadCommit, "failed", err != nil)
	if errors.Is(err, contribution.ErrContributionClosed) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("submit the contribution %s of subagent %s: %w", submission.Identity.Slug, id.AgentID, err)
	}
	return nil
}

// SubmitConnectorContribution accepts a remote agent's committed connector
// head. A lost answer is reconciled by the Flow snapshot before Submit.
func (s *SessionFlowService) SubmitConnectorContribution(ctx context.Context, projectID string, contributed conversation.ConnectorContribution, summary string) error {
	id := contribution.Identity{ProjectID: projectID, Slug: contributed.Slug}
	current, err := s.Contributions.Snapshot(ctx, id)
	var missing *dex.FlowNotFoundError
	switch {
	case err == nil && current.Record.Status.Closed():
		return contribution.ErrContributionClosed
	case err == nil && current.Record.HeadCommit == contributed.HeadCommit:
		return nil
	case err != nil && !errors.As(err, &missing):
		return err
	}
	return s.Contributions.Submit(ctx, contribution.Submission{Identity: id, Contribution: contributed, Report: summary})
}

// AdmitConnectorContribution admits a contribution while its Session is
// ACTIVE with a running execution; a closed, stopped or missing Session, such
// as a deleting Project's, denies it.
func (s *SessionFlowService) AdmitConnectorContribution(ctx context.Context, id contribution.Identity) (bool, error) {
	summary, err := s.Agent.GetLiveSessionSummary(ctx, agentloop.SessionFlowID(id.ProjectID))
	var stopped *sessionagent.SessionExecutionStoppedError
	var missing *dex.FlowNotFoundError
	switch {
	case errors.As(err, &stopped), errors.As(err, &missing):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read the Session of Project %s: %w", id.ProjectID, err)
	}
	return summary.Status == agentloop.SessionStatusActive, nil
}

// ReportConnectorEvent delivers a contribution's connector notification to its
// Session; a Session that ended takes nothing.
func (s *SessionFlowService) ReportConnectorEvent(ctx context.Context, id contribution.Identity, notification conversation.SessionNotification) (bool, error) {
	admitted, err := s.reportSessionNotification(ctx, id.ProjectID, notification)
	if errors.Is(err, sessionagent.ErrSessionInactive) {
		return false, nil
	}
	return admitted, err
}

// readContributions reads the contributions of the connectors that the
// Session's develop subagents named, at most 16, each by its FlowID, for the
// Session's Snapshot. A connector whose subagent contributed nothing has no
// contribution and is left out.
func (s *SessionFlowService) readContributions(ctx context.Context, projectID string, slugs []string) ([]contribution.Snapshot, error) {
	snapshots := make([]contribution.Snapshot, 0, len(slugs))
	for _, slug := range slugs {
		snapshot, err := s.Contributions.Snapshot(ctx, contribution.Identity{ProjectID: projectID, Slug: slug})
		var missing *dex.FlowNotFoundError
		switch {
		case errors.As(err, &missing):
			continue
		case err != nil:
			return nil, fmt.Errorf("read the contribution %s of Project %s: %w", slug, projectID, err)
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// closeSessionContributions asks every contribution of a deleting Project
// that has not ended to end and reports whether all ended; the pull requests
// and branches stay on GitHub. The search is eventually consistent: one it
// misses is denied by the closed Session at its admission, and the next pass
// asks again.
func (s *SessionFlowService) closeSessionContributions(ctx context.Context, projectID string) (bool, error) {
	identities, err := s.Contributions.ProjectContributions(ctx, projectID, true)
	if err != nil {
		return false, fmt.Errorf("find the contributions of Project %s: %w", projectID, err)
	}
	ended := true
	for _, id := range identities {
		closed, err := s.Contributions.Close(ctx, id)
		if err != nil {
			return false, fmt.Errorf("close the contribution %s: %w", id.Slug, err)
		}
		ended = ended && closed
	}
	return ended, nil
}
