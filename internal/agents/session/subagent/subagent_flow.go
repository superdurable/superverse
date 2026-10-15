// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package subagent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
)

var (
	// The subagent: identity and admitted call (installed with the start), Project, status (which
	// StopSubagent locks), frozen configuration, an early Stop, its lease copy and how it ended.
	identityAttribute = dex.DefineAttribute[agenttasks.SubagentIdentity]("Identity")
	startAttribute    = dex.DefineAttribute[startRecord]("Start")
	// dex:indexed-attribute attribute-key:ProjectID index-key:keyword2 index-type:keyword value-type:string description:"Project of the subagent's Session"
	projectIDAttribute = dex.DefineAttribute[string]("ProjectID", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword2"}))
	// dex:indexed-attribute attribute-key:SubagentStatus index-key:keyword3 index-type:keyword value-type:string description:"Subagent lifecycle status"
	statusAttribute      = dex.DefineAttribute[string]("SubagentStatus", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword3"}))
	configAttribute      = dex.DefineAttribute[Config]("Config")
	stopRequestAttribute = dex.DefineAttribute[bool]("StopRequest")
	workspaceAttribute   = dex.DefineAttribute[Workspace]("Workspace")
	outcomeAttribute     = dex.DefineAttribute[Outcome]("Outcome")

	// The subagent's conversation, kept like the Session's: state, append-only entry log,
	// the context segment with its frozen prompts, and the branches of a parallel tool wave.
	agentStateAttribute            = dex.DefineAttribute[agentloop.AgentState]("AgentState")
	messageWindowLockAttribute     = dex.DefineAttribute[bool]("MessageWindowLock")
	contextSegmentAttribute        = dex.DefineAttribute[agentloop.ContextSegment]("ContextSegment")
	contextSegmentPromptsAttribute = dex.DefineAttributeMap[conversation.SegmentPrompts]("ContextSegmentPrompts")
	currentMessagesAttribute       = dex.DefineAttributeMap[conversation.Entry]("CurrentMessages")
	archivedMessagesAttribute      = dex.DefineAttributeMap[conversation.ArchivedEntryChunk]("ArchivedMessages")
	parallelToolResultsChannel     = dex.DefineChannelMap[parallelToolResult]("ParallelToolResults")

	// Best-effort live output, shared by every SubagentFlow execution; the
	// Session's Watch relays the AgentActivity of a running subagent.
	reasoningSummaryStream = dex.DefineStream[string]("ReasoningSummary", 10<<20)
	assistantTextStream    = dex.DefineStream[string]("AssistantText", 10<<20)
	agentActivityStream    = dex.DefineStream[conversation.AgentEvent]("AgentActivity", 10<<20)
)

// SubagentFlow is a Session's background agent on one bounded task in its own worktree; it reports its end
// as a task notification and reaches its Session only through SubagentFlowDependency.
type SubagentFlow struct {
	dex.FlowDefaults
	dependency  SubagentFlowDependency
	repository  repositoryContextReader
	modelClient conversation.ModelClient
	sandbox     *sandboxtools.Runner
	diagnostics ProjectDiagnosticsReader
	// settings are the server's compaction threshold and thinking behavior;
	// efforts the base effort of each kind.
	settings agentloop.ModelSettings
	efforts  map[agenttasks.SubagentKind]conversation.Effort
}

// GetSteps registers the lifecycle (admission, loop, finish, outcome, report) and the loop,
// which the package README's Step table describes.
func (flow *SubagentFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(InitializeSubagent{flow: flow}), dex.DefineStep(ApplySubagentAdmission{flow: flow}), dex.DefineStep(RecordSubagentAdmissionFailure{}),
		dex.DefineStep(AwaitSubagentDeadline{}),
		dex.DefineStep(CallSubagentModel{flow: flow}), dex.DefineStep(CompactSubagentContext{flow: flow}), dex.DefineStep(RecordSubagentFailure{}),
		dex.DefineStep(RouteSubagentTools{flow: flow}), dex.DefineStep(ExecuteSubagentTool{flow: flow}), dex.DefineStep(SettleSubagentCommand{flow: flow}),
		dex.DefineStep(RecordSubagentToolFailure{flow: flow}), dex.DefineStep(ExecuteSubagentParallelTool{flow: flow}),
		dex.DefineStep(RecoverSubagentParallelToolExecution{}), dex.DefineStep(AwaitSubagentParallelToolResults{flow: flow}),
		dex.DefineStep(RefreshSubagentWorkspace{flow: flow}), dex.DefineStep(BeginSubagentFinish{}), dex.DefineStep(FinishSubagent{flow: flow}),
		dex.DefineStep(RefreshFinishingWorkspace{flow: flow}), dex.DefineStep(RecordSubagentOutcome{}), dex.DefineStep(ReportSubagentOutcome{flow: flow}),
		dex.DefineStep(RecordUndeliveredReport{}),
	}
}

// GetRPCs registers the stop and the retained read.
func (flow *SubagentFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.StopSubagent, dexflow.ShortRPC(statusLock)),
		dex.DefineRPC(flow.GetSubagentSnapshot, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexSummary, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexDisplay, &dex.RPCOptions{Timeout: readRPCTimeout}),
	}
}

// readRPCTimeout bounds a read RPC.
const readRPCTimeout = 5 * time.Second

// GetPersistenceSchema registers every durable value and best-effort Stream.
func (*SubagentFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{
			identityAttribute, startAttribute, projectIDAttribute, statusAttribute, configAttribute, stopRequestAttribute, workspaceAttribute, outcomeAttribute,
			agentStateAttribute, messageWindowLockAttribute, contextSegmentAttribute, contextSegmentPromptsAttribute, currentMessagesAttribute, archivedMessagesAttribute,
		},
		Channels: []dex.ChannelDef{parallelToolResultsChannel},
		Streams:  []dex.StreamDef{reasoningSummaryStream, assistantTextStream, agentActivityStream},
	}
}

