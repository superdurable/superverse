// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import {
  answeringEntries,
  whoActs,
  type Branch,
  type FlowModel,
  type StepModel,
  type WhoActs,
} from "@/components/flow/canvas/model/flow-model";
import {
  barDecode,
  cardStatus,
  isOpenStatus,
  runStateFor,
  stepReason,
  type PhaseStatus,
  type RunState,
} from "@/components/flow/canvas/model/run-overlay";
import { fanOutOf } from "@/components/flow/canvas/model/branch-routing";
import { processName, stepName } from "@/lib/shared/format";
import type { GraphEdge } from "./main-line";
import type { Box, BoxChip, RunPill, Tile, Tone, ViewOpts } from "./scene";

/**
 * The Step card, built from the project map's parts: a tile that says who acts (or which app runs),
 * the Step in words, what it does, and chips for what is docked to it. Its height is computed from
 * that content, never from zoom, so a Scene lays out identically everywhere. These measurements are
 * the card in render/NodeBox.tsx; change them together.
 */

const STEP_W = 256;
/** Padding, a 34px tile and two text lines. */
const HEAD_H = 62;
/** A title that wraps takes one more line. */
const TITLE_LINE_H = 18;
/** Characters of a 13.5px semibold title that fit beside the tile, rounded down to stay safe. */
const TITLE_CHARS = 23;
const CHIP_ROW_H = 22;
const CHIPS_PAD = 8;
/** Inside width for chips: the card less its 12px padding each side. */
const CHIP_ROOM = STEP_W - 24;
const REASON_H = 24;
const BAR_H = 10;

/** Connector ids people know by another name, and the letters their tile shows. */
const APPS: Record<string, { name: string; monogram: string }> = {
  github: { name: "GitHub", monogram: "GH" },
  gmail: { name: "Gmail", monogram: "GM" },
  slack: { name: "Slack", monogram: "SL" },
  "google-drive": { name: "Google Drive", monogram: "GD" },
  "google-sheets": { name: "Google Sheets", monogram: "GS" },
  "google-calendar": { name: "Google Calendar", monogram: "GC" },
  notion: { name: "Notion", monogram: "NO" },
  stripe: { name: "Stripe", monogram: "ST" },
  hubspot: { name: "HubSpot", monogram: "HS" },
  linkedin: { name: "LinkedIn", monogram: "LI" },
};

/** The `llm` connector reaches any model provider, so it is "AI model", not an app. */
const MODEL_CONNECTOR = "llm";

function appOf(connectorId: string): { name: string; monogram: string } {
  const known = APPS[connectorId];
  if (known) return known;
  const name = processName(connectorId);
  return {
    name,
    monogram: name
      .replace(/[^A-Za-z0-9]/g, "")
      .slice(0, 2)
      .toUpperCase(),
  };
}

/** What people call the service a connector reaches. */
export function connectorName(connectorId: string): string {
  return connectorId === MODEL_CONNECTOR ? "AI model" : appOf(connectorId).name;
}

const WHO_TILE: Record<WhoActs, Tile> = {
  person: { glyph: "person", tone: "busy" },
  outside: { glyph: "door", tone: "door" },
  machine: { glyph: "cpu", tone: "machine" },
  clock: { glyph: "clock", tone: "clock" },
  child: { glyph: "child", tone: "child" },
  waits: { glyph: "wait", tone: "busy" },
};

/** The tile of a Step: its app when a connector runs it, otherwise who acts. */
export function tileOf(flow: FlowModel, step: StepModel): Tile {
  const who = whoActs(flow, step);
  if (who === "machine" && step.connector) {
    return step.connector.connectorId === MODEL_CONNECTOR
      ? { glyph: "spark", tone: "accent" }
      : { monogram: appOf(step.connector.connectorId).monogram, tone: "app" };
  }
  return WHO_TILE[who];
}

function subtitleOf(flow: FlowModel, step: StepModel): { text: string; tone: Tone } {
  const who = whoActs(flow, step);
  const timers = (step.waitFor?.conditions ?? []).some((c) => c.kind === "timer");
  if (who === "machine" && step.connector) {
    const op = stepName(step.connector.operationId).toLowerCase();
    const model = step.connector.connectorId === MODEL_CONNECTOR;
    return { text: `${connectorName(step.connector.connectorId)} · ${op}`, tone: model ? "accent" : "machine" };
  }
  switch (who) {
    case "person":
      return { text: timers ? "Waits for a person or time" : "Waits for a person", tone: "busy" };
    case "outside":
      return { text: timers ? "Waits for a message or time" : "Waits for a message", tone: "door" };
    case "clock":
      return { text: "Waits for time", tone: "clock" };
    case "child":
      return { text: "Waits for child flows", tone: "child" };
    case "waits":
      return { text: "Waits · not reported", tone: "busy" };
    case "machine":
      return { text: "Automatic", tone: "machine" };
  }
}

