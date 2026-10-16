// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectormanifest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// TestAuthorizationGrantOptionalSourceIntegration exchanges a real code of
// the lane's Keycloak (SUPERVERSE_TEST_KEYCLOAK_ORIGIN) and assembles a
// credential whose method maps a second token from a source Keycloak's answer
// lacks, as Slack v1 maps authed_user.refresh_token, which Slack sends only
// with token rotation. An optional credential is left out and the
// authorization succeeds; the same mapping into a required credential fails
// and names it.
func TestAuthorizationGrantOptionalSourceIntegration(t *testing.T) {
	keycloak := dexflowtest.KeycloakForTest(t)
	realm := keycloak.Origin + dexflowtest.KeycloakRealmForTest + "/protocol/openid-connect"
	method := func(required bool) ManifestAuthMethod {
		var m ManifestAuthMethod
		document := `{"id": "", "type": "oauth2", "fields": [
			{"name": "access_token", "type": "secretString", "required": true},
			{"name": "user_refresh_token", "type": "secretString", "required": ` + map[bool]string{true: "true", false: "false"}[required] + `}],
			"oauth2": {"authorizationEndpoint": "` + realm + `/auth", "tokenEndpoint": "` + realm + `/token", "scopes": ["openid"], "pkce": true,
			"credentialMappings": [{"credential": "access_token", "source": "access_token"},
				{"credential": "user_refresh_token", "source": "authed_user.refresh_token"}]}}`
		if err := json.Unmarshal([]byte(document), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	optional := method(false)
	resolved, err := ResolveOAuth2(*optional.OAuth2, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifier := flowid.RandomID(48)
	challenge := sha256.Sum256([]byte(verifier))
	redirectURI := keycloak.StudioOrigin + "/api/connector-oauth/callback"
	authorization := realm + "/auth?" + url.Values{"client_id": {"superverse-e2e"}, "response_type": {"code"}, "redirect_uri": {redirectURI},
		"scope": {"openid"}, "state": {"grant-test"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"}}.Encode()
	code := keycloak.AuthorizeForTest(t, authorization).Query().Get("code")
	client := &http.Client{Timeout: 20 * time.Second}
	token, err := ExchangeAuthorizationCode(t.Context(), client, resolved, "superverse-e2e", keycloak.ClientSecret, code, redirectURI, verifier)
	if err != nil {
		t.Fatalf("exchange the code: %v", err)
	}
	received := time.Now()
	credentials, _, err := AuthorizationGrant{Method: optional, OAuth: resolved}.Credentials(t.Context(), client, token, received)
	if err != nil || len(credentials["access_token"]) < 3 {
		t.Fatalf("an answer without the optional credential's source: %v", err)
	}
	if _, mapped := credentials["user_refresh_token"]; mapped {
		t.Fatal("the absent source mapped a credential")
	}
	required := method(true)
	_, _, err = AuthorizationGrant{Method: required, OAuth: resolved}.Credentials(t.Context(), client, token, received)
	if err == nil || !strings.Contains(err.Error(), "user_refresh_token") {
		t.Fatalf("an answer without a required credential's source: %v", err)
	}
}
