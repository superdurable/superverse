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
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// previewStartFailureBound bounds a start whose application exits at startup,
// from its admission to the STOPPED Preview with the failure: the build of the
// application, its exit and the release. A start that waited for its wait
// limit would take far longer.
const previewStartFailureBound = 10 * time.Minute

// TestPreviewStartFailureDexIntegration configures the fresh Project's Preview
// (newConfiguredPreviewSession), then pushes a server that refuses to start
// without a setting the Preview does not have, as a blocked checkpoint, which
// hands nothing to the Preview. A person prepares that commit and starts the
// Preview: its application exits at startup, the start fails PREVIEW_FAILED
// with the application's reason, and the Preview releases what the start began
// and reads STOPPED with that failure, never STARTING. A later source
// preparation, which clears the failure, leaves it STOPPED, and a new start is
// admitted and fails the same way. Each failure's delivery notification opens
// an automatic turn, the scenario's replies that declare it.
func TestPreviewStartFailureDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+mockTurnTimeout+4*previewCommandTimeout)
	defer cancel()
	session, first := newConfiguredPreviewSession(t, ctx)
	session.send(ctx, mockInput(t, "preview-start-failure", 1).Content)
	agent := session.finishedTurnAfter(ctx, first.Description.LastSequence)
	requireNoFailure(t, agent.History)
	if pushed := lastToolResult(t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitBlockedForTest {
		t.Fatalf("the checkpoint was not pushed as a blocked commit: %+v", pushed)
	}
	head := session.summary(ctx).Identity.HeadCommit
	if head == session.identity.HeadCommit {
		t.Fatalf("the checkpoint left the Session's HEAD at %s", head)
	}
	if prepared := session.preparePreview(ctx); prepared.PreparedSource.SourceCommit != head {
		t.Fatalf("the preparation names %s, not the checkpoint %s", prepared.PreparedSource.SourceCommit, head)
	}

	failed := session.failedStart(ctx, head)
	// The source preparation the observed Preview stranded in STARTING.
	if prepared := session.preparePreview(ctx); prepared.Preview.Status != preview.PreviewStoppedForTest {
		t.Fatalf("the source preparation after the failed start left the Preview %s", prepared.Preview.Status)
	}
	session.failedStart(ctx, head)

	session.await(ctx, "the automatic turns of both failures", func() bool {
		agent, all := session.history(ctx)
		return len(ofRole(since(all, first.Description.LastSequence), conversation.MessageRoleAssistant)) >= 8 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	_, all := session.history(ctx)
	messages := since(all, first.Description.LastSequence)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	notifications := ofRole(messages, conversation.MessageRoleNotification)
	if len(notifications) != 2 {
		t.Fatalf("expected the notifications of the two failed starts, got %d", len(notifications))
	}
	for _, message := range notifications {
		notice := message.Message.Notification
		if notice == nil || notice.Kind != conversation.NotificationKindDelivery || notice.Outcome != conversation.DeliveryFailure ||
			notice.Owner != conversation.DeliveryOwnerApplication || notice.Code != "PREVIEW_FAILED" || notice.Phase != string(preview.CommandStartPreview) || notice.Commit != head {
			t.Fatalf("the notification does not report the application's failed start: %+v", message.Message)
		}
	}
	t.Logf("FlowID=%s: two starts of %s failed PREVIEW_FAILED (%s) and left the Preview STOPPED", session.id, head, failed.Failure.Message)
}

// failedStart starts the Session's Preview through the Session Service, as
// Studio's Start Preview does, and requires the start of commit to fail
// PREVIEW_FAILED with the application's reason within
// previewStartFailureBound, after which the release of what it began leaves
// the Preview STOPPED with that failure. It returns the Preview after the
// release.
func (s *mockSession) failedStart(ctx context.Context, commit string) preview.Snapshot {
	s.t.Helper()
	current, err := s.previews.Get(ctx, s.identity.ProjectID)
	if err != nil {
		s.t.Fatalf("the Preview read failed (%T)", err)
	}
	if current.Preview.Status != preview.PreviewStoppedForTest && current.Preview.Status != preview.PreviewNotStarted {
		s.t.Fatalf("a person cannot start the Preview from %s", current.Preview.Status)
	}
	start := preview.Command{ID: "start-" + time.Now().UTC().Format("20060102150405.000000"), Kind: preview.CommandStartPreview,
		WorkspaceRequestID: current.WorkspaceRequestID, Generation: current.Generation, ExpectedRevision: current.Revision}
	admitted := time.Now()
	if _, err = s.service().PreviewCommand(ctx, s.account, s.identity.ProjectID, start); err != nil {
		s.t.Fatalf("the Preview start was not admitted (%T)", err)
	}
	bounded, cancel := context.WithTimeout(ctx, previewStartFailureBound)
	defer cancel()
	var state preview.Snapshot
	s.await(bounded, "the failed start "+start.ID, func() bool {
		state, err = s.previews.Get(ctx, s.identity.ProjectID)
		return err == nil && state.ActiveCommand.ID == "" && (state.LastCommand.ID == start.ID || state.LastCommand.ID == string(preview.CommandRelease)+"-"+start.ID)
	})
	switch failure := state.Failure; {
	case state.LastCommand.ID != string(preview.CommandRelease)+"-"+start.ID:
		s.t.Fatalf("the failed start %s was not released; the last command is %s (%s)", start.ID, state.LastCommand.ID, failureCode(state))
	case state.Preview.Status != preview.PreviewStoppedForTest:
		s.t.Fatalf("the failed start left the Preview %s", state.Preview.Status)
	case failure == nil || failure.Code != "PREVIEW_FAILED" || failure.Phase != string(preview.CommandStartPreview) || !strings.Contains(failure.Message, "WELCOME_TEXT is not set"):
		s.t.Fatalf("the Preview does not keep the start's failure with the application's reason: %+v", failure)
	case state.Configuration.SourceCommit != "" || state.Preview.URL != "":
		s.t.Fatalf("the released start still pins %s or serves %s", state.Configuration.SourceCommit, state.Preview.URL)
	}
	s.t.Logf("the start %s of %s failed and was released %s after its admission", start.ID, commit, time.Since(admitted).Round(time.Second))
	return state
}
