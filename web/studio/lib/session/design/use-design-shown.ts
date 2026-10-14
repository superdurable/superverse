// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useSyncExternalStore } from "react";
import {
  readStoredPreference,
  STORAGE_KEYS,
  subscribeStoredPreferences,
  writeStoredPreference,
} from "@/lib/browser/storage";

/**
 * Whether the Flow tab already showed a card version's design in this browser, and the call that records
 * it. The Flow tab shows a design by itself once per card version, and its tab entry carries a marker until
 * then, so a reload or a later visit does not move the person again.
 */
export function useDesignShown(projectId: string, key: string | null): { shown: boolean; markShown: () => void } {
  const storageKey = STORAGE_KEYS.designShown(projectId);
  const stored = useSyncExternalStore(
    subscribeStoredPreferences,
    () => readStoredPreference(storageKey),
    () => null,
  );
  const markShown = useCallback(() => {
    if (key !== null && readStoredPreference(storageKey) !== key) writeStoredPreference(storageKey, key);
  }, [key, storageKey]);
  return { shown: key !== null && stored === key, markShown };
}
