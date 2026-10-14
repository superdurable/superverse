// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Browser storage by area and key. Every key Studio stores is listed here with its literal, which
 * never changes: a new literal would silently drop what browsers already hold.
 */
export const STORAGE_KEYS = {
  /** Light, dark or auto; app/layout.tsx applies it before the first paint. */
  theme: "dex-ai-platform.theme",
  /** The sidebar's width on Home and the other account pages, and inside a Project; the rail width when folded. */
  sidebarHomeWidth: "superverse.studio.sidebar-home-w.v1",
  sidebarProjectWidth: "superverse.studio.sidebar-project-w.v1",
  /** The Project chat column's width; the rail width when folded. */
  chatWidth: "superverse.studio.chat-w.v1",
  /** The Tools list's width; the rail width when folded. */
  toolsWidth: "superverse.studio.tools-w.v1",
  /** The TeamSpace last open in this browser, which `/` opens again. */
  teamSpace: "superverse.studio.team-space.v1",
  /** The tools pinned as tabs, as a JSON array of tool names. */
  pinnedTools: "superverse.studio.pinned-tools.v1",
  listPaneWidth: "dex-web.v2-list-w",
  drawerPaneWidth: "dex-web.v2-drawer-w",
  stepPanelWidth: "dex-web.v2-panel-w",
  selectedEventWidth: "dex.run.selected-event-width",
  diagnosticsTab: "dex.run.selected-tab",
  /** The Project being created, in session storage. */
  projectCreation: "superversev2.project-create.v3",
  /** The publish wizard's unsent reason to publish without running on Preview, in session storage. */
  publishSkipReason: (projectId: string) => `superversev2.publish-skip-reason.v1:${projectId}`,
  /** The card version whose Flow design the Flow tab last showed of a Project, so a reload does not move the person. */
  designShown: (projectId: string) => `superversev2.design-shown.v1:${projectId}`,
  /** What to do once a Project is ready, in session storage. */
  projectIntent: (projectId: string) => `superversev2.project-intent.v3:${projectId}`,
} as const;

type StorageArea = "local" | "session";

function storageOf(area: StorageArea): Storage {
  return area === "local" ? window.localStorage : window.sessionStorage;
}

/** Throws when the browser blocks storage. */
export function readStored(area: StorageArea, key: string): string | null {
  return storageOf(area).getItem(key);
}

/** Throws when the browser blocks storage or the area is full. */
export function writeStored(area: StorageArea, key: string, value: string): void {
  storageOf(area).setItem(key, value);
}

/** Throws when the browser blocks storage. */
export function removeStored(area: StorageArea, key: string): void {
  storageOf(area).removeItem(key);
}

/**
 * Per-browser view preferences such as pane widths and the selected tab. Private browsing or blocked
 * storage keeps a write for this page only: a preference is not worth failing the page.
 */
export function readStoredPreference(key: string): string | null {
  try {
    return readStored("local", key) ?? pagePreferences.get(key) ?? null;
  } catch {
    return pagePreferences.get(key) ?? null;
  }
}

/** Preferences that storage refused, kept for this page only. */
const pagePreferences = new Map<string, string>();
const preferenceListeners = new Set<() => void>();

export function writeStoredPreference(key: string, value: string): void {
  try {
    writeStored("local", key, value);
    pagePreferences.delete(key);
  } catch {
    pagePreferences.set(key, value);
  }
  preferenceListeners.forEach((listener) => listener());
}

/**
 * Calls `listener` after every preference write in this page and in other tabs, for a
 * useSyncExternalStore read of a preference. Returns the unsubscribe function.
 */
export function subscribeStoredPreferences(listener: () => void): () => void {
  preferenceListeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    preferenceListeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}
