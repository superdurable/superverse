// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ProjectRole__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectRole";

/** A person's role in a Project, one of the three of decision 0023. */
export type ProjectRole = "owner" | "builder" | "operator";

/**
 * Each role, in nesting order: each holds everything the roles after it hold, with no extra grants. `word` is
 * what the bar, the project switch and the sidebar show beside a Project's name and `can` what Permissions says
 * the role allows; builders change the Project with the agent; operators work on Live only.
 */
export const PROJECT_ROLES = {
  owner: {
    word: "build",
    can: "Everything a builder can, plus the project's people and deleting it",
    builds: true,
    liveOnly: false,
  },
  builder: {
    word: "build",
    can: "Change it with the agent, try it on Preview, publish, and stop or rerun runs",
    builds: true,
    liveOnly: false,
  },
  operator: {
    word: "operate",
    can: "Watch every Live run, start runs and act on any Inbox item; no Preview and no changes",
    builds: false,
    liveOnly: true,
  },
} as const satisfies Record<ProjectRole, { word: string; can: string; builds: boolean; liveOnly: boolean }>;

const SERVER_ROLES = {
  PROJECT_ROLE_UNSPECIFIED: null,
  PROJECT_ROLE_OWNER: "owner",
  PROJECT_ROLE_BUILDER: "builder",
  PROJECT_ROLE_OPERATOR: "operator",
} as const satisfies Record<ProjectRole__Output, ProjectRole | null>;

/** The role a Project snapshot's `caller_role` or a member's `role` names; null when there is none. */
export function projectRoleOf(callerRole: ProjectRole__Output): ProjectRole | null {
  return SERVER_ROLES[callerRole];
}

/** The server's name of a role, as a member change sends it. */
export function serverProjectRole(role: ProjectRole): ProjectRole__Output {
  return `PROJECT_ROLE_${role.toUpperCase() as Uppercase<ProjectRole>}`;
}

/** Owners and builders change the Project with the agent, so only they get the chat column. */
export function canBuild(role: ProjectRole): boolean {
  return PROJECT_ROLES[role].builds;
}
