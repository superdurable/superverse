// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	infradocker "github.com/superdurable/superversev2/internal/projects/infra/docker"
)

// codingProfile is the container of the Sandbox Pod template that the
// rendered coding profile declares, as a Docker container runs it. Kind and
// Docker read the same rendering of the chart, so both run the same image,
// command, environment, ports, user, limits and mounts.
type codingProfile struct {
	image            string
	entrypoint, cmd  []string
	env              []string
	podUIDVariable   string
	ports            []int
	preStop          []string
	grace            time.Duration
	user             string
	nanoCPUs, memory int64
	// workspace maps each subPath of the workspace volume to its mount path,
	// and scratch lists the mount paths of the Pod's emptyDir volumes.
	workspace map[string]string
	scratch   []string
}

// podTemplate is the part of the profile's Sandbox that a Docker Sandbox
// runs.
type podTemplate struct {
	Spec struct {
		PodTemplate struct {
			Spec struct {
				TerminationGracePeriodSeconds int `json:"terminationGracePeriodSeconds"`
				SecurityContext               struct {
					RunAsUser  int64 `json:"runAsUser"`
					RunAsGroup int64 `json:"runAsGroup"`
				} `json:"securityContext"`
				Containers []struct {
					Image   string   `json:"image"`
					Command []string `json:"command"`
					Args    []string `json:"args"`
					Env     []struct {
						Name      string `json:"name"`
						Value     string `json:"value"`
						ValueFrom *struct {
							FieldRef *struct {
								FieldPath string `json:"fieldPath"`
							} `json:"fieldRef"`
						} `json:"valueFrom"`
					} `json:"env"`
					Ports []struct {
						ContainerPort int `json:"containerPort"`
					} `json:"ports"`
					Lifecycle struct {
						PreStop struct {
							Exec struct {
								Command []string `json:"command"`
							} `json:"exec"`
						} `json:"preStop"`
					} `json:"lifecycle"`
					SecurityContext struct {
						ReadOnlyRootFilesystem bool `json:"readOnlyRootFilesystem"`
						Capabilities           struct {
							Drop []string `json:"drop"`
						} `json:"capabilities"`
					} `json:"securityContext"`
					Resources struct {
						Limits map[string]string `json:"limits"`
					} `json:"resources"`
					VolumeMounts []struct {
						Name      string `json:"name"`
						MountPath string `json:"mountPath"`
						SubPath   string `json:"subPath"`
					} `json:"volumeMounts"`
				} `json:"containers"`
				Volumes []struct {
					Name                  string    `json:"name"`
					PersistentVolumeClaim *struct{} `json:"persistentVolumeClaim"`
					EmptyDir              *struct{} `json:"emptyDir"`
				} `json:"volumes"`
			} `json:"spec"`
		} `json:"podTemplate"`
	} `json:"spec"`
}

// readProfile reads the rendered coding profile name from path and verifies
// it as Kubernetes does: an immutable ConfigMap of that name whose contract
// this platform implements. A setting the Docker runtime cannot honor is an
// error, never dropped.
func readProfile(path, name string) (codingProfile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the lane's rendered profile, from the server configuration
	if err != nil {
		return codingProfile{}, fmt.Errorf("read the coding profile %s: %w", name, err)
	}
	var document struct {
		Kind      string `json:"kind"`
		Immutable bool   `json:"immutable"`
		Metadata  struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Data map[string]string `json:"data"`
	}
	if err = yaml.Unmarshal(data, &document); err != nil {
		return codingProfile{}, fmt.Errorf("decode the coding profile %s: %w", name, err)
	}
	if document.Kind != "ConfigMap" || document.Metadata.Name != name || !document.Immutable {
		return codingProfile{}, fmt.Errorf("the coding profile file of %s is not its immutable ConfigMap", name)
	}
	if err = sandbox.VerifyCodingProfile(name, document.Data); err != nil {
		return codingProfile{}, err
	}
	var template podTemplate
	if err = yaml.Unmarshal([]byte(document.Data["sandbox.yaml"]), &template); err != nil {
		return codingProfile{}, fmt.Errorf("decode the Sandbox of coding profile %s: %w", name, err)
	}
	return template.profile(name)
}

