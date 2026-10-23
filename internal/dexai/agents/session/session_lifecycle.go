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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/flowdesign"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// sessionStartAttributes are the identity and lifecycle a Session starts with,
// installed atomically with StartFlow. The Session Service admitted the
// identity and its profile.
func sessionStartAttributes(request agentloop.StartRequest) []dex.InitialAttributeDef {
	identity, now := request.Session, time.Now().UTC()
	attributes := &dexflow.InitialAttributes{}
	dexflow.Add(attributes, projectIDAttribute, identity.ProjectID)
	dexflow.Add(attributes, sessionProfileAttribute, identity.Profile)
	dexflow.Add(attributes, headCommitAttribute, identity.HeadCommit)
	dexflow.Add(attributes, sessionStartIdentityAttribute, request.SessionStartIdentity)
	dexflow.Add(attributes, sessionStatusAttribute, agentloop.SessionStatusActive)
	dexflow.Add(attributes, closeReasonAttribute, "")
	dexflow.Add(attributes, sessionRevisionAttribute, uint64(1))
	dexflow.Add(attributes, sessionCreatedAtAttribute, now)
	dexflow.Add(attributes, sessionUpdatedAtAttribute, now)
	dexflow.Add(attributes, sessionModelAttribute, agentloop.SessionModel{Model: request.Model})
	return attributes.Values()
}

// readSessionIdentity reads the identity the Session started with and its HEAD.
func readSessionIdentity(ctx dex.Context) agentloop.SessionIdentity {
	return agentloop.SessionIdentity{
		ProjectID: comm.Must(projectIDAttribute.Get(ctx)), Profile: comm.Must(sessionProfileAttribute.Get(ctx)), HeadCommit: comm.Must(headCommitAttribute.Get(ctx)),
	}
}

// acceptsUserCommand reports whether the Session is ACTIVE, the only status in
// which it accepts a command or starts conversation work. A user command to a
// Session that is not ACTIVE answers false.
func acceptsUserCommand(ctx dex.Context) bool {
	return comm.Must(sessionStatusAttribute.Get(ctx)) == agentloop.SessionStatusActive
}

// rpcAccepted is the result of a command; false rejects a command that does not
// match the current durable state, including every command to a Session that is
// no longer ACTIVE.
func rpcAccepted(isAccepted bool) *dex.RPCResult[bool] {
	return &dex.RPCResult[bool]{Output: isAccepted}
}

// modelSelection answers UpdateSessionModel: whether the Session accepted the
// selection, and whether it refused it because the caller's revision no longer
// holds, which the caller answers by rereading.
type modelSelection struct{ Accepted, Stale, CredentialMissing bool }

// queuedMessageDeletion distinguishes authorization refusal from a stale queue identity.
type queuedMessageDeletion struct{ Deleted, NotAuthor bool }

// messageAdmission answers SendMessage: whether the Session took the message,
// and whether it refused it because it carries attachments while a local
// agent executes the Session, which receives text only.
type messageAdmission struct{ Accepted, AttachmentsRefused, CredentialMissing bool }

// setSessionStatus records a lifecycle transition at the next revision.
func setSessionStatus(ctx dex.Context, status agentloop.SessionStatus, reason string) {
	comm.MustDo(sessionStatusAttribute.Set(ctx, status))
	comm.MustDo(closeReasonAttribute.Set(ctx, reason))
	comm.MustDo(sessionRevisionAttribute.Set(ctx, comm.Must(sessionRevisionAttribute.Get(ctx))+1))
	comm.MustDo(sessionUpdatedAtAttribute.Set(ctx, ctx.FirstAttemptAt().UTC()))
}

// denySession archives a Session its Project did not admit and returns the
// cleanup receipt with which InitializeSessionAgent completes it.
func (flow *SessionAgentFlow) denySession(ctx dex.Context, identity agentloop.SessionIdentity) agentloop.SessionCleanupReceipt {
	setSessionStatus(ctx, agentloop.SessionStatusArchived, agentloop.CloseReasonAdmissionDenied.String())
	summary := comm.Must(flow.GetSessionSummary(ctx, nil)).Output
	return agentloop.SessionCleanupReceipt{ProjectID: identity.ProjectID, Reason: agentloop.CloseReasonAdmissionDenied.String(), Snapshot: &summary}
}

// incrementWaitingInputRound advances the watermark for a wait that starts
// with nothing pending.
func incrementWaitingInputRound(ctx dex.Context) {
	comm.MustDo(waitingInputRoundAttribute.Set(ctx, agentloop.NextWaitingInputRound(comm.Must(waitingInputRoundAttribute.Get(ctx)))))
}

// setAgentStatus records the state-machine boundary the conversation is at.
func setAgentStatus(ctx dex.Context, status agentloop.AgentStatus) {
	if state := comm.Must(agentStateAttribute.Get(ctx)); state.Status != status {
		state.Status = status
		comm.MustDo(agentStateAttribute.Set(ctx, state))
	}
}

