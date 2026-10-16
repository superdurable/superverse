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
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// grantOutcome is what one provider call of a grant yields: a successful
// token response to apply, or a reason the credential needs reauthorization.
// A call that got no usable answer is an error instead.
type grantOutcome struct {
	token      map[string]any
	receivedAt time.Time
	// terminal is the safe reason that the stored grant can never succeed
	// again: a declared terminal error code or a narrowed scope.
	terminal string
}

// errNoAnswer is a provider call without a usable answer: a transport
// failure, a server failure or an unreadable or unexpected response. The
// credential keeps its state, and the next read tries again.
var errNoAnswer = errors.New("the provider gave no usable answer")

// declarations are what a connection's manifest version declares for its
// method, resolved with its settings.
type declarations struct {
	method   connectormanifest.ManifestAuthMethod
	fields   []connectormanifest.ManifestField
	settings map[string]json.RawMessage
	grants   []connectormanifest.TokenGrant
	// hostKept are the credentials the release lists as kept from
	// applications.
	hostKept []string
	// oauth is the method's resolved oauth2 declarations, nil for another
	// type.
	oauth *connectormanifest.ResolvedOAuth2
}

// requestGrant sends grant's token request: a refresh with the stored
// refresh token, or a mint with the client credentials or a signed JWT
// bearer assertion. A refresh whose request was sent but whose answer was
// lost is retried once with the old token under the same lease: it succeeds
// when the request never arrived, the provider does not rotate or the old
// token is in its grace period; after a strict rotation the provider answers
// invalid_grant, and the lost token is lost either way.
func (r *CredentialReader) requestGrant(ctx context.Context, c connectiondao.Connection, d declarations, grant connectormanifest.TokenGrant, credential storedCredential) (connectormanifest.TokenResponse, error) {
	request, err := r.tokenRequest(c, d, grant, credential)
	if err != nil {
		return connectormanifest.TokenResponse{}, err
	}
	response, err := connectormanifest.SendTokenRequest(ctx, r.providers, request)
	if err != nil && grant.Kind == connectormanifest.GrantRefreshToken && ctx.Err() == nil {
		response, err = connectormanifest.SendTokenRequest(ctx, r.providers, request)
	}
	if err != nil {
		return connectormanifest.TokenResponse{}, fmt.Errorf("%w: %w", errNoAnswer, err)
	}
	return response, nil
}

// tokenRequest builds grant's request from the declarations, the stored
// credential and the connection's OAuth client.
func (r *CredentialReader) tokenRequest(c connectiondao.Connection, d declarations, grant connectormanifest.TokenGrant, credential storedCredential) (connectormanifest.TokenRequest, error) {
	parameters := url.Values{}
	for name, value := range grant.AdditionalParameters {
		parameters.Set(name, value)
	}
	switch grant.Kind {
	case connectormanifest.GrantRefreshToken:
		clientID, clientSecret, err := r.clients.oauthClient(c, d.method, credential.Credentials)
		if err != nil {
			return connectormanifest.TokenRequest{}, err
		}
		parameters.Set("grant_type", "refresh_token")
		parameters.Set(connectormanifest.TokenMemberRefreshToken, credentialText(credential.Credentials, grant.RefreshTokenCredential))
		return connectormanifest.TokenRequest{
			Endpoint: d.oauth.TokenEndpoint, Encoding: d.oauth.TokenRequestEncoding, AuthMethod: d.oauth.TokenEndpointAuthMethod,
			ClientID: clientID, ClientSecret: clientSecret, Parameters: parameters,
		}, nil
	case connectormanifest.GrantClientCredentials:
		declared := d.method.ClientCredentials
		values, err := connectormanifest.NewGrantValues(credential.Credentials, d.fields, d.settings, "")
		if err != nil {
			return connectormanifest.TokenRequest{}, err
		}
		endpoint, err := r.mintingEndpoint(values, declared.TokenEndpoint)
		if err != nil {
			return connectormanifest.TokenRequest{}, err
		}
		var authMethod string
		if declared.TokenEndpointAuthMethod != nil {
			if authMethod, err = values.Resolve(*declared.TokenEndpointAuthMethod); err != nil {
				return connectormanifest.TokenRequest{}, err
			}
		}
		parameters.Set("grant_type", "client_credentials")
		if len(declared.Scopes) > 0 {
			parameters.Set("scope", strings.Join(declared.Scopes, " "))
		}
		return connectormanifest.TokenRequest{
			Endpoint: endpoint, Encoding: declared.TokenRequestEncoding, AuthMethod: authMethod, Parameters: parameters,
			ClientID: credentialText(credential.Credentials, declared.ClientIDCredential), ClientSecret: credentialText(credential.Credentials, declared.ClientSecretCredential),
		}, nil
	case connectormanifest.GrantJWTBearer:
		assertion, endpoint, err := r.jwtBearerAssertion(d, credential)
		if err != nil {
			return connectormanifest.TokenRequest{}, err
		}
		parameters.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
		parameters.Set("assertion", assertion)
		return connectormanifest.TokenRequest{Endpoint: endpoint, Parameters: parameters}, nil
	}
	panic("connections: grant kind " + grant.Kind.String() + " has no token request")
}

