// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock model's scenario catalog that ListMockScenarios answers in Studio mock mode; development only.
import type { AgentMockScenario } from "@/lib/generated/protobuf/superverse/v2/AgentMockScenario";

/** Where a fixture scenario stands, as the server's mock model reports it with each reply. */
type MockProgress = { scenario: string; input: number; nextInput: number };

const SCENARIO_MARKER = /\[\[scenario:([a-z0-9-]+)#(\d+)\]\]/;

/**
 * The progress after a message, whose marker names its input, or after an answer (`text` null),
 * which is the next input; an input out of order leaves it as it was.
 */
export function advanceMockProgress(
  progress: MockProgress | null | undefined,
  text: string | null,
): MockProgress | null {
  const marker = text === null ? null : SCENARIO_MARKER.exec(text);
  const name = marker ? marker[1] : progress?.scenario;
  const number = marker ? Number(marker[2]) : (progress?.nextInput ?? 0);
  const inputs = MOCK_SCENARIOS.find((scenario) => scenario.name === name)?.inputs?.length ?? 0;
  const inOrder = number === 1 || (progress != null && progress.scenario === name && number === progress.input + 1);
  if (name === undefined || number < 1 || number > inputs || !inOrder) return progress ?? null;
  return { scenario: name, input: number, nextInput: number < inputs ? number + 1 : 0 };
}

/** The scenario and argument markers of a mock input, which the mock Agent's script phrases ignore. */
export const SCENARIO_MARKERS = /\s*\[\[scenario(?:-arg)?:[^\]]*\]\]/g;

/**
 * Mock mode is a local environment, so every account may select the mock model. The inputs'
 * messages are the mock Agent's script phrases, so the mock composer drives its scripts the way it
 * drives the server's scenarios: the requirements card with a request for changes and a
 * confirmation, and an ordinary turn with an argument.
 */
export const MOCK_SCENARIOS: AgentMockScenario[] = [
  {
    name: "requirements-draft-changes",
    title: "Requirements draft changed",
    category: "CATEGORY_PROJECT_FEATURE",
    needsNewProject: false,
    description: "A requirements card, a request for changes to the draft that revises it, then the confirmation.",
    arguments: [],
    inputs: [
      {
        number: 1,
        label: "Describe the application",
        kind: "KIND_MESSAGE",
        content: "confirm requirements [[scenario:requirements-draft-changes#1]]",
        answers: [],
      },
      {
        number: 2,
        label: "Ask for changes to the draft",
        kind: "KIND_REQUIREMENTS_CHANGE",
        content: "Add a weekly digest. [[scenario:requirements-draft-changes#2]]",
        answers: [],
      },
      {
        number: 3,
        label: "Confirm the revised requirements",
        kind: "KIND_REQUIREMENTS_CONFIRM",
        content: "",
        answers: [],
      },
    ],
  },
  {
    name: "build-basic",
    title: "Change the page heading",
    category: "CATEGORY_PROJECT_FEATURE",
    needsNewProject: true,
    description: "An ordinary turn that changes the heading to the heading argument and commits it.",
    arguments: [{ name: "heading", defaultValue: "Mock build" }],
    inputs: [
      {
        number: 1,
        label: "Change the heading",
        kind: "KIND_MESSAGE",
        content: "Change the main heading of the page. [[scenario:build-basic#1]]",
        answers: [],
      },
    ],
  },
];
