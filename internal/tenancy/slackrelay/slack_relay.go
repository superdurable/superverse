// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package slackrelay

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/superdurable/superversev2/internal/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
)

// The platform receives its Slack application's Events API deliveries and
// relays each event to the applications whose platform Slack connections
// serve the workspace (docs/api/slack-relay.md): Slack's events endpoint,
// the applications' trigger registrations and the signed deliveries to the
// scopes' public routes. Events pass only through this process's memory;
// nothing logs a payload, a header value or a signature.

// The relay's HTTP paths: Slack's on the public listener, the registration
// on the credential read listener, the delivery's prefix on a scope's route.
const (
	eventsPath          = "/api/slack/events"
	RegistrationPath    = "/internal/slack-triggers/register"
	triggerPathPrefix   = "/triggers/slack/"
	databaseDeadline    = time.Second
	registrationLimit   = 8
	maxRegistrationBody = 4 << 10
)

// ScopeRoutes reaches the public route of a Project scope, the way the
// platform probes it: dialing base with host as the Host header.
type ScopeRoutes interface {
	PublicRoute(projectID, scope string) (base, host string)
}

// ScopeTokens reads a scope's token from the secret object that ref names.
type ScopeTokens interface {
	ReadScopeToken(ctx context.Context, projectID, scope string, ref json.RawMessage) (string, error)
}

// Config is what a Relay needs besides its dependencies.
type Config struct {
	// SigningSecret is the platform Slack application's Signing Secret.
	SigningSecret []byte
	// MaxEventBytes bounds Slack's request body and one queued event.
	MaxEventBytes int
	// DeliveryTimeout bounds one POST to an application.
	DeliveryTimeout time.Duration
}

// delivery is one queued event for one registered connection name.
type delivery struct {
	eventID, teamID                  string
	projectID, scope, connectionName string
	body                             []byte
	// The event's metadata for the recent events, never its text.
	receivedAt                         time.Time
	eventType, eventSubtype, channelID string
}

// Relay serves the events and registration endpoints and delivers the
// queued events.
type Relay struct {
	config Config
	dao    *connectiondao.ConnectionDAO
	queue  eventqueue.Queue
	routes ScopeRoutes
	tokens ScopeTokens
	logger *slog.Logger
	client *http.Client
	// tokens holds recently read scope tokens.
	cache  *tokenCache
	recent *recentEvents
}

// New returns a relay over dao and queue.
func New(config Config, dao *connectiondao.ConnectionDAO, queue eventqueue.Queue, routes ScopeRoutes, tokens ScopeTokens, logger *slog.Logger) *Relay {
	// A delivery never follows a redirect: the platform calls only the
	// scope's own route.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Relay{config: config, dao: dao, queue: queue, routes: routes, tokens: tokens, logger: logger, client: client, cache: newTokenCache(), recent: newRecentEvents(time.Now())}
}

// Run delivers queued events until ctx ends.
func (r *Relay) Run(ctx context.Context) {
	r.queue.Consume(ctx, func(ctx context.Context, value any) { r.deliver(ctx, value.(delivery)) })
}
