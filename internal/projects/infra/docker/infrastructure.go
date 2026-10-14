// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// Infrastructure is the Docker runtime of the projects' Live scopes, which
// Local Native runs. A project's Live scope is a Docker network with the
// project's own MinIO, Temporal start-dev and Dex containers on it, under the
// aliases infra.ScopeAddress names; every Live application generation and
// every Sandbox whose Preview reads the project's storage joins it. The
// server reaches a container only through a port it publishes on 127.0.0.1,
// which it looks up for every use. Infrastructure implements the roles of
// infra, holds no state of a project and verifies the references the owning
// Flows accepted before every effect.
type Infrastructure struct {
	api    *dockerapi.Client
	names  deploymentnames.Names
	config Config
	routes *Routes
}

// Config is the runtime configuration that bootstrap validated.
type Config struct {
	// Socket is the unix socket of the Docker Engine API.
	Socket      string
	Environment deploymentnames.Environment
	// DexImage, S3Image and TemporalImage run each project's Dex, MinIO and
	// Temporal start-dev; each is pinned by digest.
	DexImage, S3Image, TemporalImage string
	// StateDir is the lane's private state directory. The route files and the
	// credentials the server generates live below it.
	StateDir string
	// Instance is the Native instance name. It scopes object names and labels
	// so a cleanup can never select another instance's resources.
	Instance string
	// NetworkPrefix is the IPv4 network from which every network the runtime
	// creates takes a /24 subnet of its own, so the lane never depends on the
	// engine's default address pools.
	NetworkPrefix netip.Prefix
	// Routes derive the public host and URL of a project's applications.
	Routes infra.PublicRoutes
	// GatewayURL is where the server sends a route's probe with its Host
	// header, and GatewayAPI the API of the gateway, which reports the routes
	// it loaded.
	GatewayURL, GatewayAPI string
}

var (
	_ infra.Storage     = (*Infrastructure)(nil)
	_ infra.Execution   = (*Infrastructure)(nil)
	_ infra.Grants      = (*Infrastructure)(nil)
	_ infra.Cleanup     = (*Infrastructure)(nil)
	_ infra.Redactor    = (*Infrastructure)(nil)
	_ infra.Connections = (*Infrastructure)(nil)
)

// New reaches the Docker Engine of config and prepares the private state
// directories. Bootstrap validated config, so only the file system can fail.
func New(config Config) (*Infrastructure, error) {
	if config.Instance == "" {
		// Direct runtime tests exercise the compatible default topology without
		// going through the launcher, which always supplies this field.
		config.Instance = DefaultNativeInstance
	}
	if bits := config.NetworkPrefix.Bits(); !config.NetworkPrefix.Addr().Is4() || bits < 8 || bits > subnetBits {
		return nil, fmt.Errorf("network prefix %s is not an IPv4 network from /8 to /%d", config.NetworkPrefix, subnetBits)
	}
	for _, directory := range []string{config.StateDir, filepath.Join(config.StateDir, credentialsDirectory), filepath.Join(config.StateDir, routesDirectory)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("prepare the Native state directory: %w", err)
		}
	}
	k := &Infrastructure{api: dockerapi.New(config.Socket), names: deploymentnames.MustFor(config.Environment), config: config}
	k.routes = &Routes{infrastructure: k, directory: filepath.Join(config.StateDir, routesDirectory)}
	return k, nil
}

// Close releases the connections to the engine.
func (k *Infrastructure) Close() error { return k.api.Close() }

// API is the engine client that the Sandbox and Live serving runtimes share.
func (k *Infrastructure) API() *dockerapi.Client { return k.api }

// Routes are the public routes of the gateway.
func (k *Infrastructure) Routes() *Routes { return k.routes }

// PublicRoutes derive the public host and URL of a project's applications.
func (k *Infrastructure) PublicRoutes() infra.PublicRoutes { return k.config.Routes }

// Redact replaces the project's internal names in text shown outside the
// platform with placeholders: its network, which also prefixes its
// containers and volumes, its workload generations and its Temporal
// namespace.
func (k *Infrastructure) Redact(project, text string) string {
	return infra.Redact(text, infra.ScopeRedactions(k.ProjectNetwork(project), project))
}

