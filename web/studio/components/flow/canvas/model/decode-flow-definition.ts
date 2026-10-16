// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Flow Definition Graph -> FlowModel. With `decode-steps.ts`, the only code that reads the wire format.
 *
 * Every derived field is a pure function of the graph:
 *  - Step->Step transitions start at decisions, so `transition.from` is remapped through the
 *    decision's `parentId` to its owning Step.
 *  - `actor` comes from wait-condition kinds, `waitFor === null` from `phase`.
 *  - Wait sentences paraphrase framework words and keep Channel and Timer identifiers verbatim.
 *  - A guard label is the last ` and ` clause with double negation undone.
 *  - Near-identical decisions group into one branch; parallel transitions merge, keeping every guard
 *    and every Connector outcome (`generated`) they are taken on.
 *  - Transitions come in source order, the order a Step's code declares its ways out, which the
 *    canvas's main line follows. The analyser sorts edges by node id, which puts line 992 after 1022.
 *  - `opensGates` joins rpc-publish->channel with channel-condition->wait.
 *  - `action` and `views` join an rpc node's name with the Flow v2 Action and view RPC names.
 * Unmodelled wire elements are listed in `dropped` rather than silently disappearing.
 */

import type { FlowDefinitionGraph, FlowDefinitionNode } from "@/lib/api/flow/flow-definition";
import type { Branch, Condition, FlowModel, ResourceModel, ResourceRef, Transition, WaitPhase } from "./flow-model";
import { decodeEntries, decodeSteps, decodeSubflows } from "./decode-steps";
import { simplifyGuard } from "./guard-text";

type WireEdge = FlowDefinitionGraph["edges"][number];

const CONTROL_KINDS = new Set(["transition", "failure_transition"]);
const RESOURCE_KINDS = new Set(["attribute", "channel", "stream"]);
const OWNER_KINDS = new Set(["step", "rpc", "timeout_handler"]);
/** Node kinds the decoder reads. Dispatch nodes are edgeless markers, re-derived per grouped section. */
const KNOWN_NODE_KINDS = new Set([
  ...OWNER_KINDS,
  ...RESOURCE_KINDS,
  "wait",
  "decision",
  "subflow",
  "wait_dispatch",
  "decision_dispatch",
]);

/** Resource edge kinds and the access each one means. */
const ACCESS: Record<string, ResourceRef["access"]> = {
  resource_read: "read",
  resource_write: "write",
  resource_publish: "publish",
  resource_lock: "lock",
};

/** Maps a decision type onto how it closes, or undefined when it moves instead. */
const CLOSES: Record<string, Branch["closes"]> = {
  gracefulComplete: "complete",
  forceComplete: "complete",
  forceFail: "fail",
  deadEnd: "deadEnd",
  forceCompleteIfChannelsEmpty: "completeIfChannelsEmpty",
};

// ---------------------------------------------------------------------------
// wait prose
// ---------------------------------------------------------------------------

/**
 * `SteeredUserMessages.for 1…MAX` -> `SteeredUserMessages`: drops the framework suffix and the
 * receiver chain the analyser records (`self.config.gate_reminder timer`), which is source syntax,
 * not a name. The final identifier stays verbatim because it is the handle somebody acts on.
 */
function cleanConditionLabel(label: string): string {
  const trimmed = label.replace(/\.for\s.*$/, "").trim();
  // Keep any trailing framework word (`timer`) while stripping the chain in front of the identifier.
  return trimmed.replace(/\b(?:self|cls)(?:\.[A-Za-z_][\w]*)*\.([A-Za-z_][\w]*)/g, "$1");
}

/**
 * One prose line for a WaitFor.
 *
 * The line between paraphrase and verbatim: framework vocabulary goes (`anyOf`,
 * `skipWaitImmediately`, `.for 1`), operator vocabulary stays (Channel and Timer names
 * are the handles somebody has to publish to, and inventing prose for them would
 * misrepresent what has to happen).
 */
function waitSentence(type: string, conditions: Condition[]): string {
  const channels = conditions.filter((c) => c.kind === "channel").map((c) => c.label);
  const timers = conditions.filter((c) => c.kind === "timer").map((c) => c.label);
  const subflows = conditions.filter((c) => c.kind === "subflow").map((c) => c.label);

  if (type === "skipWaitImmediately") return "does not wait";

  const clauses: string[] = [];
  if (channels.length > 0) {
    clauses.push(`waits for a message on ${channels.join(" or ")}`);
  }
  if (subflows.length > 0) {
    const every = type === "allOf" ? "every" : "any";
    clauses.push(`waits for ${every} of ${subflows.join(", ")}`);
  }
  if (timers.length > 0) {
    clauses.push(
      channels.length > 0 || subflows.length > 0 ? `or ${timers.join(" or ")} passes` : `waits ${timers.join(" or ")}`,
    );
  }
  // A declared wait with no resolved conditions. Say that, rather than "waits", which
  // would read as a complete answer.
  if (clauses.length === 0) return "waits — conditions not resolved by the analyser";
  return clauses.join(" ");
}

