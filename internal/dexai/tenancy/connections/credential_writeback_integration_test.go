// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connections

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// TestCredentialWriteBackCleanupIntegration commits credentials of a
// published openai release over the lane's PostgreSQL and MinIO in the
// orders that concurrent renewals produce. A writer whose lease expired and
// another writer then applies the same answer: the late commit fails and
// deletes nothing, so the object both wrote stays readable. A commit's
// cleanup that runs late, after the next lease holder wrote its object,
// deletes only the credential it replaced, so the next commit's object stays
// readable. A commit that never held the lease deletes nothing, while a
// commit under its lease removes what a lost lease left.
func TestCredentialWriteBackCleanupIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	dao := connectiondao.New(db)
	objects := artifactStore(ctx, t)
	store := credentialObjects{objects: objects}
	teamSpaceID, account := "ts"+flowid.RandomID(10), flowid.RandomID(32)
	registerAccount(ctx, t, db, account, teamSpaceID)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if _, err := objects.DeletePrefix(cleanup, teamSpacePrefix(teamSpaceID)); err != nil {
			t.Error(err)
		}
	})
	credential := func(apiKey string) storedCredential {
		return storedCredential{Credentials: map[string]json.RawMessage{"api_key": comm.Must(json.Marshal(apiKey))}}
	}
	original := staticConnection(ctx, t, dao, objects, teamSpaceID, account, "cleanup", "v0.21.1", "key-1")
	write := func(next storedCredential) objectstore.ObjectRef {
		written, err := store.write(ctx, teamSpaceID, original.ConnectionID, next)
		if err != nil {
			t.Fatal(err)
		}
		return written
	}
	readable := func(row connectiondao.Connection, step string) {
		if _, err := store.read(ctx, row); err != nil {
			t.Fatalf("%s: the committed credential is unreadable: %v", step, err)
		}
	}

	// The first writer's lease expires; the second applies the same answer.
	same := credential("key-2")
	if err := dao.ClaimRefreshLease(ctx, teamSpaceID, original.ConnectionID, original.CredentialRevision, "late", time.Second); err != nil {
		t.Fatal(err)
	}
	late := write(same)
	time.Sleep(1100 * time.Millisecond)
	if err := dao.ClaimRefreshLease(ctx, teamSpaceID, original.ConnectionID, original.CredentialRevision, "current", time.Minute); err != nil {
		t.Fatal(err)
	}
	current := write(same)
	if current != late {
		t.Fatalf("identical credentials wrote different objects")
	}
	if _, err := store.commit(ctx, dao, original, "late", late, same, ""); !errors.Is(err, connectiondao.ErrRevisionChanged) {
		t.Fatalf("a commit without the lease: %v", err)
	}
	committed, err := store.commit(ctx, dao, original, "current", current, same, "")
	if err != nil {
		t.Fatal(err)
	}
	readable(committed, "after a late commit of the same answer")

	// The next lease holder writes before the previous commit's cleanup ran.
	leftover := write(credential("lost-lease"))
	if err = dao.ClaimRefreshLease(ctx, teamSpaceID, original.ConnectionID, committed.CredentialRevision, "next", time.Minute); err != nil {
		t.Fatal(err)
	}
	next := credential("key-3")
	written := write(next)
	// A commit that never held the lease deletes nothing, not even the objects
	// of the lease holder it lost to.
	stale := write(credential("stale"))
	if _, err = store.commit(ctx, dao, committed, "stale", stale, credential("stale"), ""); !errors.Is(err, connectiondao.ErrRevisionChanged) {
		t.Fatalf("a commit without the lease: %v", err)
	}
	if _, err = objects.Get(ctx, written); err != nil {
		t.Fatalf("a commit without the lease deleted the lease holder's object: %v", err)
	}
	store.cleanReplaced(ctx, original, current)
	renewed, err := store.commit(ctx, dao, committed, "next", written, next, "")
	if err != nil {
		t.Fatal(err)
	}
	readable(renewed, "after a late cleanup")
	for _, gone := range []objectstore.ObjectRef{current, leftover, stale} {
		if _, err = objects.Get(ctx, gone); err == nil {
			t.Fatalf("object %s survived the commit under the lease", gone.Key)
		}
	}
}

// TestStoredAnswerAppliesToItsGrantIntegration recovers a Slack v1.0.0
// connection, whose bot and user tokens renew through two refresh grants,
// over the lane's PostgreSQL and MinIO. A claimant stored the user grant's
// refresh answer and crashed; now the bot grant is due. The read applies the
// stored answer to the user grant, whose refresh token Slack already spent,
// without calling Slack, and deletes the answer once committed.
func TestStoredAnswerAppliesToItsGrantIntegration(t *testing.T) {
	f := slackConnection(t)
	ctx, objects, reader, c, credentials := f.ctx, f.objects, f.reader, f.row, f.credentials
	text := func(value string) json.RawMessage { return comm.Must(json.Marshal(value)) }
	now := time.Now().UTC()
	userToken, userRefresh := "xoxe.xoxp-"+flowid.RandomID(16), "xoxe-user-"+flowid.RandomID(16)
	body := comm.Must(json.Marshal(map[string]any{"ok": true, "access_token": userToken, "refresh_token": userRefresh, "expires_in": 43200, "token_type": "user"}))
	answer := storedResponse{Grant: "user_refresh_token", ReceivedAt: now.Add(-time.Minute), Body: body}
	if _, err := objects.Put(ctx, refreshKey(c), objectstore.MediaTypeJSON, comm.Must(objectstore.EncodeJSON(answer))); err != nil {
		t.Fatal(err)
	}

	read, err := reader.read(ctx, c, 0)
	if err != nil {
		t.Fatalf("a read with the user grant's stored answer: %v", err)
	}
	if read.revision != c.CredentialRevision+1 || string(read.Values["user_token"]) != string(text(userToken)) ||
		string(read.Values["user_refresh_token"]) != string(text(userRefresh)) || string(read.Values["bot_token"]) != string(credentials["bot_token"]) {
		t.Fatalf("the stored answer was not applied to the user grant: revision %d of %d", read.revision, c.CredentialRevision)
	}
	if _, err = objects.GetCurrentJSON(ctx, refreshKey(c), &json.RawMessage{}); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("the applied answer stays stored: %v", err)
	}
}

