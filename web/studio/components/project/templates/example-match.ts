// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ExampleCategory } from "@/lib/api/example";
import type { BuiltExample, ProcessTemplate } from "./template-catalog";

/** Words too common to tell one process from another. */
const STOP_WORDS = new Set([
  "and",
  "the",
  "for",
  "with",
  "when",
  "that",
  "this",
  "from",
  "into",
  "then",
  "them",
  "they",
  "our",
  "your",
  "their",
  "its",
  "are",
  "was",
  "has",
  "have",
  "who",
  "what",
  "which",
  "should",
  "can",
  "will",
  "all",
  "any",
  "one",
  "new",
  "get",
  "gets",
  "out",
]);

/** Endings cut from a word so that "approve", "approved" and "approval" meet, longest first. */
const ENDINGS = ["ing", "ies", "ed", "es", "al", "s", "e"] as const;

/** How much a term of each field counts: the title and the apps name the process best. */
const WEIGHTS = { title: 3, apps: 3, category: 1, decider: 1, description: 1 } as const;

/** How many examples the grid shows while the person types. */
const CLOSEST_SHOWN = 6;

/** How many starting points of each category the grid shows before the person types. */
const STARTER_PER_CATEGORY = 2;

/** The start page's categories in the order it lists them. */
export const CATEGORY_ORDER: readonly ExampleCategory[] = ["Approvals", "Finance", "Support", "People", "Content"];

/** A word's stem: lower-case, with one ending cut while four letters remain ("replies" is "reply"). */
function stem(word: string): string {
  for (const ending of ENDINGS)
    if (word.endsWith(ending) && word.length - ending.length >= 4)
      return ending === "ies" ? `${word.slice(0, -3)}y` : word.slice(0, -ending.length);
  return word;
}

/** The distinct terms of a text that can tell processes apart: stems of words of three letters or more. */
function termsOf(text: string): Set<string> {
  const terms = new Set<string>();
  for (const word of text.toLowerCase().split(/[^\p{L}\p{N}]+/u))
    if (word.length >= 3 && !STOP_WORDS.has(word)) terms.add(stem(word));
  return terms;
}

/** Whether a description names any term the examples could match. */
export function describesProcess(text: string): boolean {
  return termsOf(text).size > 0;
}

/** One example a term appears in, by its position in the index, and how much the term counts there. */
interface Posting {
  position: number;
  weight: number;
}

/**
 * The built examples with each term's postings, built once per catalog so that ranking reads only the
 * postings of the typed terms: well under a millisecond for hundreds of examples.
 */
export interface ExampleIndex {
  examples: readonly BuiltExample[];
  postings: ReadonlyMap<string, readonly Posting[]>;
}

export function indexExamples(examples: readonly BuiltExample[]): ExampleIndex {
  const postings = new Map<string, Posting[]>();
  examples.forEach((example, position) => {
    const weights = new Map<string, number>();
    const fields: [string, number][] = [
      [example.title, WEIGHTS.title],
      [example.apps.join(" "), WEIGHTS.apps],
      [example.category, WEIGHTS.category],
      [example.decider, WEIGHTS.decider],
      [example.description, WEIGHTS.description],
    ];
    for (const [field, weight] of fields)
      for (const term of termsOf(field)) weights.set(term, (weights.get(term) ?? 0) + weight);
    for (const [term, weight] of weights) {
      const list = postings.get(term) ?? [];
      list.push({ position, weight });
      postings.set(term, list);
    }
  });
  return { examples, postings };
}

/**
 * The built examples closest to a description, best first and at most `limit`: a typed term counts once per
 * field it appears in, weighted by the field, and an example needs one matched term. Ties keep the catalog's
 * order, so the ranking is the same for the same text. It runs in the browser; nothing is sent.
 */
export function closestExamples(index: ExampleIndex, text: string, limit = CLOSEST_SHOWN): BuiltExample[] {
  const scores = new Map<number, number>();
  for (const term of termsOf(text))
    for (const { position, weight } of index.postings.get(term) ?? [])
      scores.set(position, (scores.get(position) ?? 0) + weight);
  return [...scores]
    .sort(([left, leftScore], [right, rightScore]) => rightScore - leftScore || left - right)
    .slice(0, limit)
    .map(([position]) => index.examples[position]);
}

/** A few starting points of each category, in the category order: the built examples, then the placeholders. */
export function starterTemplates(
  examples: readonly BuiltExample[],
  placeholders: readonly ProcessTemplate[],
): ProcessTemplate[] {
  const few = (templates: readonly ProcessTemplate[]) =>
    CATEGORY_ORDER.flatMap((category) =>
      templates.filter((template) => template.category === category).slice(0, STARTER_PER_CATEGORY),
    );
  return [...few(examples), ...few(placeholders)];
}

/** What the person narrows the whole catalog by; null filters let every example through. */
export interface ExampleFilter {
  query: string;
  category: ExampleCategory | null;
  app: string | null;
}

/** Every example the filter lets through: the closest first for a query, otherwise by category and title. */
export function browsedExamples(index: ExampleIndex, { query, category, app }: ExampleFilter): BuiltExample[] {
  const ordered = describesProcess(query)
    ? closestExamples(index, query, index.examples.length)
    : [...index.examples].sort(
        (left, right) =>
          CATEGORY_ORDER.indexOf(left.category) - CATEGORY_ORDER.indexOf(right.category) ||
          left.title.localeCompare(right.title),
      );
  return ordered.filter(
    (example) => (category === null || example.category === category) && (app === null || example.apps.includes(app)),
  );
}

/** The apps the examples work with, each once, in alphabetical order. */
export function appsOf(examples: readonly BuiltExample[]): string[] {
  return [...new Set(examples.flatMap((example) => example.apps))].sort((left, right) => left.localeCompare(right));
}
