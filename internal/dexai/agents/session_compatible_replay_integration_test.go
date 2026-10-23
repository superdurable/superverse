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
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"google.golang.org/protobuf/proto"
)

// TestCompatibleSessionReplayDexIntegration drives both real wire adapters through
// the existing Worker, real parallel Sandbox reads, archive movement and compaction.
func TestCompatibleSessionReplayDexIntegration(t *testing.T) {
	for _, model := range []conversation.Model{conversation.ModelMockResponses, conversation.ModelMockChat, conversation.ModelMuseWonder10, conversation.ModelGPT6Astra, conversation.ModelGrok47, conversation.ModelGemini4Argon} {
		t.Run(string(model), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
			defer cancel()
			session := newSelectableSession(t, ctx, model, mockInput(t, "compatible-replay", 1).Content)
			first := session.finishedTurn(ctx)
			requireNoFailure(t, first.History)
			if first.Description.ContextModel != model || first.History.NextBeforeSequence == nil {
				t.Fatal("compatible Session did not run across the archive window")
			}
			compatiblePublicProjection(t, first)
			archived, err := session.client.GetArchivedMessageRange(ctx, session.id, *first.History.NextBeforeSequence, 100)
			if err != nil {
				t.Fatal("read public compatible archive:", dexflowtest.ErrorCategory(err))
			}
			compatiblePublicProjection(t, archived)
			entries := compatibleCanonicalEntries(ctx, t, session)
			active, archivedReplay := 0, 0
			for _, item := range entries {
				for position, block := range item.Entry.Blocks {
					if strings.Contains(string(block), "Synthetic private reasoning") {
						t.Fatal("plaintext model reasoning entered canonical history")
					}
					if !conversation.IsOpaqueReplay(block) {
						continue
					}
					if _, err := conversation.ReadOpaqueReplay(block, model.Details().Provider, model.Details().Provider.APIStyle(), position); err != nil {
						t.Fatal("stored compatible replay binding is invalid")
					}
					active++
					if item.Sequence < *first.History.NextBeforeSequence {
						archivedReplay++
					}
				}
			}
			if active < 12 || archivedReplay == 0 {
				t.Fatal("private replay was not retained in both message maps")
			}
			session.send(ctx, mockInput(t, "compatible-replay", 2).Content)
			second := session.waitForSnapshot(ctx, "the compatible compaction result", func(agent agentloop.AgentSnapshot) bool {
				return endsWithReplyAfter(agent, first.Description.LastSequence)
			})
			requireNoFailure(t, second.History)
			compatiblePublicProjection(t, second)
			var segmentStart conversation.Sequence
			for _, message := range second.History.Messages {
				if message.Sequence > first.Description.LastSequence && message.Message.Content == agentloop.SegmentMarker(agentloop.SegmentReasonCompaction) {
					segmentStart = message.Sequence
				}
			}
			if segmentStart == 0 {
				t.Fatal("compatible history did not compact into a new segment")
			}
			for _, item := range compatibleCanonicalEntries(ctx, t, session) {
				// A new Messages reply can already hold its own valid opaque replay.
				if item.Sequence >= segmentStart {
					continue
				}
				for _, block := range item.Entry.Blocks {
					if conversation.IsOpaqueReplay(block) {
						t.Fatalf("closed segment entry %d retained opaque replay before segment %d", item.Sequence, segmentStart)
					}
				}
			}
			// Exercise the existing terminal cleanup path with another replayable reply.
			session.send(ctx, mockInput(t, "compatible-replay", 3).Content)
			third := session.waitForSnapshot(ctx, "the next compatible turn", func(agent agentloop.AgentSnapshot) bool {
				return endsWithReplyAfter(agent, second.Description.LastSequence)
			})
			requireNoFailure(t, third.History)
			retained := 0
			for _, item := range compatibleCanonicalEntries(ctx, t, session) {
				for _, block := range item.Entry.Blocks {
					if conversation.IsOpaqueReplay(block) {
						retained++
					}
				}
			}
			if retained == 0 {
				t.Fatal("terminal cleanup fixture has no active replay")
			}
			projecttest.DeleteForTest(ctx, t, session.projects, session.identity.ProjectID)
			for _, item := range compatibleCanonicalEntries(ctx, t, session) {
				for _, block := range item.Entry.Blocks {
					if conversation.IsOpaqueReplay(block) {
						t.Fatal("completed Session retained opaque replay")
					}
				}
			}
			t.Logf("FlowID=%s model=%s active replay blocks=%d archived replay blocks=%d; real tools, compaction and terminal cleanup completed", session.id, model, active, archivedReplay)
		})
	}
}

