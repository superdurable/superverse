// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package provision

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
)

var (
	RequestAttribute          = dex.DefineAttribute[Request]("Request")
	ResourcesAttribute        = dex.DefineAttribute[sandbox.Resources]("Resources")
	CheckoutAttribute         = dex.DefineAttribute[Checkout]("Checkout")
	WorkspaceTimingsAttribute = dex.DefineAttribute[[]dexflow.PhaseTiming]("WorkspaceTimings")
	OutcomeAttribute          = dex.DefineAttribute[*Outcome]("Outcome")
	BootstrapAttribute        = dex.DefineAttribute[Bootstrap]("Bootstrap")
)

// SandboxProvisionFlow provisions one Sandbox instance of a Session: it creates, checks out and verifies
// the instance, reports to the SessionWorkspaceFlow that started it, which owns the instance, and completes.
type SandboxProvisionFlow struct {
	dex.FlowDefaults
	provider   sandbox.InstanceProvider
	workspace  *workspace.Capabilities
	dependency SandboxProvisionFlowDependency
}

func (f *SandboxProvisionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{RequestAttribute, ResourcesAttribute, CheckoutAttribute, WorkspaceTimingsAttribute, OutcomeAttribute, BootstrapAttribute}}
}

func (f *SandboxProvisionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(ProvisionInstance{f: f}),
		dex.DefineStep(CheckoutWorkspace{f: f}),
		dex.DefineStep(StartWorkspaceBootstrap{f: f}),
		dex.DefineStep(VerifyInstanceHealthy{f: f}),
		dex.DefineStep(ReportProvisionOutcome{f: f}),
		dex.DefineStep(RecordProvisionFailure{f: f}),
	}
}

func (f *SandboxProvisionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(f.GetProvision, dexflow.ShortRPC()),
		dex.DefineRPC(f.GetDexSummary, dexflow.ShortRPC()),
		dex.DefineRPC(f.GetDexDisplay, dexflow.ShortRPC()),
	}
}

// GetProvision is unlocked and returns output only, so it also serves the retained snapshot
// of a completed provisioning; its reads are the graph's read edges.
func (*SandboxProvisionFlow) GetProvision(ctx dex.Context, _ dex.None) (*dex.RPCResult[Snapshot], error) {
	return &dex.RPCResult[Snapshot]{Output: Snapshot{
		Request:          comm.Must(RequestAttribute.Get(ctx)),
		Resources:        comm.Must(ResourcesAttribute.Get(ctx)),
		Checkout:         comm.Must(CheckoutAttribute.Get(ctx)),
		WorkspaceTimings: dexflow.ValueOrZero(WorkspaceTimingsAttribute.Get(ctx)),
		// The Outcome is nil until the provisioning ended, and nil reads back as missing.
		Outcome:   dexflow.Optional(OutcomeAttribute.Get(ctx)),
		Bootstrap: dexflow.ValueOrZero(BootstrapAttribute.Get(ctx)),
	}}, nil
}

// GetDexSummary is the provisioning row of the Dex Web Flow list, whose FlowID
// already names the Project and the instance.
func (*SandboxProvisionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay is the provisioning header of the Dex Web Flow view.
//
// dex:field attribute-key:Request value-type:json editable:false description:"Requested instance"
// dex:field attribute-key:Outcome value-type:json editable:false description:"Outcome and how the Session workspace answered its report"
// dex:field attribute-key:Bootstrap value-type:json editable:false description:"Start of the template's bootstrap command"
func (*SandboxProvisionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"Request":   comm.Must(RequestAttribute.Get(ctx)),
		"Outcome":   dexflow.Optional(OutcomeAttribute.Get(ctx)),
		"Bootstrap": dexflow.ValueOrZero(BootstrapAttribute.Get(ctx)),
	}}, nil
}

// The Steps form one chain and only one of them runs at a time, so each is the
// single writer of what it saves and none holds a lock.

// dex:group group-id:provision group-label:"Provision"
// dex:explanation text:"Query exact resources and create the instance's Sandbox on the Session's workspace volume until it is Ready."
type ProvisionInstance struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *SandboxProvisionFlow
}

func (step ProvisionInstance) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(provisionBudget, dex.ProceedToOnExecuteFailure(RecordProvisionFailure{f: step.f}, nil))
}
func (step ProvisionInstance) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	instance, refusal := s.instance(), ""
	var refused failureCode
	err := dexflow.PollUntil(ctx, providerInterval, providerRoundTimeout, func(call context.Context) (bool, error) {
		resources, ready, err := step.f.provider.Provision(call, instance)
		// The next round, and a recorded refusal, keep the resources found.
		instance.Resources = resources
		if code := provisionRefusalCode(err); code != noFailure {
			refused, refusal = code, fmt.Sprintf("provision the Sandbox: %v", err)
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("provision the Sandbox: %w", err)
		}
		return ready, nil
	})
	if err != nil {
		return nil, err
	}
	s.Resources = instance.Resources
	if refused != noFailure {
		recordOutcome(ctx, s, s.failed(refused.failure(refusal)))
		return dex.GoTo(ReportProvisionOutcome{f: step.f}, nil), nil
	}
	comm.MustDo(ResourcesAttribute.Set(ctx, s.Resources))
	comm.MustDo(WorkspaceTimingsAttribute.Set(ctx, dexflow.BeginPhase(s.WorkspaceTimings, phaseCheckout, time.Now().UTC())))
	return dex.GoTo(CheckoutWorkspace{f: step.f}, nil), nil
}

