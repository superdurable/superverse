// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph, type GlyphName } from "@/components/ui/Glyph";
import { formatFileSize, sentAttachmentWhat, type MessageAttachment } from "@/lib/session/agent-types";
import { useAttachmentDownload, useAttachmentThumbnail } from "./AttachmentFilesProvider";

const KIND_GLYPHS = { image: "image", document: "document", json: "braces" } as const satisfies Record<
  MessageAttachment["kind"],
  GlyphName
>;

function AttachmentChip({ attachment, compact }: { attachment: MessageAttachment; compact: boolean }) {
  const isImage = attachment.kind === "image";
  const thumbnail = useAttachmentThumbnail(attachment.attachmentId, isImage && !compact);
  const download = useAttachmentDownload();
  const [unavailable, setUnavailable] = useState(false);
  const label = `${attachment.fileName}, ${sentAttachmentWhat(attachment)}, ${formatFileSize(attachment.byteSize)}`;
  const body = (
    <>
      {thumbnail.url ? (
        // A short-lived presigned URL of storage; Next's image optimizer must not fetch or cache it.
        // eslint-disable-next-line @next/next/no-img-element
        <img
          alt=""
          src={thumbnail.url}
          onError={thumbnail.reload}
          className="size-9 shrink-0 rounded-md border border-line-soft object-cover"
        />
      ) : (
        <span
          aria-hidden="true"
          className={cx(
            "grid shrink-0 place-items-center rounded-md bg-band-1 text-ink-dim",
            compact ? "size-4 bg-transparent" : "size-9",
          )}
        >
          <Glyph name={KIND_GLYPHS[attachment.kind]} className={compact ? "size-3.5" : undefined} />
        </span>
      )}
      <span className="grid min-w-0 text-left">
        <span className="truncate text-[12px] text-ink">{attachment.fileName}</span>
        {!compact && (
          <span className="truncate text-[11px] text-ink-faint">
            {unavailable
              ? "Available once the agent reads it"
              : `${sentAttachmentWhat(attachment)} · ${formatFileSize(attachment.byteSize)}`}
          </span>
        )}
      </span>
    </>
  );
  const frame = cx(
    "flex max-w-56 min-w-0 items-center gap-2 rounded-lg border border-line-soft bg-panel text-left",
    compact ? "px-1.5 py-0.5" : "p-1 pr-2.5",
  );
  if (!download)
    return (
      <span className={frame} role="img" aria-label={label} data-attachment-id={attachment.attachmentId}>
        {body}
      </span>
    );
  return (
    <button
      type="button"
      className={cx(frame, "hover:border-mark focus-visible:outline-2 focus-visible:outline-accent")}
      aria-label={`${label}. Download`}
      title={`Download ${attachment.fileName}`}
      data-attachment-id={attachment.attachmentId}
      onPointerEnter={() => download.warm(attachment.attachmentId)}
      onFocus={() => download.warm(attachment.attachmentId)}
      onClick={() => void download.open(attachment.attachmentId).then((opened) => setUnavailable(!opened))}
    >
      {body}
    </button>
  );
}

/**
 * The files a user message attached, as chips with an image's thumbnail; a click downloads the file (a JSON
 * file as its redacted copy). `compact` is the queue's one-line form, without thumbnails.
 */
export function MessageAttachments({
  attachments,
  compact = false,
  className,
}: {
  attachments: readonly MessageAttachment[];
  compact?: boolean;
  className?: string;
}) {
  if (attachments.length === 0) return null;
  return (
    <ul className={cx("flex flex-wrap gap-1.5", className)} aria-label="Attached files">
      {attachments.map((attachment) => (
        <li key={attachment.attachmentId} className="min-w-0">
          <AttachmentChip attachment={attachment} compact={compact} />
        </li>
      ))}
    </ul>
  );
}
