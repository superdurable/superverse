// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package boundary

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The mappers below turn a domain, Flow-client or SDK error into the public
// status of an RPC. Provider and SDK errors can carry request details, so only
// fixed messages and safe business rejections reach the caller; a status that
// a dependency returned is never passed through. Each RPC maps an error once,
// where it returns it: request-input checks return public statuses directly,
// and Watch reads return their errors unmapped to the end of the Watch. An
// error no mapper classifies keeps its cause for the server log.

// errUnclassified is the public status of an error no mapper classifies.
var errUnclassified = status.Error(codes.Unavailable, "request could not be completed; retry or inspect server diagnostics")

// unclassifiedError carries the cause of an unclassified error to the
// outermost interceptor, which logs it and returns only errUnclassified.
type unclassifiedError struct{ cause error }

func (e *unclassifiedError) Error() string { return errUnclassified.Error() }

// GRPCStatus keeps the public status when status.Code or status.Convert reads
// the error before the interceptor strips it.
func (e *unclassifiedError) GRPCStatus() *status.Status { return status.Convert(errUnclassified) }

// RPCStatus is the mapper of every RPC without a more specific one below.
func RPCStatus(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, watch.ErrRecoveredPanic) {
		return watch.ErrRecoveredPanic
	}
	if errors.Is(err, watch.ErrInvalidCursor) {
		return status.Error(codes.InvalidArgument, "invalid resume cursor")
	}
	if errors.Is(err, watch.ErrSourcesChanged) {
		return status.Error(codes.Unavailable, "Watch sources changed; reconnect for a fresh snapshot")
	}
	if errors.Is(err, accounts.ErrInvalidCredential) {
		return status.Error(codes.Unauthenticated, "invalid or expired credentials")
	}
	if errors.Is(err, accounts.ErrAccountExists) {
		return status.Error(codes.AlreadyExists, "account already exists")
	}
	if errors.Is(err, accounts.ErrInvalidEmail) {
		return status.Error(codes.InvalidArgument, "email address is invalid")
	}
	if errors.Is(err, flowid.ErrInvalidIdentity) {
		return status.Error(codes.InvalidArgument, "resource identity is invalid")
	}
	if errors.Is(err, projects.ErrForbidden) {
		return status.Error(codes.PermissionDenied, "project access denied")
	}
	if errors.Is(err, apptemplate.ErrExampleNotOffered) {
		return withErrorInfo(codes.FailedPrecondition, "This example is not offered; pick another one", reasonExampleNotOffered)
	}
	var rejected *dexflow.Rejection
	if errors.As(err, &rejected) {
		if rejected.Stale {
			return revisionChanged(rejected.Message)
		}
		return status.Error(rejectionCode(rejected.Code), rejected.Message)
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "request cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	}
	var contention *dex.RPCLockConflictError
	if errors.As(err, &contention) {
		return status.Error(codes.Aborted, "resource admission is busy; retry the same request")
	}
	var alreadyStarted *dex.FlowAlreadyStartedError
	if errors.As(err, &alreadyStarted) {
		return status.Error(codes.AlreadyExists, "resource identity conflicts with an accepted request")
	}
	// A stopped Session is never reported as running or missing; the reason is
	// its fixed code, never engine text.
	if stopped, isStopped := errors.AsType[*sessionagent.SessionExecutionStoppedError](err); isStopped {
		return reasonStatus(codes.FailedPrecondition, "The Session's engine execution stopped ("+stopped.Reason.String()+"); its conversation stays readable", stopped.Reason.String(), dexflow.RemedyContactOperator)
	}
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) || errors.Is(err, postgres.ErrNotFound) {
		return status.Error(codes.NotFound, "resource not found")
	}
	var unavailable *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &unavailable) {
		return status.Error(codes.FailedPrecondition, "resource requires recovery")
	}
	return &unclassifiedError{cause: err}
}

// ReleaseReadStatus distinguishes an unavailable read from the Release's
// durable build outcome. Only GetRelease uses this policy; a timed-out mutation
// still has its ordinary status and uncertain-effect reconciliation.
func ReleaseReadStatus(err error) error {
	if errors.Is(err, context.Canceled) {
		return RPCStatus(err)
	}
	timedOut := errors.Is(err, context.DeadlineExceeded)
	if service, ok := errors.AsType[*dex.ServiceError](err); ok {
		timedOut = timedOut || service.Code == codes.DeadlineExceeded
	}
	if invocation, ok := errors.AsType[*dex.WorkerInvocationError](err); ok && invocation.Worker != nil {
		timedOut = timedOut || invocation.Worker.Code == codes.DeadlineExceeded
	}
	if timedOut {
		return status.Error(codes.Unavailable, "Release state read timed out; retry the read")
	}
	return RPCStatus(err)
}

