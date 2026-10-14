// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package runs

import (
	"context"
	"errors"
	"strconv"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/agents/sandbox/sessionworkspace"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/flowapi/fdg"
	"google.golang.org/grpc"
)

// servingTarget is the exact Live deployment or Session Preview a Runs
// request addresses, with the Dex connection and Worker check of its kind and
// the definitions its Worker loaded.
type servingTarget struct {
	// dial opens a connection to the target's Dex after verifying its recorded
	// identity; dexKey names that identity, under which the pool shares it.
	dial   dial
	dexKey string
	// checkWorker proves that the target's Worker serves.
	checkWorker func(context.Context) error
	// workerAddress is where Start runs new Flows; workerHeadless makes Dex
	// resolve it itself, as the Live Worker endpoint requires.
	workerAddress  string
	workerHeadless bool
	// targetRevision identifies the serving resource and its definitions,
	// without the account, so a Start can name the target it observed.
	targetRevision string
	bundle         *fdg.Bundle
	// live is the read model's Live target a Live target resolved to.
	live deployments.LiveTarget
}

// resolve authorizes actor on the target's Project and resolves the target's
// current serving deployment or Preview. A target that is no longer current is
// rejected, never silently retargeted.
func (r *Runs) resolve(ctx context.Context, actor string, target *pb.FlowApiTarget, operation Operation) (servingTarget, error) {
	scope, err := r.validateTarget(target)
	if err != nil {
		return servingTarget{}, err
	}
	project := target.GetProjectId()
	capability := projects.CapabilityBuild
	if scope.Kind == infra.ScopeLive {
		capability = operation.live
	}
	current, err := r.authorizeReady(ctx, actor, project, capability, scope.Kind == infra.ScopeLive && operation.write)
	if err != nil {
		return servingTarget{}, err
	}
	if scope.Kind != infra.ScopeLive {
		return r.resolvePreview(ctx, target, operation.write)
	}
	live, err := r.Deployments.ReadLiveTarget(ctx, project)
	if errors.Is(err, postgres.ErrNotFound) {
		return servingTarget{}, errLiveNotCurrent
	}
	if err != nil {
		return servingTarget{}, err
	}
	return r.resolveLive(ctx, live, target, current)
}

// validateTarget admits the browser-selected target and returns the scope it
// names: a Live target names an exact Release and Deployment, a Preview target
// the Project's Preview within the rule its Sandbox admission enforces.
func (r *Runs) validateTarget(target *pb.FlowApiTarget) (infra.Scope, error) {
	scope := infra.ScopeOf(target.GetScope())
	if scope.Validate(target.GetProjectId()) != nil {
		return infra.Scope{}, dexflow.Reject(dexflow.RejectionInvalidArgument, "Invalid Flow API Runs scope")
	}
	if scope.Kind == infra.ScopeLive {
		if target.GetWorkspaceRequestId() != "" || target.GetReleaseId() == "" || flowid.ValidateDeploymentID(target.GetDeploymentId()) != nil {
			return infra.Scope{}, dexflow.Reject(dexflow.RejectionInvalidArgument, "Live Runs require an exact Release and Deployment")
		}
	} else if target.GetReleaseId() != "" || target.GetDeploymentId() != "" ||
		flowid.ValidateSessionScope(r.Names, target.GetProjectId()) != nil {
		return infra.Scope{}, dexflow.Reject(dexflow.RejectionInvalidArgument, "Invalid Flow API Preview scope")
	}
	return scope, nil
}

// authorizeReady admits actor with the capability to the READY Project. A Live write gets
// ProjectFlow's current Project, whose Live pointer it confirms (decision 27);
// any other operation authorizes from the Project's read model row and gets
// nil.
func (r *Runs) authorizeReady(ctx context.Context, actor, project string, capability projects.Capability, liveWrite bool) (*projects.Project, error) {
	if liveWrite {
		current, err := r.Projects.AuthorizeCurrent(ctx, actor, project, capability)
		if err != nil {
			return nil, err
		}
		if current.ProjectStatus != projects.StatusReady {
			return nil, errProjectNotReady
		}
		return &current, nil
	}
	authorized, err := r.Projects.Authorize(ctx, actor, project, capability)
	if err != nil {
		return nil, err
	}
	if authorized.Project.ProjectStatus != projects.StatusReady {
		return nil, errProjectNotReady
	}
	return nil, nil
}

