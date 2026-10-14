// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"

	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// IssueWorkload grants one workload generation read access to its accepted
// configuration, its head and its secrets and write access to its scope's
// Trigger inboxes in the Live storage (infra.Workload.GrantResources): a MinIO service account in Local Kind,
// an IAM role with a Pod Identity association when hosted: a Live generation's own, or the scope policy
// of the Preview's principal role. A revoked generation is never issued again.
func (k *Infrastructure) IssueWorkload(ctx context.Context, s infra.Workload) error {
	return k.Backend.IssueWorkload(ctx, k, s)
}

// RevokeWorkload permanently disables a workload generation's storage identity.
func (k *Infrastructure) RevokeWorkload(ctx context.Context, s infra.Workload) error {
	if s.Generation == 0 {
		return nil
	}
	return k.Backend.RevokeWorkload(ctx, k, s)
}

// GrantEnvironment reconciles a current workload grant and returns the process
// environment through which the workload reaches its storage and
// configuration. The Backend adds how it reaches the storage: Local Kind passes
// the MinIO account's keys; hosted carries no credential, because Pod Identity
// provides the role's.
func (k *Infrastructure) GrantEnvironment(ctx context.Context, s infra.Workload) (map[string]string, error) {
	if !s.Current() {
		return nil, errors.New("workload grant is not current")
	}
	env := s.Environment()
	backend, err := k.Backend.GrantEnvironment(ctx, k, s)
	if err != nil {
		return nil, err
	}
	maps.Copy(env, backend)
	return env, nil
}

// VerifyGrantScope checks that the workload's configuration is its scope's
// configuration head in the project's accepted storage. A grant is a storage
// grant, so no Dex has to run. A Preview also needs its grant workload ID and
// its current Sandbox principal.
func (k *Infrastructure) VerifyGrantScope(ctx context.Context, s infra.Workload) error {
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
// project's Sandbox namespace and service account, with the UIDs the grant
// binds. A replaced or missing principal is an ownership conflict: the grant
// must never reach another identity.
func (k *Infrastructure) verifyPrincipal(ctx context.Context, s infra.Workload) error {
	principal := s.Principal
	if principal.Boundary.UID == "" || principal.Identity.UID == "" || principal.Identity.Boundary != principal.Boundary.Name {
		return errors.New("preview grant principal identity is incomplete")
	}
	owner := kubeapi.Object{kubeapi.ManagedByLabel: k.Names().Product}
	for key, value := range PreviewLabels(s.ProjectID) {
		owner[key] = value
	}
	for _, ref := range []struct{ path, uid string }{
		{"/api/v1/namespaces/" + principal.Boundary.Name, principal.Boundary.UID},
		{"/api/v1/namespaces/" + principal.Boundary.Name + "/serviceaccounts/" + principal.Identity.Name, principal.Identity.UID},
	} {
		o, code, err := k.API.Call(ctx, http.MethodGet, ref.path, nil)
		if err != nil {
			return err
		}
		if code == http.StatusNotFound || kubeapi.Deleting(o) || kubeapi.UID(o) != ref.uid || VerifyLabels(o, owner) != nil {
			return fmt.Errorf("%w: the Preview grant principal %s changed", infra.ErrOwnershipConflict, ref.path)
		}
	}
	return nil
}

// AcceptedSecretKeys reads the workload's accepted configuration from the
// verified Live bucket and returns the application secrets it may read.
func (k *Infrastructure) AcceptedSecretKeys(ctx context.Context, s infra.Workload, objects *infra.ApplicationObjects) ([]string, error) {
	data, err := infra.ReadExactVersion(ctx, objects, s.Configuration)
	if err != nil {
		return nil, err
	}
	return infra.ApplicationSecretKeys(data, s.ScopeRef())
}

// WorkloadLabels are the owner labels of a workload generation's own
// resources; a Preview's also carry its scope.
func (k *Infrastructure) WorkloadLabels(s infra.Workload) kubeapi.Object {
	labels := k.OwnerLabels(s.ProjectID, ComponentApplication)
	if s.Preview() {
		for key, value := range PreviewLabels(s.ProjectID) {
			labels[key] = value
		}
	}
	labels[kubeapi.WorkloadIDLabel] = s.WorkloadID
	labels[kubeapi.GenerationLabel] = strconv.FormatUint(s.Generation, 10)
	return labels
}

// WorkloadServiceAccount is the service account a Live generation's Pods run as.
func (k *Infrastructure) WorkloadServiceAccount(s infra.Workload) string {
	return k.Backend.WorkloadServiceAccount(k, s)
}
