// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

type workspaceRequestBranch uint8

const (
	workspaceRequestEnd workspaceRequestBranch = iota
	workspaceRequestSend
	workspaceRequestApply
	workspaceRequestResume
)

type workspaceRequestChoice struct {
	branch workspaceRequestBranch
	demand workspaceDemand
	next   agentloop.Continuation
}

func (step RequestSessionWorkspace) planRequestWorkspace(ctx dex.Context, demand workspaceDemand) workspaceRequestChoice {
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	if !acceptsUserCommand(ctx) {
		return workspaceRequestChoice{}
	}
	plan := ws.PlanRequest(demand.ResumeAt, demand.Lost, demand.CallID, newWorkspaceRequestID(ctx))
	if plan.ChangesWorkspace() {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
	switch plan {
	case agentloop.WorkspaceOpened:
		// The previous request's Sandbox deadline ends with it; the request is durable before its command.
		demand.RequestID = ws.RequestID
		return workspaceRequestChoice{branch: workspaceRequestSend, demand: demand}
	case agentloop.WorkspaceUsableWithoutContext:
		// Read the repository context now instead of waiting for the next instance.
		demand.RequestID, demand.PromptOnly = ws.RequestID, true
		return workspaceRequestChoice{branch: workspaceRequestApply, demand: demand}
	case agentloop.WorkspaceInFlight, agentloop.WorkspaceJoined:
		return workspaceRequestChoice{}
	case agentloop.WorkspaceUsable, agentloop.WorkspaceLossesExhausted:
		// Nothing to request: resume below.
	}
	if next := step.flow.resumeAfterWorkspace(ctx, demand, plan == agentloop.WorkspaceUsable); next != "" {
		return workspaceRequestChoice{branch: workspaceRequestResume, next: next}
	}
	return workspaceRequestChoice{}
}

// taskCall is the trusted call of a task operation on the instance of lease.
func taskCall(ctx dex.Context, lease workspace.Lease, taskID string) sandboxtools.Call {
	return sandboxtools.Call{Tool: string(conversation.ToolStopTask), Arguments: sandboxtools.TaskArguments(taskID), Lease: &lease, Root: sandboxd.WorkspaceRoot,
		HeadCommit: comm.Must(headCommitAttribute.Get(ctx)), Mutating: true, Identity: sandboxd.InvocationIdentity{FlowID: ctx.FlowID(), CallID: taskID}, Progress: func(string) {}}
}

// workspaceWaitBudget bounds the wait for the outcome of a request: it
// outlasts the SessionWorkspaceFlow's reclamation of an earlier instance (15
// minutes), its wait for the provisioning's report and the retries of its own
// report.
const workspaceWaitBudget = provision.Budget + 30*time.Minute

// workspaceDemandResult answers a WorkspaceDemandRequest: the workspace after
// the demand, whether a READY instance was lent to the Preview, the lease a
// subagent takes, and Closed for a Session that is no longer ACTIVE, which
// lends and requests nothing, or a subagent that no longer runs.
type workspaceDemandResult struct {
	Summary agentloop.WorkspaceSummary `json:"summary"`
	Lent    bool                       `json:"lent,omitempty"`
	Lease   *workspace.Lease           `json:"lease,omitempty"`
	Closed  bool                       `json:"closed,omitempty"`
}

// workspaceDemand is the input of the cold-path Steps.
type workspaceDemand struct {
	ResumeAt agentloop.Continuation `json:"return"`           // "" = RPC-triggered branch: DeadEnd when done
	Lost     string                 `json:"lost,omitempty"`   // RequestID the transport proved unreachable
	CallID   conversation.CallID    `json:"callId,omitempty"` // call that observed the loss
	// RequestID is the request the branch owns once RequestSessionWorkspace
	// opened it. PromptOnly marks a branch that owns no request and only puts
	// the repository context of a READY lease into the prompt. Reclaims marks
	// the branch that sends the reclaim command of the revoked request at its
	// Sandbox deadline, which owns no request either.
	RequestID  string `json:"requestId,omitempty"`
	PromptOnly bool   `json:"promptOnly,omitempty"`
	Reclaims   bool   `json:"reclaims,omitempty"`
	// Observed is the outcome AwaitSessionWorkspace took from the report or
	// read from the SessionWorkspaceFlow. Failure is the rejection or timeout
	// a Step decided, which RecordWorkspaceFailure records; TimedOut marks a
	// request whose outcome did not arrive within the wait.
	Observed *agentloop.WorkspaceGrant `json:"observed,omitempty"`
	Failure  string                    `json:"failure,omitempty"`
	TimedOut bool                      `json:"timedOut,omitempty"`
	// DeploysCommit is the setup commit whose Preview a Session created from
	// an example deploys once its request is READY, as the deployment of
	// DeploySequence, the greeting notice's sequence.
	DeploysCommit  string                `json:"deploysCommit,omitempty"`
	DeploySequence conversation.Sequence `json:"deploySequence,omitempty"`
}

// exampleHandoff is the Preview deployment of the setup commit that a
// Session created from an example requests on its READY workspace.
func exampleHandoff(ctx dex.Context, demand workspaceDemand) agentloop.PreviewHandoff {
	return capturedPreviewHandoff(ctx, agentloop.PreviewHandoff{Commit: demand.DeploysCommit, Sequence: demand.DeploySequence})
}

// readyPreviewHold is how long a READY Preview holds its Sandbox: the Preview
// hold, or only the idle Sandbox hold while a Session created from an example
// waits for the person's first message.
func (flow *SessionAgentFlow) readyPreviewHold(ctx dex.Context) time.Duration {
	if comm.Must(agentStateAttribute.Get(ctx)).AwaitsFirstMessage {
		return flow.sandboxHold
	}
	return flow.previewHold
}

// newWorkspaceRequestID derives the instance identity from the Step execution
// that records it, "<Step>-<n>". The execution counter never repeats within
// the Flow.
func newWorkspaceRequestID(ctx dex.Context) string {
	_, number, ok := agentloop.StepExecutionParts(ctx.StepExecutionID())
	if !ok {
		panic("a Dex Step execution ID is not <Step>-<n>: " + ctx.StepExecutionID())
	}
	return "ws-" + number
}

// admitWorkspaceUse reports whether the tools about to run may use the accepted
// lease, marks its instance in use and extends its Sandbox deadline: the main
// agent's workspace use holds the Sandbox for the Sandbox hold. Callers hold
// the Workspace lock, which makes the extension atomic with the deadline's
// reclamation decision.
func (flow *SessionAgentFlow) admitWorkspaceUse(ctx dex.Context) bool {
	ws, now := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)), time.Now()
	if !ws.IsUsable() {
		return false
	}
	comm.MustDo(workspaceUseAttribute.Set(ctx, agentloop.WorkspaceUse{Active: true, RequestID: ws.Lease.RequestID}))
	if ws.ExtendDeadline(now, flow.sandboxHold) {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
	return true
}

