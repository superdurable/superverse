// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectormanifest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// A release declares how a host refreshes and mints each method's access
// token (the connectors library's schema/token_grants.go): an oauth2 method
// that maps a refresh token refreshes with it, an apiKey method with
// clientCredentials mints with its client, and a serviceAccount method with
// jwtBearer mints with a signed assertion. A method without one keeps its
// credential as stored.

// oauthRefresh declares how an oauth2 method refreshes; nil fields take the
// defaults: invalid_grant is terminal and a response without expires_in
// keeps its token without an expiry.
type oauthRefresh struct {
	TerminalErrorCodes   []string            `json:"terminalErrorCodes,omitempty"`
	MissingExpiry        *MissingExpiry      `json:"missingExpiry,omitempty"`
	AdditionalParameters map[string]string   `json:"additionalParameters,omitempty"`
	Grants               []oauthRefreshGrant `json:"grants,omitempty"`
}

// oauthRefreshGrant is one stored refresh token, such as Slack's bot and user
// tokens, with how its response maps into credentials and which declared
// scopes it covers: scopes when empty, or userScopes.
type oauthRefreshGrant struct {
	RefreshTokenCredential string                   `json:"refreshTokenCredential"`
	CredentialMappings     []OAuthCredentialMapping `json:"credentialMappings"`
	GrantedScopes          string                   `json:"grantedScopes,omitempty"`
}

// userScopesGrant is the GrantedScopes of a grant checked against userScopes.
const userScopesGrant = "userScopes"

// MissingExpiry is the expiry of a token whose response has no expires_in:
// "keep" stores it without one; {"lifetime": "2h"} sets one.
type MissingExpiry struct {
	Lifetime string
}

// keepMissingExpiry is the scalar form of MissingExpiry.
const keepMissingExpiry = "keep"

// UnmarshalJSON reads "keep" or {"lifetime": <duration>}.
func (expiry *MissingExpiry) UnmarshalJSON(contents []byte) error {
	var scalar string
	if json.Unmarshal(contents, &scalar) == nil {
		if scalar != keepMissingExpiry {
			return fmt.Errorf("missingExpiry %q is neither keep nor a lifetime", scalar)
		}
		*expiry = MissingExpiry{}
		return nil
	}
	var lifetime struct {
		Lifetime string `json:"lifetime"`
	}
	if err := json.Unmarshal(contents, &lifetime); err != nil {
		return errors.New("missingExpiry is neither keep nor a lifetime")
	}
	if _, err := time.ParseDuration(lifetime.Lifetime); err != nil {
		return fmt.Errorf("missingExpiry lifetime %q is not a duration", lifetime.Lifetime)
	}
	*expiry = MissingExpiry{Lifetime: lifetime.Lifetime}
	return nil
}

// MarshalJSON writes "keep" or {"lifetime": <duration>}.
func (expiry MissingExpiry) MarshalJSON() ([]byte, error) {
	if expiry.Lifetime == "" {
		return json.Marshal(keepMissingExpiry)
	}
	return json.Marshal(map[string]string{"lifetime": expiry.Lifetime})
}

// Expiry is when a token issued at issuedAt without expires_in expires: nil
// to keep it.
func (expiry *MissingExpiry) Expiry(issuedAt time.Time) *time.Time {
	if expiry == nil || expiry.Lifetime == "" {
		return nil
	}
	expires := issuedAt.Add(comm.Must(time.ParseDuration(expiry.Lifetime)))
	return &expires
}

// ClientCredentialsGrant mints an apiKey method's access token with the
// client credentials grant from its stored client.
type ClientCredentialsGrant struct {
	TokenEndpoint           ValueSource              `json:"tokenEndpoint"`
	TokenEndpointAuthMethod *ValueSource             `json:"tokenEndpointAuthMethod,omitempty"`
	TokenRequestEncoding    string                   `json:"tokenRequestEncoding,omitempty"`
	ClientIDCredential      string                   `json:"clientIDCredential"`
	ClientSecretCredential  string                   `json:"clientSecretCredential"`
	Scopes                  []string                 `json:"scopes,omitempty"`
	CredentialMappings      []OAuthCredentialMapping `json:"credentialMappings"`
	TerminalErrorCodes      []string                 `json:"terminalErrorCodes,omitempty"`
	MissingExpiry           *MissingExpiry           `json:"missingExpiry,omitempty"`
	AdditionalParameters    map[string]string        `json:"additionalParameters,omitempty"`
}

