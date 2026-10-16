// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState, type JSX } from "react";
import type { BoxChip, LinkFamily, Scene, Tile } from "@/components/flow/canvas/layout/scene";
import { TileView } from "@/components/flow/canvas/render/CardParts";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";

/**
 * A legend derived from what is on screen: rows appear only when the scene contains them, and the
 * counts come from the scene, so the legend doubles as a census.
 */

const ROW = cx("flex items-center gap-2 py-[2px] text-[13px]");
const ROW_TEXT = cx("min-w-0 text-ink-dim");
const ROW_COUNT = cx("ml-auto font-mono text-xs text-ink-faint");
const HEAD = cx("mt-2 mb-0.5 text-[10.5px] font-semibold tracking-wider text-ink-faint uppercase first:mt-0");

/** What a tile means, keyed as `tileKey` names it, in legend order. */
const TILE_MEANING: [string, string][] = [
  ["person", "waits for a person"],
  ["door", "waits for a message from outside"],
  ["wait", "waits on something the analyser could not name"],
  ["clock", "waits for time"],
  ["child", "waits for child flows"],
  ["spark", "asks an AI model"],
  ["app", "calls an app through a connector"],
  ["cpu", "runs automatically"],
];

function tileKey(tile: Tile): string {
  return "glyph" in tile ? tile.glyph : "app";
}

const SAMPLE_TILE: Record<string, Tile> = {
  person: { glyph: "person", tone: "busy" },
  door: { glyph: "door", tone: "door" },
  wait: { glyph: "wait", tone: "busy" },
  clock: { glyph: "clock", tone: "clock" },
  child: { glyph: "child", tone: "child" },
  spark: { glyph: "spark", tone: "accent" },
  app: { monogram: "AP", tone: "app" },
  cpu: { glyph: "cpu", tone: "machine" },
};

const LINE: Record<LinkFamily, { swatch: string; label: string }> = {
  control: { swatch: cx("border-t-2 border-mark"), label: "what happens next" },
  failure: { swatch: cx("border-t-2 border-dashed border-fail-rule"), label: "where a failure goes" },
};

const CHIP: Record<BoxChip["kind"], { sample: string; tone: string; label: string }> = {
  answer: { sample: "Approve", tone: cx("bg-busy-subtle text-busy"), label: "an Action that answers the step" },
  entry: { sample: "⚡ call", tone: cx("bg-door-subtle text-door"), label: "a call that starts the step" },
  child: { sample: "↳ flow", tone: cx("bg-child-subtle text-child"), label: "a child flow the step starts" },
  inbound: { sample: "← 6", tone: cx("bg-band-0 text-ink-faint"), label: "steps that lead here" },
  fork: { sample: "▮", tone: cx("bg-band-0 text-ink-dim"), label: "runs every path at once" },
  jump: { sample: "↪ step", tone: cx("bg-band-0 text-ink-dim"), label: "a jump to a step drawn elsewhere" },
  failJump: { sample: "↪ step", tone: cx("bg-fail-subtle text-fail"), label: "where a failure jumps" },
  ends: { sample: "✓", tone: cx("bg-pass-subtle text-pass"), label: "completes the flow" },
  stops: { sample: "■", tone: cx("bg-band-1 text-ink-dim"), label: "stops one branch of the flow" },
  fails: { sample: "✕", tone: cx("bg-fail-subtle text-fail"), label: "fails the flow" },
};

function increment(counts: Map<string, number>, key: string): void {
  counts.set(key, (counts.get(key) ?? 0) + 1);
}

