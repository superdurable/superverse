// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useRef, useSyncExternalStore, type CSSProperties, type RefObject } from "react";
import { readStoredPreference, subscribeStoredPreferences, writeStoredPreference } from "@/lib/browser/storage";
import { useResizeHandle } from "./use-resize-handle";
import { cx } from "@/components/ui/cx";

/** By default a column dragged narrower than this snaps shut to its collapsed rail. */
const COLUMN_SNAP = 100;
const COLUMN_COLLAPSED = 36;
/** Every pane leaves at least this much of its container to the canvas or case beside it. */
const CANVAS_WIDTH_MIN = 280;

/** One resizable column: where its width lives in CSS and storage, and how wide it may get. */
export interface PaneBounds {
  cssVariable: `--${string}`;
  /** The side of the pane the handle sits on; a start-edge handle widens the pane when dragged toward the start. */
  edge: "start" | "end";
  /** Where the handle sits in its pane's layout (rendered as `data-pane`); by default on the container's end edge. */
  pane?: HandlePlacement;
  storageKey: string;
  defaultWidth: number;
  minWidth: number;
  /** The largest share of the container the pane may take. */
  maxFraction: number;
  /** The widest the pane gets, whatever its container; unbounded when absent. */
  maxWidth?: number;
  /** The width of its collapsed rail, and the width below which a drag snaps to it; 36 and 100 when absent. */
  rail?: { width: number; snapBelow: number };
  /** The width a drag starts from when none has been measured yet. */
  dragFallback: (box: DOMRect) => number;
  /** False for a pane that stops at its minimum width instead of snapping shut to the rail. */
  collapsible?: false;
  /** True for a pane that starts on its rail until the person widens it. */
  startsCollapsed?: true;
}

/** The list's handle straddles the list's end, the drawer's its start, the selected-event panel's the grid gap before it. */
type HandlePlacement = "list" | "drawer" | "selected-event";

const PLACEMENT = {
  default: cx("right-0 z-10"),
  list: cx("left-(--v2-list-w,250px) z-7 -ml-1"),
  drawer: cx("right-(--v2-drawer-w,290px) z-7 -mr-1"),
  // Below lg the Diagnostics sidebar stacks under the timeline, so there is no width to drag.
  "selected-event": cx("-left-3.25 z-10 max-lg:hidden"),
} satisfies Record<HandlePlacement | "default", string>;

function railOf(bounds: PaneBounds) {
  return bounds.rail ?? { width: COLUMN_COLLAPSED, snapBelow: COLUMN_SNAP };
}

function isCollapsedColumn(bounds: PaneBounds, width: number): boolean {
  return bounds.collapsible !== false && width < railOf(bounds).snapBelow;
}

/** The narrowest a pane gets: the collapsed rail, or its minimum width when it cannot collapse. */
function widthFloor(bounds: PaneBounds): number {
  return bounds.collapsible === false ? bounds.minWidth : railOf(bounds).width;
}

function widthCeiling(bounds: PaneBounds, containerWidth: number): number {
  const container = Number.isFinite(containerWidth) ? Math.max(0, containerWidth) : 0;
  return Math.min(container * bounds.maxFraction, container - CANVAS_WIDTH_MIN, bounds.maxWidth ?? Infinity);
}

/** A desired width within the pane's bounds, or the collapsed rail when it is snapped shut. */
function clampWidth(bounds: PaneBounds, desired: number, containerWidth: number): number {
  const ceiling = widthCeiling(bounds, containerWidth);
  const want = Number.isFinite(desired) && desired > 0 ? desired : bounds.defaultWidth;
  if (isCollapsedColumn(bounds, want)) return railOf(bounds).width;
  if (ceiling <= bounds.minWidth) return Math.max(widthFloor(bounds), ceiling);
  return Math.min(Math.max(want, bounds.minWidth), ceiling);
}

/**
 * A remembered width kept within the pane's bounds when it is read back: a value stored before the
 * bounds changed, or by an older Studio, must not hide the canvas or squeeze the pane to nothing. Below
 * the snap it is the rail; otherwise between the minimum and the maximum, and at most the pane's share
 * of the window (read on the client only, where a stored width is used at all).
 */
function storedWidth(bounds: PaneBounds, stored: number): number {
  if (isCollapsedColumn(bounds, stored)) return railOf(bounds).width;
  const share = typeof window === "undefined" ? Infinity : window.innerWidth * bounds.maxFraction;
  return Math.max(bounds.minWidth, Math.min(stored, bounds.maxWidth ?? Infinity, share));
}

/**
 * The width a drag shows while the pointer moves. Below the snap it follows the pointer from the rail
 * instead of holding at the rail, so dragging a folded pane wider visibly opens it; the snap to the rail
 * or to the minimum width applies only on release (`clampWidth`).
 */
function followWidth(bounds: PaneBounds, desired: number, containerWidth: number): number {
  if (!isCollapsedColumn(bounds, desired)) return clampWidth(bounds, desired, containerWidth);
  return Math.max(railOf(bounds).width, Math.min(desired, widthCeiling(bounds, containerWidth)));
}

/**
 * A pane width remembered per browser, with collapse to the rail and expand back. The server and the
 * first client render use the default, so a page rendered on the server hydrates without a mismatch,
 * and the stored width follows at once.
 */
