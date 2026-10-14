// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { JSX } from "react";
import type { BoxChip, RunPill, Tile, Tone } from "@/components/flow/canvas/layout/scene";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";

/**
 * The parts a Step card and a stage card share, in the project map's vocabulary: the tile that says
 * who acts, docked chips, the Start pill and the run pill. Colour always repeats what a glyph or a
 * word already says.
 */

/** A tone as text and as the tile's wash; machine work and apps stay greyscale. */
export const TONE_TEXT = {
  busy: cx("text-busy"),
  door: cx("text-door"),
  machine: cx("text-ink-faint"),
  clock: cx("text-clock"),
  child: cx("text-child"),
  accent: cx("text-accent"),
  app: cx("text-ink-dim"),
} satisfies Record<Tone, string>;

const TONE_WASH = {
  busy: cx("bg-busy-subtle"),
  door: cx("bg-door-subtle"),
  machine: cx("bg-band-0"),
  clock: cx("bg-clock-subtle"),
  child: cx("bg-child-subtle"),
  accent: cx("bg-accent-subtle"),
  app: cx("bg-band-1"),
} satisfies Record<Tone, string>;

export function TileView({ tile, small = false }: { tile: Tile; small?: boolean }): JSX.Element {
  if (small) {
    // The stage card's row of member tiles: the glyph alone, an app as a plug.
    return "glyph" in tile ? (
      <Glyph name={tile.glyph} className={cx("size-3.25", TONE_TEXT[tile.tone])} />
    ) : (
      <Glyph name="plug" className={cx("size-3.25", TONE_TEXT[tile.tone])} />
    );
  }
  return (
    <span
      className={cx(
        "grid size-8.5 flex-none place-items-center rounded-[9px]",
        TONE_WASH[tile.tone],
        TONE_TEXT[tile.tone],
      )}
      aria-hidden="true"
    >
      {"glyph" in tile ? (
        <Glyph name={tile.glyph} className="size-5" />
      ) : (
        <span className="font-mono text-[11.5px] font-bold tracking-tight">{tile.monogram}</span>
      )}
    </span>
  );
}

const CHIP_TONE = {
  jump: cx("bg-band-0 text-ink-dim"),
  failJump: cx("bg-fail-subtle text-fail"),
  inbound: cx("bg-band-0 text-ink-faint"),
  fork: cx("bg-band-0 text-ink-dim"),
  ends: cx("bg-pass-subtle text-pass"),
  stops: cx("bg-band-1 text-ink-dim"),
  fails: cx("bg-fail-subtle text-fail"),
  answer: cx("bg-busy-subtle text-busy"),
  entry: cx("bg-door-subtle text-door"),
  child: cx("bg-child-subtle text-child"),
} satisfies Record<BoxChip["kind"], string>;

const CHIP = cx("block h-4.5 max-w-full truncate rounded-full px-1.75 text-[10.5px]/[18px] whitespace-nowrap");

/**
 * Chip rows as the layout packed them. A jump chip is a button that selects where it leads; the
 * others are labels.
 */
export function ChipRows({ rows, onJump }: { rows: BoxChip[][]; onJump: (id: string) => void }): JSX.Element | null {
  if (rows.length === 0) return null;
  return (
    <span className="mt-2 flex flex-col gap-1">
      {rows.map((row, i) => (
        <span className="flex min-w-0 gap-1" key={i}>
          {row.map((chip, j) =>
            chip.targetId ? (
              <button
                type="button"
                key={j}
                className={cx(
                  CHIP,
                  CHIP_TONE[chip.kind],
                  "nodrag nopan min-w-0 hover:outline hover:outline-mark focus-visible:outline-2 focus-visible:outline-accent",
                )}
                title={`${chip.title ? `${chip.title}: ` : ""}goes to ${chip.text.replace(/^(↪ |↩ back to )/, "")}. Click to show it.`}
                data-chip={chip.kind}
                onClick={(e) => {
                  e.stopPropagation();
                  onJump(chip.targetId as string);
                }}
              >
                {chip.text}
              </button>
            ) : (
              <span
                key={j}
                className={cx(CHIP, CHIP_TONE[chip.kind], "min-w-0")}
                title={chip.title}
                data-chip={chip.kind}
              >
                {chip.text}
              </span>
            ),
          )}
        </span>
      ))}
    </span>
  );
}

/** Where the Flow begins, sitting on the card's top edge like a trigger. */
export function StartPill(): JSX.Element {
  return (
    <span
      className="absolute -top-2.25 left-3 flex items-center gap-0.5 rounded-full border-2 border-canvas bg-door-subtle px-1.5 text-[10.5px]/[14px] font-semibold text-door"
      data-start-pill="true"
    >
      <Glyph name="bolt" className="size-2.75" />
      Start
    </span>
  );
}

const PILL_TONE: Record<RunPill["status"], string> = {
  notStarted: cx("bg-band-1 text-ink-faint"),
  planned: cx("bg-band-1 text-ink-faint"),
  pending: cx("bg-busy-subtle text-busy"),
  waiting: cx("bg-busy-subtle text-busy"),
  running: cx("bg-accent-subtle text-accent"),
  completed: cx("bg-pass-subtle text-pass"),
  failed: cx("bg-fail-subtle text-fail"),
  canceled: cx("bg-band-1 text-ink-dim"),
};

/** A run's state on the card's top edge, in words. */
export function RunPillView({ pill }: { pill: RunPill }): JSX.Element {
  return (
    <span
      className={cx(
        "absolute -top-2.25 right-2.5 rounded-full border-2 border-canvas px-1.75 text-[10px]/[14px] font-semibold whitespace-nowrap",
        PILL_TONE[pill.status],
        pill.status === "running" && "motion-safe:animate-pulse-soft",
      )}
      data-status={pill.status}
    >
      {pill.text}
    </span>
  );
}