// agentSnapshot is the conversation view of GetSnapshot: the public messages
// of the current window, which read returns from CurrentMessages, the pending
// user messages and the durable interaction state with the todo list, the
// confirmed requirements and the tasks: the background commands and the
// subagents.
func agentSnapshot(ctx dex.Context, state agentloop.AgentState, read func(conversation.Sequence) conversation.Entry, todos *agentloop.TodoList) *agentloop.AgentSnapshot {
	start := max(state.FirstRetainedSequence, state.CurrentFirstSequence)
	history := conversation.HistoryPage{Messages: make([]conversation.SequencedMessage, 0, int(state.LastSequence-start+1))}
	for sequence := start; sequence <= state.LastSequence; sequence++ {
		if message, isPublic := read(sequence).PublicMessage(); isPublic {
			history.Messages = append(history.Messages, conversation.SequencedMessage{Sequence: sequence, Message: message})
		}
	}
	if start > state.FirstRetainedSequence {
		history.NextBeforeSequence = &start
	}
	queued, steered := comm.Must(queuedUserMessagesChannel.PendingMessages(ctx)), comm.Must(steeredUserMessagesChannel.PendingMessages(ctx))
	agent := &agentloop.AgentSnapshot{History: history, Queued: pendingUserMessages(queued), Steered: pendingUserMessages(steered),
		Description: agentDescription(ctx, state, len(queued), len(steered))}
	agent.Description.TodoList = todos
	agent.Description.ConfirmedRequirements = dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx))
	if confirmed := agent.Description.ConfirmedRequirements; confirmed != nil && confirmed.Spec.Design != nil {
		agent.Description.ConfirmedDesignBuilt = dexflow.ValueOrZero(builtFlowDesignAttribute.Get(ctx)) == confirmed.Spec.Design.Digest
		if agent.Description.ConfirmedDesignBuilt {
			agent.Description.ConfirmedDesignBuiltCommit = dexflow.ValueOrZero(builtFlowDesignCommitAttribute.Get(ctx))
		}
		if written := dexflow.ValueOrZero(writtenDesignStepsAttribute.Get(ctx)); written.Digest == confirmed.Spec.Design.Digest {
			agent.Description.ConfirmedDesignWrittenSteps = written.Names()
		}
	}
	agent.Description.BackgroundCommands = agenttasks.SortedBackgroundCommands(loadedBackgroundCommands(ctx))
	agent.Description.Subagents = agenttasks.SortedSubagents(loadedSubagents(ctx))
	agent.Description.MockProgress, agent.Description.ConnectorSlugs = agentloop.NewestMockProgress(history), state.ConnectorSlugs
	return agent
}

// agentDescription reads the durable interaction state a conversation view renders.
func agentDescription(ctx dex.Context, state agentloop.AgentState, queuedCount, steeredCount int) agentloop.AgentDescription {
	selected := comm.Must(sessionModelAttribute.Get(ctx))
	description := agentloop.AgentDescription{
		ModelCredentialVersion:     modelCredentialVersion(ctx, selected.Model),
		Executor:                   currentExecutor(ctx),
		Status:                     state.Status,
		WaitingInputRound:          comm.Must(waitingInputRoundAttribute.Get(ctx)),
		Model:                      selected.Model,
		ModelChangedBy:             selected.ChangedBy,
		PreviewAutoDeployChangedBy: dexflow.OptionalValue(previewAutoDeployChangedByAttribute.Get(ctx)),
		ModelRevision:              selected.Revision,
		FirstRetainedSequence:      state.FirstRetainedSequence,
		LastSequence:               state.LastSequence,
		PendingApproval:            dexflow.OptionalValue(pendingApprovalAttribute.Get(ctx)),
		PendingTimer:               dexflow.OptionalValue(pendingTimerAttribute.Get(ctx)),
		PendingUserInput:           dexflow.OptionalValue(pendingUserInputAttribute.Get(ctx)),
		PendingQueuedMessageCount:  queuedCount,
		PendingSteeredMessageCount: steeredCount,
		AvailableTools:             agentloop.SessionPromptFor(selected.Model).ToolNames,
		Turn:                       state.Turn,
		IsPreviewAutoDeployEnabled: dexflow.ValueOrZero(previewAutoDeployAttribute.Get(ctx)),
		StartingExample:            state.StartingExample,
	}
	if segment := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx)); segment != nil {
		description.ContextModel, description.Effort, description.AvailableTools = segment.Model, segment.EffectiveEffort(), segment.ToolNames
	}
	if description.Executor.Mode == remoteexec.ModeRemote {
		description.AvailableTools = agentloop.RemoteToolNames()
	}
	return description
}

// The option presets of the Session's Steps and RPCs; session_agent_flow.go
// declares the options of each one with them.

const (
	// commandRPCTimeout bounds a command RPC together with the client's lock-conflict retries.
	commandRPCTimeout = 20 * time.Second
	// readRPCTimeout bounds a read RPC and an engine status read.
	readRPCTimeout = 5 * time.Second
)

// conversationStepOptions are the options of a short conversation Step: it
// locks MessageWindowLock and the given Attributes and loads the given maps.
func conversationStepOptions(maps []dex.AttributeDef, locks ...dex.AttributeLock) *dex.StepOptions {
	return &dex.StepOptions{WaitForMethodTimeout: time.Minute, ExecuteMethodTimeout: time.Minute, ExecuteLoadAttributeMaps: maps, ExecuteLoadAttributeMapInstances: credentialInfoLoads(),
		ExecuteLockAttributes: append([]dex.AttributeLock{dex.LockAttribute(messageWindowLockAttribute)}, locks...)}
}

// lifecycleStepOptions are the lifecycle options with the failure route of one
// Step and, when given, its own Execute locks.
// startStepOptions are InitializeSessionAgent's: the lifecycle locks, the
// requirements an example's card is recorded under, and the current messages
// its notice joins.
func startStepOptions() *dex.StepOptions {
	options := lifecycleStepOptions(nil, statusLock, workspaceLock, requirementsLock)
	options.ExecuteLoadAttributeMaps = messageMaps
	return options
}

// exampleBriefCallID names the card of an example's brief, which no model call
// made, where the card's validation reports its fields.
const exampleBriefCallID conversation.CallID = "example-brief"

