// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { readStoredPreference, STORAGE_KEYS } from "@/lib/browser/storage";
import { useRemoteAgentStatus } from "@/components/session/executor/use-remote-agent-status";
import { ApiError } from "@/lib/api/transport/api-error";
import {
  clearProjectIntent,
  PROJECT_MESSAGE_DELIVERY,
  readProjectIntent,
  storeProjectIntent,
  encodedProjectIntent,
  subscribeProjectIntents,
} from "@/lib/project/project-intents";
import { AgentStatus } from "@/lib/session/agent-types";
import { commandFailureText } from "@/lib/session/session-commands";
import type { useProjectSession } from "@/lib/session/SessionProvider";

const CONNECT_TO_SEND = "Connect your local agent to send. Your draft stays editable.";
const UNCERTAIN_DELIVERY =
  "Your first message may already have arrived. Check the conversation before sending a different message.";

/** Local creation retries its persisted first message with the same Session admission key. */
export function useLocalCreation(
  state: ReturnType<typeof useProjectSession>,
  text: string,
  setText: (text: string) => void,
) {
  const encoded = useSyncExternalStore(
    subscribeProjectIntents,
    () => encodedProjectIntent(state.projectId),
    () => null,
  );
  const intent = useMemo(() => (encoded ? readProjectIntent(state.projectId) : null), [encoded, state.projectId]);
  const [error, setError] = useState("");
  const sending = useRef(false);
  const { online } = useRemoteAgentStatus();
  const remote = state.snapshot?.description.executor?.mode === "remote";
  const pending = intent !== null;
  const waitingForConnection = pending && !remote;
  const retrying = intent?.delivery === PROJECT_MESSAGE_DELIVERY.SENDING;
  const uncertain = retrying && !!error && text.trim() !== intent.prompt.trim();
  const finish = useCallback(
    (sentText: string) => {
      clearProjectIntent(state.projectId);
      if ((readStoredPreference(STORAGE_KEYS.composerDraft(state.projectId)) ?? "").trim() === sentText.trim())
        setText("");
    },
    [setText, state.projectId],
  );

  const send = useCallback(
    async (message: string, attachmentIds: readonly string[]) => {
      const owned = readProjectIntent(state.projectId);
      if (!owned) return state.send(message, attachmentIds);
      if (!remote) throw new Error(CONNECT_TO_SEND);
      if (sending.current) throw new Error("Your first message is already being sent.");
      if (owned.delivery === PROJECT_MESSAGE_DELIVERY.SENDING && message !== owned.prompt)
        throw new Error(UNCERTAIN_DELIVERY);
      sending.current = true;
      setError("");
      // Persist the exact retry payload before the RPC; Session recognizes the same admission key.
      try {
        const admitted = { ...owned, prompt: message, delivery: PROJECT_MESSAGE_DELIVERY.SENDING };
        storeProjectIntent(admitted);
      } catch (reason) {
        sending.current = false;
        setError(reason instanceof Error ? reason.message : "Could not save the first message before sending");
        throw reason;
      }
      try {
        await state.send(message, attachmentIds, owned.messageIdempotencyKey);
        finish(message);
      } catch (reason) {
        const definite =
          reason instanceof ApiError &&
          (reason.errorInfo?.reason === "AGENT_COMMAND_NOT_ADMITTED" ||
            reason.errorInfo?.reason === "AGENT_COMMAND_NOT_DISPATCHED");
        if (definite) {
          const unsent = { ...owned, delivery: PROJECT_MESSAGE_DELIVERY.UNSENT };
          storeProjectIntent(unsent);
        }
        setError(commandFailureText(reason));
        throw reason;
      } finally {
        sending.current = false;
      }
    },
    [finish, remote, state],
  );

  useEffect(() => {
    if (
      !intent ||
      !remote ||
      !online ||
      !state.snapshotCurrent ||
      state.status !== "SESSION_STATUS_ACTIVE" ||
      state.snapshot?.description.status === AgentStatus.INITIALIZING
    )
      return;
    if (sending.current || state.mutating || error) return;
    // An example or a cleared draft at connection has no automatic first message.
    if (!retrying && (!intent.prompt || !text.trim())) {
      clearProjectIntent(state.projectId);
      return;
    }
    let cancelled = false;
    queueMicrotask(() => {
      if (!cancelled) void send(retrying ? intent.prompt : text.trim(), []).catch(() => undefined);
    });
    return () => {
      cancelled = true;
    };
  }, [error, intent, online, remote, send, state, text, retrying]);

  return {
    projectId: state.projectId,
    pending,
    waitingForConnection,
    send,
    error,
    uncertain: uncertain && !state.mutating,
    acknowledgeUncertain: () => {
      clearProjectIntent(state.projectId);
      setError("");
    },
    sendDisabledReason: waitingForConnection
      ? CONNECT_TO_SEND
      : retrying
        ? "Reload to retry your first message safely."
        : null,
  };
}