// TestUnusableStoredAnswerRequiresReauthorizationIntegration stores a Slack
// v1.0.0 refresh answer that cannot be applied, an answer of a grant the
// release does not declare and one whose body is no token object, over the
// lane's PostgreSQL and MinIO. The read deletes it and marks the connection
// REAUTHORIZATION_REQUIRED instead of replaying it on every read.
func TestUnusableStoredAnswerRequiresReauthorizationIntegration(t *testing.T) {
	for name, answer := range map[string]storedResponse{
		"undeclared grant": {Grant: "other_refresh_token", ReceivedAt: time.Now().UTC(), Body: json.RawMessage(`{"ok":true,"access_token":"x"}`)},
		"no token object":  {Grant: "user_refresh_token", ReceivedAt: time.Now().UTC(), Body: json.RawMessage(`["not","a","token"]`)},
	} {
		t.Run(name, func(t *testing.T) {
			f := slackConnection(t)
			if _, err := f.objects.Put(f.ctx, refreshKey(f.row), objectstore.MediaTypeJSON, comm.Must(objectstore.EncodeJSON(answer))); err != nil {
				t.Fatal(err)
			}
			if _, err := f.reader.read(f.ctx, f.row, 0); !errors.Is(err, ErrReauthorizationRequired) {
				t.Fatalf("a read with an unusable stored answer: %v", err)
			}
			row, err := f.dao.Connection(f.ctx, f.row.TeamSpaceID, f.row.ConnectionID)
			if err != nil || row.CredentialStatus != connectiondao.CredentialReauthorizationRequired || len(row.LastFailure) == 0 {
				t.Fatalf("the connection after an unusable answer: %+v: %v", row, err)
			}
			if _, err = f.objects.GetCurrentJSON(f.ctx, refreshKey(f.row), &json.RawMessage{}); !errors.Is(err, objectstore.ErrNotFound) {
				t.Fatalf("the unusable answer stays stored: %v", err)
			}
		})
	}
}

// slackFixture is a READY Slack v1.0.0 connection whose bot grant is due and
// whose user grant is not, over the lane's PostgreSQL and MinIO.
type slackFixture struct {
	ctx         context.Context
	dao         *connectiondao.ConnectionDAO
	objects     *objectstore.Store
	reader      *CredentialReader
	row         connectiondao.Connection
	credentials map[string]json.RawMessage
}

func slackConnection(t *testing.T) slackFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	db := postgrestest.OpenForTest(t)
	f := slackFixture{ctx: ctx, dao: connectiondao.New(db), objects: artifactStore(ctx, t)}
	releases := connectormanifest.NewReleaseResolver(connectormanifest.NewReleaseHTTPClient(), connectorlibrary.OfficialReleaseDownloadURL)
	f.reader = NewCredentialReader(f.dao, f.objects, releases, NewProviderHTTPClient(true), PlatformClients{}, slog.Default())
	teamSpaceID, account := "ts"+flowid.RandomID(10), flowid.RandomID(32)
	registerAccount(ctx, t, db, account, teamSpaceID)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if _, err := f.objects.DeletePrefix(cleanup, teamSpacePrefix(teamSpaceID)); err != nil {
			t.Error(err)
		}
	})
	text := func(value string) json.RawMessage { return comm.Must(json.Marshal(value)) }
	now := time.Now().UTC()
	botExpiry, userExpiry := now.Add(20*time.Second), now.Add(2*time.Hour)
	f.credentials = map[string]json.RawMessage{
		"oauth_client_id": text("client-" + flowid.RandomID(8)), "oauth_client_secret": text("secret-" + flowid.RandomID(16)),
		"bot_token": text("xoxe.xoxb-" + flowid.RandomID(16)), "bot_refresh_token": text("xoxe-bot-" + flowid.RandomID(16)),
		"user_token": text("xoxe.xoxp-" + flowid.RandomID(16)), "user_refresh_token": text("xoxe-user-" + flowid.RandomID(16)),
	}
	connectormanifest.StampAuthMethod(f.credentials, "")
	module := connectormanifest.Module{ConnectorID: "slack", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/slack", ModuleVersion: "v1.0.0"}
	f.row = readyConnection(ctx, t, f.dao, f.objects, teamSpaceID, account, "slack", module, "", storedCredential{Credentials: f.credentials, Grants: map[string]grantTimes{
		"bot_refresh_token":  {IssuedAt: now.Add(-12 * time.Hour), ExpiresAt: &botExpiry},
		"user_refresh_token": {IssuedAt: now.Add(-10 * time.Hour), ExpiresAt: &userExpiry},
	}})
	return f
}
