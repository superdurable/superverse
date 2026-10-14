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
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
)

// A read returns a connection's credential. When the grant that renews it is
// due, the read refreshes or mints it first, under the connection's lease, so
// one provider call at a time renews one connection and a refresh token that
// a provider invalidates on use is never spent twice. Nothing renews a
// credential that nobody reads.

const (
	// refreshLease is how long one read may renew a connection.
	refreshLease = 30 * time.Second
	// leaseMargin ends a provider call this long before its lease.
	leaseMargin = 5 * time.Second
	// minRenewal is the least time a renewal starts with.
	minRenewal = 10 * time.Second
	// leasePoll spaces the rereads of a read that waits for another's lease.
	leasePoll = 200 * time.Millisecond
	// readBudget bounds one read, waiting and renewal included, below the
	// listener's 40-second write timeout.
	readBudget = 35 * time.Second
)

// failureCode is the closed set of last_failure codes a connection records.
type failureCode string

const (
	// failureCredentialRejected is a credential that its provider will never
	// renew again.
	failureCredentialRejected failureCode = "CREDENTIAL_REJECTED"
	// failureAuthorizationFailed is an OAuth authorization that did not
	// complete; the credential stays as it was.
	failureAuthorizationFailed failureCode = "AUTHORIZATION_FAILED"
)

// ErrReauthorizationRequired is a credential that needs a new authorization,
// or new client or key material, before it can be read again.
var ErrReauthorizationRequired = errors.New("the connection needs reauthorization")

// errConnectorVersionUnsupported is a connector release that does not list
// the credentials a host keeps from applications. It predates TeamSpace
// connections, so no connection is created with it or moved to it, and no
// credential is read through it.
var errConnectorVersionUnsupported = errors.New("the connector version predates TeamSpace connections")

// Credential is the credential a read returns: its values, revision and the
// times of the grant that expires first, nil for a credential nothing renews.
type Credential struct {
	Values              map[string]json.RawMessage
	revision            uint64
	issuedAt, expiresAt *time.Time
	// hostKept are the values an application never reads, as the release
	// lists them.
	hostKept []string
}

// CredentialReader reads TeamSpace connection credentials, renewing them as
// each connector's manifest declares.
type CredentialReader struct {
	dao       *connectiondao.ConnectionDAO
	objects   credentialObjects
	releases  *connectormanifest.ReleaseResolver
	providers *http.Client
	clients   PlatformClients
	logger    *slog.Logger
}

// NewCredentialReader reads with dao and objects, resolves manifests with
// releases, calls providers with the provider client and logs to logger.
func NewCredentialReader(dao *connectiondao.ConnectionDAO, objects *objectstore.Store, releases *connectormanifest.ReleaseResolver, providers *http.Client, clients PlatformClients,
	logger *slog.Logger) *CredentialReader {
	return &CredentialReader{dao: dao, objects: credentialObjects{objects: objects}, releases: releases, providers: providers, clients: clients, logger: logger}
}

// ReadConnection is a read for the platform's own use, such as Studio's
// pickers, whose caller the API or a selection authorized for the
// connection.
func (r *CredentialReader) ReadConnection(ctx context.Context, connectionID string) (Credential, error) {
	c, err := r.dao.ConnectionByID(ctx, connectionID)
	if err != nil {
		return Credential{}, err
	}
	if c.ConnectionStatus != connectiondao.ConnectionActive {
		return Credential{}, connectiondao.ErrNotFound
	}
	return r.read(ctx, c, 0)
}

// StoredValues returns the values a connection's current credential holds,
// renewing nothing: what a redaction of an application's output replaces. A
// connection without a credential has none.
func (r *CredentialReader) StoredValues(ctx context.Context, connectionID string) (map[string]json.RawMessage, error) {
	c, err := r.dao.ConnectionByID(ctx, connectionID)
	if err != nil || len(c.CredentialRef) == 0 {
		return nil, err
	}
	credential, err := r.objects.read(ctx, c)
	return credential.Credentials, err
}

