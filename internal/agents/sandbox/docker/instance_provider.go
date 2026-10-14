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
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/projects/infra/docker"
)

// InstanceProvider runs Sandbox instances as Docker containers of the coding
// image. The isolation boundary of a Project's Session is its network, its workspace the pair
// of volumes for the repository and the Preview state, and an instance one
// container with the hardening of the coding profile's Pod. It queries exact
// identities before every effect and owns no lifecycle state. It implements
// sandbox.InstanceProvider.
type InstanceProvider struct {
	// Infrastructure supplies the engine, the names, the owner labels and the
	// routes.
	*infradocker.Infrastructure
	// ProfileName is the rendered coding profile in ProfilePath.
	ProfileName, ProfilePath string
	Workspace                *workspace.Capabilities
	// Connections reaches the Preview Dex of an instance.
	Connections infra.Connections
}

var _ sandbox.InstanceProvider = (*InstanceProvider)(nil)

// network is the network of the Project's Sandbox.
func (p *InstanceProvider) network(id sandbox.Identity) string {
	return p.SessionNetwork(id.ProjectID)
}

// SessionBoundary is the network of the Project's Sandbox.
func (p *InstanceProvider) SessionBoundary(projectID string) string {
	return p.SessionNetwork(projectID)
}

// containerName is the Session's Sandbox container: one instance at a time.
func (p *InstanceProvider) containerName(id sandbox.Identity) string {
	return p.network(id) + "-" + infradocker.SandboxAlias
}

// workspaceName is the base name of the Session's workspace volumes.
func (p *InstanceProvider) workspaceName(id sandbox.Identity) string {
	return p.network(id) + "-workspace"
}

// workspaceVolumes are the Session's workspace volumes, one per subPath.
func (p *InstanceProvider) workspaceVolumes(id sandbox.Identity) []string {
	var volumes []string
	for _, subPath := range infradocker.WorkspaceSubPaths {
		volumes = append(volumes, infradocker.WorkspaceVolume(p.workspaceName(id), subPath))
	}
	return volumes
}

// owner labels mark every object of the Project's Sandbox; the coding profile
// is checked on its own, because a volume of another profile is a refusal,
// not a foreign object.
func (p *InstanceProvider) owner(id sandbox.Identity) map[string]string {
	return p.PreviewLabels(id.ProjectID)
}

// profiled are the owner labels with the coding profile.
func (p *InstanceProvider) profiled(id sandbox.Identity) map[string]string {
	return infradocker.WithLabel(p.owner(id), deploymentnames.SandboxProfileLabel, infra.CodingSandboxProfile)
}

// instanceLabels mark the container of one instance: the Session's, its
// workspace request and generation, the image contract and the instance UID.
func (p *InstanceProvider) instanceLabels(s sandbox.Instance, uid string) map[string]string {
	labels := p.profiled(s.Identity)
	labels[deploymentnames.GenerationLabel] = strconv.FormatUint(s.Generation, 10)
	labels[deploymentnames.WorkspaceRequestLabel] = s.WorkspaceRequestID
	labels[sandbox.ImageContractLabel] = strconv.Itoa(sandbox.ImageContractRevision)
	labels[infradocker.InstanceUIDLabel] = uid
	return labels
}

// adopt accepts found unless a different object was recorded before.
func adopt(recorded, found sandbox.ResourceRef, kind string) (sandbox.ResourceRef, error) {
	if recorded.UID != "" && recorded.UID != found.UID {
		return recorded, fmt.Errorf("%w: %s UID changed from %s to %s", infra.ErrOwnershipConflict, kind, recorded.UID, found.UID)
	}
	return found, nil
}

