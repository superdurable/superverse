// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { studioRoutes } from "@/lib/shared/studio-routes";
import type { Remedy } from "./problem";

/**
 * The links that carry out a Project's remedies: Setup of the environment the problem concerns, the
 * Agent's conversation for a change to the application or its workspace, and the connections to
 * authorize again. A page adds its own actions, such as Try again, beside them.
 */
export function projectRemedyLinks(
  projectId: string,
  setupHref: string = studioRoutes.previewConfiguration(projectId),
): Partial<Record<Remedy, { href: string }>> {
  const agent = { href: studioRoutes.projectSurface(projectId, "flow") };
  return {
    FAILURE_REMEDY_OPEN_SETUP: { href: setupHref },
    FAILURE_REMEDY_ASK_AGENT: agent,
    FAILURE_REMEDY_FREE_WORKSPACE: agent,
    FAILURE_REMEDY_REAUTHORIZE: { href: studioRoutes.previewConfiguration(projectId) },
  };
}
