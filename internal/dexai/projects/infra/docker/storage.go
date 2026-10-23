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
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	madmin "github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// component is a part of a project's scope that runs as one container on the
// project network, under the component's name as its alias, with a named
// volume for its data.
type component struct {
	component infra.Component
	image     string
	// command follows the image's entrypoint; env may hold credentials.
	command, env []string
	// volume is where the component's data volume is mounted, if it has one.
	volume string
	port   int
	// prepare gives a new container what it needs before it starts.
	prepare func(ctx context.Context, id string) error
	// replaceable lets a container of another image be replaced in place under
	// the same service identity, as a Kubernetes Deployment rolls; any other
	// component of another image is a conflict.
	replaceable bool
	// revision is the digest of the configuration a replaceable component
	// starts with; a container of another revision is replaced too.
	revision string
	// address is the component's fixed address on the project network, which
	// a workload may keep; without one the engine allocates an address.
	address netip.Addr
}

// componentStopGrace is how long a project component gets to stop.
const componentStopGrace = 30 * time.Second

// ensureComponent converges a component of the project's scope and returns
// the reference of its service, also while the component does not accept
// connections yet (infra.ErrPending). The service UID is the container's
// serviceUIDLabel: a container whose UID differs from a non-empty acceptedUID
// is a replacement and is never adopted, and a missing container of an
// accepted service is created again under that UID, on the same volume, as a
// Kubernetes Deployment replaces its Pod. A container this call starts is
// pending: the engine reports it running, and its published port accepts,
// before its process listens, so readiness is observed from the next round on,
// as Kubernetes observes a new Pod only through its readiness probe.
func (k *Infrastructure) ensureComponent(ctx context.Context, project string, network infra.ResourceRef, c component, acceptedUID string) (infra.ResourceRef, error) {
	name, labels := k.componentName(project, c.component), k.OwnerLabels(project, c.component)
	existing, err := k.OwnedContainer(ctx, name, labels)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	uid := acceptedUID
	started := existing == nil || !existing.State.Running
	if existing != nil {
		found := existing.Config.Labels[serviceUIDLabel]
		if found == "" || acceptedUID != "" && found != acceptedUID {
			return infra.ResourceRef{}, fmt.Errorf("%w: the project %s container was replaced", infra.ErrOwnershipConflict, c.component)
		}
		uid = found
		if fixed, _ := existing.FixedAddress(network.Name); c.address.IsValid() && fixed != c.address.String() {
			return infra.ResourceRef{}, fmt.Errorf("the project %s container predates its fixed address %s: delete the project and create it again", c.component, c.address)
		}
		if existing.Config.Image != c.image || existing.Config.Labels[revisionLabel] != c.revision {
			if !c.replaceable {
				return infra.ResourceRef{}, fmt.Errorf("the project %s container runs another image than this server's", c.component)
			}
			if err = pending(k.RemoveContainer(ctx, name, labels, componentStopGrace))("the project " + c.component.String() + " container of another image or configuration"); err != nil {
				return infra.ResourceRef{}, err
			}
			started = true
		}
	}
	if uid == "" {
		uid = uuid.NewString()
	}
	containerLabels := WithLabel(labels, serviceUIDLabel, uid)
	if c.revision != "" {
		containerLabels = WithLabel(containerLabels, revisionLabel, c.revision)
	}
	spec := dockerapi.ContainerSpec{Image: c.image, Cmd: c.command, Env: c.env, Labels: containerLabels,
		StopTimeout: int(componentStopGrace / time.Second), HostConfig: dockerapi.HostConfig{
			CapDrop: []string{"ALL"}, SecurityOpt: []string{noNewPrivileges}, NanoCPUs: 2e9, Memory: 2 << 30,
			RestartPolicy: dockerapi.RestartPolicy{Name: restartUnlessStopped},
		}}
	if c.volume != "" {
		volume := name + "-data"
		if _, err = k.EnsureVolume(ctx, volume, labels); err != nil {
			return infra.ResourceRef{}, err
		}
		spec.HostConfig.Mounts = []dockerapi.Mount{dockerapi.VolumeMount(volume, c.volume)}
	}
	if c.address.IsValid() {
		spec.AttachAt(network.Name, c.address.String(), c.component.String())
	} else {
		spec.Attach(network.Name, c.component.String())
	}
	spec.Publish(c.port)
	container, err := k.EnsureContainer(ctx, name, spec, c.prepare)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	ref := infra.ResourceRef{Boundary: network.Name, Name: name, UID: container.Config.Labels[serviceUIDLabel]}
	if acceptedUID != "" && ref.UID != acceptedUID {
		return infra.ResourceRef{}, fmt.Errorf("%w: the project %s container was replaced", infra.ErrOwnershipConflict, c.component)
	}
	if started {
		return ref, fmt.Errorf("%w: the project %s container is starting", infra.ErrPending, c.component)
	}
	address, err := container.PublishedAddress(c.port)
	if err != nil || !accepts(ctx, address) {
		return ref, fmt.Errorf("%w: the project %s container does not accept connections yet", infra.ErrPending, c.component)
	}
	return ref, nil
}

