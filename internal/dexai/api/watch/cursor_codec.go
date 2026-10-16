// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package watch

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
)

// CursorCodec signs the cursors this API hands out: Watch resume cursors and
// listing page tokens. A cursor is bound to its scope, so it cannot be replayed
// against another caller, resource or query.
type CursorCodec struct{ key []byte }

// ErrInvalidCursor rejects a forged, stale or wrongly scoped cursor.
var ErrInvalidCursor = errors.New("invalid progress cursor")

type cursorPayload struct {
	Scope   string            `json:"scope"`
	Sources map[string]string `json:"sources"`
}

// NewCursorCodec signs with the login signing key, which accounts.NewTokenSigner
// has already required to hold at least 32 bytes.
func NewCursorCodec(key []byte) *CursorCodec {
	if len(key) < 32 {
		panic("cursor signing key must contain at least 32 bytes")
	}
	return &CursorCodec{key: append([]byte(nil), key...)}
}

// EncodePage signs the next-page token of a provider; the last page has none.
func (c *CursorCodec) EncodePage(scope, token string) string {
	if token == "" {
		return ""
	}
	return c.encode("listing/"+scope, map[string]string{"next": token})
}

// DecodePage verifies an untrusted page token of scope and returns the
// provider token it signs.
func (c *CursorCodec) DecodePage(scope, cursor string) (string, error) {
	values, err := c.decode("listing/"+scope, cursor)
	if err != nil {
		return "", err
	}
	return values["next"], nil
}

// encode signs the cursors of scope.
func (c *CursorCodec) encode(scope string, cursors map[string]string) string {
	data := comm.Must(json.Marshal(cursorPayload{scope, cursors}))
	encoded := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// decode verifies an untrusted cursor of scope. The empty cursor resumes nothing.
func (c *CursorCodec) decode(scope, token string) (map[string]string, error) {
	if token == "" {
		return map[string]string{}, nil
	}
	if len(token) > 32768 {
		return nil, ErrInvalidCursor
	}
	encoded, signature, found := strings.Cut(token, ".")
	if !found || strings.Contains(signature, ".") {
		return nil, ErrInvalidCursor
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(encoded))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, ErrInvalidCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var payload cursorPayload
	if json.Unmarshal(data, &payload) != nil || payload.Scope != scope || len(payload.Sources) > 32 {
		return nil, ErrInvalidCursor
	}
	return payload.Sources, nil
}
