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
import { STORAGE_KEYS } from "@/lib/browser/storage";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";
import { TeamSpaceSwitcher } from "@/components/teamspace/TeamSpaceSwitcher";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import {
  PaneResizeHandle,
  paneWidthStyle,
  useCollapsibleColumn,
  type PaneBounds,
} from "@/components/ui/pane/PaneResizeHandle";
import { AccountMenu } from "./AccountMenu";
import { DexMark } from "./DexMark";
import { RAIL_LABEL, SIDEBAR_HEADING, SIDEBAR_HEADING_RULE, SIDEBAR_ROW, SIDEBAR_ROW_ON } from "./sidebar-styles";
import { SidebarProjects } from "./SidebarProjects";
import { ThemeToggle } from "./ThemeToggle";

/** 240px open and a 56px rail. A drag below 150px snaps to the rail; open, it is 190 to 340px wide. */
const SIDEBAR = {
  cssVariable: "--sidebar-w",
  edge: "end",
  defaultWidth: 240,
  minWidth: 190,
  maxWidth: 340,
  maxFraction: 0.5,
  rail: { width: 56, snapBelow: 150 },
  dragFallback: () => 240,
} as const satisfies Omit<PaneBounds, "storageKey">;

/** Open on the account's own pages; a rail inside a Project, where the Builder needs the width. */
const HOME_SIDEBAR: PaneBounds = { ...SIDEBAR, storageKey: STORAGE_KEYS.sidebarHomeWidth };
const PROJECT_SIDEBAR: PaneBounds = {
  ...SIDEBAR,
  storageKey: STORAGE_KEYS.sidebarProjectWidth,
  startsCollapsed: true,
};

/**
 * The signed-in sidebar on every page: the mark, the TeamSpace switcher, Jump to, New project, Home, the
 * TeamSpace's Projects, its Team section (Connections, People) kept in view below them, and the theme and
 * account at the foot. Dragged narrow it folds to a rail of icons and tiles, which opens over the
 * page after a short hover; a double-click or Enter on its edge toggles it. Its width is remembered
 * per browser, separately on the account's pages and inside a Project.
 */
export function Sidebar({
  activeProjectId,
  measureRef,
  pathname,
  onJump,
}: {
  activeProjectId?: string;
  /** The app frame the sidebar's width is measured against. */
  measureRef: RefObject<HTMLElement | null>;
  /** The page's path: a new one reads the Projects again. */
  pathname: string;
  /** Opens Jump to. */
  onJump: () => void;
}) {
  const column = useCollapsibleColumn(activeProjectId ? PROJECT_SIDEBAR : HOME_SIDEBAR);
  const targetRef = useRef<HTMLElement>(null);
  const railed = column.isCollapsed;
  const label = railed ? RAIL_LABEL : undefined;
  const { teamSpaceId } = useTeamSpace();
  const homeHref = teamSpaceId ? studioRoutes.teamSpace(teamSpaceId) : studioRoutes.home;
  const newProjectActive =
    (teamSpaceId !== null && pathname === studioRoutes.projectStart(teamSpaceId)) ||
    pathname === studioRoutes.projectCreation;
  const homeActive = pathname === homeHref;

  return (
    <aside
      ref={targetRef}
      aria-label="Studio"
      data-rail={railed ? "true" : "false"}
      style={paneWidthStyle(column)}
      className="group/sidebar relative z-30 w-(--sidebar-w) shrink-0"
    >
      <div
        className={cx(
          "absolute inset-y-0 left-0 flex w-(--sidebar-w) flex-col overflow-hidden border-r border-line-soft bg-well transition-[width,box-shadow] duration-140 ease-out motion-reduce:transition-none",
          railed &&
            "group-focus-within/sidebar:w-60 group-focus-within/sidebar:shadow-(--shadow-lift) group-hover/sidebar:w-60 group-hover/sidebar:shadow-(--shadow-lift) group-hover/sidebar:delay-280",
        )}
      >
        <div className="flex h-header shrink-0 items-center gap-2 pr-2.5 pl-3">
          <Link
            href={studioRoutes.home}
            aria-label="Dex home"
            className="grid size-8 shrink-0 place-items-center rounded-md focus-visible:shadow-(--focus-ring) focus-visible:outline-none"
          >
            <DexMark size={28} />
          </Link>
          <span className={cx("text-base font-bold tracking-[-0.01em] text-ink", label)}>Dex</span>
        </div>
        <div className="shrink-0 pt-1">
          <TeamSpaceSwitcher rowClassName={SIDEBAR_ROW} labelClassName={label} />
        </div>
        <nav aria-label="Studio" className="flex shrink-0 flex-col gap-1 pt-2 pb-1">
          <button type="button" onClick={onJump} title="Jump to (⌘K)" className={SIDEBAR_ROW}>
            <Glyph name="search" className="size-4.5" />
            <span className={cx("min-w-0 truncate", label)}>Jump to</span>
            <kbd className={cx("ml-auto font-mono text-[11px] text-ink-faint", label)}>⌘K</kbd>
          </button>
          {/* Every member of a TeamSpace, and the platform admin, may start a Project in it. */}
          {teamSpaceId && (
            <Link
              href={studioRoutes.projectStart(teamSpaceId)}
              aria-current={newProjectActive ? "page" : undefined}
              title="New project"
              className={cx(SIDEBAR_ROW, newProjectActive && SIDEBAR_ROW_ON)}
            >
              <Glyph name="plus" className="size-4.5" />
              <span className={cx("min-w-0 truncate", label)}>New project</span>
            </Link>
          )}
          <Link
            href={homeHref}
            aria-current={homeActive ? "page" : undefined}
            title="Home"
            className={cx(SIDEBAR_ROW, homeActive && SIDEBAR_ROW_ON)}
          >
            <Glyph name="home" className="size-4.5" />
            <span className={cx("min-w-0 truncate", label)}>Home</span>
          </Link>
        </nav>
        <SidebarProjects
          teamSpaceId={teamSpaceId}
          activeId={activeProjectId}
          refreshKey={pathname}
          labelClassName={label}
          railed={railed}
        />
        {teamSpaceId && (
          <div className="shrink-0 pb-2">
            <div className="relative">
              <h2 className={cx(SIDEBAR_HEADING, label)}>Team</h2>
              {railed && <span aria-hidden="true" className={SIDEBAR_HEADING_RULE} />}
            </div>
            <nav aria-label="Team" className="flex flex-col gap-1">
              {[
                { href: studioRoutes.teamSpaceConnections(teamSpaceId), glyph: "plug" as const, text: "Connections" },
                { href: studioRoutes.teamSpacePeople(teamSpaceId), glyph: "person" as const, text: "People" },
              ].map((item) => (
                <Link
                  key={item.href}
                  href={item.href}
                  aria-current={pathname === item.href ? "page" : undefined}
                  title={item.text}
                  className={cx(SIDEBAR_ROW, pathname === item.href && SIDEBAR_ROW_ON)}
                >
                  <Glyph name={item.glyph} className="size-4.5" />
                  <span className={cx("min-w-0 truncate", label)}>{item.text}</span>
                </Link>
              ))}
            </nav>
          </div>
        )}
        <div className="shrink-0 border-t border-line-soft pt-2 pb-2.5">
          <ThemeToggle labelClassName={label} />
          <AccountMenu labelClassName={label} />
        </div>
        <PaneResizeHandle
          column={column}
          targetRef={targetRef}
          measureRef={measureRef}
          ariaLabel="Resize the sidebar; drag narrow to fold it into a rail"
          onToggle={column.toggle}
        />
      </div>
    </aside>
  );
}
