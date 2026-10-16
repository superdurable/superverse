// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package slackrelay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// Slack's request signature (https://docs.slack.dev/authentication/verifying-requests-from-slack).
const (
	slackTimestampHeader = "X-Slack-Request-Timestamp"
	slackSignatureHeader = "X-Slack-Signature"
	slackSignatureScheme = "v0"
	signatureWindow      = 5 * time.Minute
)

// eventType is the closed set of Slack payload and inner event types the
// relay distinguishes; every other inner type is relayed.
type eventType string

const (
	eventURLVerification eventType = "url_verification"
	eventCallback        eventType = "event_callback"
	eventAppUninstalled  eventType = "app_uninstalled"
	eventTokensRevoked   eventType = "tokens_revoked"
)

// dropReason is the closed set of reasons a log record gives for an event or
// delivery that the relay drops.
type dropReason string

const (
	dropBodyTooLarge     dropReason = "body_too_large"
	dropInvalidSignature dropReason = "invalid_signature"
	dropMalformed        dropReason = "malformed"
	dropUnknownType      dropReason = "unknown_type"
	dropNoRoute          dropReason = "no_route"
	dropRoutingFailed    dropReason = "routing_failed"
	dropEventTooLarge    dropReason = "event_too_large"
	dropQueueFull        dropReason = "queue_full"
	dropScopeQueueFull   dropReason = "scope_queue_full"
	dropUninstallFailed  dropReason = "uninstall_failed"
	dropTokenUnavailable dropReason = "token_unavailable"
	dropDeliveryFailed   dropReason = "delivery_failed"
)

// The last_failure codes of the connections an uninstall event marks.
const (
	failureSlackAppUninstalled = "SLACK_APP_UNINSTALLED"
	failureSlackTokensRevoked  = "SLACK_TOKENS_REVOKED"
)

// uninstallFailure is the last_failure an uninstall event of kind gives a
// connection; only the two uninstall types reach it.
func uninstallFailure(kind eventType) dexflow.Failure {
	switch kind {
	case eventAppUninstalled:
		return dexflow.Failure{Code: failureSlackAppUninstalled, Message: "Slack reported the platform application uninstalled from the workspace; authorize the connection again.", Remedy: dexflow.RemedyReauthorize}
	case eventTokensRevoked:
		return dexflow.Failure{Code: failureSlackTokensRevoked, Message: "Slack reported the platform application's bot token revoked in the workspace; authorize the connection again.", Remedy: dexflow.RemedyReauthorize}
	}
	panic("not an uninstall event: " + string(kind))
}

// queueDropReason names a refusal of the queue, which returns only its
// typed errors.
func queueDropReason(err error) dropReason {
	switch {
	case errors.Is(err, eventqueue.ErrTooLarge):
		return dropEventTooLarge
	case errors.Is(err, eventqueue.ErrKeyFull):
		return dropScopeQueueFull
	case errors.Is(err, eventqueue.ErrFull):
		return dropQueueFull
	}
	panic(err)
}

// authorization is one installation that an Events API callback concerns.
type authorization struct {
	TeamID       string  `json:"team_id"`
	EnterpriseID *string `json:"enterprise_id"`
}

// slackPayload is the part of a Slack request the relay reads; event stays
// the exact bytes Slack sent.
type slackPayload struct {
	Type           eventType       `json:"type"`
	Challenge      string          `json:"challenge"`
	EventID        string          `json:"event_id"`
	EventTime      int64           `json:"event_time"`
	TeamID         string          `json:"team_id"`
	EnterpriseID   *string         `json:"enterprise_id"`
	Event          json.RawMessage `json:"event"`
	Authorizations []authorization `json:"authorizations"`
}

// ServeEvents answers Slack's POST /api/slack/events. Every request answers
// 200 at once: Slack disables the platform application's events for every
// customer when too many deliveries fail, so each refusal drops the request
// and logs why.
func (r *Relay) ServeEvents(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != eventsPath {
		http.NotFound(response, request)
		return
	}
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, int64(r.config.MaxEventBytes)+1))
	switch {
	case err != nil:
		r.drop(request.Context(), dropMalformed)
		return
	case len(body) > r.config.MaxEventBytes:
		r.drop(request.Context(), dropBodyTooLarge)
		return
	case !r.signedBySlack(request.Header, body, time.Now()):
		r.drop(request.Context(), dropInvalidSignature)
		return
	}
	var payload slackPayload
	if json.Unmarshal(body, &payload) != nil {
		r.drop(request.Context(), dropMalformed)
		return
	}
	switch payload.Type {
	case eventURLVerification:
		response.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(response, payload.Challenge)
	case eventCallback:
		ctx, cancel := context.WithTimeout(request.Context(), databaseDeadline)
		defer cancel()
		r.receive(ctx, payload)
	default:
		r.drop(request.Context(), dropUnknownType, slog.String("event_id", payload.EventID))
	}
}

