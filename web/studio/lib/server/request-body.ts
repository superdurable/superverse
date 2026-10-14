// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { isRecord, type UnknownRecord } from "@/lib/shared/json";

/** An error whose HTTP status and message the BFF may show the browser. */
export function httpError(status: number, message: string): Error & { status: number } {
  return Object.assign(new Error(message), { status });
}

const SIXTY_FOUR_BIT_RANGE = {
  int64: [-BigInt("9223372036854775808"), BigInt("9223372036854775807")],
  uint64: [BigInt(0), BigInt("18446744073709551615")],
};

/**
 * A 64-bit integer the browser supplied, as its decimal string: a safe integer number or the
 * canonical decimal digits within the kind's range. Anything else is null.
 */
export function decimal64(kind: "int64" | "uint64", value: unknown): string | null {
  const text = typeof value === "number" && Number.isSafeInteger(value) ? String(value) : value;
  if (typeof text !== "string" || !/^(0|-?[1-9][0-9]*)$/.test(text)) return null;
  const [min, max] = SIXTY_FOUR_BIT_RANGE[kind];
  const number = BigInt(text);
  return number >= min && number <= max ? text : null;
}

/** An unsigned 64-bit request header; absent means 0. */
export function parseUint64(value: string | null, name: string): string {
  if (value === null) return "0";
  const text = decimal64("uint64", value);
  if (text === null) throw httpError(400, `Invalid ${name}`);
  return text;
}

/**
 * Reads at most maxBytes of a request body. Content-Length is only an early
 * rejection hint; the bytes actually read are what count.
 */
export async function readBoundedBody(request: Request, maxBytes: number): Promise<Uint8Array> {
  const declared = request.headers.get("content-length")?.trim();
  const rejection =
    declared === undefined
      ? null
      : !/^[0-9]+$/.test(declared)
        ? httpError(400, "Request body is invalid")
        : BigInt(declared) > BigInt(maxBytes)
          ? httpError(413, "Request body is too large")
          : null;
  if (rejection) {
    if (request.body && !request.body.locked) void request.body.cancel("request body rejected").catch(() => undefined);
    throw rejection;
  }
  if (!request.body) return new Uint8Array();
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    for (;;) {
      const result = await reader.read().catch(() => {
        throw httpError(400, "Request body is invalid");
      });
      if (result.done) break;
      if (result.value.byteLength > maxBytes - total) {
        void reader.cancel("request body rejected").catch(() => undefined);
        throw httpError(413, "Request body is too large");
      }
      chunks.push(result.value);
      total += result.value.byteLength;
    }
  } finally {
    reader.releaseLock();
  }
  const body = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return body;
}

/** A JSON object body of at most 1 MiB; GET, HEAD and an empty body read as {}. */
export async function readJSONObject(request: Request): Promise<UnknownRecord> {
  if (request.method === "GET" || request.method === "HEAD") return {};
  const bytes = await readBoundedBody(request, 1024 * 1024);
  if (bytes.byteLength === 0) return {};
  let value: unknown;
  try {
    value = JSON.parse(new TextDecoder().decode(bytes));
  } catch {
    throw httpError(400, "Invalid JSON request body");
  }
  if (!isRecord(value)) throw httpError(400, "Invalid JSON request body");
  return Object.fromEntries(Object.entries(value));
}
