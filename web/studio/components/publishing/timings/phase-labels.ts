// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Display labels of the phase codes the platform records. Phase is an open set, so a code that is not listed here is
 * shown as the server sent it.
 */

/** The phases of a Release build revision and of its source preparation: the builder's, then the Flow's own. */
export const RELEASE_PHASE_LABELS: Readonly<Record<string, string>> = {
  QUEUE: "Queue",
  HOST_PREPARATION: "Host preparation",
  DEPENDENCY_PREPARATION: "Dependency preparation",
  ANALYSIS: "Analysis",
  COMPILATION: "Compilation",
  IMAGE_PUBLICATION: "Image publication",
  CONFIGURATION_FREEZE: "Configuration freeze",
  ARTIFACT_PUBLICATION: "Artifact publication",
};

/** The phases of a deployment: the Steps of ProjectDeployFlow, including the two parallel preparation branches. */
export const DEPLOYMENT_PHASE_LABELS: Readonly<Record<string, string>> = {
  AdmitDeployment: "Admit deployment",
  AcquireProjectDeployment: "Acquire deployment lease",
  VerifyReleaseAndConfiguration: "Verify release and configuration",
  PublishDefinitions: "Publish Flow definitions",
  PrepareLiveExecution: "Prepare Live execution",
  PrepareLiveTemporal: "Prepare Live engine",
  PrepareLiveAccess: "Prepare Live access",
  StartLiveDex: "Start Live Dex",
  ApplyApplication: "Apply application",
  VerifyReadiness: "Verify readiness",
  CommitCurrentLive: "Commit current Live",
  RequestReplacedRetirement: "Retire the replaced Live",
  ReleaseProjectDeployment: "Release deployment lease",
  CleanupCancelledDeployment: "Clean up cancelled deployment",
  RetireReplacedGeneration: "Retire after its replacement",
};