// admitStartInput admits the start input in the start Step's commit: the
// creator's first message into QueuedUserMessages and, for a Project created
// from an example, the example in AgentState. A picked example's card becomes
// version 1 of the requirements, with its design built at the setup commit,
// and the notice that greets the person; a brief without a card the card's
// schema accepts is the notice alone. It returns the workspace demand that
// provisions the Sandbox and deploys the setup commit's Preview, the notice's
// sequence naming that deployment, and false without a picked example. A
// matched example records nothing else: the first message is the request, the
// agent writes version 1 and the first turn demands the workspace.
func admitStartInput(ctx dex.Context, input agentloop.SessionStartInput) (workspaceDemand, bool) {
	if input.FirstMessage != nil {
		comm.MustDo(queuedUserMessagesChannel.Publish(ctx, *input.FirstMessage))
	}
	brief := input.ExampleBrief
	if brief == nil {
		return workspaceDemand{}, false
	}
	state := comm.Must(agentStateAttribute.Get(ctx))
	state.StartingExample = brief.StartingExample()
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	if state.StartingExample.Choice == agentloop.ExampleChoiceMatched {
		return workspaceDemand{}, false
	}
	problem := brief.Problem
	if problem == "" {
		call := conversation.ToolCall{ID: exampleBriefCallID, Name: conversation.ToolConfirmRequirements, Arguments: conversation.JSONObject(brief.Card)}
		if card, design, err := agentloop.RequestedRequirements(call, nil); err != nil {
			problem = err.Error()
		} else {
			recorded := agentloop.RecordRequirements(nil, card, "", ctx.FirstAttemptAt().UTC())
			recordFlowDesign(ctx, nil, recorded.Spec.Design, design)
			comm.MustDo(confirmedRequirementsAttribute.Set(ctx, recorded))
			if recorded.Spec.Design != nil {
				comm.MustDo(builtFlowDesignAttribute.Set(ctx, recorded.Spec.Design.Digest))
				comm.MustDo(builtFlowDesignCommitAttribute.Set(ctx, comm.Must(headCommitAttribute.Get(ctx))))
			}
		}
	}
	notice := agentloop.ExampleStartedNotice(*brief)
	if problem != "" {
		notice = agentloop.ExampleBriefRejectedNotice(*brief, problem)
	}
	sequence := appendEntry(ctx, notice)
	state = comm.Must(agentStateAttribute.Get(ctx))
	state.AwaitsFirstMessage = input.FirstMessage == nil
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	return workspaceDemand{DeploysCommit: comm.Must(headCommitAttribute.Get(ctx)), DeploySequence: sequence}, true
}

func lifecycleStepOptions(failure *dex.ExecuteFailure, locks ...dex.AttributeLock) *dex.StepOptions {
	options := *sessionLifecycleStepOptions
	options.ExecuteFailure = failure
	if len(locks) > 0 {
		options.ExecuteLockAttributes = locks
	}
	return &options
}

// withExecuteFailure is a copy of options with the Execute failure route of a Step.
func withExecuteFailure(options *dex.StepOptions, failure *dex.ExecuteFailure) *dex.StepOptions {
	copied := *options
	copied.ExecuteFailure = failure
	return &copied
}

// interruptStepOptions are the options of one InterruptAgent: a few idempotent
// Sandbox stops under a heartbeat, without a lock, and its failure route.
func interruptStepOptions(failure *dex.ExecuteFailure) *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: 3 * time.Minute, HeartbeatTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteRetry: dexflow.StepRetry(6 * time.Minute), ExecuteLoadAttributeMaps: taskMaps, ExecuteFailure: failure}
}

// interruptMovementOptions are the options of the InterruptAgent of one Stop,
// whose failure route to FinishInterrupt carries that Stop's loads.
func interruptMovementOptions(interruption agentInterruption) *dex.StepOptions {
	return interruptStepOptions(dex.ProceedToOnExecuteFailure(FinishInterrupt{}, finishInterruptOptions(interruption)))
}

// finishInterruptOptions load, for a Stop that ends a wave, the results its
// branches published before the Stop; FinishInterrupt otherwise needs only its
// registered options.
func finishInterruptOptions(interruption agentInterruption) *dex.StepOptions {
	running := interruption.Running
	if !interruption.EndsTurn || running == nil || running.Kind != agentloop.RunningWave {
		return nil
	}
	return &dex.StepOptions{ExecuteLoadChannelMapInstances: []dex.ChannelMapLoad{parallelToolResultsChannel.LoadMessages(running.BatchID)}}
}

func remoteIdleStepOptions() *dex.StepOptions {
	options := *awaitUserStepOptions
	options.WaitForLoadChannels = []dex.ChannelDef{remoteInboxChannel}
	options.ExecuteLoadChannels = []dex.ChannelDef{remoteInboxChannel}
	options.WaitForLockAttributes = []dex.AttributeLock{executorLock, interruptLock}
	return &options
}

func remoteStepOptions() *dex.StepOptions {
	options := *checkSteeredStepOptions
	options.WaitForLoadChannels = []dex.ChannelDef{remoteInboxChannel}
	options.ExecuteLoadChannels = []dex.ChannelDef{remoteInboxChannel}
	options.WaitForLockAttributes = []dex.AttributeLock{executorLock, interruptLock}
	return &options
}

// serialToolStepOptions is the execution policy of one serial ExecuteTool
// movement. A call whose retries are exhausted is recorded as a platform
// failure the model reads; a bash call has the polling options of its own
// timeout and is settled from the command ledger.
func serialToolStepOptions(ctx dex.Context, call conversation.ToolCall) *dex.StepOptions {
	if call.Name == conversation.ToolBash {
		return commandStepOptions(call)
	}
	options := parallelToolStepOptions(agentloop.CatalogTool(call.Name))
	options.ExecuteLoadAttributeMaps = messageMaps
	configureConnectorToolOptions(options, call)
	configureTaskWorktreeToolOptions(options, call)
	switch agentID, isSubagent := subagentOfCall(call); {
	case call.Name == conversation.ToolStartAgent:
		// The new worktree keeps the worktrees of the unfinished subagents.
		options.ExecuteLoadAttributeMaps = taskMessageMaps
		options.ExecuteLoadAttributeMapInstances = credentialInfoLoads()
		model := subagentCredentialModel(ctx, call)
		if !model.IsMock() {
			options.ExecuteLoadAttributeMapInstances = append(options.ExecuteLoadAttributeMapInstances, modelKeysAttribute.Load(modelcredential.ForModel(model).String()))
		}
		options.ExecuteLockAttributes = append(options.ExecuteLockAttributes, credentialInfoLocks()...)
	case call.Name == conversation.ToolReportEvolveReview:
		// A report pins or unpins records by review, at most two pinned.
		options.ExecuteLoadAttributeMaps = taskMessageMaps
	case isSubagent:
		options.ExecuteLoadAttributeMapInstances = []dex.AttributeMapLoad{subagentsAttribute.Load(agentID)}
	}
	options.ExecuteFailure = ExecuteTool{}.GetStepOptions().ExecuteFailure
	return options
}

