// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode, RefObject } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph, type GlyphName } from "@/components/ui/Glyph";
import { PaneResizeHandle, type CollapsibleColumn } from "./PaneResizeHandle";

/**
 * A folded pane: only its icon, which says what is folded and opens it again. What the pane holds is
 * chosen after opening it, so the rail lists nothing else.
 */
export function PaneRail({ glyph, label, onExpand }: { glyph: GlyphName; label: string; onExpand: () => void }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onExpand}
      className="mx-auto grid size-8 shrink-0 place-items-center rounded-[7px] text-ink-dim hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none"
    >
      <Glyph name={glyph} className="size-4.5" />
    </button>
  );
}

/**
 * The list column Runs, the Inbox and Connectors share: a heading with an optional note and action,
 * the caller's rows, the collapsed rail, and the handle that resizes the column.
 */
export function ListColumn({
  heading,
  headingTitle,
  note,
  action,
  expandTitle,
  resizeLabel,
  pane,
  shellRef,
  bodyRef,
  railGlyph,
  foldLabel,
  children,
}: {
  heading: string;
  /** Hover text for the heading. */
  headingTitle?: string;
  note?: string;
  action?: ReactNode;
  expandTitle: string;
  resizeLabel: string;
  pane: CollapsibleColumn;
  /** The element carrying the column's width variable. */
  shellRef: RefObject<HTMLElement | null>;
  /** The container the column's width is measured against. */
  bodyRef: RefObject<HTMLElement | null>;
  /** The icon of the folded column, which opens it again. */
  railGlyph: GlyphName;
  /** With a label, the heading has a ‹ button that folds the column to its rail. */
  foldLabel?: string;
  children: ReactNode;
}) {
  return (
    <>
      <aside
        className={cx(
          "flex min-h-0 min-w-0 flex-col overflow-hidden border-r border-line-faint bg-panel dark:bg-transparent",
          pane.isCollapsed ? "pt-2" : "px-3 py-3.5",
        )}
        aria-label={heading}
        data-collapsed={pane.isCollapsed ? "true" : undefined}
      >
        {pane.isCollapsed ? (
          <PaneRail glyph={railGlyph} label={expandTitle} onExpand={pane.expand} />
        ) : (
          <>
            <div className="mb-2 flex min-h-7 items-center gap-2">
              <span className="text-xs tracking-wider text-ink-faint uppercase" title={headingTitle}>
                {heading}
              </span>
              {note !== undefined && <span className="text-[10px] tracking-wider text-ink-faint">{note}</span>}
              {action}
              {foldLabel && (
                <button
                  type="button"
                  aria-label={foldLabel}
                  title={foldLabel}
                  onClick={pane.collapse}
                  className={cx(
                    "grid size-6.5 place-items-center rounded-md text-ink-faint hover:bg-hover hover:text-ink",
                    !action && "ml-auto",
                  )}
                >
                  ‹
                </button>
              )}
            </div>
            {children}
          </>
        )}
      </aside>
      <PaneResizeHandle
        column={pane}
        targetRef={shellRef}
        measureRef={bodyRef}
        ariaLabel={resizeLabel}
        onToggle={pane.toggle}
      />
    </>
  );
}
