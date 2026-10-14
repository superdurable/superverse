// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/backgroundcommand"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/flowdesign"
	"github.com/superdurable/superversev2/internal/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
)

var (
	// The Session resource: identity and lifecycle.
	// dex:indexed-attribute attribute-key:ProjectID index-key:keyword2 index-type:keyword value-type:string description:"Owning project"
	projectIDAttribute            = dex.DefineAttribute[string]("ProjectID", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword2"}))
	sessionProfileAttribute       = dex.DefineAttribute[string]("Profile")
	sessionStartIdentityAttribute = dex.DefineAttribute[string]("StartIdentity")
	headCommitAttribute           = dex.DefineAttribute[string]("HeadCommit")
	// dex:indexed-attribute attribute-key:SessionStatus index-key:keyword3 index-type:keyword value-type:string description:"Session lifecycle status"
	sessionStatusAttribute    = dex.DefineAttribute[string]("SessionStatus", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword3"}))
	closeReasonAttribute      = dex.DefineAttribute[string]("CloseReason")
	sessionRevisionAttribute  = dex.DefineAttribute[uint64]("Revision")
	sessionCreatedAtAttribute = dex.DefineAttribute[time.Time]("CreatedAt")
	sessionUpdatedAtAttribute = dex.DefineAttribute[time.Time]("UpdatedAt")

	// The conversation: the selected model, the state machine, the
	// append-only entry log and the context segment that replays it.
	sessionModelAttribute          = dex.DefineAttribute[agentloop.SessionModel]("SessionModel")
	executorAttribute              = dex.DefineAttribute[remoteexec.Executor]("Executor")
	remoteInboxChannel             = dex.DefineChannel[remoteexec.InboxItem]("RemoteInbox")
	agentStateAttribute            = dex.DefineAttribute[agentloop.AgentState]("AgentState")
	messageWindowLockAttribute     = dex.DefineAttribute[bool]("MessageWindowLock")
	waitingInputRoundAttribute     = dex.DefineAttribute[agentloop.WaitingInputRound]("WaitingInputRound")
	contextSegmentAttribute        = dex.DefineAttribute[agentloop.ContextSegment]("ContextSegment")
	contextSegmentPromptsAttribute = dex.DefineAttributeMap[conversation.SegmentPrompts]("ContextSegmentPrompts")
	currentMessagesAttribute       = dex.DefineAttributeMap[conversation.Entry]("CurrentMessages")
	archivedMessagesAttribute      = dex.DefineAttributeMap[conversation.ArchivedEntryChunk]("ArchivedMessages")
	queuedUserMessagesChannel      = dex.DefineChannel[conversation.PendingUserMessage]("QueuedUserMessages")
	steeredUserMessagesChannel     = dex.DefineChannel[conversation.PendingUserMessage]("SteeredUserMessages")

	// The Agent's todo list, confirmed requirements with their Flow designs, and what a turn
	// waits for: answers, approvals, a timer and the branches of a parallel tool wave.
	todoListAttribute              = dex.DefineAttribute[agentloop.TodoList]("TodoList")
	confirmedRequirementsAttribute = dex.DefineAttribute[agentloop.ConfirmedRequirements]("ConfirmedRequirements")
	flowDesignsAttribute           = dex.DefineAttributeMap[flowdesign.Design]("FlowDesigns")
	builtFlowDesignAttribute       = dex.DefineAttribute[string]("BuiltFlowDesign")
	builtFlowDesignCommitAttribute = dex.DefineAttribute[string]("BuiltFlowDesignCommit")
	pendingUserInputAttribute      = dex.DefineAttribute[agentloop.PendingUserInput]("PendingUserInput")
	pendingApprovalAttribute       = dex.DefineAttribute[agentloop.PendingApproval]("PendingApproval")
	pendingTimerAttribute          = dex.DefineAttribute[agentloop.PendingTimer]("PendingTimer")
	answeredUserInputsChannel      = dex.DefineChannel[agentloop.AnsweredUserInput]("AnsweredUserInputs")
	toolApprovalsChannel           = dex.DefineChannelMap[agentloop.ToolApproval]("ToolApprovals")
	parallelToolResultsChannel     = dex.DefineChannelMap[parallelToolResult]("ParallelToolResults")

	// What happens beside the turn: the user's Stop, notifications and their admitted IDs, background
	// commands with the stop of each one's watch by task ID, subagents and the Preview's automatic deployment.
	agentInterruptAttribute             = dex.DefineAttribute[agentloop.AgentInterrupt]("AgentInterrupt")
	notificationReceiptsAttribute       = dex.DefineAttribute[[]string]("NotificationReceipts")
	sessionNotificationsChannel         = dex.DefineChannel[conversation.SessionNotification]("SessionNotifications")
	backgroundCommandsAttribute         = dex.DefineAttributeMap[agenttasks.BackgroundCommand]("BackgroundCommands")
	backgroundCommandStopsChannel       = dex.DefineChannelMap[backgroundCommandStop]("BackgroundCommandStops")
	subagentsAttribute                  = dex.DefineAttributeMap[agenttasks.SubagentRecord]("Subagents")
	previewHandoffAttribute             = dex.DefineAttribute[agentloop.PreviewHandoff]("PreviewHandoff")
	previewAutoDeployAttribute          = dex.DefineAttribute[bool]("PreviewAutoDeploy")
	previewAutoDeployChangedByAttribute = dex.DefineAttribute[conversation.Actor]("PreviewAutoDeployChangedBy")

	// The Session workspace (its request, accepted lease and the wave using it) and one
	// outcome per WorkspaceRequestID from the SessionWorkspaceFlow, which only the waiting Step consumes.
	workspaceAttribute       = dex.DefineAttribute[agentloop.SessionWorkspace]("Workspace")
	workspaceUseAttribute    = dex.DefineAttribute[agentloop.WorkspaceUse]("WorkspaceUse")
	workspaceOutcomesChannel = dex.DefineChannelMap[agentloop.WorkspaceReport]("WorkspaceOutcomes")

	// Best-effort live output; the Snapshot is the durable record.
	reasoningSummaryStream = dex.DefineStream[string]("ReasoningSummary", 10<<20)
	assistantTextStream    = dex.DefineStream[string]("AssistantText", 10<<20)
	agentActivityStream    = dex.DefineStream[conversation.AgentEvent]("AgentActivity", 10<<20)
)

// SessionAgentFlow owns the durable coding Session and its AI Agent state machine.
type SessionAgentFlow struct {
	dex.FlowDefaults
	dependency     SessionAgentFlowDependency
	repository     repositoryContextReader
	modelClient    conversation.ModelClient
	sandbox        *sandboxtools.Runner
	diagnostics    ProjectDiagnosticsReader
	automaticTurns agentloop.AutomaticTurnBounds // how many consecutive turns notifications open
	sandboxHold    time.Duration                 // how long the main agent's workspace use holds the Sandbox
	previewHold    time.Duration                 // how long a READY Preview holds the Sandbox it serves on
	settings       agentloop.ModelSettings
}

// GetSteps registers the state machine that the package README's Step graph describes;
// only Steps that call a dependency or read a server setting hold the Flow.
func (flow *SessionAgentFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(InitializeSessionAgent{flow: flow}), dex.DefineStep(ActivateSession{}),
		dex.DefineStep(AwaitUser{flow: flow}), dex.DefineStep(AwaitRemoteAgent{flow: flow}), dex.DefineStep(AnsweredInput{flow: flow}), dex.DefineStep(CheckSteered{flow: flow}), dex.DefineStep(AwaitAgentTimer{}),
		dex.DefineStep(CompactContext{flow: flow}), dex.DefineStep(CallModel{flow: flow}),
		dex.DefineStep(RecordPlatformModelResult{flow: flow}), dex.DefineStep(RecordModelFailure{}), dex.DefineStep(RecordCompactionFailure{flow: flow}),
		dex.DefineStep(RouteTool{flow: flow}), dex.DefineStep(AwaitToolApproval{}), dex.DefineStep(ExecuteTool{flow: flow}),
		dex.DefineStep(ExecuteParallelTool{flow: flow}), dex.DefineStep(AwaitParallelToolResults{}),
		dex.DefineStep(RecoverParallelToolExecution{}), dex.DefineStep(RecordToolFailure{}), dex.DefineStep(SettleForegroundCommand{flow: flow}), dex.DefineStep(ReportPlatformIssue{flow: flow}), dex.DefineStep(AbandonPlatformIssue{}),
		dex.DefineStep(AwaitBackgroundCommand{flow: flow}), dex.DefineStep(AbandonBackgroundCommand{}), dex.DefineStep(InterruptAgent{flow: flow}), dex.DefineStep(FinishInterrupt{}),
		dex.DefineStep(RequestSessionWorkspace{flow: flow}), dex.DefineStep(SendWorkspaceCommand{flow: flow}),
		dex.DefineStep(AwaitSessionWorkspace{flow: flow}), dex.DefineStep(ApplySessionWorkspace{flow: flow}), dex.DefineStep(RecordWorkspaceFailure{flow: flow}),
		dex.DefineStep(AwaitSandboxDeadline{flow: flow}),
		dex.DefineStep(DeployPreviewHandoff{flow: flow}), dex.DefineStep(AbandonPreviewHandoff{}),
		dex.DefineStep(CompleteProjectSession{}),
	}
}

// GetRPCs registers the conversation commands, the reads, the lifecycle
// commands and the workspace demand.
func (flow *SessionAgentFlow) GetRPCs() []dex.RPCDef {
	queue := []dex.ChannelDef{queuedUserMessagesChannel}
	return []dex.RPCDef{
		dex.DefineRPC(flow.SendMessage, userCommandRPC(false, nil, inputLock)),
		dex.DefineRPC(flow.SteerMessage, userCommandRPC(true, queue)),
		dex.DefineRPC(flow.DeleteQueuedMessage, userCommandRPC(true, queue)),
		dex.DefineRPC(flow.AnswerQuestions, loadDesigns(userCommandRPC(false, nil, inputLock, requirementsLock), flowdesign.SlotPending)),
		dex.DefineRPC(flow.ApproveTool, userCommandRPC(true, nil, approvalLock)),
		// A model selection writes only SessionModel, so it never waits for a
		// model call; the next turn's rollover applies it.
		dex.DefineRPC(flow.UpdateSessionModel, &dex.RPCOptions{Timeout: commandRPCTimeout, LockAttributes: []dex.AttributeLock{statusLock, dex.LockAttribute(sessionModelAttribute)}}),
		dex.DefineRPC(flow.UpdatePreviewAutoDeploy, &dex.RPCOptions{Timeout: commandRPCTimeout, LockAttributes: []dex.AttributeLock{statusLock, dex.LockAttribute(previewAutoDeployAttribute), dex.LockAttribute(previewAutoDeployChangedByAttribute)}}),
		// A Stop decides on AgentState without its writers' MessageWindowLock,
		// which a running model call holds.
		dex.DefineRPC(flow.InterruptAgent, &dex.RPCOptions{Timeout: commandRPCTimeout, LockAttributes: []dex.AttributeLock{statusLock, interruptLock, executorLock}, LoadAttributeMaps: taskMaps}),
		// A ready Preview's report and a workspace command's outcome decide on Workspace.
		dex.DefineRPC(flow.ReportSessionNotification, dexflow.ShortRPC(statusLock, receiptsLock, workspaceLock)),
		dex.DefineRPC(flow.ReportWorkspaceOutcome, dexflow.ShortRPC(statusLock, receiptsLock, workspaceLock)),
		// Every write of a STARTING record whose subagent may have started holds
		// the AgentInterrupt lock.
		dex.DefineRPC(flow.AdmitSubagent, dexflow.ShortRPC(statusLock, interruptLock)),
		dex.DefineRPC(flow.GetSnapshot, &dex.RPCOptions{Timeout: readRPCTimeout, LoadAttributeMaps: []dex.AttributeDef{currentMessagesAttribute, backgroundCommandsAttribute, subagentsAttribute},
			LoadChannels: []dex.ChannelDef{queuedUserMessagesChannel, steeredUserMessagesChannel}}),
		dex.DefineRPC(flow.GetArchivedMessages, &dex.RPCOptions{Timeout: commandRPCTimeout}),
		dex.DefineRPC(flow.GetFlowDesign, loadDesigns(&dex.RPCOptions{Timeout: readRPCTimeout}, flowdesign.Slots...)),
		dex.DefineRPC(flow.GetSessionSummary, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.ReadExecutor, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.ReadRemote, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.SetExecutor, &dex.RPCOptions{Timeout: commandRPCTimeout, LockAttributes: []dex.AttributeLock{statusLock, executorLock, interruptLock}, LoadAttributeMaps: taskMaps, LoadChannels: []dex.ChannelDef{remoteInboxChannel}}),
		dex.DefineRPC(flow.ReportRemote, &dex.RPCOptions{Timeout: commandRPCTimeout, LockAttributes: []dex.AttributeLock{statusLock, executorLock}, LoadChannels: []dex.ChannelDef{remoteInboxChannel}}),
		dex.DefineRPC(flow.GetDexSummary, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexDisplay, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.CompleteProjectSession, &dex.RPCOptions{Timeout: commandRPCTimeout, LockAttributes: []dex.AttributeLock{statusLock, executorLock, dex.LockAttribute(sessionRevisionAttribute)},
			LoadAttributeMaps: []dex.AttributeDef{backgroundCommandsAttribute}}),
		dex.DefineRPC(flow.RequestSessionWorkspace, dexflow.ShortRPC(statusLock, workspaceLock)),
		dex.DefineRPC(flow.RenewSessionWorkspace, dexflow.ShortRPC(statusLock, workspaceLock)),
	}
}

// GetPersistenceSchema registers every durable value and best-effort Stream.
func (*SessionAgentFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{
			projectIDAttribute, sessionProfileAttribute, sessionStartIdentityAttribute, headCommitAttribute,
			sessionStatusAttribute, closeReasonAttribute, sessionRevisionAttribute, sessionCreatedAtAttribute, sessionUpdatedAtAttribute,
			sessionModelAttribute, executorAttribute, agentStateAttribute, messageWindowLockAttribute, waitingInputRoundAttribute,
			contextSegmentAttribute, contextSegmentPromptsAttribute, currentMessagesAttribute, archivedMessagesAttribute, todoListAttribute,
			confirmedRequirementsAttribute, flowDesignsAttribute, builtFlowDesignAttribute, builtFlowDesignCommitAttribute, pendingUserInputAttribute, pendingApprovalAttribute, pendingTimerAttribute,
			workspaceAttribute, workspaceUseAttribute,
			agentInterruptAttribute, notificationReceiptsAttribute, backgroundCommandsAttribute, subagentsAttribute, previewHandoffAttribute, previewAutoDeployAttribute, previewAutoDeployChangedByAttribute,
		},
		Channels: []dex.ChannelDef{
			queuedUserMessagesChannel, steeredUserMessagesChannel, answeredUserInputsChannel, remoteInboxChannel,
			toolApprovalsChannel, parallelToolResultsChannel, sessionNotificationsChannel, backgroundCommandStopsChannel, workspaceOutcomesChannel,
		},
		Streams: []dex.StreamDef{reasoningSummaryStream, assistantTextStream, agentActivityStream},
	}
}

