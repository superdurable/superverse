// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * A Dex Flow as the canvas draws it, decoded once from the Flow Definition Graph by
 * `decode-flow-definition.ts`, and the graph queries over it. Layout code reads this model and never
 * the wire format.
 */

import type { FlowDefinitionConditionKind, SourceSpan } from "@/lib/api/flow/flow-definition";

/**
 * Who has to act before a Step can move, from its wait-condition kinds: a Channel needs an actor
 * outside the Flow, a SubFlow a child, a Timer the clock, and no wait only the machine.
 * `unknown` marks a declared wait whose conditions the analyser could not resolve, so a hidden
 * human gate is never drawn as machine work.
 */
export type Actor = "external" | "child" | "clock" | "machine" | "unknown";

export interface Condition {
  kind: FlowDefinitionConditionKind;
  /** Verbatim from the wire minus the `.for N` suffix. Identifiers are never paraphrased. */
  label: string;
  resourceId?: string;
  subFlowId?: string;
  expression?: string;
}

export interface WaitPhase {
  /** 'until' | 'anyOf' | 'allOf' | 'anyComboOf' | 'skipWaitImmediately' | 'failure' | … */
  type: string;
  conditions: Condition[];
  /** One prose line: framework vocabulary paraphrased, identifiers verbatim. */
  sentence: string;
}

export interface Branch {
  /** Near-identical decisions share one branch. */
  decisionTypes: string[];
  /** The last `and` clause, double negation undone. Null when unconditional. */
  guard: string | null;
  /** Every guard that merged into this branch, unsimplified. */
  fullGuards: string[];
  targets: { stepId: string; multiplicity?: string }[];
  closes?: "complete" | "fail" | "deadEnd" | "completeIfChannelsEmpty";
  cancels?: { stepId: string; scope: "all" | "siblings" }[];
  checkedChannels?: string[];
}

interface ExecutePhase {
  branches: Branch[];
}

export interface ResourceRef {
  resourceId: string;
  access: "read" | "write" | "publish" | "lock";
  label: string;
  /** Which phase touches it, from `edge.metadata.phase`. */
  phase?: string;
}

/**
 * Dex has no recovery Step type: a Step becomes a failure target only when other Steps route their
 * exhausted Execute retries to it. `fallback` catches one Step, `sink` catches several and stops,
 * `dispatcher` catches several and sends the Flow onward.
 */
export type RecoveryRole = "none" | "fallback" | "sink" | "dispatcher";

export interface StepModel {
  id: string;
  /** The durable identifier. A display label may sit in front of it but never replaces it. */
  stepType: string;
  label: string;
  isStart: boolean;
  actor: Actor;
  /** Many ordinary transitions converge here, so its inbound edges stay latent. */
  isHub: boolean;
  /** Two or more failure edges converge here. A compensation target, not a legibility problem. */
  isRecoveryHub: boolean;
  recoveryRole: RecoveryRole;
  /** Every Step that can fail routes here, so the failure edges express one policy. */
  hasUniformFailurePolicy: boolean;
  /** Ordinary inbound transitions only. Failure edges are counted separately. */
  inboundCount: number;
  inboundFailureCount: number;
  /** Null when the Step has no WaitFor at all (`phase === 'execute'`). */
  waitFor: WaitPhase | null;
  execute: ExecutePhase;
  resources: ResourceRef[];
  span?: SourceSpan;
  /** True when dexcli identified this Step as a Connector SDK factory Step. */
  isConnectorStep: boolean;
  /** Static Connector identity emitted by Flow Definition Graph 2.0. */
  connector?: {
    connectorId: string;
    operationId: string;
    operationKind: string;
    connectionName: string;
    moduleVersion: string;
  };
  /** One-sentence Step purpose from `dex:explanation`, when the FDG carries it. */
  explanation?: string;
}

