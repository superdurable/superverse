// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connections

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
)

// A connection's OAuth client is the one its creator entered, stored with the
// credential (the user source), or an application the platform registered
// for the connector and method in this environment (the platform source),
// whose secret is a server secret and never enters a credential.

// PlatformClient is a platform OAuth application: its client and the scopes
// its provider verified it for.
type PlatformClient struct {
	ClientID, clientSecret string
	Scopes                 []string
}

// PlatformClients are the platform's applications by platformClientKey.
type PlatformClients map[string]PlatformClient

// platformClientKeyPattern admits "<connector>/<method>"; the unnamed method of a
// connector without declared methods is "default".
var platformClientKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}/[A-Za-z0-9._-]{1,64}$`)

// unnamedMethod names the method of a connector that declares none.
const unnamedMethod = "default"

// platformClientKey is the key of a connector's method among PlatformClients.
func platformClientKey(connectorID, methodID string) string {
	if methodID == "" {
		methodID = unnamedMethod
	}
	return connectorID + "/" + methodID
}

// ValidatePlatformClient checks one configured platform application: its
// key, a client ID and a non-empty list of distinct scopes without spaces.
func ValidatePlatformClient(key, clientID string, scopes []string) error {
	if !platformClientKeyPattern.MatchString(key) {
		return fmt.Errorf("platform client %q is not <connector>/<method>", key)
	}
	if strings.TrimSpace(clientID) == "" {
		return fmt.Errorf("platform client %q has no client ID", key)
	}
	if len(scopes) == 0 {
		return fmt.Errorf("platform client %q lists no scopes", key)
	}
	for index, scope := range scopes {
		if scope == "" || strings.ContainsFunc(scope, unicode.IsSpace) || slices.Contains(scopes[:index], scope) {
			return fmt.Errorf("platform client %q lists a blank, spaced or repeated scope", key)
		}
	}
	return nil
}

// ParsePlatformClientSecrets reads the JSON map from PlatformClientKey to
// client secret and joins it with the configured clients: every configured
// client needs a non-blank secret and every secret a configured client. Its
// errors name keys, never secrets.
func ParsePlatformClientSecrets(document string, clients map[string]PlatformClient) (PlatformClients, error) {
	secrets := map[string]string{}
	if strings.TrimSpace(document) != "" {
		if err := json.Unmarshal([]byte(document), &secrets); err != nil {
			return nil, errors.New("the platform OAuth client secrets are not a JSON object of strings")
		}
	}
	joined := PlatformClients{}
	for key, client := range clients {
		secret := secrets[key]
		if strings.TrimSpace(secret) == "" {
			return nil, fmt.Errorf("platform OAuth client %q has no secret", key)
		}
		client.clientSecret = secret
		joined[key] = client
	}
	for key := range secrets {
		if _, configured := clients[key]; !configured {
			return nil, fmt.Errorf("a secret names platform OAuth client %q, which is not configured", key)
		}
	}
	return joined, nil
}

// availableFor returns the platform application of a method when it is
// verified for every scope the method declares and the authorization fills
// every credential field the method requires; a scope that the connection
// owner enters cannot be known in advance, so such a method has none.
func (clients PlatformClients) availableFor(connectorID string, method connectormanifest.ManifestAuthMethod) (PlatformClient, bool) {
	client, found := clients[platformClientKey(connectorID, method.ID)]
	if !found || method.OAuth2 == nil || method.OAuth2.Scopes.FromSetting != "" || !connectormanifest.AuthorizationFillsCredentials(method) {
		return PlatformClient{}, false
	}
	for _, scope := range append(append([]string{}, method.OAuth2.Scopes.List...), method.OAuth2.UserScopes...) {
		if !slices.Contains(client.Scopes, scope) {
			return PlatformClient{}, false
		}
	}
	return client, true
}

// oauthClient is the client of a connection's token requests: from the
// stored credential for the user source, from the platform's configuration
// for the platform source.
func (clients PlatformClients) oauthClient(c connectiondao.Connection, method connectormanifest.ManifestAuthMethod, credentials map[string]json.RawMessage) (id, secret string, err error) {
	if c.ClientSource == connectiondao.ClientPlatform {
		client, found := clients[platformClientKey(c.ConnectorID, c.AuthMethodID)]
		if !found {
			return "", "", fmt.Errorf("the platform OAuth client of %s is not configured", platformClientKey(c.ConnectorID, c.AuthMethodID))
		}
		return client.ClientID, client.clientSecret, nil
	}
	if method.OAuth2 == nil {
		return "", "", nil
	}
	return credentialText(credentials, method.OAuth2.ClientIDCredential), credentialText(credentials, method.OAuth2.ClientSecretCredential), nil
}

// providerRequestTimeout bounds one provider request.
const providerRequestTimeout = 30 * time.Second

// NewProviderHTTPClient is the one client of every provider call: token and
// identity endpoints and Studio commands. It connects only where policy
// allows, checking each resolved address when it dials, and never follows a
// redirect. allowPrivateHTTP selects the local profiles' policy, which also
// allows plain http to loopback and private addresses.
func NewProviderHTTPClient(allowPrivateHTTP bool) *http.Client {
	policy := providerhttp.PublicHTTPS
	if allowPrivateHTTP {
		policy = providerhttp.PrivateNetworkHTTP
	}
	transport, _ := http.DefaultTransport.(*http.Transport)
	transport = transport.Clone()
	transport.MaxConnsPerHost = 8
	transport.MaxIdleConnsPerHost = 4
	transport.MaxIdleConns = 64
	// The base client has no other transport, so the policy client cannot fail.
	client, err := providerhttp.NewAddressPolicyClient(&http.Client{Transport: transport}, providerRequestTimeout, policy)
	if err != nil {
		panic(err)
	}
	return client
}

// credentialText is a credential field's string, "" when absent.
func credentialText(credentials map[string]json.RawMessage, name string) string {
	var value string
	if name != "" {
		_ = json.Unmarshal(credentials[name], &value)
	}
	return value
}