// Conversation Steps lock MessageWindowLock and the interactions they settle; Steps that admit
// tool use add Workspace. Tool Steps lock nothing: locks span retries and fail RPCs fast.
var (
	messageMaps      = []dex.AttributeDef{currentMessagesAttribute}
	statusLock       = dex.LockAttribute(sessionStatusAttribute)
	executorLock     = dex.LockAttribute(executorAttribute)
	inputLock        = dex.LockAttribute(pendingUserInputAttribute)
	requirementsLock = dex.LockAttribute(confirmedRequirementsAttribute)
	approvalLock     = dex.LockAttribute(pendingApprovalAttribute)
	workspaceLock    = dex.LockAttribute(workspaceAttribute)
	interruptLock    = dex.LockAttribute(agentInterruptAttribute)
	receiptsLock     = dex.LockAttribute(notificationReceiptsAttribute)
	taskMaps         = []dex.AttributeDef{backgroundCommandsAttribute, subagentsAttribute}
	shortStepRetry   = dexflow.StepRetry(2 * time.Minute)
	// Steps that admit, stop, fail or forget a subagent's start load its records; a STARTING
	// record whose subagent may have started is written only under the AgentInterrupt lock.
	taskMessageMaps = []dex.AttributeDef{currentMessagesAttribute, subagentsAttribute, backgroundCommandsAttribute}

	// The Steps that consume notifications lock Workspace, whose deadline a
	// task's end extends, and AgentInterrupt, which applies a Stop.
	messageMutationStepOptions      = conversationStepOptions(messageMaps, inputLock, approvalLock, executorLock)
	finishInterruptStepOptions      = conversationStepOptions(taskMessageMaps, inputLock, approvalLock, interruptLock, executorLock)
	awaitUserStepOptions            = conversationStepOptions(messageMaps, inputLock, approvalLock, workspaceLock, interruptLock, executorLock)
	messageContextStepOptions       = conversationStepOptions(taskMessageMaps, inputLock, interruptLock, executorLock)
	checkSteeredStepOptions         = conversationStepOptions(taskMessageMaps, inputLock, workspaceLock, interruptLock, executorLock)
	routeToolStepOptions            = conversationStepOptions(taskMessageMaps, inputLock, approvalLock, workspaceLock, interruptLock, executorLock)
	recordToolFailureStepOptions    = conversationStepOptions(taskMessageMaps, interruptLock, executorLock)
	parallelToolRecoveryStepOptions = &dex.StepOptions{WaitForMethodTimeout: time.Minute, ExecuteMethodTimeout: time.Minute}
	timerStepOptions                = &dex.StepOptions{ExecuteLoadAttributeMaps: messageMaps, ExecuteLockAttributes: messageMutationStepOptions.ExecuteLockAttributes}

	// A background command's end is recorded under its notification's receipts lock, by one writer at a
	// time; a failed WaitFor records it lost, exhausted retries AbandonBackgroundCommand.
	abandonBackgroundCommandStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second, ExecuteRetry: shortStepRetry, ExecuteLoadAttributeMaps: taskMaps}
	backgroundExitStepOptions           = &dex.StepOptions{WaitForFailure: dex.ProceedOnWaitForFailure, ExecuteMethodTimeout: 30 * time.Second, ExecuteRetry: shortStepRetry,
		ExecuteLoadAttributeMaps: taskMaps, ExecuteLockAttributes: []dex.AttributeLock{receiptsLock}}

	// The lifecycle Steps lock SessionStatus.
	sessionLifecycleStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteRetry: shortStepRetry, ExecuteLockAttributes: []dex.AttributeLock{statusLock}}
)

// SendMessage queues a user message unless a question awaits its answers.
func (*SessionAgentFlow) SendMessage(ctx dex.Context, input conversation.PendingUserMessage) (*dex.RPCResult[bool], error) {
	return sendMessage(ctx, input)
}

// SteerMessage moves a queued message into the Steer queue, which the running
// turn applies at its next safe boundary.
func (*SessionAgentFlow) SteerMessage(ctx dex.Context, input conversation.SteerMessageRequest) (*dex.RPCResult[bool], error) {
	return steerMessage(ctx, input)
}

// DeleteQueuedMessage removes one exact queued user message.
func (*SessionAgentFlow) DeleteQueuedMessage(ctx dex.Context, input conversation.DeleteQueuedMessageRequest) (*dex.RPCResult[queuedMessageDeletion], error) {
	return deleteQueuedMessage(ctx, input)
}

// AnswerQuestions answers the pending input in one commit (a question batch or a
// requirements card); a confirmation stores ConfirmedRequirements in that commit.
func (*SessionAgentFlow) AnswerQuestions(ctx dex.Context, input agentloop.AnswerQuestionsRequest) (*dex.RPCResult[bool], error) {
	pending := dexflow.OptionalValue(pendingUserInputAttribute.Get(ctx))
	if !acceptsUserCommand(ctx) || pending == nil || pending.CallID != input.CallID {
		return rpcAccepted(false), nil
	}
	stored := dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx))
	answer, isValid := agentloop.AnswerPendingInput(*pending, input, stored, time.Now().UTC())
	if !isValid {
		return rpcAccepted(false), nil
	}
	comm.MustDo(pendingUserInputAttribute.Delete(ctx))
	if answer.Confirmed != nil {
		confirmFlowDesign(ctx, stored, *pending.Requirements, answer.Confirmed.Spec.Design)
		comm.MustDo(confirmedRequirementsAttribute.Set(ctx, *answer.Confirmed))
	}
	comm.MustDo(answeredUserInputsChannel.Publish(ctx, agentloop.AnsweredUserInput{CallID: pending.CallID, Message: answer.Message, Description: answer.Description}))
	return rpcAccepted(true), nil
}

// ApproveTool decides the approval of the pending call with this exact ID.
func (*SessionAgentFlow) ApproveTool(ctx dex.Context, input agentloop.ToolApprovalRequest) (*dex.RPCResult[bool], error) {
	if pending := dexflow.OptionalValue(pendingApprovalAttribute.Get(ctx)); !acceptsUserCommand(ctx) || pending == nil || pending.CallID != input.CallID {
		return rpcAccepted(false), nil
	}
	comm.MustDo(pendingApprovalAttribute.Delete(ctx))
	comm.MustDo(toolApprovalsChannel.Publish(ctx, string(input.CallID), agentloop.ToolApproval{Approved: input.Approved}))
	return rpcAccepted(true), nil
}

// UpdateSessionModel selects the Session's model at an exact revision at any time.
// The next turn rolls the context over to it; history, pending input and workspace stay.
func (*SessionAgentFlow) UpdateSessionModel(ctx dex.Context, input agentloop.UpdateSessionModelRequest) (*dex.RPCResult[modelSelection], error) {
	if !acceptsUserCommand(ctx) {
		return &dex.RPCResult[modelSelection]{}, nil
	}
	current := comm.Must(sessionModelAttribute.Get(ctx))
	selected, isAccepted := agentloop.ChangeSessionModel(current, input)
	if selected != current {
		comm.MustDo(sessionModelAttribute.Set(ctx, selected))
	}
	return &dex.RPCResult[modelSelection]{Output: modelSelection{Accepted: isAccepted, Stale: !isAccepted}}, nil
}

// UpdatePreviewAutoDeploy turns the Preview's deployment of each pushed commit on or off
// (Session Service only); a handoff published while it was off is never deployed later.
func (*SessionAgentFlow) UpdatePreviewAutoDeploy(ctx dex.Context, input agentloop.PreviewAutoDeployRequest) (*dex.RPCResult[bool], error) {
	return updatePreviewAutoDeploy(ctx, input)
}

