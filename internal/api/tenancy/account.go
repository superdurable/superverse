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

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/api/boundary"
	"github.com/superdurable/superversev2/internal/tenancy/accounts"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	login, err := s.Accounts.Register(ctx, in.GetEmail(), in.GetPassword(), in.GetRequestId())
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.RegisterAccountResponse{Login: accountLogin(login)}, nil
}

func (s *accountServer) Login(ctx context.Context, in *pb.LoginRequest) (*pb.LoginResponse, error) {
	login, err := s.Accounts.Login(ctx, in.GetEmail(), in.GetPassword())
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.LoginResponse{Login: accountLogin(login)}, nil
}

func (s *accountServer) Logout(ctx context.Context, in *pb.LogoutRequest) (*pb.LogoutResponse, error) {
	if _, err := s.Authenticate(ctx, in.GetCaller()); err != nil {
		return nil, err
	}
	if err := s.Accounts.Logout(ctx, in.GetCaller().GetSessionToken()); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.LogoutResponse{}, nil
}

func (s *accountServer) GetCurrentAccount(ctx context.Context, in *pb.GetCurrentAccountRequest) (*pb.GetCurrentAccountResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	return &pb.GetCurrentAccountResponse{Account: accountSnapshot(account)}, nil
}

func accountSnapshot(a accounts.AccountSummary) *pb.AccountSnapshot {
	return &pb.AccountSnapshot{AccountId: a.AccountID, Email: a.CanonicalEmail, IsAdmin: a.PlatformAdmin}
}

func accountLogin(l accounts.Login) *pb.AccountLogin {
	return &pb.AccountLogin{Account: accountSnapshot(l.Account), SessionToken: l.Token, ExpiresAt: timestamppb.New(l.ExpiresAt)}
}
