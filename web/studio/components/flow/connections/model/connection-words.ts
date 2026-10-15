// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { cx } from "@/components/ui/cx";
import type { ConnectionStatus, ConnectionView, ReleaseManifest } from "@/lib/api/flow/connector";

/** A status's hue: green connected, red not, amber needs someone, grey cannot be set up here. */
type Tone = "pass" | "busy" | "fail" | "quiet";
type Words = { words: string; tone: Tone };

/** How a connection's status reads, and its hue. */
const STATUS_WORDS: Record<ConnectionStatus, Words> = {
  Ready: { words: "connected", tone: "pass" },
  Missing: { words: "not set up", tone: "fail" },
  Expired: { words: "sign in again", tone: "busy" },
  "Reauthorization required": { words: "sign in again", tone: "busy" },
  Conflict: { words: "set up again", tone: "fail" },
  Unsupported: { words: "can't be set up here", tone: "quiet" },
};

/** A connection that works: its credential is stored and current. */
export const isWorking = (connection: ConnectionView) => connection.status === "Ready";

/** A connection whose authorization lapsed, which its connection settings sign in again. */
export const needsSignIn = (connection: ConnectionView) =>
  connection.status === "Expired" || connection.status === "Reauthorization required";

export function connectionWords(connection: ConnectionView): Words {
  return STATUS_WORDS[connection.status];
}

/** A trigger in the connector's own words, such as "Receive a matching thread reply."; its name without one. */
export function triggerWords(manifest: ReleaseManifest | null, triggerName: string): string {
  return manifest?.spec.triggers?.find((trigger) => trigger.name === triggerName)?.description || triggerName;
}

