// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState, type FormEvent } from "react";
import { useAccount } from "@/lib/account/AccountProvider";
import { addTeamSpaceMember, removeTeamSpaceMember, setTeamSpaceMemberRole } from "@/lib/api/tenancy/team-space-member";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import {
  serverTeamSpaceRole,
  TEAM_SPACE_ROLES,
  teamSpaceRoleOf,
  type TeamSpaceRole,
} from "@/lib/teamspace/team-space-roles";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";
import { useTeamSpaceMembers } from "@/lib/teamspace/use-members";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { MemberTable, type MemberRow } from "./MemberTable";

const ROLES = (Object.keys(TEAM_SPACE_ROLES) as TeamSpaceRole[]).map((value) => ({
  value,
  word: TEAM_SPACE_ROLES[value].word,
}));

const FIELD = cx(
  "rounded-lg border border-line bg-panel px-3 py-1.5 text-sm text-ink placeholder:text-ink-faint focus:border-accent focus:shadow-(--focus-ring) focus:outline-none",
);

/**
 * People: a Teamspace's members and their Teamspace role. Its admins, and the platform admin, add registered
 * accounts by email, change roles and remove members; a Teamspace keeps at least one admin, and the only owner of
 * a Project stays until another owner is made. Being a member grants nothing inside a Project.
 */
export function PeoplePage() {
  const account = useAccount();
  const { teamSpace, noteRevision } = useTeamSpace();
  const teamSpaceId = teamSpace?.teamSpaceId ?? null;
  const list = useTeamSpaceMembers(teamSpaceId);
  const action = useSingleFlight<"add" | "role" | "remove">();
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<TeamSpaceRole>("member");
  const [notice, setNotice] = useState("");
  if (!teamSpace || !teamSpaceId) return null;
  const editable = teamSpaceRoleOf(teamSpace.callerRole) === "admin" || Boolean(account?.isAdmin);
  const members = list.members ?? [];
  const admins = members.filter((member) => member.role === "TEAM_SPACE_ROLE_ADMIN");
  const rows: MemberRow<TeamSpaceRole>[] = members.map((member) => {
    const only = admins.length === 1 && member.role === "TEAM_SPACE_ROLE_ADMIN";
    return {
      accountId: member.accountId,
      email: member.email,
      role: teamSpaceRoleOf(member.role) ?? "member",
      fixed: only,
      note: only ? "The only admin: a Teamspace keeps at least one" : undefined,
    };
  });
  const changed = (answer: { members: typeof members; revision: string }) => {
    list.replace(answer.members);
    noteRevision(answer.revision);
  };
  const add = (event: FormEvent) => {
    event.preventDefault();
    const address = email.trim();
    if (!address) return;
    setNotice("");
    void action.run("add", async (signal) => {
      const answer = await addTeamSpaceMember({ teamSpaceId, email: address, role: serverTeamSpaceRole(role) }, signal);
      changed(answer);
      setEmail("");
      setNotice(
        answer.regainedProjectCount > 0
          ? `${address} regains their roles in ${answer.regainedProjectCount} project${answer.regainedProjectCount === 1 ? "" : "s"}.`
          : `${address} is a member now. Give them a role in a project from its Permissions.`,
      );
    });
  };
  return (
    <main className="min-h-0 flex-1 overflow-y-auto px-8 pt-6.5 pb-15 max-sm:px-4">
      <h2 className="text-[22px] font-semibold tracking-[-0.01em] text-ink">People</h2>
      <p className="mt-0.5 mb-5.5 max-w-2xl text-[13.5px] text-ink-faint">
        {teamSpace.name} has {members.length} member{members.length === 1 ? "" : "s"}. Being a member grants nothing
        inside a project; each person gets a role per project.
      </p>
      {editable && (
        <form className="mb-4 flex max-w-2xl flex-wrap items-end gap-2" onSubmit={add} aria-label="Add a person">
          <label className="grid min-w-60 flex-1 gap-1 text-[12.5px] text-ink-dim">
            Email of a registered account
            <input
              type="email"
              className={FIELD}
              value={email}
              placeholder="someone@example.com"
              onChange={(event) => setEmail(event.target.value)}
            />
          </label>
          <label className="grid gap-1 text-[12.5px] text-ink-dim">
            Role
            <Dropdown
              label="Role"
              size="md"
              inline
              value={role}
              onChange={setRole}
              items={ROLES.map((item) => ({ value: item.value, label: item.word }))}
            />
          </label>
          <Button type="submit" variant="strong" disabled={action.busy !== null || !email.trim()}>
            {action.busy === "add" ? "Adding…" : "Add"}
          </Button>
        </form>
      )}
      {notice && (
        <Banner tone="busy" role="status" className="mb-4 max-w-2xl">
          {notice}
        </Banner>
      )}
      {(action.error || list.error) && (
        <Banner role="alert" className="mb-4 max-w-2xl">
          {action.error || list.error}
        </Banner>
      )}
      {list.members === null && !list.error ? (
        <p className="text-[13px] text-ink-faint" role="status">
          Loading people…
        </p>
      ) : (
        <div className="max-w-3xl">
          <MemberTable
            label="Teamspace members"
            rows={rows}
            roles={ROLES}
            selfId={account?.accountId ?? null}
            editable={editable}
            busy={action.busy !== null}
            onRole={(accountId, next) =>
              void action.run("role", async (signal) =>
                changed(
                  await setTeamSpaceMemberRole({ teamSpaceId, accountId, role: serverTeamSpaceRole(next) }, signal),
                ),
              )
            }
            onRemove={(accountId) =>
              void action.run("remove", async (signal) =>
                changed(await removeTeamSpaceMember({ teamSpaceId, accountId }, signal)),
              )
            }
          />
        </div>
      )}
    </main>
  );
}
