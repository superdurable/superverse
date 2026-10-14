// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { cx } from "@/components/ui/cx";
import type { PhaseStatus, Reason } from "@/components/flow/canvas/model/run-overlay";
import type { Box, BoxRow } from "@/components/flow/canvas/layout/scene";

/**
 * How a card looks for what it is and what the run is doing, in the project map's style: a panel
 * card with a soft border and lift. Colour is a second channel, never the only one: a card always
 * writes its status as text, so removing every colour would lose emphasis, not meaning.
 */

/** The border says who must act: amber a person, teal someone outside, slate a timer, purple a child. */
const WAIT_EDGE = {
  external: cx("border-busy-rule"),
  unknown: cx("border-dashed border-busy-rule"),
  clock: cx("border-clock/60"),
  child: cx("border-child/60"),
  machine: null,
} as const;

/** Run state outranks the actor's border; otherwise the card keeps its quiet edge. */
function edgeOf(box: Box): string {
  if (box.emphasis === "failed") return cx("border-fail-rule");
  if (box.emphasis === "active") return cx("border-accent-rule");
  if (box.emphasis === "planned") return cx("border-dashed border-line");
  // Failure handling, drawn beside the Step that fails into it.
  if (box.detourOf !== undefined) return cx("border-dashed border-fail-rule");
  const actor = box.actor === undefined ? null : WAIT_EDGE[box.actor];
  return actor ?? cx("border-line-soft hover:border-line");
}

/**
 * A div[role=button], not a <button>: the jump chips and the outcome bar inside are themselves buttons
 * and nesting interactive controls is invalid.
 */
export function cardStyle(box: Box, selected: boolean): string {
  return cx(
    "relative flex cursor-pointer flex-col rounded-xl border px-3 pt-3 pb-2.5 text-left text-ink",
    edgeOf(box),
    box.emphasis === "failed" ? "bg-fail-subtle" : box.emphasis === "planned" ? "bg-panel/60" : "bg-panel",
    selected
      ? "shadow-[0_0_0_2px_var(--color-accent)]"
      : "shadow-[0_1px_2px_rgba(15,23,36,0.06)] hover:shadow-(--shadow-lift)",
  );
}

/** A stage card: a little stack of cards, because that is what it holds. */
export function stageStyle(box: Box, selected: boolean): string {
  return cx(
    "relative flex cursor-pointer flex-col rounded-2xl border bg-panel px-3.5 pt-3.5 pb-3 text-left text-ink",
    box.actor === "external" ? "border-busy-rule" : "border-line-soft hover:border-line",
    selected
      ? "shadow-[0_0_0_2px_var(--color-accent)]"
      : "shadow-[5px_5px_0_-1px_var(--color-panel),5px_5px_0_0_var(--color-line-soft)] hover:shadow-[5px_5px_0_-1px_var(--color-panel),5px_5px_0_0_var(--color-line),0_10px_28px_-12px_rgba(15,23,36,0.22)]",
  );
}

/** The card of an RPC in a Flow without Steps: an Action takes the door hue, a view is dashed. */
export function rpcCardStyle(box: Box, selected: boolean): string {
  return cx(
    "flex cursor-pointer flex-col gap-0.5 overflow-hidden rounded-xl px-3 py-2.5 text-left text-ink",
    box.rpcRole === "action"
      ? "border-2 border-door bg-linear-to-b from-door-subtle to-panel to-42%"
      : "border bg-panel",
    box.rpcRole === "view" ? "border-dashed border-door" : box.rpcRole === undefined && "border-line-soft",
    selected ? "shadow-[0_0_0_2px_var(--color-accent)]" : "shadow-[0_1px_2px_rgba(15,23,36,0.06)]",
  );
}

/**
 * The ring of the Step a run is at now, in the hue of who must act: amber for a person or someone
 * outside, red for a failure, slate for a timer, blue while the machine runs.
 */
export function nowRing(box: Box): string {
  return cx(
    "outline-3 outline-offset-2",
    box.emphasis === "failed"
      ? "outline-fail-rule"
      : box.actor === "external" || box.actor === "unknown"
        ? "outline-busy-rule"
        : box.actor === "clock"
          ? "outline-clock"
          : "outline-accent-rule",
  );
}

/** One segment of the state bar; the 2px floor keeps one failure among a thousand visible. */
export const SEGMENT = {
  notStarted: cx("bg-mark-faint"),
  canceled: cx("bg-mark-faint"),
  completed: cx("bg-pass"),
  waiting: cx("bg-accent"),
  running: cx("bg-accent"),
  pending: cx("bg-accent"),
  failed: cx("bg-fail"),
} satisfies Record<PhaseStatus, string>;

export const ROW_TONE = {
  normal: null,
  quiet: cx("text-ink-faint"),
  warn: cx("text-fail-rule"),
} satisfies Record<NonNullable<BoxRow["tone"]>, string | null>;

/** The reason line, only on a blocked or failed card, in the hue of who must act. */
export const REASON_TONE = {
  blocked: cx("text-busy"),
  failed: cx("text-fail"),
  terminal: cx("text-ink-dim"),
} satisfies Record<Reason["tone"], string>;
