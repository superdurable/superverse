// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// dataPlaneTimeout bounds one data-plane check: the dial and DescribeNamespace.
const dataPlaneTimeout = 10 * time.Second

// temporalHome is where the Temporal start-dev keeps its database. The image
// runs as its own user, whose home the volume mounted there takes over, so the
// store is writable without a root init step.
const temporalHome = "/home/temporal"

// EnsureTemporal converges a project's Temporal start-dev on its own volume
// with the namespace and search attributes its Dex uses, on the project's
// accepted storage, and ends with one data-plane check: an answer other than
// Ready is infra.ErrPending. It returns the references it observed even with
// an error, and a reference the call does not reach stays the accepted one.
func (k *Infrastructure) EnsureTemporal(ctx context.Context, project string, storage infra.StorageRefs, accepted infra.ExecutionRefs) (infra.ExecutionRefs, error) {
	refs := accepted
	refs.TemporalNamespace = infra.LocalTemporalNamespace(project)
	if accepted.TemporalNamespace != "" && accepted.TemporalNamespace != refs.TemporalNamespace {
		return accepted, errors.New("project Temporal namespace changed")
	}
	if err := k.VerifyProjectStorage(ctx, project, storage); err != nil {
		return refs, err
	}
	temporal := component{component: infra.ComponentTemporal, image: k.config.TemporalImage, command: infra.TemporalStartDevArgs(refs.TemporalNamespace, temporalHome+"/temporal.db"),
		volume: temporalHome, port: infra.TemporalPort}
	service, err := k.ensureComponent(ctx, project, storage.Boundary, temporal, accepted.TemporalCredential.UID)
	if service.UID != "" {
		refs.TemporalCredential = service
	}
	if err != nil {
		return refs, err
	}
	answer, err := k.describeTemporalNamespace(ctx, project, service, refs.TemporalNamespace)
	if err == nil && answer != infra.DataPlaneReady {
		err = fmt.Errorf("%w: the Temporal data plane is %s", infra.ErrPending, answer)
	}
	return refs, err
}

// CheckTemporalDataPlane reads a project's accepted Temporal without changing
// anything and asks its data plane once, within 10 seconds, to describe the
// namespace.
func (k *Infrastructure) CheckTemporalDataPlane(ctx context.Context, project string, storage infra.StorageRefs, accepted infra.ExecutionRefs) (infra.DataPlaneAnswer, error) {
	if err := k.verifyTemporal(project, storage, accepted); err != nil {
		return "", err
	}
	return k.describeTemporalNamespace(ctx, project, accepted.TemporalCredential, accepted.TemporalNamespace)
}

// verifyTemporal requires the accepted Temporal to be the project's own.
func (k *Infrastructure) verifyTemporal(project string, storage infra.StorageRefs, accepted infra.ExecutionRefs) error {
	if accepted.TemporalNamespace != infra.LocalTemporalNamespace(project) || accepted.TemporalCredential.UID == "" || accepted.TemporalCredential.Boundary != storage.Boundary.Name {
		return errors.New("project Temporal resource identity is incomplete")
	}
	return nil
}

// describeTemporalNamespace asks the project's Temporal once to describe the
// namespace, through the port its accepted container publishes now.
func (k *Infrastructure) describeTemporalNamespace(ctx context.Context, project string, service infra.ResourceRef, namespace string) (answer infra.DataPlaneAnswer, err error) {
	ctx, cancel := context.WithTimeout(ctx, dataPlaneTimeout)
	defer cancel()
	address, err := k.componentAddress(ctx, project, service, infra.ComponentTemporal, infra.TemporalPort)
	switch {
	case errors.Is(err, infra.ErrOwnershipConflict) || errors.Is(err, errIncomplete):
		return "", err
	case err != nil:
		// A Temporal that is absent or publishes no port does not answer.
		return infra.DataPlaneUnreachable, nil
	}
	connection, closeConnection, err := dialPrivate(ctx, address)
	if err != nil {
		return "", fmt.Errorf("dial the Temporal data plane: %w", err)
	}
	defer func() { err = errors.Join(err, closeConnection()) }()
	return infra.DescribeTemporalNamespace(ctx, connection, namespace)
}

