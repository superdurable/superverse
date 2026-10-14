// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useSearchParams, useSelectedLayoutSegment } from "next/navigation";
import { environmentOf, isProjectSurface, isProjectTool } from "@/lib/shared/studio-routes";

/**
 * Where the Builder is, for a frame rendered by the Builder's layout: the surface one segment below
 * it, the open tool and the version shown. The layout outlives a move between surfaces, so the
 * frame and its chat stay mounted while only the surface changes. A segment the page is about to
 * redirect from reads as the Flow.
 */
export function useBuilderLocation() {
  const segment = useSelectedLayoutSegment();
  const search = useSearchParams();
  const tool = search.get("tool");
  return {
    surface: segment && isProjectSurface(segment) ? segment : "flow",
    tool: isProjectTool(tool) ? tool : "publishing",
    environment: environmentOf(search.get("v")),
    /** The Process (Flow Type) the picker narrows every surface to, or null for all of them. */
    process: search.get("flowType") || null,
    /** The tool is open as a pinned tab, without the Tools list. */
    pinnedView: segment === "tools" && search.get("pinned") === "1",
  } as const;
}
