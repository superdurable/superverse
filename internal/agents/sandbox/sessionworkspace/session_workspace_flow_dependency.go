// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sessionworkspace

import (
	"context"
	"errors"

	"github.com/superdurable/superversev2/internal/agents/sandbox"
)

// ErrWorkspaceStartRejected marks a provisioning start that asking again
// cannot change, such as a Project that no longer admits a workspace. It
// wraps the reason, which the Flow records with the failed request.
var ErrWorkspaceStartRejected = errors.New("session workspace start rejected")

// ProvisionRequest starts the provisioning of the instance WorkspaceRequestID
// on what the earlier instances retained, checking out the Session HEAD.
type ProvisionRequest struct {
	WorkspaceRequestID string
	HeadCommit         string
	Retained           sandbox.Retained
}

// SessionWorkspaceFlowDependency is what SessionWorkspaceFlow consumes from
// the Session Service, which lives in another package because it also fronts
// the Session Agent Flow, the provisionings and the Preview. Every method is
// called only from the Flow's command Steps, never from a tool.
type SessionWorkspaceFlowDependency interface {
	// StartProvisioning idempotently starts the SandboxProvisionFlow of the
	// request. An error wrapping ErrWorkspaceStartRejected is a business
	// rejection.
	StartProvisioning(ctx context.Context, projectID string, request ProvisionRequest) error
	// ObserveProvisioning reads the outcome of the provisioning of requestID
	// from the provisioning, which owns it: nil while it runs, a failed one
	// also for a provisioning that stopped or never started.
	ObserveProvisioning(ctx context.Context, projectID, requestID string) (*Provisioning, error)
	// ReleasePreviewInstance asks the Project's Preview to stop using
	// instance requestID before the Flow reclaims it, and reports whether the
	// Preview no longer uses it.
	ReleasePreviewInstance(ctx context.Context, projectID, requestID string) (bool, error)
	// ReportWorkspaceOutcome tells the Project's Session the outcome of a
	// command. False means that the Session waits for no such request or is
	// closed; an error is asked again.
	ReportWorkspaceOutcome(ctx context.Context, projectID string, outcome Outcome) (bool, error)
}
