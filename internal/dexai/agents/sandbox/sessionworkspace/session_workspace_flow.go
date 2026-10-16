// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sessionworkspace

import (
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
)

var (
	// The Project, its current instance, the outcome of the last command and the admission of commands.
	projectIDAttribute = dex.DefineAttribute[string]("ProjectID")
	InstanceAttribute  = dex.DefineAttribute[Instance]("Instance")
	OutcomeAttribute   = dex.DefineAttribute[Outcome]("Outcome")
	admissionAttribute = dex.DefineAttribute[Admission]("Admission")
	// The Session's commands, run one at a time, and one provisioning report per WorkspaceRequestID.
	workspaceCommandsChannel = dex.DefineChannel[WorkspaceCommand]("WorkspaceCommands")
	provisionOutcomesChannel = dex.DefineChannelMap[ProvisionReport]("ProvisionOutcomes")
)

// SessionWorkspaceFlow owns the Sandbox instances of a Project's Session: it provisions, reclaims and
// replaces them one command at a time, so one volume carries one Pod, and reports each outcome.
type SessionWorkspaceFlow struct {
	dex.FlowDefaults
	dependency SessionWorkspaceFlowDependency
	reclaimer  *reclaimer
}

// GetSteps registers the command loop the package README's Step graph describes.
func (flow *SessionWorkspaceFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(AwaitWorkspaceCommand{}), dex.DefineStep(ProvisionInstance{flow: flow}), dex.DefineStep(AwaitProvisioningOutcome{flow: flow}),
		dex.DefineStep(ReclaimInstance{flow: flow}), dex.DefineStep(RecordProvisionFailure{}), dex.DefineStep(RecordReclaimFailure{}),
		dex.DefineStep(ReportToSession{flow: flow}), dex.DefineStep(RecordReportFailure{}), dex.DefineStep(CompleteSessionWorkspace{}),
	}
}

// GetRPCs registers the Session's commands, the provisioning's report, the retained read and the close.
func (flow *SessionWorkspaceFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.SubmitWorkspaceCommand, dexflow.ShortRPC(admissionLock)),
		dex.DefineRPC(flow.ReportProvisionOutcome, dexflow.ShortRPC(admissionLock)),
		dex.DefineRPC(flow.GetSessionWorkspace, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexSummary, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexDisplay, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.CompleteSessionWorkspace, dexflow.ShortRPC(admissionLock)),
	}
}

// GetPersistenceSchema registers every durable value.
func (*SessionWorkspaceFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{projectIDAttribute, InstanceAttribute, OutcomeAttribute, admissionAttribute},
		Channels:   []dex.ChannelDef{workspaceCommandsChannel, provisionOutcomesChannel},
	}
}

// Only the RPCs write Admission; the Steps form one chain, so each is the single writer of
// Instance and Outcome and none holds a lock that would make the RPCs wait.
var (
	admissionLock         = dex.LockAttribute(admissionAttribute)
	readRPCTimeout        = 5 * time.Second
	commandStepOptions    = dexflow.StateStepOptions()
	completionStepOptions = &dex.StepOptions{ExecuteMethodTimeout: 30 * time.Second, ExecuteDurability: dex.StepDurabilitySync,
		ExecuteRetry: dexflow.StepRetry(2 * time.Minute), ExecuteLoadChannels: []dex.ChannelDef{workspaceCommandsChannel}}
)

// provisioningWaitBudget bounds the wait for a provisioning's report: it outlasts the
// provisioning's own wait limits and the retries of its report.
const provisioningWaitBudget = provision.Budget + 10*time.Minute

// SubmitWorkspaceCommand admits one command of the Session (Session Service only): accepted when
// published or a repeat, refused for an older request's command or once closing (admit).
func (*SessionWorkspaceFlow) SubmitWorkspaceCommand(ctx dex.Context, command WorkspaceCommand) (*dex.RPCResult[commandAnswer], error) {
	if err := command.validate(); err != nil {
		return nil, fmt.Errorf("invalid workspace command %q: %w", command.ID, err)
	}
	admission := dexflow.ValueOrZero(admissionAttribute.Get(ctx))
	publishes, accepted := admission.admit(command)
	if publishes {
		admission.Last = &command
		comm.MustDo(admissionAttribute.Set(ctx, admission))
		comm.MustDo(workspaceCommandsChannel.Publish(ctx, command))
	}
	return &dex.RPCResult[commandAnswer]{Output: commandAnswer{Accepted: accepted, Closed: admission.Closed}}, nil
}

