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
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

// projectCleanupLockWait bounds the completion call of one Project cleanup
// pass, including its wait for a Session Step that holds the RPC's lock. Past
// it the pass returns the *dex.RPCLockConflictError, or the call's
// DeadlineExceeded when the Session answers more slowly, as it can while its
// engine moves to a new run. Project keeps the Session pending for either and
// asks again in its next pass, which reads the Session first; a longer wait
// would spend the round timeout of the Project Step that cleans several
// children.
const projectCleanupLockWait = 2 * time.Second

// CleanupProjectSession performs one reconciliation pass of a deleting
// Project's one Session. ProjectFlow owns polling and may acknowledge
// membership only after true; no public API exposes this path. The pass never
// depends on Session Steps being runnable:
//
//  1. A running Session is completed once: CompleteProjectSession cancels every
//     branch, a workspace command it sends included, archives it and
//     completes it. A Session whose engine already ended abnormally is not
//     asked, and a Session archived for the cleanup is not asked again.
//  2. The Session's SessionWorkspaceFlow is ended: CompleteSessionWorkspace
//     refuses later commands, cancels a provisioning or reclamation and
//     completes. A provisioning of the instance it recorded that may still
//     run is waited for, so that nothing is created after the release.
//  3. Every subagent of the Session that has not ended is stopped and waited
//     for: subagents are not Project children, so they are found by search.
//  4. The Project's Preview is ended, and its resources are released only
//     once it no longer runs.
//  5. Every resource of the Session is released by label and owner through
//     the instance provider: the Pod, the Preview's route objects, grants and
//     data, the workspace volume and the namespace.
//
// It answers true only once the Session and its workspace Flow have ended
// and every resource is absent. The workspace Flow is read again after the
// Session's completion, which no Step of the Session outlives, so a command
// that a cancelled Step sent late is ended by the next pass. A typed refusal keeps its classification in the Project's deletion
// state: a provider's access denial, an owner or UID conflict, a missing
// owner or a receipt that does not match.
func (s *SessionFlowService) CleanupProjectSession(ctx context.Context, projectID string) (bool, error) {
	s.logColdPath(ctx, "CleanupProjectSession", projectID)
	project, err := s.Projects.ReadSessionProject(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("read Project %s of its Session: %w", projectID, err)
	}
	if project.ProjectStatus != projects.StatusDeleting {
		return false, fmt.Errorf("project deletion is not admitted: Project %s is %s", projectID, project.ProjectStatus)
	}
	deleted, err := s.RemoteConnections.DeleteProject(ctx, projectID, projects.StatusDeleting, projects.StatusDeleted)
	if err != nil || !deleted {
		return false, err
	}
	id := agentloop.SessionFlowID(projectID)
	session, err := s.Agent.GetSessionSummary(ctx, id)
	var missing *dex.FlowNotFoundError
	sessionMissing := errors.As(err, &missing)
	if err != nil && !sessionMissing {
		return false, fmt.Errorf("read the Session of Project %s: %w", projectID, err)
	}
	abnormal := false
	if !sessionMissing {
		// A retained read succeeds after the engine failed, was canceled or was
		// terminated; such a Session cannot complete and is discharged below by
		// exact membership and resource absence.
		if abnormal, err = s.Agent.HasAbnormalProjectSessionTermination(ctx, id); err != nil {
			return false, fmt.Errorf("read the engine status of the Session of Project %s: %w", projectID, err)
		}
		archived := session.Status == agentloop.SessionStatusArchived && session.CloseReason == agentloop.CloseReasonProjectCleanupComplete.String()
		if !abnormal && !archived {
			if err = s.Agent.CompleteProjectSession(ctx, projectID, projectCleanupLockWait); err != nil {
				return false, fmt.Errorf("complete the Session of Project %s: %w", projectID, err)
			}
		}
	}
	instance, ended, err := s.endSessionWorkspace(ctx, projectID)
	if err != nil || !ended {
		return false, err
	}
	if stopped, stopErr := s.stopSessionSubagents(ctx, projectID); stopErr != nil || !stopped {
		return false, stopErr
	}
	if closed, closeErr := s.closeSessionContributions(ctx, projectID); closeErr != nil || !closed {
		return false, closeErr
	}
	if ended, err = s.Previews.End(ctx, projectID); err != nil || !ended {
		return false, err
	}
	identity := sandbox.Identity{ProjectID: projectID}
	if released, releaseErr := s.releaseSessionResources(ctx, identity, instance.Resources); releaseErr != nil || !released {
		return false, releaseErr
	}
	switch {
	case sessionMissing:
		return false, projects.ErrCleanupOwnerMissing
	case abnormal:
		// Failed, canceled and terminated engines cannot write a graceful receipt.
		// Exact membership and resource absence, rather than engine closure alone,
		// allow Project to discharge their cleanup responsibility.
		current, readErr := s.Projects.ReadSessionProject(ctx, projectID)
		if readErr != nil {
			return false, fmt.Errorf("read Project %s again: %w", projectID, readErr)
		}
		if current.ProjectStatus != projects.StatusDeleting {
			return false, fmt.Errorf("project deletion is no longer admitted: Project %s is %s", projectID, current.ProjectStatus)
		}
		return true, nil
	}
	completed, err := s.Agent.ProjectSessionCompleted(ctx, projectID)
	if errors.Is(err, sessionagent.ErrSessionCleanupReceiptMismatch) {
		return false, fmt.Errorf("%w: %w", projects.ErrCleanupReceiptMismatch, err)
	}
	if err != nil || !completed {
		return false, err
	}
	_, ended, err = s.Workspaces.End(ctx, projectID)
	return ended, err
}

