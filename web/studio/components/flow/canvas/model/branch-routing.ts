// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * The shape of a Step's outgoing branches, when it is worth naming on the card.
 *
 * Dex has two branching shapes: `goToMany` starts several Steps together (a fork, UML's heavy bar),
 * while several guarded `goTo` decisions take the first match (a choice). A choice whose guards all
 * compare one name against single tokens is a switch, and the card names that discriminant; the
 * case values stay in the panel because they usually name their destination anyway. Both shapes are
 * detected structurally, never by branch count.
 */

import { simplifyGuard } from "./guard-text";
import type { Branch, StepModel } from "./flow-model";

interface CaseGuard {
  /** The name every conforming branch tests. */
  key: string;
  /** The single token it is compared against, kept to prove every merged guard tests the same name. */
  value: string;
}

/** A dotted identifier, so a call such as `len(executions)` is never hoisted onto a card. */
const KEY = /^[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*$/;

/** One token: an identifier, a number, or a quoted name (quotes kept, since it is a name). */
const VALUE = /^(?:[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*|-?\d+(?:\.\d+)?|'[^']*'|"[^"]*")$/;

/** `target == INIT` -> `{ key: 'target', value: 'INIT' }`. Null for anything else. */
function caseGuard(guard: string | null | undefined): CaseGuard | null {
  if (guard === null || guard === undefined) return null;
  const parts = guard.split(" == ");
  if (parts.length !== 2) return null;
  const key = (parts[0] ?? "").trim();
  const value = (parts[1] ?? "").trim();
  if (!KEY.test(key) || !VALUE.test(value)) return null;
  return { key, value };
}

/**
 * The case this branch is, or null. Reads `fullGuards` because a grouped branch's `guard` is
 * already the lossy "N conditions"; a grouped branch conforms only when every merged guard is a
 * case on the same name.
 */
function branchCase(branch: Branch): { key: string; values: string[] } | null {
  if (branch.fullGuards.length === 0) return null;
  const cases = branch.fullGuards.map((g) => caseGuard(simplifyGuard(g)));
  const first = cases[0];
  if (first === null || first === undefined) return null;
  if (cases.some((c) => c === null || c.key !== first.key)) return null;
  return { key: first.key, values: cases.map((c) => (c as CaseGuard).value) };
}

/**
 * The one name this Step's moving branches route on, or null. At least two branches must agree
 * (one `X == V` beside one `X != V` is a yes/no test, not a switch), and they must be a strict
 * majority, which is why the card says the path depends on the name rather than that it decides
 * every path.
 */
function discriminantOf(step: StepModel): string | null {
  const moving = step.execute.branches.filter((b) => b.targets.length > 0);
  if (moving.length < 2) return null;
  const tally = new Map<string, number>();
  for (const b of moving) {
    const c = branchCase(b);
    if (c === null) continue;
    tally.set(c.key, (tally.get(c.key) ?? 0) + 1);
  }
  let best: string | null = null;
  let bestCount = 0;
  for (const [key, count] of tally) {
    if (count > bestCount) {
      best = key;
      bestCount = count;
    }
  }
  if (best === null || bestCount < 2) return null;
  return bestCount * 2 > moving.length ? best : null;
}

/**
 * How many distinct Steps one decision starts at once, or 0 when nothing fans out. Read from the
 * targets rather than the decision verb.
 */
export function fanOutOf(step: StepModel): number {
  let most = 0;
  for (const b of step.execute.branches) {
    most = Math.max(most, new Set(b.targets.map((t) => t.stepId)).size);
  }
  return most > 1 ? most : 0;
}

/**
 * The case values that send this Step to `targetStepId`, as a line's label: `"reply"` for
 * `update.Kind == "reply"`. Null unless the Step routes on one name (`discriminantOf`) and every
 * branch to that target is a case on it, so a label never paraphrases a general condition.
 */
export function caseLabel(step: StepModel, targetStepId: string): string | null {
  const key = discriminantOf(step);
  if (key === null) return null;
  const values: string[] = [];
  for (const branch of step.execute.branches) {
    if (!branch.targets.some((t) => t.stepId === targetStepId)) continue;
    const c = branchCase(branch);
    if (c === null || c.key !== key) return null;
    values.push(...c.values.map((v) => v.replace(/^["']|["']$/g, "")));
  }
  return values.length > 0 ? [...new Set(values)].join(" / ") : null;
}