// ReportProvisionOutcome admits the report of the provisioning of the current instance
// (Session Service only); a repeat answers true, any other instance's report false.
func (*SessionWorkspaceFlow) ReportProvisionOutcome(ctx dex.Context, report ProvisionReport) (*dex.RPCResult[bool], error) {
	instance := comm.Must(InstanceAttribute.Get(ctx))
	if instance.InstanceID != report.WorkspaceRequestID {
		return &dex.RPCResult[bool]{Output: false}, nil
	}
	if instance.State == InstanceProvisioning && provisionOutcomesChannel.Size(ctx, report.WorkspaceRequestID) == 0 {
		comm.MustDo(provisionOutcomesChannel.Publish(ctx, report.WorkspaceRequestID, report))
	}
	return &dex.RPCResult[bool]{Output: true}, nil
}

// GetSessionWorkspace reads the instance without a lock, so it also serves a completed
// execution for the engine's retention; Studio Runs, the Preview and the cleanup read it.
func (*SessionWorkspaceFlow) GetSessionWorkspace(ctx dex.Context, _ dex.None) (*dex.RPCResult[Snapshot], error) {
	snapshot := Snapshot{ProjectID: comm.Must(projectIDAttribute.Get(ctx)), Instance: comm.Must(InstanceAttribute.Get(ctx)),
		Outcome: dexflow.OptionalValue(OutcomeAttribute.Get(ctx)), Closed: dexflow.ValueOrZero(admissionAttribute.Get(ctx)).Closed}
	return &dex.RPCResult[Snapshot]{Output: snapshot}, nil
}

// GetDexSummary is the workspace row of the Dex Web Flow list, whose FlowID names the Project.
func (*SessionWorkspaceFlow) GetDexSummary(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay is the workspace header of the Dex Web Flow view.
//
// dex:field attribute-key:Instance value-type:json editable:false description:"The Session's current Sandbox instance"
// dex:field attribute-key:Outcome value-type:json editable:false description:"Outcome of the last command and how the Session answered it"
func (*SessionWorkspaceFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"Instance": comm.Must(InstanceAttribute.Get(ctx)), "Outcome": dexflow.OptionalValue(OutcomeAttribute.Get(ctx)),
	}}, nil
}

// CompleteSessionWorkspace ends the Flow of a deleting Project (Session Service only): it refuses
// later commands and cancels a provisioning or reclamation; the deletion releases by label.
func (*SessionWorkspaceFlow) CompleteSessionWorkspace(ctx dex.Context, _ dex.None) (*dex.RPCResult[bool], error) {
	admission := dexflow.ValueOrZero(admissionAttribute.Get(ctx))
	if admission.Closed {
		return &dex.RPCResult[bool]{Output: true}, nil
	}
	admission.Closed = true
	comm.MustDo(admissionAttribute.Set(ctx, admission))
	return (&dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{dex.MovementOf(CompleteSessionWorkspace{}, nil)}}).CancelSteps(
		AwaitWorkspaceCommand{}, ProvisionInstance{}, AwaitProvisioningOutcome{}, ReclaimInstance{}, RecordProvisionFailure{}, RecordReclaimFailure{}, ReportToSession{}, RecordReportFailure{}), nil
}

// dex:group group-id:command group-label:"Command"
// dex:explanation text:"Wait for the next command of the Session, then record which instance it provisions or reclaims; a provisioning first reclaims an instance whose Pod may still run."
type AwaitWorkspaceCommand struct {
	dex.StepDefaults
}

func (AwaitWorkspaceCommand) GetStepOptions() *dex.StepOptions { return commandStepOptions }

func (AwaitWorkspaceCommand) WaitFor(dex.Context, dex.None) (*dex.Wait, error) {
	return dex.Until(workspaceCommandsChannel.ForOne()), nil
}

