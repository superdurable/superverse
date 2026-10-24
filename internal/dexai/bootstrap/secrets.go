// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

type loginSecretRing struct {
	Current            string `json:"current"`
	Previous           string `json:"previous"`
	PreviousValidUntil string `json:"previous_valid_until"`
}

func loginHostSecrets(settings serverconfig.LoginHostConfig) (*accounts.LoginHost, error) {
	if !settings.HandoffEnabled && !settings.ExchangeEnabled {
		return nil, nil
	}
	var issuer loginSecretRing
	if err := json.Unmarshal([]byte(os.Getenv("SUPERVERSE_LOGIN_HOST_ISSUER_SECRET_RING")), &issuer); err != nil {
		return nil, errors.New("invalid login issuer secret ring")
	}
	issuerKeys, err := decodeLoginSecretRing(issuer)
	if err != nil {
		return nil, err
	}
	var configured map[string]loginSecretRing
	if err := json.Unmarshal([]byte(os.Getenv("SUPERVERSE_LOGIN_HOST_CLIENT_SECRET_RINGS")), &configured); err != nil {
		return nil, errors.New("invalid login client secret rings")
	}
	products := make([]accounts.LoginProduct, 0, len(settings.Clients))
	for id, client := range settings.Clients {
		if !client.Enabled {
			continue
		}
		ring, exists := configured[id]
		if !exists {
			return nil, errors.New("an enabled login client has no secret ring")
		}
		keys, err := decodeLoginSecretRing(ring)
		if err != nil {
			return nil, err
		}
		var audience accounts.ProductAudience
		switch id {
		case "studio":
			audience = accounts.ProductAudienceStudio
		case "engine":
			audience = accounts.ProductAudienceEngine
		default:
			return nil, errors.New("an enabled login client has an unknown audience")
		}
		products = append(products, accounts.LoginProduct{ID: id, Origin: client.Origin,
			RedirectURI: client.RedirectURI, Audience: audience, Secrets: keys})
		delete(configured, id)
	}
	if len(configured) != 0 {
		return nil, errors.New("a login client secret has no enabled client")
	}
	return accounts.NewLoginHost(issuerKeys, products, settings.HandoffEnabled, settings.ExchangeEnabled,
		settings.IssuePerMinute, settings.IssueBurst, settings.ExchangePerMinute, settings.ExchangeBurst), nil
}

func decodeLoginSecretRing(value loginSecretRing) (accounts.SecretRing, error) {
	decode := func(raw string) ([]byte, error) {
		bytes, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(bytes) != 32 || base64.RawURLEncoding.EncodeToString(bytes) != raw {
			return nil, errors.New("invalid login secret ring key")
		}
		return bytes, nil
	}
	current, err := decode(value.Current)
	if err != nil {
		return accounts.SecretRing{}, err
	}
	if value.Previous == "" && value.PreviousValidUntil == "" {
		return accounts.SecretRing{Current: current}, nil
	}
	previous, err := decode(value.Previous)
	if err != nil {
		return accounts.SecretRing{}, err
	}
	until, err := time.Parse(time.RFC3339, value.PreviousValidUntil)
	if err != nil || !until.After(time.Now()) || until.After(time.Now().Add(24*time.Hour)) {
		return accounts.SecretRing{}, errors.New("invalid login secret overlap expiry")
	}
	return accounts.SecretRing{Current: current, Previous: previous, PreviousValidUntil: until}, nil
}

// The secrets of the server are the only settings it reads from its
// environment; every other setting is in cmd/superverse-server/config. The
// AWS SDK reads its own credentials from the environment or the pod identity.
// Nothing here is logged, put into an error or returned by an API.

// cookieSigningKey is the server-only key that signs Account session tokens
// and Watch cursors, and from which the keys of the connection forms' nonces
// derive; every replica reads the same key.
func cookieSigningKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SUPERVERSE_COOKIE_SIGNING_KEY"))
	if err != nil {
		return nil, errors.New("invalid login signing key encoding")
	}
	return key, nil
}

// localGitSigningKey is the key that signs the repository credentials of the
// local Git host: standard base64 of at least 32 bytes.
func localGitSigningKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SUPERVERSE_LOCAL_GIT_SIGNING_KEY"))
	if err != nil || len(key) < 32 {
		return nil, errors.New("SUPERVERSE_LOCAL_GIT_SIGNING_KEY must be standard base64 of at least 32 bytes")
	}
	return key, nil
}

// postgresPassword is the password of the server's database role, or of the
// owner role for migrations.
func postgresPassword(role string) (string, error) {
	variable := "SUPERVERSE_POSTGRES_PASSWORD"
	if role == postgres.OwnerRole {
		variable = "SUPERVERSE_POSTGRES_OWNER_PASSWORD"
	}
	password := os.Getenv(variable)
	if password == "" {
		return "", errors.New(variable + " is required")
	}
	return password, nil
}

// platformOAuthClients joins the configured platform OAuth clients with
// their secrets, SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS: a JSON object from
// "<connector>/<method>" to client secret. A configured client without a
// secret, or a secret without a client, refuses the start.
func platformOAuthClients(configured map[string]serverconfig.PlatformOAuthClient) (connections.PlatformClients, error) {
	clients := make(map[string]connections.PlatformClient, len(configured))
	for key, client := range configured {
		clients[key] = connections.PlatformClient{ClientID: client.ClientID, Scopes: client.Scopes}
	}
	return connections.ParsePlatformClientSecrets(os.Getenv("SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS"), clients)
}

// slackSigningSecret is the platform Slack application's Signing Secret,
// SUPERVERSE_SLACK_SIGNING_SECRET; the Slack events listener serves only
// with it.
func slackSigningSecret() []byte { return []byte(os.Getenv("SUPERVERSE_SLACK_SIGNING_SECRET")) }

// localBuildToken is the shared token of the Local Kind build service,
// preferably read from its file; it is empty when none is configured.
func localBuildToken() (string, error) {
	token := os.Getenv("SUPERVERSE_LOCAL_BUILD_TOKEN")
	if file := os.Getenv("SUPERVERSE_LOCAL_BUILD_TOKEN_FILE"); file != "" {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			return "", fmt.Errorf("read local build token: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	return token, nil
}

// gitHubPrivateKey prefers the key file, then the base64 value, then the raw value.
func gitHubPrivateKey() ([]byte, error) {
	if path := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH"); path != "" {
		key, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("read GitHub private key: %w", err)
		}
		return key, nil
	}
	if value := os.Getenv("GITHUB_APP_PRIVATE_KEY_BASE64"); value != "" {
		key, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, errors.New("invalid GitHub private key encoding")
		}
		return key, nil
	}
	if key := os.Getenv("GITHUB_APP_PRIVATE_KEY"); key != "" {
		return []byte(key), nil
	}
	return nil, errors.New("one of GITHUB_APP_PRIVATE_KEY_PATH, GITHUB_APP_PRIVATE_KEY_BASE64 or GITHUB_APP_PRIVATE_KEY is required")
}
