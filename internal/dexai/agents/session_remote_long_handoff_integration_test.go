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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"google.golang.org/protobuf/proto"
)

// The lane must use agent.compactionTokens=200000. No dependency is replaced
// except the approved model: reports, reads, tools and cleanup use the existing
// real HTTP/SQL/Dex/Sandbox/Git/storage fixture. The provider checks its actual
// request as well as exact scripted assistant replay, without collecting it.
func TestSessionRemoteLongHandoffHTTPDexIntegration(t *testing.T) {
	for _, summarized := range []bool{false, true} {
		t.Run(fmt.Sprintf("pre_remote_summary_%t", summarized), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
			defer cancel()
			session, api, caller := remoteSessionAPI(ctx, t)
			if summarized {
				session.send(ctx, mockInput(t, "remote-long-summary", 1).Content)
				prelude := session.finishedTurn(ctx)
				requireNoFailure(t, prelude.History)
				if markers(prelude.History.Messages, agentloop.SegmentMarker(agentloop.SegmentReasonCompaction)) != 1 {
					t.Fatal("genuine pre-remote compaction did not complete")
				}
			}
			previous := longHandoffSegment(ctx, t, session)
			if summarized && (previous.Summary == "" || previous.Reason != agentloop.SegmentReasonCompaction) || !summarized && previous.Summary != "" {
				t.Fatal("the two fixtures do not have their distinct authoritative summary state")
			}
			if summarized && (!strings.Contains(previous.Summary, "[[scenario:remote-long-summary#1]]") || !strings.Contains(previous.Summary, "[[scenario-step:1]]") || !strings.Contains(previous.Summary, "[[scenario-arg:budget=1]]") || !strings.Contains(previous.Summary, "[[scenario-arg:task=cmd-bbbbbbbbbbbb]]")) {
				t.Fatal("genuine compaction did not establish the prior marker/argument contamination regression")
			}
			before, err := session.client.GetSnapshot(ctx, session.id)
			if err != nil || before.Agent == nil || !flowid.IsCommitSHA(before.Session.Identity.HeadCommit) {
				t.Fatal("fresh fixture lacks its canonical initial repository HEAD")
			}
			head := before.Session.Identity.HeadCommit
			started := time.Now().UTC()
			remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
			var active bridgeapi.ActivateResponse
			remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "long-handoff", ProviderThreadID: "long-handoff", HostLabel: "integration host"}}, &active, http.StatusOK)
			if active.Epoch != 1 || active.Generation != 1 {
				t.Fatal("long fixture requires exact fresh remote epoch/generation")
			}
			sequence := uint64(0)
			report := func(input bridgeapi.Report) {
				t.Helper()
				sequence++
				input.Epoch, input.Generation, input.Sequence = active.Epoch, active.Generation, sequence
				var admitted bridgeapi.ReportResponse
				remote.post(ctx, bridgeapi.PathReport, input, &admitted, http.StatusOK)
				if admitted.Reported != sequence || admitted.Duplicate {
					t.Fatal("remote fixture lost exact original report admission")
				}
			}
			// Align a real tool_use at 10n and its result at 10n+1. Later
			// retention deletes the call's entire chunk but preserves its result.
			last := before.Agent.Description.LastSequence
			callSequence := ((last+1)/10 + 1) * 10
			padding := make([]bridgeapi.Event, int(callSequence-last-1))
			for index := range padding {
				padding[index] = bridgeapi.Event{Kind: bridgeapi.EventKindImportedMessage, Role: bridgeapi.EntryRoleUser, Text: "Long handoff cutoff alignment"}
			}
			if len(padding) == 0 {
				t.Fatal("prelude unexpectedly leaves no public alignment input")
			}
			report(bridgeapi.Report{Events: padding})
			longHandoffAwaitSequence(ctx, t, session, callSequence-1)
			report(bridgeapi.Report{ToolCall: &bridgeapi.ToolCall{Name: "write", Arguments: json.RawMessage(`{"path":"raexec-long-dropped.txt","content":"Dropped unit proof\n"}`)}})
			orphan := conversation.CallID("remote-1-2")
			remote.awaitResult(ctx, active.Epoch, orphan, "Created raexec-long-dropped.txt")
			longHandoffAwaitSequence(ctx, t, session, callSequence+1)
			orphanSequence := callSequence + 1
			for first := 1; first <= 993; first += 32 {
				events := make([]bridgeapi.Event, min(32, 994-first))
				for index := range events {
					events[index] = bridgeapi.Event{Kind: bridgeapi.EventKindImportedMessage, Role: bridgeapi.EntryRoleUser, Text: fmt.Sprintf("Long remote entry %04d ", first+index) + strings.Repeat("x", 128)}
				}
				report(bridgeapi.Report{Events: events})
				longHandoffAwaitSequence(ctx, t, session, orphanSequence+conversation.Sequence(first+len(events)-1))
			}
			if sequence != 34 {
				t.Fatal("filler reports do not leave the exact scripted tail identity")
			}
			report(bridgeapi.Report{Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: mockInput(t, "remote-long-tail", 1).Content}}})
			longHandoffAwaitSequence(ctx, t, session, orphanSequence+994)
			for reply := 1; reply <= 2; reply++ {
				expected := mockmodel.RemoteReplyForTest("remote-long-tail", 1, reply)
				if expected.Text != "" || len(expected.ToolCalls) != 1 {
					t.Fatal("tail must contain exactly two independently settled tool units")
				}
				call := expected.ToolCalls[0]
				if call.ID != conversation.CallID(fmt.Sprintf("remote-1-%d", sequence+1)) {
					t.Fatal("tail report differs from the unchanged exact script")
				}
				report(bridgeapi.Report{ToolCall: &bridgeapi.ToolCall{Name: string(call.Name), Arguments: json.RawMessage(call.Arguments.String())}})
				want := "Created raexec-long-handoff.txt"
				if reply == 2 {
					want = "Long remote handoff proof"
					// A user gap preserves two chronological separate assistant units.
				}
				remote.awaitResult(ctx, active.Epoch, call.ID, want)
			}
			longHandoffAwaitSequence(ctx, t, session, orphanSequence+998)
			report(bridgeapi.Report{TurnCompleted: true})
			session.await(ctx, "long remote turn is idle", func() bool {
				current, err := session.client.GetSnapshot(ctx, session.id)
				return err == nil && current.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
			})
			retained, messages := session.history(ctx)
			if retained.Description.LastSequence-before.Agent.Description.LastSequence <= 1000 || retained.Description.FirstRetainedSequence != orphanSequence || retained.Description.LastSequence != orphanSequence+998 {
				t.Fatal("long fixture did not establish its exact authoritative retention cutoff")
			}
			longHandoffRequireUnits(t, messages, orphan, true)
			for _, item := range messages {
				if item.Message.ModelCall != nil {
					t.Fatal("retained remote history contains an unexpected platform model reply")
				}
			}
			var synced bridgeapi.SyncResponse
			remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch}, &synced, http.StatusOK)
			if synced.Dropped == nil || synced.Dropped.From != 1 || synced.Dropped.Through != uint64(orphanSequence-1) {
				t.Fatal("remote sync did not expose the exact canonical drop range")
			}
			if _, err := api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: session.identity.ProjectID, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
				t.Fatal(err)
			}
			switched, err := session.client.GetSnapshot(ctx, session.id)
			if err != nil || switched.Agent.Description.Executor.Handoff == nil || switched.Session.Identity.HeadCommit != head || switched.Agent.Description.LastSequence != retained.Description.LastSequence {
				t.Fatal("switch changed HEAD/history or omitted its deferred handoff")
			}
			handoff := switched.Agent.Description.Executor.Handoff
			if len(handoff.Owners) != 1 || handoff.Owners[0].Email != caller.GetCanonicalEmail() || handoff.From.Before(started) || handoff.To.Before(handoff.From) || handoff.To.After(time.Now().UTC()) || handoff.Reason != remoteexec.HandoffSwitched {
				t.Fatal("handoff owner, reason or bounded interval differs from authenticated authority")
			}
			notice := handoff.Notice(head)
			lines := strings.Split(notice, "\n")
			if len(lines) != 2 || lines[1] != "Only the recent part of that work is in the context above; older work is in the repository and its commits." {
				t.Fatal("approved first-line metadata/older-work notice shape changed")
			}
			summaryHash := "none"
			if summarized {
				summaryHash = longHandoffHash(previous.Summary)
			}
			content := mockInput(t, "remote-long-continuation", 1).Content
			for name, value := range map[string]string{"head": head, "notice": longHandoffHash(notice), "summary": summaryHash, "budget": "400000", "task": "cmd-cccccccccccc"} {
				content += " " + mockmodel.ArgumentMarkerForTest(name, value)
			}
			session.send(ctx, content)
			finished := session.finishedTurnAfter(ctx, retained.Description.LastSequence)
			requireNoFailure(t, finished.History)
			if !strings.Contains(finished.History.Messages[len(finished.History.Messages)-1].Message.Content, "Long remote handoff replay and platform continuation are coherent") {
				t.Fatal("strict long provider replay or actual platform continuation failed")
			}
			requireMock(t, finished.History.Messages[len(finished.History.Messages)-1], mockInputTokens)
			requireProgress(t, finished, conversation.MockProgress{Scenario: "remote-long-continuation", Input: 1})
			segment := longHandoffSegment(ctx, t, session)
			prompt := agentloop.SessionPromptFor(conversation.ModelMock)
			promptBytes := int64(len(prompt.Prompts.System))
			for _, tool := range prompt.Prompts.Tools {
				promptBytes += int64(len(tool))
			}
			if segment.Reason != agentloop.SegmentReasonExecutor || segment.Number != previous.Number+1 || segment.FirstSequence != orphanSequence+1 || segment.Summary != previous.Summary || segment.BaseBytes > 400000+int64(remoteexec.MaximumHandoffBytes)+promptBytes || finished.Description.Executor.Handoff != nil {
				t.Fatal("executor segment reassembled the dropped tool unit, lost summary, exceeded its bound or retained the consumed handoff")
			}
			_, messages = session.history(ctx)
			longHandoffRequireUnits(t, messages, orphan, false)
			if markers(messages, remoteexec.HandoffNoticeLabel) != 1 {
				t.Fatal("long continuation duplicated its canonical handoff")
			}
			if current := session.summary(ctx); current.Identity.HeadCommit != head {
				t.Fatal("long continuation replaced the original latest canonical HEAD")
			}
			actual := remoteSandboxOperator(ctx, t, session.workspace(ctx).Resources, "cd /workspace && git rev-parse HEAD && cat raexec-long-handoff.txt")
			if !strings.HasPrefix(strings.TrimSpace(actual), head+"\n") || !strings.Contains(actual, "Long remote handoff proof") {
				t.Fatal("real Git/Sandbox content differs from the original handoff source")
			}
			t.Logf("FlowID=%s remote entries=%d dropped=1..%d orphan=%s context first=%d original HEAD=%s summary=%t", session.id, retained.Description.LastSequence-before.Agent.Description.LastSequence, orphanSequence-1, orphan, segment.FirstSequence, head, summarized)
		})
	}
	t.Run("byte_budget_with_pre_remote_summary", testLongRemoteHandoffByteBudget)
}