// The isolation every container of the runtime gets.
const (
	noNewPrivileges      = "no-new-privileges:true"
	restartUnlessStopped = "unless-stopped"
)

// componentAddress is the loopback address of the accepted service of a
// project component on its port.
func (k *Infrastructure) componentAddress(ctx context.Context, project string, service infra.ResourceRef, c infra.Component, port int) (string, error) {
	name := k.componentName(project, c)
	if service.Name != name || service.UID == "" {
		return "", errIncomplete
	}
	container, err := k.OwnedContainer(ctx, name, WithLabel(k.OwnerLabels(project, c), serviceUIDLabel, service.UID))
	if err != nil {
		return "", err
	}
	if container == nil {
		return "", fmt.Errorf("the project %s container is absent", c)
	}
	return container.PublishedAddress(port)
}

// EnsureStorage converges a project's configuration storage: its network, the
// root key pair of its MinIO, the MinIO on its volume and the versioned
// application bucket. It returns infra.ErrPending while MinIO becomes ready,
// which is when it answers its readiness endpoint: a published port accepts
// connections before the MinIO in the container listens.
// The owning Flow records the returned references even with an error, so
// partially provisioned resources stay owned for cleanup; a reference the
// call does not reach stays the accepted one.
func (k *Infrastructure) EnsureStorage(ctx context.Context, project string, accepted infra.StorageRefs) (infra.StorageRefs, error) {
	network, err := k.EnsureNetwork(ctx, k.ProjectNetwork(project), k.OwnerLabels(project, infra.ComponentInfrastructure), accepted.Boundary.UID)
	if err != nil {
		return accepted, err
	}
	refs := accepted
	refs.Boundary, refs.StorageBucket, refs.BlobPrefix = network, infra.LocalApplicationBucket, infra.ConfigurationPrefix
	root, err := ensureCredential(k.rootCredentialPath(project), rootOwner(project, network.UID), rootKeys)
	if err != nil {
		return refs, err
	}
	address, err := k.storageAddress(ctx, project, network)
	if err != nil {
		return refs, err
	}
	minio := component{component: infra.ComponentStorage, image: k.config.S3Image, command: []string{"server", "/data"}, volume: "/data", port: infra.StoragePort,
		env: []string{"MINIO_ROOT_USER=" + root.Access, "MINIO_ROOT_PASSWORD=" + root.Secret}, address: address}
	service, err := k.ensureComponent(ctx, project, network, minio, accepted.Storage.UID)
	if service.UID != "" {
		refs.Storage = service
	}
	if err != nil {
		return refs, err
	}
	storage, err := k.openStorage(ctx, project, network.UID, service)
	if err != nil {
		return refs, err
	}
	if err = storage.answers(ctx); err != nil {
		return refs, err
	}
	return refs, infra.EnsureBucket(ctx, storage.objects, infra.OAuthLifecycle(project))
}

// storageAnswerTimeout bounds the readiness probe of a project MinIO, so a
// round that waits for the storage keeps time to hand its references on.
const storageAnswerTimeout = 5 * time.Second

// answers reports a MinIO that does not answer its readiness endpoint within
// storageAnswerTimeout, one that still starts or does not run, as
// infra.ErrPending.
func (c minioConnection) answers(ctx context.Context) error {
	probe, cancel := context.WithTimeout(ctx, storageAnswerTimeout)
	defer cancel()
	request := comm.Must(http.NewRequestWithContext(probe, http.MethodGet, "http://"+c.address+"/minio/health/ready", nil))
	response, err := storageProbeClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: the project storage does not answer yet: %w", infra.ErrPending, err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: the project storage answers %s", infra.ErrPending, response.Status)
	}
	return nil
}

