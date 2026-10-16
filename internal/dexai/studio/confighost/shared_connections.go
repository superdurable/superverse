// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package confighost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/configitems"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// Every connection name of a scope selects a TeamSpace connection: its entry
// names the connection (sharedConnectionId) and carries the connection's
// settings as the declared release reads them, and the document's
// credentialBroker names the platform's credential read endpoint with the
// scope's token. The entry holds no credential. A freeze copies the
// connections' current settings into the entries it freezes, so a Live
// version keeps its settings and the Preview takes changed ones at its next
// start; a Preview freeze also makes the connections it selects the
// Preview's usages.

// selection is the body of a connection name's selection: a TeamSpace
// connection, or on Live's next-publish layer following Preview.
type selection struct {
	TeamSpaceConnectionID string `json:"teamSpaceConnectionId"`
	FollowPreview         bool   `json:"followPreview"`
}

// handleSelectConnection selects the TeamSpace connection of one connection
// name of the request's prepared source: an ACTIVE connection of the
// Project's TeamSpace with the declared connector, major version and method,
// whose OAuth scopes the declared release does not change.
func (host *ConfigurationHost) handleSelectConnection(response http.ResponseWriter, request *http.Request) {
	key := pathConnection(request)
	admission, declaration, admitted := host.admitConnection(response, request, key)
	if !admitted {
		return
	}
	ctx := request.Context()
	var body selection
	if decodeBody(response, request, 1<<10, &body) != nil || (body.TeamSpaceConnectionID == "") != body.FollowPreview {
		writeCodedError(response, http.StatusBadRequest, "CONNECTOR_SELECTION_INVALID", "Connection selection request is invalid")
		return
	}
	if body.FollowPreview {
		host.writeFollowed(response, request, admission, configitems.ConnectionKey(key.ConnectorID, key.ConnectionName),
			map[string]any{"connectorId": key.ConnectorID, "connectionName": key.ConnectionName})
		return
	}
	release, err := host.connectorReleases.Resolve(ctx, declaration.module())
	if err != nil {
		writeReleaseUnavailable(response, err)
		return
	}
	connection, settings, err := host.shared.Connections.Select(ctx, connections.Selection{
		TeamSpaceID: admission.TeamSpaceID, ConnectionID: body.TeamSpaceConnectionID, ProjectID: host.project.scope.ProjectID,
		Scope: host.project.scopeKind(), Declared: declaration.module(), DeclaredMethodID: declaration.AuthMethodID,
	})
	if !writeConnectionError(response, err) {
		return
	}
	draft, err := host.project.loadConnectionDraft(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if draft.document.CredentialBroker, err = host.credentialBroker(ctx, admission); err != nil {
		writeStoreError(response, err)
		return
	}
	draft.replaceConnection(connectorconfig.ConnectionConfiguration{
		ConnectorID: key.ConnectorID, ConnectionName: key.ConnectionName, ModulePath: declaration.ModulePath, Provider: release.Manifest.Spec.Provider,
		AuthMethodID: declaration.AuthMethodID, SharedConnectionID: connection.ConnectionID, Configuration: comm.Must(json.Marshal(settings)),
	})
	if err = draft.save(ctx, admission); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"connectorId": key.ConnectorID, "connectionName": key.ConnectionName, "sharedConnectionId": connection.ConnectionID,
		"configurationRevision": strconv.FormatUint(draft.document.Revision, 10),
	})
}

// credentialBroker is the document section that names the credential read
// endpoint and the scope's token, which the first selection of a scope
// creates as a secret object of the edited configuration.
func (host *ConfigurationHost) credentialBroker(ctx context.Context, request *configurationRequest) (*connectorconfig.CredentialBrokerConfiguration, error) {
	stored, err := host.shared.Usages.ScopeToken(ctx, host.project.scope.ProjectID, host.project.scopeKind(), func(token string) (json.RawMessage, error) {
		reference, err := request.drafts.CreateCredentialBrokerToken(ctx, token)
		if err != nil {
			return nil, err
		}
		return json.Marshal(reference)
	})
	if err != nil {
		return nil, err
	}
	var reference connectorconfig.ApplicationSecretRef
	if err = json.Unmarshal(stored, &reference); err != nil {
		return nil, err
	}
	return &connectorconfig.CredentialBrokerConfiguration{Endpoint: host.shared.ReadURL, TokenSecretRef: reference}, nil
}

