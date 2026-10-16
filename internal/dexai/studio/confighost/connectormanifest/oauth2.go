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
	"errors"
	"fmt"
	"maps"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// The token request encoding and endpoint authentication a release may
// declare; empty selects a form body with client_secret_post.
const (
	formTokenRequestEncoding    = "form"
	jsonTokenRequestEncoding    = "json"
	clientSecretPostAuthMethod  = "client_secret_post"
	clientSecretBasicAuthMethod = "client_secret_basic"
)

// ValidateOAuthMethod admits the OAuth declarations of a release's method:
// the supported token request encoding and endpoint authentication, endpoints
// and scopes that are a literal or a connection setting, literal endpoints
// that are absolute https URLs, and authorization parameters the host does
// not set itself. ResolveOAuth2 checks what a setting supplies.
func ValidateOAuthMethod(oauth ManifestOAuth2) error {
	switch oauth.TokenRequestEncoding {
	case "", formTokenRequestEncoding, jsonTokenRequestEncoding:
	default:
		return fmt.Errorf("token request encoding %q is not supported", oauth.TokenRequestEncoding)
	}
	if method := oauth.TokenEndpointAuthMethod; method != nil {
		switch {
		case !method.isLiteralOrSetting() || method.List != nil:
			return errors.New("token endpoint auth method must be a literal or a connection setting")
		case method.Value != "" && method.Value != clientSecretPostAuthMethod && method.Value != clientSecretBasicAuthMethod:
			return fmt.Errorf("token endpoint auth method %q is not supported", method.Value)
		}
	}
	endpoints := []ValueSource{oauth.AuthorizationEndpoint, oauth.TokenEndpoint}
	for _, derivation := range oauth.CredentialDerivations {
		endpoints = append(endpoints, derivation.Endpoint)
	}
	for _, endpoint := range endpoints {
		if !endpoint.isLiteralOrSetting() || endpoint.List != nil {
			return errors.New("an OAuth endpoint must be a URL or a connection setting")
		}
		if endpoint.Value == "" {
			continue
		}
		if parsed, err := url.Parse(endpoint.Value); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("OAuth endpoint %q must be absolute HTTPS", endpoint.Value)
		}
	}
	if !oauth.Scopes.isEmpty() && (!oauth.Scopes.isLiteralOrSetting() || oauth.Scopes.Value != "") {
		return errors.New("OAuth scopes must be a list or a connection setting")
	}
	for name, value := range oauth.AuthorizationParameters {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" || reservedAuthorizationParameters[name] {
			return fmt.Errorf("authorization parameter %q is invalid", name)
		}
	}
	return nil
}

// ValidateOAuthCredentials admits the credentials an actor enters before an
// OAuth authorization: ordinary values for non-secret fields and secrets for
// secret ones. The fields the exchange supplies are never entered.
func ValidateOAuthCredentials(method ManifestAuthMethod, values map[string]json.RawMessage, secrets map[string]string) error {
	exchanged := oauthExchangedCredentials(*method.OAuth2)
	declared := make(map[string]ManifestField, len(method.Fields))
	for _, field := range method.Fields {
		declared[field.Name] = field
	}
	for name := range values {
		if field, found := declared[name]; !found || field.Type == fieldTypeSecretString || exchanged[name] {
			return fmt.Errorf("field %q is not an entered non-secret credential", name)
		}
	}
	entered := maps.Clone(values)
	for name, secret := range secrets {
		if field, found := declared[name]; !found || field.Type != fieldTypeSecretString || exchanged[name] {
			return fmt.Errorf("field %q is not an entered secret credential", name)
		}
		entered[name] = comm.Must(json.Marshal(secret))
	}
	return ValidateCredentialFields(method.Fields, entered, exchanged)
}

// oauthExchangedCredentials names the credentials an OAuth exchange supplies:
// the client and every mapped or derived credential. Without mappings the
// access token is the credential.
func oauthExchangedCredentials(oauth ManifestOAuth2) map[string]bool {
	exchanged := map[string]bool{}
	for _, name := range []string{oauth.ClientIDCredential, oauth.ClientSecretCredential} {
		if name != "" {
			exchanged[name] = true
		}
	}
	for _, mapping := range oauth.CredentialMappings {
		exchanged[mapping.Credential] = true
	}
	if len(oauth.CredentialMappings) == 0 {
		exchanged[TokenMemberAccessToken] = true
	}
	for _, derivation := range oauth.CredentialDerivations {
		exchanged[derivation.Credential] = true
	}
	return exchanged
}

// AuthorizationFillsCredentials reports whether an authorization alone fills
// every credential field the OAuth method requires: a mapped or derived
// credential, or the client ID and secret, which a platform application
// supplies. A method that also needs an entered credential, such as Slack v0's
// app-level token, cannot use a platform application.
func AuthorizationFillsCredentials(method ManifestAuthMethod) bool {
	exchanged := oauthExchangedCredentials(*method.OAuth2)
	for _, field := range method.Fields {
		if field.Required && field.Default == nil && !exchanged[field.Name] {
			return false
		}
	}
	return true
}

// ClientSecretRequired reports whether the method's client secret is a
// required credential field.
func ClientSecretRequired(method ManifestAuthMethod) bool {
	if method.OAuth2.ClientSecretCredential == "" {
		return false
	}
	for _, field := range method.Fields {
		if field.Name == method.OAuth2.ClientSecretCredential {
			return field.Required
		}
	}
	return false
}

