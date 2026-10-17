// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import {
  closestCatalogPositions,
  EXAMPLE_CATEGORIES,
  namesSearchTerm,
  type ExampleCategory,
} from "@/lib/examples/example-catalog";
import { CLOSEST_SHOWN } from "@/lib/examples/example-search";
import { BUILT_EXAMPLES, type BuiltExample, type ProcessTemplate } from "./template-catalog";

/** How many starting points of each category the grid shows before the person types. */
const STARTER_PER_CATEGORY = 2;

/** The start page's categories in the order it lists them. */
export const CATEGORY_ORDER = EXAMPLE_CATEGORIES;

/** Whether a description names any search term, so that the grid ranks the examples by it. */
export function describesProcess(text: string): boolean {
  return namesSearchTerm(text);
}

/**
 * The built examples closest to a description, best first and at most `limit`, by the search the server
 * shortlists with (lib/examples): rare words count more, ties keep the catalog's order, so the ranking is the
 * same for the same text. It runs in the browser; nothing is sent.
 */
export function closestExamples(text: string, limit = CLOSEST_SHOWN): BuiltExample[] {
  return closestCatalogPositions(text, limit).map((position) => BUILT_EXAMPLES[position]);
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
export function browsedExamples({ query, category, app }: ExampleFilter): BuiltExample[] {
  const ordered = describesProcess(query)
    ? closestExamples(query, BUILT_EXAMPLES.length)
    : [...BUILT_EXAMPLES].sort(
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
