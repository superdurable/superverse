// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowDefinitionGraph } from "@/lib/api/flow/flow-definition";
import type {
  Actor,
  Branch,
  EntryModel,
  EntryView,
  RecoveryRole,
  ResourceRef,
  StepModel,
  SubFlowRef,
  Transition,
  WaitPhase,
} from "./flow-model";

/** The Steps, entries and SubFlows of a Flow, each with the roles read off the decoded graph. */

function branchKey(b: Branch): string {
  return JSON.stringify([
    [...b.decisionTypes].sort(),
    b.targets.map((t) => `${t.stepId}${t.multiplicity ?? ""}`).sort(),
    (b.checkedChannels ?? []).slice().sort(),
    (b.cancels ?? []).map((c) => `${c.stepId}:${c.scope}`).sort(),
    b.closes ?? "",
  ]);
}

function groupBranches(raw: Branch[]): Branch[] {
  const byKey = new Map<string, Branch>();
  for (const b of raw) {
    const key = branchKey(b);
    const seen = byKey.get(key);
    if (seen === undefined) {
      byKey.set(key, { ...b, fullGuards: [...b.fullGuards] });
      continue;
    }
    seen.fullGuards.push(...b.fullGuards);
    // Multiple guards reaching an identical outcome: the count is the honest label.
    if (seen.guard !== b.guard) seen.guard = `${seen.fullGuards.length} conditions`;
  }
  return [...byKey.values()];
}

/**
 * Who has to act before a Step can move. A declared wait whose conditions the analyser did not
 * resolve is `unknown`, never rounded down to `machine`: that is how a human gate becomes invisible.
 */
function actorOf(wait: WaitPhase | null): Actor {
  if (wait === null) return "machine";
  const kinds = new Set(wait.conditions.map((c) => c.kind));
  if (kinds.has("channel")) return "external";
  if (kinds.has("subflow")) return "child";
  if (kinds.has("timer")) return "clock";
  return "unknown";
}

/**
 * Dex names no Step as "recovery", so the role is read off the failure relation: how many Steps
 * route exhausted retries here, and whether this Step sends the Flow onward afterwards.
 */
function recoveryRoleOf(failures: number, onward: number): RecoveryRole {
  if (failures === 0) return "none";
  if (failures === 1) return "fallback";
  return onward === 0 ? "sink" : "dispatcher";
}

/**
 * Per-Step transition counts, self-loops excluded. Control and failure in-degree are counted
 * separately because they mean different things: many transitions arriving is a legibility
 * problem, many failure edges arriving is an ordinary compensation target. Ordinary out-degree
 * tells a recovery sink from a dispatcher.
 */
function transitionDegrees(transitions: Transition[]): {
  inbound: Map<string, number>;
  inboundFailure: Map<string, number>;
  outbound: Map<string, number>;
} {
  const count = (map: Map<string, number>, id: string): void => {
    map.set(id, (map.get(id) ?? 0) + 1);
  };
  const degrees = {
    inbound: new Map<string, number>(),
    inboundFailure: new Map<string, number>(),
    outbound: new Map<string, number>(),
  };
  for (const t of transitions) {
    if (t.isSelfLoop) continue;
    if (t.kind === "failure_transition") {
      count(degrees.inboundFailure, t.toStepId);
    } else {
      count(degrees.inbound, t.toStepId);
      count(degrees.outbound, t.fromStepId);
    }
  }
  return degrees;
}

/** The start Step first, then by label and StepType. */
function readingOrder(a: StepModel, b: StepModel): number {
  if (a.isStart !== b.isStart) return a.isStart ? -1 : 1;
  return a.label.localeCompare(b.label) || a.stepType.localeCompare(b.stepType);
}

