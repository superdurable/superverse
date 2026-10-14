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
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
	"google.golang.org/grpc/connectivity"
)

// CheckWorker proves the current ownership of a Live generation's application
// Worker and that its gRPC endpoint accepts an HTTP/2 connection. The identity
// checks are bounded by 15 seconds and the connection by two.
func (c *Connections) CheckWorker(ctx context.Context, target infra.WorkerTarget) (err error) {
	k := c.Infrastructure
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	address, err := verifyWorkerTarget(ctx, k, target)
	if err != nil {
		return err
	}
	connection, closeConnection, err := k.DialPrivate(ctx, address)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeConnection()) }()
	probe, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	connection.Connect()
	for {
		state := connection.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown || !connection.WaitForStateChange(probe, state) {
			return errors.New("current application Worker is not ready")
		}
	}
}

// verifyWorkerTarget checks that the target names a Live generation's Worker,
// that its namespace, Deployment and Service are still the accepted, owned
// objects of the generation and that the Live Worker endpoint selects it. It
// returns the cluster address of the Worker's
// port.
func verifyWorkerTarget(ctx context.Context, k *kube.Infrastructure, t infra.WorkerTarget) (string, error) {
	if t.Generation == 0 || t.Namespace.UID == "" || t.Controller.UID == "" || t.Service.UID == "" || flowid.ValidateDeploymentID(t.DeploymentID) != nil {
		return "", errors.New("application Worker target identity is incomplete")
	}
	namespace, name := k.ProjectNamespace(t.ProjectID), infra.WorkloadName(t.DeploymentID, t.Generation)
	if t.Namespace.Name != namespace || t.Controller.Name != name || t.Controller.Boundary != namespace || t.Service.Name != name || t.Service.Boundary != namespace {
		return "", errors.New("application Worker resource scope differs")
	}
	if _, err := readOwnedWorkerResource(ctx, k, t, "/api/v1/namespaces/"+namespace, t.Namespace.UID, false); err != nil {
		return "", err
	}
	if _, err := readOwnedWorkerResource(ctx, k, t, "/apis/apps/v1/namespaces/"+namespace+"/deployments/"+name, t.Controller.UID, true); err != nil {
		return "", err
	}
	service, err := readOwnedWorkerResource(ctx, k, t, "/api/v1/namespaces/"+namespace+"/services/"+name, t.Service.UID, true)
	if err != nil {
		return "", err
	}
	if err = verifyWorkerService(t, service); err != nil {
		return "", err
	}
	// New Flows run on the Live Worker endpoint, which must select this
	// generation.
	endpoint, err := readOwnedWorkerResource(ctx, k, t, "/api/v1/namespaces/"+namespace+"/services/"+infra.LiveWorkerEndpoint, "", true)
	if err != nil {
		return "", err
	}
	if err = verifyWorkerService(t, endpoint); err != nil {
		return "", err
	}
	return kubeapi.ServiceAddress(namespace, name, infra.LiveWorkerPort), nil
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

// verifyWorkerService checks that the Service is a ClusterIP Service, headless
// or not, that selects only the target generation's Pods and exposes exactly
// the Worker port.
func verifyWorkerService(t infra.WorkerTarget, service kubeapi.Object) error {
	spec := kubeapi.Child(service, "spec")
	selector := kubeapi.Child(spec, "selector")
	if kubeapi.String(spec, "type") != "" && kubeapi.String(spec, "type") != "ClusterIP" || spec["externalName"] != nil || len(kubeapi.Items(spec, "externalIPs")) != 0 || kubeapi.String(selector, kubeapi.ProjectLabel) != t.ProjectID || kubeapi.String(selector, kubeapi.GenerationLabel) != strconv.FormatUint(t.Generation, 10) || kubeapi.String(selector, kubeapi.WorkloadIDLabel) != t.DeploymentID {
		return errors.New("application Worker Service selector differs")
	}
	matches := 0
	for _, item := range kubeapi.Items(spec, "ports") {
		declared, ok := item.(map[string]any)
		if ok && declared["port"] == float64(infra.LiveWorkerPort) && declared["targetPort"] == float64(infra.LiveWorkerPort) && (kubeapi.String(declared, "protocol") == "" || kubeapi.String(declared, "protocol") == "TCP") {
			matches++
		}
	}
	if matches != 1 {
		return errors.New("application Worker Service port differs")
	}
	return nil
}
