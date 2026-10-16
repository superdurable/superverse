// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/** The longest name a description gives. */
const MAX_LENGTH = 40;

/** How a request often opens, which names nothing of the process: "Please build me a", "We need to". */
const OPENING =
  /^(?:please\s+)?(?:(?:i|we)\s+(?:want|need|would like)\s+(?:you\s+)?(?:to\s+)?)?(?:(?:build|create|make|set up|automate)\s+(?:me\s+|us\s+)?)?(?:an?\s+|the\s+)?/i;

/** Small words a shortened name does not end on. */
const DANGLING = new Set(["a", "an", "the", "and", "or", "of", "to", "for", "in", "on", "at", "by", "from", "with"]);

/**
 * A short Project name from a description: its first clause without a request's opening words, cut at a
 * word boundary to at most 40 characters, in sentence case. Empty when the description has no words.
 */
export function projectNameFrom(description: string): string {
  const clause = description.trim().split(/[.,;:!?()\n–—]|\s-\s/)[0] ?? "";
  const words = clause.replace(OPENING, "").trim().split(/\s+/).filter(Boolean);
  const kept: string[] = [];
  for (const word of words) {
    if ([...kept, word].join(" ").length > MAX_LENGTH) break;
    kept.push(word);
  }
  if (kept.length === 0 && words.length > 0) kept.push(words[0].slice(0, MAX_LENGTH));
  while (kept.length > 1 && DANGLING.has(kept[kept.length - 1].toLowerCase())) kept.pop();
  const name = kept.join(" ");
  // A description typed in capitals reads as a sentence; otherwise the words keep their case, such as Slack.
  const cased = /[a-z]/.test(name) ? name : name.toLowerCase();
  return cased.charAt(0).toUpperCase() + cased.slice(1);
}
