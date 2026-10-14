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

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
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