// Real remote activation closes the platform protocol segment. Its brief and
// delivery history must remain public while private replay is removed from both maps.
func TestCompatibleRemoteReplayPrivacyHTTPDexIntegration(t *testing.T) {
	for _, model := range []conversation.Model{conversation.ModelMockResponses, conversation.ModelMockChat} {
		t.Run(string(model), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
			defer cancel()
			session, api, caller := remoteSessionAPI(ctx, t)
			started := session.awaitStarted(ctx)
			if err := session.client.UpdateSessionModel(ctx, session.id, agentloop.UpdateSessionModelRequest{Model: model, ExpectedRevision: started.ModelRevision}); err != nil {
				t.Fatal("select compatible fixture:", dexflowtest.ErrorCategory(err))
			}
			session.send(ctx, mockInput(t, "compatible-replay", 1).Content)
			initial := session.finishedTurn(ctx)
			requireNoFailure(t, initial.History)
			retained := false
			for _, item := range compatibleCanonicalEntries(ctx, t, session) {
				for _, block := range item.Entry.Blocks {
					retained = retained || conversation.IsOpaqueReplay(block)
				}
			}
			if !retained {
				t.Fatal("handoff fixture has no private replay")
			}
			before := session.remoteModelCheckpoint(ctx)
			remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
			var active bridgeapi.ActivateResponse
			remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "compatible-privacy", HostLabel: "integration host"}}, &active, http.StatusOK)
			compatiblePublicProjection(t, active)
			session.await(ctx, "closed compatible replay cleanup", func() bool {
				for _, item := range compatibleCanonicalEntries(ctx, t, session) {
					for _, block := range item.Entry.Blocks {
						if conversation.IsOpaqueReplay(block) {
							return false
						}
					}
				}
				return true
			})
			var synced bridgeapi.SyncResponse
			session.await(ctx, "public compatible remote brief", func() bool {
				remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, Context: true}, &synced, http.StatusOK)
				compatiblePublicProjection(t, synced)
				return synced.AgentContextState == bridgeapi.AgentContextReady
			})
			if !strings.Contains(synced.AgentContext, "<repository_context>") || len(synced.Entries) == 0 {
				t.Fatal("remote recovery omitted its repository brief or canonical history")
			}
			requireRemoteModelInterval(t, session.remoteModelCheckpoint(ctx), before)
			t.Logf("FlowID=%s model=%s remote brief/history excluded private replay; executor switch purged both canonical maps without another provider call", session.id, model)
		})
	}
}