// reclaimAtDeadline decides, under the Workspace lock, when the Sandbox
// deadline timer of requestID fired. It reports waitsAgain when the deadline
// moved later, or a tool, a background command or an unfinished subagent
// still uses the instance, which holds it for another Sandbox hold; every
// task has a hard limit.
// Otherwise it revokes the lease in the same commit and reports reclaims, so
// a following Step sends the reclaim command only once no tool can be
// dispatched on the instance; neither is reported when the request settled
// otherwise. The Session knows by itself whether its tools stopped, so no
// other Flow is asked.
func (flow *SessionAgentFlow) reclaimAtDeadline(ctx dex.Context, requestID string) (reclaims, waitsAgain bool) {
	ws, now := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)), time.Now()
	if ws.RequestID != requestID || ws.State != agentloop.WorkspaceReady {
		return false, false
	}
	if ws.SandboxDeadline.After(now) {
		return false, true
	}
	use := dexflow.ValueOrZero(workspaceUseAttribute.Get(ctx))
	if use.Active && use.RequestID == ws.Lease.RequestID || slices.ContainsFunc(agenttasks.RunningBackgroundCommands(loadedBackgroundCommands(ctx)), func(command agenttasks.BackgroundCommand) bool {
		return command.WorkspaceRequestID == ws.Lease.RequestID
	}) || len(agenttasks.UnfinishedSubagents(loadedSubagents(ctx), now)) > 0 {
		ws.ExtendDeadline(now, flow.sandboxHold)
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
		return false, true
	}
	ws.Revoke()
	comm.MustDo(workspaceAttribute.Set(ctx, ws))
	return true, false
}

// admitWorkspaceOutcome admits a report of the SessionWorkspaceFlow under the
// receipts and Workspace locks and reports whether it is admitted; a repeated
// report is admitted again without another effect. A reclamation clears the
// Preview's hold of the instance. A ready or failed outcome is admitted only
// while the Session waits for exactly that request, and it reaches only
// AwaitSessionWorkspace through its WorkspaceOutcomes instance. Anything
// else, such as a late outcome after a replacement, is refused: the workspace
// Flow reclaims the instance it created.
func admitWorkspaceOutcome(ctx dex.Context, report agentloop.WorkspaceReport) bool {
	id := report.WorkspaceRequestID
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	if report.Reclaimed {
		if ws.PreviewHolds == id {
			ws.Reclaimed(id)
			comm.MustDo(workspaceAttribute.Set(ctx, ws))
		}
		return true
	}
	isNew, _, next := agentloop.AdmitNotification(dexflow.ValueOrZero(notificationReceiptsAttribute.Get(ctx)), 0, "workspace/"+id)
	if !isNew {
		return true
	}
	if report.Grant == nil || ws.State != agentloop.WorkspaceRequested || ws.RequestID != id {
		return false
	}
	comm.MustDo(workspaceOutcomesChannel.Publish(ctx, id, report))
	comm.MustDo(notificationReceiptsAttribute.Set(ctx, next))
	return true
}

// endToolWork records that no dispatched tool remains on the instance and no
// tool work is in flight. Finishing paths call it without the Workspace lock;
// only the reclamation decision reads WorkspaceUse.
func endToolWork(ctx dex.Context) {
	setRunningWork(ctx, nil)
	if dexflow.ValueOrZero(workspaceUseAttribute.Get(ctx)) != (agentloop.WorkspaceUse{}) {
		comm.MustDo(workspaceUseAttribute.Set(ctx, agentloop.WorkspaceUse{}))
	}
}

