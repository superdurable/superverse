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
	"testing"

	"github.com/superdurable/superversev2/internal/agents/sandbox/sessionworkspace"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// PreviewReplacementTimeoutForTest bounds the whole of a PreviewReplacementForTest scenario: a
// configured Preview start, the instance replacement and a second start.
const PreviewReplacementTimeoutForTest = previewSetupTimeout + previewCommandTimeout + 2*mockTurnTimeout

// PreviewReplacementForTest is the Session of a throwaway Project whose Preview serves on its
// instance, which the scenario replaces. The Studio Runs that the external tests drive import this
// package, so only these exports reach the helpers of the in-package tests.
type PreviewReplacementForTest struct {
	// Service, Projects and Objects are what Studio Runs reads the Session, the Project and the
	// Preview's definitions with; ProjectID and Account are the Project and its owner.
	Service  *SessionFlowService
	Projects *projects.ProjectFlowClient
	Objects  *objectstore.Store
	// Connections reaches the Preview engines of the lane's Sandboxes as the lane's server does.
	Connections infra.Connections
	ProjectID   string
	Account     string
	session     *mockSession
	sandboxes   sandboxObjects
}

// NewPreviewReplacementForTest brings up a Session whose Preview, configured as a person does in
// Studio, is READY on its first instance.
func NewPreviewReplacementForTest(t *testing.T, ctx context.Context) *PreviewReplacementForTest {
	t.Helper()
	session, _ := newConfiguredPreviewSession(t, ctx)
	session.startPreview(ctx)
	return &PreviewReplacementForTest{Service: session.service(), Projects: session.projects, Objects: artifactStore(ctx, t),
		ProjectID: session.identity.ProjectID, Account: session.account, session: session, sandboxes: newSandboxObjects(t),
		Connections: laneConnections(t)}
}

// Instance is the ID of the Session's current instance, which a Runs request names.
func (p *PreviewReplacementForTest) Instance(ctx context.Context) string {
	return p.session.workspace(ctx).InstanceID
}

// ReplaceInstance removes the Session's READY instance behind its back, as a lost node does, and
// lets the next turn provision a new one on the same volume.
func (p *PreviewReplacementForTest) ReplaceInstance(ctx context.Context) {
	s := p.session
	s.t.Helper()
	lost := s.workspace(ctx)
	p.sandboxes.removeInstance(ctx, lost.Resources)
	s.await(ctx, "the removal of the instance", func() bool { return p.sandboxes.instanceGone(ctx, lost.Resources) })
	snapshot, err := s.client.GetSnapshot(ctx, s.id)
	if err != nil || snapshot.Agent == nil {
		s.t.Fatalf("the Session read failed (%T)", err)
	}
	next := conversation.PendingUserMessage{MessageID: conversation.MessageID("replacement-" + lost.InstanceID),
		Value: conversation.UserMessage{Content: mockInput(s.t, "inspect-basic", 1).Content}}
	if err = s.client.SendPendingMessage(ctx, s.id, next); err != nil {
		s.t.Fatalf("the next message was not admitted (%T)", err)
	}
	requireNoFailure(s.t, s.finishedTurnAfter(ctx, snapshot.Agent.Description.LastSequence).History)
	if replaced := s.workspace(ctx); replaced.InstanceID == lost.InstanceID || replaced.Instance != sessionworkspace.InstanceReady {
		s.t.Fatalf("the turn did not run on a new instance: %s %s", replaced.InstanceID, replaced.Instance)
	}
}

// StartPreview prepares the source on the Session's current instance, which the Preview's earlier
// preparation does not cover, and starts the Project's Preview there, as Studio does, requiring it
// READY.
func (p *PreviewReplacementForTest) StartPreview(ctx context.Context) {
	p.session.t.Helper()
	p.session.preparePreview(ctx)
	p.session.startPreview(ctx)
}
