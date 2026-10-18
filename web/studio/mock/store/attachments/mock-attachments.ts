// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Development-only attachment staging over the mock store: no storage, never acceptance evidence.
import { createHash } from "node:crypto";
import type { AgentAttachment } from "@/lib/generated/protobuf/superverse/v2/AgentAttachment";
import type { Attachment } from "@/lib/generated/protobuf/superverse/v2/Attachment";
import type { AttachmentDownload } from "@/lib/generated/protobuf/superverse/v2/AttachmentDownload";
import type { AttachmentUploadTarget } from "@/lib/generated/protobuf/superverse/v2/AttachmentUploadTarget";
import type { CreateAttachmentUploadRequest__Output } from "@/lib/generated/protobuf/superverse/v2/CreateAttachmentUploadRequest";
import { httpError } from "@/lib/server/request-body";
import type { MockStore } from "@/mock/store/store";
import { prepareMockAttachment } from "./attachment-preparation";

/*
 * An attachment of the mock: CreateAttachmentUpload presigns a same-origin PUT to the mock route
 * (app/api/mock-attachments/[attachmentId]), which keeps the bytes in memory; CompleteAttachmentUpload
 * prepares them at once. A message that carries an attachment records its Project, which then has its
 * downloads, as the server's copy manifest does. Weak store keys release everything with the store.
 */
type MockAttachmentRow = {
  attachment: Attachment;
  uploaderAccountId: string;
  sha256: string;
  bytes: Buffer | null;
  projects: Set<string>;
};

const UPLOAD_VALIDITY_MS = 15 * 60_000;
const STAGING_MS = 24 * 3_600_000;
const DOWNLOAD_VALIDITY_MS = 5 * 60_000;
/** The bytes one mock store holds; a PUT past it is refused, as a full bucket would refuse it. */
const STORE_BYTES = 128 * 1024 * 1024;

const KINDS: Record<string, { kind: NonNullable<Attachment["kind"]>; extensions: string[]; maxBytes: number }> = {
  "image/png": { kind: "ATTACHMENT_KIND_IMAGE", extensions: [".png"], maxBytes: 1 << 20 },
  "image/jpeg": { kind: "ATTACHMENT_KIND_IMAGE", extensions: [".jpg", ".jpeg"], maxBytes: 1 << 20 },
  "image/webp": { kind: "ATTACHMENT_KIND_IMAGE", extensions: [".webp"], maxBytes: 1 << 20 },
  "image/gif": { kind: "ATTACHMENT_KIND_IMAGE", extensions: [".gif"], maxBytes: 1 << 20 },
  "text/markdown": { kind: "ATTACHMENT_KIND_DOCUMENT", extensions: [".md", ".markdown"], maxBytes: 20 << 20 },
  "text/plain": { kind: "ATTACHMENT_KIND_DOCUMENT", extensions: [".txt"], maxBytes: 20 << 20 },
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document": {
    kind: "ATTACHMENT_KIND_DOCUMENT",
    extensions: [".docx"],
    maxBytes: 20 << 20,
  },
  "application/pdf": { kind: "ATTACHMENT_KIND_DOCUMENT", extensions: [".pdf"], maxBytes: 20 << 20 },
  "application/json": { kind: "ATTACHMENT_KIND_JSON", extensions: [".json"], maxBytes: 4 << 20 },
};

const stores = new WeakMap<MockStore, Map<string, MockAttachmentRow>>();

function rows(store: MockStore): Map<string, MockAttachmentRow> {
  let found = stores.get(store);
  if (!found) stores.set(store, (found = new Map()));
  return found;
}

const iso = (ms: number) => new Date(ms).toISOString().replace(/\.\d{3}Z$/, "Z");

/** A row, with its staging expired once its time passed. */
function row(store: MockStore, attachmentId: string): MockAttachmentRow {
  const found = rows(store).get(attachmentId);
  if (!found) throw httpError(404, "the TeamSpace holds no such attachment");
  const status = found.attachment.status;
  if (status !== "ATTACHMENT_STATUS_DELETED" && Date.parse(found.attachment.expiresAt ?? "") <= Date.now()) {
    found.attachment.status = "ATTACHMENT_STATUS_EXPIRED";
    found.bytes = null;
  }
  return found;
}