// Loop Steps write the message log under MessageWindowLock; only lifecycle Steps and StopSubagent write
// the status, under its lock, so each end is decided once and a short locked Step never delays a stop.
var (
	statusLock   = dex.LockAttribute(statusAttribute)
	messageMaps  = []dex.AttributeDef{currentMessagesAttribute}
	messageLocks = []dex.AttributeLock{dex.LockAttribute(messageWindowLockAttribute)}
	shortRetry   = dexflow.StepRetry(2 * time.Minute)

	loopStepOptions = &dex.StepOptions{WaitForMethodTimeout: time.Minute, ExecuteMethodTimeout: time.Minute, ExecuteRetry: shortRetry,
		ExecuteLoadAttributeMaps: messageMaps, ExecuteLockAttributes: messageLocks}
	statusStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync, ExecuteRetry: shortRetry,
		ExecuteLockAttributes: []dex.AttributeLock{statusLock}}
	// A model call streams for up to ten minutes per attempt; its movement
	// selects the archive chunks and the prompts of the segment.
	modelStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 10 * time.Minute, HeartbeatTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteLoadAttributeMaps: messageMaps, ExecuteLockAttributes: messageLocks,
		ExecuteRetry: dexflow.CappedStepRetry(3, 30*time.Minute)}
	parallelToolRecoveryStepOptions = &dex.StepOptions{ExecuteMethodTimeout: time.Minute}
)

// StopSubagent stops a running subagent (worktree saved, loop and timer cancelled) or records the Stop of
// an initializing one for its admission; any other subagent is ending or ended and a repeat changes nothing.
func (*SubagentFlow) StopSubagent(ctx dex.Context, _ dex.None) (*dex.RPCResult[stopAnswer], error) {
	switch status := statusOf(comm.Must(statusAttribute.Get(ctx))); status {
	case StatusInitializing:
		comm.MustDo(stopRequestAttribute.Set(ctx, true))
		return &dex.RPCResult[stopAnswer]{}, nil
	case StatusRunning:
		comm.MustDo(statusAttribute.Set(ctx, StatusFinishing.String()))
		return (&dex.RPCResult[stopAnswer]{NextSteps: []dex.StepMovement{dex.MovementOf(FinishSubagent{}, subagentFinish{Status: agenttasks.SubagentStopped}, dex.WithStepOptions(finishOptions(ctx)))}}).CancelSteps(
			AwaitSubagentDeadline{}, CallSubagentModel{}, CompactSubagentContext{}, RecordSubagentFailure{}, RouteSubagentTools{}, ExecuteSubagentTool{}, SettleSubagentCommand{},
			RecordSubagentToolFailure{}, ExecuteSubagentParallelTool{}, RecoverSubagentParallelToolExecution{}, AwaitSubagentParallelToolResults{}, RefreshSubagentWorkspace{},
			BeginSubagentFinish{}), nil
	default:
		return &dex.RPCResult[stopAnswer]{Output: stopAnswer{Ended: status.ended()}}, nil
	}
}

// GetSubagentSnapshot reads the subagent without a lock, so it also serves a
// completed execution for the engine's retention.
func (*SubagentFlow) GetSubagentSnapshot(ctx dex.Context, _ dex.None) (*dex.RPCResult[subagentSnapshot], error) {
	identity, start := comm.Must(identityAttribute.Get(ctx)), comm.Must(startAttribute.Get(ctx))
	if comm.Must(projectIDAttribute.Get(ctx)) != identity.ProjectID {
		panic("a subagent's ProjectID differs from its identity")
	}
	snapshot := subagentSnapshot{Identity: identity, Kind: start.Call.Kind, Description: start.Call.Description, BaseCommit: start.BaseCommit,
		Status: statusOf(comm.Must(statusAttribute.Get(ctx))), Config: dexflow.OptionalValue(configAttribute.Get(ctx)), Outcome: dexflow.OptionalValue(outcomeAttribute.Get(ctx))}
	if state := dexflow.OptionalValue(agentStateAttribute.Get(ctx)); state != nil {
		snapshot.LastSequence = state.LastSequence
	}
	return &dex.RPCResult[subagentSnapshot]{Output: snapshot}, nil
}

// GetDexSummary is the subagent row of the Dex Web Flow list; its FlowID names the Session
// and the agent, and Dex shows the indexed status itself.
func (*SubagentFlow) GetDexSummary(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay is the subagent header of the Dex Web Flow view: its status
// and how it ended.
//
// dex:field attribute-key:SubagentStatus value-type:string editable:false description:"Subagent lifecycle status"
// dex:field attribute-key:Outcome value-type:json editable:false description:"How the subagent ended and whether its report reached the Session"
func (*SubagentFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"SubagentStatus": comm.Must(statusAttribute.Get(ctx)), "Outcome": dexflow.OptionalValue(outcomeAttribute.Get(ctx)),
	}}, nil
}

// dex:group group-id:lifecycle group-label:"Lifecycle"
// dex:explanation text:"Ask the Session to admit the subagent, then freeze its configuration and open its first segment with the task, holding no status lock."
type InitializeSubagent struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SubagentFlow
}

// GetStepOptions: provider options whose exhausted retries go to RecordSubagentAdmissionFailure;
// the Session RPC and the repository read hold no status lock, so a StopSubagent never waits.
func (InitializeSubagent) GetStepOptions() *dex.StepOptions {
	options := dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordSubagentAdmissionFailure{}, nil))
	options.ExecuteLockAttributes = messageLocks
	return options
}

