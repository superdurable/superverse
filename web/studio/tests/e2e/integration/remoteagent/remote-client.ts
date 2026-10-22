// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { object, string } from "@/tests/e2e/support/json-assert";
import type { RpcOutput } from "@/lib/shared/rpc-methods";

/** Assert the actual HTTP uint64 representation, without truncating a fixture's unsafe number. */
export function wireInteger(value: unknown, label: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0)
    throw new Error(`${label} must be a safe nonnegative HTTP integer`);
  return value;
}

/** External user behavior only: every request reaches the real listener; only transient reads retry. */
export class RemoteClient {
  #token = "";
  readonly #origin: URL;
  constructor(
    readonly connectionId: string,
    endpoint: string,
  ) {
    this.#origin = new URL(endpoint);
    if (
      this.#origin.pathname !== "/" ||
      this.#origin.search ||
      this.#origin.hash ||
      this.#origin.username ||
      this.#origin.password
    )
      throw new Error("Pairing endpoint must be a credential-free origin");
  }

  async post(path: "pair" | "activate" | "sync" | "report" | "disconnect", input: unknown, expected = 200) {
    const retryRead = path === "sync" && expected === 200;
    const attemptTimeout = retryRead ? 40_000 : 30_000;
    const deadline = Date.now() + (retryRead ? 120_000 : attemptTimeout);
    let backoff = 100;
    for (;;) {
      let response: globalThis.Response;
      try {
        response = await fetch(new URL(`/remote-agent/v1/${path}`, this.#origin), {
          method: "POST",
          redirect: "error",
          signal: AbortSignal.timeout(Math.min(attemptTimeout, Math.max(1, deadline - Date.now()))),
          headers: {
            "Content-Type": "application/json",
            ...(this.#token ? { Authorization: `Bearer ${this.#token}` } : {}),
          },
          body: JSON.stringify(input),
        });
      } catch {
        if (retryRead && Date.now() + backoff < deadline) {
          await new Promise((resolve) => setTimeout(resolve, backoff));
          backoff = Math.min(1000, backoff * 2);
          continue;
        }
        throw new Error(
          `Remote ${path} transport failed; ${retryRead ? "read retry deadline exhausted" : "outcome is unknown"}`,
        );
      }
      // Sync is read-only. Match dexai's bounded backoff without ever
      // replaying an activation/report with an uncertain external outcome.
      if (retryRead && [429, 503].includes(response.status)) {
        const retryAfter = Number(response.headers.get("retry-after"));
        const delay = Math.max(backoff, Number.isFinite(retryAfter) ? retryAfter * 1000 : 0);
        if (Date.now() + delay < deadline) {
          await response.body?.cancel();
          await new Promise((resolve) => setTimeout(resolve, delay));
          backoff = Math.min(1000, backoff * 2);
          continue;
        }
      }
      if (response.status !== expected) {
        await response.body?.cancel();
        throw new Error(`Remote ${path} returned HTTP ${response.status}`);
      }
      try {
        return object(await response.json(), `Remote ${path} response`);
      } catch {
        throw new Error(`Remote ${path} returned an invalid JSON object`);
      }
    }
  }

  async pair(pairing: RpcOutput<"CreateRemoteAgentPairing">, version = "0.0.0-dev") {
    const result = await this.post("pair", { code: pairing.code, bridge: { version, os: "darwin", arch: "arm64" } });
    this.#token = string(result.token, "Bridge token");
    if (this.#token.length !== 43 || result.connectionId !== this.connectionId)
      throw new Error("Pair response did not identify the exact connection with a canonical token");
  }

  async activate(label: string) {
    return (await this.activateWithCursor(label)).fence;
  }

  async activateWithCursor(label: string) {
    const response = await this.post("activate", {
      bridgeVersion: "0.0.0-dev",
      binding: {
        codexVersion: "integration",
        providerThreadId: label,
        hostLabel: label,
      },
    });
    return {
      fence: {
        epoch: wireInteger(response.epoch, "Epoch"),
        generation: wireInteger(response.generation, "Generation"),
      },
      lastSequence: wireInteger(response.lastSequence, "Activation cursor"),
    };
  }

  forget() {
    this.#token = "";
  }
}

/** Read only the canonical outer envelope; body text is escaped data. */
export function inputText(entry: unknown): string | null {
  const value = object(entry, "Remote entry");
  if (value.role !== "user" || value.kind !== "user_message" || typeof value.text !== "string") return null;
  const text = value.text;
  const prefix = ["Project message (JSON): ", "Superverse message (JSON): "].find((candidate) =>
    text.startsWith(candidate),
  );
  if (!prefix) throw new Error("Attributed user entry lacks its canonical sender envelope");
  const body = object(JSON.parse(text.slice(prefix.length)), "Sender envelope");
  return string(body.text, "Public input text");
}
