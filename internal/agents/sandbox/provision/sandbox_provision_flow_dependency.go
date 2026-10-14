// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package provision

import (
	"context"

	"github.com/superdurable/superversev2/internal/agents/sandbox"
)

// SandboxProvisionFlowDependency is what SandboxProvisionFlow consumes from
// the Session Service: the report of its outcome to the SessionWorkspaceFlow
// that started it. The Service lives in another package because the
// workspace Flow's package imports this one.
type SandboxProvisionFlowDependency interface {
	// ReportProvisionOutcome tells the Project's SessionWorkspaceFlow that
	// the provisioning of requestID ended with status. False means that it
	// waits for no such instance or ended. An error is asked again.
	ReportProvisionOutcome(ctx context.Context, id sandbox.Identity, requestID string, status OutcomeStatus) (bool, error)
}
