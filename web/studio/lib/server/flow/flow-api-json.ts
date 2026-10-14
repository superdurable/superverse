// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { httpError } from "@/lib/server/request-body";
import { isRecord } from "@/lib/shared/json";

/**
 * A JSON object whose field values stay as their original JSON text. FDG-defined
 * values (int64 numbers, nested documents) cross to gRPC as exact bytes instead
 * of being reparsed into JavaScript numbers.
 */
export class FlowApiJson {
  readonly fields: ReadonlyMap<string, string>;

  constructor(text = "{}", allowed?: readonly string[]) {
    text = text.trim();
    let value: unknown;
    try {
      value = JSON.parse(text);
    } catch {
      throw httpError(400, "Expected a JSON object");
    }
    if (!isRecord(value)) throw httpError(400, "Expected a JSON object");
    const fields = new Map<string, string>();
    let cursor = skip(text, 1);
    while (text[cursor] !== "}") {
      const keyEnd = stringEnd(text, cursor);
      const key: unknown = JSON.parse(text.slice(cursor, keyEnd));
      if (typeof key !== "string" || fields.has(key) || (allowed && !allowed.includes(key)))
        throw httpError(400, "Unexpected or repeated JSON field");
      cursor = skip(text, keyEnd);
      if (text[cursor++] !== ":") throw httpError(400, "Invalid JSON field");
      cursor = skip(text, cursor);
      const end = valueEnd(text, cursor);
      fields.set(key, text.slice(cursor, end));
      cursor = skip(text, end);
      if (text[cursor] === ",") cursor = skip(text, cursor + 1);
      else if (text[cursor] !== "}") throw httpError(400, "Invalid JSON object");
    }
    this.fields = fields;
  }

  raw(name: string, fallback = "{}"): Buffer {
    return Buffer.from(this.fields.get(name) ?? fallback);
  }

  string(name: string, fallback = ""): string {
    const value: unknown = JSON.parse(this.fields.get(name) ?? JSON.stringify(fallback));
    if (value === null) return fallback;
    if (typeof value !== "string") throw httpError(400, `Invalid ${name}`);
    return value;
  }

  integer(name: string, fallback = 0): number {
    const value: unknown = JSON.parse(this.fields.get(name) ?? String(fallback));
    if (typeof value !== "number" || !Number.isSafeInteger(value)) throw httpError(400, `Invalid ${name}`);
    return value;
  }

  boolean(name: string, fallback = false): boolean {
    const value: unknown = JSON.parse(this.fields.get(name) ?? String(fallback));
    if (typeof value !== "boolean") throw httpError(400, `Invalid ${name}`);
    return value;
  }

  strings(name: string): string[] {
    const value: unknown = JSON.parse(this.fields.get(name) ?? "[]");
    if (!Array.isArray(value) || !value.every((item): item is string => typeof item === "string"))
      throw httpError(400, `Invalid ${name}`);
    return value;
  }

  stringMap(name: string): Record<string, string> {
    const value = new FlowApiJson(this.fields.get(name) ?? "{}");
    return Object.fromEntries([...value.fields.keys()].map((key) => [key, value.string(key)]));
  }

  objects(name: string, allowed: readonly string[]): FlowApiJson[] {
    const raw = this.fields.get(name) ?? "[]";
    if (!Array.isArray(JSON.parse(raw))) throw httpError(400, `Invalid ${name}`);
    const result: FlowApiJson[] = [];
    let cursor = skip(raw, 1);
    while (raw[cursor] !== "]") {
      const end = valueEnd(raw, cursor);
      result.push(new FlowApiJson(raw.slice(cursor, end), allowed));
      cursor = skip(raw, end);
      if (raw[cursor] === ",") cursor = skip(raw, cursor + 1);
    }
    return result;
  }

  /** This object as JSON text with one string field set. */
  withString(name: string, value: string): string {
    return `{${[...new Map([...this.fields, [name, JSON.stringify(value)]]).entries()].map(([key, raw]) => `${JSON.stringify(key)}:${raw}`).join(",")}}`;
  }
}

function skip(text: string, cursor: number): number {
  while (cursor < text.length && /\s/.test(text[cursor])) cursor++;
  return cursor;
}

function stringEnd(text: string, cursor: number): number {
  if (text[cursor] !== '"') throw httpError(400, "Invalid JSON string");
  for (let at = cursor + 1; at < text.length; at++) {
    if (text[at] === "\\") at++;
    else if (text[at] === '"') return at + 1;
  }
  throw httpError(400, "Incomplete JSON string");
}

function valueEnd(text: string, cursor: number): number {
  let depth = 0;
  for (let at = cursor; at < text.length; at++) {
    const char = text[at];
    if (char === '"') {
      at = stringEnd(text, at) - 1;
      continue;
    }
    if (char === "{" || char === "[") depth++;
    else if (char === "}" || char === "]") {
      if (depth === 0) return at;
      depth--;
    } else if (char === "," && depth === 0) return at;
  }
  throw httpError(400, "Incomplete JSON value");
}
