// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";
import type { ReleaseCandidate__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseCandidate";
import { required } from "@/lib/shared/protobuf";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";
import { unary } from "./transport/unary";
import { readUnary } from "./transport/read-unary";

// Browser client of ReleaseService. A Release ID is the full lowercase commit SHA of its source.

export async function listReleaseCandidates(
  input: Required<RpcInput<"ListReleaseCandidates">>,
  signal: AbortSignal,
): Promise<ReleaseCandidate__Output[]> {
  return (await readUnary("ListReleaseCandidates", input, { signal })).candidates;
}

export function listReleases(input: RpcInput<"ListReleases">, signal: AbortSignal): Promise<RpcOutput<"ListReleases">> {
  return readUnary("ListReleases", input, { signal });
}

/** The Release and, once its source is prepared, its immutable Flow Definitions. */
export function getRelease(
  input: Required<RpcInput<"GetRelease">>,
  signal: AbortSignal,
): Promise<RpcOutput<"GetRelease">> {
  return readUnary("GetRelease", input, { signal });
}

/** Prepares the Release of one main-branch commit and returns its ID. */
export async function prepareRelease(
  input: Required<RpcInput<"PrepareRelease">>,
  signal: AbortSignal,
): Promise<string> {
  return (await unary("PrepareRelease", input, { signal })).releaseId;
}

/** The build command: empty `previewSkipReason` when a Preview start validated the version or Live runs its code. */
export type ReleaseBuildInput = Required<RpcInput<"StartReleaseBuild">>;

/** Freezes the configuration, at the revisions the page observed, into a new build revision. */
export async function startReleaseBuild(input: ReleaseBuildInput, signal: AbortSignal): Promise<void> {
  await unary("StartReleaseBuild", input, { signal });
}

/**
 * Builds a version's frozen copy from Live's next-publish layer, which its build freezes: refused when the
 * layer revision (`expectedRevision`, "0" while there is none) or Preview's applied revision the page showed
 * moved. The answer is the version's configuration, whose `revision` is the copy's.
 */
export async function deriveLiveConfiguration(
  input: Required<RpcInput<"DeriveLiveConfiguration">>,
  signal: AbortSignal,
): Promise<ProjectConfigurationSnapshot__Output> {
  return required((await unary("DeriveLiveConfiguration", input, { signal })).configuration, "configuration");
}

/**
 * Asks to cancel a build until its artifacts commit; the same request ID is the same cancellation. A build
 * whose automatic deployment may be admitted is refused with RELEASE_BUILD_DEPLOYING, whose ErrorInfo names
 * that deployment in `deploymentId`.
 */
export async function cancelReleaseBuild(
  input: Required<RpcInput<"CancelReleaseBuild">>,
  signal: AbortSignal,
): Promise<void> {
  await unary("CancelReleaseBuild", input, { signal });
}

/**
 * One page of a build's redacted log from `cursor` (empty for the start): whole lines, the cursor after
 * them, whether the log is complete, and the earliest time to read again.
 */
export function getReleaseBuildLog(
  input: Required<RpcInput<"GetReleaseBuildLog">>,
  signal: AbortSignal,
): Promise<RpcOutput<"GetReleaseBuildLog">> {
  return readUnary("GetReleaseBuildLog", input, { signal });
}
