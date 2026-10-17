// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useRef, type JSX } from "react";
import { Chip } from "@/components/ui/Chip";
import { PopoverCard, usePopover } from "@/components/ui/Popover";
import type { Direction } from "@/components/flow/canvas/layout/scene";
import type { StageState } from "./use-stage-scene";
import { cx } from "@/components/ui/cx";

const GROUP_LABEL = cx("text-xs tracking-wider whitespace-nowrap text-ink-faint uppercase");

/** The View button sits beside the zoom group and reads like it. */
const VIEW_BUTTON = cx(
  "grid h-7 place-items-center rounded-lg border border-line-soft bg-panel px-2.5 text-xs text-ink-dim hover:bg-hover hover:text-ink focus-visible:shadow-[inset_0_0_0_2px_var(--color-accent-rule)] focus-visible:outline-none aria-expanded:text-ink",
);

interface Choice<T extends string> {
  id: T;
  label: string;
  title: string;
}

function RadioRow<T extends string>({
  group,
  items,
  current,
  pick,
}: {
  group: string;
  items: Choice<T>[];
  current: T;
  pick: (id: T) => void;
}): JSX.Element {
  return (
    <div className="flex gap-1" role="radiogroup" aria-label={group}>
      {items.map((i) => (
        <Chip
          key={i.id}
          role="radio"
          aria-checked={current === i.id}
          on={current === i.id}
          title={i.title}
          onClick={() => pick(i.id)}
        >
          {i.label}
        </Chip>
      ))}
    </div>
  );
}

// A Flow reads top-down by default, as Zapier draws it; left-right, as n8n draws it, is the reader's choice.
const DIRECTION_CHOICES: Choice<Direction>[] = [
  { id: "tb", label: "Top-down", title: "Lay the Flow out top-down" },
  { id: "lr", label: "Left-right", title: "Lay the Flow out left-right" },
];

/**
 * The drawing's own settings behind one View button beside the zoom: which stages are open, and which way
 * the Flow runs. Collapsed and Expanded only close or open every stage; each also starts the view again, so
 * Collapsed is one click back to the view the canvas opened on. Neither is marked while some stages are open
 * and some closed. The card stays open while the reader changes both; a press outside closes it, and so does
 * Escape, which returns the focus to the button.
 */
export function CanvasControls({
  stages,
  direction,
  onCollapseAll,
  onExpandAll,
  onDirection,
}: {
  /** Null for a Flow without stages, which has nothing to open or close. */
  stages: StageState | null;
  direction: Direction;
  onCollapseAll: () => void;
  onExpandAll: () => void;
  onDirection: (d: Direction) => void;
}): JSX.Element {
  const anchor = useRef<HTMLSpanElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  // Escape closes the card, which takes the focus with it; the button takes it back. The Escape is noted
  // before the popover's own listener closes the card, which React then commits within that keydown.
  const escaped = useRef(false);
  useEffect(() => {
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") escaped.current = true;
    };
    window.addEventListener("keydown", escape, true);
    return () => window.removeEventListener("keydown", escape, true);
  }, []);
  const menu = usePopover(anchor);
  useEffect(() => {
    if (!menu.open && escaped.current && document.activeElement === document.body) button.current?.focus();
    escaped.current = false;
  }, [menu.open]);
  return (
    <span ref={anchor} className="inline-flex">
      <button
        ref={button}
        type="button"
        className={VIEW_BUTTON}
        title="How the flow is drawn"
        aria-haspopup="dialog"
        aria-expanded={menu.open}
        onClick={menu.toggle}
      >
        View
      </button>
      {menu.open && (
        <PopoverCard anchorRef={anchor} align="end" label="View" autoFocus tabIndex={-1}>
          <div className="grid gap-2.5 p-1.5">
            {stages === null ? null : (
              <div className="grid gap-1.25" role="group" aria-label="Stages">
                <span className={GROUP_LABEL}>Stages</span>
                <div className="flex gap-1">
                  <Chip
                    aria-pressed={stages === "collapsed"}
                    on={stages === "collapsed"}
                    title="Close every stage and return to the view the canvas opens on"
                    onClick={onCollapseAll}
                  >
                    Collapsed
                  </Chip>
                  <Chip
                    aria-pressed={stages === "expanded"}
                    on={stages === "expanded"}
                    title="Open every stage to show all of its steps"
                    onClick={onExpandAll}
                  >
                    Expanded
                  </Chip>
                </div>
              </div>
            )}
            <div className="grid gap-1.25">
              <span className={GROUP_LABEL}>Direction</span>
              <RadioRow group="Direction" items={DIRECTION_CHOICES} current={direction} pick={onDirection} />
            </div>
          </div>
        </PopoverCard>
      )}
    </span>
  );
}