// ServiceAccountGrants holds a serviceAccount method's minting declaration.
type ServiceAccountGrants struct {
	JWTBearer *JWTBearerGrant `json:"jwtBearer,omitempty"`
}

// JWTBearerGrant mints a serviceAccount method's access token with an RS256
// assertion that the stored private key signs.
type JWTBearerGrant struct {
	KeyCredential      string                   `json:"keyCredential,omitempty"`
	PrivateKey         ValueSource              `json:"privateKey"`
	Issuer             ValueSource              `json:"issuer"`
	Subject            *ValueSource             `json:"subject,omitempty"`
	Audience           ValueSource              `json:"audience"`
	TokenEndpoint      ValueSource              `json:"tokenEndpoint"`
	Scopes             []string                 `json:"scopes,omitempty"`
	Lifetime           string                   `json:"lifetime"`
	CredentialMappings []OAuthCredentialMapping `json:"credentialMappings"`
	CredentialCopies   []CredentialCopy         `json:"credentialCopies,omitempty"`
	TerminalErrorCodes []string                 `json:"terminalErrorCodes,omitempty"`
	MissingExpiry      *MissingExpiry           `json:"missingExpiry,omitempty"`
}

// CredentialCopy sets one credential field from another after each mint.
type CredentialCopy struct {
	Credential     string `json:"credential"`
	FromCredential string `json:"fromCredential"`
}

// GrantKind is how a grant obtains its access token.
type GrantKind struct{ name string }

// The grant kinds.
var (
	GrantRefreshToken      = GrantKind{"refreshToken"}
	GrantClientCredentials = GrantKind{"clientCredentials"}
	GrantJWTBearer         = GrantKind{"jwtBearer"}
)

// String is the kind's name.
func (kind GrantKind) String() string { return kind.name }

// TokenGrant is one grant of a method with its defaults applied. Its ID names
// it in a credential's grant times: the refresh token's credential field, or
// the minting kind.
type TokenGrant struct {
	Kind                   GrantKind
	ID                     string
	RefreshTokenCredential string
	CredentialMappings     []OAuthCredentialMapping
	UsesUserScopes         bool
	TerminalErrorCodes     []string
	MissingExpiry          *MissingExpiry
	AdditionalParameters   map[string]string
}

// Grants returns the method's declared grants, none for a method whose
// credential is kept as stored.
func Grants(method ManifestAuthMethod) []TokenGrant {
	var grants []TokenGrant
	if oauth := method.OAuth2; oauth != nil {
		refresh := oauth.Refresh
		if refresh == nil {
			refresh = &oauthRefresh{}
		}
		terminal := defaultCodes(refresh.TerminalErrorCodes, "invalid_grant")
		declared := refresh.Grants
		if len(declared) == 0 {
			for _, mapping := range oauth.CredentialMappings {
				if mapping.Source == TokenMemberRefreshToken {
					declared = []oauthRefreshGrant{{RefreshTokenCredential: mapping.Credential, CredentialMappings: oauth.CredentialMappings}}
				}
			}
		}
		for _, grant := range declared {
			grants = append(grants, TokenGrant{
				Kind: GrantRefreshToken, ID: grant.RefreshTokenCredential, RefreshTokenCredential: grant.RefreshTokenCredential,
				CredentialMappings: grant.CredentialMappings, UsesUserScopes: grant.GrantedScopes == userScopesGrant,
				TerminalErrorCodes: terminal, MissingExpiry: refresh.MissingExpiry, AdditionalParameters: refresh.AdditionalParameters,
			})
		}
	}
	if grant := method.ClientCredentials; grant != nil {
		grants = append(grants, TokenGrant{
			Kind: GrantClientCredentials, ID: GrantClientCredentials.name, CredentialMappings: grant.CredentialMappings,
			TerminalErrorCodes: defaultCodes(grant.TerminalErrorCodes, "invalid_client"), MissingExpiry: grant.MissingExpiry,
			AdditionalParameters: grant.AdditionalParameters,
		})
	}
	if method.ServiceAccount != nil && method.ServiceAccount.JWTBearer != nil {
		grant := method.ServiceAccount.JWTBearer
		grants = append(grants, TokenGrant{
			Kind: GrantJWTBearer, ID: GrantJWTBearer.name, CredentialMappings: grant.CredentialMappings,
			TerminalErrorCodes: defaultCodes(grant.TerminalErrorCodes, "invalid_grant"), MissingExpiry: grant.MissingExpiry,
		})
	}
	return grants
}

