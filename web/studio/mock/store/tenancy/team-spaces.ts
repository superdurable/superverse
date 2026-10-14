// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Explicit development data only: TeamSpaces, their members and the Projects' members of mock mode.
import type { AccountSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AccountSnapshot";
import { rpcFailure } from "@/lib/server/grpc-client";
import { httpError } from "@/lib/server/request-body";
import { REVISION_CHANGED } from "@/lib/shared/revision-conflict";
import { mockAccountByEmail } from "@/mock/store/accounts";
import { nowIso, randomHex } from "@/mock/store/clock";
import type { MockStore } from "@/mock/store/store";

/** A TeamSpace role and a Project role, as TeamSpaceFlow and ProjectFlow persist them. */
export type MockTeamSpaceRole = "ADMIN" | "MEMBER";
export type MockProjectRole = "OWNER" | "BUILDER" | "OPERATOR";

export interface MockMember<R> {
  accountId: string;
  email: string;
  role: R;
  addedAt: string;
  addedByAccountId: string;
}

export interface MockTeamSpace {
  id: string;
  name: string;
  /** A TeamSpace starts READY; DELETED ones stay, unlisted. */
  status: "READY" | "DELETING" | "DELETED";
  creatorAccountId: string;
  revision: number;
  createdAt: string;
  updatedAt: string;
  members: Map<string, MockMember<MockTeamSpaceRole>>;
}

export interface MockTenancy {
  teamSpaces: Map<string, MockTeamSpace>;
  /** Admitted creations, keyed `${accountId}:${requestId}`, with the name they were made with. */
  admissions: Map<string, { teamSpaceId: string; name: string }>;
  /** Every Project's TeamSpace, by Project ID. */
  projectTeamSpace: Map<string, string>;
  /** Every Project's members by account ID, kept when the Project is deleted. */
  projectMembers: Map<string, Map<string, MockMember<MockProjectRole>>>;
  /** The signed-in accounts that joined the seed TeamSpaces. */
  joined: Set<string>;
}

/** People of the seed TeamSpace who never sign in to mock mode. */
const PEOPLE = {
  priya: { accountId: "0000000000000000000000000000a001", email: "priya@example.com" },
  sam: { accountId: "0000000000000000000000000000a002", email: "sam@example.com" },
  jo: { accountId: "0000000000000000000000000000a003", email: "jo@example.com" },
};

/** The role a signed-in account gets in the seed Projects, in their order: build, operate and none. */
const SEED_PROJECT_ROLES: readonly (MockProjectRole | null)[] = ["OWNER", "OPERATOR", null];

const tenancies = new WeakMap<MockStore, MockTenancy>();

export function member<R>(
  person: { accountId: string; email: string },
  role: R,
  addedByAccountId: string,
  addedAt = nowIso(),
): MockMember<R> {
  return { accountId: person.accountId, email: person.email, role, addedAt, addedByAccountId };
}

/** A seed member, added the day before. */
function seeded<R>(person: { accountId: string; email: string }, role: R): MockMember<R> {
  return member(person, role, PEOPLE.priya.accountId, nowIso(-86_400_000));
}

function seedTeamSpace(id: string, name: string, people: MockMember<MockTeamSpaceRole>[]): MockTeamSpace {
  const at = nowIso(-7 * 86_400_000);
  return {
    id,
    name,
    status: "READY",
    creatorAccountId: PEOPLE.priya.accountId,
    revision: people.length,
    createdAt: at,
    updatedAt: at,
    members: new Map(people.map((item) => [item.accountId, item])),
  };
}

// The seed TeamSpaces: Customer Ops holds the seed Projects, Finance none.
function createTenancy(store: MockStore): MockTenancy {
  const ops = seedTeamSpace("custops1", "Customer Ops", [
    seeded(PEOPLE.priya, "ADMIN"),
    seeded(PEOPLE.sam, "MEMBER"),
    seeded(PEOPLE.jo, "MEMBER"),
  ]);
  const finance = seedTeamSpace("finance1", "Finance", [seeded(PEOPLE.priya, "ADMIN")]);
  const projectIds = [...store.projects.keys()];
  return {
    teamSpaces: new Map([
      [ops.id, ops],
      [finance.id, finance],
    ]),
    admissions: new Map(),
    projectTeamSpace: new Map(projectIds.map((id) => [id, ops.id])),
    projectMembers: new Map(
      projectIds.map((id) => [
        id,
        new Map<string, MockMember<MockProjectRole>>([
          [PEOPLE.priya.accountId, seeded(PEOPLE.priya, "OWNER")],
          [PEOPLE.sam.accountId, seeded(PEOPLE.sam, "BUILDER")],
          [PEOPLE.jo.accountId, seeded(PEOPLE.jo, "OPERATOR")],
        ]),
      ]),
    ),
    joined: new Set(),
  };
}

/**
 * The store's TeamSpaces, with `account` in them: an account's first call makes it an admin of Customer Ops,
 * a member of Finance and the owner and operator of two seed Projects, as every account sees the seed Project.
 */
/** The store's tenancy once an account has read it; null before. */
export function existingMockTenancy(store: MockStore): MockTenancy | null {
  return tenancies.get(store) ?? null;
}

export function mockTenancy(store: MockStore, account: AccountSnapshot__Output): MockTenancy {
  let tenancy = tenancies.get(store);
  if (!tenancy) tenancies.set(store, (tenancy = createTenancy(store)));
  if (!tenancy.joined.has(account.accountId)) {
    tenancy.joined.add(account.accountId);
    const person = { accountId: account.accountId, email: account.email };
    tenancy.teamSpaces.get("custops1")?.members.set(account.accountId, seeded(person, "ADMIN"));
    tenancy.teamSpaces.get("finance1")?.members.set(account.accountId, seeded(person, "MEMBER"));
    [...tenancy.projectMembers.values()].forEach((members, index) => {
      const role = SEED_PROJECT_ROLES[index] ?? null;
      if (role) members.set(account.accountId, seeded(person, role));
    });
  }
  return tenancy;
}

/** The caller's role in the TeamSpace, or null outside it. */
export function teamSpaceRoleOf(teamSpace: MockTeamSpace, account: AccountSnapshot__Output): MockTeamSpaceRole | null {
  return teamSpace.members.get(account.accountId)?.role ?? null;
}

/** A TeamSpace the caller may read: a member's, or any for the platform admin. */
export function readableTeamSpace(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  teamSpaceId: string,
): MockTeamSpace {
  const teamSpace = tenancy.teamSpaces.get(teamSpaceId);
  if (!teamSpace || teamSpace.status === "DELETED") throw httpError(404, "TeamSpace not found");
  if (!account.isAdmin && !teamSpaceRoleOf(teamSpace, account)) throw httpError(403, "Not a member of this TeamSpace");
  return teamSpace;
}

/** A READY TeamSpace whose admins, or the platform admin, change it. */
function administeredTeamSpace(tenancy: MockTenancy, account: AccountSnapshot__Output, teamSpaceId: string) {
  const teamSpace = readableTeamSpace(tenancy, account, teamSpaceId);
  if (!account.isAdmin && teamSpaceRoleOf(teamSpace, account) !== "ADMIN")
    throw httpError(403, "Only an admin of this TeamSpace changes it");
  if (teamSpace.status !== "READY") throw httpError(409, "The TeamSpace is being deleted");
  return teamSpace;
}

function changed(teamSpace: MockTeamSpace) {
  teamSpace.revision += 1;
  teamSpace.updatedAt = nowIso();
}

export function createMockTeamSpace(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  requestId: string,
  name: string,
): string {
  const trimmed = name.trim();
  if (!trimmed || trimmed.length > 80) throw httpError(400, "A TeamSpace name has 1 to 80 characters");
  const key = `${account.accountId}:${requestId}`;
  const admitted = tenancy.admissions.get(key);
  if (admitted) {
    if (admitted.name !== trimmed) throw httpError(409, "This request already created another TeamSpace");
    return admitted.teamSpaceId;
  }
  const at = nowIso();
  const id = randomHex(8);
  tenancy.teamSpaces.set(id, {
    id,
    name: trimmed,
    status: "READY",
    creatorAccountId: account.accountId,
    revision: 1,
    createdAt: at,
    updatedAt: at,
    members: new Map([[account.accountId, member(account, "ADMIN", account.accountId)]]),
  });
  tenancy.admissions.set(key, { teamSpaceId: id, name: trimmed });
  return id;
}

/** The caller's TeamSpaces, every one for the platform admin, oldest first; DELETED never. */
export function listMockTeamSpaces(tenancy: MockTenancy, account: AccountSnapshot__Output): MockTeamSpace[] {
  return [...tenancy.teamSpaces.values()].filter(
    (teamSpace) => teamSpace.status !== "DELETED" && (account.isAdmin || teamSpaceRoleOf(teamSpace, account)),
  );
}

export function renameMockTeamSpace(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  input: { teamSpaceId: string; expectedRevision: string; name: string },
): MockTeamSpace {
  const teamSpace = administeredTeamSpace(tenancy, account, input.teamSpaceId);
  if (String(teamSpace.revision) !== input.expectedRevision)
    throw rpcFailure(409, "The TeamSpace changed since it was read", REVISION_CHANGED);
  const name = input.name.trim();
  if (!name || name.length > 80) throw httpError(400, "A TeamSpace name has 1 to 80 characters");
  teamSpace.name = name;
  changed(teamSpace);
  return teamSpace;
}

/** Only the platform admin deletes a TeamSpace; the mock ends the deletion at once, after its Projects. */
export function deleteMockTeamSpace(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  teamSpaceId: string,
  deleteProject: (projectId: string) => void,
): MockTeamSpace {
  if (!account.isAdmin) throw httpError(403, "Only the platform administrator deletes a TeamSpace");
  const teamSpace = readableTeamSpace(tenancy, account, teamSpaceId);
  if (teamSpace.status === "READY") {
    teamSpace.status = "DELETING";
    changed(teamSpace);
    for (const [projectId, owner] of tenancy.projectTeamSpace) if (owner === teamSpaceId) deleteProject(projectId);
  }
  const accepted = { ...teamSpace, members: new Map(teamSpace.members) };
  teamSpace.status = "DELETED";
  teamSpace.members.clear();
  return accepted;
}

const TEAM_SPACE_ROLES: Record<string, MockTeamSpaceRole> = {
  TEAM_SPACE_ROLE_ADMIN: "ADMIN",
  TEAM_SPACE_ROLE_MEMBER: "MEMBER",
};

function teamSpaceRole(value: string): MockTeamSpaceRole {
  const role = TEAM_SPACE_ROLES[value];
  if (!role) throw httpError(400, "role is admin or member");
  return role;
}

/** At least one admin stays: a change that would leave none is refused. */
function keepAnAdmin(teamSpace: MockTeamSpace, accountId: string, role: MockTeamSpaceRole | null) {
  const admins = [...teamSpace.members.values()].filter(
    (item) => item.role === "ADMIN" && item.accountId !== accountId,
  );
  if (role !== "ADMIN" && admins.length === 0) throw httpError(409, "A TeamSpace keeps at least one admin");
}

/** Adds a registered account; the count is of the Projects in which a returning member still holds a role. */
export function addMockTeamSpaceMember(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  input: { teamSpaceId: string; email: string; role: string },
): { teamSpace: MockTeamSpace; regainedProjectCount: number } {
  const teamSpace = administeredTeamSpace(tenancy, account, input.teamSpaceId);
  const role = teamSpaceRole(input.role);
  const added = mockAccountByEmail(input.email);
  if (!added) throw httpError(404, `No account is registered as ${input.email.trim()}; they need to register first`);
  const current = teamSpace.members.get(added.accountId);
  if (current?.role === role) return { teamSpace, regainedProjectCount: 0 };
  if (current) throw httpError(409, `${added.email} is already a member; change their role instead`);
  if (teamSpace.members.size >= 500) throw httpError(409, "A TeamSpace holds at most 500 members");
  teamSpace.members.set(added.accountId, member(added, role, account.accountId));
  changed(teamSpace);
  const regainedProjectCount = [...tenancy.projectTeamSpace].filter(
    ([projectId, owner]) => owner === teamSpace.id && tenancy.projectMembers.get(projectId)?.has(added.accountId),
  ).length;
  return { teamSpace, regainedProjectCount };
}

export function setMockTeamSpaceMemberRole(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  input: { teamSpaceId: string; accountId: string; role: string },
): MockTeamSpace {
  const teamSpace = administeredTeamSpace(tenancy, account, input.teamSpaceId);
  const role = teamSpaceRole(input.role);
  const current = teamSpace.members.get(input.accountId);
  if (!current) throw httpError(404, "Not a member of this TeamSpace");
  if (current.role === role) return teamSpace;
  keepAnAdmin(teamSpace, input.accountId, role);
  current.role = role;
  changed(teamSpace);
  return teamSpace;
}

/** Refused while the member is the only owner of a Project of the TeamSpace, which the message names. */
export function removeMockTeamSpaceMember(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  input: { teamSpaceId: string; accountId: string },
  projectName: (projectId: string) => string | null,
): MockTeamSpace {
  const teamSpace = administeredTeamSpace(tenancy, account, input.teamSpaceId);
  const current = teamSpace.members.get(input.accountId);
  if (!current) return teamSpace;
  keepAnAdmin(teamSpace, input.accountId, null);
  const soleOwner = [...tenancy.projectTeamSpace]
    .filter(([projectId, owner]) => owner === teamSpace.id && projectName(projectId) !== null)
    .filter(([projectId]) => {
      const owners = [...(tenancy.projectMembers.get(projectId)?.values() ?? [])].filter(
        (item) => item.role === "OWNER",
      );
      return owners.length === 1 && owners[0].accountId === input.accountId;
    })
    .map(([projectId]) => projectName(projectId));
  if (soleOwner.length > 0)
    throw httpError(
      409,
      `${current.email} is the only owner of ${soleOwner.join(", ")}; make someone else an owner there first`,
    );
  teamSpace.members.delete(input.accountId);
  changed(teamSpace);
  return teamSpace;
}
