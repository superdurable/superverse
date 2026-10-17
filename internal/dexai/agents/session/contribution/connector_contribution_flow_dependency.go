// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package contribution

import (
	"context"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// ConnectorContributionFlowDependency is what ConnectorContributionFlow
// consumes from the Session Service, which fronts the contribution's Session:
// the admission of the contribution and the delivery of its connector
// notifications.
type ConnectorContributionFlowDependency interface {
	// AdmitConnectorContribution reports whether the Session of the
	// contribution is open, ACTIVE with a running execution, so that the
	// contribution may start; a deleting Project's Session denies it.
	AdmitConnectorContribution(ctx context.Context, id Identity) (bool, error)
	// ReportConnectorEvent delivers a connector notification to the Session
	// through ReportSessionNotification: true when admitted or a duplicate.
	ReportConnectorEvent(ctx context.Context, id Identity, notification conversation.SessionNotification) (bool, error)
}

// ReleaseCandidates publishes the release candidate of a contribution's head
// and observes it: connectorlibrary.Library.
type ReleaseCandidates interface {
	// RequestReleaseCandidate asks the library to publish candidate.
	RequestReleaseCandidate(ctx context.Context, candidate connectorlibrary.ReleaseCandidate, location connectorlibrary.Location) error
	// IsReleaseCandidatePublished reports whether candidate's release holds its assets.
	IsReleaseCandidatePublished(ctx context.Context, candidate connectorlibrary.ReleaseCandidate, location connectorlibrary.Location) (bool, error)
	// ReadReleaseCandidateRun observes the exact correlated workflow run.
	ReadReleaseCandidateRun(ctx context.Context, candidate connectorlibrary.ReleaseCandidate, location connectorlibrary.Location, createdAfter time.Time) (connectorlibrary.ReleaseCandidateRun, error)
	// ReadReleaseCandidateBlockers checks stable release and pull request facts
	// before the workflow is dispatched.
	ReadReleaseCandidateBlockers(ctx context.Context, candidate connectorlibrary.ReleaseCandidate, pullRequestNumber int64, location connectorlibrary.Location) (connectorlibrary.ReleaseCandidateBlockers, error)
}

// ConnectorPullRequests opens the draft pull request of a contribution branch
// in the upstream library: connectorlibrary.Library.
type ConnectorPullRequests interface {
	// OpensPullRequests reports whether the server names an upstream library.
	OpensPullRequests() bool
	// OpenDraftPullRequest finds or opens the pull request of the branch,
	// pushing a branch of the local Git host to the upstream library first.
	OpenDraftPullRequest(ctx context.Context, draft connectorlibrary.PullRequestDraft, location connectorlibrary.Location) (connectorlibrary.PullRequest, error)
}

// ConnectorChecks reads GitHub Actions for one exact connector branch head.
// Each scheduled check observes aggregate suites once; failed-job details
// are read only after a terminal failure or on an agent's read request.
type ConnectorChecks interface {
	ReadHeadChecks(context.Context, string) (connectorlibrary.HeadChecks, error)
	ReadFailedHeadJobs(context.Context, connectorlibrary.HeadChecks) (connectorlibrary.HeadChecks, error)
	ReadReleaseCandidateRun(context.Context, connectorlibrary.ReleaseCandidate, connectorlibrary.Location, time.Time) (connectorlibrary.ReleaseCandidateRun, error)
}

// ConnectorCheckRedactor applies the same Project-scoped output redaction as
// the read-only diagnostics tools before CI text enters durable Flow state.
type ConnectorCheckRedactor interface {
	RedactConnectorCheck(context.Context, string, string) string
}
