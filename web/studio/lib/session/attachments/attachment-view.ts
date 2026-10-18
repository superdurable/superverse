// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { AgentAttachment__Output } from "@/lib/generated/protobuf/superverse/v2/AgentAttachment";
import type { Attachment__Output } from "@/lib/generated/protobuf/superverse/v2/Attachment";
import type { AttachmentKind__Output } from "@/lib/generated/protobuf/superverse/v2/AttachmentKind";
import type { AttachmentStatus__Output } from "@/lib/generated/protobuf/superverse/v2/AttachmentStatus";
import { attachmentKindLabel, type AttachmentKind } from "./attachment-files";

/*
 * The browser view of the attachment protobuf: AttachmentService's Attachment for a chip in the composer,
 * and AgentAttachment for a sent message. A decoded int64 arrives as a decimal string; an unspecified enum
 * value is a server defect.
 */

const KINDS = {
  ATTACHMENT_KIND_IMAGE: "image",
  ATTACHMENT_KIND_DOCUMENT: "document",
  ATTACHMENT_KIND_JSON: "json",
} as const satisfies Record<Exclude<AttachmentKind__Output, "ATTACHMENT_KIND_UNSPECIFIED">, AttachmentKind>;

function kindView(value: AttachmentKind__Output): AttachmentKind {
  if (value === "ATTACHMENT_KIND_UNSPECIFIED") throw new Error("Attachment kind is unavailable");
  return KINDS[value];
}

function byteSize(value: string): number {
  const size = Number(value);
  if (!/^\d+$/.test(value) || !Number.isSafeInteger(size)) throw new Error("Invalid attachment size");
  return size;
}

/**
 * Where an attachment stands for its chip: still on its way (`working`, the server's VERIFYING or PREPARING),
 * waiting for its bytes, ready, failed, or gone (expired, deleted or being deleted).
 */
type AttachmentProgress = "awaiting_upload" | "working" | "ready" | "failed" | "gone";

const PROGRESS = {
  ATTACHMENT_STATUS_REGISTERING: "awaiting_upload",
  ATTACHMENT_STATUS_AWAITING_UPLOAD: "awaiting_upload",
  ATTACHMENT_STATUS_VERIFYING: "working",
  ATTACHMENT_STATUS_PREPARING: "working",
  ATTACHMENT_STATUS_READY: "ready",
  ATTACHMENT_STATUS_FAILED: "failed",
  ATTACHMENT_STATUS_DELETING: "gone",
  ATTACHMENT_STATUS_EXPIRED: "gone",
  ATTACHMENT_STATUS_DELETED: "gone",
} as const satisfies Record<Exclude<AttachmentStatus__Output, "ATTACHMENT_STATUS_UNSPECIFIED">, AttachmentProgress>;

/** What a ready attachment's chip says about it: an image's size, a document's sections, what JSON held. */
export interface AttachmentSummaryView {
  width: number;
  height: number;
  sections: number;
  shownSections: number;
  jsonFormat: string;
  nodes: number;
  redactions: number;
}

/** The sentence of a failure the server gave none for. */
const UNREADABLE = "The file could not be read. Try attaching it again.";

/** A READY attachment's staging end in epoch milliseconds, or null when the server named none. */
function expiryOf(value: Attachment__Output): number | null {
  const time = value.expiresAt ? Date.parse(value.expiresAt) : NaN;
  return Number.isFinite(time) ? time : null;
}

export function attachmentStateView(value: Attachment__Output | null) {
  if (!value) throw new Error("Attachment is unavailable");
  if (value.status === "ATTACHMENT_STATUS_UNSPECIFIED") throw new Error("Attachment status is unavailable");
  const summary = value.summary;
  return {
    attachmentId: value.attachmentId,
    progress: PROGRESS[value.status],
    expiresAt: value.status === "ATTACHMENT_STATUS_READY" ? expiryOf(value) : null,
    summary: summary
      ? {
          width: summary.width,
          height: summary.height,
          sections: summary.sections,
          shownSections: summary.shownSections,
          jsonFormat: summary.jsonFormat,
          nodes: summary.nodes,
          redactions: summary.redactions,
        }
      : null,
    failure: value.status === "ATTACHMENT_STATUS_FAILED" ? value.failure?.message || UNREADABLE : null,
  };
}

/** An attachment of a sent message, as its chip in the conversation shows it. */
export function agentAttachmentView(value: AgentAttachment__Output) {
  return {
    attachmentId: value.attachmentId,
    fileName: value.fileName,
    mediaType: value.mediaType,
    kind: kindView(value.kind),
    byteSize: byteSize(value.byteSize),
    width: value.width,
    height: value.height,
    jsonFormat: value.jsonFormat,
    redactions: value.redactions,
  };
}

const JSON_FORMATS: Record<string, string> = { n8n: "n8n workflow", generic: "JSON" };

function keysRemoved(count: number): string {
  return `${count} ${count === 1 ? "key" : "keys"} removed`;
}

/** What a ready chip says: "1200 × 800", "23 sections, the first 7 shown", "n8n workflow · 12 nodes · 3 keys removed". */
export function attachmentSummaryText(kind: AttachmentKind, summary: AttachmentSummaryView): string {
  switch (kind) {
    case "image":
      return summary.width > 0 ? `${summary.width} × ${summary.height}` : "Image";
    case "document":
      if (summary.sections === 0) return "Document";
      if (summary.shownSections >= summary.sections)
        return `${summary.sections} ${summary.sections === 1 ? "section" : "sections"}`;
      return `${summary.sections} sections, the first ${summary.shownSections} shown`;
    case "json":
      return [
        JSON_FORMATS[summary.jsonFormat] ?? "JSON",
        ...(summary.nodes > 0 ? [`${summary.nodes} ${summary.nodes === 1 ? "node" : "nodes"}`] : []),
        ...(summary.redactions > 0 ? [keysRemoved(summary.redactions)] : []),
      ].join(" · ");
  }
}

/** What a sent file was, in words: its kind, or what its JSON held with the keys removed from it. */
export function sentAttachmentWhat(attachment: ReturnType<typeof agentAttachmentView>): string {
  if (attachment.kind !== "json") return attachmentKindLabel(attachment.kind);
  const format = JSON_FORMATS[attachment.jsonFormat] ?? "JSON";
  return attachment.redactions > 0 ? `${format}, ${keysRemoved(attachment.redactions)}` : format;
}
