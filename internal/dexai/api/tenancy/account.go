// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package tenancy

import (
	"context"
	"encoding/base64"
	"errors"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	loginClientMetadata = "x-superverse-auth-client"
	loginSecretMetadata = "x-superverse-auth-secret"
)

// accountServer serves AccountService, which the browser reaches only through
// Studio's /api/auth, the only writer of the login cookie.
type accountServer struct {
	pb.UnimplementedAccountServiceServer
	*boundary.Boundary
}

func (s *accountServer) RegisterAccount(ctx context.Context, in *pb.RegisterAccountRequest) (*pb.RegisterAccountResponse, error) {
	if err := boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	onLogin, err := s.loginSurface(ctx)
	if err != nil {
		return nil, err
	}
	var login accounts.Login
	if onLogin {
		login, err = s.Accounts.RegisterOnLoginHost(ctx, in.GetEmail(), in.GetPassword(), in.GetRequestId())
	} else {
		login, err = s.Accounts.Register(ctx, in.GetEmail(), in.GetPassword(), in.GetRequestId())
	}
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.RegisterAccountResponse{Login: accountLogin(login)}, nil
}

func (s *accountServer) Login(ctx context.Context, in *pb.LoginRequest) (*pb.LoginResponse, error) {
	onLogin, err := s.loginSurface(ctx)
	if err != nil {
		return nil, err
	}
	var login accounts.Login
	if onLogin {
		login, err = s.Accounts.LoginOnLoginHost(ctx, in.GetEmail(), in.GetPassword())
	} else {
		login, err = s.Accounts.Login(ctx, in.GetEmail(), in.GetPassword())
	}
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.LoginResponse{Login: accountLogin(login)}, nil
}

func (s *accountServer) Logout(ctx context.Context, in *pb.LogoutRequest) (*pb.LogoutResponse, error) {
	if _, err := s.authenticateLogoutSurface(ctx, in.GetCaller()); err != nil {
		return nil, err
	}
	if err := s.Accounts.Logout(ctx, in.GetCaller().GetSessionToken()); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.LogoutResponse{}, nil
}

func (s *accountServer) authenticateLogoutSurface(ctx context.Context, caller *pb.CallerContext) (accounts.AccountSummary, error) {
	id, secret, supplied := loginCredential(ctx)
	if !supplied || id != string(accounts.ProductAudienceEngine) {
		return s.authenticateAccountSurface(ctx, caller)
	}
	product, err := s.Accounts.LoginHost.Product(id, secret)
	if err != nil {
		return accounts.AccountSummary{}, handoffStatus(err)
	}
	if caller == nil {
		return accounts.AccountSummary{}, status.Error(codes.Unauthenticated, "Engine caller required")
	}
	account, _, err := s.Accounts.AuthenticateProduct(ctx, caller.GetAccountId(), caller.GetCanonicalEmail(), caller.GetSessionToken(), product.Audience)
	return account, boundary.RPCStatus(err)
}

