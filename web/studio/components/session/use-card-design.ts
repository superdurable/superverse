// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useMemo } from "react";
import type { DesignChanges } from "@/components/agent";
import { designChangeLines } from "@/components/flow/scope/flow-design";
import { useProcessDefinitions } from "@/lib/project/ProcessDefinitionsProvider";
import { useFlowDesign } from "@/lib/session/design/use-flow-design";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { studioRoutes } from "@/lib/shared/studio-routes";

/**
 * What the waiting card's Flow design changes in the current source, which is compared only when it is the
 * Session head's or the template's initial design, and the Flow tab that draws it.
 */
export function useCardDesign(): DesignChanges | undefined {
  const design = useFlowDesign(true);
  const { currentSource } = useProcessDefinitions();
  const session = useSessionResource();
  const head = session?.resource?.headCommit;
  const projectId = session?.projectId;
  const body = design?.phase === "waiting" ? design.body : null;
  return useMemo(() => {
    if (!body || !projectId) return undefined;
    const current = currentSource && currentSource.commit === head ? currentSource.graphs : null;
    const first = body.flows[0]?.flowType;
    return {
      lines: designChangeLines(body, current),
      href: first ? studioRoutes.projectSurface(projectId, "flow", { flowType: first }) : undefined,
    };
  }, [body, currentSource, head, projectId]);
}
