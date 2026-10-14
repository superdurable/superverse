// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/flowdesign"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
)

// eventPollTimeout bounds one long poll of a best-effort Stream, on this
// server's clock.
const eventPollTimeout = 20 * time.Second

// ErrCommandRejected reports a valid command that does not match the current
// durable state of the Session; the command had no effect.
var ErrCommandRejected = errors.New("agent command was rejected by current session state")

// ErrModelRevisionChanged reports a model selection at a revision that no
// longer holds; it had no effect, and the caller rereads the Session.
var ErrModelRevisionChanged = errors.New("the Session's model revision changed")

// ErrFlowDesignNotFound reports a slot of the Session's Flow designs that
// holds no design.
var ErrFlowDesignNotFound = errors.New("the Session holds no flow design in this slot")

// ErrFlowDesignRevisionChanged reports a slot that holds another design than
// the digest the caller read: a newer card or confirmation replaced it, and the
// caller rereads the Session.
var ErrFlowDesignRevisionChanged = errors.New("the Session's flow design changed")

// SessionExecutionStoppedError reports a Session whose engine execution ended
// by failure, cancellation or operator termination while its retained status
// still reads ACTIVE. Such a Session never counts as running: no
// command reaches it. summary is the retained lifecycle when a read found it,
// and zero when a command found the execution gone.
type SessionExecutionStoppedError struct {
	Reason  agentloop.ExecutionFailure
	summary agentloop.SessionSummary
}

// SummaryForTest is summary for the tests.
func (e *SessionExecutionStoppedError) SummaryForTest() agentloop.SessionSummary { return e.summary }

func (e *SessionExecutionStoppedError) Error() string {
	return "the Session's engine execution stopped: " + e.Reason.String()
}

// SessionAgentFlowClient is the typed application boundary around Dex Agent
// operations. The API validates user input; the client exposes no persistence
// descriptor.
type SessionAgentFlowClient struct {
	sdk  *dex.Client
	flow *SessionAgentFlow
	// execution reads current engine liveness by stable FlowID. It never
	// supplies business history or exposes RunID to resource consumers.
	execution dexpb.FlowServiceClient
	model     conversation.Model
}

// newSessionAgentFlowClient constructs the client; model is the model a new
// Session starts with.
func newSessionAgentFlowClient(sdkClient *dex.Client, flow *SessionAgentFlow, execution dexpb.FlowServiceClient, model conversation.Model) *SessionAgentFlowClient {
	return &SessionAgentFlowClient{sdk: sdkClient, flow: flow, execution: execution, model: model}
}

// SessionRequest returns the accepted start of the Session of identity's
// Project with the model a new Session starts with and its Start RequestID,
// the fingerprint of the Project. The repository context reaches the model
// once a workspace lease has been accepted.
func (client *SessionAgentFlowClient) SessionRequest(identity agentloop.SessionIdentity) agentloop.StartRequest {
	return agentloop.StartRequest{Session: identity, Model: client.model, SessionStartIdentity: agentloop.SessionStartIdentity(identity.ProjectID)}
}

// StartAccepted starts the Project's Session, session-<projectID>, without
// waiting for workspace or model preparation. The Project fingerprint is the
// Start RequestID, so a retried or replayed start joins the accepted
// execution even when server defaults changed, and the FlowID is never
// started again. The Session has no Flow timeout: it never expires and ends
// only with its Project.
func (client *SessionAgentFlowClient) StartAccepted(ctx context.Context, request agentloop.StartRequest) error {
	if request.SessionStartIdentity != agentloop.SessionStartIdentity(request.Session.ProjectID) {
		panic("a Session start requires the Project fingerprint as its request identity")
	}
	durability := dex.StepDurabilityAsync
	_, err := client.sdk.StartFlow(ctx, client.flow, string(agentloop.SessionFlowID(request.Session.ProjectID)),
		nil,
		dex.StartFlowOptions{
			RequestID:      &request.SessionStartIdentity,
			AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true},
			Attributes:     sessionStartAttributes(request),
			IDReusePolicy:  dex.IDReuseDisallow,
			ConfigOverride: &dex.FlowConfig{StepDurability: &durability},
		})
	return err
}

// The commands below lock Session Attributes, so each retries an explicit lock
// conflict within its deadline. Commands are not deduplicated: one retried
// after a lost response may apply twice.

