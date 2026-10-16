// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

func invalidBuildRevision() error {
	return dexflow.Reject(dexflow.RejectionInvalidArgument, "build revision requires a build-UUIDv4 identity")
}

// readyProject authorizes actor on the READY Project it returns, ProjectFlow's
// current state, which the admission of a write requires.
func (c *ProjectReleaseFlowClient) readyProject(ctx context.Context, actor, project string) (projects.Project, error) {
	p, err := c.projects.AuthorizeCurrent(ctx, actor, project, projects.CapabilityBuild)
	if err != nil {
		return projects.Project{}, err
	}
	if p.ProjectStatus != projects.StatusReady {
		return projects.Project{}, dexflow.Reject(dexflow.RejectionFailedPrecondition, "project is not ready")
	}
	return p, nil
}

// BuildRevision admits one Save and Build of a prepared Release. The caller's
// build revision ID keeps the operation's identity across retries; the build
// recipe is always the composition's default.
func (c *ProjectReleaseFlowClient) BuildRevision(ctx context.Context, actor, project, release string, in BuildRevisionRequest) (string, error) {
	if !build.RevisionIDPattern.MatchString(in.BuildRevisionID) {
		return "", invalidBuildRevision()
	}
	p, err := c.readyProject(ctx, actor, project)
	if err != nil {
		return "", err
	}
	state, err := c.ReadSnapshot(ctx, project, release)
	if err != nil {
		return "", err
	}
	in.ActorAccountID = actor
	// Replay comes first: a retried request after a lost response keeps its accepted admission, whatever
	// Preview and Live did since, and the RPC compares only its input.
	_, err = c.ReadBuildRevision(ctx, project, release, in.BuildRevisionID)
	if rejected, isRejected := errors.AsType[*dexflow.Rejection](err); isRejected && rejected.HasCode(dexflow.RejectionNotFound) {
		if in.PreviewValidation, err = c.admitNewBuild(ctx, p, state, in); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return c.requestBuild(ctx, project, release, state, in)
}

// requestBuild asks the Release to admit in, whose validation admission
// decided, against the Release state the caller read.
func (c *ProjectReleaseFlowClient) requestBuild(ctx context.Context, project, release string, state Snapshot, in BuildRevisionRequest) (string, error) {
	// Admission reads the requested entry and the one active build it may supersede.
	in.PreviousBuildRevisionID = state.ActiveBuildRevisionID
	loads := []dex.AttributeMapLoad{ReleaseBuildRevisions.Load(in.BuildRevisionID)}
	locks := []dex.AttributeLock{dex.LockAttributeMap(ReleaseBuildRevisions, in.BuildRevisionID)}
	if in.PreviousBuildRevisionID != "" && in.PreviousBuildRevisionID != in.BuildRevisionID {
		loads = append(loads, ReleaseBuildRevisions.Load(in.PreviousBuildRevisionID))
		locks = append(locks, dex.LockAttributeMap(ReleaseBuildRevisions, in.PreviousBuildRevisionID))
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var result buildRevisionResult
	if err := c.projects.Dex.InvokeRPCWithOptions(ctx, comm.Must(FlowID(project, release)), c.flow.RequestReleaseBuildRevision, in, &result, dex.RPCInvokeOptions{LoadAttributeMapInstances: loads, LockAttributeMapInstances: locks}); err != nil {
		return "", err
	}
	if err := result.Rejection.Err(); err != nil {
		return "", err
	}
	return result.BuildRevisionID, nil
}

// admitNewBuild decides how a new build meets the publishing rule and checks its Live draft. The Release
// Live serves is LIVE_CODE; otherwise a READY Preview start of the commit validates it, and only without
// one does the builder's skip reason count. Gaps are left to the freeze, which itemizes them.
func (c *ProjectReleaseFlowClient) admitNewBuild(ctx context.Context, p projects.Project, s Snapshot, in BuildRevisionRequest) (*PreviewValidation, error) {
	if s.PreparedSource.Status != Ready {
		return nil, dexflow.Reject(dexflow.RejectionFailedPrecondition, "release source is not prepared")
	}
	var validation *PreviewValidation
	if live := p.CurrentLive; live != nil && live.Ready && live.ReleaseID == s.ReleaseID {
		validation = &PreviewValidation{Kind: PreviewLiveCode}
	} else {
		started, err := c.Previews.ReadPreviewValidation(ctx, p.ProjectID, s.SourceCommit)
		switch {
		case err != nil:
			return nil, fmt.Errorf("read the Preview starts of %s: %w", s.SourceCommit, err)
		case started != nil:
			validation = started
		case in.PreviewSkipReason != "":
			validation = &PreviewValidation{Kind: PreviewSkipped, SkipReason: in.PreviewSkipReason}
		default:
			return nil, ErrPreviewValidationRequired
		}
	}
	validation.ActorAccountID = in.ActorAccountID
	if err := c.flow.publishing.CheckReleaseDraft(ctx, in.ActorAccountID, s, in.ConfigurationRevision); err != nil {
		return nil, err
	}
	return validation, nil
}

// RecoverBuildRevision admits an explicit recovery of the Release's active
// build at its expected revision.
func (c *ProjectReleaseFlowClient) RecoverBuildRevision(ctx context.Context, actor, project, release string, in BuildRevisionRecovery) error {
	if !build.RevisionIDPattern.MatchString(in.BuildRevisionID) {
		return invalidBuildRevision()
	}
	flowID, err := FlowID(project, release)
	if err != nil {
		return err
	}
	if _, err = c.readyProject(ctx, actor, project); err != nil {
		return err
	}
	in.ActorAccountID = actor
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var result buildRevisionResult
	err = c.projects.Dex.InvokeRPCWithOptions(ctx, flowID, c.flow.RequestReleaseBuildRecovery, in, &result, dex.RPCInvokeOptions{LoadAttributeMapInstances: []dex.AttributeMapLoad{ReleaseBuildRevisions.Load(in.BuildRevisionID)}, LockAttributeMapInstances: []dex.AttributeLock{dex.LockAttributeMap(ReleaseBuildRevisions, in.BuildRevisionID)}})
	if err != nil {
		return err
	}
	return result.Rejection.Err()
}

// ReadBuildRevision reads one exact retained map entry. The caller owns
// Project authorization.
func (c *ProjectReleaseFlowClient) ReadBuildRevision(ctx context.Context, project, release, buildRevisionID string) (ReleaseBuildRevision, error) {
	if !build.RevisionIDPattern.MatchString(buildRevisionID) {
		return ReleaseBuildRevision{}, dexflow.Reject(dexflow.RejectionInvalidArgument, "invalid release build revision")
	}
	flowID, err := FlowID(project, release)
	if err != nil {
		return ReleaseBuildRevision{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var result ReleaseBuildRevision
	if err = dexflow.InvokeRetainedRead(ctx, c.projects.Dex, flowID, c.flow.GetReleaseBuildRevision, buildRevisionID, &result, ReleaseBuildRevisions.Load(buildRevisionID)); err != nil {
		return ReleaseBuildRevision{}, err
	}
	if result.BuildRevisionID == "" {
		return ReleaseBuildRevision{}, dexflow.Reject(dexflow.RejectionNotFound, "release build revision was not found")
	}
	return result, nil
}

// ReadBuildSnapshot reads the Release with one of its build revisions. The
// caller owns Project authorization.
func (c *ProjectReleaseFlowClient) ReadBuildSnapshot(ctx context.Context, project, release, buildRevisionID string) (BuildSnapshot, error) {
	state, err := c.ReadSnapshot(ctx, project, release)
	if err != nil {
		return BuildSnapshot{}, err
	}
	record, err := c.ReadBuildRevision(ctx, project, release, buildRevisionID)
	if err != nil {
		return BuildSnapshot{}, err
	}
	return BuildSnapshot{Release: state, Build: record}, nil
}

// ReadBuildLog reads one page of the provider log of a build revision as read
// asks; a build no provider build of which ran has an empty, complete page. It
// is read on demand and never stored; the log is build output, so the caller
// redacts it. The caller read the Release and its revision and owns Project
// authorization.
func (c *ProjectReleaseFlowClient) ReadBuildLog(ctx context.Context, snapshot BuildSnapshot, read build.LogRead) (build.LogPage, error) {
	if snapshot.Build.Configuration == nil {
		return build.LogPage{Next: read.Cursor, Complete: true}, nil
	}
	request, err := c.flow.revisionRequest(snapshot.Release, snapshot.Build)
	if err != nil {
		return build.LogPage{}, err
	}
	return c.flow.builder.ReadLog(ctx, request, read)
}

// CancelBuildRevision admits a person's cancellation of the Release's active
// build until its artifacts commit. A build that is deploying is refused with
// BuildDeployingError naming its deployment, one that is not in progress with
// ErrBuildNotCancellable.
func (c *ProjectReleaseFlowClient) CancelBuildRevision(ctx context.Context, actor, project, release string, in BuildRevisionCancellation) error {
	if !build.RevisionIDPattern.MatchString(in.BuildRevisionID) {
		return invalidBuildRevision()
	}
	flowID, err := FlowID(project, release)
	if err != nil {
		return err
	}
	if _, err = c.readyProject(ctx, actor, project); err != nil {
		return err
	}
	in.ActorAccountID = actor
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var result buildRevisionResult
	err = c.projects.Dex.InvokeRPCWithOptions(ctx, flowID, c.flow.RequestReleaseBuildCancellation, in, &result, dex.RPCInvokeOptions{LoadAttributeMapInstances: []dex.AttributeMapLoad{ReleaseBuildRevisions.Load(in.BuildRevisionID)}, LockAttributeMapInstances: []dex.AttributeLock{dex.LockAttributeMap(ReleaseBuildRevisions, in.BuildRevisionID)}})
	if _, ended := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); ended {
		// A Release that does not run answers from its retained build: missing, cancelled, or ended.
		retained, err := c.ReadBuildRevision(ctx, project, release, in.BuildRevisionID)
		if err != nil {
			return err
		}
		if retained.Cancellation != nil {
			return nil
		}
		return ErrBuildNotCancellable
	}
	if err != nil {
		return err
	}
	switch result.Refusal {
	case refusalDeploying:
		return &BuildDeployingError{deploymentID: result.DeploymentID}
	case refusalNotCancellable:
		return fmt.Errorf("%w: %w", ErrBuildNotCancellable, result.Rejection)
	}
	return result.Rejection.Err()
}
