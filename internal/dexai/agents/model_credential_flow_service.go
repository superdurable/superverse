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
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
)

// ModelCredentialFlowService owns the only cross-Flow credential mutation path.
type ModelCredentialFlowService struct {
	Sessions  *sessionagent.SessionAgentFlowClient
	Subagents *subagent.SubagentFlowClient
	HTTP      *http.Client
	Logger    *slog.Logger
	// ValidationBaseURL is empty in production; real-dependency tests use the mock model endpoint.
	ValidationBaseURL string
}

// Mutate reserves a writer, changes the hard-delete value once and confirms exact readback.
// An ambiguous mutation keeps the provider disabled until the same operation reconciles.
func (s *ModelCredentialFlowService) Mutate(ctx context.Context, flowID agentloop.FlowID, input modelcredential.Mutation, key string) (modelcredential.Info, error) {
	current, err := s.Sessions.ReadModelCredentialInfo(ctx, flowID, input.Provider)
	if err != nil {
		return modelcredential.Info{}, err
	}
	if current.OperationID != input.OperationID && current.Revision != input.ExpectedRevision {
		return current, modelcredential.ErrRevision
	}
	if !input.Delete && current.OperationID != input.OperationID {
		if err := modelprovider.ValidateCredential(ctx, s.HTTP, key, conversation.ModelHaiku45, s.ValidationBaseURL, s.Logger); err != nil {
			return current, err
		}
	}
	admission, err := s.Sessions.ReserveModelCredential(ctx, flowID, input)
	if err != nil {
		return current, err
	}
	if admission.Info.Status != modelcredential.StatusPending {
		if input.Delete {
			key, err := s.Sessions.ReadModelCredential(ctx, flowID, input.Provider)
			if err != nil {
				return admission.Info, err
			}
			if key != nil {
				return admission.Info, modelcredential.ErrPending
			}
		}
		return admission.Info, nil
	}
	expected := (*modelcredential.Envelope)(nil)
	if !input.Delete {
		expected = &modelcredential.Envelope{Revision: admission.Info.Revision, Key: key}
	}
	input.WriteAttempt = admission.Info.WriteAttempt
	if admission.Fresh {
		// This write is deliberately never replayed after a lost response.
		writeErr := s.Sessions.WriteModelCredential(ctx, flowID, input, expected)
		actual, readErr := s.Sessions.ReadModelCredential(ctx, flowID, input.Provider)
		if readErr != nil {
			return admission.Info, readErr
		}
		if !sameCredential(actual, expected) {
			if writeErr != nil {
				return admission.Info, writeErr
			}
			return admission.Info, modelcredential.ErrPending
		}
	} else {
		actual, err := s.Sessions.ReadModelCredential(ctx, flowID, input.Provider)
		if err != nil {
			return admission.Info, err
		}
		if !input.Delete && (actual == nil || actual.Revision != admission.Info.Revision || actual.Key == "") {
			input.Cancel = true
			canceled, cancelErr := s.Sessions.FinalizeModelCredential(ctx, flowID, input)
			if cancelErr != nil {
				return admission.Info, cancelErr
			}
			return canceled, modelcredential.ErrCanceled
		}
		if input.Delete {
			input.RetryDelete = true
			retry, reserveErr := s.Sessions.ReserveModelCredential(ctx, flowID, input)
			if reserveErr != nil {
				return admission.Info, reserveErr
			}
			input.WriteAttempt = retry.Info.WriteAttempt
			var writeErr error
			if actual != nil {
				writeErr = s.Sessions.WriteModelCredential(ctx, flowID, input, nil)
			}
			remaining, readErr := s.Sessions.ReadModelCredential(ctx, flowID, input.Provider)
			if readErr != nil {
				return admission.Info, readErr
			}
			if remaining != nil {
				if writeErr != nil {
					return admission.Info, writeErr
				}
				return admission.Info, modelcredential.ErrPending
			}
		}
	}
	snapshot, err := s.Sessions.GetSnapshot(ctx, flowID)
	if err != nil {
		return admission.Info, err
	}
	holders := append([]modelcredential.Holder{}, admission.Info.Holders...)
	if snapshot.Agent != nil {
		for _, record := range snapshot.Agent.Description.Subagents {
			if !record.Status.Finished() {
				id := agenttasks.SubagentIdentity{ProjectID: snapshot.Session.Identity.ProjectID, AgentID: record.AgentID}
				if err = s.Subagents.StopForCredentialChange(ctx, id); err != nil {
					return admission.Info, err
				}
			}
			holders = append(holders, modelcredential.Holder{AgentID: record.AgentID})
		}
	}
	for _, holder := range holders {
		id := agenttasks.SubagentIdentity{ProjectID: snapshot.Session.Identity.ProjectID, AgentID: holder.AgentID}
		if err = s.clearSubagentCredential(ctx, id, fmt.Sprintf("%s/child-%s/retry-%d", input.OperationID, holder.AgentID, input.WriteAttempt)); err != nil {
			return admission.Info, err
		}
	}
	if snapshot.Agent != nil && snapshot.Agent.Description.Status != agentloop.AgentStatusWaitingForMessage && snapshot.Agent.Description.Status != agentloop.AgentStatusInitializing {
		if err = s.Sessions.InterruptAgent(ctx, flowID, agentloop.InterruptAgentRequest{Actor: &input.Actor, Turn: snapshot.Agent.Description.Turn, PlatformOnly: true}); err != nil {
			return admission.Info, err
		}
	}
	return s.Sessions.FinalizeModelCredential(ctx, flowID, input)
}

