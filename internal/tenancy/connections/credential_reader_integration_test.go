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
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
)

type readerFixture struct {
	db          *sql.DB
	dao         *connectiondao.ConnectionDAO
	objects     *objectstore.Store
	reader      *CredentialReader
	teamSpaceID string
	account     string
}

// newReaderFixture opens the lane's PostgreSQL and MinIO with a reader over
// the published releases and one account in a new TeamSpace.
func newReaderFixture(ctx context.Context, t *testing.T) readerFixture {
	t.Helper()
	db := postgrestest.OpenForTest(t)
	dao := connectiondao.New(db)
	objects := artifactStore(ctx, t)
	releases := connectormanifest.NewReleaseResolver(connectormanifest.NewReleaseHTTPClient(), connectorlibrary.OfficialReleaseDownloadURL)
	f := readerFixture{db: db, dao: dao, objects: objects, teamSpaceID: "ts" + flowid.RandomID(10), account: flowid.RandomID(32),
		reader: NewCredentialReader(dao, objects, releases, NewProviderHTTPClient(true), PlatformClients{}, slog.Default())}
	registerAccount(ctx, t, db, f.account, f.teamSpaceID)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if _, err := NewUsages(dao, objects).DeleteTeamSpaceConnections(cleanup, f.teamSpaceID); err != nil {
			t.Error(err)
		}
	})
	return f
}

// TestStaleExpiryMarkRereadsIntegration reads a credential that nothing can
// renew and whose expiry passed, from a row another reader already marked
// REAUTHORIZATION_REQUIRED, over the lane's PostgreSQL and MinIO. The lost
// mark is not an error of the read: it rereads the row and answers that the
// connection needs reauthorization, as a reader of the current row does.
func TestStaleExpiryMarkRereadsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	f := newReaderFixture(ctx, t)
	row := staticConnection(ctx, t, f.dao, f.objects, f.teamSpaceID, f.account, "expired", "v0.21.1", "key-1")
	failure := reauthorizationFailure("marked by another reader")
	if err := f.dao.MarkReauthorizationRequired(ctx, f.teamSpaceID, row.ConnectionID, row.CredentialRevision, failure); err != nil {
		t.Fatal(err)
	}
	// The reader holds the row from before the mark, with its expiry passed.
	stale := row
	stale.CredentialExpiresAt = sql.Null[time.Time]{V: time.Now().Add(-time.Minute), Valid: true}
	_, err := f.reader.read(ctx, stale, 0)
	if !errors.Is(err, ErrReauthorizationRequired) || errors.Is(err, connectiondao.ErrRevisionChanged) {
		t.Fatalf("a read from a row another reader marked: %v", err)
	}
}

// TestRenewalNeedsTimeLeftIntegration reads a gmail v0.23.0 credential whose
// grants expired while another reader holds the connection's lease for its
// whole 30 seconds, over the lane's PostgreSQL and MinIO. The read waits for
// the lease, finds less than the least renewal time of its 35-second budget
// left and starts no renewal: no lease claim, no provider call, no change to
// the credential; it answers that it has no answer.
func TestRenewalNeedsTimeLeftIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	f := newReaderFixture(ctx, t)
	module := connectormanifest.Module{ConnectorID: "gmail", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/google/gmail", ModuleVersion: "v0.23.0"}
	text := func(value string) json.RawMessage { return comm.Must(json.Marshal(value)) }
	issued := time.Now().UTC().Add(-2 * time.Hour)
	expires := issued.Add(time.Hour)
	credentials := map[string]json.RawMessage{
		"oauth_client_id": text("client-" + flowid.RandomID(8)), "oauth_client_secret": text("secret-" + flowid.RandomID(16)),
		"access_token": text("access-" + flowid.RandomID(16)), "refresh_token": text("refresh-" + flowid.RandomID(16)), "primary_email": text("user@example.com"),
	}
	connectormanifest.StampAuthMethod(credentials, "google-oauth")
	row := readyConnection(ctx, t, f.dao, f.objects, f.teamSpaceID, f.account, "gmail", module, "google-oauth",
		storedCredential{Credentials: credentials, Grants: map[string]grantTimes{"refresh_token": {IssuedAt: issued, ExpiresAt: &expires}}})
	if err := f.dao.ClaimRefreshLease(ctx, f.teamSpaceID, row.ConnectionID, row.CredentialRevision, "other", refreshLease); err != nil {
		t.Fatal(err)
	}
	held, err := f.dao.Connection(ctx, f.teamSpaceID, row.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.reader.read(ctx, row, 0)
	if !errors.Is(err, errNoAnswer) {
		t.Fatalf("a read with too little of its budget left to renew: %v", err)
	}
	after, err := f.dao.Connection(ctx, f.teamSpaceID, row.ConnectionID)
	if err != nil || after.CredentialStatus != connectiondao.CredentialReady || after.CredentialRevision != row.CredentialRevision ||
		!after.RefreshLeaseUntil.V.Equal(held.RefreshLeaseUntil.V) {
		t.Fatalf("the read renewed or claimed: %+v: %v", after, err)
	}
}