// Execute asks for the admission before any effect; a denied subagent prepares nothing, an admitted one
// freezes its configuration and opens its first segment, and ApplySubagentAdmission decides on.
func (step InitializeSubagent) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	identity, start, ws, now := comm.Must(identityAttribute.Get(ctx)), comm.Must(startAttribute.Get(ctx)), comm.Must(workspaceAttribute.Get(ctx)), time.Now().UTC()
	answer, err := step.flow.dependency.AdmitSubagent(ctx, identity, agenttasks.SubagentAdmission{AgentID: identity.AgentID, Fingerprint: start.Fingerprint, BaseCommit: start.BaseCommit})
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("admit subagent %s with its Session: %w", identity.AgentID, err))
	}
	if !answer.Admitted {
		return dex.GoTo(ApplySubagentAdmission{}, answer), nil
	}
	config := step.flow.freeze(start, now)
	ws.RepositoryContext = comm.TruncateUTF8(step.flow.repositoryContext(ctx, start, ws.Lease), agentloop.MaximumRepositoryContextBytes)
	comm.MustDo(configAttribute.Set(ctx, config))
	comm.MustDo(workspaceAttribute.Set(ctx, ws))
	// MessageWindowLock holds no information; Steps only lock it. This write is
	// its one direct use, which the pinned Dex CLI requires of every primitive.
	comm.MustDo(messageWindowLockAttribute.Set(ctx, false))
	state := agentloop.NewAgentState()
	state.Turn, state.Status = 1, agentloop.AgentStatusCallingModel
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	openFirstSegment(ctx, config, ws.RepositoryContext, start.Call.Prompt)
	return dex.GoTo(ApplySubagentAdmission{}, answer), nil
}

// dex:group group-id:lifecycle group-label:"Lifecycle"
// dex:explanation text:"Fail a subagent whose Session could not be asked to admit it, without an effect, and report the failure."
type RecordSubagentAdmissionFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (RecordSubagentAdmissionFailure) GetStepOptions() *dex.StepOptions { return statusStepOptions }

// Execute fails a subagent whose admission could not be asked within its retries; it made no effect,
// so it reports at once and the Session's record ends FAILED instead of waiting to count as lost.
func (RecordSubagentAdmissionFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	failure := "its Session could not be asked to admit it"
	if detail := dexflow.RecoveryDetail(ctx); detail != "" {
		failure += ": " + detail
	}
	finish := subagentFinish{Status: agenttasks.SubagentFailed, Failure: failure}
	comm.MustDo(outcomeAttribute.Set(ctx, Outcome{Status: StatusFailed, Report: describeEnd(finish), Failure: failure, FinishedAt: time.Now().UTC()}))
	comm.MustDo(statusAttribute.Set(ctx, StatusFailed.String()))
	return dex.GoTo(ReportSubagentOutcome{}, nil), nil
}

// dex:group group-id:lifecycle group-label:"Lifecycle"
// dex:explanation text:"Under the status lock, end a denied subagent, reporting one whose start a Stop overtook, stop one that a Stop reached while it initialized, or start its loop beside its deadline."
type ApplySubagentAdmission struct {
	dex.StepDefaultsNoWaitFor[agenttasks.SubagentAdmissionAnswer]
	flow *SubagentFlow
}

func (ApplySubagentAdmission) GetStepOptions() *dex.StepOptions { return statusStepOptions }

// Execute ends a denied subagent without an effect (reporting stopped if a Stop preceded the start, since
// the model waits for it); an admitted one stopped meanwhile saves its worktree, any other starts its loop.
func (step ApplySubagentAdmission) Execute(ctx dex.Context, answer agenttasks.SubagentAdmissionAnswer) (*dex.StepDecision, error) {
	switch {
	case answer.Stopped:
		comm.MustDo(outcomeAttribute.Set(ctx, Outcome{Status: StatusStopped, Report: "The subagent was stopped before it started, so it changed nothing.", FinishedAt: time.Now().UTC()}))
		comm.MustDo(statusAttribute.Set(ctx, StatusStopped.String()))
		return dex.GoTo(ReportSubagentOutcome{}, nil), nil
	case !answer.Admitted:
		comm.MustDo(statusAttribute.Set(ctx, StatusDenied.String()))
		comm.MustDo(outcomeAttribute.Set(ctx, Outcome{Status: StatusDenied, Failure: "admission_denied", FinishedAt: time.Now().UTC()}))
		return dex.GracefulComplete(receipt{Identity: comm.Must(identityAttribute.Get(ctx)), Status: StatusDenied}), nil
	case dexflow.ValueOrZero(stopRequestAttribute.Get(ctx)):
		comm.MustDo(statusAttribute.Set(ctx, StatusFinishing.String()))
		return dex.GoTo(FinishSubagent{}, subagentFinish{Status: agenttasks.SubagentStopped}, dex.WithStepOptions(finishOptions(ctx))), nil
	}
	comm.MustDo(statusAttribute.Set(ctx, StatusRunning.String()))
	return dex.GoToMany(dex.MovementOf(CallSubagentModel{}, nil, dex.WithStepOptions(step.flow.planModelCall(ctx).load)), dex.MovementOf(AwaitSubagentDeadline{}, nil)), nil
}

// freeze is the configuration a subagent keeps: the Session's model and HEAD at its start,
// its kind's duration and the server's base effort of the kind, clamped to the model.
func (flow *SubagentFlow) freeze(start startRecord, now time.Time) Config {
	kind := start.Call.Kind
	return Config{Model: start.Model, Kind: kind, Effort: modelprovider.ProfileOf(start.Model).BaseEffort(flow.efforts[kind]),
		StartedAt: now, Deadline: now.Add(kind.Duration()), HeadCommit: start.HeadCommit}
}

// dex:group group-id:lifecycle group-label:"Lifecycle"
// dex:explanation text:"Wait for the deadline of the subagent's duration limit, then end its loop as timed out."
type AwaitSubagentDeadline struct {
	dex.StepDefaults
}

func (AwaitSubagentDeadline) GetStepOptions() *dex.StepOptions { return statusStepOptions }

// WaitFor waits only on the business timer of the duration limit.
func (AwaitSubagentDeadline) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	deadline := comm.Must(configAttribute.Get(ctx)).Deadline
	return dex.Until(dex.Timer(max(deadline.Sub(ctx.FirstAttemptAt()), time.Second))), nil
}

