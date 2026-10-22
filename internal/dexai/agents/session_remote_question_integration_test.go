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
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Both branches use a real remote HTTP question, the actual Session answer RPC
// and an actual newly paired connection's activation. Nothing forces pending state.
func TestSessionRemoteQuestionTakeoverHTTPDexIntegration(t *testing.T) {
	for _, answered := range []bool{false, true} {
		t.Run(fmt.Sprintf("answer_before_takeover_%t", answered), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
			defer cancel()
			session, api, caller := remoteSessionAPI(ctx, t)
			remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
			binding := bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "question-takeover", HostLabel: "integration host"}}
			var active bridgeapi.ActivateResponse
			remote.post(ctx, bridgeapi.PathActivate, binding, &active, http.StatusOK)
			arguments := json.RawMessage(`{"questions":[{"id":"audience","header":"Audience","question":"Which audience should this serve?","options":[{"label":"Customers","description":"Serve external customers."},{"label":"Staff","description":"Serve internal staff."}]}]}`)
			var admitted bridgeapi.ReportResponse
			remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolRequestUserInput), Arguments: arguments}}, &admitted, http.StatusOK)
			callID := conversation.CallID(fmt.Sprintf("remote-%d-1", active.Epoch))
			session.await(ctx, "remote question wait", func() bool {
				snapshot, err := session.client.GetSnapshot(ctx, session.id)
				return err == nil && snapshot.Agent.Description.PendingUserInput != nil && snapshot.Agent.Description.PendingUserInput.CallID == callID
			})
			var projected bridgeapi.SyncResponse
			remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch}, &projected, http.StatusOK)
			if projected.PendingQuestion == nil || projected.PendingQuestion.CallID != string(callID) || !strings.Contains(projected.PendingQuestion.Text, "audience") {
				t.Fatal("sync did not project the canonical remote question")
			}
			next := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
			beforeTakeover, err := session.client.GetSnapshot(ctx, session.id)
			if err != nil || beforeTakeover.Agent == nil {
				t.Fatal("question takeover lacks its pre-takeover snapshot boundary")
			}
			if answered {
				err := session.client.AnswerQuestions(ctx, session.id, agentloop.AnswerQuestionsRequest{Actor: &conversation.Actor{AccountID: caller.GetAccountId(), Email: caller.GetCanonicalEmail()}, CallID: callID, Answers: []agentloop.UserInputAnswer{{QuestionID: "audience", Answer: "Customers"}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			var takeover bridgeapi.ActivateResponse
			next.post(ctx, bridgeapi.PathActivate, binding, &takeover, http.StatusOK)
			if takeover.Epoch != active.Epoch+1 || takeover.Generation <= active.Generation {
				t.Fatal("question takeover did not advance its fences")
			}
			settlement, stopSettlement := context.WithTimeout(ctx, 30*time.Second)
			defer stopSettlement()
			session.await(settlement, "question takeover settlement", func() bool {
				snapshot, err := session.client.GetSnapshot(settlement, session.id)
				if err != nil || snapshot.Agent.Description.PendingUserInput != nil {
					return false
				}
				answers, markers := 0, 0
				for _, entry := range snapshot.Agent.History.Messages {
					if entry.Message.AnsweredInputCallID != nil && *entry.Message.AnsweredInputCallID == callID {
						answers++
					}
					if entry.Sequence > beforeTakeover.Agent.Description.LastSequence && entry.Message.Role == conversation.MessageRoleSystem && entry.Message.Content == "Input interrupted by takeover" {
						markers++
					}
				}
				if answered {
					return answers == 1 && markers == 0
				}
				return answers == 0 && markers == 1
			})
			var refusal bridgeapi.ErrorBody
			remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 2, TurnCompleted: true}, &refusal, http.StatusConflict)
			if refusal.Code != bridgeapi.ErrorExecutorChanged {
				t.Fatal("replaced question owner could still report")
			}
		})
	}
}

// The Session keeps the question answer in its canonical input projection
// after the remote agent's asking turn ended, as in the 9oj2 incident.
func TestSessionRemoteQuestionAfterTurnCompletedHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	binding := bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration",
		ProviderThreadID: "completed-question", HostLabel: "integration host"}}
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, binding, &active, http.StatusOK)
	arguments := json.RawMessage(`{"questions":[{"id":"github","header":"GitHub signals","question":"Monitor GitHub now?","options":[{"label":"No","description":"Defer GitHub collection."},{"label":"Yes","description":"Monitor selected accounts."}]}]}`)
	var admitted bridgeapi.ReportResponse
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 1,
		ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolRequestUserInput), Arguments: arguments}}, &admitted, http.StatusOK)
	callID := conversation.CallID(fmt.Sprintf("remote-%d-1", active.Epoch))
	session.await(ctx, "question from completed remote turn", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		return err == nil && snapshot.Agent.Description.PendingUserInput != nil && snapshot.Agent.Description.PendingUserInput.CallID == callID
	})
	remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: 2,
		TurnCompleted: true}, &admitted, http.StatusOK)
	session.await(ctx, "remote turn completion before answer", func() bool {
		var sync bridgeapi.SyncResponse
		remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, Context: true}, &sync, http.StatusOK)
		return sync.SettledThrough >= 2
	})
	answer := "No GitHub collection in this phase"
	if err := session.client.AnswerQuestions(ctx, session.id, agentloop.AnswerQuestionsRequest{
		Actor: &conversation.Actor{AccountID: caller.GetAccountId(), Email: caller.GetCanonicalEmail()}, CallID: callID,
		Answers: []agentloop.UserInputAnswer{{QuestionID: "github", Answer: answer}},
	}); err != nil {
		t.Fatal(err)
	}
	session.await(ctx, "answer after completed remote turn", func() bool {
		var sync bridgeapi.SyncResponse
		remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: active.Epoch, After: active.LastSequence, Context: true}, &sync, http.StatusOK)
		for _, entry := range sync.Entries {
			if entry.AnsweredInputCallID == string(callID) && strings.Contains(entry.Text, answer) && entry.Via == "" && entry.Role == bridgeapi.EntryRoleUser && entry.Kind == bridgeapi.EntryKindUserMessage {
				return true
			}
		}
		return false
	})
}