func defaultCodes(codes []string, fallback string) []string {
	if len(codes) == 0 {
		return []string{fallback}
	}
	return codes
}

// AuthMethodCredential is the credential that records a declared method's
// ID, where the application's credential decoder reads it. A client-supplied
// value could contradict the method, so only the host sets it.
const AuthMethodCredential = "auth_method"

// StampAuthMethod records methodID in credentials. The unnamed method of a
// Connector without declared methods records nothing.
func StampAuthMethod(credentials map[string]json.RawMessage, methodID string) {
	if methodID != "" {
		credentials[AuthMethodCredential] = comm.Must(json.Marshal(methodID))
	}
}

// TokenRequest is one request to an OAuth token endpoint. A client without a
// secret is a public client that sends only its ID.
type TokenRequest struct {
	Endpoint, Encoding, AuthMethod string
	ClientID, ClientSecret         string
	Parameters                     url.Values
}

// TokenResponse is a token endpoint's answer, uninterpreted, so the caller
// can store it before it parses it.
type TokenResponse struct {
	Status int
	Body   []byte
}

// tokenResponseLimit bounds a token or identity response.
const tokenResponseLimit = 1 << 20

// SendTokenRequest posts request with client, which applies the address
// policy and never follows a redirect. An error means no response was
// received; whether the request reached the provider is then unknown.
func SendTokenRequest(ctx context.Context, client *http.Client, request TokenRequest) (TokenResponse, error) {
	form := url.Values{}
	for name, values := range request.Parameters {
		form[name] = values
	}
	usesBasicAuth := request.AuthMethod == clientSecretBasicAuthMethod && request.ClientSecret != ""
	if request.ClientID != "" && !usesBasicAuth {
		form.Set("client_id", request.ClientID)
		if request.ClientSecret != "" {
			form.Set("client_secret", request.ClientSecret)
		}
	}
	body, contentType := encodeTokenRequest(form, request.Encoding)
	post := comm.Must(http.NewRequestWithContext(ctx, http.MethodPost, request.Endpoint, strings.NewReader(body)))
	post.Header.Set("Content-Type", contentType)
	post.Header.Set("Accept", "application/json")
	if usesBasicAuth {
		// Zoom, Calendly and Notion document base64(id:secret) without RFC 6749 form-encoding.
		post.SetBasicAuth(request.ClientID, request.ClientSecret)
	}
	response, err := client.Do(post)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("call the token endpoint: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	contents, err := readResponseBody(response.Body, tokenResponseLimit)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("read the token response: %w", err)
	}
	return TokenResponse{Status: response.StatusCode, Body: contents}, nil
}

// Object decodes the response as a JSON object. Its errors never include the
// body, which may hold tokens.
func (response TokenResponse) Object() (map[string]any, error) {
	var object map[string]any
	if err := json.Unmarshal(response.Body, &object); err != nil || object == nil {
		return nil, fmt.Errorf("the token response (HTTP %d) is not a JSON object", response.Status)
	}
	return object, nil
}