// InterruptAgent is the Stop button for the turn the user saw (admitInterrupt): its Steps
// stop the tasks; the turn ends at once if its work is cancellable, else at CheckSteered's next boundary.
func (*SessionAgentFlow) InterruptAgent(ctx dex.Context, input agentloop.InterruptAgentRequest) (*dex.RPCResult[bool], error) {
	interruption, plan, err := interruptAgent(ctx, input)
	if err != nil {
		return nil, err
	}
	if !plan.Admitted || plan.Joined {
		return rpcAccepted(plan.Admitted), nil
	}
	if interruption.EndsTurn {
		// Keep the cancellation set identical to SetExecutor below.
		return (&dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{dex.MovementOf(InterruptAgent{}, interruption, dex.WithStepOptions(interruptMovementOptions(interruption)))}}).CancelSteps(CompactContext{}, CallModel{}, RecordPlatformModelResult{}, RecordModelFailure{}, RecordCompactionFailure{}, ExecuteTool{}, ExecuteParallelTool{},
			RecoverParallelToolExecution{}, AwaitParallelToolResults{}, RecordToolFailure{}, SettleForegroundCommand{}, AwaitAgentTimer{}, AwaitToolApproval{}, AwaitRemoteAgent{}), nil
	}
	return &dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{dex.MovementOf(InterruptAgent{}, interruption, dex.WithStepOptions(interruptMovementOptions(interruption)))}}, nil
}

// ReportSessionNotification admits one notification (Session Service only): true when published or a
// duplicate ID, false when not ACTIVE or 32 are pending. A ready Preview's hold ignores a full Channel.
func (flow *SessionAgentFlow) ReportSessionNotification(ctx dex.Context, notification conversation.SessionNotification) (*dex.RPCResult[bool], error) {
	if err := prepareSessionNotification(ctx, notification); err != nil {
		return nil, err
	}
	if !acceptsUserCommand(ctx) {
		return rpcAccepted(false), nil
	}
	if notification.IsPreviewReady() {
		holdSandbox(ctx, notification.ResourceID, flow.previewHold)
	}
	return rpcAccepted(publishNotification(ctx, notification, dexflow.ValueOrZero(notificationReceiptsAttribute.Get(ctx)))), nil
}

// ReportWorkspaceOutcome admits an outcome of the SessionWorkspaceFlow (Session Service only): true
// for a reclamation, a repeat or the request an ACTIVE Session waits for (admitWorkspaceOutcome).
func (*SessionAgentFlow) ReportWorkspaceOutcome(ctx dex.Context, report agentloop.WorkspaceReport) (*dex.RPCResult[bool], error) {
	return rpcAccepted(acceptsUserCommand(ctx) && admitWorkspaceOutcome(ctx, report)), nil
}

// AdmitSubagent admits a subagent's first Step against its STARTING record (Session Service
// only, record locked and loaded): it becomes RUNNING while ACTIVE with no Stop pending, else denied.
func (*SessionAgentFlow) AdmitSubagent(ctx dex.Context, admission agenttasks.SubagentAdmission) (*dex.RPCResult[agenttasks.SubagentAdmissionAnswer], error) {
	return admitSubagent(ctx, admission)
}

// ReadExecutor returns the scoped, Session-owned executor admission view.
func (flow *SessionAgentFlow) ReadExecutor(ctx dex.Context, input agentloop.SessionScopeInput) (*dex.RPCResult[remoteexec.Executor], error) {
	return readExecutor(ctx, input)
}

// GetSnapshot reads the Session and its conversation (agentSnapshot) in one invocation,
// consuming no Channel; the conversation is absent until InitializeSessionAgent commits it.
func (flow *SessionAgentFlow) GetSnapshot(ctx dex.Context, _ dex.None) (*dex.RPCResult[agentloop.SessionSnapshot], error) {
	return flow.getSessionSnapshot(ctx)
}

// GetArchivedMessages reads the retained archive chunk that ends before the
// given sequence; Found is false for a chunk no longer retained.
func (*SessionAgentFlow) GetArchivedMessages(ctx dex.Context, before conversation.Sequence) (*dex.RPCResult[archivedMessagesRPCOutput], error) {
	return getArchivedMessages(ctx, before)
}

// GetFlowDesign reads the design a slot of FlowDesigns holds; Found is false
// when it holds none.
func (*SessionAgentFlow) GetFlowDesign(ctx dex.Context, slot flowdesign.Slot) (*dex.RPCResult[flowDesignRPCOutput], error) {
	return getFlowDesign(ctx, slot)
}

// GetSessionSummary is the lifecycle read for listing, admission and cleanup. It holds the only
// direct reads of StartIdentity and HeadCommit, which the pinned Dex CLI requires of each.
func (*SessionAgentFlow) GetSessionSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[agentloop.SessionSummary], error) {
	comm.Must(sessionStartIdentityAttribute.Get(ctx))
	status := comm.Must(sessionStatusAttribute.Get(ctx))
	summary := agentloop.SessionSummary{
		Identity: agentloop.SessionIdentity{ProjectID: comm.Must(projectIDAttribute.Get(ctx)), Profile: comm.Must(sessionProfileAttribute.Get(ctx)),
			HeadCommit: comm.Must(headCommitAttribute.Get(ctx))},
		Status: status, CloseReason: comm.Must(closeReasonAttribute.Get(ctx)), Revision: comm.Must(sessionRevisionAttribute.Get(ctx)),
		CreatedAt: comm.Must(sessionCreatedAtAttribute.Get(ctx)), UpdatedAt: comm.Must(sessionUpdatedAtAttribute.Get(ctx)),
		Workspace: dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).Summary(),
	}
	// WorkspaceUse is missing until InitializeSessionAgent commits; only a value read proves a drained wave.
	use, err := workspaceUseAttribute.Get(ctx)
	summary.WorkspaceUse, summary.WorkspaceUseKnown = dexflow.ValueOrZero(use, err), err == nil
	return &dex.RPCResult[agentloop.SessionSummary]{Output: summary}, nil
}

// GetDexSummary is the Session row of the Dex Web Flow list, which shows the
// indexed Project already.
//
// dex:field attribute-key:HeadCommit value-type:string editable:false description:"Repository HEAD the Agent works on"
func (*SessionAgentFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"HeadCommit": comm.Must(headCommitAttribute.Get(ctx))}}, nil
}

// GetDexDisplay is the Session header of the Dex Web Flow view.
//
// dex:field attribute-key:ProjectID value-type:string editable:false description:"Owning project"
// dex:field attribute-key:Profile value-type:string editable:false description:"Coding profile"
// dex:field attribute-key:CloseReason value-type:string editable:false description:"Why the session closed"
func (*SessionAgentFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"ProjectID": comm.Must(projectIDAttribute.Get(ctx)), "Profile": comm.Must(sessionProfileAttribute.Get(ctx)), "CloseReason": comm.Must(closeReasonAttribute.Get(ctx)),
	}}, nil
}

// CompleteProjectSession is the Session's only close, for a deleting Project: it cancels the Agent's
// branches and any workspace command, stops each background command's watch, then archives and completes.
func (*SessionAgentFlow) CompleteProjectSession(ctx dex.Context, input agentloop.SessionScopeInput) (*dex.RPCResult[bool], error) {
	if comm.Must(projectIDAttribute.Get(ctx)) != input.ProjectID {
		return nil, errors.New("project cleanup scope mismatch")
	}
	if comm.Must(sessionStatusAttribute.Get(ctx)) == agentloop.SessionStatusArchived && comm.Must(closeReasonAttribute.Get(ctx)) == agentloop.CloseReasonProjectCleanupComplete.String() {
		return rpcAccepted(true), nil
	}
	setSessionStatus(ctx, agentloop.SessionStatusArchived, agentloop.CloseReasonProjectCleanupComplete.String())
	stopBackgroundCommandWatches(ctx)
	return (&dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{dex.MovementOf(CompleteProjectSession{}, nil)}}).CancelSteps(
		ActivateSession{}, AwaitUser{}, AwaitRemoteAgent{}, AnsweredInput{}, CompactContext{}, CallModel{}, RecordPlatformModelResult{}, RecordModelFailure{}, RecordCompactionFailure{}, CheckSteered{}, RouteTool{}, AwaitToolApproval{}, ExecuteTool{}, ExecuteParallelTool{}, RecoverParallelToolExecution{}, AwaitParallelToolResults{}, RecordToolFailure{}, SettleForegroundCommand{}, ReportPlatformIssue{}, AbandonPlatformIssue{}, InterruptAgent{}, FinishInterrupt{}, AwaitAgentTimer{}, RequestSessionWorkspace{}, SendWorkspaceCommand{}, AwaitSessionWorkspace{}, ApplySessionWorkspace{}, RecordWorkspaceFailure{}, AwaitSandboxDeadline{},
		DeployPreviewHandoff{}, AbandonPreviewHandoff{}), nil
}

// RequestSessionWorkspace asks for a workspace outside the conversation (the Preview target,
// the Preview, a subagent): a READY instance is lent, else the cold path opens a request (PlanDemand).
func (flow *SessionAgentFlow) RequestSessionWorkspace(ctx dex.Context, input agentloop.WorkspaceDemandRequest) (*dex.RPCResult[workspaceDemandResult], error) {
	if !acceptsUserCommand(ctx) || input.ForAgentID != "" && !isSubagentRunning(ctx, input.ForAgentID) {
		return &dex.RPCResult[workspaceDemandResult]{Output: workspaceDemandResult{Closed: true}}, nil
	}
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	plan := ws.PlanDemand(input)
	if isExtended := plan.Lent && ws.ExtendDeadline(time.Now(), flow.sandboxHold); plan.Recorded || isExtended {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
	output := workspaceDemandResult{Summary: ws.Summary(), Lent: plan.Lent, Lease: plan.Lease}
	if !plan.Opens {
		return &dex.RPCResult[workspaceDemandResult]{Output: output}, nil
	}
	return &dex.RPCResult[workspaceDemandResult]{Output: output, NextSteps: []dex.StepMovement{dex.MovementOf(RequestSessionWorkspace{}, workspaceDemand{})}}, nil
}

// RenewSessionWorkspace is the user's "Keep running" (Session Service only): it moves the READY
// instance's Sandbox deadline to at least now + agentloop.KeepRunningHold, else answers false.
func (*SessionAgentFlow) RenewSessionWorkspace(ctx dex.Context, _ dex.None) (*dex.RPCResult[bool], error) {
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	if !acceptsUserCommand(ctx) || ws.State != agentloop.WorkspaceReady {
		return rpcAccepted(false), nil
	}
	if ws.ExtendDeadline(time.Now(), agentloop.KeepRunningHold) {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
	return rpcAccepted(true), nil
}

// dex:group group-id:session group-label:"Session"
// dex:explanation text:"Admit the Session, then initialize the Agent state, the entry log and an empty workspace."
type InitializeSessionAgent struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SessionAgentFlow
}

func (InitializeSessionAgent) GetStepOptions() *dex.StepOptions {
	return lifecycleStepOptions(nil, statusLock, workspaceLock)
}

// Execute admits the Session with its owner; the execution exists before the admission, so a
// lost start response never leaves cleanup membership without an execution to reconcile.
func (step InitializeSessionAgent) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	identity := readSessionIdentity(ctx)
	admitted, err := step.flow.dependency.AdmitSession(ctx, identity)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("admit the Session to Project %s: %w", identity.ProjectID, err))
	}
	if !admitted {
		setSessionStatus(ctx, agentloop.SessionStatusArchived, agentloop.CloseReasonAdmissionDenied.String())
		summary := comm.Must(step.flow.GetSessionSummary(ctx, nil)).Output
		return dex.GracefulComplete(agentloop.SessionCleanupReceipt{ProjectID: identity.ProjectID, Reason: agentloop.CloseReasonAdmissionDenied.String(), Snapshot: &summary}), nil
	}
	// MessageWindowLock holds no information, as Steps and RPCs only lock it; this write and the
	// WaitingInputRound write are their only direct uses, which the pinned Dex CLI requires.
	comm.MustDo(messageWindowLockAttribute.Set(ctx, false))
	comm.MustDo(agentStateAttribute.Set(ctx, agentloop.NewAgentState()))
	comm.MustDo(waitingInputRoundAttribute.Set(ctx, 0))
	// No workspace until a demand provisions one; automatic deployment is on.
	comm.MustDo(workspaceAttribute.Set(ctx, agentloop.SessionWorkspace{}))
	comm.MustDo(workspaceUseAttribute.Set(ctx, agentloop.WorkspaceUse{}))
	comm.MustDo(previewAutoDeployAttribute.Set(ctx, true))
	return dex.GoTo(ActivateSession{}, nil), nil
}