func longHandoffHash(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}

func longHandoffAwaitSequence(ctx context.Context, t *testing.T, session *mockSession, sequence conversation.Sequence) {
	t.Helper()
	session.await(ctx, "long handoff canonical append", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && snapshot.Agent.Description.LastSequence >= sequence
	})
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || snapshot.Agent.Description.LastSequence != sequence {
		t.Fatal("fixture acquired unexpected canonical entries")
	}
}

// Only this ordinary typed Attribute is decoded. No model reply, hidden block
// or execution history is collected; canonical public reads supply all chat.
func longHandoffSegment(ctx context.Context, t *testing.T, session *mockSession) agentloop.ContextSegment {
	t.Helper()
	state, err := session.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(session.id)})
	if err != nil || state == nil || proto.Size(state) > 4<<20 {
		t.Fatal("bounded canonical context Attribute read failed")
	}
	for _, attribute := range state.GetAttributes() {
		if attribute.GetKey() == "ContextSegment" {
			var segment agentloop.ContextSegment
			decodeHistoryJSON(ctx, t, session.reader, string(session.id), attribute.GetValue(), &segment)
			if len(segment.Summary) > 8<<10 {
				t.Fatal("retained platform summary exceeds its UTF-8 byte bound")
			}
			return segment
		}
	}
	return agentloop.ContextSegment{}
}