// SendPendingMessage preserves the caller's queue identity without claiming exactly-once admission.
func (client *SessionAgentFlowClient) SendPendingMessage(ctx context.Context, flowID agentloop.FlowID, pending conversation.PendingUserMessage) error {
	return client.command(ctx, flowID, client.flow.SendMessage, pending, commandRPCTimeout)
}

// UpdateSessionModel selects the Session's model at an exact revision; the next
// turn rolls the context over to it.
func (client *SessionAgentFlowClient) UpdateSessionModel(ctx context.Context, flowID agentloop.FlowID, request agentloop.UpdateSessionModelRequest) error {
	selection, err := dexflow.InvokeLocked[modelSelection](ctx, client.sdk, commandRPCTimeout, string(flowID), client.flow.UpdateSessionModel, request)
	switch {
	case err != nil:
		return client.ended(ctx, flowID, err)
	case selection.Stale:
		return ErrModelRevisionChanged
	case !selection.Accepted:
		return ErrCommandRejected
	}
	return nil
}

// UpdatePreviewAutoDeploy turns the Preview's deployment of each pushed commit
// on or off; a Session that is not ACTIVE rejects it.
func (client *SessionAgentFlowClient) UpdatePreviewAutoDeploy(ctx context.Context, flowID agentloop.FlowID, request agentloop.PreviewAutoDeployRequest) error {
	return client.command(ctx, flowID, client.flow.UpdatePreviewAutoDeploy, request, commandRPCTimeout)
}

// AnswerQuestions answers one exact pending input batch.
func (client *SessionAgentFlowClient) AnswerQuestions(ctx context.Context, flowID agentloop.FlowID, request agentloop.AnswerQuestionsRequest) error {
	return client.command(ctx, flowID, client.flow.AnswerQuestions, request, commandRPCTimeout)
}

// SteerMessage moves one queued message into steering.
func (client *SessionAgentFlowClient) SteerMessage(ctx context.Context, flowID agentloop.FlowID, request conversation.SteerMessageRequest) error {
	return client.command(ctx, flowID, client.flow.SteerMessage, request, commandRPCTimeout)
}

// ErrQueuedMessageNotAuthor reports a definite refusal under the queue lock.
var ErrQueuedMessageNotAuthor = errors.New("queued message belongs to another author")

// DeleteQueuedMessage removes one exact pending user message.
func (client *SessionAgentFlowClient) DeleteQueuedMessage(ctx context.Context, flowID agentloop.FlowID, request conversation.DeleteQueuedMessageRequest) error {
	result, err := dexflow.InvokeLocked[queuedMessageDeletion](ctx, client.sdk, commandRPCTimeout, string(flowID), client.flow.DeleteQueuedMessage, request)
	if err != nil {
		return client.ended(ctx, flowID, err)
	}
	if result.NotAuthor {
		return ErrQueuedMessageNotAuthor
	}
	if !result.Deleted {
		return ErrCommandRejected
	}
	return nil
}

// ApproveTool decides the approval of the pending tool call.
func (client *SessionAgentFlowClient) ApproveTool(ctx context.Context, flowID agentloop.FlowID, request agentloop.ToolApprovalRequest) error {
	return client.command(ctx, flowID, client.flow.ApproveTool, request, commandRPCTimeout)
}

// InterruptAgent is the Stop button for the turn the user saw: it stops that
// turn and every running task. A Stop of an earlier turn, or with nothing
// running, is rejected.
func (client *SessionAgentFlowClient) InterruptAgent(ctx context.Context, flowID agentloop.FlowID, request agentloop.InterruptAgentRequest) error {
	return client.command(ctx, flowID, client.flow.InterruptAgent, request, commandRPCTimeout)
}

// ReportSessionNotification admits one notification from outside the Session,
// for the Session Service: true when it is published or repeats an admitted
// ID, false when the Session is not ACTIVE or its notification queue is full.
// A missing or closed Session returns ErrSessionInactive.
func (client *SessionAgentFlowClient) ReportSessionNotification(ctx context.Context, flowID agentloop.FlowID, notification conversation.SessionNotification) (bool, error) {
	admitted, err := dexflow.InvokeLocked[bool](ctx, client.sdk, workspaceCallbackTimeout, string(flowID), client.flow.ReportSessionNotification, notification)
	return admitted, sessionInactive(err)
}