// EnsureExecutionAccess verifies the project's accepted storage. In Docker the
// project Dex and the applications need no service account or traffic
// policy: the Dex reaches the storage with the root key pair and every
// application with its grant's.
func (k *Infrastructure) EnsureExecutionAccess(ctx context.Context, project string, storage infra.StorageRefs) error {
	return k.VerifyProjectStorage(ctx, project, storage)
}

// dexConfigDirectory holds the project Dex's configuration file.
const dexConfigDirectory = "/etc/dex"

// StartProjectDex converges a project's Live Dex on its accepted storage and
// Temporal, which it reads without changing anything, and returns the
// references with the Dex service, also while the Dex does not accept
// connections yet. The configuration, which holds the MinIO root key pair, is
// copied into the container before it starts; a container of another Dex
// image is replaced in place under the same service identity.
func (k *Infrastructure) StartProjectDex(ctx context.Context, project string, storage infra.StorageRefs, accepted infra.ExecutionRefs) (infra.ExecutionRefs, error) {
	if err := k.VerifyProjectStorage(ctx, project, storage); err != nil {
		return accepted, err
	}
	if err := k.verifyTemporal(project, storage, accepted); err != nil {
		return accepted, err
	}
	root, exists, err := readCredential(k.rootCredentialPath(project), rootOwner(project, storage.Boundary.UID))
	if err != nil || !exists {
		return accepted, errors.Join(errors.New("project storage credentials are absent"), err)
	}
	access := infra.LocalTemporalAccess(accepted.TemporalNamespace)
	config := comm.Must(json.Marshal(infra.ProjectDexConfig(infra.LocalDexTemporal(access), infra.LocalDexStorage(storage.StorageBucket, root.Access, root.Secret), 0)))
	dex := component{component: infra.ComponentDex, image: k.config.DexImage, command: []string{"--services", "api,interpreter"}, port: infra.DexPort, replaceable: true,
		revision: fmt.Sprintf("%x", sha256.Sum256(config)),
		env:      []string{"CONFIG_TEMPLATE_PATH=" + dexConfigDirectory + "/config.yaml", "TEMPORAL_HOST_PORT=" + access.HostPort},
		prepare: func(ctx context.Context, id string) error {
			return k.api.CopyToContainer(ctx, id, "/etc", dexConfigArchive(config))
		}}
	refs := accepted
	service, err := k.ensureComponent(ctx, project, storage.Boundary, dex, accepted.DexService.UID)
	if service.UID != "" {
		refs.DexService = service
	}
	return refs, err
}

// dexConfigArchive is the tar archive that places the configuration as
// dex/config.yaml, readable only by the Dex's user.
func dexConfigArchive(config []byte) []byte {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	comm.MustDo(writer.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "dex/", Mode: 0o700}))
	comm.MustDo(writer.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "dex/config.yaml", Mode: 0o400, Size: int64(len(config))}))
	comm.Must(writer.Write(config))
	comm.MustDo(writer.Close())
	return archive.Bytes()
}

// verifyProjectDex checks the project's storage references, that its Dex is
// the accepted service and that it accepts connections, and returns the
// address the Dex publishes now.
func (k *Infrastructure) verifyProjectDex(ctx context.Context, project string, refs infra.ScopeRefs) (string, error) {
	if err := k.VerifyProjectStorage(ctx, project, refs.Storage); err != nil {
		return "", err
	}
	dex := refs.Execution.DexService
	if dex.Boundary != refs.Storage.Boundary.Name {
		return "", errors.New("project Dex resource identity is incomplete")
	}
	address, err := k.componentAddress(ctx, project, dex, infra.ComponentDex, infra.DexPort)
	if err != nil {
		return "", err
	}
	if !accepts(ctx, address) {
		return "", fmt.Errorf("%w: the project Dex does not accept connections yet", infra.ErrPending)
	}
	return address, nil
}

// dialPrivate connects to a loopback address the engine published. The
// returned function closes the connection, once.
func dialPrivate(ctx context.Context, address string) (*grpc.ClientConn, func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	connection, err := grpc.NewClient("passthrough:///"+address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy())
	if err != nil {
		return nil, nil, err
	}
	var once sync.Once
	var closeErr error
	return connection, func() error {
		once.Do(func() { closeErr = connection.Close() })
		return closeErr
	}, nil
}
