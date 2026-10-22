// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package infra

import (
	"net"
	"regexp"
	"strconv"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
)

// Fixed ports of project workloads. Services, network policies and Local Native
// forwarding admit only these.
const (
	DexPort             = 8801
	TemporalPort        = 7233
	StoragePort         = 9000
	WorkerReadinessPort = 8081
)

// CredentialReadPort is the platform server's port of the credential read
// endpoint in the cluster: the chart's container and Service port and the
// one port of the server that project applications and Sandboxes may reach.
const CredentialReadPort = 8086

// ScopeAddress is the address under which the workloads of a project's Live
// scope reach one of its components on port: the component's Service in the
// project namespace, or its alias in the project's Docker network.
func ScopeAddress(component Component, port int) string {
	return net.JoinHostPort(component.String(), strconv.Itoa(port))
}

// WorkloadName names the resources of one generation of a Live deployment or
// of the Project's Preview grant: its Kubernetes objects, storage credential and
// IAM role. It is the only derivation of app-<workload>-<generation>.
func WorkloadName(workloadID string, generation uint64) string {
	return "app-" + workloadID + "-" + strconv.FormatUint(generation, 10)
}

// PublicRoutes is the fixed operator configuration of the public routes to
// project applications: Live at live-<project> and Preview at preview-<project>.
// Live serving and Preview exposure derive every public host and URL from it.
// Every host is one label below the environment's domain, so the platform's
// one wildcard rule, record and certificate serve all of them.
type PublicRoutes struct {
	Environment deploymentnames.Environment
	// Local Kind serves every route on LocalDomain:LocalPort and probes it
	// through the gateway at LocalGatewayURL.
	Local           bool
	LocalDomain     string
	LocalPort       int
	LocalGatewayURL string
}

// Host is the public host name of a project's Live or Preview application.
func (r PublicRoutes) Host(kind ScopeKind, project string) string {
	if r.Local {
		return kind.String() + "-" + project + "." + r.LocalDomain
	}
	// Hosted hosts keep the environment label even in prod, whose DNS zone is
	// dexai.dev, so published application URLs stay stable.
	return kind.String() + "-" + project + "." + r.Environment.String() + ".dexai.dev"
}

// URL is the public base URL of a project's Live or Preview application.
func (r PublicRoutes) URL(kind ScopeKind, project string) string {
	if r.Local {
		return "http://" + net.JoinHostPort(r.Host(kind, project), strconv.Itoa(r.LocalPort))
	}
	return "https://" + r.Host(kind, project)
}

// HostFromContainers is the name under which a container of the local Docker
// Engine reaches this machine, and through it the processes of the Native lane
// and the ports that other containers publish on 127.0.0.1.
const HostFromContainers = "host.docker.internal"

// CodingSandboxProfile is the coding profile of the Sandbox Pods that a
// Preview's storage grant lets reach the Live storage in Local Kind.
const CodingSandboxProfile = "coding-v4"

// ImmutableImage is an image reference pinned by digest.
var ImmutableImage = regexp.MustCompile(`^[^\s]+@sha256:[a-f0-9]{64}$`)
