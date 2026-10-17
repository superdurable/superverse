// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/flowdesign"
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
type modelSelection struct{ Accepted, Stale bool }

// queuedMessageDeletion distinguishes authorization refusal from a stale queue identity.
type queuedMessageDeletion struct{ Deleted, NotAuthor bool }

// setSessionStatus records a lifecycle transition at the next revision.
func setSessionStatus(ctx dex.Context, status agentloop.SessionStatus, reason string) {
	comm.MustDo(sessionStatusAttribute.Set(ctx, status))
	comm.MustDo(closeReasonAttribute.Set(ctx, reason))
	comm.MustDo(sessionRevisionAttribute.Set(ctx, comm.Must(sessionRevisionAttribute.Get(ctx))+1))
	comm.MustDo(sessionUpdatedAtAttribute.Set(ctx, ctx.FirstAttemptAt().UTC()))
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
	return &dex.StepOptions{WaitForMethodTimeout: time.Minute, ExecuteMethodTimeout: time.Minute, ExecuteLoadAttributeMaps: maps,
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
			recorded := agentloop.RecordRequirements(nil, card, ctx.FirstAttemptAt().UTC())
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
// card without one keeps the stored design, and a card that replaces the
// requirements without one deletes it. A design an old waiting card left is
// deleted. WrittenDesignSteps follows the current design: a new design starts
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
