// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// DexAddress is the private gRPC address of a project's Live Dex.
func DexAddress(execution infra.ExecutionRefs) string {
	return kubeapi.ServiceAddress(execution.DexService.Boundary, execution.DexService.Name, infra.DexPort)
}

// WorkerAddress is the address of the project's Live Worker endpoint, the
// headless Service that selects the current Live generation's Pod.
func (k *Infrastructure) WorkerAddress(project string) string {
	return kubeapi.ServiceAddress(k.ProjectNamespace(project), infra.LiveWorkerEndpoint, infra.LiveWorkerPort)
}

// routeGatewayName is the Gateway that every project route names as its
// parent; the platform chart creates it in GatewayNamespace.
const routeGatewayName = "projects"

// GatewayNamespace holds the platform's Envoy Gateway: its controller, the
// Gateway of the project routes and its proxies.
func (k *Infrastructure) GatewayNamespace() string { return k.Names().Product + "-gateway" }

// RouteGateway is the parent of every project route.
func (k *Infrastructure) RouteGateway() kubeapi.Object {
	return kubeapi.GatewayParent(k.GatewayNamespace(), routeGatewayName)
}