// subagentRecord locks and loads the Subagents record of agentID for one
// invocation, so a read-modify-write of the record holds it.
func subagentRecord(agentID string) dex.RPCInvokeOptions {
	return dex.RPCInvokeOptions{LockAttributeMapInstances: []dex.AttributeLock{dex.LockAttributeMap(subagentsAttribute, agentID)},
		LoadAttributeMapInstances: []dex.AttributeMapLoad{subagentsAttribute.Load(agentID)}}
}

// ReportSubagentNotification delivers the task notification of the Session's
// subagent agentID, for the Session Service: its record takes the end, and
// the answer is ReportSessionNotification's. A missing or closed Session
// returns ErrSessionInactive.
func (client *SessionAgentFlowClient) ReportSubagentNotification(ctx context.Context, flowID agentloop.FlowID, agentID string, notification conversation.SessionNotification) (bool, error) {
	admitted, err := dexflow.InvokeLockedWith[bool](ctx, client.sdk, workspaceCallbackTimeout, string(flowID), client.flow.ReportSessionNotification, notification, subagentRecord(agentID))
	return admitted, sessionInactive(err)
}

// AdmitSubagent answers the admission of a subagent's first Step, for the
// Session Service: admitted only for the call its record admitted while the
// Session is ACTIVE and no Stop is pending, and stopped for a record a Stop
// ended before the subagent started. A missing or closed Session returns
// ErrSessionInactive.
func (client *SessionAgentFlowClient) AdmitSubagent(ctx context.Context, flowID agentloop.FlowID, admission agenttasks.SubagentAdmission) (agenttasks.SubagentAdmissionAnswer, error) {
	answer, err := dexflow.InvokeLockedWith[agenttasks.SubagentAdmissionAnswer](ctx, client.sdk, workspaceCallbackTimeout, string(flowID), client.flow.AdmitSubagent, admission, subagentRecord(admission.AgentID))
	return answer, sessionInactive(err)
}

// RequestSubagentWorkspace asks the Session for the lease of its instance for
// its running subagent agentID, reporting the lease of request lost that the
// subagent's transport proved unreachable: nil while the Session provisions
// an instance. A Session that is no longer ACTIVE, or whose record of the
// subagent no longer runs, answers ErrSessionNotActive; one whose engine
// execution ended answers like a command that found it ended.
func (client *SessionAgentFlowClient) RequestSubagentWorkspace(ctx context.Context, flowID agentloop.FlowID, agentID, lost string) (*workspace.Lease, error) {
	demand := agentloop.WorkspaceDemandRequest{ForAgentID: agentID, Lost: lost}
	result, err := dexflow.InvokeLockedWith[workspaceDemandResult](ctx, client.sdk, workspaceCallbackTimeout, string(flowID), client.flow.RequestSessionWorkspace, demand,
		dex.RPCInvokeOptions{LoadAttributeMapInstances: []dex.AttributeMapLoad{subagentsAttribute.Load(agentID)}})
	switch {
	case err != nil:
		return nil, client.ended(ctx, flowID, err)
	case result.Closed:
		return nil, ErrSessionNotActive
	}
	return result.Lease, nil
}

// ReportWorkspaceOutcome tells the Session an outcome of its
// SessionWorkspaceFlow, for the Session Service: true when the Session
// admitted the report, false when it waits for no such request. A
// missing or closed Session returns ErrSessionInactive.
func (client *SessionAgentFlowClient) ReportWorkspaceOutcome(ctx context.Context, flowID agentloop.FlowID, report agentloop.WorkspaceReport) (bool, error) {
	admitted, err := dexflow.InvokeLocked[bool](ctx, client.sdk, workspaceCallbackTimeout, string(flowID), client.flow.ReportWorkspaceOutcome, report)
	return admitted, sessionInactive(err)
}

// command invokes a command RPC, retrying a lock conflict within lockWait; a
// Session that answers false rejected it.
func (client *SessionAgentFlowClient) command(ctx context.Context, flowID agentloop.FlowID, rpc, input any, lockWait time.Duration) error {
	accepted, err := dexflow.InvokeLocked[bool](ctx, client.sdk, lockWait, string(flowID), rpc, input)
	if err == nil && !accepted {
		return ErrCommandRejected
	}
	return client.ended(ctx, flowID, err)
}

