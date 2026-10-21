// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package hosted

import (
	"context"

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"google.golang.org/grpc"
)

var _ kube.Backend = (*CloudAccount)(nil)

// BucketBoundToNamespace is true: the bucket name derives from the namespace UID.
func (*CloudAccount) BucketBoundToNamespace() bool { return true }

// StorageComplete is true: a bucket has no Service to record.
func (*CloudAccount) StorageComplete(infra.StorageRefs) bool { return true }

// ExtraEgress admits the Pod Identity agent to the Dex and the applications,
// and the Dex also the private Temporal frontend.
func (*CloudAccount) ExtraEgress(_ *kube.Infrastructure, component kube.Component) []any {
	if component != kube.ComponentDex && component != kube.ComponentApplication {
		return nil
	}
	egress := []any{kube.EgressTo(kubeapi.Object{"ipBlock": kubeapi.Object{"cidr": "169.254.170.23/32"}}, 80)}
	if component == kube.ComponentDex {
		egress = append(egress, kube.EgressTo(kubeapi.Object{"namespaceSelector": kubeapi.Object{"matchLabels": kubeapi.Object{"kubernetes.io/metadata.name": "temporal"}}, "podSelector": kubeapi.Object{"matchLabels": kubeapi.Object{"app.kubernetes.io/component": "frontend", "app.kubernetes.io/name": "xtemporal"}}}, infra.TemporalPort))
	}
	return egress
}

// DialTemporal connects to the private frontend without credentials or TLS.
func (*CloudAccount) DialTemporal(ctx context.Context, k *kube.Infrastructure, _ string, _ infra.StorageRefs, _ infra.ResourceRef, access infra.TemporalAccess) (context.Context, *grpc.ClientConn, func() error, error) {
	connection, closeConnection, err := k.DialPrivate(ctx, access.HostPort)
	return ctx, connection, closeConnection, err
}

// SharedApplicationServiceAccount is false: each generation's grant creates the
// service account its IAM role trusts, and its cleanup removes it.
func (*CloudAccount) SharedApplicationServiceAccount() bool { return false }

// ApplicationIngressPeer is none: the application port admits any peer, the
// Envoy Gateway proxies of the project routes among them.
func (*CloudAccount) ApplicationIngressPeer(*kube.Infrastructure) kubeapi.Object { return nil }

// ApplicationRuntimeClass is gVisor on the Live node pool, apart from the Sandboxes.
func (*CloudAccount) ApplicationRuntimeClass() string { return "runsc-live" }

// ProbeBase is the public URL itself.
func (*CloudAccount) ProbeBase(_ infra.PublicRoutes, publicURL string) string { return publicURL }

func (h *CloudAccount) Bucket(k *kube.Infrastructure, project, namespaceUID string) string {
	return h.storage.Bucket(k, project, namespaceUID)
}

func (h *CloudAccount) Objects(ctx context.Context, k *kube.Infrastructure, project, namespaceUID string) (*infra.ApplicationObjects, error) {
	return h.storage.Objects(ctx, k, project, namespaceUID)
}

func (h *CloudAccount) EnsureStorage(ctx context.Context, k *kube.Infrastructure, project, namespaceUID, prior string) (infra.ResourceRef, error) {
	return h.storage.EnsureStorage(ctx, k, project, namespaceUID, prior)
}

func (h *CloudAccount) IssueWorkload(ctx context.Context, k *kube.Infrastructure, s infra.Workload) error {
	if err := k.VerifyGrantScope(ctx, s); err != nil {
		return err
	}
	objects, err := h.Objects(ctx, k, s.ProjectID, s.Storage.Boundary.UID)
	if err != nil {
		return err
	}
	return h.identity.IssueWorkloadWithObjects(ctx, k, s, objects)
}

func (h *CloudAccount) GrantEnvironment(ctx context.Context, k *kube.Infrastructure, s infra.Workload) (map[string]string, error) {
	if err := k.VerifyGrantScope(ctx, s); err != nil {
		return nil, err
	}
	objects, err := h.Objects(ctx, k, s.ProjectID, s.Storage.Boundary.UID)
	if err != nil {
		return nil, err
	}
	return h.identity.GrantEnvironmentWithObjects(ctx, k, s, objects)
}

func (h *CloudAccount) EnsureDexIdentity(ctx context.Context, k *kube.Infrastructure, project string, storage infra.StorageRefs) error {
	return h.identity.EnsureDexIdentity(ctx, k, project, storage)
}

func (h *CloudAccount) WorkloadServiceAccount(k *kube.Infrastructure, s infra.Workload) string {
	return h.identity.WorkloadServiceAccount(k, s)
}

func (h *CloudAccount) RevokeWorkload(ctx context.Context, k *kube.Infrastructure, s infra.Workload) error {
	return h.identity.RevokeWorkload(ctx, k, s)
}

func (h *CloudAccount) DestroyPreviewGrants(ctx context.Context, k *kube.Infrastructure, project string, storage infra.StorageRefs, namespace kubeapi.Object, principalNamespace string) error {
	return h.identity.DestroyPreviewGrants(ctx, k, project, storage, namespace, principalNamespace)
}
