// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

// TestAccountGRPCIntegration drives the actual gRPC API into the accounts of
// the lane's PostgreSQL. Browser acceptance is separate.
func TestAccountGRPCIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	signer := comm.Must(accounts.NewTokenSigner([]byte(strings.Repeat("x", 32))))
	service := &accounts.Accounts{DAO: accountdao.New(postgrestest.OpenForTest(t), []string{"admin@superverse.test"}), Signer: signer}
	// The real Session Service rejects a malformed Project ID before it reads
	// any dependency: every Session, Agent and Preview request validates the
	// Project ID while it authorizes the Project. Its Project client has no
	// Dex client, so a request that reached Dex would fail otherwise.
	sessionService := &agents.SessionFlowService{Projects: &projects.ProjectFlowClient{ProjectIDLength: 4, PlatformAdminEmails: []string{"admin@superverse.test"}}, Logger: slog.Default()}
	var failureLogs bytes.Buffer
	connection := serveLoopback(ctx, t, &Server{Boundary: &boundary.Boundary{Logger: slog.New(slog.NewTextHandler(&failureLogs, nil)), Accounts: service}, ProjectIDLength: 4, Sessions: sessionService})
	accountAPI, sessionAPI := pb.NewAccountServiceClient(connection), pb.NewSessionServiceClient(connection)
	assertUnauthenticated := func(err error) {
		t.Helper()
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected unauthenticated, got %v", err)
		}
	}
	id := flowid.RandomID(12)
	registered, err := accountAPI.RegisterAccount(ctx, &pb.RegisterAccountRequest{Email: "bff-" + id + "@example.com", Password: "correct-password", RequestId: "bff-" + id})
	if err != nil {
		t.Fatal(err)
	}
	login := registered.GetLogin()
	caller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
	secretText := "private-request-payload-" + id
	agentAPIForLog := pb.NewAgentServiceClient(connection)
	_, err = agentAPIForLog.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: "invalid-project", MessageId: "message-" + id, Text: secretText, Caller: caller})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected rejected Agent request, got %v", err)
	}
	logged := failureLogs.String()
	if strings.Count(logged, "method=/superverse.v2.AgentService/SendMessage") != 1 || !strings.Contains(logged, "code=InvalidArgument") || !strings.Contains(logged, "account="+caller.GetAccountId()) || !strings.Contains(logged, "project=invalid-project") || strings.Contains(logged, secretText) || strings.Contains(logged, caller.GetSessionToken()) {
		t.Fatalf("failed RPC log lacks bounded status/identity or exposes request data: %q", logged)
	}
	t.Cleanup(func() {
		if logoutErr := service.Logout(context.Background(), login.GetSessionToken()); logoutErr != nil {
			t.Errorf("test login revocation failed (%T)", logoutErr)
		}
	})
	if delta := time.Until(login.GetExpiresAt().AsTime()); delta < 7*24*time.Hour-time.Minute || delta > 7*24*time.Hour {
		t.Fatal("session expiry not fixed seven days")
	}
	current, err := accountAPI.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{Caller: caller})
	if err != nil || current.GetAccount().GetAccountId() != caller.GetAccountId() {
		t.Fatal("explicit caller not accepted", err)
	}
	// The Project addresses its one Session and Preview: a UUID, a Project ID
	// of another length or form, and a Project ID with a Session ID of the
	// earlier format are refused before Dex.
	agentAPI, previewAPI := pb.NewAgentServiceClient(connection), pb.NewPreviewServiceClient(connection)
	rpcError := func(_ any, err error) error { return err }
	for _, projectID := range []string{"3c9486dd-6ead-452a-889f-72b9c783f108", "ABCD", "abc", "abcd-s8m4p7w2", strings.Repeat("a", 25)} {
		for _, request := range []struct {
			name string
			call func() error
		}{
			{"GetSession", func() error {
				return rpcError(sessionAPI.GetSession(ctx, &pb.GetSessionRequest{ProjectId: projectID, Caller: caller}))
			}},
			{"WatchSession", func() error {
				stream, watchErr := sessionAPI.WatchSession(ctx, &pb.WatchSessionRequest{ProjectId: projectID, Caller: caller})
				if watchErr != nil {
					return watchErr
				}
				return rpcError(stream.Recv())
			}},
			{"ListArchivedMessages", func() error {
				return rpcError(sessionAPI.ListArchivedMessages(ctx, &pb.ListArchivedMessagesRequest{ProjectId: projectID, BeforeSequence: 11, Caller: caller}))
			}},
			{"SendMessage", func() error {
				return rpcError(agentAPI.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: projectID, MessageId: "message-" + id, Text: "hello", Caller: caller}))
			}},
			{"GetPreview", func() error {
				return rpcError(previewAPI.GetPreview(ctx, &pb.GetPreviewRequest{ProjectId: projectID, Caller: caller}))
			}},
		} {
			if err = request.call(); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("%s of Project %q should fail before Dex as InvalidArgument: %v", request.name, projectID, err)
			}
		}
	}
	projectAPI := pb.NewProjectServiceClient(connection)
	// The authenticated real gRPC boundary must reject absent sources, including
	// the retired field 7 on the wire, before it reaches any Project dependency.
	for _, legacy := range []bool{false, true} {
		request := &pb.CreateProjectRequest{RequestId: "source-probe-" + id, Name: "Source probe", Caller: caller}
		if legacy {
			request.ProtoReflect().SetUnknown(protowire.AppendBytes(protowire.AppendTag(nil, 7, protowire.BytesType), protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 1)))
		}
		_, err = projectAPI.CreateProject(ctx, request)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("creation without managed source (legacy=%t) reached dependencies: %v", legacy, err)
		}
	}
	if err = connection.Invoke(ctx, "/superverse.v2.ProjectService/ListImportableRepositories", &pb.ListProjectsRequest{Caller: caller}, &pb.ListProjectsResponse{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("removed repository listing RPC is still callable: %v", err)
	}
	_, err = accountAPI.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{})
	assertUnauthenticated(err)
	legacy := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+caller.GetSessionToken()))
	_, err = accountAPI.GetCurrentAccount(legacy, &pb.GetCurrentAccountRequest{})
	assertUnauthenticated(err)
	withCaller := func(change func(*pb.CallerContext)) *pb.CallerContext {
		changed := &pb.CallerContext{AccountId: caller.GetAccountId(), CanonicalEmail: caller.GetCanonicalEmail(), SessionToken: caller.GetSessionToken()}
		change(changed)
		return changed
	}
	for _, forge := range []func(*pb.CallerContext){
		func(c *pb.CallerContext) { c.AccountId = "another-account" },
		func(c *pb.CallerContext) { c.CanonicalEmail = "other@example.com" },
		func(c *pb.CallerContext) { c.CanonicalEmail = strings.ToUpper(c.CanonicalEmail) },
		func(c *pb.CallerContext) { c.SessionToken = "forged" },
	} {
		_, err = accountAPI.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{Caller: withCaller(forge)})
		assertUnauthenticated(err)
	}
	expired := withCaller(func(c *pb.CallerContext) {
		c.SessionToken = signer.IssueForTest("account-"+c.CanonicalEmail, "login-expired", time.Now().Add(-time.Second))
	})
	_, err = accountAPI.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{Caller: expired})
	assertUnauthenticated(err)
	loggedIn, err := accountAPI.Login(ctx, &pb.LoginRequest{Email: caller.GetCanonicalEmail(), Password: "correct-password"})
	if err != nil {
		t.Fatal(err)
	}
	second := withCaller(func(c *pb.CallerContext) { c.SessionToken = loggedIn.GetLogin().GetSessionToken() })
	if _, err = accountAPI.Logout(ctx, &pb.LogoutRequest{Caller: second}); err != nil {
		t.Fatal(err)
	}
	_, err = accountAPI.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{Caller: second})
	assertUnauthenticated(err)
	if _, err = accountAPI.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{Caller: caller}); err != nil {
		t.Fatal("unrelated login revoked", err)
	}
	t.Logf("verified typed gRPC caller account=%s accountID=%s, missing/forged caller and metadata-only identity rejected, malformed Project IDs of the Session rejected before Dex, expiry and precise logout enforced", caller.GetCanonicalEmail(), caller.GetAccountId())
}

