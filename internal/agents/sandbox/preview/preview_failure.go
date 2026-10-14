// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"errors"
	"strings"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/platform"
	"github.com/superdurable/superversev2/internal/platform/repohost"
	projectconfig "github.com/superdurable/superversev2/internal/projects/config"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// failureCode is the code of a recorded Preview failure: its persisted name,
// the message the user reads, what the user can do about it and whether asking
// again can help. The codes are
// declared once below, and a persisted name is looked up with failureCodeOf.
// A zero failureCode is no code, and reading it panics.
type failureCode struct{ definition *failureDefinition }

type failureDefinition struct {
	name, message string
	remedy        dexflow.FailureRemedy
	// isTerminal marks a failure that cannot be retried as it is.
	isTerminal bool
}

// String is the persisted spelling of the code.
func (code failureCode) String() string { return code.definition.name }

// noFailure is what a classifier returns for an error that has no definite
// code, which its caller asks again.
var noFailure failureCode

// failureCodes are the declared codes by their persisted spelling.
var failureCodes = map[string]failureCode{}

func defineFailure(name, message string, remedy dexflow.FailureRemedy, isTerminal bool) failureCode {
	if _, isDeclared := failureCodes[name]; isDeclared {
		panic("Preview failure code " + name + " is declared twice")
	}
	code := failureCode{definition: &failureDefinition{name: name, message: message, remedy: remedy, isTerminal: isTerminal}}
	failureCodes[name] = code
	return code
}

func recoverableCode(name, message string, remedy dexflow.FailureRemedy) failureCode {
	return defineFailure(name, message, remedy, false)
}

// terminalCode is a code no retry recovers: an operator acts on it.
func terminalCode(name, message string) failureCode {
	return defineFailure(name, message, dexflow.RemedyContactOperator, true)
}

// failureCodeOf is the code a persisted spelling names. Only declared codes are
// persisted, so another spelling is a programming error.
func failureCodeOf(name string) failureCode {
	code, isDeclared := failureCodes[name]
	if !isDeclared {
		panic("unknown Preview failure code " + name)
	}
	return code
}

// The failure codes of the Preview's workspace and of a stopped execution.
var (
	// failureExecutionStopped is how a read reports a Preview whose engine
	// execution stopped while a command was in progress.
	failureExecutionStopped = terminalCode("PREVIEW_EXECUTION_STOPPED", "The Preview's engine execution stopped while a command was in progress. An operator must recover the execution before Preview starts again.")

	failureInvalidCommand       = recoverableCode(string(rejectionInvalidCommand), "The Preview received a command of an unknown kind and ran nothing. This is a platform defect; report it to the operator.", dexflow.RemedyContactOperator)
	failureSessionClosed        = recoverableCode("PREVIEW_SESSION_CLOSED", "The Project's Session does not run, so it lends no Sandbox to the Preview: the Project is being deleted, or the Session stopped and an operator must recover it.", dexflow.RemedyContactOperator)
	failureWorkspaceFailed      = recoverableCode("PREVIEW_WORKSPACE_FAILED", "The Session's Sandbox could not be prepared for the Preview. Check the Session's workspace failure, then try again.", dexflow.RemedyApplyAgain)
	failureWorkspaceUnavailable = recoverableCode("PREVIEW_WORKSPACE_UNAVAILABLE", "The Session's Sandbox did not become ready for the Preview in time. Check the Session's workspace, then try again.", dexflow.RemedyApplyAgain)
)

// The failure codes of a Preview stop and of the reclamation of its instance.
var (
	failurePreviewCleanupPermissionDenied  = terminalCode("PREVIEW_CLEANUP_PERMISSION_DENIED", "The provider denied Preview cleanup. Restore scope authorization before retrying.")
	failurePreviewCleanupOwnershipConflict = terminalCode("PREVIEW_CLEANUP_OWNERSHIP_CONFLICT", "Preview cleanup ownership changed. Resolve the scope or UID conflict before retrying.")
	failurePreviewCleanupOutcomeUnknown    = terminalCode("PREVIEW_CLEANUP_OUTCOME_UNKNOWN", "Preview cleanup is unconfirmed. Reconcile retained resources before retrying.")
	failurePreviewCleanupDeadline          = recoverableCode("PREVIEW_CLEANUP_DEADLINE", "Preview cleanup did not finish in time. Check that the Preview process and storage grant are gone, then stop Preview again.", dexflow.RemedyContactOperator)
)