func longHandoffRequireUnits(t *testing.T, messages []conversation.SequencedMessage, orphan conversation.CallID, requireOrphan bool) {
	t.Helper()
	uses, results := map[conversation.CallID]conversation.Sequence{}, map[conversation.CallID]conversation.Sequence{}
	for _, item := range messages {
		for _, call := range item.Message.ToolCalls {
			if _, duplicate := uses[call.ID]; duplicate {
				t.Fatal("canonical history duplicated a tool use")
			}
			uses[call.ID] = item.Sequence
		}
		if item.Message.ToolCallID != nil {
			id := *item.Message.ToolCallID
			if _, duplicate := results[id]; duplicate || item.Message.ToolExecution == nil || item.Message.ToolExecution.Outcome != conversation.ToolOutcomeSucceeded {
				t.Fatal("canonical tool result is duplicated or not successful")
			}
			results[id] = item.Sequence
		}
	}
	if uses[orphan] != 0 || requireOrphan && results[orphan] == 0 {
		t.Fatal("cutoff did not delete the old use while retaining its actual result")
	}
	write, read := conversation.CallID("remote-1-36"), conversation.CallID("remote-1-37")
	if uses[write] == 0 || results[write] != uses[write]+1 || uses[read] != results[write]+1 || results[read] != uses[read]+1 {
		t.Fatal("retained tail lost complete chronological tool-use/result units")
	}
}