// Succeeded reports a 2xx response with an access token and no OAuth error,
// which some providers report with HTTP 200.
func (response TokenResponse) Succeeded() bool {
	object, err := response.Object()
	if err != nil || response.Status < 200 || response.Status > 299 {
		return false
	}
	_, hasToken := ResponseText(object, TokenMemberAccessToken)
	errorCode, _ := object["error"].(string)
	return hasToken && errorCode == ""
}

// TerminalError returns the response's OAuth error code when it is one of
// terminal and the status is below 500; a server failure is never terminal,
// because some providers report invalid_grant during outages.
func (response TokenResponse) TerminalError(terminal []string) (string, bool) {
	if response.Status >= 500 {
		return "", false
	}
	object, err := response.Object()
	if err != nil {
		return "", false
	}
	code, _ := object["error"].(string)
	for _, candidate := range terminal {
		if code != "" && code == candidate {
			return code, true
		}
	}
	return "", false
}

// ExchangeAuthorizationCode exchanges an authorization code at the resolved
// token endpoint and returns the response object uninterpreted: the caller
// stores it before it reads it, because the spent code cannot reproduce it.
func ExchangeAuthorizationCode(ctx context.Context, client *http.Client, oauth ResolvedOAuth2, clientID, clientSecret, code, redirectURI, verifier string) (map[string]any, error) {
	parameters := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}}
	if oauth.pkce {
		parameters.Set("code_verifier", verifier)
	}
	response, err := SendTokenRequest(ctx, client, TokenRequest{
		Endpoint: oauth.TokenEndpoint, Encoding: oauth.TokenRequestEncoding, AuthMethod: oauth.TokenEndpointAuthMethod,
		ClientID: clientID, ClientSecret: clientSecret, Parameters: parameters,
	})
	if err != nil {
		return nil, fmt.Errorf("OAuth token endpoint: %w", err)
	}
	if response.Status < 200 || response.Status > 299 {
		return nil, fmt.Errorf("OAuth token endpoint: HTTP %d", response.Status)
	}
	return response.Object()
}

// AuthorizationGrant is what an authorization code exchange yields for a
// method: the client the connection uses, when the host stores it with the
// credential, and what the actor entered.
type AuthorizationGrant struct {
	Method                 ManifestAuthMethod
	OAuth                  ResolvedOAuth2
	ClientID, ClientSecret string
	// StoresClient records the client in the credential; a host-registered
	// client stays in the host's configuration.
	StoresClient      bool
	CredentialValues  map[string]json.RawMessage
	CredentialSecrets map[string]string
}

// Credentials interprets an exchange's token response once and assembles
// the connection's credentials: the client, the entered credentials, the
// mapped token values, the derived identity values and the method marker. The
// response must report no error and cover the declared scopes. The expiry is
// nil when the response names no positive lifetime.
func (grant AuthorizationGrant) Credentials(ctx context.Context, client *http.Client, token map[string]any, receivedAt time.Time) (map[string]json.RawMessage, *time.Time, error) {
	oauth := grant.OAuth
	if rejection := token["error"]; rejection != nil && rejection != "" {
		// The error field is an RFC 6749 error code, never token material.
		return nil, nil, fmt.Errorf("OAuth provider rejected the token exchange: %v", rejection)
	}
	if !HasRequiredScopes(token, oauth.Scopes) || len(oauth.UserScopes) > 0 && !hasRequiredUserScopes(token, oauth.UserScopes) {
		return nil, nil, errors.New("OAuth grant scope is insufficient")
	}
	expiresAt, err := TokenExpiry(token, receivedAt)
	if err != nil {
		return nil, nil, err
	}
	credentials := make(map[string]json.RawMessage)
	setText := func(name, value string) { credentials[name] = comm.Must(json.Marshal(value)) }
	if grant.StoresClient && oauth.ClientIDCredential != "" {
		setText(oauth.ClientIDCredential, grant.ClientID)
	}
	if grant.StoresClient && oauth.ClientSecretCredential != "" && grant.ClientSecret != "" {
		setText(oauth.ClientSecretCredential, grant.ClientSecret)
	}
	for name, value := range grant.CredentialSecrets {
		setText(name, value)
	}
	for name, value := range grant.CredentialValues {
		credentials[name] = value
	}
	mappings := oauth.credentialMappings
	if len(mappings) == 0 {
		mappings = []OAuthCredentialMapping{{Credential: TokenMemberAccessToken, Source: TokenMemberAccessToken}}
	}
	for _, mapping := range mappings {
		value, found := ResponseText(token, mapping.Source)
		if !found {
			if mapping.Source == TokenMemberRefreshToken || !requiredCredential(grant.Method, mapping.Credential) {
				// A provider that issues no refresh token, such as Slack's
				// authed_user.refresh_token without token rotation, leaves the
				// connection static; reauthorization replaces it after expiry.
				continue
			}
			return nil, nil, fmt.Errorf("OAuth mapped credential %s is absent from the token answer's %s", mapping.Credential, mapping.Source)
		}
		setText(mapping.Credential, value)
	}
	if len(oauth.credentialDerivations) > 0 {
		accessToken, found := ResponseText(token, TokenMemberAccessToken)
		if !found {
			return nil, nil, errors.New("OAuth token response is missing the access token")
		}
		derived, err := deriveCredentials(ctx, client, accessToken, oauth.credentialDerivations)
		if err != nil {
			return nil, nil, err
		}
		for name, value := range derived {
			setText(name, value)
		}
	}
	StampAuthMethod(credentials, grant.Method.ID)
	return credentials, expiresAt, nil
}