// ended classifies a command that found no running execution, because it raced
// the end of the engine execution: a stopped execution is a
// *SessionExecutionStoppedError and a completed one rejected the command. Any
// other error is returned as is, and a failed liveness read keeps the command
// error.
func (client *SessionAgentFlowClient) ended(ctx context.Context, flowID agentloop.FlowID, err error) error {
	var inactive *dex.FlowNotActiveOrNotFoundError
	if !errors.As(err, &inactive) {
		return err
	}
	failure, readErr := client.executionFailure(ctx, flowID)
	switch {
	case readErr != nil:
		return fmt.Errorf("%w; the engine status is unavailable: %w", err, readErr)
	case failure == agentloop.ExecutionClosed:
		return fmt.Errorf("%w: %w", ErrCommandRejected, err)
	case failure != agentloop.ExecutionRunning:
		return &SessionExecutionStoppedError{Reason: failure}
	}
	return err
}

// GetSnapshot reads one atomic durable application view, with the engine
// liveness of a Session that is not archived yet.
func (client *SessionAgentFlowClient) GetSnapshot(ctx context.Context, flowID agentloop.FlowID) (agentloop.SessionSnapshot, error) {
	var snapshot agentloop.SessionSnapshot
	if err := dexflow.InvokeRetainedRead(ctx, client.sdk, string(flowID), client.flow.GetSnapshot, nil, &snapshot); err != nil {
		return agentloop.SessionSnapshot{}, err
	}
	if snapshot.Session.Status == agentloop.SessionStatusActive {
		failure, err := client.executionFailure(ctx, flowID)
		if err != nil {
			return agentloop.SessionSnapshot{}, err
		}
		snapshot.ExecutionFailure = failure
	}
	return snapshot, nil
}

// ReadExecutor reads the live Session's executor for pairing and revocation
// admission. Credentials and authentication never use this Flow read.
func (client *SessionAgentFlowClient) ReadExecutor(ctx context.Context, projectID string) (remoteexec.Executor, error) {
	flowID := agentloop.SessionFlowID(projectID)
	if summary, err := client.GetLiveSessionSummary(ctx, flowID); err != nil {
		return remoteexec.Executor{}, err
	} else if summary.Status != agentloop.SessionStatusActive {
		return remoteexec.Executor{}, ErrCommandRejected
	}
	var executor remoteexec.Executor
	err := dexflow.InvokeRetainedRead(ctx, client.sdk, string(flowID), client.flow.ReadExecutor,
		agentloop.SessionScopeInput{ProjectID: projectID}, &executor)
	return executor, err
}

// GetSessionSummary reads the resource lifecycle retained after the Agent is
// archived. It trusts the retained status, so only internal reads of retained
// state use it; an authorization reads GetLiveSessionSummary.
func (client *SessionAgentFlowClient) GetSessionSummary(ctx context.Context, flowID agentloop.FlowID) (agentloop.SessionSummary, error) {
	var summary agentloop.SessionSummary
	err := dexflow.InvokeRetainedRead(ctx, client.sdk, string(flowID), client.flow.GetSessionSummary, nil, &summary)
	return summary, err
}

// GetLiveSessionSummary reads the retained lifecycle and, while it reads ACTIVE,
// joins a bounded engine status read of exactly this execution. A
// stopped execution is a *SessionExecutionStoppedError that carries the
// retained summary. An unavailable engine read fails; it never reports a
// running or a stopped Session.
func (client *SessionAgentFlowClient) GetLiveSessionSummary(ctx context.Context, flowID agentloop.FlowID) (agentloop.SessionSummary, error) {
	summary, err := client.GetSessionSummary(ctx, flowID)
	if err != nil || summary.Status != agentloop.SessionStatusActive {
		return summary, err
	}
	failure, err := client.executionFailure(ctx, flowID)
	if err != nil {
		return agentloop.SessionSummary{}, err
	}
	if failure != agentloop.ExecutionRunning {
		return agentloop.SessionSummary{}, &SessionExecutionStoppedError{Reason: failure, summary: summary}
	}
	return summary, nil
}

