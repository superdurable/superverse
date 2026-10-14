// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { SessionProvider } from "@/lib/session/SessionProvider";
import { canBuild, projectRoleOf } from "@/lib/teamspace/project-roles";

/**
 * The Project page's Session: ProjectFlow starts the Project's one Session once the Project is
 * READY, so the page owns it from then on and has none before. Only those who build read it.
 */
export function ProjectSessionProvider({ projectId, children }: { projectId: string; children: ReactNode }) {
  const project = useProjectResource()?.snapshot;
  const role = project ? projectRoleOf(project.callerRole) : null;
  const ready =
    project?.projectId === projectId && project.status === "PROJECT_STATUS_READY" && role !== null && canBuild(role);
  return (
    <SessionProvider projectId={projectId} active={ready}>
      {children}
    </SessionProvider>
  );
}