// jwtBearerAssertion signs the RS256 assertion of a JWT bearer grant with the
// stored private key and returns it with the resolved token endpoint.
func (r *CredentialReader) jwtBearerAssertion(d declarations, credential storedCredential) (string, string, error) {
	declared := d.method.ServiceAccount.JWTBearer
	values, err := connectormanifest.NewGrantValues(credential.Credentials, d.fields, d.settings, declared.KeyCredential)
	if err != nil {
		return "", "", err
	}
	claims := map[string]string{}
	for name, source := range map[string]connectormanifest.ValueSource{"privateKey": declared.PrivateKey, "issuer": declared.Issuer, "audience": declared.Audience} {
		if claims[name], err = values.Resolve(source); err != nil {
			return "", "", fmt.Errorf("JWT bearer %s: %w", name, err)
		}
	}
	if declared.Subject != nil {
		if claims["subject"], err = values.Resolve(*declared.Subject); err != nil {
			return "", "", fmt.Errorf("JWT bearer subject: %w", err)
		}
	}
	endpoint, err := r.mintingEndpoint(values, declared.TokenEndpoint)
	if err != nil {
		return "", "", err
	}
	lifetime, err := time.ParseDuration(declared.Lifetime)
	if err != nil {
		return "", "", fmt.Errorf("JWT bearer lifetime %q is not a duration", declared.Lifetime)
	}
	key, err := oauthtoken.ParseRSAPrivateKeyPEM(claims["privateKey"])
	if err != nil {
		return "", "", errors.New("the JWT bearer private key is not an RSA PEM key")
	}
	assertion, err := oauthtoken.SignJWTBearerAssertion(oauthtoken.JWTBearerAssertion{
		Issuer: claims["issuer"], Subject: claims["subject"], Audience: claims["audience"], Scope: strings.Join(declared.Scopes, " "),
		IssuedAt: time.Now(), Lifetime: lifetime,
	}, key)
	if err != nil {
		return "", "", fmt.Errorf("sign the JWT bearer assertion: %w", err)
	}
	return assertion.Reveal(), endpoint, nil
}

// mintingEndpoint resolves a minting grant's token endpoint. A literal is
// https; one a credential, setting or key supplies must be an absolute URL,
// and the provider client's address policy decides where it may connect.
func (r *CredentialReader) mintingEndpoint(values connectormanifest.GrantValues, source connectormanifest.ValueSource) (string, error) {
	endpoint, err := values.Resolve(source)
	if err != nil {
		return "", fmt.Errorf("token endpoint: %w", err)
	}
	target, err := url.Parse(endpoint)
	if err != nil || target.Hostname() == "" || target.User != nil || target.Scheme != "https" && (source.Value != "" || target.Scheme != "http") {
		return "", errors.New("the token endpoint is not an absolute URL that the profile allows")
	}
	return endpoint, nil
}

// interpret classifies a grant's answer: a success, a terminal rejection, a
// narrowed scope, or no usable answer. A response without scope keeps the
// granted scopes (RFC 6749 section 5.1).
func interpret(d declarations, grant connectormanifest.TokenGrant, response connectormanifest.TokenResponse, receivedAt time.Time) (grantOutcome, error) {
	if code, terminal := response.TerminalError(grant.TerminalErrorCodes); terminal {
		return grantOutcome{terminal: "the provider answered " + code}, nil
	}
	if !response.Succeeded() {
		return grantOutcome{}, fmt.Errorf("%w: HTTP %d", errNoAnswer, response.Status)
	}
	token, err := response.Object()
	if err != nil {
		return grantOutcome{}, fmt.Errorf("%w: %w", errNoAnswer, err)
	}
	if grant.Kind == connectormanifest.GrantRefreshToken {
		declared := d.oauth.Scopes
		if grant.UsesUserScopes {
			declared = d.oauth.UserScopes
		}
		if !connectormanifest.HasRequiredScopes(token, declared) {
			return grantOutcome{terminal: "the provider no longer grants the declared scopes"}, nil
		}
	}
	return grantOutcome{token: token, receivedAt: receivedAt}, nil
}
