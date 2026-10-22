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
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
)

// The fixture uses actual Dex writes and a real Sandbox worktree. A deliberately
// stale retained copy parks an unavailable-provider child at its existing model
// credential retry, before routing can invoke an unavailable provider.
func exerciseBYOKChildIsolation(t *testing.T, ctx context.Context, config serverConfig, owned *resources, reader dexpb.FlowServiceClient, sessions *sessionagent.SessionAgentFlowClient, children *subagent.SubagentFlowClient, api pb.ModelCredentialServiceClient, agent pb.AgentServiceClient, project string, caller *pb.CallerContext, key string, probe *mockmodel.CredentialProbeForTest, wait func(string, func() bool)) agenttasks.SubagentIdentity {
	t.Helper()
	owner := agentloop.SessionFlowID(project)
	provider := modelcredential.ForModel(conversation.ModelGPT6Astra)
	send := func() {
		t.Helper()
		if _, err := agent.SendMessage(ctx, &pb.SendMessageRequest{ProjectId: project, MessageId: flowid.RandomID(20), Text: "Continue the credential isolation check", Caller: caller}); err != nil {
			t.Fatal("send actual Anthropic isolation request")
		}
	}
	snapshot := func() agentloop.AgentDescription {
		t.Helper()
		got, err := sessions.GetSnapshot(ctx, owner)
		if err != nil || got.Agent == nil {
			t.Fatal("read actual isolation Session snapshot")
		}
		return got.Agent.Description
	}
	initialTurn := snapshot().Turn
	send()
	wait("isolation workspace turn", func() bool {
		state := snapshot()
		return state.Status == agentloop.AgentStatusWaitingForMessage && state.Turn > initialTurn
	})
	wait("isolation workspace ready", func() bool {
		got, err := sessions.GetSnapshot(ctx, owner)
		return err == nil && got.Session.Workspace.State == agentloop.WorkspaceReady
	})
	ws := readBYOKAttribute[agentloop.SessionWorkspace](t, ctx, reader, string(owner), "Workspace")
	runtime, err := newDockerProviders(ctx, config, owned)
	if err != nil {
		t.Fatal("open actual Native workspace providers")
	}
	caps := workspace.NewCapabilities(&git.HostCredentials{Host: config.host}, config.agent.ConnectorLibrary, runtime.sandboxAddresses, config.agent.ModuleRedirects...)
	owned.add(caps.Close)
	runner := sandboxtools.NewRunner(caps.SandboxResolver(), caps.Git())
	info := func() modelcredential.Info {
		t.Helper()
		value, err := sessions.ReadModelCredentialInfo(ctx, owner, provider)
		if err != nil {
			t.Fatal("read provider B metadata")
		}
		return value
	}
	reseed := func() {
		t.Helper()
		current := info()
		mutation := modelcredential.Mutation{Provider: provider, OperationID: flowid.RandomID(20), ExpectedRevision: current.Revision}
		admitted, err := sessions.ReserveModelCredential(ctx, owner, mutation)
		if err != nil || !admitted.Fresh {
			t.Fatal("reserve provider B test source")
		}
		if err := sessions.WriteModelCredential(ctx, owner, mutation, &modelcredential.Envelope{Revision: admitted.Info.Revision, Key: key}); err != nil {
			t.Fatal("write provider B test source")
		}
		if _, err := sessions.FinalizeModelCredential(ctx, owner, mutation); err != nil {
			t.Fatal("finalize provider B test source")
		}
	}
	park := func() agenttasks.SubagentIdentity {
		t.Helper()
		current := info()
		callID := conversation.CallID(flowid.RandomID(20))
		identity := agenttasks.SubagentIdentity{ProjectID: project, AgentID: flowid.AgentTaskID(string(owner), string(callID))}
		call := agenttasks.SubagentCall{Kind: agenttasks.SubagentInvestigate, Description: "Provider B credential isolation", Prompt: "Inspect the workspace"}
		result, err := runner.Run(ctx, sandboxtools.CreateAgentWorktree, sandboxtools.Call{Tool: string(conversation.ToolStartAgent), Arguments: sandboxtools.AgentWorktreeArguments(identity.AgentID, nil), Identity: sandboxd.InvocationIdentity{FlowID: string(owner), CallID: string(callID)}, Lease: &ws.Lease, Root: sandboxd.WorkspaceRoot})
		if err != nil || result.Failure != nil || result.Worktree == nil {
			t.Fatal("create actual provider B child worktree")
		}
		fingerprint := call.Fingerprint(string(owner), identity.AgentID)
		record := agenttasks.NewSubagentRecord(identity.AgentID, callID, call, fingerprint, time.Now())
		record.Model = conversation.ModelGPT6Astra
		writeBYOKAttribute(t, ctx, reader, string(owner), "Subagents/"+identity.AgentID, record)
		current.Holders = append(current.Holders, modelcredential.Holder{AgentID: identity.AgentID, CallID: string(callID), Revision: current.Revision})
		writeBYOKAttribute(t, ctx, reader, string(owner), "ModelKeyInfo/"+provider.String(), current)
		if err := children.Start(ctx, agenttasks.SubagentStart{Identity: identity, Call: call, Fingerprint: fingerprint, BaseCommit: result.Worktree.Base, Model: conversation.ModelGPT6Astra, Lease: ws.Lease, CredentialRevision: current.Revision + 1}); err != nil {
			t.Fatal("start actual provider B child")
		}
		if err := children.WriteModelCredential(ctx, identity, flowid.RandomID(20), &modelcredential.Envelope{Revision: current.Revision, Key: key}); err != nil {
			t.Fatal("retain stale child credential copy")
		}
		wait("observed provider B child credential retry", func() bool {
			child, err := children.SnapshotForTest(ctx, identity)
			if err != nil || child.Config == nil || child.Config.Model != conversation.ModelGPT6Astra || child.Outcome != nil {
				return false
			}
			state, err := reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: identity.FlowID()})
			if err != nil {
				return false
			}
			for _, step := range state.GetActiveStepExecutions() {
				if step.GetStepType() == dex.GetFinalStepType(subagent.CallSubagentModel{}) && step.GetLastFailureInfo().GetAttempt() > 0 {
					return true
				}
			}
			return false
		})
		return identity
	}
	clear := func(identity agenttasks.SubagentIdentity) {
		t.Helper()
		revision := info().Revision
		if _, err := api.DeleteModelCredential(ctx, &pb.DeleteModelCredentialRequest{ProjectId: project, Provider: pb.ModelProvider_MODEL_PROVIDER_OPENAI, RequestId: flowid.RandomID(20), ExpectedRevision: &revision, Caller: caller}); err != nil {
			t.Fatal("delete provider B through actual resource API")
		}
		wait("own-provider child stop reaches retained record", func() bool {
			for _, record := range snapshot().Subagents {
				if record.AgentID == identity.AgentID {
					return record.Status.Finished()
				}
			}
			return false
		})
		assertBYOKChildAbsent(t, ctx, sessions, children, owner, identity, provider)
	}
	first := park()
	before := info()
	record := readBYOKAttribute[agenttasks.SubagentRecord](t, ctx, reader, string(owner), "Subagents/"+first.AgentID)
	revision := uint64(1)
	if _, err := api.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project, Provider: pb.ModelProvider_MODEL_PROVIDER_ANTHROPIC, RequestId: "isolation-anthropic-replace", ExpectedRevision: &revision, ApiKey: key, Caller: caller}); err != nil {
		t.Fatal("replace provider A through actual API")
	}
	afterRecord := readBYOKAttribute[agenttasks.SubagentRecord](t, ctx, reader, string(owner), "Subagents/"+first.AgentID)
	copy, err := children.ReadModelCredential(ctx, first)
	if err != nil || copy == nil || copy.Key != key || copy.Revision != before.Revision || !reflect.DeepEqual(before, info()) || !reflect.DeepEqual(record, afterRecord) {
		t.Fatal("provider A mutation changed provider B holder, record or retained copy")
	}
	clear(first)
	reseed()
	second := park()
	entered, release := probe.HoldNextForTest()
	defer release()
	send()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("actual Anthropic request did not enter mock endpoint")
	}
	active := snapshot()
	if active.Status != agentloop.AgentStatusCallingModel || active.ContextModel != conversation.ModelHaiku45 {
		t.Fatal("no in-flight Anthropic context for isolation fence")
	}
	clear(second)
	unchanged := snapshot()
	if unchanged.Status != active.Status || unchanged.ContextModel != active.ContextModel || unchanged.Turn != active.Turn || !reflect.DeepEqual(unchanged.Executor, active.Executor) {
		t.Fatal("provider B mutation interrupted provider A context or executor")
	}
	release()
	wait("provider A request completes after unrelated mutation", func() bool { return snapshot().Status == agentloop.AgentStatusWaitingForMessage })
	reseed()
	third := park()
	t.Log("Actual provider B child retry: Anthropic mutation preserves holder, record and copy; own-provider deletion clears them; in-flight Anthropic ContextModel stays unchanged")
	return third
}