// read returns c's credential, renewing it first when its due grant needs
// it. rejected is the revision of a credential a provider rejected, which
// forces a renewal unless it was issued within the forced-refresh floor.
func (r *CredentialReader) read(ctx context.Context, c connectiondao.Connection, rejected uint64) (Credential, error) {
	budgetEnd := time.Now().Add(readBudget)
	ctx, cancel := context.WithDeadline(ctx, budgetEnd)
	defer cancel()
	for {
		if c.CredentialStatus != connectiondao.CredentialReady {
			return Credential{}, ErrReauthorizationRequired
		}
		d, err := resolveDeclarations(ctx, r.releases, c)
		if err != nil {
			return Credential{}, err
		}
		credential, err := r.objects.read(ctx, c)
		if err != nil {
			return Credential{}, err
		}
		grant, due, static := dueGrant(d.grants, credential, time.Now(), rejected == c.CredentialRevision)
		if static && c.CredentialExpiresAt.Valid && !time.Now().Before(c.CredentialExpiresAt.V) {
			// Nothing can renew an expired credential without a refresh token.
			err = r.dao.MarkReauthorizationRequired(context.WithoutCancel(ctx), c.TeamSpaceID, c.ConnectionID, c.CredentialRevision,
				reauthorizationFailure("the credential expired and nothing renews it"))
			if errors.Is(err, connectiondao.ErrRevisionChanged) {
				if c, err = r.dao.Connection(ctx, c.TeamSpaceID, c.ConnectionID); err != nil {
					return Credential{}, err
				}
				continue
			}
			return Credential{}, errors.Join(ErrReauthorizationRequired, err)
		}
		if !due {
			return ready(c, credential, d), nil
		}
		owner := flowid.RandomID(24)
		// The renewal ends a margin before the lease, measured from before
		// the claim on the local clock, and within the read's budget.
		deadline := time.Now().Add(refreshLease - leaseMargin)
		if budgetEnd.Before(deadline) {
			deadline = budgetEnd
		}
		if time.Until(deadline) < minRenewal {
			// Too little of the budget is left to finish a provider call.
			if current := ready(c, credential, d); current.expiresAt != nil && time.Now().Before(*current.expiresAt) {
				return current, nil
			}
			return Credential{}, fmt.Errorf("%w: the read's budget ended before the renewal", errNoAnswer)
		}
		err = r.dao.ClaimRefreshLease(ctx, c.TeamSpaceID, c.ConnectionID, c.CredentialRevision, owner, refreshLease)
		if errors.Is(err, connectiondao.ErrRevisionChanged) {
			if c, err = r.awaitLease(ctx, c); err != nil {
				return Credential{}, err
			}
			continue
		}
		if err != nil {
			return Credential{}, err
		}
		renewed, err := r.renew(ctx, c, d, grant, credential, owner, deadline)
		if !errors.Is(err, connectiondao.ErrRevisionChanged) {
			return renewed, err
		}
		if c, err = r.dao.Connection(ctx, c.TeamSpaceID, c.ConnectionID); err != nil {
			return Credential{}, err
		}
	}
}

// awaitLease rereads a connection that another read is renewing until its
// credential or status changes or the lease expires, and returns the row.
func (r *CredentialReader) awaitLease(ctx context.Context, c connectiondao.Connection) (connectiondao.Connection, error) {
	for {
		select {
		case <-ctx.Done():
			return connectiondao.Connection{}, fmt.Errorf("%w: another read is renewing the credential: %w", errNoAnswer, ctx.Err())
		case <-time.After(leasePoll):
		}
		next, err := r.dao.Connection(ctx, c.TeamSpaceID, c.ConnectionID)
		if err != nil {
			return connectiondao.Connection{}, err
		}
		if next.CredentialRevision != c.CredentialRevision || next.CredentialStatus != c.CredentialStatus || next.ConnectionStatus != c.ConnectionStatus ||
			!next.RefreshLeaseUntil.Valid || next.RefreshLeaseUntil.V.Before(time.Now()) {
			return next, nil
		}
	}
}