export interface EntryModel {
  id: string;
  kind: "rpc" | "timeoutHandler";
  name: string;
  /** RPC handlers emit `rpcResult` decisions, which may also move the Flow. */
  branches: Branch[];
  resources: ResourceRef[];
  /**
   * Channels this entry publishes to, joined onward to the Steps that wait on them. The wire
   * never encodes this as one edge; it is the join of `rpc -publish-> channel` and
   * `channel -condition-> wait`, and it is the human-input path.
   */
  opensGates: { channelId: string; stepIds: string[] }[];
  /** The Flow v2 Action this RPC implements, when the definition declares one. */
  action?: { label: string; requiredPermission: string };
  /** The Flow v2 views this RPC serves. Empty for every other entry. */
  views: EntryView[];
  span?: SourceSpan;
}

export type EntryView = "summary" | "display";

export interface ResourceModel {
  id: string;
  kind: "attribute" | "channel" | "stream";
  name: string;
  valueType: string;
  isMap: boolean;
  /** Steps and entries that touch it, by access kind. */
  touchedBy: ResourceRef[];
}

export interface SubFlowRef {
  id: string;
  flowType: string;
  external: boolean;
  startedByStepId?: string;
}

export interface Transition {
  id: string;
  fromStepId: string;
  toStepId: string;
  kind: "transition" | "failure_transition";
  guard: string | null;
  /** Parallel transitions merge; this holds every guard that collapsed into one line. */
  mergedGuards: string[];
  multiplicity?: string;
  isSelfLoop: boolean;
}

export interface FlowModel {
  flowType: string;
  startStepId?: string;
  steps: StepModel[];
  entries: EntryModel[];
  resources: ResourceModel[];
  subflows: SubFlowRef[];
  transitions: Transition[];
  /** Wire elements the decoder did not model, so the canvas can say so rather than look complete. */
  dropped: string[];
}

export function resourceById(flow: FlowModel, id: string): ResourceModel | undefined {
  return flow.resources.find((r) => r.id === id);
}

export function stepById(flow: FlowModel, id: string): StepModel | undefined {
  return flow.steps.find((s) => s.id === id);
}

export function stepByType(flow: FlowModel, stepType: string): StepModel | undefined {
  return flow.steps.find((s) => s.stepType === stepType);
}

/** A Step's display label, or its id without the `step:` prefix when the id names no Step. */
export function stepLabel(flow: FlowModel, id: string): string {
  return stepById(flow, id)?.label ?? id.replace(/^step:/, "");
}

/** The Steps a transition of either kind joins to this Step, in or out, other than the Step itself. */
export function joinedSteps(flow: FlowModel, stepId: string): string[] {
  const joined = new Set<string>();
  for (const t of flow.transitions) {
    if (t.fromStepId === stepId) joined.add(t.toStepId);
    if (t.toStepId === stepId) joined.add(t.fromStepId);
  }
  joined.delete(stepId);
  return [...joined];
}

/** The Steps one `kind` transition leads to from this Step. A self-loop is not a successor. */
export function successors(flow: FlowModel, stepId: string, kind: Transition["kind"]): string[] {
  return flow.transitions
    .filter((t) => t.fromStepId === stepId && t.kind === kind && !t.isSelfLoop)
    .map((t) => t.toStepId);
}

/** The Steps one `kind` transition leads from into this Step. A self-loop is not a predecessor. */
export function predecessors(flow: FlowModel, stepId: string, kind: Transition["kind"]): string[] {
  return flow.transitions
    .filter((t) => t.toStepId === stepId && t.kind === kind && !t.isSelfLoop)
    .map((t) => t.fromStepId);
}

/**
 * Step ids reachable from the start Step through ordinary transitions, in breadth-first order,
 * which is the order a reader meets them. A definition that marks no start begins at its first Step.
 */
export function reachableFromStart(flow: FlowModel): string[] {
  const start = flow.steps.find((s) => s.isStart) ?? flow.steps[0];
  if (start === undefined) return [];
  const order = [start.id];
  const seen = new Set(order);
  for (let i = 0; i < order.length; i += 1) {
    for (const next of successors(flow, order[i], "transition")) {
      if (seen.has(next)) continue;
      seen.add(next);
      order.push(next);
    }
  }
  return order;
}

/**
 * The Steps of the Flow's failure handling: those only failure transitions lead to, directly or
 * through other such Steps, such as a record of exhausted retries or a repair after a crash. Every
 * other Step is the main story, which the Flow reaches on purpose: from its start, from an RPC, or
 * as a Step nothing leads into. Dex's analyzer judges Step groups on the same split.
 */
