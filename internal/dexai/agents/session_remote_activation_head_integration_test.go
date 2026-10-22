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
	"net/http"
	"testing"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

// The test acts as the Bridge through the real HTTP endpoint while the Session
// and its conversation run in Dex. A post-activation message must stay ahead
// of the returned cursor, including after an activation response is retried.
func TestSessionRemoteActivationHeadHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	before, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	binding := bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration",
		ProviderThreadID: "activation-head", HostLabel: "integration host"}}
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, binding, &active, http.StatusOK)
	if active.LastSequence != uint64(before.Agent.Description.LastSequence) {
		t.Fatalf("activation head = %d, previous conversation head = %d", active.LastSequence, before.Agent.Description.LastSequence)
	}
	const firstMessage = "Deliver the first message after remote activation"
	session.send(ctx, firstMessage)
	session.await(ctx, "post-activation message append", func() bool {
		snapshot, readErr := session.client.GetSnapshot(ctx, session.id)
		return readErr == nil && uint64(snapshot.Agent.Description.LastSequence) > active.LastSequence
	})
	var repeated bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, binding, &repeated, http.StatusOK)
	if repeated.Epoch != active.Epoch || repeated.Generation != active.Generation || repeated.LastSequence != active.LastSequence {
		t.Fatalf("repeat activation advanced the fence or head: first=(%d,%d,%d) repeat=(%d,%d,%d)",
			active.Epoch, active.Generation, active.LastSequence, repeated.Epoch, repeated.Generation, repeated.LastSequence)
	}
	var delivered bridgeapi.SyncResponse
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, After: repeated.LastSequence, Context: true}, &delivered, http.StatusOK)
	if delivered.ExaminedThrough <= repeated.LastSequence {
		t.Fatal("sync did not advance past the activation head")
	}
	for _, entry := range delivered.Entries {
		if entry.Sequence > repeated.LastSequence && entry.Kind == bridgeapi.EntryKindUserMessage && entry.Text == firstMessage {
			return
		}
	}
	t.Fatal("sync omitted the first message appended after activation")
}