// Provision converges the instance's network, workspace volumes and container
// and returns its resources; ready reports that the container runs and its
// sandboxd answers its health check. A recorded identity that no longer
// matches is a failure, never adopted. The container is created only once
// the workspace volumes are of this profile and no other container mounts
// them: ErrProfileMismatch and ErrVolumeInUse refuse it, and a container of an
// earlier instance that is being removed is waited for.
func (p *InstanceProvider) Provision(ctx context.Context, s sandbox.Instance) (sandbox.Resources, bool, error) {
	r, id := s.Resources, s.Identity
	profile, err := readProfile(p.ProfilePath, p.ProfileName)
	if err != nil {
		return r, false, err
	}
	network, err := p.EnsureNetwork(ctx, p.network(id), p.profiled(id), r.Boundary.UID)
	if err != nil {
		return r, false, err
	}
	if r.Boundary, err = adopt(r.Boundary, sandbox.ResourceRef{Name: network.Name, UID: network.UID}, "network"); err != nil {
		return r, false, err
	}
	volume, err := p.ensureWorkspace(ctx, id, r.Volume.UID)
	if err != nil {
		return r, false, err
	}
	if r.Volume, err = adopt(r.Volume, volume, "workspace"); err != nil {
		return r, false, err
	}
	if r.Identity, err = adopt(r.Identity, volume, "workspace identity"); err != nil {
		return r, false, err
	}
	r.Profile = infra.CodingSandboxProfile
	free, err := p.workspaceFree(ctx, s)
	if err != nil || !free {
		return r, false, err
	}
	project, err := p.projectNetwork(ctx, id.ProjectID)
	if err != nil {
		return r, false, err
	}
	container, err := p.ensureInstance(ctx, s, r, profile, project)
	if err != nil {
		return r, false, err
	}
	instance := sandbox.ResourceRef{Boundary: network.Name, Name: p.containerName(id), UID: container.Config.Labels[infradocker.InstanceUIDLabel]}
	if r.Instance, err = adopt(r.Instance, instance, "Sandbox"); err != nil {
		return r, false, err
	}
	if r.ServiceUID != "" && r.ServiceUID != container.ID {
		return r, false, fmt.Errorf("%w: Sandbox container ID changed from %s to %s", infra.ErrOwnershipConflict, r.ServiceUID, container.ID)
	}
	r.ServiceName, r.ServiceUID, r.ImageDigest = infradocker.SandboxAlias, container.ID, container.Config.Image
	return r, p.healthy(ctx, *container), nil
}

// ensureWorkspace converges the Session's two workspace volumes under one
// workspace UID: the volumes', the recorded one, or a new one. A retained
// volume of another coding profile is sandbox.ErrProfileMismatch.
func (p *InstanceProvider) ensureWorkspace(ctx context.Context, id sandbox.Identity, recordedUID string) (sandbox.ResourceRef, error) {
	uid := recordedUID
	for _, name := range p.workspaceVolumes(id) {
		volume, err := p.API().InspectVolume(ctx, name)
		if err != nil {
			return sandbox.ResourceRef{}, err
		}
		if volume == nil {
			continue
		}
		if err = infradocker.VerifyLabels(volume.Labels, p.owner(id)); err != nil {
			return sandbox.ResourceRef{}, fmt.Errorf("volume %s: %w", name, err)
		}
		if profile := volume.Labels[deploymentnames.SandboxProfileLabel]; profile != infra.CodingSandboxProfile {
			return sandbox.ResourceRef{}, fmt.Errorf("%w: volume %s is %q", sandbox.ErrProfileMismatch, name, profile)
		}
		found := volume.Labels[infradocker.WorkspaceUIDLabel]
		if found == "" || uid != "" && found != uid {
			return sandbox.ResourceRef{}, fmt.Errorf("%w: workspace volume %s has another workspace UID", infra.ErrOwnershipConflict, name)
		}
		uid = found
	}
	if uid == "" {
		uid = uuid.NewString()
	}
	for _, name := range p.workspaceVolumes(id) {
		if _, err := p.EnsureVolume(ctx, name, infradocker.WithLabel(p.profiled(id), infradocker.WorkspaceUIDLabel, uid)); err != nil {
			return sandbox.ResourceRef{}, err
		}
	}
	return sandbox.ResourceRef{Boundary: p.network(id), Name: p.workspaceName(id), UID: uid}, nil
}