/** How a Step ends the Flow: certainly when every branch closes this way, otherwise as one outcome. */
const ENDS: Record<NonNullable<Branch["closes"]>, { kind: BoxChip["kind"]; always: string; may: string }> = {
  complete: { kind: "ends", always: "✓ completes", may: "✓ can complete" },
  completeIfChannelsEmpty: { kind: "ends", always: "✓ completes if queues are empty", may: "✓ can complete" },
  deadEnd: { kind: "stops", always: "■ stops this branch", may: "■ can stop this branch" },
  fail: { kind: "fails", always: "✕ fails the flow", may: "✕ can fail the flow" },
};

/**
 * A jump docked to its source, with the title of where it leads: a Step, or a collapsed stage. An
 * exit leaves a failure detour for the main story.
 */
export type DockedJump = GraphEdge & { label: string; exit?: boolean };

/** What the layout docks to a Step card instead of drawing. */
export interface Docked {
  jumps: DockedJump[];
  arriving: number;
  /** Failure transitions that leave the Step; the card counts them and selecting it draws them. */
  failures?: number;
  /** In a failure detour: the other Steps whose failures this one also catches. */
  alsoCatches?: number;
}

/** Everything docked to a Step, in the order a reader wants it: who answers, how it starts, where it jumps, how it ends. */
function chipsOf(flow: FlowModel, step: StepModel, { jumps, arriving, alsoCatches = 0 }: Docked): BoxChip[] {
  const chips: BoxChip[] = [];
  const answers = answeringEntries(flow, step);
  for (const entry of answers.slice(0, 4)) {
    chips.push({
      kind: "answer",
      text: entry.action?.label ?? stepName(entry.name),
      title: `${entry.name} answers this step`,
    });
  }
  if (answers.length > 4) chips.push({ kind: "answer", text: `+${answers.length - 4} more` });
  for (const entry of flow.entries) {
    if (!entry.branches.some((b) => b.targets.some((t) => t.stepId === step.id))) continue;
    chips.push({ kind: "entry", text: `⚡ ${stepName(entry.name)}`, title: `The call ${entry.name} starts this step` });
  }
  for (const subflow of flow.subflows) {
    if (subflow.startedByStepId !== step.id) continue;
    chips.push({ kind: "child", text: `↳ ${processName(subflow.flowType)}`, title: `Starts ${subflow.flowType}` });
  }
  // A fork starts every path at once, which lines alone cannot tell from a choice.
  const fanOut = fanOutOf(step);
  if (fanOut > 0) chips.push({ kind: "fork", text: fanOut === 2 ? "▮ both paths run" : `▮ all ${fanOut} paths run` });
  if (arriving > 3)
    chips.push({ kind: "inbound", text: `← ${arriving} steps lead here`, title: "The nearest three are drawn" });
  if (alsoCatches > 0)
    chips.push({
      kind: "inbound",
      text: `← also catches ${alsoCatches} other step${alsoCatches === 1 ? "" : "s"}`,
      title: "Other steps fail into this one too",
    });
  for (const jump of jumps) {
    const text = jump.exit ? `↩ back to ${jump.label}` : `↪ ${jump.label}`;
    chips.push(
      jump.failure
        ? { kind: "failJump", text, targetId: jump.to, title: "On failure" }
        : { kind: "jump", text, targetId: jump.to },
    );
  }
  const branches = step.execute.branches;
  for (const close of new Set(branches.flatMap((b) => (b.closes ? [b.closes] : [])))) {
    const always = branches.every((b) => b.closes === close);
    chips.push({ kind: ENDS[close].kind, text: always ? ENDS[close].always : ENDS[close].may });
  }
  return chips;
}

/** A chip's width at the card's 10.5px chip size: about 6px a character plus its padding. */
function chipWidth(chip: BoxChip): number {
  return Math.min(CHIP_ROOM, Math.ceil(chip.text.length * 6.1) + 16);
}