// FlowNotFound reports a read of a Flow that does not exist.
func FlowNotFound(err error) bool {
	var missing *dex.FlowNotFoundError
	return errors.As(err, &missing)
}

// loggedDetailBytes bounds the detail of a Dex error in a log line.
const loggedDetailBytes = 512

// DexErrorAttrs describes the Dex error of a failed read as log attributes, for
// the investigation of a resource that cannot be read: the service code and
// sub-status, for a Worker invocation the Worker's own status and error type,
// and the detail, which names what the Worker or Dex could not find, such as a
// blob key. A Dex error carries no credentials; the detail is bounded
// all the same. An error that is no Dex answer is described by its type.
func DexErrorAttrs(err error) []any {
	service, ok := errors.AsType[*dex.ServiceError](err)
	if !ok {
		return []any{"error_type", fmt.Sprintf("%T", err)}
	}
	attrs := []any{"dex_code", service.Code.String(), "dex_sub_status", subStatusName(service.SubStatus)}
	if invocation, isWorker := errors.AsType[*dex.WorkerInvocationError](err); isWorker && invocation.Worker != nil {
		attrs = append(attrs, "worker_status", invocation.Worker.Code.String())
		if invocation.Worker.Type != "" {
			attrs = append(attrs, "worker_error_type", invocation.Worker.Type)
		}
	}
	return append(attrs, "detail", comm.TruncateUTF8(service.Detail, loggedDetailBytes))
}

// subStatusName names the sub-status that Dex classified an error with.
func subStatusName(subStatus dex.ErrorSubStatus) string {
	switch subStatus {
	case dex.ErrorSubStatusUncategorized:
		return "uncategorized"
	case dex.ErrorSubStatusFlowAlreadyStarted:
		return "flow_already_started"
	case dex.ErrorSubStatusFlowNotFound:
		return "flow_not_found"
	case dex.ErrorSubStatusWorkerAPI:
		return "worker_api"
	case dex.ErrorSubStatusLongPollTimeout:
		return "long_poll_timeout"
	case dex.ErrorSubStatusChannelMessageNotFound:
		return "channel_message_not_found"
	case dex.ErrorSubStatusRequestTimeout:
		return "request_timeout"
	default:
		return "unknown_" + strconv.Itoa(int(subStatus))
	}
}

// rejectionCode is the status code a Flow's business rejection names.
func rejectionCode(name string) codes.Code {
	switch name {
	case dexflow.RejectionResourceExhausted.String():
		return codes.ResourceExhausted
	case dexflow.RejectionNotFound.String():
		return codes.NotFound
	case dexflow.RejectionPermissionDenied.String():
		return codes.PermissionDenied
	case dexflow.RejectionAborted.String():
		return codes.Aborted
	case dexflow.RejectionInvalidArgument.String():
		return codes.InvalidArgument
	case dexflow.RejectionAlreadyExists.String():
		return codes.AlreadyExists
	default:
		return codes.FailedPrecondition
	}
}

// SessionRPCStatus maps the errors of Session and Preview operations.
func SessionRPCStatus(err error) error {
	if errors.Is(err, preview.ErrConflict) {
		return revisionChanged("resource generation or revision changed")
	}
	if errors.Is(err, preview.ErrDefinitionMismatch) {
		return withErrorInfo(codes.FailedPrecondition, "Preview definitions do not match their source; prepare the Preview source again", reasonPreviewSourceNotPrepared)
	}
	if reason, unavailable := sandboxUnavailable(err); unavailable {
		return withErrorInfo(codes.FailedPrecondition, reason, reasonPreviewUnavailable)
	}
	if errors.Is(err, agents.ErrSessionNotActive) {
		return status.Error(codes.FailedPrecondition, agents.ErrSessionNotActive.Error())
	}
	return RPCStatus(err)
}

// sandboxUnavailable classifies typed Preview answers that ask the caller to
// retry later; none of them reports a failed or unknown effect. It returns the
// fixed text of the matched answer and never the wrapped SDK detail.
func sandboxUnavailable(err error) (string, bool) {
	for _, known := range []error{preview.ErrBusy, preview.ErrNotReady, preview.ErrClosed} {
		if errors.Is(err, known) {
			return known.Error(), true
		}
	}
	return "", false
}

