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
	"errors"

	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
	"google.golang.org/grpc"
)

// EnsureTemporal runs the project's own Temporal with the namespace and search
// attributes its Dex uses, and returns the access and its Service, also while
// Temporal becomes ready.
func (b *Backend) EnsureTemporal(ctx context.Context, k *kube.Infrastructure, project string, _ infra.StorageRefs, namespace string, accepted infra.ResourceRef) (infra.TemporalAccess, infra.ResourceRef, error) {
	temporal := persistentComponent{component: kube.ComponentTemporal, image: b.TemporalImage, volume: "temporal-data", size: "5Gi", args: infra.TemporalStartDevArgs(namespace, "/data/temporal.db"), port: infra.TemporalPort}
	service, err := b.ensureComponent(ctx, k, project, temporal, accepted.UID)
	return infra.LocalTemporalAccess(namespace), service, err
}

// ReadTemporalAccess requires the accepted Temporal Service to be the project's
// own and returns the access to it.
func (b *Backend) ReadTemporalAccess(ctx context.Context, k *kube.Infrastructure, project string, _ infra.StorageRefs, namespace string, credential infra.ResourceRef) (infra.TemporalAccess, error) {
	service, err := k.GetOwned(ctx, k.NamespacePath(project)+"/services/"+kube.ComponentTemporal.String(), project, kube.ComponentTemporal)
	if err != nil {
		return infra.TemporalAccess{}, err
	}
	if service == nil || kubeapi.UID(service) != credential.UID {
		return infra.TemporalAccess{}, errors.New("project Temporal Service is absent or replaced")
	}
	return infra.LocalTemporalAccess(namespace), nil
}

// DialTemporal dials the project's Temporal Service through its credential
// reference.
func (b *Backend) DialTemporal(ctx context.Context, k *kube.Infrastructure, _ string, storage infra.StorageRefs, credential infra.ResourceRef, _ infra.TemporalAccess) (context.Context, *grpc.ClientConn, func() error, error) {
	connection, closeConnection, err := k.DialPrivate(ctx, kubeapi.ServiceAddress(storage.Boundary.Name, credential.Name, infra.TemporalPort))
	return ctx, connection, closeConnection, err
}

// DexSpec runs the project's Dex on the project's Temporal and on its MinIO
// with the root credentials.
func (b *Backend) DexSpec(ctx context.Context, k *kube.Infrastructure, project string, storage infra.StorageRefs, access infra.TemporalAccess) (kube.ProjectDexSpec, error) {
	key, secret, err := b.rootCredentials(ctx, k, project)
	if err != nil {
		return kube.ProjectDexSpec{}, err
	}
	return kube.ProjectDexSpec{Temporal: infra.LocalDexTemporal(access), Storage: infra.LocalDexStorage(storage.StorageBucket, key, secret)}, nil
}
