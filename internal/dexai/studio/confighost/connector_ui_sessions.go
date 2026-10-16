// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package confighost

import (
	"net/http"
	"time"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm/signedtoken"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
)

// A UI session is the connection form Studio opens for a declared
// connection: Studio renders the release manifest itself and may run the
// release's read-only Studio commands with the connection's credential. Its
// nonce is a token signed for signedtoken.ConnectorUISession that binds the
// actor, the Project scope and the prepared source it was opened for, and the
// connection's module, for uiSessionLifetime. The source is immutable, so it
// also fixes the session's AppManifest and definition revisions. The server
// keeps nothing, so any replica serves a session another opened, and a
// rotation of the root key voids the open sessions.

const uiSessionLifetime = 10 * time.Minute

// uiSessionClaims are what a UI session's nonce binds.
type uiSessionClaims struct {
	ActorID      string                   `json:"actor"`
	ProjectID    string                   `json:"project"`
	ScopeKind    string                   `json:"scope"`
	SourceCommit string                   `json:"source"`
	Module       connectormanifest.Module `json:"module"`
}

type uiSessionView struct {
	ConnectorID    string                            `json:"connectorId"`
	ConnectionName string                            `json:"connectionName"`
	SessionNonce   string                            `json:"sessionNonce,omitempty"`
	Manifest       connectormanifest.ReleaseManifest `json:"manifest"`
}

// handleOpenUISession opens a UI session for a declared connection.
func (host *ConfigurationHost) handleOpenUISession(response http.ResponseWriter, request *http.Request) {
	var key connectorconfig.ConnectionKey
	if decodeBody(response, request, 1<<16, &key) != nil {
		writeCodedError(response, http.StatusBadRequest, "CONNECTOR_REQUEST_INVALID", "Connector UI session request is invalid")
		return
	}
	admission, declaration, admitted := host.admitConnection(response, request, key)
	if !admitted {
		return
	}
	ctx := request.Context()
	module := declaration.module()
	release, err := host.connectorReleases.Resolve(ctx, module)
	if err != nil {
		writeReleaseUnavailable(response, err)
		return
	}
	claims := uiSessionClaims{ActorID: admission.ActorID, ProjectID: host.project.scope.ProjectID, ScopeKind: host.project.scope.Kind,
		SourceCommit: admission.SourceCommit, Module: module}
	writeJSON(response, http.StatusOK, uiSessionView{ConnectorID: key.ConnectorID, ConnectionName: key.ConnectionName, Manifest: release.Manifest,
		SessionNonce: host.uiSessionTokens().Seal(claims, time.Now().Add(uiSessionLifetime))})
}

// handleProviderCommand runs one of a form's Studio commands with the
// credential of the TeamSpace connection the connection name selects.
func (host *ConfigurationHost) handleProviderCommand(response http.ResponseWriter, request *http.Request) {
	var session uiSessionClaims
	if host.uiSessionTokens().Open(request.PathValue("sessionNonce"), time.Now(), &session) != nil ||
		session.ProjectID != host.project.scope.ProjectID || session.ScopeKind != host.project.scope.Kind {
		writeCodedError(response, http.StatusNotFound, "CONNECTOR_UI_SESSION_NOT_FOUND", "Connector UI session is missing or expired")
		return
	}
	if caller := requestOf(request); caller.ActorID != session.ActorID || caller.SourceCommit != session.SourceCommit {
		writeCodedError(response, http.StatusConflict, "CONNECTOR_SESSION_TARGET_CHANGED", "Configuration source or actor changed; reopen this connection")
		return
	}
	release, err := host.connectorReleases.Resolve(request.Context(), session.Module)
	if err != nil {
		writeReleaseUnavailable(response, err)
		return
	}
	command, found := release.StudioCommand(request.PathValue("commandId"))
	if !found {
		writeCodedError(response, http.StatusNotFound, "CONNECTOR_STUDIO_COMMAND_NOT_FOUND", "Connector Studio command is not declared by this release")
		return
	}
	admission, _, admitted := host.admitConnection(response, request, session.Module.Key())
	if !admitted {
		return
	}
	var body struct {
		Parameters map[string]string `json:"parameters"`
	}
	if decodeBody(response, request, 1<<16, &body) != nil {
		writeCodedError(response, http.StatusBadRequest, "CONNECTOR_STUDIO_COMMAND_INVALID", "Connector Studio command parameters are invalid")
		return
	}
	ctx := request.Context()
	draft, err := host.project.loadConnectionDraft(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	entry, found := draft.entry(session.Module.Key())
	if !found || entry.SharedConnectionID == "" {
		writeCodedError(response, http.StatusConflict, "CONNECTOR_CONNECTION_NOT_READY", "Connector connection is not selected")
		return
	}
	// The read renews an expiring credential first, as an application's does.
	credential, err := host.shared.Reader.ReadConnection(ctx, entry.SharedConnectionID)
	if !writeConnectionError(response, err) {
		return
	}
	value, err := connectormanifest.RunStudioCommand(ctx, host.studioCommands, command, body.Parameters, credential.Values)
	if err != nil {
		writeCodedError(response, http.StatusBadGateway, "CONNECTOR_PROVIDER_COMMAND_FAILED", "Connector provider command failed: "+err.Error())
		return
	}
	writeJSON(response, http.StatusOK, value)
}

// uiSessionTokens seals and opens the UI sessions' nonces.
func (host *ConfigurationHost) uiSessionTokens() *signedtoken.Signer {
	return signedtoken.New(host.shared.FormKey, signedtoken.ConnectorUISession)
}