// This independent fixture leaves the two no-cutoff cases unchanged.
func testLongRemoteHandoffByteBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	session.send(ctx, mockInput(t, "remote-long-summary", 1).Content)
	prelude := session.finishedTurn(ctx)
	requireNoFailure(t, prelude.History)
	if markers(prelude.History.Messages, agentloop.SegmentMarker(agentloop.SegmentReasonCompaction)) != 1 {
		t.Fatal("genuine pre-remote compaction did not complete")
	}
	previous := longHandoffSegment(ctx, t, session)
	if previous.Summary == "" || previous.Reason != agentloop.SegmentReasonCompaction {
		t.Fatal("byte fixture lacks its genuine authoritative pre-remote summary")
	}
	if !strings.Contains(previous.Summary, "[[scenario:remote-long-summary#1]]") || !strings.Contains(previous.Summary, "[[scenario-step:1]]") || !strings.Contains(previous.Summary, "[[scenario-arg:budget=1]]") || !strings.Contains(previous.Summary, "[[scenario-arg:task=cmd-bbbbbbbbbbbb]]") {
		t.Fatal("genuine compaction did not establish the prior marker/argument contamination regression")
	}
	before, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || before.Agent == nil || !flowid.IsCommitSHA(before.Session.Identity.HeadCommit) {
		t.Fatal("fresh fixture lacks its canonical initial repository HEAD")
	}
	head := before.Session.Identity.HeadCommit
	started := time.Now().UTC()
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "long-handoff", ProviderThreadID: "long-handoff", HostLabel: "integration host"}}, &active, http.StatusOK)
	if active.Epoch != 1 || active.Generation != 1 {
		t.Fatal("long fixture requires exact fresh remote epoch/generation")
	}
	sequence := uint64(0)
	report := func(input bridgeapi.Report) {
		t.Helper()
		sequence++
		input.Epoch, input.Generation, input.Sequence = active.Epoch, active.Generation, sequence
		encoded, err := json.Marshal(input)
		if err != nil || len(encoded) > 512<<10 || input.Validate() != nil {
			t.Fatal("byte fixture report exceeds the real endpoint admission limits")
		}
		var admitted bridgeapi.ReportResponse
		remote.post(ctx, bridgeapi.PathReport, input, &admitted, http.StatusOK)
		if admitted.Reported != sequence || admitted.Duplicate {
			t.Fatal("remote fixture lost exact original report admission")
		}
	}
	// Align a real tool_use at 10n and its result at 10n+1. Later
	// retention deletes the call's entire chunk but preserves its result.
	last := before.Agent.Description.LastSequence
	callSequence := ((last+1)/10 + 2) * 10
	padding := make([]bridgeapi.Event, int(callSequence-last-1))
	for index := range padding {
		padding[index] = bridgeapi.Event{Kind: bridgeapi.EventKindImportedMessage, Role: bridgeapi.EntryRoleUser, Text: "Long handoff cutoff alignment"}
	}
	if len(padding)+993 <= 1000 || len(padding) > 32 {
		t.Fatal("byte fixture must import more than 1000 real entries in bounded reports")
	}
	report(bridgeapi.Report{Events: padding})
	longHandoffAwaitSequence(ctx, t, session, callSequence-1)
	report(bridgeapi.Report{ToolCall: &bridgeapi.ToolCall{Name: "write", Arguments: json.RawMessage(`{"path":"raexec-long-dropped.txt","content":"Dropped unit proof\n"}`)}})
	orphan := conversation.CallID("remote-1-2")
	remote.awaitResult(ctx, active.Epoch, orphan, "Created raexec-long-dropped.txt")
	longHandoffAwaitSequence(ctx, t, session, callSequence+1)
	orphanSequence := callSequence + 1
	for first := 1; first <= 993; first += 32 {
		events := make([]bridgeapi.Event, min(32, 994-first))
		for index := range events {
			events[index] = bridgeapi.Event{Kind: bridgeapi.EventKindImportedMessage, Role: bridgeapi.EntryRoleUser, Text: mockmodel.LongHandoffByteFillerForTest(first + index)}
		}
		report(bridgeapi.Report{Events: events})
		longHandoffAwaitSequence(ctx, t, session, orphanSequence+conversation.Sequence(first+len(events)-1))
	}
	if sequence != 34 {
		t.Fatal("filler reports do not leave the exact scripted tail identity")
	}
	report(bridgeapi.Report{Events: []bridgeapi.Event{{Kind: bridgeapi.EventKindUserMessage, Text: mockInput(t, "remote-long-tail", 1).Content}}})
	longHandoffAwaitSequence(ctx, t, session, orphanSequence+994)
	for reply := 1; reply <= 2; reply++ {
		expected := mockmodel.RemoteReplyForTest("remote-long-tail", 1, reply)
		if expected.Text != "" || len(expected.ToolCalls) != 1 {
			t.Fatal("tail must contain exactly two independently settled tool units")
		}
		call := expected.ToolCalls[0]
		if call.ID != conversation.CallID(fmt.Sprintf("remote-1-%d", sequence+1)) {
			t.Fatal("tail report differs from the unchanged exact script")
		}
		report(bridgeapi.Report{ToolCall: &bridgeapi.ToolCall{Name: string(call.Name), Arguments: json.RawMessage(call.Arguments.String())}})
		want := "Created raexec-long-handoff.txt"
		if reply == 2 {
			want = "Long remote handoff proof"
			// A user gap preserves two chronological separate assistant units.
		}
		remote.awaitResult(ctx, active.Epoch, call.ID, want)
	}
	longHandoffAwaitSequence(ctx, t, session, orphanSequence+998)
	report(bridgeapi.Report{TurnCompleted: true})
	session.await(ctx, "long remote turn is idle", func() bool {
		current, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && current.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	retained, messages := session.history(ctx)
	if retained.Description.LastSequence-before.Agent.Description.LastSequence <= 1000 || retained.Description.FirstRetainedSequence != orphanSequence || retained.Description.LastSequence != orphanSequence+998 {
		t.Fatal("long fixture did not establish its exact authoritative retention cutoff")
	}
	longHandoffRequireUnits(t, messages, orphan, true)
	for _, item := range messages {
		if item.Message.ModelCall != nil {
			t.Fatal("retained remote history contains an unexpected platform model reply")
		}
	}
	candidate := longHandoffCanonicalEntries(ctx, t, session, orphanSequence+1, retained.Description.LastSequence)
	var candidateBlocks, fillerBlocks []json.RawMessage
	candidateBytes := int64(0)
	for index, item := range candidate {
		candidateBytes += item.Entry.Bytes()
		candidateBlocks = append(candidateBlocks, item.Entry.Blocks...)
		if index < 993 {
			public, visible := item.Entry.PublicMessage()
			if !visible || item.Entry.Role != conversation.EntryRoleUser || !item.Entry.Meta.Imported || public.Content != mockmodel.LongHandoffByteFillerForTest(index+1) {
				t.Fatal("canonical imported candidate differs from the original bounded fixture")
			}
			fillerBlocks = append(fillerBlocks, item.Entry.Blocks...)
		}
	}
	if len(candidate) != 998 || candidateBytes <= 400000 {
		t.Fatal("canonical valid retained candidate does not independently exceed the byte budget")
	}
	var synced bridgeapi.SyncResponse
	remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch}, &synced, http.StatusOK)
	if synced.Dropped == nil || synced.Dropped.From != 1 || synced.Dropped.Through != uint64(orphanSequence-1) {
		t.Fatal("remote sync did not expose the exact canonical drop range")
	}
	if _, err := api.SetSessionExecutor(ctx, &pb.SetSessionExecutorRequest{ProjectId: session.identity.ProjectID, BasisEpoch: active.Epoch, Caller: caller}); err != nil {
		t.Fatal(err)
	}
	switched, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || switched.Agent.Description.Executor.Handoff == nil || switched.Session.Identity.HeadCommit != head || switched.Agent.Description.LastSequence != retained.Description.LastSequence {
		t.Fatal("switch changed HEAD/history or omitted its deferred handoff")
	}
	handoff := switched.Agent.Description.Executor.Handoff
	if len(handoff.Owners) != 1 || handoff.Owners[0].Email != caller.GetCanonicalEmail() || handoff.From.Before(started) || handoff.To.Before(handoff.From) || handoff.To.After(time.Now().UTC()) || handoff.Reason != remoteexec.HandoffSwitched {
		t.Fatal("handoff owner, reason or bounded interval differs from authenticated authority")
	}
	notice := handoff.Notice(head)
	lines := strings.Split(notice, "\n")
	if len(lines) != 2 || lines[1] != "Only the recent part of that work is in the context above; older work is in the repository and its commits." {
		t.Fatal("approved first-line metadata/older-work notice shape changed")
	}
	summaryHash := longHandoffHash(previous.Summary)
	content := mockInput(t, "remote-long-continuation", 1).Content
	arguments := map[string]string{"head": head, "notice": longHandoffHash(notice), "summary": summaryHash, "budget": "400000", "task": "cmd-cccccccccccc", "bytecase": "1", "candidate_bytes": strconv.FormatInt(candidateBytes, 10), "candidate_digest": longHandoffBlockDigest(candidateBlocks), "candidate_count": strconv.Itoa(len(candidateBlocks)), "predecessors_digest": longHandoffBlockDigest(fillerBlocks), "predecessors_count": strconv.Itoa(len(fillerBlocks))}
	for _, name := range []string{"budget", "bytecase", "candidate_bytes", "candidate_count", "candidate_digest", "head", "notice", "predecessors_count", "predecessors_digest", "summary", "task"} {
		content += " " + mockmodel.ArgumentMarkerForTest(name, arguments[name])
	}
	session.send(ctx, content)
	finished := session.finishedTurnAfter(ctx, retained.Description.LastSequence)
	requireNoFailure(t, finished.History)
	if !strings.Contains(finished.History.Messages[len(finished.History.Messages)-1].Message.Content, "Long remote handoff replay and platform continuation are coherent") {
		t.Fatal("strict long provider replay or actual platform continuation failed")
	}
	requireMock(t, finished.History.Messages[len(finished.History.Messages)-1], mockInputTokens)
	requireProgress(t, finished, conversation.MockProgress{Scenario: "remote-long-continuation", Input: 1})
	segment := longHandoffSegment(ctx, t, session)
	prompt := agentloop.SessionPromptFor(conversation.ModelMock)
	promptBytes := int64(len(prompt.Prompts.System))
	for _, tool := range prompt.Prompts.Tools {
		promptBytes += int64(len(tool))
	}
	if segment.Reason != agentloop.SegmentReasonExecutor || segment.Number != previous.Number+1 || segment.FirstSequence <= orphanSequence+1 || segment.FirstSequence <= retained.Description.FirstRetainedSequence || segment.FirstSequence > orphanSequence+993 || segment.Summary != previous.Summary || segment.BaseBytes > 400000+int64(remoteexec.MaximumHandoffBytes)+promptBytes || finished.Description.Executor.Handoff != nil {
		t.Fatal("executor segment reassembled the dropped tool unit, lost summary, exceeded its bound or retained the consumed handoff")
	}
	currentEntries := longHandoffCanonicalEntries(ctx, t, session, retained.Description.LastSequence+1, retained.Description.LastSequence+1)
	current := currentEntries[0].Entry
	expectedCurrent := (conversation.UserMessage{Content: content}).Entry() // Matches mockSession.send exactly.
	if len(current.Blocks) != 1 || string(current.Blocks[0]) != string(expectedCurrent.Blocks[0]) || current.Meta.Actor != nil || current.Meta.Via != nil {
		t.Fatal("actual queued current input differs from its original immutable encoding")
	}
	chosenBytes := current.Bytes() // Stored before openExecutorSegment selects the suffix.
	for _, item := range candidate {
		if item.Sequence >= segment.FirstSequence {
			chosenBytes += item.Entry.Bytes()
		}
	}
	predecessor := candidate[int(segment.FirstSequence-orphanSequence)-2].Entry
	if chosenBytes > 400000 || chosenBytes+predecessor.Bytes() <= 400000 {
		t.Fatal("actual context suffix is over budget or not maximal under immutable entry encoding")
	}
	_, messages = session.history(ctx)
	longHandoffRequireUnits(t, messages, orphan, false)
	if markers(messages, remoteexec.HandoffNoticeLabel) != 1 {
		t.Fatal("long continuation duplicated its canonical handoff")
	}
	if current := session.summary(ctx); current.Identity.HeadCommit != head {
		t.Fatal("long continuation replaced the original latest canonical HEAD")
	}
	actual := remoteSandboxOperator(ctx, t, session.workspace(ctx).Resources, "cd /workspace && git rev-parse HEAD && cat raexec-long-handoff.txt")
	if !strings.HasPrefix(strings.TrimSpace(actual), head+"\n") || !strings.Contains(actual, "Long remote handoff proof") {
		t.Fatal("real Git/Sandbox content differs from the original handoff source")
	}
	t.Logf("FlowID=%s imported=%d candidate bytes=%d chosen bytes=%d with predecessor=%d retention floor=%d context first=%d original HEAD=%s", session.id, len(padding)+993, candidateBytes, chosenBytes, chosenBytes+predecessor.Bytes(), retained.Description.FirstRetainedSequence, segment.FirstSequence, head)
}