// storageProbeClient asks a project MinIO whether it is ready and never
// follows a redirect.
var storageProbeClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// storageAddress is the fixed address of the project storage on the accepted
// project network network.
func (k *Infrastructure) storageAddress(ctx context.Context, project string, network infra.ResourceRef) (netip.Addr, error) {
	current, err := k.CurrentNetwork(ctx, network.Name, network.UID, k.OwnerLabels(project, infra.ComponentInfrastructure))
	if err != nil {
		return netip.Addr{}, err
	}
	return fixedAddress(current, storageHost)
}

// storageEndpoint is the endpoint of the project storage that a workload on
// the project network keeps for its grant's life: the storage container's
// fixed address on the project network, which the container keeps across a
// replacement, and which the Connector SDK accepts as a local endpoint, being
// a private address. The container must hold it now.
func (k *Infrastructure) storageEndpoint(ctx context.Context, project string, storage infra.StorageRefs) (string, error) {
	address, err := k.storageAddress(ctx, project, storage.Boundary)
	if err != nil {
		return "", err
	}
	labels := k.OwnerLabels(project, infra.ComponentStorage)
	if storage.Storage.UID != "" {
		labels = WithLabel(labels, serviceUIDLabel, storage.Storage.UID)
	}
	container, err := k.OwnedContainer(ctx, k.componentName(project, infra.ComponentStorage), labels)
	if err != nil {
		return "", err
	}
	if container == nil {
		return "", fmt.Errorf("%w: the project storage container is absent", infra.ErrPending)
	}
	if fixed, _ := container.FixedAddress(storage.Boundary.Name); fixed != address.String() {
		return "", fmt.Errorf("the project storage container does not hold its fixed address %s", address)
	}
	return "http://" + net.JoinHostPort(address.String(), strconv.Itoa(infra.StoragePort)), nil
}

// Complete reports whether every storage identity is recorded, the MinIO
// service included.
func (k *Infrastructure) Complete(r infra.StorageRefs) bool {
	return r.Boundary.UID != "" && r.StorageBucket != "" && r.BlobPrefix != "" && r.Storage.UID != ""
}

// VerifyProjectStorage checks the project's accepted storage references and
// that its network is still the accepted one.
func (k *Infrastructure) VerifyProjectStorage(ctx context.Context, project string, storage infra.StorageRefs) error {
	if storage.Boundary.Name != k.ProjectNetwork(project) || storage.Boundary.UID == "" || storage.StorageBucket != infra.LocalApplicationBucket || storage.BlobPrefix != infra.ConfigurationPrefix {
		return errors.New("project storage resource identity is incomplete")
	}
	_, err := k.CurrentNetwork(ctx, storage.Boundary.Name, storage.Boundary.UID, k.OwnerLabels(project, infra.ComponentInfrastructure))
	return err
}

// LiveStorage are the storage references of a project from the network ID and
// bucket a Flow pinned; the network name and the key prefix are fixed.
func (k *Infrastructure) LiveStorage(project, networkID, bucket string) infra.StorageRefs {
	return infra.StorageRefs{Boundary: infra.ResourceRef{Name: k.ProjectNetwork(project), UID: networkID}, StorageBucket: bucket, BlobPrefix: infra.ConfigurationPrefix}
}

// S3 returns the project's bucket on the MinIO of the network networkID.
func (k *Infrastructure) S3(ctx context.Context, project, networkID string) (*infra.ApplicationObjects, error) {
	storage, err := k.openStorage(ctx, project, networkID, infra.ResourceRef{})
	return storage.objects, err
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
	storage, err := k.openStorage(ctx, scope.ProjectID, scope.Storage.Boundary.UID, infra.ResourceRef{})
	if err != nil {
		return nil, err
	}
	return infra.ReadExactVersion(ctx, storage.objects, ref)
}