// lostWorkspaceDemand ends the tool work of a call or a wave that found its
// workspace unreachable before it dispatched anything, and is the cold-path
// demand that routes the calls again once a new instance is ready.
func lostWorkspaceDemand(ctx dex.Context, callID conversation.CallID) workspaceDemand {
	lost := dexflow.ValueOrZero(workspaceUseAttribute.Get(ctx)).RequestID
	endToolWork(ctx)
	return workspaceDemand{ResumeAt: agentloop.ContinueRouteTool, Lost: lost, CallID: callID}
}

// holdSandbox moves the Sandbox deadline of the READY instance to at least
// now + hold: after a task's end any current instance, for a ready Preview
// only instanceID, the instance it serves on. The caller holds the Workspace
// lock.
func holdSandbox(ctx dex.Context, instanceID string, hold time.Duration) {
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	if (instanceID == "" || ws.Lease.RequestID == instanceID) && ws.ExtendDeadline(time.Now(), hold) {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
}

// resumeAfterWorkspace prepares leaving the cold path in the commit of the
// Step that settled it and returns the continuation the waiting branch resumes
// through CheckSteered, or "" to end an RPC-triggered branch or one beside the
// conversation. A model continuation, which only a branch an earlier binary
// started carries, opens the Session's first segment if it has none, with the
// repository context if the lease provided it. A failed tool demand answers
// the pending call with a failure the model reads, and a start_agent call
// answered so started no subagent, whose record is forgotten. Nothing here
// fails the Session.
func (flow *SessionAgentFlow) resumeAfterWorkspace(ctx dex.Context, demand workspaceDemand, isReady bool) agentloop.Continuation {
	switch demand.ResumeAt {
	case "":
		return ""
	case agentloop.ContinueAwaitRemote, agentloop.ContinuePrepareContext:
		// The conversation goes on beside this branch; the next model-call
		// boundary appends a repository context the segment awaits.
		return ""
	case agentloop.ContinueStartTurn, agentloop.ContinueCompactContext, agentloop.ContinueCallModel:
		if dexflow.OptionalValue(contextSegmentAttribute.Get(ctx)) == nil {
			flow.openSessionStartSegment(ctx)
		}
		return demand.ResumeAt
	case agentloop.ContinueRouteTool:
		if isReady {
			return agentloop.ContinueRouteTool
		}
		call := agentloop.PendingToolCall(comm.Must(agentStateAttribute.Get(ctx)))
		abandonSubagentStart(ctx, call)
		return finishToolExecution(ctx, call, agentloop.WorkspaceUnavailableResult(), agentloop.ToolMessageTiming{})
	default:
		panic(fmt.Sprintf("unsupported workspace continuation %q", demand.ResumeAt))
	}
}

// adoptWorkspaceHead makes the HEAD a newly granted instance observed the
// Session HEAD. Only the Step that owns the request applies it, before any tool
// is dispatched on that instance.
func adoptWorkspaceHead(ctx dex.Context, lease workspace.Lease) {
	confirmSessionHead(ctx, lease.Repository.ExpectedHeadSHA, ctx.FirstAttemptAt().UTC())
}

// confirmSessionHead records a verified HEAD in the commit of the calling Step.
func confirmSessionHead(ctx dex.Context, commit string, at time.Time) {
	if flowid.IsCommitSHA(commit) && comm.Must(headCommitAttribute.Get(ctx)) != commit {
		comm.MustDo(headCommitAttribute.Set(ctx, commit))
		comm.MustDo(sessionUpdatedAtAttribute.Set(ctx, at))
	}
}

// appendLateRepositoryContext appends the repository context that segment
// awaits as one user notice, once the workspace read it, and clears the mark
// in the same commit, so it is never appended twice. It returns the segment
// as recorded. A failed workspace appends nothing.
func appendLateRepositoryContext(ctx dex.Context, segment agentloop.ContextSegment) *agentloop.ContextSegment {
	repositoryContext := dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).RepositoryContext
	if !segment.AwaitsRepositoryContext || repositoryContext == "" {
		return &segment
	}
	appendEntry(ctx, agentloop.RepositoryContextNotice(repositoryContext))
	segment = comm.Must(contextSegmentAttribute.Get(ctx))
	segment.AwaitsRepositoryContext = false
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	return &segment
}

// requestCatalogMatches are the released connectors that the user's own words
// in request name, as the warm per-replica catalog copy finds them without a
// network call; "" when it finds none or is cold.
func (flow *SessionAgentFlow) requestCatalogMatches(request []conversation.Entry) string {
	var words []string
	for _, entry := range request {
		if entry.Kind != conversation.EntryKindUserMessage {
			continue
		}
		if message, isPublic := entry.PublicMessage(); isPublic {
			words = append(words, message.Content)
		}
	}
	return flow.catalog.RequestMatches(strings.Join(words, "\n\n"))
}

// startingExampleSection is the starting_example section of a segment, or
// "" once it is gone: a picked example's while describesPicked, which its
// callers set for the Session's first segment and, for the remote agent,
// while the Session awaits the person's first message; a matched one's until
// the Session's first build, the first pushed commit_and_push or the build of
// a Flow design. It describes what the start Step and the agent recorded:
// whether version 1 exists.
func startingExampleSection(ctx dex.Context, describesPicked bool) string {
	started := comm.Must(agentStateAttribute.Get(ctx)).StartingExample
	switch {
	case started == nil:
		return ""
	case started.Choice == agentloop.ExampleChoiceMatched &&
		(dexflow.ValueOrZero(previewHandoffAttribute.Get(ctx)).Commit != "" || dexflow.OptionalValue(builtFlowDesignAttribute.Get(ctx)) != nil):
		return ""
	case started.Choice == agentloop.ExampleChoicePicked && !describesPicked:
		return ""
	}
	recorded := dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx)) != nil
	return comm.TruncateUTF8(startingExampleText(*started, recorded), agentloop.MaximumStartingExampleBytes)
}

