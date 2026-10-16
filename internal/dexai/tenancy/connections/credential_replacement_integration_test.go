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
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// TestCredentialReplacementNeedsCurrentRevisionIntegration replaces the API
// key of a published openai v0.21.1 connection over the lane's PostgreSQL and
// MinIO. A replacement that names the current credential revision succeeds and
// advances it; a second one that still names the old revision is
// ErrRevisionChanged and leaves the first key in place.
func TestCredentialReplacementNeedsCurrentRevisionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	dao := connectiondao.New(db)
	objects := artifactStore(ctx, t)
	releaseClient := connectormanifest.NewReleaseHTTPClient()
	releases := connectormanifest.NewReleaseResolver(releaseClient, connectorlibrary.OfficialReleaseDownloadURL)
	providers := NewProviderHTTPClient(true)
	reader := NewCredentialReader(dao, objects, releases, providers, PlatformClients{}, slog.Default())
	shared := NewTeamSpaceConnections(dao, objects, releases, connectormanifest.NewCatalog(releaseClient), PlatformClients{}, providers, reader, nil, true)
	usages := NewUsages(dao, objects)
	teamSpaceID, account := "ts"+flowid.RandomID(10), flowid.RandomID(32)
	registerAccount(ctx, t, db, account, teamSpaceID)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if _, err := usages.DeleteTeamSpaceConnections(cleanup, teamSpaceID); err != nil {
			t.Error(err)
		}
	})
	original := staticConnection(ctx, t, dao, objects, teamSpaceID, account, "replace", "v0.21.1", "key-1")
	replace := func(apiKey string, expectedCredentialRevision uint64) (connectiondao.Connection, error) {
		return shared.Update(ctx, ConnectionUpdate{TeamSpaceID: teamSpaceID, ConnectionID: original.ConnectionID, ActorAccountID: account,
			ExpectedRevision: original.Revision, ExpectedCredentialRevision: expectedCredentialRevision,
			Credentials:         map[string]json.RawMessage{"api_key": comm.Must(json.Marshal(apiKey))},
			MayChangeCredential: func(context.Context) (bool, error) { return true, nil }})
	}

	replaced, err := replace("key-2", original.CredentialRevision)
	if err != nil || replaced.CredentialRevision != original.CredentialRevision+1 {
		t.Fatalf("a replacement at the current credential revision: %+v: %v", replaced, err)
	}
	if _, err = replace("key-3", original.CredentialRevision); !errors.Is(err, connectiondao.ErrRevisionChanged) {
		t.Fatalf("a replacement at a stale credential revision: %v", err)
	}
	row, err := dao.Connection(ctx, teamSpaceID, original.ConnectionID)
	if err != nil || row.CredentialRevision != replaced.CredentialRevision {
		t.Fatalf("the refused replacement moved the credential: %+v: %v", row, err)
	}
	stored, err := (credentialObjects{objects: objects}).read(ctx, row)
	if err != nil || string(stored.Credentials["api_key"]) != `"key-2"` {
		t.Fatalf("the credential after the refused replacement: %v", err)
	}
}
