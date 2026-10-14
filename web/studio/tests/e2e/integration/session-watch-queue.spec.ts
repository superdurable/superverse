// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { performance } from "node:perf_hooks";
import { callerFromRequest } from "@/lib/server/caller-envelope";
import { callRpc, grpcBackend, type Caller } from "@/lib/server/grpc-client";
import { handleRpcRequest } from "@/lib/server/rpc/rpc-handler";
import { ownerProjectFixture, type OwnerProject } from "@/tests/e2e/support/private-files";
import { withDeadline } from "@/tests/e2e/support/session-transport";

function fixture(): OwnerProject {
  const file = process.env.SUPERVERSE_E2E_WATCH_FIXTURE;
  if (
    !file ||
    !process.env.SUPERVERSE_E2E_BASE_URL ||
    !process.env.SUPERVERSE_GRPC_ADDRESS ||
    process.env.SUPERVERSE_STUDIO_MOCK === "true"
  )
    throw new Error("Real Watch integration requires a private fixture, running Studio and merged gRPC");
  return ownerProjectFixture(file, "Watch fixture");
}
const bounded = <T>(promise: Promise<T>, milliseconds: number) =>
  withDeadline(promise, milliseconds, "Real Watch integration deadline exceeded");

type SnapshotFrame = { resumeCursor?: string; snapshot?: { projectId?: string } };

/**
 * The first read of a Watch, which must be its snapshot event: the bytes read and the decoded frame.
 * A frame that is not JSON fails with `label` alone, never the frame's content.
 */
async function firstSnapshotFrame(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  label: string,
): Promise<{ bytes: number; frame: SnapshotFrame }> {
  const first = await bounded(reader.read(), 15_000);
  if (first.done || !first.value.byteLength) throw new Error(`${label} omitted its initial snapshot`);
  const text = new TextDecoder().decode(first.value);
  expect(text.includes("event: snapshot\n")).toBe(true);
  const data =
    text
      .split("\n")
      .find((line) => line.startsWith("data: "))
      ?.slice(6) ?? "";
  try {
    return { bytes: first.value.byteLength, frame: JSON.parse(data) };
  } catch {
    throw new Error(`${label} initial frame is not valid JSON`);
  }
}

