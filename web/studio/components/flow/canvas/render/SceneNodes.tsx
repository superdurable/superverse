// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { Handle, type Node, type NodeProps, Position } from "@xyflow/react";
import type { JSX, KeyboardEvent, ReactNode } from "react";
import type { Box } from "@/components/flow/canvas/layout/scene";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { NodeBox } from "./NodeBox";
import { StageBox } from "./StageBox";

/** The React Flow node types of the canvas: a card (Step, stage or RPC), a group's band and the legend. */

/**
 * Anchors only. Visible handles would read as "you can connect these", which is exactly what this
 * surface must not suggest. React Flow drops an edge whose ends have no handle; the routed edge
 * draws the layout's own path and ignores where these sit.
 */
const ANCHOR = cx("pointer-events-none size-px min-h-0 min-w-0 border-0 bg-transparent opacity-0");

/**
 * Where those anchors sit, declared with each node. React Flow keeps measured handles only on a node
 * object that carries its measurements, and the canvas builds new node objects from the Scene on every
 * run poll and selection; without these, every such render dropped all the lines until the next
 * measurement and redrew them.
 */
export function anchorHandles(w: number, h: number): NonNullable<Node["handles"]> {
  return [
    { type: "target", position: Position.Top, x: w / 2, y: 0, width: 1, height: 1 },
    { type: "source", position: Position.Bottom, x: w / 2, y: h, width: 1, height: 1 },
  ];
}

export type BoxData = {
  box: Box;
  selected: boolean;
  now: boolean;
  onSelect: (id: string) => void;
  onOpen: (id: string) => void;
  onJump: (id: string) => void;
};

function BoxNode({ data }: NodeProps): JSX.Element {
  const d = data as unknown as BoxData;
  return (
    <>
      <Handle type="target" position={Position.Top} className={ANCHOR} />
      {d.box.kind === "stage" ? (
        <StageBox box={d.box} now={d.now} onOpen={d.onOpen} onJump={d.onJump} />
      ) : (
        <NodeBox box={d.box} selected={d.selected} now={d.now} onSelect={d.onSelect} onJump={d.onJump} />
      )}
      {d.box.mark && (
        <span
          className="pointer-events-none absolute -top-2.5 right-3 z-10 rounded-full border border-accent bg-panel px-1.75 text-[10.5px] font-semibold text-accent"
          data-mark={d.box.mark}
        >
          {d.box.mark}
        </span>
      )}
      {d.now && (
        <span
          aria-hidden="true"
          data-now-tag="true"
          className="pointer-events-none absolute -bottom-2.5 left-1/2 z-10 -translate-x-1/2 rounded-full border border-line bg-panel px-1.75 text-[10.5px] font-semibold text-ink"
        >
          now
        </span>
      )}
      <Handle type="source" position={Position.Bottom} className={ANCHOR} />
    </>
  );
}

/**
 * A group's band, hugging its members. It is achromatic, because colour on this canvas says who must
 * act and a region is not an actor. Its label selects the stage; its ⊖ closes the stage into one card.
 */
function BandNode({ data }: NodeProps): JSX.Element {
  const d = data as unknown as {
    groupId: string;
    label: string;
    hue: number;
    w: number;
    h: number;
    selected: boolean;
    collapsible: boolean;
    onPick: (groupId: string) => void;
    onClose: (groupId: string) => void;
  };
  const act = () => d.onPick(d.groupId);
  return (
    <div
      className={cx(
        "pointer-events-auto rounded-2xl border border-dashed",
        d.hue % 2 === 0 ? "bg-band-0/70" : "bg-band-1/70",
        d.selected ? "border-solid border-ink-faint" : "border-line-soft",
      )}
      data-band={d.groupId}
      style={{ width: d.w, height: d.h }}
    >
      <span
        role="button"
        tabIndex={0}
        className="absolute top-1.5 left-3 flex cursor-pointer items-center gap-1 rounded-md px-0.5 text-[10.5px] font-semibold tracking-[0.09em] text-ink-faint uppercase hover:text-ink-dim focus-visible:outline-2 focus-visible:outline-accent"
        title={`${d.label}: the steps this stage groups`}
        onClick={act}
        onKeyDown={(e: KeyboardEvent<HTMLSpanElement>) => {
          if (e.key !== "Enter" && e.key !== " ") return;
          e.preventDefault();
          act();
        }}
      >
        {d.label}
      </span>
      {d.collapsible ? (
        <button
          type="button"
          className="nodrag nopan absolute top-1 right-1.5 grid size-6 cursor-pointer place-items-center rounded-full text-ink-dim hover:bg-band-1 hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
          aria-label={`Close ${d.label} into one card`}
          title={`Close ${d.label} into one card`}
          onClick={() => d.onClose(d.groupId)}
        >
          <Glyph name="minusCircle" className="size-4.5" />
        </button>
      ) : null}
    </div>
  );
}

/** The legend is a node in scene coordinates, so it pans and zooms with what it decodes. */
function LegendNode({ data }: NodeProps): JSX.Element {
  const d = data as unknown as { legend: ReactNode };
  return (
    <div className="nodrag nopan pointer-events-auto flex h-full flex-col items-start justify-end">{d.legend}</div>
  );
}

export const NODE_TYPES = { pbox: BoxNode, band: BandNode, legend: LegendNode };