// executionFailure classifies the engine status of exactly this Session
// execution. It is diagnostic engine state, never a substitute for the Session
// snapshot.
func (client *SessionAgentFlowClient) executionFailure(ctx context.Context, flowID agentloop.FlowID) (agentloop.ExecutionFailure, error) {
	// The raw engine read bypasses the SDK's FlowID validation.
	if flowID == "" {
		return agentloop.ExecutionRunning, errors.New("session FlowID must not be empty")
	}
	ctx, cancel := context.WithTimeout(ctx, readRPCTimeout)
	defer cancel()
	summary, err := client.execution.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: string(flowID)})
	if err != nil {
		return agentloop.ExecutionRunning, err
	}
	if summary.GetFlowExecutionId().GetFlowId() != string(flowID) || summary.GetFlowType() != dex.GetFinalFlowType(client.flow) {
		return agentloop.ExecutionRunning, errors.New("session execution scope mismatch")
	}
	switch summary.GetFlowStatus() {
	case dexpb.FlowStatus_FLOW_STATUS_RUNNING:
		return agentloop.ExecutionRunning, nil
	case dexpb.FlowStatus_FLOW_STATUS_CANCELED:
		return agentloop.ExecutionCanceled, nil
	case dexpb.FlowStatus_FLOW_STATUS_TERMINATED:
		return agentloop.ExecutionTerminated, nil
	case dexpb.FlowStatus_FLOW_STATUS_FAILED, dexpb.FlowStatus_FLOW_STATUS_SERVER_SIDE_TIMEOUT_INTERNAL_ONLY:
		return agentloop.ExecutionFailed, nil
	case dexpb.FlowStatus_FLOW_STATUS_COMPLETED:
		return agentloop.ExecutionClosed, nil
	default:
		return agentloop.ExecutionRunning, errors.New("session execution status is unavailable")
	}
}

// GetArchivedMessageRange reads up to limit archived messages before a
// ten-message boundary, one immutable chunk per read.
func (client *SessionAgentFlowClient) GetArchivedMessageRange(ctx context.Context, flowID agentloop.FlowID, before conversation.Sequence, limit int) (conversation.HistoryPage, error) {
	messages := make([]conversation.SequencedMessage, 0, limit)
	nextBefore := &before
	for len(messages) < limit && nextBefore != nil {
		first, _ := agentloop.ArchivedMessageChunkFirst(*nextBefore)
		var chunk archivedMessagesRPCOutput
		err := dexflow.InvokeRetainedRead(ctx, client.sdk, string(flowID), client.flow.GetArchivedMessages, *nextBefore, &chunk, archivedMessagesAttribute.Load(agentloop.SequenceKey(first)))
		if err != nil {
			return conversation.HistoryPage{}, err
		}
		if !chunk.Found {
			return conversation.HistoryPage{}, &conversation.ArchivedMessagesNotFoundError{BeforeSequence: *nextBefore}
		}
		messages = append(chunk.Page.Messages, messages...)
		nextBefore = chunk.Page.NextBeforeSequence
	}
	return conversation.HistoryPage{Messages: messages, NextBeforeSequence: nextBefore}, nil
}

// GetFlowDesign reads the design of slot whose digest the caller read from
// the Session's Snapshot.
func (client *SessionAgentFlowClient) GetFlowDesign(ctx context.Context, flowID agentloop.FlowID, slot flowdesign.Slot, digest string) (flowdesign.Design, error) {
	var read flowDesignRPCOutput
	if err := dexflow.InvokeRetainedRead(ctx, client.sdk, string(flowID), client.flow.GetFlowDesign, slot, &read); err != nil {
		return flowdesign.Design{}, err
	}
	if !read.Found {
		return flowdesign.Design{}, ErrFlowDesignNotFound
	}
	if read.Design.Digest() != digest {
		return flowdesign.Design{}, ErrFlowDesignRevisionChanged
	}
	return read.Design, nil
}

// ReadEvent long-polls exactly one best-effort Stream.
func (client *SessionAgentFlowClient) ReadEvent(ctx context.Context, flowID agentloop.FlowID, stream EventStream, resumeToken ResumeToken) (StreamEvent, error) {
	definition, kind := stream.definition()
	event := StreamEvent{Kind: kind}
	var value any = &event.Text
	if kind == StreamEventKindActivity {
		value = &event.Activity
	}
	ctx, endPoll := comm.WithLongPollWindow(ctx, eventPollTimeout)
	defer endPoll()
	message, err := client.sdk.ReadStream(ctx, string(flowID), definition, string(resumeToken), value)
	if err != nil {
		if comm.LongPollWindowEnded(ctx) {
			return StreamEvent{}, comm.ErrLongPollWindowEnded
		}
		return StreamEvent{}, err
	}
	event.ResumeToken, event.CreatedAt, event.Source = ResumeToken(message.ResumeToken), message.CreatedTime, message.Source
	return event, nil
}