// Execute ends a running subagent at its deadline; one that is already ending
// keeps its end.
func (AwaitSubagentDeadline) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if statusOf(comm.Must(statusAttribute.Get(ctx))) != StatusRunning {
		return dex.DeadEnd(), nil
	}
	comm.MustDo(statusAttribute.Set(ctx, StatusFinishing.String()))
	return dex.GoTo(FinishSubagent{}, subagentFinish{Status: agenttasks.SubagentTimedOut}, dex.WithStepOptions(finishOptions(ctx))).CancelSteps(
		CallSubagentModel{}, CompactSubagentContext{}, RecordSubagentFailure{}, RouteSubagentTools{}, ExecuteSubagentTool{}, SettleSubagentCommand{},
		RecordSubagentToolFailure{}, ExecuteSubagentParallelTool{}, RecoverSubagentParallelToolExecution{}, AwaitSubagentParallelToolResults{}, RefreshSubagentWorkspace{},
		BeginSubagentFinish{}), nil
}

// dex:group group-id:model group-label:"Model"
// dex:explanation text:"Make one model call and record its reply; a reply without tools ends the task with a report."
type CallSubagentModel struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SubagentFlow
}

func (CallSubagentModel) GetStepOptions() *dex.StepOptions {
	options := *modelStepOptions
	options.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordSubagentFailure{}, nil)
	return &options
}

// Execute streams and records one reply; one without a tool call is the report.
// A rejection a retry cannot change fails the subagent, any other error retries.
func (step CallSubagentModel) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	segment := comm.Must(contextSegmentAttribute.Get(ctx))
	prompts := comm.Must(contextSegmentPromptsAttribute.Get(ctx, segment.PromptsKey()))
	state := comm.Must(agentStateAttribute.Get(ctx))
	read := func(sequence conversation.Sequence) conversation.Entry {
		if sequence >= state.CurrentFirstSequence {
			return comm.Must(currentMessagesAttribute.Get(ctx, agentloop.SequenceKey(sequence)))
		}
		return archivedEntry(comm.Must(archivedMessagesAttribute.Get(ctx, agentloop.SequenceKey(archiveChunkFirst(sequence)))), sequence)
	}
	request := step.flow.segmentRequest(ctx, agentloop.ModelCallKindResponse, prompts, read)
	assistant, assistantErr := dex.NewBufferedTextStream(ctx, assistantTextStream)
	reasoning, reasoningErr := dex.NewBufferedTextStream(ctx, reasoningSummaryStream)
	if err := errors.Join(assistantErr, reasoningErr); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("open the model text Streams: %w", err))
	}
	progress, attempt := modelProgress{ctx: ctx, messageSequence: state.LastSequence + 1}, ctx.Attempt()
	progress.write(conversation.AgentEvent{Kind: conversation.EventKindModelStarted, Message: "Calling " + segment.Model.ProviderModel() + ".", Attempt: &attempt})
	// These writes are the text Streams' only direct uses; the pinned Dex CLI
	// draws a Stream only from a Write call in the Step method.
	request.WriteAssistant = func(chunk string) error { dexflow.StreamWritten(ctx, assistant.Write(chunk)); return nil }
	request.WriteReasoning = func(chunk string) error { dexflow.StreamWritten(ctx, reasoning.Write(chunk)); return nil }
	request.WriteActivity = progress.writeActivity
	defer dexflow.KeepAlive(ctx, agentloop.ModelHeartbeat{Phase: agentloop.HeartbeatPhaseModelCall})()
	reply, err := step.flow.modelClient.Complete(ctx, request)
	if _, isRejected := errors.AsType[*conversation.ModelRejectedError](err); isRejected {
		return dex.GoTo(BeginSubagentFinish{}, subagentFinish{Status: agenttasks.SubagentFailed, Failure: modelFailure(err)}), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("call model %s: %w", segment.Model, err))
	}
	reply.Metrics.Effort = segment.EffectiveEffort()
	progress.write(conversation.AgentEvent{Kind: conversation.EventKindModelCompleted, Message: agentloop.ModelReplyActivityMessage(reply), Attempt: &attempt})
	next := recordReply(ctx, reply, ctx.FirstAttemptAt().UTC())
	switch {
	case next.finish != nil:
		return dex.GoTo(BeginSubagentFinish{}, *next.finish), nil
	case next.routes:
		return dex.GoTo(RouteSubagentTools{}, nil), nil
	}
	plan := step.flow.planModelCall(ctx)
	if next.compacts || plan.compacts {
		return dex.GoTo(CompactSubagentContext{}, nil, dex.WithStepOptions(plan.load)), nil
	}
	return dex.GoTo(CallSubagentModel{}, nil, dex.WithStepOptions(plan.load)), nil
}

// dex:group group-id:model group-label:"Model"
// dex:explanation text:"Summarize the context segment and open the next one with the summary and the task."
type CompactSubagentContext struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SubagentFlow
}

func (CompactSubagentContext) GetStepOptions() *dex.StepOptions {
	options := *modelStepOptions
	options.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordSubagentFailure{}, nil)
	return &options
}