// The failure codes of the Preview commands.
var (
	failurePreviewDirtyHead         = recoverableCode("PREVIEW_DIRTY_HEAD", "Commit and push all workspace changes, then build processes again.", dexflow.RemedyAskAgent)
	failurePreviewHeadNotReadable   = recoverableCode("PREVIEW_HEAD_NOT_READABLE", "Push the current Sandbox commit to main, then build processes again.", dexflow.RemedyAskAgent)
	failurePreviewManifest          = recoverableCode("PREVIEW_MANIFEST_FAILED", "Process manifest registration failed. Check dex-app.yaml and the Project configuration service, then build processes again.", dexflow.RemedyAskAgent)
	failurePreviewFDGInvalid        = recoverableCode("PREVIEW_FDG_INVALID", "Fix the process definitions, then commit, push, and build processes again.", dexflow.RemedyAskAgent)
	failurePreviewSourceNotPrepared = recoverableCode("PREVIEW_SOURCE_NOT_PREPARED", "Build the pushed version's processes before starting Preview.", dexflow.RemedyApplyAgain)
	failurePreviewConfiguration     = recoverableCode("PREVIEW_CONFIGURATION_FAILED", "Complete Setup · Preview, then Apply.", dexflow.RemedyOpenSetup)
	// failurePreviewIncompatible refuses a start whose configuration lacks
	// what the source declares; the refusal names every gap, and the running
	// Preview is left untouched.
	failurePreviewIncompatible = recoverableCode("PREVIEW_CONFIGURATION_INCOMPATIBLE", "The Preview configuration does not provide what the source declares.", dexflow.RemedyOpenSetup)
	// failurePreviewConfigurationChanged refuses an applying start whose
	// configuration changed after the user pressed Apply.
	//lint:ignore U1000 declared with its remedy here; the applying start (STUDIOSETUP APPLY) refuses with it
	failurePreviewConfigurationChanged = recoverableCode("PREVIEW_CONFIGURATION_CHANGED", "Setup changed after you pressed Apply: review it and Apply again.", dexflow.RemedyApplyAgain)
	failurePreviewSource               = recoverableCode("PREVIEW_SOURCE_FAILED", "Building processes did not finish. Check the cause, then build processes again.", dexflow.RemedyApplyAgain)
	failurePreviewStateFull            = recoverableCode("PREVIEW_STATE_FULL", "The workspace volume has less than 512 MiB free for the Preview engine. Free space in the workspace, then start Preview again.", dexflow.RemedyFreeWorkspace)
	failurePreviewStateInconsistent    = terminalCode("PREVIEW_STATE_INCONSISTENT", "The Preview engine's state on the workspace volume lost its execution store while its data remains. An operator must repair the Preview state before Preview can start.")
	failurePreviewEnvironment          = recoverableCode("PREVIEW_ENVIRONMENT_FAILED", "The Preview engine could not start in the Sandbox. Check the cause and the Preview logs, then start Preview again.", dexflow.RemedyApplyAgain)
	failurePreviewEnvironmentDeadline  = recoverableCode("PREVIEW_ENVIRONMENT_DEADLINE", "The Preview engine did not become ready in time. Check the Preview logs, then start Preview again.", dexflow.RemedyApplyAgain)
	failurePreviewOwnershipConflict    = terminalCode("PREVIEW_OWNERSHIP_CONFLICT", "A Sandbox, Service, grant or route object of the Preview changed owner or UID. Resolve the owner or UID conflict before starting Preview again.")
	failurePreview                     = recoverableCode("PREVIEW_FAILED", "The Preview application failed to build or to start. Check the cause and the Preview logs, fix the application, then prepare and start Preview again.", dexflow.RemedyAskAgent)
	failurePreviewStartDeadline        = recoverableCode("PREVIEW_START_DEADLINE", "The Preview supervisor did not finish the start in time. Start Preview again.", dexflow.RemedyApplyAgain)
	failurePreviewStartUnavailable     = recoverableCode("PREVIEW_START_UNAVAILABLE", "Preview could not start: the storage grant or the Preview supervisor did not answer. Check the cause, then start Preview again.", dexflow.RemedyApplyAgain)
	failurePreviewRoute                = recoverableCode("PREVIEW_ROUTE_FAILED", "Preview could not publish its route. Check the cause and the gateway, then start Preview again.", dexflow.RemedyApplyAgain)
	failurePreviewRouteDeadline        = recoverableCode("PREVIEW_ROUTE_DEADLINE", "The Preview route did not answer through the gateway in time. Check the gateway and the Preview application, then start Preview again.", dexflow.RemedyApplyAgain)
)

