// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// sandboxDexCheck bounds the identity checks and the health check of a
// Sandbox's Preview Dex together.
const sandboxDexCheck = 10 * time.Second

// SandboxAlias is the alias of a Sandbox container on its Session's network,
// which a Sandbox Service names in Kubernetes.
const SandboxAlias = "coding"

// DialProjectDex opens one private gRPC connection to the project's Live Dex
// after verifying the accepted network and Dex service and that the Dex
// accepts connections. The connection belongs to the caller's request: the
// returned function closes it, and so does the end of ctx.
func (k *Infrastructure) DialProjectDex(ctx context.Context, project string, refs infra.ScopeRefs) (*grpc.ClientConn, func() error, error) {
	address, err := k.verifyProjectDex(ctx, project, refs)
	if err != nil {
		return nil, nil, err
	}
	return dialOwned(ctx, address)
}

// dialOwned dials address for the request of ctx, which closes it.
func dialOwned(ctx context.Context, address string) (*grpc.ClientConn, func() error, error) {
	connection, closeConnection, err := dialPrivate(ctx, address)
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = closeConnection() })
	return connection, func() error { stop(); return closeConnection() }, nil
}

// CheckWorker proves the current ownership of a Live generation's application
// Worker and that its gRPC endpoint accepts an HTTP/2 connection. The identity
// checks are bounded by 15 seconds and the connection by two.
func (k *Infrastructure) CheckWorker(ctx context.Context, t infra.WorkerTarget) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	address, err := k.verifyWorker(ctx, t)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/readyz", nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errors.New("application Worker is not registered")
	}
	return nil
}

// verifyWorker checks that the target names a Live generation's Worker: the
// project network, the generation's network, which stands for its Service,
// and its application container are still the accepted, owned objects of the
// generation. It returns the loopback address of the Worker port.
func (k *Infrastructure) verifyWorker(ctx context.Context, t infra.WorkerTarget) (string, error) {
	if t.Generation == 0 || t.Namespace.UID == "" || t.Controller.UID == "" || t.Service.UID == "" || flowid.ValidateDeploymentID(t.DeploymentID) != nil {
		return "", errors.New("application Worker target identity is incomplete")
	}
	network, name := k.ProjectNetwork(t.ProjectID), k.GenerationName(t.ProjectID, t.DeploymentID, t.Generation)
	if t.Namespace.Name != network || t.Controller.Name != name+"-worker" || t.Controller.Boundary != network || t.Service.Name != name || t.Service.Boundary != network {
		return "", errors.New("application Worker resource scope differs")
	}
	if _, err := k.CurrentNetwork(ctx, network, t.Namespace.UID, k.OwnerLabels(t.ProjectID, infra.ComponentInfrastructure)); err != nil {
		return "", err
	}
	labels := k.GenerationLabels(t.ProjectID, t.DeploymentID, t.Generation)
	if _, err := k.CurrentNetwork(ctx, name, t.Service.UID, labels); err != nil {
		return "", err
	}
	container, err := k.OwnedContainer(ctx, name+"-worker", labels)
	if err != nil {
		return "", err
	}
	if container == nil || container.ID != t.Controller.UID || !container.Joined(t.Service.UID) || !container.Joined(t.Namespace.UID) {
		return "", errors.New("application Worker resource ownership changed")
	}
	if !container.State.Running {
		return "", errors.New("application Worker is not running")
	}
	return container.PublishedAddress(infra.WorkerReadinessPort)
}

// GenerationLabels are the owner labels of a Live deployment generation's
// network and container.
func (k *Infrastructure) GenerationLabels(project, deploymentID string, generation uint64) map[string]string {
	labels := k.OwnerLabels(project, infra.ComponentApplication)
	labels[deploymentnames.WorkloadIDLabel] = deploymentID
	labels[deploymentnames.GenerationLabel] = strconv.FormatUint(generation, 10)
	return labels
}