// Read only the retained public remote range; hydrate one exact ten-entry
// chunk or CurrentMessages entry at a time, never a platform reply or history.
func longHandoffCanonicalEntries(ctx context.Context, t *testing.T, session *mockSession, first, last conversation.Sequence) []conversation.SequencedEntry {
	t.Helper()
	state, err := session.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(session.id)})
	if err != nil || state == nil || proto.Size(state) > 4<<20 {
		t.Fatal("bounded canonical remote entry state read failed")
	}
	entries := map[conversation.Sequence]conversation.Entry{}
	for _, attribute := range state.GetAttributes() {
		owner, key, found := strings.Cut(attribute.GetKey(), "/")
		if !found || owner != "CurrentMessages" && owner != "ArchivedMessages" {
			continue
		}
		sequence, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			t.Fatal("canonical entry map has a malformed instance key")
		}
		start := conversation.Sequence(sequence)
		end := start
		if owner == "ArchivedMessages" {
			end += agentloop.ArchiveMessageChunkSize - 1
		}
		if end < first || start > last {
			continue
		}
		var selected []conversation.SequencedEntry
		if owner == "ArchivedMessages" {
			var chunk conversation.ArchivedEntryChunk
			decodeHistoryJSON(ctx, t, session.reader, string(session.id), attribute.GetValue(), &chunk)
			if len(chunk.Entries) != int(agentloop.ArchiveMessageChunkSize) {
				t.Fatal("canonical archive chunk is not complete")
			}
			selected = chunk.Entries
		} else {
			var entry conversation.Entry
			decodeHistoryJSON(ctx, t, session.reader, string(session.id), attribute.GetValue(), &entry)
			selected = []conversation.SequencedEntry{{Sequence: start, Entry: entry}}
		}
		encoded, err := json.Marshal(selected)
		if err != nil || len(encoded) > 64<<10 {
			t.Fatal("individual canonical entry load exceeded its small fixture bound")
		}
		for _, item := range selected {
			if item.Sequence < first || item.Sequence > last {
				continue
			}
			if _, duplicate := entries[item.Sequence]; duplicate || item.Entry.Meta.ModelCall != nil {
				t.Fatal("canonical remote range duplicates an entry or contains a platform reply")
			}
			for _, raw := range item.Entry.Blocks {
				var header struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(raw, &header) != nil || header.Type != "text" && header.Type != "tool_use" && header.Type != "tool_result" {
					t.Fatal("canonical remote range contains a non-public block")
				}
			}
			entries[item.Sequence] = item.Entry
		}
	}
	result := make([]conversation.SequencedEntry, 0, int(last-first+1))
	for sequence := first; sequence <= last; sequence++ {
		entry, found := entries[sequence]
		if !found {
			t.Fatal("canonical remote candidate has a missing interior entry")
		}
		result = append(result, conversation.SequencedEntry{Sequence: sequence, Entry: entry})
	}
	return result
}

// The newline delimits immutable JSON blocks; JSON strings cannot contain an
// unescaped newline. The provider uses exactly this digest and byte encoding.
func longHandoffBlockDigest(blocks []json.RawMessage) string {
	digest := sha256.New()
	for _, raw := range blocks {
		digest.Write(raw)
		digest.Write([]byte{'\n'})
	}
	return hex.EncodeToString(digest.Sum(nil))
}
