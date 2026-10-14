// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { TeamSpaceRole__Output } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceRole";

/** A person's role in a TeamSpace: admins change its members and name; members see all of its Projects. */
export type TeamSpaceRole = "admin" | "member";

/** Each role with the word the switcher and People show. */
export const TEAM_SPACE_ROLES = {
  admin: { word: "admin" },
  member: { word: "member" },
} as const satisfies Record<TeamSpaceRole, { word: string }>;

const SERVER_ROLES = {
  TEAM_SPACE_ROLE_UNSPECIFIED: null,
  TEAM_SPACE_ROLE_ADMIN: "admin",
  TEAM_SPACE_ROLE_MEMBER: "member",
} as const satisfies Record<TeamSpaceRole__Output, TeamSpaceRole | null>;

/** The role a TeamSpace snapshot's `caller_role` names; null outside the TeamSpace, as for the platform admin. */
export function teamSpaceRoleOf(callerRole: TeamSpaceRole__Output): TeamSpaceRole | null {
  return SERVER_ROLES[callerRole];
}

/** The server's name of a role, as a member change sends it. */
export function serverTeamSpaceRole(role: TeamSpaceRole): TeamSpaceRole__Output {
  return `TEAM_SPACE_ROLE_${role.toUpperCase() as Uppercase<TeamSpaceRole>}`;
}
