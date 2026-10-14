// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import {
  expect,
  type APIRequestContext,
  type APIResponse,
  type Locator,
  type Request,
  type Response,
} from "@playwright/test";
import { lstat, mkdir } from "node:fs/promises";
import { isAbsolute } from "node:path";
import type { RpcInput, RpcOutput, UnaryMethod } from "@/lib/shared/rpc-methods";
import { requireCondition, string } from "./json-assert";

// Generic real-Studio helpers shared by the journeys. Never record credentials,
// OAuth codes, inbox contents or private URLs.

/** Studio's origin, which every BFF POST must carry. */
export function studioOrigin(): string {
  return new URL(string(process.env.SUPERVERSE_E2E_BASE_URL, "Studio base URL")).origin;
}

/** Posts one business RPC through /api/rpc as a Studio page does: same origin, JSON body, `context`'s login. */
export function rpc<M extends UnaryMethod>(
  context: APIRequestContext,
  method: M,
  input: RpcInput<M>,
  options: { timeout?: number } = {},
): Promise<APIResponse> {
  return context.post(`/api/rpc/${method}`, { headers: { origin: studioOrigin() }, data: input, ...options });
}

/** How long rpcResult asks again while the server answers that it is temporarily unavailable. */
const UNAVAILABLE_RETRY_MS = 60_000;

/**
 * The decoded response of one RPC that must succeed. A 503 is the server's temporary unavailability, such as a Dex
 * read deadline under host load, and is asked again after its Retry-After for up to a minute; every caller sends a
 * read or an idempotent command. Any other failure carries only its HTTP status, never the body.
 */
export async function rpcResult<M extends UnaryMethod>(
  context: APIRequestContext,
  method: M,
  input: RpcInput<M>,
): Promise<RpcOutput<M>> {
  const deadline = Date.now() + UNAVAILABLE_RETRY_MS;
  for (;;) {
    const response = await rpc(context, method, input);
    if (response.status() === 503 && Date.now() < deadline) {
      const seconds = Number(response.headers()["retry-after"]);
      await new Promise((resolve) => setTimeout(resolve, 1000 * (seconds >= 1 && seconds <= 5 ? seconds : 1)));
      continue;
    }
    if (!response.ok())
      throw Object.assign(new Error(`${method} returned HTTP ${response.status()}`), { status: response.status() });
    return response.json();
  }
}

/** A request ID the page generates: a version 4 UUID. */
export const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

/** Matches the page's POST of `method` to /api/rpc. */
export const rpcPosted = (method: UnaryMethod) => (request: Request) =>
  request.method() === "POST" && new URL(request.url()).pathname === `/api/rpc/${method}`;

/** Matches the response to the page's POST of `method`, for page.waitForResponse. */
export const rpcAnswered = (method: UnaryMethod) => (response: Response) => rpcPosted(method)(response.request());

export async function privateOwnerStateDirectory(): Promise<string> {
  const directory = string(process.env.SUPERVERSE_E2E_PRIVATE_STATE_DIR, "Private owner-state directory");
  requireCondition(isAbsolute(directory), "Private owner-state directory must be an explicit absolute path");
  try {
    await mkdir(directory, { recursive: true, mode: 0o700 });
    const info = await lstat(directory);
    requireCondition(
      info.isDirectory() && !info.isSymbolicLink() && (info.mode & 0o777) === 0o700,
      "Private owner-state directory must be a regular directory with mode 0700",
    );
  } catch {
    throw new Error("Private owner-state directory is unavailable or is not mode 0700");
  }
  return directory;
}

export async function safeFill(locator: Locator, value: string): Promise<void> {
  try {
    await locator.fill(value);
  } catch {
    throw new Error("A private configuration field could not be filled");
  }
}

/**
 * Chooses option `name` of a Studio dropdown: clicks its trigger, the button that names the choice, and the
 * option of that name in the listbox it opens, which closes once the option is chosen.
 */
export async function chooseOption(trigger: Locator, name: string | RegExp): Promise<void> {
  await trigger.click();
  const listbox = trigger.page().getByRole("listbox");
  await listbox.getByRole("option", { name, exact: typeof name === "string" }).click();
  await expect(listbox).toHaveCount(0);
}
