// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	connections "github.com/superdurable/superversev2/internal/agents/remoteagent"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/api/boundary"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/projecttest"
	"github.com/superdurable/superversev2/internal/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/tenancy/accounts"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Actual gRPC/HTTP handlers reach the lane PostgreSQL, Dex and Project/Sandbox
// lifecycle. Neither loopback transport replaces an application dependency.
func TestRemoteAgentPairingDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	s := newActorSession(t, ctx)
	dao := remoteagentdao.New(postgrestest.OpenForTest(t))
	s.service.RemoteConnections = dao
	accountService := &accounts.Accounts{DAO: accountdao.New(postgrestest.OpenForTest(t), "admin@superverse.test"), Signer: comm.Must(accounts.NewTokenSigner([]byte(strings.Repeat("a", 32))))}
	capability := &connections.Connections{DAO: dao, Sessions: s.service, MinimumBridgeVersion: "0.1.0", PlatformAdminEmail: "admin@superverse.test"}
	transport := serveLoopback(ctx, t, &Server{Boundary: &boundary.Boundary{Accounts: accountService, Logger: slog.Default()}, RemoteAgents: capability})
	rpc := pb.NewRemoteAgentServiceClient(transport)
	endpoint := httptest.NewServer(connections.NewEndpoint(capability, t.TempDir(), slog.Default(), 1000, 4, 16))
	defer endpoint.Close()
	client := &http.Client{Timeout: 15 * time.Second}
	create := func(caller *pb.CallerContext) *pb.CreateRemoteAgentPairingResponse {
		t.Helper()
		response, err := rpc.CreateRemoteAgentPairing(ctx, &pb.CreateRemoteAgentPairingRequest{ProjectId: s.project.ProjectID, Caller: caller})
		if err != nil {
			t.Fatalf("pairing admission failed (%T): %v", err, err)
		}
		if len(response.GetCode()) != 10 || strings.Trim(response.GetCode(), "0123456789ABCDEFGHJKMNPQRSTVWXYZ") != "" || response.GetBasisEpoch() != 0 || response.GetCurrentConnectionId() != "" {
			t.Fatal("pairing did not reflect the actual platform executor and canonical code")
		}
		if ttl := time.Until(response.GetExpiresAt().AsTime()); ttl < 9*time.Minute || ttl > 10*time.Minute {
			t.Fatal("pairing lifetime is not ten minutes")
		}
		return response
	}
	pair := func(code string) bridgeapi.PairResponse {
		t.Helper()
		raw, _ := json.Marshal(bridgeapi.PairRequest{Code: code, Bridge: bridgeapi.BridgeInfo{Version: "0.1.0", OS: "darwin", Arch: "arm64"}})
		body := remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathPair, raw, "", http.StatusOK, "")
		var result bridgeapi.PairResponse
		if json.Unmarshal(body, &result) != nil || result.ProjectID != s.project.ProjectID || len(result.Token) != 43 {
			t.Fatal("redemption omitted its scoped one-time token")
		}
		return result
	}
	first := create(s.first)
	normalized := strings.ToLower(first.GetCode()[:4] + " - " + first.GetCode()[4:])
	outdated, _ := json.Marshal(bridgeapi.PairRequest{Code: first.GetCode(), Bridge: bridgeapi.BridgeInfo{Version: "0.0.1", OS: "darwin", Arch: "arm64"}})
	remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathPair, outdated, "", http.StatusForbidden, bridgeapi.ErrorBridgeOutdated)
	dev, _ := json.Marshal(bridgeapi.PairRequest{Code: first.GetCode(), Bridge: bridgeapi.BridgeInfo{Version: bridgeapi.DevBridgeVersion, OS: "darwin", Arch: "arm64"}})
	remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathPair, dev, "", http.StatusForbidden, bridgeapi.ErrorBridgeOutdated)
	credential := pair(normalized)
	if credential.ConnectionID != first.GetConnectionId() {
		t.Fatal("redemption returned another connection")
	}
	reused, _ := json.Marshal(bridgeapi.PairRequest{Code: first.GetCode(), Bridge: bridgeapi.BridgeInfo{Version: "0.1.0", OS: "darwin", Arch: "arm64"}})
	remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathPair, reused, "", http.StatusUnauthorized, bridgeapi.ErrorPairingCodeInvalid)
	listed, err := rpc.ListRemoteAgentConnections(ctx, &pb.ListRemoteAgentConnectionsRequest{ProjectId: s.project.ProjectID, Caller: s.owner})
	if err != nil || len(listed.GetConnections()) != 1 || listed.GetConnections()[0].GetStatus() != pb.RemoteAgentConnectionStatus_REMOTE_AGENT_CONNECTION_STATUS_PAIRED || listed.GetCurrentConnectionId() != "" {
		t.Fatal("pairing was mistaken for active execution or absent from real list")
	}
	encoded, _ := json.Marshal(listed)
	if bytes.Contains(encoded, []byte(first.GetCode())) || bytes.Contains(encoded, []byte(credential.Token)) || bytes.Contains(encoded, []byte("tokenHash")) || bytes.Contains(encoded, []byte("pairingCodeHash")) {
		t.Fatal("public list disclosed credentials")
	}
	remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathDisconnect, []byte(`{}`), credential.Token, http.StatusOK, "")
	remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathDisconnect, []byte(`{}`), credential.Token, http.StatusUnauthorized, bridgeapi.ErrorUnauthorized)
	// A real Project membership change invalidates the very next authenticated
	// endpoint request after its authoritative SQL projection catches up.
	demoted := pair(create(s.first).GetCode())
	if _, err = s.service.Projects.SetMemberRole(ctx, s.project.ProjectID, projects.MemberChange{ActorAccountID: s.owner.GetAccountId(), AccountID: s.first.GetAccountId(), Role: projects.RoleOperator}); err != nil {
		t.Fatal(err)
	}
	awaitTrue(ctx, t, "actual role-loss projection", func() bool {
		access, readErr := dao.Authenticate(ctx, remoteTokenHash(demoted.Token), "admin@superverse.test", accountdao.StatusActive)
		if readErr != nil {
			t.Fatalf("role projection SQL error: %v", readErr)
		}
		return access.ProjectRole.Valid && access.ProjectRole.V == projects.RoleOperator
	})
	remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathDisconnect, []byte(`{}`), demoted.Token, http.StatusForbidden, bridgeapi.ErrorForbidden)
	if _, err = rpc.RevokeRemoteAgentConnection(ctx, &pb.RevokeRemoteAgentConnectionRequest{ProjectId: s.project.ProjectID, ConnectionId: demoted.ConnectionID, Caller: s.owner}); err != nil {
		t.Fatal(err)
	}
	// A freshly paired credential is invalid immediately after normal logout.
	second := pair(create(s.second).GetCode())
	if _, err = pb.NewAccountServiceClient(transport).Logout(ctx, &pb.LogoutRequest{Caller: s.second}); err != nil {
		t.Fatal("normal logout failed")
	}
	remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathDisconnect, []byte(`{}`), second.Token, http.StatusUnauthorized, bridgeapi.ErrorUnauthorized)
	// Restore this account's cleanup login without restoring the credential
	// bound to its original issuing login.
	accountAPI := pb.NewAccountServiceClient(transport)
	loggedIn, err := accountAPI.Login(ctx, &pb.LoginRequest{Email: s.second.GetCanonicalEmail(), Password: "password-" + strings.TrimSuffix(s.second.GetCanonicalEmail(), "@example.com")})
	if err != nil {
		t.Fatal("sign in the logged-out fixture account failed")
	}
	login := loggedIn.GetLogin()
	cleanupCaller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
	cleanupTransferred := false
	t.Cleanup(func() {
		if !cleanupTransferred && cleanupCaller.GetSessionToken() != "" {
			if _, err := accountAPI.Logout(context.WithoutCancel(ctx), &pb.LogoutRequest{Caller: cleanupCaller}); err != nil {
				t.Error("revoke the replacement fixture login failed")
			}
		}
	})
	if cleanupCaller.GetAccountId() != s.second.GetAccountId() || cleanupCaller.GetCanonicalEmail() != s.second.GetCanonicalEmail() || cleanupCaller.GetSessionToken() == "" || cleanupCaller.GetSessionToken() == s.second.GetSessionToken() {
		t.Fatal("replacement login changed account identity or omitted a new session token")
	}
	// registerNamedCaller captured this pointer; transfer the new login to it.
	s.second.SessionToken = cleanupCaller.GetSessionToken()
	cleanupTransferred = true
	remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathDisconnect, []byte(`{}`), second.Token, http.StatusUnauthorized, bridgeapi.ErrorUnauthorized)
	if _, err = rpc.RevokeRemoteAgentConnection(ctx, &pb.RevokeRemoteAgentConnectionRequest{ProjectId: s.project.ProjectID, ConnectionId: second.ConnectionID, Caller: s.owner}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		create(s.owner)
	}
	_, err = rpc.CreateRemoteAgentPairing(ctx, &pb.CreateRemoteAgentPairingRequest{ProjectId: s.project.ProjectID, Caller: s.owner})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatal("seventh connection did not fail with typed cap refusal")
	}
	found := false
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Reason == "REMOTE_AGENT_CONNECTIONS_FULL" {
			found = true
		}
	}
	if !found {
		t.Fatal("connection cap omitted ErrorInfo reason")
	}
	// Revoke exact identities through the authenticated public API.
	listed, err = rpc.ListRemoteAgentConnections(ctx, &pb.ListRemoteAgentConnectionsRequest{ProjectId: s.project.ProjectID, Caller: s.owner})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range listed.GetConnections() {
		if _, err = rpc.RevokeRemoteAgentConnection(ctx, &pb.RevokeRemoteAgentConnectionRequest{ProjectId: s.project.ProjectID, ConnectionId: row.GetConnectionId(), Caller: s.owner}); err != nil {
			t.Fatal(err)
		}
	}

	// The real Project deletion and existing Session cleanup release credential
	// rows; the SQL DAO must not acknowledge their deletion independently early.
	deleted := pair(create(s.owner).GetCode())
	projecttest.DeleteForTest(ctx, t, s.service.Projects, s.project.ProjectID)
	rows, err := dao.List(ctx, s.project.ProjectID)
	if err != nil || len(rows) != 0 {
		t.Fatal("Project deletion left remote credentials")
	}
	remoteHTTP(t, client, http.MethodPost, endpoint.URL+bridgeapi.PathDisconnect, []byte(`{}`), deleted.Token, http.StatusUnauthorized, bridgeapi.ErrorUnauthorized)
}

func TestRemoteAgentHTTPRestrictionsDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	s := newActorSession(t, ctx)
	service := &connections.Connections{DAO: remoteagentdao.New(postgrestest.OpenForTest(t)), Sessions: s.service, MinimumBridgeVersion: "0.1.0", PlatformAdminEmail: "admin@superverse.test"}
	root := t.TempDir()
	endpoint := httptest.NewServer(connections.NewEndpoint(service, root, slog.Default(), 1000, 4, 16))
	defer endpoint.Close()
	client := &http.Client{Timeout: 15 * time.Second}
	checkRemotePairAdmission(ctx, t, endpoint.Listener.Addr().String(), endpoint.URL, client)
	for _, probe := range []struct {
		method, path, body string
		status             int
		code               bridgeapi.ErrorCode
	}{
		{http.MethodGet, bridgeapi.PathPair, "", 405, bridgeapi.ErrorMalformed},
		{http.MethodPost, bridgeapi.PathPair, `null`, 400, bridgeapi.ErrorMalformed},
		{http.MethodPost, bridgeapi.PathPair, `{"unknown":1}`, 400, bridgeapi.ErrorMalformed},
		{http.MethodPost, bridgeapi.PathPair, `{} {}`, 400, bridgeapi.ErrorMalformed},
		{http.MethodPost, bridgeapi.PathPair, strings.Repeat("x", int(bridgeapi.MaxPairRequestBytes)+1), 413, bridgeapi.ErrorTooLarge},
		{http.MethodPost, bridgeapi.PathDisconnect, `{"token":"forged"}`, 400, bridgeapi.ErrorMalformed},
		{http.MethodPost, bridgeapi.PathDisconnect, `{}`, 401, bridgeapi.ErrorUnauthorized},
		{http.MethodPost, bridgeapi.PathPair + "/", `{}`, 404, bridgeapi.ErrorNotFound},
		{http.MethodPost, bridgeapi.PathPair + "?code=forged", `{}`, 400, bridgeapi.ErrorMalformed},
		{http.MethodPost, "/remote-agent/v1/%70air", `{}`, 400, bridgeapi.ErrorMalformed},
		{http.MethodPost, bridgeapi.PathActivate, `{}`, 401, bridgeapi.ErrorUnauthorized},
		{http.MethodPost, bridgeapi.PathSync, `{}`, 401, bridgeapi.ErrorUnauthorized},
		{http.MethodPost, bridgeapi.PathReport, `{}`, 401, bridgeapi.ErrorUnauthorized},
		{http.MethodGet, "/remote-agent/v1/downloads/../private", "", 404, bridgeapi.ErrorNotFound},
		{http.MethodGet, "/remote-agent/v1/downloads/%2e%2e/private", "", 400, bridgeapi.ErrorMalformed},
		{http.MethodGet, "/remote-agent/v1/downloads/arbitrary", "", 404, bridgeapi.ErrorNotFound},
		{http.MethodPost, bridgeapi.PathDownloadsManifest, `{}`, 405, bridgeapi.ErrorMalformed},
	} {
		remoteHTTP(t, client, probe.method, endpoint.URL+probe.path, []byte(probe.body), "", probe.status, probe.code)
	}
	body := remoteHTTP(t, client, http.MethodGet, endpoint.URL+bridgeapi.PathDownloadsManifest, nil, "", 200, "")
	var manifest bridgeapi.DownloadsManifest
	if json.Unmarshal(body, &manifest) != nil || manifest.MinimumBridgeVersion != "0.1.0" || len(manifest.Artifacts) != 0 {
		t.Fatal("missing local builds did not yield an empty environment manifest")
	}
	artifact := []byte("actual local distribution bytes")
	digest := sha256.Sum256(artifact)
	name := "superverse-bridge-darwin-arm64"
	manifest.Artifacts = []bridgeapi.Artifact{{Name: "superverse-bridge", Version: "0.1.0", OS: "darwin", Arch: "arm64", SHA256: hex.EncodeToString(digest[:]), Path: name}}
	raw, _ := json.Marshal(manifest)
	if os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0600) != nil || os.WriteFile(filepath.Join(root, name), artifact, 0600) != nil {
		t.Fatal("write owned distribution artifacts")
	}
	if body = remoteHTTP(t, client, http.MethodGet, endpoint.URL+"/remote-agent/v1/downloads/"+name, nil, "", 200, ""); !bytes.Equal(body, artifact) {
		t.Fatal("download changed artifact bytes")
	}
	// A fixed artifact filename cannot escape its configured root via a symlink.
	outside := filepath.Join(t.TempDir(), "private")
	if os.WriteFile(outside, []byte("private"), 0600) != nil || os.Remove(filepath.Join(root, name)) != nil || os.Symlink(outside, filepath.Join(root, name)) != nil {
		t.Fatal("prepare actual filesystem containment check")
	}
	remoteHTTP(t, client, http.MethodGet, endpoint.URL+"/remote-agent/v1/downloads/"+name, nil, "", 503, bridgeapi.ErrorUnavailable)
	if os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`null`), 0600) != nil {
		t.Fatal("write malformed distribution manifest")
	}
	remoteHTTP(t, client, http.MethodGet, endpoint.URL+bridgeapi.PathDownloadsManifest, nil, "", 503, bridgeapi.ErrorUnavailable)
}