/** The collapsible legend card drawn beside the Flow: the census, then what the drawing leaves out. */
export function CanvasLegend({ scene }: { scene: Scene }): JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    // Scene furniture rather than a floating panel: a dashed outline and translucent fill, no shadow.
    // Collapsed, it is only its own toggle, a chip sized to its text.
    <div
      className={cx("rounded-xl border border-dashed border-line-soft bg-bg/85 hover:border-mark", open && "w-full")}
      data-open={open ? "true" : undefined}
    >
      <button
        type="button"
        className={cx(
          "block text-left text-xs tracking-wider text-ink-faint uppercase hover:text-ink-dim",
          open ? "w-full px-3 py-1.75" : "px-2.5 py-1.25",
        )}
        onClick={() => setOpen((current) => !current)}
        aria-expanded={open}
      >
        Legend {open ? "▾" : "▸"}
      </button>
      {open ? (
        <>
          <Census scene={scene} />
          {scene.notes.length > 0 ? (
            <div className="mt-0.5 border-t border-line-faint px-3 pb-2.5">
              {scene.notes.map((note) => (
                <p className="mt-2 text-[12.5px]/normal text-ink-faint" key={note}>
                  {note}
                </p>
              ))}
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  );
}

function Census({ scene }: { scene: Scene }): JSX.Element | null {
  const tiles = new Map<string, number>();
  const chips = new Map<string, number>();
  for (const box of scene.boxes) {
    if (box.tile) increment(tiles, tileKey(box.tile));
    for (const tile of box.stage?.tiles ?? []) increment(tiles, tileKey(tile));
    for (const row of box.chips ?? []) for (const chip of row) increment(chips, chip.kind);
  }
  const lines = new Map<string, number>();
  for (const link of scene.links) increment(lines, link.family);
  const stages = scene.boxes.filter((box) => box.kind === "stage").length;
  const unwritten = scene.boxes.filter((box) => box.unwritten).length;
  const marks = [
    scene.boxes.some((box) => box.isStart) && { glyph: "bolt" as const, label: "where the flow begins" },
    scene.boxes.some((box) => box.loops || box.stage?.loops) && {
      glyph: "refresh" as const,
      label: "runs again on its own",
    },
  ].filter((mark) => mark !== false);
  if (tiles.size === 0 && lines.size === 0 && chips.size === 0) return null;
  return (
    <section className="border-b border-line-faint px-3.5 pt-2.5 pb-1.5">
      {tiles.size > 0 ? (
        <>
          <h4 className={HEAD}>Who acts</h4>
          <ul>
            {TILE_MEANING.filter(([key]) => tiles.has(key)).map(([key, label]) => (
              <li className={ROW} key={key} data-tile={key}>
                <span className="origin-left scale-[0.7]">
                  <TileView tile={SAMPLE_TILE[key]} />
                </span>
                <span className={cx(ROW_TEXT, "-ml-2.5")}>{label}</span>
                <span className={ROW_COUNT}>{tiles.get(key)}</span>
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {stages > 0 || unwritten > 0 || lines.size > 0 || marks.length > 0 ? <h4 className={HEAD}>Shapes</h4> : null}
      <ul>
        {unwritten > 0 ? (
          <li className={ROW} data-shape="unwritten">
            <span className="h-3 w-4 flex-none rounded-[4px] border border-dashed border-line bg-panel/60" />
            <span className={ROW_TEXT}>a step whose code is not written yet</span>
            <span className={ROW_COUNT}>{unwritten}</span>
          </li>
        ) : null}
        {stages > 0 ? (
          <li className={ROW}>
            <span className="h-3 w-4 flex-none rounded-[4px] border border-line-soft shadow-[2px_2px_0_-1px_var(--color-panel),2px_2px_0_0_var(--color-line-soft)]" />
            <span className={ROW_TEXT}>a stage: click it to see its steps</span>
            <span className={ROW_COUNT}>{stages}</span>
          </li>
        ) : null}
        {(["control", "failure"] as const)
          .filter((family) => lines.has(family))
          .map((family) => (
            <li className={ROW} key={family}>
              <span className={cx("h-0 w-4 flex-none", LINE[family].swatch)} data-family={family} />
              <span className={ROW_TEXT}>{LINE[family].label}</span>
              <span className={ROW_COUNT}>{lines.get(family)}</span>
            </li>
          ))}
        {marks.map((mark) => (
          <li className={ROW} key={mark.label}>
            <Glyph name={mark.glyph} className="size-3.5 text-ink-faint" />
            <span className={ROW_TEXT}>{mark.label}</span>
          </li>
        ))}
      </ul>
      {chips.size > 0 ? (
        <>
          <h4 className={HEAD}>On a card</h4>
          <ul>
            {(Object.keys(CHIP) as BoxChip["kind"][])
              .filter((kind) => chips.has(kind))
              .map((kind) => (
                <li className={ROW} key={kind} data-chip={kind}>
                  <span className={cx("flex-none rounded-full px-1.5 text-[10.5px]/[16px]", CHIP[kind].tone)}>
                    {CHIP[kind].sample}
                  </span>
                  <span className={ROW_TEXT}>{CHIP[kind].label}</span>
                  <span className={ROW_COUNT}>{chips.get(kind)}</span>
                </li>
              ))}
          </ul>
        </>
      ) : null}
    </section>
  );
}
