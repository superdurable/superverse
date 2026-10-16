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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue/memory"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

func postSlackEvent(ctx context.Context, t *testing.T, server *httptest.Server, secret, body []byte) {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("v0:" + timestamp + ":"))
	_, _ = mac.Write(body)
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodPost, server.URL+eventsPath, bytes.NewReader(body)))
	request.Header.Set(slackTimestampHeader, timestamp)
	request.Header.Set(slackSignatureHeader, "v0="+hex.EncodeToString(mac.Sum(nil)))
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("the events endpoint answered %v (%v)", response, err)
	}
	_ = response.Body.Close()
}

func slackMessage(team, id string, inner map[string]any) []byte {
	return comm.Must(json.Marshal(map[string]any{"type": "event_callback", "event_id": id, "event_time": time.Now().Unix(), "team_id": team,
		"event": inner, "authorizations": []map[string]any{{"team_id": team, "enterprise_id": nil}}}))
}

// TestSlackRelayRecentEventsQueueFullAndUnroutedIntegration offers events to a
// relay that has no consumer and a one-event scope queue: the second event is
// recorded as dropped for want of room. An event of a workspace whose
// connection has no registration is counted for that connection, not listed.
func TestSlackRelayRecentEventsQueueFullAndUnroutedIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	dao := connectiondao.New(db)
	team := "T" + strings.ToUpper(flowid.RandomID(9))
	project := registeredScope(ctx, t, db, dao, team)
	unregisteredTeam := "T" + strings.ToUpper(flowid.RandomID(9))
	teamSpaceID, connectionID, _, _ := slackConnectionWithUsage(ctx, t, db, dao, unregisteredTeam)
	secret := []byte(flowid.RandomID(32))
	queue := memory.New(eventqueue.Limits{MaxItems: 1000, MaxBytes: 1 << 20, MaxItemBytes: 8 << 10, MaxKeyItems: 1, Concurrency: 1}, slog.New(&recordedDrops{}))
	relay := New(Config{SigningSecret: secret, MaxEventBytes: 16 << 10, DeliveryTimeout: time.Second}, dao, queue, nil, nil, slog.New(&recordedDrops{}))
	events := httptest.NewServer(http.HandlerFunc(relay.ServeEvents))
	t.Cleanup(events.Close)
	for _, id := range []string{"EvFirst", "EvSecond"} {
		postSlackEvent(ctx, t, events, secret, slackMessage(team, id, map[string]any{"type": "message", "channel": "C1"}))
	}
	entries := relay.RecentEvents(project, "preview").Events
	if len(entries) != 1 || entries[0].EventID != "EvSecond" || entries[0].Fate != FateQueueFull || entries[0].HTTPStatus != 0 ||
		entries[0].EventType != "message" || entries[0].ChannelID != "C1" || entries[0].TeamID != team || entries[0].ConnectionName != "support" {
		t.Fatalf("the refused event was not recorded as queue full: %+v", entries)
	}
	// Every string of an entry is cut to 128 bytes.
	postSlackEvent(ctx, t, events, secret, slackMessage(team, "EvLong", map[string]any{"type": "message", "channel": strings.Repeat("C", 1000)}))
	if long := relay.RecentEvents(project, "preview").Events[0]; long.EventID != "EvLong" || len(long.ChannelID) != maxMetadataBytes {
		t.Fatalf("the channel was not cut to %d bytes: %+v", maxMetadataBytes, long)
	}
	// An event over the per-event size limit is recorded as too large.
	postSlackEvent(ctx, t, events, secret, slackMessage(team, "EvHuge", map[string]any{"type": "message", "channel": "C1", "text": strings.Repeat("x", 9<<10)}))
	if huge := relay.RecentEvents(project, "preview").Events[0]; huge.EventID != "EvHuge" || huge.Fate != FateEventTooLarge {
		t.Fatalf("an oversized event was recorded as %+v", huge)
	}
	// The 51st entry evicts the oldest, newest first, and no entry holds text.
	for i := range recentEventsPerScope {
		postSlackEvent(ctx, t, events, secret, slackMessage(team, "EvMore"+strconv.Itoa(i), map[string]any{"type": "message", "channel": "C1", "text": "TOP-SECRET-MESSAGE-TEXT"}))
	}
	view := relay.RecentEvents(project, "preview")
	if len(view.Events) != recentEventsPerScope || view.Events[0].EventID != "EvMore"+strconv.Itoa(recentEventsPerScope-1) || view.BufferStartedAt.IsZero() {
		t.Fatalf("the ring did not keep the newest %d: %d entries, newest %s", recentEventsPerScope, len(view.Events), view.Events[0].EventID)
	}
	for _, entry := range view.Events {
		if entry.EventID == "EvSecond" {
			t.Fatal("the oldest entry was not evicted")
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", view), "TOP-SECRET-MESSAGE-TEXT") {
		t.Fatal("an entry holds the event's text")
	}
	if other := relay.RecentEvents(project, "live"); other.Events != nil {
		t.Fatalf("another scope reads this scope's entries: %+v", other)
	}

	postSlackEvent(ctx, t, events, secret, slackMessage(unregisteredTeam, "EvUnrouted", map[string]any{"type": "message", "channel": "C1"}))
	unrouted := relay.UnroutedEvents(teamSpaceID, connectionID)
	if unrouted.Count != 1 || unrouted.LastEventID != "EvUnrouted" || unrouted.LastEventAt.IsZero() {
		t.Fatalf("the event without a route was not counted for its connection: %+v", unrouted)
	}
	if other := relay.UnroutedEvents(teamSpaceID, "other"); other.Count != 0 {
		t.Fatalf("another connection was counted: %+v", other)
	}
}