export function failureHandling(flow: FlowModel): ReadonlySet<string> {
  const ids = new Set(flow.steps.map((s) => s.id));
  const linked = flow.transitions.filter((t) => !t.isSelfLoop && ids.has(t.toStepId));
  const entered = new Set(linked.map((t) => t.toStepId));
  const start = flow.steps.find((s) => s.isStart) ?? flow.steps[0];
  const roots = [
    ...(start ? [start.id] : []),
    ...linked.filter((t) => t.kind === "transition" && !ids.has(t.fromStepId)).map((t) => t.toStepId),
    ...flow.steps.filter((s) => !entered.has(s.id)).map((s) => s.id),
  ];
  const story = new Set(roots);
  const queue = [...story];
  for (let i = 0; i < queue.length; i += 1) {
    for (const t of linked) {
      if (t.kind !== "transition" || t.fromStepId !== queue[i] || story.has(t.toStepId)) continue;
      story.add(t.toStepId);
      queue.push(t.toStepId);
    }
  }
  return new Set(flow.steps.filter((s) => !story.has(s.id)).map((s) => s.id));
}

const stories = new WeakMap<FlowModel, FlowModel>();

/**
 * The Flow as the canvas draws it by default: its failure-handling Steps and every failure transition
 * left out, so only what the Flow does on purpose remains. Cached, and its own main story.
 */
export function mainStory(flow: FlowModel): FlowModel {
  const known = stories.get(flow);
  if (known) return known;
  const hidden = failureHandling(flow);
  const story: FlowModel = {
    ...flow,
    steps: flow.steps.filter((s) => !hidden.has(s.id)),
    transitions: flow.transitions.filter(
      (t) => t.kind === "transition" && !hidden.has(t.fromStepId) && !hidden.has(t.toStepId),
    ),
  };
  stories.set(flow, story);
  stories.set(story, story);
  return story;
}

/** The failure transitions that leave a Step, other than one back to itself. */
export function failureRoutes(flow: FlowModel, stepId: string): Transition[] {
  return flow.transitions.filter((t) => t.kind === "failure_transition" && t.fromStepId === stepId && !t.isSelfLoop);
}

/**
 * The entry points that answer this Step's gate: RPCs that publish to a Channel it waits on. This
 * is why Channels need not be drawn as nodes.
 */
export function answeredBy(flow: FlowModel, step: StepModel): string[] {
  const channels = new Set(
    (step.waitFor?.conditions ?? []).flatMap((c) =>
      c.kind === "channel" && c.resourceId !== undefined ? [c.resourceId] : [],
    ),
  );
  if (channels.size === 0) return [];
  const entries = flow.entries.filter((entry) => entry.opensGates.some((gate) => channels.has(gate.channelId)));
  return [...new Set(entries.map((entry) => entry.name))];
}

/**
 * Who acts at a Step, as the project map and the canvas name it: a person answers an Action, someone
 * outside calls in, the clock fires, a child Flow finishes, or the machine runs on. A wait the
 * analyser could not resolve is shown as a wait, never as machine work.
 */
export type WhoActs = "person" | "outside" | "machine" | "clock" | "child" | "waits";

/** The Action labels, or RPC names, of the entries that publish to a Channel this Step waits on. */
export function answeringEntries(flow: FlowModel, step: StepModel): EntryModel[] {
  const channels = new Set(
    (step.waitFor?.conditions ?? []).flatMap((c) => (c.kind === "channel" && c.resourceId ? [c.resourceId] : [])),
  );
  return flow.entries.filter((entry) => entry.opensGates.some((gate) => channels.has(gate.channelId)));
}

export function whoActs(flow: FlowModel, step: StepModel): WhoActs {
  switch (step.actor) {
    case "external":
      return answeringEntries(flow, step).some((entry) => entry.action) ? "person" : "outside";
    case "clock":
      return "clock";
    case "child":
      return "child";
    case "machine":
      return "machine";
    case "unknown":
      return "waits";
  }
}
