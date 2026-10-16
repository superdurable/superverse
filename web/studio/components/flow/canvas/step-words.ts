// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import {
  answeringEntries,
  failureRoutes,
  resourceById,
  stepById,
  whoActs,
  type Branch,
  type Condition,
  type FlowModel,
  type StepModel,
  type WhoActs,
} from "@/components/flow/canvas/model/flow-model";
import { caseValues } from "@/components/flow/canvas/model/branch-routing";
import { connectorName, MODEL_CONNECTOR, stepTitle } from "@/components/flow/canvas/layout/step-card";
import { processName, stepName } from "@/lib/shared/format";

/**
 * A Step in the words a business person would use: what it does, what it waits for and where it goes
 * next. Every line is read from the definition; what the definition does not say is never guessed.
 */

/** One way out: "If generated → Await summary approval", "Then → Save summary result". */
interface WayOut {
  /** "If <outcome>", "Then", "Or" or "If it keeps failing". */
  lead: string;
  /** A Step's title, or how the process ends. */
  to: string;
  failure: boolean;
}

export interface StepWords {
  does: string;
  waits: string;
  ways: WayOut[];
}

const OR = new Intl.ListFormat("en", { type: "disjunction" });
const AND = new Intl.ListFormat("en", { type: "conjunction" });

const unique = <T>(items: T[]): T[] => [...new Set(items)];

/** `Invalid response` reads "invalid response" mid-sentence; `AI failed` keeps its acronym. */
function midSentence(words: string): string {
  return words.length > 1 && words[1] === words[1].toLowerCase() ? words[0].toLowerCase() + words.slice(1) : words;
}

/** An outcome or case value in words: `refundmodel.BillingConfirmed` reads "billing confirmed". */
function valueWords(value: string): string {
  const name = /^[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)+$/.test(value) ? (value.split(".").pop() ?? value) : value;
  return midSentence(stepName(name));
}

function titleOf(flow: FlowModel, stepId: string): string {
  const step = stepById(flow, stepId);
  return step ? stepTitle(step) : stepName(stepId.replace(/^step:/, ""));
}

/** A wait that is declared but skipped runs like a Step without one. */
function waits(step: StepModel): boolean {
  return step.waitFor !== null && step.waitFor.type !== "skipWaitImmediately";
}

const WAITS_FOR: Record<WhoActs, string> = {
  person: "Waits for a person to act.",
  outside: "Waits for a message.",
  clock: "Waits for a set time.",
  child: "Waits for other processes to finish.",
  waits: "Waits for something the definition does not name.",
  machine: "Runs automatically.",
};

/** The kind of Step, for one the definition does not explain; the card's subtitle as a sentence. */
function kindWords(flow: FlowModel, step: StepModel): string {
  if (waits(step)) return WAITS_FOR[whoActs(flow, step)];
  if (!step.connector) return WAITS_FOR.machine;
  const operation = midSentence(stepName(step.connector.operationId));
  return step.connector.connectorId === MODEL_CONNECTOR
    ? `Asks an AI model to ${operation}.`
    : `Calls ${connectorName(step.connector.connectorId)} to ${operation}.`;
}

/** Who sends the message a Step waits on: a person's Action, another Step, or someone outside. */
function messageWords(flow: FlowModel, step: StepModel, channels: Condition[]): string {
  const entries = answeringEntries(flow, step);
  const actions = unique(entries.flatMap((entry) => (entry.action ? [`“${entry.action.label}”`] : [])));
  if (actions.length > 0) return `A person chooses ${OR.format(actions)}`;
  const publishers = channels
    .flatMap((c) => (c.resourceId ? (resourceById(flow, c.resourceId)?.touchedBy ?? []) : []))
    .filter((ref) => ref.access === "publish" && ref.resourceId !== step.id);
  const steps = unique(
    publishers.flatMap((ref) => (stepById(flow, ref.resourceId) ? [titleOf(flow, ref.resourceId)] : [])),
  );
  if (steps.length > 0) return `A message from ${OR.format(steps)}`;
  // An RPC is called from outside; with no traced sender, only the message itself is known.
  return entries.length > 0 ? "A message from outside this process" : "A message sent to this process";
}