export function packChips(chips: BoxChip[], room = CHIP_ROOM): BoxChip[][] {
  const rows: BoxChip[][] = [];
  let row: BoxChip[] = [];
  let used = 0;
  for (const chip of chips) {
    const w = chipWidth(chip);
    if (row.length > 0 && used + w > room) {
      rows.push(row);
      row = [];
      used = 0;
    }
    row.push(chip);
    used += w + 4;
  }
  if (row.length > 0) rows.push(row);
  return rows;
}

/** Characters a title gives up to the ⚠ count beside it. */
const FAILURES_CHARS = 4;

/** Lines a title takes beside the tile: one, or two when its words do not fit. */
function titleLines(title: string, chars = TITLE_CHARS): 1 | 2 {
  let line = 0;
  let lines = 1;
  for (const word of title.split(" ")) {
    const need = line === 0 ? word.length : line + 1 + word.length;
    if (need > chars && line > 0) {
      lines += 1;
      line = word.length;
    } else line = need;
  }
  return lines > 1 ? 2 : 1;
}

const PILL_TEXT: Record<PhaseStatus, string> = {
  notStarted: "Not started",
  pending: "Waiting",
  waiting: "Waiting",
  running: "Running",
  completed: "✓ Done",
  failed: "✕ Failed",
  canceled: "Canceled",
};

function pillOf(run: RunState | null, person: boolean): RunPill | undefined {
  if (run === null) return undefined;
  if (run.planned) return { status: "planned", text: "Next" };
  const status = cardStatus(run.current);
  const base = (status === "waiting" || status === "pending") && person ? "Waiting for you" : PILL_TEXT[status];
  const count = run.executions.length;
  return { status, text: count > 1 ? `${base} ×${count}` : base };
}

/** The title a reader sees: the analyser's display name, or the Step type in words. */
export function stepTitle(step: StepModel): string {
  return step.label === step.stepType ? stepName(step.stepType) : step.label;
}

/** A Step's card, at the origin; the layout moves it. */
export function stepBox(flow: FlowModel, step: StepModel, opts: ViewOpts, docked: Docked): Box {
  const overlay = opts.run;
  const run = overlay === null ? null : runStateFor(overlay, step.stepType);
  const executed = run !== null && !run.planned ? run : null;
  const status = executed === null ? null : cardStatus(executed.current);
  const title = stepTitle(step);
  const chips = packChips(chipsOf(flow, step, docked));
  const failures = docked.failures ?? 0;
  const bar = executed !== null && executed.bar.length > 1 ? executed.bar : undefined;
  const reason = overlay === null ? undefined : (stepReason(overlay, step.stepType) ?? undefined);
  const subtitle = subtitleOf(flow, step);
  const h =
    HEAD_H +
    (titleLines(title, failures > 0 ? TITLE_CHARS - FAILURES_CHARS : TITLE_CHARS) - 1) * TITLE_LINE_H +
    (chips.length > 0 ? chips.length * CHIP_ROW_H + CHIPS_PAD : 0) +
    (reason === undefined ? 0 : REASON_H) +
    (bar === undefined ? 0 : BAR_H);
  return {
    id: step.id,
    x: 0,
    y: 0,
    w: STEP_W,
    h,
    kind: "step",
    title,
    identity: step.stepType,
    subtitle: subtitle.text,
    subtitleTone: subtitle.tone,
    tile: tileOf(flow, step),
    actor: step.actor,
    chips,
    pill: pillOf(run, whoActs(flow, step) === "person"),
    reason,
    bar,
    barTitle: bar === undefined || executed === null ? undefined : barDecode(bar, executed.executions),
    loops: flow.transitions.some((t) => t.isSelfLoop && t.fromStepId === step.id),
    recovery: recoveryMark(step),
    failures: failures > 0 ? failures : undefined,
    isStart: step.isStart || undefined,
    emphasis:
      run?.planned === true
        ? "planned"
        : status === "failed"
          ? "failed"
          : status && isOpenStatus(status)
            ? "active"
            : undefined,
  };
}

/** Whether the Flow stops at this failure target or carries on from it. */
function recoveryMark(step: StepModel): Box["recovery"] {
  const failures = step.inboundFailureCount;
  if (step.recoveryRole === "sink")
    return { glyph: "⇥", title: `Catches failures from ${failures} steps, and the flow stops here` };
  if (step.recoveryRole === "dispatcher")
    return { glyph: "⇉", title: `Catches failures from ${failures} steps and sends the flow on` };
  return undefined;
}
