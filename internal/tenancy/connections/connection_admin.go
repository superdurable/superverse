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
	"maps"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
)

// A TeamSpace configures a connector once per major version as a named
// connection, and its Projects select connections per connection name and
// scope. Creating, editing, selecting and deleting are requests: one
// transaction enforces each rule, and the API authorizes every caller.

var (
	// ErrInvalid is a request that the release, the connection or a rule
	// refuses; it wraps the reason.
	ErrInvalid = errors.New("invalid connection request")
	// errScopesDiffer is a selection whose declared release asks for other
	// OAuth scopes than the release the connection was authorized for; it
	// changes nothing until the connection is reauthorized to that release.
	errScopesDiffer = errors.New("the connection was authorized for other OAuth scopes")
	// ErrRequestReused is a creation whose request ID created another
	// connection.
	ErrRequestReused = errors.New("the request ID created another connection; create with another one")
	// ErrCredentialChangeForbidden is a credential change that the update's
	// MayChangeCredential does not admit.
	ErrCredentialChangeForbidden = errors.New("live versions of Projects you do not build use this connection; a TeamSpace admin or a builder of every one of them changes its credential")
	// ErrNotFound is an absent connection. The other answers of the database
	// are connectiondao's: a stale revision fence, a name another connection
	// holds, and a deletion of a used connection.
	ErrNotFound = connectiondao.ErrNotFound
)

// RefusalReason is a refusal Studio branches on: the code of a configuration
// document and the ErrorInfo reason of the API, with what the person can do
// about it.
type RefusalReason struct {
	name   string
	remedy dexflow.FailureRemedy
}

func (reason RefusalReason) String() string { return reason.name }

// Remedy is what the person can do about the refusal.
func (reason RefusalReason) Remedy() dexflow.FailureRemedy { return reason.remedy }

// The refusals of connection requests, each with the error it names.
var (
	ReasonConnectorVersionUnsupported = RefusalReason{"CONNECTOR_VERSION_UNSUPPORTED", dexflow.RemedyOpenSetup}
	ReasonScopesDiffer                = RefusalReason{"CONNECTION_SCOPES_DIFFER", dexflow.RemedyReauthorize}
	ReasonConnectionDeleted           = RefusalReason{"CONNECTION_DELETED", dexflow.RemedyOpenSetup}
	ReasonConnectionInUse             = RefusalReason{"CONNECTION_IN_USE", dexflow.RemedyOpenSetup}
	ReasonConnectionNameTaken         = RefusalReason{"CONNECTION_NAME_TAKEN", dexflow.RemedyOpenSetup}
	ReasonOAuthDenied                 = RefusalReason{"OAUTH_DENIED", dexflow.RemedyReauthorize}
)

// refusals pairs each refusal with its error, in the order Refusal tests them:
// the more specific first.
var refusals = []struct {
	err    error
	reason RefusalReason
}{
	{errConnectorVersionUnsupported, ReasonConnectorVersionUnsupported},
	{errScopesDiffer, ReasonScopesDiffer},
	{ErrConnectionDeleted, ReasonConnectionDeleted},
	{connectiondao.ErrUnavailable, ReasonConnectionDeleted},
	{connectiondao.ErrInUse, ReasonConnectionInUse},
	{connectiondao.ErrNameTaken, ReasonConnectionNameTaken},
	{errOAuthDenied, ReasonOAuthDenied},
}

// Refusal returns the refusal err names, if any.
func Refusal(err error) (RefusalReason, bool) {
	for _, refusal := range refusals {
		if errors.Is(err, refusal.err) {
			return refusal.reason, true
		}
	}
	return RefusalReason{}, false
}

// TeamSpaceConnections manages the connections of TeamSpaces.
type TeamSpaceConnections struct {
	dao       *connectiondao.ConnectionDAO
	objects   credentialObjects
	releases  *connectormanifest.ReleaseResolver
	catalog   *connectormanifest.Catalog
	clients   PlatformClients
	providers *http.Client
	usages    *Usages
	// reader reads the credential a form's Studio commands send.
	reader *CredentialReader
	// commands runs forms' Studio commands through the provider client.
	commands *http.Client
	// formKey is the root key from which the key of the forms' nonces
	// derives; a secret.
	formKey []byte
	// allowPrivateHTTP lets an authorization endpoint use plain http, as the
	// local profiles' address policy does.
	allowPrivateHTTP bool
}