func sameCredential(left, right *modelcredential.Envelope) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// startSubagent copies only while the calling Session Step holds its provider instance lock.
// The hard-delete ModelKey Attribute carries the secret; no Start/RPC payload carries it.
func (s *ModelCredentialFlowService) startSubagent(ctx context.Context, start agenttasks.SubagentStart, key modelcredential.Envelope, attempt int32) error {
	if err := s.Subagents.Start(ctx, start); err != nil {
		return err
	}
	if start.Model == conversation.ModelMock {
		return nil
	}
	revision, running, err := s.Subagents.CredentialTarget(ctx, start.Identity)
	if err != nil {
		return err
	}
	if !running {
		return s.clearSubagentCredential(ctx, start.Identity, fmt.Sprintf("%s/copy-%d", start.RequestID(), attempt))
	}
	if revision != key.Revision {
		if err := s.Subagents.StopForCredentialChange(ctx, start.Identity); err != nil {
			return err
		}
		if err := s.clearSubagentCredential(ctx, start.Identity, fmt.Sprintf("%s/copy-%d", start.RequestID(), attempt)); err != nil {
			return err
		}
		return modelcredential.ErrCopyStopped
	}
	existing, err := s.Subagents.ReadModelCredential(ctx, start.Identity)
	if err != nil {
		return err
	}
	if !sameCredential(existing, &key) {
		if attempt > 1 {
			if err := s.Subagents.StopForCredentialChange(ctx, start.Identity); err != nil {
				return err
			}
			if err := s.clearSubagentCredential(ctx, start.Identity, fmt.Sprintf("%s/copy-%d", start.RequestID(), attempt)); err != nil {
				return err
			}
			return modelcredential.ErrCopyStopped
		}
		writeErr := s.Subagents.WriteModelCredential(ctx, start.Identity, start.RequestID(), &key)
		existing, err = s.Subagents.ReadModelCredential(ctx, start.Identity)
		if err != nil {
			return err
		}
		if !sameCredential(existing, &key) {
			if writeErr != nil {
				return writeErr
			}
			return modelcredential.ErrPending
		}
	}
	_, running, err = s.Subagents.CredentialTarget(ctx, start.Identity)
	if err != nil {
		return err
	}
	if !running {
		return s.clearSubagentCredential(ctx, start.Identity, fmt.Sprintf("%s/copy-%d", start.RequestID(), attempt))
	}
	return nil
}

