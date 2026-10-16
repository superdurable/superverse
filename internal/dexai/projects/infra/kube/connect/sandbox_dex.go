// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// sandboxDexCheck bounds the identity checks and the health check together.
const sandboxDexCheck = 10 * time.Second

// DialSandboxDex opens one private gRPC connection to the Preview Dex of a
// Sandbox instance. It first verifies that the namespace, Sandbox and Service
// are still the recorded objects of the project's instance, then that the Dex
// answers its health check. A replaced or foreign object is
// infra.ErrOwnershipConflict; a Sandbox that is not Ready or a Dex that does
// not answer yet is infra.ErrPending. Authentication and business
// authorization stay with the caller. The connection belongs to the caller's
// request: the returned function closes it, and so does the end of ctx.
func (c *Connections) DialSandboxDex(ctx context.Context, target infra.SandboxDexTarget) (*grpc.ClientConn, func() error, error) {
	k := c.Infrastructure
	check, cancel := context.WithTimeout(ctx, sandboxDexCheck)
	defer cancel()
	if err := verifySandboxDex(check, k, target); err != nil {
		return nil, nil, err
	}
	connection, closeConnection, err := k.DialPrivate(ctx, kubeapi.ServiceAddress(target.Namespace.Name, target.Service.Name, infra.DexPort))
	if err != nil {
		return nil, nil, err
	}
	if _, err = dexpb.NewFlowServiceClient(connection).HealthCheck(check, &emptypb.Empty{}); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("%w: the Sandbox's Preview Dex does not answer its health check: %w", infra.ErrPending, err), closeConnection())
	}
	stop := context.AfterFunc(ctx, func() { _ = closeConnection() })
	return connection, func() error { stop(); return closeConnection() }, nil
}

// ProbeSandboxDex runs the checks of DialSandboxDex without keeping the
// connection.
func (c *Connections) ProbeSandboxDex(ctx context.Context, target infra.SandboxDexTarget) error {
	_, closeConnection, err := c.DialSandboxDex(ctx, target)
	if err != nil {
		return err
	}
	return closeConnection()
}

// verifySandboxDex checks that the target's namespace and Sandbox carry the
// Preview's owner labels from infra.PreviewKeysOf and their recorded UIDs, that
// the Sandbox belongs to the target's instance and is Ready, and that the
// Service is the Sandbox's own headless Service with the Dex port.
func verifySandboxDex(ctx context.Context, k *kube.Infrastructure, t infra.SandboxDexTarget) error {
	namespace := t.Namespace.Name
	if t.Generation == 0 || t.WorkspaceRequestID == "" || t.Namespace.UID == "" || t.Sandbox.UID == "" || t.Service.UID == "" || t.Sandbox.Boundary != namespace || t.Service.Boundary != namespace {
		return errors.New("sandbox Dex target identity is incomplete")
	}
	owner := kubeapi.Object{kubeapi.ManagedByLabel: k.Names().Product}
	for key, value := range kube.PreviewLabels(t.ProjectID) {
		owner[key] = value
	}
	if _, err := readRecordedObject(ctx, k, "/api/v1/namespaces/"+namespace, t.Namespace.UID, owner); err != nil {
		return err
	}
	instance := kubeapi.Object{kubeapi.WorkspaceRequestLabel: t.WorkspaceRequestID}
	for key, value := range owner {
		instance[key] = value
	}
	sandbox, err := readRecordedObject(ctx, k, "/apis/agents.x-k8s.io/v1beta1/namespaces/"+namespace+"/sandboxes/"+t.Sandbox.Name, t.Sandbox.UID, instance)
	if err != nil {
		return err
	}
	if !kubeapi.ObservedReady(sandbox) {
		return fmt.Errorf("%w: Sandbox %s/%s is not Ready", infra.ErrPending, namespace, t.Sandbox.Name)
	}
	service, err := readRecordedObject(ctx, k, "/api/v1/namespaces/"+namespace+"/services/"+t.Service.Name, t.Service.UID, nil)
	if err != nil {
		return err
	}
	if !kubeapi.ControlledBy(service, "Sandbox", t.Sandbox.UID) || kubeapi.String(kubeapi.Child(service, "spec"), "clusterIP") != "None" || !exposesDex(service) {
		return fmt.Errorf("%w: Service %s/%s is not the Sandbox's headless Dex Service", infra.ErrOwnershipConflict, namespace, t.Service.Name)
	}
	return nil
}

// readRecordedObject reads the object at path and returns it only when it is
// the recorded one: alive, with uid and every label of labels. Anything else,
// absence included, is an ownership conflict, because the recorded object was
// replaced or deleted.
func readRecordedObject(ctx context.Context, k *kube.Infrastructure, path, uid string, labels kubeapi.Object) (kubeapi.Object, error) {
	o, code, err := k.API.Call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	owned := code != http.StatusNotFound && !kubeapi.Deleting(o) && kubeapi.UID(o) == uid
	found := kubeapi.Labels(o)
	for key, value := range labels {
		owned = owned && found[key] == value
	}
	if !owned {
		return nil, fmt.Errorf("%w: %s is not the recorded object", infra.ErrOwnershipConflict, path)
	}
	return o, nil
}

// exposesDex reports whether the Service exposes the Dex port over TCP.
func exposesDex(service kubeapi.Object) bool {
	for _, item := range kubeapi.Items(kubeapi.Child(service, "spec"), "ports") {
		port, ok := item.(map[string]any)
		if ok && port["port"] == float64(infra.DexPort) && (kubeapi.String(port, "protocol") == "" || kubeapi.String(port, "protocol") == "TCP") {
			return true
		}
	}
	return false
}