// commandStepOptions is the policy of an ExecuteTool movement for bash: a
// polling Step bounded by the call's own timeout, whose failure route settles
// the command from the Sandbox's command ledger.
func commandStepOptions(call conversation.ToolCall) *dex.StepOptions {
	options := dexflow.PollingStepOptions(sandboxtools.CommandTimeout(call.Arguments.String()), ExecuteTool{command: true}.GetStepOptions().ExecuteFailure)
	options.ExecuteLoadAttributeMaps = []dex.AttributeDef{currentMessagesAttribute, backgroundCommandsAttribute}
	return options
}

// parallelToolStepOptions is the execution policy of one ExecuteParallelTool
// branch. A branch whose retries are exhausted always publishes a result, so
// that the join can resolve the batch.
func parallelToolStepOptions(definition agentloop.ToolDefinition) *dex.StepOptions {
	return &dex.StepOptions{
		ExecuteMethodTimeout:     definition.AttemptTimeout,
		ExecuteLoadAttributeMaps: remoteWorktreeFileMapLoads(definition.Name),
		HeartbeatTimeout:         time.Minute,
		ExecuteDurability:        toolExecuteDurability(definition),
		ExecuteRetry: &dex.RetryPolicy{
			InitialInterval: agentloop.ToolRetryInitialInterval, BackoffCoefficient: agentloop.ToolRetryBackoffCoefficient, MaximumInterval: agentloop.ToolRetryMaximumInterval,
			// #nosec G115 -- the catalog init check bounds MaximumAttempts to int32.
			MaximumAttempts: int32(definition.MaximumAttempts), TotalDuration: definition.RetryTotalDuration,
		},
		ExecuteFailure: ExecuteParallelTool{}.GetStepOptions().ExecuteFailure,
	}
}

// toolExecuteDurability starts a long-running call directly as a synchronous
// activity; others use the default asynchronous local execution.
func toolExecuteDurability(definition agentloop.ToolDefinition) dex.StepDurability {
	if definition.LongRunning {
		return dex.StepDurabilitySync
	}
	return dex.StepDurabilityDefault
}

// userCommandRPC locks SessionStatus, which the Project's deletion fences, and
// the Attributes the command decides on.
func userCommandRPC(isTransactional bool, loadChannels []dex.ChannelDef, locks ...dex.AttributeLock) *dex.RPCOptions {
	locks = append([]dex.AttributeLock{statusLock}, locks...)
	return &dex.RPCOptions{Timeout: commandRPCTimeout, IsTransactional: isTransactional, LockAttributes: locks, LoadChannels: loadChannels}
}

// loadDesigns adds the exact FlowDesigns instances of slots to options.
func loadDesigns(options *dex.RPCOptions, slots ...flowdesign.Slot) *dex.RPCOptions {
	for _, slot := range slots {
		options.LoadAttributeMapInstances = append(options.LoadAttributeMapInstances, flowDesignsAttribute.Load(string(slot)))
	}
	return options
}

// flowDesignRPCOutput is GetFlowDesign's result: the design a slot holds.
type flowDesignRPCOutput struct {
	Design flowdesign.Design `json:"design"`
	Found  bool              `json:"found"`
}

// recordFlowDesign moves FlowDesigns to a card recorded over stored, whose
// version carries recorded: the card's own design becomes the current one, a
// card without one keeps the stored design while no push built it, and a card
// that replaces the requirements, or revises a built design, without one
// deletes it. A design an old waiting card left is deleted. WrittenDesignSteps follows the current design: a new design starts
// it afresh, keeping the written Steps it keeps, and a deleted one deletes it.
func recordFlowDesign(ctx dex.Context, stored *agentloop.ConfirmedRequirements, recorded *flowdesign.Reference, design *flowdesign.Design) {
	switch {
	case design != nil:
		comm.MustDo(flowDesignsAttribute.Set(ctx, string(flowdesign.SlotConfirmed), *design))
		comm.MustDo(writtenDesignStepsAttribute.Set(ctx, flowdesign.NewWrittenSteps(*design, dexflow.OptionalValue(writtenDesignStepsAttribute.Get(ctx)))))
	case recorded == nil && stored != nil && stored.Spec.Design != nil:
		comm.MustDo(flowDesignsAttribute.Delete(ctx, string(flowdesign.SlotConfirmed)))
		comm.MustDo(writtenDesignStepsAttribute.Delete(ctx))
	}
	comm.MustDo(flowDesignsAttribute.Delete(ctx, string(flowdesign.SlotPending)))
}

// recordDesignProgress records, in the commit of a tool result, how far the
// agent built the current Flow design: a succeeded write or edit marks the
// designed Steps it declares written, and a result that BuildsDesign records
// that the push built the design, if there is one, and its commit.
func recordDesignProgress(ctx dex.Context, call conversation.ToolCall, result agentloop.ToolExecutionResult) {
	if filePath, text, isWritten := agentloop.WrittenSource(call, result); isWritten {
		if written := dexflow.OptionalValue(writtenDesignStepsAttribute.Get(ctx)); written != nil && written.Record(filePath, text) {
			comm.MustDo(writtenDesignStepsAttribute.Set(ctx, *written))
		}
	}
	confirmed := dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx))
	if confirmed != nil && confirmed.Spec.Design != nil && agentloop.BuildsDesign(call, result) {
		comm.MustDo(builtFlowDesignAttribute.Set(ctx, confirmed.Spec.Design.Digest))
		comm.MustDo(builtFlowDesignCommitAttribute.Set(ctx, result.HeadCommit))
	}
}

// confirmedRevision is the revision of the confirmed requirements, 0 while
// none is recorded, which a requirements draft names as its base.
func confirmedRevision(ctx dex.Context) int64 {
	return dexflow.ValueOrZero(confirmedRequirementsAttribute.Get(ctx)).Revision
}