// NewTeamSpaceConnections manages connections with dao and objects, resolves
// releases and the catalog, reads credentials with reader and calls providers
// with the provider client; formKey is the root key of the forms' nonces.
func NewTeamSpaceConnections(dao *connectiondao.ConnectionDAO, objects *objectstore.Store, releases *connectormanifest.ReleaseResolver, catalog *connectormanifest.Catalog,
	clients PlatformClients, providers *http.Client, reader *CredentialReader, formKey []byte, allowPrivateHTTP bool) *TeamSpaceConnections {
	return &TeamSpaceConnections{dao: dao, objects: credentialObjects{objects: objects}, releases: releases, catalog: catalog, clients: clients,
		providers: providers, usages: NewUsages(dao, objects), reader: reader, commands: connectormanifest.StudioCommandClient(providers),
		formKey: formKey, allowPrivateHTTP: allowPrivateHTTP}
}

// Creation is a new connection of a release or release candidate. Settings
// and Credentials hold the values the creator entered; an OAuth method takes
// its credentials only from an authorization. CopyFrom copies a connection of
// another major version instead: its settings unless Settings is set, and its
// user OAuth client.
type Creation struct {
	TeamSpaceID, RequestID, ActorAccountID string
	Name                                   string
	Release                                connectormanifest.Module
	AuthMethodID                           string
	ClientSource                           connectiondao.ClientSource
	Settings, Credentials                  map[string]json.RawMessage
	CopyFrom                               string
}

// Create creates the connection, or returns the one an earlier delivery of
// the same request created; the same request ID with other content is
// ErrInvalid. A connection of an OAuth method that copies none is created by
// its authorization instead (OAuthStart.Creation), so that one which never
// completes leaves none behind.
func (t *TeamSpaceConnections) Create(ctx context.Context, creation Creation) (connectiondao.Connection, error) {
	row, credentials, method, err := t.newConnection(ctx, creation)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	if method.Type == connectormanifest.AuthTypeOAuth2 && creation.CopyFrom == "" {
		return connectiondao.Connection{}, fmt.Errorf("%w: an OAuth connection is created when its authorization completes; start the authorization with the new connection", ErrInvalid)
	}
	created, err := t.insert(ctx, row)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	if created.CredentialRevision != 0 || credentials == nil {
		return created, nil
	}
	status := connectiondao.CredentialReady
	if method.Type == connectormanifest.AuthTypeOAuth2 {
		// A copied client still needs the new major version's authorization.
		status = connectiondao.CredentialAuthorizing
	}
	return t.replaceCredential(ctx, created, created.CredentialRevision, storedCredential{Credentials: credentials}, status, creation.ActorAccountID)
}