function uploadTarget(store: MockStore, entry: MockAttachmentRow): AttachmentUploadTarget {
  return {
    url: `/api/mock-attachments/${entry.attachment.attachmentId}`,
    headers: [
      { name: "content-type", value: entry.attachment.mediaType },
      { name: "if-none-match", value: "*" },
      { name: "x-amz-checksum-sha256", value: Buffer.from(entry.sha256, "hex").toString("base64") },
      { name: "x-amz-meta-sha256", value: entry.sha256 },
    ],
    expiresAt: iso(Date.now() + UPLOAD_VALIDITY_MS),
  };
}

/** Declares a file of a member; a replay of the request ID answers the same attachment. */
export function createMockUpload(
  store: MockStore,
  accountId: string,
  request: CreateAttachmentUploadRequest__Output,
): { attachment: Attachment; upload?: AttachmentUploadTarget } {
  const declared = KINDS[request.mediaType];
  const size = Number(request.byteSize);
  const extension = request.fileName.slice(request.fileName.lastIndexOf(".")).toLowerCase();
  if (!request.requestId || !declared || !declared.extensions.includes(extension) || /[/\\]/.test(request.fileName))
    throw httpError(400, "the file cannot be attached: its name and type do not match a supported format");
  if (!(size > 0 && size <= declared.maxBytes)) throw httpError(400, "the file cannot be attached: it is too large");
  if (!/^[0-9a-f]{64}$/.test(request.sha256)) throw httpError(400, "the file cannot be attached: invalid sha256");
  const attachmentId = `m${createHash("sha256").update(`${accountId}:${request.requestId}`).digest("hex").slice(0, 11)}`;
  let entry = rows(store).get(attachmentId);
  if (!entry) {
    entry = {
      attachment: {
        attachmentId,
        teamSpaceId: request.teamSpaceId,
        fileName: request.fileName,
        mediaType: request.mediaType,
        kind: declared.kind,
        byteSize: String(size),
        status: "ATTACHMENT_STATUS_AWAITING_UPLOAD",
        expiresAt: iso(Date.now() + STAGING_MS),
      },
      uploaderAccountId: accountId,
      sha256: request.sha256,
      bytes: null,
      projects: new Set(),
    };
    rows(store).set(attachmentId, entry);
  }
  const current = row(store, attachmentId);
  return current.attachment.status === "ATTACHMENT_STATUS_AWAITING_UPLOAD"
    ? { attachment: current.attachment, upload: uploadTarget(store, current) }
    : { attachment: current.attachment };
}

/** Takes the presigned PUT as storage would: the signed type, length and digest, written once. Returns its status. */
export function putMockUpload(store: MockStore, attachmentId: string, headers: Headers, body: Buffer): number {
  const entry = rows(store).get(attachmentId);
  if (!entry || entry.attachment.status !== "ATTACHMENT_STATUS_AWAITING_UPLOAD") return 403;
  if (entry.bytes) return 412;
  if (headers.get("content-type") !== entry.attachment.mediaType || headers.get("if-none-match") !== "*") return 403;
  if (body.length !== Number(entry.attachment.byteSize)) return 400;
  if (createHash("sha256").update(body).digest("hex") !== entry.sha256) return 400;
  const held = [...rows(store).values()].reduce((total, item) => total + (item.bytes?.length ?? 0), 0);
  if (held + body.length > STORE_BYTES) return 507;
  entry.bytes = body;
  return 200;
}

/** Ends the upload wait of the uploader's attachment and prepares it at once; without bytes it still awaits them. */
export function completeMockUpload(store: MockStore, accountId: string, attachmentId: string): Attachment {
  const entry = row(store, attachmentId);
  if (entry.uploaderAccountId !== accountId) throw httpError(403, "only its uploader completes an attachment");
  const attachment = entry.attachment;
  if (attachment.status !== "ATTACHMENT_STATUS_AWAITING_UPLOAD" || !entry.bytes) return attachment;
  const prepared = prepareMockAttachment(entry.bytes, attachment.mediaType ?? "");
  if (prepared.ready) {
    attachment.status = "ATTACHMENT_STATUS_READY";
    attachment.summary = prepared.summary;
    entry.bytes = prepared.bytes;
  } else {
    attachment.status = "ATTACHMENT_STATUS_FAILED";
    attachment.failure = { code: prepared.code, message: prepared.message };
    entry.bytes = null;
  }
  return attachment;
}

