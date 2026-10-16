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
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/signedtoken"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
)

// A connection form is what Studio opens to create or edit a TeamSpace
// connection: the release manifest Studio renders, the methods whose OAuth
// the platform's application can authorize, and the release's read-only
// Studio commands, which run with the credential of an existing connection.
// The form's nonce is a token signed for signedtoken.ConnectionForm that binds
// the actor, TeamSpace, connection and release for formLifetime. The server
// keeps nothing, so any replica runs the commands of a form another opened,
// and a rotation of the root key voids the open forms.

const formLifetime = 10 * time.Minute

// formClaims are what a form's nonce binds.
type formClaims struct {
	ActorID      string                   `json:"actor"`
	TeamSpaceID  string                   `json:"teamSpace"`
	ConnectionID string                   `json:"connection"`
	Release      connectormanifest.Module `json:"release"`
}

// Form is an open connection form.
type Form struct {
	SessionNonce string
	Manifest     json.RawMessage
	// PlatformClientMethods are the IDs of the OAuth methods the platform's
	// application can authorize; the unnamed method's ID is empty.
	PlatformClientMethods []string
}

// OpenForm opens the form of the connection connectionID, at its manifest
// version, or of a new connection of module when connectionID is empty. A
// release that predates TeamSpace connections is refused.
func (t *TeamSpaceConnections) OpenForm(ctx context.Context, actorID, teamSpaceID, connectionID string, module connectormanifest.Module) (Form, error) {
	if connectionID != "" {
		row, err := t.dao.Connection(ctx, teamSpaceID, connectionID)
		if err != nil {
			return Form{}, err
		}
		module = connectormanifest.Module{ConnectorID: row.ConnectorID, ModulePath: row.ModulePath, ModuleVersion: row.ManifestVersion}
	}
	release, _, err := t.method(ctx, module, "")
	if err != nil {
		return Form{}, err
	}
	form := Form{Manifest: comm.Must(json.Marshal(release.Manifest))}
	for _, method := range declaredMethods(release.Manifest.Spec.Auth) {
		if _, available := t.clients.availableFor(module.ConnectorID, method); available {
			form.PlatformClientMethods = append(form.PlatformClientMethods, method.ID)
		}
	}
	form.SessionNonce = t.formTokens().Seal(formClaims{ActorID: actorID, TeamSpaceID: teamSpaceID, ConnectionID: connectionID, Release: module},
		time.Now().Add(formLifetime))
	return form, nil
}

// RunFormCommand runs one of a form's Studio commands with the credential of
// the form's connection, renewing an expiring credential first as an
// application's read does.
func (t *TeamSpaceConnections) RunFormCommand(ctx context.Context, actorID, teamSpaceID, nonce, commandID string, parameters map[string]string) (map[string]any, error) {
	var form formClaims
	if t.formTokens().Open(nonce, time.Now(), &form) != nil || form.ActorID != actorID || form.TeamSpaceID != teamSpaceID {
		return nil, fmt.Errorf("%w: the connection form is missing or expired; open it again", ErrInvalid)
	}
	if form.ConnectionID == "" {
		return nil, fmt.Errorf("%w: a Studio command needs a saved connection", ErrInvalid)
	}
	release, err := t.releases.Resolve(ctx, form.Release)
	if err != nil {
		return nil, fmt.Errorf("resolve %s %s: %w", form.Release.ConnectorID, form.Release.ModuleVersion, err)
	}
	command, found := release.StudioCommand(commandID)
	if !found {
		return nil, fmt.Errorf("%w: the release declares no Studio command %q", ErrInvalid, commandID)
	}
	row, err := t.dao.Connection(ctx, teamSpaceID, form.ConnectionID)
	if err != nil {
		return nil, err
	}
	credential, err := t.reader.read(ctx, row, 0)
	if err != nil {
		return nil, err
	}
	return connectormanifest.RunStudioCommand(ctx, t.commands, command, parameters, credential.Values)
}

// declaredMethods are the authentication methods of auth: its declared
// methods, or auth itself as the unnamed one.
func declaredMethods(auth connectormanifest.ManifestAuth) []connectormanifest.ManifestAuthMethod {
	if len(auth.Methods) > 0 {
		return auth.Methods
	}
	method, _ := auth.Method("")
	return []connectormanifest.ManifestAuthMethod{method}
}

// formTokens seals and opens the forms' nonces.
func (t *TeamSpaceConnections) formTokens() *signedtoken.Signer {
	return signedtoken.New(t.formKey, signedtoken.ConnectionForm)
}