// endSessionWorkspace ends the Session's SessionWorkspaceFlow and waits for a
// provisioning of the instance it recorded, which may still run, to end. It
// returns that instance, whose resources the release then finds by label.
func (s *SessionFlowService) endSessionWorkspace(ctx context.Context, projectID string) (sessionworkspace.Instance, bool, error) {
	state, ended, err := s.Workspaces.End(ctx, projectID)
	if err != nil || !ended {
		return sessionworkspace.Instance{}, false, err
	}
	instance := state.Instance
	if instance.InstanceID == "" {
		return instance, true, nil
	}
	provisioning, err := s.ObserveProvisioning(ctx, projectID, instance.InstanceID)
	if err != nil || provisioning == nil {
		return sessionworkspace.Instance{}, false, err
	}
	if instance.Resources == (sandbox.Resources{}) {
		instance.Resources = provisioning.Resources
	}
	return instance, true, nil
}

// releaseSessionResources releases, by label and owner, every resource of a
// deleting Project's Session and reports whether all of them are gone. It
// finds the namespace, the volume and the Sandbox by their deterministic
// names, checking the Sandbox's owner labels and any UID the Session
// recorded, so a Session that recorded nothing is released too. Each part
// queries its exact object before it deletes, with the UID as a
// precondition, so a pass that was cut off is settled by the next one. No
// dirty workspace is protected: deleting the Project discards it.
func (s *SessionFlowService) releaseSessionResources(ctx context.Context, id sandbox.Identity, known sandbox.Resources) (bool, error) {
	instance := sandbox.Instance{Identity: id, Generation: sandbox.InstanceGeneration, Resources: known}
	found, err := s.Instances.DiscoverResources(ctx, instance)
	if err != nil {
		return false, fmt.Errorf("find the Sandbox resources of Project %s: %w", id.ProjectID, err)
	}
	instance.Resources = found
	var done bool
	// Deleting the Pod stops the Preview application and its engine with it.
	if done, err = s.Instances.DeletePod(ctx, instance); err != nil || !done {
		return false, wrapRelease("the Sandbox", err)
	}
	instance.Resources = found.WithoutPod()
	// A stop removes the Preview's route; this release removes one that a
	// Preview ended by the deletion while it served left behind.
	if done, err = s.Instances.ReleasePreviewRoute(ctx, instance); err != nil || !done {
		return false, wrapRelease("the Preview route", err)
	}
	if done, err = s.PreviewData.DeletePreviewData(ctx, id.ProjectID, s.Instances.SessionBoundary(id.ProjectID)); err != nil || !done {
		return false, wrapRelease("the Preview data", err)
	}
	if done, err = s.Instances.DeleteVolume(ctx, instance); err != nil || !done {
		return false, wrapRelease("the workspace volume", err)
	}
	done, err = s.Instances.DeleteNamespace(ctx, instance)
	return done, wrapRelease("the Session namespace", err)
}

// wrapRelease names the resource whose release failed, keeping the typed cause.
func wrapRelease(resource string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("release %s: %w", resource, err)
}