// recordsNoCardDesign reports whether the cards the agent records carry no
// Flow design, which every segment then says: the Project started from an
// example, or a pushed, unblocked commit_and_push of the agent's handed a
// commit to the Preview. Only a default-template Project before its first
// build gets a design.
func recordsNoCardDesign(ctx dex.Context) bool {
	return comm.Must(agentStateAttribute.Get(ctx)).StartingExample != nil || dexflow.ValueOrZero(previewHandoffAttribute.Get(ctx)).Commit != ""
}

// Opening a segment writes its segment_start entry from the sections above:
// the Session's first segment, a compaction's, a rollover's, an unsummarized
// one and the executor handoff's. planModelCall decides when one opens.

// openSessionStartSegment opens the first segment of the Session on the
// selected model.
func (flow *SessionAgentFlow) openSessionStartSegment(ctx dex.Context) agentloop.ContextSegment {
	return flow.openSegment(ctx, agentloop.SegmentReasonSessionStart, comm.Must(sessionModelAttribute.Get(ctx)).Model, "", currentEntry)
}

// openSegment opens the next segment for reason on model with summary: one
// segment_start entry with the summary, the repository context, the confirmed
// requirements, the card design rule once the agent's cards carry no Flow
// design (recordsNoCardDesign), the todo list, the starting example while it
// is described,
// for the Session's first segment the catalog matches of its request, the
// attachments of earlier messages, the workflow import of the request's
// exports, and the turn's pending request as read finds it, then the notice
// of a reporting turn; the references of the request's attachments
// (agentloop.SegmentAttachments) follow the text. A segment without the
// repository context awaits it. The segment and its prompts replace the
// previous ones, and archive chunks before it become eligible for retention.
func (flow *SessionAgentFlow) openSegment(ctx dex.Context, reason agentloop.SegmentReason, model conversation.Model, summary string, read entryReader) agentloop.ContextSegment {
	previous, request := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx)), pendingRequestEntries(ctx, read)
	start := agentloop.SegmentStart{Summary: summary, RepositoryContext: dexflow.ValueOrZero(workspaceAttribute.Get(ctx)).RepositoryContext,
		ConfirmedRequirements: dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx)),
		TodoList:              dexflow.OptionalValue(todoListAttribute.Get(ctx)), PendingRequest: conversation.PendingRequestText(request),
		StartingExample: startingExampleSection(ctx, reason == agentloop.SegmentReasonSessionStart), NoCardDesign: recordsNoCardDesign(ctx)}
	if reason == agentloop.SegmentReasonSessionStart {
		start.CatalogMatches = flow.requestCatalogMatches(request)
	}
	pending, earlier := requestAttachments(request), []agentloop.RememberedAttachment(nil)
	if previous != nil {
		earlier = previous.Attachments
	}
	shown, listed := agentloop.SegmentAttachments(pending, earlier, flow.settings.Attachments)
	start.Attachments = listed
	for _, attachment := range pending {
		if attachment.IsWorkflowExport() {
			start.WorkflowExports = append(start.WorkflowExports, attachment)
		}
	}
	entry := conversation.SegmentStartEntry(start.Text(), agentloop.SegmentMarker(reason), shown...)
	first := appendEntry(ctx, entry)
	segment := agentloop.NewSegment(previous, first, reason, model, flow.settings, entry.Bytes()).OpenedWithAttachments(shown, pending)
	segment.Summary = comm.TruncateUTF8(summary, 8<<10)
	segment.AwaitsRepositoryContext = start.RepositoryContext == ""
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	comm.MustDo(contextSegmentPromptsAttribute.Set(ctx, segment.PromptsKey(), agentloop.SessionPromptFor(model).Prompts))
	if previous != nil {
		comm.MustDo(contextSegmentPromptsAttribute.Delete(ctx, previous.PromptsKey()))
	}
	trimRetainedEntries(ctx)
	return comm.Must(contextSegmentAttribute.Get(ctx))
}

// pendingRequestEntries are the user messages the current turn works on that
// read finds. A compaction and its failure route load the chunks that hold
// them (segmentLoad) and read them with loadedEntry. The first segment opens in
// Steps that load no archive chunk and reads only CurrentMessages
// (currentEntry), so a message archived before it is left out.
func pendingRequestEntries(ctx dex.Context, read entryReader) []conversation.Entry {
	state := comm.Must(agentStateAttribute.Get(ctx))
	var entries []conversation.Entry
	for _, sequence := range state.TurnRequestSequences {
		if entry, found := read(ctx, state, sequence); found {
			entries = append(entries, entry)
		}
	}
	return entries
}

