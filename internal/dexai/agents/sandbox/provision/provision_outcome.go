// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package provision

import (
	"errors"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// Request is the immutable input of one provisioning: the Project of the
// Session with the repository and HEAD its workspace checks out, the
// WorkspaceRequestID that names the instance and what the Session's earlier
// instances retained.
type Request struct {
	Identity           sandbox.Identity
	WorkspaceRequestID string
	Retained           sandbox.Retained
}

// instance is the instance the request provisions, with the resources found so far.
func (r Request) instance(resources sandbox.Resources) sandbox.Instance {
	return sandbox.Instance{Identity: r.Identity, WorkspaceRequestID: r.WorkspaceRequestID, Generation: sandbox.InstanceGeneration, Resources: resources}
}

// requestIdentity is what the Start RequestID fingerprints: the Session's
// FlowID, the instance and the retained volume with its profile. A retry of
// the same request repeats it; another request for the same FlowID cannot.
func (r Request) requestIdentity() string {
	return flowid.Fingerprint(struct {
		SessionFlowID, WorkspaceRequestID string
		Volume                            sandbox.ResourceRef
		Profile                           string
	}{flowid.SessionFlowID(r.Identity.ProjectID), r.WorkspaceRequestID, r.Retained.Resources.Volume, r.Retained.Resources.Profile})
}

// Checkout is what the checkout found: the commit the instance records,
// which the lease expects as HEAD, and whether the volume holds uncommitted
// changes (sandbox.Workspace*).
type Checkout struct {
	HeadCommit, WorkspaceState string
}

// OutcomeStatus is how one provisioning ended. It is a struct that string
// literals cannot convert to: the values below are its only ones, persisted
// by their names, and the zero OutcomeStatus is no outcome.
type OutcomeStatus struct{ name string }

var (
	OutcomeReady  = OutcomeStatus{"ready"}
	OutcomeFailed = OutcomeStatus{"failed"}
)

func (status OutcomeStatus) String() string               { return status.name }
func (status OutcomeStatus) MarshalText() ([]byte, error) { return []byte(status.name), nil }
func (status *OutcomeStatus) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, status, OutcomeStatus.String, OutcomeReady, OutcomeFailed)
}

// reportAnswer is how the SessionWorkspaceFlow answered the report of the outcome, a
// struct like OutcomeStatus; the zero reportAnswer is a report not answered
// yet.
type reportAnswer struct{ name string }

var (
	// reportAdmitted: the workspace Flow waited for this instance and took the outcome.
	reportAdmitted = reportAnswer{"admitted"}
	// reportDeclined: the workspace Flow waits for no such instance or ended.
	// What the provisioning created is reclaimed by its next reclamation or
	// released with the Project.
	reportDeclined = reportAnswer{"declined"}
	// reportUndelivered: the report's retries ran out; the workspace Flow
	// reads the outcome when its own bounded wait ends.
	reportUndelivered = reportAnswer{"undelivered"}
)

func (answer reportAnswer) String() string               { return answer.name }
func (answer reportAnswer) MarshalText() ([]byte, error) { return []byte(answer.name), nil }
func (answer *reportAnswer) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, answer, reportAnswer.String, reportAdmitted, reportDeclined, reportUndelivered)
}

// Outcome is how the provisioning ended: ready with the instance's resources
// and its checkout, or failed with the resources found so far, which the
// workspace Flow reclaims. Reported is how it answered the report, with the
// cause of an undelivered one.
type Outcome struct {
	Status      OutcomeStatus
	Resources   sandbox.Resources
	Checkout    Checkout
	Failure     *dexflow.Failure
	Reported    reportAnswer
	ReportCause string
}

// Snapshot is one provisioning, read from its Attributes. Request is fixed by
// the start; Resources, Checkout, WorkspaceTimings and Bootstrap are
// observations of the Steps; Outcome is nil until the provisioning ended, and
// once it is set no Step causes another provider effect.
type Snapshot struct {
	Request          Request
	Resources        sandbox.Resources
	Checkout         Checkout
	WorkspaceTimings []dexflow.PhaseTiming
	Outcome          *Outcome
	Bootstrap        Bootstrap
}

// Bootstrap is how the provisioning started the bootstrap command of the
// checked-out template, which nothing waits for: the command ledger run
// CommandID and when it started, or Refusal, why no run was started or that a
// start whose retries ran out was not confirmed. Declared is false when HEAD declares no
// bootstrap command; the zero Bootstrap is a start not yet attempted.
type Bootstrap struct {
	Declared  bool
	CommandID string
	StartedAt time.Time
	Refusal   string
}

// bootstrapCommandID is the command ledger ID of the instance's bootstrap
// run, such as platform-bootstrap-ws-41.
func (r Request) bootstrapCommandID() sandboxd.ExecutionID {
	return sandboxd.ExecutionID("platform-bootstrap-" + r.WorkspaceRequestID)
}

