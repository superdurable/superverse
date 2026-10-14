// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Struct, Struct__Output } from "@/lib/generated/protobuf/google/protobuf/Struct";
import type { Timestamp, Timestamp__Output } from "@/lib/generated/protobuf/google/protobuf/Timestamp";
import type { Value, Value__Output } from "@/lib/generated/protobuf/google/protobuf/Value";
import type { JsonObject, JsonValue } from "./json";

// Conversions between protobuf well-known types and their JSON forms. Every
// loader decodes with longs as strings and oneofs set, so a Value names its kind.

/** A message field Go always sets, such as the DTO a response wraps; an absent one is a server defect. */
export function required<T>(value: T | null | undefined, name: string): T {
  if (value === null || value === undefined) throw new Error(`The ${name} is unavailable.`);
  return value;
}

/** A generated enum value name without its prefix; UNSPECIFIED, which means absent, is "". */
export type EnumText<V extends string, P extends string> = V extends `${P}_UNSPECIFIED`
  ? ""
  : V extends `${P}_${infer T}`
    ? T
    : never;

/**
 * The persisted domain value an enum value names, which is also the text Studio shows:
 * enumText("PROJECT_STATUS_READY", "PROJECT_STATUS") is "READY".
 */
export function enumText<P extends string, V extends `${P}_${string}`>(value: V, prefix: P): EnumText<V, P> {
  const text = value.slice(prefix.length + 1);
  return (text === "UNSPECIFIED" ? "" : text) as EnumText<V, P>;
}

export function timestampToISO(value: Timestamp__Output | null | undefined): string | null {
  return value ? new Date(Number(value.seconds) * 1000 + value.nanos / 1e6).toISOString() : null;
}

export function isoToTimestamp(value: string | null | undefined): Timestamp | null {
  const milliseconds = Date.parse(value ?? "");
  return Number.isFinite(milliseconds)
    ? { seconds: String(Math.floor(milliseconds / 1000)), nanos: (milliseconds % 1000) * 1e6 }
    : null;
}

export function decodeStruct(value: Struct__Output | null | undefined): JsonObject | null {
  return value ? Object.fromEntries(Object.entries(value.fields).map(([key, item]) => [key, decodeValue(item)])) : null;
}

function decodeValue(value: Value__Output): JsonValue {
  switch (value.kind) {
    case "nullValue":
      return null;
    case "stringValue":
      return value.stringValue ?? "";
    case "numberValue":
      return value.numberValue ?? 0;
    case "boolValue":
      return value.boolValue ?? false;
    case "listValue":
      return value.listValue?.values.map(decodeValue) ?? [];
    case "structValue":
      return decodeStruct(value.structValue) ?? {};
    case undefined:
      throw new Error("Invalid protobuf JSON value");
  }
}

export function encodeStruct(value: object): Struct {
  return { fields: Object.fromEntries(Object.entries(value).map(([key, item]) => [key, encodeValue(item)])) };
}

function encodeValue(value: unknown): Value {
  if (value === null || value === undefined) return { nullValue: "NULL_VALUE" };
  if (typeof value === "string") return { stringValue: value };
  if (typeof value === "number" && Number.isFinite(value)) return { numberValue: value };
  if (typeof value === "boolean") return { boolValue: value };
  if (Array.isArray(value)) return { listValue: { values: value.map(encodeValue) } };
  if (typeof value === "object") return { structValue: encodeStruct(value) };
  throw new Error("Value is not JSON");
}
