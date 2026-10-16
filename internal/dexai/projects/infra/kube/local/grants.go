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
	"encoding/base64"

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// IssueWorkload grants the workload its MinIO service account.
func (b *Backend) IssueWorkload(ctx context.Context, k *kube.Infrastructure, s infra.Workload) error {
	_, _, err := b.issueWorkload(ctx, k, s)
	return err
}

// issueWorkload grants the workload its MinIO service account and returns the
// account's key pair.
func (b *Backend) issueWorkload(ctx context.Context, k *kube.Infrastructure, s infra.Workload) (access, secret string, err error) {
	if err = k.VerifyGrantScope(ctx, s); err != nil {
		return "", "", err
	}
	if s.Preview() {
		// The Sandbox reaches the Live MinIO only through the storage policy,
		// which a project whose scope was converged before it admitted
		// Sandboxes still lacks.
		if err = k.EnsureComponentTraffic(ctx, s.ProjectID, kube.ComponentStorage); err != nil {
			return "", "", err
		}
	}
	minio, err := b.openStorage(ctx, k, s.ProjectID, s.Storage.Boundary.UID)
	if err != nil {
		return "", "", err
	}
	keys, err := k.AcceptedSecretKeys(ctx, s, minio.objects)
	if err != nil {
		return "", "", err
	}
	if access, secret, err = b.grantCredential(ctx, k, s); err != nil {
		return "", "", err
	}
	if err = infra.EnsureStorageAccount(ctx, minio.admin, minio.root, access, secret, s.AccountDescription(), infra.GrantPolicy(s, keys)); err != nil {
		return "", "", err
	}
	return access, secret, nil
}

// RevokeWorkload permanently disables the workload generation's MinIO service
// account.
func (b *Backend) RevokeWorkload(ctx context.Context, k *kube.Infrastructure, s infra.Workload) error {
	// The storage account lives in the Live MinIO, so an absent namespace
	// leaves nothing to revoke.
	namespace, err := k.CleanupNamespace(ctx, s.ProjectID, s.Storage.Boundary.UID)
	if err != nil || namespace == nil {
		return err
	}
	s.Storage.Boundary = kube.ResourceReference(namespace)
	minio, err := b.openStorage(ctx, k, s.ProjectID, s.Storage.Boundary.UID)
	if err != nil {
		return err
	}
	access, secret, err := b.grantCredential(ctx, k, s)
	if err != nil {
		return err
	}
	return infra.RevokeStorageAccount(ctx, minio.admin, minio.root, access, secret, s.AccountDescription())
}

// GrantEnvironment issues the grant and passes the MinIO account's keys, its
// endpoint and region to the workload.
func (b *Backend) GrantEnvironment(ctx context.Context, k *kube.Infrastructure, s infra.Workload) (map[string]string, error) {
	access, secret, err := b.issueWorkload(ctx, k, s)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		infra.AccessKeyIDVariable: access, infra.SecretAccessKeyVariable: secret, "AWS_REGION": infra.LocalStorageRegion,
		"DEX_PROJECT_STORAGE_ENDPOINT": storageURL(k.ProjectNamespace(s.ProjectID)), "DEX_PROJECT_ALLOW_LOCAL_STORAGE": "true",
	}, nil
}

// grantCredential returns the storage key pair of a workload generation, kept
// in a Secret of the Live namespace so a lost response never loses the key.
func (b *Backend) grantCredential(ctx context.Context, k *kube.Infrastructure, s infra.Workload) (string, string, error) {
	metadata := kubeapi.Metadata(k.ProjectNamespace(s.ProjectID), grantSecretName(s.Name()), k.WorkloadLabels(s))
	found, err := k.API.GetOrCreate(ctx, kubeapi.ImmutableSecret(metadata, kubeapi.Object{
		infra.AccessKeyIDVariable:     base64.StdEncoding.EncodeToString([]byte(infra.RandomSecret()[:20])),
		infra.SecretAccessKeyVariable: base64.StdEncoding.EncodeToString([]byte(infra.RandomSecret()[:32])),
	}))
	if err != nil {
		return "", "", err
	}
	if err = kube.VerifyLabels(found, k.WorkloadLabels(s)); err != nil {
		return "", "", err
	}
	return kube.CredentialPair(found)
}

// grantSecretName names the Secret that holds the storage key pair of the
// workload generation named workload.
func grantSecretName(workload string) string { return workload + "-storage" }
