// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue/memory"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/api"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/slackrelay"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// previewRoute reaches the Preview's public route through the lane's gateway
// the way the Native relay does: the gateway's loopback port, with the
// route's host name as the Host header.
type previewRoute struct{ publicURL string }

func (r previewRoute) PublicRoute(string, string) (base, host string) {
	target := comm.Must(url.Parse(r.publicURL))
	return "http://" + net.JoinHostPort("127.0.0.1", target.Port()), target.Host
}

// TestSlackRelayRecentEventsDexIntegration runs the Slack event relay of this
// process (not the lane server's own listener, which is a different process
// whose memory no test can read) as the events listener over the lane's real
// PostgreSQL, against the slack-relay-trigger scenario's real running Preview
// through the lane gateway, and reads the recent events through
// ProjectService.ListRecentTriggerEvents served in this process to a really
// registered and logged-in builder of the Project.
//
// A signed message event is recorded as RECORDED with the application's 200
// and only metadata (type, channel, team, connection name, never the text). A
// second event after the Preview stops is recorded as NOT_RUNNING or
// APPLICATION_UNAVAILABLE, whichever the lane's gateway answers for a stopped
// Preview, and the test logs which. The builder reads Preview and Live; an
// account outside the Project is PERMISSION_DENIED for both.
//
// Not covered here: TIMED_OUT, TOKEN_UNAVAILABLE, a gateway 502 or 504
// specifically and the scope eviction.
func TestSlackRelayRecentEventsDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), agents.SlackRelayTimeoutForTest)
	defer cancel()
	preview := agents.NewSlackRelayPreviewForTest(t, ctx)
	db := postgrestest.OpenForTest(t)

	secret := []byte(flowid.RandomID(32))
	logger := slog.New(slog.DiscardHandler)
	queue := memory.New(eventqueue.Limits{MaxItems: 100, MaxBytes: 4 << 20, MaxItemBytes: 64 << 10, MaxKeyItems: 100, Concurrency: 2}, logger)
	relay := slackrelay.New(slackrelay.Config{SigningSecret: secret, MaxEventBytes: 64 << 10, DeliveryTimeout: 20 * time.Second},
		preview.DAO, queue, previewRoute{publicURL: preview.PreviewURL}, preview.ScopeTokens(t), logger)
	relaying, stopRelay := context.WithCancel(ctx)
	defer stopRelay()
	go relay.Run(relaying)
	events := httptest.NewServer(http.HandlerFunc(relay.ServeEvents))
	t.Cleanup(events.Close)
	poster := slackPoster{t: t, url: events.URL + "/api/slack/events", secret: secret}

	// The API of this process over the lane's database and Dex.
	key := []byte(strings.Repeat("e", 32))
	b := &boundary.Boundary{Logger: logger, Accounts: &accounts.Accounts{DAO: accountdao.New(db, "admin@superverse.test"), Signer: comm.Must(accounts.NewTokenSigner(key))}, Cursors: watch.NewCursorCodec(key)}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := api.NewGRPCServer(&api.Server{Boundary: b, ProjectIDLength: preview.Projects.ProjectIDLength, Projects: preview.Projects, RecentEvents: relay})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	accountAPI, projectAPI := pb.NewAccountServiceClient(connection), pb.NewProjectServiceClient(connection)
	register := func(name string) (*pb.CallerContext, string) {
		suffix := flowid.RandomID(10)
		registered, err := accountAPI.RegisterAccount(ctx, &pb.RegisterAccountRequest{Email: name + "-" + suffix + "@example.com", Password: "password-" + suffix, RequestId: "register-" + suffix})
		if err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
		login := registered.GetLogin()
		caller := &pb.CallerContext{AccountId: login.GetAccount().GetAccountId(), CanonicalEmail: login.GetAccount().GetEmail(), SessionToken: login.GetSessionToken()}
		t.Cleanup(func() { _, _ = accountAPI.Logout(context.WithoutCancel(ctx), &pb.LogoutRequest{Caller: caller}) })
		return caller, login.GetAccount().GetAccountId()
	}
	builder, builderID := register("builder")
	stranger, _ := register("stranger")

	// The Project's owner adds the builder to the TeamSpace and the Project.
	teamSpaceFlow := &teamspaces.TeamSpaceFlow{}
	teamSpaces := &teamspaces.TeamSpaceFlowClient{Dex: dexflowtest.ServerWorkerClient(t, teamSpaceFlow), Flow: teamSpaceFlow}
	owner := preview.Account
	if update, err := teamSpaces.AddMember(ctx, preview.Connection.TeamSpaceID, teamspaces.MemberChange{Actor: teamspaces.Actor{AccountID: owner}, AccountID: builderID, Role: teamspaces.RoleMember}); err != nil || update.Rejection != nil {
		t.Fatalf("add the builder to the TeamSpace: %v %+v", err, update.Rejection)
	}
	if update, err := preview.Projects.AddMember(ctx, preview.ProjectID, projects.MemberChange{ActorAccountID: owner, AccountID: builderID, Role: projects.RoleBuilder}); err != nil || update.Rejection != nil {
		t.Fatalf("add the builder to the Project: %v %+v", err, update.Rejection)
	}
	list := func(caller *pb.CallerContext, scope pb.ProjectScope) (*pb.ListRecentTriggerEventsResponse, error) {
		return projectAPI.ListRecentTriggerEvents(ctx, &pb.ListRecentTriggerEventsRequest{ProjectId: preview.ProjectID, Scope: scope, Caller: caller})
	}
	preview.Await(ctx, "the builder's read model rows", func() bool {
		_, err := list(builder, pb.ProjectScope_PROJECT_SCOPE_PREVIEW)
		return err == nil
	})
	for _, scope := range []pb.ProjectScope{pb.ProjectScope_PROJECT_SCOPE_PREVIEW, pb.ProjectScope_PROJECT_SCOPE_LIVE} {
		if _, err := list(stranger, scope); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("a stranger reading %s answered %v", scope, err)
		}
	}
	if live, err := list(builder, pb.ProjectScope_PROJECT_SCOPE_LIVE); err != nil || len(live.GetEvents()) != 0 || live.GetBufferStartedAt() == nil {
		t.Fatalf("the builder's read of Live answered %v (%v)", live, err)
	}

	awaitFate := func(eventID string, want ...pb.RecentTriggerEvent_Fate) *pb.RecentTriggerEvent {
		t.Helper()
		var found *pb.RecentTriggerEvent
		preview.Await(ctx, "the recent event "+eventID, func() bool {
			response, err := list(builder, pb.ProjectScope_PROJECT_SCOPE_PREVIEW)
			if err != nil {
				t.Fatalf("ListRecentTriggerEvents: %v", err)
			}
			for _, event := range response.GetEvents() {
				if event.GetEventId() == eventID {
					found = event
					return true
				}
			}
			return false
		})
		for _, fate := range want {
			if found.GetFate() == fate {
				return found
			}
		}
		t.Fatalf("event %s has fate %s (HTTP %d), want one of %v", eventID, found.GetFate(), found.GetHttpStatus(), want)
		return nil
	}
	const messageText = "a message text that must never be stored"

	// A running Preview records the event with the application's 200.
	first := "Ev" + strings.ToUpper(flowid.RandomID(10))
	poster.requireOK(ctx, slackMessage(first, preview.Team, preview.Channel, messageText), time.Now(), true)
	got := awaitFate(first, pb.RecentTriggerEvent_FATE_RECORDED)
	if got.GetHttpStatus() != http.StatusOK || got.GetEventType() != "message" || got.GetChannelId() != preview.Channel || got.GetTeamId() != preview.Team ||
		got.GetConnectionName() != agents.SlackRelayConnectionForTest || got.GetReceivedAt() == nil {
		t.Fatalf("the recorded event is %v", got)
	}
	if strings.Contains(fmt.Sprint(got), messageText) {
		t.Fatalf("the recent event holds the message text: %v", got)
	}

	// A stopped Preview does not take it.
	preview.Stop(ctx)
	second := "Ev" + strings.ToUpper(flowid.RandomID(10))
	poster.requireOK(ctx, slackMessage(second, preview.Team, preview.Channel, messageText), time.Now(), true)
	missed := awaitFate(second, pb.RecentTriggerEvent_FATE_NOT_RUNNING, pb.RecentTriggerEvent_FATE_APPLICATION_UNAVAILABLE)
	t.Logf("project %s: %s recorded with HTTP 200; after the Preview stopped %s was %s (HTTP %d)", preview.ProjectID, first, second, missed.GetFate(), missed.GetHttpStatus())
	response, err := list(builder, pb.ProjectScope_PROJECT_SCOPE_PREVIEW)
	if err != nil || len(response.GetEvents()) != 2 || response.GetEvents()[0].GetEventId() != second || strings.Contains(fmt.Sprint(response), messageText) {
		t.Fatalf("the list is %v (%v), want the stopped event first and then the recorded one, without text", response, err)
	}
}
