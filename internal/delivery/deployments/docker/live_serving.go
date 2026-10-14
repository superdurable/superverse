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
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/projects/infra/docker"
)

// LiveServing is the Docker implementation of deployments.Serving, which
// Local Native runs. Each deployment generation is a network of its own, which
// stands for the generation's Service and exists before its application, and
// an application container of the Release image on that network and on the
// project network under the generation's name as its alias. The Live
// generation also holds the Live Worker alias there, where the project Dex
// reaches its Worker. The project's Live route moves to a generation through
// the gateway's route files.
type LiveServing struct {
	*infradocker.Infrastructure
}

var _ deployments.Serving = (*LiveServing)(nil)

// applicationUser is the unprivileged user of every application container.
const applicationUser = "65532:65532"

// generation is the name and the owner labels of the generation's network
// and container.
func (l *LiveServing) generation(s deployments.Snapshot) (string, map[string]string) {
	return l.GenerationName(s.ProjectID, s.DeploymentID, s.Lease.Generation), l.GenerationLabels(s.ProjectID, s.DeploymentID, s.Lease.Generation)
}

// projectNetwork verifies that the project network is still the one the
// deployment accepted.
func (l *LiveServing) projectNetwork(ctx context.Context, s deployments.Snapshot) error {
	_, err := l.CurrentNetwork(ctx, l.ProjectNetwork(s.ProjectID), s.Resources.Storage.Boundary.UID, l.OwnerLabels(s.ProjectID, infra.ComponentInfrastructure))
	return err
}

// EnsureGenerationService reconciles the generation's network, which exists
// before its application, so readiness and the Live route bind a stable
// identity: its network ID.
func (l *LiveServing) EnsureGenerationService(ctx context.Context, s deployments.Snapshot) (infra.ResourceRef, error) {
	if err := l.projectNetwork(ctx, s); err != nil {
		return infra.ResourceRef{}, err
	}
	name, labels := l.generation(s)
	network, err := l.EnsureNetwork(ctx, name, labels, s.Resources.Service.UID)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	return infra.ResourceRef{Boundary: l.ProjectNetwork(s.ProjectID), Name: name, UID: network.UID, Generation: s.Lease.Generation}, nil
}

// ApplyApplication runs the generation's application from its immutable image
// on the generation's and the project's networks. The application reads its
// configuration through the SDK; environment holds the grant's storage and
// project references, which may be secret, and reaches the container only as
// its environment when it is created.
func (l *LiveServing) ApplyApplication(ctx context.Context, s deployments.Snapshot, environment map[string]string) (infra.ResourceRef, error) {
	name, labels := l.generation(s)
	if s.Resources.Storage.Boundary.UID == "" || s.Resources.Service.Name != name || s.Resources.Service.UID == "" {
		return infra.ResourceRef{}, fmt.Errorf("the application infrastructure of %s has no admitted network identity", name)
	}
	if err := l.projectNetwork(ctx, s); err != nil {
		return infra.ResourceRef{}, err
	}
	if _, err := l.CurrentNetwork(ctx, name, s.Resources.Service.UID, labels); err != nil {
		return infra.ResourceRef{}, err
	}
	var env []string
	for _, setting := range deployments.ApplicationSettings(s, l.WorkerAddress(s.ProjectID), l.PublicRoutes().URL(infra.ScopeLive, s.ProjectID)) {
		env = append(env, setting[0]+"="+setting[1])
	}
	for _, key := range slices.Sorted(maps.Keys(environment)) {
		value := environment[key]
		if strings.ContainsRune(value, '\x00') || len(value) > 32768 {
			// The value may be a credential; only its name is reported.
			return infra.ResourceRef{}, fmt.Errorf("application environment value %s holds a NUL byte or exceeds 32768 bytes", key)
		}
		env = append(env, key+"="+value)
	}
	spec := dockerapi.ContainerSpec{Image: s.Inputs.ImageReference, Env: env, User: applicationUser, Labels: labels, HostConfig: dockerapi.HostConfig{
		ReadonlyRootfs: true, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges:true"}, NanoCPUs: 2e9, Memory: 1 << 30,
		// /tmp is an anonymous volume on disk, which the container's removal deletes.
		Mounts: []dockerapi.Mount{dockerapi.VolumeMount("", "/tmp")}, RestartPolicy: dockerapi.RestartPolicy{Name: "unless-stopped"},
	}}
	spec.Attach(name)
	spec.Attach(l.ProjectNetwork(s.ProjectID), infra.WorkloadName(s.DeploymentID, s.Lease.Generation))
	spec.Publish(s.Inputs.ApplicationPort, infra.LiveWorkerPort)
	container, err := l.EnsureContainer(ctx, name, spec, nil)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	if container.Config.Image != s.Inputs.ImageReference {
		return infra.ResourceRef{}, fmt.Errorf("application container %s runs %s, not the release image %s", name, container.Config.Image, s.Inputs.ImageReference)
	}
	return infra.ResourceRef{Boundary: l.ProjectNetwork(s.ProjectID), Name: name, UID: container.ID, Generation: s.Lease.Generation}, nil
}

