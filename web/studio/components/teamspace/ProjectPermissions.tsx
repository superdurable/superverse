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
import { addProjectMember, removeProjectMember, setProjectMemberRole } from "@/lib/api/tenancy/project-member";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { PROJECT_ROLES, projectRoleOf, serverProjectRole, type ProjectRole } from "@/lib/teamspace/project-roles";
import { useProjectMembers, useTeamSpaceMembers } from "@/lib/teamspace/use-members";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { MemberTable, type MemberRow } from "./MemberTable";

const ROLES = (Object.keys(PROJECT_ROLES) as ProjectRole[]).map((value) => ({
  value,
  word: value,
}));
/** What every role dropdown says first: the roles nest. */
const ROLE_NOTE = "Each role includes everything after it.";

/**
 * Tools, Permissions: the Project's members, one role each. Its owners, and the platform admin, add members of
 * the Project's TeamSpace, change roles and remove members; a Project keeps at least one owner. A member who left
 * the TeamSpace keeps the row, but the role grants nothing until they return.
 */
export function ProjectPermissions({
  projectId,
  teamSpaceId,
  role,
}: {
  projectId: string;
  teamSpaceId: string;
  /** The reader's role in the Project. */
  role: ProjectRole;
}) {
  const account = useAccount();
  const list = useProjectMembers(projectId);
  const editable = role === "owner";
  const people = useTeamSpaceMembers(editable ? teamSpaceId : null);
  const action = useSingleFlight<"add" | "role" | "remove">();
  const [added, setAdded] = useState("");
  const [addedRole, setAddedRole] = useState<ProjectRole>("builder");
  const members = list.members ?? [];
  const owners = members.filter((member) => member.role === "PROJECT_ROLE_OWNER");
  const rows: MemberRow<ProjectRole>[] = members.map((member) => {
    const only = owners.length === 1 && member.role === "PROJECT_ROLE_OWNER";
    return {
      accountId: member.accountId,
      email: member.email,
      role: projectRoleOf(member.role) ?? "operator",
      fixed: only,
      muted: !member.isTeamSpaceMember,
      note: !member.isTeamSpaceMember
        ? "Left the TeamSpace: this role grants nothing until they return"
        : only
          ? "The only owner: a project keeps at least one"
          : undefined,
    };
  });
  const candidates = (people.members ?? []).filter(
    (person) => !members.some((member) => member.accountId === person.accountId),
  );
  const changed = (answer: { members: typeof members }) => list.replace(answer.members);
  const add = (event: FormEvent) => {
    event.preventDefault();
    if (!added) return;
    void action.run("add", async (signal) => {
      changed(await addProjectMember({ projectId, accountId: added, role: serverProjectRole(addedRole) }, signal));
      setAdded("");
    });
  };
  return (
    <div>
      {editable && (
        <form className="mb-4 flex flex-wrap items-end gap-2" onSubmit={add} aria-label="Add to the project">
          <label className="grid min-w-60 flex-1 gap-1 text-[12.5px] text-ink-dim">
            A member of the TeamSpace
            <Dropdown
              label="A member of the TeamSpace"
              size="md"
              disabled={candidates.length === 0}
              value={added || null}
              onChange={setAdded}
              placeholder={
                people.members === null
                  ? "Loading the TeamSpace's people…"
                  : candidates.length
                    ? "Choose a person"
                    : "Everyone is in"
              }
              items={candidates.map((person) => ({ value: person.accountId, label: person.email }))}
            />
          </label>
          <label className="grid gap-1 text-[12.5px] text-ink-dim">
            Role
            <Dropdown
              label="Role"
              size="md"
              inline
              value={addedRole}
              onChange={setAddedRole}
              note={ROLE_NOTE}
              items={ROLES.map((item) => ({ value: item.value, label: item.word, sub: PROJECT_ROLES[item.value].can }))}
            />
          </label>
          <Button type="submit" variant="strong" disabled={action.busy !== null || !added}>
            {action.busy === "add" ? "Adding…" : "Add"}
          </Button>
        </form>
      )}
      {(action.error || list.error || people.error) && (
        <Banner role="alert" className="mb-4">
          {action.error || list.error || people.error}
        </Banner>
      )}
      {list.members === null && !list.error ? (
        <p className="text-[13px] text-ink-faint" role="status">
          Loading members…
        </p>
      ) : (
        <MemberTable
          label="Project members"
          rows={rows}
          roles={ROLES}
          selfId={account?.accountId ?? null}
          editable={editable}
          busy={action.busy !== null}
          describe={(item) => PROJECT_ROLES[item].can}
          roleNote={ROLE_NOTE}
          onRole={(accountId, next) =>
            void action.run("role", async (signal) =>
              changed(await setProjectMemberRole({ projectId, accountId, role: serverProjectRole(next) }, signal)),
            )
          }
          onRemove={(accountId) =>
            void action.run("remove", async (signal) =>
              changed(await removeProjectMember({ projectId, accountId }, signal)),
            )
          }
        />
      )}
    </div>
  );
}