// workspaceFree reports whether no container but the instance's own mounts
// the workspace volumes. The Preview engine's state must have one writer. A
// container of an earlier instance that is being removed is not free yet; one
// that nobody removes is sandbox.ErrVolumeInUse, because the earlier
// instance's reclamation already waited for its container to go.
func (p *InstanceProvider) workspaceFree(ctx context.Context, s sandbox.Instance) (bool, error) {
	free := true
	for _, name := range p.workspaceVolumes(s.Identity) {
		containers, err := p.API().ListContainers(ctx, dockerapi.Filters{"volume": {name}})
		if err != nil {
			return false, err
		}
		for _, container := range containers {
			if container.Labels[deploymentnames.WorkspaceRequestLabel] == s.WorkspaceRequestID && infradocker.VerifyLabels(container.Labels, p.owner(s.Identity)) == nil {
				continue
			}
			if container.State != dockerapi.StateRemoving {
				return false, fmt.Errorf("%w: container %s mounts volume %s", sandbox.ErrVolumeInUse, container.ID, name)
			}
			free = false
		}
	}
	return free, nil
}

// projectNetwork is the network of the project's Live scope, which the
// Sandbox joins so its Preview reaches the Live storage.
func (p *InstanceProvider) projectNetwork(ctx context.Context, project string) (string, error) {
	name := p.ProjectNetwork(project)
	network, err := p.CleanupNetwork(ctx, name, "", p.OwnerLabels(project, infra.ComponentInfrastructure))
	if err != nil {
		return "", err
	}
	if network == nil {
		return "", fmt.Errorf("the project network %s is absent", name)
	}
	return name, nil
}

// ensureInstance converges the instance's container. A container of another
// workspace request is never adopted, and a recorded instance whose container
// is gone is not created again: the instance is lost, as a Sandbox whose
// object was deleted. A created or exited container of this instance is
// started; nothing restarts it otherwise.
func (p *InstanceProvider) ensureInstance(ctx context.Context, s sandbox.Instance, r sandbox.Resources, profile codingProfile, project string) (*dockerapi.Container, error) {
	name := p.containerName(s.Identity)
	existing, err := p.OwnedContainer(ctx, name, p.owner(s.Identity))
	if err != nil {
		return nil, err
	}
	var uid string
	switch {
	case existing != nil && existing.Config.Labels[deploymentnames.WorkspaceRequestLabel] != s.WorkspaceRequestID:
		return nil, fmt.Errorf("%w: container %s belongs to another instance", infra.ErrOwnershipConflict, name)
	case existing != nil:
		uid = existing.Config.Labels[infradocker.InstanceUIDLabel]
	case r.Instance.UID != "":
		return nil, fmt.Errorf("%w: the recorded Sandbox container %s is gone", infra.ErrOwnershipConflict, name)
	default:
		uid = uuid.NewString()
	}
	spec := dockerapi.ContainerSpec{Image: profile.image, Entrypoint: profile.entrypoint, Cmd: profile.cmd, User: profile.user,
		Env: append(append([]string{}, profile.env...), profile.podUIDVariable+"="+uid), Labels: p.instanceLabels(s, uid), StopTimeout: int(profile.grace / time.Second),
		HostConfig: dockerapi.HostConfig{ReadonlyRootfs: true, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges:true"},
			NanoCPUs: profile.nanoCPUs, Memory: profile.memory, RestartPolicy: dockerapi.RestartPolicy{Name: "no"}}}
	for _, subPath := range infradocker.WorkspaceSubPaths {
		spec.HostConfig.Mounts = append(spec.HostConfig.Mounts, dockerapi.VolumeMount(infradocker.WorkspaceVolume(p.workspaceName(s.Identity), subPath), profile.workspace[subPath]))
	}
	for _, target := range profile.scratch {
		// An anonymous volume on disk, removed with the container, as an
		// emptyDir goes with its Pod; tmpfs would count against the memory.
		spec.HostConfig.Mounts = append(spec.HostConfig.Mounts, dockerapi.VolumeMount("", target))
	}
	spec.Attach(p.network(s.Identity), infradocker.SandboxAlias)
	spec.Attach(project)
	spec.Publish(profile.ports...)
	return p.EnsureContainer(ctx, name, spec, nil)
}