func remoteHTTP(t *testing.T, client *http.Client, method, url string, body []byte, token string, want int, code bridgeapi.ErrorCode) []byte {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal("construct endpoint request")
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("remote endpoint transport failed (%T)", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil || response.StatusCode != want {
		t.Fatalf("remote endpoint status=%d expected=%d read-error=%T", response.StatusCode, want, err)
	}
	if code != "" {
		var refused bridgeapi.ErrorBody
		if json.Unmarshal(raw, &refused) != nil || refused.Code != code {
			t.Fatal("remote endpoint omitted its exact typed refusal")
		}
		if len(raw) > 8<<10 {
			t.Fatal("remote endpoint refusal exceeded its bound")
		}
	}
	if want == http.StatusTooManyRequests && response.Header.Get("Retry-After") == "" {
		t.Fatal("admission refusal omitted Retry-After")
	}
	if want == http.StatusMethodNotAllowed && response.Header.Get("Allow") == "" {
		t.Fatal("method refusal omitted Allow")
	}
	return raw
}

func remoteTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// The actual listener acknowledges Expect only when its admitted decoder reads
// the body. Sixteen held transports therefore prove pre-body admission without
// replacing a dependency or exposing an internal semaphore for tests.
func checkRemotePairAdmission(ctx context.Context, t *testing.T, address, origin string, client *http.Client) {
	t.Helper()
	var admitted []struct {
		conn   net.Conn
		reader *bufio.Reader
	}
	for range 16 {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err = fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: 2\r\nExpect: 100-continue\r\n\r\n", bridgeapi.PathPair, address); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(conn)
		response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
		if err != nil {
			t.Fatalf("pair body was not admitted: transport-error=%T", err)
		}
		if response.StatusCode != http.StatusContinue {
			_ = response.Body.Close()
			t.Fatalf("pair body was not admitted: status=%d expected=%d", response.StatusCode, http.StatusContinue)
		}
		_ = response.Body.Close()
		// Keep the admitted body pending until the cap refusal is observed.
		if err = conn.SetDeadline(time.Time{}); err != nil {
			t.Fatal(err)
		}
		admitted = append(admitted, struct {
			conn   net.Conn
			reader *bufio.Reader
		}{conn, reader})
	}
	remoteHTTP(t, client, http.MethodPost, origin+bridgeapi.PathPair, []byte(`{}`), "", http.StatusTooManyRequests, bridgeapi.ErrorRateLimited)
	for _, held := range admitted {
		if err := held.conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := held.conn.Write([]byte(`{]`)); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(held.reader, &http.Request{Method: http.MethodPost})
		if err != nil {
			t.Fatalf("admitted pair did not settle its malformed body: transport-error=%T", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (8<<10)+1))
		_ = response.Body.Close()
		var refused bridgeapi.ErrorBody
		decodeErr := json.Unmarshal(raw, &refused)
		if readErr != nil || len(raw) > 8<<10 || decodeErr != nil || response.StatusCode != http.StatusBadRequest || refused.Code != bridgeapi.ErrorMalformed {
			t.Fatalf("admitted pair did not settle its malformed body: status=%d expected=%d code=%.32q expected-code=%q bytes=%d read-error=%T decode-error=%T",
				response.StatusCode, http.StatusBadRequest, refused.Code, bridgeapi.ErrorMalformed, len(raw), readErr, decodeErr)
		}
	}
}
