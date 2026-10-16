// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import {
  failureHandling,
  mainStory,
  reachableFromStart,
  stepById,
  stepByType,
  type FlowModel,
} from "@/components/flow/canvas/model/flow-model";
import { stepTitle } from "@/components/flow/canvas/layout/step-card";
import { processName, stepName } from "@/lib/shared/format";
import { decodeDesignedFlow } from "@/components/flow/design/design-compare";
import type { DesignedFlow, DesignedStep, FlowDesign } from "@/components/flow/design/design-graph";

/**
 * A Flow design in business words for the Process Brief: Steps in the canvas's reading order with the
 * canvas's titles, who acts, each condition of an ordinary way out with where it leads, and the number of
 * failure-handling Steps, which the canvas's own split of the main story decides.
 */

/** A requirements card as the Brief reads it, which the Session's card satisfies. */
export interface BriefCard {
  title: string;
  summary: string;
  features: readonly { id: string; text: string }[];
  connections: readonly {
    need: string;
    connector: string;
    operations: readonly string[];
    /** "new" when the person must create it. */
    status: string;
    userSetup: string;
  }[];
  userSetup: readonly string[];
  acceptance: readonly string[];
}

/** One line of the Brief: who or what it is about, and what it says. */
export interface BriefLine {
  key: string;
  lead: string;
  text: string;
}

/** One rule: the condition of an ordinary way out, and the Step or end it leads to. */
export interface BriefRule {
  key: string;
  when: string;
  to: string;
}

/** What a Step waits for, in the words of a badge. */
export type StepWait = "person" | "message" | "time";

interface BriefStep {
  key: string;
  title: string;
  explanation: string;
  waits: StepWait | null;
}

export interface BriefProcess {
  flowType: string;
  name: string;
  purpose: string;
  /** The main story in reading order. */
  steps: BriefStep[];
}

export interface DesignBrief {
  processes: BriefProcess[];
  startsWhen: BriefLine[];
  people: BriefLine[];
  rules: BriefRule[];
  failureSteps: number;
}

interface ReadFlow {
  flow: DesignedFlow;
  name: string;
  /** Main-story Steps in reading order, with their titles. */
  story: { step: DesignedStep; title: string }[];
  /** The canvas's title of any Step of the Flow. */
  titleOf: (stepType: string) => string;
  failureSteps: number;
}

const WAITS: Partial<Record<string, StepWait>> = {
  KIND_ACTION: "person",
  KIND_CHANNEL: "message",
  KIND_TIMER: "time",
};

/** The main story in the canvas's order: breadth-first from the start, then the Steps it does not reach. */
function storyOrder(model: FlowModel): string[] {
  const story = mainStory(model);
  const reached = reachableFromStart(story);
  return [...reached, ...story.steps.map((step) => step.id).filter((id) => !reached.includes(id))];
}

function readFlow(flow: DesignedFlow): ReadFlow {
  const name = processName(flow.flowType);
  const model = decodeDesignedFlow(flow)?.model;
  if (!model)
    return {
      flow,
      name,
      story: flow.steps.map((step) => ({ step, title: stepName(step.stepType) })),
      titleOf: stepName,
      failureSteps: 0,
    };
  const story = storyOrder(model).flatMap((id) => {
    const modelled = stepById(model, id);
    const step = flow.steps.find((item) => item.stepType === modelled?.stepType);
    return modelled && step ? [{ step, title: stepTitle(modelled) }] : [];
  });
  const titleOf = (stepType: string) => {
    const modelled = stepByType(model, stepType);
    return modelled ? stepTitle(modelled) : stepName(stepType);
  };
  return { flow, name, story, titleOf, failureSteps: failureHandling(model).size };
}

const quoted = (label: string) => `“${label}”`;

/** What starts a Flow: the wait of its start Step, or the start Step itself with its explanation. */
function startOf(read: ReadFlow, startedBy: readonly string[]): Omit<BriefLine, "key"> {
  if (startedBy.length > 0) return { lead: read.name, text: `Started by ${startedBy.join(", ")}.` };
  const start = read.story.find(({ step }) => step.stepType === read.flow.startStep) ?? read.story[0];
  if (!start) return { lead: read.name, text: read.flow.purpose };
  const { kind, name } = start.step.waitsFor ?? { kind: "KIND_NONE", name: "" };
  const action = read.flow.actions.find((item) => item.rpc === name);
  if (kind === "KIND_ACTION" && action)
    return { lead: start.title, text: `When a person chooses ${quoted(action.label)}.` };
  if (kind === "KIND_CHANNEL") return { lead: start.title, text: `When a message arrives (${stepName(name)}).` };
  if (kind === "KIND_TIMER") return { lead: start.title, text: stepName(name) };
  return { lead: start.title, text: start.step.explanation };
}

/** Who acts: a person at each Action, at the Steps that wait for it. */
function peopleOf(read: ReadFlow): Omit<BriefLine, "key">[] {
  return read.flow.actions.map((action) => {
    const waiting = read.story.filter(
      ({ step }) => step.waitsFor?.kind === "KIND_ACTION" && step.waitsFor.name === action.rpc,
    );
    return {
      lead: action.label,
      text:
        waiting.length > 0
          ? `A person chooses it at ${waiting.map(({ title }) => title).join(", ")}.`
          : "A person can choose it.",
    };
  });
}

/** Where a way out leads: the canvas's title of its Step, or the end it reaches. */
function target(read: ReadFlow, next: DesignedStep["next"][number]): string {
  if (next.end === "END_COMPLETE") return "Done";
  if (next.end === "END_FAIL") return "Stops as failed";
  return read.titleOf(next.toStep);
}

/** The conditions of the ordinary ways out, in reading order; failure routes are only counted. */
function rulesOf(read: ReadFlow): Omit<BriefRule, "key">[] {
  return read.story.flatMap(({ step }) =>
    step.next
      .filter((next) => !next.isFailureRoute && next.when.trim() !== "")
      .map((next) => ({ when: next.when.trim(), to: target(read, next) })),
  );
}

/** The Brief of a Flow design; with more than one process, each line names its process. */
export function designBrief(design: FlowDesign): DesignBrief {
  const reads = design.flows.map(readFlow);
  const several = reads.length > 1;
  const named = (read: ReadFlow, lines: Omit<BriefLine, "key">[], part: string): BriefLine[] =>
    lines.map((line, index) => ({
      ...line,
      key: `${read.flow.flowType}:${part}:${String(index)}`,
      lead: several && line.lead !== read.name ? `${read.name} · ${line.lead}` : line.lead,
    }));
  const startersOf = (flowType: string) =>
    reads.filter((read) => read.flow.starts.includes(flowType)).map((read) => read.name);
  return {
    processes: reads.map((read) => ({
      flowType: read.flow.flowType,
      name: read.name,
      purpose: read.flow.purpose,
      steps: read.story.map(({ step, title }) => ({
        key: step.stepType,
        title,
        explanation: step.explanation,
        waits: WAITS[step.waitsFor?.kind ?? ""] ?? null,
      })),
    })),
    startsWhen: reads.flatMap((read) => named(read, [startOf(read, startersOf(read.flow.flowType))], "start")),
    people: reads.flatMap((read) => named(read, peopleOf(read), "people")),
    rules: reads.flatMap((read) =>
      rulesOf(read).map((rule, index) => ({
        ...rule,
        key: `${read.flow.flowType}:rule:${String(index)}`,
        when: several ? `${read.name} · ${rule.when}` : rule.when,
      })),
    ),
    failureSteps: reads.reduce((sum, read) => sum + read.failureSteps, 0),
  };
}
