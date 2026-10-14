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
	"fmt"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// DestroyScope removes the scope's infrastructure once the owning Flow has
// stopped its workloads. It returns ErrPending until everything is gone and
// removes only resources of the scope's exact ownership.
func (k *Infrastructure) DestroyScope(ctx context.Context, project string, refs infra.ScopeRefs) error {
	return k.Backend.DestroyScope(ctx, k, project, refs)
}

// CleanupNamespace returns the scope's namespace for cleanup, or nil when it is
// absent: only cleanup treats absence as success. A namespace of another owner
// or UID is an ownership conflict. An owned namespace that is
// terminating is pending, because namespaced RBAC can already be gone while the
// namespace and its workloads still terminate.
func (k *Infrastructure) CleanupNamespace(ctx context.Context, project, uid string) (kubeapi.Object, error) {
	// An invalid project would name another namespace, whose absence would then
	// read as completed cleanup.
	if !flowid.IsLowerAlphanumeric(project) {
		panic("infra: invalid cleanup project " + strconv.Quote(project))
	}
	o, err := k.GetOwned(ctx, k.NamespacePath(project), project, ComponentInfrastructure)
	if err != nil || o == nil {
		return nil, err
	}
	if uid != "" && kubeapi.UID(o) != uid {
		return nil, fmt.Errorf("%w: namespace UID changed", infra.ErrOwnershipConflict)
	}
	if kubeapi.Deleting(o) {
		return nil, fmt.Errorf("%w: namespace %s is terminating", infra.ErrPending, k.ProjectNamespace(project))
	}
	return o, nil
}

// DestroyPreviewConfiguration deletes every object version below the
// project's Preview prefix in the Live bucket, in bounded batches, once its
// Preview ended. It returns ErrPending until the prefix is empty. An absent
// Live namespace took the storage with it, because Live cleanup deletes the
// bucket before the namespace.
func (k *Infrastructure) DestroyPreviewConfiguration(ctx context.Context, project string, storage infra.StorageRefs) error {
	namespace, err := k.CleanupNamespace(ctx, project, storage.Boundary.UID)
	if err != nil || namespace == nil {
		return err
	}
	objects, err := k.Backend.Objects(ctx, k, project, kubeapi.UID(namespace))
	if err != nil {
		return err
	}
	// The configuration store pins the hosted bucket owner on every request.
	store := comm.Must(objectstore.NewStore(infra.ConfigurationS3Client(objects), objects.Bucket, objects.KMSKeyARN, strings.TrimSuffix(infra.ConfigurationPrefix, "/"), objects.KMSKeyARN == ""))
	done, err := store.DeletePrefix(ctx, strings.TrimPrefix(infra.PreviewKeysOf(project).ConfigurationPrefix, infra.ConfigurationPrefix))
	if err != nil || done {
		return err
	}
	return fmt.Errorf("%w: the Preview configuration of project %s is being deleted", infra.ErrPending, project)
}

// DestroyPreviewGrants deletes every storage grant issued to the project's
// Preview, revoked fences included, once the Preview ended: in Local Kind the
// MinIO accounts its grants marked and their credential Secrets, hosted the
// Pod Identity associations in principalNamespace and the IAM roles. It
// returns ErrPending until they are gone.
func (k *Infrastructure) DestroyPreviewGrants(ctx context.Context, project string, storage infra.StorageRefs, principalNamespace string) error {
	namespace, err := k.CleanupNamespace(ctx, project, storage.Boundary.UID)
	if err != nil {
		return err
	}
	return k.Backend.DestroyPreviewGrants(ctx, k, project, storage, namespace, principalNamespace)
}

// ReconcileCleanupError rechecks a 403 for an object inside the scope's
// namespace against the namespace itself. A denial while the namespace is
// active remains a permission failure. Absence proves only that its namespaced
// resources are gone; hosted cleanup still continues with the retained UID.
func (k *Infrastructure) ReconcileCleanupError(ctx context.Context, project, uid string, err error) error {
	// A denial of the namespace read itself never becomes successful cleanup.
	if !kubeapi.IsForbiddenIn(err, k.ProjectNamespace(project)) {
		return err
	}
	namespace, readErr := k.CleanupNamespace(ctx, project, uid)
	if readErr != nil {
		return readErr
	}
	if namespace == nil {
		return nil
	}
	return err
}
