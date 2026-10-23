// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readStored, writeStored, removeStored, STORAGE_KEYS } from "@/lib/browser/storage";
import { getModelCredential, saveModelCredential, type ModelProvider } from "@/lib/api/model-credential";

/** Never serialize this value. It crosses client navigation in memory only. */
export type StartingCredential = {
  provider: ModelProvider;
  apiKey: string;
  endpoint: { baseUrl: string; customModelId: string; contextWindow: string; maxOutputTokens: string };
};
// A single active creation, replaced rather than accumulated across navigation.
let staged: { requestId: string; credential: StartingCredential } | null = null;
export function stageStartingCredential(requestId: string, credential: StartingCredential | null) {
  staged = credential ? { requestId, credential } : null;
}
export function takeStartingCredential(requestId: string) {
  const value = staged?.requestId === requestId ? staged.credential : null;
  staged = null;
  return value;
}
let accepted: {
  projectId: string;
  credential: StartingCredential | null;
  messageId: string;
  sendFirstMessage: boolean;
  attachmentIds: readonly string[];
  admission?: Promise<void>;
  sending?: boolean;
} | null = null;
export function acceptStartingCredential(value: NonNullable<typeof accepted>) {
  accepted = value;
}
export function takeAcceptedCredential(projectId: string) {
  if (accepted?.projectId !== projectId) return null;
  return accepted;
}

/** One admission survives React effect remounts; it never retries uncertain credential writes. */
export function admitStartingCredential(projectId: string) {
  const value = takeAcceptedCredential(projectId);
  if (!value) return null;
  if (value.admission) return value.admission;
  const credential = value.credential;
  value.credential = null;
  value.admission = (async () => {
    if (!credential) return;
    const signal = AbortSignal.timeout(30000);
    const info = await getModelCredential(projectId, credential.provider, signal);
    await saveModelCredential(
      projectId,
      credential.provider,
      info.revision,
      crypto.randomUUID(),
      credential.apiKey,
      signal,
      credential.endpoint,
    );
  })();
  return value.admission;
}
export function forgetAcceptedCredential(projectId: string) {
  if (accepted?.projectId === projectId) accepted = null;
}

/** Claims the automatic delivery once, including across component remounts. */
export function claimStartingMessage(projectId: string) {
  if (!accepted || accepted.projectId !== projectId || accepted.sending) return false;
  accepted.sending = true;
  return true;
}

/** Only the non-secret admission ID crosses reload; a key never enters this marker. */
export function markStartingDelivery(projectId: string, messageId: string) {
  writeStored("session", STORAGE_KEYS.platformFirstDelivery(projectId), messageId);
}
export function hasStartingDelivery(projectId: string) {
  if (typeof window === "undefined") return false;
  try {
    return readStored("session", STORAGE_KEYS.platformFirstDelivery(projectId)) !== null;
  } catch {
    return true;
  }
}
export function clearStartingDelivery(projectId: string) {
  removeStored("session", STORAGE_KEYS.platformFirstDelivery(projectId));
}