func readRemote(ctx dex.Context, input remoteexec.ReadRequest) (*dex.RPCResult[RemoteRead], error) {
	result := &dex.RPCResult[RemoteRead]{}
	if !acceptsUserCommand(ctx) || input.ProjectID != comm.Must(projectIDAttribute.Get(ctx)) {
		result.Output.Refusal = &remoteexec.ReportRefusal{Code: remoteexec.RefusalSessionClosed, Message: "Session does not accept reads"}
		return result, nil
	}
	executor := currentExecutor(ctx)
	result.Output.Executor = executor
	if err := executor.CheckReader(input); err != nil {
		result.Output.Refusal = &remoteexec.ReportRefusal{Code: remoteexec.RefusalExecutorChanged, Message: "executor changed", Epoch: executor.Epoch, Generation: executor.Generation}
		return result, nil
	}
	if executor.Mode == remoteexec.ModePlatform {
		result.Output.Delivery.Through = input.After
		return result, nil
	}
	if executor.Remote.ActivationSequence != nil {
		input.HistoryThrough = *executor.Remote.ActivationSequence
	}
	state := comm.Must(agentStateAttribute.Get(ctx))
	if input.After < 0 || input.After > state.LastSequence || len(input.ArchiveKeys) > 1 || len(input.CurrentKeys) > 16 {
		result.Output.Refusal = &remoteexec.ReportRefusal{Code: remoteexec.RefusalMalformed, Message: "invalid remote cursor or archive selection"}
		return result, nil
	}
	last := state.LastSequence
	result.Output.Delivery = remoteexec.Delivery{FirstRetainedSequence: state.FirstRetainedSequence, LastSequence: last}
	first := last
	if input.After < last {
		first = max(input.After+1, state.FirstRetainedSequence)
	}
	end := last
	var archiveKeys, currentKeys []string
	if input.After < last {
		if first < state.CurrentFirstSequence {
			chunkFirst := (first-1)/agentloop.ArchiveMessageChunkSize*agentloop.ArchiveMessageChunkSize + 1
			end = min(last, chunkFirst+agentloop.ArchiveMessageChunkSize-1)
			archiveKeys = []string{agentloop.SequenceKey(chunkFirst)}
		} else {
			if last-first > 15 {
				end = first + 15
			}
			for sequence := first; ; sequence++ {
				currentKeys = append(currentKeys, agentloop.SequenceKey(sequence))
				if sequence == end {
					break
				}
			}
		}
	}
	if !slices.Equal(archiveKeys, input.ArchiveKeys) || !slices.Equal(currentKeys, input.CurrentKeys) {
		result.Output.NeedArchiveKeys, result.Output.NeedCurrentKeys = archiveKeys, currentKeys
		result.Output.NeedsArchives = true
		return result, nil
	}
	var entries []conversation.SequencedEntry
	if input.After < last {
		for sequence := first; sequence <= end; sequence++ {
			entry, found := loadedEntry(ctx, state, sequence)
			if !found {
				return nil, fmt.Errorf("remote read missing selected entry %d", sequence)
			}
			entries = append(entries, conversation.SequencedEntry{Sequence: sequence, Entry: entry})
			if sequence == end {
				break
			}
		}
	}
	delivery, err := remoteexec.BoundedDelivery(input, state.FirstRetainedSequence, last, entries)
	if err != nil {
		return nil, err
	}
	result.Output.Delivery = delivery
	result.Output.Head = comm.Must(headCommitAttribute.Get(ctx))
	result.Output.SettledThrough = executor.SettledThrough(state.PendingToolCalls[state.PendingToolIndex:])
	result.Output.Interrupt = dexflow.OptionalValue(agentInterruptAttribute.Get(ctx))
	if pending := dexflow.OptionalValue(pendingUserInputAttribute.Get(ctx)); pending != nil {
		projected := &RemotePendingInput{Kind: pending.Kind.CurrentKind()}
		if len(pending.CallID) <= 48 && remoteexec.IsCallID(executor.Epoch, pending.CallID) {
			projected.CallID = pending.CallID
		}
		var text strings.Builder
		for _, question := range pending.Questions {
			fmt.Fprintf(&text, "%s: %s\n", question.Header, question.Question)
			for _, option := range question.Options {
				fmt.Fprintf(&text, "- %s: %s\n", option.Label, option.Description)
			}
		}
		if pending.Requirements != nil {
			text.WriteString((agentloop.SegmentStart{ConfirmedRequirements: &agentloop.ConfirmedRequirements{Spec: *pending.Requirements}}).Text())
		}
		projected.Text = cappedStateText(text.String(), "question", agentloop.MaximumProgressJSONBytes-512)
		result.Output.PendingInput = projected
	}
	if requirements := dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx)); requirements != nil {
		result.Output.Requirements = (agentloop.SegmentStart{ConfirmedRequirements: requirements}).Text()
		if len(comm.Must(json.Marshal(result.Output.Requirements))) > agentloop.MaximumProgressJSONBytes {
			result.Output.Requirements = "[truncated: open Studio for the full requirements]"
		}
	}
	if todos := dexflow.OptionalValue(todoListAttribute.Get(ctx)); todos != nil {
		if len(comm.Must(json.Marshal(todos.Items))) > agentloop.MaximumProgressJSONBytes {
			result.Output.Todos = []agentloop.TodoItem{{Content: "[truncated: open Studio for the full todos]", Status: conversation.TaskStatusPending}}
		} else {
			result.Output.Todos = todos.Items
		}
	}
	{
		workspace := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
		result.Output.RepositoryContextAvailable = workspace.RepositoryContext != ""
		if input.RepositoryContext {
			result.Output.RepositoryContext = workspace.RepositoryContext
			// A picked example is described to the remote agent until the person's first message.
			result.Output.StartingExample = startingExampleSection(ctx, state.AwaitsFirstMessage)
			result.Output.NoCardDesign = recordsNoCardDesign(ctx)
		}
		result.Output.WorkspaceState = workspace.State
	}
	return result, nil
}

