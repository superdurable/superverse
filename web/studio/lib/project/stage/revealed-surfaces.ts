// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useSyncExternalStore } from "react";
import {
  readStoredPreference,
  STORAGE_KEYS,
  subscribeStoredPreferences,
  writeStoredPreference,
} from "@/lib/browser/storage";
import { isRecord } from "@/lib/shared/json";
import { isProjectSurface, type ProjectSurface } from "@/lib/shared/studio-routes";
import type { ProjectStageState } from "./ProjectStageProvider";

/** The tabs a Project has shown in this browser, and those not visited since they appeared. */
interface TabRecord {
  revealed: ProjectSurface[];
  unvisited: ProjectSurface[];
}

function surfacesOf(value: unknown): ProjectSurface[] {
  return Array.isArray(value)
    ? value.filter((item): item is ProjectSurface => typeof item === "string" && isProjectSurface(item))
    : [];
}

function parse(stored: string | null): TabRecord | null {
  if (stored === null) return null;
  try {
    const value: unknown = JSON.parse(stored);
    if (isRecord(value)) return { revealed: surfacesOf(value.revealed), unvisited: surfacesOf(value.unvisited) };
  } catch {
    // A corrupt record starts again from what is available now.
  }
  return null;
}

/**
 * The record after this render: a Project shown here for the first time records what it shows as seen;
 * a tab that appears later joins the shown ones and stays new until it is open.
 */
function nextRecord(
  record: TabRecord | null,
  available: readonly ProjectSurface[] | null,
  current: ProjectSurface | null,
): TabRecord | null {
  if (record === null) return available && { revealed: [...available], unvisited: [] };
  const appeared = (available ?? []).filter((surface) => !record.revealed.includes(surface));
  return {
    revealed: [...record.revealed, ...appeared],
    unvisited: [...record.unvisited, ...appeared].filter((surface) => surface !== current),
  };
}

/**
 * The surfaces a builder's Builder has something to show on, past the first conversation: the Flow from
 * the switch; Runs, Goals and Tools once processes are built from the agent's own push; the Inbox once a run
 * waits for a person; the Interface once the Preview has served a page of the agent's push, or Live runs.
 */
export function availableSurfaces(state: ProjectStageState, waiting: number): ProjectSurface[] {
  const surfaces: ProjectSurface[] = ["flow"];
  if (state.hasOwnProcesses) surfaces.push("runs", "goals", "tools");
  if (waiting > 0) surfaces.push("inbox");
  if (state.previewServed || state.hasPublished) surfaces.push("interface");
  return surfaces;
}

/**
 * The Builder's tabs: those with something to show now, kept once they appeared in this browser, so a tab
 * never goes away again, and the surface open now, which a link may name before its tab appears. `fresh`
 * holds the tabs that appeared since the Project was first shown here and were not open since.
 * `available` is null while the Project's facts are read, and then nothing is recorded.
 */
export function useRevealedSurfaces(
  projectId: string,
  available: readonly ProjectSurface[] | null,
  current: ProjectSurface | null,
): { shown: ReadonlySet<ProjectSurface>; fresh: ReadonlySet<ProjectSurface> } {
  const key = STORAGE_KEYS.projectTabs(projectId);
  const record = parse(
    useSyncExternalStore(
      subscribeStoredPreferences,
      () => readStoredPreference(key),
      () => null,
    ),
  );
  const next = nextRecord(record, available, current);
  const encoded = next && JSON.stringify(next);
  useEffect(() => {
    if (encoded !== null && encoded !== readStoredPreference(key)) writeStoredPreference(key, encoded);
  }, [encoded, key]);
  return {
    shown: new Set([...(next?.revealed ?? []), ...(available ?? []), ...(current ? [current] : [])]),
    fresh: new Set(next?.unvisited ?? []),
  };
}