// requestAttachments are the attachments the entries of a pending request
// hold, each once, in request order.
func requestAttachments(request []conversation.Entry) []conversation.AttachmentRef {
	var attachments []conversation.AttachmentRef
	seen := map[string]bool{}
	for _, entry := range request {
		for _, reference := range entry.AttachmentReferences() {
			if !seen[reference.Attachment.ID] {
				seen[reference.Attachment.ID] = true
				attachments = append(attachments, reference.Attachment)
			}
		}
	}
	return attachments
}

// executorContextLoads selects exact retained chunks; at most the newest 1000
// entries contribute to an executor handoff. Missing loads retry the same
// existing boundary Step, never the model or a new context lifecycle.
func executorContextLoads(ctx dex.Context) []dex.AttributeMapLoad {
	state := comm.Must(agentStateAttribute.Get(ctx))
	first := max(state.FirstRetainedSequence, state.LastSequence-999)
	first = (first-1)/agentloop.ArchiveMessageChunkSize*agentloop.ArchiveMessageChunkSize + 1
	var loads []dex.AttributeMapLoad
	for sequence := first; sequence < state.CurrentFirstSequence; sequence += agentloop.ArchiveMessageChunkSize {
		key := agentloop.SequenceKey(sequence)
		_, err := archivedMessagesAttribute.Get(ctx, key)
		if dexflow.IsAttributeMapNotLoaded(err) {
			loads = append(loads, archivedMessagesAttribute.Load(key))
		} else {
			comm.MustDo(err)
		}
	}
	return loads
}

func (flow *SessionAgentFlow) openExecutorSegment(ctx dex.Context, executor remoteexec.Executor) agentloop.ContextSegment {
	state := comm.Must(agentStateAttribute.Get(ctx))
	read := retainedEntries(ctx, state)
	entries := make([]conversation.SequencedEntry, 0, 1000)
	for sequence := max(state.FirstRetainedSequence, state.LastSequence-999); sequence <= state.LastSequence; sequence++ {
		entries = append(entries, conversation.SequencedEntry{Sequence: sequence, Entry: read(sequence)})
	}
	first := agentloop.ExecutorContextStart(entries, flow.settings.CompactionTokens, flow.settings.Attachments)
	previous := dexflow.OptionalValue(contextSegmentAttribute.Get(ctx))
	summary := ""
	if previous != nil {
		summary = previous.Summary
	}
	handoff := executor.Handoff
	text := handoff.Notice(comm.Must(headCommitAttribute.Get(ctx)))
	section := func(label, value string, budget int) {
		if value != "" {
			text += "\n\n" + label + ":\n" + boundedHandoffText(value, budget)
		}
	}
	section("Older platform summary", summary, 8<<10)
	if requirements := dexflow.OptionalValue(confirmedRequirementsAttribute.Get(ctx)); requirements != nil {
		section("Current requirements", (agentloop.SegmentStart{ConfirmedRequirements: requirements}).Text(), 2<<10)
	}
	if recordsNoCardDesign(ctx) {
		section("Card design", (agentloop.SegmentStart{NoCardDesign: true}).Text(), 1<<10)
	}
	if todos := dexflow.OptionalValue(todoListAttribute.Get(ctx)); todos != nil {
		section("Todos", (agentloop.SegmentStart{TodoList: todos}).Text(), 2<<10)
	}
	var commands []string
	for _, command := range loadedBackgroundCommands(ctx) {
		if command.IsRunning() {
			commands = append(commands, command.TaskID)
		}
	}
	section("Running background commands", strings.Join(commands, ", "), 1<<10)
	// The starting example takes what the notice leaves, so it never pushes out the sections before it.
	if room := remoteexec.MaximumHandoffBytes - len(text) - len("\n\nStarting example:\n"); room >= minimumHandoffExampleBytes {
		section("Starting example", startingExampleSection(ctx, false), room)
	}
	entry := conversation.NoticeEntry(conversation.EntryRoleUser, boundedHandoffText(text, remoteexec.MaximumHandoffBytes), remoteexec.HandoffNoticeLabel)
	last := appendEntry(ctx, entry)
	if first == 0 {
		first = last
	}
	bytes, shown := entry.Bytes(), []conversation.AttachmentReference(nil)
	for _, item := range entries {
		if item.Sequence >= first {
			bytes += item.Entry.Bytes()
			shown = append(shown, item.Entry.AttachmentReferences()...)
		}
	}
	model := comm.Must(sessionModelAttribute.Get(ctx)).Model
	segment := agentloop.NewSegment(previous, first, agentloop.SegmentReasonExecutor, model, flow.settings, bytes).OpenedWithAttachments(shown, nil)
	segment.ProtocolStartSequence = last
	segment.Summary = comm.TruncateUTF8(summary, 8<<10)
	comm.MustDo(contextSegmentAttribute.Set(ctx, segment))
	comm.MustDo(contextSegmentPromptsAttribute.Set(ctx, segment.PromptsKey(), agentloop.SessionPromptFor(model).Prompts))
	if previous != nil {
		comm.MustDo(contextSegmentPromptsAttribute.Delete(ctx, previous.PromptsKey()))
	}
	executor.Handoff = nil
	comm.MustDo(executorAttribute.Set(ctx, executor))
	trimRetainedEntries(ctx)
	return comm.Must(contextSegmentAttribute.Get(ctx))
}

// minimumHandoffExampleBytes is the least room for the starting example in
// the executor handoff notice; with less, the notice leaves it out.
const minimumHandoffExampleBytes = 512