// dex:group group-id:provision group-label:"Provision"
// dex:explanation text:"Wait until sandboxd accepts connections, then reconcile the precise repository checkout."
type CheckoutWorkspace struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *SandboxProvisionFlow
}

func (step CheckoutWorkspace) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(reachBudget+checkoutTimeout, dex.ProceedToOnExecuteFailure(RecordProvisionFailure{f: step.f}, nil))
}
func (step CheckoutWorkspace) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	binding := s.instance().Binding()
	// A Ready Sandbox can still be unreachable while its Service endpoints
	// propagate, so the checkout first waits until sandboxd accepts connections.
	err := dexflow.PollUntil(ctx, providerInterval, reachRoundTimeout, func(call context.Context) (bool, error) {
		reached := step.f.workspace.Reach(call, binding)
		if reached != nil && !errors.Is(reached, workspace.ErrUnreachable) {
			return false, fmt.Errorf("reach the Sandbox workspace: %w", reached)
		}
		return reached == nil, nil
	})
	if err != nil {
		return nil, err
	}
	// Checkout reconciles the admitted repository before repeating Git effects; once the retries
	// run out RecordProvisionFailure records CHECKOUT_FAILED with the redacted error.
	defer dexflow.KeepAlive(ctx, nil)()
	call, cancel := context.WithTimeout(ctx, checkoutTimeout)
	defer cancel()
	identity := s.Request.Identity
	out, err := step.f.workspace.Checkout(call, binding, identity.WorkspaceRepository(identity.Repository.HeadCommit))
	var known *sandboxd.ToolFailure
	if errors.As(err, &known) || errors.Is(err, repohost.ErrOtherHost) {
		// The workspace itself refuses the checkout (files but no repository, or another
		// repository host); asking again cannot change that.
		recordOutcome(ctx, s, s.failed(failureCheckout.failure(err.Error())))
		return dex.GoTo(ReportProvisionOutcome{f: step.f}, nil), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("check out the Sandbox repository: %w", err))
	}
	s.Checkout.recordHead(out)
	comm.MustDo(CheckoutAttribute.Set(ctx, s.Checkout))
	comm.MustDo(WorkspaceTimingsAttribute.Set(ctx, dexflow.EndPhase(s.WorkspaceTimings, time.Now().UTC())))
	return dex.GoTo(StartWorkspaceBootstrap{f: step.f}, nil), nil
}

// dex:group group-id:provision group-label:"Provision"
// dex:explanation text:"Start the bootstrap command the checked-out template declares as a command ledger run, without waiting for it; an error is recorded and never fails the provisioning."
type StartWorkspaceBootstrap struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *SandboxProvisionFlow
}

func (step StartWorkspaceBootstrap) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordProvisionFailure{f: step.f}, nil))
}
func (step StartWorkspaceBootstrap) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	id := s.Request.bootstrapCommandID()
	call, cancel := context.WithTimeout(ctx, bootstrapStartTimeout)
	defer cancel()
	started, err := step.f.workspace.StartBootstrap(call, s.instance().Binding(), id)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("start the workspace bootstrap %s: %w", id, err))
	}
	comm.MustDo(BootstrapAttribute.Set(ctx, recordedBootstrap(id, started)))
	return dex.GoTo(VerifyInstanceHealthy{f: step.f}, nil), nil
}

// dex:group group-id:provision group-label:"Provision"
// dex:explanation text:"Confirm that the checked-out instance is still the provisioned one, Ready and reachable; the editor workspace becomes ready without registering a manifest."
type VerifyInstanceHealthy struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *SandboxProvisionFlow
}

func (step VerifyInstanceHealthy) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(verifyBudget, dex.ProceedToOnExecuteFailure(RecordProvisionFailure{f: step.f}, nil))
}
func (step VerifyInstanceHealthy) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	instance, refusal := s.instance(), ""
	err := dexflow.PollUntil(ctx, providerInterval, providerRoundTimeout, func(call context.Context) (bool, error) {
		// Provisioning again adopts every recorded object only with its
		// recorded UID, so a Sandbox or Service replaced since is refused.
		resources, ready, err := step.f.provider.Provision(call, instance)
		if provisionRefusalCode(err) != noFailure {
			refusal = fmt.Sprintf("verify the provisioned Sandbox: %v", err)
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("verify the provisioned Sandbox: %w", err)
		}
		if !ready {
			return false, nil
		}
		instance.Resources = resources
		reached := step.f.workspace.Reach(call, instance.Binding())
		if reached != nil && !errors.Is(reached, workspace.ErrUnreachable) {
			return false, fmt.Errorf("reach the Sandbox workspace: %w", reached)
		}
		return reached == nil, nil
	})
	if err != nil {
		return nil, err
	}
	if refusal != "" {
		recordOutcome(ctx, s, s.failed(failureUnhealthy.failure(refusal)))
	} else {
		s.Resources = instance.Resources
		recordOutcome(ctx, s, Outcome{Status: OutcomeReady, Resources: s.Resources, Checkout: s.Checkout})
	}
	return dex.GoTo(ReportProvisionOutcome{f: step.f}, nil), nil
}

