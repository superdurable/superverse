// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/deliverydao"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// ProjectDeployFlowClient starts, reads and cleans up deployments. It reaches
// Dex through the Project client; the SDK resolves flow, a type reference, to
// the registered worker Flow.
type ProjectDeployFlowClient struct {
	flow     *ProjectDeployFlow
	projects *projects.ProjectFlowClient
	// serving removes the current Live generation when the Project is cleaned
	// up.
	serving Serving
	grants  infra.Grants
	// Releases reads the build an explicit deployment names. Composition sets
	// it as a back reference, because the Release client is built on this one.
	Releases *releases.ProjectReleaseFlowClient
	// Rows reads the deployments' read model and the Live target; composition
	// sets it.
	Rows *deliverydao.DeliveryDAO
}

func NewProjectDeployFlowClient(projectClient *projects.ProjectFlowClient, serving Serving, grants infra.Grants) *ProjectDeployFlowClient {
	return &ProjectDeployFlowClient{flow: &ProjectDeployFlow{}, projects: projectClient, serving: serving, grants: grants}
}

// FlowID is deploy-<project>-<deployment>.
func FlowID(project, deployment string) (string, error) {
	return flowid.ProjectChild("deploy", project, deployment)
}

// DeployReadyBuild is a caller's explicit deployment: it requires the exact
// build READY with the configuration revision it froze, then starts or joins
// its deployment.
func (c *ProjectDeployFlowClient) DeployReadyBuild(ctx context.Context, account, requestID string, in Start) (string, error) {
	p, err := c.admit(ctx, account, in)
	if err != nil {
		return "", err
	}
	build, err := c.Releases.ReadBuildRevision(ctx, in.ProjectID, in.ReleaseID, in.BuildRevisionID)
	if err != nil {
		return "", err
	}
	if build.Status != releases.Ready || build.Configuration == nil || build.Configuration.ConfigurationRevision != in.ConfigurationRevision {
		return "", dexflow.Reject(dexflow.RejectionFailedPrecondition, "exact Release build and configuration are not ready")
	}
	return c.start(ctx, account, requestID, p, in)
}

// admit checks the identities that name Live resources, which automatic
// deployments bring without the API's validation, and authorizes the account
// on the READY Project it returns, ProjectFlow's current state.
func (c *ProjectDeployFlowClient) admit(ctx context.Context, account string, in Start) (projects.Project, error) {
	if _, err := FlowID(in.ProjectID, in.DeploymentID); err != nil {
		return projects.Project{}, err
	}
	if err := flowid.ValidateDeploymentID(in.DeploymentID); err != nil {
		return projects.Project{}, err
	}
	if in.ConfigurationRevision == 0 {
		return projects.Project{}, dexflow.Reject(dexflow.RejectionInvalidArgument, "deployment requires a frozen configuration revision")
	}
	p, err := c.projects.AuthorizeCurrent(ctx, account, in.ProjectID, projects.CapabilityBuild)
	if err != nil {
		return projects.Project{}, err
	}
	if p.ProjectStatus != projects.StatusReady {
		return projects.Project{}, dexflow.Reject(dexflow.RejectionFailedPrecondition, "project is not ready")
	}
	return p, nil
}

