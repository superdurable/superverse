// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// The Flow that the slack-relay-trigger scenario's application starts per
// Slack event, and its FlowID prefix.
const (
	relaySlackFlowType   = "SlackMessageFlow"
	relaySlackFlowPrefix = "slack-message-"
)

// TestSlackRelayDexIntegration drives the Slack event relay end to end on the
// lane (agents.NewSlackRelayPreviewForTest): the Preview serves an
// application whose Slack trigger on a seeded platform connection starts
// SlackMessageFlow, and its start registered the connection name. Signed
// events posted to the server's real events listener are routed by their
// authorizations, queued in memory and posted through the lane's gateway to
// the application's trigger receiver, which starts one Flow per event ID, also
// when the same event comes twice. url_verification answers its challenge only
// when signed; a wrong signature, a stale timestamp, an oversized body,
// another workspace and an unknown payload type all answer 200 and start
// nothing. A request with a wrong relay HMAC posted to the public route is
// refused 400. The registration endpoint refuses an unknown token and a URL
// member and answers CONNECTION_NOT_SELECTED for a connection the scope does
// not use. Finally app_uninstalled marks the connection for reauthorization.
func TestSlackRelayDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), agents.SlackRelayTimeoutForTest)
	defer cancel()
	preview := agents.NewSlackRelayPreviewForTest(t, ctx)
	team, channel := preview.Team, preview.Channel

	events, secret := dexflowtest.SlackRelayForTest(t)
	relay := slackPoster{t: t, url: events, secret: secret}
	challenge := "challenge-" + flowid.RandomID(12)
	verification := comm.Must(json.Marshal(map[string]string{"type": "url_verification", "challenge": challenge, "token": "deprecated"}))
	if status, body := relay.post(ctx, verification, time.Now(), true); status != http.StatusOK || body != challenge {
		t.Fatalf("a signed url_verification answered %d %q", status, body)
	}
	if status, body := relay.post(ctx, verification, time.Now(), false); status != http.StatusOK || body != "" {
		t.Fatalf("an unsigned url_verification answered %d %q", status, body)
	}

	first := "Ev" + strings.ToUpper(flowid.RandomID(10))
	relay.requireOK(ctx, slackMessage(first, team, channel, "first message"), time.Now(), true)
	awaitSlackFlow(ctx, preview, first)
	relay.requireOK(ctx, slackMessage(first, team, channel, "first message again"), time.Now(), true)

	refused := map[string][]byte{}
	for what, build := range map[string]func(string) []byte{
		"wrong signature": func(id string) []byte { return slackMessage(id, team, channel, "wrong signature") },
		"stale timestamp": func(id string) []byte { return slackMessage(id, team, channel, "stale") },
		"oversized body":  func(id string) []byte { return slackMessage(id, team, channel, strings.Repeat("x", 1<<20)) },
		"other workspace": func(id string) []byte {
			return slackMessage(id, "T"+strings.ToUpper(flowid.RandomID(9)), channel, "other team")
		},
		"unknown type": func(id string) []byte {
			return comm.Must(json.Marshal(map[string]any{"type": "app_rate_limited", "event_id": id, "team_id": team}))
		},
	} {
		id := "Ev" + strings.ToUpper(flowid.RandomID(10))
		refused[what] = []byte(id)
		at, signed := time.Now(), true
		switch what {
		case "wrong signature":
			signed = false
		case "stale timestamp":
			at = at.Add(-6 * time.Minute)
		}
		relay.requireOK(ctx, build(id), at, signed)
	}
	// A request to the public route with a wrong relay HMAC is refused there.
	tampered := "Ev" + strings.ToUpper(flowid.RandomID(10))
	if status := postRelayedWithWrongHMAC(ctx, t, preview.PreviewURL, relayedBody(tampered, team, channel)); status != http.StatusBadRequest {
		t.Fatalf("the receiver answered %d to a relayed request with a wrong HMAC", status)
	}
	// One scope's events arrive in order, so once the last one started its
	// Flow the refused ones would have.
	last := "Ev" + strings.ToUpper(flowid.RandomID(10))
	relay.requireOK(ctx, slackMessage(last, team, channel, "last message"), time.Now(), true)
	awaitSlackFlow(ctx, preview, last)
	for what, id := range refused {
		if slackFlowExists(ctx, preview, string(id)) {
			t.Fatalf("the %s event %s started a Flow", what, id)
		}
	}
	if slackFlowExists(ctx, preview, tampered) {
		t.Fatalf("the tampered relayed request %s started a Flow", tampered)
	}
	// The repeated first event started no second Flow: the Preview has exactly
	// the first and the last event's Flows.
	want := []string{relaySlackFlowPrefix + first, relaySlackFlowPrefix + last}
	slices.Sort(want)
	preview.Await(ctx, "the search index of both Flows", func() bool { return len(preview.FlowIDs(ctx, relaySlackFlowType)) >= len(want) })
	if ids := preview.FlowIDs(ctx, relaySlackFlowType); !slices.Equal(ids, want) {
		t.Fatalf("the Preview has the SlackMessageFlows %v, not %v", ids, want)
	}

	preview.RequireRegistrationRefusals(ctx, t)

	// Revoking only a person's user token keeps the platform's bot working.
	userRevocation := comm.Must(json.Marshal(map[string]any{"type": "event_callback", "event_id": "Ev" + strings.ToUpper(flowid.RandomID(10)), "event_time": time.Now().Unix(),
		"team_id": team, "event": map[string]any{"type": "tokens_revoked", "tokens": map[string]any{"oauth": []string{"U0RELAYTEST"}}},
		"authorizations": []map[string]any{{"team_id": team, "enterprise_id": nil}}}))
	relay.requireOK(ctx, userRevocation, time.Now(), true)
	if kept, err := preview.DAO.Connection(ctx, preview.Connection.TeamSpaceID, preview.Connection.ConnectionID); err != nil || kept.CredentialStatus != connectiondao.CredentialReady {
		t.Fatalf("a user token revocation left the connection %s: %v", kept.CredentialStatus, err)
	}
	uninstall := comm.Must(json.Marshal(map[string]any{"type": "event_callback", "event_id": "Ev" + strings.ToUpper(flowid.RandomID(10)), "event_time": time.Now().Unix(),
		"team_id": team, "event": map[string]any{"type": "app_uninstalled"}, "authorizations": []map[string]any{{"team_id": team, "enterprise_id": nil}}}))
	relay.requireOK(ctx, uninstall, time.Now(), true)
	marked, err := preview.DAO.Connection(ctx, preview.Connection.TeamSpaceID, preview.Connection.ConnectionID)
	if err != nil || marked.CredentialStatus != connectiondao.CredentialReauthorizationRequired {
		t.Fatalf("app_uninstalled left the connection %s: %v", marked.CredentialStatus, err)
	}
	t.Logf("project %s: the relay started SlackMessageFlow for %s and %s and nothing for the refused events", preview.ProjectID, first, last)
}

