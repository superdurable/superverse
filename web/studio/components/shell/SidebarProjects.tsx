// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import type { ProjectStatus__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectStatus";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import { useProjectList } from "@/lib/project/use-project-list";
import { enumText } from "@/lib/shared/protobuf";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { projectRoleOf } from "@/lib/teamspace/project-roles";
import { ProjectRoleChip } from "@/components/teamspace/ProjectRoleChip";
import { cx } from "@/components/ui/cx";
import { SIDEBAR_HEADING, SIDEBAR_HEADING_RULE, SIDEBAR_ROW, SIDEBAR_ROW_ON } from "./sidebar-styles";

/* Shape, not only hue: pass and fail are 5.3 apart for a deuteranope, so SETUP_FAILED
   is a square. Provisioning and deleting are the machine working, so they take
   the progress blue. A READY Project needs no mark. Setup failure remains owned by the
   Setup snapshot. */
const markFor = {
  PROJECT_STATUS_UNSPECIFIED: null,
  PROJECT_STATUS_CREATING: cx("animate-pulse-soft rounded-full bg-accent motion-reduce:animate-none"),
  PROJECT_STATUS_READY: null,
  PROJECT_STATUS_SETUP_FAILED: cx("rounded-[1px] bg-fail"),
  PROJECT_STATUS_DELETING: cx("animate-pulse-soft rounded-full bg-accent motion-reduce:animate-none"),
  PROJECT_STATUS_DELETED: null,
} satisfies Record<ProjectStatus__Output, string | null>;

function statusMark(project: ProjectSnapshot__Output) {
  return project.setup?.status === "SETUP_STATUS_SETUP_FAILED"
    ? markFor.PROJECT_STATUS_SETUP_FAILED
    : (markFor[project.status] ?? null);
}

/** The initial a Project's tile shows. */
function initialOf(name: string) {
  return (name.trim()[0] ?? "?").toUpperCase();
}

/**
 * The sidebar's Projects: every Project of the Teamspace, one row each with its initial tile, name, the
 * person's role in it and lifecycle mark, the open one highlighted. A Project in which the person has no role is
 * not a link.
 */
export function SidebarProjects({
  teamSpaceId,
  activeId,
  refreshKey,
  labelClassName,
  railed,
}: {
  teamSpaceId: string | null;
  activeId?: string;
  refreshKey: unknown;
  labelClassName?: string;
  /** In the rail the section heading becomes a rule. */
  railed: boolean;
}) {
  const projects = useProjectList(teamSpaceId, refreshKey);
  const heading = "Projects";

  return (
    <section aria-label={heading} className="flex min-h-0 flex-1 flex-col" data-testid="workspace-project-list">
      <div className="relative">
        <h2 className={cx(SIDEBAR_HEADING, labelClassName)}>{heading}</h2>
        {/* In the rail the heading is a rule, until the rail opens over the page. */}
        {railed && <span aria-hidden="true" className={SIDEBAR_HEADING_RULE} />}
      </div>
      <ul className="flex min-h-0 flex-1 flex-col gap-1 overflow-x-hidden overflow-y-auto pb-2">
        {projects.items.length === 0 && !projects.error && (
          <li className={cx("px-5 py-1 text-[12.5px] whitespace-nowrap text-ink-faint", labelClassName)}>
            {!teamSpaceId ? "No Teamspace yet" : projects.loaded ? "No projects yet" : "Loading projects…"}
          </li>
        )}
        {projects.error && (
          <li className={cx("px-5 py-1 text-[12.5px] text-fail", labelClassName)}>{projects.error}</li>
        )}
        {projects.items.map((project) => {
          const role = projectRoleOf(project.callerRole);
          if (!role)
            return (
              <li key={project.projectId} data-project-id={project.projectId} className="my-px">
                <div
                  aria-disabled="true"
                  title={`${project.name} · you have no role in this project`}
                  className={cx(SIDEBAR_ROW, "cursor-default text-ink-faint hover:bg-transparent hover:text-ink-faint")}
                >
                  <span
                    aria-hidden="true"
                    className="-ml-px grid size-5 shrink-0 place-items-center rounded-[5px] bg-band-1 text-[11px] font-semibold"
                  >
                    {initialOf(project.name)}
                  </span>
                  <span className={cx("min-w-0 flex-1 truncate", labelClassName)}>{project.name}</span>
                  <ProjectRoleChip role={null} className={labelClassName} />
                </div>
              </li>
            );
          const active = project.projectId === activeId;
          const mark = statusMark(project);
          const status = enumText(project.status, "PROJECT_STATUS").toLowerCase().replaceAll("_", " ");
          return (
            <li key={project.projectId} data-project-id={project.projectId}>
              <Link
                href={studioRoutes.projectSurface(project.projectId, "flow")}
                aria-current={active ? "page" : undefined}
                title={`${project.name} · ${status}`}
                className={cx(SIDEBAR_ROW, active && SIDEBAR_ROW_ON)}
              >
                <span
                  aria-hidden="true"
                  className={cx(
                    "-ml-px grid size-5 shrink-0 place-items-center rounded-[5px] text-[11px] font-semibold",
                    active ? "bg-ink text-panel" : "bg-band-1 text-ink",
                  )}
                >
                  {initialOf(project.name)}
                </span>
                <span className={cx("min-w-0 flex-1 truncate", labelClassName)}>{project.name}</span>
                <ProjectRoleChip role={role} className={labelClassName} />
                {mark && <span aria-label={status} className={cx("size-1.5 shrink-0", mark, labelClassName)} />}
              </Link>
            </li>
          );
        })}
        {projects.nextPageToken && (
          <li className={cx("px-4 pt-1", labelClassName)}>
            <button
              type="button"
              disabled={projects.loading}
              onClick={() => void projects.loadMore()}
              className="w-full rounded-md border border-line-soft px-2 py-1 text-[12.5px] text-ink-faint hover:bg-panel hover:text-ink-dim disabled:opacity-40"
            >
              {projects.loading ? "Loading…" : "Load more"}
            </button>
          </li>
        )}
      </ul>
    </section>
  );
}