func writeBYOKAttribute(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flow, key string, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal("encode non-secret isolation fixture")
	}
	_, err = reader.SetAttributes(ctx, &dexpb.SetAttributesRequest{FlowId: flow, RequestId: flowid.RandomID(20), Attributes: []*dexpb.AttributeWrite{{Key: key, Value: &dexpb.Value{Kind: &dexpb.Value_ObjValue{ObjValue: &dexpb.EncodedObject{Encoding: "json", Payload: payload}}}}}})
	if err != nil {
		t.Fatal("write real Dex isolation fixture metadata")
	}
}
func readBYOKAttribute[T any](t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flow, key string) T {
	t.Helper()
	var value T
	got, err := reader.GetAttributes(ctx, &dexpb.GetAttributesRequest{FlowId: flow, Keys: []string{key}})
	if err != nil || len(got.GetAttributes()) != 1 {
		t.Fatalf("read exact non-secret fixture %s (%T)", key, err)
	}
	raw := got.GetAttributes()[0].GetValue()
	if blob := raw.GetInternalBlobIdForObjValue(); blob != "" {
		loaded, loadErr := reader.LoadBlobs(ctx, &dexpb.LoadBlobsRequest{Entries: []*dexpb.LoadBlobRequestEntry{{FlowId: flow, BlobValue: raw}}})
		if loadErr != nil {
			t.Fatalf("hydrate exact non-secret fixture %s (%T)", key, loadErr)
		}
		raw = loaded.GetValues()[blob]
	}
	if json.Unmarshal(raw.GetObjValue().GetPayload(), &value) != nil {
		t.Fatalf("decode exact non-secret fixture %s", key)
	}

	return value
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
