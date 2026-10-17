// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import dynamic from "next/dynamic";
import Link from "next/link";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import { useProjectList } from "@/lib/project/use-project-list";
import { versionLabel } from "@/lib/shared/version/version";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { projectRoleOf } from "@/lib/teamspace/project-roles";
import { ProjectRoleChip } from "@/components/teamspace/ProjectRoleChip";
import { Glyph } from "@/components/ui/Glyph";

// The start form, with the bundled example catalog and its search index, loads only for a Teamspace without
// Projects, so the Projects list does not ship them.
const ProjectStartForm = dynamic(() => import("./ProjectStartForm").then((m) => m.ProjectStartForm));

const CARD = "flex min-h-36 flex-col gap-2 rounded-xl border border-line-soft bg-panel p-3.5 text-left";

/** What a Project card says about where it stands. */
function statusWords(project: ProjectSnapshot__Output): { words: string; live: boolean } {
  if (project.setup?.status === "SETUP_STATUS_SETUP_FAILED") return { words: "Setup failed", live: false };
  switch (project.status) {
    case "PROJECT_STATUS_CREATING":
      return { words: "Being set up…", live: false };
    case "PROJECT_STATUS_DELETING":
      return { words: "Being deleted…", live: false };
    case "PROJECT_STATUS_READY":
      return project.live && !project.live.isRemoving
        ? { words: `Live · ${versionLabel(project.live.version)}`, live: true }
        : { words: "Not published yet", live: false };
    case "PROJECT_STATUS_SETUP_FAILED":
      return { words: "Setup failed", live: false };
    case "PROJECT_STATUS_UNSPECIFIED":
    case "PROJECT_STATUS_DELETED":
      return { words: "", live: false };
  }
}

/**
 * A Teamspace's Projects as cards, newest first, each with the person's role in it and opening its Builder,
 * and New project; a Project in which the person has no role is listed but does not open. A Teamspace without
 * Projects starts its first one here.
 */
export function ProjectHome({ teamSpaceId }: { teamSpaceId: string }) {
  const projects = useProjectList(teamSpaceId, "home");
  if (projects.loaded && projects.items.length === 0 && !projects.error)
    return <ProjectStartForm teamSpaceId={teamSpaceId} />;
  return (
    <main className="min-h-0 flex-1 overflow-y-auto px-8 pt-6.5 pb-15 max-sm:px-4">
      <h2 className="text-[22px] font-semibold tracking-[-0.01em] text-ink">Projects</h2>
      <p className="mt-0.5 mb-5.5 text-[13.5px] text-ink-faint">Every project of this Teamspace, newest first.</p>
      {projects.error && <p className="mb-3 text-sm text-fail">{projects.error}</p>}
      <div className="grid max-w-5xl grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-3">
        {!projects.loaded &&
          [0, 1, 2].map((index) => (
            <div key={index} className={`${CARD} animate-pulse-soft motion-reduce:animate-none`} aria-hidden="true" />
          ))}
        {projects.items.map((project) => {
          const status = statusWords(project);
          const role = projectRoleOf(project.callerRole);
          if (!role)
            return (
              <div
                key={project.projectId}
                aria-disabled="true"
                title="You have no role in this project; an owner can add you"
                className={`${CARD} text-ink-faint`}
              >
                <span className="flex items-center gap-2">
                  <b className="min-w-0 flex-1 truncate text-[15px] font-semibold">{project.name}</b>
                  <ProjectRoleChip role={null} />
                </span>
                <span className="mt-auto text-[12.5px]">{status.words}</span>
              </div>
            );
          return (
            <Link
              key={project.projectId}
              href={studioRoutes.projectSurface(project.projectId, "flow")}
              className={`${CARD} hover:border-line hover:shadow-(--shadow-lift) focus-visible:shadow-(--focus-ring) focus-visible:outline-none`}
            >
              <span className="flex items-center gap-2">
                <span
                  aria-hidden="true"
                  className="grid size-5 shrink-0 place-items-center rounded-[5px] bg-band-1 text-[11px] font-semibold text-ink"
                >
                  {(project.name.trim()[0] ?? "?").toUpperCase()}
                </span>
                <b className="min-w-0 flex-1 truncate text-[15px] font-semibold text-ink">{project.name}</b>
                <ProjectRoleChip role={role} />
              </span>
              <span className="truncate font-mono text-[11.5px] text-ink-faint">{project.repository?.fullName}</span>
              <span className="mt-auto flex items-center gap-1.5 text-[12.5px] text-ink-faint">
                <span
                  aria-hidden="true"
                  className={
                    status.live ? "size-2 rounded-full bg-pass" : "size-2 rounded-full border-[1.5px] border-mark"
                  }
                />
                {status.words}
              </span>
            </Link>
          );
        })}
        <Link
          href={studioRoutes.projectStart(teamSpaceId)}
          className={`${CARD} items-center justify-center border-dashed text-ink-faint hover:border-line hover:text-ink`}
        >
          <Glyph name="plus" />
          <span>New project</span>
          <span className="text-xs">You build every project you create</span>
        </Link>
      </div>
      {projects.nextPageToken && (
        <button
          type="button"
          disabled={projects.loading}
          onClick={() => void projects.loadMore()}
          className="mt-4 rounded-md border border-line-soft px-3 py-1.5 text-[13px] text-ink-faint hover:bg-panel hover:text-ink-dim"
        >
          {projects.loading ? "Loading…" : "Load more"}
        </button>
      )}
    </main>
  );
}