// The label keys only the Docker runtime stamps. A Docker volume has no ID and
// the ID of a container changes when it is replaced, so the runtime records
// identities that outlive both in labels.
const (
	// NativeInstanceLabel scopes every Docker object Local Native creates.
	NativeInstanceLabel = "superdurable.io/native-instance"
	// DefaultNativeInstance is the instance of a launcher that names none. It
	// also owns the objects created before instances existed, which carry no
	// NativeInstanceLabel, because Docker cannot add a label to an object.
	DefaultNativeInstance = "default"
	// serviceUIDLabel carries the identity of a project component's service,
	// which the owning Flow accepts and which survives the replacement of the
	// component's container.
	serviceUIDLabel = "superdurable.io/service-uid"
	// revisionLabel carries the digest of a replaceable component's
	// configuration, whose change replaces the container.
	revisionLabel = "superdurable.io/config-digest"
	// InstanceUIDLabel carries the UID of a Sandbox instance: its container's
	// identity, stable across restarts, which SUPERVERSE_POD_UID repeats.
	InstanceUIDLabel = "superdurable.io/instance-uid"
	// WorkspaceUIDLabel carries the UID of a Session's workspace, the volumes
	// whose identity a Preview grant's principal binds.
	WorkspaceUIDLabel = "superdurable.io/workspace-uid"
)

// ProjectNetwork names the network of the project's Live scope; it prefixes
// the names of the scope's containers and volumes.
func (k *Infrastructure) ProjectNetwork(project string) string {
	return k.instanceName(k.names.ProjectNamespace(project))
}

// componentName names the container of a component of the project's scope.
func (k *Infrastructure) componentName(project string, component infra.Component) string {
	return k.ProjectNetwork(project) + "-" + component.String()
}

// GenerationName names the network and the container of one Live deployment
// generation.
func (k *Infrastructure) GenerationName(project, deploymentID string, generation uint64) string {
	return k.ProjectNetwork(project) + "-" + infra.WorkloadName(deploymentID, generation)
}

// SessionNetwork names the network of the Sandbox of a project's Session, its
// isolation boundary.
func (k *Infrastructure) SessionNetwork(project string) string {
	return k.instanceName(k.names.SessionNamespace(project))
}

// WorkspaceSubPaths are the subPaths of the coding profile's workspace
// volume, the repository checkout and the Preview engine state. Docker keeps
// each in a named volume of its own, which replaces the Pod's subPaths and
// fsGroup: a new volume takes the ownership of the image directory it is
// mounted over.
var WorkspaceSubPaths = []string{"repository", "preview-state"}

// WorkspaceVolume names the volume of a project's workspace, whose base name
// is workspace, that holds subPath.
func WorkspaceVolume(workspace, subPath string) string { return workspace + "-" + subPath }

// OwnerLabels mark an object as one component of a project's Live scope.
func (k *Infrastructure) OwnerLabels(project string, component infra.Component) map[string]string {
	return map[string]string{
		deploymentnames.ManagedByLabel: k.names.Product, deploymentnames.PartOfLabel: k.names.Product + "-live",
		deploymentnames.ProjectLabel: project, deploymentnames.ComponentLabel: component.String(), NativeInstanceLabel: k.config.Instance,
	}
}

// PreviewLabels mark the objects of a project's Preview scope: the Sandbox of
// its Session.
func (k *Infrastructure) PreviewLabels(project string) map[string]string {
	return map[string]string{deploymentnames.ManagedByLabel: k.names.Product, deploymentnames.ProjectLabel: project, deploymentnames.ScopeLabel: infra.ScopePreview.String(), NativeInstanceLabel: k.config.Instance}
}

// instanceName keeps default instance identities compatible and suffixes named
// instances before Docker sees a potentially colliding project identifier.
func (k *Infrastructure) instanceName(name string) string {
	if k.config.Instance == DefaultNativeInstance {
		return name
	}
	return name + "-" + k.config.Instance
}

// WithLabel is a copy of labels that also carries key with value.
func WithLabel(labels map[string]string, key, value string) map[string]string {
	result := make(map[string]string, len(labels)+1)
	maps.Copy(result, labels)
	result[key] = value
	return result
}

// VerifyLabels requires every label of want on found, and a scope label
// exactly when want names one, so a Preview's object is never adopted as a
// Live one or the reverse.
func VerifyLabels(found, want map[string]string) error {
	owned := found[deploymentnames.ScopeLabel] == want[deploymentnames.ScopeLabel]
	for key, value := range want {
		got, present := found[key]
		if key == NativeInstanceLabel && !present {
			got = DefaultNativeInstance
		}
		owned = owned && got == value
	}
	if !owned {
		return fmt.Errorf("%w: object labels", infra.ErrOwnershipConflict)
	}
	return nil
}

// SelectorLabels are the labels a listing selects objects of labels by. The
// default instance's objects may lack NativeInstanceLabel, so its listings
// leave that label out and VerifyLabels checks each object listed.
func SelectorLabels(labels map[string]string) map[string]string {
	if labels[NativeInstanceLabel] != DefaultNativeInstance {
		return labels
	}
	selector := maps.Clone(labels)
	delete(selector, NativeInstanceLabel)
	return selector
}

// errIncomplete reports a reference the owning Flow has not completed.
var errIncomplete = errors.New("project resource identity is incomplete")