// renew runs one grant's provider call under the lease owner holds and
// writes its result back by deadline. The call outlives the read's caller: a
// refresh that reached a provider must be stored, or a rotated token is lost. When
// the provider gives no usable answer, the current credential is returned
// while it has not expired.
func (r *CredentialReader) renew(ctx context.Context, c connectiondao.Connection, d declarations, grant connectormanifest.TokenGrant, credential storedCredential, owner string, deadline time.Time) (Credential, error) {
	call, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()
	outcome, grant, stored, err := r.grantOutcome(call, c, d, grant, credential)
	if err != nil && stored != nil {
		// A stored answer that cannot be read is spent as well.
		return Credential{}, errors.Join(err, r.requireReauthorization(call, c, owner, stored, refreshAnswerUnusable))
	}
	if err != nil {
		err = errors.Join(err, r.dao.ReleaseRefreshLease(call, c.TeamSpaceID, c.ConnectionID, owner))
		if current := ready(c, credential, d); current.expiresAt != nil && time.Now().Before(*current.expiresAt) {
			return current, nil
		}
		return Credential{}, err
	}
	if outcome.terminal != "" {
		return Credential{}, r.requireReauthorization(call, c, owner, stored, outcome.terminal)
	}
	var copies []connectormanifest.CredentialCopy
	if grant.Kind == connectormanifest.GrantJWTBearer {
		copies = d.method.ServiceAccount.JWTBearer.CredentialCopies
	}
	next, err := credential.apply(grant, copies, outcome.token, outcome.receivedAt)
	if err != nil && stored != nil {
		// The answer spent the refresh token, and replaying it fails alike.
		return Credential{}, errors.Join(err, r.requireReauthorization(call, c, owner, stored, refreshAnswerUnusable))
	}
	if err != nil {
		return Credential{}, errors.Join(err, r.dao.ReleaseRefreshLease(call, c.TeamSpaceID, c.ConnectionID, owner))
	}
	written, err := r.objects.write(call, c.TeamSpaceID, c.ConnectionID, next)
	if err != nil {
		return Credential{}, errors.Join(err, r.dao.ReleaseRefreshLease(call, c.TeamSpaceID, c.ConnectionID, owner))
	}
	committed, err := r.objects.commit(call, r.dao, c, owner, written, next, "")
	if err != nil {
		return Credential{}, err
	}
	return ready(committed, next, d), nil
}

// refreshAnswerUnusable is the reason of a successful refresh answer that
// cannot be applied.
const refreshAnswerUnusable = "the provider's refresh answer cannot be applied"

// requireReauthorization marks c's credential as needing reauthorization for
// reason under owner's lease and deletes the stored answer, if any.
func (r *CredentialReader) requireReauthorization(ctx context.Context, c connectiondao.Connection, owner string, stored *objectstore.ObjectRef, reason string) error {
	err := r.dao.RequireReauthorization(ctx, c.TeamSpaceID, c.ConnectionID, c.CredentialRevision, owner, reauthorizationFailure(reason))
	if stored != nil {
		err = errors.Join(err, r.objects.objects.Delete(ctx, *stored))
	}
	return errors.Join(ErrReauthorizationRequired, err)
}

