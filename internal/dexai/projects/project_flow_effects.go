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
	"fmt"
	"slices"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// The child lifecycle ports let the permanent owner start and clean up its
// children without importing their packages.
type SetupLifecycle interface {
	// StartForProject starts the Project's accepted Setup.
	StartForProject(ctx context.Context, project Project) error
	// RemoveForProjectCleanup asks a Setup to stop for Project deletion and
	// reports whether its cleanup completed.
	RemoveForProjectCleanup(ctx context.Context, flowID string) (bool, error)
}

// SessionLifecycle starts the READY Project's one Session and cleans it up for
// the Project's deletion.
type SessionLifecycle interface {
	// StartForProject starts the Session of the READY project with the
	// creator's model and first message, if any; a repeated start joins its
	// execution, which keeps the first start's message.
	StartForProject(ctx context.Context, project Project, initial *SessionStartRequest) error
	// CleanupProjectSession performs one cleanup pass of the deleting
	// Project's Session and reports whether it ended and released everything.
	CleanupProjectSession(ctx context.Context, projectID string) (bool, error)
}

// ConnectionUsages releases the TeamSpace connection usages and scope tokens
// of a deleting Project; the connections package implements it.
type ConnectionUsages interface {
	// ReleaseConnectionUsages deletes the Project's usages and scope tokens. It
	// is idempotent.
	ReleaseConnectionUsages(ctx context.Context, projectID string) error
}

type ReleaseCleanup interface {
	RemoveForProjectCleanup(ctx context.Context, projectID, releaseID, requestID string) (bool, error)
}

type DeploymentCleanup interface {
	RemoveForProjectCleanup(ctx context.Context, projectID, deploymentID, requestID string) (bool, error)
	RevokeForLiveRemoval(ctx context.Context, projectID, deploymentID, requestID string) error
	RemoveServing(ctx context.Context, project Project) error
}

func (f *ProjectFlow) removeLiveServing(ctx context.Context, p Project) error {
	if p.CurrentLive == nil {
		return nil
	}
	return f.Deployments.RemoveServing(ctx, p)
}

func (f *ProjectFlow) revokeDeploymentGrant(ctx context.Context, p Project) error {
	if p.CurrentLive == nil {
		return nil
	}
	return f.Deployments.RevokeForLiveRemoval(ctx, p.ProjectID, p.CurrentLive.DeploymentID, p.ActiveCommand.ID)
}

// stopAndCleanChildren asks the owners of at most cleanupBatch children to
// clean them up, acknowledges each child whose cleanup completed and returns
// the children still registered: those it did not reach first, then the
// pending ones, so a child that stays pending cannot starve the others.
// Sessions are stopped before Releases, whose shared resources a Deployment
// may still use, so a Release waits while any Deployment remains.
func (f *ProjectFlow) stopAndCleanChildren(ctx context.Context, p Project, children []ChildFlowReference) ([]ChildFlowReference, error) {
	hasDeployment := slices.ContainsFunc(children, func(child ChildFlowReference) bool { return child.FlowType == ChildDeploy.FlowType() })
	batch := children[:min(cleanupBatch, len(children))]
	remaining := slices.Clone(children[len(batch):])
	for _, child := range batch {
		gone, err := f.cleanUpRegisteredChild(ctx, p, child, hasDeployment)
		switch {
		case childCallPending(ctx, err):
			// The next round reads the child again before it repeats the call.
			remaining = append(remaining, child)
		case err != nil:
			return nil, fmt.Errorf("clean up child %s: %w", child.FlowID, err)
		case !gone:
			remaining = append(remaining, child)
		}
	}
	return remaining, nil
}

// cleanUpRegisteredChild asks the owner of a child that is still registered to
// clean it up, acknowledges the child once its owner confirmed the cleanup and
// reports whether the child left the membership. A Release waits while
// hasDeployment.
func (f *ProjectFlow) cleanUpRegisteredChild(ctx context.Context, p Project, child ChildFlowReference, hasDeployment bool) (bool, error) {
	// A child cleaned up earlier may have acknowledged this entry too.
	membership, err := f.Client.readChild(ctx, p.ProjectID, child.FlowID)
	if err != nil {
		return false, fmt.Errorf("read the membership: %w", err)
	}
	if membership.FlowID == "" {
		return true, nil
	}
	// An entry is written once, its FlowID fixes its FlowType, and a
	// deleting Project admits no registration.
	if membership != child {
		panic("child admission " + child.FlowID + " changed during cleanup")
	}
	if child.FlowType == ChildRelease.FlowType() && hasDeployment {
		return false, nil
	}
	done, err := f.cleanUpChild(ctx, p, child)
	if err != nil || !done {
		return false, err
	}
	if err = f.Client.AcknowledgeChild(ctx, p.ProjectID, child); err != nil {
		return false, fmt.Errorf("acknowledge the cleanup: %w", err)
	}
	return true, nil
}

