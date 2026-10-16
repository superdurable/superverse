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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// A credential lives only in immutable objects of the platform bucket under
// its connection's prefix: credentials/<sha256> is the current credential,
// and refresh/<credential revision> the raw answer of a refresh in progress,
// which the commit of that revision deletes. The row references the current
// object.

// storedCredential is a credential object: the values the connector decodes,
// and the issue time and expiry of each grant that refreshes or mints them.
type storedCredential struct {
	Credentials map[string]json.RawMessage `json:"credentials"`
	Grants      map[string]grantTimes      `json:"grants,omitempty"`
}

// grantTimes is when a grant's tokens were issued and expire; a nil expiry
// keeps the token until a provider rejects it.
type grantTimes struct {
	IssuedAt  time.Time  `json:"issuedAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// storedResponse is a refresh's raw provider answer, stored before it is
// parsed, so a claimant after a crash applies it instead of spending the
// refresh token again. Only successful answers are stored.
type storedResponse struct {
	Grant      string          `json:"grant"`
	ReceivedAt time.Time       `json:"receivedAt"`
	Body       json.RawMessage `json:"body"`
}

// connectionPrefix holds every object of a connection.
func connectionPrefix(teamSpaceID, connectionID string) string {
	return "teamspaces/" + teamSpaceID + "/connections/" + connectionID + "/"
}

// teamSpacePrefix holds every object of a TeamSpace's connections.
func teamSpacePrefix(teamSpaceID string) string { return "teamspaces/" + teamSpaceID + "/" }

func refreshKey(c connectiondao.Connection) string {
	return connectionPrefix(c.TeamSpaceID, c.ConnectionID) + "refresh/" + strconv.FormatUint(c.CredentialRevision, 10)
}

// credentialObjects writes and reads credential objects of the platform
// bucket.
type credentialObjects struct {
	objects *objectstore.Store
}

// read returns the credential a row references.
func (store credentialObjects) read(ctx context.Context, c connectiondao.Connection) (storedCredential, error) {
	var ref objectstore.ObjectRef
	if err := json.Unmarshal(c.CredentialRef, &ref); err != nil {
		return storedCredential{}, fmt.Errorf("connection %s has no credential reference: %w", c.ConnectionID, err)
	}
	var credential storedCredential
	if err := store.objects.GetJSON(ctx, ref, &credential); err != nil {
		return storedCredential{}, fmt.Errorf("read the credential of connection %s: %w", c.ConnectionID, err)
	}
	return credential, nil
}

// write stores credential as a new object of the connection and returns its
// reference. The key is the digest of the content, so a retry writes the
// same object.
func (store credentialObjects) write(ctx context.Context, teamSpaceID, connectionID string, credential storedCredential) (objectstore.ObjectRef, error) {
	contents := comm.Must(objectstore.EncodeJSON(credential))
	digest := sha256.Sum256(contents)
	ref, err := store.objects.Put(ctx, connectionPrefix(teamSpaceID, connectionID)+"credentials/"+hex.EncodeToString(digest[:]), objectstore.MediaTypeJSON, contents)
	if err != nil {
		return objectstore.ObjectRef{}, fmt.Errorf("write the credential of connection %s: %w", connectionID, err)
	}
	return ref, nil
}

// commit makes written the connection's READY credential under the lease
// owner holds. actor is the account of an authorization or key change, empty
// for a renewal.
func (store credentialObjects) commit(ctx context.Context, dao *connectiondao.ConnectionDAO, c connectiondao.Connection, owner string, written objectstore.ObjectRef,
	credential storedCredential, actor string) (connectiondao.Connection, error) {
	return store.commitWithStatus(ctx, dao, c, owner, written, credential, actor, connectiondao.CredentialReady)
}

// commitWithStatus commits as commit does, with the given status: a copied
// OAuth client waits for its authorization. A commit cleans up what a crash
// or a lost lease left only while it holds the lease, and then deletes the
// credential it replaced and the stored answer of its revision, nothing
// else: an object outside those may be a newer lease holder's. A commit that
// lost the lease deletes nothing, not even its own object, which may be
// identical to the one the new lease holder writes.
func (store credentialObjects) commitWithStatus(ctx context.Context, dao *connectiondao.ConnectionDAO, c connectiondao.Connection, owner string, written objectstore.ObjectRef,
	credential storedCredential, actor string, status connectiondao.CredentialStatus) (connectiondao.Connection, error) {
	return store.commitAccount(ctx, dao, c, owner, written, credential, actor, status, nil)
}

// commitAccount commits as commitWithStatus does and, when account is set,
// records the provider account an authorization installed the connection in.
func (store credentialObjects) commitAccount(ctx context.Context, dao *connectiondao.ConnectionDAO, c connectiondao.Connection, owner string, written objectstore.ObjectRef,
	credential storedCredential, actor string, status connectiondao.CredentialStatus, account *connectiondao.ProviderAccount) (connectiondao.Connection, error) {
	store.cleanUnderLease(ctx, dao, c, owner, written)
	issuedAt, expiresAt := credential.earliestExpiry()
	committed, err := dao.CommitCredential(ctx, connectiondao.CredentialCommit{
		TeamSpaceID: c.TeamSpaceID, ConnectionID: c.ConnectionID, ExpectedRevision: c.CredentialRevision, LeaseOwner: owner,
		CredentialRef: comm.Must(json.Marshal(written)), IssuedAt: nullTime(issuedAt), ExpiresAt: nullTime(expiresAt),
		Status: status, UpdatedByAccountID: sql.Null[string]{V: actor, Valid: actor != ""}, Renewal: actor == "", ProviderAccount: account,
	})
	if err != nil {
		return connectiondao.Connection{}, err
	}
	store.cleanReplaced(context.WithoutCancel(ctx), c, written)
	return committed, nil
}

// cleanupBudget bounds the cleanup a commit makes under its lease; the lease
// must last longer than it.
const cleanupBudget = 10 * time.Second

// cleanUnderLease deletes every object of c's connection but its current
// credential, written and the stored answer of c's revision, when owner holds
// the lease long enough that nobody else writes meanwhile. A failed cleanup
// leaves objects that the next commit deletes.
func (store credentialObjects) cleanUnderLease(ctx context.Context, dao *connectiondao.ConnectionDAO, c connectiondao.Connection, owner string, written objectstore.ObjectRef) {
	if held, err := dao.LeaseHeldFor(ctx, c.TeamSpaceID, c.ConnectionID, owner, cleanupBudget); err != nil || !held {
		return
	}
	cleanup, cancel := context.WithTimeout(ctx, cleanupBudget-2*time.Second)
	defer cancel()
	keep := []string{written.Key, store.objects.FullKey(refreshKey(c))}
	if current, found := credentialRef(c); found {
		keep = append(keep, current.Key)
	}
	_, _ = store.objects.DeletePrefixExcept(cleanup, connectionPrefix(c.TeamSpaceID, c.ConnectionID), keep...)
}

// cleanReplaced deletes, after the commit of written over c, the credential c
// referenced and the stored answer of c's revision, which no reader uses any
// more. A failed deletion leaves them to the next commit's cleanup.
func (store credentialObjects) cleanReplaced(ctx context.Context, c connectiondao.Connection, written objectstore.ObjectRef) {
	if current, found := credentialRef(c); found && current != written {
		_ = store.objects.Delete(ctx, current)
	}
	if ref, err := store.objects.GetCurrentJSON(ctx, refreshKey(c), &json.RawMessage{}); err == nil {
		_ = store.objects.Delete(ctx, ref)
	}
}

// credentialRef is the credential object c references, if any.
func credentialRef(c connectiondao.Connection) (objectstore.ObjectRef, bool) {
	var ref objectstore.ObjectRef
	if len(c.CredentialRef) == 0 {
		return ref, false
	}
	comm.MustDo(json.Unmarshal(c.CredentialRef, &ref))
	return ref, true
}

// earliestExpiry is the issue time and expiry of the grant that expires
// first, which the row records; nil times for a credential no grant renews.
func (credential storedCredential) earliestExpiry() (*time.Time, *time.Time) {
	var chosen *grantTimes
	for _, times := range credential.Grants {
		if chosen == nil || expiresBefore(times, *chosen) {
			chosen = &times
		}
	}
	if chosen == nil {
		return nil, nil
	}
	issuedAt := chosen.IssuedAt
	return &issuedAt, chosen.ExpiresAt
}

// expiresBefore orders grants by expiry; a grant without one comes last.
func expiresBefore(first, second grantTimes) bool {
	switch {
	case first.ExpiresAt == nil:
		return false
	case second.ExpiresAt == nil:
		return true
	}
	return first.ExpiresAt.Before(*second.ExpiresAt)
}

// apply maps a grant's successful token response into a copy of the
// credential: each mapped value it carries, the copies a JWT bearer grant
// declares and the grant's times; a value the response omits keeps the
// stored one.
func (credential storedCredential) apply(grant connectormanifest.TokenGrant, copies []connectormanifest.CredentialCopy, token map[string]any, receivedAt time.Time) (storedCredential, error) {
	next := storedCredential{Credentials: map[string]json.RawMessage{}, Grants: map[string]grantTimes{}}
	for name, value := range credential.Credentials {
		next.Credentials[name] = value
	}
	for id, times := range credential.Grants {
		next.Grants[id] = times
	}
	mappings := grant.CredentialMappings
	if len(mappings) == 0 {
		mappings = []connectormanifest.OAuthCredentialMapping{{Credential: connectormanifest.TokenMemberAccessToken, Source: connectormanifest.TokenMemberAccessToken}}
	}
	for _, mapping := range mappings {
		if value, found := connectormanifest.ResponseText(token, mapping.Source); found {
			next.Credentials[mapping.Credential] = comm.Must(json.Marshal(value))
		}
	}
	for _, copied := range copies {
		if value, found := next.Credentials[copied.FromCredential]; found {
			next.Credentials[copied.Credential] = value
		}
	}
	expiresAt, err := connectormanifest.TokenExpiry(token, receivedAt)
	if err != nil {
		return storedCredential{}, err
	}
	if expiresAt == nil {
		expiresAt = grant.MissingExpiry.Expiry(receivedAt)
	}
	next.Grants[grant.ID] = grantTimes{IssuedAt: receivedAt, ExpiresAt: expiresAt}
	return next, nil
}

func nullTime(value *time.Time) sql.Null[time.Time] {
	if value == nil {
		return sql.Null[time.Time]{}
	}
	return sql.Null[time.Time]{V: *value, Valid: true}
}

// AuthorizeForTest commits values as the READY credential of c with the
// provider account an authorization records, as an OAuth completion of the
// platform client does; the local lanes have no platform OAuth application.
func AuthorizeForTest(ctx context.Context, dao *connectiondao.ConnectionDAO, objects *objectstore.Store, c connectiondao.Connection,
	values map[string]json.RawMessage, account connectiondao.ProviderAccount, actor string) (connectiondao.Connection, error) {
	store := credentialObjects{objects: objects}
	credential := storedCredential{Credentials: values}
	written, err := store.write(ctx, c.TeamSpaceID, c.ConnectionID, credential)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	owner := flowid.RandomID(24)
	if err = dao.ClaimAuthorizationLease(ctx, c.TeamSpaceID, c.ConnectionID, c.CredentialRevision, owner, refreshLease); err != nil {
		return connectiondao.Connection{}, err
	}
	return store.commitAccount(ctx, dao, c, owner, written, credential, actor, connectiondao.CredentialReady, &account)
}
