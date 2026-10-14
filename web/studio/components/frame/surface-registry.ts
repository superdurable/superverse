// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { GlyphName } from "@/components/ui/Glyph";
import type { ProjectSurface, ProjectTool } from "@/lib/shared/studio-routes";
import { PROJECT_ROLES, type ProjectRole } from "@/lib/teamspace/project-roles";

/** The roles in nesting order: a role opens what the roles after it open. */
const ROLE_ORDER = Object.keys(PROJECT_ROLES) as ProjectRole[];

interface SurfaceEntry {
  label: string;
  glyph: GlyphName;
  /** Whether the Preview | Live switch sets what the surface shows. */
  followsEnvironment: boolean;
  /** The least role that may open it. */
  openTo: ProjectRole;
}

/** The Builder's surfaces, in tab order. Every surface the URL can name is here, and nothing else. */
export const SURFACES = {
  flow: { label: "Flow", glyph: "flow", followsEnvironment: true, openTo: "operator" },
  runs: { label: "Runs", glyph: "runs", followsEnvironment: true, openTo: "operator" },
  inbox: { label: "Inbox", glyph: "inbox", followsEnvironment: true, openTo: "operator" },
  interface: { label: "Interface", glyph: "window", followsEnvironment: true, openTo: "operator" },
  tools: { label: "Tools", glyph: "tools", followsEnvironment: false, openTo: "builder" },
} as const satisfies Record<ProjectSurface, SurfaceEntry>;

/** The tools of Tools, one flat list in this order. */
export const TOOLS = {
  publishing: { label: "Publishing", glyph: "upload" },
  setup: { label: "Setup", glyph: "sliders" },
  permissions: { label: "Permissions", glyph: "shield" },
} as const satisfies Record<ProjectTool, { label: string; glyph: GlyphName }>;

export function canOpenSurface(role: ProjectRole, surface: ProjectSurface): boolean {
  return ROLE_ORDER.indexOf(role) <= ROLE_ORDER.indexOf(SURFACES[surface].openTo);
}

/** The surfaces `role` may open, in tab order. */
export function surfacesFor(role: ProjectRole): ProjectSurface[] {
  return (Object.keys(SURFACES) as ProjectSurface[]).filter((surface) => canOpenSurface(role, surface));
}
