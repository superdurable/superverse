// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package accounts

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// maximumTokenLength bounds a bearer before it is parsed or hashed.
const maximumTokenLength = 2048

// loginClaims is the signed payload of a login token. It names the login; the
// login's login_sessions row decides whether the token is still valid.
type loginClaims struct {
	Account   string `json:"account"`
	SessionID string `json:"session"`
	ExpiresAt int64  `json:"expires"`
	Nonce     string `json:"nonce"`
}

// TokenSigner issues and verifies login tokens: base64url(claims).base64url(HMAC-SHA256).
type TokenSigner struct{ key []byte }

func NewTokenSigner(key []byte) (*TokenSigner, error) {
	if len(key) < 32 {
		return nil, errors.New("login signing key must contain at least 32 bytes")
	}
	return &TokenSigner{key: append([]byte(nil), key...)}, nil
}

func (s *TokenSigner) issue(account, sessionID string, expires time.Time) string {
	nonce := make([]byte, 32)
	// crypto/rand.Read never returns an error.
	comm.Must(rand.Read(nonce))
	payload := comm.Must(json.Marshal(loginClaims{account, sessionID, expires.Unix(), base64.RawURLEncoding.EncodeToString(nonce)}))
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(s.sign(encoded))
}

// verify checks the signature, shape and expiry of an untrusted bearer token.
func (s *TokenSigner) verify(token string, now time.Time) (loginClaims, error) {
	if len(token) > maximumTokenLength {
		return loginClaims{}, ErrInvalidCredential
	}
	encoded, signature, found := strings.Cut(token, ".")
	if !found || strings.Contains(signature, ".") {
		return loginClaims{}, ErrInvalidCredential
	}
	mac, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(mac, s.sign(encoded)) {
		return loginClaims{}, ErrInvalidCredential
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return loginClaims{}, ErrInvalidCredential
	}
	var claims loginClaims
	if json.Unmarshal(payload, &claims) != nil || !strings.HasPrefix(claims.Account, "account-") || !strings.HasPrefix(claims.SessionID, "login-") || claims.ExpiresAt <= now.Unix() {
		return loginClaims{}, ErrInvalidCredential
	}
	if nonce, err := base64.RawURLEncoding.DecodeString(claims.Nonce); err != nil || len(nonce) != 32 {
		return loginClaims{}, ErrInvalidCredential
	}
	return claims, nil
}

func (s *TokenSigner) sign(encoded string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(encoded))
	return mac.Sum(nil)
}

// tokenHash is the only form in which a login token is stored.
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IssueForTest is issue for the tests.
func (r *TokenSigner) IssueForTest(account string, sessionID string, expires time.Time) string {
	return r.issue(account, sessionID, expires)
}
