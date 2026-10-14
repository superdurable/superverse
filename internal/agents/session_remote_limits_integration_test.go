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
	"strings"
	"testing"

	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
)

// Admission failures use the real endpoint and verify that neither malformed
// payloads nor an attempted server-only control can advance the Session watermark.
func TestSessionRemoteReportBoundsHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "report-bounds", ProviderThreadID: "report-bounds", HostLabel: "integration host"}}, &active, http.StatusOK)
	var cursorRefusal bridgeapi.ErrorBody
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, After: active.LastSequence + 1_000_000}, &cursorRefusal, http.StatusBadRequest)
	if cursorRefusal.Code != bridgeapi.ErrorMalformed {
		t.Fatal("future delivery cursor was not a typed malformed request")
	}
	base := func() map[string]any {
		return map[string]any{"epoch": active.Epoch, "generation": active.Generation, "sequence": 1}
	}
	events := make([]bridgeapi.Event, 33)
	for index := range events {
		events[index] = bridgeapi.Event{Kind: bridgeapi.EventKindActivity, Text: "bounded activity"}
	}
	cases := []struct {
		name   string
		fields map[string]any
		status int
		code   bridgeapi.ErrorCode
	}{
		{"not_one_of", map[string]any{"events": []bridgeapi.Event{{Kind: bridgeapi.EventKindActivity, Text: "activity"}}, "turnCompleted": true}, http.StatusBadRequest, bridgeapi.ErrorMalformed},
		{"events_33", map[string]any{"events": events}, http.StatusBadRequest, bridgeapi.ErrorMalformed},
		{"event_above_64KiB", map[string]any{"events": []bridgeapi.Event{{Kind: bridgeapi.EventKindAssistantMessage, Text: strings.Repeat("a", (64<<10)+1)}}}, http.StatusBadRequest, bridgeapi.ErrorMalformed},
		{"arguments_above_256KiB", map[string]any{"toolCall": map[string]any{"name": "read", "arguments": map[string]any{"path": strings.Repeat("a", 256<<10)}}}, http.StatusBadRequest, bridgeapi.ErrorMalformed},
		{"hidden_reasoning", map[string]any{"events": []map[string]any{{"kind": "reasoning", "text": "must never be ingested"}}}, http.StatusBadRequest, bridgeapi.ErrorMalformed},
		{"server_control", map[string]any{"turnCompleted": true, "control": map[string]any{"epoch": active.Epoch, "generation": active.Generation}}, http.StatusBadRequest, bridgeapi.ErrorMalformed},
		{"http_above_512KiB", map[string]any{"events": []bridgeapi.Event{{Kind: bridgeapi.EventKindAssistantMessage, Text: strings.Repeat("a", (512<<10)+1)}}}, http.StatusRequestEntityTooLarge, bridgeapi.ErrorTooLarge},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			remote := remote
			remote.t = t
			input := base()
			for key, value := range item.fields {
				input[key] = value
			}
			var refusal bridgeapi.ErrorBody
			remote.post(ctx, bridgeapi.PathReport, input, &refusal, item.status)
			if refusal.Code != item.code {
				t.Fatalf("bound refusal code = %s, expected %s", refusal.Code, item.code)
			}
		})
	}
	var gap bridgeapi.ErrorBody
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 2, TurnCompleted: true}, &gap, http.StatusConflict)
	if gap.Code != bridgeapi.ErrorSequenceGap || gap.ExpectedSequence != 1 {
		t.Fatal("invalid payload advanced the report watermark")
	}
	var accepted bridgeapi.ReportResponse
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1, TurnCompleted: true}, &accepted, http.StatusOK)
	if accepted.Reported != 1 || accepted.Duplicate {
		t.Fatal("first valid report was not admitted exactly once")
	}
}
