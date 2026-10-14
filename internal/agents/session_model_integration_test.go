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
	"testing"

	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

// This creates a throwaway READY Project, whose Session starts on the
// server's start model, and selects the Session's model through the real
// UpdateSessionModel RPC. It sends no message, so no model is called and no
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
