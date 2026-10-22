// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { StudioLink as Link } from "@/lib/browser/StudioLink";
import type { ReactNode, RefObject } from "react";
import type { ProjectSurface } from "@/lib/shared/studio-routes";
import { cx } from "@/components/ui/cx";
import { Glyph, type GlyphName } from "@/components/ui/Glyph";
import type { BarFit } from "./use-bar-fit";

/** One surface tab: what the URL names it, and how it is labelled. */
export interface SurfaceTab {
  surface: ProjectSurface;
  label: string;
  glyph: GlyphName;
}

/**
 * The surface tabs, starting at the surface's edge. As the bar's `fit` tightens, the tabs that are not
 * open show only their icon, then the open one too; only past the last fit do they scroll. `before`
 * holds the process picker and `after` the tool tabs and +; the bar measures all of them (`navRef` and the others).
 * `badges` adds a count, such as items waiting in the Inbox, and `notes` says in the tab's accessible
 * name what a badge marks. A tab that appeared since the person last looked carries a small dot until it
 * is opened (`fresh`). The open tab's underline is the environment's colour.
 */
export function TabStrip({
  tabs,
  current,
  hrefOf,
  before,
  after,
  badges,
  notes,
  fresh,
  fit,
  navRef,
  beforeRef,
  tabsRef,
  afterRef,
}: {
  tabs: readonly SurfaceTab[];
  /** The open surface, or null while a pinned tool's tab is open. */
  current: ProjectSurface | null;
  hrefOf: (surface: ProjectSurface) => string;
  before?: ReactNode;
  after?: ReactNode;
  badges?: Partial<Record<ProjectSurface, ReactNode>>;
  /** What a tab's badge says, added to its accessible name, such as "setup needs you". */
  notes?: Partial<Record<ProjectSurface, string>>;
  /** Tabs that appeared and were not opened since. */
  fresh?: ReadonlySet<ProjectSurface>;
  fit: BarFit;
  /** The parts the bar measures to choose its fit. */
  navRef: RefObject<HTMLElement | null>;
  beforeRef: RefObject<HTMLDivElement | null>;
  tabsRef: RefObject<HTMLDivElement | null>;
  afterRef: RefObject<HTMLDivElement | null>;
}) {
  return (
    <nav
      ref={navRef}
      aria-label="Surface"
      className="flex min-w-0 flex-1 items-stretch gap-px pl-1.5 max-sm:min-h-header"
    >
      <div ref={beforeRef} className="flex shrink-0 items-stretch">
        {before}
      </div>
      <div ref={tabsRef} className="flex min-w-0 [scrollbar-width:none] items-stretch gap-px overflow-x-auto">
        {tabs.map(({ surface, label, glyph }) => {
          const open = surface === current;
          return (
            <Link
              key={surface}
              href={hrefOf(surface)}
              aria-current={open ? "page" : undefined}
              aria-label={notes?.[surface] ? `${label}, ${notes[surface]}` : label}
              title={label}
              className={cx(
                "-mb-px inline-flex shrink-0 items-center gap-1.75 border-b-3 px-2.75 text-[13px] whitespace-nowrap focus-visible:shadow-[inset_0_0_0_2px_var(--color-accent-rule)] focus-visible:outline-none",
                open ? "border-b-env font-semibold text-ink" : "border-b-transparent text-ink-faint hover:text-ink",
              )}
            >
              <Glyph name={glyph} className="size-4.5" />
              {(fit === "labels" || (fit === "open" && open)) && <span>{label}</span>}
              {badges?.[surface]}
              {/* A tab's own mark, such as Setup needing a person, says more than that it is new. */}
              {fresh?.has(surface) && !open && !badges?.[surface] && (
                <span
                  aria-hidden="true"
                  title="New"
                  className="mt-3.5 -ml-0.5 size-1.5 shrink-0 self-start rounded-full bg-accent"
                  data-testid="new-tab-mark"
                />
              )}
            </Link>
          );
        })}
      </div>
      <div ref={afterRef} className="flex shrink-0 items-stretch">
        {after}
      </div>
    </nav>
  );
}
