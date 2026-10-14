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
	"fmt"
	"net/http"

	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// DestroyScope deletes the UID-pinned namespace, which takes the MinIO, the
// Temporal and every workload with it, and returns infra.ErrPending until it
// is gone.
func (b *Backend) DestroyScope(ctx context.Context, k *kube.Infrastructure, project string, refs infra.ScopeRefs) error {
	namespace, err := k.CleanupNamespace(ctx, project, refs.Storage.Boundary.UID)
	if err != nil || namespace == nil {
		return err
	}
	// The deterministic namespace can exist after provisioning failed before its
	// reference was committed. Its exact V2/project ownership is reconciled here.
	if err = k.Remove(ctx, "/api/v1/namespaces", kube.ResourceReference(namespace), project, kube.ComponentInfrastructure); err != nil {
		return err
	}
	remaining, err := k.CleanupNamespace(ctx, project, kubeapi.UID(namespace))
	if err != nil || remaining == nil {
		return err
	}
	return fmt.Errorf("%w: namespace %s is being deleted", infra.ErrPending, k.ProjectNamespace(project))
}

// DestroyPreviewGrants deletes the MinIO accounts that the project's Preview
// grants marked and their credential Secrets, one per call, and returns
// infra.ErrPending until they are gone.
func (b *Backend) DestroyPreviewGrants(ctx context.Context, k *kube.Infrastructure, project string, _ infra.StorageRefs, namespace kubeapi.Object, _ string) error {
	if namespace == nil {
		// The accounts and Secrets lived in the Live namespace and went with it.
		return nil
	}
	minio, err := b.openStorage(ctx, k, project, kubeapi.UID(namespace))
	if err != nil {
		return err
	}
	accounts, err := minio.admin.ListServiceAccounts(ctx, minio.root)
	if err != nil {
		return fmt.Errorf("list the Live storage accounts: %w", err)
	}
	keys := infra.PreviewKeysOf(project)
	for _, account := range accounts.Accounts {
		name, marked := keys.MarkedGrant(account.Description)
		if !marked {
			continue
		}
		if err = b.destroyGrant(ctx, k, minio, project, keys, name, account.AccessKey); err != nil {
			return err
		}
		return fmt.Errorf("%w: the Preview storage grant %s of project %s is being deleted", infra.ErrPending, name, project)
	}
	return nil
}

// destroyGrant deletes one Preview grant generation's credential Secret and
// then its MinIO account, which marks the grant, so a lost answer leaves the
// account for the next call to find. The Secret must carry the Preview's owner
// labels and the account must be the root user's.
func (b *Backend) destroyGrant(ctx context.Context, k *kube.Infrastructure, minio minioConnection, project string, keys infra.PreviewKeys, name, access string) error {
	path := k.NamespacePath(project) + "/secrets/" + grantSecretName(name)
	secret, code, err := k.API.Call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if code != http.StatusNotFound {
		owner := k.OwnerLabels(project, kube.ComponentApplication)
		for key, value := range kube.PreviewLabels(project) {
			owner[key] = value
		}
		owner[kubeapi.WorkloadIDLabel] = keys.GrantWorkloadID
		if err = kube.VerifyLabels(secret, owner); err != nil {
			return err
		}
		if _, _, err = k.API.Call(ctx, http.MethodDelete, path, kubeapi.DeleteOptions(kubeapi.UID(secret))); err != nil {
			return err
		}
	}
	info, err := minio.admin.InfoServiceAccount(ctx, access)
	switch {
	case infra.MissingStorageAccount(err):
		return nil
	case err != nil:
		return fmt.Errorf("read the storage account of %s: %w", name, err)
	case info.ParentUser != minio.root:
		return fmt.Errorf("%w: the storage account of %s has another parent", infra.ErrOwnershipConflict, name)
	}
	if err = minio.admin.DeleteServiceAccount(ctx, access); err != nil {
		return fmt.Errorf("delete the storage account of %s: %w", name, err)
	}
	return nil
}