// Execute replays the segment with an instruction to summarize it for the next segment; a summary the model
// cannot give fails the subagent. Its direct AgentActivity write is the one the pinned Dex CLI requires.
func (step CompactSubagentContext) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	segment := comm.Must(contextSegmentAttribute.Get(ctx))
	prompts := comm.Must(contextSegmentPromptsAttribute.Get(ctx, segment.PromptsKey()))
	state := comm.Must(agentStateAttribute.Get(ctx))
	read := func(sequence conversation.Sequence) conversation.Entry {
		if sequence >= state.CurrentFirstSequence {
			return comm.Must(currentMessagesAttribute.Get(ctx, agentloop.SequenceKey(sequence)))
		}
		return archivedEntry(comm.Must(archivedMessagesAttribute.Get(ctx, agentloop.SequenceKey(archiveChunkFirst(sequence)))), sequence)
	}
	request := step.flow.segmentRequest(ctx, agentloop.ModelCallKindCompaction, prompts, read, conversation.NoticeEntry(conversation.EntryRoleSystem, conversation.CompactionInstruction, ""))
	defer dexflow.KeepAlive(ctx, agentloop.CompactionHeartbeat{Phase: agentloop.HeartbeatPhaseCompacting, Segment: segment.Number})()
	summary, err := step.flow.summarize(ctx, request)
	if _, isRejected := errors.AsType[*conversation.ModelRejectedError](err); isRejected {
		return dex.GoTo(BeginSubagentFinish{}, subagentFinish{Status: agenttasks.SubagentFailed, Failure: "its context could not be compacted: " + modelFailure(err)}), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("summarize context segment %d with %s: %w", segment.Number, segment.Model, err))
	}
	ws, start := comm.Must(workspaceAttribute.Get(ctx)), comm.Must(startAttribute.Get(ctx))
	opened := openNextSegment(ctx, summary, ws.RepositoryContext, start.Call.Prompt)
	dexflow.StreamWritten(ctx, agentActivityStream.Write(ctx, conversation.AgentEvent{Kind: conversation.EventKindCompacted, Message: "Context compacted.", MessageSequence: &opened.FirstSequence}))
	return dex.GoTo(CallSubagentModel{}, nil, dex.WithStepOptions(step.flow.planModelCall(ctx).load)), nil
}

// RecordSubagentFailure receives a model call, a compaction or a lease
// refresh whose retries or wait limit ran out.
//
// dex:group group-id:model group-label:"Model"
// dex:explanation text:"Fail the subagent whose model call, compaction or lease refresh could not finish, with the bounded cause."
type RecordSubagentFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (RecordSubagentFailure) GetStepOptions() *dex.StepOptions { return loopStepOptions }

func (RecordSubagentFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	failure := "its model call did not finish before the retries ran out"
	if dexflow.RecoveryStep(ctx) == dex.GetFinalStepType(RefreshSubagentWorkspace{}) {
		failure = "its Session lent it no reachable workspace within the wait limit"
	}
	if detail := dexflow.RecoveryDetail(ctx); detail != "" {
		failure += ": " + detail
	}
	return dex.GoTo(BeginSubagentFinish{}, subagentFinish{Status: agenttasks.SubagentFailed, Failure: failure}), nil
}

// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Answer a call the subagent may not make or set_effort, or run the call serially or in a read-only wave."
type RouteSubagentTools struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SubagentFlow
}

func (RouteSubagentTools) GetStepOptions() *dex.StepOptions { return loopStepOptions }

func (step RouteSubagentTools) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	route, batch := routeCall(ctx)
	switch route {
	case routeAnswered:
		if advancePastCall(ctx) {
			return dex.GoTo(RouteSubagentTools{}, nil), nil
		}
		return dex.GoTo(CallSubagentModel{}, nil, dex.WithStepOptions(step.flow.planModelCall(ctx).load)), nil
	case routeWave:
		movements := []dex.StepMovement{dex.MovementOf(AwaitSubagentParallelToolResults{}, batch)}
		for _, record := range batch.Records {
			branch := parallelToolExecutionInput{ResultInstance: batch.BatchID, Index: record.Index, Call: record.Call}
			options := parallelToolStepOptions(agentloop.CatalogTool(record.Call.Name), dex.ProceedToOnExecuteFailure(RecoverSubagentParallelToolExecution{}, parallelToolRecoveryStepOptions))
			movements = append(movements, dex.MovementOf(ExecuteSubagentParallelTool{}, branch, dex.WithStepOptions(options)))
		}
		return dex.GoToMany(movements...), nil
	case routeSerial:
	}
	call := agentloop.PendingToolCall(comm.Must(agentStateAttribute.Get(ctx)))
	options := serialToolStepOptions(call, ExecuteSubagentTool{command: true}.GetStepOptions().ExecuteFailure, ExecuteSubagentTool{}.GetStepOptions().ExecuteFailure)
	return dex.GoTo(ExecuteSubagentTool{}, nil, dex.WithStepOptions(options)), nil
}

// ExecuteSubagentTool declares the failure route of a call that is not a
// command; RouteSubagentTools attaches the complete options of each call.
//
// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Run one tool on the lease copy in the worktree, with the call's stable identity and bounded execution."
type ExecuteSubagentTool struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SubagentFlow
	// command selects the failure route of a bash call in GetStepOptions.
	command bool
}

// GetStepOptions declares the failure routes: a bash call goes to SettleSubagentCommand (settling from
// the ledger), every other call to RecordSubagentToolFailure, which is also the registered route.
func (step ExecuteSubagentTool) GetStepOptions() *dex.StepOptions {
	if step.command {
		return &dex.StepOptions{ExecuteFailure: dex.ProceedToOnExecuteFailure(SettleSubagentCommand{}, nil)}
	}
	return &dex.StepOptions{ExecuteFailure: dex.ProceedToOnExecuteFailure(RecordSubagentToolFailure{}, loopStepOptions)}
}

