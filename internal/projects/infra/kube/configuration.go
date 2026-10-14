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
	"strings"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// VerifyProjectStorage checks the project's accepted storage references and
// that its namespace is still the accepted one. Configuration is readable and
// writable while no Dex or workload runs.
func (k *Infrastructure) VerifyProjectStorage(ctx context.Context, project string, storage infra.StorageRefs) error {
	if storage.Boundary.Name != k.ProjectNamespace(project) || storage.Boundary.UID == "" || storage.StorageBucket != k.bucket(project, storage.Boundary.UID) || storage.BlobPrefix != infra.ConfigurationPrefix {
		return errors.New("project storage resource identity is incomplete")
	}
	_, err := k.CurrentNamespace(ctx, project, storage.Boundary.UID)
	return err
}

// LiveStorage are the storage references of a project from the namespace UID
// and bucket a Flow pinned; the namespace name and the key prefix are fixed. A
// Preview reads its configuration there.
func (k *Infrastructure) LiveStorage(project, namespaceUID, bucket string) infra.StorageRefs {
	return infra.StorageRefs{Boundary: infra.ResourceRef{Name: k.ProjectNamespace(project), UID: namespaceUID}, StorageBucket: bucket, BlobPrefix: infra.ConfigurationPrefix}
}

// ReadConfiguration reads the exact accepted version of the scope's
// configuration head from the project's storage.
func (k *Infrastructure) ReadConfiguration(ctx context.Context, scope infra.ScopeRef, ref objectstore.ObjectRef) ([]byte, error) {
	if err := k.VerifyProjectStorage(ctx, scope.ProjectID, scope.Storage); err != nil {
		return nil, err
	}
	if !infra.IsConfigurationHead(scope, ref) {
		return nil, errors.New("configuration object scope mismatch")
	}
	objects, err := k.Backend.Objects(ctx, k, scope.ProjectID, scope.Storage.Boundary.UID)
	if err != nil {
		return nil, err
	}
	return infra.ReadExactVersion(ctx, objects, ref)
}

// ConfigurationObjects returns the Connector SDK object store of the scope in
// the project's storage, confined to the scope's own configuration prefix. It
// owns no configuration lifecycle or application permission.
func (k *Infrastructure) ConfigurationObjects(ctx context.Context, scope infra.ScopeRef) (connectorconfig.ObjectStore, error) {
	if err := k.VerifyProjectStorage(ctx, scope.ProjectID, scope.Storage); err != nil {
		return nil, err
	}
	objects, err := k.Backend.Objects(ctx, k, scope.ProjectID, scope.Storage.Boundary.UID)
	if err != nil {
		return nil, err
	}
	prefix, err := (connectorconfig.Scope{ProjectID: scope.ProjectID, Kind: scope.Scope.Kind.String()}).Prefix()
	if err != nil {
		return nil, err
	}
	store, err := connectorconfig.NewS3ObjectStore(ctx, &connectorconfig.S3StoreConfig{Client: infra.ConfigurationS3Client(objects), Bucket: objects.Bucket, Prefix: scope.Storage.BlobPrefix, KMSKeyID: objects.KMSKeyARN, AllowUnencrypted: objects.KMSKeyARN == ""})
	if err != nil {
		return nil, err
	}
	return &configurationObjects{infrastructure: k, project: scope.ProjectID, storage: scope.Storage, prefix: prefix + "/", store: store}, nil
}

// configurationObjects admits only keys below the scope's prefix and verifies
// the scope's namespace before every operation.
type configurationObjects struct {
	infrastructure *Infrastructure
	project        string
	storage        infra.StorageRefs
	prefix         string
	store          connectorconfig.ObjectStore
}

func (s *configurationObjects) verify(ctx context.Context, key string) error {
	if !strings.HasPrefix(key, s.prefix) {
		return infra.ErrAccessDenied
	}
	return s.infrastructure.VerifyProjectStorage(ctx, s.project, s.storage)
}

func (s *configurationObjects) ReadObject(ctx context.Context, key, version string) (connectorconfig.Object, error) {
	if err := s.verify(ctx, key); err != nil {
		return connectorconfig.Object{}, err
	}
	return s.store.ReadObject(ctx, key, version)
}

func (s *configurationObjects) CreateObject(ctx context.Context, key string, contents []byte) (connectorconfig.Object, error) {
	if err := s.verify(ctx, key); err != nil {
		return connectorconfig.Object{}, err
	}
	return s.store.CreateObject(ctx, key, contents)
}

func (s *configurationObjects) CompareAndSwapObject(ctx context.Context, key, expectedETag string, contents []byte) (connectorconfig.Object, error) {
	if err := s.verify(ctx, key); err != nil {
		return connectorconfig.Object{}, err
	}
	return s.store.CompareAndSwapObject(ctx, key, expectedETag, contents)
}