// newConnection validates a creation and returns the connection it creates,
// not yet stored, with the credentials it starts from (nil for none) and its
// method.
func (t *TeamSpaceConnections) newConnection(ctx context.Context, creation Creation) (connectiondao.Connection, map[string]json.RawMessage, connectormanifest.ManifestAuthMethod, error) {
	release, method, err := t.method(ctx, creation.Release, creation.AuthMethodID)
	if err != nil {
		return connectiondao.Connection{}, nil, method, err
	}
	source, credentials, err := t.copySource(ctx, creation, method)
	if err != nil {
		return connectiondao.Connection{}, nil, method, err
	}
	settings := creation.Settings
	if settings == nil {
		settings = map[string]json.RawMessage{}
		if source != nil {
			comm.MustDo(json.Unmarshal(source.Settings, &settings))
		}
	}
	settings = connectormanifest.DeclaredSettings(connectormanifest.ConfigurationFields(release.Manifest, method), settings)
	if err = t.validate(creation.Release.ConnectorID, release.Manifest, method, settings, creation.ClientSource); err != nil {
		return connectiondao.Connection{}, nil, method, err
	}
	switch {
	case method.Type == connectormanifest.AuthTypeOAuth2 && len(creation.Credentials) > 0:
		return connectiondao.Connection{}, nil, method, fmt.Errorf("%w: an OAuth connection takes its credentials from an authorization", ErrInvalid)
	case method.Type != connectormanifest.AuthTypeOAuth2 && source == nil:
		// A method without credential fields, such as a public feed's, is READY with none.
		credentials = maps.Clone(creation.Credentials)
		if credentials == nil {
			credentials = map[string]json.RawMessage{}
		}
		if err = connectormanifest.ValidateCredentialFields(method.Fields, credentials, nil); err != nil {
			return connectiondao.Connection{}, nil, method, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		connectormanifest.StampAuthMethod(credentials, method.ID)
	}
	clientSource := creation.ClientSource
	if clientSource == "" {
		clientSource = connectiondao.ClientUser
	}
	now := time.Now().UTC()
	return connectiondao.Connection{
		TeamSpaceID: creation.TeamSpaceID, ConnectionID: flowid.TeamSpaceConnectionCreationID(creation.ActorAccountID, creation.RequestID),
		CreationFingerprint: flowid.Fingerprint(struct {
			TeamSpaceID, Name, ConnectorID, ModulePath, Version, AuthMethodID, CopyFrom string
			ClientSource                                                                connectiondao.ClientSource
			Settings                                                                    map[string]json.RawMessage
		}{creation.TeamSpaceID, creation.Name, creation.Release.ConnectorID, creation.Release.ModulePath, creation.Release.ModuleVersion, method.ID, creation.CopyFrom, clientSource, settings}),
		Name: creation.Name, ConnectorID: creation.Release.ConnectorID, ModulePath: creation.Release.ModulePath,
		VersionLine: connectormanifest.VersionLine(creation.Release.ModuleVersion), ManifestVersion: creation.Release.ModuleVersion,
		AuthMethodID: method.ID, ClientSource: clientSource, Settings: comm.Must(json.Marshal(settings)), Revision: 1,
		CredentialStatus: connectiondao.CredentialAuthorizing, ConnectionStatus: connectiondao.ConnectionActive,
		CreatorAccountID: creation.ActorAccountID, UpdatedByAccountID: creation.ActorAccountID, CreatedAt: now, UpdatedAt: now,
	}, credentials, method, nil
}

// insert stores a new connection, or returns the one an earlier delivery of
// the same creation stored.
func (t *TeamSpaceConnections) insert(ctx context.Context, row connectiondao.Connection) (connectiondao.Connection, error) {
	created, _, err := t.dao.Create(ctx, row)
	if errors.Is(err, connectiondao.ErrIDTaken) {
		return connectiondao.Connection{}, ErrRequestReused
	}
	if err != nil {
		return connectiondao.Connection{}, err
	}
	if created.CreationFingerprint != row.CreationFingerprint {
		return connectiondao.Connection{}, ErrRequestReused
	}
	return created, nil
}

// copySource returns the connection a creation copies, nil for none, with
// the credential the copy starts from: the user OAuth client of an OAuth
// method, which needs a new authorization, and nothing of another method,
// whose credentials are entered again.
func (t *TeamSpaceConnections) copySource(ctx context.Context, creation Creation, method connectormanifest.ManifestAuthMethod) (*connectiondao.Connection, map[string]json.RawMessage, error) {
	if creation.CopyFrom == "" {
		return nil, nil, nil
	}
	source, err := t.dao.Connection(ctx, creation.TeamSpaceID, creation.CopyFrom)
	if err != nil {
		return nil, nil, err
	}
	if source.ConnectionStatus != connectiondao.ConnectionActive || source.ConnectorID != creation.Release.ConnectorID ||
		source.VersionLine == connectormanifest.VersionLine(creation.Release.ModuleVersion) {
		return nil, nil, fmt.Errorf("%w: a copy takes an ACTIVE connection of the same connector and another major version", ErrInvalid)
	}
	if method.Type != connectormanifest.AuthTypeOAuth2 || source.ClientSource != connectiondao.ClientUser || len(source.CredentialRef) == 0 {
		return &source, nil, nil
	}
	stored, err := t.objects.read(ctx, source)
	if err != nil {
		return nil, nil, err
	}
	credentials := map[string]json.RawMessage{}
	for _, name := range []string{method.OAuth2.ClientIDCredential, method.OAuth2.ClientSecretCredential} {
		if value, found := stored.Credentials[name]; found && name != "" {
			credentials[name] = value
		}
	}
	return &source, credentials, nil
}

// ConnectionUpdate is an edit of a connection's settings, or of the credential of a
// method whose credentials are entered. Settings nil keeps the settings;
// Credentials nil keeps the credential, and KeepFields keeps single values.
// MayChangeCredential admits a credential change, which replacing the
// credential and editing a setting that a grant reads both are.
type ConnectionUpdate struct {
	TeamSpaceID, ConnectionID, ActorAccountID    string
	ExpectedRevision, ExpectedCredentialRevision uint64
	Settings, Credentials                        map[string]json.RawMessage
	KeepFields                                   []string
	MayChangeCredential                          func(context.Context) (bool, error)
}

// Update applies an edit. Settings validate against the newest release of the
// connection's major version the catalog lists, which then refreshes it,
// unless it declares other OAuth scopes than the credential carries.
// Editing a setting that a grant reads, such as the token endpoint or the
// scopes, is a credential change and needs a new authorization.
func (t *TeamSpaceConnections) Update(ctx context.Context, update ConnectionUpdate) (connectiondao.Connection, error) {
	row, err := t.dao.Connection(ctx, update.TeamSpaceID, update.ConnectionID)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	if row.ConnectionStatus != connectiondao.ConnectionActive {
		return connectiondao.Connection{}, ErrNotFound
	}
	if update.Credentials != nil {
		if err = admitCredentialChange(ctx, update); err != nil {
			return connectiondao.Connection{}, err
		}
		if row.CredentialRevision != update.ExpectedCredentialRevision {
			return connectiondao.Connection{}, connectiondao.ErrRevisionChanged
		}
	}
	if update.Settings != nil {
		if row, err = t.updateSettings(ctx, row, update); err != nil {
			return connectiondao.Connection{}, err
		}
	}
	if update.Credentials == nil {
		return row, nil
	}
	_, method, err := t.method(ctx, connectormanifest.Module{ConnectorID: row.ConnectorID, ModulePath: row.ModulePath, ModuleVersion: row.ManifestVersion}, row.AuthMethodID)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	if method.Type == connectormanifest.AuthTypeOAuth2 {
		return connectiondao.Connection{}, fmt.Errorf("%w: an OAuth connection replaces its credentials by authorizing again", ErrInvalid)
	}
	credentials := maps.Clone(update.Credentials)
	if _, set := credentials[connectormanifest.AuthMethodCredential]; set {
		return connectiondao.Connection{}, fmt.Errorf("%w: credentials must not set the authentication method", ErrInvalid)
	}
	kept := map[string]bool{}
	if len(update.KeepFields) > 0 {
		if len(row.CredentialRef) == 0 {
			return connectiondao.Connection{}, connectiondao.ErrRevisionChanged
		}
		current, err := t.objects.read(ctx, row)
		if err != nil {
			return connectiondao.Connection{}, err
		}
		for _, name := range update.KeepFields {
			value, found := current.Credentials[name]
			if _, set := credentials[name]; !found || set {
				return connectiondao.Connection{}, fmt.Errorf("%w: credential field %q cannot be kept", ErrInvalid, name)
			}
			credentials[name], kept[name] = value, true
		}
	}
	if err = connectormanifest.ValidateCredentialFields(method.Fields, credentials, nil); err != nil {
		return connectiondao.Connection{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	connectormanifest.StampAuthMethod(credentials, method.ID)
	// A settings change in the same update may have advanced the revision.
	return t.replaceCredential(ctx, row, row.CredentialRevision, storedCredential{Credentials: credentials}, connectiondao.CredentialReady, update.ActorAccountID)
}

// admitCredentialChange refuses a credential change that the update does not
// admit.
func admitCredentialChange(ctx context.Context, update ConnectionUpdate) error {
	admitted, err := update.MayChangeCredential(ctx)
	if err != nil {
		return err
	}
	if !admitted {
		return ErrCredentialChangeForbidden
	}
	return nil
}

// updateSettings validates and stores new settings.
func (t *TeamSpaceConnections) updateSettings(ctx context.Context, row connectiondao.Connection, update ConnectionUpdate) (connectiondao.Connection, error) {
	module := connectormanifest.Module{ConnectorID: row.ConnectorID, ModulePath: row.ModulePath, ModuleVersion: row.ManifestVersion}
	release, current, err := t.method(ctx, module, row.AuthMethodID)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	method := current
	// A newer release refreshes the connection only when the credential
	// carries the scopes it declares.
	if latest, found, err := t.catalog.LatestRelease(ctx, row.ConnectorID, row.VersionLine); err != nil {
		return connectiondao.Connection{}, err
	} else if found && latest.ModulePath == row.ModulePath && connectormanifest.IsNewerVersion(row.ManifestVersion, latest.Version) {
		module.ModuleVersion = latest.Version
		newer, newerMethod, err := t.method(ctx, module, row.AuthMethodID)
		if err != nil {
			return connectiondao.Connection{}, err
		}
		if sameScopes(current, newerMethod) {
			release, method = newer, newerMethod
		} else {
			module.ModuleVersion = row.ManifestVersion
		}
	}
	settings := connectormanifest.DeclaredSettings(connectormanifest.ConfigurationFields(release.Manifest, method), update.Settings)
	if err = t.validate(row.ConnectorID, release.Manifest, method, settings, row.ClientSource); err != nil {
		return connectiondao.Connection{}, err
	}
	var previous map[string]json.RawMessage
	comm.MustDo(json.Unmarshal(row.Settings, &previous))
	var reauthorization json.RawMessage
	for _, name := range slices.Concat(connectormanifest.CredentialSettings(current), connectormanifest.CredentialSettings(method)) {
		if !sameJSON(previous[name], settings[name]) {
			// The credential would reach another endpoint or carry other
			// scopes: the change needs the right to change the credential.
			if err = admitCredentialChange(ctx, update); err != nil {
				return connectiondao.Connection{}, err
			}
			reauthorization = reauthorizationFailure("a setting its grants read changed")
			break
		}
	}
	return t.dao.UpdateSettings(ctx, row.TeamSpaceID, row.ConnectionID, update.ExpectedRevision, comm.Must(json.Marshal(settings)), module.ModuleVersion, update.ActorAccountID, reauthorization)
}

// Delete deletes an unused connection at the expected revision with its
// credential objects; deleting a deleted connection removes what is left.
func (t *TeamSpaceConnections) Delete(ctx context.Context, teamSpaceID, connectionID string, expectedRevision uint64, actorAccountID, deletedProjectStatus string) error {
	if err := t.dao.Delete(ctx, teamSpaceID, connectionID, expectedRevision, actorAccountID, deletedProjectStatus); err != nil {
		return err
	}
	for {
		done, err := t.objects.objects.DeletePrefix(ctx, connectionPrefix(teamSpaceID, connectionID))
		if err != nil || done {
			return err
		}
	}
}

// Connections returns a page of a TeamSpace's Connections with their usages.
func (t *TeamSpaceConnections) Connections(ctx context.Context, teamSpaceID, connectorID string, statuses []connectiondao.ConnectionStatus, after *postgres.PageKey,
	limit int) ([]connectiondao.Connection, []connectiondao.Usage, *postgres.PageKey, error) {
	page, next, err := t.dao.Connections(ctx, teamSpaceID, connectorID, statuses, after, limit)
	if err != nil {
		return nil, nil, nil, err
	}
	ids := make([]string, len(page))
	for index, c := range page {
		ids[index] = c.ConnectionID
	}
	usages, err := t.dao.Usages(ctx, teamSpaceID, ids)
	return page, usages, next, err
}

// Connection returns a Connection with its usages and the names of its
// stored credential values, never their contents.
func (t *TeamSpaceConnections) Connection(ctx context.Context, teamSpaceID, connectionID string) (connectiondao.Connection, []connectiondao.Usage, []string, error) {
	c, err := t.dao.Connection(ctx, teamSpaceID, connectionID)
	if err != nil {
		return connectiondao.Connection{}, nil, nil, err
	}
	usages, err := t.dao.Usages(ctx, teamSpaceID, []string{connectionID})
	if err != nil || len(c.CredentialRef) == 0 {
		return c, usages, nil, err
	}
	stored, err := t.objects.read(ctx, c)
	if err != nil {
		return connectiondao.Connection{}, nil, nil, err
	}
	var fields []string
	for name := range stored.Credentials {
		if name != connectormanifest.AuthMethodCredential {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	return c, usages, fields, nil
}

// Catalog returns the connectors a TeamSpace may create connections of: the
// published catalog's released connectors at their latest release, with the
// logos their releases publish.
func (t *TeamSpaceConnections) Catalog(ctx context.Context) ([]connectormanifest.CatalogEntry, error) {
	entries, err := t.catalog.Entries(ctx)
	if err != nil {
		return nil, err
	}
	return t.catalog.WithLogos(ctx, entries), nil
}

// Selection is the choice of a connection for one connection name of a
// Project scope, at the release the application declares for it.
type Selection struct {
	TeamSpaceID, ConnectionID, ProjectID string
	Scope                                infra.ScopeKind
	Declared                             connectormanifest.Module
	DeclaredMethodID                     string
}

// Select admits a selection and returns the connection with its settings as
// the declared release reads them, which the scope's configuration entry
// freezes. The connection must be ACTIVE, of the declared connector, major
// version and method, and its settings must validate against the declared
// release. A declared release that asks for other OAuth scopes than the
// release the connection was authorized for is refused and changes nothing:
// the connection must be reauthorized to that release first. A newer declared
// release refreshes the connection from then on. A Preview selection records
// the scope's usage at once; a Live one at the deployment.
func (t *TeamSpaceConnections) Select(ctx context.Context, selection Selection) (connectiondao.Connection, map[string]json.RawMessage, error) {
	declaredRelease, declaredMethod, err := t.method(ctx, selection.Declared, selection.DeclaredMethodID)
	if err != nil {
		return connectiondao.Connection{}, nil, err
	}
	row, err := t.dao.Connection(ctx, selection.TeamSpaceID, selection.ConnectionID)
	if err != nil {
		return connectiondao.Connection{}, nil, err
	}
	if row.ConnectionStatus != connectiondao.ConnectionActive || row.ConnectorID != selection.Declared.ConnectorID ||
		row.VersionLine != connectormanifest.VersionLine(selection.Declared.ModuleVersion) || row.AuthMethodID != declaredMethod.ID {
		return connectiondao.Connection{}, nil, fmt.Errorf("%w: the connection is not an ACTIVE connection of the declared connector, major version and method", ErrInvalid)
	}
	var stored map[string]json.RawMessage
	comm.MustDo(json.Unmarshal(row.Settings, &stored))
	fields := connectormanifest.ConfigurationFields(declaredRelease.Manifest, declaredMethod)
	settings := connectormanifest.DeclaredSettings(fields, stored)
	if err = connectormanifest.NormalizeConfigurationFields(fields, settings, t.allowPrivateHTTP); err != nil {
		return connectiondao.Connection{}, nil, fmt.Errorf("%w: the connection's settings do not fit the declared release: %w", ErrInvalid, err)
	}
	_, authorizedMethod, err := t.method(ctx, connectormanifest.Module{ConnectorID: row.ConnectorID, ModulePath: row.ModulePath, ModuleVersion: row.ManifestVersion}, row.AuthMethodID)
	if err != nil {
		return connectiondao.Connection{}, nil, err
	}
	if !sameScopes(declaredMethod, authorizedMethod) {
		return connectiondao.Connection{}, nil, fmt.Errorf("%w: the connection was authorized for the OAuth scopes of %s, and the application declares %s, which needs other scopes; "+
			"someone who may change the connection's credential must reauthorize it to %s", errScopesDiffer, row.ManifestVersion, selection.Declared.ModuleVersion, selection.Declared.ModuleVersion)
	}
	if _, err = t.dao.AdvanceManifestVersion(ctx, row.TeamSpaceID, row.ConnectionID, selection.Declared.ModuleVersion, connectormanifest.IsNewerVersion); err != nil {
		return connectiondao.Connection{}, nil, err
	}
	if selection.Scope == infra.ScopePreview {
		if err = t.usages.addUsage(ctx, selection.TeamSpaceID, row.ConnectionID, selection.ProjectID, infra.ScopePreview); err != nil {
			return connectiondao.Connection{}, nil, err
		}
	}
	return row, settings, nil
}

// SelectedConnection returns the connection a configuration entry selects;
// the selection checked its TeamSpace.
func (t *TeamSpaceConnections) SelectedConnection(ctx context.Context, connectionID string) (connectiondao.Connection, error) {
	return t.dao.ConnectionByID(ctx, connectionID)
}

// FrozenConnection returns the connection a configuration entry selects when
// it belongs to teamSpaceID, the Project's TeamSpace, as a freeze reads it;
// one of another TeamSpace is ErrNotFound.
func (t *TeamSpaceConnections) FrozenConnection(ctx context.Context, teamSpaceID, connectionID string) (connectiondao.Connection, error) {
	return t.dao.Connection(ctx, teamSpaceID, connectionID)
}

// method resolves a release and one of its methods: a published release or
// release candidate, and a declared method.
func (t *TeamSpaceConnections) method(ctx context.Context, module connectormanifest.Module, methodID string) (connectormanifest.Release, connectormanifest.ManifestAuthMethod, error) {
	if !module.IsPublishedRelease() {
		return connectormanifest.Release{}, connectormanifest.ManifestAuthMethod{}, fmt.Errorf("%w: %s %s is not a published release", ErrInvalid, module.ConnectorID, module.ModuleVersion)
	}
	release, err := t.releases.Resolve(ctx, module)
	if err != nil {
		return connectormanifest.Release{}, connectormanifest.ManifestAuthMethod{}, fmt.Errorf("resolve %s %s: %w", module.ConnectorID, module.ModuleVersion, err)
	}
	method, found := release.Manifest.Spec.Auth.Method(methodID)
	if !found {
		return connectormanifest.Release{}, connectormanifest.ManifestAuthMethod{}, fmt.Errorf("%w: %s %s declares no method %q", ErrInvalid, module.ConnectorID, module.ModuleVersion, methodID)
	}
	if _, declared := release.HostKeptCredentials(method.ID); !declared {
		supported, err := t.supportedRelease(ctx, module, method.ID)
		if err != nil {
			return connectormanifest.Release{}, connectormanifest.ManifestAuthMethod{}, err
		}
		return connectormanifest.Release{}, connectormanifest.ManifestAuthMethod{}, fmt.Errorf("%w: %w: %s %s; %s", ErrInvalid, errConnectorVersionUnsupported, module.ConnectorID, module.ModuleVersion, supported)
	}
	return release, method, nil
}

// supportedRelease names, for a refused release, the newest release of its
// connector and major version that lists the credentials a host keeps, the
// version to use instead.
func (t *TeamSpaceConnections) supportedRelease(ctx context.Context, module connectormanifest.Module, methodID string) (string, error) {
	none := "no release of " + module.ConnectorID + " " + connectormanifest.VersionLine(module.ModuleVersion) + " supports them yet"
	latest, found, err := t.catalog.LatestRelease(ctx, module.ConnectorID, connectormanifest.VersionLine(module.ModuleVersion))
	if err != nil {
		return "", fmt.Errorf("find a release of %s that supports TeamSpace connections: %w", module.ConnectorID, err)
	}
	if !found || latest.ModulePath != module.ModulePath || latest.Version == module.ModuleVersion {
		return none, nil
	}
	release, err := t.releases.Resolve(ctx, latest.Module())
	if err != nil {
		return "", fmt.Errorf("resolve %s %s: %w", latest.ConnectorID, latest.Version, err)
	}
	if method, found := release.Manifest.Spec.Auth.Method(methodID); found {
		if _, declared := release.HostKeptCredentials(method.ID); declared {
			return "use " + latest.Version + " or newer", nil
		}
	}
	return none, nil
}

// validate checks a connection's settings and OAuth client against a
// release's method.
func (t *TeamSpaceConnections) validate(connectorID string, manifest connectormanifest.ReleaseManifest, method connectormanifest.ManifestAuthMethod, settings map[string]json.RawMessage, source connectiondao.ClientSource) error {
	if err := connectormanifest.NormalizeConfigurationFields(connectormanifest.ConfigurationFields(manifest, method), settings, t.allowPrivateHTTP); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if method.Type != connectormanifest.AuthTypeOAuth2 {
		if source == connectiondao.ClientPlatform {
			return fmt.Errorf("%w: only an OAuth method uses a platform client", ErrInvalid)
		}
		return nil
	}
	if err := connectormanifest.ValidateOAuthMethod(*method.OAuth2); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, err := connectormanifest.ResolveOAuth2(*method.OAuth2, connectormanifest.ConfigurationFields(manifest, method), settings); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, available := t.clients.availableFor(connectorID, method); source == connectiondao.ClientPlatform && !available {
		return fmt.Errorf("%w: no platform OAuth client covers the scopes of %s", ErrInvalid, platformClientKey(connectorID, method.ID))
	}
	return nil
}

// replaceCredential writes credential as a connection's credential at the
// expected credential revision, under a lease of its own.
func (t *TeamSpaceConnections) replaceCredential(ctx context.Context, row connectiondao.Connection, expectedRevision uint64, credential storedCredential, status connectiondao.CredentialStatus, actor string) (connectiondao.Connection, error) {
	owner := flowid.RandomID(24)
	if err := t.dao.ClaimAuthorizationLease(ctx, row.TeamSpaceID, row.ConnectionID, expectedRevision, owner, refreshLease); err != nil {
		return connectiondao.Connection{}, err
	}
	written, err := t.objects.write(ctx, row.TeamSpaceID, row.ConnectionID, credential)
	if err != nil {
		return connectiondao.Connection{}, errors.Join(err, t.dao.ReleaseRefreshLease(ctx, row.TeamSpaceID, row.ConnectionID, owner))
	}
	row.CredentialRevision = expectedRevision
	committed, err := t.objects.commitWithStatus(ctx, t.dao, row, owner, written, credential, actor, status)
	if err != nil {
		return connectiondao.Connection{}, err
	}
	return committed, nil
}

// sameScopes reports whether two releases of a method declare the same OAuth
// scopes; scopes a setting supplies are the connection's own either way.
func sameScopes(first, second connectormanifest.ManifestAuthMethod) bool {
	if first.OAuth2 == nil || second.OAuth2 == nil {
		return first.OAuth2 == second.OAuth2
	}
	return first.OAuth2.Scopes.FromSetting == second.OAuth2.Scopes.FromSetting && sameSet(first.OAuth2.Scopes.List, second.OAuth2.Scopes.List) &&
		sameSet(first.OAuth2.UserScopes, second.OAuth2.UserScopes)
}

func sameSet(first, second []string) bool {
	first, second = slices.Clone(first), slices.Clone(second)
	slices.Sort(first)
	slices.Sort(second)
	return slices.Equal(slices.Compact(first), slices.Compact(second))
}

// sameJSON compares two JSON values by their compact encoding; two absent
// values are the same.
func sameJSON(first, second json.RawMessage) bool {
	if len(first) == 0 || len(second) == 0 {
		return len(first) == len(second)
	}
	var left, right any
	return json.Unmarshal(first, &left) == nil && json.Unmarshal(second, &right) == nil &&
		string(comm.Must(json.Marshal(left))) == string(comm.Must(json.Marshal(right)))
}