// Close releases the provider transport; the bootstrap owns the Service.
func (s *ModelCredentialFlowService) Close() error { s.HTTP.CloseIdleConnections(); return nil }

// cleanupProjectCredential deletes the current key before the Session completes.
// An unresolved earlier writer remains disabled and keeps Project cleanup retryable.
func (s *ModelCredentialFlowService) cleanupProjectCredential(ctx context.Context, project string, abnormal bool) error {
	flowID := agentloop.SessionFlowID(project)
	info, err := s.Sessions.ReadModelCredentialInfo(ctx, flowID, modelcredential.Anthropic)
	if err != nil {
		return err
	}
	if abnormal {
		return s.cleanupStoppedProjectCredential(ctx, project, info)
	}
	if info.Status == modelcredential.StatusPending {
		info, err = s.Mutate(ctx, flowID, modelcredential.Mutation{Provider: modelcredential.Anthropic, OperationID: info.OperationID, ExpectedRevision: info.OperationRevision, Delete: info.Delete, Actor: info.SetBy}, "")
		if err != nil && !errors.Is(err, modelcredential.ErrCanceled) {
			return err
		}
	}

	if info.OperationID == projectCredentialCleanupID(project) && info.Status == modelcredential.StatusRevoked {
		return s.cleanupStoppedProjectCredential(ctx, project, info)
	}
	_, err = s.Mutate(ctx, flowID, modelcredential.Mutation{Provider: modelcredential.Anthropic, OperationID: projectCredentialCleanupID(project), ExpectedRevision: info.Revision, Delete: true, Actor: conversation.Actor{}, Reason: modelcredential.ProjectDeletionReason}, "")
	return err
}

func (s *ModelCredentialFlowService) clearSubagentCredential(ctx context.Context, id agenttasks.SubagentIdentity, operation string) error {
	key, err := s.Subagents.ReadModelCredential(ctx, id)
	if err != nil {
		return err
	}
	if key == nil {
		return nil
	}
	writeErr := s.Subagents.WriteModelCredential(ctx, id, operation+"/clear", nil)
	key, err = s.Subagents.ReadModelCredential(ctx, id)
	if err != nil {
		return err
	}
	if key != nil {
		if writeErr != nil {
			return writeErr
		}
		return modelcredential.ErrPending
	}
	return nil
}

// cleanupStoppedProjectCredential is exact deletion after the engine can no longer run a writer.
func (s *ModelCredentialFlowService) cleanupStoppedProjectCredential(ctx context.Context, project string, info modelcredential.Info) error {
	flowID := agentloop.SessionFlowID(project)
	snapshot, err := s.Sessions.GetSnapshot(ctx, flowID)
	if err != nil {
		return err
	}
	holders := append([]modelcredential.Holder{}, info.Holders...)
	if snapshot.Agent != nil {
		for _, record := range snapshot.Agent.Description.Subagents {
			holders = append(holders, modelcredential.Holder{AgentID: record.AgentID})
		}
	}
	for _, holder := range holders {
		id := agenttasks.SubagentIdentity{ProjectID: project, AgentID: holder.AgentID}
		if err = s.Subagents.StopForCredentialChange(ctx, id); err != nil {
			return err
		}
		if err = s.clearSubagentCredential(ctx, id, fmt.Sprintf("%s/%s/%d", projectCredentialCleanupID(project), holder.AgentID, info.HighWaterRevision)); err != nil {
			return err
		}
	}
	return s.Sessions.DeleteStoppedModelCredential(ctx, flowID, projectCredentialCleanupID(project))
}

func projectCredentialCleanupID(project string) string { return "project-cleanup-" + project }
