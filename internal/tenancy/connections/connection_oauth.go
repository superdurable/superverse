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
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
)

// An OAuth authorization of a connection is one attempt: a seed with the
// private material its callback needs, and the token answer, stored before
// it is read because the spent code cannot reproduce it. Both are immutable
// objects under teamspaces/<ts>/connection-oauth/<connection>/<attempt>/,
// apart from the connection's prefix that a credential write-back empties.
// The attempt is the digest of its OAuth state. A new attempt and a completed
// one delete the connection's other attempts, and a TeamSpace's deletion
// deletes them all. An attempt may create its connection: the seed then
// holds the creation, whose ID it already knows, and the connection is
// stored only once the code exchange answered a credential, so an abandoned
// or refused authorization leaves no connection behind. A platform client's secret never enters a seed: the
// callback takes it from the platform's configuration. The callback exchanges the code under the
// connection's lease, so a concurrent or repeated callback joins the stored
// answer instead of spending the code twice. The new credential replaces
// whatever the connection holds then, also after refreshes during the
// actor's consent.

// oauthAttemptLifetime is how long an attempt's callback is accepted.
const oauthAttemptLifetime = 10 * time.Minute

// errOAuthDenied is a callback whose provider refused the authorization.
var errOAuthDenied = errors.New("the provider refused the authorization")

// oauthSeed is an attempt's private material.
type oauthSeed struct {
	TeamSpaceID, ConnectionID, ActorAccountID string
	// Version is the release whose declarations the authorization follows,
	// which refreshes the connection once it completes.
	Version string
	// ClientSource is the source of the client; ClientSecret is set only for
	// a user client.
	ClientSource                     connectiondao.ClientSource
	ClientID, ClientSecret, Verifier string
	RedirectURI                      string
	CredentialValues                 map[string]json.RawMessage
	CredentialSecrets                map[string]string
	ExpiresAt                        time.Time
	// Creation is the connection the attempt creates once it completes, nil
	// for an existing one.
	Creation *Creation `json:",omitempty"`
}

// oauthCompletion marks a completed attempt, which a repeated callback
// answers with its connection.
type oauthCompletion struct {
	ConnectionID, ActorAccountID string
}

// oauthResult is the token answer of an attempt's one exchange.
type oauthResult struct {
	ReceivedAt time.Time
	Token      map[string]any
}

// oauthConnectionPrefix holds every attempt of a connection.
func oauthConnectionPrefix(teamSpaceID, connectionID string) string {
	return teamSpacePrefix(teamSpaceID) + "connection-oauth/" + connectionID + "/"
}

func oauthAttemptPrefix(teamSpaceID, connectionID, attempt string) string {
	return oauthConnectionPrefix(teamSpaceID, connectionID) + attempt + "/"
}

// maxCleanupRounds bounds the bounded deletions one call repeats.
const maxCleanupRounds = 8

// deleteAttempts deletes a connection's attempts but the keys in keep.
func (t *TeamSpaceConnections) deleteAttempts(ctx context.Context, teamSpaceID, connectionID string, keep ...string) error {
	for range maxCleanupRounds {
		done, err := t.objects.objects.DeletePrefixExcept(ctx, oauthConnectionPrefix(teamSpaceID, connectionID), keep...)
		if err != nil || done {
			return err
		}
	}
	return nil
}

// OAuthStart starts an authorization of a connection: the existing one ConnectionID names, or
// the one Creation creates once the authorization completes. An empty ClientID reuses the user
// client the connection stores; a platform connection takes the platform's.
// Version reauthorizes the connection to a newer release of its major
// version, whose scopes an application declares; empty keeps the release
// that refreshes it.
type OAuthStart struct {
	TeamSpaceID, ConnectionID, ActorAccountID string
	Creation                                  *Creation
	Version                                   string
	ClientID, ClientSecret                    string
	CredentialValues                          map[string]json.RawMessage
	CredentialSecrets                         map[string]string
	// RedirectURI is the Studio callback the provider returns the actor to.
	RedirectURI string
}