// LatestEventResumeToken returns the token of the newest retained event of a
// best-effort Stream, or "" when it retains none.
func (client *SessionAgentFlowClient) LatestEventResumeToken(ctx context.Context, flowID agentloop.FlowID, stream EventStream) (ResumeToken, error) {
	definition, kind := stream.definition()
	if kind == StreamEventKindActivity {
		return latestResumeToken[conversation.AgentEvent](ctx, client.sdk, flowID, definition)
	}
	return latestResumeToken[string](ctx, client.sdk, flowID, definition)
}

func latestResumeToken[T any](ctx context.Context, sdk *dex.Client, flowID agentloop.FlowID, stream dex.StreamDef) (ResumeToken, error) {
	var page dex.StreamMessagesPage[T]
	if err := sdk.ListStreamMessages(ctx, string(flowID), stream, 1, "", &page); err != nil || len(page.Messages) == 0 {
		return "", err
	}
	return ResumeToken(page.Messages[0].ResumeToken), nil
}

// The Session service calls the RPCs below for the SessionWorkspaceFlow and the
// Preview of the Session's instance (notification and workspace demand) and
// for a deleting Project (completion). A Session that is missing or closed
// answers by its typed SDK error.

// workspaceCallbackTimeout bounds the lock-conflict retries of a workspace RPC,
// whose own deadline is the short RPC preset. The polling rounds that call it
// outlast it.
const workspaceCallbackTimeout = 10 * time.Second

// ErrSessionCleanupReceiptMismatch reports a completed Session whose cleanup
// receipt names another Project or an unexpected reason.
var ErrSessionCleanupReceiptMismatch = errors.New("session cleanup receipt scope mismatch")

// ErrSessionInactive reports an RPC that no running Session execution
// answered: the Session is missing or closed and wants no instance. It wraps
// the SDK error, which says which.
var ErrSessionInactive = errors.New("session execution is missing or closed")

// sessionInactive classifies a missing or closed Session as ErrSessionInactive
// and keeps the SDK error as its cause.
func sessionInactive(err error) error {
	if isSessionMissingOrClosed(err) {
		return fmt.Errorf("%w: %w", ErrSessionInactive, err)
	}
	return err
}

// WorkspaceDemand is the Session's answer to a workspace demand: the
// workspace after it, and whether its READY instance was lent to the Preview.
type WorkspaceDemand struct {
	Summary agentloop.WorkspaceSummary
	Lent    bool
}

// ErrSessionNotActive answers a workspace demand of a Session that is no
// longer ACTIVE: it lends and requests nothing.
var ErrSessionNotActive = errors.New("session is archived")

// RequestSessionWorkspace asks an ACTIVE Session for a workspace outside the
// conversation: a READY instance is lent to a Preview demand, and otherwise
// the Session requests one unless a request is in flight. A Session that is
// no longer ACTIVE answers ErrSessionNotActive; one whose engine execution
// ended answers like a command that found it ended.
func (client *SessionAgentFlowClient) RequestSessionWorkspace(ctx context.Context, flowID agentloop.FlowID, demand agentloop.WorkspaceDemandRequest) (WorkspaceDemand, error) {
	result, err := dexflow.InvokeLocked[workspaceDemandResult](ctx, client.sdk, workspaceCallbackTimeout, string(flowID), client.flow.RequestSessionWorkspace, demand)
	if err != nil {
		return WorkspaceDemand{}, client.ended(ctx, flowID, err)
	}
	if result.Closed {
		return WorkspaceDemand{}, ErrSessionNotActive
	}
	return WorkspaceDemand{Summary: result.Summary, Lent: result.Lent}, nil
}

// RenewSessionWorkspace moves the Sandbox deadline of the Session's READY
// instance to at least agentloop.KeepRunningHold from now and reports whether
// the Session was ACTIVE with a READY workspace. One whose engine execution
// ended answers like a command that found it ended.
func (client *SessionAgentFlowClient) RenewSessionWorkspace(ctx context.Context, flowID agentloop.FlowID) (bool, error) {
	renewed, err := dexflow.InvokeLocked[bool](ctx, client.sdk, workspaceCallbackTimeout, string(flowID), client.flow.RenewSessionWorkspace, nil)
	if err != nil {
		return false, client.ended(ctx, flowID, err)
	}
	return renewed, nil
}

