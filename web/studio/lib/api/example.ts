// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Example__Output } from "@/lib/generated/protobuf/superverse/v2/Example";
import { readUnary } from "./transport/read-unary";

// Browser client of ExampleService: the tested examples a Project can start from.

/** The start page's groups, as the examples and the placeholders name them. */
export type ExampleCategory = "Approvals" | "Finance" | "Support" | "People" | "Content";

const CATEGORIES: Record<Example__Output["category"], ExampleCategory | null> = {
  EXAMPLE_CATEGORY_UNSPECIFIED: null,
  EXAMPLE_CATEGORY_APPROVALS: "Approvals",
  EXAMPLE_CATEGORY_FINANCE: "Finance",
  EXAMPLE_CATEGORY_SUPPORT: "Support",
  EXAMPLE_CATEGORY_PEOPLE: "People",
  EXAMPLE_CATEGORY_CONTENT: "Content",
};

/** One example of the server's application template, as the start page shows and suggests it. */
export interface ExampleView {
  exampleId: string;
  title: string;
  category: ExampleCategory;
  apps: readonly string[];
  decider: string;
  description: string;
}

/** The examples in catalog order, the default first; one without a known category is left out. */
export async function listExamples(signal: AbortSignal): Promise<ExampleView[]> {
  const { examples } = await readUnary("ListExamples", {}, { signal });
  return examples.flatMap((example) => {
    const category = CATEGORIES[example.category];
    if (!category) return [];
    const { exampleId, title, apps, decider, description } = example;
    return [{ exampleId, title, category, apps, decider, description }];
  });
}