export function decodeSteps(
  graph: FlowDefinitionGraph,
  transitions: Transition[],
  waitByStep: Map<string, WaitPhase>,
  branchesByOwner: Map<string, Branch[]>,
  refsByOwner: Map<string, ResourceRef[]>,
): StepModel[] {
  const { inbound, inboundFailure, outbound } = transitionDegrees(transitions);
  const stepNodes = graph.nodes.filter((n) => n.kind === "step");
  return stepNodes
    .map((n): StepModel => {
      // `phase` is a perfect proxy: every 'wait_for+execute' step has a wait child,
      // every 'execute' step has none. Fall back to the wait map when phase is absent.
      const declaresWait = n.phase?.includes("wait_for") ?? waitByStep.has(n.id);
      const waitFor = declaresWait ? (waitByStep.get(n.id) ?? null) : null;
      const count = inbound.get(n.id) ?? 0;
      const failures = inboundFailure.get(n.id) ?? 0;
      return {
        id: n.id,
        stepType: n.name,
        label: typeof n.metadata?.displayName === "string" ? n.metadata.displayName : n.name,
        isStart: n.start === true || graph.flow.startStepId === n.id,
        actor: actorOf(waitFor),
        // A hub is relative to the graph: an absolute in-degree threshold that catches a real hub
        // also catches ordinary Steps in a larger Flow, so the share of all transitions decides.
        isHub: count >= 5 && count >= transitions.length * 0.25,
        isRecoveryHub: failures >= 2,
        // A blanket failure policy routes every eligible Step here. The floor of 5 keeps a small
        // saga's few failure edges drawn, since they are still countable at a glance.
        hasUniformFailurePolicy: failures >= 5 && failures >= (stepNodes.length - 1) * 0.8,
        recoveryRole: recoveryRoleOf(failures, outbound.get(n.id) ?? 0),
        inboundCount: count,
        inboundFailureCount: failures,
        waitFor,
        execute: { branches: groupBranches(branchesByOwner.get(n.id) ?? []) },
        resources: refsByOwner.get(n.id) ?? [],
        span: n.span,
        isConnectorStep: n.metadata?.connectorFactory === true,
        connector: connectorIdentity(n.metadata?.connector),
        explanation: typeof n.metadata?.explanation === "string" ? n.metadata.explanation : undefined,
      };
    })
    .sort(readingOrder);
}

/**
 * RPCs and timeout handlers, each joined to the Steps whose gates it opens (rpc -publish-> channel
 * -condition-> wait) and to the Flow v2 Action and views it implements.
 */
export function decodeEntries(
  graph: FlowDefinitionGraph,
  waitNodeToStep: Map<string, string>,
  branchesByOwner: Map<string, Branch[]>,
  refsByOwner: Map<string, ResourceRef[]>,
): EntryModel[] {
  const waitConditionEdges = graph.edges.filter((e) => e.kind === "wait_condition");
  const stepsWaitingOn = (channelId: string): string[] => [
    ...new Set(waitConditionEdges.filter((e) => e.from === channelId).flatMap((e) => waitNodeToStep.get(e.to) ?? [])),
  ];
  const actionByRpcName = new Map<string, NonNullable<EntryModel["action"]>>(
    (Array.isArray(graph.v2?.actions) ? graph.v2.actions : []).map((action) => [
      action.rpcName,
      { label: action.label, requiredPermission: action.requiredPermission },
    ]),
  );
  const viewsOf = (rpcName: string): EntryView[] =>
    (["summary", "display"] as const).filter((view) => graph.v2?.[view]?.rpcName === rpcName);

  return graph.nodes
    .filter((n) => n.kind === "rpc" || n.kind === "timeout_handler")
    .map((n): EntryModel => {
      const refs = refsByOwner.get(n.id) ?? [];
      const isRpc = n.kind === "rpc";
      const action = isRpc ? actionByRpcName.get(n.name) : undefined;
      return {
        id: n.id,
        kind: isRpc ? "rpc" : "timeoutHandler",
        name: n.name,
        branches: groupBranches(branchesByOwner.get(n.id) ?? []),
        resources: refs,
        opensGates: refs
          .filter((r) => r.access === "publish")
          .map((r) => ({ channelId: r.resourceId, stepIds: stepsWaitingOn(r.resourceId) }))
          .filter((g) => g.stepIds.length > 0),
        ...(action === undefined ? {} : { action }),
        views: isRpc ? viewsOf(n.name) : [],
        span: n.span,
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function decodeSubflows(graph: FlowDefinitionGraph, waitNodeToStep: Map<string, string>): SubFlowRef[] {
  return graph.nodes
    .filter((n) => n.kind === "subflow")
    .map((n) => {
      const edge = graph.edges.find((e) => e.kind === "subflow" && e.to === n.id);
      return {
        id: n.id,
        // A SubFlow held as a Flow attribute arrives as `self.curate`; the receiver is source syntax
        // and is dropped, the identifier is kept verbatim.
        flowType: n.name.replace(/^self\./, ""),
        external: n.external === true,
        startedByStepId: edge === undefined ? undefined : waitNodeToStep.get(edge.from),
      };
    });
}

function connectorIdentity(value: unknown): StepModel["connector"] {
  if (typeof value !== "object" || value === null) return undefined;
  const connector = value as Record<string, unknown>;
  if (
    typeof connector.connectorId !== "string" ||
    typeof connector.operationId !== "string" ||
    typeof connector.operationKind !== "string" ||
    typeof connector.connectionName !== "string" ||
    typeof connector.moduleVersion !== "string"
  )
    return undefined;
  return {
    connectorId: connector.connectorId,
    operationId: connector.operationId,
    operationKind: connector.operationKind,
    connectionName: connector.connectionName,
    moduleVersion: connector.moduleVersion,
  };
}