// AuthorizationURL is the provider's authorization endpoint with the
// release's parameters, the client, the redirect, the state and, with PKCE,
// the S256 challenge of verifier.
func AuthorizationURL(oauth ResolvedOAuth2, clientID, redirectURI, state, verifier string) string {
	endpoint := comm.Must(url.Parse(oauth.AuthorizationEndpoint))
	query := endpoint.Query()
	for name, value := range oauth.authorizationParameters {
		query.Set(name, value)
	}
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("state", state)
	if len(oauth.Scopes) > 0 {
		query.Set("scope", strings.Join(oauth.Scopes, " "))
	}
	if len(oauth.UserScopes) > 0 {
		query.Set("user_scope", strings.Join(oauth.UserScopes, " "))
	}
	if oauth.pkce {
		challenge := sha256.Sum256([]byte(verifier))
		query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
		query.Set("code_challenge_method", "S256")
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

// reservedAuthorizationParameters are set by the host, never by a release.
var reservedAuthorizationParameters = map[string]bool{
	"client_id": true, "redirect_uri": true, "response_type": true, "scope": true,
	"state": true, "code_challenge": true, "code_challenge_method": true,
}

// encodeTokenRequest returns the token request body and its Content-Type. The
// json encoding sends one object of the single-valued form parameters.
func encodeTokenRequest(form url.Values, encoding string) (string, string) {
	if encoding != jsonTokenRequestEncoding {
		return form.Encode(), "application/x-www-form-urlencoded"
	}
	parameters := make(map[string]string, len(form))
	for name := range form {
		parameters[name] = form.Get(name)
	}
	return string(comm.Must(json.Marshal(parameters))), "application/json"
}

// ResponseText returns the non-blank string at a dotted path of a provider response.
func ResponseText(response map[string]any, path string) (string, bool) {
	current, found := responsePath(response, path)
	if !found {
		return "", false
	}
	value, isText := current.(string)
	return value, isText && strings.TrimSpace(value) != ""
}

// responsePath returns the value at a dotted path of a provider response.
func responsePath(response map[string]any, path string) (any, bool) {
	var current any = response
	for _, segment := range strings.Split(path, ".") {
		object, isObject := current.(map[string]any)
		if !isObject {
			return nil, false
		}
		if current, isObject = object[segment]; !isObject {
			return nil, false
		}
	}
	return current, true
}

// TokenExpiry is when a granted token expires, nil when the token response
// names no positive lifetime. expires_in is a JSON number, or a numeric
// string as Azure AD v1 sends it.
func TokenExpiry(token map[string]any, receivedAt time.Time) (*time.Time, error) {
	var seconds int64
	switch lifetime := token["expires_in"].(type) {
	case nil:
		return nil, nil
	case float64:
		if lifetime != math.Trunc(lifetime) {
			return nil, errors.New("OAuth token lifetime is not whole seconds")
		}
		seconds = int64(lifetime)
	case string:
		parsed, err := strconv.ParseInt(lifetime, 10, 64)
		if err != nil {
			return nil, errors.New("OAuth token lifetime is not a number")
		}
		seconds = parsed
	default:
		return nil, errors.New("OAuth token lifetime is not a number")
	}
	if seconds <= 0 {
		return nil, nil
	}
	expires := receivedAt.Add(time.Duration(seconds) * time.Second)
	return &expires, nil
}

// offlineAccessScope requests refresh material (OpenID Connect Core 1.0
// section 11) and names no resource permission. GitHub honors it without
// listing it among the granted scopes, so no grant has to echo it.
const offlineAccessScope = "offline_access"

// HasRequiredScopes reports whether grant covers required. A grant that
// declares no scopes grants the requested ones (RFC 6749 section 5.1).
func HasRequiredScopes(grant map[string]any, required []string) bool {
	granted, declared := declaredScopes(grant)
	if !declared {
		return true
	}
	for _, scope := range required {
		if scope != offlineAccessScope && !granted[scope] {
			return false
		}
	}
	return true
}

// hasRequiredUserScopes checks the user grant Slack reports in authed_user.
// Without declared scopes there, only a user token proves the grant.
func hasRequiredUserScopes(response map[string]any, required []string) bool {
	userGrant, isObject := response["authed_user"].(map[string]any)
	if !isObject {
		return false
	}
	if _, declared := declaredScopes(userGrant); !declared {
		if _, hasUserToken := ResponseText(userGrant, TokenMemberAccessToken); !hasUserToken {
			return false
		}
	}
	return HasRequiredScopes(userGrant, required)
}

// declaredScopes returns the scopes a grant's scope string names or, when
// scope is absent or null, its scopes array (HubSpot). It reports false when
// the grant declares neither; a value of another JSON type names no scopes.
func declaredScopes(grant map[string]any) (map[string]bool, bool) {
	granted := make(map[string]bool)
	if scope := grant["scope"]; scope != nil {
		if text, isText := scope.(string); isText {
			for _, name := range strings.FieldsFunc(text, func(character rune) bool { return character == ' ' || character == ',' }) {
				granted[name] = true
			}
		}
		return granted, true
	}
	scopes := grant["scopes"]
	if scopes == nil {
		return nil, false
	}
	if names, isArray := scopes.([]any); isArray {
		for _, name := range names {
			if text, isText := name.(string); isText {
				granted[text] = true
			}
		}
	}
	return granted, true
}