// ---------------------------------------------------------------------------
// projection
// ---------------------------------------------------------------------------

/** Walks `parentId` up to the nearest step, rpc or timeout_handler. */
type OwnerOf = (id: string) => FlowDefinitionNode | undefined;

/** Appends to a list kept per key. */
function addTo<V>(map: Map<string, V[]>, key: string, value: V): void {
  map.set(key, [...(map.get(key) ?? []), value]);
}

function decodeFlow(graph: FlowDefinitionGraph): FlowModel {
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));
  const ownerOf: OwnerOf = (id) => {
    let cur = byId.get(id);
    // Bounded, so a parent cycle on the wire cannot hang the canvas.
    for (let hops = 0; cur !== undefined && hops < 8; hops += 1) {
      if (OWNER_KINDS.has(cur.kind)) return cur;
      if (cur.parentId === undefined) return undefined;
      cur = byId.get(cur.parentId);
    }
    return undefined;
  };

  const resources = decodeResources(graph, ownerOf);
  const waits = decodeWaits(graph, ownerOf);
  const branches = decodeBranches(graph, ownerOf);
  const transitions = decodeTransitions(graph, byId, ownerOf);
  const unknownNodes = graph.nodes.filter((n) => !KNOWN_NODE_KINDS.has(n.kind)).map((n) => `${n.kind} ${n.id}`);

  return {
    flowType: graph.flow.name,
    startStepId: graph.flow.startStepId,
    steps: decodeSteps(graph, transitions.transitions, waits.waitByStep, branches.byOwner, resources.refsByOwner),
    entries: decodeEntries(graph, waits.waitNodeToStep, branches.byOwner, resources.refsByOwner),
    resources: resources.resources,
    subflows: decodeSubflows(graph, waits.waitNodeToStep),
    transitions: transitions.transitions,
    // Anything not modelled at all is named rather than silently absent.
    dropped: [...resources.dropped, ...waits.dropped, ...branches.dropped, ...transitions.dropped, ...unknownNodes],
  };
}

/** Resource nodes, with every resource edge folded into the Step or entry that owns its actor end. */
function decodeResources(
  graph: FlowDefinitionGraph,
  ownerOf: OwnerOf,
): {
  resources: ResourceModel[];
  refsByOwner: Map<string, ResourceRef[]>;
  dropped: string[];
} {
  const resources: ResourceModel[] = graph.nodes
    .filter((n) => RESOURCE_KINDS.has(n.kind))
    .map((n) => ({
      id: n.id,
      kind: n.kind as ResourceModel["kind"],
      name: n.name,
      valueType: n.resource?.valueType ?? "unknown",
      isMap: n.resource?.map === true,
      touchedBy: [],
    }));
  const resourceIndex = new Map(resources.map((r) => [r.id, r]));
  const refsByOwner = new Map<string, ResourceRef[]>();
  const dropped: string[] = [];
  for (const e of graph.edges) {
    const access = ACCESS[e.kind];
    if (access === undefined) continue;
    // `resource_read` points resource -> consumer; the writes point actor -> resource.
    const resource = resourceIndex.get(access === "read" ? e.from : e.to);
    const owner = ownerOf(access === "read" ? e.to : e.from);
    if (resource === undefined || owner === undefined) {
      dropped.push(`${e.kind} ${e.id}`);
      continue;
    }
    const ref: ResourceRef = {
      resourceId: resource.id,
      access,
      label: e.label ?? access,
      phase: typeof e.metadata?.phase === "string" ? e.metadata.phase : undefined,
    };
    addTo(refsByOwner, owner.id, ref);
    resource.touchedBy.push({ ...ref, resourceId: owner.id });
  }
  return { resources, refsByOwner, dropped };
}

