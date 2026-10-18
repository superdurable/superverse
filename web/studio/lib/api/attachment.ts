// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { delay } from "@/lib/shared/async";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";
import { ApiError } from "./transport/api-error";
import { readUnary } from "./transport/read-unary";
import { unary } from "./transport/unary";

/*
 * Browser client of AttachmentService. The bytes never pass through it: CreateAttachmentUpload
 * answers with a presigned PUT that the browser sends to storage itself (transport/presigned-upload).
 */

/** Registration waits a bounded time for its Flow; an UNAVAILABLE answer is retried with the same request ID. */
const REGISTRATION_ATTEMPTS = 6;

/**
 * Declares one file of a TeamSpace and returns the attachment with, while it awaits its upload, the presigned
 * PUT. The request ID makes every attempt the same request: a replay answers the same attachment.
 */
export async function createAttachmentUpload(
  input: Required<RpcInput<"CreateAttachmentUpload">>,
  signal: AbortSignal,
): Promise<RpcOutput<"CreateAttachmentUpload">> {
  for (let attempt = 1; ; attempt++) {
    try {
      return await unary("CreateAttachmentUpload", input, { signal });
    } catch (error) {
      if (!(error instanceof ApiError) || error.status !== 503 || attempt === REGISTRATION_ATTEMPTS) throw error;
      await delay(1000 * attempt, signal);
      signal.throwIfAborted();
    }
  }
}

/**
 * Ends the upload wait of the caller's attachment and answers its status after at most 20 seconds: READY,
 * FAILED, or still VERIFYING or PREPARING, after which the caller asks again. Repeatable.
 */
export function completeAttachmentUpload(
  input: Required<RpcInput<"CompleteAttachmentUpload">>,
  signal: AbortSignal,
): Promise<RpcOutput<"CompleteAttachmentUpload">> {
  return unary("CompleteAttachmentUpload", input, { signal });
}

/** Deletes a staged attachment of the caller; the request ID makes a retry the same command. */
export function deleteAttachment(
  input: Required<RpcInput<"DeleteAttachment">>,
  signal: AbortSignal,
): Promise<RpcOutput<"DeleteAttachment">> {
  return unary("DeleteAttachment", input, { signal });
}

/**
 * Short-lived presigned GETs of at most 20 attachments the Project's conversation holds, inline for a
 * thumbnail or as a download with the file name; an attachment the Session has not shown yet has none.
 */
export function getAttachmentDownloads(
  input: Required<RpcInput<"GetAttachmentDownloads">>,
  signal: AbortSignal,
): Promise<RpcOutput<"GetAttachmentDownloads">> {
  return readUnary("GetAttachmentDownloads", input, { signal });
}
