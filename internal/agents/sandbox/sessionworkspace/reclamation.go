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
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

const (
	// reclaimRoundInterval spaces the rounds of a reclamation;
	// reclaimRoundTimeout bounds one round, which makes several Kubernetes,
	// sandboxd and Flow calls, of which a Flow command retries a lock conflict
	// for up to ten seconds.
	reclaimRoundInterval = 2 * time.Second
	reclaimRoundTimeout  = 40 * time.Second
	// reclaimBudget is the wait limit of a whole reclamation: the wait for a
	// provisioning that may still run, a Preview's release, the engine stop,
	// the inspection and the Pod deletion.
	reclaimBudget = 15 * time.Minute
	// engineStopGrace is how long a graceful stop of the Preview engine may
	// take before the Pod deletion stops it: the supervisor gives each of its
	// three process groups ten seconds before it kills them. It is the business
	// rule of a forced stop, not the wait limit of the reclamation.
	engineStopGrace = 3 * time.Minute
)

// ErrRefused marks a reclamation that asking again cannot finish, such as a
// Sandbox object of another owner or UID; it wraps the cause.
var ErrRefused = errors.New("the Sandbox instance cannot be reclaimed")

// reclaimer reclaims the Sandbox instances of a Project's Session for the
// SessionWorkspaceFlow, which owns them: it deletes an instance's Pod and
// keeps its namespace and workspace volume. It holds no state; one
// reclamationProgress is the progress of one attempt.
type reclaimer struct {
	provider  sandbox.InstanceProvider
	workspace *workspace.Capabilities
}

// newReclaimer is the reclaimer of the instances provider runs, whose
// workspace and Preview engine it reaches through workspaces.
func newReclaimer(provider sandbox.InstanceProvider, workspaces *workspace.Capabilities) *reclaimer {
	return &reclaimer{provider: provider, workspace: workspaces}
}

// reclamationHooks are the cross-Flow parts of a reclamation, which only the Session
// Service reaches. releasePreview asks the Project's Preview to stop using the
// instance and reports whether it no longer does; it is nil when no Preview
// holds the instance.
type reclamationHooks struct {
	releasePreview func(context.Context) (bool, error)
}

// stage is how far one attempt of a reclamation got.
type stage int

const (
	releasePreview stage = iota
	discover
	stopEngine
	inspect
	deletePod
	reclaimed
)

// reclamationProgress is one attempt to reclaim an instance whose provisioning ended,
// in the rounds of a polling Step: it has the Preview release the instance,
// finds what the instance holds no reference to, stops the Preview engine that
// the Agent or the Preview started, records what the workspace holds and
// deletes the Pod. Every round repeats only what an earlier round of the
// attempt did not finish, and a new attempt starts over: each part queries
// before it changes anything.
type reclamationProgress struct {
	reclaimer       *reclaimer
	instance        sandbox.Instance
	hooks           reclamationHooks
	stage           stage
	engineStopAsked time.Time
	volumeState     string
}

// begin starts the reclamation of instance, whose resources are those the
// workspace Flow or its ended provisioning recorded.
func (r *reclaimer) begin(instance sandbox.Instance, volumeState string, hooks reclamationHooks) *reclamationProgress {
	return &reclamationProgress{reclaimer: r, instance: instance, hooks: hooks, volumeState: volumeState}
}

// round is one round of the reclamation: done once the Pod and every Pod of
// its Sandbox are gone. An error wrapping ErrRefused is a definite refusal;
// any other error ends the attempt, and the Step's retry policy asks again.
func (r *reclamationProgress) round(ctx context.Context) (bool, error) {
	for r.stage != reclaimed {
		done, err := r.advance(ctx)
		if err != nil || !done {
			return false, err
		}
		r.stage++
	}
	return true, nil
}

