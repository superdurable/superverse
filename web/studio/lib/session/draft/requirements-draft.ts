// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { AgentRequirementsDraft__Output } from "@/lib/generated/protobuf/superverse/v2/AgentRequirementsDraft";

/**
 * The requirements card the agent is writing, as the Session Watch streams it from the card call's partial
 * input: each event is the whole draft so far. It is never the card; the Snapshot's recorded version is.
 */
export interface RequirementsDraft {
  callId: string;
  /** The turn of the model call that writes the card. */
  turn: number;
  /** The confirmed requirements' revision when that call started; 0 when none was recorded. */
  baseRevision: number;
  title: string;
  summary: string;
  features: readonly { id: string; text: string }[];
  connections: readonly { need: string; connector: string }[];
  designSummary: string;
  /** The designed Flows with the Go type names of the Steps written so far. */
  flows: readonly { flowType: string; stepTypes: readonly string[] }[];
  /** The model finished writing it; the Session records or refuses it next. */
  complete: boolean;
}

function draftInteger(value: string): number {
  const result = Number(value);
  if (!/^-?\d+$/.test(value) || !Number.isSafeInteger(result)) throw new Error("Invalid requirements draft integer");
  return result;
}

export function requirementsDraftView(value: AgentRequirementsDraft__Output): RequirementsDraft {
  return {
    callId: value.callId,
    turn: draftInteger(value.turn),
    baseRevision: draftInteger(value.baseRevision),
    title: value.title,
    summary: value.summary,
    features: value.features.map(({ id, text }) => ({ id, text })),
    connections: value.connections.map(({ need, connector }) => ({ need, connector })),
    designSummary: value.designSummary,
    flows: value.flows.map(({ flowType, stepTypes }) => ({ flowType, stepTypes })),
    complete: value.complete,
  };
}

/** What a Snapshot says of the conversation that decides whether a draft is over. */
export interface DraftContext {
  /** The confirmed requirements' revision; 0 when none is recorded. */
  revision: number;
  turn: number;
  /** The agent waits for a message: its turn ended. */
  waiting: boolean;
  /** Whether a recorded message answers the call: its tool result. */
  answers: (callId: string) => boolean;
}

/**
 * Whether the card call of a draft is over in the Snapshot: a version was recorded after the draft's base, a
 * tool result answers the call, or its turn ended. A Snapshot older than the draft's turn says nothing.
 */
export function isDraftOver(draft: RequirementsDraft, context: DraftContext): boolean {
  return (
    context.revision > draft.baseRevision ||
    context.answers(draft.callId) ||
    context.turn > draft.turn ||
    (context.turn === draft.turn && context.waiting)
  );
}
