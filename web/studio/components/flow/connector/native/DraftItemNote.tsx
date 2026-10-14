// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useSyncExternalStore } from "react";
import type { ConfigurationDraftQueue } from "@/lib/api/flow/draft/configuration-draft";
import { Button } from "@/components/ui/Button";

/** The queue's state, which every reader of one queue shares. */
export function useDraftSnapshot(queue: ConfigurationDraftQueue) {
  return useSyncExternalStore(queue.subscribe, queue.getSnapshot, queue.getSnapshot);
}

/**
 * What became of one item's autosave: saving, or changed elsewhere since this tab read it, where the
 * person keeps theirs or takes the other change; nothing is overwritten silently.
 */
export function DraftItemNote({
  queue,
  itemKey,
  onTheirs,
}: {
  queue: ConfigurationDraftQueue;
  itemKey: string;
  /** Shows the other change once the person takes it. */
  onTheirs: () => void;
}) {
  const snapshot = useDraftSnapshot(queue);
  if (snapshot.conflicts.some((conflict) => conflict.key === itemKey))
    return (
      <div
        role="alert"
        className="flex flex-wrap items-center gap-1.5 text-[11.5px] text-busy"
        data-testid="draft-conflict"
      >
        Changed elsewhere:
        <Button size="sm" onClick={() => queue.resolve(itemKey, "yours")}>
          Keep yours
        </Button>
        <Button
          size="sm"
          onClick={() => {
            queue.resolve(itemKey, "theirs");
            onTheirs();
          }}
        >
          Take theirs
        </Button>
      </div>
    );
  if (snapshot.pending.includes(itemKey))
    return (
      <p role="status" className="text-[11px] text-ink-faint">
        {snapshot.status === "failed" ? "Not saved" : "Saving…"}
      </p>
    );
  return null;
}