func boundedHandoffText(text string, budget int) string {
	if len(text) <= budget {
		return text
	}
	const marker = "\n[earlier details omitted; open Studio for the full state]"
	return comm.TruncateUTF8(text, budget-len(marker)) + marker
}

// recordStartingExampleBuild completes a matched Session's StartingExample,
// in the commit of the tool result that is the Session's first build, the
// first pushed, unblocked commit_and_push, before which neither the Preview
// handoff nor a built design exists (the moment the starting_example section
// ends), with what that build kept of the example: the Steps of the example's
// Flow definitions as this server build embeds them at the recorded tree,
// those of the Flow definitions the commit gate rendered of the pushed tree,
// which the result carries, the example's Steps among them by Flow and Step
// type, and the commit. It is written once; a later build, and a Session whose
// first build came before this record existed, write nothing.
func recordStartingExampleBuild(ctx dex.Context, call conversation.ToolCall, result agentloop.ToolExecutionResult) {
	state := comm.Must(agentStateAttribute.Get(ctx))
	started := state.StartingExample
	if started == nil || started.Choice != agentloop.ExampleChoiceMatched || started.FirstBuild != nil || !agentloop.BuildsDesign(call, result) ||
		dexflow.ValueOrZero(previewHandoffAttribute.Get(ctx)).Commit != "" || dexflow.OptionalValue(builtFlowDesignAttribute.Get(ctx)) != nil {
		return
	}
	build := agentloop.StartingExampleBuild{BuiltSteps: len(result.DefinedSteps), Commit: result.HeadCommit}
	if example, described := describedExample(*started); described {
		pushed := map[apptemplate.FlowStep]bool{}
		for _, step := range result.DefinedSteps {
			pushed[apptemplate.FlowStep{FlowType: step.FlowType, StepType: step.StepType}] = true
		}
		build.ExampleSteps = len(example.Steps)
		for _, step := range example.Steps {
			if pushed[step] {
				build.KeptSteps++
			}
		}
	}
	started.FirstBuild = &build
	comm.MustDo(agentStateAttribute.Set(ctx, state))
}

// describedExample is the example a Project started from as this server
// build embeds it, and whether the build holds it at the recorded tree; a
// build that holds it at another tree, as during a rollout, does not
// describe it.
func describedExample(started agentloop.StartingExample) (apptemplate.Example, bool) {
	example, found := apptemplate.FindExample(started.ExampleID)
	template, err := apptemplate.ExampleTemplate(started.ExampleID)
	return example, found && err == nil && template.TreeID == started.Tree
}

// startingExampleText describes the example a Project started from from what
// this server build embeds of it: how it was chosen and what to do with it,
// given whether version 1 of the requirements is recorded, its card with each
// feature's evidence and tests, and its files against the template's shared
// base. A build that holds the example at another tree describes only its
// identity.
func startingExampleText(started agentloop.StartingExample, recorded bool) string {
	var text strings.Builder
	fmt.Fprintf(&text, "This Project's first commit is the finished source of the example %s (%s, Git tree %s).\n", started.Title, started.ExampleID, started.Tree)
	example, described := describedExample(started)
	const adapt = "keep what the request needs, reusing the example's type names, feature wording, evidence and test names; change what differs; remove the rest with its connection, endpoints, page and tests.\n"
	switch {
	case started.Choice == agentloop.ExampleChoiceMatched:
		fmt.Fprintf(&text, "The platform chose it as the closest example to the person's first message, which mentions %s. The person did not choose it and is not told: do not mention the example. ",
			strings.Join(apptemplate.FacetLabels(started.MatchedOn), ", "))
		if recorded {
			text.WriteString("Version 1 of the requirements is recorded: build it from this source; " + adapt)
		} else {
			text.WriteString("Write version 1 from the person's request, without a Flow design, as every card you record for this Project: this source already holds the example's Flows, which Studio draws from it; " + adapt)
		}
	case recorded:
		text.WriteString("The person opened it in Studio: its card is recorded as version 1 and built by this source, whose Flows Studio draws. Treat the person's next message as a change to version 1; the card you record for that change carries no Flow design.\n")
	default:
		reason := "its card did not pass the requirements check"
		if !described {
			reason = "this server build does not hold the example at that tree"
		}
		fmt.Fprintf(&text, "The person opened it in Studio, but its card could not be recorded as version 1 (%s), so no requirements are recorded. "+
			"Confirm requirements before a large change; that card is the person's change and, as every card you record for this Project, carries no Flow design: Studio draws this source's real Flows.\n", reason)
	}
	if !described {
		text.WriteString("This server build does not describe that tree; read the source.\n")
		return text.String()
	}
	writeExampleCard(&text, example)
	if parent, isExtended := apptemplate.FindExample(example.Extends); isExtended {
		fmt.Fprintf(&text, "\nIt extends the example %s (%s), whose features it keeps:\n", parent.Title, parent.ID)
		writeTracedFeatures(&text, parent.Trace)
	}
	text.WriteString("\nIts files against the template's shared base:\n")
	for _, list := range []struct {
		label string
		paths []string
	}{{"adds", example.Files.Adds}, {"replaces", example.Files.Replaces}, {"removes", example.Files.Removes}} {
		paths := "none"
		if len(list.paths) > 0 {
			paths = strings.Join(list.paths, ", ")
		}
		fmt.Fprintf(&text, "- %s: %s\n", list.label, paths)
	}
	return text.String()
}

