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
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/deliverydao"
	"github.com/superdurable/superversev2/internal/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
)

// ProjectReleaseFlowClient starts, reads and changes Releases. It reaches Dex
// through the Project client; flow only names the registered Flow's RPCs and
// the optional builder.
type ProjectReleaseFlowClient struct {
	flow     *ProjectReleaseFlow
	projects *projects.ProjectFlowClient
	profiles *build.Profiles
	// Rows reads the Releases' read model; composition sets it.
	Rows *deliverydao.DeliveryDAO
	// Previews reads the Preview starts that validate a build; composition
	// sets it.
	Previews PreviewValidationReader
}

func NewProjectReleaseFlowClient(flow *ProjectReleaseFlow, projectClient *projects.ProjectFlowClient, profiles *build.Profiles) *ProjectReleaseFlowClient {
	return &ProjectReleaseFlowClient{flow: flow, projects: projectClient, profiles: profiles}
}

// FlowID is release-<project>-<commit>; the Release is the main commit.
func FlowID(project, release string) (string, error) {
	return flowid.ProjectChild("release", project, release)
}

// ReadSnapshot reads a retained Release; the caller owns authorization.
func (c *ProjectReleaseFlowClient) ReadSnapshot(ctx context.Context, project, release string) (Snapshot, error) {
	id, err := FlowID(project, release)
	if err != nil {
		return Snapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out Snapshot
	err = dexflow.InvokeRetainedRead(ctx, c.projects.Dex, id, c.flow.GetProjectRelease, nil, &out)
	return out, err
}

// Prepare starts the independent owner of one main commit and returns only
// its acceptance. A different request for the same commit joins the accepted
// Release; a failed source preparation is retried under the new request.
func (c *ProjectReleaseFlowClient) Prepare(ctx context.Context, actor, project, requestID, commit string) (string, error) {
	if !flowid.IsCommitSHA(commit) {
		return "", dexflow.Reject(dexflow.RejectionInvalidArgument, "release requires a full lowercase commit SHA")
	}
	if c.flow.builder == nil {
		return "", dexflow.Reject(dexflow.RejectionFailedPrecondition, "release builds are not configured")
	}
	p, err := c.readyProject(ctx, actor, project)
	if err != nil {
		return "", err
	}
	profile := c.profiles.Default()
	fingerprint := flowid.Fingerprint(struct {
		Project, Commit, Branch string
		RepositoryID            int64
		Generation              uint64
	}{project, commit, "main", p.RepositoryBinding.RepositoryID, p.RepositoryBinding.Generation})
	startID := flowid.Fingerprint(struct{ Actor, RequestID, Fingerprint, Profile, Digest string }{actor, requestID, fingerprint, profile.Version, profile.Digest})
	flowID := comm.Must(FlowID(project, commit))
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	durability := dex.StepDurabilityAsync
	_, err = c.projects.Dex.StartFlow(ctx, c.flow, flowID, nil, dex.StartFlowOptions{RequestID: &startID, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: initialAttributes(releaseStart{identity: dexflow.StartIdentity{RequestID: startID, Fingerprint: fingerprint, ActorAccountID: actor}, projectID: project, sourceCommit: commit, buildProfileVersion: profile.Version, buildProfileDigest: profile.Digest, repository: p.RepositoryBinding}), ConfigOverride: &dex.FlowConfig{StepDurability: &durability, AttributeStoreNames: postgres.ReleaseStore.Names()}})
	if err == nil {
		return commit, nil
	}
	var started *dex.FlowAlreadyStartedError
	if !errors.As(err, &started) {
		return "", err
	}
	accepted, err := c.ReadSnapshot(ctx, project, commit)
	switch {
	case err != nil:
		return "", err
	case accepted.Identity.Fingerprint != fingerprint:
		return "", dexflow.Reject(dexflow.RejectionAlreadyExists, "release identity conflicts with accepted source")
	case accepted.CancellationRequested || accepted.Status == Removed:
		return "", dexflow.Reject(dexflow.RejectionFailedPrecondition, "release is being removed")
	case accepted.PreparedSource.Status != Failed:
		return commit, nil
	}
	recoveryID := flowid.Fingerprint(struct{ Actor, RequestID, Fingerprint, Profile, Digest string }{actor, requestID, accepted.Identity.Fingerprint, accepted.BuildProfileVersion, accepted.BuildProfileDigest})
	var result mutationResult
	if err = c.projects.Dex.InvokeRPC(ctx, flowID, c.flow.RequestReleaseSourcePreparation, mutation{RequestID: recoveryID, ExpectedRevision: accepted.Revision}, &result); err != nil {
		return "", err
	}
	if err = result.Rejection.Err(); err != nil {
		return "", err
	}
	return commit, nil
}

// RemoveForProjectCleanup admits removal of a Release by its deleting Project
// and reports whether the Release completed its cleanup.
func (c *ProjectReleaseFlowClient) RemoveForProjectCleanup(ctx context.Context, project, release, requestID string) (bool, error) {
	state, err := c.ReadSnapshot(ctx, project, release)
	if err != nil {
		return false, err
	}
	if state.Status == Removed {
		return true, nil
	}
	var out mutationResult
	if err = c.projects.Dex.InvokeRPC(ctx, comm.Must(FlowID(project, release)), c.flow.RequestReleaseRemoval, mutation{RequestID: requestID, ExpectedRevision: state.Revision}, &out); err != nil {
		return false, err
	}
	if err = out.Rejection.Err(); err != nil {
		return false, err
	}
	return out.Release.Status == Removed, nil
}