// HasAbnormalProjectSessionTermination observes engine status without waiting
// for a running archived Session to finish. The caller must prove exact Project
// membership and all Sandbox/workspace cleanup before discharging ownership.
func (client *SessionAgentFlowClient) HasAbnormalProjectSessionTermination(ctx context.Context, flowID agentloop.FlowID) (bool, error) {
	failure, err := client.executionFailure(ctx, flowID)
	return failure != agentloop.ExecutionRunning && failure != agentloop.ExecutionClosed, err
}

// CompleteProjectSession asks the Session of a deleting Project to archive
// and complete; it completes asynchronously, and ProjectSessionCompleted reads
// whether it did. A Session whose execution already ended is not asked.
// Callers must establish authoritative Project DELETING admission first.
// lockWait bounds the wait for a Session Step that holds the status or
// revision lock; past it the lock conflict is returned.
func (client *SessionAgentFlowClient) CompleteProjectSession(ctx context.Context, projectID string, lockWait time.Duration) error {
	_, err := dexflow.InvokeLocked[bool](ctx, client.sdk, lockWait, string(agentloop.SessionFlowID(projectID)), client.flow.CompleteProjectSession, agentloop.SessionScopeInput{ProjectID: projectID})
	var inactive *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &inactive) {
		return nil
	}
	return err
}

// ProjectSessionCompleted reports whether the Session of a deleting Project
// completed with its graceful cleanup receipt, without waiting for one that
// still runs. A completed Session with another receipt is
// ErrSessionCleanupReceiptMismatch.
func (client *SessionAgentFlowClient) ProjectSessionCompleted(ctx context.Context, projectID string) (bool, error) {
	flowID := agentloop.SessionFlowID(projectID)
	failure, err := client.executionFailure(ctx, flowID)
	if err != nil || failure != agentloop.ExecutionClosed {
		return false, err
	}
	result, err := client.sdk.WaitForFlow(ctx, string(flowID), dex.WaitForFlowOptions{NeedsResults: true})
	if err != nil {
		return false, err
	}
	if result.Status != dex.FlowCompleted {
		return false, errors.New("session did not complete gracefully")
	}
	var receipt agentloop.SessionCleanupReceipt
	if err = result.DecodeSingleOutput(&receipt); err != nil {
		return false, err
	}
	if receipt.ProjectID != projectID || (receipt.Reason != agentloop.CloseReasonProjectCleanupComplete.String() && receipt.Reason != agentloop.CloseReasonAdmissionDenied.String()) {
		return false, ErrSessionCleanupReceiptMismatch
	}
	return true, nil
}

// isSessionMissingOrClosed classifies the typed outcomes of an active-required
// RPC whose target execution does not exist or has completed.
func isSessionMissingOrClosed(err error) bool {
	var inactive *dex.FlowNotActiveOrNotFoundError
	var missing *dex.FlowNotFoundError
	return errors.As(err, &inactive) || errors.As(err, &missing)
}

// NewSessionAgentFlowClientForTest is newSessionAgentFlowClient for the tests.
func NewSessionAgentFlowClientForTest(sdkClient *dex.Client, flow *SessionAgentFlow, execution dexpb.FlowServiceClient, model conversation.Model) *SessionAgentFlowClient {
	return newSessionAgentFlowClient(sdkClient, flow, execution, model)
}

// SetExecutor commits a basis-epoch activation or switch with interruption.
func (client *SessionAgentFlowClient) SetExecutor(ctx context.Context, request remoteexec.SetRequest) (remoteexec.SetResult, error) {
	flowID := agentloop.SessionFlowID(request.ProjectID)
	result, err := dexflow.InvokeLocked[remoteexec.SetResult](ctx, client.sdk, commandRPCTimeout, string(flowID), client.flow.SetExecutor, request)
	return result, client.ended(ctx, flowID, err)
}

// ReportRemote returns the committed watermark or a typed admission refusal.
func (client *SessionAgentFlowClient) ReportRemote(ctx context.Context, request remoteexec.ReportRequest) (remoteexec.ReportAnswer, error) {
	flowID := agentloop.SessionFlowID(request.ProjectID)
	result, err := dexflow.InvokeLocked[remoteexec.ReportResult](ctx, client.sdk, commandRPCTimeout, string(flowID), client.flow.ReportRemote, request)
	if err != nil {
		return remoteexec.ReportAnswer{}, client.ended(ctx, flowID, err)
	}
	if result.Refusal != nil {
		return result.Answer, result.Refusal
	}
	return result.Answer, nil
}

