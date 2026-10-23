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
	"sync"
	"testing"

	"github.com/google/uuid"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Real concurrent gRPC calls race the real Session queue consumption. A retry
// after consumption must recognize history, even if its payload has changed.
func TestSessionSameKeyMessageAdmissionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{CodexVersion: "integration", ProviderThreadID: "same-key-admission", HostLabel: "integration host"}}, &active, http.StatusOK)
	connection, err := grpc.NewClient(dexflowtest.Setting(t, "SUPERVERSE_TEST_API_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := pb.NewAgentServiceClient(connection)
	sequence := uint64(0)
	for _, concurrency := range []int{1, 12} {
		t.Run(fmt.Sprintf("concurrent_%d", concurrency), func(t *testing.T) {
			id, text := uuid.NewString(), fmt.Sprintf("Admit exactly once with %d callers", concurrency)
			request := &pb.SendMessageRequest{Caller: caller, ProjectId: session.identity.ProjectID, MessageId: id, Text: text}
			start := make(chan struct{})
			results := make(chan error, concurrency)
			var group sync.WaitGroup
			for range concurrency {
				group.Go(func() { <-start; _, err := client.SendMessage(ctx, request); results <- err })
			}
			close(start)
			group.Wait()
			close(results)
			for err := range results {
				if err != nil {
					t.Fatal(err)
				}
			}
			session.await(ctx, "one consumed same-key message", func() bool {
				snapshot, err := session.client.GetSnapshot(ctx, session.id)
				if err != nil {
					return false
				}
				count := 0
				for _, entry := range snapshot.Agent.History.Messages {
					if entry.Message.Role == conversation.MessageRoleUser && entry.Message.Content == text {
						count++
					}
				}
				if count > 1 {
					t.Fatalf("same key produced %d history messages", count)
				}
				return count == 1 && len(snapshot.Agent.Queued) == 0 && len(snapshot.Agent.Steered) == 0
			})
			before, err := session.client.GetSnapshot(ctx, session.id)
			if err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if _, err := client.SendMessage(ctx, &pb.SendMessageRequest{Caller: caller, ProjectId: session.identity.ProjectID, MessageId: id, Text: "A retry cannot replace admitted content"}); err != nil {
					t.Fatal(err)
				}
			}
			after, err := session.client.GetSnapshot(ctx, session.id)
			if err != nil {
				t.Fatal(err)
			}
			if after.Agent.Description.LastSequence != before.Agent.Description.LastSequence || len(after.Agent.Queued) != 0 || len(after.Agent.Steered) != 0 {
				t.Fatal("consumed same-key retry admitted another message")
			}
			original, replaced := 0, 0
			for _, entry := range after.Agent.History.Messages {
				if entry.Message.Content == text {
					original++
				}
				if entry.Message.Content == "A retry cannot replace admitted content" {
					replaced++
				}
			}
			if original != 1 || replaced != 0 {
				t.Fatal("same-key retry replaced the admitted message")
			}
			sequence++
			var completed bridgeapi.ReportResponse
			remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence, TurnCompleted: true}, &completed, http.StatusOK)
			session.await(ctx, "remote turn completed before next input", func() bool {
				snapshot, err := session.client.GetSnapshot(ctx, session.id)
				return err == nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage
			})
		})
	}
	t.Run("pending_queues_and_steering", func(t *testing.T) {
		var admitted bridgeapi.ReportResponse
		remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: active.Epoch, Generation: active.Generation, Sequence: sequence + 1, ToolCall: &bridgeapi.ToolCall{Name: string(conversation.ToolBash), Arguments: json.RawMessage(`{"command":"sleep 20","description":"Hold real command while admission retries and steering run","timeout_seconds":60}`)}}, &admitted, http.StatusOK)
		session.await(ctx, "real command holds input consumption", func() bool {
			snapshot, err := session.client.GetSnapshot(ctx, session.id)
			return err == nil && snapshot.Agent.Description.Status == agentloop.AgentStatusExecutingTool
		})
		id := uuid.NewString()
		request := &pb.SendMessageRequest{Caller: caller, ProjectId: session.identity.ProjectID, MessageId: id, Text: "One key through queued and steered admission"}
		send := func() {
			t.Helper()
			if _, err := client.SendMessage(ctx, request); err != nil {
				t.Fatal(err)
			}
		}
		send()
		send()
		queued, err := session.client.GetSnapshot(ctx, session.id)
		if err != nil || len(queued.Agent.Queued) != 1 || len(queued.Agent.Steered) != 0 {
			t.Fatal("same-key queued retry was not exactly one admission", err)
		}
		if _, err := client.SteerQueuedMessage(ctx, &pb.SteerQueuedMessageRequest{Caller: caller, ProjectId: session.identity.ProjectID, MessageId: id}); err != nil {
			t.Fatal(err)
		}
		send()
		send()
		steered, err := session.client.GetSnapshot(ctx, session.id)
		if err != nil || len(steered.Agent.Queued) != 0 || len(steered.Agent.Steered) != 1 {
			t.Fatal("same-key steered retry admitted a second message", err)
		}
		session.await(ctx, "steered key reaches current history", func() bool {
			snapshot, err := session.client.GetSnapshot(ctx, session.id)
			if err != nil {
				return false
			}
			count := 0
			for _, entry := range snapshot.Agent.History.Messages {
				if entry.Message.Content == request.Text {
					count++
				}
			}
			if count > 1 {
				t.Fatalf("steered key produced %d messages", count)
			}
			return count == 1 && len(snapshot.Agent.Steered) == 0 && len(snapshot.Agent.Queued) == 0
		})
		send()
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		if err != nil || len(snapshot.Agent.Steered) != 0 || len(snapshot.Agent.Queued) != 0 {
			t.Fatal("consumed steered key was admitted twice", err)
		}
	})

}