export function deleteMockAttachment(store: MockStore, accountId: string, attachmentId: string): Attachment {
  const entry = row(store, attachmentId);
  if (entry.uploaderAccountId !== accountId) throw httpError(403, "only its uploader deletes an attachment");
  // A staged attachment goes; the Projects whose messages carry it keep their copies.
  if (entry.projects.size === 0) entry.bytes = null;
  entry.attachment.status = "ATTACHMENT_STATUS_DELETED";
  return entry.attachment;
}

/** Why a message's attachment is refused, as the server's AdmissionRefusal codes say it, with its HTTP status. */
export class MockAttachmentRefusal extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

/**
 * Admits a message's attachments as the server does: each the caller's own, READY, of the Project's
 * Teamspace; the Project then holds them, unless `projectId` is null, which only checks. Returns what the
 * message shows of each.
 */
export function admitMockAttachments(
  store: MockStore,
  { accountId, teamSpaceId, projectId }: { accountId: string; teamSpaceId: string; projectId: string | null },
  ids: readonly string[],
): AgentAttachment[] {
  if (ids.length > 10 || new Set(ids).size !== ids.length)
    throw new MockAttachmentRefusal(400, "a message attaches at most ten distinct files");
  const admitted = ids.map((attachmentId) => {
    const entry = rows(store).get(attachmentId) && row(store, attachmentId);
    if (!entry || entry.attachment.teamSpaceId !== teamSpaceId)
      throw new MockAttachmentRefusal(404, `attachment_not_found: the TeamSpace holds no attachment ${attachmentId}`);
    if (entry.uploaderAccountId !== accountId)
      throw new MockAttachmentRefusal(403, `attachment_not_yours: only its uploader sends attachment ${attachmentId}`);
    const status = entry.attachment.status;
    if (status === "ATTACHMENT_STATUS_EXPIRED" || status === "ATTACHMENT_STATUS_DELETED")
      throw new MockAttachmentRefusal(409, `attachment_expired: attachment ${attachmentId} expired or was removed`);
    if (status !== "ATTACHMENT_STATUS_READY")
      throw new MockAttachmentRefusal(409, `attachment_not_ready: attachment ${attachmentId} is not READY`);
    return entry;
  });
  return admitted.map(({ attachment, projects }) => {
    if (projectId !== null) projects.add(projectId);
    return {
      attachmentId: attachment.attachmentId,
      fileName: attachment.fileName,
      mediaType: attachment.mediaType,
      kind: attachment.kind,
      byteSize: attachment.byteSize,
      width: attachment.summary?.width ?? 0,
      height: attachment.summary?.height ?? 0,
      jsonFormat: attachment.summary?.jsonFormat ?? "",
      redactions: attachment.summary?.redactions ?? 0,
    };
  });
}

/** The downloads of the attachments the Project's messages carry, through the mock route. */
export function mockDownloads(
  store: MockStore,
  projectId: string,
  ids: readonly string[],
  inline: boolean,
): AttachmentDownload[] {
  return ids.flatMap((attachmentId) => {
    const entry = rows(store).get(attachmentId);
    if (!entry?.bytes || !entry.projects.has(projectId)) return [];
    const query = new URLSearchParams({ project: projectId, disposition: inline ? "inline" : "download" });
    return [
      {
        attachmentId,
        url: `/api/mock-attachments/${attachmentId}?${query}`,
        expiresAt: iso(Date.now() + DOWNLOAD_VALIDITY_MS),
      },
    ];
  });
}

/** The file a mock download serves, or null when the Project holds no such attachment. */
export function mockAttachmentFile(store: MockStore, attachmentId: string, projectId: string) {
  const entry = rows(store).get(attachmentId);
  if (!entry?.bytes || !entry.projects.has(projectId)) return null;
  return { bytes: entry.bytes, fileName: entry.attachment.fileName ?? "", mediaType: entry.attachment.mediaType ?? "" };
}
