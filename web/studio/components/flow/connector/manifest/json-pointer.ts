// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { isRecord } from "@/lib/shared/json";
import type { ConnectorUIBinding } from "@/lib/api/flow/connector";

/**
 * Reads and writes the configuration locations a Connector UI unit is bound to. A unit's output
 * is not trusted: it may only fill its declared ports, and no path may reach a prototype.
 */

/** A copy of `configuration` with each declared port of `value` written at its binding's pointer. */
export function mergeUnitValue(
  configuration: Record<string, unknown>,
  bindings: ConnectorUIBinding[],
  value: Record<string, unknown>,
): Record<string, unknown> {
  const next = JSON.parse(JSON.stringify(configuration)) as Record<string, unknown>;
  const allowedPorts = new Set(bindings.map((binding) => binding.port));
  for (const port of Object.keys(value)) {
    if (!allowedPorts.has(port)) throw new Error(`Unit returned undeclared port ${port}`);
  }
  for (const binding of bindings) {
    if (Object.hasOwn(value, binding.port)) setJSONPointerValue(next, binding.jsonPointer, value[binding.port]);
  }
  return next;
}

export function jsonPointerValue(configuration: Record<string, unknown>, pointer: string): unknown {
  let current: unknown = configuration;
  for (const segment of jsonPointerSegments(pointer)) {
    if (!isRecord(current)) return undefined;
    current = current[segment];
  }
  return current;
}

/** One property name escaped as a JSON Pointer segment. */
export function jsonPointerSegment(name: string): string {
  return name.replace(/~/g, "~0").replace(/\//g, "~1");
}

function setJSONPointerValue(configuration: Record<string, unknown>, pointer: string, value: unknown) {
  const segments = jsonPointerSegments(pointer);
  if (segments.length === 0) throw new Error("Connector UI binding cannot replace the configuration root");
  let current = configuration;
  for (const segment of segments.slice(0, -1)) {
    if (!isRecord(current[segment])) current[segment] = {};
    current = current[segment] as Record<string, unknown>;
  }
  current[segments[segments.length - 1]] = value;
}

function jsonPointerSegments(pointer: string): string[] {
  if (!pointer.startsWith("/")) throw new Error("Connector UI binding has an invalid JSON Pointer");
  return pointer
    .slice(1)
    .split("/")
    .map((segment) => {
      const decoded = segment.replace(/~1/g, "/").replace(/~0/g, "~");
      if (decoded === "__proto__" || decoded === "prototype" || decoded === "constructor")
        throw new Error("Connector UI binding path is unsafe");
      return decoded;
    });
}