// TestMultiplePlatformAdminsGRPCIntegration uses real account SQL and the
// authenticated gRPC boundary for registration, login and current-account reads.
func TestMultiplePlatformAdminsGRPCIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	db := postgrestest.ScratchDatabaseForTest(t)
	if _, err := postgres.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	emails := []string{"admin-one@example.com", "admin-two@example.com"}
	service := &accounts.Accounts{DAO: accountdao.New(db, emails), Signer: comm.Must(accounts.NewTokenSigner([]byte(strings.Repeat("m", 32))))}
	connection := serveLoopback(ctx, t, &Server{Boundary: &boundary.Boundary{Logger: slog.Default(), Accounts: service}})
	client := pb.NewAccountServiceClient(connection)
	for _, email := range append(emails, "ordinary@example.com") {
		t.Run(email, func(t *testing.T) {
			want := email != "ordinary@example.com"
			registered, err := client.RegisterAccount(ctx, &pb.RegisterAccountRequest{Email: strings.ToUpper(email), Password: "multi-admin-password", RequestId: "register-" + email})
			if err != nil || registered.GetLogin().GetAccount().GetIsAdmin() != want {
				t.Fatalf("registration admin=%t: %v", registered.GetLogin().GetAccount().GetIsAdmin(), err)
			}
			loggedIn, err := client.Login(ctx, &pb.LoginRequest{Email: email, Password: "multi-admin-password"})
			if err != nil || loggedIn.GetLogin().GetAccount().GetIsAdmin() != want {
				t.Fatalf("login admin=%t: %v", loggedIn.GetLogin().GetAccount().GetIsAdmin(), err)
			}
			login := loggedIn.GetLogin()
			caller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: email, SessionToken: login.GetSessionToken()}
			current, err := client.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{Caller: caller})
			if err != nil || current.GetAccount().GetIsAdmin() != want {
				t.Fatalf("authenticated account admin=%t: %v", current.GetAccount().GetIsAdmin(), err)
			}
			admin, err := service.IsAdministrator(ctx, caller.GetAccountId())
			if err != nil || admin != want {
				t.Fatalf("SQL authorization admin=%t: %v", admin, err)
			}
		})
	}
}
