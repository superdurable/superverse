// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/** JSON values, and readers for untyped JSON payloads: history events, Flow state and provider responses. */

export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };
export type JsonObject = { [key: string]: JsonValue };

/** A JSON object whose fields have not been read yet. */
export type UnknownRecord = Record<string, unknown>;

export function isRecord(value: unknown): value is UnknownRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** The value when it is a non-array object, otherwise an empty record. */
export function asRecord(value: unknown): UnknownRecord {
  return isRecord(value) ? value : {};
}

/** Every element read as a record; a non-array is an empty list. */
export function asRecordArray(value: unknown): UnknownRecord[] {
  return Array.isArray(value) ? value.map(asRecord) : [];
}

/** The elements of an array; a non-array is an empty list. */
export function arrayField(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

/** The string elements of an array; a non-array is an empty list. */
export function stringArrayField(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

export function hasFields(value: UnknownRecord): boolean {
  return Object.keys(value).length > 0;
}

/** Present means set to something other than undefined, null or the empty string. */
export function isPresent(value: unknown): boolean {
  return value !== undefined && value !== null && value !== "";
}

export function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function stringField(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/** A finite number, also parsed from a numeric string; 0 otherwise. */
export function numberField(value: unknown): number {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}