// writeExampleCard writes an example's card: its title and summary, each
// feature with its trace and its connections.
func writeExampleCard(text *strings.Builder, example apptemplate.Example) {
	var card struct {
		Title       string `json:"title"`
		Summary     string `json:"summary"`
		Connections []struct {
			Need       string   `json:"need"`
			Connector  string   `json:"connector"`
			Operations []string `json:"operations"`
		} `json:"connections"`
	}
	comm.MustDo(json.Unmarshal(example.Card, &card))
	fmt.Fprintf(text, "\nIts requirements card, %s: %s\n", card.Title, card.Summary)
	writeTracedFeatures(text, example.Trace)
	for _, connection := range card.Connections {
		fmt.Fprintf(text, "- connection %s (%s): %s\n", connection.Connector, strings.Join(connection.Operations, ", "), connection.Need)
	}
}

// writeTracedFeatures writes each feature with its evidence and its tests or
// why none proves it.
func writeTracedFeatures(text *strings.Builder, features []apptemplate.TracedFeature) {
	for _, feature := range features {
		fmt.Fprintf(text, "- %s %s\n  evidence: %s\n", feature.ID, feature.Text, strings.Join(feature.Evidence, ", "))
		if len(feature.Tests) > 0 {
			fmt.Fprintf(text, "  tests: %s\n", strings.Join(feature.Tests, ", "))
		} else {
			fmt.Fprintf(text, "  untested: %s\n", feature.UntestedReason)
		}
	}
}

// readRepositoryContext reads the trusted context of the leased checkout,
// which the next segment_start carries, or a notice to a segment that awaits
// it. The caller records it in Workspace in the same commit.
func (flow *SessionAgentFlow) readRepositoryContext(ctx dex.Context, lease workspace.Lease) (string, error) {
	repositoryContext, err := flow.repository.RepositoryContext(ctx, lease)
	return comm.TruncateUTF8(repositoryContext, agentloop.MaximumRepositoryContextBytes), err
}

