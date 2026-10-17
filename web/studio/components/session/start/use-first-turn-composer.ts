// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useState, useSyncExternalStore } from "react";
import {
  readStoredPreference,
  removeStoredPreference,
  STORAGE_KEYS,
  subscribeStoredPreferences,
  writeStoredPreference,
} from "@/lib/browser/storage";

/**
 * The composer's text: what the person typed, or took back into it from the queue, kept per Project. An
 * unsent text also stays in this browser, so a reload keeps it and a Project opened from an example starts
 * with what the person had typed on the start page; nothing sends it until the person does, and sending
 * clears it.
 */
export function useComposerText(projectId: string) {
  const key = STORAGE_KEYS.composerDraft(projectId);
  const stored = useSyncExternalStore(
    subscribeStoredPreferences,
    () => readStoredPreference(key),
    () => null,
  );
  const [typed, setTyped] = useState<{ projectId: string; text: string } | null>(null);
  const text = typed?.projectId === projectId ? typed.text : (stored ?? "");
  const setText = useCallback(
    (next: string) => {
      setTyped({ projectId, text: next });
      if (next) writeStoredPreference(key, next);
      else removeStoredPreference(key);
    },
    [key, projectId],
  );
  return [text, setText] as const;
}