// The supported profile admits the actual effort tool and carries its notice
// through later adapter calls; the non-thinking profile never offers that tool.
func TestCompatibleEffortProfilesDexIntegration(t *testing.T) {
	t.Run("responses", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
		defer cancel()
		session := newSession(t, ctx, conversation.ModelMockResponses, mockInput(t, "question-answers", 1).Content)
		agent := session.pendingInput(ctx, "", 0)
		requireNoFailure(t, agent.History)
		if !slices.Contains(agent.Description.AvailableTools, conversation.ToolSetEffort) || agent.Description.Effort != conversation.EffortHigh {
			t.Fatal("Responses profile did not retain the admitted effort change")
		}
		_, messages := session.history(ctx)
		if toolResult(t, messages, conversation.ToolSetEffort, 0).Outcome != conversation.ToolOutcomeSucceeded {
			t.Fatal("Responses effort tool did not succeed")
		}
		var latestCall *conversation.ModelCallMetrics
		for _, message := range agent.History.Messages {
			if message.Message.ModelCall != nil {
				latestCall = message.Message.ModelCall
			}
		}
		if latestCall == nil || latestCall.Effort != conversation.EffortHigh {
			t.Fatal("the later Responses request lost its accepted effort")
		}
		answers := mockInput(t, "question-answers", 2)
		request := agentloop.AnswerQuestionsRequest{CallID: agent.Description.PendingUserInput.CallID}
		for index, question := range agent.Description.PendingUserInput.Questions {
			request.Answers = append(request.Answers, agentloop.UserInputAnswer{QuestionID: question.ID, Answer: answers.Answers[index]})
		}
		finished, _ := session.answer(ctx, request, agent.Description.LastSequence)
		requireNoFailure(t, finished.History)
	})
	t.Run("non-thinking-chat", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
		defer cancel()
		session := newSession(t, ctx, conversation.ModelMockChat, mockInput(t, "explain", 1).Content)
		agent := session.finishedTurn(ctx)
		requireNoFailure(t, agent.History)
		if slices.Contains(agent.Description.AvailableTools, conversation.ToolSetEffort) || agent.Description.Effort != "" {
			t.Fatal("non-thinking Chat profile offered an effort change")
		}
		for _, message := range agent.History.Messages {
			if call := message.Message.ModelCall; call != nil && call.Effort != "" {
				t.Fatal("non-thinking Chat call carried an effort control")
			}
		}
	})
}

func compatiblePublicProjection(t testing.TB, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil || strings.Contains(string(encoded), "opaque_replay") || strings.Contains(string(encoded), "mock-encrypted-") || strings.Contains(string(encoded), "Synthetic private reasoning") {
		t.Fatal("public projection exposed compatible replay")
	}
}

// Hydrate only canonical message values of the owned tiny fixture, never logs
// or arbitrary tenant state. The diagnostic envelope and each value are bounded.
func compatibleCanonicalEntries(ctx context.Context, t *testing.T, session *mockSession) []conversation.SequencedEntry {
	t.Helper()
	state, err := session.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(session.id)})
	if err != nil || state == nil || proto.Size(state) > 4<<20 {
		t.Fatal("bounded compatible message state read failed")
	}
	var result []conversation.SequencedEntry
	for _, attribute := range state.GetAttributes() {
		owner, key, found := strings.Cut(attribute.GetKey(), "/")
		if !found || owner != "CurrentMessages" && owner != "ArchivedMessages" {
			continue
		}
		number, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			t.Fatal("invalid canonical map key")
		}
		if owner == "ArchivedMessages" {
			var chunk conversation.ArchivedEntryChunk
			decodeHistoryJSON(ctx, t, session.reader, string(session.id), attribute.GetValue(), &chunk)
			result = append(result, chunk.Entries...)
		} else {
			var entry conversation.Entry
			decodeHistoryJSON(ctx, t, session.reader, string(session.id), attribute.GetValue(), &entry)
			result = append(result, conversation.SequencedEntry{Sequence: conversation.Sequence(number), Entry: entry})
		}
	}
	return result
}

// Every enabled Messages profile executes actual Sandbox tools through its Project credential.
func TestMessagesProviderSessionToolsDexIntegration(t *testing.T) {
	for _, model := range []conversation.Model{conversation.ModelDeepSeekFlash, conversation.ModelMiniMaxM3, conversation.ModelMuseWonder10} {
		t.Run(string(model), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
			defer cancel()
			session := newSelectableSession(t, ctx, model, mockInput(t, "inspect-basic", 1).Content)
			snapshot := session.finishedTurn(ctx)
			requireNoFailure(t, snapshot.History)
			if snapshot.Description.ContextModel != model {
				t.Fatal("Messages profile did not own the tool loop")
			}
			_, messages := session.history(ctx)
			for _, tool := range []conversation.ToolName{conversation.ToolGlob, conversation.ToolRead, conversation.ToolGrep, conversation.ToolBash, conversation.ToolWriteTodos} {
				if toolResult(t, messages, tool, 0).Outcome != conversation.ToolOutcomeSucceeded {
					t.Fatal("Messages Sandbox tool did not succeed", tool)
				}
			}
			compatiblePublicProjection(t, snapshot)
			t.Logf("FlowID=%s model=%s completed actual Sandbox tool loop", session.id, model)
		})
	}
}