// Execute commits the instance the command acts on before any effect, so a cleanup and a
// timeout always know which provisioning may still run.
func (AwaitWorkspaceCommand) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	task := workspaceTask{Command: comm.Must(workspaceCommandsChannel.GetConditionResults(ctx))[0]}
	instance, now := comm.Must(InstanceAttribute.Get(ctx)), time.Now()
	// Admission orders the commands, so an instance that may run a Pod belongs to the command's request or an older one.
	reclaims := instance.needsReclaim()
	switch {
	case reclaims:
		task.AwaitsProvisioning = instance.beginReclaim(task.Command.WorkspaceRequestID, now)
	case task.Command.Kind == CommandProvision:
		instance.beginProvisioning(task.Command.WorkspaceRequestID, now)
	default:
		// The instance of the request is gone already: the Session is told it is reclaimed.
		task.Outcome = &Outcome{WorkspaceRequestID: task.Command.WorkspaceRequestID, Kind: OutcomeReclaimed}
		comm.MustDo(OutcomeAttribute.Set(ctx, *task.Outcome))
		return dex.GoTo(ReportToSession{}, task), nil
	}
	comm.MustDo(InstanceAttribute.Set(ctx, instance))
	if reclaims {
		return dex.GoTo(ReclaimInstance{}, task), nil
	}
	return dex.GoTo(ProvisionInstance{}, task), nil
}

// dex:group group-id:provision group-label:"Provision"
// dex:explanation text:"Start the SandboxProvisionFlow of the request's instance on the volume earlier instances left."
type ProvisionInstance struct {
	dex.StepDefaultsNoWaitFor[workspaceTask]
	flow *SessionWorkspaceFlow
}

func (ProvisionInstance) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordProvisionFailure{}, nil))
}

// Execute starts the provisioning, which is idempotent for the request; a business
// rejection settles the request at once.
func (step ProvisionInstance) Execute(ctx dex.Context, task workspaceTask) (*dex.StepDecision, error) {
	instance := comm.Must(InstanceAttribute.Get(ctx))
	request := ProvisionRequest{WorkspaceRequestID: instance.InstanceID, HeadCommit: task.Command.HeadCommit, Retained: instance.Resources.Retained()}
	err := step.flow.dependency.StartProvisioning(ctx, comm.Must(projectIDAttribute.Get(ctx)), request)
	if errors.Is(err, ErrWorkspaceStartRejected) {
		task.Failure = failure(sandbox.WorkspaceFailureStart, err.Error())
		return dex.GoTo(RecordProvisionFailure{}, task), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("provision the Session workspace %s: %w", instance.InstanceID, err))
	}
	return dex.GoTo(AwaitProvisioningOutcome{}, task), nil
}

// dex:group group-id:provision group-label:"Provision"
// dex:explanation text:"Wait, bounded, for the provisioning's report, then read its outcome from the provisioning and record the instance ready or failed."
type AwaitProvisioningOutcome struct {
	dex.StepDefaults
	flow *SessionWorkspaceFlow
}

func (AwaitProvisioningOutcome) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordProvisionFailure{}, nil))
}

// WaitFor waits for the report of the provisioning, which only this Step consumes, or for
// the bounded wait that outlasts the provisioning.
func (AwaitProvisioningOutcome) WaitFor(_ dex.Context, task workspaceTask) (*dex.Wait, error) {
	return dex.AnyOf(provisionOutcomesChannel.ForOne(task.Command.WorkspaceRequestID), dex.Timer(provisioningWaitBudget)), nil
}

// Execute reads the outcome from the provisioning, which owns it. Without one in time the
// request fails WORKSPACE_TIMEOUT and the instance stays provisioning for the next reclamation.
func (step AwaitProvisioningOutcome) Execute(ctx dex.Context, task workspaceTask) (*dex.StepDecision, error) {
	instance := comm.Must(InstanceAttribute.Get(ctx))
	provisioning, err := step.flow.dependency.ObserveProvisioning(ctx, comm.Must(projectIDAttribute.Get(ctx)), instance.InstanceID)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("read the provisioning of %s: %w", instance.InstanceID, err))
	}
	if provisioning == nil && !ctx.HasTimerFired() {
		return nil, dex.ErrorWithStack(fmt.Errorf("the provisioning of %s reported an outcome it does not hold", instance.InstanceID))
	}
	if provisioning == nil {
		task.Failure = failure(sandbox.WorkspaceFailureTimeout, fmt.Sprintf("the provisioning of %s reported no outcome within %s", instance.InstanceID, provisioningWaitBudget))
		return dex.GoTo(RecordProvisionFailure{}, task), nil
	}
	instance.settle(*provisioning, time.Now())
	comm.MustDo(InstanceAttribute.Set(ctx, instance))
	task.Outcome = &Outcome{WorkspaceRequestID: instance.InstanceID, Kind: OutcomeReady, Lease: instance.Lease, Failure: instance.Failure}
	if instance.Failure != nil {
		task.Outcome.Kind = OutcomeFailed
	}
	comm.MustDo(OutcomeAttribute.Set(ctx, *task.Outcome))
	return dex.GoTo(ReportToSession{}, task), nil
}