// recordedBootstrap is the Bootstrap of a start of run id.
func recordedBootstrap(id sandboxd.ExecutionID, started workspace.BootstrapStart) Bootstrap {
	bootstrap := Bootstrap{Declared: started.Declared, StartedAt: started.StartedAt, Refusal: started.Refusal}
	if started.Declared && started.Refusal == "" {
		bootstrap.CommandID = string(id)
	}
	return bootstrap
}

// recordHead records the commit of a checkout: GitHub serves the Preview
// manifest from it, so it is the checkout HEAD when the remote branch
// contains it and otherwise the remote branch HEAD the checkout is based on.
// Checkout adopts a retained volume as it is, clean or dirty; only a fresh
// volume is known to be clean.
func (c *Checkout) recordHead(out git.CheckoutResult) {
	c.HeadCommit = out.BaseCommitSHA
	if out.HeadPushed {
		c.HeadCommit = out.HeadCommitSHA
	}
	if c.WorkspaceState == sandbox.WorkspaceAbsent {
		c.WorkspaceState = sandbox.WorkspaceClean
	}
}

// failed is the failed outcome of failure with the resources found so far.
func (s Snapshot) failed(failure *dexflow.Failure) Outcome {
	return Outcome{Status: OutcomeFailed, Resources: s.Resources, Checkout: s.Checkout, Failure: failure}
}

// receipt is the completion output of a provisioning.
type receipt struct {
	ProjectID, WorkspaceRequestID string
	Status                        OutcomeStatus
}

func (s Snapshot) receipt() receipt {
	return receipt{ProjectID: s.Request.Identity.ProjectID, WorkspaceRequestID: s.Request.WorkspaceRequestID, Status: s.Outcome.Status}
}

// instance is the provisioned instance as its provider sees it.
func (s Snapshot) instance() sandbox.Instance { return s.Request.instance(s.Resources) }

// Lease is the workspace capability of a ready instance, which the Session
// accepts: the instance, its transport binding and the repository expecting
// the HEAD the checkout recorded.
func (s Snapshot) Lease() workspace.Lease {
	instance := s.Request.instance(s.Outcome.Resources)
	return workspace.Lease{RequestID: s.Request.WorkspaceRequestID, Binding: instance.Binding(), Repository: s.Request.Identity.WorkspaceRepository(s.Outcome.Checkout.HeadCommit)}
}

// Phases of WorkspaceTimings: the provisioning starts provision, the
// provisioned instance starts checkout and the outcome ends the phase in
// progress.
const (
	phaseProvision = "provision"
	phaseCheckout  = "checkout"
)

// Wait limits and intervals of the provisioning's provider waits. The engine
// enforces each wait limit through PollingStepOptions.
const (
	// provisionBudget is the wait limit of the Sandbox becoming Ready, which
	// includes a cold pull of the coding image.
	provisionBudget = 30 * time.Minute
	// reachBudget is the part of the checkout wait limit that waits until a
	// Ready Sandbox's sandboxd accepts connections.
	reachBudget = 5 * time.Minute
	// checkoutTimeout bounds the Git commands of one checkout attempt; Git
	// bounds each of them at five minutes.
	checkoutTimeout = 10 * time.Minute
	// verifyBudget is the wait limit of the health check of the checked-out
	// instance.
	verifyBudget = 5 * time.Minute
	// providerInterval spaces the rounds of a provider wait; a user usually
	// watches it.
	providerInterval = 2 * time.Second
	// providerRoundTimeout bounds one round of a provider wait, which makes
	// several Kubernetes and sandboxd calls.
	providerRoundTimeout = 40 * time.Second
	// reachRoundTimeout bounds one connection to sandboxd, including the
	// ten-second startup of a native port-forward.
	reachRoundTimeout = 20 * time.Second
	// bootstrapStartTimeout bounds one attempt to start the bootstrap: a Git
	// read and two command ledger calls, within the provider attempt.
	bootstrapStartTimeout = 40 * time.Second
)

// Budget bounds a whole provisioning: the wait limits of its provider Steps
// and the report's retries. The Session waits for the outcome at most this
// long plus a margin.
const Budget = provisionBudget + reachBudget + checkoutTimeout + verifyBudget + 5*time.Minute

// failureCode is the code of a recorded provisioning failure: its persisted
// name, the message the user reads and whether asking again can help. The
// codes are declared once below. A zero failureCode is no code, and reading
// it panics.
type failureCode struct{ definition *failureDefinition }

type failureDefinition struct {
	name, message string
	// isTerminal marks a failure that cannot be retried as it is.
	isTerminal bool
}

// String is the persisted spelling of the code.
func (code failureCode) String() string { return code.definition.name }

// noFailure is what a classifier returns for an error that has no definite
// code, which its caller asks again.
var noFailure failureCode

