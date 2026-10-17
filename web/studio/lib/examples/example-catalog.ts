// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { EXAMPLE_CATEGORIES, type ExampleCategory } from "@/lib/generated/example-catalog/example-categories";
import exampleSearch from "@/lib/generated/example-catalog/example-search.json";
import { closestPositions, parseExampleSearchIndex, searchTerms } from "./example-search";

// The start page's groups are the server's closed set, which make generate-app-template writes.
export { EXAMPLE_CATEGORIES, type ExampleCategory };

/** One example of the application template this Studio was built with, as the start page shows it. */
export interface ExampleView {
  exampleId: string;
  title: string;
  category: ExampleCategory;
  apps: readonly string[];
  decider: string;
  description: string;
}

/** The generated index of the examples, the bytes the server embeds; Studio needs no request for it. */
const INDEX = parseExampleSearchIndex(exampleSearch);

function categoryOf(name: string): ExampleCategory {
  const category = EXAMPLE_CATEGORIES.find((value) => value === name);
  if (!category) throw new Error(`The generated example catalog names the unknown category ${name}`);
  return category;
}

/** The examples in catalog order, the default first. */
export const EXAMPLE_CATALOG: readonly ExampleView[] = INDEX.examples.map(
  ({ id, title, category, apps, decider, description }) => ({
    exampleId: id,
    title,
    category: categoryOf(category),
    apps,
    decider,
    description,
  }),
);

/**
 * The positions in EXAMPLE_CATALOG of the examples closest to a text, best first and at most `limit`, as the
 * server ranks them; empty for a text that names no search term. It runs in the browser; nothing is sent.
 */
export function closestCatalogPositions(text: string, limit: number): number[] {
  return closestPositions(INDEX, text, limit);
}

/** Whether a text names any search term, so that it ranks the examples even when none is close. */
export function namesSearchTerm(text: string): boolean {
  return searchTerms(INDEX, text).length > 0;
}
