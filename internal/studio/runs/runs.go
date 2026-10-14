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
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/flowapi"
	"github.com/superdurable/superversev2/internal/studio/flowapi/fdg"
	"google.golang.org/grpc"
)

// Runs serves the FlowRunService and FlowDiagnosticsService RPCs of the Flow
// API. The composition root assigns its dependencies. Every operation
// authorizes the caller and reads its target's currency afresh; only the parsed
// definitions and verified Dex connections derived from that read are cached
// per replica (docs/architecture/runs-target-cache-and-watch-authorization.md).
type Runs struct {
	Projects    *projects.ProjectFlowClient
	Deployments *deployments.ProjectDeployFlowClient
	Sessions    *agents.SessionFlowService
	Objects     *objectstore.Store
	Connections infra.Connections
	// Names derives the Session namespaces and names the product.
	Names deploymentnames.Names
	// Workspaces asks a Sandbox's Preview supervisor whether its Worker serves.
	Workspaces *workspace.Capabilities
	Logger     *slog.Logger
	// DexConnections bounds the verified Dex connections pooled on this replica.
	DexConnections int

	caches      sync.Once
	definitions *definitionsCache
	connections *dexConnectionPool
}

// cached returns the replica's caches, which the first operation creates.
func (r *Runs) cached() (*definitionsCache, *dexConnectionPool) {
	r.caches.Do(func() {
		r.definitions, r.connections = newDefinitionsCache(), newDexConnectionPool(r.DexConnections, r.Logger)
	})
	return r.definitions, r.connections
}

// Close closes the pooled Dex connections: idle ones at once, held ones when
// their operations end. Later operations fail.
func (r *Runs) Close() error {
	_, connections := r.cached()
	connections.close()
	return nil
}

// DexDialsForTest counts the Dex connections Runs dialed, pooled ones once.
func (r *Runs) DexDialsForTest() uint64 {
	_, connections := r.cached()
	return connections.dials.Load()
}

// requestTimeout ends an operation before common 60-second proxy timeouts.
const requestTimeout = 55 * time.Second

// Operation states what a Runs operation requires of its request.
type Operation struct {
	// write requires a Preview request to name the Workspace instance the
	// caller observed.
	write bool
	// pinsDefinitions interprets Flows with the serving definitions, so the
	// caller must name the definition revision it read.
	pinsDefinitions bool
	// live is the capability the operation needs on Live; on a Preview every
	// operation needs projects.CapabilityBuild.
	live projects.Capability
}

// The four kinds of Runs operations. Any supplied definition revision must
// still be the serving one.
var (
	// Read reads definitions or engine state.
	Read = Operation{live: projects.CapabilityOperateLive}
	// Write changes engine state: it stops, time-travels or deletes a
	// Channel message, which on Live is a builder's.
	Write = Operation{write: true, live: projects.CapabilityBuild}
	// PinnedRead reads Flows through the serving definitions.
	PinnedRead = Operation{pinsDefinitions: true, live: projects.CapabilityOperateLive}
	// PinnedWrite changes or starts Flows through the serving definitions,
	// which on Live is an operator's.
	PinnedWrite = Operation{write: true, pinsDefinitions: true, live: projects.CapabilityOperateLive}
)

// admits reports whether the caller's definition revision lets the operation
// run on the serving definitions.
func (operation Operation) admits(requested, serving string) bool {
	if requested == "" {
		return !operation.pinsDefinitions
	}
	return requested == serving
}

// definitionChanged answers an operation whose definition revision is no
// longer the serving one; Studio reloads its catalog.
var definitionChanged = flowapi.CodedErrorDocument(http.StatusConflict, "FLOW_DEFINITION_CHANGED", "Flow Definition updated; reload and confirm the operation again")

// Serve produces the document of one Flow API operation.
type Serve func(context.Context, *flowapi.FlowAPI) flowapi.Document

// Definitions returns the Flow Definitions document of the request's
// serving target. It reads no Dex state.
func (r *Runs) Definitions(ctx context.Context, actor string, flowAPI *pb.FlowApiContext) (flowapi.Document, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	serving, err := r.resolve(ctx, actor, flowAPI.GetTarget(), Read)
	if err != nil {
		return flowapi.Document{}, err
	}
	if !Read.admits(flowAPI.GetDefinitionRevision(), serving.bundle.Revision) {
		return definitionChanged, nil
	}
	return flowapi.Document{StatusCode: http.StatusOK, JSON: serving.bundle.DefinitionsJSON()}, nil
}

// Invoke serves one Flow API operation on the request's serving target,
// through a pooled connection to the target's Dex.
func (r *Runs) Invoke(ctx context.Context, actor string, flowAPI *pb.FlowApiContext, operation Operation, serve Serve) (flowapi.Document, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	serving, err := r.resolve(ctx, actor, flowAPI.GetTarget(), operation)
	if err != nil {
		return flowapi.Document{}, err
	}
	if !operation.admits(flowAPI.GetDefinitionRevision(), serving.bundle.Revision) {
		return definitionChanged, nil
	}
	connection, release, err := r.connect(ctx, serving)
	if err != nil {
		return flowapi.Document{}, err
	}
	defer release()
	authorization := flowapi.Authorization{ActorID: actor, TargetRevision: serving.targetRevision, WorkerTarget: serving.workerAddress, WorkerHeadless: serving.workerHeadless}
	flows := flowapi.New(dexpb.NewFlowServiceClient(connection), authorization, serving.bundle, serving.checkWorker)
	return serve(ctx, flows), nil
}

// LiveEngine is the current Live deployment of a Project with a connection to
// its Dex, which InvokeLive leases: the read model's Live target.
type LiveEngine struct {
	Flows       dexpb.FlowServiceClient
	Definitions *fdg.Bundle
	Live        deployments.LiveTarget
}

// ErrLiveNotReady reports a Project without a READY Live deployment whose
// identity, Build, configuration, grant and definitions agree.
var ErrLiveNotReady = errors.New("the Project has no ready Live deployment")

// InvokeLive serves one read of the Project's current Live deployment for a
// caller that already holds the Project scope, the Agent's platform tools,
// with the exact checks of a Studio Live target and the serving definitions.
// No account is authorized: projectID comes from the trusted scope, never
// from a request.
func (r *Runs) InvokeLive(ctx context.Context, projectID string, read func(context.Context, LiveEngine) error) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	live, err := r.Deployments.ReadLiveTarget(ctx, projectID)
	if errors.Is(err, postgres.ErrNotFound) || err == nil && live.ProjectStatus != projects.StatusReady {
		return ErrLiveNotReady
	}
	if err != nil {
		return err
	}
	serving, err := r.resolveLive(ctx, live, &pb.FlowApiTarget{ProjectId: projectID, ReleaseId: live.Live.ReleaseID, DeploymentId: live.Live.DeploymentID}, nil)
	var rejection *dexflow.Rejection
	if errors.As(err, &rejection) {
		return fmt.Errorf("%w: %s", ErrLiveNotReady, rejection.Message)
	}
	if err != nil {
		return err
	}
	connection, release, err := r.connect(ctx, serving)
	if err != nil {
		return err
	}
	defer release()
	return read(ctx, LiveEngine{Flows: dexpb.NewFlowServiceClient(connection), Definitions: serving.bundle, Live: serving.live})
}

// connect leases a verified connection to the serving target's Dex from the
// replica's pool.
func (r *Runs) connect(ctx context.Context, serving servingTarget) (*grpc.ClientConn, func(), error) {
	_, connections := r.cached()
	return connections.acquire(ctx, serving.dexKey, serving.dial)
}
