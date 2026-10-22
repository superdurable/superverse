// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"fmt"
	"slices"

	"golang.org/x/sync/semaphore"

	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

// ReviewAccess says which Projects get agent reviews after their measurement.
type ReviewAccess string

// The review access settings; measurement runs for every Project with valid goals under each.
const (
	ReviewAccessNone   ReviewAccess = "none"
	ReviewAccessListed ReviewAccess = "listed"
	ReviewAccessAll    ReviewAccess = "all"
)

// Validate refuses an undeclared review access.
func (access ReviewAccess) Validate() error {
	if !slices.Contains([]ReviewAccess{ReviewAccessNone, ReviewAccessListed, ReviewAccessAll}, access) {
		return fmt.Errorf("review access %q is none of %s, %s and %s", access, ReviewAccessNone, ReviewAccessListed, ReviewAccessAll)
	}
	return nil
}

// Settings are the server's evolve settings, which the composition root maps
// from the evolve.* keys of the server configuration.
type Settings struct {
	// MaxRunsPerExport caps the runs one export reads; the rest is truncated.
	MaxRunsPerExport int
	// MaxHistoryEventsPerRun caps the history events read per run.
	MaxHistoryEventsPerRun int
	// MaxReadsPerExport caps the Live engine reads of one export.
	MaxReadsPerExport int
	// MaxConcurrentExports bounds the exports one server replica runs at once.
	MaxConcurrentExports int
	// KeepExports is how many of a Project's newest exports stay stored.
	KeepExports int
	// ModelPrices are list prices keyed by the model ID a model connector reports.
	ModelPrices map[string]runexport.Price
	// PriceTableAsOf dates the price table; manifests carry it.
	PriceTableAsOf string
	// ReviewAccess and ReviewTeamSpaces decide where agent reviews run.
	ReviewAccess     ReviewAccess
	ReviewTeamSpaces []string
	// RetentionDays is how long the Live engine keeps closed runs, the window of a review.
	RetentionDays int
}

// ReviewsOn reports whether agent reviews run for a Project of teamSpaceID.
func (s Settings) ReviewsOn(teamSpaceID string) bool {
	switch s.ReviewAccess {
	case ReviewAccessAll:
		return true
	case ReviewAccessListed:
		return slices.Contains(s.ReviewTeamSpaces, teamSpaceID)
	case ReviewAccessNone:
	}
	return false
}

// EvolveFlowBootstrap assembles EvolveFlow with the replica's export slots and
// its one client. It holds no durable state and serves no request.
type EvolveFlowBootstrap struct {
	flow   *EvolveFlow
	client *EvolveFlowClient
}

// NewEvolveFlowBootstrap assembles the Flow; dependency serves the Live reads
// that EvolveFlow's level cannot import.
func NewEvolveFlowBootstrap(settings Settings, projectClient *projects.ProjectFlowClient, host repohost.Host, objects *objectstore.Store, dependency EvolveFlowDependency) *EvolveFlowBootstrap {
	flow := &EvolveFlow{
		settings: settings, projects: projectClient, host: host, objects: objects, dependency: dependency,
		exports: semaphore.NewWeighted(int64(settings.MaxConcurrentExports)),
	}
	return &EvolveFlowBootstrap{flow: flow, client: &EvolveFlowClient{flow: flow, projects: projectClient}}
}

// Flow returns the shared definition registered by the server bootstrap.
func (bootstrap *EvolveFlowBootstrap) Flow() *EvolveFlow { return bootstrap.flow }

// Client returns the Flow's one client, which Bind connects to Dex.
func (bootstrap *EvolveFlowBootstrap) Client() *EvolveFlowClient { return bootstrap.client }