// dex:group group-id:session group-label:"Session"
// dex:explanation text:"Begin consuming accepted input."
type ActivateSession struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (ActivateSession) GetStepOptions() *dex.StepOptions { return sessionLifecycleStepOptions }

func (ActivateSession) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if !acceptsUserCommand(ctx) {
		return dex.DeadEnd(), nil
	}
	state := comm.Must(agentStateAttribute.Get(ctx))
	state.Status = agentloop.AgentStatusWaitingForMessage
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	return dex.GoTo(AwaitUser{}, nil), nil
}

// dex:group group-id:conversation group-label:"Conversation"
// dex:explanation text:"Wait for queued input, steering, the answers to a pending question or a notification; a notification may open an automatic turn."
type AwaitUser struct {
	dex.StepDefaults
	flow *SessionAgentFlow
}

func (AwaitUser) GetStepOptions() *dex.StepOptions { return remoteIdleStepOptions() }

// WaitFor waits for the answers to pending questions or, without them, for a steered or
// queued message, last for notifications; a wait with nothing pending advances WaitingInputRound.
func (AwaitUser) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	setAgentStatus(ctx, agentloop.AgentStatusWaitingForMessage)
	if currentExecutor(ctx).Mode == remoteexec.ModeRemote && dexflow.OptionalValue(pendingUserInputAttribute.Get(ctx)) == nil {
		if answeredUserInputsChannel.Size(ctx) > 0 {
			return dex.Until(answeredUserInputsChannel.ForOne()), nil
		}
		return dex.AnyOf(remoteInboxChannel.ForN(remoteHeadCount(ctx)), steeredUserMessagesChannel.AtLeastAtMost(1, agentloop.AllPendingChannelMessages), queuedUserMessagesChannel.ForOne(), sessionNotificationsChannel.AtLeastAtMost(1, agentloop.NotificationBatchSize)), nil
	}
	if answeredUserInputsChannel.Size(ctx) > 0 {
		return dex.Until(answeredUserInputsChannel.ForOne()), nil
	}
	if dexflow.OptionalValue(pendingUserInputAttribute.Get(ctx)) != nil {
		incrementWaitingInputRound(ctx)
		return dex.AnyOf(answeredUserInputsChannel.ForOne(), remoteInboxChannel.ForN(remoteHeadCount(ctx))), nil
	}
	if steeredUserMessagesChannel.Size(ctx) == 0 && queuedUserMessagesChannel.Size(ctx) == 0 && sessionNotificationsChannel.Size(ctx) == 0 {
		incrementWaitingInputRound(ctx)
	}
	return dex.AnyOf(steeredUserMessagesChannel.AtLeastAtMost(1, agentloop.AllPendingChannelMessages), queuedUserMessagesChannel.ForOne(),
		sessionNotificationsChannel.AtLeastAtMost(1, agentloop.NotificationBatchSize), remoteInboxChannel.ForN(remoteHeadCount(ctx))), nil
}

// Execute starts the turn of the user's input; notifications are recorded and open an
// automatic turn while the bounds allow it. A Stop that no turn applied pauses them.
func (step AwaitUser) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	changed := reconcileExecutorWake(ctx)
	isStopped := takeAgentInterrupt(ctx, true) != nil
	if answered := comm.Must(answeredUserInputsChannel.GetConditionResults(ctx)); len(answered) > 0 {
		sequence := beginUserTurn(ctx, answered[0].Message)
		writeActivity(ctx, conversation.AgentEvent{Kind: conversation.EventKindUserInputAnswered, Message: answered[0].Description, CallID: &answered[0].CallID, MessageSequence: &sequence})
		return dex.GoTo(AnsweredInput{}, nil), nil
	}
	settleExecutorQuestion(ctx, changed)
	if reports := comm.Must(remoteInboxChannel.GetConditionResults(ctx)); len(reports) > 0 {
		next := step.flow.consumeRemote(ctx, reports, true)
		if next == "" {
			return dex.GoTo(AwaitUser{}, nil), nil
		}
		return dex.GoTo(CheckSteered{}, next), nil
	}
	if steered := comm.Must(steeredUserMessagesChannel.GetConditionResults(ctx)); len(steered) > 0 {
		beginSteeredTurn(ctx, steered)
		return dex.GoTo(CheckSteered{}, agentloop.ContinueStartTurn), nil
	}
	if queued := comm.Must(queuedUserMessagesChannel.GetConditionResults(ctx)); len(queued) > 0 {
		sequence := beginUserTurn(ctx, queued[0].Value)
		writeInputConsumption(ctx, queued[:1], nil, &sequence)
		return dex.GoTo(CheckSteered{}, agentloop.ContinueStartTurn), nil
	}
	// Otherwise the notification wait completed.
	if step.flow.consumeIdleNotifications(ctx, comm.Must(sessionNotificationsChannel.GetConditionResults(ctx)), isStopped) {
		return dex.GoTo(CheckSteered{}, agentloop.ContinueStartTurn), nil
	}
	return dex.GoTo(AwaitUser{}, nil), nil
}

// AnsweredInput starts the model turn of the answers without checking either
// message queue, as the question-answer contract requires.
//
// dex:group group-id:conversation group-label:"Conversation"
// dex:explanation text:"Start the model turn of the answers to the pending user question."
type AnsweredInput struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SessionAgentFlow
}

func (AnsweredInput) GetStepOptions() *dex.StepOptions { return messageContextStepOptions }

func (step AnsweredInput) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	switch plan := step.flow.planModelCall(ctx, agentloop.ContinueStartTurn); plan.next {
	case nextLoadExecutorContext:
		return dex.GoTo(AnsweredInput{}, nil, dex.WithStepOptions(plan.load)), nil
	case nextAwaitRemote:
		return dex.GoTo(AwaitRemoteAgent{}, nil), nil
	case nextRequestWorkspace:
		return dex.GoTo(RequestSessionWorkspace{}, workspaceDemand{ResumeAt: agentloop.ContinueStartTurn}), nil
	case nextCompactContext:
		return dex.GoTo(CompactContext{}, plan.compaction, dex.WithStepOptions(plan.load)), nil
	default:
		return dex.GoTo(CallModel{}, plan.admission, dex.WithStepOptions(plan.load)), nil
	}
}

// dex:group group-id:conversation group-label:"Conversation"
// dex:explanation text:"Apply the user's Stop, notifications and steering at a safe boundary before continuing."
type CheckSteered struct {
	dex.StepDefaults
	flow *SessionAgentFlow
}

func (CheckSteered) GetStepOptions() *dex.StepOptions { return checkSteeredStepOptions }

// WaitFor takes every steered message and, before a model continuation inside the turn or
// with steering, at most 16 notifications, which precede the steered text; a Stop takes none.
func (CheckSteered) WaitFor(ctx dex.Context, next agentloop.Continuation) (*dex.Wait, error) {
	state, interrupt := comm.Must(agentStateAttribute.Get(ctx)), dexflow.OptionalValue(agentInterruptAttribute.Get(ctx))
	steered := steeredUserMessagesChannel.AtMost(agentloop.AllPendingChannelMessages)
	if agentloop.TakesNotifications(next, steeredUserMessagesChannel.Size(ctx) > 0, interrupt != nil && interrupt.Turn == state.Turn) {
		return dex.AllOf(steered, sessionNotificationsChannel.AtMost(agentloop.NotificationBatchSize)), nil
	}
	return dex.Until(steered), nil
}

// Execute applies, in order, the user's Stop of the turn, the notifications
// and steering, then continues the turn.
func (step CheckSteered) Execute(ctx dex.Context, next agentloop.Continuation) (*dex.StepDecision, error) {
	steered := comm.Must(steeredUserMessagesChannel.GetConditionResults(ctx))
	if interrupt := takeAgentInterrupt(ctx, false); interrupt != nil {
		// The Stop ends the turn at this boundary; steering sent since starts the next one.
		endStoppedTurn(ctx, nil, interrupt)
		comm.MustDo(agentInterruptAttribute.Delete(ctx))
		if len(steered) == 0 {
			return dex.GoTo(AwaitUser{}, nil), nil
		}
	}
	step.flow.appendNotifications(ctx, comm.Must(sessionNotificationsChannel.GetConditionResults(ctx)))
	if len(steered) > 0 {
		abandonPendingSubagentStart(ctx)
		beginSteeredTurn(ctx, steered)
		next = agentloop.ContinueStartTurn
	}
	if (next == agentloop.ContinueRouteTool || next == agentloop.ContinueExecuteTool) && rejectStaleToolBatch(ctx) {
		return dex.GoTo(AwaitUser{}, nil), nil
	}
	switch next {
	case agentloop.ContinueStartTurn, agentloop.ContinueCompactContext, agentloop.ContinueCallModel:
		// The first segment opens once the repository context is read; the cold path resumes
		// this continuation, so a workspace that cannot be prepared only delays the model.
		switch plan := step.flow.planModelCall(ctx, next); plan.next {
		case nextLoadExecutorContext:
			return dex.GoTo(CheckSteered{}, next, dex.WithStepOptions(plan.load)), nil
		case nextAwaitRemote:
			return dex.GoToMany(dex.MovementOf(AwaitRemoteAgent{}, nil)), nil
		case nextRequestWorkspace:
			return dex.GoTo(RequestSessionWorkspace{}, workspaceDemand{ResumeAt: next}), nil
		case nextCompactContext:
			return dex.GoTo(CompactContext{}, plan.compaction, dex.WithStepOptions(plan.load)), nil
		default:
			return dex.GoTo(CallModel{}, plan.admission, dex.WithStepOptions(plan.load)), nil
		}
	case agentloop.ContinueAwaitUser:
		return dex.GoTo(AwaitUser{}, nil), nil
	case agentloop.ContinueRouteTool:
		return dex.GoToMany(dex.MovementOf(RouteTool{}, nil)), nil
	case agentloop.ContinueAwaitToolApproval:
		setRunningWork(ctx, &agentloop.RunningWork{Kind: agentloop.RunningWait})
		return dex.GoToMany(dex.MovementOf(AwaitToolApproval{}, nil)), nil
	case agentloop.ContinueAwaitTimer:
		setRunningWork(ctx, &agentloop.RunningWork{Kind: agentloop.RunningWait})
		return dex.GoToMany(dex.MovementOf(AwaitAgentTimer{}, nil)), nil
	case agentloop.ContinueExecuteTool:
		call := agentloop.PendingToolCall(comm.Must(agentStateAttribute.Get(ctx)))
		if agentloop.CatalogTool(call.Name).UsesWorkspace() && !step.flow.admitWorkspaceUse(ctx) {
			return dex.GoTo(RequestSessionWorkspace{}, workspaceDemand{ResumeAt: agentloop.ContinueRouteTool}), nil
		}
		setAgentStatus(ctx, agentloop.AgentStatusExecutingTool)
		setRunningWork(ctx, &agentloop.RunningWork{Kind: agentloop.RunningTool, ToolName: call.Name})
		state := comm.Must(agentStateAttribute.Get(ctx))
		return dex.GoToMany(dex.MovementOf(ExecuteTool{}, toolInvocation{Index: state.PendingToolIndex, Call: call, Admission: state.PendingToolsAdmission, Turn: state.Turn}, dex.WithStepOptions(serialToolStepOptions(call)))), nil
	}
	panic(fmt.Sprintf("unknown Agent continuation %q", next))
}

