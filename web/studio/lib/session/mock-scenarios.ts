// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState } from "react";
import { listMockScenarios } from "@/lib/api/agent";
import { errorMessage } from "@/lib/api/transport/api-error";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import type { AgentDescription, SequencedMessage } from "./agent-types";

/*
 * The mock model's scenarios as Studio offers them. A scenario lists its user inputs in order; the
 * mock model accepts only the next one and reports, with each reply, where its scenario stands,
 * which the Snapshot carries as AgentDescription.mockProgress. The composer offers exactly that
 * next input and sends it the way its kind arrives: a message or a request for changes with its
 * [[scenario:name#n]] marker, an answer without one.
 */

export type MockScenario = RpcOutput<"ListMockScenarios">["scenarios"][number];
export type MockScenarioInput = MockScenario["inputs"][number];
/** Where the newest mock reply left its scenario; null before one. */
export type MockProgress = AgentDescription["mockProgress"];
/** The group a scenario is listed under. */
export type MockScenarioCategory = Exclude<MockScenario["category"], "CATEGORY_UNSPECIFIED">;

/** Every category with its label, in the order the composer lists them. */
export const MOCK_CATEGORY_LABELS: Readonly<Record<MockScenarioCategory, string>> = {
  CATEGORY_PRODUCT: "E2E product",
  CATEGORY_AGENT_FEATURE: "Agent features",
  CATEGORY_PROJECT_FEATURE: "Project features",
  CATEGORY_PLATFORM_ERROR: "Platform errors",
  CATEGORY_USER_ERROR: "User errors",
  CATEGORY_AGENT_ERROR: "Agent errors",
};

/** The marks before a scenario's title: it changes the Project, it runs any time, or only its Dex test runs it. */
const MOCK_NEW_PROJECT_MARK = "✎";
const MOCK_REPEATABLE_MARK = "↻";
const MOCK_TEST_ACTION_MARK = "⊘";
export const MOCK_MARK_LEGEND = `${MOCK_NEW_PROJECT_MARK} changes the project: only in a new project · ${MOCK_REPEATABLE_MARK} runs any time · ${MOCK_TEST_ACTION_MARK} needs a test's action: only in its Dex test`;

/** The mark before a scenario's title. */
export function mockScenarioMark(scenario: MockScenario): string {
  if (scenario.testAction) return MOCK_TEST_ACTION_MARK;
  return scenario.needsNewProject ? MOCK_NEW_PROJECT_MARK : MOCK_REPEATABLE_MARK;
}

/**
 * Whether the composer offers a scenario: one that needs a test's action never; one that changes
 * the Project's source or requirements only in a new project and in its Session until its last input.
 */
export function mockScenarioOffered(scenario: MockScenario, progress: MockProgress, newProject: boolean): boolean {
  if (scenario.testAction) return false;
  return newProject || !scenario.needsNewProject || (progress?.scenario === scenario.name && progress.nextInput > 0);
}

/** The categories in the order the composer lists them. */
export const MOCK_CATEGORIES = Object.keys(MOCK_CATEGORY_LABELS) as MockScenarioCategory[];

/** What the server offers this caller: whether it may select the mock model, and the catalog. */
export interface MockScenarioCatalog {
  selectable: boolean;
  scenarios: readonly MockScenario[];
  /** Why the catalog could not be read; the mock model is then not offered. */
  error: string | null;
}

/** Reads the catalog once per page; null while the read runs. A late answer of a closed page is dropped. */
export function useMockScenarios(): MockScenarioCatalog | null {
  const [catalog, setCatalog] = useState<MockScenarioCatalog | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    listMockScenarios(controller.signal).then(
      ({ selectable, scenarios }) => {
        if (!controller.signal.aborted) setCatalog({ selectable, scenarios, error: null });
      },
      (reason: unknown) => {
        if (!controller.signal.aborted)
          setCatalog({
            selectable: false,
            scenarios: [],
            error: errorMessage(reason, "Mock scenarios are unavailable"),
          });
      },
    );
    return () => controller.abort();
  }, []);
  return catalog;
}

/** How many inputs of the scenario the mock model received, as its newest reply reported. */
export function receivedMockInputs(progress: MockProgress, scenario: MockScenario): number {
  return progress?.scenario === scenario.name ? progress.input : 0;
}

/** The number of the input a scenario takes next: the one its newest reply names, or 1 to start it (again). */
export function nextMockInput(progress: MockProgress, scenario: MockScenario): number {
  return progress?.scenario === scenario.name && progress.nextInput > 0 ? progress.nextInput : 1;
}

/** An argument value the mock model accepts: letters, digits, space and ._:/- up to 128 characters. */
export const MOCK_ARGUMENT_VALUE = /^[A-Za-z0-9 ._:/-]{1,128}$/;

/** Text without the mock model's markers ([[scenario:…]], [[scenario-arg:…]]), which no person needs to read. */
export function withoutMockMarkers(text: string): string {
  return text.replace(/\s*\[\[scenario(?:-[a-z]+)?:[^\]]*\]\]/g, "");
}

/** The text a message or a request for changes sends: its content with the scenario's argument markers. */
export function mockInputText(input: MockScenarioInput, values: Readonly<Record<string, string>>): string {
  const markers = Object.entries(values).map(([name, value]) => `[[scenario-arg:${name}=${value}]]`);
  return [input.content, ...markers].join(" ");
}

/** The messages as the conversation shows them: the mock model's markers stay stored, never on screen. */
export function withoutMockMarkersInMessages(messages: readonly SequencedMessage[]): SequencedMessage[] {
  return messages.map((entry) => {
    if (entry.message.role !== "user") return entry;
    const content = withoutMockMarkers(entry.message.content);
    return content === entry.message.content ? entry : { ...entry, message: { ...entry.message, content } };
  });
}