// dex:group group-id:provision group-label:"Provision"
// dex:explanation text:"Report the outcome to the SessionWorkspaceFlow, which admits it only while it waits for this instance, then complete."
type ReportProvisionOutcome struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *SandboxProvisionFlow
}

// GetStepOptions: bounded retries; once they run out RecordProvisionFailure records the outcome
// undelivered and completes, and the workspace Flow reads it when its own wait ends.
func (step ReportProvisionOutcome) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(dex.ProceedToOnExecuteFailure(RecordProvisionFailure{f: step.f}, nil))
}
func (step ReportProvisionOutcome) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	admitted, err := step.f.dependency.ReportProvisionOutcome(ctx, s.Request.Identity, s.Request.WorkspaceRequestID, s.Outcome.Status)
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("report the outcome of %s to its Session workspace: %w", s.Request.WorkspaceRequestID, err))
	}
	outcome := *s.Outcome
	outcome.Reported = reportDeclined
	if admitted {
		outcome.Reported = reportAdmitted
	}
	comm.MustDo(OutcomeAttribute.Set(ctx, &outcome))
	return dex.GracefulComplete(s.receipt()), nil
}

// dex:group group-id:provision group-label:"Provision"
// dex:explanation text:"Record the failure of a Step whose retries ran out or whose wait limit passed, with the final error, and report it; an undelivered report completes, and a bootstrap that did not start goes on to the health check."
type RecordProvisionFailure struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	f *SandboxProvisionFlow
}

func (RecordProvisionFailure) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }
func (step RecordProvisionFailure) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	s := readSnapshot(ctx)
	if dexflow.RecoveryStep(ctx) == dex.GetFinalStepType(StartWorkspaceBootstrap{}) {
		comm.MustDo(BootstrapAttribute.Set(ctx, Bootstrap{Refusal: "the bootstrap start was not confirmed: " + dexflow.RecoveryDetail(ctx)}))
		return dex.GoTo(VerifyInstanceHealthy{f: step.f}, nil), nil
	}
	if dexflow.RecoveryStep(ctx) == dex.GetFinalStepType(ReportProvisionOutcome{}) {
		outcome := *s.Outcome
		outcome.Reported, outcome.ReportCause = reportUndelivered, dexflow.RecoveryDetail(ctx)
		comm.MustDo(OutcomeAttribute.Set(ctx, &outcome))
		return dex.GracefulComplete(s.receipt()), nil
	}
	recordOutcome(ctx, s, s.failed(executeFailureCode(ctx).failure(dexflow.RecoveryDetail(ctx))))
	return dex.GoTo(ReportProvisionOutcome{f: step.f}, nil), nil
}

// stepFailures name the failure each provider Step records by Step type, once its retries ran
// out and once the engine ended its wait at the Step's wait limit.
var stepFailures = map[string]struct{ exhausted, timedOut failureCode }{
	dex.GetFinalStepType(ProvisionInstance{}):     {failureProvision, failureDeadline},
	dex.GetFinalStepType(CheckoutWorkspace{}):     {failureCheckout, failureCheckout},
	dex.GetFinalStepType(VerifyInstanceHealthy{}): {failureUnhealthy, failureUnhealthy},
}

// executeFailureCode is the failure code of the provider Step whose Execute failure moved the
// provisioning to RecordProvisionFailure; any other predecessor is a programming error.
func executeFailureCode(ctx dex.Context) failureCode {
	failures, found := stepFailures[dexflow.RecoveryStep(ctx)]
	if !found {
		panic("provisioning failure record has no provider Step predecessor: " + ctx.FromStepExecutionID())
	}
	if dexflow.RecoveryTimedOut(ctx) {
		return failures.timedOut
	}
	return failures.exhausted
}

// recordOutcome records the outcome with the resources found, which ends the
// phase in progress; ReportProvisionOutcome reports it next.
func recordOutcome(ctx dex.Context, s Snapshot, outcome Outcome) {
	comm.MustDo(ResourcesAttribute.Set(ctx, s.Resources))
	comm.MustDo(WorkspaceTimingsAttribute.Set(ctx, dexflow.EndPhase(s.WorkspaceTimings, time.Now().UTC())))
	comm.MustDo(OutcomeAttribute.Set(ctx, &outcome))
}
