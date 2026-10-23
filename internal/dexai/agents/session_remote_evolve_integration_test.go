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
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
)

// TestSessionRemoteEvolveHTTPDexIntegration pairs and activates a local
// agent through the real remote agent endpoint, so the Session runs in remote
// mode, which never runs the platform model or its tools:
//   - SessionFlowService.ReportEvolveNotification answers an agent review
//     with a *evolve.SessionSkip of reason remote_session, which EvolveFlow's
//     RequestReview turns into a skipped review, and the Session records
//     nothing;
//   - an evolve notification the Session admitted anyway, as one admitted
//     just before the switch, is consumed without an entry: once the Session
//     took it, neither the Session's history nor the Bridge's sync holds it,
//     and no turn opened.
//
// That EvolveFlow records both skips, a review requested of a remote Session
// and one that waits when the executor switches (the activation's
// ReportEvolveRemote), is TestEvolveRemoteSessionSkipDexIntegration, on a Live
// Project with goals.
func TestSessionRemoteEvolveHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "evolve-remote", HostLabel: "integration host"}}, &active, http.StatusOK)
	if executor, err := session.client.ReadExecutor(ctx, session.identity.ProjectID); err != nil || executor.Mode != remoteexec.ModeRemote || executor.Epoch != active.Epoch {
		t.Fatalf("the activation did not switch the Session to the local agent (%T)", err)
	}
	before, _ := session.history(ctx)

	notice := evolve.EvolveNotice{ReviewID: "r-1", OccurredAt: time.Now().UTC(), Text: "Review r-1 of the goals waits for your report."}
	var skip *evolve.SessionSkip
	if err := session.service().ReportEvolveNotification(ctx, session.identity.ProjectID, notice); !errors.As(err, &skip) || skip.Reason != evolve.ReasonRemoteSession {
		t.Fatalf("a remote Session took agent review r-1: %v", err)
	}
	admitted := evolveNotification(session.identity.ProjectID, evolve.EvolveNotice{ReviewID: "r-2", OccurredAt: time.Now().UTC(), Text: "Review r-2 of the goals waits for your report."})
	if ok, err := session.client.ReportSessionNotification(ctx, session.id, admitted); err != nil || !ok {
		t.Fatalf("the Session did not admit the notification of review r-2 (%T)", err)
	}
	session.awaitNotificationsTaken(ctx, "the notification of review r-2")
	after, messages := session.history(ctx)
	for _, message := range ofRole(messages, conversation.MessageRoleNotification) {
		if n := message.Message.Notification; n != nil && n.IsEvolve() {
			t.Fatalf("a remote Session recorded the evolve notification %s", n.ID)
		}
	}
	if after.Description.Turn != before.Description.Turn {
		t.Fatalf("an evolve notification opened turn %d of a remote Session after %d", after.Description.Turn, before.Description.Turn)
	}
	var synced bridgeapi.SyncResponse
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch}, &synced, http.StatusOK)
	for _, entry := range synced.Entries {
		if strings.Contains(entry.Text, "Review r-") {
			t.Fatalf("the Bridge received an agent review: %+v", entry)
		}
	}
}