// dex:group group-id:conversation group-label:"Conversation"
// dex:explanation text:"Wait durably for the requested timer or steering input."
type AwaitAgentTimer struct {
	dex.StepDefaults
}

func (AwaitAgentTimer) GetStepOptions() *dex.StepOptions { return timerStepOptions }

// WaitFor waits for the timer or steering.
func (AwaitAgentTimer) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	timer := comm.Must(pendingTimerAttribute.Get(ctx))
	setAgentStatus(ctx, agentloop.AgentStatusWaitingForTimer)
	writeSnapshotRequired(ctx)
	return dex.AnyOf(dex.Timer(time.Duration(timer.DurationSeconds)*time.Second), steeredUserMessagesChannel.AtLeastAtMost(1, agentloop.AllPendingChannelMessages)), nil
}

func (AwaitAgentTimer) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	setRunningWork(ctx, nil)
	state, timer := comm.Must(agentStateAttribute.Get(ctx)), comm.Must(pendingTimerAttribute.Get(ctx))
	call := agentloop.PendingToolCall(state)
	comm.MustDo(pendingTimerAttribute.Delete(ctx))
	if steered := comm.Must(steeredUserMessagesChannel.GetConditionResults(ctx)); len(steered) > 0 {
		writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindTimerResolved, "Durable timer interrupted.", call))
		appendToolResult(ctx, call, agentloop.CancelledToolResult(agentloop.CancelledBySteering), agentloop.ToolMessageTiming{})
		interruptPendingTools(ctx, state.PendingToolIndex+1, agentloop.CancelledBySteering)
		beginSteeredTurn(ctx, steered)
		return dex.GoTo(CheckSteered{}, agentloop.ContinueStartTurn), nil
	}
	writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindTimerResolved, "Durable timer completed.", call))
	appendSessionToolResult(ctx, call, agentloop.ToolResultPayload{Status: agentloop.ToolResultStatusCompleted, DurationSeconds: timer.DurationSeconds, Reason: timer.Reason})
	return dex.GoTo(CheckSteered{}, advancePendingTools(ctx, 1)), nil
}

// dex:group group-id:model group-label:"Model"
// dex:explanation text:"Summarize the context segment and open the next one that starts with the summary."
type CompactContext struct {
	dex.StepDefaultsNoWaitFor[contextCompaction]
	flow *SessionAgentFlow
}

func (CompactContext) GetStepOptions() *dex.StepOptions {
	options := *modelStepOptions
	options.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordCompactionFailure{}, modelFailureStepOptions)
	return &options
}

func (step CompactContext) Execute(ctx dex.Context, input contextCompaction) (*dex.StepDecision, error) {
	outcome, loads, err := step.flow.completeCompaction(ctx, input)
	if err != nil {
		return nil, err
	}
	if outcome == nil {
		return dex.DeadEnd(), nil
	}
	return dex.GoTo(RecordPlatformModelResult{}, *outcome, dex.WithStepOptions(loads)), nil
}

// dex:group group-id:model group-label:"Model"
// dex:explanation text:"Record a provider outcome only under the originally admitted executor and turn."
type RecordPlatformModelResult struct {
	dex.StepDefaultsNoWaitFor[platformModelOutcome]
	flow *SessionAgentFlow
}

func (RecordPlatformModelResult) GetStepOptions() *dex.StepOptions { return modelFailureStepOptions }

func (step RecordPlatformModelResult) Execute(ctx dex.Context, input platformModelOutcome) (*dex.StepDecision, error) {
	record := step.flow.recordPlatformModelResult(ctx, input)
	switch {
	case record.discard:
		return dex.DeadEnd(), nil
	case record.compaction != nil:
		return dex.GoTo(CompactContext{}, *record.compaction, dex.WithStepOptions(segmentLoad(comm.Must(agentStateAttribute.Get(ctx)), comm.Must(contextSegmentAttribute.Get(ctx)), true))), nil
	case record.incident != nil:
		return dex.GoToMany(dex.MovementOf(CheckSteered{}, record.next), dex.MovementOf(ReportPlatformIssue{}, *record.incident)), nil
	default:
		return dex.GoTo(CheckSteered{}, record.next), nil
	}
}

// RecordCompactionFailure receives a CompactContext whose retries are
// exhausted.
//
// dex:group group-id:model group-label:"Model"
// dex:explanation text:"Keep the old segment after a failed compaction, or open an unsummarized one at the hard limit, and continue the turn."
type RecordCompactionFailure struct {
	dex.StepDefaultsNoWaitFor[contextCompaction]
	flow *SessionAgentFlow
}

func (RecordCompactionFailure) GetStepOptions() *dex.StepOptions { return modelFailureStepOptions }

func (step RecordCompactionFailure) Execute(ctx dex.Context, input contextCompaction) (*dex.StepDecision, error) {
	if !acceptsUserCommand(ctx) || !modelAdmissionMatches(ctx, input.Admission) {
		return dex.DeadEnd(), nil
	}
	setRunningWork(ctx, nil)
	return dex.GoTo(CheckSteered{}, step.flow.compactionFailed(ctx, input, modelUnavailable.failure(""), false)), nil
}

// dex:group group-id:model group-label:"Model"
// dex:explanation text:"Call the segment's model with the replayed context segment and record the reply by its stop reason."
type CallModel struct {
	dex.StepDefaultsNoWaitFor[modelAdmission]
	flow *SessionAgentFlow
}

func (CallModel) GetStepOptions() *dex.StepOptions {
	options := *modelStepOptions
	options.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordModelFailure{}, modelFailureStepOptions)
	return &options
}

// Execute streams a reply and passes a bounded outcome to fresh fenced recording.
// A rejected request is recorded there; transient transport errors retry here.
func (step CallModel) Execute(ctx dex.Context, input modelAdmission) (*dex.StepDecision, error) {
	if !modelAdmissionMatches(ctx, input) {
		return dex.DeadEnd(), nil
	}
	if input.Turn == 0 {
		input.Turn = comm.Must(agentStateAttribute.Get(ctx)).Turn
	}
	segment := comm.Must(contextSegmentAttribute.Get(ctx))
	request := step.flow.segmentRequest(ctx, agentloop.ModelCallKindResponse, segment, comm.Must(contextSegmentPromptsAttribute.Get(ctx, segment.PromptsKey())))
	assistant, assistantErr := dex.NewBufferedTextStream(ctx, assistantTextStream)
	reasoning, reasoningErr := dex.NewBufferedTextStream(ctx, reasoningSummaryStream)
	if err := errors.Join(assistantErr, reasoningErr); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("open the model text Streams: %w", err))
	}
	// These writes are the text Streams' only direct uses: the pinned Dex CLI draws a Stream only
	// from a Write call in the Step method. A lost write never fails the call (StreamWritten).
	request.WriteAssistant = func(chunk string) error { dexflow.StreamWritten(ctx, assistant.Write(chunk)); return nil }
	request.WriteReasoning = func(chunk string) error { dexflow.StreamWritten(ctx, reasoning.Write(chunk)); return nil }
	outcome, err := step.flow.completeModelCall(ctx, segment, request, input)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(RecordPlatformModelResult{}, outcome), nil
}

// RecordModelFailure receives a CallModel whose retries are exhausted.
//
// dex:group group-id:model group-label:"Model"
// dex:explanation text:"Record the failed model call as a bounded turn failure and wait for the user."
type RecordModelFailure struct {
	dex.StepDefaultsNoWaitFor[modelAdmission]
}

func (RecordModelFailure) GetStepOptions() *dex.StepOptions { return modelFailureStepOptions }

func (RecordModelFailure) Execute(ctx dex.Context, input modelAdmission) (*dex.StepDecision, error) {
	if !acceptsUserCommand(ctx) || !modelAdmissionMatches(ctx, input) {
		return dex.DeadEnd(), nil
	}
	setRunningWork(ctx, nil)
	recordTurnFailure(ctx, modelUnavailable.failure(""))
	return dex.GoTo(CheckSteered{}, agentloop.ContinueAwaitUser), nil
}

// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Route built-in, serial, or parallel tools under the approved policy."
type RouteTool struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SessionAgentFlow
}

func (RouteTool) GetStepOptions() *dex.StepOptions { return routeToolStepOptions }

func (step RouteTool) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if interrupt := takeAgentInterrupt(ctx, false); interrupt != nil {
		endStoppedTurn(ctx, nil, interrupt)
		return dex.GoTo(AwaitUser{}, nil), nil
	}
	if rejectStaleToolBatch(ctx) {
		return dex.GoTo(AwaitUser{}, nil), nil
	}
	setAgentStatus(ctx, agentloop.AgentStatusRoutingTool)
	state, toolNames := comm.Must(agentStateAttribute.Get(ctx)), executorToolNames(ctx)
	call := agentloop.PendingToolCall(state)
	definition, isOffered := agentloop.SegmentTool(toolNames, call.Name)
	if currentExecutor(ctx).Mode == remoteexec.ModeRemote && !remoteToolAllowed(call) {
		isOffered = false
	}
	if !isOffered {
		refuseToolCall(ctx, call)
		return dex.GoTo(CheckSteered{}, advancePendingTools(ctx, 1)), nil
	}
	if definition.IsBuiltIn() {
		// A timer waits, a question ends the turn like a reply does, and any
		// other built-in call has its result.
		next := routeBuiltInTool(ctx, call, state)
		if next == "" {
			next = advancePendingTools(ctx, 1)
		}
		return dex.GoTo(CheckSteered{}, next), nil
	}
	if call.Name == conversation.ToolStartAgent && !admitSubagentCall(ctx, call) {
		return dex.GoTo(CheckSteered{}, advancePendingTools(ctx, 1)), nil
	}
	if batch, needsWorkspace := agentloop.ParallelToolBatch(toolNames, state); len(batch.Records) > 0 {
		if needsWorkspace && !step.flow.admitWorkspaceUse(ctx) {
			return dex.GoTo(RequestSessionWorkspace{}, workspaceDemand{ResumeAt: agentloop.ContinueRouteTool}), nil
		}
		setAgentStatus(ctx, agentloop.AgentStatusExecutingTool)
		setRunningWork(ctx, &agentloop.RunningWork{Kind: agentloop.RunningWave, BatchID: batch.BatchID, BatchSize: len(batch.Records)})
		movements := []dex.StepMovement{dex.MovementOf(AwaitParallelToolResults{}, batch)}
		for _, record := range batch.Records {
			branch := parallelToolExecutionInput{ResultInstance: batch.BatchID, Index: record.Index, Call: record.Call, Admission: state.PendingToolsAdmission, Turn: state.Turn}
			movements = append(movements, dex.MovementOf(ExecuteParallelTool{}, branch, dex.WithStepOptions(parallelToolStepOptions(agentloop.CatalogTool(record.Call.Name)))))
		}
		return dex.GoToMany(movements...), nil
	}
	if definition.RequiresApproval {
		startedAt := ctx.FirstAttemptAt().UTC()
		comm.MustDo(pendingApprovalAttribute.Set(ctx, agentloop.PendingApproval{CallID: call.ID, ToolName: call.Name, Arguments: call.Arguments, StartedAt: &startedAt}))
		writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindToolApprovalRequested, "Tool approval requested.", call))
		return dex.GoTo(CheckSteered{}, agentloop.ContinueAwaitToolApproval), nil
	}
	return dex.GoTo(CheckSteered{}, agentloop.ContinueExecuteTool), nil
}

// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Wait for approval matching the pending tool CallID."
type AwaitToolApproval struct {
	dex.StepDefaults
}

func (AwaitToolApproval) GetStepOptions() *dex.StepOptions { return messageMutationStepOptions }