// dex:group group-id:reclaim group-label:"Reclaim"
// dex:explanation text:"Wait for a provisioning that may still run, have a Preview that holds the instance release it, stop the Preview engine, record the workspace and delete the Pod, keeping the volume; then provision the request that waited for it."
type ReclaimInstance struct {
	dex.StepDefaultsNoWaitFor[workspaceTask]
	flow *SessionWorkspaceFlow
}

func (ReclaimInstance) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(reclaimBudget, dex.ProceedToOnExecuteFailure(RecordReclaimFailure{}, nil))
}

// Execute polls without a lock (reclaimInstance); the Session fenced the instance from tools
// before it sent the command. A definite refusal keeps the resources.
func (step ReclaimInstance) Execute(ctx dex.Context, task workspaceTask) (*dex.StepDecision, error) {
	instance := comm.Must(InstanceAttribute.Get(ctx))
	resources, volumeState, err := step.flow.reclaimInstance(ctx, comm.Must(projectIDAttribute.Get(ctx)), instance, task)
	if errors.Is(err, ErrRefused) {
		task.Failure = failure(sandbox.WorkspaceFailureReclaim, err.Error())
		return dex.GoTo(RecordReclaimFailure{}, task), nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now()
	instance.reclaimed(resources, volumeState, now)
	if task.Command.Kind == CommandProvision {
		instance.beginProvisioning(task.Command.WorkspaceRequestID, now)
		comm.MustDo(InstanceAttribute.Set(ctx, instance))
		return dex.GoTo(ProvisionInstance{}, task), nil
	}
	comm.MustDo(InstanceAttribute.Set(ctx, instance))
	task.Outcome = &Outcome{WorkspaceRequestID: task.Command.WorkspaceRequestID, Kind: OutcomeReclaimed}
	comm.MustDo(OutcomeAttribute.Set(ctx, *task.Outcome))
	return dex.GoTo(ReportToSession{}, task), nil
}

// dex:group group-id:provision group-label:"Provision"
// dex:explanation text:"Record a request whose provisioning was rejected, exhausted its retries or reported nothing in time, keeping the instance provisioning, and report the failure."
type RecordProvisionFailure struct {
	dex.StepDefaultsNoWaitFor[workspaceTask]
}

func (RecordProvisionFailure) GetStepOptions() *dex.StepOptions { return commandStepOptions }

// Execute keeps the instance provisioning, as an unknown outcome may still create objects:
// the next reclamation waits for the provisioning's end.
func (RecordProvisionFailure) Execute(ctx dex.Context, task workspaceTask) (*dex.StepDecision, error) {
	instance := comm.Must(InstanceAttribute.Get(ctx))
	instance.Failure = task.Failure
	if instance.Failure == nil {
		instance.Failure = failure(sandbox.WorkspaceFailureStart, dexflow.RecoveryDetail(ctx))
	}
	comm.MustDo(InstanceAttribute.Set(ctx, instance))
	task.Outcome = &Outcome{WorkspaceRequestID: task.Command.WorkspaceRequestID, Kind: OutcomeFailed, Failure: instance.Failure}
	comm.MustDo(OutcomeAttribute.Set(ctx, *task.Outcome))
	return dex.GoTo(ReportToSession{}, task), nil
}

// dex:group group-id:reclaim group-label:"Reclaim"
// dex:explanation text:"Record a reclamation that did not finish, keeping the instance's resources, and fail the request that waited for the volume."
type RecordReclaimFailure struct {
	dex.StepDefaultsNoWaitFor[workspaceTask]
}

func (RecordReclaimFailure) GetStepOptions() *dex.StepOptions { return commandStepOptions }

// Execute keeps the resources for the next reclamation or the Project's deletion; a
// reclamation of its own request tells the Session nothing.
func (RecordReclaimFailure) Execute(ctx dex.Context, task workspaceTask) (*dex.StepDecision, error) {
	instance, detail := comm.Must(InstanceAttribute.Get(ctx)), dexflow.RecoveryDetail(ctx)
	if task.Failure != nil {
		detail = task.Failure.Detail
	}
	instance.reclaimFailed(detail, time.Now())
	comm.MustDo(InstanceAttribute.Set(ctx, instance))
	if task.Command.Kind != CommandProvision {
		return dex.GoTo(AwaitWorkspaceCommand{}, nil), nil
	}
	task.Outcome = &Outcome{WorkspaceRequestID: task.Command.WorkspaceRequestID, Kind: OutcomeFailed, Failure: instance.Failure}
	comm.MustDo(OutcomeAttribute.Set(ctx, *task.Outcome))
	return dex.GoTo(ReportToSession{}, task), nil
}

// dex:group group-id:command group-label:"Command"
// dex:explanation text:"Report the outcome to the Session and record its answer; an instance the Session declined is reclaimed, and an undelivered report waits for the Session's own bounded wait."
type ReportToSession struct {
	dex.StepDefaultsNoWaitFor[workspaceTask]
	flow *SessionWorkspaceFlow
}

func (ReportToSession) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordReportFailure{}, nil))
}

