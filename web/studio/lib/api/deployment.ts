// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { DeploymentSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/DeploymentSnapshot";
import type { RpcInput } from "@/lib/shared/rpc-methods";
import { unary } from "./transport/unary";

// Browser client of DeploymentService. A Deployment always serves Live; ProjectService.RemoveLive ends it.

/**
 * Starts a Live deployment of a READY build, fenced on the Live pointer
 * revision the page observed, and returns its ID. The request ID doubles as
 * the new deployment's ID.
 */
export async function startDeployment(
  input: Omit<Required<RpcInput<"StartDeployment">>, "deploymentId">,
  signal: AbortSignal,
): Promise<string> {
  return (await unary("StartDeployment", { ...input, deploymentId: input.requestId }, { signal })).deploymentId;
}

/** A deployment's snapshot, such as when the deployment Live serves was created. */
export async function getDeployment(
  input: Required<RpcInput<"GetDeployment">>,
  signal: AbortSignal,
): Promise<DeploymentSnapshot__Output | null> {
  return (await unary("GetDeployment", input, { signal })).deployment;
}

/** A page of the Project's deployments, newest first. */
export function listDeployments(input: RpcInput<"ListDeployments">, signal: AbortSignal) {
  return unary("ListDeployments", input, { signal });
}

/**
 * Asks to cancel a deployment before it commits the new Live; the running Live is untouched and the same
 * request ID is the same cancellation. Too late is refused with DEPLOYMENT_NOT_CANCELLABLE.
 */
export async function cancelDeployment(
  input: Required<RpcInput<"CancelDeployment">>,
  signal: AbortSignal,
): Promise<void> {
  await unary("CancelDeployment", input, { signal });
}