func (AwaitToolApproval) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	setAgentStatus(ctx, agentloop.AgentStatusWaitingForToolApproval)
	writeSnapshotRequired(ctx)
	call := agentloop.PendingToolCall(comm.Must(agentStateAttribute.Get(ctx)))
	return dex.AnyOf(steeredUserMessagesChannel.AtLeastAtMost(1, agentloop.AllPendingChannelMessages), toolApprovalsChannel.ForOne(string(call.ID))), nil
}

// Execute applies steering or the decision. ApproveTool deleted PendingApproval
// in the commit that published the decision.
func (AwaitToolApproval) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	setRunningWork(ctx, nil)
	if steered := comm.Must(steeredUserMessagesChannel.GetConditionResults(ctx)); len(steered) > 0 {
		beginSteeredTurn(ctx, steered)
		return dex.GoTo(CheckSteered{}, agentloop.ContinueStartTurn), nil
	}
	call := agentloop.PendingToolCall(comm.Must(agentStateAttribute.Get(ctx)))
	if comm.Must(toolApprovalsChannel.GetConditionResults(ctx, string(call.ID)))[0].Approved {
		writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindToolApprovalResolved, "Tool approval granted.", call))
		return dex.GoTo(CheckSteered{}, agentloop.ContinueExecuteTool), nil
	}
	writeActivity(ctx, agentloop.ToolActivity(conversation.EventKindToolApprovalResolved, "Tool approval rejected.", call))
	appendSessionFailure(ctx, call, toolfailure.RejectedByUser, "the user declined this call")
	return dex.GoTo(CheckSteered{}, advancePendingTools(ctx, 1)), nil
}

// ExecuteTool declares only its failure route; CheckSteered attaches the
// complete serial options of the call to every movement.
//
// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Run one tool with stable effect identity and bounded execution."
type ExecuteTool struct {
	dex.StepDefaultsNoWaitFor[toolInvocation]
	flow *SessionAgentFlow
	// command selects the failure route of a bash call in GetStepOptions.
	command bool
}

// GetStepOptions declares the failure route: a bash call's movement takes SettleForegroundCommand
// (the Sandbox's command ledger), every other call RecordToolFailure, also the registered route.
func (step ExecuteTool) GetStepOptions() *dex.StepOptions {
	if step.command {
		return &dex.StepOptions{ExecuteFailure: dex.ProceedToOnExecuteFailure(SettleForegroundCommand{}, nil)}
	}
	return &dex.StepOptions{ExecuteFailure: dex.ProceedToOnExecuteFailure(RecordToolFailure{}, recordToolFailureStepOptions)}
}

func (step ExecuteTool) Execute(ctx dex.Context, input toolInvocation) (*dex.StepDecision, error) {
	if input.Call.ID == "" {
		return dex.GoTo(RecordToolFailure{}, input), nil
	}
	input, current := currentToolInvocation(ctx, input)
	if !current {
		return dex.DeadEnd(), nil
	}
	setAgentStatus(ctx, agentloop.AgentStatusExecutingTool)
	call := input.Call
	if ctx.Attempt() == 1 && !input.Admission.Matches(currentExecutor(ctx)) {
		return dex.GoTo(CheckSteered{}, finishToolExecution(ctx, call, agentloop.InterruptedToolResult(call, false), agentloop.ToolMessageTiming{})), nil
	}
	result, err := step.flow.executeTool(ctx, call)
	if errors.Is(err, sandboxtools.ErrWorkspaceUnavailable) {
		if ctx.Attempt() <= 1 || agentloop.CatalogTool(call.Name).WorkspaceAccess != agentloop.WorkspaceAccessMutating {
			// Nothing was dispatched: the call stays pending, consumes no Dex retry
			// and is routed again once the workspace cold path has settled.
			return dex.GoTo(RequestSessionWorkspace{}, lostWorkspaceDemand(ctx, call.ID)), nil
		}
		// An earlier attempt may have mutated the lost workspace: the unknown
		// outcome is never repeated silently; the model inspects it.
		result, err = agentloop.UnfinishedToolResult(call, "The Sandbox workspace was lost after an earlier attempt of the call.", ""), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("execute tool %q call %s: %w", call.Name, call.ID, err))
	}
	next := finishToolExecution(ctx, call, result, agentloop.ToolMessageTiming{StartedAt: ctx.FirstAttemptAt(), CompletedAt: time.Now().UTC()})
	// Schedule the pushed commit in the same durable decision as its result,
	// before steering or Stop can end the turn. The Agent never waits for the build.
	if handoff := dexflow.ValueOrZero(previewHandoffAttribute.Get(ctx)); call.Name == conversation.ToolCommitAndPush && handoff.Unpublished() && publishPreviewHandoff(ctx, &handoff) {
		return dex.GoToMany(dex.MovementOf(CheckSteered{}, next), dex.MovementOf(DeployPreviewHandoff{}, capturedPreviewHandoff(ctx, handoff))), nil
	}
	switch incident, isPlatform := toolIncident(ctx, call, result); {
	case result.Background != nil:
		return dex.GoToMany(dex.MovementOf(CheckSteered{}, next), dex.MovementOf(AwaitBackgroundCommand{}, recordBackgroundCommand(ctx, call, *result.Background))), nil
	case isPlatform:
		return dex.GoToMany(dex.MovementOf(CheckSteered{}, next), dex.MovementOf(ReportPlatformIssue{}, incident)), nil
	}
	return dex.GoTo(CheckSteered{}, next), nil
}

// ExecuteParallelTool declares the failure route every branch shares; each
// movement attaches the complete execution policy of its call.
//
// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Run one branch of an admitted parallel tool batch."
type ExecuteParallelTool struct {
	dex.StepDefaultsNoWaitFor[parallelToolExecutionInput]
	flow *SessionAgentFlow
}

func (ExecuteParallelTool) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteFailure: dex.ProceedToOnExecuteFailure(RecoverParallelToolExecution{}, parallelToolRecoveryStepOptions)}
}

func (step ExecuteParallelTool) Execute(ctx dex.Context, input parallelToolExecutionInput) (*dex.StepDecision, error) {
	if !currentParallelInvocation(ctx, input) {
		return dex.DeadEnd(), nil
	}
	branch, err := step.flow.executeBranch(ctx, input)
	if err != nil {
		return nil, err
	}
	comm.MustDo(parallelToolResultsChannel.Publish(ctx, input.ResultInstance, branch))
	return dex.DeadEnd(), nil
}

// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Join unique results from the expected tool batch."
type AwaitParallelToolResults struct {
	dex.StepDefaults
}

func (AwaitParallelToolResults) GetStepOptions() *dex.StepOptions { return messageMutationStepOptions }

func (AwaitParallelToolResults) WaitFor(_ dex.Context, batch agentloop.ToolBatchState) (*dex.Wait, error) {
	return dex.Until(parallelToolResultsChannel.ForN(batch.BatchID, len(batch.Records))), nil
}

// Execute records the wave's results and reports its platform failures. A wave is read-only, so
// an unreachable workspace discards the whole wave, whose calls are routed on a re-initialized one.
func (AwaitParallelToolResults) Execute(ctx dex.Context, input agentloop.ToolBatchState) (*dex.StepDecision, error) {
	if !currentParallelBatch(ctx, input) {
		return dex.DeadEnd(), nil
	}
	batch, lost, incidents := joinParallelToolResults(input, comm.Must(parallelToolResultsChannel.GetConditionResults(ctx, input.BatchID)))
	if lost != nil {
		return dex.GoTo(RequestSessionWorkspace{}, lostWorkspaceDemand(ctx, lost.Call.ID)), nil
	}
	movements := []dex.StepMovement{dex.MovementOf(CheckSteered{}, finishToolBatch(ctx, batch))}
	for _, incident := range incidents {
		movements = append(movements, dex.MovementOf(ReportPlatformIssue{}, incident))
	}
	return dex.GoToMany(movements...), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Publish a classified failure for one parallel tool branch."
type RecoverParallelToolExecution struct {
	dex.StepDefaultsNoWaitFor[parallelToolExecutionInput]
}

func (RecoverParallelToolExecution) GetStepOptions() *dex.StepOptions {
	return parallelToolRecoveryStepOptions
}

func (RecoverParallelToolExecution) Execute(ctx dex.Context, input parallelToolExecutionInput) (*dex.StepDecision, error) {
	if !currentParallelInvocation(ctx, input) {
		return dex.DeadEnd(), nil
	}
	comm.MustDo(parallelToolResultsChannel.Publish(ctx, input.ResultInstance, parallelToolResult{Index: input.Index, Call: input.Call,
		Result: agentloop.UnfinishedToolResult(input.Call, exhaustedRetriesMessage, dexflow.RecoveryDetail(ctx)), ErrorType: dexflow.RecoveryErrorType(ctx)}))
	return dex.DeadEnd(), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Record a serial tool call whose retries are exhausted as a platform failure the model reads, and report it beside the turn."
type RecordToolFailure struct {
	dex.StepDefaultsNoWaitFor[toolInvocation]
}

func (RecordToolFailure) GetStepOptions() *dex.StepOptions { return recordToolFailureStepOptions }

func (RecordToolFailure) Execute(ctx dex.Context, input toolInvocation) (*dex.StepDecision, error) {
	if input.Call.ID == "" {
		return dex.GoTo(ReportPlatformIssue{}, ambiguousToolInvocation(ctx)), nil
	}
	input, current := currentToolInvocation(ctx, input)
	if !current {
		return dex.DeadEnd(), nil
	}
	call := input.Call
	failSubagentStart(ctx, call)
	result := agentloop.UnfinishedToolResult(call, exhaustedRetriesMessage, dexflow.RecoveryDetail(ctx))
	next := finishToolExecution(ctx, call, result, agentloop.ToolMessageTiming{})
	return dex.GoToMany(dex.MovementOf(CheckSteered{}, next), dex.MovementOf(ReportPlatformIssue{}, unfinishedIncident(ctx, call, result))), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Settle a foreground bash command after its timeout or exhausted retries from the Sandbox's command ledger."
type SettleForegroundCommand struct {
	dex.StepDefaultsNoWaitFor[toolInvocation]
	flow *SessionAgentFlow
}

// GetStepOptions: the Step stops or reads one command and holds no lock, so
// user RPCs never wait for the Sandbox.
func (SettleForegroundCommand) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: time.Minute, HeartbeatTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteRetry: shortStepRetry, ExecuteLoadAttributeMaps: []dex.AttributeDef{currentMessagesAttribute, backgroundCommandsAttribute},
		ExecuteFailure: dex.ProceedToOnExecuteFailure(RecordToolFailure{}, recordToolFailureStepOptions)}
}

// Execute records the settled result of the call at the cursor; its own exhausted retries
// leave the outcome unknown, and RecordToolFailure records the command as interrupted.
func (step SettleForegroundCommand) Execute(ctx dex.Context, input toolInvocation) (*dex.StepDecision, error) {
	if input.Call.ID == "" {
		return dex.GoTo(RecordToolFailure{}, input), nil
	}
	input, current := currentToolInvocation(ctx, input)
	if !current {
		return dex.DeadEnd(), nil
	}
	call := input.Call
	result, timing, err := step.flow.settleCommand(ctx, call)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("settle command call %s: %w", call.ID, err))
	}
	next := finishToolExecution(ctx, call, result, timing)
	switch incident, isPlatform := toolIncident(ctx, call, result); {
	case result.Background != nil:
		return dex.GoToMany(dex.MovementOf(CheckSteered{}, next), dex.MovementOf(AwaitBackgroundCommand{}, recordBackgroundCommand(ctx, call, *result.Background))), nil
	case isPlatform:
		return dex.GoToMany(dex.MovementOf(CheckSteered{}, next), dex.MovementOf(ReportPlatformIssue{}, incident)), nil
	}
	return dex.GoTo(CheckSteered{}, next), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Beside the turn, which never waits for it, start without a lock the PlatformIssueFlow of a platform failure's signature, which files it; only a failed start is retried."
