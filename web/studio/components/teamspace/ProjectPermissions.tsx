// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useAccount } from "@/lib/account/AccountProvider";
import { removeProjectMember, setProjectMemberRole } from "@/lib/api/tenancy/project-member";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { PROJECT_ROLES, projectRoleOf, serverProjectRole, type ProjectRole } from "@/lib/teamspace/project-roles";
import { useProjectMembers } from "@/lib/teamspace/use-members";
import { Banner } from "@/components/ui/Banner";
import { MemberTable, type MemberRow } from "./MemberTable";
import { AddProjectMember, PROJECT_ROLE_ITEMS, ROLE_NOTE } from "./ProjectShare";

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
  const action = useSingleFlight<"role" | "remove">();
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
  const changed = (answer: { members: typeof members }) => list.replace(answer.members);
  return (
    <div>
      {editable && list.members !== null && (
        <AddProjectMember projectId={projectId} teamSpaceId={teamSpaceId} members={members} onAdded={list.replace} />
      )}
      {(action.error || list.error) && (
        <Banner role="alert" className="mb-4">
          {action.error || list.error}
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
          roles={PROJECT_ROLE_ITEMS}
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
