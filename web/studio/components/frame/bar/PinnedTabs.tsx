// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import type { ProjectTool } from "@/lib/shared/studio-routes";
import { cx } from "@/components/ui/cx";
import { Glyph, type GlyphName } from "@/components/ui/Glyph";
import { ActionMenu } from "@/components/ui/menu/ActionMenu";

/** One tool as the bar shows it. */
export interface BarTool {
  tool: ProjectTool;
  label: string;
  glyph: GlyphName;
}

/**
 * The tools pinned as tabs after the surface tabs, each closable, and the + that opens a tool as a
 * tab. Pinned tabs are remembered per browser.
 */
export function PinnedTabs({
  tools,
  pinned,
  current,
  hrefOf,
  onPin,
  onUnpin,
}: {
  tools: readonly BarTool[];
  pinned: readonly ProjectTool[];
  /** The pinned tool open now, or null. */
  current: ProjectTool | null;
  hrefOf: (tool: ProjectTool) => string;
  onPin: (tool: ProjectTool) => void;
  onUnpin: (tool: ProjectTool) => void;
}) {
  return (
    <>
      {pinned.map((tool) => {
        const entry = tools.find((item) => item.tool === tool);
        if (!entry) return null;
        const open = tool === current;
        return (
          <span
            key={tool}
            className={cx(
              "-mb-px inline-flex shrink-0 items-center gap-1 border-b-2 pr-1.5 text-[13px] whitespace-nowrap",
              open ? "border-b-env font-semibold text-ink" : "border-b-transparent text-ink-faint hover:text-ink",
            )}
          >
            <Link
              href={hrefOf(tool)}
              aria-current={open ? "page" : undefined}
              className="inline-flex h-full items-center gap-1.75 pl-2.75 focus-visible:outline-none"
            >
              <Glyph name={entry.glyph} className="size-4.5" />
              {/* The label follows the bar's fit, as the surface tabs' do. */}
              <span
                className={cx(
                  "group-data-[fit=icons]/bar:hidden group-data-[tight=true]/bar:hidden",
                  !open && "group-data-[narrow=true]/bar:hidden",
                )}
              >
                {entry.label}
              </span>
            </Link>
            <button
              type="button"
              aria-label={`Unpin ${entry.label}`}
              title="Unpin"
              onClick={() => onUnpin(tool)}
              className="grid size-4 place-items-center rounded-sm text-ink-faint hover:bg-hover hover:text-ink"
            >
              <Glyph name="close" className="size-3" />
            </button>
          </span>
        );
      })}
      <ActionMenu
        label="Open a tool as a tab"
        glyph="plus"
        glyphClassName="size-4.5"
        align="start"
        triggerClassName="inline-flex items-center px-2.5 text-ink-faint hover:text-ink focus-visible:shadow-[inset_0_0_0_2px_var(--color-accent-rule)] focus-visible:outline-none aria-expanded:text-ink"
        items={[
          { heading: "Open a tool as a tab" },
          ...tools.map((entry) => ({
            label: entry.label,
            glyph: entry.glyph,
            hint: pinned.includes(entry.tool) ? "open" : undefined,
            onSelect: () => onPin(entry.tool),
          })),
        ]}
      />
    </>
  );
}