type ReportPlatformIssue struct {
	dex.StepDefaultsNoWaitFor[agentloop.PlatformIncident]
	flow *SessionAgentFlow
}

func (ReportPlatformIssue) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(AbandonPlatformIssue{}, nil))
}

func (step ReportPlatformIssue) Execute(ctx dex.Context, incident agentloop.PlatformIncident) (*dex.StepDecision, error) {
	if err := step.flow.dependency.ReportPlatformIssue(ctx, readSessionIdentity(ctx), incident); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("report platform incident %s: %w", incident.Signature(), err))
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"End a report whose start exhausted its retries; the Session Service logged each failed start."
type AbandonPlatformIssue struct {
	dex.StepDefaultsNoWaitFor[agentloop.PlatformIncident]
}

func (AbandonPlatformIssue) Execute(dex.Context, agentloop.PlatformIncident) (*dex.StepDecision, error) {
	return dex.DeadEnd(), nil
}

// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Wait for the BackgroundCommandFlow SubFlow that follows a background command, or for the stop of its watch, then record how it ended and notify the Agent, unless it was stopped."
type AwaitBackgroundCommand struct {
	dex.StepDefaults
	flow *SessionAgentFlow
}

func (AwaitBackgroundCommand) GetStepOptions() *dex.StepOptions {
	options := *backgroundExitStepOptions
	options.ExecuteFailure = dex.ProceedToOnExecuteFailure(AbandonBackgroundCommand{}, abandonBackgroundCommandStepOptions)
	return &options
}

// WaitFor starts the SubFlow that polls the ledger and races it with the stop of its watch; waiting
// never delays the Session's other Steps.
func (AwaitBackgroundCommand) WaitFor(_ dex.Context, watch backgroundcommand.BackgroundCommandWatch) (*dex.Wait, error) {
	return dex.AnyOf(backgroundcommand.SubFlow(watch), backgroundCommandStopsChannel.ForOne(watch.TaskID)), nil
}

// Execute records the end once (watchedCommandEnd); a stopped command sends no notification, any other
// end is published under its task's ID, which a retry repeats.
func (step AwaitBackgroundCommand) Execute(ctx dex.Context, watch backgroundcommand.BackgroundCommandWatch) (*dex.StepDecision, error) {
	command := comm.Must(backgroundCommandsAttribute.Get(ctx, watch.TaskID))
	if !command.IsRunning() {
		return dex.DeadEnd(), nil
	}
	end, err := step.flow.watchedCommandEnd(ctx, watch)
	if err != nil {
		return nil, err
	}
	command = command.End(end)
	comm.MustDo(backgroundCommandsAttribute.Set(ctx, watch.TaskID, command))
	if notification, notifies := agenttasks.CommandNotification(command); notifies && acceptsUserCommand(ctx) {
		publishNotification(ctx, notification, dexflow.ValueOrZero(notificationReceiptsAttribute.Get(ctx)))
	}
	writeSnapshotRequired(ctx)
	return dex.DeadEnd(), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Record lost, without a notification, a background command whose recording or watch stop exhausted its retries; a watch left running ends by itself."
type AbandonBackgroundCommand struct {
	dex.StepDefaultsNoWaitFor[backgroundcommand.BackgroundCommandWatch]
}

func (AbandonBackgroundCommand) GetStepOptions() *dex.StepOptions {
	return abandonBackgroundCommandStepOptions
}

func (AbandonBackgroundCommand) Execute(ctx dex.Context, watch backgroundcommand.BackgroundCommandWatch) (*dex.StepDecision, error) {
	if command := comm.Must(backgroundCommandsAttribute.Get(ctx, watch.TaskID)); command.IsRunning() {
		comm.MustDo(backgroundCommandsAttribute.Set(ctx, watch.TaskID, command.End(agenttasks.BackgroundCommandEnd{EndReason: agenttasks.BackgroundCommandEndLost, EndedAt: time.Now().UTC()})))
		writeSnapshotRequired(ctx)
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:conversation group-label:"Conversation"
// dex:explanation text:"Stop the foreground command of the stopped turn and every running background command, holding no lock."
type InterruptAgent struct {
	dex.StepDefaultsNoWaitFor[agentInterruption]
	flow *SessionAgentFlow
}

// GetStepOptions: after exhausted retries FinishInterrupt still ends the turn,
// so that a Stop always ends it (interruptStepOptions).
func (InterruptAgent) GetStepOptions() *dex.StepOptions {
	return interruptStepOptions(dex.ProceedToOnExecuteFailure(FinishInterrupt{}, nil))
}

// Execute stops the tasks (stopTasks), each idempotently by its ledger ID,
// holding no lock, and FinishInterrupt commits the Stop.
func (step InterruptAgent) Execute(ctx dex.Context, interruption agentInterruption) (*dex.StepDecision, error) {
	if err := step.flow.stopTasks(ctx, interruption); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("stop the tasks of turn %d: %w", interruption.Turn, err))
	}
	return dex.GoToMany(dex.MovementOf(FinishInterrupt{}, interruption, dex.WithStepOptions(finishInterruptOptions(interruption)))), nil
}

// dex:group group-id:conversation group-label:"Conversation"
// dex:explanation text:"End the stopped turn: answer its unfinished calls interrupted_by_user, pause automatic turns and wait for the user."
type FinishInterrupt struct {
	dex.StepDefaultsNoWaitFor[agentInterruption]
}

func (FinishInterrupt) GetStepOptions() *dex.StepOptions { return finishInterruptStepOptions }

// Execute ends a turn whose work the Stop cancelled; otherwise CheckSteered ends it at its next
// boundary and this Step stops the turn's starting subagents (stopStartingSubagentsOf).
func (FinishInterrupt) Execute(ctx dex.Context, interruption agentInterruption) (*dex.StepDecision, error) {
	if interruption.RequestedAt.IsZero() && !interruption.EndsTurn {
		finishLegacyTaskInterrupt(ctx, interruption)
		return dex.DeadEnd(), nil
	}
	pending := matchingInterrupt(ctx, interruption)
	if pending == nil {
		return dex.DeadEnd(), nil
	}
	if !interruption.EndsTurn {
		stopStartingSubagentsOf(ctx, interruption)
		state := comm.Must(agentStateAttribute.Get(ctx))
		if state.Status == agentloop.AgentStatusWaitingForMessage {
			writeStopMarker(ctx, pending)
		}
		writeActivity(ctx, interruptActivity(ctx, "Stopped the running tasks."))
		return dex.DeadEnd(), nil
	}
	endStoppedTurn(ctx, publishedWaveResults(ctx, interruption.Running), pending)
	comm.MustDo(agentInterruptAttribute.Delete(ctx))
	return dex.GoTo(AwaitUser{}, nil), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"Record one stable Sandbox request, or join the request already in flight."
type RequestSessionWorkspace struct {
	dex.StepDefaultsNoWaitFor[workspaceDemand]
	flow *SessionAgentFlow
}

func (RequestSessionWorkspace) GetStepOptions() *dex.StepOptions { return workspaceRequestStepOptions }

func (step RequestSessionWorkspace) Execute(ctx dex.Context, demand workspaceDemand) (*dex.StepDecision, error) {
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	if !acceptsUserCommand(ctx) {
		return dex.DeadEnd(), nil
	}
	plan := ws.PlanRequest(demand.ResumeAt, demand.Lost, demand.CallID, newWorkspaceRequestID(ctx))
	if plan.ChangesWorkspace() {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
	switch plan {
	case agentloop.WorkspaceOpened:
		// The previous request's Sandbox deadline ends with it; the request is durable before its command.
		demand.RequestID = ws.RequestID
		return dex.GoTo(SendWorkspaceCommand{}, demand).CancelSteps(AwaitSandboxDeadline{}), nil
	case agentloop.WorkspaceUsableWithoutContext:
		// Read the repository context now instead of waiting for the next instance.
		demand.RequestID, demand.PromptOnly = ws.RequestID, true
		return dex.GoTo(ApplySessionWorkspace{}, demand), nil
	case agentloop.WorkspaceInFlight, agentloop.WorkspaceJoined:
		return dex.DeadEnd(), nil
	case agentloop.WorkspaceUsable, agentloop.WorkspaceLossesExhausted:
		// Nothing to request: resume below.
	}
	if next := step.flow.resumeAfterWorkspace(ctx, demand, plan == agentloop.WorkspaceUsable); next != "" {
		return dex.GoTo(CheckSteered{}, next), nil
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"Send the SessionWorkspaceFlow the command of the request, which provisions its instance after reclaiming an earlier one, or the reclaim command of a revoked request."
type SendWorkspaceCommand struct {
	dex.StepDefaultsNoWaitFor[workspaceDemand]
	flow *SessionAgentFlow
}

func (SendWorkspaceCommand) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordWorkspaceFailure{}, nil))
}

// Execute sends the command without a lock: it is idempotent by its ID, and every settling Step
// rereads Workspace under its lock. A refusal settles the request at once.
func (step SendWorkspaceCommand) Execute(ctx dex.Context, demand workspaceDemand) (*dex.StepDecision, error) {
	ws, identity := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)), readSessionIdentity(ctx)
	send := step.flow.dependency.ProvisionWorkspace
	if demand.Reclaims {
		send = step.flow.dependency.ReclaimWorkspace
	} else if ws.RequestID != demand.RequestID || ws.State != agentloop.WorkspaceRequested {
		// The request settled, or another replaced it, before this Step ran.
		return dex.GoTo(ApplySessionWorkspace{}, demand), nil
	}
	accepted, err := send(ctx, identity, demand.RequestID, ws.PreviewHolds)
	switch {
	case errors.Is(err, ErrWorkspaceCommandRejected) && demand.Reclaims:
		return dex.DeadEnd(), nil
	case errors.Is(err, ErrWorkspaceCommandRejected):
		demand.Failure = err.Error()
		return dex.GoTo(RecordWorkspaceFailure{}, demand), nil
	case err != nil:
		return nil, dex.ErrorWithStack(fmt.Errorf("send the workspace command of %s: %w", demand.RequestID, err))
	case demand.Reclaims:
		return dex.DeadEnd(), nil
	case !accepted:
		demand.Failure = "the Session workspace refused the command of the replaced request " + demand.RequestID
		return dex.GoTo(RecordWorkspaceFailure{}, demand), nil
	}
	return dex.GoTo(AwaitSessionWorkspace{}, demand), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"Wait, bounded, for the outcome the SessionWorkspaceFlow reports, or read it from that Flow once the wait ends."
type AwaitSessionWorkspace struct {
	dex.StepDefaults
	flow *SessionAgentFlow
}

func (AwaitSessionWorkspace) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordWorkspaceFailure{}, nil))
}

// WaitFor waits for the outcome the SessionWorkspaceFlow reports, which only
// this Step consumes, or for the bounded wait that outlasts its command.
func (AwaitSessionWorkspace) WaitFor(_ dex.Context, demand workspaceDemand) (*dex.Wait, error) {
	return dex.AnyOf(workspaceOutcomesChannel.ForOne(demand.RequestID), dex.Timer(workspaceWaitBudget)), nil
}

