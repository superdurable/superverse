// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestLoginHostGRPCIntegration exercises the real gRPC boundary and a real
// scratch PostgreSQL database, including one-winner redemption and audiences.
func TestLoginHostGRPCIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	db := postgrestest.ScratchDatabaseForTest(t)
	if _, err := postgres.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	serverDB := postgrestest.ScratchServerDatabaseForTest(t, db)
	issuerKey, productKey := []byte(strings.Repeat("i", 32)), []byte(strings.Repeat("p", 32))
	const origin = "http://studio.native.localhost:21000"
	const callback = origin + "/api/auth/callback"
	service := &accounts.Accounts{
		DAO:    accountdao.New(serverDB, []string{"admin@superverse.test"}),
		Signer: comm.Must(accounts.NewTokenSigner([]byte(strings.Repeat("s", 32)))),
		LoginHost: accounts.NewLoginHost(accounts.SecretRing{Current: issuerKey}, []accounts.LoginProduct{{
			ID: "studio", Origin: origin, RedirectURI: callback, Audience: accounts.ProductAudienceStudio,
			Secrets: accounts.SecretRing{Current: productKey},
		}}, true, true, 60, 5, 120, 10),
	}
	connection := serveLoopback(ctx, t, &Server{Boundary: &boundary.Boundary{Logger: slog.Default(), Accounts: service}})
	client := pb.NewAccountServiceClient(connection)
	issuer := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-superverse-auth-client", "login",
		"x-superverse-auth-secret", base64.RawURLEncoding.EncodeToString(issuerKey)))
	product := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-superverse-auth-client", "studio",
		"x-superverse-auth-secret", base64.RawURLEncoding.EncodeToString(productKey)))
	id := flowid.RandomID(12)
	registered, err := client.RegisterAccount(issuer, &pb.RegisterAccountRequest{Email: "login-host-" + id + "@example.com",
		Password: "a-long-test-password", RequestId: "register-" + id})
	if err != nil {
		t.Fatal(err)
	}
	login := registered.GetLogin()
	caller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
	if _, err := client.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{Caller: caller}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("login session reached product account API: %v", err)
	}
	if _, err := client.GetCurrentAccount(issuer, &pb.GetCurrentAccountRequest{Caller: caller}); err != nil {
		t.Fatal(err)
	}
	verifier := make([]byte, 32)
	if _, err := rand.Read(verifier); err != nil {
		t.Fatal(err)
	}
	verifierText := base64.RawURLEncoding.EncodeToString(verifier)
	challenge := sha256.Sum256([]byte(verifierText))
	challengeText := base64.RawURLEncoding.EncodeToString(challenge[:])
	issue := func() string {
		t.Helper()
		issued, err := client.CreateLoginAuthorizationCode(issuer, &pb.CreateLoginAuthorizationCodeRequest{
			ProductOrigin: origin, RedirectUri: callback, CodeChallenge: challengeText, Caller: caller,
		})
		if err != nil {
			t.Fatal(err)
		}
		if delta := time.Until(issued.GetAuthorizationCode().GetExpiresAt().AsTime()); delta < 58*time.Second || delta > 61*time.Second {
			t.Fatalf("code TTL was not 60 seconds: %v", delta)
		}
		return issued.GetAuthorizationCode().GetCode()
	}
	code := issue()
	if _, err := client.ExchangeLoginAuthorizationCode(ctx, &pb.ExchangeLoginAuthorizationCodeRequest{
		Code: code, CodeVerifier: verifierText, RedirectUri: callback,
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("exchange accepted no product credential: %v", err)
	}
	if _, err := client.ExchangeLoginAuthorizationCode(product, &pb.ExchangeLoginAuthorizationCodeRequest{
		Code: code, CodeVerifier: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), RedirectUri: callback,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("wrong verifier consumed or accepted code: %v", err)
	}
	var wg sync.WaitGroup
	results := make([]error, 2)
	logins := make([]*pb.AccountLogin, 2)
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			response, exchangeErr := client.ExchangeLoginAuthorizationCode(product, &pb.ExchangeLoginAuthorizationCodeRequest{
				Code: code, CodeVerifier: verifierText, RedirectUri: callback,
			})
			results[index] = exchangeErr
			if response != nil {
				logins[index] = response.GetLogin()
			}
		}(i)
	}
	wg.Wait()
	winners := 0
	var productLogin *pb.AccountLogin
	for i, result := range results {
		if result == nil {
			winners++
			productLogin = logins[i]
		} else if status.Code(result) != codes.InvalidArgument {
			t.Fatalf("concurrent redemption had an unexpected result: %v", result)
		}
	}
	if winners != 1 {
		t.Fatalf("%d of two redemptions won", winners)
	}
	productCaller := &pb.CallerContext{AccountId: productLogin.GetAccount().GetAccountId(),
		CanonicalEmail: productLogin.GetAccount().GetEmail(), SessionToken: productLogin.GetSessionToken()}
	if !strings.HasPrefix(productLogin.GetSessionId(), "login-") || productLogin.GetAccount().GetEmail() != registered.GetLogin().GetAccount().GetEmail() {
		t.Fatal("exchange omitted its product session ID or canonical email")
	}
	var productAudience string
	if err := db.QueryRowContext(ctx, `SELECT audience FROM login_sessions WHERE session_id = $1`, productLogin.GetSessionId()).Scan(&productAudience); err != nil || productAudience != string(accounts.ProductAudienceStudio) {
		t.Fatalf("studio exchange stored audience %q: %v", productAudience, err)
	}
	if _, err := client.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{Caller: productCaller}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetCurrentAccount(issuer, &pb.GetCurrentAccountRequest{Caller: productCaller}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("product session reached login account API: %v", err)
	}
	if _, err := client.CreateLoginAuthorizationCode(issuer, &pb.CreateLoginAuthorizationCodeRequest{
		ProductOrigin: origin, RedirectUri: callback, CodeChallenge: challengeText, Caller: productCaller,
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("product session issued a login code: %v", err)
	}
	if _, err := client.ExchangeLoginAuthorizationCode(product, &pb.ExchangeLoginAuthorizationCodeRequest{
		Code: code, CodeVerifier: verifierText, RedirectUri: callback, Caller: caller,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("exchange accepted a caller supplied by the product: %v", err)
	}
	// Expiry is checked with the database clock even when the physical row has
	// not yet been cleaned; an expired redemption commits issuer-local cleanup.
	expiring := issue()
	expiringHash := sha256.Sum256([]byte(expiring))
	if _, err := db.ExecContext(ctx, `UPDATE login_authorization_codes SET expires_at = clock_timestamp()
		WHERE code_hash = $1`, expiringHash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExchangeLoginAuthorizationCode(product, &pb.ExchangeLoginAuthorizationCodeRequest{
		Code: expiring, CodeVerifier: verifierText, RedirectUri: callback,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expired code was accepted: %v", err)
	}
	var retained int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM login_authorization_codes WHERE code_hash = $1`, expiringHash[:]).Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("expired grant was not cleaned after rejection: rows=%d err=%v", retained, err)
	}
	// A second real gRPC server and independent process-local bucket share the
	// same SQL authority. Five outstanding grants are admitted; the sixth is
	// rejected by the issuer-row lock, not by the second server's bucket.
	second := &accounts.Accounts{DAO: service.DAO, Signer: service.Signer,
		LoginHost: accounts.NewLoginHost(accounts.SecretRing{Current: issuerKey}, []accounts.LoginProduct{{
			ID: "studio", Origin: origin, RedirectURI: callback, Audience: accounts.ProductAudienceStudio,
			Secrets: accounts.SecretRing{Current: productKey},
		}}, true, true, 60, 5, 120, 10)}
	secondConnection := serveLoopback(ctx, t, &Server{Boundary: &boundary.Boundary{Logger: slog.Default(), Accounts: second}})
	secondClient := pb.NewAccountServiceClient(secondConnection)
	for range 5 {
		if _, err := secondClient.CreateLoginAuthorizationCode(issuer, &pb.CreateLoginAuthorizationCodeRequest{
			ProductOrigin: origin, RedirectUri: callback, CodeChallenge: challengeText, Caller: caller,
		}); err != nil {
			t.Fatalf("one of five grants was rejected: %v", err)
		}
	}
	if _, err := client.CreateLoginAuthorizationCode(issuer, &pb.CreateLoginAuthorizationCodeRequest{
		ProductOrigin: origin, RedirectUri: callback, CodeChallenge: challengeText, Caller: caller,
	}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("sixth outstanding grant was not rejected: %v", err)
	}
	if _, err := client.Logout(issuer, &pb.LogoutRequest{Caller: caller}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM login_authorization_codes WHERE account_id = $1`, caller.AccountId).Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("issuer logout did not cascade the five grants: rows=%d err=%v", retained, err)
	}
	if _, err := client.GetCurrentAccount(ctx, &pb.GetCurrentAccountRequest{Caller: productCaller}); err != nil {
		t.Fatalf("login-host logout revoked the independent product session: %v", err)
	}
	if _, err := client.Logout(ctx, &pb.LogoutRequest{Caller: productCaller}); err != nil {
		t.Fatal(err)
	}
}

// TestLoginHostReplicaBudgetsIntegration proves that both bounded admission
// buckets reset per real server instance while an issued SQL grant survives it.
func TestLoginHostReplicaBudgetsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	ownerDB := postgrestest.ScratchDatabaseForTest(t)
	if _, err := postgres.Migrate(ctx, ownerDB); err != nil {
		t.Fatal(err)
	}
	serverDB := postgrestest.ScratchServerDatabaseForTest(t, ownerDB)
	issuerKey, productKey := []byte(strings.Repeat("i", 32)), []byte(strings.Repeat("p", 32))
	const origin = "http://studio.native.localhost:21000"
	const callback = origin + "/api/auth/callback"
	secret := []byte(strings.Repeat("s", 32))
	newClient := func() pb.AccountServiceClient {
		service := &accounts.Accounts{
			DAO: accountdao.New(serverDB, nil), Signer: comm.Must(accounts.NewTokenSigner(secret)),
			LoginHost: accounts.NewLoginHost(accounts.SecretRing{Current: issuerKey}, []accounts.LoginProduct{{
				ID: "studio", Origin: origin, RedirectURI: callback, Audience: accounts.ProductAudienceStudio,
				Secrets: accounts.SecretRing{Current: productKey},
			}}, true, true, 1, 1, 1, 1),
		}
		return pb.NewAccountServiceClient(serveLoopback(ctx, t, &Server{Boundary: &boundary.Boundary{
			Logger: slog.Default(), Accounts: service,
		}}))
	}
	first := newClient()
	issuer := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-superverse-auth-client", "login",
		"x-superverse-auth-secret", base64.RawURLEncoding.EncodeToString(issuerKey)))
	product := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-superverse-auth-client", "studio",
		"x-superverse-auth-secret", base64.RawURLEncoding.EncodeToString(productKey)))
	id := flowid.RandomID(12)
	registered, err := first.RegisterAccount(issuer, &pb.RegisterAccountRequest{Email: "login-budget-" + id + "@example.com",
		Password: "a-long-test-password", RequestId: "register-" + id})
	if err != nil {
		t.Fatal(err)
	}
	login := registered.GetLogin()
	caller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
	verifier := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	challenge := sha256.Sum256([]byte(verifier))
	request := &pb.CreateLoginAuthorizationCodeRequest{ProductOrigin: origin, RedirectUri: callback,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), Caller: caller}
	issued, err := first.CreateLoginAuthorizationCode(issuer, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.CreateLoginAuthorizationCode(issuer, request); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("issuer bucket did not reject its second attempt: %v", err)
	}
	bad := &pb.ExchangeLoginAuthorizationCodeRequest{Code: "lh1_" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		CodeVerifier: verifier, RedirectUri: callback}
	if _, err := first.ExchangeLoginAuthorizationCode(product, bad); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("first exchange attempt did not reach SQL: %v", err)
	}
	if _, err := first.ExchangeLoginAuthorizationCode(product, bad); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("exchange bucket did not reject its second attempt: %v", err)
	}
	restarted := newClient()
	if _, err := restarted.CreateLoginAuthorizationCode(issuer, request); err != nil {
		t.Fatalf("a new server did not start with a fresh issue budget: %v", err)
	}
	result, err := restarted.ExchangeLoginAuthorizationCode(product, &pb.ExchangeLoginAuthorizationCodeRequest{
		Code: issued.GetAuthorizationCode().GetCode(), CodeVerifier: verifier, RedirectUri: callback,
	})
	if err != nil || result.GetLogin().GetSessionId() == "" {
		t.Fatalf("a new server did not redeem the old server's durable grant: %v", err)
	}
}