// FailureIncompatibleForTest is the code of a start refused for its
// configuration, which other packages' tests name.
var FailureIncompatibleForTest = failurePreviewIncompatible.String()

// providerStep is the registered Step type of a provider Step, which
// dexflow.RecoveryStep names.
func providerStep(step dex.Step[dex.None]) string { return dex.GetFinalStepType(step) }

// providerFailures name the failures each provider Step records, by its Step
// type: once its retries ran out, and once the engine ended its wait at the
// Step's wait limit; a Step without a code of its own for the wait limit
// records its exhausted code. The final error is the cause.
var providerFailures = map[string]struct{ exhausted, timedOut failureCode }{
	providerStep(AcquireSessionWorkspace{}):     {failureWorkspaceUnavailable, failureWorkspaceUnavailable},
	providerStep(ReadPreviewSourceVersion{}):    {failurePreviewSource, failurePreviewSource},
	providerStep(PreparePreviewSource{}):        {failurePreviewSource, failurePreviewSource},
	providerStep(PreparePreviewConfiguration{}): {failurePreviewConfiguration, failurePreviewConfiguration},
	providerStep(EnsurePreviewEnvironment{}):    {failurePreviewEnvironment, failurePreviewEnvironmentDeadline},
	providerStep(StartPreview{}):                {failurePreviewStartUnavailable, failurePreviewStartDeadline},
	providerStep(ProbeAndExposePreview{}):       {failurePreviewRoute, failurePreviewRouteDeadline},
	// Whether the last attempt removed anything is unknown.
	providerStep(StopPreview{}): {failurePreviewCleanupOutcomeUnknown, failurePreviewCleanupDeadline},
}

// executeFailureCode is the failure code of the provider Step whose Execute
// failure moved the Preview to RecordPreviewFailure: its timeout code when
// the engine ended it at its wait limit, else its exhausted code. Only
// provider Steps route their failure to RecordPreviewFailure, so another
// predecessor is a programming error.
func executeFailureCode(ctx dex.Context) failureCode {
	failures, found := providerFailures[dexflow.RecoveryStep(ctx)]
	if !found {
		panic("preview failure record has no provider Step predecessor: " + ctx.FromStepExecutionID())
	}
	if dexflow.RecoveryTimedOut(ctx) {
		return failures.timedOut
	}
	return failures.exhausted
}

// newFailure is the user-visible record of code in the phase of kind: its
// message, then its cause, truncated. The cause is an error or a redacted,
// truncated provider answer; no request or credential ever enters it.
func newFailure(code failureCode, kind CommandKind, reason string) *dexflow.Failure {
	return &dexflow.Failure{Code: code.String(), Phase: string(kind), Message: dexflow.FailureMessage(code.definition.message, reason), Recoverable: !code.definition.isTerminal, Remedy: code.definition.remedy}
}

// previewRefusal is a definite answer a Step got, which asking again cannot
// change: the input of RecordPreviewRefusal. The deciding Step sets Owner from
// the typed cause, never from the code: who must act on it, the application's
// source, the user's configuration or the platform. Gaps are the
// configuration gaps of an incompatible start.
type previewRefusal struct {
	Code  string                       `json:"code"`
	Cause string                       `json:"cause"`
	Owner conversation.DeliveryOwner   `json:"owner,omitzero"`
	Gaps  []appmanifest.RequirementGap `json:"gaps,omitempty"`
}