// Execute takes the reported outcome; once the wait ended without one it reads the outcome from the
// workspace Flow, which owns it, and without one the request settles as WORKSPACE_TIMEOUT.
func (step AwaitSessionWorkspace) Execute(ctx dex.Context, demand workspaceDemand) (*dex.StepDecision, error) {
	var observed *agentloop.WorkspaceGrant
	if reports := comm.Must(workspaceOutcomesChannel.GetConditionResults(ctx, demand.RequestID)); len(reports) > 0 {
		observed = reports[0].Grant
	} else if read, err := step.flow.dependency.ObserveWorkspace(ctx, readSessionIdentity(ctx), demand.RequestID); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("read the outcome of the Session workspace %s: %w", demand.RequestID, err))
	} else {
		observed = read
	}
	if observed == nil {
		demand.TimedOut, demand.Failure = true, fmt.Sprintf("the Session workspace reported no outcome of %s within %s", demand.RequestID, workspaceWaitBudget)
		return dex.GoTo(RecordWorkspaceFailure{}, demand), nil
	}
	demand.Observed = observed
	return dex.GoTo(ApplySessionWorkspace{}, demand), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"Settle the request from the observed instance, apply an accepted lease and resume the waiting branches."
type ApplySessionWorkspace struct {
	dex.StepDefaultsNoWaitFor[workspaceDemand]
	flow *SessionAgentFlow
}

func (ApplySessionWorkspace) GetStepOptions() *dex.StepOptions { return applyWorkspaceStepOptions() }

// Execute settles the request with the outcome AwaitSessionWorkspace read,
// then applies it (applyWorkspace).
func (step ApplySessionWorkspace) Execute(ctx dex.Context, demand workspaceDemand) (*dex.StepDecision, error) {
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	if ws.RequestID != demand.RequestID {
		// The request of this branch settled and another replaced it: an RPC
		// demand ends, and the conversation asks again.
		if demand.ResumeAt == "" {
			return dex.DeadEnd(), nil
		}
		return dex.GoTo(RequestSessionWorkspace{}, workspaceDemand{ResumeAt: demand.ResumeAt}), nil
	}
	demand, isReady, err := step.flow.applyWorkspace(ctx, ws, demand)
	if err != nil {
		return nil, err
	}
	// The owner of a READY request arms its Sandbox deadline.
	next, arms := step.flow.resumeAfterWorkspace(ctx, demand, isReady), isReady && !demand.PromptOnly
	switch {
	case arms && next != "":
		return dex.GoToMany(dex.MovementOf(CheckSteered{}, next), dex.MovementOf(AwaitSandboxDeadline{}, workspaceDemand{RequestID: demand.RequestID})), nil
	case arms:
		return dex.GoTo(AwaitSandboxDeadline{}, workspaceDemand{RequestID: demand.RequestID}), nil
	case next != "":
		return dex.GoTo(CheckSteered{}, next), nil
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"Settle a workspace request whose Step was rejected, exhausted its retries or passed its wait limit, record why, and resume the waiting branches."
type RecordWorkspaceFailure struct {
	dex.StepDefaultsNoWaitFor[workspaceDemand]
	flow *SessionAgentFlow
}

func (RecordWorkspaceFailure) GetStepOptions() *dex.StepOptions { return workspaceRequestStepOptions }

// Execute records the failure with the request of this branch (settleWorkspaceFailure); a
// request whose outcome did not arrive within the wait settles as WORKSPACE_TIMEOUT.
func (step RecordWorkspaceFailure) Execute(ctx dex.Context, demand workspaceDemand) (*dex.StepDecision, error) {
	ws, arms := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)), false
	if ws.RequestID == demand.RequestID {
		var err error
		if demand, arms, err = step.flow.settleWorkspaceFailure(ctx, &ws, demand, workspaceFailureCode(demand)); err != nil {
			return nil, err
		}
	}
	// A READY request whose lease effects failed still gets its Sandbox deadline.
	switch next := step.flow.resumeAfterWorkspace(ctx, demand, ws.IsUsable()); {
	case arms && next != "":
		return dex.GoToMany(dex.MovementOf(CheckSteered{}, next), dex.MovementOf(AwaitSandboxDeadline{}, workspaceDemand{RequestID: demand.RequestID})), nil
	case arms:
		return dex.GoTo(AwaitSandboxDeadline{}, workspaceDemand{RequestID: demand.RequestID}), nil
	case next != "":
		return dex.GoTo(CheckSteered{}, next), nil
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"Wait for the Sandbox deadline of the READY request, then revoke the lease of the idle instance and have the SessionWorkspaceFlow reclaim it."
type AwaitSandboxDeadline struct {
	dex.StepDefaults
	flow *SessionAgentFlow
}

func (AwaitSandboxDeadline) GetStepOptions() *dex.StepOptions { return sandboxDeadlineStepOptions }

// WaitFor waits only on a business timer until the deadline; the deadline only moves later, so
// no wake Channel is needed: a timer that finds a later deadline waits again.
func (AwaitSandboxDeadline) WaitFor(ctx dex.Context, _ workspaceDemand) (*dex.Wait, error) {
	deadline := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).SandboxDeadline
	return dex.Until(dex.Timer(max(deadline.Sub(ctx.FirstAttemptAt()), time.Second))), nil
}

// Execute revokes the idle instance's lease at the deadline (reclaimAtDeadline), committed before
// SendWorkspaceCommand sends the reclaim command, or waits again for a later deadline.
func (step AwaitSandboxDeadline) Execute(ctx dex.Context, demand workspaceDemand) (*dex.StepDecision, error) {
	reclaims, waitsAgain := step.flow.reclaimAtDeadline(ctx, demand.RequestID)
	switch {
	case waitsAgain:
		return dex.GoTo(AwaitSandboxDeadline{}, demand), nil
	case reclaims:
		return dex.GoTo(SendWorkspaceCommand{}, workspaceDemand{RequestID: demand.RequestID, Reclaims: true}), nil
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"Ask the Preview once to build and deploy each pushed commit, on the accepted workspace; reclamation races resume it."
type DeployPreviewHandoff struct {
	dex.StepDefaultsNoWaitFor[agentloop.PreviewHandoff]
	flow *SessionAgentFlow
}

func (DeployPreviewHandoff) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: 45 * time.Second, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteRetry: dexflow.StepRetry(3 * time.Minute), ExecuteFailure: dex.ProceedToOnExecuteFailure(AbandonPreviewHandoff{}, nil)}
}

// Execute uses the captured workspace, or reads it for legacy inputs only, and calls the Service.
// Preview answers a retried sequence DUPLICATE; a lost answer never deploys twice. Nothing is recorded.
func (step DeployPreviewHandoff) Execute(ctx dex.Context, handoff agentloop.PreviewHandoff) (*dex.StepDecision, error) {
	if handoff.Session == nil {
		// Compatibility for platform branches accepted before immutable inputs.
		if ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)); ws.Lease.RequestID != "" {
			identity := readSessionIdentity(ctx)
			handoff.Session, handoff.WorkspaceRequestID = &identity, ws.Lease.RequestID
		}
	}
	if handoff.Session != nil && handoff.WorkspaceRequestID != "" {
		if err := step.flow.dependency.RequestPreviewDeployment(ctx, *handoff.Session, handoff.WorkspaceRequestID, handoff); err != nil {
			return nil, dex.ErrorWithStack(fmt.Errorf("request the Preview deployment of %s: %w", handoff.Commit, err))
		}
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"End a handoff whose deployment request exhausted its retries; the next handoff or a manual Start deploys again."
type AbandonPreviewHandoff struct {
	dex.StepDefaultsNoWaitFor[agentloop.PreviewHandoff]
}

func (AbandonPreviewHandoff) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second}
}

// Execute keeps nothing: the engine history holds the cause.
func (AbandonPreviewHandoff) Execute(dex.Context, agentloop.PreviewHandoff) (*dex.StepDecision, error) {
	return dex.DeadEnd(), nil
}

// dex:group group-id:session group-label:"Session"
// dex:explanation text:"Complete the Session of a deleting Project, which releases its resources by label."
type CompleteProjectSession struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (CompleteProjectSession) GetStepOptions() *dex.StepOptions { return lifecycleStepOptions(nil) }

// Execute completes the Session irreversibly, so it checks again the admission
// the CompleteProjectSession RPC recorded.
func (CompleteProjectSession) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	status, reason := comm.Must(sessionStatusAttribute.Get(ctx)), comm.Must(closeReasonAttribute.Get(ctx))
	if status != agentloop.SessionStatusArchived || reason != agentloop.CloseReasonProjectCleanupComplete.String() {
		return nil, dex.ErrorWithStack(fmt.Errorf("project cleanup admission missing: Session is %s with reason %q", status, reason))
	}
	return dex.GracefulComplete(agentloop.SessionCleanupReceipt{ProjectID: readSessionIdentity(ctx).ProjectID, Reason: reason}), nil
}

// SetExecutor changes only the exact scoped Session's executor fence.
func (flow *SessionAgentFlow) SetExecutor(ctx dex.Context, input remoteexec.SetRequest) (*dex.RPCResult[remoteexec.SetResult], error) {
	output, interruption, err := setExecutor(ctx, input)
	if err != nil {
		return nil, err
	}
	if interruption == nil {
		return &dex.RPCResult[remoteexec.SetResult]{Output: output}, nil
	}
	if interruption.EndsTurn {
		// Keep the cancellation set identical to InterruptAgent above.
		return (&dex.RPCResult[remoteexec.SetResult]{Output: output, NextSteps: []dex.StepMovement{dex.MovementOf(InterruptAgent{}, *interruption, dex.WithStepOptions(interruptMovementOptions(*interruption)))}}).CancelSteps(CompactContext{}, CallModel{}, RecordPlatformModelResult{}, RecordModelFailure{}, RecordCompactionFailure{}, ExecuteTool{}, ExecuteParallelTool{},
			RecoverParallelToolExecution{}, AwaitParallelToolResults{}, RecordToolFailure{}, SettleForegroundCommand{}, AwaitAgentTimer{}, AwaitToolApproval{}, AwaitRemoteAgent{}), nil
	}
	return &dex.RPCResult[remoteexec.SetResult]{Output: output, NextSteps: []dex.StepMovement{dex.MovementOf(InterruptAgent{}, *interruption, dex.WithStepOptions(interruptMovementOptions(*interruption)))}}, nil
}

// ReportRemote atomically admits a report and publishes its inbox message.
func (*SessionAgentFlow) ReportRemote(ctx dex.Context, input remoteexec.ReportRequest) (*dex.RPCResult[remoteexec.ReportResult], error) {
	return reportRemote(ctx, input)
}

// dex:group group-id:conversation group-label:"Conversation"
// dex:explanation text:"Wait on remote reports, steering and notifications, then use the Session's ordinary tool path."
type AwaitRemoteAgent struct {
	dex.StepDefaults
	flow *SessionAgentFlow
}

func (AwaitRemoteAgent) GetStepOptions() *dex.StepOptions { return remoteStepOptions() }

func (AwaitRemoteAgent) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	if currentExecutor(ctx).Mode != remoteexec.ModeRemote || dexflow.OptionalValue(agentInterruptAttribute.Get(ctx)) != nil {
		return dex.Until(steeredUserMessagesChannel.AtMost(agentloop.AllPendingChannelMessages)), nil
	}
	return dex.AnyOf(remoteInboxChannel.ForN(remoteHeadCount(ctx)), steeredUserMessagesChannel.AtLeastAtMost(1, agentloop.AllPendingChannelMessages), sessionNotificationsChannel.AtLeastAtMost(1, agentloop.NotificationBatchSize)), nil
}

func (step AwaitRemoteAgent) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	next := step.flow.executeRemote(ctx)
	if next == "" {
		return dex.GoTo(AwaitUser{}, nil), nil
	}
	return dex.GoTo(CheckSteered{}, next), nil
}

// ReadRemote is a scoped, query-only current executor projection.
// dex:invocation-load attribute-map:CurrentMessages
// dex:invocation-load attribute-map:ArchivedMessages
func (*SessionAgentFlow) ReadRemote(ctx dex.Context, input remoteexec.ReadRequest) (*dex.RPCResult[RemoteRead], error) {
	return readRemote(ctx, input)
}
