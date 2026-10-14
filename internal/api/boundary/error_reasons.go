// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package boundary

import (
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// errorReason is an ErrorInfo reason Studio branches on, with the remedy it
// declares, which the ErrorInfo carries in its metadata key "remedy" as the
// FailureRemedy enum value name. Studio never chooses a remedy from a reason.
type errorReason struct {
	name   string
	remedy dexflow.FailureRemedy
}

// The reasons the business API answers with, besides the connection
// refusals and the stopped Session, which declare theirs with their sets.
var (
	// reasonRevisionChanged is every revision fence that no longer holds,
	// which Studio answers by rereading.
	reasonRevisionChanged = errorReason{"REVISION_CHANGED", dexflow.RemedyApplyAgain}
	// reasonCommandNotAdmitted is an Agent command that was not admitted.
	reasonCommandNotAdmitted = errorReason{"AGENT_COMMAND_NOT_ADMITTED", dexflow.RemedyApplyAgain}
	// reasonDeploymentInProgress is a release of unused connections while a
	// deployment runs.
	reasonDeploymentInProgress = errorReason{"DEPLOYMENT_IN_PROGRESS", dexflow.RemedyApplyAgain}
	// reasonPreviewUnavailable is a Preview answer that asks to retry later:
	// busy, not ready or closed.
	reasonPreviewUnavailable = errorReason{"PREVIEW_UNAVAILABLE", dexflow.RemedyApplyAgain}
	// reasonPreviewSourceNotPrepared is a Preview operation that needs the
	// current source prepared, or prepared again for its definitions.
	reasonPreviewSourceNotPrepared = errorReason{"PREVIEW_SOURCE_NOT_PREPARED", dexflow.RemedyApplyAgain}
	// reasonConfigurationNotReady is a configuration that needs review or
	// authorization before the operation.
	reasonConfigurationNotReady = errorReason{"CONFIGURATION_NOT_READY", dexflow.RemedyOpenSetup}
	// reasonConfigurationInvalid is a configuration field or target that is
	// invalid.
	reasonConfigurationInvalid = errorReason{"CONFIGURATION_INVALID", dexflow.RemedyOpenSetup}
)

// The admission refusals of a Release build (STUDIOSETUP PUBGATE), declared
// here with their remedies so that every reason has one owner; the Release
// admission maps its refusals onto them.
var (
	// reasonPreviewValidationRequired refuses a build of a commit no Preview
	// start validated, without a reason to skip it.
	reasonPreviewValidationRequired = errorReason{"PREVIEW_VALIDATION_REQUIRED", dexflow.RemedyApplyAgain}
	// reasonReleaseConfigurationStale refuses a version's frozen copy built
	// before Live, Preview's applied version or Live's next publish changed.
	reasonReleaseConfigurationStale = errorReason{"RELEASE_CONFIGURATION_STALE", dexflow.RemedyOpenSetup}
)

// The refusals of a person's cancellation of a build or a deployment
// (decision 0027), declared here with their remedies.
var (
	// reasonReleaseBuildNotCancellable refuses a build that is not the
	// Release's active one or already ended.
	reasonReleaseBuildNotCancellable = errorReason{"RELEASE_BUILD_NOT_CANCELLABLE", dexflow.RemedyApplyAgain}
	// reasonReleaseBuildDeploying refuses a build whose artifacts committed:
	// its automatic deployment, which the message names, is cancelled instead.
	reasonReleaseBuildDeploying = errorReason{"RELEASE_BUILD_DEPLOYING", dexflow.RemedyCancelDeployment}
	// reasonDeploymentNotCancellable refuses a deployment that is live or
	// going live, retiring or ended.
	reasonDeploymentNotCancellable = errorReason{"DEPLOYMENT_NOT_CANCELLABLE", dexflow.RemedyApplyAgain}
)

// ReasonRevisionChangedForTest is the name of reasonRevisionChanged for the
// tests of the services.
var ReasonRevisionChangedForTest = reasonRevisionChanged.name

// remedyMetadataKey is the ErrorInfo metadata key of the remedy.
const remedyMetadataKey = "remedy"

// RemedyMetadataKeyForTest is remedyMetadataKey for the tests of the services.
const RemedyMetadataKeyForTest = remedyMetadataKey

// deploymentMetadataKey is the ErrorInfo metadata key of the deployment that
// RELEASE_BUILD_DEPLOYING names, the one its remedy cancels.
const deploymentMetadataKey = "deploymentId"

// withErrorInfo is a status whose ErrorInfo reason Studio branches on, with
// the remedy the reason declares.
func withErrorInfo(code codes.Code, message string, reason errorReason) error {
	return reasonStatus(code, message, reason.name, reason.remedy)
}

// reasonStatus is withErrorInfo for a reason of another closed set.
func reasonStatus(code codes.Code, message, reason string, remedy dexflow.FailureRemedy) error {
	return errorInfoStatus(code, message, reason, map[string]string{remedyMetadataKey: Enum[pb.FailureRemedy](remedy.String()).String()})
}

// withDeploymentErrorInfo is withErrorInfo naming the deployment that the
// reason's remedy acts on.
func withDeploymentErrorInfo(code codes.Code, message string, reason errorReason, deploymentID string) error {
	metadata := map[string]string{remedyMetadataKey: Enum[pb.FailureRemedy](reason.remedy.String()).String(), deploymentMetadataKey: deploymentID}
	return errorInfoStatus(code, message, reason.name, metadata)
}

func errorInfoStatus(code codes.Code, message, reason string, metadata map[string]string) error {
	return comm.Must(status.New(code, message).WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: "superverse.v2", Metadata: metadata})).Err()
}
