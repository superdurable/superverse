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
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// CheckWorker verifies exact deployment ownership and its registration readiness.
func (c *Connections) CheckWorker(ctx context.Context, target infra.WorkerTarget) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	k := c.Infrastructure
	if target.Generation == 0 || target.Namespace.UID == "" || target.Controller.UID == "" || target.Service.UID == "" || flowid.ValidateDeploymentID(target.DeploymentID) != nil {
		return errors.New("application Worker target identity is incomplete")
	}
	namespace := k.ProjectNamespace(target.ProjectID)
	name := infra.WorkloadName(target.DeploymentID, target.Generation)
	if target.Namespace.Name != namespace || target.Controller.Name != name+"-worker" || target.Controller.Boundary != namespace || target.Service.Name != name || target.Service.Boundary != namespace {
		return errors.New("application Worker scope differs")
	}
	if _, err := readOwnedWorkerResource(ctx, k, target, "/api/v1/namespaces/"+namespace, target.Namespace.UID, false); err != nil {
		return err
	}
	if _, err := readOwnedWorkerResource(ctx, k, target, "/api/v1/namespaces/"+namespace+"/services/"+name, target.Service.UID, true); err != nil {
		return err
	}
	if _, err := readOwnedWorkerResource(ctx, k, target, "/apis/apps/v1/namespaces/"+namespace+"/deployments/"+name+"-worker", target.Controller.UID, true); err != nil {
		return err
	}
	_, ready, err := k.DeploymentReady(ctx, target.ProjectID, name+"-worker", kube.ComponentApplication, target.Controller.UID)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("application Worker is not registered")
	}
	return nil
}

// readOwnedWorkerResource reads one object of the target's Worker and returns
// it only when it is the accepted one (a non-empty uid) of the target's
// project and Live scope, and, with checkGeneration, of its generation.
func readOwnedWorkerResource(ctx context.Context, k *kube.Infrastructure, t infra.WorkerTarget, path, uid string, checkGeneration bool) (kubeapi.Object, error) {
	o, code, err := k.API.Call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, errors.New("application Worker resource is absent")
	}
	labels := kubeapi.Labels(o)
	if kubeapi.UID(o) == "" || kubeapi.Deleting(o) || uid != "" && kubeapi.UID(o) != uid || kubeapi.String(labels, kubeapi.ManagedByLabel) != k.Names().Product || kubeapi.String(labels, kubeapi.ProjectLabel) != t.ProjectID || kubeapi.String(labels, kubeapi.ScopeLabel) != "" {
		return nil, errors.New("application Worker resource ownership changed")
	}
	if kubeapi.String(labels, kubeapi.PartOfLabel) != k.Names().Product+"-live" {
		return nil, errors.New("application Worker resource left the Live scope")
	}
	if checkGeneration && (kubeapi.String(labels, kubeapi.GenerationLabel) != strconv.FormatUint(t.Generation, 10) || kubeapi.String(labels, kubeapi.WorkloadIDLabel) != t.DeploymentID) {
		return nil, errors.New("application Worker resource generation changed")
	}
	return o, nil
}
