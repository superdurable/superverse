// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package connections

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
)

// An actual published release that predates the list of host-kept
// credentials, gmail v0.22.0, is refused at creation and at selection, and the
// refusal names the release that lists them, gmail v0.23.0, from the actual
// catalog. The stores are nil because both refusals come before any database
// or object read.
func TestPreWaveReleaseRefusedLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	httpClient := connectormanifest.NewReleaseHTTPClient()
	defer httpClient.CloseIdleConnections()
	shared := NewTeamSpaceConnections(nil, nil, connectormanifest.NewReleaseResolver(httpClient, connectorlibrary.OfficialReleaseDownloadURL),
		connectormanifest.NewCatalog(httpClient), PlatformClients{}, NewProviderHTTPClient(false), nil, nil, false)
	gmail := connectormanifest.Module{ConnectorID: "gmail", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/google/gmail", ModuleVersion: "v0.22.0"}
	_, err := shared.Create(ctx, Creation{TeamSpaceID: "tslive", RequestID: "live", ActorAccountID: "live", Name: "gmail", Release: gmail, AuthMethodID: "google-oauth"})
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, errConnectorVersionUnsupported) {
		t.Fatalf("a connection of a release without host-kept credentials was created: %v", err)
	}
	_, _, err = shared.Select(ctx, Selection{TeamSpaceID: "tslive", ConnectionID: "clive", ProjectID: "plive", Scope: infra.ScopePreview, Declared: gmail, DeclaredMethodID: "google-oauth"})
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, errConnectorVersionUnsupported) || !strings.Contains(err.Error(), "use v0.23.0 or newer") {
		t.Fatalf("a release without host-kept credentials was selected: %v", err)
	}
	t.Logf("refusal: %v", err)
}

// The platform's OAuth application is offered for a method only when it is
// registered for every scope the actual release declares and the
// authorization fills every credential the method requires: gmail v0.23.0's
// google-oauth qualifies, Slack v0.22.0 needs the entered app-level token, and
// a registration missing one of gmail's scopes is not offered.
func TestPlatformClientOfferLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	httpClient := connectormanifest.NewReleaseHTTPClient()
	defer httpClient.CloseIdleConnections()
	releases := connectormanifest.NewReleaseResolver(httpClient, connectorlibrary.OfficialReleaseDownloadURL)
	method := func(module connectormanifest.Module, methodID string) connectormanifest.ManifestAuthMethod {
		release, err := releases.Resolve(ctx, module)
		if err != nil {
			t.Fatalf("resolve %s %s: %v", module.ConnectorID, module.ModuleVersion, err)
		}
		found, ok := release.Manifest.Spec.Auth.Method(methodID)
		if !ok || found.OAuth2 == nil {
			t.Fatalf("%s %s has no OAuth method %q", module.ConnectorID, module.ModuleVersion, methodID)
		}
		return found
	}
	gmail := method(connectormanifest.Module{ConnectorID: "gmail", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/google/gmail", ModuleVersion: "v0.23.0"}, "google-oauth")
	slack := method(connectormanifest.Module{ConnectorID: "slack", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/slack", ModuleVersion: "v0.22.0"}, "")
	every := func(m connectormanifest.ManifestAuthMethod) []string {
		return append(append([]string{}, m.OAuth2.Scopes.List...), m.OAuth2.UserScopes...)
	}
	clients := PlatformClients{
		platformClientKey("gmail", gmail.ID): {ClientID: "gmail-client", Scopes: every(gmail)},
		platformClientKey("slack", slack.ID): {ClientID: "slack-client", Scopes: every(slack)},
	}
	if _, offered := clients.availableFor("gmail", gmail); !offered {
		t.Fatal("the platform application covering gmail's scopes was not offered")
	}
	if _, offered := clients.availableFor("slack", slack); offered {
		t.Fatal("the platform application was offered for Slack v0, whose app-level token no authorization returns")
	}
	narrow := PlatformClients{platformClientKey("gmail", gmail.ID): {ClientID: "gmail-client", Scopes: every(gmail)[1:]}}
	if _, offered := narrow.availableFor("gmail", gmail); offered {
		t.Fatal("a platform application missing one of gmail's scopes was offered")
	}
}