// Execute records the call's result. A transport proving the lease unreachable dispatched nothing, so the call
// reruns on a refreshed lease unless a mutating attempt may have run it or it lost its lease too often.
func (step ExecuteSubagentTool) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	call := agentloop.PendingToolCall(comm.Must(agentStateAttribute.Get(ctx)))
	result, err := step.flow.executeTool(ctx, call)
	timing := agentloop.ToolMessageTiming{StartedAt: ctx.FirstAttemptAt(), CompletedAt: time.Now().UTC()}
	switch {
	case errors.Is(err, sandboxtools.ErrWorkspaceUnavailable) && ctx.Attempt() > 1 && agentloop.CatalogTool(call.Name).WorkspaceAccess == agentloop.WorkspaceAccessMutating:
		result = agentloop.UnfinishedToolResult(call, "The Sandbox workspace was lost after an earlier attempt of the call.", "")
	case errors.Is(err, sandboxtools.ErrWorkspaceUnavailable) && lostWorkspace(ctx, call.ID):
		return dex.GoTo(RefreshSubagentWorkspace{}, nil), nil
	case errors.Is(err, sandboxtools.ErrWorkspaceUnavailable):
		result = agentloop.WorkspaceUnavailableResult()
	case err != nil:
		return nil, dex.ErrorWithStack(fmt.Errorf("execute tool %q call %s: %w", call.Name, call.ID, err))
	}
	if finishToolExecution(ctx, call, result, timing) {
		return dex.GoTo(RouteSubagentTools{}, nil), nil
	}
	plan := step.flow.planModelCall(ctx)
	if plan.compacts {
		return dex.GoTo(CompactSubagentContext{}, nil, dex.WithStepOptions(plan.load)), nil
	}
	return dex.GoTo(CallSubagentModel{}, nil, dex.WithStepOptions(plan.load)), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Settle a command past its timeout or exhausted retries from the ledger: a subagent's command is stopped, never moved to the background."
type SettleSubagentCommand struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SubagentFlow
}

// GetStepOptions: the Step stops or reads one command; once its own retries
// run out the command is recorded as interrupted.
func (SettleSubagentCommand) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: time.Minute, HeartbeatTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync, ExecuteRetry: shortRetry,
		ExecuteLoadAttributeMaps: messageMaps, ExecuteLockAttributes: messageLocks, ExecuteFailure: dex.ProceedToOnExecuteFailure(RecordSubagentToolFailure{}, loopStepOptions)}
}

func (step SettleSubagentCommand) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	call := agentloop.PendingToolCall(comm.Must(agentStateAttribute.Get(ctx)))
	result, timing, err := step.flow.settleCommand(ctx, call)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("settle command call %s: %w", call.ID, err))
	}
	if finishToolExecution(ctx, call, result, timing) {
		return dex.GoTo(RouteSubagentTools{}, nil), nil
	}
	plan := step.flow.planModelCall(ctx)
	if plan.compacts {
		return dex.GoTo(CompactSubagentContext{}, nil, dex.WithStepOptions(plan.load)), nil
	}
	return dex.GoTo(CallSubagentModel{}, nil, dex.WithStepOptions(plan.load)), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Record a serial call whose retries are exhausted as a platform failure the model reads, with an unknown effect for a call that may change the worktree."
type RecordSubagentToolFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SubagentFlow
}

func (RecordSubagentToolFailure) GetStepOptions() *dex.StepOptions { return loopStepOptions }

func (step RecordSubagentToolFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	call := agentloop.PendingToolCall(comm.Must(agentStateAttribute.Get(ctx)))
	if finishToolExecution(ctx, call, agentloop.UnfinishedToolResult(call, exhaustedRetriesMessage, dexflow.RecoveryDetail(ctx)), agentloop.ToolMessageTiming{}) {
		return dex.GoTo(RouteSubagentTools{}, nil), nil
	}
	plan := step.flow.planModelCall(ctx)
	if plan.compacts {
		return dex.GoTo(CompactSubagentContext{}, nil, dex.WithStepOptions(plan.load)), nil
	}
	return dex.GoTo(CallSubagentModel{}, nil, dex.WithStepOptions(plan.load)), nil
}

// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Run one branch of a read-only wave and publish its result."
type ExecuteSubagentParallelTool struct {
	dex.StepDefaultsNoWaitFor[parallelToolExecutionInput]
	flow *SubagentFlow
}

func (ExecuteSubagentParallelTool) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteFailure: dex.ProceedToOnExecuteFailure(RecoverSubagentParallelToolExecution{}, parallelToolRecoveryStepOptions)}
}

// Execute publishes the branch's result in its own commit; a branch that
// found its workspace unreachable dispatched nothing, and the join decides.
func (step ExecuteSubagentParallelTool) Execute(ctx dex.Context, input parallelToolExecutionInput) (*dex.StepDecision, error) {
	result, err := step.flow.executeTool(ctx, input.Call)
	branch := parallelToolResult{Index: input.Index, Call: input.Call, Result: result, Timing: agentloop.ToolMessageTiming{StartedAt: ctx.FirstAttemptAt(), CompletedAt: time.Now().UTC()}}
	switch {
	case errors.Is(err, sandboxtools.ErrWorkspaceUnavailable):
		branch.Result, branch.WorkspaceLost = agentloop.WorkspaceUnavailableResult(), true
	case err != nil:
		return nil, dex.ErrorWithStack(fmt.Errorf("execute tool %q call %s: %w", input.Call.Name, input.Call.ID, err))
	}
	comm.MustDo(parallelToolResultsChannel.Publish(ctx, input.ResultInstance, branch))
	return dex.DeadEnd(), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Publish a classified failure for one branch whose retries are exhausted."
type RecoverSubagentParallelToolExecution struct {
	dex.StepDefaultsNoWaitFor[parallelToolExecutionInput]
}

func (RecoverSubagentParallelToolExecution) GetStepOptions() *dex.StepOptions {
	return parallelToolRecoveryStepOptions
}

func (RecoverSubagentParallelToolExecution) Execute(ctx dex.Context, input parallelToolExecutionInput) (*dex.StepDecision, error) {
	comm.MustDo(parallelToolResultsChannel.Publish(ctx, input.ResultInstance, parallelToolResult{Index: input.Index, Call: input.Call,
		Result: agentloop.UnfinishedToolResult(input.Call, exhaustedRetriesMessage, dexflow.RecoveryDetail(ctx))}))
	return dex.DeadEnd(), nil
}

// dex:group group-id:tools group-label:"Tools"
// dex:explanation text:"Join the results of a read-only wave exactly once; a wave that lost its lease is routed again after a refresh."
type AwaitSubagentParallelToolResults struct {
	dex.StepDefaults
	flow *SubagentFlow
}

func (AwaitSubagentParallelToolResults) GetStepOptions() *dex.StepOptions { return loopStepOptions }

func (AwaitSubagentParallelToolResults) WaitFor(_ dex.Context, batch agentloop.ToolBatchState) (*dex.Wait, error) {
	return dex.Until(parallelToolResultsChannel.ForN(batch.BatchID, len(batch.Records))), nil
}

// Execute records the wave; it is read-only, so a branch that found its lease unreachable
// discards the wave, which is routed again on a refreshed lease.
func (step AwaitSubagentParallelToolResults) Execute(ctx dex.Context, input agentloop.ToolBatchState) (*dex.StepDecision, error) {
	batch, lost := joinParallelToolResults(input, comm.Must(parallelToolResultsChannel.GetConditionResults(ctx, input.BatchID)))
	if lost != nil && lostWorkspace(ctx, lost.Call.ID) {
		return dex.GoTo(RefreshSubagentWorkspace{}, nil), nil
	}
	if finishToolBatch(ctx, batch) {
		return dex.GoTo(RouteSubagentTools{}, nil), nil
	}
	plan := step.flow.planModelCall(ctx)
	if plan.compacts {
		return dex.GoTo(CompactSubagentContext{}, nil, dex.WithStepOptions(plan.load)), nil
	}
	return dex.GoTo(CallSubagentModel{}, nil, dex.WithStepOptions(plan.load)), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"Ask the Session every ten seconds for the lease of its instance after the transport proved the subagent's copy unreachable."
type RefreshSubagentWorkspace struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SubagentFlow
}

// GetStepOptions: a polling Step whose wait limit fails the subagent; it holds
// no lock while it waits.
func (RefreshSubagentWorkspace) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(refreshWaitLimit, dex.ProceedToOnExecuteFailure(RecordSubagentFailure{}, nil))
}

