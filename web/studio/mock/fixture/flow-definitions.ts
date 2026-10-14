// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * Three definitions copied from `dex-web: docs/src/data/flow-definitions/`, which
 * are real analyzer output rather than fixtures written for this work. Each is the
 * only one of its classification; the omitted definitions add no coverage.
 *
 * Loaded through dynamic import only when a process surface requests them, keeping
 * sample flows out of the shared startup bundle.
 *
 * The mock Project is a template Project whose Agent added the summary approval:
 * its Flow Definitions are the ones its recorded runs started under (flow-runs.ts),
 * so the project catalog stays honest instead of relabeling an unrelated sample graph.
 */

import { arrayField } from "@/lib/shared/json";
import { loadProjectFlowDefinitions } from "./flow-runs";

/** The revision of the mock Project's Flow Definitions, which every Release, Preview source and catalog carries. */
export const MOCK_DEFINITION_REVISION = "mock-revision-1";

export const FIXTURE_NAMES = {
  /* schema 2.0, 125 steps, 4 actions, all five uiSlots, two permissions */
  READY: "customer-refund-agentic",
  /* schema 2.0, zero actions: an inbox that will never fill */
  NO_ACTIONS: "customer-refund",
  /* schema 1.0: the version the renderer cannot read */
  OLD_SCHEMA: "user-onboarding-process",
  /* the mock Project's own TextSummaryFlow with its approval, drawn when no fixture is named */
  PROJECT: "app-template",
} as const;

const LOADERS: Record<string, () => Promise<unknown>> = {
  [FIXTURE_NAMES.READY]: async () =>
    (await import("./customer-refund-agentic.json", { with: { type: "json" } })).default,
  [FIXTURE_NAMES.NO_ACTIONS]: async () => (await import("./customer-refund.json", { with: { type: "json" } })).default,
  [FIXTURE_NAMES.OLD_SCHEMA]: async () =>
    (await import("./user-onboarding-process.json", { with: { type: "json" } })).default,
  [FIXTURE_NAMES.PROJECT]: async () => (await loadProjectFlowDefinitions())[0].graph,
};

/* Reserved fixture name that forces the error register, so the one state no happy
   path reaches is still reviewable. */
const FORCE_ERROR = "force-error";

export function isKnownFixture(name: string): boolean {
  return name === FORCE_ERROR || Object.hasOwn(LOADERS, name);
}

/** A fixture's definition, unknown until classified: fixtures cover old and invalid schemas on purpose. */
export async function loadFixtureDefinition(name: string): Promise<{ definition: unknown; failed: boolean }> {
  if (name === FORCE_ERROR) return { definition: null, failed: true };
  const load = LOADERS[name];
  if (!load) return { definition: null, failed: true };
  return { definition: await load(), failed: false };
}

/*
 * What a mock surface can do with a fixture flow definition, decided before
 * anything renders. The distinction it draws -- unavailable versus empty -- is
 * the one people most often get wrong.
 */
export const DEFINITION_STATES = {
  MISSING: "missing",
  UNSUPPORTED_VERSION: "unsupported-version",
  INVALID: "invalid",
  READY: "ready",
} as const;

export interface DefinitionClassification {
  state: (typeof DEFINITION_STATES)[keyof typeof DEFINITION_STATES];
  reason: string | null;
}

/** The fields of a fixture graph the mock surfaces read; any of them may be absent or mistyped. */
interface FixtureGraph {
  schemaVersion?: unknown;
  valid?: unknown;
  diagnostics?: unknown;
  flow?: { name?: unknown };
  v2?: { actions?: unknown };
}

export function classifyDefinition(definition: unknown): DefinitionClassification {
  if (!definition || typeof definition !== "object") {
    return { state: DEFINITION_STATES.MISSING, reason: null };
  }
  const graph = definition as FixtureGraph;
  const schemaVersion = typeof graph.schemaVersion === "string" ? graph.schemaVersion : null;
  if (schemaVersion !== "2.0") {
    return {
      state: DEFINITION_STATES.UNSUPPORTED_VERSION,
      reason: `needs schema 2.0, found ${schemaVersion ?? "none"}`,
    };
  }
  // `valid: false` means the analyzer ran and disagreed with the source, which is
  // a different problem from an old schema and needs the diagnostics to say why.
  if (graph.valid === false) {
    const first = Array.isArray(graph.diagnostics) ? (graph.diagnostics[0] as { message?: string } | null) : null;
    return {
      state: DEFINITION_STATES.INVALID,
      reason: first?.message ?? "the analyzer reported diagnostics",
    };
  }
  return { state: DEFINITION_STATES.READY, reason: null };
}

/*
 * A flow that declares no actions will never put anything in an inbox. That is
 * empty, not unavailable: nothing is broken and nothing is coming.
 */
export function inboxActions(definition: unknown): unknown[] {
  return arrayField((definition as FixtureGraph | null)?.v2?.actions);
}
