// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useRef, useState, type FormEvent } from "react";
import { addProjectMember } from "@/lib/api/tenancy/project-member";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import type { ProjectMemberSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectMemberSnapshot";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { PROJECT_ROLES, serverProjectRole, type ProjectRole } from "@/lib/teamspace/project-roles";
import { useProjectMembers, useTeamSpaceMembers } from "@/lib/teamspace/use-members";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { PopoverCard, usePopover } from "@/components/ui/Popover";

/** Each project role, by its word. */
export const PROJECT_ROLE_ITEMS = (Object.keys(PROJECT_ROLES) as ProjectRole[]).map((value) => ({
  value,
  word: value,
}));
/** What every role dropdown says first: the roles nest. */
export const ROLE_NOTE = "Each role includes everything after it.";

/**
 * Adds a member of the Project's Teamspace with one role: Tools ▸ Permissions and the bar's Share use it alike.
 * Only an owner sees it; the server refuses anyone else.
 */
export function AddProjectMember({
  projectId,
  teamSpaceId,
  members,
  onAdded,
  stacked = false,
}: {
  projectId: string;
  teamSpaceId: string;
  /** The Project's members, whom the person list leaves out. */
  members: readonly ProjectMemberSnapshot__Output[];
  onAdded: (members: ProjectMemberSnapshot__Output[]) => void;
  /** Each field on its own line, as in a popover. */
  stacked?: boolean;
}) {
  const people = useTeamSpaceMembers(teamSpaceId);
  const action = useSingleFlight<"add">();
  const [added, setAdded] = useState("");
  const [addedRole, setAddedRole] = useState<ProjectRole>("builder");
  const candidates = (people.members ?? []).filter(
    (person) => !members.some((member) => member.accountId === person.accountId),
  );
  const add = (event: FormEvent) => {
    event.preventDefault();
    if (!added) return;
    void action.run("add", async (signal) => {
      onAdded(
        (await addProjectMember({ projectId, accountId: added, role: serverProjectRole(addedRole) }, signal)).members,
      );
      setAdded("");
    });
  };
  return (
    <>
      <form
        className={stacked ? "grid gap-2" : "mb-4 flex flex-wrap items-end gap-2"}
        onSubmit={add}
        aria-label="Add to the project"
      >
        <label
          className={
            stacked ? "grid gap-1 text-[12.5px] text-ink-dim" : "grid min-w-60 flex-1 gap-1 text-[12.5px] text-ink-dim"
          }
        >
          A member of the Teamspace
          <Dropdown
            label="A member of the Teamspace"
            size="md"
            disabled={candidates.length === 0}
            value={added || null}
            onChange={setAdded}
            placeholder={
              people.members === null
                ? "Loading the Teamspace's people…"
                : candidates.length
                  ? "Choose a person"
                  : "Everyone is in"
            }
            items={candidates.map((person) => ({ value: person.accountId, label: person.email }))}
          />
        </label>
        <div className={stacked ? "flex items-end gap-2" : "contents"}>
          <label className="grid gap-1 text-[12.5px] text-ink-dim">
            Role
            <Dropdown
              label="Role"
              size="md"
              inline
              value={addedRole}
              onChange={setAddedRole}
              note={ROLE_NOTE}
              items={PROJECT_ROLE_ITEMS.map((item) => ({
                value: item.value,
                label: item.word,
                sub: PROJECT_ROLES[item.value].can,
              }))}
            />
          </label>
          <Button
            type="submit"
            variant="strong"
            className={stacked ? "ml-auto" : undefined}
            disabled={action.busy !== null || !added}
          >
            {action.busy === "add" ? "Adding…" : "Add"}
          </Button>
        </div>
      </form>
      {(action.error || people.error) && (
        <Banner role="alert" className={stacked ? "mt-2" : "mb-4"}>
          {action.error || people.error}
        </Banner>
      )}
    </>
  );
}

/**
 * The bar's Share: a small popover to invite a member of the Teamspace into the Project, for its owners, and to
 * copy the link to what this page shows, which members open as it is. Who has access is in Tools ▸ Permissions.
 */
export function ProjectShare({
  projectId,
  projectName,
  teamSpaceId,
  owner,
}: {
  projectId: string;
  projectName: string;
  teamSpaceId: string;
  /** The reader owns the Project, so may add people. */
  owner: boolean;
}) {
  const anchor = useRef<HTMLSpanElement>(null);
  const popover = usePopover(anchor);
  const [copied, setCopied] = useState(false);
  const members = useProjectMembers(popover.open && owner ? projectId : null);
  const copy = () =>
    void navigator.clipboard.writeText(window.location.href).then(
      () => {
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1600);
      },
      () => undefined,
    );
  return (
    <span ref={anchor} className="inline-flex">
      <Button
        variant="quiet"
        aria-haspopup="dialog"
        aria-expanded={popover.open}
        title="Invite people, or copy the link"
        onClick={popover.toggle}
        data-testid="project-share"
      >
        <Glyph name="users" />
        <span className="group-data-[tight=true]/bar:hidden">Share</span>
      </Button>
      {popover.open && (
        <PopoverCard
          anchorRef={anchor}
          align="end"
          label={`Share ${projectName}`}
          className="w-[340px] max-w-[calc(100vw-16px)]"
        >
          <div className="grid gap-3 p-1.5 text-[12.5px]">
            <p className="font-semibold text-ink">Invite to {projectName}</p>
            {owner ? (
              members.members === null ? (
                <p className="text-ink-faint" role="status">
                  Loading members…
                </p>
              ) : (
                <AddProjectMember
                  projectId={projectId}
                  teamSpaceId={teamSpaceId}
                  members={members.members}
                  onAdded={members.replace}
                  stacked
                />
              )
            ) : (
              <p className="text-ink-dim">Only the project&apos;s owners add people.</p>
            )}
            <div className="flex items-center gap-2 border-t border-line-faint pt-3">
              <Button size="sm" onClick={copy}>
                <Glyph name={copied ? "check" : "link"} className="size-3.5" />
                {copied ? "Copied" : "Copy link"}
              </Button>
              <Link
                className="ml-auto text-accent hover:underline"
                href={studioRoutes.tool(projectId, "permissions")}
                onClick={popover.close}
              >
                Who has access ›
              </Link>
            </div>
          </div>
        </PopoverCard>
      )}
    </span>
  );
}
