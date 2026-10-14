// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type APIResponse, type BrowserContext, type Page } from "@playwright/test";
import { readSession } from "@/tests/e2e/support/project";
import { rpc, rpcResult, studioOrigin } from "@/tests/e2e/support/studio-api";
import type { RemoteClient } from "./remote-client";

/** One request, followed by canonical reconciliation even when its response is lost. Never retries a mutation. */
async function settleMutation(label: string, send: () => Promise<APIResponse>, reconcile: () => Promise<void>) {
  const failures: unknown[] = [];
  try {
    const response = await send();
    if (!response.ok()) failures.push(new Error(`${label} returned HTTP ${response.status()}`));
  } catch {
    failures.push(new Error(`${label} response unavailable; no retry was sent`));
  }
  try {
    await reconcile();
  } catch (error) {
    failures.push(error);
  }
  if (failures.length) throw new AggregateError(failures, `${label} failed; canonical reconciliation was attempted`);
}

/** These sets are test ownership, never executor authority; every mutation rereads actual canonical state. */
export async function cleanupRemoteFixture(state: {
  page: Page;
  projectId: string;
  admitted: boolean;
  pairingAccounts: Set<string>;
  connections: Set<string>;
  messages: Set<string>;
  clients: RemoteClient[];
  logins: Page[];
  contexts: BrowserContext[];
  finalizers: (() => Promise<void>)[];
  builderAccountId: string | null;
}) {
  const { page, projectId, connections, messages } = state;
  const errors: unknown[] = [];
  const attempt = async (work: () => Promise<unknown>) => {
    try {
      await work();
    } catch (error) {
      errors.push(error);
    }
  };
  for (const work of state.finalizers.toReversed()) await attempt(work);
  if (state.admitted) {
    // Exclusive fixture, empty initial list, exact authenticated accounts:
    // reconcile a creation whose response was lost without adopting another user's row.
    await attempt(async () => {
      const current = await rpcResult(page.request, "ListRemoteAgentConnections", { projectId });
      for (const row of current.connections)
        if (row.owner && state.pairingAccounts.has(row.owner.accountId)) connections.add(row.connectionId);
    });
    await attempt(async () => {
      const snapshot = await readSession(page, projectId);
      for (const message of snapshot.agent?.queued ?? [])
        if (messages.has(message.messageId)) {
          await settleMutation(
            "Owned queued-message deletion",
            () =>
              rpc(
                page.request,
                "DeleteQueuedMessage",
                {
                  projectId,
                  messageId: message.messageId,
                },
                { timeout: 15_000 },
              ),
            async () => {
              const after = await readSession(page, projectId);
              if (after.agent?.queued.some((item) => item.messageId === message.messageId))
                throw new Error("Owned queued input remains after cleanup");
            },
          );
        }
    });
    await attempt(async () => {
      let snapshot = await readSession(page, projectId);
      const description = snapshot.agent?.description;
      const authority = description?.executor;
      if (
        authority?.mode !== "SESSION_EXECUTOR_MODE_REMOTE" ||
        !authority.remote ||
        !connections.has(authority.remote.connectionId)
      )
        return;
      if (snapshot.agent?.queued.some((item) => messages.has(item.messageId)))
        throw new Error("Owned input remains queued; refusing platform fallback during cleanup");
      if (!description) throw new Error("Owned executor description is absent");
      if (description.status !== "STATUS_WAITING_FOR_MESSAGE") {
        await settleMutation(
          "Owned remote turn Stop",
          () =>
            rpc(
              page.request,
              "InterruptAgent",
              {
                projectId,
                turn: description.turn,
              },
              { timeout: 15_000 },
            ),
          async () => {
            await expect
              .poll(async () => (await readSession(page, projectId)).agent?.description?.status, { timeout: 60_000 })
              .toBe("STATUS_WAITING_FOR_MESSAGE");
          },
        );
      }
      snapshot = await readSession(page, projectId);
      const fresh = snapshot.agent?.description?.executor;
      if (
        fresh?.mode !== "SESSION_EXECUTOR_MODE_REMOTE" ||
        !fresh.remote ||
        !connections.has(fresh.remote.connectionId)
      )
        return;
      await settleMutation(
        "Owned executor switch",
        () =>
          rpc(
            page.request,
            "SetSessionExecutor",
            {
              projectId,
              basisEpoch: fresh.epoch,
            },
            { timeout: 15_000 },
          ),
        async () => {
          const after = await readSession(page, projectId);
          if (
            after.agent?.description?.executor?.remote &&
            connections.has(after.agent.description.executor.remote.connectionId)
          )
            throw new Error("Owned executor remains selected after cleanup");
        },
      );
    });
    for (const connectionId of connections)
      await attempt(async () => {
        const current = await rpcResult(page.request, "ListRemoteAgentConnections", { projectId });
        if (current.currentConnectionId === connectionId)
          throw new Error("Owned connection is current; refusing credential-only cleanup");
        if (!current.connections.some((row) => row.connectionId === connectionId)) return;
        await settleMutation(
          "Exact connection revocation",
          () =>
            rpc(
              page.request,
              "RevokeRemoteAgentConnection",
              {
                projectId,
                connectionId,
              },
              { timeout: 15_000 },
            ),
          async () => {
            const after = await rpcResult(page.request, "ListRemoteAgentConnections", { projectId });
            if (after.connections.some((row) => row.connectionId === connectionId))
              throw new Error("Exact owned credential remains visible after cleanup");
          },
        );
      });
    if (state.builderAccountId)
      await attempt(async () => {
        const accountId = state.builderAccountId;
        if (!accountId) return;
        const current = await rpcResult(page.request, "ListProjectMembers", { projectId });
        if (!current.members.some((member) => member.accountId === accountId)) return;
        await settleMutation(
          "Exact Builder membership removal",
          () =>
            rpc(
              page.request,
              "RemoveProjectMember",
              {
                projectId,
                accountId,
              },
              { timeout: 15_000 },
            ),
          async () => {
            await expect
              .poll(
                async () =>
                  (await rpcResult(page.request, "ListProjectMembers", { projectId })).members.some(
                    (member) => member.accountId === accountId,
                  ),
                { timeout: 60_000 },
              )
              .toBe(false);
          },
        );
      });
  }
  for (const login of state.logins.toReversed())
    await attempt(async () => {
      const response = await login.request.post("/api/auth/logout", {
        headers: { origin: studioOrigin() },
        data: {},
        timeout: 15_000,
      });
      if (!response.ok()) throw new Error(`Own login cleanup returned HTTP ${response.status()}`);
    });
  for (const client of state.clients) client.forget();
  for (const context of state.contexts) await attempt(() => context.close());
  if (errors.length)
    throw new AggregateError(errors, "Remote fixture cleanup failed; exact residuals need reconciliation");
}
