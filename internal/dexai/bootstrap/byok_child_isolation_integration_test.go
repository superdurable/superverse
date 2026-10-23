// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"context"
	"fmt"
	"google.golang.org/grpc/status"
	"reflect"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
)

// Real start_agent calls create provider-B worktrees and child model/tool calls.
// Provider A mutates while those B children execute actual Sandbox commands.
func exerciseBYOKChildIsolation(t *testing.T, ctx context.Context, sessions *sessionagent.SessionAgentFlowClient, children *subagent.SubagentFlowClient, api pb.ModelCredentialServiceClient, agent pb.AgentServiceClient, project string, caller *pb.CallerContext, key string, probe *mockmodel.CredentialProbeForTest, model conversation.Model, wait func(string, func() bool)) agenttasks.SubagentIdentity {
	t.Helper()
	owner := agentloop.SessionFlowID(project)
	provider := modelcredential.ForModel(model)
	snapshot := func() agentloop.AgentDescription {
		t.Helper()
		got, err := sessions.GetSnapshot(ctx, owner)
		if err != nil || got.Agent == nil {
			t.Fatalf("read actual isolation Session snapshot (%s; grpc=%s; agent present=%t)", dexflowtest.ErrorCategory(err), status.Code(err), got.Agent != nil)
		}
		return got.Agent.Description
	}
	info := func() modelcredential.Info {
		t.Helper()
		value, err := sessions.ReadModelCredentialInfo(ctx, owner, provider)
		if err != nil {
			t.Fatal("read provider B metadata")
		}
		return value
	}
	saveB := func() {
		t.Helper()
		revision := info().Revision
		if _, err := api.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project, Provider: boundary.Enum[pb.ModelProvider](provider.String()), RequestId: flowid.RandomID(20), ExpectedRevision: &revision, ApiKey: mockmodel.APIKeyForTest, Caller: caller}); err != nil {
			t.Fatal("save B key through actual mock inference admission")
		}
	}
	choose := func(model pb.AgentModel) {
		t.Helper()
		revision := snapshot().ModelRevision
		if _, err := agent.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: project, Model: model, ExpectedRevision: &revision, Caller: caller}); err != nil {
			t.Fatal("switch actual keyed Session provider")
		}
	}
	send := func(input int) {
		t.Helper()
		if _, err := agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project, MessageId: flowid.RandomID(20), Text: mockInput(t, "byok-copy", input).Content, Caller: caller}); err != nil {
			t.Fatal("send actual provider isolation scenario")
		}
	}
	live := func(count int) []agenttasks.SubagentIdentity {
		t.Helper()
		var found []agenttasks.SubagentIdentity
		var logged time.Time
		wait("live provider B child model and Sandbox tool execution", func() bool {
			found = nil
			var progress []string
			for _, record := range snapshot().Subagents {
				if record.Model != model || record.Status.Finished() {
					progress = append(progress, fmt.Sprintf("%s record=%s model=%s", record.AgentID, record.Status, record.Model))
					continue
				}
				id := agenttasks.SubagentIdentity{ProjectID: project, AgentID: record.AgentID}
				child, err := children.SnapshotForTest(ctx, id)
				copy, copyErr := children.ReadModelCredential(ctx, id)
				matchesModel := child.Config != nil && child.Config.Model == model
				keyMatches := copy != nil && copy.Key == mockmodel.APIKeyForTest
				progress = append(progress, fmt.Sprintf("%s record=%s sequence=%d outcome=%t model_match=%t key_match=%t state_error=%s copy_error=%s", record.AgentID, record.Status, child.LastSequence, child.Outcome != nil, matchesModel, keyMatches, dexflowtest.ErrorCategory(err), dexflowtest.ErrorCategory(copyErr)))
				if err == nil && child.Config != nil && child.Config.Model == model && child.LastSequence >= 3 && child.Outcome == nil && copyErr == nil && copy != nil && copy.Revision == info().Revision && copy.Key == mockmodel.APIKeyForTest {
					found = append(found, id)
				}
			}
			if time.Since(logged) >= 30*time.Second {
				t.Logf("Provider %s live-child progress: ready=%d expected=%d %v", provider.String(), len(found), count, progress)
				logged = time.Now()
			}
			return len(found) == count
		})
		return found
	}
	choices, err := agent.ListAgentModels(ctx, &pb.ListAgentModelsRequest{Caller: caller})
	if err != nil {
		t.Fatal("read configured isolation models")
	}
	var selected pb.AgentModel
	for _, choice := range choices.GetModels() {
		if choice.GetLabel() == model.Details().Label {
			selected = choice.GetModel()
		}
	}
	if selected == pb.AgentModel_AGENT_MODEL_UNSPECIFIED {
		t.Fatal("isolation model unavailable")
	}
	saveB()
	choose(selected)
	firstTurn := snapshot().Turn
	send(1)
	first := live(agenttasks.MaximumRunningSubagents)
	wait("B parent completed while its children run", func() bool {
		state := snapshot()
		return state.Turn > firstTurn && state.Status == agentloop.AgentStatusWaitingForMessage
	})
	before := info()
	records := snapshot().Subagents
	choose(pb.AgentModel_AGENT_MODEL_HAIKU_4_5)
	aInfo, err := sessions.ReadModelCredentialInfo(ctx, owner, modelcredential.Anthropic)
	if err != nil {
		t.Fatal("read A key revision")
	}
	revision := aInfo.Revision
	if _, err := api.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: flowid.RandomID(20), ExpectedRevision: &revision, ApiKey: key, Caller: caller}); err != nil {
		t.Fatal("replace provider A through actual API")
	}
	if !reflect.DeepEqual(before, info()) || !reflect.DeepEqual(records, snapshot().Subagents) {
		t.Fatal("provider A mutation changed provider B holders or records")
	}
	for _, id := range first {
		copy, err := children.ReadModelCredential(ctx, id)
		if err != nil || copy == nil || copy.Key != mockmodel.APIKeyForTest || copy.Revision != before.Revision {
			t.Fatal("provider A mutation changed live B child copy")
		}
	}
	entered, release := probe.HoldNextForTest()
	defer release()
	send(2)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("actual Anthropic request did not enter endpoint")
	}
	active := snapshot()
	if active.Status != agentloop.AgentStatusCallingModel || active.ContextModel != conversation.ModelHaiku45 {
		t.Fatal("no in-flight A context for provider fence")
	}
	revision = info().Revision
	if _, err := api.DeleteModelCredential(ctx, &pb.DeleteModelCredentialRequest{ProjectId: project, Provider: boundary.Enum[pb.ModelProvider](provider.String()), RequestId: flowid.RandomID(20), ExpectedRevision: &revision, Caller: caller}); err != nil {
		t.Fatalf("delete B key through actual API (%s)", status.Code(err))
	}
	wait("live B child stops reach retained records", func() bool {
		for _, record := range snapshot().Subagents {
			if !record.Status.Finished() {
				return false
			}
		}
		return true
	})
	for _, id := range first {
		assertBYOKChildAbsent(t, ctx, sessions, children, owner, id, provider)
	}
	unchanged := snapshot()
	if unchanged.Status != active.Status || unchanged.ContextModel != active.ContextModel || unchanged.Turn != active.Turn || !reflect.DeepEqual(unchanged.Executor, active.Executor) {
		t.Fatal("B mutation interrupted A context, turn or executor")
	}
	release()
	wait("A request completed after unrelated B deletion", func() bool { return snapshot().Status == agentloop.AgentStatusWaitingForMessage })
	saveB()
	choose(selected)
	send(3)
	retained := live(1)
	t.Log("Live compatible-provider child model/tool execution: A mutation preserves B holder, record and key copy; B deletion stops and clears them while in-flight A context stays unchanged")
	return retained[0]
}

func assertBYOKChildAbsent(t *testing.T, ctx context.Context, sessions *sessionagent.SessionAgentFlowClient, children *subagent.SubagentFlowClient, owner agentloop.FlowID, child agenttasks.SubagentIdentity, provider modelcredential.Provider) {
	t.Helper()
	copy, err := children.ReadModelCredential(ctx, child)
	info, infoErr := sessions.ReadModelCredentialInfo(ctx, owner, provider)
	if err != nil || infoErr != nil || copy != nil || len(info.Holders) != 0 {
		t.Fatal("provider mutation or Project deletion retained child copy or holders")
	}
	snapshot, err := sessions.GetSnapshot(ctx, owner)
	if err != nil || snapshot.Agent == nil {
		t.Fatal("read retained child record")
	}
	for _, record := range snapshot.Agent.Description.Subagents {
		if record.AgentID == child.AgentID {
			if !record.Status.Finished() {
				t.Fatal("provider mutation retained running child record")
			}
			return
		}
	}
	t.Fatal("provider mutation removed retained child record")
}