// applyWorkspace settles the request of demand with the outcome
// AwaitSessionWorkspace took, then applies an accepted lease, and records
// Workspace when it changed. Each effect is idempotent and commits with the
// calling Step; only the owner of the request adopts its HEAD, announces the
// settlement to a waiting Preview and takes the joined continuation. It
// returns the demand with the continuation to resume and whether the request
// is READY.
func (flow *SessionAgentFlow) applyWorkspace(ctx dex.Context, ws agentloop.SessionWorkspace, demand workspaceDemand) (workspaceDemand, bool, error) {
	isOwner, isChanged, identity := demand.ownsRequest(), false, readSessionIdentity(ctx)
	if ws.State == agentloop.WorkspaceRequested {
		// Only AwaitSessionWorkspace reaches a request still in flight, with
		// the outcome it took.
		if err := ws.Accept(identity, *demand.Observed); err != nil {
			return demand, false, dex.ErrorWithStack(fmt.Errorf("accept Sandbox instance %s: %w", ws.RequestID, err))
		}
		ws.ExtendDeadline(time.Now(), flow.sandboxHold)
		isChanged = true
	}
	if isOwner && (demand.ResumeAt == "" || demand.ResumeAt.IsBesideConversation()) && ws.JoinedResumeAt != "" {
		demand.ResumeAt, isChanged = ws.TakeJoined(), true
	}
	if ws.State == agentloop.WorkspaceReady {
		if isOwner {
			adoptWorkspaceHead(ctx, ws.Lease)
		}
		if ws.RepositoryContext == "" && (isOwner || demand.ResumeAt != agentloop.ContinueRouteTool) {
			repositoryContext, err := flow.readRepositoryContext(ctx, ws.Lease)
			if err != nil {
				return demand, false, dex.ErrorWithStack(fmt.Errorf("read the repository context of Sandbox instance %s: %w", ws.RequestID, err))
			}
			ws.RepositoryContext, isChanged = repositoryContext, true
		}
	}
	if isOwner && ws.PreviewWaits != "" {
		if err := flow.announceSettlement(ctx, &ws); err != nil {
			return demand, false, err
		}
		isChanged = true
	}
	if isChanged {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
	return demand, ws.State == agentloop.WorkspaceReady, nil
}

// settleWorkspaceFailure records code, the failure of the Step that exhausted
// its retries, or the business rejection or timeout a Step decided, with the
// request of demand, which the caller found current. Only the owner settles
// the request, announces the settlement to a waiting Preview, takes a joined
// continuation and adopts the HEAD. It returns the demand with the
// continuation to resume and whether the owner's request is READY without a
// Sandbox deadline wait, which the caller then arms.
func (flow *SessionAgentFlow) settleWorkspaceFailure(ctx dex.Context, ws *agentloop.SessionWorkspace, demand workspaceDemand, code string) (workspaceDemand, bool, error) {
	detail := cmp.Or(dexflow.RecoveryDetail(ctx), dexflow.TruncateCause(demand.Failure))
	isOwner := demand.ownsRequest()
	switch {
	case isOwner && ws.State == agentloop.WorkspaceRequested:
		// The SessionWorkspaceFlow keeps the instance: a provisioning whose
		// outcome is unknown is reclaimed before the next one.
		ws.Fail(code, detail)
	default:
		// The owner's lease effects failed, or a branch that owns no request
		// (a prompt-only one, or the reclaim command's) failed: only the
		// detail is recorded.
		ws.FailureDetail = detail
	}
	if isOwner && ws.PreviewWaits != "" {
		if err := flow.announceSettlement(ctx, ws); err != nil {
			return demand, false, err
		}
	}
	if isOwner && (demand.ResumeAt == "" || demand.ResumeAt.IsBesideConversation()) {
		// A tool that joined the failed request reads that the workspace is unavailable.
		demand.ResumeAt = ws.TakeJoined()
	}
	comm.MustDo(workspaceAttribute.Set(ctx, *ws))
	if isOwner && ws.State == agentloop.WorkspaceReady {
		adoptWorkspaceHead(ctx, ws.Lease)
	}
	return demand, isOwner && ws.State == agentloop.WorkspaceReady, nil
}

// announceSettlement tells the Preview command that waits for the request of
// ws that it settled, READY or with its failure code, and records that no
// Preview waits any more; the caller commits ws. A failed request refuses the
// command, and a READY one is lent when the Preview asks again. A retried Step
// announces again; the Preview admits the announcement only while that
// command is active.
func (flow *SessionAgentFlow) announceSettlement(ctx dex.Context, ws *agentloop.SessionWorkspace) error {
	failure := ""
	if ws.State == agentloop.WorkspaceFailed {
		failure = ws.Failure
	}
	if err := flow.dependency.NotifyPreviewWorkspace(ctx, readSessionIdentity(ctx), ws.PreviewWaits, ws.RequestID, failure); err != nil {
		return dex.ErrorWithStack(fmt.Errorf("announce the settled workspace %s to Preview command %s: %w", ws.RequestID, ws.PreviewWaits, err))
	}
	ws.TakePreviewWaits()
	return nil
}

// workspaceFailureCode is the failure code of a request whose Step moved to
// RecordWorkspaceFailure: WORKSPACE_TIMEOUT when its outcome did not arrive
// within the wait, else WORKSPACE_START_FAILED.
func workspaceFailureCode(demand workspaceDemand) string {
	if demand.TimedOut {
		return sandbox.WorkspaceFailureTimeout
	}
	return sandbox.WorkspaceFailureStart
}

// ownsRequest reports whether the branch owns its request: it settles it,
// announces the settlement to a waiting Preview and takes a joined
// continuation.
func (demand workspaceDemand) ownsRequest() bool { return !demand.PromptOnly && !demand.Reclaims }

// Workspace option presets keep admission and reclamation beside their owner.
var (
	// The cold-path Steps that commit lock only Workspace, so RPCs and model calls never wait;
	// they load messages and subagent records as settling a request may answer a start_agent call.
	workspaceRequestStepOptions = &dex.StepOptions{ExecuteMethodTimeout: time.Minute, ExecuteDurability: dex.StepDurabilityAsync,
		ExecuteLoadAttributeMaps: taskMessageMaps, ExecuteLockAttributes: []dex.AttributeLock{workspaceLock}}
	workspaceApplyStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteLoadAttributeMaps: taskMessageMaps, ExecuteLockAttributes: []dex.AttributeLock{workspaceLock}, ExecuteRetry: shortStepRetry}
	// AwaitSandboxDeadline waits on a business timer and decides under the Workspace lock,
	// atomically with the dispatches that extend it: a running tool or task holds the Sandbox.
	sandboxDeadlineStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteLockAttributes: []dex.AttributeLock{workspaceLock}, ExecuteRetry: shortStepRetry, ExecuteLoadAttributeMaps: taskMaps}
)

// applyWorkspaceStepOptions attaches the workspace failure route to its settlement policy.
func applyWorkspaceStepOptions() *dex.StepOptions {
	options := *workspaceApplyStepOptions
	options.ExecuteFailure = dex.ProceedToOnExecuteFailure(RecordWorkspaceFailure{}, nil)
	return &options
}

// updatePreviewAutoDeploy records the account selection for pushed Preview handoffs.
func updatePreviewAutoDeploy(ctx dex.Context, input agentloop.PreviewAutoDeployRequest) (*dex.RPCResult[bool], error) {
	if !acceptsUserCommand(ctx) {
		return rpcAccepted(false), nil
	}
	if dexflow.ValueOrZero(previewAutoDeployAttribute.Get(ctx)) != input.IsEnabled {
		comm.MustDo(previewAutoDeployAttribute.Set(ctx, input.IsEnabled))
		if input.Actor != nil {
			comm.MustDo(previewAutoDeployChangedByAttribute.Set(ctx, *input.Actor))
		} else {
			comm.MustDo(previewAutoDeployChangedByAttribute.Delete(ctx))
		}
	}
	return rpcAccepted(true), nil
}

// renewSessionWorkspace updates a READY workspace lease under the command locks.
func renewSessionWorkspace(ctx dex.Context) bool {
	ws := dexflow.ValueOrZero(workspaceAttribute.Get(ctx))
	if !acceptsUserCommand(ctx) || ws.State != agentloop.WorkspaceReady {
		return false
	}
	if ws.ExtendDeadline(time.Now(), agentloop.KeepRunningHold) {
		comm.MustDo(workspaceAttribute.Set(ctx, ws))
	}
	return true
}
