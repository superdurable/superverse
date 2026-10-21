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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

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

// anthropicAPIKey is the Claude API key; without it only the mock model runs.
func anthropicAPIKey() string { return os.Getenv("ANTHROPIC_API_KEY") }

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
