// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { completeAttachmentUpload, createAttachmentUpload, deleteAttachment } from "@/lib/api/attachment";
import { ApiError, newRequestId } from "@/lib/api/transport/api-error";
import { PresignedUploadError, putPresigned, UPLOAD_ALREADY_STORED } from "@/lib/api/transport/presigned-upload";
import { delay, isAbortError } from "@/lib/shared/async";
import { fitsKindLimit, LOCAL_REFUSALS, type AttachmentFormat } from "./attachment-files";
import type { ChipPatch } from "./attachment-chips";
import { attachmentStateView } from "./attachment-view";
import { ImageNotUsable, normalizeImage, sha256Hex } from "./file-preparation";

/*
 * One file's way from the browser to READY: normalize an image, hash the bytes, declare the file
 * (CreateAttachmentUpload), PUT the bytes to storage with the signed headers, then ask
 * CompleteAttachmentUpload, whose bounded wait answers READY, FAILED or "still working", until the file is
 * settled. Each step reports its chip's patch; an abort ends the work silently.
 */

/** Each CompleteAttachmentUpload call waits up to 20 seconds; together they wait about three minutes. */
const SETTLE_ROUNDS = 9;
/** An upload storage holds may take a moment to show; the Flow is asked again this often first. */
const MISSING_UPLOAD_ROUNDS = 3;

const NOT_FINISHED = "The upload did not finish. Remove the file and attach it again.";
const TOO_SLOW = "The file is taking too long to read. Remove it and attach it again.";
const NOT_ATTACHED = "The file could not be attached. Remove it and try again.";
/** HTTP 403 from storage: the presigned URL expired or its signature no longer holds. */
const URL_REFUSED = 403;

type Report = (patch: ChipPatch) => void;

/** The chip's failure for an error of a request: the server's reason for a refusal, else a plain sentence. */
function failureOf(reason: unknown): ChipPatch {
  if (reason instanceof ApiError && reason.status === 404) return { phase: "expired" };
  if (reason instanceof ApiError && reason.status >= 400 && reason.status < 500 && reason.status !== 429)
    return { phase: "failed", failure: reason.message };
  return { phase: "failed", failure: reason instanceof PresignedUploadError ? NOT_FINISHED : NOT_ATTACHED };
}

/**
 * Asks the server until the attachment is settled. `uploaded` says the bytes were just PUT, so an
 * attachment that still awaits them is asked again a few times; a remembered chip's never will be.
 */
export async function settleAttachment(
  teamSpaceId: string,
  attachmentId: string,
  uploaded: boolean,
  report: Report,
  signal: AbortSignal,
): Promise<void> {
  try {
    for (let round = 1; round <= SETTLE_ROUNDS; round++) {
      const { attachment } = await completeAttachmentUpload({ teamSpaceId, attachmentId }, signal);
      const state = attachmentStateView(attachment);
      switch (state.progress) {
        case "ready":
          return report({ phase: "ready", summary: state.summary, expiresAt: state.expiresAt });
        case "failed":
          return report({ phase: "failed", failure: state.failure });
        case "gone":
          return report({ phase: "expired" });
        case "awaiting_upload":
          if (!uploaded || round >= MISSING_UPLOAD_ROUNDS) return report({ phase: "failed", failure: NOT_FINISHED });
          await delay(1000, signal);
          break;
        case "working":
          break;
      }
      signal.throwIfAborted();
    }
    report({ phase: "failed", failure: TOO_SLOW });
  } catch (reason) {
    if (!isAbortError(reason)) report(failureOf(reason));
  }
}

/** The bytes Studio uploads for a file: an image normalized, any other file as it is within its limit. */
async function uploadedBytes(file: File, format: AttachmentFormat, report: Report) {
  if (format.kind !== "image") {
    if (!fitsKindLimit(format.kind, file.size)) return null;
    return { blob: file as Blob, name: file.name, mediaType: format.mediaType };
  }
  const image = await normalizeImage(file, format);
  report({
    name: image.name,
    mediaType: image.mediaType,
    size: image.blob.size,
    summary: {
      width: image.width,
      height: image.height,
      sections: 0,
      shownSections: 0,
      jsonFormat: "",
      nodes: 0,
      redactions: 0,
    },
  });
  return image;
}

/**
 * Uploads one file of the TeamSpace as the chip `requestId` and reports each step. The chip learns its
 * attachment ID (`attachmentId` in a patch) as soon as the file is declared, which the caller remembers.
 */
export async function uploadAttachmentFile(
  {
    teamSpaceId,
    requestId,
    file,
    format,
  }: { teamSpaceId: string; requestId: string; file: File; format: AttachmentFormat },
  report: Report,
  signal: AbortSignal,
): Promise<void> {
  try {
    const bytes = await uploadedBytes(file, format, report);
    if (!bytes) return report({ phase: "failed", failure: LOCAL_REFUSALS.tooLarge });
    report({ phase: "hashing" });
    const sha256 = await sha256Hex(bytes.blob);
    signal.throwIfAborted();
    report({ phase: "registering", sha256 });
    // The same request ID declares the same file again; while it awaits its bytes, that signs a fresh PUT.
    const declare = () =>
      createAttachmentUpload(
        {
          teamSpaceId,
          requestId,
          fileName: bytes.name,
          mediaType: bytes.mediaType,
          byteSize: String(bytes.blob.size),
          sha256,
        },
        signal,
      );
    const created = await declare();
    const state = attachmentStateView(created.attachment);
    report({ attachmentId: state.attachmentId });
    if (state.progress === "failed") return report({ phase: "failed", failure: state.failure });
    if (state.progress === "gone") return report({ phase: "expired" });
    let target = created.upload;
    for (let attempt = 1; target; attempt++) {
      report({ phase: "uploading", percent: 0 });
      try {
        await putPresigned(
          target,
          bytes.blob,
          (fraction) => report({ phase: "uploading", percent: Math.min(99, Math.round(fraction * 100)) }),
          signal,
        );
        target = null;
      } catch (reason) {
        if (!(reason instanceof PresignedUploadError)) throw reason;
        // The object key holds the digest, so an object already stored there holds these bytes.
        if (reason.status === UPLOAD_ALREADY_STORED) break;
        // Storage refuses an expired URL, or one signed with credentials that expired: once, ask for a fresh one.
        if (reason.status !== URL_REFUSED || attempt > 1) throw reason;
        target = (await declare()).upload;
      }
    }
    report({ phase: "reading", percent: 100 });
    await settleAttachment(teamSpaceId, state.attachmentId, true, report, signal);
  } catch (reason) {
    if (isAbortError(reason)) return;
    if (reason instanceof ImageNotUsable)
      return report({
        phase: "failed",
        failure: reason.tooLarge ? LOCAL_REFUSALS.tooLarge : LOCAL_REFUSALS.unsupported,
      });
    report(failureOf(reason));
  }
}

/**
 * Deletes a removed chip's staged attachment. Best effort: an attachment that is not deleted expires with
 * its staging retention, and a new request ID makes this a new command.
 */
export function discardAttachment(teamSpaceId: string, attachmentId: string): void {
  void deleteAttachment({ teamSpaceId, attachmentId, requestId: newRequestId() }, AbortSignal.timeout(30_000)).catch(
    () => undefined,
  );
}
