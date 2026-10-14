// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Guard text as a reader sees it. A guard arrives with every earlier branch negated in front of it;
 * only its last clause is the branch's own.
 */

const FLIPS: [string, string][] = [
  ["!=", "=="],
  ["==", "!="],
  [">=", "<"],
  ["<=", ">"],
  [">", "<="],
  ["<", ">="],
];

function stripOuterParens(s: string): string {
  let out = s.trim();
  while (out.startsWith("(") && out.endsWith(")")) {
    // Only strip when the leading paren actually closes at the end, so
    // `(a) and (b)` is left alone.
    let depth = 0;
    let matches = true;
    for (let i = 0; i < out.length; i += 1) {
      if (out[i] === "(") depth += 1;
      else if (out[i] === ")") {
        depth -= 1;
        if (depth === 0 && i !== out.length - 1) {
          matches = false;
          break;
        }
      }
    }
    if (!matches) break;
    out = out.slice(1, -1).trim();
  }
  return out;
}

/**
 * A guard arrives as every preceding branch negated and ANDed with this one, so an
 * `else` clause reads as `not (not records)`. Only the last clause is this branch's
 * own, and a doubled negation is worth undoing before showing it to anyone.
 */
export function simplifyGuard(raw: string | undefined): string | null {
  if (raw === undefined) return null;
  const trimmed = raw.trim();
  if (trimmed === "") return null;
  if (trimmed === "otherwise") return "otherwise";

  // Split on top-level ` and ` only; a nested `and` inside parens is part of one clause.
  const parts: string[] = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < trimmed.length; i += 1) {
    const ch = trimmed[i];
    if (ch === "(") depth += 1;
    else if (ch === ")") depth -= 1;
    else if (depth === 0 && trimmed.startsWith(" and ", i)) {
      parts.push(trimmed.slice(start, i));
      start = i + 5;
      i += 4;
    }
  }
  parts.push(trimmed.slice(start));

  let clause = stripOuterParens(parts[parts.length - 1] ?? trimmed);

  // `not not x` -> `x`, repeatedly.
  let changed = true;
  while (changed) {
    changed = false;
    const inner = /^not\s+(.*)$/.exec(clause);
    if (inner) {
      const body = stripOuterParens(inner[1]);
      const nested = /^not\s+(.*)$/.exec(body);
      if (nested) {
        clause = stripOuterParens(nested[1]);
        changed = true;
        continue;
      }
      // `not (a != b)` -> `a == b`, for one top-level comparison only.
      for (const [from, to] of FLIPS) {
        const at = topLevelIndexOf(body, ` ${from} `);
        if (at !== -1) {
          clause = `${body.slice(0, at)} ${to} ${body.slice(at + from.length + 2)}`;
          changed = true;
          break;
        }
      }
    }
  }
  return clause === "" ? null : clause;
}

function topLevelIndexOf(s: string, needle: string): number {
  let depth = 0;
  for (let i = 0; i < s.length; i += 1) {
    if (s[i] === "(") depth += 1;
    else if (s[i] === ")") depth -= 1;
    else if (depth === 0 && s.startsWith(needle, i)) return i;
  }
  return -1;
}
