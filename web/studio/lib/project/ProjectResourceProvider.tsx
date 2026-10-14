// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, type ReactNode } from "react";
import { useResourceWatch } from "./use-resource-watch";

type ProjectResource = ReturnType<typeof useResourceWatch<"project">>;
const ProjectResourceContext = createContext<ProjectResource | undefined>(undefined);

// One page owns one Project Watch. Shell, publishing and configuration share its
// authoritative snapshot; none creates a parallel Project read/polling loop.
export function ProjectResourceProvider({ projectId, children }: { projectId: string; children: ReactNode }) {
  const watch = useResourceWatch("project", projectId, projectId);
  return <ProjectResourceContext.Provider value={watch}>{children}</ProjectResourceContext.Provider>;
}
export function useProjectResource() {
  const value = useContext(ProjectResourceContext);
  if (value === undefined) throw new Error("Project resource owner is missing");
  return value;
}