// grantOutcome returns the outcome of a grant's provider call and the grant
// it belongs to. A successful refresh answer that an earlier claimant stored
// under the same credential revision is applied to the grant it answered,
// whichever grant is due now, without calling a provider: its refresh token is
// spent; an answer that cannot be applied is terminal. Otherwise grant's
// answer is stored before it is parsed. stored is the stored answer's
// reference, when there is one.
func (r *CredentialReader) grantOutcome(ctx context.Context, c connectiondao.Connection, d declarations, grant connectormanifest.TokenGrant,
	credential storedCredential) (grantOutcome, connectormanifest.TokenGrant, *objectstore.ObjectRef, error) {
	var answer storedResponse
	ref, err := r.objects.objects.GetCurrentJSON(ctx, refreshKey(c), &answer)
	switch {
	case err == nil:
		answered, declared := declaredGrant(d.grants, answer.Grant)
		if !declared {
			return grantOutcome{terminal: refreshAnswerUnusable}, grant, &ref, nil
		}
		outcome, err := interpret(d, answered, connectormanifest.TokenResponse{Status: http.StatusOK, Body: answer.Body}, answer.ReceivedAt)
		if err != nil {
			return grantOutcome{terminal: refreshAnswerUnusable}, answered, &ref, nil
		}
		return outcome, answered, &ref, nil
	case !errors.Is(err, objectstore.ErrNotFound):
		return grantOutcome{}, grant, nil, err
	}
	response, err := r.requestGrant(ctx, c, d, grant, credential)
	if err != nil {
		return grantOutcome{}, grant, nil, err
	}
	receivedAt := time.Now().UTC()
	var stored *objectstore.ObjectRef
	if grant.Kind == connectormanifest.GrantRefreshToken && response.Succeeded() {
		answer := storedResponse{Grant: grant.ID, ReceivedAt: receivedAt, Body: response.Body}
		// A failed store leaves the credential object written next as the
		// only copy of the new tokens; the warning names no content.
		if ref, err := r.objects.objects.Put(ctx, refreshKey(c), objectstore.MediaTypeJSON, comm.Must(objectstore.EncodeJSON(answer))); err == nil {
			stored = &ref
		} else {
			r.logger.WarnContext(ctx, "store a refresh answer", "connection", c.ConnectionID, "grant", grant.ID, "error", err.Error())
		}
	}
	outcome, err := interpret(d, grant, response, receivedAt)
	return outcome, grant, stored, err
}

// declaredGrant is the grant of id that grants declare.
func declaredGrant(grants []connectormanifest.TokenGrant, id string) (connectormanifest.TokenGrant, bool) {
	for _, grant := range grants {
		if grant.ID == id {
			return grant, true
		}
	}
	return connectormanifest.TokenGrant{}, false
}

// resolveDeclarations resolves what the manifest version of c declares for
// its method with its settings.
func resolveDeclarations(ctx context.Context, releases *connectormanifest.ReleaseResolver, c connectiondao.Connection) (declarations, error) {
	release, err := releases.Resolve(ctx, connectormanifest.Module{ConnectorID: c.ConnectorID, ModulePath: c.ModulePath, ModuleVersion: c.ManifestVersion})
	if err != nil {
		return declarations{}, fmt.Errorf("resolve %s %s: %w", c.ConnectorID, c.ManifestVersion, err)
	}
	method, found := release.Manifest.Spec.Auth.Method(c.AuthMethodID)
	if !found {
		return declarations{}, fmt.Errorf("%s %s declares no method %q", c.ConnectorID, c.ManifestVersion, c.AuthMethodID)
	}
	hostKept, declared := release.HostKeptCredentials(method.ID)
	if !declared {
		return declarations{}, fmt.Errorf("%w: %s %s", errConnectorVersionUnsupported, c.ConnectorID, c.ManifestVersion)
	}
	d := declarations{method: method, fields: connectormanifest.ConfigurationFields(release.Manifest, method), settings: map[string]json.RawMessage{},
		grants: connectormanifest.Grants(method), hostKept: hostKept}
	if err = json.Unmarshal(c.Settings, &d.settings); err != nil {
		return declarations{}, fmt.Errorf("connection %s has invalid settings: %w", c.ConnectionID, err)
	}
	if method.OAuth2 != nil {
		resolved, err := connectormanifest.ResolveOAuth2(*method.OAuth2, d.fields, d.settings)
		if err != nil {
			return declarations{}, fmt.Errorf("resolve the OAuth declarations of connection %s: %w", c.ConnectionID, err)
		}
		d.oauth = &resolved
	}
	return d, nil
}

