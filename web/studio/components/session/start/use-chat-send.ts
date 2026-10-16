// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback } from "react";
import { useProjectSession } from "@/lib/session/SessionProvider";

/**
 * The text composer's send, with what the message is about, when a surface named it, leading the text. While
 * a requirements card waits, the message answers it as the changes the person asks for (Request changes), which
 * the agent answers with a revised card; otherwise it goes through the creation intent (`send`), which delivers
 * a pending first message.
 */
export function useChatSend({
  send,
  about,
  clearAbout,
  markSent,
}: {
  send: (text: string) => Promise<unknown>;
  about: string | null;
  clearAbout: () => void;
  markSent: () => void;
}): (text: string) => Promise<void> {
  const { snapshot, decideRequirements } = useProjectSession();
  const pending = snapshot?.description.pendingUserInput;
  const card = pending?.kind === "requirements" ? pending.callId : null;
  return useCallback(
    (text: string) => {
      markSent();
      const message = about ? `About: ${about}\n${text}` : text;
      const sent =
        card === null ? send(message) : decideRequirements(card, { isConfirmed: false, requestedChanges: message });
      return sent.then(clearAbout);
    },
    [about, card, clearAbout, decideRequirements, markSent, send],
  );
}