// ConfigurationObjects returns the Connector SDK object store of the scope in
// the project's storage, confined to the scope's own configuration prefix,
// which checks the project network before every operation.
func (k *Infrastructure) ConfigurationObjects(ctx context.Context, scope infra.ScopeRef) (connectorconfig.ObjectStore, error) {
	if err := k.VerifyProjectStorage(ctx, scope.ProjectID, scope.Storage); err != nil {
		return nil, err
	}
	storage, err := k.openStorage(ctx, scope.ProjectID, scope.Storage.Boundary.UID, infra.ResourceRef{})
	if err != nil {
		return nil, err
	}
	prefix, err := (connectorconfig.Scope{ProjectID: scope.ProjectID, Kind: scope.Scope.Kind.String()}).Prefix()
	if err != nil {
		return nil, err
	}
	objects := storage.objects
	store, err := connectorconfig.NewS3ObjectStore(ctx, &connectorconfig.S3StoreConfig{Client: infra.ConfigurationS3Client(objects), Bucket: objects.Bucket, Prefix: scope.Storage.BlobPrefix, AllowUnencrypted: true})
	if err != nil {
		return nil, err
	}
	return &configurationObjects{infrastructure: k, project: scope.ProjectID, storage: scope.Storage, prefix: prefix + "/", store: store}, nil
}

// configurationObjects admits only keys below the scope's prefix and verifies
// the project network before every operation.
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

// minioConnection is a project's MinIO, reached with its root key pair.
type minioConnection struct {
	objects *infra.ApplicationObjects
	admin   *madmin.AdminClient
	// root is the root access key, the parent user of every workload account.
	root string
	// address is the loopback address MinIO publishes now.
	address string
}

// openStorage connects to the MinIO of the project network networkID through
// the port it publishes now, with the root key pair the server created for
// that network. A non-empty service also binds the accepted MinIO service.
func (k *Infrastructure) openStorage(ctx context.Context, project, networkID string, service infra.ResourceRef) (minioConnection, error) {
	if networkID == "" {
		return minioConnection{}, errIncomplete
	}
	root, exists, err := readCredential(k.rootCredentialPath(project), rootOwner(project, networkID))
	if err != nil {
		return minioConnection{}, err
	}
	if !exists {
		return minioConnection{}, errors.New("project storage credentials are absent")
	}
	labels := k.OwnerLabels(project, infra.ComponentStorage)
	if service.UID != "" {
		labels = WithLabel(labels, serviceUIDLabel, service.UID)
	}
	address, err := k.storagePublishedAddress(ctx, project, networkID, labels)
	if err != nil {
		return minioConnection{}, fmt.Errorf("%w: the project storage is not reachable yet: %w", infra.ErrPending, err)
	}
	objects := &infra.ApplicationObjects{Client: infra.ProjectS3("http://"+address, root.Access, root.Secret, nil), Bucket: infra.LocalApplicationBucket}
	admin := comm.Must(madmin.NewWithOptions(address, &madmin.Options{Creds: credentials.NewStaticV4(root.Access, root.Secret, "")}))
	return minioConnection{objects: objects, admin: admin, root: root.Access, address: address}, nil
}

// storagePublishedAddress recovers an owned stopped MinIO even after project
// setup completed, when Preview cleanup opens storage without EnsureStorage.
func (k *Infrastructure) storagePublishedAddress(ctx context.Context, project, networkID string, labels map[string]string) (string, error) {
	if _, err := k.CurrentNetwork(ctx, k.ProjectNetwork(project), networkID, k.OwnerLabels(project, infra.ComponentInfrastructure)); err != nil {
		return "", err
	}
	name := k.componentName(project, infra.ComponentStorage)
	container, err := k.OwnedContainer(ctx, name, labels)
	if err != nil {
		return "", err
	}
	if container == nil {
		return "", fmt.Errorf("container %s is absent", name)
	}
	switch container.State.Status {
	case dockerapi.StateCreated, dockerapi.StateExited, dockerapi.StateDead:
		if container.Config.Labels[serviceUIDLabel] == "" {
			return "", fmt.Errorf("%w: the project storage container has no service identity", infra.ErrOwnershipConflict)
		}
		if err = k.api.StartContainer(ctx, container.ID); err != nil {
			return "", fmt.Errorf("start the project storage container: %w", err)
		}
		return "", fmt.Errorf("%w: the project storage container is starting", infra.ErrPending)
	default:
		return container.PublishedAddress(infra.StoragePort)
	}
}