// Execute makes one call through the Service; a closed or replaced Session declines, which
// is only recorded, and a declined instance is reclaimed before the next command.
func (step ReportToSession) Execute(ctx dex.Context, task workspaceTask) (*dex.StepDecision, error) {
	admitted, err := step.flow.dependency.ReportWorkspaceOutcome(ctx, comm.Must(projectIDAttribute.Get(ctx)), *task.Outcome)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("report the %s outcome of %s to the Session: %w", task.Outcome.Kind, task.Outcome.WorkspaceRequestID, err))
	}
	outcome := *task.Outcome
	outcome.Reported = reportDeclined
	if admitted {
		outcome.Reported = reportAdmitted
	}
	comm.MustDo(OutcomeAttribute.Set(ctx, outcome))
	instance := comm.Must(InstanceAttribute.Get(ctx))
	if admitted || outcome.Kind == OutcomeReclaimed || instance.InstanceID != outcome.WorkspaceRequestID || !instance.needsReclaim() {
		return dex.GoTo(AwaitWorkspaceCommand{}, nil), nil
	}
	reclamation := workspaceTask{Command: NewCommand(CommandReclaim, instance.InstanceID, "", task.Command.PreviewHolds)}
	reclamation.AwaitsProvisioning = instance.beginReclaim(instance.InstanceID, time.Now())
	comm.MustDo(InstanceAttribute.Set(ctx, instance))
	return dex.GoTo(ReclaimInstance{}, reclamation), nil
}

// dex:group group-id:command group-label:"Command"
// dex:explanation text:"Record a report whose retries ran out as undelivered and wait for the next command; the Session reads the outcome once its own wait ends."
type RecordReportFailure struct {
	dex.StepDefaultsNoWaitFor[workspaceTask]
}

func (RecordReportFailure) GetStepOptions() *dex.StepOptions { return commandStepOptions }

func (RecordReportFailure) Execute(ctx dex.Context, task workspaceTask) (*dex.StepDecision, error) {
	outcome := *task.Outcome
	outcome.Reported = reportUndelivered
	comm.MustDo(OutcomeAttribute.Set(ctx, outcome))
	return dex.GoTo(AwaitWorkspaceCommand{}, nil), nil
}

// dex:group group-id:command group-label:"Command"
// dex:explanation text:"Complete the workspace of a deleting Project, discarding pending commands; the deletion releases the instance's resources by label."
type CompleteSessionWorkspace struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (CompleteSessionWorkspace) GetStepOptions() *dex.StepOptions { return completionStepOptions }

func (CompleteSessionWorkspace) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	for _, message := range comm.Must(workspaceCommandsChannel.PendingMessages(ctx)) {
		comm.MustDo(workspaceCommandsChannel.Delete(ctx, message.MessageID))
	}
	return dex.GracefulComplete(receipt{ProjectID: comm.Must(projectIDAttribute.Get(ctx))}), nil
}
