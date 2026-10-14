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
	"fmt"
	"strings"

	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/platform/dockerapi"
	projectconfig "github.com/superdurable/superversev2/internal/projects/config"
)

// liveLogReadBytes bounds the log lines one read keeps.
const liveLogReadBytes = 256 << 10

// liveApplication is one read of a Live application container at the ID its
// deployment recorded, which the Agent's diagnostics take the environment to
// redact and the log from.
type liveApplication struct {
	serving    *LiveServing
	deployment deployments.Snapshot
	container  dockerapi.Container
}

// ReadApplication inspects the application container once at the ID and
// labels the deployment recorded; another container is
// deployments.ErrApplicationChanged. Its environment and log come from that
// container.
func (l *LiveServing) ReadApplication(ctx context.Context, deployment deployments.Snapshot) (deployments.LiveApplication, error) {
	container, err := l.application(ctx, deployment)
	if err != nil {
		return nil, err
	}
	return &liveApplication{serving: l, deployment: deployment, container: *container}, nil
}

// Environment returns every value of the application's environment beyond its
// fixed settings, the grant among them, and every key pair the server
// generated for the project, each when it is long enough to be redacted.
func (a *liveApplication) Environment(context.Context) ([]string, error) {
	fixed := map[string]bool{}
	for _, setting := range deployments.ApplicationSettings(a.deployment, "", "") {
		fixed[setting[0]] = true
	}
	var values []string
	for _, variable := range a.container.Config.Env {
		name, value, _ := strings.Cut(variable, "=")
		if !fixed[name] && len(value) >= projectconfig.MinimumRedactedValueBytes {
			values = append(values, value)
		}
	}
	generated, err := a.serving.CredentialValues(a.deployment.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read the Live application environment: %w", err)
	}
	for _, value := range generated {
		if len(value) >= projectconfig.MinimumRedactedValueBytes {
			values = append(values, value)
		}
	}
	return values, nil
}

// Log reads the last lines of the application container's log, whole lines
// only and not yet redacted, with its state, which explains an empty or
// restarting log.
func (a *liveApplication) Log(ctx context.Context, tailLines int) (deployments.ApplicationLog, error) {
	log := deployments.ApplicationLog{State: containerState(a.container.State), Restarts: int64(a.container.RestartCount)}
	text, err := a.serving.API().ContainerLogs(ctx, a.container.ID, tailLines, liveLogReadBytes)
	if err != nil {
		return deployments.ApplicationLog{}, fmt.Errorf("read the Live application log: %w", err)
	}
	if len(text) == liveLogReadBytes {
		// The read stopped inside a line, which may hold a value redaction
		// would not recognize in part.
		text = text[strings.IndexByte(text, '\n')+1:]
	}
	log.Log = text
	return log, nil
}

// containerState describes a container's state: running, or waiting or
// terminated with its reason and exit code.
func containerState(state dockerapi.ContainerState) string {
	switch {
	case state.Running && !state.Restarting:
		return "running"
	case state.Restarting:
		return fmt.Sprintf("waiting: restarting (exit code %d)", state.ExitCode)
	case state.Status == dockerapi.StateCreated:
		return "waiting: created"
	case state.OOMKilled:
		return fmt.Sprintf("terminated: OOMKilled (exit code %d)", state.ExitCode)
	default:
		return fmt.Sprintf("terminated: %s (exit code %d)", state.Status, state.ExitCode)
	}
}
