// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { getAttachmentDownloads } from "@/lib/api/attachment";

/** How a link opens its file: inline, for an image's thumbnail, or as a download with the file name. */
type AttachmentDisposition = "inline" | "download";

/** A short-lived URL of an attachment and when it stops working (milliseconds since the epoch). */
interface AttachmentLink {
  url: string;
  expiresAt: number;
}

/**
 * The links of a Project's attachments, which the Session page gives the transport-free conversation: null
 * for an attachment that has none yet, such as one of a message the Session has not shown the model yet.
 */
export interface AttachmentFileLinks {
  link(attachmentId: string, disposition: AttachmentDisposition): Promise<AttachmentLink | null>;
}

/** GetAttachmentDownloads names at most this many attachments. */
const BATCH = 20;
/** A link is renewed this long before it expires, so a click never opens an expired one. */
const RENEW_BEFORE_MS = 30_000;
/** An attachment without a link is asked about again after this long. */
const MISSING_FOR_MS = 4_000;

type Waiter = { resolve: (link: AttachmentLink | null) => void };

/**
 * The links of the Project's attachments through GetAttachmentDownloads: the requests of one moment are
 * batched (20 per call), a link is reused until shortly before it expires, and a missing one is asked again
 * only after a few seconds. A failed read answers null; the chip shows its file without a thumbnail.
 */
export function projectAttachmentLinks(projectId: string): AttachmentFileLinks {
  const links = new Map<string, AttachmentLink>();
  const missing = new Map<string, number>();
  const waiting = new Map<AttachmentDisposition, Map<string, Waiter[]>>();
  const key = (attachmentId: string, disposition: AttachmentDisposition) => `${disposition}:${attachmentId}`;

  async function flush(disposition: AttachmentDisposition) {
    const batch = waiting.get(disposition);
    waiting.delete(disposition);
    if (!batch) return;
    const ids = [...batch.keys()];
    for (let start = 0; start < ids.length; start += BATCH) {
      const chunk = ids.slice(start, start + BATCH);
      let found: Map<string, AttachmentLink> = new Map();
      try {
        const { downloads } = await getAttachmentDownloads(
          {
            projectId,
            attachmentIds: chunk,
            disposition: disposition === "inline" ? "ATTACHMENT_DISPOSITION_INLINE" : "ATTACHMENT_DISPOSITION_DOWNLOAD",
          },
          AbortSignal.timeout(30_000),
        );
        found = new Map(
          downloads.map((download) => [
            download.attachmentId,
            { url: download.url, expiresAt: Date.parse(download.expiresAt) || Date.now() + 60_000 },
          ]),
        );
      } catch {
        // No link this time; the next request asks again.
      }
      for (const attachmentId of chunk) {
        const link = found.get(attachmentId) ?? null;
        if (link) links.set(key(attachmentId, disposition), link);
        else missing.set(key(attachmentId, disposition), Date.now());
        for (const waiter of batch.get(attachmentId) ?? []) waiter.resolve(link);
      }
    }
  }

  return {
    link(attachmentId, disposition) {
      const cached = links.get(key(attachmentId, disposition));
      if (cached && cached.expiresAt - Date.now() > RENEW_BEFORE_MS) return Promise.resolve(cached);
      const missingSince = missing.get(key(attachmentId, disposition));
      if (missingSince !== undefined && Date.now() - missingSince < MISSING_FOR_MS) return Promise.resolve(null);
      return new Promise((resolve) => {
        let batch = waiting.get(disposition);
        if (!batch) {
          waiting.set(disposition, (batch = new Map()));
          // Every request of this moment joins one call.
          setTimeout(() => void flush(disposition), 0);
        }
        batch.set(attachmentId, [...(batch.get(attachmentId) ?? []), { resolve }]);
      });
    },
  };
}