// cappedStateText measures JSON escaping, not only raw UTF-8. It changes only
// the transient projection; durable state and exact routing IDs stay intact.
func cappedStateText(text, field string, budget int) string {
	if len(comm.Must(json.Marshal(text))) <= budget {
		return text
	}
	marker := "\n[truncated: open Studio for the full " + field + "]"
	low, high := 0, len(text)
	for low < high {
		middle := low + (high-low+1)/2
		if len(comm.Must(json.Marshal(comm.TruncateUTF8(text, middle)+marker))) <= budget {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return comm.TruncateUTF8(text, low) + marker
}

// getFlowDesign reads the exact slot loaded by the scoped read RPC.
func getFlowDesign(ctx dex.Context, slot flowdesign.Slot) (*dex.RPCResult[flowDesignRPCOutput], error) {
	design, err := flowDesignsAttribute.Get(ctx, string(slot))
	if dexflow.IsAttributeNotFound(err) {
		return &dex.RPCResult[flowDesignRPCOutput]{}, nil
	}
	return &dex.RPCResult[flowDesignRPCOutput]{Output: flowDesignRPCOutput{Design: comm.Must(design, err), Found: true}}, nil
}

// ReadModelCredentialInfo reads only the provider's exact non-secret instance.
func readModelCredentialInfo(ctx dex.Context, provider modelcredential.Provider) (*dex.RPCResult[modelcredential.Info], error) {
	info, err := modelKeyInfoAttribute.Get(ctx, provider.String())
	if dexflow.IsAttributeNotFound(err) {
		err = nil
	}
	return &dex.RPCResult[modelcredential.Info]{Output: info}, err
}

// ReserveModelCredential serializes one writer across replicas without recording a secret.
func reserveModelCredential(ctx dex.Context, input modelcredential.Mutation) (*dex.RPCResult[modelcredential.Admission], error) {
	if !acceptsUserCommand(ctx) {
		return nil, ErrCommandRejected
	}
	info := dexflow.ValueOrZero(modelKeyInfoAttribute.Get(ctx, input.Provider.String()))
	result := modelcredential.Admission{Info: info}
	if info.OperationID == input.OperationID && info.OperationRevision == input.ExpectedRevision && info.Delete == input.Delete {
		if input.RetryDelete && input.Delete && info.Status == modelcredential.StatusPending {
			info.WriteAttempt++
			comm.MustDo(modelKeyInfoAttribute.Set(ctx, input.Provider.String(), info))
		}
		result.Info, result.Admitted = info, true
	} else if info.Revision == input.ExpectedRevision && info.Status != modelcredential.StatusPending {
		if input.Reason == (modelcredential.ChangeReason{}) {
			input.Reason = modelcredential.ChangedReason
		}
		previous := info
		previous.Previous = nil
		revision := max(info.Revision, info.HighWaterRevision) + 1
		info = modelcredential.Info{Endpoint: input.Endpoint, ChangeReason: input.Reason, Holders: info.Holders, Previous: &previous, HighWaterRevision: revision, OperationRevision: input.ExpectedRevision, Revision: revision, Status: modelcredential.StatusPending, OperationID: input.OperationID, Delete: input.Delete, SetBy: input.Actor, SetAt: time.Now().UTC(), Last4: input.Last4}
		comm.MustDo(modelKeyInfoAttribute.Set(ctx, input.Provider.String(), info))
		result = modelcredential.Admission{Info: info, Admitted: true, Fresh: true}
	}
	return &dex.RPCResult[modelcredential.Admission]{Output: result}, nil
}

// FinalizeModelCredential publishes metadata only after exact secret readback and copy cleanup.
func finalizeModelCredential(ctx dex.Context, input modelcredential.Mutation) (*dex.RPCResult[modelcredential.Info], error) {
	info := comm.Must(modelKeyInfoAttribute.Get(ctx, input.Provider.String()))
	if info.OperationID != input.OperationID || info.OperationRevision != input.ExpectedRevision || info.Delete != input.Delete {
		return nil, modelcredential.ErrRevision
	}
	if info.Status == modelcredential.StatusPending {
		if input.Cancel {
			previous := modelcredential.Info{}
			if info.Previous != nil {
				previous = *info.Previous
			}
			previous.HighWaterRevision = max(info.HighWaterRevision, info.Revision)
			comm.MustDo(modelKeyInfoAttribute.Set(ctx, input.Provider.String(), previous))
			return &dex.RPCResult[modelcredential.Info]{Output: previous}, nil
		}
		info.Previous = nil
		info.Holders = nil
		info.Status = modelcredential.StatusActive
		if input.Delete {
			info.Status = modelcredential.StatusRevoked
			info.Last4 = ""
		}
		comm.MustDo(modelKeyInfoAttribute.Set(ctx, input.Provider.String(), info))
	}
	return &dex.RPCResult[modelcredential.Info]{Output: info}, nil
}

// closedReplayRange captures the closed segment's inclusive history range.
// Cleanup changes payloads only; message identity and business history stay owned by their maps.
type closedReplayRange struct {
	First conversation.Sequence `json:"first"`
	Last  conversation.Sequence `json:"last"`
}

func closedModelReplayRange(ctx dex.Context) closedReplayRange {
	segment := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx))
	if segment == nil || modelprovider.ProfileOf(segment.Model).Capabilities.Replay != conversation.ReplayEncryptedOpaque {
		return closedReplayRange{}
	}
	return closedReplayRange{First: segment.FirstSequence, Last: comm.Must(agentStateAttribute.Get(ctx)).LastSequence}
}

// closedReplayOptions includes even currently unarchived chunks: a queued cleanup
// may run after the window moves. It never requests the whole ArchivedMessages map.
func closedReplayOptions(scope closedReplayRange) *dex.StepOptions {
	options := conversationStepOptions(messageMaps)
	options.ExecuteRetry = shortStepRetry
	if scope.First <= 0 {
		return options
	}
	size := conversation.Sequence(agentloop.ArchiveMessageChunkSize)
	for first := agentloop.ArchiveChunkFirst(scope.First); first <= scope.Last; first += size {
		options.ExecuteLoadAttributeMapInstances = append(options.ExecuteLoadAttributeMapInstances, archivedMessagesAttribute.Load(agentloop.SequenceKey(first)))
	}
	return options
}

