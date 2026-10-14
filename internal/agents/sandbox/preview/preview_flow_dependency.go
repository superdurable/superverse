// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"context"
	"errors"

	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

// ErrSessionClosed reports a Session that is not ACTIVE: it lends no
// workspace to its Preview.
var ErrSessionClosed = errors.New("the Session is not active")

// SessionWorkspace is the Session's answer to a Preview that asks for its
// workspace: its current instance WorkspaceRequestID with the instance's
// resources once Ready, or the request in flight whose settlement the
// Session announces through NotifyWorkspaceSettled.
type SessionWorkspace struct {
	WorkspaceRequestID string
	Ready              bool
	Resources          sandbox.Resources
}

// PreviewFlowDependency is what PreviewFlow consumes from the Session Service
// of the Session that owns the Preview. The Service lives in another package
// because it also fronts the Session Agent Flow.
type PreviewFlowDependency interface {
	// AcquireSessionWorkspace asks the Session for its current instance for
	// the active command commandID. A Ready instance is lent to the Preview:
	// the SessionWorkspaceFlow asks the Preview to release it before it
	// reclaims it. Otherwise the Session requests one, unless a request is in
	// flight, and announces its settlement to commandID. Both manual and automatic
	// builds resume a reclaimed workspace. ErrSessionClosed when the Session is not ACTIVE.
	AcquireSessionWorkspace(ctx context.Context, id sandbox.Identity, commandID string) (SessionWorkspace, error)
	// ReportPreviewOutcome tells the Session the outcome of a deployment, a
	// start or a source preparation, best effort: a Session that does not
	// admit it drops it, and the Preview keeps the outcome. The notification's
	// ID makes a repeat a no-op. An error is a delivery that may be repeated.
	ReportPreviewOutcome(ctx context.Context, id sandbox.Identity, notification conversation.SessionNotification) error
}