// RemotePendingInput preserves the exact pending question identity without its
// provider prompts or tool arguments.
type RemotePendingInput struct {
	CallID conversation.CallID            `json:"call_id,omitempty"`
	Kind   agentloop.PendingUserInputKind `json:"kind"`
	Text   string                         `json:"text"`
}

// RemoteRead is a typed, transient projection of current Session authority.
// NeedArchiveKeys are server-derived and carry no business state or cursor.
type RemoteRead struct {
	Executor        remoteexec.Executor       `json:"executor"`
	Delivery        remoteexec.Delivery       `json:"delivery"`
	NeedsArchives   bool                      `json:"needs_archives,omitempty"`
	NeedCurrentKeys []string                  `json:"need_current_keys,omitempty"`
	NeedArchiveKeys []string                  `json:"need_archive_keys,omitempty"`
	Refusal         *remoteexec.ReportRefusal `json:"refusal,omitempty"`
	PendingInput    *RemotePendingInput       `json:"pending_input,omitempty"`
	Requirements    string                    `json:"requirements,omitempty"`
	Todos           []agentloop.TodoItem      `json:"todos,omitempty"`
	Head            string                    `json:"head"`
	Interrupt       *agentloop.AgentInterrupt `json:"interrupt,omitempty"`
	// SettledThrough is the highest report whose outcome the Session recorded.
	SettledThrough uint64 `json:"settled_through"`
	// RepositoryContext is the Workspace's, when the request asked for it.
	RepositoryContext string `json:"repository_context,omitempty"`
}

// ReadRemote negotiates 16 exact current entries or one ten-entry archive chunk. The initial read,
// data read and one retention-race retry share a single read deadline. Every
// pass reauthenticates the current executor in the RPC; no map key comes from
// an HTTP client. A second changed floor returns a retryable observation gap.
func (client *SessionAgentFlowClient) ReadRemote(ctx context.Context, request remoteexec.ReadRequest) (RemoteRead, error) {
	ctx, cancel := context.WithTimeout(ctx, readRPCTimeout)
	defer cancel()
	flowID := agentloop.SessionFlowID(request.ProjectID)
	request.ArchiveKeys, request.CurrentKeys = nil, nil
	var loads []dex.AttributeMapLoad
	for pass := 0; pass < 3; pass++ {
		var result RemoteRead
		if err := dexflow.InvokeRetainedRead(ctx, client.sdk, string(flowID), client.flow.ReadRemote, request, &result, loads...); err != nil {
			return RemoteRead{}, err
		}
		if result.Refusal != nil {
			return RemoteRead{}, result.Refusal
		}
		if !result.NeedsArchives {
			return result, nil
		}
		if len(result.NeedArchiveKeys) > 1 || len(result.NeedCurrentKeys) > 16 || len(result.NeedArchiveKeys) > 0 && len(result.NeedCurrentKeys) > 0 {
			return RemoteRead{}, errors.New("remote archive selection exceeded its bound")
		}
		request.ArchiveKeys, request.CurrentKeys = result.NeedArchiveKeys, result.NeedCurrentKeys
		loads = nil
		for _, key := range request.CurrentKeys {
			loads = append(loads, currentMessagesAttribute.Load(key))
		}
		for _, key := range request.ArchiveKeys {
			loads = append(loads, archivedMessagesAttribute.Load(key))
		}
	}
	return RemoteRead{}, errors.New("remote retained history moved twice during exact loading")
}

// ReadRemoteHead reads only canonical authority and the current sequence, without
// selecting any entry map instance. Activation never hydrates an old chat window.
func (client *SessionAgentFlowClient) ReadRemoteHead(ctx context.Context, project, connection string, epoch uint64) (conversation.Sequence, error) {
	ctx, cancel := context.WithTimeout(ctx, readRPCTimeout)
	defer cancel()
	var result RemoteRead
	request := remoteexec.ReadRequest{ProjectID: project, ConnectionID: connection, Epoch: epoch}
	if err := dexflow.InvokeRetainedRead(ctx, client.sdk, string(agentloop.SessionFlowID(project)), client.flow.ReadRemote, request, &result); err != nil {
		return 0, err
	}
	if result.Refusal != nil {
		return 0, result.Refusal
	}
	return result.Delivery.LastSequence, nil
}
