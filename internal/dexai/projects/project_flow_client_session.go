// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

import (
	"context"
	"errors"

	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// RegisterSessionChild admits the Project's already-started Session as its
// only Session-scoped child, under the key session-<projectID>. The Session
// owns its Sandbox instances; they are never Project children. False is an
// authoritative denial before any effect.
func (c *ProjectFlowClient) RegisterSessionChild(ctx context.Context, projectID string) (bool, error) {
	err := c.AdmitChild(ctx, projectID, ChildSession, flowid.SessionFlowID(projectID))
	if errors.Is(err, ErrChildAdmissionClosed) {
		return false, nil
	}
	return err == nil, err
}

// ReadSessionProject reads the Project of its registered Session. A Session
// the Project never admitted or already acknowledged, and the Session of a
// deleted Project, has no authority over the Project.
func (c *ProjectFlowClient) ReadSessionProject(ctx context.Context, projectID string) (Project, error) {
	project, err := c.Read(ctx, projectID)
	if err != nil {
		return Project{}, err
	}
	if project.ProjectStatus == StatusDeleted {
		return Project{}, ErrForbidden
	}
	child, err := c.readChild(ctx, projectID, flowid.SessionFlowID(projectID))
	if err != nil {
		return Project{}, err
	}
	if child.FlowType != ChildSession.FlowType() {
		return Project{}, ErrForbidden
	}
	return project, nil
}
