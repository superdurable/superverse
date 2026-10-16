// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deliverydao"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
)

// EventOutcome is how a Live event ended, RUNNING until it ends. A struct that
// string literals cannot convert to; its String is the name of the protobuf
// StartOutcome value, and the zero EventOutcome is none.
type EventOutcome struct{ name string }

var (
	EventRunning   = EventOutcome{"RUNNING"}
	EventReady     = EventOutcome{"READY"}
	EventFailed    = EventOutcome{"FAILED"}
	EventCancelled = EventOutcome{"CANCELLED"}
)

func (outcome EventOutcome) String() string { return outcome.name }

// LiveEvent is one entry of a Project's Live history as the read model holds
// it: the publishing of a build revision that deploys automatically
// (Published), or the deployment of an existing version that no build started.
type LiveEvent struct {
	ID                         string
	Published                  bool
	ReleaseID, BuildRevisionID string
	DeploymentID               string
	ConfigurationRevision      uint64
	// ActorAccountID is empty when the read model does not name the actor.
	ActorAccountID string
	Outcome        EventOutcome
	Failure        *dexflow.Failure
	Timings        []dexflow.PhaseTiming
	SourceCommit   string
	// PreviewSkipReason is why the builder skipped the Preview check, for a published event.
	PreviewSkipReason string
	// Version is the zero value for a Release prepared before versions were recorded.
	Version repohost.CommitVersion
	// RolledBack marks the deployment of an older Release than the Live Release it
	// replaced. Release creation times decide it, never commit order.
	RolledBack bool
	// Cancelled marks an entry a person cancelled: its build or its deployment.
	Cancelled  bool
	OccurredAt time.Time
}

// ListLiveEvents returns at most limit entries of the Project's Live history
// from the read model, newest first, after the page key when one is given, and
// the key of the page after them. It is a read of the Release and Deployment
// tables alone, with no Flow involved. The caller authorized the Project.
func ListLiveEvents(ctx context.Context, dao *deliverydao.DeliveryDAO, project string, after *postgres.PageKey, limit int) ([]LiveEvent, *postgres.PageKey, error) {
	rows, next, err := dao.ListLiveEvents(ctx, project, after, limit)
	if err != nil {
		return nil, nil, err
	}
	events := make([]LiveEvent, 0, len(rows))
	for _, row := range rows {
		event, decodeErr := liveEventOf(row)
		if decodeErr != nil {
			return nil, nil, decodeErr
		}
		events = append(events, event)
	}
	return events, next, nil
}

func liveEventOf(row deliverydao.LiveEventRow) (LiveEvent, error) {
	e := LiveEvent{ID: row.EventID, Published: row.Published, ReleaseID: row.ReleaseID, BuildRevisionID: row.BuildRevisionID, DeploymentID: row.DeploymentID.V,
		ConfigurationRevision: postgres.Unsigned(row.ConfigurationRevision), ActorAccountID: row.ActorAccountID.V, SourceCommit: row.SourceCommit.V, OccurredAt: row.OccurredAt}
	var identity dexflow.StartIdentity
	var validation *releases.PreviewValidation
	var buildFailure, autoDeployFailure, deploymentFailure *dexflow.Failure
	var buildTimings, deploymentTimings []dexflow.PhaseTiming
	err := errors.Join(
		postgres.DecodeJSON("start_identity", row.StartIdentity, &identity),
		postgres.DecodeJSON("failure", row.BuildFailure, &buildFailure),
		postgres.DecodeJSON("auto_deploy_failure", row.AutoDeployFailure, &autoDeployFailure),
		postgres.DecodeJSON("preview_validation", row.PreviewValidation, &validation),
		postgres.DecodeJSON("failure", row.DeploymentFailure, &deploymentFailure),
		postgres.DecodeJSON("timings", row.BuildTimings, &buildTimings),
		postgres.DecodeJSON("phase_timings", row.DeploymentTimings, &deploymentTimings),
		postgres.DecodeJSON("prepared_source.Version", row.Version, &e.Version),
	)
	if err != nil {
		return LiveEvent{}, fmt.Errorf("Live event %s: %w", row.EventID, err)
	}
	e.Cancelled = len(row.Cancellation) > 0
	if !row.Published {
		e.ActorAccountID = identity.ActorAccountID
		e.Outcome, e.Failure, e.Timings = deploymentOutcome(row.DeploymentStatus.V), deploymentFailure, deploymentTimings
		e.RolledBack = row.ReplacedReleaseCreatedAt.Valid && row.ReleaseCreatedAt.Valid && row.ReleaseCreatedAt.V.Before(row.ReplacedReleaseCreatedAt.V)
		return e, nil
	}
	e.Timings = buildTimings
	if validation != nil && validation.Kind == releases.PreviewSkipped {
		e.PreviewSkipReason = validation.SkipReason
	}
	switch {
	case row.DeploymentID.Valid && row.DeploymentStatus.Valid:
		e.Outcome, e.Failure, e.Timings = deploymentOutcome(row.DeploymentStatus.V), deploymentFailure, deploymentTimings
	case row.BuildStatus.V == releases.Cancelled || e.Cancelled:
		e.Outcome = EventCancelled
	case row.BuildStatus.V == string(releases.Failed) || buildFailure != nil || autoDeployFailure != nil:
		e.Outcome = EventFailed
	default:
		e.Outcome = EventRunning
	}
	if e.Failure == nil {
		e.Failure = buildFailure
	}
	if e.Failure == nil {
		e.Failure = autoDeployFailure
	}
	return e, nil
}

// deploymentOutcome is the outcome of a deployment in status. The cleanup
// states of a Project's deletion or a person's cancellation end a deployment
// that never served.
func deploymentOutcome(status Status) EventOutcome {
	switch status {
	case Ready:
		return EventReady
	case Failed:
		return EventFailed
	case Cancelling, Removed, Cancelled:
		return EventCancelled
	default:
		return EventRunning
	}
}
