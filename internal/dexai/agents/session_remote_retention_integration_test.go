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
	"fmt"
	"net/http"
	"testing"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// More than1000 actual public inputs exercise the existing archive and remote
// retention independently of ContextSegment. Switching itself invokes no model.
func TestSessionRemoteRetentionHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "retained-delivery", HostLabel: "integration host"}}, &active, http.StatusOK)
	var accepted bridgeapi.ReportResponse
	for report := 1; report <= 34; report++ {
		events := make([]bridgeapi.Event, 32)
		for index := range events {
			events[index] = bridgeapi.Event{Kind: bridgeapi.EventKindImportedMessage, Role: bridgeapi.EntryRoleUser, Text: fmt.Sprintf("Retained remote input %04d", (report-1)*32+index+1)}
		}
		remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: uint64(report), Events: events}, &accepted, http.StatusOK)
		// Wait for canonical consumption, not the admission watermark, so the test
		// does not depend on host speed or flood the8-report pending budget.
		minimum := conversation.Sequence(report * 32)
		session.await(ctx, "canonical retention append", func() bool {
			snapshot, err := session.client.GetSnapshot(ctx, session.id)
			return err == nil && snapshot.Agent.Description.LastSequence >= minimum
		})
	}
	// A newly connected Bridge must recover the former connection's canonical
	// imported user entries; its own echoes are deliberately filtered by sync.
	remote = pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "retained-reconnect", HostLabel: "integration host"}}, &active, http.StatusOK)
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	description := snapshot.Agent.Description
	if description.FirstRetainedSequence <= 1 || description.LastSequence-description.FirstRetainedSequence+1 > 1000 {
		t.Fatal("remote retention remained pinned to the old platform segment or exceeded1000 entries")
	}
	var response bridgeapi.SyncResponse
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch}, &response, http.StatusOK)
	if response.Dropped == nil || response.Dropped.From != 1 || response.Dropped.Through != uint64(description.FirstRetainedSequence-1) {
		t.Fatal("reconnect omitted its exact canonical dropped range")
	}
	firstThrough := response.ExaminedThrough
	remote.after, remote.digest = response.ExaminedThrough, response.StateDigest
	passes := 1
	for remote.after < uint64(description.LastSequence) {
		remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, After: remote.after, StateDigest: remote.digest}, &response, http.StatusOK)
		if response.ExaminedThrough <= remote.after || response.ExaminedThrough > response.LastSequence || len(response.Entries) > bridgeapi.MaxSyncEntries {
			t.Fatal("retained archive delivery skipped or failed to advance its examined cursor")
		}
		remote.after, remote.digest = response.ExaminedThrough, response.StateDigest
		passes++
	}
	if passes < 2 || firstThrough >= uint64(description.LastSequence) {
		t.Fatal("retention did not exercise multiple current/archive delivery passes")
	}
	var completed bridgeapi.ReportResponse
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1, TurnCompleted: true}, &completed, http.StatusOK)
	owner := conversation.Actor{AccountID: caller.GetAccountId(), Email: caller.GetCanonicalEmail()}
	switched, err := session.client.SetExecutor(ctx, remoteexec.SetRequest{ProjectID: session.identity.ProjectID, BasisEpoch: active.Epoch, Mode: remoteexec.ModePlatform, Owner: owner, Reason: remoteexec.HandoffSwitched})
	if err != nil || !switched.Accepted || switched.Executor.Handoff == nil {
		t.Fatal("leaving remote did not retain its authoritative deferred handoff")
	}
	session.await(ctx, "platform authority without a model request", func() bool {
		current, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && current.Agent.Description.Executor.Mode == remoteexec.ModePlatform
	})
	current, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range current.Agent.History.Messages {
		if entry.Message.ModelCall != nil {
			t.Fatal("switching executor itself recorded a platform provider request")
		}
	}
}
