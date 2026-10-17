// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { getProject } from "@/lib/api/project";
import { ApiError } from "@/lib/api/transport/api-error";
import { useResourceWatch } from "./use-resource-watch";

type ProjectResource = ReturnType<typeof useResourceWatch<"project">>;
const ProjectResourceContext = createContext<ProjectResource | undefined>(undefined);

// One page owns one Project Watch. Shell, publishing and configuration share its
// authoritative snapshot; none creates a parallel Project read/polling loop.
export function ProjectResourceProvider({ projectId, children }: { projectId: string; children: ReactNode }) {
  const watch = useResourceWatch("project", projectId, projectId);
  const refusal = useRefusalReason(projectId, Boolean(watch?.disconnected && !watch.snapshot && !watch.error));
  const value = watch && refusal ? { ...watch, error: refusal } : watch;
  return <ProjectResourceContext.Provider value={value}>{children}</ProjectResourceContext.Provider>;
}

/**
 * Why a Watch refused before its first snapshot, which EventSource cannot tell: one Project read
 * names it, such as a caller who is not the Project's member. Empty while unknown or not refused.
 */
function useRefusalReason(projectId: string, refused: boolean): string {
  const [reason, setReason] = useState({ projectId, text: "" });
  useEffect(() => {
    if (!refused) return;
    const controller = new AbortController();
    const settle = (text: string) => setReason({ projectId, text });
    getProject({ projectId }, controller.signal).then(
      () => settle("The project's live updates did not connect. Try again."),
      (error: unknown) => {
        if (controller.signal.aborted) return;
        if (error instanceof ApiError && error.status === 403)
          settle("You are not a member of this project. Ask its owner to add you.");
        else if (error instanceof ApiError && error.status === 404) settle("This project does not exist.");
        else settle(error instanceof Error ? error.message : "The project could not be read.");
      },
    );
    return () => controller.abort();
  }, [projectId, refused]);
  return refused && reason.projectId === projectId ? reason.text : "";
}
export function useProjectResource() {
  const value = useContext(ProjectResourceContext);
  if (value === undefined) throw new Error("Project resource owner is missing");
  return value;
}
