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
	"reflect"
	"testing"

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
)

// Real start_agent calls create provider-B worktrees and child model/tool calls.
// Provider A mutates while those B children execute actual Sandbox commands.
func exerciseBYOKChildIsolation(t *testing.T, ctx context.Context, sessions *sessionagent.SessionAgentFlowClient, children *subagent.SubagentFlowClient, api pb.ModelCredentialServiceClient, agent pb.AgentServiceClient, project string, caller *pb.CallerContext, key string, probe *mockmodel.CredentialProbeForTest, wait func(string, func() bool)) agenttasks.SubagentIdentity {
	t.Helper()
	owner := agentloop.SessionFlowID(project)
	provider := modelcredential.ForModel(conversation.ModelGPT6Astra)
	snapshot := func() agentloop.AgentDescription {
		t.Helper()
		got, err := sessions.GetSnapshot(ctx, owner)
		if err != nil || got.Agent == nil {
			t.Fatalf("read actual isolation Session snapshot (%s; agent present=%t)", dexflowtest.ErrorCategory(err), got.Agent != nil)
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
		if _, err := api.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project, Provider: pb.ModelProvider_MODEL_PROVIDER_OPENAI, RequestId: flowid.RandomID(20), ExpectedRevision: &revision, ApiKey: mockmodel.APIKey, Caller: caller}); err != nil {
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
		wait("live provider B child model and Sandbox tool execution", func() bool {
			found = nil
			for _, record := range snapshot().Subagents {
				if record.Model != conversation.ModelGPT6Astra || record.Status.Finished() {
					continue
				}
				id := agenttasks.SubagentIdentity{ProjectID: project, AgentID: record.AgentID}
				child, err := children.SnapshotForTest(ctx, id)
				copy, copyErr := children.ReadModelCredential(ctx, id)
				if err == nil && child.Config != nil && child.Config.Model == conversation.ModelGPT6Astra && child.LastSequence >= 3 && child.Outcome == nil && copyErr == nil && copy != nil && copy.Revision == info().Revision && copy.Key == mockmodel.APIKey {
					found = append(found, id)
				}
			}
			return len(found) == count
		})
		return found
	}
	saveB()
	choose(pb.AgentModel_AGENT_MODEL_GPT_6_ASTRA)
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
		if err != nil || copy == nil || copy.Key != mockmodel.APIKey || copy.Revision != before.Revision {
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
	if _, err := api.DeleteModelCredential(ctx, &pb.DeleteModelCredentialRequest{ProjectId: project, Provider: pb.ModelProvider_MODEL_PROVIDER_OPENAI, RequestId: flowid.RandomID(20), ExpectedRevision: &revision, Caller: caller}); err != nil {
		t.Fatal("delete B key through actual API")
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
	choose(pb.AgentModel_AGENT_MODEL_GPT_6_ASTRA)
	send(3)
	retained := live(1)
	t.Log("Live OpenAI child model/tool execution: A mutation preserves B holder, record and key copy; B deletion stops and clears them while in-flight A context stays unchanged")
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
