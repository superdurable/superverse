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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue/memory"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// recordedDrops captures the relay's drop records by reason and scope.
type recordedDrops struct {
	mutex   sync.Mutex
	records []map[string]string
}

func (r *recordedDrops) Enabled(context.Context, slog.Level) bool { return true }
func (r *recordedDrops) WithAttrs([]slog.Attr) slog.Handler       { return r }
func (r *recordedDrops) WithGroup(string) slog.Handler            { return r }
func (r *recordedDrops) Handle(_ context.Context, record slog.Record) error {
	values := map[string]string{"message": record.Message}
	record.Attrs(func(attribute slog.Attr) bool { values[attribute.Key] = attribute.Value.String(); return true })
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.records = append(r.records, values)
	return nil
}

func (r *recordedDrops) count(reason dropReason, projectID string) int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	count := 0
	for _, record := range r.records {
		if record["reason"] == string(reason) && (projectID == "" || record["project_id"] == projectID) {
			count++
		}
	}
	return count
}

// TestSlackRelayQueueLimitsIntegration serves the events endpoint over the
// lane's PostgreSQL with the memory queue under small limits and no consumer:
// two scopes registered for one workspace each get every event while their
// scope queue has room; a scope at its limit drops the next event while the
// other scope still takes it; the total limit then drops events of every
// scope; an event over the per-event size is dropped. Every drop answers 200
// and logs its reason with the scope. An unknown workspace drops as a routing
// miss, a stale timestamp as an invalid signature.
func TestSlackRelayQueueLimitsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	dao := connectiondao.New(db)
	team := "T" + strings.ToUpper(flowid.RandomID(9))
	first, second := registeredScope(ctx, t, db, dao, team), registeredScope(ctx, t, db, dao, team)

	drops := &recordedDrops{}
	secret := []byte(flowid.RandomID(32))
	queue := memory.New(eventqueue.Limits{MaxItems: 5, MaxBytes: 1 << 20, MaxItemBytes: 8 << 10, MaxKeyItems: 2, Concurrency: 1}, slog.New(drops))
	relay := New(Config{SigningSecret: secret, MaxEventBytes: 16 << 10, DeliveryTimeout: time.Second}, dao, queue, nil, nil, slog.New(drops))
	server := httptest.NewServer(http.HandlerFunc(relay.ServeEvents))
	t.Cleanup(server.Close)
	post := func(body []byte, at time.Time) {
		t.Helper()
		timestamp := strconv.FormatInt(at.Unix(), 10)
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
	message := func(team, text string) []byte {
		return comm.Must(json.Marshal(map[string]any{"type": "event_callback", "event_id": "Ev" + flowid.RandomID(10), "event_time": time.Now().Unix(), "team_id": team,
			"event":          map[string]any{"type": "message", "channel": "C1", "text": text, "ts": "1760000000.000100"},
			"authorizations": []map[string]any{{"team_id": team, "enterprise_id": nil}}}))
	}

	// Two events fill both scope queues: four items of five.
	post(message(team, "one"), time.Now())
	post(message(team, "two"), time.Now())
	if drops.count(dropScopeQueueFull, "") != 0 || drops.count(dropQueueFull, "") != 0 {
		t.Fatalf("events within the limits were dropped: %+v", drops.records)
	}
	// The third finds both scope queues full.
	post(message(team, "three"), time.Now())
	if drops.count(dropScopeQueueFull, first) != 1 || drops.count(dropScopeQueueFull, second) != 1 {
		t.Fatalf("a full scope queue did not drop the third event: %+v", drops.records)
	}
	// A third scope still takes one, the fifth item; then the total refuses
	// every scope's next event.
	third := registeredScope(ctx, t, db, dao, team)
	post(message(team, "four"), time.Now())
	if drops.count(dropQueueFull, third) != 0 || drops.count(dropScopeQueueFull, third) != 0 {
		t.Fatalf("the third scope's first event was dropped: %+v", drops.records)
	}
	post(message(team, "five"), time.Now())
	if drops.count(dropQueueFull, third) != 1 {
		t.Fatalf("the full queue did not drop the third scope's next event: %+v", drops.records)
	}
	post(message(team, strings.Repeat("x", 9<<10)), time.Now())
	if drops.count(dropEventTooLarge, "") == 0 {
		t.Fatalf("an event over the per-event limit was queued: %+v", drops.records)
	}
	post(message(team, strings.Repeat("x", 17<<10)), time.Now())
	if drops.count(dropBodyTooLarge, "") != 1 {
		t.Fatalf("a body over the endpoint's limit was read: %+v", drops.records)
	}
	post(message("T"+strings.ToUpper(flowid.RandomID(9)), "unknown"), time.Now())
	if drops.count(dropNoRoute, "") != 1 {
		t.Fatalf("an unknown workspace was not a routing miss: %+v", drops.records)
	}
	post(message(team, "stale"), time.Now().Add(-6*time.Minute))
	if drops.count(dropInvalidSignature, "") != 1 {
		t.Fatalf("a stale timestamp was not refused: %+v", drops.records)
	}
}

