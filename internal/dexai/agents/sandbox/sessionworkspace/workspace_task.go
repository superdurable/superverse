// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sessionworkspace

import (
	"context"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
)

// A reclamation's wait for a provisioning's end reads the provisioning and
// its engine status in each round.
const (
	provisionPollInterval     = 5 * time.Second
	provisionPollRoundTimeout = 20 * time.Second
)

// workspaceTask is the input of every Step, so that each failure route takes
// the input of the Step that failed: the command the Flow runs, whether the
// reclaimed instance's provisioning may still run, the outcome to report and
// a failure a Step decided.
type workspaceTask struct {
	Command            WorkspaceCommand `json:"command"`
	AwaitsProvisioning bool             `json:"awaitsProvisioning,omitempty"`
	Outcome            *Outcome         `json:"outcome,omitempty"`
	Failure            *Failure         `json:"failure,omitempty"`
}

// ProvisionReport names the instance whose provisioning decided its outcome,
// which the Flow then reads from the provisioning.
type ProvisionReport struct {
	WorkspaceRequestID string `json:"workspaceRequestId"`
}

// commandAnswer answers a submitted command: Accepted when admitted or a
// repeat of an admitted one, Closed when refused because the Flow closes.
type commandAnswer struct {
	Accepted bool `json:"accepted"`
	Closed   bool `json:"closed,omitempty"`
}

// receipt is the completion output of the Flow.
type receipt struct {
	ProjectID string
}

// reclaimInstance waits for a provisioning that may still run, then reclaims
// the instance on the resources it recorded, asking the Preview to release it
// only while it holds it. It returns the resources without the Pod and the
// volume state; an error wrapping ErrRefused is a definite refusal.
func (flow *SessionWorkspaceFlow) reclaimInstance(ctx dex.Context, projectID string, instance Instance, task workspaceTask) (sandbox.Resources, string, error) {
	if task.AwaitsProvisioning {
		resources, err := flow.awaitProvisioningEnd(ctx, projectID, instance.InstanceID)
		if err != nil {
			return sandbox.Resources{}, "", err
		}
		if resources != (sandbox.Resources{}) {
			instance.Resources = resources
		}
	}
	var hooks reclamationHooks
	if task.Command.PreviewHolds != "" && task.Command.PreviewHolds == instance.InstanceID {
		hooks.releasePreview = func(call context.Context) (bool, error) {
			return flow.dependency.ReleasePreviewInstance(call, projectID, instance.InstanceID)
		}
	}
	reclamation := flow.reclaimer.begin(instance.sandboxInstance(projectID), instance.VolumeState, hooks)
	if err := dexflow.PollUntil(ctx, reclaimRoundInterval, reclaimRoundTimeout, reclamation.round); err != nil {
		return sandbox.Resources{}, "", err
	}
	resources, volumeState := reclamation.outcome()
	return resources, volumeState, nil
}

// awaitProvisioningEnd polls the provisioning of instanceID, whose outcome
// was never recorded, until it ended and returns the resources it recorded:
// what it created is reclaimed only once it can create nothing more.
func (flow *SessionWorkspaceFlow) awaitProvisioningEnd(ctx dex.Context, projectID, instanceID string) (sandbox.Resources, error) {
	var resources sandbox.Resources
	err := dexflow.PollUntil(ctx, provisionPollInterval, provisionPollRoundTimeout, func(call context.Context) (bool, error) {
		provisioning, err := flow.dependency.ObserveProvisioning(call, projectID, instanceID)
		if err != nil {
			return false, fmt.Errorf("read the provisioning of %s: %w", instanceID, err)
		}
		if provisioning != nil {
			resources = provisioning.Resources
		}
		return provisioning != nil, nil
	})
	return resources, err
}