// advance runs the current stage and reports whether it finished.
func (r *reclamationProgress) advance(ctx context.Context) (bool, error) {
	switch r.stage {
	case releasePreview:
		if r.hooks.releasePreview == nil {
			return true, nil
		}
		released, err := r.hooks.releasePreview(ctx)
		if err != nil {
			return false, fmt.Errorf("release the instance %s from the Preview: %w", r.instance.WorkspaceRequestID, err)
		}
		return released, nil
	case discover:
		return r.discover(ctx)
	case stopEngine:
		return r.stopEngine(ctx)
	case inspect:
		r.inspect(ctx)
		return true, nil
	case deletePod:
		deleted, err := r.reclaimer.provider.DeletePod(ctx, r.instance)
		if errors.Is(err, infra.ErrOwnershipConflict) {
			return false, fmt.Errorf("%w: delete the Sandbox %s: %w", ErrRefused, r.instance.Resources.Instance.Name, err)
		}
		if err != nil {
			return false, fmt.Errorf("delete the Sandbox %s: %w", r.instance.Resources.Instance.Name, err)
		}
		return deleted, nil
	default:
		// round never advances a finished reclamation.
		panic(fmt.Sprintf("advance of the finished reclamation stage %d", r.stage))
	}
}

// discover completes the references of an instance whose provisioning ended
// before it recorded them, from the deterministic names of its namespace,
// volume and Sandbox; an object of another owner is refused.
func (r *reclamationProgress) discover(ctx context.Context) (bool, error) {
	resources := r.instance.Resources
	if resources.Boundary.UID != "" && resources.Volume.UID != "" && resources.Instance.UID != "" {
		return true, nil
	}
	found, err := r.reclaimer.provider.DiscoverResources(ctx, r.instance)
	if errors.Is(err, infra.ErrOwnershipConflict) {
		return false, fmt.Errorf("%w: discover the resources of %s: %w", ErrRefused, r.instance.WorkspaceRequestID, err)
	}
	if err != nil {
		return false, fmt.Errorf("discover the resources of %s: %w", r.instance.WorkspaceRequestID, err)
	}
	r.instance.Resources = found
	return true, nil
}

// stopEngine stops the Sandbox-local Preview engine, which the Agent may have
// started without a Preview, so that the Pod deletion finds its state on the
// volume at rest. An unreachable supervisor, and one still stopping after the
// grace, leave the stop to the Pod deletion, whose preStop hook asks the
// supervisor again within the termination grace period.
func (r *reclamationProgress) stopEngine(ctx context.Context) (bool, error) {
	if r.instance.Resources.Instance.UID == "" || r.instance.Resources.ServiceName == "" {
		// A Sandbox that never published its Service ran no engine.
		return true, nil
	}
	if r.engineStopAsked.IsZero() {
		r.engineStopAsked = time.Now()
	}
	answer, err := r.reclaimer.workspace.StopPreviewEnvironment(ctx, r.instance.Binding(), "environment/"+r.instance.WorkspaceRequestID+"/stop")
	switch {
	case errors.Is(err, workspace.ErrUnreachable):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("stop the Preview environment: %w", err)
	}
	return answer.State == workspace.PreviewStateStopped || time.Since(r.engineStopAsked) > engineStopGrace, nil
}

// inspect records whether the kept volume holds uncommitted changes. The
// volume outlives the Pod either way, so an inspection that fails records
// UNKNOWN instead of failing the reclamation.
func (r *reclamationProgress) inspect(ctx context.Context) {
	if r.instance.Resources.Instance.UID == "" || r.instance.Resources.ServiceName == "" || r.volumeState == sandbox.WorkspaceAbsent {
		return
	}
	dirty, _, err := r.reclaimer.workspace.Inspect(ctx, r.instance.Binding())
	switch {
	case err != nil:
		r.volumeState = sandbox.WorkspaceUnknown
	case dirty:
		r.volumeState = sandbox.WorkspaceDirty
	default:
		r.volumeState = sandbox.WorkspaceClean
	}
}

// outcome is what the finished reclamation leaves: the resources without the
// Pod and the volume state it found.
func (r *reclamationProgress) outcome() (resources sandbox.Resources, volumeState string) {
	return r.instance.Resources.WithoutPod(), r.volumeState
}
