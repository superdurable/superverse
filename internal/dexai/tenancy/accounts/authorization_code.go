// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
)

var (
	ErrInvalidClient   = errors.New("invalid login exchange client")
	ErrInvalidGrant    = errors.New("invalid login authorization grant")
	ErrRateLimited     = errors.New("login handoff rate limited")
	ErrHandoffBusy     = errors.New("login handoff unavailable")
	ErrHandoffDisabled = errors.New("login handoff disabled")
)

// LoginProduct is an exact first-party client. Credentials are private bytes;
// URLs and client identities come from the validated environment registry.
type LoginProduct struct {
	ID, Origin, RedirectURI string
	Audience                ProductAudience
	Secrets                 SecretRing
}

// ProductAudience is the closed set of first-party session audiences.
type ProductAudience string

const (
	ProductAudienceStudio ProductAudience = "studio"
	ProductAudienceEngine ProductAudience = "engine"
)

func (audience ProductAudience) valid() bool {
	return audience == ProductAudienceStudio || audience == ProductAudienceEngine
}

// SecretRing accepts a current key and one previous key until its explicit
// expiry, no later than 24 hours after a rotation begins.
type SecretRing struct {
	Current, Previous  []byte
	PreviousValidUntil time.Time
}

// LoginHost owns the two bounded per-replica admission budgets and credentials.
// Its SQL DAO, rather than this process, owns single use and session admission.
type LoginHost struct {
	issuerSecrets                   SecretRing
	products                        []LoginProduct
	handoffEnabled, exchangeEnabled bool
	issue                           *loginBucket
	exchange                        *loginBucket
	slots                           chan struct{}
}

// NewLoginHost creates the process-local overload controls. Configuration has
// already validated the positive rates and exact, distinct registered origins.
func NewLoginHost(issuerSecrets SecretRing, products []LoginProduct, handoffEnabled, exchangeEnabled bool,
	issuePerMinute, issueBurst, exchangePerMinute, exchangeBurst int) *LoginHost {
	return &LoginHost{
		issuerSecrets: issuerSecrets, products: products,
		handoffEnabled: handoffEnabled, exchangeEnabled: exchangeEnabled,
		issue:    newLoginBucket(issuePerMinute, issueBurst),
		exchange: newLoginBucket(exchangePerMinute, exchangeBurst),
		slots:    make(chan struct{}, 32),
	}
}

// AdmitIssue counts an attempt before validating issuer credentials and keeps
// admission within the server's fixed 32-operation SQL budget.
func (h *LoginHost) AdmitIssue() (func(), error) {
	if h == nil || !h.handoffEnabled {
		return nil, ErrHandoffDisabled
	}
	return h.admit(h.issue)
}

// AdmitExchange has an independent rate bucket and shares the SQL semaphore.
func (h *LoginHost) AdmitExchange() (func(), error) {
	if h == nil || !h.exchangeEnabled {
		return nil, ErrHandoffDisabled
	}
	return h.admit(h.exchange)
}

func (h *LoginHost) admit(bucket *loginBucket) (func(), error) {
	if h == nil {
		return nil, ErrHandoffDisabled
	}
	if !bucket.take(time.Now()) {
		return nil, ErrRateLimited
	}
	select {
	case h.slots <- struct{}{}:
		return func() { <-h.slots }, nil
	default:
		return nil, ErrHandoffBusy
	}
}

// AuthenticatesIssuer accepts only the login BFF's separate service secret.
func (h *LoginHost) AuthenticatesIssuer(secret []byte) bool {
	return h != nil && h.issuerSecrets.matches(secret)
}

// Product authenticates a configured client before any code lookup.
func (h *LoginHost) Product(id string, secret []byte) (LoginProduct, error) {
	if h == nil {
		return LoginProduct{}, ErrHandoffDisabled
	}
	for _, product := range h.products {
		if product.ID == id && product.Audience.valid() && product.Secrets.matches(secret) {
			return product, nil
		}
	}
	return LoginProduct{}, ErrInvalidClient
}

