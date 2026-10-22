// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// remoteReport maps the shared wire contract into the authenticated durable
// payload. The endpoint bounds the original HTTP body to 512 KiB before this
// call; admission separately bounds the server-enriched pending Channel data.
func remoteReport(connectionID string, input bridgeapi.Report) (remoteexec.RemoteReport, error) {
	if err := input.Validate(); err != nil {
		return remoteexec.RemoteReport{}, err
	}
	report := remoteexec.RemoteReport{ConnectionID: connectionID, Epoch: input.Epoch, Generation: input.Generation,
		Sequence: input.Sequence, ProviderTurn: input.ProviderTurn, TurnCompleted: input.TurnCompleted}
	if input.Events != nil {
		report.Events = make([]remoteexec.RemoteEvent, 0, len(input.Events))
		for _, event := range input.Events {
			if event.EntrySequence > math.MaxInt64 {
				return remoteexec.RemoteReport{}, errors.New("delivery entry sequence exceeds the conversation sequence range")
			}
			report.Events = append(report.Events, remoteexec.RemoteEvent{Kind: remoteexec.EventKind(event.Kind), Text: event.Text,
				Role: conversation.EntryRole(event.Role), EntrySequence: conversation.Sequence(event.EntrySequence)})
		}
	}
	if input.ToolCall != nil {
		arguments, err := conversation.ParseJSONObject(string(input.ToolCall.Arguments))
		if err != nil {
			return remoteexec.RemoteReport{}, err
		}
		report.ToolCall = &remoteexec.RemoteToolCall{Name: conversation.ToolName(input.ToolCall.Name), Arguments: arguments}
	}
	return report, report.Validate()
}

// reportResponse maps the committed admission watermark without advancing it.
func reportResponse(answer remoteexec.ReportAnswer) bridgeapi.ReportResponse {
	return bridgeapi.ReportResponse{Epoch: answer.Epoch, Generation: answer.Generation, Reported: answer.Reported, Duplicate: answer.Duplicate}
}

// refusalResponse retains the exact fence and expected report sequence.
func refusalResponse(refusal *remoteexec.ReportRefusal) bridgeapi.ErrorBody {
	code := bridgeapi.ErrorExecutorChanged
	switch refusal.Code {
	case remoteexec.RefusalMalformed:
		code = bridgeapi.ErrorMalformed
	case remoteexec.RefusalExecutorChanged:
	case remoteexec.RefusalStaleGeneration:
		code = bridgeapi.ErrorStaleGeneration
	case remoteexec.RefusalSequenceGap:
		code = bridgeapi.ErrorSequenceGap
	case remoteexec.RefusalInboxFull:
		code = bridgeapi.ErrorReportsPending
	case remoteexec.RefusalSessionClosed:
		code = bridgeapi.ErrorNotFound
	default:
		panic("undeclared remote executor refusal")
	}
	return bridgeapi.ErrorBody{Code: code, Message: refusal.Message, Epoch: refusal.Epoch, Generation: refusal.Generation, ExpectedSequence: refusal.ExpectedSequence}
}