// refusal is a refusal the platform owns: any cause that is neither the
// application's source nor the user's configuration.
func refusal(code failureCode, cause error) previewRefusal {
	return previewRefusal{Code: code.String(), Cause: cause.Error(), Owner: conversation.DeliveryOwnerPlatform}
}

// applicationRefusal is a refusal of the application's own source: its build
// or start failed, its definitions or its dex-app.yaml are invalid.
func applicationRefusal(code failureCode, cause error) previewRefusal {
	refused := refusal(code, cause)
	refused.Owner = conversation.DeliveryOwnerApplication
	return refused
}

// configurationRefusal is a refusal the user's configuration owns.
func configurationRefusal(code failureCode, cause error) previewRefusal {
	refused := refusal(code, cause)
	refused.Owner = conversation.DeliveryOwnerConfiguration
	return refused
}

// incompatibleRefusal refuses a start whose configuration lacks what the
// source declares, naming every gap: the user's configuration owns it.
func incompatibleRefusal(gaps []appmanifest.RequirementGap) previewRefusal {
	cause := "Configure " + strings.Join(appmanifest.RequirementGapNames(gaps), ", ") + ", then start Preview"
	return previewRefusal{Code: failurePreviewIncompatible.String(), Cause: cause, Owner: conversation.DeliveryOwnerConfiguration, Gaps: gaps}
}

// sourceRefusal classifies a source preparation's failure to prepare its
// definitions: a rejected source or definitions that do not match it belong to
// the application. Any other error has no definite answer and is asked again.
func sourceRefusal(err error) (previewRefusal, bool) {
	if errors.Is(err, workspace.ErrPreviewDefinitionsRejected) || errors.Is(err, ErrDefinitionMismatch) || errors.Is(err, projectconfig.ErrInvalidDefinitions) {
		return applicationRefusal(failurePreviewFDGInvalid, err), true
	}
	return previewRefusal{}, false
}

// manifestRefusal classifies a failed manifest registration of a pushed
// commit: its rejected content belongs to the application, a missing source
// and any other registration failure to the platform.
func manifestRefusal(err error) previewRefusal {
	if errors.Is(err, appmanifest.ErrInvalidAppManifest) {
		return applicationRefusal(failurePreviewManifest, err)
	}
	return refusal(manifestFailureCode(err), err)
}

// previewCleanupRefusalCode classifies a definite refusal of Preview cleanup by
// its typed cause only. Every other error, an unknown outcome included, is
// asked again and has no code.
func previewCleanupRefusalCode(err error) failureCode {
	switch {
	case errors.Is(err, infra.ErrOwnershipConflict):
		return failurePreviewCleanupOwnershipConflict
	case platform.IsHTTPAccessDenied(err):
		// Only provider HTTP responses: a gRPC denial from the sandboxd
		// transport is asked again.
		return failurePreviewCleanupPermissionDenied
	}
	return noFailure
}

// environmentRefusalCode is the failure code of the Preview supervisor's
// definite refusal to run the engine, by its answer, or noFailure for any other
// answer.
func environmentRefusalCode(state string) failureCode {
	switch state {
	case workspace.PreviewStateFull:
		return failurePreviewStateFull
	case workspace.PreviewStateInconsistent:
		return failurePreviewStateInconsistent
	case workspace.PreviewStateFailed:
		return failurePreviewEnvironment
	}
	return noFailure
}

// manifestFailureCode classifies a failed manifest registration of a pushed
// commit. A published commit can still carry an invalid manifest, or the
// registration can fail; only the host's typed missing source, or a commit
// main does not reach, asks for a push.
func manifestFailureCode(err error) failureCode {
	if errors.Is(err, repohost.ErrNotFound) || errors.Is(err, repohost.ErrCommitUnreachable) {
		return failurePreviewHeadNotReadable
	}
	return failurePreviewManifest
}