function childWords(flow: FlowModel, children: Condition[]): string {
  const names = unique(
    children.flatMap((c) => {
      const subflow = flow.subflows.find((s) => s.id === c.subFlowId);
      return subflow ? [processName(subflow.flowType)] : [];
    }),
  );
  return names.length > 0 ? `${AND.format(names)} to finish` : "Other processes to finish";
}

const UNITS = ["second", "minute", "hour", "day", "week"];

/** A timer's length when the definition spells it out: `24 hours timer` or `24 * time.Hour`. */
function durationOf(timer: Condition): string | null {
  const label = timer.label.replace(/\s+timer$/, "").trim();
  const spelled = /^(\d+(?:\.\d+)?)\s+([a-z]+?)s?$/i.exec(label);
  const go = /^(?:(\d+)\s*\*\s*)?time\.([A-Za-z]+)(?:\s*\*\s*(\d+))?$/.exec(label);
  const count = spelled?.[1] ?? go?.[1] ?? go?.[3] ?? (go ? "1" : null);
  const unit = (spelled?.[2] ?? go?.[2] ?? "").toLowerCase();
  if (count === null || !UNITS.includes(unit)) return null;
  return `${count} ${unit}${count === "1" ? "" : "s"}`;
}

function waitWords(flow: FlowModel, step: StepModel): string {
  const wait = step.waitFor;
  if (wait === null || !waits(step)) return "Runs straight away.";
  const of = (kind: Condition["kind"]) => wait.conditions.filter((c) => c.kind === kind);
  const parts: string[] = [];
  if (of("channel").length > 0) parts.push(messageWords(flow, step, of("channel")));
  if (of("subflow").length > 0) parts.push(childWords(flow, of("subflow")));
  const all = wait.type === "allOf";
  const joined = parts.join(all ? " and " : ", or ");
  const timers = of("timer");
  if (timers.length === 0) return parts.length > 0 ? `${joined}.` : "Something the definition does not name.";
  const span = timers.length === 1 ? durationOf(timers[0]) : null;
  if (parts.length === 0) return span ? `${span} to pass.` : "A timer to run out.";
  if (all) return `${joined}, and ${span ? `${span} to pass` : "a timer to run out"}.`;
  return span ? `${joined}, for up to ${span}.` : `${joined}, or a timer to run out.`;
}

const ENDS: Record<NonNullable<Branch["closes"]>, string> = {
  complete: "The process finishes",
  completeIfChannelsEmpty: "The process finishes if no messages are waiting",
  fail: "The process fails",
  deadEnd: "This path stops",
};

/** Every way out of a Step in the order its code declares them, then its endings and its failure handling. */
function waysOut(flow: FlowModel, step: StepModel): WayOut[] {
  const ways: WayOut[] = [];
  const add = (when: string[] | null, to: string) =>
    ways.push({ lead: when ? `If ${OR.format(when)}` : ways.length > 0 ? "Or" : "Then", to, failure: false });
  // A fork starts every path at once, so its targets are one way out.
  const forked = new Set<string>();
  for (const branch of step.execute.branches) {
    const targets = unique(branch.targets.map((t) => t.stepId));
    if (targets.length < 2) continue;
    for (const id of targets) forked.add(id);
    add(null, `${AND.format(targets.map((id) => titleOf(flow, id)))}, at the same time`);
  }
  const moves = flow.transitions.filter(
    (t) => t.fromStepId === step.id && t.kind === "transition" && !forked.has(t.toStepId),
  );
  // Running again reads as the last resort it usually is, after every way onward.
  for (const t of [...moves.filter((m) => !m.isSelfLoop), ...moves.filter((m) => m.isSelfLoop)]) {
    const when = (t.outcomes.length > 0 ? t.outcomes : caseValues(step, t.toStepId))?.map(valueWords) ?? null;
    add(when, t.isSelfLoop ? "This step again" : titleOf(flow, t.toStepId));
  }
  for (const close of unique(step.execute.branches.flatMap((b) => (b.closes ? [b.closes] : []))))
    add(null, ENDS[close]);
  for (const id of unique(failureRoutes(flow, step.id).map((t) => t.toStepId)))
    ways.push({ lead: "If it keeps failing", to: titleOf(flow, id), failure: true });
  return ways;
}

export function stepWords(flow: FlowModel, step: StepModel): StepWords {
  return {
    does: step.explanation ?? kindWords(flow, step),
    waits: waitWords(flow, step),
    ways: waysOut(flow, step),
  };
}
