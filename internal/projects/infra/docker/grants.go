// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"context"
	"errors"
	"fmt"

	"github.com/superdurable/superversev2/internal/projects/infra"
)

// IssueWorkload grants one workload generation read access to its accepted
// configuration, its head and its secrets and write access to its scope's
// Trigger inboxes (infra.GrantPolicy): a MinIO service account of the
// project's MinIO, whose key pair the server keeps in a credential file. A revoked generation's account stays as a
// fence, so issuing it again fails.
func (k *Infrastructure) IssueWorkload(ctx context.Context, s infra.Workload) error {
	_, err := k.issueWorkload(ctx, s)
	return err
}

// issueWorkload issues the grant and returns the key pair of its account.
func (k *Infrastructure) issueWorkload(ctx context.Context, s infra.Workload) (credential, error) {
	if err := k.verifyGrantScope(ctx, s); err != nil {
		return credential{}, err
	}
	minio, err := k.openStorage(ctx, s.ProjectID, s.Storage.Boundary.UID, infra.ResourceRef{})
	if err != nil {
		return credential{}, err
	}
	data, err := infra.ReadExactVersion(ctx, minio.objects, s.Configuration)
	if err != nil {
		return credential{}, err
	}
	keys, err := infra.ApplicationSecretKeys(data, s.ScopeRef())
	if err != nil {
		return credential{}, err
	}
	grant, err := ensureCredential(k.grantCredentialPath(s.ProjectID, s.Name()), grantOwner(s), grantKeys)
	if err != nil {
		return credential{}, err
	}
	if err = infra.EnsureStorageAccount(ctx, minio.admin, minio.root, grant.Access, grant.Secret, s.AccountDescription(), infra.GrantPolicy(s, keys)); err != nil {
		return credential{}, err
	}
	return grant, nil
}

// RevokeWorkload permanently disables the workload generation's MinIO service
// account. The account lives in the project's MinIO, so an absent project
// network leaves nothing to revoke.
func (k *Infrastructure) RevokeWorkload(ctx context.Context, s infra.Workload) error {
	if s.Generation == 0 {
		return nil
	}
	network, err := k.CleanupNetwork(ctx, k.ProjectNetwork(s.ProjectID), s.Storage.Boundary.UID, k.OwnerLabels(s.ProjectID, infra.ComponentInfrastructure))
	if err != nil || network == nil {
		return err
	}
	s.Storage.Boundary = infra.ResourceRef{Name: network.Name, UID: network.ID}
	minio, err := k.openStorage(ctx, s.ProjectID, network.ID, infra.ResourceRef{})
	if err != nil {
		return err
	}
	grant, err := ensureCredential(k.grantCredentialPath(s.ProjectID, s.Name()), grantOwner(s), grantKeys)
	if err != nil {
		return err
	}
	return infra.RevokeStorageAccount(ctx, minio.admin, minio.root, grant.Access, grant.Secret, s.AccountDescription())
}

// GrantEnvironment reconciles a current workload grant and returns the process
// environment through which the workload reaches its storage and
// configuration: the shared application contract, the account's key pair and
// the project's MinIO at its fixed address on the project network, which both
// a Live application and a Sandbox on that network reach and the Connector
// SDK accepts as a local endpoint.
func (k *Infrastructure) GrantEnvironment(ctx context.Context, s infra.Workload) (map[string]string, error) {
	if !s.Current() {
		return nil, errors.New("workload grant is not current")
	}
	grant, err := k.issueWorkload(ctx, s)
	if err != nil {
		return nil, err
	}
	endpoint, err := k.storageEndpoint(ctx, s.ProjectID, s.Storage)
	if err != nil {
		return nil, err
	}
	env := s.Environment()
	env[infra.AccessKeyIDVariable], env[infra.SecretAccessKeyVariable], env["AWS_REGION"] = grant.Access, grant.Secret, infra.LocalStorageRegion
	env["DEX_PROJECT_STORAGE_ENDPOINT"], env["DEX_PROJECT_ALLOW_LOCAL_STORAGE"] = endpoint, "true"
	return env, nil
}

// verifyGrantScope checks that the workload's configuration is its scope's
// configuration head in the project's accepted storage. A Preview also needs
// its grant workload ID and its current Sandbox principal.
func (k *Infrastructure) verifyGrantScope(ctx context.Context, s infra.Workload) error {
	if !infra.IsConfigurationHead(s.ScopeRef(), s.Configuration) {
		return errors.New("workload configuration scope mismatch")
	}
	if s.Preview() && s.WorkloadID != infra.PreviewKeysOf(s.ProjectID).GrantWorkloadID {
		return errors.New("workload identity does not match its Preview")
	}
	if err := k.VerifyProjectStorage(ctx, s.ProjectID, s.Storage); err != nil || !s.Preview() {
		return err
	}
	return k.verifyPrincipal(ctx, s)
}

// verifyPrincipal checks that a Preview grant's principal is still the
// project's Sandbox network, by ID, and its workspace volumes, by the UID label
// they carry. A cold start attaches the next instance to the same network and
// volumes, so the principal survives the instance. A replaced or missing
// principal is an ownership conflict: the grant must never reach another
// identity.
func (k *Infrastructure) verifyPrincipal(ctx context.Context, s infra.Workload) error {
	principal := s.Principal
	if principal == nil || principal.Boundary.UID == "" || principal.Identity.UID == "" || principal.Identity.Boundary != principal.Boundary.Name ||
		principal.Boundary.Name != k.SessionNetwork(s.ProjectID) {
		return errors.New("preview grant principal identity is incomplete")
	}
	owner := k.PreviewLabels(s.ProjectID)
	network, err := k.api.InspectNetwork(ctx, principal.Boundary.Name)
	if err != nil {
		return err
	}
	if network == nil || network.ID != principal.Boundary.UID || VerifyLabels(network.Labels, owner) != nil {
		return fmt.Errorf("%w: the Preview grant principal network %s changed", infra.ErrOwnershipConflict, principal.Boundary.Name)
	}
	for _, subPath := range WorkspaceSubPaths {
		name := WorkspaceVolume(principal.Identity.Name, subPath)
		volume, err := k.api.InspectVolume(ctx, name)
		if err != nil {
			return err
		}
		if volume == nil || VerifyLabels(volume.Labels, WithLabel(owner, WorkspaceUIDLabel, principal.Identity.UID)) != nil {
			return fmt.Errorf("%w: the Preview grant principal volume %s changed", infra.ErrOwnershipConflict, name)
		}
	}
	return nil
}