// refreshWaitLimit bounds how long a subagent waits for a reachable lease.
const refreshWaitLimit = 30 * time.Minute

// Execute reports the lost instance and takes the lease of the next one the Session lends, where the call
// at the cursor runs again; a Session that no longer lends the workspace fails the subagent.
func (step RefreshSubagentWorkspace) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	ws := comm.Must(workspaceAttribute.Get(ctx))
	refreshed, err := step.flow.awaitLease(ctx, comm.Must(identityAttribute.Get(ctx)), ws.Lease.RequestID)
	if err != nil {
		return nil, err
	}
	if refreshed.Closed {
		return dex.GoTo(BeginSubagentFinish{}, subagentFinish{Status: agenttasks.SubagentFailed, Failure: "its Session no longer lends it the workspace"}), nil
	}
	ws.Lease = *refreshed.Lease
	comm.MustDo(workspaceAttribute.Set(ctx, ws))
	return dex.GoTo(RouteSubagentTools{}, nil), nil
}

// dex:group group-id:lifecycle group-label:"Lifecycle"
// dex:explanation text:"End the loop of a running subagent once: it completed with its report or failed."
type BeginSubagentFinish struct {
	dex.StepDefaultsNoWaitFor[subagentFinish]
}

func (BeginSubagentFinish) GetStepOptions() *dex.StepOptions { return statusStepOptions }

// Execute ends a running subagent with the loop's outcome; a Stop or the
// deadline that ended it first keeps its end.
func (BeginSubagentFinish) Execute(ctx dex.Context, finish subagentFinish) (*dex.StepDecision, error) {
	if statusOf(comm.Must(statusAttribute.Get(ctx))) != StatusRunning {
		return dex.DeadEnd(), nil
	}
	comm.MustDo(statusAttribute.Set(ctx, StatusFinishing.String()))
	return dex.GoTo(FinishSubagent{}, finish, dex.WithStepOptions(finishOptions(ctx))).CancelSteps(AwaitSubagentDeadline{}), nil
}

// dex:group group-id:lifecycle group-label:"Lifecycle"
// dex:explanation text:"Stop a command the loop left running and save the worktree into its result ref, keeping the changes for the merge."
type FinishSubagent struct {
	dex.StepDefaultsNoWaitFor[subagentFinish]
	flow *SubagentFlow
}

// GetStepOptions: a few Sandbox operations without a lock (a develop subagent's movements carry
// contributionFinishOptions); exhausted retries record the outcome with its changes unsaved.
func (FinishSubagent) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: 2 * time.Minute, HeartbeatTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteRetry: dexflow.StepRetry(10 * time.Minute), ExecuteLoadAttributeMaps: messageMaps,
		ExecuteFailure: dex.ProceedToOnExecuteFailure(RecordSubagentOutcome{}, nil)}
}

// Execute stops an interrupted command and saves the worktree or contributes the branch (saveWork),
// idempotent on the volume; an unreachable lease is refreshed from the Session at most twice.
func (step FinishSubagent) Execute(ctx dex.Context, finish subagentFinish) (*dex.StepDecision, error) {
	if err := step.flow.stopRunningCommand(ctx); err != nil {
		return nil, dex.ErrorWithStack(err)
	}
	err := step.flow.saveWork(ctx, &finish)
	switch {
	case errors.Is(err, sandboxtools.ErrWorkspaceUnavailable) && finish.Refreshes < maximumWorkspaceLosses:
		finish.Refreshes++
		return dex.GoTo(RefreshFinishingWorkspace{}, finish), nil
	case errors.Is(err, sandboxtools.ErrWorkspaceUnavailable):
		finish.Unsaved = "the Sandbox stayed unreachable"
	case err != nil:
		return nil, dex.ErrorWithStack(fmt.Errorf("save the worktree: %w", err))
	}
	if finish.Report == "" {
		finish.Report = comm.TruncateUTF8(lastReplyText(ctx), maximumReportBytes)
	}
	return dex.GoTo(RecordSubagentOutcome{}, finish), nil
}