// signedBySlack verifies v0=HMAC-SHA256(secret, "v0:<timestamp>:<body>") in
// constant time, within the timestamp window around now.
func (r *Relay) signedBySlack(header http.Header, body []byte, now time.Time) bool {
	timestamp := header.Get(slackTimestampHeader)
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || now.Sub(time.Unix(seconds, 0)).Abs() > signatureWindow {
		return false
	}
	received, err := hex.DecodeString(strings.TrimPrefix(header.Get(slackSignatureHeader), slackSignatureScheme+"="))
	if err != nil || !strings.HasPrefix(header.Get(slackSignatureHeader), slackSignatureScheme+"=") {
		return false
	}
	mac := hmac.New(sha256.New, r.config.SigningSecret)
	_, _ = mac.Write([]byte(slackSignatureScheme + ":" + timestamp + ":"))
	_, _ = mac.Write(body)
	return hmac.Equal(received, mac.Sum(nil))
}

// receive handles a verified event callback: an uninstall marks the
// installation's connections, and every other event goes to the queue once
// per registered connection name of each routed scope.
func (r *Relay) receive(ctx context.Context, payload slackPayload) {
	var inner struct {
		Type eventType `json:"type"`
		// Tokens lists the user (oauth) and bot tokens a tokens_revoked event
		// revoked, by user ID.
		Tokens struct {
			Bot []string `json:"bot"`
		} `json:"tokens"`
	}
	if len(payload.Event) == 0 || json.Unmarshal(payload.Event, &inner) != nil || payload.EventID == "" {
		r.drop(ctx, dropMalformed, slog.String("event_id", payload.EventID))
		return
	}
	accounts := installations(payload)
	if inner.Type == eventTokensRevoked && len(inner.Tokens.Bot) == 0 {
		// A person revoked only their user token; the platform's bot token,
		// which the connections use, still works.
		r.logger.LogAttrs(ctx, slog.LevelInfo, "slack relay kept connections after a user token revocation",
			slog.String("event_id", payload.EventID), slog.String("team_id", payload.TeamID))
		return
	}
	if inner.Type == eventAppUninstalled || inner.Type == eventTokensRevoked {
		r.uninstall(ctx, payload.EventID, time.Unix(payload.EventTime, 0), inner.Type, accounts)
		return
	}
	body := relayedBody(payload)
	metadata := eventMetadataOf(payload.Event)
	var routes []connectiondao.TriggerRoute
	for _, account := range accounts {
		routed, err := r.dao.TriggerRoutes(ctx, connections.SlackConnectorID, account)
		if err != nil {
			r.drop(ctx, dropRoutingFailed, accountAttributes(payload.EventID, account)...)
			return
		}
		routes = append(routes, routed...)
	}
	slices.SortFunc(routes, compareRoutes)
	routes = slices.Compact(routes)
	if len(routes) == 0 {
		r.countUnrouted(ctx, payload.EventID, accounts)
		r.drop(ctx, dropNoRoute, slog.String("event_id", payload.EventID), slog.String("team_id", payload.TeamID))
		return
	}
	for _, route := range routes {
		item := delivery{eventID: payload.EventID, teamID: payload.TeamID, projectID: route.ProjectID, scope: route.Scope, connectionName: route.ConnectionName, body: body,
			receivedAt: time.Now(), eventType: metadata.eventType, eventSubtype: metadata.subtype, channelID: metadata.channel}
		if err := r.queue.Offer(eventqueue.Item{Key: scopeKey(route.ProjectID, route.Scope), Size: len(body), Value: item}); err != nil {
			reason := queueDropReason(err)
			fate := FateQueueFull
			if reason == dropEventTooLarge {
				fate = FateEventTooLarge
			}
			r.record(item, fate, 0, 0)
			r.drop(ctx, reason, item.attributes()...)
		}
	}
}

// eventMetadata is the only part of the inner event the recent events keep.
type eventMetadata struct{ eventType, subtype, channel string }

