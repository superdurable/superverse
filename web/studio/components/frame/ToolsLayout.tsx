// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useRef, type ReactNode } from "react";
import type { ProjectTool } from "@/lib/shared/studio-routes";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { PaneResizeHandle, paneWidthStyle, useCollapsibleColumn } from "@/components/ui/pane/PaneResizeHandle";
import { TOOLS_PANE } from "./frame-panes";
import { TOOLS } from "./surface-registry";

/** The amber mark of something that needs a person, such as Setup with missing or unapplied items. */
export function NeedsYouMark({ title, className }: { title: string; className?: string }) {
  return (
    <span
      aria-hidden="true"
      title={title}
      className={cx("size-1.75 shrink-0 rounded-full bg-busy", className)}
      data-testid="needs-you-mark"
    />
  );
}

/**
 * Tools: one flat list of the Project's tools beside the open one. The list folds to a rail of icons
 * when dragged narrow, and each row's pin, shown on hover, opens the tool as a tab of its own. A
 * pinned tab shows the tool alone. A tool that needs a person carries an amber mark (`needsYou`).
 */
export function ToolsLayout({
  current,
  alone,
  hrefOf,
  onPin,
  needsYou,
  children,
}: {
  current: ProjectTool;
  /** The tool opened as a pinned tab, without the list. */
  alone: boolean;
  hrefOf: (tool: ProjectTool) => string;
  onPin: (tool: ProjectTool) => void;
  /** The mark's hint of each tool that needs a person, such as "Setup needs you". */
  needsYou?: Partial<Record<ProjectTool, string>>;
  children: ReactNode;
}) {
  const column = useCollapsibleColumn(TOOLS_PANE);
  const shellRef = useRef<HTMLDivElement>(null);
  const railed = column.isCollapsed;
  if (alone) return <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto">{children}</div>;
  return (
    <div ref={shellRef} style={paneWidthStyle(column)} className="flex min-h-0 flex-1">
      <nav
        aria-label="Tools"
        data-rail={railed ? "true" : undefined}
        className="relative flex w-(--tools-w) shrink-0 flex-col gap-1 overflow-x-hidden overflow-y-auto border-r border-line-soft bg-well py-2"
      >
        {(Object.keys(TOOLS) as ProjectTool[]).map((tool) => (
          <div key={tool} className="group/tool relative">
            <Link
              href={hrefOf(tool)}
              aria-current={tool === current ? "page" : undefined}
              aria-label={needsYou?.[tool] ? `${TOOLS[tool].label}, needs you` : TOOLS[tool].label}
              title={TOOLS[tool].label}
              className={cx(
                "mx-1.5 flex h-8 items-center gap-2.5 rounded-[7px] px-2.5 text-sm whitespace-nowrap focus-visible:shadow-(--focus-ring) focus-visible:outline-none",
                tool === current
                  ? "bg-panel text-ink shadow-[0_0_0_1px_var(--color-line-soft)]"
                  : "text-ink-dim hover:bg-hover hover:text-ink",
              )}
            >
              <Glyph name={TOOLS[tool].glyph} className="size-4.5" />
              {!railed && <span className="min-w-0 truncate">{TOOLS[tool].label}</span>}
              {needsYou?.[tool] && <NeedsYouMark title={needsYou[tool]} className={railed ? "-ml-2 self-start" : ""} />}
            </Link>
            {!railed && (
              <button
                type="button"
                aria-label={`Pin ${TOOLS[tool].label} as a tab`}
                title="Pin as a tab"
                onClick={() => onPin(tool)}
                className="absolute top-1.5 right-3 grid size-5.5 place-items-center rounded-[5px] text-ink-faint opacity-0 group-focus-within/tool:opacity-100 group-hover/tool:opacity-100 hover:bg-band-1 hover:text-ink focus-visible:opacity-100"
              >
                <Glyph name="pin" className="size-3.5" />
              </button>
            )}
          </div>
        ))}
        <PaneResizeHandle
          column={column}
          targetRef={shellRef}
          measureRef={shellRef}
          ariaLabel="Resize the Tools list; drag narrow to fold it into icons"
          onToggle={column.toggle}
        />
      </nav>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto">{children}</div>
    </div>
  );
}
