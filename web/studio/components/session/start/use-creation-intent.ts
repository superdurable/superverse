// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { clearProjectIntent, readProjectIntent, type ProjectIntent } from "@/lib/project/project-intents";
import { AgentStatus, isAgentModel, type AgentModel } from "@/lib/session/agent-types";
import { useProjectSession } from "@/lib/session/SessionProvider";

/** The stored creation intent of the Project; a browser that blocks storage holds none. */
function storedIntent(projectId: string): ProjectIntent | null {
  try {
    return readProjectIntent(projectId);
  } catch {
    return null;
  }
}

/** Forgets the settled intent; one that storage keeps settles again on the next read. */
function settleIntent(projectId: string): void {
  try {
    clearProjectIntent(projectId);
  } catch {
    // Kept: the conversation that settled it settles it again.
  }
}

/**
 * Delivers the creation intent that the creation page stored as the first turn of the Project's
 * Session. Once the new Session waits for its first message with an empty conversation, Studio
 * applies the model the person chose, when it differs from the Session's (free before the first
 * message), and then sends the message with the intent's key as its ID. An intent stored without a
 * model, or with a name the picker does not offer, takes `startingModel`, which is null while it
 * is still being read; delivery waits for it. A conversation that holds a message settles the
 * intent, so a reload never sends it again, and a send whose answer was lost can at worst queue
 * the message twice.
 *
 * A model change or a send that fails is not repeated: the intent is kept, `failed` turns true,
 * the page shows the command's error and the composer takes the message back as a draft, which
 * `send` delivers under the intent's key when the person retries, with the model its picker shows.
 */
export function useCreationIntent(startingModel: AgentModel | null): {
  /** The intent while its message is not sent, for the mock composer's scenario and the retry. */
  pending: ProjectIntent | null;
  /** Whether the delivery of `pending` failed, so that the person sends it. */
  failed: boolean;
  /** The composer's send: the intent's message while it is pending, otherwise a new message. */
  send: (text: string) => Promise<unknown>;
} {
  const { projectId, snapshot, status, updateModel, send: sendMessage } = useProjectSession();
  const stored = useMemo(() => storedIntent(projectId), [projectId]);
  const [sentKey, setSentKey] = useState<string | null>(null);
  const [failedKey, setFailedKey] = useState<string | null>(null);
  const deliveredKey = useRef<string | null>(null);
  const description = snapshot?.description ?? null;
  const settled =
    snapshot !== null &&
    (snapshot.description.lastSequence > 0 || snapshot.queued.length > 0 || snapshot.steered.length > 0);
  const waiting = status === "SESSION_STATUS_ACTIVE" && description?.status === AgentStatus.WAITING_FOR_MESSAGE;
  const pending = snapshot !== null && !settled && stored !== null && stored.idempotencyKey !== sentKey ? stored : null;

  useEffect(() => {
    if (stored && settled) settleIntent(projectId);
  }, [projectId, settled, stored]);

  const model = pending && (pending.model !== null && isAgentModel(pending.model) ? pending.model : startingModel);

  useEffect(() => {
    if (!pending || !model || !description || !waiting || deliveredKey.current === pending.idempotencyKey) return;
    deliveredKey.current = pending.idempotencyKey;
    const { initialMessage, idempotencyKey } = pending;
    void (async () => {
      if (model !== description.model) await updateModel(model, description.modelRevision);
      await sendMessage(initialMessage, idempotencyKey);
    })().then(
      () => {
        setSentKey(idempotencyKey);
        settleIntent(projectId);
      },
      () => setFailedKey(idempotencyKey),
    );
  }, [description, model, pending, projectId, sendMessage, updateModel, waiting]);

  const send = useCallback(
    async (text: string) => {
      if (!pending) return sendMessage(text);
      await sendMessage(text, pending.idempotencyKey);
      setSentKey(pending.idempotencyKey);
      settleIntent(projectId);
    },
    [pending, projectId, sendMessage],
  );
  return { pending, failed: pending !== null && pending.idempotencyKey === failedKey, send };
}
