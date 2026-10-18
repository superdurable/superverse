// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * The files a message may attach, as the server's formats table declares them
 * (docs/architecture/attachments/upload-and-preparation.md#kinds-formats-and-limits). The server checks
 * every rule again; Studio checks them first so that a refused file fails at once, without an upload.
 */

/** The kind of an attachment, the Agent UI literal of AttachmentKind. */
export type AttachmentKind = "image" | "document" | "json";

const MIB = 1024 * 1024;

/**
 * The server's default limits (`attachments.*` in cmd/superverse-server/config). Studio normalizes images to
 * them before upload; a server configured below them refuses a larger file when it is declared or read.
 */
export const ATTACHMENT_LIMITS = {
  perMessage: 10,
  imageLongEdge: 2000,
  imageBytes: MIB,
  documentBytes: 20 * MIB,
  jsonBytes: 4 * MIB,
} as const;

const KIND_DETAILS = {
  image: { label: "image", maxBytes: ATTACHMENT_LIMITS.imageBytes },
  document: { label: "document", maxBytes: ATTACHMENT_LIMITS.documentBytes },
  json: { label: "JSON", maxBytes: ATTACHMENT_LIMITS.jsonBytes },
} as const satisfies Record<AttachmentKind, { label: string; maxBytes: number }>;

/** One accepted format: its media type, its extensions (the first is the one Studio writes) and its kind. */
export interface AttachmentFormat {
  mediaType: string;
  extensions: readonly string[];
  kind: AttachmentKind;
}

const FORMATS: readonly AttachmentFormat[] = [
  { mediaType: "image/png", extensions: [".png"], kind: "image" },
  { mediaType: "image/jpeg", extensions: [".jpg", ".jpeg"], kind: "image" },
  { mediaType: "image/webp", extensions: [".webp"], kind: "image" },
  { mediaType: "image/gif", extensions: [".gif"], kind: "image" },
  { mediaType: "text/markdown", extensions: [".md", ".markdown"], kind: "document" },
  { mediaType: "text/plain", extensions: [".txt"], kind: "document" },
  {
    mediaType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    extensions: [".docx"],
    kind: "document",
  },
  { mediaType: "application/pdf", extensions: [".pdf"], kind: "document" },
  { mediaType: "application/json", extensions: [".json"], kind: "json" },
];

/** The file picker's `accept`: every extension and media type of the table. */
export const ACCEPTED_FILES = FORMATS.flatMap(({ mediaType, extensions }) => [...extensions, mediaType]).join(",");

/** The sentences of the refusals Studio decides before an upload; the server's own failures carry theirs. */
export const LOCAL_REFUSALS = {
  unsupported: "This file is not a valid PNG, JPEG, WebP, GIF, Markdown, text, Word, PDF or JSON file.",
  tooLarge: "This file is larger than the limit for its type.",
  tooMany: `Up to ${ATTACHMENT_LIMITS.perMessage} files per message`,
  /** A local agent runs the Session: in this version only the platform model reads attached files. */
  needsPlatformAgent: "Attachments work with the Superverse agent",
} as const;

function extensionOf(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(dot).toLowerCase() : "";
}

/**
 * The format of a file by its extension, which the server requires to match the declared media type; the
 * browser's own type is unreliable (empty for Markdown on most systems). Null for any other file.
 */
export function attachmentFormat(name: string): AttachmentFormat | null {
  const extension = extensionOf(name);
  return FORMATS.find((format) => format.extensions.includes(extension)) ?? null;
}

/** The format of a media type Studio encodes an image as. */
export function imageFormat(mediaType: string): AttachmentFormat {
  const format = FORMATS.find((item) => item.mediaType === mediaType && item.kind === "image");
  if (!format) throw new Error(`${mediaType} is not an image format`);
  return format;
}

/** `name` with the extension of `format`, unless it already has one of its extensions. */
export function nameWithExtension(name: string, format: AttachmentFormat): string {
  if (format.extensions.includes(extensionOf(name))) return name;
  const dot = name.lastIndexOf(".");
  return `${dot > 0 ? name.slice(0, dot) : name}${format.extensions[0]}`;
}

/** Whether a file of `kind` may be uploaded at `size` bytes; images are normalized to their limit first. */
export function fitsKindLimit(kind: AttachmentKind, size: number): boolean {
  return size > 0 && size <= KIND_DETAILS[kind].maxBytes;
}

export function attachmentKindLabel(kind: AttachmentKind): string {
  return KIND_DETAILS[kind].label;
}

/** A size as people read it: 412 KB, 1.2 MB. */
export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < MIB) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / MIB).toFixed(bytes < 10 * MIB ? 1 : 0)} MB`;
}
