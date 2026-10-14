// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package local

import (
	"context"

	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// Backend is the Local Kind environment of the project infrastructure: the
// project's own MinIO and Temporal start-dev run in its namespace, each on a
// volume of its own, and a workload reaches the storage with the key pair of a
// MinIO service account. Every application generation shares the namespace's
// application service account. It implements kube.Backend.
type Backend struct {
	// S3Image and TemporalImage run the MinIO and the Temporal start-dev; both
	// are pinned by digest.
	S3Image, TemporalImage string
}

var _ kube.Backend = (*Backend)(nil)

// Bucket is the bucket of every Local Kind scope's own MinIO.
func (*Backend) Bucket(*kube.Infrastructure, string, string) string {
	return infra.LocalApplicationBucket
}

// BucketBoundToNamespace is false: the bucket name is constant.
func (*Backend) BucketBoundToNamespace() bool { return false }

// Objects connects to the project's MinIO with its root credentials.
func (b *Backend) Objects(ctx context.Context, k *kube.Infrastructure, project, namespaceUID string) (*infra.ApplicationObjects, error) {
	storage, err := b.openStorage(ctx, k, project, namespaceUID)
	return storage.objects, err
}

// StorageComplete requires the MinIO Service.
func (*Backend) StorageComplete(refs infra.StorageRefs) bool { return refs.Storage.UID != "" }

// keycloakPort and keycloakLabels select the lane's Keycloak in the platform
// namespace (templates/local-dependencies.yaml), the OAuth provider whose
// endpoints the connection tests' applications call.
const keycloakPort = 8080

var keycloakLabels = kubeapi.Object{"app": "keycloak"}

// ExtraEgress admits applications to the lane's Keycloak; the traffic every
// environment shares is all another Local Kind component needs.
func (*Backend) ExtraEgress(k *kube.Infrastructure, component kube.Component) []any {
	if component != kube.ComponentApplication {
		return nil
	}
	return []any{kube.EgressTo(kubeapi.Peer(kubeapi.NamespaceNamed(k.Names().PlatformNamespace), keycloakLabels), keycloakPort)}
}

// TemporalNamespace is app-<project>.
func (*Backend) TemporalNamespace(_ *kube.Infrastructure, project string) string {
	return infra.LocalTemporalNamespace(project)
}

// SharedApplicationServiceAccount is true: every generation runs as the
// namespace's application service account.
func (*Backend) SharedApplicationServiceAccount() bool { return true }

// EnsureDexIdentity is none: the Dex reaches the MinIO with its key pair.
func (*Backend) EnsureDexIdentity(context.Context, *kube.Infrastructure, string, infra.StorageRefs) error {
	return nil
}

// WorkloadServiceAccount is the application service account.
func (*Backend) WorkloadServiceAccount(*kube.Infrastructure, infra.Workload) string {
	return kube.ComponentApplication.String()
}

// RedactionPatterns is none: the shared patterns name everything a Local Kind
// project has.
func (*Backend) RedactionPatterns(*kube.Infrastructure, string) []infra.Redaction { return nil }

// ApplicationIngressPeer is the platform namespace, whose server probes a
// generation's health before it routes to it, and the gateway namespace,
// where the Envoy proxies that serve the project routes run.
func (*Backend) ApplicationIngressPeer(k *kube.Infrastructure) kubeapi.Object {
	return kubeapi.NamespacesPeer(k.Names().PlatformNamespace, k.GatewayNamespace())
}

// ApplicationRuntimeClass is the cluster's default.
func (*Backend) ApplicationRuntimeClass() string { return "" }

// ProbeBase is the gateway Service, which routes by the Host header.
func (*Backend) ProbeBase(routes infra.PublicRoutes, _ string) string { return routes.LocalGatewayURL }