// dueGrant returns the grant a read renews first and whether one is due;
// static reports a credential that no grant can renew. Of the due grants,
// the one that expires first goes first; another due grant waits for the next
// read, so a rotating provider is never refreshed needlessly. A forced
// renewal takes the grant issued longest ago, unless it was issued within
// the forced-refresh floor.
func dueGrant(grants []connectormanifest.TokenGrant, credential storedCredential, now time.Time, forced bool) (connectormanifest.TokenGrant, bool, bool) {
	var renewable []connectormanifest.TokenGrant
	for _, grant := range grants {
		if grant.Kind == connectormanifest.GrantRefreshToken && credentialText(credential.Credentials, grant.RefreshTokenCredential) == "" {
			continue
		}
		renewable = append(renewable, grant)
	}
	if len(renewable) == 0 {
		return connectormanifest.TokenGrant{}, false, true
	}
	if forced {
		oldest := renewable[0]
		for _, grant := range renewable[1:] {
			if credential.Grants[grant.ID].IssuedAt.Before(credential.Grants[oldest.ID].IssuedAt) {
				oldest = grant
			}
		}
		times, known := credential.Grants[oldest.ID]
		if known && now.Before(times.IssuedAt.Add(oauthtoken.ForcedRefreshFloor(&times.IssuedAt, times.ExpiresAt))) {
			return connectormanifest.TokenGrant{}, false, false
		}
		return oldest, true, false
	}
	var chosen connectormanifest.TokenGrant
	var chosenUrgency time.Time
	due := false
	for _, grant := range renewable {
		times, known := credential.Grants[grant.ID]
		if known && !oauthtoken.IsRefreshRequiredWithIssueTime(hasAccessToken(grant, credential), &times.IssuedAt, times.ExpiresAt, now, oauthtoken.KeepWhenExpiryMissing) {
			continue
		}
		// A grant never renewed is the most urgent; one without an expiry
		// is forced and comes last.
		var urgency time.Time
		switch {
		case !known:
		case times.ExpiresAt == nil:
			urgency = now.Add(100 * 365 * 24 * time.Hour)
		default:
			urgency = *times.ExpiresAt
		}
		if !due || urgency.Before(chosenUrgency) {
			chosen, chosenUrgency, due = grant, urgency, true
		}
	}
	return chosen, due, false
}

// hasAccessToken reports whether the credential holds the access token that
// grant maps.
func hasAccessToken(grant connectormanifest.TokenGrant, credential storedCredential) bool {
	for _, mapping := range grant.CredentialMappings {
		if mapping.Source == connectormanifest.TokenMemberAccessToken {
			return credentialText(credential.Credentials, mapping.Credential) != ""
		}
	}
	return credentialText(credential.Credentials, connectormanifest.TokenMemberAccessToken) != ""
}

// ready is the credential c references, with the times its row records.
func ready(c connectiondao.Connection, credential storedCredential, d declarations) Credential {
	read := Credential{Values: credential.Credentials, revision: c.CredentialRevision, hostKept: d.hostKept}
	if c.CredentialIssuedAt.Valid {
		read.issuedAt = &c.CredentialIssuedAt.V
	}
	if c.CredentialExpiresAt.Valid {
		read.expiresAt = &c.CredentialExpiresAt.V
	}
	return read
}

// reauthorizationFailure is the safe last_failure of a credential that
// needs a new authorization for reason.
func reauthorizationFailure(reason string) json.RawMessage {
	return comm.Must(json.Marshal(dexflow.Failure{Code: string(failureCredentialRejected), Message: "Authorize the connection again: " + reason + ".", Remedy: dexflow.RemedyReauthorize}))
}

// forApplication is the credential an application reads: every value but
// those the platform keeps.
func (credential Credential) forApplication() map[string]json.RawMessage {
	values := make(map[string]json.RawMessage, len(credential.Values))
	for name, value := range credential.Values {
		if !slices.Contains(credential.hostKept, name) {
			values[name] = value
		}
	}
	return values
}
