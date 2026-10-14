// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package signedtoken

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// Purpose is one use of signed tokens. Each purpose signs with its own key,
// derived from the root key, so a token of one purpose never verifies as
// another.
type Purpose struct{ label string }

// The purposes of signed tokens; the label is the key derivation's info.
var (
	// ConnectionForm binds a TeamSpace connection form to its actor and connection.
	ConnectionForm = Purpose{"superverse/connection-form/v1"}
	// ConnectorUISession binds a Project connector form to its actor, scope and source.
	ConnectorUISession = Purpose{"superverse/connector-ui-session/v1"}
)

// errInvalid is a token that is malformed, signed for another purpose or
// with another key, or expired.
var errInvalid = errors.New("signed token is invalid or expired")

// Signer seals and opens the tokens of one purpose. It holds no state, so
// every replica that derives it from the same root key opens the tokens of
// every other.
type Signer struct{ key []byte }

// New derives purpose's key from rootKey, which holds at least 32 bytes,
// with HKDF-SHA256.
func New(rootKey []byte, purpose Purpose) *Signer {
	if len(rootKey) < 32 || purpose.label == "" {
		panic("signed token root key must hold at least 32 bytes and the purpose must be declared")
	}
	return &Signer{key: comm.Must(hkdf.Key(sha256.New, rootKey, nil, purpose.label, sha256.Size))}
}

type envelope struct {
	ExpiresAt int64           `json:"exp"`
	Claims    json.RawMessage `json:"claims"`
}

// Seal signs claims, which marshal to JSON, until expiresAt.
func (s *Signer) Seal(claims any, expiresAt time.Time) string {
	sealed := envelope{ExpiresAt: expiresAt.Unix(), Claims: comm.Must(json.Marshal(claims))}
	payload := base64.RawURLEncoding.EncodeToString(comm.Must(json.Marshal(sealed)))
	return payload + "." + base64.RawURLEncoding.EncodeToString(s.sign(payload))
}

// Open verifies an untrusted token at now and decodes its claims strictly
// into claims. Every failure is errInvalid, which tells nothing more.
func (s *Signer) Open(token string, now time.Time, claims any) error {
	payload, signature, found := strings.Cut(token, ".")
	mac, err := base64.RawURLEncoding.DecodeString(signature)
	if !found || err != nil || !hmac.Equal(mac, s.sign(payload)) {
		return errInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return errInvalid
	}
	var sealed envelope
	if comm.DecodeStrictJSON(data, &sealed) != nil || !now.Before(time.Unix(sealed.ExpiresAt, 0)) || comm.DecodeStrictJSON(sealed.Claims, claims) != nil {
		return errInvalid
	}
	return nil
}

func (s *Signer) sign(payload string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}