// DialSandboxDex opens one private gRPC connection to the Preview Dex of a
// Sandbox instance. It first verifies that the project's Sandbox network and the
// instance's container are still the recorded objects, then that the Dex
// answers its health check. A replaced or foreign object is
// infra.ErrOwnershipConflict; a container that is not running, or a Dex that
// does not answer yet, is infra.ErrPending. The connection belongs to the
// caller's request: the returned function closes it, and so does the end of
// ctx.
func (k *Infrastructure) DialSandboxDex(ctx context.Context, t infra.SandboxDexTarget) (*grpc.ClientConn, func() error, error) {
	check, cancel := context.WithTimeout(ctx, sandboxDexCheck)
	defer cancel()
	address, err := k.verifySandboxDex(check, t)
	if err != nil {
		return nil, nil, err
	}
	connection, closeConnection, err := dialOwned(ctx, address)
	if err != nil {
		return nil, nil, err
	}
	if _, err = dexpb.NewFlowServiceClient(connection).HealthCheck(check, &emptypb.Empty{}); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("%w: the Sandbox's Preview Dex does not answer its health check: %w", infra.ErrPending, err), closeConnection())
	}
	return connection, closeConnection, nil
}

// ProbeSandboxDex runs the checks of DialSandboxDex without keeping the
// connection.
func (k *Infrastructure) ProbeSandboxDex(ctx context.Context, t infra.SandboxDexTarget) error {
	_, closeConnection, err := k.DialSandboxDex(ctx, t)
	if err != nil {
		return err
	}
	return closeConnection()
}

// verifySandboxDex checks that the target's network carries the Preview's
// owner labels and recorded ID, and that its Service, the container the
// instance's Sandbox runs in, is the recorded instance of the target's
// workspace request, attached to that network under the recorded alias. It
// returns the loopback address of the Dex port.
func (k *Infrastructure) verifySandboxDex(ctx context.Context, t infra.SandboxDexTarget) (string, error) {
	network := t.Namespace.Name
	if t.Generation == 0 || t.WorkspaceRequestID == "" || t.Namespace.UID == "" || t.Sandbox.UID == "" || t.Service.UID == "" ||
		network != k.SessionNetwork(t.ProjectID) || t.Sandbox.Boundary != network || t.Service.Boundary != network {
		return "", errors.New("sandbox Dex target identity is incomplete")
	}
	owner := k.PreviewLabels(t.ProjectID)
	found, err := k.api.InspectNetwork(ctx, network)
	if err != nil {
		return "", err
	}
	if found == nil || found.ID != t.Namespace.UID || VerifyLabels(found.Labels, owner) != nil {
		return "", fmt.Errorf("%w: network %s is not the recorded one", infra.ErrOwnershipConflict, network)
	}
	container, err := k.api.InspectContainer(ctx, t.Service.UID)
	if err != nil {
		return "", err
	}
	instance := WithLabel(WithLabel(owner, deploymentnames.WorkspaceRequestLabel, t.WorkspaceRequestID), InstanceUIDLabel, t.Sandbox.UID)
	if container == nil || container.ID != t.Service.UID || container.Name != "/"+t.Sandbox.Name || VerifyLabels(container.Config.Labels, instance) != nil {
		return "", fmt.Errorf("%w: the Sandbox container %s is not the recorded instance", infra.ErrOwnershipConflict, t.Sandbox.Name)
	}
	if !container.State.Running {
		return "", fmt.Errorf("%w: the Sandbox container %s is %s", infra.ErrPending, t.Sandbox.Name, container.State.Status)
	}
	if !container.Attached(found.ID, t.Service.Name) {
		return "", fmt.Errorf("%w: the Sandbox container %s is not on its network as %s", infra.ErrOwnershipConflict, t.Sandbox.Name, t.Service.Name)
	}
	return container.PublishedAddress(infra.DexPort)
}