// registeredScope records a platform Slack connection of team in a new
// TeamSpace, a scope token and a usage of a new Project's Preview, and its
// registration of one connection name, and deletes them when the test ends.
func registeredScope(ctx context.Context, t *testing.T, db *sql.DB, dao *connectiondao.ConnectionDAO, team string) string {
	t.Helper()
	_, connectionID, project, token := slackConnectionWithUsage(ctx, t, db, dao, team)
	if err := dao.RegisterTrigger(ctx, connectiondao.TriggerRegistration{TokenHash: connections.ScopeTokenHash(token), ConnectionID: connectionID,
		ConnectionName: "support", ConnectorID: connections.SlackConnectorID, Limit: registrationLimit}); err != nil {
		t.Fatal(err)
	}
	return project
}

// slackConnectionWithUsage records a platform Slack connection of team in a
// new TeamSpace, a scope token and a usage of a new Project's Preview, without
// a registration, and deletes them when the test ends.
func slackConnectionWithUsage(ctx context.Context, t *testing.T, db *sql.DB, dao *connectiondao.ConnectionDAO, team string) (teamSpaceID, connectionID, project, token string) {
	t.Helper()
	teamSpaceID, account, project := "ts"+flowid.RandomID(10), flowid.RandomID(32), "p"+flowid.RandomID(7)
	now := time.Now().UTC()
	if _, _, err := accountdao.New(db, []string{"admin@superverse.test"}).Register(ctx, accountdao.Account{
		AccountID: account, CanonicalEmail: "relay-" + account + "@example.com", Status: accountdao.StatusActive,
		PasswordRecord: json.RawMessage(`{}`), CredentialRevision: 1, AuthEpoch: 1, RegistrationFingerprint: account, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if err := dao.DeleteProjectUsages(cleanup, project); err != nil {
			t.Error(err)
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM team_space_connections WHERE team_space_id = $1`, teamSpaceID); err != nil {
			t.Errorf("clean up the connection: %v", err)
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM accounts WHERE account_id = $1`, account); err != nil {
			t.Errorf("clean up the account: %v", err)
		}
	})
	c, _, err := dao.Create(ctx, connectiondao.Connection{
		TeamSpaceID: teamSpaceID, ConnectionID: "c" + flowid.RandomID(11), CreationFingerprint: project, Name: "slack-" + project, ConnectorID: connections.SlackConnectorID,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/slack", VersionLine: "v1", ManifestVersion: "v1.0.0",
		ClientSource: connectiondao.ClientPlatform, Settings: json.RawMessage(`{}`), Revision: 1, CredentialStatus: connectiondao.CredentialAuthorizing,
		ConnectionStatus: connectiondao.ConnectionActive, CreatorAccountID: account, UpdatedByAccountID: account, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = dao.ClaimAuthorizationLease(ctx, teamSpaceID, c.ConnectionID, c.CredentialRevision, "seed", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = dao.CommitCredential(ctx, connectiondao.CredentialCommit{TeamSpaceID: teamSpaceID, ConnectionID: c.ConnectionID, ExpectedRevision: c.CredentialRevision,
		LeaseOwner: "seed", CredentialRef: json.RawMessage(`{"Key":"k"}`), Status: connectiondao.CredentialReady, ProviderAccount: &connectiondao.ProviderAccount{AccountID: team}}); err != nil {
		t.Fatal(err)
	}
	token = flowid.RandomID(32)
	if _, err = dao.RecordScopeToken(ctx, project, "preview", connections.ScopeTokenHash(token), json.RawMessage(`{"key":"token"}`)); err != nil {
		t.Fatal(err)
	}
	if err = dao.AddUsages(ctx, teamSpaceID, []string{c.ConnectionID}, project, "preview", false); err != nil {
		t.Fatal(err)
	}
	return teamSpaceID, c.ConnectionID, project, token
}
