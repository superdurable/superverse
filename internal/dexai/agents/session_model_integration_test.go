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
	"log/slog"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
)

// This creates a throwaway READY Project, whose Session starts on the
// server's start model, and selects the Session's model through the real
// UpdateSessionModel RPC after validating a synthetic Project key at the mock
// endpoint. It sends no message, so no inference is called and no
// workspace is requested: the selection only records SessionModel, which the
// next turn's rollover applies. An exact retry is acknowledged without another
// revision, and a stale revision is refused as a changed revision.
func TestSessionModelSelectionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newSession(t, ctx, "", "")
	if started := session.awaitStarted(ctx); started.Model == "" || started.ModelRevision != 0 || started.ContextModel != "" {
		t.Fatalf("a new Session runs %q at revision %d with context model %q", started.Model, started.ModelRevision, started.ContextModel)
	}
	installSyntheticProjectKey(t, ctx, session, "model-selection")

	selectModel := func(model conversation.Model, expected uint64) error {
		return session.client.UpdateSessionModel(ctx, session.id, agentloop.UpdateSessionModelRequest{Model: model, ExpectedRevision: expected})
	}
	if err := selectModel(conversation.ModelSonnet55, 0); err != nil {
		t.Fatalf("selecting Sonnet at revision 0 failed (%T)", err)
	}
	if err := selectModel(conversation.ModelSonnet55, 0); err != nil {
		t.Fatalf("the exact retry was not acknowledged (%T)", err)
	}
	if err := selectModel(conversation.ModelHaiku45, 0); !errors.Is(err, sessionagent.ErrModelRevisionChanged) {
		t.Fatalf("a stale revision was not refused: %v", err)
	}
	if err := selectModel(conversation.ModelHaiku45, 1); err != nil {
		t.Fatalf("selecting Haiku at revision 1 failed (%T)", err)
	}
	// No turn started, so no segment opened on any model.
	if selected := session.awaitStarted(ctx); selected.Model != conversation.ModelHaiku45 || selected.ModelRevision != 2 || selected.ContextModel != "" {
		t.Fatalf("the Session selects %q at revision %d with context model %q", selected.Model, selected.ModelRevision, selected.ContextModel)
	}
	t.Logf("FlowID=%s selected Sonnet then Haiku; retry acknowledged, stale revision refused", session.id)
}

// installSyntheticProjectKey satisfies admission through the existing model API
// and real credential owner, with cleanup before the throwaway Project closes.
func installSyntheticProjectKey(t *testing.T, ctx context.Context, session *mockSession, label string) {
	t.Helper()
	key := "synthetic-byok-" + label + "-" + flowid.RandomID(20)
	logger := slog.New(slog.DiscardHandler)
	endpoint, probe, stop, err := mockmodel.StartCredentialProbeForTest(ctx, logger, key)
	if err != nil {
		t.Fatalf("start %s credential probe (%T)", label, err)
	}
	t.Cleanup(stop)
	credentials := &ModelCredentialFlowService{Sessions: session.client, Subagents: session.subagents,
		HTTP: modelprovider.NewProviderHTTPClient(), Logger: logger, ValidationBaseURL: endpoint}
	t.Cleanup(func() {
		if err := credentials.Close(); err != nil {
			t.Errorf("close synthetic credential service (%T)", err)
		}
	})
	actor := conversation.Actor{AccountID: session.account}
	if _, err := credentials.Mutate(ctx, session.id, modelcredential.Mutation{Provider: modelcredential.Anthropic,
		OperationID: label + "-set", ExpectedRevision: 0, Last4: key[len(key)-4:], Actor: actor}, key); err != nil {
		t.Fatalf("admit %s synthetic Project key (%T)", label, err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), time.Minute)
		defer done()
		if _, err := credentials.Mutate(cleanup, session.id, modelcredential.Mutation{Provider: modelcredential.Anthropic,
			OperationID: label + "-delete", ExpectedRevision: 1, Delete: true, Actor: actor}, ""); err != nil {
			t.Errorf("delete %s synthetic Project key (%T)", label, err)
		}
	})
	if probe.RequestsForTest() != 1 {
		t.Fatal("the synthetic credential was not validated exactly once at the local probe")
	}
}
