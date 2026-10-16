// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useSyncExternalStore } from "react";

/**
 * Whether the Flow tab shows the Process Brief now, so the chat leaves out its pointer to it: the Brief
 * counts itself while it is mounted.
 */
let mounted = 0;
const listeners = new Set<() => void>();

function changed(by: number) {
  mounted += by;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** True while a Process Brief is on screen. */
export function useBriefOnScreen(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => mounted > 0,
    () => false,
  );
}

/** Counts the calling Brief as on screen while it is mounted. */
export function useBriefShown(): void {
  useEffect(() => {
    changed(1);
    return () => changed(-1);
  }, []);
}
