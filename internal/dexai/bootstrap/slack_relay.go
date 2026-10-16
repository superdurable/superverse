// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"log/slog"
	"net/url"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue/memory"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/slackrelay"
)

// newSlackRelay composes the Slack event relay with the configured queue,
// the scopes' public routes and their tokens in the configuration storage.
func newSlackRelay(config serverConfig, dao *connectiondao.ConnectionDAO, configuration *projectconfig.Configuration, logger *slog.Logger) *slackrelay.Relay {
	settings := config.slackRelay
	limits := eventqueue.Limits{
		MaxItems: settings.Queue.MaxEvents, MaxBytes: settings.Queue.MaxBytes, MaxItemBytes: settings.Queue.MaxEventBytes,
		MaxKeyItems: settings.Queue.MaxScopeEvents, Concurrency: settings.Delivery.Concurrency,
	}
	// Validate admits only the memory implementation.
	queue := memory.New(limits, logger)
	return slackrelay.New(slackrelay.Config{SigningSecret: config.slackSigningSecret, MaxEventBytes: settings.Queue.MaxEventBytes, DeliveryTimeout: settings.Delivery.Timeout},
		dao, queue, relayRoutes{routes: config.routes, native: config.docker != nil}, configuration, logger)
}

// relayRoutes reach a scope's public route as the platform probes it: the
// public URL on AWS; on Local Kind the gateway with the URL's host and port,
// as the Ingress probes send it; on Local Native the lane's gateway with the
// route's host, as its gateway's routes name it.
type relayRoutes struct {
	routes infra.PublicRoutes
	native bool
}

// PublicRoute is the base URL to dial and the Host header of a scope's
// route.
func (r relayRoutes) PublicRoute(projectID, scope string) (string, string) {
	kind, known := infra.ParseScopeKind(scope)
	if !known {
		panic("unknown scope " + scope)
	}
	public := r.routes.URL(kind, projectID)
	switch {
	case r.native:
		return r.routes.LocalGatewayURL, r.routes.Host(kind, projectID)
	case r.routes.Local:
		return r.routes.LocalGatewayURL, comm.Must(url.Parse(public)).Host
	}
	return public, comm.Must(url.Parse(public)).Host
}