func defineFailure(name, message string, isTerminal bool) failureCode {
	return failureCode{definition: &failureDefinition{name: name, message: message, isTerminal: isTerminal}}
}

// The failure codes of a provisioning. The exported code is the one other
// packages report themselves.
var (
	// failureExecutionStopped is how a read reports a provisioning whose engine
	// execution stopped before it recorded an outcome.
	failureExecutionStopped = defineFailure("WORKSPACE_EXECUTION_STOPPED", "The Sandbox provisioning's engine execution stopped before it ended. An operator reconciles what it created; the next workspace request reclaims it.", true)

	failureProfileMismatch = defineFailure("WORKSPACE_PROFILE_MISMATCH", "The retained workspace volume belongs to an earlier coding profile, which this platform no longer runs, and no workspace can start on it. Delete this Project and create a new one; its pushed commits stay in its archived repository.", true)
	failureVolumeInUse     = defineFailure("WORKSPACE_VOLUME_IN_USE", "Another Pod that nothing deletes mounts the workspace volume. Remove it, then request the workspace again.", true)
	failureProvision       = defineFailure("PROVISION_FAILED", "The Sandbox could not be provisioned. Check the cause; if a Sandbox object has another owner, resolve that conflict. Then request the workspace again.", false)
	failureDeadline        = defineFailure("PROVISION_DEADLINE", "The Sandbox was not ready in time. Check why its Pod or volume stays pending, then request the workspace again.", false)
	failureCheckout        = defineFailure("CHECKOUT_FAILED", "The Project repository could not be checked out into the workspace. Check the cause, the repository and its default branch, then request the workspace again.", false)
	failureUnhealthy       = defineFailure("INSTANCE_UNHEALTHY", "The checked-out Sandbox did not stay Ready and reachable, or its Sandbox or Service changed. Check the cause, then request the workspace again.", false)
)

// failure is the record of code with its cause, an error or a redacted
// provider answer truncated; no request or credential ever enters it. A
// terminal code needs an operator; any other is asked again.
func (code failureCode) failure(cause string) *dexflow.Failure {
	remedy := dexflow.RemedyApplyAgain
	if code.definition.isTerminal {
		remedy = dexflow.RemedyContactOperator
	}
	return &dexflow.Failure{Code: code.String(), Phase: phaseProvision, Message: dexflow.FailureMessage(code.definition.message, cause), Recoverable: !code.definition.isTerminal, Remedy: remedy}
}

// provisionRefusalCode classifies a definite refusal of provisioning by its
// typed cause. Every other error is asked again and has no code.
func provisionRefusalCode(err error) failureCode {
	switch {
	case errors.Is(err, sandbox.ErrProfileMismatch):
		return failureProfileMismatch
	case errors.Is(err, sandbox.ErrVolumeInUse):
		return failureVolumeInUse
	case errors.Is(err, infra.ErrOwnershipConflict):
		return failureProvision
	}
	return noFailure
}

// initialAttributes admits one provisioning and returns its initial
// Attributes. The Project must fit the deployment names of its Sandbox.
func initialAttributes(in Request, names deploymentnames.Names) ([]dex.InitialAttributeDef, error) {
	if err := flowid.ValidateSessionScope(names, in.Identity.ProjectID); err != nil {
		return nil, err
	}
	if in.WorkspaceRequestID == "" {
		return nil, errors.New("the provisioning names no workspace request")
	}
	// Only the namespace and the volume outlive an instance; the Pod is always new.
	retained := in.Retained.Resources
	resources := sandbox.Resources{Boundary: retained.Boundary, Volume: retained.Volume, Profile: retained.Profile}
	checkout := Checkout{WorkspaceState: sandbox.WorkspaceAbsent}
	if resources.Volume.UID != "" {
		checkout.WorkspaceState = sandbox.WorkspaceUnknown
	}
	attributes := &dexflow.InitialAttributes{}
	dexflow.Add(attributes, RequestAttribute, in)
	dexflow.Add(attributes, ResourcesAttribute, resources)
	dexflow.Add(attributes, CheckoutAttribute, checkout)
	dexflow.Add(attributes, WorkspaceTimingsAttribute, dexflow.BeginPhase(nil, phaseProvision, time.Now().UTC()))
	return attributes.Values(), nil
}

// readSnapshot reads the provisioning for a Step through GetProvision's reads.
func readSnapshot(ctx dex.Context) Snapshot {
	return comm.Must((*SandboxProvisionFlow).GetProvision(nil, ctx, nil)).Output
}

// The tests name these constants.
const (
	PhaseProvisionForTest = phaseProvision
	PhaseCheckoutForTest  = phaseCheckout
)

// The tests name these values.
var (
	FailureExecutionStoppedForTest = failureExecutionStopped
	ReportAdmittedForTest          = reportAdmitted
)