// start starts or joins the deployment of an admitted request on Project p.
// An unresolved deployment of the same build and configuration is the target
// even under another proposed ID, and so is a serving deployment whose lease
// release lost its response; any other desired deployment is rejected until
// its effects are reconciled. The caller's request ID becomes the first
// request receipt; Dex receives a fingerprint of the complete accepted input.
func (c *ProjectDeployFlowClient) start(ctx context.Context, account, requestID string, p projects.Project, in Start) (string, error) {
	if p.ActiveDeployment != nil {
		accepted, readErr := c.readSnapshot(ctx, in.ProjectID, p.ActiveDeployment.DeploymentID)
		if readErr != nil {
			return "", readErr
		}
		if accepted.Status != Cancelled {
			return c.join(ctx, p, accepted, requestID, in)
		}
		// Only an acquire admitted after its cleanup's read leaves a cancelled deployment's lease,
		// and that deployment ran no provider effect after it.
		if err := c.projects.ReleaseDeployment(ctx, in.ProjectID, *p.ActiveDeployment); err != nil {
			return "", fmt.Errorf("release the lease of the cancelled deployment %s: %w", accepted.DeploymentID, err)
		}
		if p, readErr = c.projects.Read(ctx, in.ProjectID); readErr != nil {
			return "", readErr
		}
		if p.ActiveDeployment != nil {
			return "", dexflow.Reject(dexflow.RejectionFailedPrecondition, "another desired deployment retains unresolved effects")
		}
	}
	if live := p.CurrentLive; live != nil {
		serving, readErr := c.readSnapshot(ctx, in.ProjectID, live.DeploymentID)
		if readErr != nil {
			return "", readErr
		}
		if serving.Status == Failed && serving.Providers.Phase == phaseReleaseLease && serving.ReleaseID == in.ReleaseID && serving.BuildRevisionID == in.BuildRevisionID && serving.ConfigurationRevision == in.ConfigurationRevision {
			// It must still be the exact Live generation whose lease release
			// lost its response.
			if serving.Lease.DeploymentID != live.DeploymentID || serving.Lease.Generation == 0 || serving.Lease.Generation != live.Revision || serving.ReleaseID != live.ReleaseID {
				return "", dexflow.Reject(dexflow.RejectionAborted, "serving deployment generation changed")
			}
			// The caller may have read Live before or after that commit.
			if in.ExpectedLiveRevision != serving.ExpectedLiveRevision && in.ExpectedLiveRevision != live.Revision {
				return "", dexflow.RejectStale("Live generation changed")
			}
			return c.resume(ctx, serving, requestID)
		}
	}
	fingerprint := flowid.Fingerprint(struct {
		Project, Deployment, Release, Account       string
		ConfigurationRevision, ExpectedLiveRevision uint64
		Build                                       string
	}{in.ProjectID, in.DeploymentID, in.ReleaseID, account, in.ConfigurationRevision, in.ExpectedLiveRevision, in.BuildRevisionID})
	// The complete logical input in Dex's request identity rejects a reused
	// request ID with different input.
	startID := flowid.Fingerprint(dexflow.StartIdentity{RequestID: requestID, Fingerprint: fingerprint, ActorAccountID: account})
	identity := dexflow.StartIdentity{RequestID: startID, Fingerprint: fingerprint, ActorAccountID: account}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	durability := dex.StepDurabilityAsync
	_, err := c.projects.Dex.StartFlow(ctx, c.flow, comm.Must(FlowID(in.ProjectID, in.DeploymentID)), nil, dex.StartFlowOptions{IDReusePolicy: dex.IDReuseDisallow, RequestID: &startID, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: initialAttributes(in, identity, requestID), ConfigOverride: &dex.FlowConfig{StepDurability: &durability, AttributeStoreNames: postgres.DeploymentStore.Names()}})
	if err == nil {
		return in.DeploymentID, nil
	}
	var started *dex.FlowAlreadyStartedError
	if !errors.As(err, &started) {
		return "", err
	}
	accepted, err := c.readSnapshot(ctx, in.ProjectID, in.DeploymentID)
	if err != nil {
		return "", err
	}
	if accepted.Identity.Fingerprint != fingerprint {
		return "", dexflow.Reject(dexflow.RejectionAlreadyExists, "deployment identity conflicts with accepted inputs")
	}
	return c.resume(ctx, accepted, requestID)
}

// join resumes the unresolved deployment accepted that holds the Project's
// lease, when it is the requested build and configuration; any other desired
// deployment is rejected until its effects are reconciled.
func (c *ProjectDeployFlowClient) join(ctx context.Context, p projects.Project, accepted Snapshot, requestID string, in Start) (string, error) {
	if accepted.ReleaseID != in.ReleaseID || accepted.BuildRevisionID != in.BuildRevisionID || accepted.ConfigurationRevision != in.ConfigurationRevision {
		return "", dexflow.Reject(dexflow.RejectionFailedPrecondition, "another desired deployment retains unresolved effects")
	}
	// The accepted deployment may already be Live, so a caller that read
	// Live after its commit expects its generation instead.
	if accepted.ExpectedLiveRevision != in.ExpectedLiveRevision && (p.CurrentLive == nil || p.CurrentLive.DeploymentID != accepted.DeploymentID || p.CurrentLive.Revision != in.ExpectedLiveRevision) {
		return "", dexflow.RejectStale("Live generation changed")
	}
	return c.resume(ctx, accepted, requestID)
}