// profile converts the Pod template's single container.
func (t podTemplate) profile(name string) (codingProfile, error) {
	pod := t.Spec.PodTemplate.Spec
	if len(pod.Containers) != 1 {
		return codingProfile{}, fmt.Errorf("coding profile %s declares %d containers, not 1", name, len(pod.Containers))
	}
	container := pod.Containers[0]
	security := container.SecurityContext
	if !security.ReadOnlyRootFilesystem || !slices.Equal(security.Capabilities.Drop, []string{"ALL"}) || pod.SecurityContext.RunAsUser == 0 {
		return codingProfile{}, fmt.Errorf("coding profile %s does not run read-only, without capabilities and as a user", name)
	}
	profile := codingProfile{image: container.Image, entrypoint: container.Command, cmd: container.Args, preStop: container.Lifecycle.PreStop.Exec.Command,
		grace: time.Duration(pod.TerminationGracePeriodSeconds) * time.Second, workspace: map[string]string{},
		user: strconv.FormatInt(pod.SecurityContext.RunAsUser, 10) + ":" + strconv.FormatInt(pod.SecurityContext.RunAsGroup, 10)}
	for _, variable := range container.Env {
		switch {
		case variable.ValueFrom == nil:
			profile.env = append(profile.env, variable.Name+"="+variable.Value)
		case variable.ValueFrom.FieldRef != nil && variable.ValueFrom.FieldRef.FieldPath == "metadata.uid":
			// A Pod's UID is the instance's in Docker.
			profile.podUIDVariable = variable.Name
		default:
			return codingProfile{}, fmt.Errorf("coding profile %s takes %s from a source Docker has no counterpart of", name, variable.Name)
		}
	}
	for _, port := range container.Ports {
		profile.ports = append(profile.ports, port.ContainerPort)
	}
	// The Preview application listens beside sandboxd; its route reaches it on
	// the Pod's own address in Kubernetes and on a published port here.
	profile.ports = append(profile.ports, workspace.PreviewApplicationPort)
	var err error
	if profile.nanoCPUs, err = cpuNanos(container.Resources.Limits["cpu"]); err != nil {
		return codingProfile{}, fmt.Errorf("coding profile %s: %w", name, err)
	}
	if profile.memory, err = memoryBytes(container.Resources.Limits["memory"]); err != nil {
		return codingProfile{}, fmt.Errorf("coding profile %s: %w", name, err)
	}
	volumes := map[string]bool{}
	for _, volume := range pod.Volumes {
		switch {
		case volume.PersistentVolumeClaim != nil:
			volumes[volume.Name] = true
		case volume.EmptyDir != nil:
			volumes[volume.Name] = false
		}
	}
	for _, mount := range container.VolumeMounts {
		persistent, declared := volumes[mount.Name]
		switch {
		case !declared:
			return codingProfile{}, fmt.Errorf("coding profile %s mounts %s, which Docker has no counterpart of", name, mount.Name)
		case persistent && mount.SubPath != "":
			profile.workspace[mount.SubPath] = mount.MountPath
		case persistent:
			return codingProfile{}, fmt.Errorf("coding profile %s mounts its workspace without a subPath", name)
		default:
			profile.scratch = append(profile.scratch, mount.MountPath)
		}
	}
	if profile.image == "" || profile.podUIDVariable == "" || profile.grace <= 0 {
		return codingProfile{}, errors.New("coding profile " + name + " lacks its image, Pod UID variable or grace period")
	}
	// Docker keeps each subPath of the workspace in a volume of its own.
	if len(profile.workspace) != len(infradocker.WorkspaceSubPaths) {
		return codingProfile{}, fmt.Errorf("coding profile %s mounts %d workspace subPaths, not %v", name, len(profile.workspace), infradocker.WorkspaceSubPaths)
	}
	for _, subPath := range infradocker.WorkspaceSubPaths {
		if profile.workspace[subPath] == "" {
			return codingProfile{}, fmt.Errorf("coding profile %s does not mount the workspace subPath %s", name, subPath)
		}
	}
	return profile, nil
}

// cpuNanos converts a Kubernetes CPU quantity, such as 2 or 500m, into
// billionths of a CPU.
func cpuNanos(quantity string) (int64, error) {
	if milli, ok := strings.CutSuffix(quantity, "m"); ok {
		value, err := strconv.ParseInt(milli, 10, 64)
		return value * 1e6, err
	}
	value, err := strconv.ParseInt(quantity, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unsupported CPU limit %q", quantity)
	}
	return value * 1e9, nil
}

// memoryBytes converts a Kubernetes memory quantity with a binary suffix,
// such as 4Gi, into bytes.
func memoryBytes(quantity string) (int64, error) {
	for suffix, shift := range map[string]uint{"Ki": 10, "Mi": 20, "Gi": 30} {
		if number, ok := strings.CutSuffix(quantity, suffix); ok {
			value, err := strconv.ParseInt(number, 10, 64)
			return value << shift, err
		}
	}
	return 0, fmt.Errorf("unsupported memory limit %q", quantity)
}
