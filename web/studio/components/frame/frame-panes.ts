// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readStoredPreference, STORAGE_KEYS, writeStoredPreference } from "@/lib/browser/storage";
import type { PaneBounds } from "@/components/ui/pane/PaneResizeHandle";

/**
 * The chat column: 400px, a 40px rail, 280 to 560px open; a drag below 230px snaps to the rail. Its
 * width variable sits on the frame, so the bar's left block lines up with it.
 */
export const CHAT_PANE: PaneBounds = {
  cssVariable: "--chat-w",
  edge: "end",
  storageKey: STORAGE_KEYS.chatWidth,
  defaultWidth: 400,
  minWidth: 280,
  maxWidth: 560,
  maxFraction: 0.6,
  rail: { width: 40, snapBelow: 230 },
  dragFallback: () => 400,
};

/** The Tools list: 200px, 150 to 280px open, folding to a 48px rail of icons below 120px. */
export const TOOLS_PANE: PaneBounds = {
  cssVariable: "--tools-w",
  edge: "end",
  storageKey: STORAGE_KEYS.toolsWidth,
  defaultWidth: 200,
  minWidth: 150,
  maxWidth: 280,
  maxFraction: 0.4,
  rail: { width: 48, snapBelow: 120 },
  dragFallback: () => 200,
};

/** Opens the chat column at its default width when it is folded, so what was handed to it is seen. */
export function openChatColumn(): void {
  const stored = Number(readStoredPreference(CHAT_PANE.storageKey));
  if (Number.isFinite(stored) && stored > 0 && stored < (CHAT_PANE.rail?.snapBelow ?? 0))
    writeStoredPreference(CHAT_PANE.storageKey, String(CHAT_PANE.defaultWidth));
}