export function useCollapsibleColumn(bounds: PaneBounds) {
  const stored = Number(
    useSyncExternalStore(
      subscribeStoredPreferences,
      () => readStoredPreference(bounds.storageKey),
      () => null,
    ),
  );
  const width =
    Number.isFinite(stored) && stored > 0
      ? storedWidth(bounds, stored)
      : bounds.startsCollapsed
        ? railOf(bounds).width
        : bounds.defaultWidth;
  const lastExpandedRef = useRef(isCollapsedColumn(bounds, width) ? bounds.defaultWidth : width);

  const commit = useCallback(
    (next: number) => {
      const rounded = Math.round(next);
      if (!isCollapsedColumn(bounds, rounded)) lastExpandedRef.current = rounded;
      writeStoredPreference(bounds.storageKey, String(rounded));
    },
    [bounds],
  );

  const expand = useCallback(() => commit(lastExpandedRef.current), [commit]);
  const collapse = useCallback(() => commit(railOf(bounds).width), [bounds, commit]);
  const isCollapsed = isCollapsedColumn(bounds, width);
  /** Collapses or expands the pane, or restores the default width of one that cannot collapse. */
  const toggle = bounds.collapsible === false ? () => commit(bounds.defaultWidth) : isCollapsed ? expand : collapse;

  return { bounds, width, commit, expand, collapse, toggle, isCollapsed };
}

export type CollapsibleColumn = ReturnType<typeof useCollapsibleColumn>;

/** Sets each column's width variable, on the element whose variable the column's handle drags. */
export function paneWidthStyle(...columns: CollapsibleColumn[]): CSSProperties {
  return Object.fromEntries(
    columns.map((column) => [column.bounds.cssVariable, `${Math.round(column.width)}px`]),
  ) as CSSProperties;
}

/**
 * A drag and keyboard handle that resizes one column. During a drag only the CSS variable on
 * `targetRef` changes; the width is committed when the pointer is released.
 */
export function PaneResizeHandle({
  column,
  targetRef,
  measureRef,
  ariaLabel,
  onToggle,
}: {
  column: CollapsibleColumn;
  targetRef: RefObject<HTMLElement | null>;
  /** The container the column's bounds are measured against. */
  measureRef: RefObject<HTMLElement | null>;
  ariaLabel: string;
  /** What a double-click or Enter does, usually the column's `toggle`. */
  onToggle?: () => void;
}) {
  const { bounds, width: value, commit: onCommit } = column;
  const liveRef = useRef(value);
  const invert = bounds.edge === "start";

  useEffect(() => {
    liveRef.current = value;
  }, [value]);

  const clampLive = (desired: number) => {
    const box = measureRef.current?.getBoundingClientRect();
    return box ? clampWidth(bounds, desired, box.width) : desired;
  };

  const writeLive = (next: number, handle: HTMLElement) => {
    liveRef.current = next;
    targetRef.current?.style.setProperty(bounds.cssVariable, `${Math.round(next)}px`);
    const box = measureRef.current?.getBoundingClientRect();
    if (!box) return;
    handle.dataset.atFloor = next <= widthFloor(bounds) ? "true" : "";
    handle.dataset.atCeiling = next >= widthCeiling(bounds, box.width) ? "true" : "";
  };

  const gestures = useResizeHandle({
    keyStep: { normal: 8, shift: 40 },
    moveBy: (pixels) => {
      const current = liveRef.current > 0 ? liveRef.current : clampLive(0);
      onCommit(clampLive(current + (invert ? -pixels : pixels)));
    },
    startDrag: (event) => {
      const box = measureRef.current?.getBoundingClientRect();
      if (!box) return null;
      const startSize = clampLive(liveRef.current || bounds.dragFallback(box));
      liveRef.current = startSize;
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      return { startPointer: event.clientX, startSize };
    },
    drag: (event, start) => {
      const rawDelta = event.clientX - start.startPointer;
      const desired = start.startSize + (invert ? -rawDelta : rawDelta);
      const box = measureRef.current?.getBoundingClientRect();
      writeLive(box ? followWidth(bounds, desired, box.width) : desired, event.currentTarget);
    },
    // The width a drag reached is kept even when the gesture was cancelled, snapped to the rail or
    // to the minimum width.
    endDrag: (event) => {
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      delete event.currentTarget.dataset.atFloor;
      delete event.currentTarget.dataset.atCeiling;
      // Written back as well as committed: when the snap lands on the width the drag started from,
      // React sees no change and would leave the dragged width on the variable.
      const settled = clampLive(liveRef.current);
      liveRef.current = settled;
      targetRef.current?.style.setProperty(bounds.cssVariable, `${Math.round(settled)}px`);
      onCommit(settled);
    },
  });

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={ariaLabel}
      aria-valuenow={Math.round(value)}
      aria-valuemin={widthFloor(bounds)}
      tabIndex={0}
      data-axis="column"
      data-edge={bounds.edge}
      data-pane={bounds.pane}
      className={cx(
        "absolute inset-y-0 w-2.25 cursor-col-resize touch-none bg-transparent transition-colors duration-110 ease-in-out hover:bg-accent-wash focus-visible:bg-accent-wash focus-visible:outline-none data-[at-ceiling=true]:bg-accent-wash-strong data-[at-floor=true]:bg-accent-wash-strong",
        PLACEMENT[bounds.pane ?? "default"],
      )}
      {...gestures}
      onKeyDown={(event) => {
        if (event.key === "Enter" && onToggle) {
          event.preventDefault();
          onToggle();
        } else gestures.onKeyDown(event);
      }}
      onDoubleClick={onToggle}
    />
  );
}
