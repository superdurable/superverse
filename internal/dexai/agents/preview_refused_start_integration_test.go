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
	"slices"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
)

// TestPreviewRefusedStartKeepsServingDexIntegration starts a READY Preview
// again after its source preparation was refused: the Session's workspace
// holds an uncommitted note, so the preparation ends PREVIEW_DIRTY_HEAD and the
// instance has no READY preparation. The next start is refused
// PREVIEW_SOURCE_NOT_PREPARED by VerifyPreviewSource, before it pins anything,
// so it started nothing: the Preview that served before keeps serving, and no
// release stops it. Only the model is the mock.
func TestPreviewRefusedStartKeepsServingDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+2*mockTurnTimeout+3*previewCommandTimeout)
	defer cancel()
	session, first := newConfiguredPreviewSession(t, ctx)
	served := session.startPreview(ctx)
	project := session.identity.ProjectID

	note := "refused-start-" + time.Now().UTC().Format("20060102150405")
	session.send(ctx, mockInput(t, "workspace-note", 1).Content+" "+mockmodel.ArgumentMarkerForTest("note", note))
	requireNoFailure(t, session.finishedTurnAfter(ctx, first.Description.LastSequence).History)
	current, err := session.previews.Get(ctx, project)
	if err != nil {
		t.Fatalf("the Preview read failed (%T)", err)
	}
	prepare := preview.Command{ID: "prepare-" + time.Now().UTC().Format("20060102150405.000000"), Kind: preview.CommandPreparePreviewSource,
		WorkspaceRequestID: current.WorkspaceRequestID, Generation: current.Generation, ExpectedRevision: current.Revision}
	if err = session.service().PreparePreviewSource(ctx, session.account, project, prepare); err != nil {
		t.Fatalf("the source preparation was not admitted (%T)", err)
	}
	if dirty := session.awaitPreview(ctx, prepare.ID); dirty.Failure == nil || dirty.Failure.Code != "PREVIEW_DIRTY_HEAD" || dirty.Preview.Status != preview.PreviewReady {
		t.Fatalf("the preparation of the dirty workspace was not refused with the Preview serving: %s (%s)", dirty.Preview.Status, failureCode(dirty))
	}

	mark := markHistory(t, ctx, session.reader, preview.FlowID(project))
	current, err = session.previews.Get(ctx, project)
	if err != nil {
		t.Fatalf("the Preview read failed (%T)", err)
	}
	start := preview.Command{ID: "start-" + time.Now().UTC().Format("20060102150405.000000"), Kind: preview.CommandStartPreview,
		WorkspaceRequestID: current.WorkspaceRequestID, Generation: current.Generation, ExpectedRevision: current.Revision}
	if _, err = session.service().PreviewCommand(ctx, session.account, project, start); err != nil {
		t.Fatalf("the Preview start was not admitted (%T)", err)
	}
	// The start ends refused; a release the Preview admitted after it would
	// end too, so the wait accepts either as the last command.
	var after preview.Snapshot
	session.await(ctx, "the refused start and whatever it admitted", func() bool {
		var readErr error
		after, readErr = session.previews.Get(ctx, project)
		return readErr == nil && after.ActiveCommand.ID == "" && (after.LastCommand.ID == start.ID || after.LastCommand.ID == string(preview.CommandRelease)+"-"+start.ID)
	})
	if after.Failure == nil || after.Failure.Code != "PREVIEW_SOURCE_NOT_PREPARED" {
		t.Fatalf("the start without a READY preparation was not refused PREVIEW_SOURCE_NOT_PREPARED (%s)", failureCode(after))
	}
	// Nothing ran a release or a stop, and the Preview serves what it served.
	steps := completedStepsSince(t, ctx, session.reader, mark)
	if after.Preview.Status != preview.PreviewReady || after.Preview.URL != served.Preview.URL || after.Configuration.ObjectVersion != served.Configuration.ObjectVersion ||
		slices.Contains(steps, dex.GetFinalStepType(preview.StopPreview{})) {
		t.Fatalf("the start refused before it pinned anything stopped the serving Preview: %s after %s, steps %v", after.Preview.Status, after.LastCommand.Kind, steps)
	}
	session.stopPreview(ctx)
	t.Logf("FlowID=%s: %s was refused PREVIEW_SOURCE_NOT_PREPARED and the Preview kept serving %s", session.id, start.ID, served.Preview.SourceCommit)
}
