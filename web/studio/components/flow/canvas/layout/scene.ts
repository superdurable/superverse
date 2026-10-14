// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { StepGroup } from "@/components/flow/canvas/model/step-groups";
import type { Actor } from "@/components/flow/canvas/model/flow-model";
import type { PhaseStatus, Reason, RunOverlay } from "@/components/flow/canvas/model/run-overlay";

/**
 * A Scene is plain geometry: the layout produces one and only `render/Stage.tsx` turns it into
 * React Flow nodes.
 */

/** Layout direction is a setting: a Flow reads top-down by default, as Zapier draws it. */
export type Direction = "tb" | "lr";

export interface ViewOpts {
  direction: Direction;
  /** Run state, or null for the definition. Execution is strictly additive to the drawing. */
  run: RunOverlay | null;
  /**
   * Semantic groups from the definition, its `dex:group` labels. A group that is a run or a choice of
   * two or more Steps is a stage: one card while it is closed, its Steps in one labelled box while it
   * is open. Opening and closing stages is the only thing Collapsed and Expanded change.
   */
  groups?: StepGroup[];
  /** The stages drawn open; Collapsed opens none and Expanded every one. */
  openStages?: ReadonlySet<string>;
}

/**
 * The kind of card a box draws: a Step, a collapsed stage, an RPC of a Flow without Steps, or a
 * pointer in a failure detour to a main-story Step drawn elsewhere.
 */
type BoxKind = "step" | "stage" | "rpcCard" | "pointer";

/** The icon tile at a card's left: a Glyph in the tone of who acts, or an app's monogram. */
export type Tile =
  | { glyph: "person" | "door" | "cpu" | "clock" | "child" | "wait" | "spark"; tone: Tone }
  | { monogram: string; tone: Tone };

/** The palette tones a card may take; each maps to the token set in app/globals.css. */
export type Tone = "busy" | "door" | "machine" | "clock" | "child" | "accent" | "app";

/**
 * Something docked to a card instead of drawn as a box with a line: where a Step jumps when the
 * line would cross the drawing, how it ends the Flow, the Actions that answer it, the call that starts
 * it and the child Flow it launches.
 */
export interface BoxChip {
  kind: "jump" | "failJump" | "inbound" | "fork" | "ends" | "stops" | "fails" | "answer" | "entry" | "child";
  text: string;
  /** The Step a jump leads to, so a click can select it. */
  targetId?: string;
  title?: string;
}

export interface BoxRow {
  /** A small leading glyph. Text, not an icon font. */
  glyph?: string;
  text: string;
  /** Right-aligned trailing text. Identity only, never an expression. */
  trail?: string;
  tone?: "normal" | "quiet" | "warn";
}

/** What a collapsed stage card summarizes about its Steps. */
export interface StageSummary {
  stepCount: number;
  /** One tile per member Step, in reading order. */
  tiles: Tile[];
  /** Plain facts: who it waits for and the apps it uses. */
  facts: string;
  /** Some transition stays inside the stage, so it repeats. */
  loops: boolean;
}

/** A card's run state, written as words so it never rests on colour. */
export interface RunPill {
  status: PhaseStatus | "planned";
  text: string;
}

export interface Box {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
  kind: BoxKind;
  title: string;
  /** The durable identifier behind a friendly title, for hover and the panel. */
  identity?: string;
  /** What the box does or waits for, never a value. */
  subtitle?: string;
  subtitleTone?: Tone;
  tile?: Tile;
  /** Drives the border that says who has to act. */
  actor?: Actor;
  /** Chips packed into rows that fit the card's width. */
  chips?: BoxChip[][];
  stage?: StageSummary;
  /** What an `rpcCard` is in the Flow v2 definition, when it is an Action or a view. */
  rpcRole?: "action" | "view";
  pill?: RunPill;
  /** One templated line, rendered only when blocked or failed. */
  reason?: Reason;
  rows?: BoxRow[];
  /** The Step can transition to itself (BPMN's loop marker). */
  loops?: boolean;
  /** A second mark for a Step that is also a shared failure target; a gate can be both. */
  recovery?: { glyph: string; title: string };
  /** Where the Flow begins: a definition fact, so it does not compete with run state for emphasis. */
  isStart?: boolean;
  emphasis?: "active" | "failed" | "planned";
  /** The proportional state bar for a StepType whose executions ended differently. */
  bar?: { status: PhaseStatus; count: number }[];
  /** The bar's hover decode: a count per status plus the wait-kind breakdown. */
  barTitle?: string;
  /** A stage that holds the Step the run is waiting on, so it can carry the "now" ring. */
  holds?: string[];
  /** Failure transitions that leave this Step, drawn only when it is selected. Stage cards carry none. */
  failures?: number;
  /** Drawn beside this box, a Step or the stage holding it, as part of its failure detour. */
  detourOf?: string;
  /** Where a pointer leads: a main-story Step, or the collapsed stage that holds it. */
  target?: string;
  /** A design's mark against the current source, drawn as text above the card. */
  mark?: "new" | "changed";
}

/** `control` and `failure` are flow inside the process. */
export type LinkFamily = "control" | "failure";

export interface Link {
  id: string;
  from: string;
  to: string;
  family: LinkFamily;
  /** The routed polyline, in scene coordinates, start to end. */
  points: { x: number; y: number }[];
  /**
   * A case value the line is taken on, always shown where the layout placed it; or a count of merged
   * conditions, shown only while one end is selected, since full conditions belong in the panel.
   */
  label?: { text: string; at?: { x: number; y: number }; always: boolean };
}

/** A labelled background region hugging the members of one group, never another Step. */
export interface Band {
  id: string;
  groupId: string;
  x: number;
  y: number;
  w: number;
  h: number;
  label: string;
  /** Alternates the tint, so adjacent regions are distinguishable without meaning anything. */
  hue: number;
  /** An open stage, which its ⊖ closes into one card again. */
  collapsible: boolean;
}

export interface Scene {
  boxes: Box[];
  links: Link[];
  bands: Band[];
  width: number;
  height: number;
  /** Shown in the legend, to admit what the drawing cannot show. */
  notes: string[];
}

export function boundsOf(boxes: Box[], bands: Band[], links: Link[] = [], pad = 40): { width: number; height: number } {
  let maxX = 0;
  let maxY = 0;
  for (const b of [...boxes, ...bands]) {
    maxX = Math.max(maxX, b.x + b.w);
    maxY = Math.max(maxY, b.y + b.h);
  }
  // A side lane can run just beside the drawing.
  for (const p of links.flatMap((link) => link.points)) {
    maxX = Math.max(maxX, p.x);
    maxY = Math.max(maxY, p.y);
  }
  return { width: maxX + pad, height: maxY + pad };
}
