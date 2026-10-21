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
	"errors"
	"fmt"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/hosted/identity"
	"net/http"

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// DestroyScope removes a hosted scope: its project Dex, the IAM roles and
// Pod Identity associations, the self-hosted Temporal namespace and the bucket, and
// finally the namespace. Cloud resources outlive serving generations, and the
// namespace goes last because its UID binds every external resource.
func (h *CloudAccount) DestroyScope(ctx context.Context, k *kube.Infrastructure, project string, refs infra.ScopeRefs) error {
	namespace, err := k.CleanupNamespace(ctx, project, refs.Storage.Boundary.UID)
	if err != nil {
		return err
	}
	uid := refs.Storage.Boundary.UID
	if namespace != nil {
		uid = kubeapi.UID(namespace)
	}
	if uid == "" {
		return nil
	}
	if namespace != nil {
		switch err = removeDex(ctx, k, project); {
		case errors.Is(err, infra.ErrPending):
			return err
		case err != nil:
			// Continue only when a 403 inside the namespace proves it absent.
			if err = k.ReconcileCleanupError(ctx, project, uid, err); err != nil {
				return err
			}
			namespace = nil
		}
	}
	if err = h.identity.DestroyIdentities(ctx, k, identity.LiveName(k, project), k.ProjectNamespace(project), identity.LiveTags(k, project, uid)); err != nil {
		return err
	}
	if err = h.destroyTemporal(ctx, k, project, uid, refs.Execution.TemporalCredential); err != nil {
		return err
	}
	if err = h.storage.DestroyBucket(ctx, k, project, uid); err != nil {
		return err
	}
	if namespace == nil {
		return nil
	}
	if err = k.Remove(ctx, "/api/v1/namespaces", kube.ResourceReference(namespace), project, kube.ComponentInfrastructure); err != nil {
		return err
	}
	return fmt.Errorf("%w: namespace %s is being deleted", infra.ErrPending, k.ProjectNamespace(project))
}

// removeDex deletes the Project Dex Deployment before its cloud identities
// and returns infra.ErrPending until the Deployment is absent.
func removeDex(ctx context.Context, k *kube.Infrastructure, project string) error {
	path := "/apis/apps/v1/namespaces/" + k.ProjectNamespace(project) + "/deployments"
	dex, code, err := k.API.Call(ctx, http.MethodGet, path+"/dex", nil)
	if err != nil || code == http.StatusNotFound {
		return err
	}
	if err = k.Remove(ctx, path, kube.ResourceReference(dex), project, kube.ComponentDex); err != nil {
		return err
	}
	return fmt.Errorf("%w: the project Dex Deployment is being deleted", infra.ErrPending)
}
