// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
	"github.com/superdurable/superversev2/internal/dexai/studio/runs"
)

// The Project diagnostics serve EvolveFlow's run export as its
// evolve.EvolveFlowDependency: the same Live target, connection pool and
// redaction as read_live_app_runs, for a Project the Flow owns. They also
// answer its decision notices, which no Session takes yet.

// InvokeEvolveLive runs read on the Project's current Live engine within one
// bounded Runs call.
func (d *ProjectDiagnostics) InvokeEvolveLive(ctx context.Context, projectID string, read func(context.Context, dexpb.FlowServiceClient) error) error {
	return evolveLiveError(d.Runs.InvokeLive(ctx, projectID, func(ctx context.Context, live runs.LiveEngine) error {
		return read(ctx, live.Flows)
	}))
}

// CurrentLive returns the Project's current Live deployment, with the checks
// of InvokeEvolveLive, as ProjectDeployFlow announces it to EvolveFlow: its
// Flow also answers the revision it replaced, which the read model lacks.
func (d *ProjectDiagnostics) CurrentLive(ctx context.Context, projectID string) (evolve.LiveRelease, error) {
	var release evolve.LiveRelease
	err := d.Runs.InvokeLive(ctx, projectID, func(ctx context.Context, live runs.LiveEngine) error {
		var err error
		release, err = d.Deployments.LiveRelease(ctx, live.Live)
		return err
	})
	return release, evolveLiveError(err)
}

// LiveRedaction returns the redaction of read_live_app_runs: the Live
// deployment's configured values and its application's environment.
func (d *ProjectDiagnostics) LiveRedaction(ctx context.Context, projectID string) (func(string) string, error) {
	var redaction redactor
	err := d.Runs.InvokeLive(ctx, projectID, func(ctx context.Context, live runs.LiveEngine) error {
		application, err := d.Serving.ReadApplication(ctx, live.Live.Deployment)
		if err != nil {
			return fmt.Errorf("read the Live application: %w", err)
		}
		redaction, err = d.liveRedaction(ctx, live, application)
		return err
	})
	if err != nil {
		return nil, evolveLiveError(err)
	}
	return redaction.text, nil
}

// LiveTimeline lists the Project's Live releases from the deployments read model.
func (d *ProjectDiagnostics) LiveTimeline(ctx context.Context, projectID string, until time.Time) ([]runexport.Release, error) {
	changes, err := d.Deployments.LiveTimeline(ctx, projectID, until)
	if err != nil {
		return nil, fmt.Errorf("read the Live timeline: %w", err)
	}
	timeline := make([]runexport.Release, 0, len(changes))
	for _, change := range changes {
		timeline = append(timeline, runexport.Release{Release: change.ReleaseID, BuildCommit: change.BuildCommit, LiveAt: change.CommittedAt})
	}
	return timeline, nil
}

// evolveLiveError classifies a Project without a ready Live deployment for EvolveFlow.
func evolveLiveError(err error) error {
	if errors.Is(err, runs.ErrLiveNotReady) {
		return fmt.Errorf("%w: %w", evolve.ErrLiveNotReady, err)
	}
	return err
}
