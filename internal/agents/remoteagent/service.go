// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/agents"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/tenancy/accounts"
	"golang.org/x/mod/semver"
)

// ErrCurrentExecutor refuses removal of the connection the Session names now.
var ErrCurrentExecutor = errors.New("use the Superverse agent before removing its current connection")

// Connections owns pairing and authentication; the Session owns executor authority.
// Only bootstrap assembles it, using existing singleton clients and SQL pool.
type Connections struct {
	DAO                  *remoteagentdao.DAO
	Sessions             *agents.SessionFlowService
	Endpoint             string
	MinimumBridgeVersion string
	AcceptDevBridge      bool
	PlatformAdminEmail   string
}

// Pairing is the one-time Studio response; Code is never listed or persisted.
type Pairing struct {
	ConnectionID string
	Code         string
	ExpiresAt    time.Time
	Endpoint     string
	Executor     remoteexec.Executor
}

// CreatePairing admits an authenticated Builder and records the Session's
// current epoch as the immutable basis for a later RAEXEC activation.
func (s *Connections) CreatePairing(ctx context.Context, account accounts.AccountSummary, loginSessionID, projectID string) (Pairing, error) {
	if _, err := s.Sessions.AuthorizeSession(ctx, account, projectID); err != nil {
		return Pairing{}, err
	}
	executor, err := s.Sessions.Agent.ReadExecutor(ctx, projectID)
	if err != nil {
		return Pairing{}, err
	}
	if executor.Epoch > uint64(1<<63-1) {
		return Pairing{}, errors.New("executor epoch exceeds credential storage range")
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	random := make([]byte, 10)
	if _, err = rand.Read(random); err != nil {
		return Pairing{}, err
	}
	for i := range random {
		random[i] = alphabet[random[i]&31]
	}
	code, now := string(random), time.Now().UTC()
	pairing := Pairing{ConnectionID: "remote-" + flowid.RandomID(20), Code: code,
		ExpiresAt: now.Add(10 * time.Minute), Endpoint: s.Endpoint, Executor: executor}
	err = s.DAO.CreatePairing(ctx, remoteagentdao.Connection{
		ConnectionID: pairing.ConnectionID, ProjectID: projectID, AccountID: account.AccountID, LoginSessionID: loginSessionID,
		PairingCodeHash: sql.Null[string]{V: credentialHash(code), Valid: true}, PairingExpiresAt: pairing.ExpiresAt,
		BasisEpoch: int64(executor.Epoch), CreatedAt: now,
	}, s.PlatformAdminEmail, accountdao.StatusActive, authorizeAccess)
	if errors.Is(err, remoteagentdao.ErrCredentialInvalid) {
		return Pairing{}, accounts.ErrInvalidCredential
	}
	if err != nil {
		return Pairing{}, err
	}
	return pairing, nil
}

// pair redeems a code exactly once, after checking Bridge compatibility.
func (s *Connections) pair(ctx context.Context, request bridgeapi.PairRequest) (bridgeapi.PairResponse, error) {
	code := normalizeCode(request.Code)
	if len(code) != 10 || strings.Trim(code, "0123456789ABCDEFGHJKMNPQRSTVWXYZ") != "" {
		return bridgeapi.PairResponse{}, refusal(bridgeapi.ErrorPairingCodeInvalid)
	}
	if request.Bridge.OS != "darwin" && request.Bridge.OS != "linux" || request.Bridge.Arch != "arm64" && request.Bridge.Arch != "amd64" || len(request.Bridge.Version) > 64 || request.Bridge.Version == "" {
		return bridgeapi.PairResponse{}, refusal(bridgeapi.ErrorMalformed)
	}
	if !(s.AcceptDevBridge && request.Bridge.Version == bridgeapi.DevBridgeVersion) &&
		(!semver.IsValid("v"+request.Bridge.Version) || semver.Compare("v"+request.Bridge.Version, "v"+s.MinimumBridgeVersion) < 0) {
		return bridgeapi.PairResponse{}, refusal(bridgeapi.ErrorBridgeOutdated)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return bridgeapi.PairResponse{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	access, err := s.DAO.Redeem(ctx, credentialHash(code), credentialHash(token), s.PlatformAdminEmail, accountdao.StatusActive, func(access remoteagentdao.Access) error {
		if err := authorizeAccess(access); errors.Is(err, postgres.ErrNotFound) {
			return refusal(bridgeapi.ErrorNotFound)
		} else {
			return err
		}
	})
	if errors.Is(err, postgres.ErrNotFound) {
		return bridgeapi.PairResponse{}, refusal(bridgeapi.ErrorPairingCodeInvalid)
	}
	if err != nil {
		return bridgeapi.PairResponse{}, err
	}
	return bridgeapi.PairResponse{ConnectionID: access.ConnectionID, Token: token, ProjectID: access.ProjectID, ProjectName: access.ProjectName.V}, nil
}

// authenticate admits a canonical 32-byte token with one current SQL join.
// It never reads a Flow; authorization and missing Project remain distinct.
func (s *Connections) authenticate(ctx context.Context, token string) (remoteagentdao.Access, error) {
	if len(token) != 43 {
		return remoteagentdao.Access{}, refusal(bridgeapi.ErrorUnauthorized)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return remoteagentdao.Access{}, refusal(bridgeapi.ErrorUnauthorized)
	}
	access, err := s.DAO.Authenticate(ctx, credentialHash(token), s.PlatformAdminEmail, accountdao.StatusActive)
	if errors.Is(err, postgres.ErrNotFound) {
		return remoteagentdao.Access{}, refusal(bridgeapi.ErrorUnauthorized)
	}
	if err != nil {
		return remoteagentdao.Access{}, err
	}
	if err = authorizeAccess(access); err != nil {
		return remoteagentdao.Access{}, err
	}
	if err = s.DAO.Touch(ctx, access.ConnectionID); err != nil {
		return remoteagentdao.Access{}, err
	}
	return access, nil
}

// disconnect revokes only the authenticated connection, without changing the
// Session executor or cancelling work it already admitted.
func (s *Connections) disconnect(ctx context.Context, access remoteagentdao.Access) error {
	return s.DAO.Revoke(ctx, access.ProjectID, access.ConnectionID, remoteagentdao.RevokeDisconnected)
}

// List returns credential records and the independently authoritative executor.
func (s *Connections) List(ctx context.Context, account accounts.AccountSummary, projectID string) ([]remoteagentdao.ListedConnection, remoteexec.Executor, error) {
	if _, err := s.Sessions.AuthorizeSession(ctx, account, projectID); err != nil {
		return nil, remoteexec.Executor{}, err
	}
	executor, err := s.Sessions.Agent.ReadExecutor(ctx, projectID)
	if err != nil {
		return nil, remoteexec.Executor{}, err
	}
	rows, err := s.DAO.List(ctx, projectID)
	return rows, executor, err
}

// Revoke refuses whichever connection is the executor at the admission read.
// Its post-revoke mirror closes activation races using an exact epoch CAS.
func (s *Connections) Revoke(ctx context.Context, account accounts.AccountSummary, projectID, connectionID string) error {
	caller, err := s.Sessions.AuthorizeSession(ctx, account, projectID)
	if err != nil {
		return err
	}
	executor, err := s.Sessions.Agent.ReadExecutor(ctx, projectID)
	if err != nil {
		return err
	}
	if executor.Remote != nil && executor.Remote.ConnectionID == connectionID {
		return ErrCurrentExecutor
	}
	if err := s.DAO.Revoke(ctx, projectID, connectionID, remoteagentdao.RevokeRemoved); err != nil {
		return err
	}
	return s.compensateRemoval(ctx, projectID, connectionID, caller.Actor)
}

func authorizeAccess(access remoteagentdao.Access) error {
	if !access.ProjectStatus.Valid || access.ProjectStatus.V == projects.StatusDeleting || access.ProjectStatus.V == projects.StatusDeleted {
		return postgres.ErrNotFound
	}
	if !projects.CapabilityBuild.Allows(access.PlatformAdmin, access.ProjectRole.V, access.TeamSpaceRole.V) {
		return projects.ErrForbidden
	}
	return nil
}

func credentialHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

// requestError carries a declared transport refusal without extending shared DTOs.
type requestError struct{ code bridgeapi.ErrorCode }

func (e *requestError) Error() string { return string(e.code) }

func refusal(code bridgeapi.ErrorCode) error { return &requestError{code: code} }

// normalizeCode accepts the formatted code used by Studio and the Bridge.
func normalizeCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(code))
}
