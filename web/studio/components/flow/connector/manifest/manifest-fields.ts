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

/** Words people write in capitals, or in their own spelling, inside a field name. */
const SPELLED: Record<string, string> = Object.fromEntries(
  ["api", "id", "url", "uri", "http", "https", "ai", "llm", "sms", "sse", "json", "sql", "ip", "csv"]
    .map((word) => [word, word.toUpperCase()])
    .concat([
      ["ids", "IDs"],
      ["urls", "URLs"],
      ["oauth", "OAuth"],
      ["openai", "OpenAI"],
    ]),
);

/**
 * A field name as people say it: "refundAmount" is "Refund amount", "api_key" is "API key" and
 * "SUMMARY_GREETING" is "Summary greeting"; a name without words stays as it is.
 */
export function humanFieldName(name: string): string {
  const shouting = /^[A-Z0-9_]+$/.test(name) ? name.toLowerCase() : name;
  const words = shouting
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .split(/[\s_.-]+/)
    .filter(Boolean)
    .map((word) => SPELLED[word.toLowerCase()] ?? (/^[A-Z0-9]{2,}$/.test(word) ? word : word.toLowerCase()));
  if (words.length === 0) return name;
  return (
    words[0].charAt(0).toUpperCase() +
    words[0].slice(1) +
    words
      .slice(1)
      .map((word) => ` ${word}`)
      .join("")
  );
}

/** A manifest field's label: its title when the release gives one, else its name in words. */
export function manifestFieldLabel(field: ManifestField): string {
  const title = (field as ManifestField & { title?: unknown }).title;
  return typeof title === "string" && title.trim() ? title.trim() : humanFieldName(field.name);
}

export function isManifestFieldInputRequired(field: ManifestField): boolean {
  return field.required && field.default === undefined;
}

/** The value a field's text stands for; structured types are typed as JSON and checked against the field. */
export function parseManifestFieldInput(field: ManifestField, text: string): unknown {
  if (!["integer", "boolean", "stringList", "stringMap"].includes(field.type)) return text;
  const label = manifestFieldLabel(field);
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    throw new Error(`${label} must contain valid ${field.type} JSON`);
  }
  if (field.type === "integer" && (typeof value !== "number" || !Number.isSafeInteger(value)))
    throw new Error(`${label} must be an integer`);
  if (field.type === "boolean" && typeof value !== "boolean") throw new Error(`${label} must be true or false`);
  if (field.type === "stringList") {
    if (
      !Array.isArray(value) ||
      value.some((item) => typeof item !== "string" || (field.enum?.length && !field.enum.includes(item)))
    )
      throw new Error(`${label} must contain permitted string values`);
    if (field.uniqueItems && new Set(value).size !== value.length) throw new Error(`${label} must not repeat values`);
  }
  if (field.type === "stringMap" && (!isRecord(value) || Object.values(value).some((item) => typeof item !== "string")))
    throw new Error(`${label} must contain string map values`);
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