// slackPoster posts Slack Events API requests to the lane server's events
// listener, signed with the lane's test Signing Secret.
type slackPoster struct {
	t      *testing.T
	url    string
	secret []byte
}

// post sends body as Slack sends it at at, signed or with a wrong signature,
// and returns the status and the answered text.
func (p slackPoster) post(ctx context.Context, body []byte, at time.Time, signed bool) (int, string) {
	p.t.Helper()
	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, p.secret)
	_, _ = mac.Write([]byte("v0:" + timestamp + ":"))
	_, _ = mac.Write(body)
	signature := mac.Sum(nil)
	if !signed {
		signature[0] ^= 0xff
	}
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Slack-Request-Timestamp", timestamp)
	request.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(signature))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		p.t.Fatalf("the Slack events listener did not answer (%T)", err)
	}
	defer func() { _ = response.Body.Close() }()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return response.StatusCode, string(answer)
}

// requireOK posts body and requires the empty 200 every event gets.
func (p slackPoster) requireOK(ctx context.Context, body []byte, at time.Time, signed bool) {
	p.t.Helper()
	if status, answer := p.post(ctx, body, at, signed); status != http.StatusOK || answer != "" {
		p.t.Fatalf("the events listener answered %d %q", status, answer)
	}
}

// slackMessage is an Events API callback of a top-level channel message in
// team's workspace, authorized for that installation.
func slackMessage(eventID, team, channel, text string) []byte {
	return comm.Must(json.Marshal(map[string]any{
		"token": "deprecated", "team_id": team, "api_app_id": "A0RELAYTEST", "type": "event_callback", "event_id": eventID, "event_time": time.Now().Unix(),
		"event": map[string]any{"type": "message", "channel": channel, "channel_type": "channel", "user": "U0RELAYTEST", "text": text,
			"ts": strconv.FormatInt(time.Now().UnixMicro(), 10)[:10] + ".000100", "event_ts": strconv.FormatInt(time.Now().Unix(), 10) + ".000100"},
		"authorizations": []map[string]any{{"team_id": team, "enterprise_id": nil, "user_id": "U0BOT", "is_bot": true, "is_enterprise_install": false}},
	}))
}

// relayedBody is the body the relay posts for a message event.
func relayedBody(eventID, team, channel string) []byte {
	return comm.Must(json.Marshal(map[string]any{"event_id": eventID, "event_time": time.Now().Unix(), "team_id": team,
		"event": map[string]any{"type": "message", "channel": channel, "user": "U0RELAYTEST", "text": "tampered", "ts": "1760000000.000100"}}))
}

// postRelayedWithWrongHMAC posts body to the trigger receiver of the
// support connection on the Preview's public route through the lane's
// gateway, signed with a key that is not the scope token.
func postRelayedWithWrongHMAC(ctx context.Context, t *testing.T, publicURL string, body []byte) int {
	t.Helper()
	target := comm.Must(url.Parse(publicURL))
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte("not-the-scope-token"))
	_, _ = mac.Write([]byte("v1:" + timestamp + ":"))
	_, _ = mac.Write(body)
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", target.Port()))
	}}}
	path := "/triggers/slack/" + base64.RawURLEncoding.EncodeToString([]byte(agents.SlackRelayConnectionForTest))
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(publicURL, "/")+path, bytes.NewReader(body)))
	request.Header.Set(projectconfig.RelayTimestampHeader, timestamp)
	request.Header.Set(projectconfig.RelaySignatureHeader, "v1="+hex.EncodeToString(mac.Sum(nil)))
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("the gateway did not answer the relayed request (%T)", err)
	}
	_ = response.Body.Close()
	return response.StatusCode
}

// awaitSlackFlow waits until the Preview's SlackMessageFlow of eventID
// completed.
func awaitSlackFlow(ctx context.Context, preview *agents.SlackRelayPreviewForTest, eventID string) {
	preview.Await(ctx, "SlackMessageFlow of "+eventID, func() bool {
		status, _ := preview.FlowStatus(ctx, relaySlackFlowPrefix+eventID)
		return status == dexpb.FlowStatus_FLOW_STATUS_COMPLETED
	})
}

// slackFlowExists reports whether the event started a SlackMessageFlow.
func slackFlowExists(ctx context.Context, preview *agents.SlackRelayPreviewForTest, eventID string) bool {
	_, found := preview.FlowStatus(ctx, relaySlackFlowPrefix+eventID)
	return found
}