// dex:group group-id:workspace group-label:"Workspace"
// dex:explanation text:"Ask the Session every ten seconds for the lease of its current instance when the finishing subagent's copy proved unreachable, then save the worktree on it."
type RefreshFinishingWorkspace struct {
	dex.StepDefaultsNoWaitFor[subagentFinish]
	flow *SubagentFlow
}

// GetStepOptions: a polling Step like RefreshSubagentWorkspace; past its wait
// limit RecordSubagentOutcome records the outcome with its changes unsaved.
func (RefreshFinishingWorkspace) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(refreshWaitLimit, dex.ProceedToOnExecuteFailure(RecordSubagentOutcome{}, nil))
}

// Execute reports the unreachable lease and takes the lease of the instance that replaced it on the same
// volume; a Session that no longer lends the workspace leaves the changes unsaved in the worktree.
func (step RefreshFinishingWorkspace) Execute(ctx dex.Context, finish subagentFinish) (*dex.StepDecision, error) {
	ws := comm.Must(workspaceAttribute.Get(ctx))
	refreshed, err := step.flow.awaitLease(ctx, comm.Must(identityAttribute.Get(ctx)), ws.Lease.RequestID)
	if err != nil {
		return nil, err
	}
	if refreshed.Closed {
		finish.Unsaved = "its Session no longer lends it the workspace"
		return dex.GoTo(RecordSubagentOutcome{}, finish), nil
	}
	ws.Lease = *refreshed.Lease
	comm.MustDo(workspaceAttribute.Set(ctx, ws))
	return dex.GoTo(FinishSubagent{}, finish, dex.WithStepOptions(finishOptions(ctx))), nil
}

// dex:group group-id:lifecycle group-label:"Lifecycle"
// dex:explanation text:"Record how the subagent ended, drain the results of a stopped wave and report the end."
type RecordSubagentOutcome struct {
	dex.StepDefaultsNoWaitFor[subagentFinish]
}

func (RecordSubagentOutcome) GetStepOptions() *dex.StepOptions {
	options := *statusStepOptions
	options.ExecuteLoadChannelMaps = []dex.ChannelDef{parallelToolResultsChannel}
	return &options
}

// Execute records the outcome under the status lock, also when exhausted retries left the worktree
// unsaved, and drains the results of a wave a Stop or the deadline cancelled.
func (RecordSubagentOutcome) Execute(ctx dex.Context, finish subagentFinish) (*dex.StepDecision, error) {
	if detail := dexflow.RecoveryDetail(ctx); detail != "" {
		finish.Unsaved = "its worktree could not be saved: " + detail
	}
	for _, instance := range parallelToolResultsChannel.AllInstanceKeys(ctx) {
		for _, message := range comm.Must(parallelToolResultsChannel.PendingMessages(ctx, instance)) {
			comm.MustDo(parallelToolResultsChannel.Delete(ctx, instance, message.MessageID))
		}
	}
	status := endStatus(finish.Status)
	comm.MustDo(outcomeAttribute.Set(ctx, Outcome{Status: status, Report: describeEnd(finish), Changes: finish.Changes, Failure: finish.Failure, Unsaved: finish.Unsaved,
		FinishedAt: time.Now().UTC(), Contribution: finish.Contribution, Refused: finish.Refused}))
	comm.MustDo(statusAttribute.Set(ctx, status.String()))
	return dex.GoTo(ReportSubagentOutcome{}, nil), nil
}

// dex:group group-id:lifecycle group-label:"Lifecycle"
// dex:explanation text:"Deliver the task notification of the end to the Session, waiting while its queue is full, then complete."
type ReportSubagentOutcome struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *SubagentFlow
}

// GetStepOptions: a polling Step of up to two hours, because the Session may
// be busy; past it the report stays undelivered and readable here.
func (ReportSubagentOutcome) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(reportWaitLimit, dex.ProceedToOnExecuteFailure(RecordUndeliveredReport{}, nil))
}

// reportWaitLimit bounds how long a report waits for its Session.
const reportWaitLimit = 2 * time.Hour

// Execute delivers the notification, whose ID makes a repeated delivery
// harmless; a Session that ended never receives it.
func (step ReportSubagentOutcome) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	identity, start, outcome := comm.Must(identityAttribute.Get(ctx)), comm.Must(startAttribute.Get(ctx)), comm.Must(outcomeAttribute.Get(ctx))
	notification := agenttasks.SubagentNotification(identity, outcome.Status.outcome(), start.Call.Description, outcome.Report, outcome.Changes, outcome.Unsaved != "", outcome.FinishedAt,
		outcome.Contribution)
	answer := ReportBusy
	err := dexflow.PollUntil(ctx, 10*time.Second, 10*time.Second, func(round context.Context) (bool, error) {
		reported, err := step.flow.dependency.ReportSubagentOutcome(round, identity, notification)
		if err != nil {
			return false, fmt.Errorf("report the end of subagent %s: %w", identity.AgentID, err)
		}
		answer = reported
		return answer != ReportBusy, nil
	})
	if err != nil {
		return nil, err
	}
	outcome.Delivery = reportUndelivered
	if answer == ReportAdmitted {
		outcome.Delivery = reportDelivered
	}
	comm.MustDo(outcomeAttribute.Set(ctx, outcome))
	return dex.GracefulComplete(receipt{Identity: identity, Status: outcome.Status}), nil
}

// dex:group group-id:lifecycle group-label:"Lifecycle"
// dex:explanation text:"Complete a subagent whose report could not be delivered within its wait limit; its outcome stays readable."
type RecordUndeliveredReport struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (RecordUndeliveredReport) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (RecordUndeliveredReport) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	identity, outcome := comm.Must(identityAttribute.Get(ctx)), comm.Must(outcomeAttribute.Get(ctx))
	outcome.Delivery = reportUndelivered
	comm.MustDo(outcomeAttribute.Set(ctx, outcome))
	return dex.GracefulComplete(receipt{Identity: identity, Status: outcome.Status}), nil
}
