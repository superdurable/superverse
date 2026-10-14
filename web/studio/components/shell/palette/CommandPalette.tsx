// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useState, type KeyboardEvent } from "react";
import { useProjectList } from "@/lib/project/use-project-list";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { projectRoleOf } from "@/lib/teamspace/project-roles";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";
import { cx } from "@/components/ui/cx";
import { Dialog } from "@/components/ui/Dialog";
import { usePageEntries, type PaletteEntry } from "./use-palette-entries";

/** At most this many matches are listed; typing narrows them. */
const SHOWN = 12;

/**
 * Jump to (⌘K or Ctrl K): the TeamSpace's Home, New project and every Project the person has a role in, and the
 * open Project's Processes, surfaces and tools, filtered as the person types; arrows move, Enter opens, Escape
 * closes.
 */
export function CommandPalette({ onClose }: { onClose: () => void }) {
  const router = useRouter();
  const { teamSpaceId } = useTeamSpace();
  const projects = useProjectList(teamSpaceId, "palette");
  const page = usePageEntries();
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState(0);
  const entries: PaletteEntry[] = [
    { label: "Home", hint: "page", href: teamSpaceId ? studioRoutes.teamSpace(teamSpaceId) : studioRoutes.home },
    ...(teamSpaceId
      ? [
          { label: "New project", hint: "page", href: studioRoutes.projectStart(teamSpaceId) },
          { label: "People", hint: "page", href: studioRoutes.teamSpacePeople(teamSpaceId) },
        ]
      : []),
    ...page,
    // A Project in which the person has no role has no Builder to open.
    ...projects.items
      .filter((project) => projectRoleOf(project.callerRole))
      .map((project) => ({
        label: project.name,
        hint: "project",
        href: studioRoutes.projectSurface(project.projectId, "flow"),
      })),
  ];
  const words = query.trim().toLowerCase();
  const matches = entries.filter((entry) => entry.label.toLowerCase().includes(words)).slice(0, SHOWN);
  const selected = Math.min(index, Math.max(matches.length - 1, 0));

  const open = (entry: PaletteEntry | undefined) => {
    if (!entry) return;
    onClose();
    router.push(entry.href);
  };
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const count = matches.length || 1;
      setIndex((selected + (event.key === "ArrowDown" ? 1 : count - 1)) % count);
    } else if (event.key === "Enter") {
      event.preventDefault();
      open(matches[selected]);
    }
  };

  return (
    <Dialog aria-label="Jump to" onDismiss={onClose} className="mt-[12vh] mb-auto w-[min(560px,calc(100vw-2rem))] p-0">
      <input
        autoFocus
        aria-label="Jump to"
        placeholder="Jump to a project, process, tab or tool"
        value={query}
        onChange={(event) => {
          setQuery(event.target.value);
          setIndex(0);
        }}
        onKeyDown={onKey}
        className="w-full border-b border-line-soft bg-panel px-4 py-3.5 text-[15px] text-ink outline-none placeholder:text-ink-faint"
      />
      <ul role="listbox" aria-label="Matches" className="max-h-90 overflow-y-auto p-1.5">
        {matches.map((entry, position) => (
          <li key={`${entry.hint}:${entry.href}:${entry.label}`} role="option" aria-selected={position === selected}>
            <button
              type="button"
              onClick={() => open(entry)}
              onMouseEnter={() => setIndex(position)}
              className={cx(
                "flex w-full items-center gap-2.5 rounded-[7px] px-2.5 py-2 text-left text-[13.5px] text-ink",
                position === selected && "bg-accent-wash",
              )}
            >
              <span className="min-w-0 truncate">{entry.label}</span>
              <small className="ml-auto shrink-0 text-xs text-ink-faint">{entry.hint}</small>
            </button>
          </li>
        ))}
        {matches.length === 0 && <li className="px-4 py-4 text-center text-[13px] text-ink-faint">No matches</li>}
      </ul>
    </Dialog>
  );
}
