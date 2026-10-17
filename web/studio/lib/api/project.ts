// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import { delay } from "@/lib/shared/async";
import { required } from "@/lib/shared/protobuf";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";
import { ApiError, newRequestId } from "./transport/api-error";
import { unary } from "./transport/unary";
import { readUnary } from "./transport/read-unary";

// Browser client of ProjectService: Projects, their configuration and their Live pointer.

/** The configuration of the Project's Preview, or the Live configuration of one Release. */
export type ConfigurationTarget =
  | { projectId: string; scope: "PROJECT_SCOPE_PREVIEW" }
  | { projectId: string; scope: "PROJECT_SCOPE_LIVE"; releaseId: string }
  // Live's next-publish layer, checked against a Release's source, or the Preview's with an empty releaseId.
  | { projectId: string; scope: "PROJECT_SCOPE_LIVE"; releaseId: string; nextPublish: true };

/**
 * Starts a Project and returns its ID. A 503 is retried up to four times: the
 * request ID makes every attempt the same request.
 */
export async function createProject(input: RpcInput<"CreateProject">, signal: AbortSignal): Promise<string> {
  for (let attempt = 0; ; attempt++) {
    try {
      return (await unary("CreateProject", input, { signal })).projectId;
    } catch (error) {
      if (!(error instanceof ApiError) || error.status !== 503 || attempt === 4 || signal.aborted) throw error;
      await delay(1000, signal);
      signal.throwIfAborted();
    }
  }
}

/** The Project with the caller's role; a caller who is not its member gets a 403 ApiError. */
export function getProject(input: RpcInput<"GetProject">, signal: AbortSignal): Promise<RpcOutput<"GetProject">> {
  return readUnary("GetProject", input, { signal });
}

export function listProjects(input: RpcInput<"ListProjects">, signal: AbortSignal): Promise<RpcOutput<"ListProjects">> {
  return readUnary("ListProjects", input, { signal });
}

/** One page of an environment's history, newest first; Preview needs the builder role and Live the operator role. */
export function listEnvironmentEvents(
  input: RpcInput<"ListEnvironmentEvents">,
  signal: AbortSignal,
): Promise<RpcOutput<"ListEnvironmentEvents">> {
  return readUnary("ListEnvironmentEvents", input, { signal });
}

export async function deleteProject(
  input: Required<RpcInput<"DeleteProject">>,
  signal: AbortSignal,
): Promise<ProjectSnapshot__Output> {
  return required((await unary("DeleteProject", input, { signal })).project, "deleted Project");
}

export async function getProjectConfiguration(
  input: { target: ConfigurationTarget },
  signal: AbortSignal,
): Promise<ProjectConfigurationSnapshot__Output> {
  return required((await readUnary("GetProjectConfiguration", input, { signal })).configuration, "configuration");
}

/** Ends Live serving at the Live pointer the page shows; a pointer that changed since is a 409. */
export async function removeLive(
  input: Omit<Required<RpcInput<"RemoveLive">>, "requestId">,
  signal: AbortSignal,
): Promise<void> {
  await unary("RemoveLive", { ...input, requestId: newRequestId() }, { signal });
}

/**
 * The Slack events the relay recently routed to the Project's Preview or Live, newest first, with
 * when the relay began recording. The list lives in the server's memory.
 */
export function listRecentTriggerEvents(
  input: { projectId: string; scope: "PROJECT_SCOPE_PREVIEW" | "PROJECT_SCOPE_LIVE" },
  signal: AbortSignal,
): Promise<RpcOutput<"ListRecentTriggerEvents">> {
  return readUnary("ListRecentTriggerEvents", input, { signal });
}