// StartOAuth stores a new attempt and returns the provider's authorization
// URL and when the attempt expires. The URL's scheme is https, or http where
// the profile allows plain http; the platform never connects to it.
func (t *TeamSpaceConnections) StartOAuth(ctx context.Context, start OAuthStart) (string, time.Time, error) {
	row, creates, err := t.authorizationTarget(ctx, start)
	if err != nil {
		return "", time.Time{}, err
	}
	version := row.ManifestVersion
	if start.Version != "" && start.Version != version {
		if connectormanifest.VersionLine(start.Version) != row.VersionLine || !connectormanifest.IsNewerVersion(version, start.Version) {
			return "", time.Time{}, fmt.Errorf("%w: a connection is reauthorized only to a newer release of its major version", ErrInvalid)
		}
		if _, _, err = t.method(ctx, connectormanifest.Module{ConnectorID: row.ConnectorID, ModulePath: row.ModulePath, ModuleVersion: start.Version}, row.AuthMethodID); err != nil {
			return "", time.Time{}, err
		}
		version = start.Version
	}
	// Until the authorization completes, the connection keeps refreshing with
	// the release it was authorized for.
	authorized := row
	authorized.ManifestVersion = version
	d, err := resolveDeclarations(ctx, t.releases, authorized)
	if err != nil {
		return "", time.Time{}, err
	}
	if d.oauth == nil {
		return "", time.Time{}, fmt.Errorf("%w: the connection's method does not authorize through OAuth", ErrInvalid)
	}
	if err = connectormanifest.ValidateOAuthCredentials(d.method, start.CredentialValues, start.CredentialSecrets); err != nil {
		return "", time.Time{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if endpoint := comm.Must(url.Parse(d.oauth.AuthorizationEndpoint)); endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && t.allowPrivateHTTP) {
		return "", time.Time{}, fmt.Errorf("%w: the authorization endpoint must use https", ErrInvalid)
	}
	clientID, clientSecret, err := t.startClient(ctx, row, d, start)
	if err != nil {
		return "", time.Time{}, err
	}
	if row.ClientSource == connectiondao.ClientPlatform {
		clientSecret = ""
	}
	state, verifier := randomToken(32), randomToken(32)
	seed := oauthSeed{
		TeamSpaceID: row.TeamSpaceID, ConnectionID: row.ConnectionID, ActorAccountID: start.ActorAccountID, Version: version,
		ClientSource: row.ClientSource, ClientID: clientID, ClientSecret: clientSecret, Verifier: verifier, RedirectURI: start.RedirectURI,
		CredentialValues: start.CredentialValues, CredentialSecrets: start.CredentialSecrets, ExpiresAt: time.Now().UTC().Add(oauthAttemptLifetime),
	}
	if creates {
		seed.Creation = start.Creation
	}
	// A new attempt voids the connection's earlier ones and their secrets.
	if err = t.deleteAttempts(ctx, row.TeamSpaceID, row.ConnectionID); err != nil {
		return "", time.Time{}, fmt.Errorf("delete the earlier OAuth attempts: %w", err)
	}
	attempt := oauthAttempt(state)
	if _, err = t.objects.objects.Put(ctx, oauthAttemptPrefix(row.TeamSpaceID, row.ConnectionID, attempt)+"seed", objectstore.MediaTypeJSON, comm.Must(objectstore.EncodeJSON(seed))); err != nil {
		return "", time.Time{}, fmt.Errorf("store the OAuth attempt: %w", err)
	}
	return connectormanifest.AuthorizationURL(*d.oauth, clientID, start.RedirectURI, teamSpaceState(row.TeamSpaceID, row.ConnectionID, state), verifier), seed.ExpiresAt, nil
}

// authorizationTarget is the connection an authorization starts for: the
// ACTIVE one start names, or the one its creation creates, not yet stored,
// with true. A creation's name must be free now as well as at completion,
// and a creation an earlier attempt of the same request completed
// reauthorizes that connection.
func (t *TeamSpaceConnections) authorizationTarget(ctx context.Context, start OAuthStart) (connectiondao.Connection, bool, error) {
	if start.Creation == nil {
		row, err := t.dao.Connection(ctx, start.TeamSpaceID, start.ConnectionID)
		if err != nil {
			return connectiondao.Connection{}, false, err
		}
		if row.ConnectionStatus != connectiondao.ConnectionActive {
			return connectiondao.Connection{}, false, ErrNotFound
		}
		return row, false, nil
	}
	if start.Version != "" || start.Creation.CopyFrom != "" || len(start.Creation.Credentials) > 0 {
		return connectiondao.Connection{}, false, fmt.Errorf("%w: a new connection's authorization takes its release from the creation, which copies no connection and enters no credentials", ErrInvalid)
	}
	row, _, method, err := t.newConnection(ctx, *start.Creation)
	if err != nil {
		return connectiondao.Connection{}, false, err
	}
	if method.Type != connectormanifest.AuthTypeOAuth2 {
		return connectiondao.Connection{}, false, fmt.Errorf("%w: only a connection of an OAuth method is created by its authorization", ErrInvalid)
	}
	existing, err := t.dao.Connection(ctx, row.TeamSpaceID, row.ConnectionID)
	switch {
	case err == nil && existing.CreationFingerprint != row.CreationFingerprint:
		return connectiondao.Connection{}, false, ErrRequestReused
	case err == nil && existing.ConnectionStatus != connectiondao.ConnectionActive:
		return connectiondao.Connection{}, false, ErrNotFound
	case err == nil:
		return existing, false, nil
	case !errors.Is(err, connectiondao.ErrNotFound):
		return connectiondao.Connection{}, false, err
	}
	taken, err := t.dao.NameTaken(ctx, row.TeamSpaceID, row.ConnectorID, row.VersionLine, row.Name)
	if err != nil {
		return connectiondao.Connection{}, false, err
	}
	if taken {
		return connectiondao.Connection{}, false, connectiondao.ErrNameTaken
	}
	return row, true, nil
}

// startClient returns the client an attempt authorizes with: the platform's
// application, the client the actor entered, or the user client the
// connection stores, which a reauthorization reuses.
func (t *TeamSpaceConnections) startClient(ctx context.Context, row connectiondao.Connection, d declarations, start OAuthStart) (string, string, error) {
	if row.ClientSource == connectiondao.ClientPlatform {
		client, available := t.clients.availableFor(row.ConnectorID, d.method)
		if !available {
			return "", "", fmt.Errorf("%w: no platform OAuth client covers the scopes of %s", ErrInvalid, platformClientKey(row.ConnectorID, d.method.ID))
		}
		return client.ClientID, client.clientSecret, nil
	}
	if strings.TrimSpace(start.ClientID) != "" {
		if connectormanifest.ClientSecretRequired(d.method) && strings.TrimSpace(start.ClientSecret) == "" {
			return "", "", fmt.Errorf("%w: the OAuth client needs its secret", ErrInvalid)
		}
		return start.ClientID, start.ClientSecret, nil
	}
	if len(row.CredentialRef) == 0 {
		return "", "", fmt.Errorf("%w: the connection stores no OAuth client yet", ErrInvalid)
	}
	stored, err := t.objects.read(ctx, row)
	if err != nil {
		return "", "", err
	}
	clientID := credentialText(stored.Credentials, d.oauth.ClientIDCredential)
	if clientID == "" {
		return "", "", fmt.Errorf("%w: the connection stores no OAuth client yet", ErrInvalid)
	}
	return clientID, credentialText(stored.Credentials, d.oauth.ClientSecretCredential), nil
}

// OAuthCallback is the provider's return of an attempt to Studio.
// MayChangeCredential authorizes the completion at the moment it takes effect:
// the actor must still be allowed to change the connection's credential.
type OAuthCallback struct {
	ActorAccountID, State, Code, ProviderError, RedirectURI string
	MayChangeCredential                                     func(ctx context.Context, teamSpaceID, connectionID string) error
}

// CompleteOAuth exchanges an attempt's code and makes the result the
// connection's READY credential, or returns the connection when the attempt
// already completed. An attempt of another actor, a stale callback origin, an
// expired attempt or a connection whose credential moved on is ErrInvalid.
func (t *TeamSpaceConnections) CompleteOAuth(ctx context.Context, callback OAuthCallback) (connectiondao.Connection, error) {
	teamSpaceID, connectionID, state, valid := parseTeamSpaceState(callback.State)
	if !valid {
		return connectiondao.Connection{}, fmt.Errorf("%w: the OAuth state is invalid", ErrInvalid)
	}
	prefix := oauthAttemptPrefix(teamSpaceID, connectionID, oauthAttempt(state))
	var marker oauthCompletion
	if _, err := t.objects.objects.GetCurrentJSON(ctx, prefix+"completed", &marker); err == nil {
		// A repeated callback of a completed attempt, whose secrets are gone.
		if marker.ActorAccountID != callback.ActorAccountID {
			return connectiondao.Connection{}, fmt.Errorf("%w: the OAuth attempt belongs to another actor", ErrInvalid)
		}
		return t.dao.Connection(ctx, teamSpaceID, marker.ConnectionID)
	} else if !errors.Is(err, objectstore.ErrNotFound) {
		return connectiondao.Connection{}, err
	}
	var seed oauthSeed
	if _, err := t.objects.objects.GetCurrentJSON(ctx, prefix+"seed", &seed); err != nil {
		if errors.Is(err, objectstore.ErrNotFound) {
			return connectiondao.Connection{}, fmt.Errorf("%w: the OAuth attempt is unknown", ErrInvalid)
		}
		return connectiondao.Connection{}, err
	}
	if seed.ActorAccountID != callback.ActorAccountID || seed.RedirectURI != callback.RedirectURI || seed.ConnectionID != connectionID || !time.Now().Before(seed.ExpiresAt) {
		return connectiondao.Connection{}, fmt.Errorf("%w: the OAuth attempt belongs to another actor or origin, or expired", ErrInvalid)
	}
	if callback.ProviderError != "" {
		return connectiondao.Connection{}, errOAuthDenied
	}
	if err := callback.MayChangeCredential(ctx, teamSpaceID, seed.ConnectionID); err != nil {
		return connectiondao.Connection{}, err
	}
	if seed.Creation != nil {
		// Until a callback stores it, the connection exists only in the seed.
		if _, err := t.dao.Connection(ctx, teamSpaceID, seed.ConnectionID); errors.Is(err, connectiondao.ErrNotFound) {
			return t.createAuthorized(ctx, seed, callback.Code, prefix)
		} else if err != nil {
			return connectiondao.Connection{}, err
		}
	}
	row, owner, err := t.claimForAuthorization(ctx, seed)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	authorized := row
	authorized.ManifestVersion = seed.Version
	d, err := resolveDeclarations(ctx, t.releases, authorized)
	if err == nil {
		var completed connectiondao.Connection
		if completed, err = t.exchange(ctx, row, d, seed, callback.Code, prefix, owner); err == nil {
			return completed, nil
		}
	}
	// The connection shows why its authorization failed; the message names
	// the step and the provider's error code, never token material.
	failure := comm.Must(json.Marshal(dexflow.Failure{Code: string(failureAuthorizationFailed), Message: comm.TailUTF8(err.Error(), maxFailureMessageBytes), Remedy: dexflow.RemedyReauthorize}))
	return connectiondao.Connection{}, errors.Join(err, t.dao.RecordAuthorizationFailure(context.WithoutCancel(ctx), row.TeamSpaceID, row.ConnectionID, owner, failure))
}

// maxFailureMessageBytes bounds a recorded failure's message.
const maxFailureMessageBytes = 1024

// claimForAuthorization takes the lease of the attempt's connection at its
// current credential revision, waiting while a read renews it.
func (t *TeamSpaceConnections) claimForAuthorization(ctx context.Context, seed oauthSeed) (connectiondao.Connection, string, error) {
	ctx, cancel := context.WithTimeout(ctx, readBudget)
	defer cancel()
	owner := flowid.RandomID(24)
	for {
		row, err := t.dao.Connection(ctx, seed.TeamSpaceID, seed.ConnectionID)
		if err != nil {
			return connectiondao.Connection{}, "", err
		}
		if row.ConnectionStatus != connectiondao.ConnectionActive {
			return connectiondao.Connection{}, "", ErrNotFound
		}
		err = t.dao.ClaimAuthorizationLease(ctx, row.TeamSpaceID, row.ConnectionID, row.CredentialRevision, owner, refreshLease)
		if err == nil {
			return row, owner, nil
		}
		if !errors.Is(err, connectiondao.ErrRevisionChanged) {
			return connectiondao.Connection{}, "", err
		}
		select {
		case <-ctx.Done():
			return connectiondao.Connection{}, "", fmt.Errorf("%w: the connection is being renewed: %w", errNoAnswer, ctx.Err())
		case <-time.After(leasePoll):
		}
	}
}

// createAuthorized completes the attempt of a new connection: the exchange
// answers the credential first, and only then is the connection stored and
// the credential committed to it, so a refused code leaves no connection and
// the attempt usable. A repeated callback finds the stored connection and
// takes the path of an existing one.
func (t *TeamSpaceConnections) createAuthorized(ctx context.Context, seed oauthSeed, code, prefix string) (connectiondao.Connection, error) {
	row, _, _, err := t.newConnection(ctx, *seed.Creation)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	d, err := resolveDeclarations(ctx, t.releases, row)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	answer, err := t.answer(ctx, row, d, seed, code, prefix)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	if _, err = t.insert(ctx, row); err != nil {
		return connectiondao.Connection{}, err
	}
	created, owner, err := t.claimForAuthorization(ctx, seed)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	return t.commit(ctx, created, seed, answer, prefix, owner)
}

// exchange spends the code, or reuses the stored answer of an earlier
// callback, and commits the credential under the lease owner holds.
func (t *TeamSpaceConnections) exchange(ctx context.Context, row connectiondao.Connection, d declarations, seed oauthSeed, code, prefix, owner string) (connectiondao.Connection, error) {
	answer, err := t.answer(ctx, row, d, seed, code, prefix)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	return t.commit(ctx, row, seed, answer, prefix, owner)
}

// authorizationAnswer is the credential an attempt's exchange answered, with
// the provider account it installed in.
type authorizationAnswer struct {
	credential storedCredential
	account    *connectiondao.ProviderAccount
}

// answer spends the code, or reuses the stored answer of an earlier callback,
// and returns the credential it grants; it writes nothing to the connection.
func (t *TeamSpaceConnections) answer(ctx context.Context, row connectiondao.Connection, d declarations, seed oauthSeed, code, prefix string) (authorizationAnswer, error) {
	clientSecret := seed.ClientSecret
	if seed.ClientSource == connectiondao.ClientPlatform {
		client, available := t.clients.availableFor(row.ConnectorID, d.method)
		if !available || client.ClientID != seed.ClientID {
			return authorizationAnswer{}, fmt.Errorf("%w: the platform OAuth client of the attempt is no longer configured", ErrInvalid)
		}
		clientSecret = client.clientSecret
	}
	var result oauthResult
	if _, err := t.objects.objects.GetCurrentJSON(ctx, prefix+"result", &result); errors.Is(err, objectstore.ErrNotFound) {
		token, err := connectormanifest.ExchangeAuthorizationCode(ctx, t.providers, *d.oauth, seed.ClientID, clientSecret, code, seed.RedirectURI, seed.Verifier)
		if err != nil {
			return authorizationAnswer{}, fmt.Errorf("%w: the code exchange failed; authorize again: %w", ErrInvalid, err)
		}
		result = oauthResult{ReceivedAt: time.Now().UTC(), Token: token}
		if _, err = t.objects.objects.Put(ctx, prefix+"result", objectstore.MediaTypeJSON, comm.Must(objectstore.EncodeJSON(result))); err != nil {
			return authorizationAnswer{}, fmt.Errorf("store the OAuth answer: %w", err)
		}
	} else if err != nil {
		return authorizationAnswer{}, err
	}
	account, err := slackInstallation(row, result.Token)
	if err != nil {
		return authorizationAnswer{}, err
	}
	grant := connectormanifest.AuthorizationGrant{
		Method: d.method, OAuth: *d.oauth, ClientID: seed.ClientID, ClientSecret: clientSecret, StoresClient: row.ClientSource == connectiondao.ClientUser,
		CredentialValues: seed.CredentialValues, CredentialSecrets: seed.CredentialSecrets,
	}
	credentials, expiresAt, err := grant.Credentials(ctx, t.providers, result.Token, result.ReceivedAt)
	if err != nil {
		return authorizationAnswer{}, fmt.Errorf("%w: authorize again: %w", ErrInvalid, err)
	}
	credential := storedCredential{Credentials: credentials, Grants: map[string]grantTimes{}}
	for _, renewal := range d.grants {
		credential.Grants[renewal.ID] = grantTimes{IssuedAt: result.ReceivedAt, ExpiresAt: expiresAt}
	}
	return authorizationAnswer{credential: credential, account: account}, nil
}

// commit stores an answered credential as the connection's READY one under
// the lease owner holds, and leaves only the attempt's completion marker.
func (t *TeamSpaceConnections) commit(ctx context.Context, row connectiondao.Connection, seed oauthSeed, answer authorizationAnswer, prefix, owner string) (connectiondao.Connection, error) {
	credential := answer.credential
	written, err := t.objects.write(ctx, row.TeamSpaceID, row.ConnectionID, credential)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	committed, err := t.objects.commitAccount(ctx, t.dao, row, owner, written, credential, seed.ActorAccountID, connectiondao.CredentialReady, answer.account)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	if seed.Version != row.ManifestVersion {
		// The new credential has the scopes of the release it was
		// authorized for, which refreshes it from now on.
		if _, err = t.dao.AdvanceManifestVersion(ctx, row.TeamSpaceID, row.ConnectionID, seed.Version, connectormanifest.IsNewerVersion); err != nil {
			return connectiondao.Connection{}, err
		}
		if committed, err = t.dao.Connection(ctx, row.TeamSpaceID, row.ConnectionID); err != nil {
			return connectiondao.Connection{}, err
		}
	}
	// The marker lets a repeated callback answer with the connection; every
	// other object of the connection's attempts holds secrets and leaves once
	// the credential holds them. A failed cleanup leaves them to the next
	// attempt or the TeamSpace's deletion.
	completion := oauthCompletion{ConnectionID: row.ConnectionID, ActorAccountID: seed.ActorAccountID}
	if marker, err := t.objects.objects.Put(ctx, prefix+"completed", objectstore.MediaTypeJSON, comm.Must(objectstore.EncodeJSON(completion))); err == nil {
		_ = t.deleteAttempts(ctx, row.TeamSpaceID, row.ConnectionID, marker.Key)
	}
	return committed, nil
}

// teamSpaceStatePrefix marks the OAuth state of a TeamSpace connection's
// attempt, which carries the TeamSpace and the connection so the callback
// finds its objects.
const teamSpaceStatePrefix = "tsc."

func teamSpaceState(teamSpaceID, connectionID, state string) string {
	return teamSpaceStatePrefix + teamSpaceID + "." + connectionID + "." + state
}

func parseTeamSpaceState(value string) (teamSpaceID, connectionID, state string, valid bool) {
	rest, found := strings.CutPrefix(value, teamSpaceStatePrefix)
	if !found {
		return "", "", "", false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 || !flowid.IsLowerAlphanumeric(parts[0]) || !flowid.IsLowerAlphanumeric(parts[1]) || len(parts[2]) != 43 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// oauthAttempt is the attempt ID of an OAuth state.
func oauthAttempt(state string) string {
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:])
}

// randomToken returns byteCount random bytes, base64url-encoded.
func randomToken(byteCount int) string {
	value := make([]byte, byteCount)
	_, _ = rand.Read(value) // crypto/rand.Read never fails; it crashes the program instead.
	return base64.RawURLEncoding.EncodeToString(value)
}

// SlackConnectorID is the connector whose authorizations record the Slack
// workspace they installed in, which routes the platform Slack
// application's relayed events.
const SlackConnectorID = "slack"

// slackInstallation is the workspace and Enterprise Grid organization of a
// Slack connection's oauth.v2.access answer, nil for another connector. A
// platform connection refuses an org-level installation, which has no
// workspace to route events by.
func slackInstallation(row connectiondao.Connection, token map[string]any) (*connectiondao.ProviderAccount, error) {
	if row.ConnectorID != SlackConnectorID {
		return nil, nil
	}
	if enterpriseInstall, _ := token["is_enterprise_install"].(bool); enterpriseInstall && row.ClientSource == connectiondao.ClientPlatform {
		return nil, fmt.Errorf("%w: Slack installed the application for the whole Enterprise Grid organization; install it in a workspace instead", ErrInvalid)
	}
	id := func(field string) string {
		object, _ := token[field].(map[string]any)
		value, _ := object["id"].(string)
		return value
	}
	team := id("team")
	switch {
	case team == "" && row.ClientSource == connectiondao.ClientPlatform:
		return nil, fmt.Errorf("%w: Slack's answer names no workspace; authorize again", ErrInvalid)
	case team == "":
		return nil, nil
	}
	account := &connectiondao.ProviderAccount{AccountID: team}
	if enterprise := id("enterprise"); enterprise != "" {
		account.OrganizationID = sql.Null[string]{V: enterprise, Valid: true}
	}
	return account, nil
}
