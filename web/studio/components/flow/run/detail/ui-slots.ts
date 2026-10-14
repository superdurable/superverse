// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowV2Definition, FlowV2Field } from "@/lib/api/flow/flow-definition";
import type { FlowSearchRow } from "@/lib/api/flow/flow-types";

/**
 * Where a Flow says each of its Attributes belongs, read off the Display contract.
 *
 * Declared once, on Display, and resolved against whatever the reader actually has: a list row holds
 * indexed Attributes and a Summary read, a drawer holds the whole Display. So one declaration serves
 * both surfaces and each draws the slots it can reach.
 */
type UISlotName = "title" | "subtitle" | "status" | "recommendation" | "reason";

/**
 * Slot order is the order a reader needs them: what this is, what it is about, where it has got to,
 * what is proposed, and why.
 */
const UI_SLOT_NAMES: readonly UISlotName[] = ["title", "subtitle", "status", "recommendation", "reason"];

/** The Display fields per slot, in declaration order. Only `reason` is ever longer than one. */
export type UISlots = ReadonlyMap<UISlotName, FlowV2Field[]>;

function isUISlotName(value: string): value is UISlotName {
  return (UI_SLOT_NAMES as readonly string[]).includes(value);
}

export function fieldsBySlot(definition: FlowV2Definition): UISlots {
  const slots = new Map<UISlotName, FlowV2Field[]>();
  for (const field of definition.display.fields) {
    if (field.uiSlot === undefined || !isUISlotName(field.uiSlot)) continue;
    slots.set(field.uiSlot, [...(slots.get(field.uiSlot) ?? []), field]);
  }
  return slots;
}

/**
 * What a list row can say about a run.
 *
 * Every part has a fallback, because a slot is optional and most Flows declare none: a row must read
 * the same for a Flow that filled every slot and one that filled none.
 */
export interface RunRow {
  /** Names the run to a person. The Flow ID when nothing better is declared. */
  title: string;
  /** True when the title is the Flow ID, so the row can stop repeating it. */
  titleIsFlowID: boolean;
  /** Where the run has got to, in the Flow's own words when it has any. */
  status: string;
}

export function runRow(flow: FlowSearchRow, slots: UISlots): RunRow {
  const title = firstValue(flow, slots.get("title"));
  const status = firstValue(flow, slots.get("status"));
  return {
    title: title ?? flow.flowId,
    titleIsFlowID: title === null,
    status: status ?? flow.flowStatus,
  };
}

/**
 * The first slotted Attribute this run carries a value for.
 *
 * Indexed Attributes before Summary only because a row is more likely to be filtered on one; either
 * source is equally live. A slot whose Attribute is Display-only resolves to null here and is drawn
 * by the drawer instead.
 */
function firstValue(flow: FlowSearchRow, fields: FlowV2Field[] | undefined): string | null {
  for (const field of fields ?? []) {
    const value = flow.indexedAttributes[field.attributeKey] ?? flow.summary?.[field.attributeKey];
    if (value === null || value === undefined || value === "") continue;
    return String(value);
  }
  return null;
}

/** The slotted Display fields in slot order, not in whatever order the author declared them. */
export function leadFields(definition: FlowV2Definition): FlowV2Field[] {
  const slots = fieldsBySlot(definition);
  return UI_SLOT_NAMES.flatMap((slot) => slots.get(slot) ?? []);
}