// healthy reports whether the container runs and its sandboxd answers its
// health check, the probe of the coding profile's Pod.
func (p *InstanceProvider) healthy(ctx context.Context, container dockerapi.Container) bool {
	address, err := container.PublishedAddress(workspace.SandboxdFilesystemPort)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/v1/health", nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode == http.StatusOK
}

// DiscoverResources completes the references a reclamation or the deleting
// Project's release needs from the deterministic names of the Session's
// network, workspace volumes and container: the release may know none of
// them, and a provisioning whose attempts failed after creating objects
// recorded none. Every object found must carry the Session's owner labels and
// any recorded UID; an absent object keeps the zero reference.
func (p *InstanceProvider) DiscoverResources(ctx context.Context, s sandbox.Instance) (sandbox.Resources, error) {
	r, id := s.Resources, s.Identity
	network, err := p.CleanupNetwork(ctx, p.network(id), "", p.owner(id))
	if err != nil || network == nil {
		return r, err
	}
	if r.Boundary, err = adopt(r.Boundary, sandbox.ResourceRef{Name: network.Name, UID: network.ID}, "network"); err != nil {
		return r, err
	}
	if r.Volume.UID == "" {
		repository := p.workspaceVolumes(id)[0]
		volume, err := p.API().InspectVolume(ctx, repository)
		if err != nil {
			return r, err
		}
		if volume != nil {
			if err = infradocker.VerifyLabels(volume.Labels, p.owner(id)); err != nil {
				return r, fmt.Errorf("volume %s: %w", repository, err)
			}
			r.Volume = sandbox.ResourceRef{Boundary: network.Name, Name: p.workspaceName(id), UID: volume.Labels[infradocker.WorkspaceUIDLabel]}
		}
	}
	if r.Instance.UID == "" {
		container, err := p.OwnedContainer(ctx, p.containerName(id), p.owner(id))
		if err != nil || container == nil {
			return r, err
		}
		r.Instance = sandbox.ResourceRef{Boundary: network.Name, Name: p.containerName(id), UID: container.Config.Labels[infradocker.InstanceUIDLabel]}
		r.ServiceName, r.ServiceUID = infradocker.SandboxAlias, container.ID
	}
	return r, nil
}

// SandboxAddresses resolves the sandboxd of an admitted Sandbox: the
// container that carries the binding's instance UID and the Session's owner
// labels, running and attached to the binding's network under its alias,
// through the ports it publishes now.
func (p *InstanceProvider) SandboxAddresses(ctx context.Context, binding workspace.Binding) (string, string, error) {
	labels := infradocker.WithLabel(p.PreviewLabels(binding.ProjectID), infradocker.InstanceUIDLabel, binding.SandboxUID)
	containers, err := p.API().ListContainers(ctx, dockerapi.Labels(infradocker.SelectorLabels(labels)))
	if err != nil {
		return "", "", err
	}
	containers = slices.DeleteFunc(containers, func(c dockerapi.ContainerSummary) bool { return infradocker.VerifyLabels(c.Labels, labels) != nil })
	if len(containers) != 1 {
		return "", "", fmt.Errorf("%d containers carry the instance %s", len(containers), binding.SandboxUID)
	}
	container, err := p.API().InspectContainer(ctx, containers[0].ID)
	if err != nil || container == nil {
		return "", "", errors.Join(errors.New("the instance's container is gone"), err)
	}
	network, err := p.API().InspectNetwork(ctx, binding.SandboxBoundary)
	if err != nil || network == nil || !container.Attached(network.ID, binding.SandboxServiceName) {
		return "", "", errors.Join(errors.New("the instance's container is not on its network"), err)
	}
	process, err := container.PublishedAddress(workspace.SandboxdProcessPort)
	if err != nil {
		return "", "", err
	}
	filesystem, err := container.PublishedAddress(workspace.SandboxdFilesystemPort)
	return process, filesystem, err
}