/** "1 trigger · 2 steps": what of the code uses a connection name. */
export function usesWords(connection: ConnectionView): string {
  const triggers = (connection.triggerUses ?? []).length;
  const steps = new Set(connection.uses.map((use) => use.stepName || use.stepId)).size;
  return [
    triggers ? `${triggers} ${triggers === 1 ? "trigger" : "triggers"}` : "",
    steps ? `${steps} ${steps === 1 ? "step" : "steps"}` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

/*
 * The table's grid, laid out by the tool panel's width (container `tool`), never the viewport's: wide,
 * a row holds the name and a control per column; below 780px a row shows status chips and its controls
 * move into the open row, each captioned; below 520px they stack. Live's one column keeps its control in
 * the row and goes under the name only below 520px.
 */

/** A line of the two-column table: the name, Preview, Next publish and the fold. */
export const TWO_COLUMNS = cx(
  "grid grid-cols-[minmax(0,1fr)_minmax(232px,0.75fr)_minmax(232px,0.75fr)_22px] items-center gap-x-2.5 px-3.5",
  "@max-[780px]/tool:grid-cols-[minmax(0,1fr)_auto_22px]",
);
/**
 * The Setup bar, a box over the two-column table on its columns: Apply to Preview with Preview's state under it
 * over the Preview column, and Publish with the next publish's state under it over the Next publish column, each
 * button as wide as the column's dropdowns. Its border and padding match the table's. Once the columns become
 * chips, each state sits beside its button, and the two buttons, one width, end where the rows' chips end.
 */
export const ACTION_COLUMNS = cx(
  "grid grid-cols-[minmax(0,1fr)_minmax(232px,0.75fr)_minmax(232px,0.75fr)_22px] gap-x-2.5 gap-y-1.5",
  "rounded-[10px] border border-line-soft bg-panel px-3.5 py-3",
  "@max-[780px]/tool:grid-cols-[minmax(0,1fr)_auto_22px] @max-[780px]/tool:gap-y-2",
);
/** Where each part of the Setup bar sits on those columns, wide and once they become chips. */
export const ACTION_CELLS = {
  saving: cx(
    "col-start-1 row-start-1 self-center text-[12px] text-ink-faint",
    "@max-[780px]/tool:col-span-3 @max-[780px]/tool:row-start-3",
  ),
  apply: cx("col-start-2 row-start-1 w-full", "@max-[780px]/tool:w-38"),
  publish: cx(
    "col-start-3 row-start-1 w-full",
    "@max-[780px]/tool:col-start-2 @max-[780px]/tool:row-start-2 @max-[780px]/tool:w-38",
  ),
  preview: cx("col-start-2 row-start-2", "self-center @max-[780px]/tool:col-start-1 @max-[780px]/tool:row-start-1"),
  next: cx("col-start-3 row-start-2", "self-center @max-[780px]/tool:col-start-1 @max-[780px]/tool:row-start-2"),
} as const;
/** A line of Live's one-column table: the name, the next publish and the fold. */
export const ONE_COLUMN = cx(
  "grid grid-cols-[minmax(0,1fr)_minmax(262px,0.75fr)_22px] items-center gap-x-2.5 px-3.5",
  "@max-[780px]/tool:grid-cols-[minmax(0,1fr)_minmax(0,56%)_22px]",
  "@max-[520px]/tool:grid-cols-[minmax(0,1fr)_22px]",
);
/** A setting's line under a row of the two-column table: its name, then a control per column. */
export const TWO_COLUMN_SETTING = cx(
  "grid min-h-9.5 grid-cols-[minmax(0,1fr)_minmax(232px,0.75fr)_minmax(232px,0.75fr)_22px] items-center gap-x-2.5 px-3.5 text-[12.5px]",
  "@max-[780px]/tool:grid-cols-2 @max-[780px]/tool:gap-y-1 @max-[780px]/tool:pt-1.5 @max-[780px]/tool:pr-3.5 @max-[780px]/tool:pb-2 @max-[780px]/tool:pl-12",
  "@max-[520px]/tool:grid-cols-1 @max-[520px]/tool:pl-8.5",
);
/** A setting's line under a row of Live's one-column table. */
export const ONE_COLUMN_SETTING = cx(
  "grid min-h-9.5 grid-cols-[minmax(0,1fr)_minmax(262px,0.75fr)_22px] items-center gap-x-2.5 px-3.5 text-[12.5px]",
  "@max-[780px]/tool:grid-cols-[minmax(0,1fr)_minmax(0,56%)_22px]",
  "@max-[520px]/tool:grid-cols-1 @max-[520px]/tool:gap-y-1 @max-[520px]/tool:py-1.5 @max-[520px]/tool:pl-8.5",
);
/** A setting's name, indented under its row; it takes the whole line once the controls are captioned. */
export const SETTING_NAME = cx("min-w-0 pl-12 text-ink", "@max-[780px]/tool:col-span-full @max-[780px]/tool:pl-0");
/** A setting's name in Live's one column, which spans the line only once the control stacks under it. */
export const ONE_COLUMN_SETTING_NAME = cx("min-w-0 pl-12 text-ink", "@max-[520px]/tool:pl-0");
/** A cell's caption, Preview or Next publish, shown once the controls leave the row. */
export const CAPTION = cx("mb-0.5 hidden text-[11px] text-ink-faint @max-[780px]/tool:block");
/** What shows only while the row holds the controls, and what only once they move into the open row. */
export const WIDE_ONLY = cx("@max-[780px]/tool:hidden");
export const NARROW_ONLY = cx("hidden @max-[780px]/tool:flex");
export const NARROW_GRID = cx("hidden @max-[780px]/tool:grid");

/**
 * The hover hints of Setup's column labels and of the Setup bar's lines. The Next publish column on Preview and the Live tab
 * edit the same next-publish layer, but each shows the settings its own version declares, so the two match
 * only when Preview and Live run the same version.
 */
export const SETUP_HINTS = {
  preview: "What Preview runs with. Changes save as you make them and reach Preview when you apply them.",
  nextPublish:
    "What Live gets when you publish this version. Changes reach Live only when you publish. The Live tab shows the same settings when Live runs this version.",
  live: "What Live gets at its next publish. Changes reach Live only when you publish; a blue dot marks one that waits. Preview's Next publish column shows the same settings when Preview runs this version.",
} as const;
