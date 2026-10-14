// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package boundary

import (
	"context"
	"log/slog"

	"github.com/superdurable/dex/sdk-go/dex"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/api/watch"
	"github.com/superdurable/superversev2/internal/tenancy/accounts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Boundary is the trust boundary that every business gRPC handler shares: it
// authenticates the explicit caller, recovers panics, signs cursors and maps
// outcomes to public statuses.
type Boundary struct {
	Logger   *slog.Logger
	Accounts *accounts.Accounts
	Dex      *dex.Client
	Cursors  *watch.CursorCodec
}

// Authenticate verifies the explicit caller of an RPC against its current login.
func (b *Boundary) Authenticate(ctx context.Context, caller *pb.CallerContext) (accounts.AccountSummary, error) {
	if caller == nil {
		return accounts.AccountSummary{}, status.Error(codes.Unauthenticated, "explicit caller identity is required")
	}
	account, err := b.Accounts.Authenticate(ctx, caller.GetAccountId(), caller.GetCanonicalEmail(), caller.GetSessionToken())
	return account, RPCStatus(err)
}

// watchRequest is the caller and resume cursor that every Watch RPC names.
type watchRequest interface {
	GetCaller() *pb.CallerContext
	GetResumeCursor() string
}

// BindWatch binds a Watch to the caller and resume cursor of in. A failed
// reauthentication stays unmapped, because a Watch maps every error once,
// where it ends.
func (b *Boundary) BindWatch(in watchRequest, scope string) watch.Connection {
	caller := in.GetCaller()
	return watch.Connection{
		Scope: scope, ResumeCursor: in.GetResumeCursor(), Cursors: b.Cursors, Logger: b.Logger,
		Reauthenticate: func(ctx context.Context) (string, error) {
			account, err := b.Accounts.Authenticate(ctx, caller.GetAccountId(), caller.GetCanonicalEmail(), caller.GetSessionToken())
			return account.AccountID, err
		},
	}
}
