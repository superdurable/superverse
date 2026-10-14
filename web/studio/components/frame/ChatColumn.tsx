// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode, RefObject } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { PaneRail } from "@/components/ui/pane/ListColumn";
import { PaneResizeHandle, type CollapsibleColumn } from "@/components/ui/pane/PaneResizeHandle";

/**
 * The Project's chat, the same conversation on every surface. Dragged narrow it folds to a 40px rail
 * that holds only the chat's icon, which opens it again; the conversation stays mounted while folded,
 * so a draft and the scroll position survive.
 */
export function ChatColumn({
  column,
  targetRef,
  measureRef,
  status,
  children,
}: {
  column: CollapsibleColumn;
  /** The element that carries the column's width variable: the frame, so the bar lines up. */
  targetRef: RefObject<HTMLElement | null>;
  measureRef: RefObject<HTMLElement | null>;
  /** Beside the heading: the live connection and the workspace's time. */
  status?: ReactNode;
  children: ReactNode;
}) {
  const folded = column.isCollapsed;
  return (
    <section
      aria-label="Project chat"
      data-folded={folded ? "true" : undefined}
      className="relative flex min-h-0 w-(--chat-w) shrink-0 flex-col border-r border-line-soft bg-bg"
    >
      {folded && (
        <div className="flex min-h-0 flex-1 flex-col pt-2">
          <PaneRail glyph="chat" label="Open the chat" onExpand={column.expand} />
        </div>
      )}
      <div className={cx("flex min-h-0 flex-1 flex-col", folded && "hidden")}>
        <div className="flex h-9.5 shrink-0 items-center gap-2 border-b border-line-faint pr-1.5 pl-3.5">
          <h2 className="text-[13px] font-semibold text-ink">Project chat</h2>
          {status}
          <button
            type="button"
            aria-label="Fold the chat"
            title="Fold the chat"
            onClick={column.collapse}
            className="ml-auto grid size-7 place-items-center rounded-md text-ink-faint hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none"
          >
            <Glyph name="chevronLeft" />
          </button>
        </div>
        {children}
      </div>
      <PaneResizeHandle
        column={column}
        targetRef={targetRef}
        measureRef={measureRef}
        ariaLabel="Resize the chat; drag narrow to fold it into a rail"
        onToggle={column.toggle}
      />
    </section>
  );
}
