// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { randomUUID } from "node:crypto";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";

function developerKey(): string {
  const explicit = process.env.ANTHROPIC_API_KEY?.trim();
  if (explicit) return explicit;
  const common = execFileSync("git", ["rev-parse", "--path-format=absolute", "--git-common-dir"], {
    encoding: "utf8",
  }).trim();
  let value = "";
  try {
    value =
      readFileSync(join(dirname(common), ".env"), "utf8")
        .split(/\r?\n/)
        .find((line) => line.startsWith("ANTHROPIC_API_KEY="))
        ?.slice("ANTHROPIC_API_KEY=".length)
        .trim() ?? "";
  } catch {
    throw new Error("Real-model acceptance requires the developer Anthropic key");
  }
  if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'")))
    value = value.slice(1, -1);
  if (!value) throw new Error("Real-model acceptance requires the developer Anthropic key");
  return value;
}

/** Secret transport uses native fetch outside Playwright's request tracing; no key is attached to reports. */
export async function activateAcceptanceCredential(
  page: Page,
  projectId: string,
  readSession: () => Promise<SessionSnapshot__Output>,
  resume: () => Promise<unknown>,
) {
  const base = process.env.SUPERVERSE_E2E_BASE_URL;
  if (!base) throw new Error("Real-model acceptance requires the Studio base URL");
  const origin = new URL(base).origin;
  const cookies = (await page.context().cookies(origin)).map((cookie) => `${cookie.name}=${cookie.value}`).join("; ");
  const call = async (method: string, input: object) => {
    const response = await fetch(`${origin}/api/rpc/${method}`, {
      method: "POST",
      headers: { "content-type": "application/json", origin, cookie: cookies },
      body: JSON.stringify(input),
      signal: AbortSignal.timeout(30_000),
    });
    if (!response.ok) throw new Error(`Project credential acceptance RPC ${method} returned HTTP ${response.status}`);
    return response;
  };
  await expect
    .poll(async () => (await readSession()).agent?.description?.status, { timeout: 60_000 })
    .toBe("STATUS_WAITING_FOR_MESSAGE");
  const before = (await readSession()).agent?.description?.turn ?? "0";
  const current = (await (
    await call("GetModelCredentialInfo", { projectId, provider: "MODEL_PROVIDER_ANTHROPIC" })
  ).json()) as {
    credential: { revision: string };
  };
  const metadata = current.credential;
  let key = developerKey();
  try {
    await call("SetModelCredential", {
      projectId,
      provider: "MODEL_PROVIDER_ANTHROPIC",
      requestId: randomUUID(),
      expectedRevision: metadata.revision,
      apiKey: key,
    });
  } finally {
    key = "";
  }
  await resume();
  await expect
    .poll(async () => BigInt((await readSession()).agent?.description?.turn ?? "0") > BigInt(before), {
      timeout: 30_000,
    })
    .toBe(true);
}
