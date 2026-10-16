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
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

// errRepositoryNotReady rejects a Project without the repository binding and
// AppManifest that every Session works on.
var errRepositoryNotReady = errors.New("project repository is not ready")

// authorizeProject admits the account to the Project, as the read model holds
// it. Only a READY Project with its repository and AppManifest starts a
// Session, so the Session's existence proves them, and the Session's own Flow
// admits every write.
func (s *SessionFlowService) authorizeProject(ctx context.Context, account, projectID string) error {
	_, err := s.Projects.Authorize(ctx, account, projectID, projects.CapabilityBuild)
	return err
}

// sandboxRepository is the repository the Sandbox of a Project's Session checks
// out. A Project without a bound repository and AppManifest has none.
func sandboxRepository(project projects.Project) (sandbox.Repository, error) {
	binding := project.RepositoryBinding
	owner, name, found := strings.Cut(binding.FullName, "/")
	if !found || strings.Contains(name, "/") || project.AppManifest == nil {
		return sandbox.Repository{}, errRepositoryNotReady
	}
	return sandbox.Repository{InstallationID: binding.InstallationID, RepositoryID: binding.RepositoryID, Owner: owner, Name: name, CloneURL: binding.URL, DefaultBranch: binding.DefaultBranch}, nil
}

// sandboxIdentity is the identity of the Session's Sandbox instances, which
// check out the Session's HEAD.
func sandboxIdentity(project projects.Project, id agentloop.SessionIdentity) (sandbox.Identity, error) {
	repository, err := sandboxRepository(project)
	if err != nil {
		return sandbox.Identity{}, err
	}
	repository.HeadCommit = id.HeadCommit
	return sandbox.Identity{ProjectID: id.ProjectID, Repository: repository}, nil
}

// AdmitSession runs only from the Session's first durable Step, after StartFlow:
// it registers session-<projectID> as the Project's only Session-scoped child.
func (s *SessionFlowService) AdmitSession(ctx context.Context, id agentloop.SessionIdentity) (bool, error) {
	return s.Projects.RegisterSessionChild(ctx, id.ProjectID)
}
