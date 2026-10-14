// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { PreviewSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewSnapshot";
import type { RpcInput } from "@/lib/shared/rpc-methods";
import { errorMessage, newRequestId, type PendingCommand } from "./transport/api-error";
import { unary } from "./transport/unary";

/*
 * Browser client of PreviewService: the Project's one Preview. Each command is
 * fenced to the Preview revision and Sandbox instance it was decided against,
 * and the page's Session Watch reports its effect.
 */

/** The input of a fenced Preview command: start, stop and source preparation take the same fields. */
export type PreviewCommandInput = Required<RpcInput<"StopPreview">>;

/** Sent only by an applying start: the head revision the page applies. */
export interface ApplyOptions {
  applyConfiguration: true;
  configurationRevision: string;
}

/**
 * A new command with its own request ID, decided against `preview`: the server
 * rejects it for any other Preview revision or Sandbox instance. No Preview yet
 * is the first-demand form, valid only before the first instance.
 */
export function newPreviewCommand(
  projectId: string,
  preview: PreviewSnapshot__Output | null | undefined,
): PendingCommand<PreviewCommandInput> {
  return {
    input: {
      projectId,
      requestId: newRequestId(),
      expectedRevision: preview?.revision ?? "0",
      generation: preview?.generation ?? "0",
      workspaceRequestId: preview?.workspaceRequestId ?? "",
    },
    uncertain: false,
  };
}

export async function startPreview(
  fence: PreviewCommandInput,
  options: ApplyOptions | undefined,
  signal: AbortSignal,
): Promise<void> {
  await unary("StartPreview", { ...fence, ...options }, { signal });
}

export async function stopPreview(input: Required<RpcInput<"StopPreview">>, signal: AbortSignal): Promise<void> {
  await unary("StopPreview", input, { signal });
}

/** Turns the Preview's deployment of each commit the Agent pushes on or off. */
export async function updatePreviewAutoDeploy(
  projectId: string,
  isEnabled: boolean,
  signal: AbortSignal,
): Promise<void> {
  await unary("UpdatePreviewAutoDeploy", { projectId, isEnabled }, { signal });
}

export async function preparePreviewSource(
  input: Required<RpcInput<"PreparePreviewSource">>,
  signal: AbortSignal,
): Promise<void> {
  await unary("PreparePreviewSource", input, { signal });
}

/** The Flow Definitions of the source prepared for the Session's head; refused while the head is unprepared. */
export function getPreviewDefinitions(input: Required<RpcInput<"GetPreviewDefinitions">>, signal: AbortSignal) {
  return unary("GetPreviewDefinitions", input, { signal });
}

/**
 * After a definite rejection, which may come from a stale revision, re-reads
 * the Session so the next explicit click decides on current state; nothing is
 * resubmitted. Always throws the rejection, or a refresh hint if the read fails.
 */
export async function refreshAfterRejection(
  refresh: (signal: AbortSignal, propagateFailure: boolean) => Promise<void>,
  signal: AbortSignal,
  reason: unknown,
  fallback: string,
): Promise<never> {
  await refresh(signal, true).catch(() => {
    throw new Error(`${errorMessage(reason, fallback)} Refresh the Session before trying again.`);
  });
  throw reason;
}