// projectCompletionOptions retains lifecycle durability and its status fence
// while adding the message lock and exact replay loads.
func projectCompletionOptions(scope closedReplayRange) *dex.StepOptions {
	options := closedReplayOptions(scope)
	lifecycle := lifecycleStepOptions(nil)
	options.ExecuteDurability = lifecycle.ExecuteDurability
	options.ExecuteMethodTimeout = lifecycle.ExecuteMethodTimeout
	options.ExecuteLockAttributes = append(lifecycle.ExecuteLockAttributes, options.ExecuteLockAttributes...)
	return options
}

func purgeClosedModelReplay(ctx dex.Context, scope closedReplayRange) {
	if scope.First <= 0 {
		return
	}
	state := comm.Must(agentStateAttribute.Get(ctx))
	agentloop.PurgeModelReplay(state, scope.First, scope.Last, agentloop.ModelReplayHistory{
		ReadCurrent:  func(key string) conversation.Entry { return comm.Must(currentMessagesAttribute.Get(ctx, key)) },
		WriteCurrent: func(key string, entry conversation.Entry) { comm.MustDo(currentMessagesAttribute.Set(ctx, key, entry)) },
		ReadArchive: func(key string) conversation.ArchivedEntryChunk {
			return comm.Must(archivedMessagesAttribute.Get(ctx, key))
		},
		WriteArchive: func(key string, chunk conversation.ArchivedEntryChunk) {
			comm.MustDo(archivedMessagesAttribute.Set(ctx, key, chunk))
		},
	})
}

func completeProjectSession(ctx dex.Context) (agentloop.SessionCleanupReceipt, error) {
	status, reason := comm.Must(sessionStatusAttribute.Get(ctx)), comm.Must(closeReasonAttribute.Get(ctx))
	if status != agentloop.SessionStatusArchived || reason != agentloop.CloseReasonProjectCleanupComplete.String() {
		return agentloop.SessionCleanupReceipt{}, dex.ErrorWithStack(fmt.Errorf("project cleanup admission missing: Session is %s with reason %q", status, reason))
	}
	purgeClosedModelReplay(ctx, closedModelReplayRange(ctx))
	return agentloop.SessionCleanupReceipt{ProjectID: readSessionIdentity(ctx).ProjectID, Reason: reason}, nil
}

func sessionSummary(ctx dex.Context, head string) *dex.RPCResult[agentloop.SessionSummary] {
	status := comm.Must(sessionStatusAttribute.Get(ctx))
	summary := agentloop.SessionSummary{
		Identity: agentloop.SessionIdentity{ProjectID: comm.Must(projectIDAttribute.Get(ctx)), Profile: comm.Must(sessionProfileAttribute.Get(ctx)),
			HeadCommit: head},
		Status: status, CloseReason: comm.Must(closeReasonAttribute.Get(ctx)), Revision: comm.Must(sessionRevisionAttribute.Get(ctx)),
		CreatedAt: comm.Must(sessionCreatedAtAttribute.Get(ctx)), UpdatedAt: comm.Must(sessionUpdatedAtAttribute.Get(ctx)),
		Workspace: dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).Summary(),
	}
	// WorkspaceUse is missing until InitializeSessionAgent commits; only a value read proves a drained wave.
	use, err := workspaceUseAttribute.Get(ctx)
	summary.WorkspaceUse, summary.WorkspaceUseKnown = dexflow.ValueOrZero(use, err), err == nil
	return &dex.RPCResult[agentloop.SessionSummary]{Output: summary}
}

// UpdateSessionModel selects the Session's model at an exact revision at any time.
// The next turn rolls the context over to it; history, pending input and workspace stay.
func updateSessionModel(ctx dex.Context, input agentloop.UpdateSessionModelRequest) (*dex.RPCResult[modelSelection], error) {
	if !acceptsUserCommand(ctx) {
		return &dex.RPCResult[modelSelection]{}, nil
	}
	if !modelCredentialReady(ctx, input.Model) {
		return &dex.RPCResult[modelSelection]{Output: modelSelection{CredentialMissing: true}}, nil
	}
	current := comm.Must(sessionModelAttribute.Get(ctx))
	selected, isAccepted := agentloop.ChangeSessionModel(current, input)
	if selected != current {
		comm.MustDo(sessionModelAttribute.Set(ctx, selected))
	}
	return &dex.RPCResult[modelSelection]{Output: modelSelection{Accepted: isAccepted, Stale: !isAccepted}}, nil
}

// modelCredentialReady reads the non-secret admission fence; model Steps also verify the envelope.
func modelCredentialReady(ctx dex.Context, model conversation.Model) bool {
	if model.IsMock() || currentExecutor(ctx).Mode == remoteexec.ModeRemote {
		return true
	}
	info := dexflow.ValueOrZero(modelKeyInfoAttribute.Get(ctx, modelcredential.ForModel(model).String()))
	return info.Active() && (model.IsCustom() == (info.Endpoint.BaseURL != ""))
}

func credentialAdmissionRPC(options *dex.RPCOptions) *dex.RPCOptions {
	options.LoadAttributeMapInstances = append(options.LoadAttributeMapInstances, credentialInfoLoads()...)
	return options
}

