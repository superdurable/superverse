// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { redirect } from "next/navigation";
import { ProjectSurface } from "@/components/project/ProjectSurface";
import {
  environmentOf,
  isProjectSurface,
  isProjectTool,
  movedSurfaceHref,
  movedToolOf,
  studioRoutes,
} from "@/lib/shared/studio-routes";
import { MockProjectSurface } from "@/mock/surface/MockProjectSurface";
import { isStudioMockEnabled } from "@/mock/enabled";

export default async function ProjectSurfacePage(props: PageProps<"/p/[id]/[surface]">) {
  const { id, surface } = await props.params;
  // A repeated query parameter selects its first value.
  const query = Object.fromEntries(
    Object.entries(await props.searchParams).map(([name, value]) => [name, Array.isArray(value) ? value[0] : value]),
  );
  // Build, Publishing and unknown surfaces lead to where their content lives now.
  if (!isProjectSurface(surface)) redirect(movedSurfaceHref(id, surface, query));
  // Connectors and Configuration became Setup; their URLs keep the rest of the query.
  const moved = movedToolOf(query.tool);
  if (moved) redirect(studioRoutes.tool(id, moved, query));
  const tool = isProjectTool(query.tool) ? query.tool : "publishing";
  if (isStudioMockEnabled()) return <MockProjectSurface projectId={id} surface={surface} tool={tool} query={query} />;
  return (
    <ProjectSurface
      projectId={id}
      surface={surface}
      tool={tool}
      environment={environmentOf(query.v)}
      releaseId={query.release ?? null}
    />
  );
}