// resume admits only continuation of the accepted immutable inputs and their
// unresolved generation.
func (c *ProjectDeployFlowClient) resume(ctx context.Context, accepted Snapshot, requestID string) (string, error) {
	if accepted.Status == Removed || accepted.Status == Retiring || accepted.Status == Retired || accepted.Status == Cancelled || accepted.cancelling() {
		return "", dexflow.Reject(dexflow.RejectionFailedPrecondition, "deployment cannot resume serving")
	}
	// A failed retirement resumes without the lease, which it released when it was Live.
	if accepted.Lease.Generation != 0 && accepted.Status == Failed && accepted.Providers.Phase != phaseReleaseLease && accepted.Providers.Phase != phaseRetire {
		project, err := c.projects.Read(ctx, accepted.ProjectID)
		if err != nil {
			return "", err
		}
		if project.ActiveDeployment == nil || *project.ActiveDeployment != accepted.Lease {
			return "", dexflow.Reject(dexflow.RejectionAborted, "deployment no longer owns its project generation")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := c.mutate(ctx, accepted, c.flow.RequestLiveDeployment, requestID); err != nil {
		return "", err
	}
	return accepted.DeploymentID, nil
}

// mutate invokes a Revision-guarded deployment RPC at the Revision of s. It
// returns the deployment after the call, or the call's rejection as the error.
func (c *ProjectDeployFlowClient) mutate(ctx context.Context, s Snapshot, rpc any, requestID string) (Snapshot, error) {
	var result mutationResult
	if err := c.projects.Dex.InvokeRPC(ctx, comm.Must(FlowID(s.ProjectID, s.DeploymentID)), rpc, mutation{RequestID: requestID, ExpectedRevision: s.Revision}, &result); err != nil {
		return Snapshot{}, err
	}
	return result.Deployment, result.Rejection.Err()
}

// readSnapshot reads a retained deployment from its Flow; the caller owns
// authorization.
func (c *ProjectDeployFlowClient) readSnapshot(ctx context.Context, project, deployment string) (Snapshot, error) {
	id, err := FlowID(project, deployment)
	if err != nil {
		return Snapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out Snapshot
	err = dexflow.InvokeRetainedRead(ctx, c.projects.Dex, id, c.flow.GetProjectDeployment, nil, &out)
	return out, err
}

// RemoveForProjectCleanup admits cleanup of a deployment by its deleting
// Project and reports whether it completed. The Project calls it only after
// its deletion committed Live absent, so the deployment no longer serves.
func (c *ProjectDeployFlowClient) RemoveForProjectCleanup(ctx context.Context, project, deployment, requestID string) (bool, error) {
	state, err := c.readSnapshot(ctx, project, deployment)
	if err != nil {
		return false, err
	}
	// A retired or cancelled generation's grant is revoked and its objects are gone.
	if state.Status == Removed && state.CleanupRequested || state.Status == Retired || state.Status == Cancelled {
		return true, nil
	}
	state, err = c.mutate(ctx, state, c.flow.RequestProjectCleanup, requestID)
	return state.Status == Removed && state.CleanupRequested, err
}

// ErrDeploymentNotCancellable refuses a person's cancellation of a deployment
// that is live, retiring or ended, or that the Flow chose to commit as Live.
var ErrDeploymentNotCancellable = errors.New("the deployment is live or going live, so it can no longer be cancelled")

// Cancel admits a person's cancellation of a deployment of the READY Project
// before the Flow chooses to commit it as Live. The deployment then removes its
// generation and releases its lease; the running Live is untouched. Its
// Project's deletion refuses it, and so does a deployment that cannot be
// cancelled any more, with ErrDeploymentNotCancellable.
func (c *ProjectDeployFlowClient) Cancel(ctx context.Context, account, project, deployment, requestID string) error {
	id, err := FlowID(project, deployment)
	if err != nil {
		return err
	}
	if err = flowid.ValidateDeploymentID(deployment); err != nil {
		return err
	}
	p, err := c.projects.AuthorizeCurrent(ctx, account, project, projects.CapabilityBuild)
	if err != nil {
		return err
	}
	if p.ProjectStatus != projects.StatusReady {
		return dexflow.Reject(dexflow.RejectionFailedPrecondition, "project is not ready")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var result mutationResult
	err = c.projects.Dex.InvokeRPC(ctx, id, c.flow.RequestDeploymentCancellation, cancellationRequest{RequestID: requestID, ActorAccountID: account}, &result)
	if _, ended := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); ended {
		// A deployment that completed answers from its retained snapshot: CANCELLED, or ended otherwise.
		if result.Deployment, err = c.readSnapshot(ctx, project, deployment); err != nil {
			return err
		}
		if result.Deployment.Status == Cancelled {
			return nil
		}
		return ErrDeploymentNotCancellable
	}
	if err != nil {
		return err
	}
	if result.Rejection != nil && result.Rejection.HasCode(dexflow.RejectionFailedPrecondition) && !result.Deployment.CleanupRequested {
		return fmt.Errorf("%w: %w", ErrDeploymentNotCancellable, result.Rejection)
	}
	return result.Rejection.Err()
}

// RevokeForLiveRemoval revokes the current Live deployment's workload grant
// for the Project's admitted Live removal or deletion, before the Project
// clears its Live pointer.
func (c *ProjectDeployFlowClient) RevokeForLiveRemoval(ctx context.Context, project, deployment, requestID string) error {
	p, err := c.projects.Read(ctx, project)
	if err != nil {
		return err
	}
	removing := p.ProjectStatus == projects.StatusDeleting || (p.ActiveCommand != nil && p.ActiveCommand.Kind == projects.CommandRemoveLive)
	if !removing || p.CurrentLive == nil || p.CurrentLive.DeploymentID != deployment {
		return errors.New("project has not admitted removal of this Live deployment")
	}
	state, err := c.readSnapshot(ctx, project, deployment)
	if err != nil {
		return err
	}
	if err = c.grants.RevokeWorkload(ctx, liveWorkload(state)); errors.Is(err, infra.ErrPending) {
		return fmt.Errorf("%w: %w", projects.ErrCleanupPending, err)
	} else if err != nil {
		return fmt.Errorf("revoke the workload grant of deployment %s: %w", deployment, err)
	}
	_, err = c.mutate(ctx, state, c.flow.RevokeDeploymentGrant, requestID)
	return err
}

// requestRetirement asks the Live generation that the committed deployment s
// replaced to retire. The request ID names s and its generation, so a retry
// repeats the same request, which the replaced generation admits once; one
// that cannot retire answers unchanged.
func (c *ProjectDeployFlowClient) requestRetirement(ctx context.Context, s Snapshot) error {
	id, err := FlowID(s.ProjectID, s.ReplacedLive.DeploymentID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	in := retirementRequest{RequestID: fmt.Sprintf("retire-%s-g%d", s.DeploymentID, s.Lease.Generation), ReplacedBy: ReplacedGeneration{DeploymentID: s.DeploymentID, Generation: s.Lease.Generation}}
	var result mutationResult
	if err = c.projects.Dex.InvokeRPC(ctx, id, c.flow.RequestLiveRetirement, in, &result); err != nil {
		return fmt.Errorf("ask deployment %s to retire: %w", s.ReplacedLive.DeploymentID, err)
	}
	if err = result.Rejection.Err(); err != nil {
		return fmt.Errorf("deployment %s refused to retire: %w", s.ReplacedLive.DeploymentID, err)
	}
	return nil
}

// RemoveServing removes the Project's current Live generation and route.
func (c *ProjectDeployFlowClient) RemoveServing(ctx context.Context, p projects.Project) error {
	return c.serving.RemoveServing(ctx, p)
}

// ReadSnapshotForTest is readSnapshot for the tests.
func (c *ProjectDeployFlowClient) ReadSnapshotForTest(ctx context.Context, project, deployment string) (Snapshot, error) {
	return c.readSnapshot(ctx, project, deployment)
}
