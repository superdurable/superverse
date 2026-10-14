// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/tenancy/teamspaces"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// watchRevocationBound is the Watch's authorization interval, 5 seconds, with
// a margin for one authorization and the stream's teardown.
const watchRevocationBound = 8 * time.Second

// TestWatchSessionRevocationDexIntegration opens WatchSession through the
// lane's business API as a builder of a Project whose Session is idle, so every
// Stream read of the Watch blocks. The owner removes the builder; once the read
// model no longer grants the builder the Project, the stream must end with
// PERMISSION_DENIED within the Watch's authorization interval, with no frame
// needed to notice it.
func TestWatchSessionRevocationDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	s := newMockSession(t, ctx, "")
	project := s.identity.ProjectID

	connection, err := grpc.NewClient(dexflowtest.Setting(t, "SUPERVERSE_TEST_API_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("the business API connection failed: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	builder := registerCaller(ctx, t, pb.NewAccountServiceClient(connection))
	member := builder.GetAccountId()

	teamSpaceFlow := &teamspaces.TeamSpaceFlow{}
	teamSpaces := &teamspaces.TeamSpaceFlowClient{Dex: dexflowtest.ServerWorkerClient(t, teamSpaceFlow), Flow: teamSpaceFlow}
	joined, err := teamSpaces.AddMember(ctx, s.teamSpaceID, teamspaces.MemberChange{Actor: teamspaces.Actor{AccountID: s.account}, AccountID: member, Role: teamspaces.RoleMember})
	if err != nil || joined.Rejection != nil {
		t.Fatalf("the TeamSpace did not admit the builder: %v %v", err, joined.Rejection)
	}
	added, err := s.projects.AddMember(ctx, project, projects.MemberChange{ActorAccountID: s.account, AccountID: member, Role: projects.RoleBuilder})
	if err != nil || added.Rejection != nil {
		t.Fatalf("the Project did not admit the builder: %v %v", err, added.Rejection)
	}
	s.await(ctx, "the builder's access in the read model", func() bool {
		_, err := s.projects.Authorize(ctx, member, project, projects.CapabilityBuild)
		return err == nil
	})

	stream, err := pb.NewSessionServiceClient(connection).WatchSession(ctx, &pb.WatchSessionRequest{ProjectId: project, Caller: builder})
	if err != nil {
		t.Fatalf("the builder's WatchSession was refused: %v", err)
	}
	if first, err := stream.Recv(); err != nil || first.GetSnapshot() == nil {
		t.Fatalf("the Watch did not start with a snapshot: %v", err)
	}
	ended := make(chan error, 1)
	go func() {
		for {
			if _, err := stream.Recv(); err != nil {
				ended <- err
				return
			}
		}
	}()

	removed, err := s.projects.RemoveMember(ctx, project, projects.MemberChange{ActorAccountID: s.account, AccountID: member})
	if err != nil || removed.Rejection != nil {
		t.Fatalf("the owner could not remove the builder: %v %v", err, removed.Rejection)
	}
	s.await(ctx, "the removal in the read model", func() bool {
		_, err := s.projects.Authorize(ctx, member, project, projects.CapabilityBuild)
		return errors.Is(err, projects.ErrForbidden)
	})
	revoked := time.Now()
	select {
	case err = <-ended:
	case <-time.After(watchRevocationBound):
		t.Fatalf("the Watch still served the removed builder %s after the read model revoked its access", watchRevocationBound)
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("the Watch of the removed builder ended with %v, not PERMISSION_DENIED", err)
	}
	t.Logf("Project %s: the removed builder's WatchSession ended %s after the read model revoked its access", project, time.Since(revoked).Round(time.Millisecond))
}

// registerCaller registers an ordinary account through the business API and
// returns the caller context of its login.
func registerCaller(ctx context.Context, t *testing.T, accounts pb.AccountServiceClient) *pb.CallerContext {
	t.Helper()
	request := flowid.RandomID(24)
	registered, err := accounts.RegisterAccount(ctx, &pb.RegisterAccountRequest{RequestId: request, Email: "watch-test-" + request + "@example.com", Password: request})
	login := registered.GetLogin()
	if err != nil || login.GetAccount().GetAccountId() == "" {
		t.Fatalf("the account registration failed (%s)", status.Code(err))
	}
	t.Cleanup(func() {
		caller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
		_, _ = accounts.Logout(context.WithoutCancel(ctx), &pb.LogoutRequest{Caller: caller})
	})
	return &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
}
