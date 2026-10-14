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

	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/platform/dockerapi"
	infradocker "github.com/superdurable/superversev2/internal/projects/infra/docker"
)

// DeletePod stops and removes the recorded instance's container, with its
// anonymous scratch volume, and reports whether it is gone; only its absence
// frees the workspace volumes for the next instance. As a Pod's preStop hook
// and grace period, the stop first asks the Preview supervisor to shut down,
// then sends the stop signal and kills the container after the profile's grace
// period. The engine completes a stop it started, so a round cut off by its
// timeout finds the container stopped.
func (p *InstanceProvider) DeletePod(ctx context.Context, s sandbox.Instance) (bool, error) {
	if s.Resources.Instance.UID == "" {
		return true, nil
	}
	name := p.containerName(s.Identity)
	container, err := p.OwnedContainer(ctx, name, infradocker.WithLabel(p.owner(s.Identity), infradocker.InstanceUIDLabel, s.Resources.Instance.UID))
	if err != nil || container == nil {
		return container == nil && err == nil, err
	}
	if container.State.Running {
		profile, profileErr := readProfile(p.ProfilePath, p.ProfileName)
		if profileErr != nil {
			return false, profileErr
		}
		if len(profile.preStop) > 0 {
			// A failed hook does not stop the deletion, as in Kubernetes.
			_, _ = p.API().Exec(ctx, container.ID, profile.preStop)
		}
		if err = p.API().StopContainer(ctx, container.ID, profile.grace); err != nil {
			return false, err
		}
	}
	if err = p.API().RemoveContainer(ctx, container.ID); dockerapi.IsConflict(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	remaining, err := p.API().InspectContainer(ctx, container.ID)
	return remaining == nil, err
}

// DeleteVolume deletes the recorded workspace volumes, each only while it
// carries the recorded workspace UID.
func (p *InstanceProvider) DeleteVolume(ctx context.Context, s sandbox.Instance) (bool, error) {
	if s.Resources.Volume.UID == "" {
		return true, nil
	}
	labels := infradocker.WithLabel(p.owner(s.Identity), infradocker.WorkspaceUIDLabel, s.Resources.Volume.UID)
	for _, name := range p.workspaceVolumes(s.Identity) {
		if gone, err := p.RemoveVolume(ctx, name, labels); err != nil || !gone {
			return false, err
		}
	}
	return true, nil
}

// DeleteNamespace deletes the recorded Sandbox network. A network that a
// container still uses is not gone yet.
func (p *InstanceProvider) DeleteNamespace(ctx context.Context, s sandbox.Instance) (bool, error) {
	if s.Resources.Boundary.UID == "" {
		return true, nil
	}
	return p.RemoveNetwork(ctx, p.network(s.Identity), s.Resources.Boundary.UID, p.owner(s.Identity))
}
