// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useSearchParams } from "next/navigation";
import type { ReactNode } from "react";
import { ProjectBuilder } from "@/components/project/ProjectBuilder";
import { resolveRole } from "./project-surfaces";

const LENS_QUERY = ["role", "fixture"] as const;

/**
 * The production Builder over the mock store's Project, drawn by the Builder's layout, with the role
 * lens and fixture kept on every link; the bar's role word shows the lens.
 */
export function MockFrame({ projectId, children }: { projectId: string; children: ReactNode }) {
  const role = resolveRole(useSearchParams().get("role") ?? undefined);
  return (
    <ProjectBuilder projectId={projectId} role={role} persistentQuery={LENS_QUERY}>
      {children}
    </ProjectBuilder>
  );
}
