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

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
)

// EvolveProjectForTest is a LiveProjectForTest whose Session can push the
// goals of the mock scenario evolve-goals, with a client of the Project's
// EvolveFlow, which the lane's server Worker runs, and a reader of its engine
// execution.
type EvolveProjectForTest struct {
	LiveProjectForTest
	Evolve    *evolve.EvolveFlowClient
	Execution dexpb.FlowServiceClient
	session   *mockSession
}

// NewEvolveProjectForTest deploys a throwaway Project's HEAD to Live, as
// NewLiveProjectForTest does, keeping its Session.
func NewEvolveProjectForTest(t *testing.T, ctx context.Context) EvolveProjectForTest {
	t.Helper()
	session := newMockSession(t, ctx, "")
	live := newLiveDelivery(session)
	head := session.identity.HeadCommit
	project := liveProjectOf(t, ctx, session, live, head)
	// This client registry supplies the definition only; the server Worker runs the Flow.
	measurement := evolve.NewEvolveFlowBootstrap(evolve.Settings{}, live.projects, nil, nil, nil)
	measurement.Client().Bind(dexflowtest.ServerWorkerClient(t, measurement.Flow()))
	return EvolveProjectForTest{LiveProjectForTest: project, Evolve: measurement.Client(), Execution: session.reader, session: session}
}

// PushGoalsForTest sends content, an input of the evolve-goals scenario that
// writes .superverse/goals.md and pushes it through the commit gate, and
// returns the pushed commit, the Project's new HEAD.
func (p EvolveProjectForTest) PushGoalsForTest(ctx context.Context, content string) string {
	s := p.session
	s.t.Helper()
	before := s.description(ctx).LastSequence
	s.send(ctx, content)
	agent := s.finishedTurnAfter(ctx, before)
	requireNoFailure(s.t, agent.History)
	if pushed := lastToolResult(s.t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded || pushed.ResultCode != agentloop.CommitPushedForTest {
		s.t.Fatalf("the goals were not pushed: %+v", pushed)
	}
	return s.summary(ctx).Identity.HeadCommit
}
