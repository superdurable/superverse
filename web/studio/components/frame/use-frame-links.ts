// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useSearchParams } from "next/navigation";
import { useCallback } from "react";
import type { StatusFix } from "@/lib/project/setup/environment-status";
import {
  studioRoutes,
  type ProjectEnvironment,
  type ProjectSurface,
  type ProjectTool,
} from "@/lib/shared/studio-routes";

/** The query a move between surfaces keeps: the version shown and the Process picked. */
const KEPT = ["v", "flowType"] as const;

/**
 * Links between the Builder's surfaces, versions and Processes. Moving to another surface keeps the
 * version and the Process (and the caller's `persistent` parameters, such as the mock's role lens) and
 * drops the selection of the surface left behind. Switching Preview | Live keeps the surface, its tool
 * and the Process and drops selections that named the other version. Picking a Process keeps the
 * surface and the version. A status's fix opens the Interface or Setup of its environment, or
 * Publishing, which shows both. A Release opens in Publishing, as for an Undo that rolls back to it.
 */
export function useFrameLinks(projectId: string, surface: ProjectSurface, persistent: readonly string[] = []) {
  const search = useSearchParams();
  const keep = useCallback(
    (names: readonly string[]) =>
      Object.fromEntries([...KEPT, ...persistent, ...names].map((name) => [name, search.get(name)])),
    [persistent, search],
  );
  const surfaceHref = useCallback(
    (next: ProjectSurface) => studioRoutes.projectSurface(projectId, next, keep(next === "tools" ? ["tool"] : [])),
    [keep, projectId],
  );
  const environmentHref = useCallback(
    (environment: ProjectEnvironment) =>
      studioRoutes.projectSurface(projectId, surface, {
        ...keep(surface === "tools" ? ["tool", "pinned"] : []),
        v: environment === "live" ? "live" : null,
      }),
    [keep, projectId, surface],
  );
  const toolHref = useCallback((tool: ProjectTool) => studioRoutes.tool(projectId, tool, keep([])), [keep, projectId]);
  const releaseHref = useCallback(
    (releaseId: string) => studioRoutes.publishingDeployment(projectId, releaseId, keep([])),
    [keep, projectId],
  );
  const pinnedToolHref = useCallback(
    (tool: ProjectTool) => studioRoutes.tool(projectId, tool, { ...keep([]), pinned: "1" }),
    [keep, projectId],
  );
  const processHref = useCallback(
    (process: string | null) =>
      studioRoutes.projectSurface(projectId, surface, {
        ...keep(surface === "tools" ? ["tool"] : []),
        flowType: process,
      }),
    [keep, projectId, surface],
  );
  const fixHref = useCallback(
    (environment: ProjectEnvironment, fix: StatusFix) => {
      if (fix.to === "publishing") return studioRoutes.tool(projectId, "publishing", keep([]));
      const query = { ...keep([]), v: environment === "live" ? "live" : null };
      if (fix.to === "interface") return studioRoutes.projectSurface(projectId, "interface", query);
      const href = studioRoutes.tool(projectId, "setup", query);
      return fix.anchor ? `${href}#${fix.anchor}` : href;
    },
    [keep, projectId],
  );
  return { surfaceHref, environmentHref, toolHref, releaseHref, pinnedToolHref, processHref, fixHref };
}
