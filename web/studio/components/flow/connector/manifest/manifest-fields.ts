// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { isRecord } from "@/lib/shared/json";
import type { ManifestField } from "@/lib/api/flow/connector";

/** Release-manifest fields as form text: how a stored value is shown, and how typed text is parsed back. */

export function isManifestFieldInputRequired(field: ManifestField): boolean {
  return field.required && field.default === undefined;
}

/** The value a field's text stands for; structured types are typed as JSON and checked against the field. */
export function parseManifestFieldInput(field: ManifestField, text: string): unknown {
  if (!["integer", "boolean", "stringList", "stringMap"].includes(field.type)) return text;
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    throw new Error(`${field.name} must contain valid ${field.type} JSON`);
  }
  if (field.type === "integer" && (typeof value !== "number" || !Number.isSafeInteger(value)))
    throw new Error(`${field.name} must be an integer`);
  if (field.type === "boolean" && typeof value !== "boolean") throw new Error(`${field.name} must be true or false`);
  if (field.type === "stringList") {
    if (
      !Array.isArray(value) ||
      value.some((item) => typeof item !== "string" || (field.enum?.length && !field.enum.includes(item)))
    )
      throw new Error(`${field.name} must contain permitted string values`);
    if (field.uniqueItems && new Set(value).size !== value.length)
      throw new Error(`${field.name} must not repeat values`);
  }
  if (field.type === "stringMap" && (!isRecord(value) || Object.values(value).some((item) => typeof item !== "string")))
    throw new Error(`${field.name} must contain string map values`);
  return value;
}

/** A stored value as the text its input shows; a duration stored in nanoseconds keeps its unit. */
export function manifestFormFieldText(field: ManifestField, value: unknown): string {
  if (field.type === "duration" && typeof value === "number" && Number.isSafeInteger(value)) return `${value}ns`;
  return typeof value === "string" ? value : (JSON.stringify(value) ?? "");
}

export function manifestFieldDefaultText(field: ManifestField): string | undefined {
  if (field.default === undefined || field.type === "secretString") return undefined;
  const value = typeof field.default === "string" ? field.default : JSON.stringify(field.default);
  return `(Default: ${value})`;
}

/** A description split around its http(s) URLs, so they can render as links. */
export function descriptionParts(description: string): { text: string; url?: string }[] {
  const parts: { text: string; url?: string }[] = [];
  const pattern = /https?:\/\/[^\s,;)]+/g;
  let start = 0;
  for (const match of description.matchAll(pattern)) {
    const index = match.index ?? 0;
    if (index > start) parts.push({ text: description.slice(start, index) });
    parts.push({ text: match[0], url: match[0] });
    start = index + match[0].length;
  }
  if (start < description.length) parts.push({ text: description.slice(start) });
  return parts;
}
