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
	"errors"
	"fmt"
	"net/http"

	madmin "github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// persistentComponent is a component that runs in the project namespace on its
// own volume: the MinIO or the Temporal.
type persistentComponent struct {
	component           kube.Component
	image, volume, size string
	args                []string
	port                int
	env                 []any
}

// ensureComponent converges a component's service account, traffic policy,
// volume, Deployment and Service, and returns the Service, also while the
// Deployment becomes ready. A Service whose UID differs from the accepted one is
// a replacement and is never adopted.
func (b *Backend) ensureComponent(ctx context.Context, k *kube.Infrastructure, project string, c persistentComponent, acceptedUID string) (infra.ResourceRef, error) {
	if err := k.EnsureServiceAccount(ctx, project, c.component); err != nil {
		return infra.ResourceRef{}, err
	}
	if err := k.EnsureComponentTraffic(ctx, project, c.component); err != nil {
		return infra.ResourceRef{}, err
	}
	claim := kubeapi.PersistentVolumeClaim(k.Metadata(project, c.volume, c.component), "standard", c.size)
	if _, err := k.Ensure(ctx, claim, project, c.component); err != nil {
		return infra.ResourceRef{}, err
	}
	deployment := k.InfrastructureDeployment(project, c.component, c.image, c.args, c.port, c.env, []any{kubeapi.VolumeMount("data", "/data")}, []any{kubeapi.ClaimVolume("data", c.volume)})
	kube.MarkSpec(deployment)
	if _, err := k.Ensure(ctx, deployment, project, c.component); err != nil {
		return infra.ResourceRef{}, err
	}
	name := c.component.String()
	found, err := k.Ensure(ctx, k.Service(project, name, c.component, k.OwnerLabels(project, c.component), kubeapi.ServicePort("http", c.port, c.port)), project, c.component)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	service := kube.ResourceReference(found)
	if acceptedUID != "" && service.UID != acceptedUID {
		return infra.ResourceRef{}, fmt.Errorf("the project %s Service UID changed", name)
	}
	_, ready, err := k.DeploymentReady(ctx, project, name, c.component, "")
	if err == nil && !ready {
		err = fmt.Errorf("%w: the %s Deployment is not ready", infra.ErrPending, name)
	}
	return service, err
}

// EnsureStorage runs the project's own MinIO with its root credentials and
// creates its versioned application bucket. It returns the MinIO Service, also
// while MinIO becomes ready.
func (b *Backend) EnsureStorage(ctx context.Context, k *kube.Infrastructure, project, namespaceUID, acceptedServiceUID string) (infra.ResourceRef, error) {
	access, secret, err := b.ensureRootCredentials(ctx, k, project)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	minio := persistentComponent{component: kube.ComponentStorage, image: b.S3Image, volume: "object-data", size: "10Gi", args: []string{"server", "/data"}, port: infra.StoragePort, env: []any{
		kubeapi.SecretEnvVar("MINIO_ROOT_USER", rootKeys, infra.AccessKeyIDVariable), kubeapi.SecretEnvVar("MINIO_ROOT_PASSWORD", rootKeys, infra.SecretAccessKeyVariable),
	}}
	service, err := b.ensureComponent(ctx, k, project, minio, acceptedServiceUID)
	if err != nil {
		return service, err
	}
	storage, err := b.connectStorage(ctx, k, project, namespaceUID, access, secret)
	if err != nil {
		return service, err
	}
	return service, infra.EnsureBucket(ctx, storage.objects, infra.OAuthLifecycle(project))
}

// minioConnection is a scope's MinIO, reached with its root credentials.
type minioConnection struct {
	objects *infra.ApplicationObjects
	admin   *madmin.AdminClient
	// root is the root access key, the parent user of every workload account.
	root string
}

// openStorage connects to a provisioned scope's MinIO.
func (b *Backend) openStorage(ctx context.Context, k *kube.Infrastructure, project, namespaceUID string) (minioConnection, error) {
	access, secret, err := b.rootCredentials(ctx, k, project)
	if err != nil {
		return minioConnection{}, err
	}
	return b.connectStorage(ctx, k, project, namespaceUID, access, secret)
}

// connectStorage connects to a scope's MinIO with known root credentials.
func (b *Backend) connectStorage(ctx context.Context, k *kube.Infrastructure, project, namespaceUID, access, secret string) (minioConnection, error) {
	admin := &madmin.Options{Creds: credentials.NewStaticV4(access, secret, "")}
	var client *http.Client
	if k.Dial != nil {
		client = k.ServiceHTTP()
		admin.Transport = client.Transport
	}
	objects := &infra.ApplicationObjects{Client: infra.ProjectS3(storageURL(k.ProjectNamespace(project)), access, secret, client), Bucket: infra.LocalApplicationBucket}
	return minioConnection{objects: objects, admin: comm.Must(madmin.NewWithOptions(kubeapi.ServiceAddress(k.ProjectNamespace(project), kube.ComponentStorage.String(), infra.StoragePort), admin)), root: access}, nil
}

// rootKeys names the Secret that holds the root key pair of a scope's MinIO.
const rootKeys = "object-credentials"

// ensureRootCredentials returns the root credentials of a scope's MinIO,
// creating them once with the scope.
func (b *Backend) ensureRootCredentials(ctx context.Context, k *kube.Infrastructure, project string) (access, secret string, err error) {
	desired := kubeapi.ImmutableSecret(k.Metadata(project, rootKeys, kube.ComponentStorage), kubeapi.Object{
		infra.AccessKeyIDVariable:     base64.StdEncoding.EncodeToString([]byte(infra.RandomSecret()[:24])),
		infra.SecretAccessKeyVariable: base64.StdEncoding.EncodeToString([]byte(infra.RandomSecret())),
	})
	found, err := k.Ensure(ctx, desired, project, kube.ComponentStorage)
	if err != nil {
		return "", "", err
	}
	return kube.CredentialPair(found)
}

// rootCredentials reads the root credentials of a provisioned scope's MinIO.
func (b *Backend) rootCredentials(ctx context.Context, k *kube.Infrastructure, project string) (access, secret string, err error) {
	found, err := k.GetOwned(ctx, k.NamespacePath(project)+"/secrets/"+rootKeys, project, kube.ComponentStorage)
	if err != nil {
		return "", "", err
	}
	if found == nil {
		return "", "", errors.New("project storage credentials are absent")
	}
	return kube.CredentialPair(found)
}

// storageURL is the S3 endpoint of a scope's MinIO.
func storageURL(namespace string) string {
	return "http://" + kubeapi.ServiceAddress(namespace, kube.ComponentStorage.String(), infra.StoragePort)
}
