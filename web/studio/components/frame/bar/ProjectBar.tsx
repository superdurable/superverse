// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRef, type ReactNode } from "react";
import type { ProjectEnvironment, ProjectSurface } from "@/lib/shared/studio-routes";
import { TabStrip, type SurfaceTab } from "./TabStrip";
import { useBarFit } from "./use-bar-fit";

/**
 * The Builder's one 48px bar, lined up with the columns below it. The left block (the Project and
 * the Preview | Live switch) is as wide as the chat column, so the tabs start where the surface does;
 * with the chat folded it fits its content. The Project's actions sit at the right. When the bar runs
 * out of room its parts give way in turn (`useBarFit`); `data-fit` with `data-narrow` and `data-tight`
 * tell them how far. `data-environment` gives the environment's colours (`--color-env`) to the parts
 * that show which one is open: grey on Preview, green on Live.
 */
export function ProjectBar({
  environment,
  left,
  tabs,
  current,
  hrefOf,
  chatOpen,
  tabsBefore,
  tabsAfter,
  badges,
  notes,
  actions,
}: {
  /** The environment every surface shows. */
  environment: ProjectEnvironment;
  left: ReactNode;
  tabs: readonly SurfaceTab[];
  current: ProjectSurface | null;
  hrefOf: (surface: ProjectSurface) => string;
  chatOpen: boolean;
  tabsBefore?: ReactNode;
  tabsAfter?: ReactNode;
  badges?: Partial<Record<ProjectSurface, ReactNode>>;
  /** What a tab's badge says, for the tab's accessible name. */
  notes?: Partial<Record<ProjectSurface, string>>;
  actions?: ReactNode;
}) {
  const header = useRef<HTMLElement>(null);
  const leftBlock = useRef<HTMLDivElement>(null);
  const nav = useRef<HTMLElement>(null);
  const before = useRef<HTMLDivElement>(null);
  const tabStrip = useRef<HTMLDivElement>(null);
  const after = useRef<HTMLDivElement>(null);
  const actionBlock = useRef<HTMLDivElement>(null);
  const fit = useBarFit({
    header,
    left: leftBlock,
    nav,
    before,
    tabs: tabStrip,
    after,
    actions: actionBlock,
  });
  return (
    <header
      ref={header}
      data-environment={environment}
      data-fit={fit}
      data-narrow={fit === "open" || fit === "icons" ? "true" : undefined}
      data-tight={fit === "tight" ? "true" : undefined}
      className="group/bar relative z-20 flex h-header shrink-0 items-stretch border-b border-line-soft bg-panel pr-2.5 max-sm:h-auto max-sm:flex-wrap"
    >
      <div
        ref={leftBlock}
        className={
          chatOpen && fit !== "tight"
            ? "flex w-(--chat-w) min-w-max shrink-0 items-center gap-2 pr-2 pl-3.5 max-sm:w-full max-sm:min-w-0 max-sm:flex-wrap max-sm:py-1.5"
            : "flex shrink-0 items-center gap-2 pr-2 pl-3.5 max-sm:w-full max-sm:min-w-0 max-sm:flex-wrap max-sm:py-1.5"
        }
      >
        {left}
      </div>
      <TabStrip
        tabs={tabs}
        current={current}
        hrefOf={hrefOf}
        before={tabsBefore}
        after={tabsAfter}
        badges={badges}
        notes={notes}
        fit={fit}
        navRef={nav}
        beforeRef={before}
        tabsRef={tabStrip}
        afterRef={after}
      />
      <div ref={actionBlock} className="ml-auto flex shrink-0 items-center gap-2 pl-2.5">
        {actions}
      </div>
    </header>
  );
}
