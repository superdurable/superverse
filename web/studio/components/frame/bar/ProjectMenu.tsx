// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useRef, type RefObject } from "react";
import { useProjectList } from "@/lib/project/use-project-list";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { projectRoleOf, type ProjectRole } from "@/lib/teamspace/project-roles";
import { ProjectRoleChip } from "@/components/teamspace/ProjectRoleChip";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { POPOVER_ITEM, PopoverCard, usePopover } from "@/components/ui/Popover";

/**
 * The Project's name at the head of the bar, with the person's role in it unless they own it; it opens the
 * switch to another Project of its Teamspace, where each Project shows the person's role, or that they have none.
 */
export function ProjectMenu({
  projectId,
  teamSpaceId,
  name,
  role,
}: {
  projectId: string;
  teamSpaceId: string | null;
  name: string;
  role: ProjectRole;
}) {
  const anchor = useRef<HTMLDivElement>(null);
  const popover = usePopover(anchor);
  return (
    <div ref={anchor} className="relative flex min-w-0 items-center gap-2">
      <button
        type="button"
        aria-expanded={popover.open}
        aria-haspopup="dialog"
        onClick={popover.toggle}
        aria-label={`${name}, switch project`}
        title="Switch project"
        className="-ml-0.5 inline-flex max-w-64 min-w-0 items-center gap-1.5 rounded-md px-0.5 text-[15px] leading-tight font-semibold text-ink hover:text-accent focus-visible:shadow-(--focus-ring) focus-visible:outline-none"
      >
        <span className="min-w-0 truncate">{name}</span>
        <Glyph name="chevronDown" className="size-3.5" />
      </button>
      {role !== "owner" && <ProjectRoleChip role={role} />}
      {popover.open && (
        <ProjectList projectId={projectId} teamSpaceId={teamSpaceId} anchorRef={anchor} onPick={popover.close} />
      )}
    </div>
  );
}

/** Mounted only while open, so opening reads the Projects again. */
function ProjectList({
  projectId,
  teamSpaceId,
  anchorRef,
  onPick,
}: {
  projectId: string;
  teamSpaceId: string | null;
  anchorRef: RefObject<HTMLElement | null>;
  onPick: () => void;
}) {
  const projects = useProjectList(teamSpaceId, "menu");
  return (
    <PopoverCard anchorRef={anchorRef} label="Switch project" className="max-h-[70vh] w-80">
      {projects.items.map((project) => {
        const role = projectRoleOf(project.callerRole);
        if (!role)
          return (
            <p
              key={project.projectId}
              aria-disabled="true"
              title="You have no role in this project; an owner can add you"
              className={cx(POPOVER_ITEM, "cursor-not-allowed text-ink-faint hover:bg-transparent")}
            >
              <span className="min-w-0 truncate">{project.name}</span>
            </p>
          );
        return (
          <Link
            key={project.projectId}
            href={studioRoutes.projectSurface(project.projectId, "flow")}
            aria-current={project.projectId === projectId ? "page" : undefined}
            onClick={onPick}
            className={POPOVER_ITEM}
          >
            <span
              aria-hidden="true"
              className="grid size-5 shrink-0 place-items-center rounded-[5px] bg-band-1 text-[11px] font-semibold"
            >
              {(project.name.trim()[0] ?? "?").toUpperCase()}
            </span>
            <span className="min-w-0 flex-1 truncate">{project.name}</span>
            <ProjectRoleChip role={role} />
          </Link>
        );
      })}
      {!projects.loaded && <p className="px-2.25 py-1.75 text-ink-faint">Loading projects…</p>}
      {projects.error && <p className="px-2.25 py-1.75 text-fail">{projects.error}</p>}
      <hr className="mx-0.5 my-1 border-line-faint" />
      {teamSpaceId && (
        <Link href={studioRoutes.projectStart(teamSpaceId)} onClick={onPick} className={POPOVER_ITEM}>
          <Glyph name="plus" />
          New project
        </Link>
      )}
      <Link href={studioRoutes.project(projectId)} onClick={onPick} className={cx(POPOVER_ITEM, "text-ink-dim")}>
        <Glyph name="folder" />
        Project details
      </Link>
    </PopoverCard>
  );
}
