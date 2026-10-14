// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package githubapp authenticates as one installation of the GitHub App: it signs
// the App JWT, mints installation and repository-scoped tokens, and calls the
// GitHub REST API with them. The GitHub repository host uses it, and so do the
// other features that act through the same App.
package githubapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// App acts as one installation of a GitHub App. Bootstrap validates every field
// at startup.
type App struct {
	AppID, InstallationID int64
	PrivateKey            *rsa.PrivateKey
	// Permissions, when set, restricts the installation token of Call to these
	// permissions, such as issues alone for a feature that needs nothing else
	// of the installation; empty, the token holds every permission of the App.
	Permissions map[string]string

	mu      sync.Mutex
	token   string
	expires time.Time
}

// Token is an installation access token and its expiry.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

// ParsePrivateKey reads the RSA private key of the App, as PKCS#1 or PKCS#8 PEM.
func ParsePrivateKey(value []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(value)
	if block == nil {
		return nil, errors.New("GitHub private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("GitHub private key is invalid")
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("GitHub App requires an RSA key")
	}
	return rsaKey, nil
}

// RepositoryToken mints a token restricted to the contents of one repository,
// for the coding tools and builds of that repository.
func (a *App) RepositoryToken(ctx context.Context, repositoryID int64) (Token, error) {
	return a.mintToken(ctx, []int64{repositoryID})
}

// OperationToken mints a fresh token for one operation, restricted to the
// named repositories of the installation (names without the owner), or to no
// repository when none is named, and to permissions. It is never cached: the
// caller holds it for that operation only.
func (a *App) OperationToken(ctx context.Context, repositories []string, permissions map[string]string) (Token, error) {
	jwt, err := a.jwt()
	if err != nil {
		return Token{}, err
	}
	payload := map[string]any{"permissions": permissions}
	if len(repositories) > 0 {
		payload["repositories"] = repositories
	}
	return mint(ctx, jwt, a.InstallationID, payload)
}

// CallWithToken sends one JSON request to the GitHub API authenticated by a
// token of OperationToken. A lost or unreadable response is an
// *platform.OutcomeUnknownError matching ErrOutcomeUnknown.
func (a *App) CallWithToken(ctx context.Context, token Token, method, path string, input, output any) error {
	return request(ctx, method, path, token.Value, input, output)
}

// installationToken returns the cached installation-wide token, minting a new
// one when it expires within a minute.
func (a *App) installationToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token == "" || !a.expires.After(time.Now().Add(time.Minute)) {
		minted, err := a.mintToken(ctx, nil)
		if err != nil {
			return "", err
		}
		a.token, a.expires = minted.Value, minted.ExpiresAt
	}
	return a.token, nil
}

func (a *App) mintToken(ctx context.Context, repositoryIDs []int64) (Token, error) {
	jwt, err := a.jwt()
	if err != nil {
		return Token{}, err
	}
	payload := map[string]any{}
	switch {
	case repositoryIDs != nil:
		payload["repository_ids"] = repositoryIDs
		payload["permissions"] = map[string]string{"contents": "write", "metadata": "read"}
	case len(a.Permissions) > 0:
		payload["permissions"] = a.Permissions
	}
	return mint(ctx, jwt, a.InstallationID, payload)
}

// mint asks GitHub, as the App, for a token of the installation with the
// restrictions of payload.
func mint(ctx context.Context, jwt string, installationID int64, payload map[string]any) (Token, error) {
	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	now := time.Now()
	if err := request(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(installationID, 10)+"/access_tokens", jwt, payload, &result); err != nil {
		return Token{}, err
	}
	if result.Token == "" || !result.ExpiresAt.After(now) {
		return Token{}, errors.New("GitHub returned an invalid installation credential")
	}
	return Token{Value: result.Token, ExpiresAt: result.ExpiresAt}, nil
}

// jwt signs the short-lived token that authenticates the App itself, for the
// endpoints that describe the App and its installations.
func (a *App) jwt() (string, error) {
	encode := base64.RawURLEncoding.EncodeToString
	now := time.Now()
	claims := comm.Must(json.Marshal(struct {
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
		Issuer    string `json:"iss"`
	}{now.Add(-time.Minute).Unix(), now.Add(9 * time.Minute).Unix(), strconv.FormatInt(a.AppID, 10)}))
	input := encode([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + encode(claims)
	sum := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.PrivateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return input + "." + encode(signature), nil
}

// Call sends one JSON request to the GitHub API as the installation. A lost or
// unreadable response is an *platform.OutcomeUnknownError matching
// ErrOutcomeUnknown: the call may have taken effect.
func (a *App) Call(ctx context.Context, method, path string, input, output any) error {
	token, err := a.installationToken(ctx)
	if err != nil {
		return err
	}
	return request(ctx, method, path, token, input, output)
}

// CallAsApp sends one JSON request authenticated by the App JWT, for the
// endpoints of the App itself.
func (a *App) CallAsApp(ctx context.Context, method, path string, output any) error {
	jwt, err := a.jwt()
	if err != nil {
		return err
	}
	return request(ctx, method, path, jwt, nil, output)
}
