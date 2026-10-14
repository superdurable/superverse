// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Assertions for JSON read from fixtures and real service responses.

import { isRecord, type UnknownRecord } from "@/lib/shared/json";

export function object(value: unknown, label: string): UnknownRecord {
  if (!isRecord(value)) throw new Error(`${label} must be an object`);
  return value;
}

export function string(value: unknown, label: string): string {
  if (typeof value !== "string" || !value.trim() || /[\r\n\0]/.test(value))
    throw new Error(`${label} must be a nonempty single-line string`);
  return value;
}

export function matching(value: unknown, pattern: RegExp, label: string): string {
  if (typeof value !== "string" || !pattern.test(value)) throw new Error(`${label} is invalid`);
  return value;
}

export function array(value: unknown, label: string): unknown[] {
  if (!Array.isArray(value)) throw new Error(`${label} must be an array`);
  return value;
}

export function requireCondition(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}