// refreshSharedSettings copies the current settings of every connection that
// document selects into its entry, as the declared release reads them, and
// returns the document, the Project's TeamSpace, read when an entry selects a
// connection, and the selected connections, which must belong to it. The
// readiness check before a freeze proved each selection valid.
func (host *ConfigurationHost) refreshSharedSettings(ctx context.Context, document connectorconfig.Configuration, manifest projectManifestRecord) (connectorconfig.Configuration, string, []string, error) {
	refreshed := document
	refreshed.Connections = append([]connectorconfig.ConnectionConfiguration{}, document.Connections...)
	var selected []string
	var teamSpaceID string
	var err error
	for index, entry := range refreshed.Connections {
		if entry.SharedConnectionID == "" {
			continue
		}
		declaration, declared := manifest.declaration(entry.ConnectorID, entry.ConnectionName)
		if !declared {
			continue
		}
		if teamSpaceID == "" {
			if teamSpaceID, err = host.shared.ProjectTeamSpace(ctx, host.project.scope.ProjectID); err != nil {
				return connectorconfig.Configuration{}, "", nil, err
			}
		}
		connection, err := host.shared.Connections.FrozenConnection(ctx, teamSpaceID, entry.SharedConnectionID)
		if err != nil {
			return connectorconfig.Configuration{}, "", nil, err
		}
		release, err := host.connectorReleases.Resolve(ctx, declaration.module())
		if err != nil {
			return connectorconfig.Configuration{}, "", nil, err
		}
		method, _ := release.Manifest.Spec.Auth.Method(declaration.AuthMethodID)
		var settings map[string]json.RawMessage
		comm.MustDo(json.Unmarshal(connection.Settings, &settings))
		fields := connectormanifest.ConfigurationFields(release.Manifest, method)
		settings = connectormanifest.DeclaredSettings(fields, settings)
		if err = connectormanifest.NormalizeConfigurationFields(fields, settings, host.shared.PlainHTTP); err != nil {
			return connectorconfig.Configuration{}, "", nil, err
		}
		refreshed.Connections[index].Configuration = comm.Must(json.Marshal(settings))
		selected = append(selected, connection.ConnectionID)
	}
	return refreshed, teamSpaceID, selected, nil
}

// sameSettings reports whether two documents' entries hold the same
// settings.
func sameSettings(first, second connectorconfig.Configuration) bool {
	if len(first.Connections) != len(second.Connections) {
		return false
	}
	for index := range first.Connections {
		if !bytes.Equal(first.Connections[index].Configuration, second.Connections[index].Configuration) {
			return false
		}
	}
	return true
}

// selectedConnection is what a connection name's view shows of the
// connection it selects.
type selectedConnection struct {
	Name             string `json:"name"`
	ConnectionStatus string `json:"connectionStatus"`
	CredentialStatus string `json:"credentialStatus"`
	ClientSource     string `json:"clientSource"`
	ManifestVersion  string `json:"manifestVersion"`
}

// sharedSelection reads the connection an entry selects; a connection that
// no longer exists reads as none.
func (host *ConfigurationHost) sharedSelection(ctx context.Context, entry connectorconfig.ConnectionConfiguration) (*connectiondao.Connection, error) {
	if entry.SharedConnectionID == "" {
		return nil, nil
	}
	connection, err := host.shared.Connections.SelectedConnection(ctx, entry.SharedConnectionID)
	if errors.Is(err, connections.ErrNotFound) {
		return nil, nil
	}
	return &connection, err
}

// writeConnectionError writes the answer of a refused connection operation
// and reports whether err is nil.
func writeConnectionError(response http.ResponseWriter, err error) bool {
	if err == nil {
		return true
	}
	if reason, refused := connections.Refusal(err); refused {
		writeCodedError(response, http.StatusConflict, reason.String(), err.Error())
		return false
	}
	switch {
	case errors.Is(err, connections.ErrInvalid):
		writeCodedError(response, http.StatusConflict, "CONNECTOR_SELECTION_INVALID", err.Error())
	case errors.Is(err, connections.ErrNotFound):
		writeCodedError(response, http.StatusNotFound, "TEAM_SPACE_CONNECTION_NOT_FOUND", "The TeamSpace connection does not exist")
	case errors.Is(err, connections.ErrReauthorizationRequired):
		writeCodedError(response, http.StatusConflict, "CONNECTOR_CREDENTIAL_UNAVAILABLE", "The connection needs a new authorization")
	default:
		writeStoreError(response, err)
	}
	return false
}

// declaration is the AppManifest's declaration of a connection name.
func (record projectManifestRecord) declaration(connectorID, connectionName string) (projectConnectorDeclaration, bool) {
	for _, declaration := range record.Manifest.Connectors {
		if declaration.ConnectorID == connectorID && declaration.ConnectionName == connectionName {
			return declaration, true
		}
	}
	return projectConnectorDeclaration{}, false
}

// scopeKind is the scope's kind.
func (project *projectConfiguration) scopeKind() infra.ScopeKind {
	if project.scope.Kind == infra.ScopePreview.String() {
		return infra.ScopePreview
	}
	return infra.ScopeLive
}

// SharedSelections returns the TeamSpace connection that configuration
// selects for each connection name that selects one.
func SharedSelections(configuration connectorconfig.Configuration) map[string]string {
	selected := map[string]string{}
	for _, connection := range configuration.Connections {
		if connection.SharedConnectionID != "" {
			selected[connection.ConnectionName] = connection.SharedConnectionID
		}
	}
	return selected
}
