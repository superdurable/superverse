// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback } from "react";

/**
 * The text composer's send, with what the message is about, when a surface named it, leading the text. A
 * message is how the person changes what the agent builds: it queues while a turn runs, and a requirements
 * card a Session still waits at takes it as its reply.
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
  return useCallback(
    (text: string) => {
      markSent();
      return send(about ? `About: ${about}\n${text}` : text).then(clearAbout);
    },
    [about, clearAbout, markSent, send],
  );
}
