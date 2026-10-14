// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useSyncExternalStore } from "react";
import {
  readStoredPreference,
  STORAGE_KEYS,
  subscribeStoredPreferences,
  writeStoredPreference,
} from "@/lib/browser/storage";
import { isProjectTool, movedToolOf, type ProjectTool } from "@/lib/shared/studio-routes";

function parse(stored: string | null): ProjectTool[] {
  try {
    const value: unknown = JSON.parse(stored ?? "[]");
    if (!Array.isArray(value)) return [];
    // A tab pinned before Connectors and Configuration became Setup opens Setup, once.
    const tools = value.map((item) => (typeof item === "string" ? (movedToolOf(item) ?? item) : item));
    return [...new Set(tools.filter((item): item is ProjectTool => isProjectTool(item)))];
  } catch {
    return [];
  }
}

/**
 * The tools pinned as tabs, remembered per browser and never shared. The stored string is the
 * snapshot, so every read of the same storage returns the same list identity for React.
 */
export function usePinnedTools() {
  const stored = useSyncExternalStore(
    subscribeStoredPreferences,
    () => readStoredPreference(STORAGE_KEYS.pinnedTools),
    () => null,
  );
  const pinned = parse(stored);
  const save = useCallback(
    (next: ProjectTool[]) => writeStoredPreference(STORAGE_KEYS.pinnedTools, JSON.stringify(next)),
    [],
  );
  return {
    pinned,
    pin: (tool: ProjectTool) => (pinned.includes(tool) ? undefined : save([...pinned, tool])),
    unpin: (tool: ProjectTool) => save(pinned.filter((item) => item !== tool)),
  };
}