func (s *accountServer) GetCurrentAccount(ctx context.Context, in *pb.GetCurrentAccountRequest) (*pb.GetCurrentAccountResponse, error) {
	account, err := s.authenticateAccountSurface(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	return &pb.GetCurrentAccountResponse{Account: accountSnapshot(account)}, nil
}

func (s *accountServer) CreateLoginAuthorizationCode(ctx context.Context, in *pb.CreateLoginAuthorizationCodeRequest) (*pb.CreateLoginAuthorizationCodeResponse, error) {
	release, err := s.Accounts.LoginHost.AdmitIssue()
	if err != nil {
		return nil, handoffStatus(err)
	}
	defer release()
	id, secret, supplied := loginCredential(ctx)
	if !supplied || id != "login" || !s.Accounts.LoginHost.AuthenticatesIssuer(secret) {
		return nil, status.Error(codes.Unauthenticated, "invalid login client")
	}
	if in.GetCaller() == nil {
		return nil, status.Error(codes.Unauthenticated, "login caller required")
	}
	caller := in.GetCaller()
	code, expiresAt, err := s.Accounts.IssueAuthorizationCode(ctx, caller.GetAccountId(), caller.GetCanonicalEmail(), caller.GetSessionToken(),
		in.GetProductOrigin(), in.GetRedirectUri(), in.GetCodeChallenge())
	if err != nil {
		return nil, handoffStatus(err)
	}
	return &pb.CreateLoginAuthorizationCodeResponse{AuthorizationCode: &pb.LoginAuthorizationCode{Code: code, ExpiresAt: timestamppb.New(expiresAt)}}, nil
}

func (s *accountServer) ExchangeLoginAuthorizationCode(ctx context.Context, in *pb.ExchangeLoginAuthorizationCodeRequest) (*pb.ExchangeLoginAuthorizationCodeResponse, error) {
	release, err := s.Accounts.LoginHost.AdmitExchange()
	if err != nil {
		return nil, handoffStatus(err)
	}
	defer release()
	if in.GetCaller() != nil {
		return nil, status.Error(codes.InvalidArgument, "caller is forbidden on exchange")
	}
	id, secret, supplied := loginCredential(ctx)
	if !supplied {
		return nil, status.Error(codes.Unauthenticated, "invalid login client")
	}
	product, err := s.Accounts.LoginHost.Product(id, secret)
	if err != nil {
		return nil, handoffStatus(err)
	}
	login, err := s.Accounts.ExchangeAuthorizationCode(ctx, product, in.GetCode(), in.GetCodeVerifier(), in.GetRedirectUri())
	if err != nil {
		return nil, handoffStatus(err)
	}
	return &pb.ExchangeLoginAuthorizationCodeResponse{Login: accountLogin(login)}, nil
}

func (s *accountServer) loginSurface(ctx context.Context) (bool, error) {
	id, secret, supplied := loginCredential(ctx)
	if !supplied {
		return false, nil
	}
	if id != "login" || !s.Accounts.LoginHost.AuthenticatesIssuer(secret) {
		return false, status.Error(codes.Unauthenticated, "invalid login client")
	}
	return true, nil
}

func (s *accountServer) authenticateAccountSurface(ctx context.Context, caller *pb.CallerContext) (accounts.AccountSummary, error) {
	onLogin, err := s.loginSurface(ctx)
	if err != nil {
		return accounts.AccountSummary{}, err
	}
	if !onLogin {
		return s.Authenticate(ctx, caller)
	}
	if caller == nil {
		return accounts.AccountSummary{}, status.Error(codes.Unauthenticated, "login caller required")
	}
	account, _, err := s.Accounts.AuthenticateLoginHost(ctx, caller.GetAccountId(), caller.GetCanonicalEmail(), caller.GetSessionToken())
	return account, boundary.RPCStatus(err)
}

func loginCredential(ctx context.Context) (string, []byte, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", nil, false
	}
	ids, secrets := md.Get(loginClientMetadata), md.Get(loginSecretMetadata)
	if len(ids) == 0 && len(secrets) == 0 {
		return "", nil, false
	}
	if len(ids) != 1 || len(secrets) != 1 {
		return "", nil, true
	}
	secret, err := base64.RawURLEncoding.DecodeString(secrets[0])
	if err != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != secrets[0] {
		return "", nil, true
	}
	return ids[0], secret, true
}

func handoffStatus(err error) error {
	switch {
	case errors.Is(err, accounts.ErrInvalidClient):
		return status.Error(codes.Unauthenticated, "invalid login client")
	case errors.Is(err, accounts.ErrInvalidGrant), errors.Is(err, accountdao.ErrInvalidGrant):
		return status.Error(codes.InvalidArgument, "invalid login grant")
	case errors.Is(err, accounts.ErrRateLimited), errors.Is(err, accountdao.ErrCodeLimit):
		return status.Error(codes.ResourceExhausted, "login handoff rate limited")
	case errors.Is(err, accounts.ErrHandoffBusy), errors.Is(err, accounts.ErrHandoffDisabled):
		return status.Error(codes.Unavailable, "login handoff unavailable")
	default:
		return boundary.RPCStatus(err)
	}
}

func accountSnapshot(a accounts.AccountSummary) *pb.AccountSnapshot {
	return &pb.AccountSnapshot{AccountId: a.AccountID, Email: a.CanonicalEmail, IsAdmin: a.PlatformAdmin}
}

func accountLogin(l accounts.Login) *pb.AccountLogin {
	return &pb.AccountLogin{Account: accountSnapshot(l.Account), SessionToken: l.Token, ExpiresAt: timestamppb.New(l.ExpiresAt), SessionId: l.SessionID}
}
