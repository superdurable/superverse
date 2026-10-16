// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Explicit development data only: the Projects' Teamspaces, members and the caller's role in them.
import type { AccountSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AccountSnapshot";
import { httpError } from "@/lib/server/request-body";
import { nowIso } from "@/mock/store/clock";
import type { MockProject } from "@/mock/store/store";
import {
  member,
  readableTeamSpace,
  teamSpaceRoleOf,
  type MockMember,
  type MockProjectRole,
  type MockTenancy,
} from "./team-spaces";

/**
 * The caller's effective role in a Project: its Project role while it is a member of the Project's
 * Teamspace, OWNER for the platform admin, null otherwise.
 */
export function mockProjectRole(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  projectId: string,
): MockProjectRole | null {
  if (account.isAdmin) return "OWNER";
  const teamSpace = tenancy.teamSpaces.get(tenancy.projectTeamSpace.get(projectId) ?? "");
  if (!teamSpace || !teamSpaceRoleOf(teamSpace, account)) return null;
  return tenancy.projectMembers.get(projectId)?.get(account.accountId)?.role ?? null;
}

/** Reading a Project needs a role in it, as ViewProject does. */
export function requireMockProjectRole(tenancy: MockTenancy, account: AccountSnapshot__Output, projectId: string) {
  const role = mockProjectRole(tenancy, account, projectId);
  if (!role) throw httpError(403, "You have no role in this project");
  return role;
}

/** Whether the caller may create a Project in the Teamspace: a member of it while READY, or the platform admin. */
export function admitMockProject(tenancy: MockTenancy, account: AccountSnapshot__Output, teamSpaceId: string) {
  if (!teamSpaceId) throw httpError(400, "team_space_id is required");
  const teamSpace = readableTeamSpace(tenancy, account, teamSpaceId);
  if (teamSpace.status !== "READY") throw httpError(409, "The Teamspace is being deleted");
}

/** Registers a new Project in its Teamspace with its creator as the owner, as ProjectFlow's first Step does. */
export function registerMockProject(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  teamSpaceId: string,
  projectId: string,
) {
  if (tenancy.projectTeamSpace.has(projectId)) return;
  tenancy.projectTeamSpace.set(projectId, teamSpaceId);
  tenancy.projectMembers.set(projectId, new Map([[account.accountId, member(account, "OWNER", account.accountId)]]));
}

/** Deleting a Project needs its owner, an admin of its Teamspace or the platform admin. */
export function requireMockProjectDeletion(tenancy: MockTenancy, account: AccountSnapshot__Output, projectId: string) {
  const teamSpace = tenancy.teamSpaces.get(tenancy.projectTeamSpace.get(projectId) ?? "");
  if (teamSpace && teamSpaceRoleOf(teamSpace, account) === "ADMIN") return;
  if (mockProjectRole(tenancy, account, projectId) !== "OWNER")
    throw httpError(403, "Only an owner of this project or an admin of its Teamspace deletes it");
}

/** The Projects of the Teamspace, in the order the store keeps them. */
export function mockTeamSpaceProjects(tenancy: MockTenancy, teamSpaceId: string, projects: Iterable<MockProject>) {
  return [...projects].filter((project) => tenancy.projectTeamSpace.get(project.id) === teamSpaceId);
}

const PROJECT_ROLES: Record<string, MockProjectRole> = {
  PROJECT_ROLE_OWNER: "OWNER",
  PROJECT_ROLE_BUILDER: "BUILDER",
  PROJECT_ROLE_OPERATOR: "OPERATOR",
};

function projectRole(value: string): MockProjectRole {
  const role = PROJECT_ROLES[value];
  if (!role) throw httpError(400, "role is owner, builder or operator");
  return role;
}

/** A Project's members, which every role may read. */
export function mockProjectMembers(tenancy: MockTenancy, account: AccountSnapshot__Output, projectId: string) {
  requireMockProjectRole(tenancy, account, projectId);
  return tenancy.projectMembers.get(projectId) ?? new Map<string, MockMember<MockProjectRole>>();
}

/** The members of a live Project whose owners, or the platform admin, change them. */
function ownedMembers(tenancy: MockTenancy, account: AccountSnapshot__Output, project: MockProject) {
  if (project.state === "DELETED") throw httpError(409, "The project is deleted");
  if (requireMockProjectRole(tenancy, account, project.id) !== "OWNER")
    throw httpError(403, "Only an owner of this project changes its members");
  return mockProjectMembers(tenancy, account, project.id);
}

function keepAnOwner(
  members: Map<string, MockMember<MockProjectRole>>,
  accountId: string,
  role: MockProjectRole | null,
) {
  const owners = [...members.values()].filter((item) => item.role === "OWNER" && item.accountId !== accountId);
  if (role !== "OWNER" && owners.length === 0) throw httpError(409, "A project keeps at least one owner");
}

function changed(project: MockProject) {
  project.revision += 1;
  project.updatedAt = nowIso();
}

/** Adds a member of the Project's Teamspace; adding one with the same role changes nothing. */
export function addMockProjectMember(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  project: MockProject,
  input: { accountId: string; role: string },
) {
  const members = ownedMembers(tenancy, account, project);
  const role = projectRole(input.role);
  const teamSpace = tenancy.teamSpaces.get(tenancy.projectTeamSpace.get(project.id) ?? "");
  const person = teamSpace?.members.get(input.accountId);
  if (!person) throw httpError(409, "Only members of the project's Teamspace join it");
  const current = members.get(input.accountId);
  if (current?.role === role) return members;
  if (current) throw httpError(409, `${person.email} is already in this project; change their role instead`);
  if (members.size >= 200) throw httpError(409, "A project holds at most 200 members");
  members.set(input.accountId, member(person, role, account.accountId));
  changed(project);
  return members;
}

export function setMockProjectMemberRole(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  project: MockProject,
  input: { accountId: string; role: string },
) {
  const members = ownedMembers(tenancy, account, project);
  const role = projectRole(input.role);
  const current = members.get(input.accountId);
  if (!current) throw httpError(404, "Not a member of this project");
  if (current.role === role) return members;
  keepAnOwner(members, input.accountId, role);
  current.role = role;
  changed(project);
  return members;
}

export function removeMockProjectMember(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  project: MockProject,
  accountId: string,
) {
  const members = ownedMembers(tenancy, account, project);
  if (!members.has(accountId)) return members;
  keepAnOwner(members, accountId, null);
  members.delete(accountId);
  changed(project);
  return members;
}