// IssueAuthorizationCode creates a fresh opaque grant for a login-audience
// caller. A registered origin alone selects its fixed product callback.
func (a *Accounts) IssueAuthorizationCode(ctx context.Context, accountID, email, token, origin, redirectURI,
	challenge string) (string, time.Time, error) {
	if a.LoginHost == nil {
		return "", time.Time{}, ErrHandoffDisabled
	}
	registered := false
	for _, product := range a.LoginHost.products {
		if origin == product.Origin && redirectURI == product.RedirectURI {
			registered = true
			break
		}
	}
	digest, err := decodeGrantPart(challenge)
	if !registered || err != nil || len(origin) > 512 || len(redirectURI) > 512 {
		return "", time.Time{}, ErrInvalidGrant
	}
	account, issuerID, err := a.AuthenticateLoginHost(ctx, accountID, email, token)
	if err != nil {
		return "", time.Time{}, err
	}
	random := make([]byte, 32)
	comm.Must(rand.Read(random))
	code := "lh1_" + base64.RawURLEncoding.EncodeToString(random)
	hash := sha256.Sum256([]byte(code))
	expiry, err := a.DAO.IssueAuthorizationCode(ctx, accountdao.AuthorizationCode{
		Hash: hash[:], Challenge: digest, AccountID: account.AccountID,
		IssuerSessionID: issuerID, IssuerTokenHash: tokenHash(token),
		ProductOrigin: origin, RedirectURI: redirectURI,
	})
	if errors.Is(err, accountdao.ErrInvalidGrant) {
		return "", time.Time{}, ErrInvalidGrant
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return code, expiry, nil
}

// ExchangeAuthorizationCode atomically turns one grant into an independent
// product session. No response is replayed after a committed redemption.
func (a *Accounts) ExchangeAuthorizationCode(ctx context.Context, product LoginProduct, code, verifier,
	redirectURI string) (Login, error) {
	if !product.Audience.valid() || redirectURI != product.RedirectURI || len(code) != 47 || code[:4] != "lh1_" {
		return Login{}, ErrInvalidGrant
	}
	if _, err := decodeGrantPart(code[4:]); err != nil {
		return Login{}, ErrInvalidGrant
	}
	if _, err := decodeGrantPart(verifier); err != nil {
		return Login{}, ErrInvalidGrant
	}
	challenge := sha256.Sum256([]byte(verifier))
	hash := sha256.Sum256([]byte(code))
	var token string
	account, session, err := a.DAO.ExchangeAuthorizationCode(ctx, hash[:], challenge[:], product.Origin,
		product.RedirectURI, func(account accountdao.Account, admittedAt time.Time) accountdao.LoginSession {
			sessionID := "login-" + flowid.RandomID(24)
			expiry := admittedAt.Add(loginLifetime)
			token = a.Signer.issue(accountKey(account.CanonicalEmail), sessionID, expiry)
			return accountdao.LoginSession{
				SessionID: sessionID, AccountID: account.AccountID, TokenHash: tokenHash(token),
				Audience: string(product.Audience), IssuedAt: admittedAt, ExpiresAt: expiry,
			}
		})
	if errors.Is(err, accountdao.ErrInvalidGrant) {
		return Login{}, ErrInvalidGrant
	}
	if err != nil {
		return Login{}, err
	}
	return Login{Account: a.summary(account), Token: token, ExpiresAt: session.ExpiresAt, SessionID: session.SessionID}, nil
}

func decodeGrantPart(value string) ([]byte, error) {
	if len(value) != 43 {
		return nil, ErrInvalidGrant
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, ErrInvalidGrant
	}
	return decoded, nil
}

func (ring SecretRing) matches(candidate []byte) bool {
	if len(candidate) != 32 {
		return false
	}
	valid := subtle.ConstantTimeCompare(ring.Current, candidate)
	if time.Now().Before(ring.PreviousValidUntil) {
		valid |= subtle.ConstantTimeCompare(ring.Previous, candidate)
	}
	return valid == 1
}

type loginBucket struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	tokens  float64
	updated time.Time
}

func newLoginBucket(perMinute, burst int) *loginBucket {
	return &loginBucket{rate: float64(perMinute) / 60, burst: float64(burst), tokens: float64(burst), updated: time.Now()}
}

func (b *loginBucket) take(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens += now.Sub(b.updated).Seconds() * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.updated = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
