// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowV2StartInputSchema } from "@/lib/api/flow/flow-definition";
import { processName } from "@/lib/shared/format";
import { humanFieldName } from "@/components/flow/connector/manifest/manifest-fields";

/**
 * What a start schema or field may carry beyond the kinds Dex declares today: a title, a description and
 * a default or examples, as JSON Schema spells them. The form reads them when present and never needs them.
 */
interface Described {
  title?: unknown;
  description?: unknown;
  default?: unknown;
  examples?: unknown;
}

/** One field of an object schema, as the start definition lists it. */
export type StartField = NonNullable<FlowV2StartInputSchema["fields"]>[number];

/** A description short enough to be a field's label. */
const SHORT_LABEL = 48;

const text = (value: unknown) => (typeof value === "string" ? value.trim() : "");

/**
 * A field's label and hint: its title, or a short description, else its name in words ("refundAmount" is
 * "Refund amount"); a longer description becomes the hint under the field.
 */
export function startFieldWords(field: StartField): { label: string; hint: string } {
  const own = field as StartField & Described;
  const schema = field.schema as FlowV2StartInputSchema & Described;
  const title = text(own.title) || text(schema.title);
  const description = text(own.description) || text(schema.description);
  if (title) return { label: title, hint: description };
  if (description && description.length <= SHORT_LABEL && !description.includes("\n"))
    return { label: description.replace(/\.$/, ""), hint: "" };
  return { label: humanFieldName(field.name), hint: description };
}

/** A schema's suggested value: its default, else its first example; undefined without either. */
export function suggestedValue(schema: FlowV2StartInputSchema, field?: StartField): unknown {
  for (const source of [field as Described | undefined, schema as Described]) {
    if (!source) continue;
    if (source.default !== undefined) return source.default;
    if (Array.isArray(source.examples) && source.examples.length > 0) return source.examples[0];
  }
  return undefined;
}

/** The words a start says on success: "Started a test run of Text summary." */
export function startedWords(flowType: string, live: boolean): string {
  return `Started a ${live ? "live" : "test"} run of ${processName(flowType)}.`;
}