// eventMetadataOf reads the type, subtype and channel of an inner event with
// a struct that has no member for its text, users, files or blocks. The
// channel of some event types is an object, which leaves it empty.
func eventMetadataOf(event json.RawMessage) eventMetadata {
	var inner struct {
		Type    string          `json:"type"`
		Subtype string          `json:"subtype"`
		Channel json.RawMessage `json:"channel"`
	}
	_ = json.Unmarshal(event, &inner)
	var channel string
	_ = json.Unmarshal(inner.Channel, &channel)
	return eventMetadata{eventType: inner.Type, subtype: inner.Subtype, channel: channel}
}

// countUnrouted counts an event without a route for each active platform
// connection of its installations, the common reason a connection sees
// nothing. A failed lookup only loses the count.
func (r *Relay) countUnrouted(ctx context.Context, eventID string, accounts []connectiondao.ProviderAccount) {
	for _, account := range accounts {
		keys, err := r.dao.PlatformConnections(ctx, connections.SlackConnectorID, account)
		if err != nil {
			r.logger.LogAttrs(ctx, slog.LevelWarn, "slack relay could not count an unrouted event", slog.String("event_id", eventID), slog.String("error", comm.TailUTF8(err.Error(), 256)))
			continue
		}
		for _, key := range keys {
			r.recent.countUnrouted(key, eventID, time.Now())
		}
	}
}

// installations are the Slack installations an event concerns: its
// authorizations, or the outer team and enterprise of an event without any.
// An org-level installation has no team and routes nowhere.
func installations(payload slackPayload) []connectiondao.ProviderAccount {
	entries := payload.Authorizations
	if len(entries) == 0 {
		entries = []authorization{{TeamID: payload.TeamID, EnterpriseID: payload.EnterpriseID}}
	}
	var accounts []connectiondao.ProviderAccount
	for _, entry := range entries {
		if entry.TeamID == "" {
			continue
		}
		account := connectiondao.ProviderAccount{AccountID: entry.TeamID}
		if entry.EnterpriseID != nil && *entry.EnterpriseID != "" {
			account.OrganizationID = sql.Null[string]{V: *entry.EnterpriseID, Valid: true}
		}
		if !slices.Contains(accounts, account) {
			accounts = append(accounts, account)
		}
	}
	return accounts
}

// relayedBody is Slack's event_id, event_time and outer team_id with the
// exact bytes of its inner event.
func relayedBody(payload slackPayload) []byte {
	var body bytes.Buffer
	body.WriteString(`{"event_id":`)
	body.Write(comm.Must(json.Marshal(payload.EventID)))
	body.WriteString(`,"event_time":` + strconv.FormatInt(payload.EventTime, 10) + `,"team_id":`)
	body.Write(comm.Must(json.Marshal(payload.TeamID)))
	body.WriteString(`,"event":`)
	body.Write(payload.Event)
	body.WriteString(`}`)
	return body.Bytes()
}

// uninstall marks the platform Slack connections of each installation as
// needing reauthorization.
func (r *Relay) uninstall(ctx context.Context, eventID string, occurredAt time.Time, kind eventType, accounts []connectiondao.ProviderAccount) {
	failure := comm.Must(json.Marshal(uninstallFailure(kind)))
	for _, account := range accounts {
		marked, err := r.dao.RequireAccountReauthorization(ctx, connections.SlackConnectorID, account, occurredAt, failure)
		if err != nil {
			r.drop(ctx, dropUninstallFailed, accountAttributes(eventID, account)...)
			continue
		}
		r.logger.LogAttrs(ctx, slog.LevelInfo, "slack relay marked connections for reauthorization",
			append(accountAttributes(eventID, account), slog.String("event_type", string(kind)), slog.Int64("connections", marked))...)
	}
}

func compareRoutes(a, b connectiondao.TriggerRoute) int {
	return strings.Compare(a.ProjectID+"\x00"+a.Scope+"\x00"+a.ConnectionName, b.ProjectID+"\x00"+b.Scope+"\x00"+b.ConnectionName)
}

// drop logs a dropped request, event or delivery with its reason.
func (r *Relay) drop(ctx context.Context, reason dropReason, attributes ...slog.Attr) {
	r.logger.LogAttrs(ctx, slog.LevelWarn, "slack relay dropped an event", append([]slog.Attr{slog.String("reason", string(reason))}, attributes...)...)
}

func accountAttributes(eventID string, account connectiondao.ProviderAccount) []slog.Attr {
	return []slog.Attr{slog.String("event_id", eventID), slog.String("team_id", account.AccountID), slog.String("enterprise_id", account.OrganizationID.V)}
}

func (d delivery) attributes() []slog.Attr {
	return []slog.Attr{slog.String("event_id", d.eventID), slog.String("team_id", d.teamID), slog.String("project_id", d.projectID), slog.String("scope", d.scope)}
}
