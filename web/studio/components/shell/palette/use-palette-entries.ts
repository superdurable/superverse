// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useSyncExternalStore } from "react";

/** One place Jump to can open: its words, a hint of what it is, and where it leads. */
export interface PaletteEntry {
  label: string;
  hint: string;
  href: string;
}

let current: readonly PaletteEntry[] = [];
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/**
 * Offers `entries` to Jump to while the calling page is mounted, such as the open Project's
 * Processes, surfaces and tools. The last page to offer entries owns the list.
 */
export function usePaletteEntries(entries: readonly PaletteEntry[]) {
  const key = JSON.stringify(entries);
  useEffect(() => {
    current = JSON.parse(key) as PaletteEntry[];
    listeners.forEach((listener) => listener());
    return () => {
      current = [];
      listeners.forEach((listener) => listener());
    };
  }, [key]);
}

/** The entries the mounted page offers. */
export function usePageEntries(): readonly PaletteEntry[] {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => current,
  );
}
