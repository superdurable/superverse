// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package localgit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// tokenPrefix names the token format. A token is
//
//	sv2lg1.<base64url(claims JSON)>.<base64url(HMAC-SHA256(key, "sv2lg1." + base64url(claims JSON)))>
//
// with unpadded base64url. infra-as-code/scripts/kind/git_host.py mints the
// administrator token of the E2E in this same format.
const tokenPrefix = "sv2lg1"

// tokenScope is what a token may reach.
type tokenScope string

const (
	// scopeRepository reads and pushes one repository.
	scopeRepository tokenScope = "repository"
	// scopeAdministrator reads the JSON interface of every repository.
	scopeAdministrator tokenScope = "administrator"
)

// tokenClaims are the signed content of a token.
type tokenClaims struct {
	Scope tokenScope `json:"scope"`
	// RepositoryID is the one repository of a repository token.
	RepositoryID int64 `json:"repositoryId,omitempty"`
	// ExpiresAt is the Unix time in seconds after which the token is refused.
	ExpiresAt int64 `json:"expiresAt"`
}

var (
	errTokenInvalid = errors.New("token is not a valid signed token")
	errTokenExpired = errors.New("token has expired")
)

// tokenSigner signs and verifies tokens with the server's local Git signing key.
type tokenSigner struct{ key []byte }

func (s tokenSigner) mint(claims tokenClaims) string {
	payload := base64.RawURLEncoding.EncodeToString(comm.Must(json.Marshal(claims)))
	return tokenPrefix + "." + payload + "." + s.sign(payload)
}

func (s tokenSigner) sign(payload string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(tokenPrefix + "." + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verify returns the claims of a token whose signature is valid and that has not
// expired at now.
func (s tokenSigner) verify(token string, now time.Time) (tokenClaims, error) {
	prefix, rest, found := strings.Cut(token, ".")
	payload, signature, signed := strings.Cut(rest, ".")
	if !found || !signed || prefix != tokenPrefix || !hmac.Equal([]byte(signature), []byte(s.sign(payload))) {
		return tokenClaims{}, errTokenInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return tokenClaims{}, errTokenInvalid
	}
	var claims tokenClaims
	if err = json.Unmarshal(data, &claims); err != nil || (claims.Scope != scopeRepository && claims.Scope != scopeAdministrator) {
		return tokenClaims{}, errTokenInvalid
	}
	if claims.ExpiresAt <= now.Unix() {
		return tokenClaims{}, errTokenExpired
	}
	return claims, nil
}
