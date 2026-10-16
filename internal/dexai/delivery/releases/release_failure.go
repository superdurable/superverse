// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"errors"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// releaseFailureKind is one kind of failure a Release records: its persisted
// code, who must act on it and what the user can do about it. The kinds are
// declared once below; a code that two owners share is two kinds.
type releaseFailureKind struct {
	code   string
	owner  conversation.DeliveryOwner
	remedy dexflow.FailureRemedy
}

var (
	// failureSourcePlatform is a source preparation the platform could not
	// finish.
	failureSourcePlatform = releaseFailureKind{"RELEASE_SOURCE_FAILED", conversation.DeliveryOwnerPlatform, dexflow.RemedyPublishAgain}
	// failureSourceInvalid is a commit whose source the preparation rejects.
	failureSourceInvalid = releaseFailureKind{"RELEASE_SOURCE_FAILED", conversation.DeliveryOwnerApplication, dexflow.RemedyAskAgent}
	// failureBuildFailed is a build the provider reported FAILED.
	failureBuildFailed = releaseFailureKind{"BUILD_FAILED", conversation.DeliveryOwnerApplication, dexflow.RemedyAskAgent}
	// failureBuildUnknown is a build whose outcome the provider cannot confirm.
	failureBuildUnknown = releaseFailureKind{"BUILD_UNKNOWN", conversation.DeliveryOwnerPlatform, dexflow.RemedyContactOperator}
	// failureBuildIncomplete is a draft the freeze refused with its gaps.
	failureBuildIncomplete = releaseFailureKind{"RELEASE_BUILD_FAILED", conversation.DeliveryOwnerConfiguration, dexflow.RemedyOpenSetup}
	// failureBuildInterrupted is a build phase that ended without a result.
	failureBuildInterrupted = releaseFailureKind{"RELEASE_BUILD_FAILED", conversation.DeliveryOwnerPlatform, dexflow.RemedyPublishAgain}
	// failureCancelUnconfirmed is a cancelled build whose provider did not
	// confirm that it stopped.
	failureCancelUnconfirmed = releaseFailureKind{"BUILD_CANCEL_UNCONFIRMED", conversation.DeliveryOwnerPlatform, dexflow.RemedyContactOperator}
	// failureCleanup is a Release cleanup that did not finish.
	failureCleanup = releaseFailureKind{"RELEASE_CLEANUP_FAILED", conversation.DeliveryOwnerPlatform, dexflow.RemedyContactOperator}
)

// failure is the record of kind in phase, which an explicit retry recovers.
func (kind releaseFailureKind) failure(phase, message string) *dexflow.Failure {
	return &dexflow.Failure{Code: kind.code, Phase: phase, Message: message, Recoverable: true, Remedy: kind.remedy}
}

// rejectedAutoDeployFailure is the failure of an automatic deployment that the
// deployment refused with rejection: the build stays READY, the platform owns
// it, and publishing again retries it.
func rejectedAutoDeployFailure(rejection *dexflow.Rejection) *dexflow.Failure {
	message := "Automatic deployment was rejected: " + rejection.Message + "; the built revision remains available."
	return &dexflow.Failure{Code: rejection.Code, Phase: phaseDeploy, Message: message, Remedy: dexflow.RemedyPublishAgain}
}

// The admission refusals of a new build, which the API answers with their
// declared ErrorInfo reasons. Gaps are not among them: the freeze refuses a
// draft with gaps and records them itemized.
var (
	// ErrPreviewValidationRequired refuses a build of a commit that no READY
	// Preview start ran, without a reason to skip it, that Live does not run.
	ErrPreviewValidationRequired = errors.New("run this version on Preview first, or give a reason to publish it without Preview")
	// ErrConfigurationStale refuses a version's frozen copy built before Live,
	// Preview's applied version or Live's next-publish layer changed.
	ErrConfigurationStale = errors.New("the configuration copy was built before Live, Preview's applied version or Live's next publish changed; build it again")
	// ErrBuildNotCancellable refuses the cancellation of a build that is not
	// the Release's active one or already ended.
	ErrBuildNotCancellable = errors.New("the build is not in progress, so there is nothing to cancel")
)

// BuildDeployingError refuses the cancellation of a build whose artifacts
// committed, so its automatic deployment, which the message names, is or may
// be admitted: that deployment is cancelled instead.
type BuildDeployingError struct{ deploymentID string }

func (e *BuildDeployingError) Error() string {
	return "the build is published and starts its deployment " + e.deploymentID + "; cancel that deployment instead"
}

// DeploymentID is the deployment to cancel instead.
func (e *BuildDeployingError) DeploymentID() string { return e.deploymentID }
