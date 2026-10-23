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
  /** The Teamspace last open in this browser, which `/` opens again. */
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
  /** The creation page's resumable pointer to a pending local-agent Project. */
  projectIntent: "superversev2.project-intent.v1",
  /** A local-agent Project keeps its pending first-message admission separately from other Projects. */
  localProjectIntent: (projectId: string) => `superversev2.local-project-intent.v1:${projectId}`,
  /** A provider-backed creation's first Send crossed the RPC boundary; no credential or message content. */
  platformFirstDelivery: (projectId: string) => `superversev2.platform-first-delivery.v1:${projectId}`,
  /** The publish wizard's unsent reason to publish without running on Preview, in session storage. */
  publishSkipReason: (projectId: string) => `superversev2.publish-skip-reason.v1:${projectId}`,
  /** The card version whose Flow design the Flow tab last showed of a Project, so a reload does not move the person. */
  designShown: (projectId: string) => `superversev2.design-shown.v1:${projectId}`,
  /** Whether one surface's "More details" fold is open, per browser. */
  moreDetails: (surface: string) => `superversev2.more-details.v1:${surface}`,
  /** How far a Project created in this browser has come from its first conversation; absent for any other. */
  projectStage: (projectId: string) => `superversev2.project-stage.v1:${projectId}`,
  /** The Builder tabs a Project has shown in this browser and those not visited since they appeared, as JSON. */
  projectTabs: (projectId: string) => `superversev2.project-tabs.v1:${projectId}`,
  /** The unsent composer text; local creation may send its current draft once after connection. */
  composerDraft: (projectId: string) => `superversev2.composer-draft.v1:${projectId}`,
  /**
   * The attachments of a composer that its message has not sent yet, in session storage: their IDs and
   * names, never their bytes. `scope` is the Project, or `start:<Teamspace>` for the start page.
   */
  composerAttachments: (scope: string) => `superversev2.composer-attachments.v1:${scope}`,
  /** Whether this browser showed the line that the AI model reads attached files. */
  attachmentDisclosure: "superversev2.attachment-disclosure.v1",
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

export function removeStoredPreference(key: string): void {
  pagePreferences.delete(key);
  try {
    removeStored("local", key);
  } catch {
    // Blocked storage holds nothing to remove.
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