/** Each Step's WaitFor, keyed by Step id, and which Step owns each wait node. */
function decodeWaits(
  graph: FlowDefinitionGraph,
  ownerOf: OwnerOf,
): {
  waitByStep: Map<string, WaitPhase>;
  waitNodeToStep: Map<string, string>;
  dropped: string[];
} {
  const waitByStep = new Map<string, WaitPhase>();
  const waitNodeToStep = new Map<string, string>();
  const dropped: string[] = [];
  for (const n of graph.nodes) {
    if (n.kind !== "wait" || n.wait === undefined) continue;
    const owner = ownerOf(n.parentId ?? n.id);
    if (owner === undefined) {
      dropped.push(`wait ${n.id}`);
      continue;
    }
    waitNodeToStep.set(n.id, owner.id);
    const conditions: Condition[] = n.wait.conditions.map((c) => ({
      kind: c.kind,
      label: cleanConditionLabel(c.label),
      resourceId: c.resourceId,
      subFlowId: c.subFlowId,
      expression: c.expression,
    }));
    // A Step may declare up to 2 waits; they merge into one phase for reading, typed by the first.
    const existing = waitByStep.get(owner.id);
    const type = existing?.type ?? n.wait.type;
    const merged = [...(existing?.conditions ?? []), ...conditions];
    waitByStep.set(owner.id, { type, conditions: merged, sentence: waitSentence(type, merged) });
  }
  return { waitByStep, waitNodeToStep, dropped };
}

/** Every decision as a branch of the Step or entry that owns it. */
function decodeBranches(
  graph: FlowDefinitionGraph,
  ownerOf: OwnerOf,
): {
  byOwner: Map<string, Branch[]>;
  dropped: string[];
} {
  const byOwner = new Map<string, Branch[]>();
  const dropped: string[] = [];
  for (const n of graph.nodes) {
    if (n.kind !== "decision" || n.decision === undefined) continue;
    const owner = ownerOf(n.parentId ?? n.id);
    if (owner === undefined) {
      dropped.push(`decision ${n.id}`);
      continue;
    }
    addTo(byOwner, owner.id, {
      decisionTypes: [n.decision.type],
      guard: simplifyGuard(n.condition),
      fullGuards: n.condition === undefined ? [] : [n.condition],
      targets: graph.edges
        .filter((e) => e.kind === "transition" && e.from === n.id)
        .map((e) => ({ stepId: e.to, multiplicity: e.multiplicity })),
      closes: CLOSES[n.decision.type],
      cancels: n.decision.cancellations,
      checkedChannels: n.decision.checkedChannels,
    });
  }
  return { byOwner, dropped };
}

/**
 * Control edges collapsed to owner -> Step. A transition starts at a decision, so its `from` is
 * remapped to the decision's owning Step. Parallel edges merge: several branches converging on one
 * target read as one arrow that keeps every guard.
 */
function decodeTransitions(
  graph: FlowDefinitionGraph,
  byId: Map<string, FlowDefinitionNode>,
  ownerOf: OwnerOf,
): { transitions: Transition[]; dropped: string[] } {
  const byPair = new Map<string, Transition>();
  const dropped: string[] = [];
  for (const e of graph.edges.filter((edge) => CONTROL_KINDS.has(edge.kind)).sort(bySource)) {
    const fromOwner = ownerOf(e.from);
    const toNode = byId.get(e.to);
    if (fromOwner === undefined || toNode === undefined) {
      dropped.push(`${e.kind} ${e.id}`);
      continue;
    }
    const condition = byId.get(e.from)?.condition;
    const outcome = e.metadata?.connectorBranch === true && e.label ? e.label : undefined;
    const key = `${fromOwner.id}->${toNode.id}:${e.kind}`;
    const prior = byPair.get(key);
    if (prior !== undefined) {
      if (outcome !== undefined && !prior.outcomes.includes(outcome)) prior.outcomes.push(outcome);
      if (condition !== undefined) prior.mergedGuards.push(condition);
      prior.guard = `${prior.mergedGuards.length} conditions`;
      continue;
    }
    byPair.set(key, {
      id: e.id,
      fromStepId: fromOwner.id,
      toStepId: toNode.id,
      kind: e.kind as Transition["kind"],
      guard: simplifyGuard(condition),
      mergedGuards: condition === undefined ? [] : [condition],
      outcomes: outcome === undefined ? [] : [outcome],
      multiplicity: e.multiplicity,
      isSelfLoop: fromOwner.id === toNode.id,
    });
  }
  return { transitions: [...byPair.values()], dropped };
}

/**
 * Source position order. An edge without a position, such as an Execute failure route declared in a
 * Step's options, follows the positioned ones in wire order (the sort is stable).
 */
function bySource(a: WireEdge, b: WireEdge): number {
  if (a.span === undefined || b.span === undefined) return Number(a.span === undefined) - Number(b.span === undefined);
  return a.span.startLine - b.span.startLine || a.span.startColumn - b.span.startColumn;
}

/** Null for a graph the decoder cannot read, so the canvas shows its no-valid-definition state. */
export function safeDecode(graph: FlowDefinitionGraph): FlowModel | null {
  if (!Array.isArray(graph.nodes) || !Array.isArray(graph.edges)) return null;
  try {
    return decodeFlow(graph);
  } catch {
    return null;
  }
}
