// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * Browser-local intents that outlive a navigation: the Project being created
 * and the first message of its Session with the model the person chose for it.
 * They hold request IDs so that a retry, reload or remount repeats the same
 * request. Corrupt values are discarded and never sent. Storage errors reach
 * the caller: a creation the browser could not keep must not start.
 */

import { readStored, removeStored, STORAGE_KEYS, writeStored } from "@/lib/browser/storage";
import { isRecord, type UnknownRecord } from "@/lib/shared/json";

/**
 * What the person chose on the creation page for the Session's first turn: its model, as the
 * Session's model picker names it, and, on the mock model, the scenario and argument values whose
 * first input the message is. The creation page always stores a model; null is an intent stored
 * before the model was chosen there, which takes the starting model when Build delivers it. Build,
 * which owns the Session's models, checks the model name.
 */
type FirstMessageChoice = {
  model: string | null;
  mock: { scenario: string; arguments: Record<string, string> } | null;
};

/** A template-created Project in its TeamSpace and the first message of its Session, with stable retry identities. */
export type PendingProjectCreation = FirstMessageChoice & {
  teamSpaceId: string;
  name: string;
  prompt: string;
  idempotencyKey: string;
  messageIdempotencyKey: string;
};

/**
 * The creation intent of a Project: the first message of the Session that ProjectFlow starts once
 * the Project is READY, and the model it runs on. Build applies the model and sends the message
 * once. Its key is the message's ID, so a resend names the same message.
 */
export type ProjectIntent = FirstMessageChoice & { initialMessage: string; idempotencyKey: string };

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

function isMockChoice(value: unknown): value is NonNullable<FirstMessageChoice["mock"]> {
  return (
    isRecord(value) &&
    nonEmpty(value.scenario) &&
    isRecord(value.arguments) &&
    Object.values(value.arguments).every((argument) => typeof argument === "string")
  );
}

/** The model and mock input of a stored value, each null when absent; null when either is invalid. */
function readChoice(value: UnknownRecord): FirstMessageChoice | null {
  const model = value.model ?? null;
  const mock = value.mock ?? null;
  if ((model === null || nonEmpty(model)) && (mock === null || isMockChoice(mock))) return { model, mock };
  return null;
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
  const choice = readChoice(value);
  if (
    choice &&
    nonEmpty(value.teamSpaceId) &&
    nonEmpty(value.name) &&
    nonEmpty(value.prompt) &&
    nonEmpty(value.messageIdempotencyKey) &&
    nonEmpty(value.idempotencyKey)
  )
    return {
      ...choice,
      teamSpaceId: value.teamSpaceId,
      name: value.name,
      prompt: value.prompt,
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

export function readProjectIntent(projectId: string): ProjectIntent | null {
  const value = read(STORAGE_KEYS.projectIntent(projectId));
  const choice = value && readChoice(value);
  if (value && choice && nonEmpty(value.initialMessage) && nonEmpty(value.idempotencyKey))
    return { ...choice, initialMessage: value.initialMessage, idempotencyKey: value.idempotencyKey };
  if (value) removeStored("session", STORAGE_KEYS.projectIntent(projectId));
  return null;
}

export function storeProjectIntent(projectId: string, intent: ProjectIntent): void {
  writeStored("session", STORAGE_KEYS.projectIntent(projectId), JSON.stringify(intent));
}

export function clearProjectIntent(projectId: string): void {
  removeStored("session", STORAGE_KEYS.projectIntent(projectId));
}
