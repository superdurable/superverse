// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The generated messages of the mock's Teamspaces and members, which the backend's codec round trip completes.
import type { AccountSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AccountSnapshot";
import type { ProjectMemberSnapshot } from "@/lib/generated/protobuf/superverse/v2/ProjectMemberSnapshot";
import type { ProjectSnapshot } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import type { TeamSpaceMemberSnapshot } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceMemberSnapshot";
import type { TeamSpaceOverview } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceOverview";
import type { TeamSpaceSnapshot } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceSnapshot";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import type { MockStore } from "@/mock/store/store";
import { listMockConnections } from "./connections";
import { mockProjectRole, mockTeamSpaceProjects } from "./project-members";
import {
  teamSpaceRoleOf,
  type MockMember,
  type MockProjectRole,
  type MockTeamSpace,
  type MockTenancy,
} from "./team-spaces";

export function mockTeamSpaceSnapshot(teamSpace: MockTeamSpace, account: AccountSnapshot__Output): TeamSpaceSnapshot {
  const role = teamSpaceRoleOf(teamSpace, account);
  return {
    teamSpaceId: teamSpace.id,
    name: teamSpace.name,
    status: `TEAM_SPACE_STATUS_${teamSpace.status}`,
    revision: teamSpace.revision,
    creatorAccountId: teamSpace.creatorAccountId,
    callerRole: role ? `TEAM_SPACE_ROLE_${role}` : "TEAM_SPACE_ROLE_UNSPECIFIED",
    createdAt: isoToTimestamp(teamSpace.createdAt),
    updatedAt: isoToTimestamp(teamSpace.updatedAt),
  };
}

export function mockTeamSpaceMembers(teamSpace: MockTeamSpace): TeamSpaceMemberSnapshot[] {
  return [...teamSpace.members.values()].map((item) => ({
    accountId: item.accountId,
    email: item.email,
    role: `TEAM_SPACE_ROLE_${item.role}`,
    addedAt: isoToTimestamp(item.addedAt),
    addedByAccountId: item.addedByAccountId,
  }));
}

/** A Project's members, each marked by whether it still belongs to the Project's Teamspace. */
export function mockProjectMemberSnapshots(
  tenancy: MockTenancy,
  projectId: string,
  members: Map<string, MockMember<MockProjectRole>>,
): ProjectMemberSnapshot[] {
  const teamSpace = tenancy.teamSpaces.get(tenancy.projectTeamSpace.get(projectId) ?? "");
  return [...members.values()].map((item) => ({
    accountId: item.accountId,
    email: item.email,
    role: `PROJECT_ROLE_${item.role}`,
    addedAt: isoToTimestamp(item.addedAt),
    addedByAccountId: item.addedByAccountId,
    isTeamSpaceMember: Boolean(teamSpace?.members.has(item.accountId)),
  }));
}

/** The counts of a Teamspace's Home; DELETED Projects do not count. */
export function mockTeamSpaceOverview(
  store: MockStore,
  tenancy: MockTenancy,
  teamSpace: MockTeamSpace,
  account: AccountSnapshot__Output,
): TeamSpaceOverview {
  const projects = mockTeamSpaceProjects(tenancy, teamSpace.id, store.projects.values()).filter(
    (project) => project.state !== "DELETED",
  );
  return {
    teamSpace: mockTeamSpaceSnapshot(teamSpace, account),
    memberCount: teamSpace.members.size,
    projectCount: projects.length,
    liveProjectCount: projects.filter((project) => store.liveDeployments.has(project.id)).length,
    connectionCount: listMockConnections(tenancy, teamSpace.id, "", []).length,
  };
}

/**
 * A Project snapshot with its Teamspace and the caller's role. A listed Project in which the caller has no role
 * carries only what a Teamspace member may see of it.
 */
export function withCallerRole(
  snapshot: ProjectSnapshot,
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
): ProjectSnapshot {
  const projectId = snapshot.projectId ?? "";
  const role = mockProjectRole(tenancy, account, projectId);
  const teamSpaceId = tenancy.projectTeamSpace.get(projectId) ?? "";
  if (role) return { ...snapshot, teamSpaceId, callerRole: `PROJECT_ROLE_${role}` };
  return {
    projectId,
    name: snapshot.name,
    status: snapshot.status,
    setup: snapshot.setup && { status: snapshot.setup.status },
    live: snapshot.live,
    teamSpaceId,
    callerRole: "PROJECT_ROLE_UNSPECIFIED",
  };
}