// errProjectNotReady and errLiveNotCurrent reject a target that serves no
// Runs now: a Project that is not READY, or a Live deployment that the
// Project's Live pointer no longer names.
var (
	errProjectNotReady = dexflow.Reject(dexflow.RejectionFailedPrecondition, "Project is not ready for Runs")
	errLiveNotCurrent  = dexflow.Reject(dexflow.RejectionFailedPrecondition, "Requested Deployment is no longer current in Live")
)

// resolveLive requires the target to be the Live deployment that the read
// model's Live target selects: the one the Project's Live pointer names,
// READY, with the pointer's Release and build revision. ProjectDeployFlow
// verified the build, configuration, grant and definitions it pinned, so they
// are read, not compared again. current, for a write, is ProjectFlow's
// Project, whose Live pointer must still name that deployment.
func (r *Runs) resolveLive(ctx context.Context, live deployments.LiveTarget, target *pb.FlowApiTarget, current *projects.Project) (servingTarget, error) {
	pointer, deployment := live.Live, live.Deployment
	if !pointer.Ready ||
		pointer.ReleaseID != target.GetReleaseId() ||
		pointer.DeploymentID != target.GetDeploymentId() ||
		pointer.Revision == 0 ||
		target.GetGeneration() != 0 && target.GetGeneration() != pointer.Revision {
		return servingTarget{}, errLiveNotCurrent
	}
	if current != nil && (current.CurrentLive == nil || current.CurrentLive.DeploymentID != pointer.DeploymentID || current.CurrentLive.Revision != pointer.Revision) {
		return servingTarget{}, errLiveNotCurrent
	}
	recorded := deployment.Inputs.Definitions
	definitions, _ := r.cached()
	bundle, err := definitions.load(definitionsKey(recorded, "live"), func() ([]appmanifest.FlowDefinition, error) {
		return releases.ReadRecordedDefinitions(ctx, r.Objects, recorded)
	})
	if err != nil {
		return servingTarget{}, err
	}
	refs := infra.ScopeRefs{Storage: live.Storage, Execution: live.Execution}
	worker := infra.WorkerTarget{
		ProjectID: deployment.ProjectID, Namespace: deployment.Resources.Storage.Boundary,
		Controller: deployment.Providers.Application, Service: deployment.Resources.Service,
		DeploymentID: pointer.DeploymentID, Generation: pointer.Revision,
	}
	return servingTarget{
		dial: func(ctx context.Context) (*grpc.ClientConn, func() error, error) {
			return r.Connections.DialProjectDex(ctx, deployment.ProjectID, refs)
		},
		dexKey: identityKey("live", deployment.ProjectID, refs.Storage.Boundary.Name, refs.Storage.Boundary.UID,
			refs.Execution.DexService.Boundary, refs.Execution.DexService.Name, refs.Execution.DexService.UID, refs.Execution.TemporalNamespace),
		checkWorker:    func(ctx context.Context) error { return r.Connections.CheckWorker(ctx, worker) },
		workerAddress:  r.Connections.WorkerAddress(deployment.ProjectID),
		workerHeadless: true,
		targetRevision: flowid.Fingerprint([]string{"live", deployment.ProjectID, pointer.DeploymentID, strconv.FormatUint(pointer.Revision, 10),
			pointer.ReleaseID, pointer.BuildRevisionID, bundle.Revision}),
		bundle: bundle,
		live:   live,
	}, nil
}