// childCallPending reports whether a call that reads, cleans up or
// acknowledges a child leaves only that child pending, so the round goes on
// with the others and the next round repeats the call: the call met lock
// contention or went unanswered. Each such call is an idempotent Dex call to
// the child's owner or to this Project, whose state confirms its effect: the
// child's cleanup receipt or the membership.
func childCallPending(round context.Context, err error) bool {
	var conflict *dex.RPCLockConflictError
	return errors.As(err, &conflict) || unansweredCall(round, err)
}

// cleanUpChild asks one child's owner to clean it up and reports whether it is gone.
func (f *ProjectFlow) cleanUpChild(ctx context.Context, p Project, child ChildFlowReference) (bool, error) {
	requestID := "project-delete-" + p.ProjectID
	switch childFlows[child.FlowType] {
	case ChildSetup:
		return f.Setups.RemoveForProjectCleanup(ctx, child.FlowID)
	case ChildSession:
		// The Session's cleanup also releases every Sandbox resource it owns.
		return f.Sessions.CleanupProjectSession(ctx, p.ProjectID)
	case ChildRelease:
		return f.Releases.RemoveForProjectCleanup(ctx, p.ProjectID, registeredChildID(p.ProjectID, child), requestID)
	case ChildDeploy:
		return f.Deployments.RemoveForProjectCleanup(ctx, p.ProjectID, registeredChildID(p.ProjectID, child), requestID)
	}
	// Registration admits only the child types above.
	panic("registered child " + child.FlowID + " has unsupported type " + child.FlowType)
}

// registeredChildID is the child's ID within the Project. Registration admits
// only FlowIDs that parse, so a failure means corrupted membership.
func registeredChildID(projectID string, child ChildFlowReference) string {
	id, ok := flowid.ParseProjectChild(childFlows[child.FlowType].kind, projectID, child.FlowID)
	if !ok {
		panic("registered child " + child.FlowID + " does not belong to project " + projectID)
	}
	return id
}

// destroyLiveDataAndInfrastructure runs after every child acknowledged its
// cleanup, so no Setup or deployment can still extend the Project's resource
// identities.
func (f *ProjectFlow) destroyLiveDataAndInfrastructure(ctx context.Context, p Project) error {
	if err := f.Cleanup.DestroyScope(ctx, p.ProjectID, infra.ScopeRefs{Storage: p.Storage, Execution: p.LiveExecution}); err != nil {
		if errors.Is(err, infra.ErrPending) {
			return fmt.Errorf("%w: %w", ErrCleanupPending, err)
		}
		return fmt.Errorf("destroy the Live scope: %w", err)
	}
	done, err := f.Objects.DeleteProjectObjects(ctx, p.ProjectID)
	if err != nil {
		return fmt.Errorf("delete the project objects: %w", err)
	}
	if !done {
		return fmt.Errorf("%w: project objects remain", ErrCleanupPending)
	}
	return nil
}

// releaseRepositoryBinding ends the Project's exact repository binding, which
// completes it, once archiveManagedRepository archived the repository.
func (f *ProjectFlow) releaseRepositoryBinding(ctx context.Context, p Project) error {
	if p.RepositoryBinding.RepositoryID == 0 {
		return nil
	}
	return f.Repositories.Release(ctx, p.ProjectID, p.RepositoryBinding.RepositoryID, p.RepositoryBinding.Generation)
}

// archiveManagedRepository archives the repository the Project committed.
// Setup owns an uncommitted one, and StopAndCleanChildren confirmed its cleanup.
// A repository bound under another repository host is refused: this server's
// host would read it as absent, which archival takes for success.
func (f *ProjectFlow) archiveManagedRepository(ctx context.Context, p Project) error {
	binding := p.RepositoryBinding
	if binding.RepositoryID == 0 {
		return nil
	}
	if err := repohost.CheckInstallation(f.Host, binding.InstallationID); err != nil {
		return fmt.Errorf("archive repository %s: %w", binding.FullName, err)
	}
	return f.Host.ArchiveManaged(ctx, p.ProjectID, p.SetupID(), binding.FullName)
}