// Standalone real dependency integration, outside the three full acceptance cases.
// The adapter runs in this process; HTTP login/logout and all gRPC/Dex reads are real.
// Never attach fixture contents, cookies, Session payloads, or model text.
test.use({ trace: "off", screenshot: "off", video: "off" });
for (const trigger of ["revocation", "request-abort"] as const) {
  test(`Session Watch discards queued real data after ${trigger}`, async ({}, testInfo) => {
    test.setTimeout(120_000);
    const input = fixture();
    const origin = new URL(process.env.SUPERVERSE_E2E_BASE_URL!).origin;
    const expectedError = trigger === "revocation" ? "Resource Watch failed" : "Resource Watch canceled";
    const abort = new AbortController();
    const readers: ReadableStreamDefaultReader<Uint8Array>[] = [];
    const failures: Error[] = [];
    let phase = "secondary login";
    let cookie = "",
      caller: Caller | undefined,
      revoked = false;
    let queuedBytes = 0,
      elapsedMilliseconds = 0;
    const auth = async (action: "login" | "logout" | "session") => {
      const response = await fetch(`${origin}/api/auth/${action}`, {
        method: action === "session" ? "GET" : "POST",
        headers: { origin, "content-type": "application/json", ...(cookie ? { cookie } : {}) },
        body:
          action === "login"
            ? JSON.stringify({ email: input.email, password: input.password })
            : action === "logout"
              ? "{}"
              : undefined,
        signal: AbortSignal.timeout(10_000),
      });
      // Status-only checks avoid placing private response bodies in assertion output.
      expect(response.status).toBe(action === "session" ? 401 : 200);
      return response;
    };
    try {
      const login = await auth("login");
      cookie = login.headers.get("set-cookie")?.split(";")[0] ?? "";
      expect(cookie.startsWith("sv2_session=")).toBe(true);
      await login.arrayBuffer();
      const session = { projectId: input.projectId };
      const watchURL = (query: Record<string, string>) =>
        `${origin}/api/rpc/WatchSession?${new URLSearchParams({ ...session, ...query })}`;
      const request = new Request(watchURL({}), { headers: { cookie, origin }, signal: abort.signal });
      caller = callerFromRequest(request);
      phase = "current account authentication";
      await callRpc("GetCurrentAccount", {}, caller, AbortSignal.timeout(10_000));
      // Exercise actual BFF rejection and actual Go input rejection, not fake
      // responses. Neither invalid request can invoke the durable Agent command.
      for (const [method, body, reason] of [
        ["AnswerQuestions", { ...session, unknownField: true }, "AGENT_COMMAND_NOT_DISPATCHED"],
        ["SteerQueuedMessage", { ...session, messageId: "" }, "AGENT_COMMAND_NOT_ADMITTED"],
      ] as const) {
        phase = "typed command rejection";
        const rejected = await fetch(`${origin}/api/rpc/${method}`, {
          method: "POST",
          headers: { cookie, origin, "content-type": "application/json" },
          body: JSON.stringify(body),
          signal: AbortSignal.timeout(15_000),
        });
        expect(rejected.status).toBe(400);
        const problem: { errorInfo?: { reason?: string; domain?: string; metadata?: object } } = await rejected.json();
        expect(problem.errorInfo?.reason).toBe(reason);
        expect(problem.errorInfo?.domain).toBe("superverse.v2");
        expect(Object.keys(problem.errorInfo?.metadata ?? {})).toHaveLength(0);
      }
      phase = "real Watch admission";
      const response = await handleRpcRequest(request, "WatchSession", grpcBackend);
      expect(response.status).toBe(200);
      if (!response.body) throw new Error("Real Watch response has no body");
      const [witness, unread] = response.body.tee();
      readers.push(witness.getReader(), unread.getReader());
      const expectInputSession = ({ snapshot }: SnapshotFrame) =>
        expect(snapshot?.projectId === input.projectId).toBe(true);
      phase = "initial scoped snapshot";
      const { bytes: initialBytes, frame } = await firstSnapshotFrame(readers[0], "Real Watch");
      expectInputSession(frame);
      if (typeof frame.resumeCursor !== "string" || !frame.resumeCursor)
        throw new Error("Real Watch omitted its signed resume cursor");
      phase = "snapshot-first resumed Watch";
      const resumed = await handleRpcRequest(
        new Request(watchURL({ resumeCursor: frame.resumeCursor }), {
          headers: { cookie, origin },
          signal: abort.signal,
        }),
        "WatchSession",
        grpcBackend,
      );
      expect(resumed.status).toBe(200);
      if (!resumed.body) throw new Error("Resumed real Watch has no body");
      const resumedReader = resumed.body.getReader();
      readers.push(resumedReader);
      expectInputSession((await firstSnapshotFrame(resumedReader, "Resumed real Watch")).frame);
      await bounded(resumedReader.cancel(), 3000);
      queuedBytes = initialBytes;
      // No read on branch 2 yet: the observed real snapshot is still buffered there.
      const terminal = (async () => {
        let frames = 0,
          bytes = 0;
        for (;;) {
          let item: ReadableStreamReadResult<Uint8Array>;
          try {
            item = await readers[0].read();
          } catch (error) {
            return error instanceof Error && error.message === expectedError;
          }
          if (item.done) return false; // close/drain must never count as error/discard.
          frames++;
          bytes += item.value.byteLength;
          if (frames > 12 || bytes > 2 * 1024 * 1024)
            throw new Error("Real Watch observation exceeded its bounded frame budget");
        }
      })();
      // Observe errors immediately even if the concurrent auth request itself fails.
      void terminal.catch(() => {});
      const started = performance.now();
      phase = "termination trigger";
      if (trigger === "revocation") {
        await (await auth("logout")).arrayBuffer();
        revoked = true;
        await (await auth("session")).arrayBuffer();
      } else abort.abort();
      phase = "witness sanitized failure";
      expect(await bounded(terminal, 16_000)).toBe(true);
      elapsedMilliseconds = Math.round(performance.now() - started);
      phase = "unread snapshot discard";
      let discarded = false;
      try {
        await bounded(readers[1].read(), 3000);
      } catch (error) {
        discarded = error instanceof Error && error.message === expectedError;
      }
      expect(discarded, "Unread real snapshot bytes must be discarded, not drained after termination").toBe(true);
      if (trigger === "request-abort") {
        phase = "post-abort Session authorization";
        const read = await callRpc("GetSession", session, caller, AbortSignal.timeout(10_000));
        expect(read.session?.projectId === input.projectId).toBe(true);
      }
    } catch {
      // SDK errors can carry transport metadata/details. Never retain their
      // message, stack, cause or object in the reporter's AggregateError.
      failures.push(new Error(`Actual Watch check failed during ${phase}`));
    } finally {
      abort.abort();
      await bounded(Promise.allSettled(readers.map((reader) => reader.cancel())), 3000).catch(() =>
        failures.push(new Error("Watch reader cleanup exceeded its bound")),
      );
      // Always revoke only this secondary login, including failed assertions. The
      // original test/browser cookie is never loaded into this integration test.
      if (cookie && !revoked) {
        try {
          await (await auth("logout")).arrayBuffer();
          revoked = true;
          await (await auth("session")).arrayBuffer();
        } catch {
          failures.push(new Error("Secondary integration login revocation could not be confirmed"));
        }
      }
      if (cookie && !revoked) failures.push(new Error("Secondary integration login cleanup incomplete"));
    }
    if (failures.length) throw new AggregateError(failures, "Actual Session Watch integration failed");
    await testInfo.attach("real-watch-queue-outcome", {
      body: JSON.stringify({
        projectId: input.projectId,
        trigger,
        resumedSnapshotVerified: true,
        typedRejectionsVerified: true,
        queuedBytes,
        elapsedMilliseconds,
        secondaryLoginRevoked: revoked,
        boundary: "in-process BFF; real Studio auth and merged gRPC/Dex",
      }),
      contentType: "application/json",
    });
  });
}
