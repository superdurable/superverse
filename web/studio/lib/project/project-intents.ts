// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * The browser-local intent of the Project being created: it outlives the navigation to the creation
 * page and holds the request IDs, so that a retry, reload or remount repeats the same CreateProject
 * request. For a local agent, ProjectIntent retains the first message through pairing. Corrupt values are discarded and never
 * sent. Storage errors reach the caller: a creation the browser could not keep must not start.
 */

import { readStored, removeStored, STORAGE_KEYS, writeStored } from "@/lib/browser/storage";
import { isRecord, type UnknownRecord } from "@/lib/shared/json";

export const PROJECT_EXECUTOR = { SUPERVERSE: "superverse", LOCAL: "local" } as const;
export type ProjectExecutor = (typeof PROJECT_EXECUTOR)[keyof typeof PROJECT_EXECUTOR];
export const PROJECT_MESSAGE_DELIVERY = { UNSENT: "unsent", SENDING: "sending" } as const;

/**
 * A template-created Project in its Teamspace with the first message (`prompt`), selected agent
 * and optional Superverse model, with stable retry identities.
 * The creation page always stores a model; null is a creation stored before the model was chosen there,
 * which the server starts on its default model.
 */
export type PendingProjectCreation = {
  teamSpaceId: string;
  name: string;
  prompt: string;
  executor: ProjectExecutor;
  model: string | null;
  idempotencyKey: string;
  messageIdempotencyKey: string;
};

/** The local-agent first message remains browser-owned after CreateProject accepts the Project. */
export type ProjectIntent = PendingProjectCreation & {
  projectId: string;
  delivery: (typeof PROJECT_MESSAGE_DELIVERY)[keyof typeof PROJECT_MESSAGE_DELIVERY];
};

function read(key: string): UnknownRecord | null {
  const encoded = readStored("session", key);
  if (!encoded) return null;
  try {
    const value: unknown = JSON.parse(encoded);
    if (isRecord(value)) return value;
  } catch {
    // Discarded below.
  }
  removeStored("session", key);
  return null;
}

function nonEmpty(value: unknown): value is string {
  return typeof value === "string" && value.trim() !== "";
}

export function hasPendingProjectCreation(): boolean {
  return readStored("session", STORAGE_KEYS.projectCreation) !== null;
}

/** The raw stored creation, used to notice that another tab replaced it. */
export function encodedPendingProjectCreation(): string | null {
  return readStored("session", STORAGE_KEYS.projectCreation);
}

export function readPendingProjectCreation(): PendingProjectCreation | null {
  const value = read(STORAGE_KEYS.projectCreation);
  if (!value) return null;
  const model = value.model ?? null;
  const executor = value.executor ?? PROJECT_EXECUTOR.SUPERVERSE;
  if (
    (executor === PROJECT_EXECUTOR.SUPERVERSE || executor === PROJECT_EXECUTOR.LOCAL) &&
    (model === null || nonEmpty(model)) &&
    nonEmpty(value.teamSpaceId) &&
    nonEmpty(value.name) &&
    nonEmpty(value.prompt) &&
    nonEmpty(value.messageIdempotencyKey) &&
    nonEmpty(value.idempotencyKey)
  )
    return {
      teamSpaceId: value.teamSpaceId,
      name: value.name,
      prompt: value.prompt,
      executor,
      model,
      idempotencyKey: value.idempotencyKey,
      messageIdempotencyKey: value.messageIdempotencyKey,
    };
  removeStored("session", STORAGE_KEYS.projectCreation);
  return null;
}

export function storePendingProjectCreation(creation: PendingProjectCreation): void {
  writeStored("session", STORAGE_KEYS.projectCreation, JSON.stringify(creation));
}

export function clearPendingProjectCreation(): void {
  removeStored("session", STORAGE_KEYS.projectCreation);
}

export function readProjectIntent(): ProjectIntent | null {
  const value = read(STORAGE_KEYS.projectIntent);
  if (!value) return null;
  const model = value.model ?? null;
  if (
    nonEmpty(value.projectId) &&
    nonEmpty(value.teamSpaceId) &&
    nonEmpty(value.name) &&
    nonEmpty(value.prompt) &&
    nonEmpty(value.idempotencyKey) &&
    nonEmpty(value.messageIdempotencyKey) &&
    value.executor === PROJECT_EXECUTOR.LOCAL &&
    (model === null || nonEmpty(model)) &&
    (value.delivery === PROJECT_MESSAGE_DELIVERY.UNSENT || value.delivery === PROJECT_MESSAGE_DELIVERY.SENDING)
  )
    return {
      projectId: value.projectId,
      teamSpaceId: value.teamSpaceId,
      name: value.name,
      prompt: value.prompt,
      executor: value.executor,
      model,
      idempotencyKey: value.idempotencyKey,
      messageIdempotencyKey: value.messageIdempotencyKey,
      delivery: value.delivery,
    };
  removeStored("session", STORAGE_KEYS.projectIntent);
  return null;
}

export function storeProjectIntent(intent: ProjectIntent): void {
  writeStored("session", STORAGE_KEYS.projectIntent, JSON.stringify(intent));
}

export function clearProjectIntent(projectId: string): void {
  if (readProjectIntent()?.projectId === projectId) removeStored("session", STORAGE_KEYS.projectIntent);
}
