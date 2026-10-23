// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readStoredPreference, STORAGE_KEYS } from "@/lib/browser/storage";
import { useCallback, useEffect, useRef, useState } from "react";
import type { useLocalCreation } from "./use-local-creation";
import type { useProjectSession } from "@/lib/session/SessionProvider";
import {
  admitStartingCredential,
  claimStartingMessage,
  markStartingDelivery,
  hasStartingDelivery,
  clearStartingDelivery,
  forgetAcceptedCredential,
  takeAcceptedCredential,
} from "@/lib/session/credentials/starting-credential";
import type { AttachmentTray } from "@/lib/session/attachments/use-attachment-tray";
import { errorMessage } from "@/lib/api/transport/api-error";
import { AgentStatus } from "@/lib/session/agent-types";
import { ModelCredentialStatus } from "@/lib/api/model-credential";

/** A platform first message stays editable until the Project's key is admitted. Reload never auto-sends. */
export function useStartingCreation(
  state: ReturnType<typeof useProjectSession>,
  text: string,
  setText: (text: string) => void,
  local: ReturnType<typeof useLocalCreation>,
  tray: AttachmentTray,
) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(() => hasStartingDelivery(state.projectId));
  const [admitted, setAdmitted] = useState(false);
  const current = useRef({ state, text, setText });
  useEffect(() => {
    current.current = { state, text, setText };
  });
  const pending = takeAcceptedCredential(state.projectId);
  const canAdmit =
    state.snapshotCurrent &&
    state.status === "SESSION_STATUS_ACTIVE" &&
    !!state.snapshot?.description &&
    state.snapshot.description.status !== AgentStatus.INITIALIZING;
  useEffect(() => {
    if (!pending || !canAdmit) return;
    let live = true;
    queueMicrotask(() => {
      if (!live) return;
      setBusy(true);
      const admission = admitStartingCredential(state.projectId);
      void admission
        ?.then(async () => {
          await current.current.state.credential.refresh();
          if (!live) return;
          setAdmitted(true);
          setBusy(false);
        })
        .catch(async (reason) => {
          await current.current.state.credential.refresh();
          if (!live) return;
          setError(errorMessage(reason, "Could not save the provider key"));
          setBusy(false);
          forgetAcceptedCredential(state.projectId);
        });
    });
    return () => {
      live = false;
    };
  }, [pending, canAdmit, state.projectId]);
  const ready =
    state.snapshot?.description.model === "mock" ||
    state.snapshot?.description.executor?.mode === "remote" ||
    state.credential.info?.status === ModelCredentialStatus.MODEL_CREDENTIAL_STATUS_ACTIVE;
  const deliver = useCallback(
    async (message: string, ids: readonly string[]) => {
      const owned = takeAcceptedCredential(state.projectId);
      if (!owned) return local.send(message, ids);
      if (busy || !ready || !admitted) throw new Error("Wait for the Project key before sending.");
      if (!claimStartingMessage(state.projectId)) throw new Error("The first message is already being sent.");
      setBusy(true);
      let attempted = false;
      try {
        markStartingDelivery(state.projectId, owned.messageId);
        attempted = true;
        await state.send(message, ids, owned.messageId);
        clearStartingDelivery(state.projectId);
        if (current.current.text.trim() === message.trim()) current.current.setText("");
        forgetAcceptedCredential(state.projectId);
      } catch (reason) {
        setError(
          errorMessage(
            reason,
            "The first message may already have arrived. Check the conversation before sending again.",
          ),
        );
        setUncertain(attempted);
        forgetAcceptedCredential(state.projectId);
        throw reason;
      } finally {
        setBusy(false);
      }
    },
    [admitted, busy, local, ready, state],
  );
  useEffect(() => {
    if (!pending || busy || !ready || !admitted || !canAdmit || pending.sending || state.mutating || !tray.ready)
      return;
    const message = text.trim();
    // useSyncExternalStore first hydrates with its server snapshot; an empty render must not discard a stored draft.
    if (!message && readStoredPreference(STORAGE_KEYS.composerDraft(state.projectId))) return;
    if (!pending.sendFirstMessage || (!message && !tray.attachmentIds.length)) {
      forgetAcceptedCredential(state.projectId);
      return;
    }
    void tray.send((ids) => deliver(message, ids)).catch(() => undefined);
  }, [admitted, busy, canAdmit, deliver, pending, ready, state.mutating, state.projectId, text, tray]);
  const missingKey =
    !ready &&
    !local.pending &&
    state.snapshot?.description.executor?.mode !== "remote" &&
    state.snapshot?.description.model !== "mock";
  return {
    ...local,
    send: deliver,
    pending: local.pending || !!pending || busy,
    error: error || local.error,
    uncertain: uncertain || local.uncertain,
    acknowledgeUncertain: () => {
      try {
        clearStartingDelivery(state.projectId);
      } catch {
        setError("Could not clear the first-message check in this browser.");
        return;
      }
      setUncertain(false);
      setError("");
      local.acknowledgeUncertain();
    },
    sendDisabledReason:
      local.sendDisabledReason ??
      (busy || (!!pending && !admitted)
        ? "Saving the Project key and sending your first message…"
        : uncertain
          ? "Check the conversation before sending again."
          : missingKey
            ? "Set this provider's API key in the Agent picker to send. Your draft stays editable."
            : null),
  };
}
