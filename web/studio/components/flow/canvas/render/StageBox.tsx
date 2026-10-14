// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { JSX } from "react";
import type { Box } from "@/components/flow/canvas/layout/scene";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { nowRing, stageStyle } from "./box-styles";
import { ChipRows, RunPillView, StartPill, TileView } from "./CardParts";
import { keyActivates } from "./NodeBox";

/**
 * A collapsed stage: its label, how many Steps it holds, each kind of tile its Steps carry once, who
 * it waits for and the apps it uses. Activating it opens the stage in place. Its measurements are
 * layout/stage-card.ts's; change them together.
 */
export function StageBox({
  box,
  now,
  onOpen,
  onJump,
}: {
  box: Box;
  /** The stage holds the Step the run is at now. */
  now: boolean;
  onOpen: (id: string) => void;
  onJump: (id: string) => void;
}): JSX.Element | null {
  const stage = box.stage;
  if (stage === undefined) return null;
  const open = () => onOpen(box.id);
  return (
    <div
      role="button"
      tabIndex={0}
      className={cx("nodrag nopan group", stageStyle(box, false), now && nowRing(box))}
      data-kind="stage"
      data-pick={box.title}
      data-now={now ? "true" : undefined}
      aria-label={`${box.title}, ${stage.stepCount} steps${box.pill ? `, ${box.pill.text}` : ""}${box.mark ? `, ${box.mark}` : ""}. Open the stage.`}
      title={`${box.title}: ⊕ opens it to show its ${stage.stepCount} steps`}
      style={{ width: `${box.w}px`, height: `${box.h}px` }}
      onClick={open}
      onKeyDown={(e) => keyActivates(e, open)}
    >
      {box.isStart ? <StartPill /> : null}
      {box.pill ? <RunPillView pill={box.pill} /> : null}
      <span className="flex items-baseline gap-2">
        <b className="min-w-0 flex-1 truncate text-[14.5px]/5 font-semibold tracking-[-0.01em]">{box.title}</b>
        {stage.loops ? (
          <span className="flex-none self-center text-ink-faint" title="Some steps here repeat">
            <Glyph name="refresh" className="size-3.25" />
          </span>
        ) : null}
        <span className="flex-none font-mono text-[11px] text-ink-faint">
          {stage.stepCount} step{stage.stepCount === 1 ? "" : "s"}
        </span>
        <span className="-mr-1 flex-none self-center text-ink-faint group-hover:text-accent" aria-hidden="true">
          <Glyph name="plusCircle" className="size-4.5" />
        </span>
      </span>
      <span className="mt-2 flex h-3.5 items-center gap-1" aria-hidden="true">
        {stage.tiles.map((tile, i) => (
          <TileView key={i} tile={tile} small />
        ))}
      </span>
      <span className="mt-1.5 truncate text-[11.5px]/4 text-ink-dim">{stage.facts}</span>
      <ChipRows rows={box.chips ?? []} onJump={onJump} />
    </div>
  );
}
