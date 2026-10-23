// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"context"
	"errors"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// EvolveFlowDependency is what EvolveFlow reads of a Project's Live
// deployment, and how it tells the Project's Session of a review or a decision, through
// packages above its level: the Project diagnostics implement it and the
// composition root injects them. Only runs of the Project's Live, published
// deployment count (owner rule 2026-10-22): only its current Live engine may
// serve these reads, and no scope or engine parameter may be added, so a
// Preview, test or other engine never feeds a goal value.
type EvolveFlowDependency interface {
	// InvokeEvolveLive runs read on a connection to the Project's current Live
	// engine, within one bounded call that also bounds read. A Project without
	// a ready Live deployment is ErrLiveNotReady.
	InvokeEvolveLive(ctx context.Context, projectID string, read func(context.Context, dexpb.FlowServiceClient) error) error
	// CurrentLive returns the Project's current Live deployment as its Live
	// announcement carries it, the revision it replaced included, with the same
	// checks as InvokeEvolveLive. A Project without a ready Live deployment is
	// ErrLiveNotReady.
	CurrentLive(ctx context.Context, projectID string) (LiveRelease, error)
	// LiveRedaction returns the redaction of every value the Project's Live
	// application can hold: its configured values and its environment. It may
	// be ErrLiveNotReady.
	LiveRedaction(ctx context.Context, projectID string) (func(string) string, error)
	// LiveTimeline lists the Project's Live releases committed until until,
	// oldest first, each with its build commit and its Live commit time.
	LiveTimeline(ctx context.Context, projectID string, until time.Time) ([]runexport.Release, error)
	// ReportEvolveNotification tells the Project's Session of a review that
	// waits for the agent, or of a person's decision on a proposal, once per
	// notice; the Session takes it only while its Agent waits for the user. A
	// Session that cannot take it at all is a *SessionSkip with the reason,
	// one whose queue is full ErrSessionBusy, which the caller asks again.
	ReportEvolveNotification(ctx context.Context, projectID string, notice EvolveNotice) error
}

// ErrLiveNotReady reports a Project without a ready Live deployment.
var ErrLiveNotReady = errors.New("the Project has no ready Live deployment")

// ErrSessionBusy reports a Session that cannot take a notification now, its
// queue being full; the delivery is asked again.
var ErrSessionBusy = errors.New("the Session's notification queue is full")

// SessionSkip reports a Session that cannot take an agent review or a
// decision at all: it is not active, it runs a local agent, or its Project
// has no model key (the mock model is exempt). A review it skips ends
// skipped with Reason.
type SessionSkip struct{ Reason ReviewReason }

func (skip *SessionSkip) Error() string {
	return "the Session cannot take an agent review: " + string(skip.Reason)
}