// deriveCredentials reads each derived credential from its identity endpoint
// with the granted token, once per endpoint. A derivation with VerifiedBy
// requires the identity provider to report the value verified.
func deriveCredentials(ctx context.Context, client *http.Client, accessToken string, derivations []resolvedDerivation) (map[string]string, error) {
	credentials := make(map[string]string, len(derivations))
	responses := make(map[string]map[string]any)
	for _, derivation := range derivations {
		identity, read := responses[derivation.endpoint]
		if !read {
			var err error
			if identity, err = readIdentity(ctx, client, derivation.endpoint, accessToken); err != nil {
				return nil, err
			}
			responses[derivation.endpoint] = identity
		}
		if derivation.verifiedBy != "" {
			if verified, found := responsePath(identity, derivation.verifiedBy); !found || verified != true {
				return nil, fmt.Errorf("OAuth identity claim %q is not verified", derivation.source)
			}
		}
		value, found := ResponseText(identity, derivation.source)
		if !found {
			return nil, fmt.Errorf("OAuth identity response is missing %q", derivation.source)
		}
		credentials[derivation.credential] = value
	}
	return credentials, nil
}

func readIdentity(ctx context.Context, client *http.Client, endpoint, accessToken string) (map[string]any, error) {
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	identity, err := providerJSONObject(client, request, tokenResponseLimit, func(status int) bool { return status == http.StatusOK })
	if err != nil {
		return nil, fmt.Errorf("OAuth identity endpoint: %w", err)
	}
	return identity, nil
}

// providerJSONObject sends request with client and returns the JSON object of
// a response of at most limit bytes whose status ok accepts. The response
// body stays out of every error: it may echo the request's credential.
func providerJSONObject(client *http.Client, request *http.Request, limit int64, ok func(status int) bool) (map[string]any, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	contents, err := readResponseBody(response.Body, limit)
	if err != nil {
		return nil, fmt.Errorf("read the response: %w", err)
	}
	if !ok(response.StatusCode) {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var object map[string]any
	if err = json.Unmarshal(contents, &object); err != nil {
		return nil, fmt.Errorf("decode the response as a JSON object: %w", err)
	}
	if object == nil {
		return nil, errors.New("the response is null, not a JSON object")
	}
	return object, nil
}

// successStatus accepts every 2xx status.
func successStatus(status int) bool { return status >= 200 && status <= 299 }

// requiredCredential reports whether method requires the credential field
// name.
func requiredCredential(method ManifestAuthMethod, name string) bool {
	for _, field := range method.Fields {
		if field.Name == name {
			return field.Required
		}
	}
	return false
}
