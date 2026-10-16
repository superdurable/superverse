// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package teamspaces

import (
	"context"
	"errors"
	"fmt"
)

// ErrProjectClosed is a Project whose execution closed before it was
// DELETED, which no deletion request reaches any more; DeleteForTeamSpace
// wraps it with the Project and its last status.
var ErrProjectClosed = errors.New("the Project's execution closed before it was DELETED")

// ProjectCleanup deletes the Projects of a deleting TeamSpace. The Project
// package implements it and the bootstrap injects it, since the Project
// package imports this one.
type ProjectCleanup interface {
	// DeleteForTeamSpace asks the Project to delete itself under the deletion
	// command commandID, unless it already is deleting, and reports whether it
	// is DELETED. It is idempotent; a Project that is not ready for the
	// request, such as one running another command, reports false, and one
	// whose execution closed undeleted is ErrProjectClosed.
	DeleteForTeamSpace(ctx context.Context, projectID, commandID string) (bool, error)
}

// ConnectionCleanup deletes the connections of a TeamSpace whose Projects are
// all deleted. The connections package implements it and the bootstrap
// injects it.
type ConnectionCleanup interface {
	// DeleteTeamSpaceConnections deletes a bounded part of the TeamSpace's
	// connections, their usages and their objects, and reports whether
	// nothing is left. It is idempotent and never refused because of usages.
	DeleteTeamSpaceConnections(ctx context.Context, teamSpaceID string) (done bool, err error)
}

// The codes and phase of the failures a TeamSpace records.
const (
	failureDeletionFailed  = "DELETION_FAILED"
	failureDeletionTimeout = "DELETION_TIMEOUT"
	failureProjectClosed   = "PROJECT_CLOSED"
	failureDeletionPhase   = "DeleteTeamSpaceResources"
)

// deleteProjects asks the first deletionBatch pending Projects to delete
// themselves and returns those still pending: the Projects it did not reach,
// then those not DELETED yet, so every Project is asked in turn.
func (f *TeamSpaceFlow) deleteProjects(ctx context.Context, pending []string, commandID string) ([]string, error) {
	batch := pending[:min(len(pending), deletionBatch)]
	remaining := append([]string{}, pending[len(batch):]...)
	for _, projectID := range batch {
		deleted, err := f.Projects.DeleteForTeamSpace(ctx, projectID, commandID)
		if err != nil {
			return nil, fmt.Errorf("delete Project %s of the TeamSpace: %w", projectID, err)
		}
		if !deleted {
			remaining = append(remaining, projectID)
		}
	}
	return remaining, nil
}