// resolvePreview requires the ACTIVE Session's READY request on its READY
// instance and the Project's Preview on that same instance, with a READY
// Preview engine and a READY Preview. The instance's identities come from the
// SessionWorkspaceFlow, which owns the instance, and the Preview must have
// acquired exactly that instance. A write must name the
// Workspace instance the caller observed; a read may omit it, but a supplied
// instance must still match. The connection reaches the Dex inside that
// Sandbox, which runs new Flows on the Preview application's Worker inside
// the Pod.
func (r *Runs) resolvePreview(ctx context.Context, target *pb.FlowApiTarget, write bool) (servingTarget, error) {
	if write && (target.GetWorkspaceRequestId() == "" || target.GetGeneration() == 0) {
		return servingTarget{}, dexflow.Reject(dexflow.RejectionInvalidArgument, "Preview writes require the observed Workspace request and generation")
	}
	session, err := r.Sessions.ReadPreviewServing(ctx, target.GetProjectId())
	if err != nil {
		return servingTarget{}, err
	}
	if session.Session.Status != agentloop.SessionStatusActive || session.Session.Workspace.State != agentloop.WorkspaceReady || session.Workspace == nil ||
		session.Workspace.Instance.State != sessionworkspace.InstanceReady || session.Workspace.Instance.InstanceID != session.Session.Workspace.RequestID || session.PreviewState == nil {
		return servingTarget{}, dexflow.Reject(dexflow.RejectionFailedPrecondition, "Preview Session has no ready Workspace")
	}
	ws, state := session.Workspace.Instance, *session.PreviewState
	if state.WorkspaceRequestID != ws.InstanceID ||
		state.Resources.Instance.UID != ws.Resources.Instance.UID ||
		target.GetWorkspaceRequestId() != "" && target.GetWorkspaceRequestId() != ws.InstanceID ||
		target.GetGeneration() != 0 && target.GetGeneration() != sandbox.InstanceGeneration {
		return servingTarget{}, dexflow.Reject(dexflow.RejectionFailedPrecondition, "Preview Workspace instance changed")
	}
	served := state.Preview
	if served.Status != preview.PreviewReady ||
		served.Revision == 0 ||
		served.SourceCommit == "" ||
		served.DefinitionRevision == "" {
		return servingTarget{}, dexflow.Reject(dexflow.RejectionFailedPrecondition, "Start Preview in Build before opening Runs")
	}
	bundle, err := r.previewDefinitions(ctx, state)
	if err != nil {
		return servingTarget{}, err
	}
	// The runtime verifies these recorded names and UIDs when it dials the
	// Preview's Dex; this only requires a complete, consistent record.
	instance := sandbox.Instance{Identity: state.Identity, WorkspaceRequestID: ws.InstanceID, Generation: sandbox.InstanceGeneration, Resources: ws.Resources}
	resources, environment := instance.Resources, state.Environment
	if resources.Boundary.Name == "" ||
		resources.Instance.Boundary != resources.Boundary.Name ||
		resources.Boundary.UID == "" ||
		resources.Instance.UID == "" ||
		resources.ServiceUID == "" {
		return servingTarget{}, dexflow.Reject(dexflow.RejectionFailedPrecondition, "Preview Workspace resource identity is unavailable")
	}
	if environment.Status != preview.EnvironmentReady || environment.InstanceUID != resources.Instance.UID || environment.ServiceUID != resources.ServiceUID {
		return servingTarget{}, dexflow.Reject(dexflow.RejectionFailedPrecondition, "The Preview engine is not ready")
	}
	engine := instance.DexTarget()
	identity := workspace.PreviewIdentity{Commit: served.SourceCommit, ConfigurationDigest: state.Configuration.Digest, GrantGeneration: state.Configuration.Grant.Generation}
	return servingTarget{
		dial: func(ctx context.Context) (*grpc.ClientConn, func() error, error) {
			return r.Connections.DialSandboxDex(ctx, engine)
		},
		dexKey: identityKey("preview", engine.ProjectID, engine.WorkspaceRequestID, strconv.FormatUint(engine.Generation, 10),
			engine.Namespace.Name, engine.Namespace.UID, engine.Sandbox.Name, engine.Sandbox.UID, engine.Service.Name, engine.Service.UID),
		checkWorker: func(ctx context.Context) error {
			return r.Workspaces.CheckPreviewWorker(ctx, instance.Binding(), identity)
		},
		workerAddress: workspace.PreviewWorkerAddress,
		targetRevision: flowid.Fingerprint([]string{"preview", target.GetProjectId(), instance.WorkspaceRequestID,
			strconv.FormatUint(instance.Generation, 10), strconv.FormatUint(served.Revision, 10), served.SourceCommit, bundle.Revision,
			resources.Instance.UID, environment.TemporalClusterID}),
		bundle: bundle,
	}, nil
}

// identityKey joins the recorded identity of a cached value.
func identityKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

// definitionsKey names the definitions stored in the object, and what else
// the read checks them against. An object without a content digest is not
// cached: the empty key.
func definitionsKey(object objectstore.ObjectRef, checked ...string) string {
	if object.Digest == "" {
		return ""
	}
	return identityKey(append([]string{object.Key, object.Version, object.Digest}, checked...)...)
}