// syncResponse maps only ReadRemote's public projection. The digest is a
// disposable hash of current state, never another durable revision or cursor.
func syncResponse(read session.RemoteRead, request bridgeapi.SyncRequest) (bridgeapi.SyncResponse, error) {
	response := bridgeapi.SyncResponse{Epoch: read.Executor.Epoch, Generation: read.Executor.Generation,
		LastSequence: uint64(read.Delivery.LastSequence), ExaminedThrough: uint64(read.Delivery.Through), SettledThrough: read.SettledThrough,
		Entries: make([]bridgeapi.Entry, 0, len(read.Delivery.Entries)), ToolSetDigest: agentloop.RemoteToolDigest(), Requirements: read.Requirements}
	if read.Executor.Mode == remoteexec.ModePlatform {
		response.Idle = true
		response.ToolSetDigest = ""
	} else if read.Executor.Remote.ActivationSequence != nil {
		response.ActivationSequence = uint64(*read.Executor.Remote.ActivationSequence)
	}
	switch {
	case read.RepositoryContextAvailable:
		response.AgentContextState = bridgeapi.AgentContextReady
	case read.WorkspaceState == agentloop.WorkspaceRequested || read.WorkspaceState == "":
		response.AgentContextState = bridgeapi.AgentContextPending
	default:
		response.AgentContextState = bridgeapi.AgentContextUnavailable
	}
	if read.Delivery.Truncated {
		response.Dropped = &bridgeapi.DroppedRange{From: request.After + 1, Through: uint64(read.Delivery.FirstRetainedSequence - 1)}
	}
	if pending := read.PendingInput; pending != nil {
		response.PendingQuestion = &bridgeapi.PendingQuestion{CallID: string(pending.CallID), Kind: string(pending.Kind), Text: pending.Text}
	}
	for _, todo := range read.Todos {
		response.Todos = append(response.Todos, bridgeapi.Todo{Content: todo.Content, Status: string(todo.Status)})
	}
	if read.Head != "" {
		response.Head = &bridgeapi.Head{Commit: read.Head}
	}
	if interrupt := read.Interrupt; interrupt != nil {
		response.Interrupt = &bridgeapi.Interrupt{Turn: interrupt.Turn, RequestedAt: interrupt.RequestedAt}
		if interrupt.Actor != nil {
			response.Interrupt.By = interrupt.Actor.Email
		}
	}
	stateProjection := response
	stateProjection.LastSequence, stateProjection.ExaminedThrough, stateProjection.Dropped = 0, 0, nil
	state, err := json.Marshal(stateProjection)
	if err != nil {
		return bridgeapi.SyncResponse{}, err
	}
	digest := sha256.Sum256(state)
	response.StateDigest = hex.EncodeToString(digest[:])
	if response.StateDigest == request.StateDigest {
		response.PendingQuestion, response.Requirements, response.Todos = nil, "", nil
	}
	if request.Context && !response.Idle {
		response.AgentContext = agentloop.RemoteAgentContext(read.RepositoryContext, read.StartingExample, read.NoCardDesign)
	}
	for _, item := range read.Delivery.Entries {
		entry, err := syncEntry(item)
		if err != nil {
			return bridgeapi.SyncResponse{}, err
		}
		response.Entries = append(response.Entries, entry)
	}

	encoded, err := json.Marshal(response)
	if err != nil {
		return bridgeapi.SyncResponse{}, err
	}
	if len(encoded) > bridgeapi.MaxSyncResponseBytes {
		return bridgeapi.SyncResponse{}, errors.New("remote projection exceeds the encoded response bound")
	}
	return response, nil
}

// syncEntry maps one projected entry, which is text or a tool result; the
// projection already named the attachments a message left out
// (conversation.UndeliveredAttachmentsNote), and a reference never reaches
// the Bridge.
func syncEntry(item conversation.SequencedEntry) (bridgeapi.Entry, error) {
	if len(item.Entry.AttachmentReferences()) > 0 {
		panic("remote projection returned an attachment reference")
	}
	entry := bridgeapi.Entry{Sequence: uint64(item.Sequence), Role: bridgeapi.EntryRole(item.Entry.Role)}
	switch item.Entry.Kind {
	case conversation.EntryKindModelReply:
		entry.Kind = bridgeapi.EntryKindModelReply
	case conversation.EntryKindUserMessage:
		entry.Kind = bridgeapi.EntryKindUserMessage
	case conversation.EntryKindNotice:
		entry.Kind = bridgeapi.EntryKindNotice
	case conversation.EntryKindNotification:
		entry.Kind = bridgeapi.EntryKindNotification
	case conversation.EntryKindToolResult:
		entry.Kind = bridgeapi.EntryKindToolResult
	default:
		panic("remote projection returned a nonpublic entry kind")
	}
	if item.Entry.Meta.Via != nil {
		entry.Via = *item.Entry.Meta.Via
	}
	if item.Entry.Meta.AnsweredInputCallID != nil {
		entry.AnsweredInputCallID = string(*item.Entry.Meta.AnsweredInputCallID)
	}
	if entry.Kind == bridgeapi.EntryKindToolResult {
		for _, raw := range item.Entry.Blocks {
			var result struct {
				CallID  string `json:"tool_use_id"`
				Content string `json:"content"`
				IsError bool   `json:"is_error"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				return bridgeapi.Entry{}, err
			}
			entry.ToolResults = append(entry.ToolResults, bridgeapi.ToolResult{CallID: result.CallID, Content: result.Content, IsError: result.IsError,
				Status: toolResultStatus(item.Entry.Meta.ToolCall, result.Content, result.IsError)})
		}
	} else {
		entry.Text = item.Entry.Text()
	}
	return entry, nil
}

// toolResultStatus classifies request_user_input's waiting placeholder from
// the Session's own typed result payload; every other result has none.
func toolResultStatus(call *conversation.ToolCallSummary, content string, isError bool) bridgeapi.ToolResultStatus {
	if isError || call == nil || call.Name != conversation.ToolRequestUserInput {
		return ""
	}
	var payload agentloop.ToolResultPayload
	if json.Unmarshal([]byte(content), &payload) != nil {
		return ""
	}
	switch payload.Status {
	case agentloop.ToolResultStatusWaitingForUser:
		return bridgeapi.ToolResultStatusWaitingForUser
	default:
		return ""
	}
}
