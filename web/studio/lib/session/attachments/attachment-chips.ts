// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { isRecord } from "@/lib/shared/json";
import { attachmentFormat, attachmentKindLabel, formatFileSize, type AttachmentKind } from "./attachment-files";
import { attachmentSummaryText, type AttachmentSummaryView } from "./attachment-view";

/*
 * The composer's chips: one state machine per file, keyed by the upload's request ID. An update names its
 * chip's key; one for a chip that was removed or replaced changes nothing, so a late answer of an earlier
 * attempt never reaches a newer chip.
 */

/**
 * Where a chip's file stands: normalized (an image), hashed, registered, uploaded with its percent, read by
 * the server, or, after a reload, checked again; then ready, failed with its reason, or expired.
 */
type ChipPhase =
  "preparing" | "hashing" | "registering" | "uploading" | "reading" | "checking" | "ready" | "failed" | "expired";

export interface AttachmentChip {
  /** The upload's request ID, which a retry of CreateAttachmentUpload repeats. */
  key: string;
  name: string;
  mediaType: string;
  size: number;
  kind: AttachmentKind;
  /** A local object URL of an image, for its thumbnail; the tray revokes it with the chip. */
  previewUrl: string | null;
  attachmentId: string | null;
  sha256: string | null;
  phase: ChipPhase;
  /** The upload's progress, 0 to 100, while uploading. */
  percent: number;
  summary: AttachmentSummaryView | null;
  /** The failure's sentence, in business language. */
  failure: string | null;
  /** When a ready attachment's staging ends, in epoch milliseconds, after which it can no longer be sent. */
  expiresAt: number | null;
}

/** What one step of a chip's work changes; the key is the chip's. */
export type ChipPatch = Partial<Omit<AttachmentChip, "key">>;

/**
 * A chip the browser remembers once its attachment exists, so that a reload shows it again and asks the
 * server where it stands. Never the bytes.
 */
export interface RememberedAttachment {
  attachmentId: string;
  uploadRequestId: string;
  name: string;
  mediaType: string;
  size: number;
  /** Empty for a file taken back from the queue, whose digest the browser never computed. */
  sha256: string;
}

export type ChipAction =
  | { type: "added"; chips: readonly AttachmentChip[] }
  | { type: "patched"; key: string; patch: ChipPatch }
  | { type: "removed"; key: string }
  | { type: "forgotten"; keys: ReadonlySet<string> }
  | { type: "cleared" };

export function chipsReducer(chips: readonly AttachmentChip[], action: ChipAction): readonly AttachmentChip[] {
  switch (action.type) {
    case "added":
      return [...chips, ...action.chips.filter((chip) => !chips.some((existing) => existing.key === chip.key))];
    case "patched":
      return chips.map((chip) => (chip.key === action.key ? { ...chip, ...action.patch } : chip));
    case "removed":
      return chips.filter((chip) => chip.key !== action.key);
    case "forgotten":
      return chips.filter((chip) => !action.keys.has(chip.key));
    case "cleared":
      return [];
  }
}

/** A new chip for a file, before any work. */
export function newChip(key: string, file: { name: string; size: number }, kind: AttachmentKind, mediaType: string) {
  return {
    key,
    name: file.name,
    mediaType,
    size: file.size,
    kind,
    previewUrl: null,
    attachmentId: null,
    sha256: null,
    phase: "preparing",
    percent: 0,
    summary: null,
    failure: null,
    expiresAt: null,
  } satisfies AttachmentChip;
}

/** A remembered chip after a reload, shown at once with its name and size while the server is asked. */
export function rememberedChip(entry: RememberedAttachment): AttachmentChip {
  const kind = attachmentFormat(entry.name)?.kind ?? "document";
  return {
    ...newChip(entry.uploadRequestId, entry, kind, entry.mediaType),
    attachmentId: entry.attachmentId,
    sha256: entry.sha256,
    phase: "checking",
  };
}

/** The remembered form of a chip whose attachment exists. */
export function rememberedOf(chip: AttachmentChip): RememberedAttachment | null {
  if (!chip.attachmentId || chip.phase === "failed" || chip.phase === "expired") return null;
  return {
    attachmentId: chip.attachmentId,
    uploadRequestId: chip.key,
    name: chip.name,
    mediaType: chip.mediaType,
    size: chip.size,
    sha256: chip.sha256 ?? "",
  };
}

const nonEmpty = (value: unknown): value is string => typeof value === "string" && value !== "";

/** The remembered chips of a stored value; anything corrupt is dropped, never sent. */
export function readRemembered(value: unknown): RememberedAttachment[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry: unknown) =>
    isRecord(entry) &&
    nonEmpty(entry.attachmentId) &&
    nonEmpty(entry.uploadRequestId) &&
    nonEmpty(entry.name) &&
    nonEmpty(entry.mediaType) &&
    typeof entry.sha256 === "string" &&
    typeof entry.size === "number" &&
    Number.isSafeInteger(entry.size) &&
    entry.size > 0
      ? [
          {
            attachmentId: entry.attachmentId,
            uploadRequestId: entry.uploadRequestId,
            name: entry.name,
            mediaType: entry.mediaType,
            size: entry.size,
            sha256: entry.sha256,
          },
        ]
      : [],
  );
}

const PHASE_TEXT = {
  preparing: "Preparing…",
  hashing: "Hashing…",
  registering: "Starting the upload…",
  uploading: "Uploading",
  reading: "Reading…",
  checking: "Checking…",
  ready: "Ready",
  failed: "Failed",
  expired: "Expired. Attach it again.",
} as const satisfies Record<ChipPhase, string>;

/** What a chip says under its name: its progress, what a ready file holds, or why it failed. */
export function chipStatusText(chip: AttachmentChip): string {
  switch (chip.phase) {
    case "uploading":
      return `${PHASE_TEXT.uploading} ${chip.percent}%`;
    case "ready":
      return chip.summary ? attachmentSummaryText(chip.kind, chip.summary) : PHASE_TEXT.ready;
    case "failed":
      return chip.failure ?? PHASE_TEXT.failed;
    case "preparing":
    case "hashing":
    case "registering":
    case "reading":
    case "checking":
    case "expired":
      return PHASE_TEXT[chip.phase];
  }
}

/** Whether a chip's work still runs. */
export function chipWorking(chip: AttachmentChip): boolean {
  return chip.phase !== "ready" && chip.phase !== "failed" && chip.phase !== "expired";
}

/** A chip's accessible name: "order-process.png, image, 412 KB, ready". */
export function chipLabel(chip: AttachmentChip): string {
  const state =
    chip.phase === "ready"
      ? "ready"
      : chip.phase === "failed"
        ? `failed: ${chip.failure ?? "the file could not be attached"}`
        : chipStatusText(chip).replace(/…$/, "").toLowerCase();
  return `${chip.name}, ${attachmentKindLabel(chip.kind)}, ${formatFileSize(chip.size)}, ${state}`;
}