// application reads the generation's application container at the ID the
// deployment accepted; another container is deployments.ErrApplicationChanged.
func (l *LiveServing) application(ctx context.Context, s deployments.Snapshot) (*dockerapi.Container, error) {
	name, labels := l.generation(s)
	container, err := l.API().InspectContainer(ctx, s.Providers.Application.UID)
	if err != nil {
		return nil, fmt.Errorf("read the Live application container: %w", err)
	}
	if container == nil || s.Providers.Application.UID == "" || container.Name != "/"+name || infradocker.VerifyLabels(container.Config.Labels, labels) != nil {
		return nil, deployments.ErrApplicationChanged
	}
	return container, nil
}

// VerifyReadiness reports the generation ready when its published
// definitions, its container of the accepted image and its network are the
// accepted ones, the application answers its health path and its Worker
// accepts connections. A container that is not running yet, an unhealthy
// application or a Worker that does not answer yet is infra.ErrPending with
// the reason.
func (l *LiveServing) VerifyReadiness(ctx context.Context, s deployments.Snapshot) (deployments.Readiness, error) {
	if err := l.projectNetwork(ctx, s); err != nil {
		return deployments.Readiness{}, err
	}
	if _, err := deployments.ReadPublication(ctx, l, s); err != nil {
		return deployments.Readiness{}, err
	}
	name, labels := l.generation(s)
	container, err := l.application(ctx, s)
	if err != nil {
		return deployments.Readiness{}, err
	}
	if container.Config.Image != s.Inputs.ImageReference {
		return deployments.Readiness{}, fmt.Errorf("application container %s runs %s, not the release image %s", name, container.Config.Image, s.Inputs.ImageReference)
	}
	if _, err = l.CurrentNetwork(ctx, name, s.Resources.Service.UID, labels); err != nil {
		return deployments.Readiness{}, err
	}
	address, err := container.PublishedAddress(s.Inputs.ApplicationPort)
	if err != nil {
		return deployments.Readiness{}, fmt.Errorf("%w: application container %s: %w", infra.ErrPending, name, err)
	}
	if err = probeHealth(ctx, "http://"+address+s.Inputs.HealthPath); err != nil {
		return deployments.Readiness{}, fmt.Errorf("%w: application health probe of %s: %w", infra.ErrPending, name, err)
	}
	if err = l.probeWorker(ctx, container); err != nil {
		return deployments.Readiness{}, err
	}
	return deployments.Readiness{Ready: true, ImageDigest: s.Inputs.ImageDigest, BundleDigest: s.Inputs.BundleDigest}, nil
}

// probeHealth requires HTTP 200 from a health URL within three seconds,
// without following redirects.
func probeHealth(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("application probe returned %d", response.StatusCode)
	}
	return nil
}

// errRouteMoved: the Live route moved to another generation before the
// activation committed its definitions.
var errRouteMoved = errors.New("the Live route moved to another generation before FDG activation")