// prepareSubagentCredential commits a bounded holder before a child can receive a key.
func (flow *SessionAgentFlow) prepareSubagentCredential(ctx dex.Context, input toolInvocation) (*dex.StepDecision, error) {
	input.CredentialPrepared = true
	id := flowid.AgentTaskID(ctx.FlowID(), string(input.Call.ID))
	record := comm.Must(subagentsAttribute.Get(ctx, id))
	if record.Model == "" {
		record.Model = comm.Must(sessionModelAttribute.Get(ctx)).Model
		comm.MustDo(subagentsAttribute.Set(ctx, id, record))
	}
	model := subagentCredentialModel(ctx, input.Call)
	if !model.IsMock() {
		provider := modelcredential.ForModel(model)
		info := dexflow.ValueOrZero(modelKeyInfoAttribute.Get(ctx, provider.String()))
		if !info.Active() {
			return rejectCredentialStart(ctx, input.Call)
		}
		agentID := flowid.AgentTaskID(ctx.FlowID(), string(input.Call.ID))
		for _, holder := range info.Holders {
			if holder.AgentID == agentID && holder.CallID == string(input.Call.ID) {
				return dex.GoTo(ExecuteTool{}, input, dex.WithStepOptions(serialToolStepOptions(ctx, input.Call))), nil
			}
		}
		if len(info.Holders) >= agenttasks.MaximumRunningSubagents {
			cleared, err := flow.dependency.ReconcileSubagentCredentials(ctx, readSessionIdentity(ctx).ProjectID, info.Holders, fmt.Sprintf("%s/attempt-%d", input.Call.ID, ctx.Attempt()))
			if err != nil {
				return nil, dex.ErrorWithStack(err)
			}
			info.Holders = slices.DeleteFunc(info.Holders, func(h modelcredential.Holder) bool { return slices.Contains(cleared, h.AgentID) })
		}
		if len(info.Holders) >= agenttasks.MaximumRunningSubagents {
			return rejectCredentialStart(ctx, input.Call)
		}
		info.Holders = append(info.Holders, modelcredential.Holder{AgentID: agentID, CallID: string(input.Call.ID), Revision: info.Revision})
		comm.MustDo(modelKeyInfoAttribute.Set(ctx, provider.String(), info))
	}
	return dex.GoTo(ExecuteTool{}, input, dex.WithStepOptions(serialToolStepOptions(ctx, input.Call))), nil
}

func rejectCredentialStart(ctx dex.Context, call conversation.ToolCall) (*dex.StepDecision, error) {
	appendSessionFailure(ctx, call, toolfailure.ModelCredentialUnavailable, "The Project key is unavailable or its previous subagents still hold it. Finish or stop the previous tasks, or replace the Project key.")
	forgetSubagentStart(ctx, flowid.AgentTaskID(ctx.FlowID(), string(call.ID)))
	return dex.GoTo(CheckSteered{}, advancePendingTools(ctx, 1)), nil
}

// startCredentialSubagent passes the transient key only to the in-process cross-child Service.
func (flow *SessionAgentFlow) startCredentialSubagent(ctx dex.Context, execution context.Context, start agenttasks.SubagentStart) error {
	key, err := preparedSubagentCredential(ctx, start.Identity.AgentID, start.Model)
	if err != nil {
		return err
	}
	start.CredentialRevision = key.Revision
	if start.Model.IsCustom() {
		start.Endpoint = comm.Must(modelKeyInfoAttribute.Get(ctx, modelcredential.ForModel(start.Model).String())).Endpoint
	}
	return flow.dependency.StartSubagent(execution, start, key, ctx.Attempt())
}

func preparedSubagentCredential(ctx dex.Context, agentID string, model conversation.Model) (modelcredential.Envelope, error) {
	key, err := sessionModelCredential(ctx, model)
	if err != nil {
		return modelcredential.Envelope{}, err
	}
	if !model.IsMock() {
		info := comm.Must(modelKeyInfoAttribute.Get(ctx, modelcredential.ForModel(model).String()))
		var admitted bool
		for _, holder := range info.Holders {
			if holder.AgentID == agentID && holder.Revision == key.Revision {
				admitted = true
				break
			}
		}
		if !admitted {
			return modelcredential.Envelope{}, modelcredential.ErrUnavailable
		}
	}
	return key, nil
}

func credentialStartFailure(ctx dex.Context, call conversation.ToolCall, agentID string, err error) (agentloop.ToolExecutionResult, error) {
	if errors.Is(err, modelcredential.ErrUnavailable) {
		forgetSubagentStart(ctx, agentID)
		return agentloop.SessionFailureResult(toolfailure.ModelCredentialUnavailable, "The model credential changed; this subagent was not started."), nil
	}
	if errors.Is(err, modelcredential.ErrCopyStopped) {
		return agentloop.FailureResult(conversation.ToolFailure{Code: toolfailure.ModelCredentialUnavailable, Effect: toolfailure.EffectPartial, Message: "The model credential changed; this subagent was stopped. Its task notification follows."}), nil
	}
	return agentloop.ToolExecutionResult{}, fmt.Errorf("start subagent %s: %w", agentID, err)
}

// credentialInfoLoads covers the closed provider set without loading any secret.
func credentialInfoLoads() []dex.AttributeMapLoad {
	loads := make([]dex.AttributeMapLoad, 0, len(modelcredential.Providers()))
	for _, provider := range modelcredential.Providers() {
		loads = append(loads, modelKeyInfoAttribute.Load(provider.String()))
	}
	return loads
}

func credentialInfoLocks() []dex.AttributeLock {
	locks := make([]dex.AttributeLock, 0, len(modelcredential.Providers()))
	for _, provider := range modelcredential.Providers() {
		locks = append(locks, dex.LockAttributeMap(modelKeyInfoAttribute, provider.String()))
	}
	return locks
}

func modelCredentialVersion(ctx dex.Context, selected conversation.Model) string {
	var version strings.Builder
	version.WriteString(string(selected))
	for _, provider := range modelcredential.Providers() {
		info := dexflow.ValueOrZero(modelKeyInfoAttribute.Get(ctx, provider.String()))
		fmt.Fprintf(&version, ";%s/%d/%s/%d", provider.String(), info.Revision, info.Status.String(), info.HighWaterRevision)
	}
	return version.String()
}

// subagentCredentialModel freezes old STARTING records before their first copy.
func subagentCredentialModel(ctx dex.Context, call conversation.ToolCall) conversation.Model {
	id := flowid.AgentTaskID(ctx.FlowID(), string(call.ID))
	record := comm.Must(subagentsAttribute.Get(ctx, id))
	if record.Model != "" {
		return record.Model
	}
	return comm.Must(sessionModelAttribute.Get(ctx)).Model
}

// credentialSegmentChanged also fences official segments written before revision freezing existed.
func credentialSegmentChanged(ctx dex.Context, segment agentloop.ContextSegment) bool {
	if segment.Model.IsMock() {
		return false
	}
	info := dexflow.ValueOrZero(modelKeyInfoAttribute.Get(ctx, modelcredential.ForModel(segment.Model).String()))
	return segment.Endpoint != info.Endpoint || segment.CredentialRevision > 0 && segment.CredentialRevision != info.Revision
}
