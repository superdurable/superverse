// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Development-only stand-in for the server's preparation: what a ready chip shows, never acceptance evidence.
import type { AttachmentSummary } from "@/lib/generated/protobuf/superverse/v2/AttachmentSummary";

/** The result of preparing uploaded bytes: a summary and, for JSON, the redacted copy; or a failure code. */
export type MockPreparation =
  { ready: true; summary: AttachmentSummary; bytes: Buffer } | { ready: false; code: string; message: string };

/** The sentences of the server's failure codes the mock decides, as the attachments package declares them. */
const FAILURES = {
  unsupported_content: "This file is not a valid PNG, JPEG, WebP, GIF, Markdown, text, Word, PDF or JSON file.",
  invalid_json: "This JSON could not be read: it is not valid or is nested too deeply.",
} as const;

function failed(code: keyof typeof FAILURES): MockPreparation {
  return { ready: false, code, message: FAILURES[code] };
}

/** The redaction marker of the server's JSON preparation. */
const MARKER = "‹redacted:superverse›";
const SECRET_NAME =
  /api[_-]?key|access[_-]?token|auth[_-]?token|refresh[_-]?token|token|secret|password|private[_-]?key|authorization|bearer/i;
const TOKEN_SHAPED = /^(?:sk-|xox[abposr]-|ghp_|github_pat_|AKIA[0-9A-Z]{16})/;

/** Like the server, replaces long secret values (not n8n expressions) with the marker and counts them. */
function redact(value: unknown, key: string | null, count: { redactions: number }): unknown {
  if (typeof value === "string") {
    const secret = (key !== null && SECRET_NAME.test(key)) || TOKEN_SHAPED.test(value);
    if (secret && value.length >= 8 && !value.startsWith("=") && value !== MARKER) {
      count.redactions += 1;
      return MARKER;
    }
    return value;
  }
  if (Array.isArray(value)) return value.map((item) => redact(item, key, count));
  if (value && typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    // A name and value pair, such as a header, is secret when its name is.
    const pairName = entries.find(([name]) => name === "name" || name === "key")?.[1];
    return Object.fromEntries(
      entries.map(([name, item]) => [
        name,
        redact(item, name === "value" && typeof pairName === "string" ? pairName : name, count),
      ]),
    );
  }
  return value;
}

function isN8nExport(value: unknown): value is { nodes: unknown[] } {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const { nodes, connections } = value as { nodes?: unknown; connections?: unknown };
  if (!Array.isArray(nodes) || !connections || typeof connections !== "object") return false;
  return nodes.some(
    (node) =>
      node &&
      typeof node === "object" &&
      typeof (node as { type?: unknown }).type === "string" &&
      /^(?:n8n-nodes-|@n8n\/)/.test((node as { type: string }).type),
  );
}

function prepareJson(bytes: Buffer): MockPreparation {
  let value: unknown;
  try {
    value = JSON.parse(bytes.toString("utf8"));
  } catch {
    return failed("invalid_json");
  }
  const count = { redactions: 0 };
  const redacted = redact(value, null, count);
  const nodes = isN8nExport(value) ? value.nodes.length : null;
  return {
    ready: true,
    summary: { jsonFormat: nodes === null ? "generic" : "n8n", nodes: nodes ?? 0, redactions: count.redactions },
    bytes: Buffer.from(JSON.stringify(redacted, null, 2)),
  };
}

/** The dimensions an image header declares, or null for a format the mock does not read. */
function imageSize(bytes: Buffer, mediaType: string): { width: number; height: number } | null {
  if (mediaType === "image/png" && bytes.length >= 24)
    return { width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20) };
  if (mediaType === "image/gif" && bytes.length >= 10)
    return { width: bytes.readUInt16LE(6), height: bytes.readUInt16LE(8) };
  if (mediaType === "image/jpeg")
    for (let offset = 2; offset + 9 < bytes.length;) {
      if (bytes[offset] !== 0xff) return null;
      const marker = bytes[offset + 1];
      if (marker >= 0xc0 && marker <= 0xc3)
        return { width: bytes.readUInt16BE(offset + 7), height: bytes.readUInt16BE(offset + 5) };
      offset += 2 + bytes.readUInt16BE(offset + 2);
    }
  return null;
}

/** Whether the bytes start as their declared type does, as the extractor sniffs them. */
function sniffs(bytes: Buffer, mediaType: string): boolean {
  const ascii = bytes.subarray(0, 12).toString("latin1");
  switch (mediaType) {
    case "image/png":
      return ascii.startsWith("\x89PNG");
    case "image/jpeg":
      return bytes[0] === 0xff && bytes[1] === 0xd8;
    case "image/gif":
      return ascii.startsWith("GIF8");
    case "image/webp":
      return ascii.startsWith("RIFF") && ascii.slice(8, 12) === "WEBP";
    case "application/pdf":
      return ascii.startsWith("%PDF-");
    case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
      return ascii.startsWith("PK");
    default:
      return !bytes.includes(0);
  }
}

/**
 * Prepares uploaded bytes as the server would, approximately: the content must sniff as its type; an image
 * shows its header's dimensions, Markdown its headings as sections, JSON its format, nodes and redactions.
 */
export function prepareMockAttachment(bytes: Buffer, mediaType: string): MockPreparation {
  if (!sniffs(bytes, mediaType)) return failed("unsupported_content");
  if (mediaType === "application/json") return prepareJson(bytes);
  if (mediaType.startsWith("image/")) {
    const size = imageSize(bytes, mediaType) ?? { width: 1200, height: 800 };
    return { ready: true, summary: size, bytes };
  }
  const headings =
    mediaType === "text/markdown"
      ? bytes
          .toString("utf8")
          .split("\n")
          .filter((line) => /^#{1,6}\s/.test(line)).length
      : 0;
  const sections = Math.max(1, mediaType === "text/plain" ? 1 : headings || 3);
  return { ready: true, summary: { sections, shownSections: sections, truncated: false, pages: 0 }, bytes };
}