// ConfigurationRPCStatus maps the errors of project configuration operations.
func ConfigurationRPCStatus(err error) error {
	if errors.Is(err, projectconfig.ErrInvalid) {
		return withErrorInfo(codes.InvalidArgument, "configuration field or target is invalid", reasonConfigurationInvalid)
	}
	if errors.Is(err, projectconfig.ErrNotReady) {
		return withErrorInfo(codes.FailedPrecondition, "configuration needs review or authorization", reasonConfigurationNotReady)
	}
	if errors.Is(err, projectconfig.ErrConflict) {
		return revisionChanged("project configuration revision changed")
	}
	if errors.Is(err, infra.ErrAccessDenied) {
		return status.Error(codes.PermissionDenied, "project configuration scope access denied")
	}
	// The Preview configuration is the one of the source the Preview prepared
	// for the Session's HEAD; only preparing that source makes it readable.
	if errors.Is(err, preview.ErrSourceNotPrepared) {
		return withErrorInfo(codes.FailedPrecondition, "Prepare the current source", reasonPreviewSourceNotPrepared)
	}
	if errors.Is(err, preview.ErrDefinitionMismatch) {
		return withErrorInfo(codes.FailedPrecondition, "Preview definitions do not match their source; prepare the Preview source again", reasonPreviewSourceNotPrepared)
	}
	return RPCStatus(err)
}

// ReleaseBuildRPCStatus maps the errors of a build admission: its refusals
// carry their declared reasons, and a draft refused by the configuration
// owner maps as a configuration error.
func ReleaseBuildRPCStatus(err error) error {
	switch {
	case errors.Is(err, releases.ErrPreviewValidationRequired):
		return withErrorInfo(codes.FailedPrecondition, releases.ErrPreviewValidationRequired.Error(), reasonPreviewValidationRequired)
	case errors.Is(err, releases.ErrConfigurationStale):
		return withErrorInfo(codes.FailedPrecondition, releases.ErrConfigurationStale.Error(), reasonReleaseConfigurationStale)
	}
	return ConfigurationRPCStatus(err)
}

// CancellationRPCStatus maps the refusals of a person's cancellation of a
// build or a deployment to their reasons; any other error maps like one of a
// Release build.
func CancellationRPCStatus(err error) error {
	if deploying, isDeploying := errors.AsType[*releases.BuildDeployingError](err); isDeploying {
		return withDeploymentErrorInfo(codes.FailedPrecondition, deploying.Error(), reasonReleaseBuildDeploying, deploying.DeploymentID())
	}
	switch {
	case errors.Is(err, releases.ErrBuildNotCancellable):
		return withErrorInfo(codes.FailedPrecondition, releases.ErrBuildNotCancellable.Error(), reasonReleaseBuildNotCancellable)
	case errors.Is(err, deployments.ErrDeploymentNotCancellable):
		return withErrorInfo(codes.FailedPrecondition, deployments.ErrDeploymentNotCancellable.Error(), reasonDeploymentNotCancellable)
	case errors.Is(err, build.ErrInvalidLogCursor):
		return status.Error(codes.InvalidArgument, "invalid build log cursor")
	}
	return ReleaseBuildRPCStatus(err)
}

// AgentRPCStatus maps the errors of Agent commands and reads. Any other error
// comes from the Session and maps like one.
func AgentRPCStatus(err error) error {
	var archived *conversation.ArchivedMessagesNotFoundError
	if errors.As(err, &archived) {
		return status.Error(codes.NotFound, "archived messages are no longer retained")
	}
	for _, changed := range []error{sessionagent.ErrModelRevisionChanged, sessionagent.ErrFlowDesignRevisionChanged} {
		if errors.Is(err, changed) {
			return revisionChanged(changed.Error())
		}
	}
	if errors.Is(err, sessionagent.ErrFlowDesignNotFound) {
		return status.Error(codes.NotFound, sessionagent.ErrFlowDesignNotFound.Error())
	}
	var channel *dex.ChannelMessageNotFoundError
	var started *dex.FlowAlreadyStartedError
	if errors.Is(err, sessionagent.ErrCommandRejected) || errors.As(err, &channel) || errors.As(err, &started) {
		return status.Error(codes.FailedPrecondition, "command conflicts with current session state")
	}
	return SessionRPCStatus(err)
}

// NotAdmitted marks the public status of an Agent command that was not
// admitted: a failure before dispatch or an authoritative false command
// receipt. An uncertain transport outcome is never inferred from status text.
func NotAdmitted(public error) error {
	failure := status.Convert(public)
	return withErrorInfo(failure.Code(), failure.Message(), reasonCommandNotAdmitted)
}

// revisionChanged is the ABORTED status of a stale revision fence.
func revisionChanged(message string) error {
	return withErrorInfo(codes.Aborted, message, reasonRevisionChanged)
}
