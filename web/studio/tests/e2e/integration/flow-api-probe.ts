// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { APIRequestContext, APIResponse, PlaywrightWorkerArgs } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { isAbsolute } from "node:path";
import type { AccountSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AccountSnapshot";
import { callerFromEnvelope } from "@/lib/server/caller-envelope";
import { callRpc, type Caller } from "@/lib/server/grpc-client";
import { isRecord, type UnknownRecord } from "@/lib/shared/json";
import type { RpcInput, RpcOutput, UnaryMethod } from "@/lib/shared/rpc-methods";
import { readPrivateJSON } from "@/tests/e2e/support/private-files";

// The private fixture and the owned request contexts and logins of flow-api-authorization.spec.ts.
// Failures never carry fixture contents, cookies or response bodies.

type Credentials = { email: string; password: string };
export type FlowApiFixture = {
  projectId: string;
  releaseId: string;
  sourceCommit: string;
  owner: Credentials;
  nonOwner: Credentials;
};
export type Scope = {
  name: string;
  purpose: "runs" | "configuration";
  base: string;
  headers: Record<string, string>;
  csrf: string;
  definition: string;
  flowType: string;
};
type OwnedLogin = { context: APIRequestContext; cookie: string; caller: Caller; revoked: boolean };
export type Login = OwnedLogin & { account: AccountSnapshot__Output };

export class ProbeFailure extends Error {}

export function requireProbe(condition: unknown, message: string): asserts condition {
  if (!condition) throw new ProbeFailure(message);
}

function object(value: unknown): UnknownRecord {
  requireProbe(isRecord(value), "Expected an object in the private fixture or service response");
  return value;
}

function credentials(value: unknown): Credentials {
  const data = object(value);
  requireProbe(
    typeof data.email === "string" &&
      data.email.includes("@") &&
      typeof data.password === "string" &&
      data.password.length > 0,
    "Fixture requires existing account credentials",
  );
  return { email: data.email, password: data.password };
}

export function flowApiFixture(): FlowApiFixture {
  const filename = process.env.SUPERVERSE_E2E_FLOW_API_FIXTURE;
  requireProbe(
    filename &&
      isAbsolute(filename) &&
      process.env.SUPERVERSE_E2E_BASE_URL &&
      process.env.SUPERVERSE_GRPC_ADDRESS &&
      process.env.SUPERVERSE_STUDIO_MOCK !== "true",
    "Requires a private Flow API fixture, real Studio URL and matching merged gRPC server",
  );
  try {
    const data = object(readPrivateJSON(filename, "Flow API fixture", 16 * 1024));
    // Schema 2 names the Project alone, which addresses its one Session and Preview.
    requireProbe(
      data.schemaVersion === 2 &&
        Object.keys(data).every((key) =>
          ["schemaVersion", "projectId", "releaseId", "sourceCommit", "owner", "nonOwner"].includes(key),
        ),
      "Unsupported Flow API fixture schema",
    );
    requireProbe(
      typeof data.projectId === "string" &&
        /^[a-z0-9]{1,32}$/.test(data.projectId) &&
        typeof data.releaseId === "string" &&
        /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(data.releaseId) &&
        typeof data.sourceCommit === "string" &&
        /^[0-9a-f]{40}$/.test(data.sourceCommit),
      "Fixture must identify an existing Project, serving Release and prepared commit",
    );
    return {
      projectId: data.projectId,
      releaseId: data.releaseId,
      sourceCommit: data.sourceCommit,
      owner: credentials(data.owner),
      nonOwner: credentials(data.nonOwner),
    };
  } catch {
    throw new ProbeFailure("Invalid private Flow API fixture; contents are not included in diagnostics");
  }
}

export async function checked<T>(response: APIResponse, status: number, label: string, code?: string): Promise<T> {
  try {
    requireProbe(response.status() === status, `${label}: expected HTTP ${status}, received ${response.status()}`);
    requireProbe(response.headers()["cache-control"]?.includes("no-store"), `${label}: response must not be cached`);
    const body: T = await response.json();
    if (code) requireProbe(object(body).code === code, `${label}: unexpected safe error code`);
    return body;
  } finally {
    await response.dispose();
  }
}

function revision(value: unknown): string {
  requireProbe(
    typeof value === "string" || (typeof value === "number" && Number.isSafeInteger(value)),
    "Configuration revision is not losslessly represented",
  );
  const text = String(value);
  requireProbe(/^(0|[1-9][0-9]*)$/.test(text), "Configuration revision is invalid");
  return text;
}

/** The read that opens a scope and returns its CSRF token: the Runs catalog or the configuration's connections. */
export function firstReadPath(scope: Scope): string {
  return scope.purpose === "runs" ? "/api/v2/catalog" : "/api/v2/connector-connections";
}

export function staleDefinition(current: string): string {
  const value = `sha256:${"0".repeat(64)}`;
  return current === value ? `sha256:${"1".repeat(64)}` : value;
}

/** The scenario's own request contexts and secondary logins, and the probes it sends through them. */
export function flowApiProbe(playwright: PlaywrightWorkerArgs["playwright"], baseURL: string) {
  const origin = new URL(baseURL).origin;
  const contexts: APIRequestContext[] = [];
  const logins: OwnedLogin[] = [];
  const missingConnection = { connectorId: `authorization-probe-${randomUUID()}`, connectionName: "undeclared" };

  const newContext = async (cookie?: string) => {
    const context = await playwright.request.newContext({
      baseURL,
      timeout: 15_000,
      extraHTTPHeaders: { origin, ...(cookie ? { cookie: `sv2_session=${cookie}` } : {}) },
    });
    contexts.push(context);
    return context;
  };

  const login = async (account: Credentials): Promise<Login> => {
    const context = await newContext();
    let owned: OwnedLogin | undefined;
    const trackIssuedCookie = async () => {
      const cookie = (await context.storageState()).cookies.find((item) => item.name === "sv2_session")?.value;
      if (!cookie) return;
      const caller = callerFromEnvelope(cookie);
      requireProbe(caller, "Issued secondary login cannot be identified for revocation");
      owned = { context, cookie, caller, revoked: false };
      logins.push(owned);
    };
    try {
      const response = await context.post("/api/auth/login", { data: account });
      // Own the issued login before any response/status/body assertion can fail.
      await trackIssuedCookie();
      const body = await checked<{ account: AccountSnapshot__Output }>(response, 200, "Secondary login");
      requireProbe(owned && body.account, "Secondary login must identify an account");
      return Object.assign(owned, { account: body.account });
    } finally {
      // A transport failure may still have installed the real response cookie.
      if (!owned) await trackIssuedCookie();
    }
  };

  /** One business RPC through the real /api/rpc route of this context's login. */
  const call = async <M extends UnaryMethod>(context: APIRequestContext, method: M, input: RpcInput<M>, status = 200) =>
    checked<RpcOutput<M>>(await context.post(`/api/rpc/${method}`, { data: input }), status, method);

  const read = async <T>(
    context: APIRequestContext,
    scope: Scope,
    suffix: string,
    status = 200,
    headers = scope.headers,
  ) => checked<T>(await context.get(`${scope.base}${suffix}`, { headers }), status, scope.name);

  /** A scope after its first read: the session-bound CSRF token and exact definitions it returned. */
  const load = async (context: APIRequestContext, scope: Scope): Promise<Scope> => {
    const response = await context.get(`${scope.base}${firstReadPath(scope)}`, { headers: scope.headers });
    const csrf = response.headers()["x-superverse-csrf-token"];
    const body = await checked<Record<string, unknown>>(response, 200, scope.name);
    requireProbe(
      csrf &&
        /^[A-Za-z0-9_-]{43}$/.test(csrf) &&
        body.csrfToken === csrf &&
        typeof body.definitionRevision === "string" &&
        body.definitionRevision.length > 0,
      `${scope.name}: missing session-bound CSRF or exact definitions`,
    );
    const loaded = { ...scope, csrf, definition: body.definitionRevision };
    if (scope.purpose === "runs") {
      requireProbe(
        body.enabled === true && Array.isArray(body.flows) && body.flows.length > 0,
        `${scope.name}: requires a serving FDG catalog`,
      );
      const entry = object(body.flows[0]);
      requireProbe(
        typeof entry.flowType === "string" && entry.flowType.length > 0,
        `${scope.name}: requires a declared Flow type`,
      );
      loaded.flowType = entry.flowType;
    } else {
      loaded.headers = {
        ...scope.headers,
        "x-dex-app-manifest-revision": revision(body.appManifestRevision),
        "x-dex-configuration-revision": revision(body.configurationRevision),
      };
    }
    return loaded;
  };

  const writeHeaders = (scope: Scope, headers: Record<string, string>) => ({
    ...scope.headers,
    "x-csrf-token": scope.csrf,
    "x-dex-flow-definition-revision": scope.definition,
    ...headers,
  });

  const search = async (
    context: APIRequestContext,
    scope: Scope,
    status = 200,
    headers: Record<string, string> = {},
    extra: Record<string, unknown> = {},
    code?: string,
  ) =>
    checked(
      await context.post(`${scope.base}/api/v2/search`, {
        headers: writeHeaders(scope, headers),
        data: { flowType: scope.flowType, pageSize: 1, filters: [], workQueueOnly: false, ...extra },
      }),
      status,
      `${scope.name} search`,
      code,
    );

  /** A native-form POST for an undeclared connection, which must fail before any write. */
  const configurationProbe = async (
    context: APIRequestContext,
    scope: Scope,
    status = 404,
    headers: Record<string, string> = {},
    code?: string,
  ) =>
    checked(
      await context.post(`${scope.base}/api/v2/connector-native-sessions`, {
        headers: writeHeaders(scope, headers),
        data: missingConnection,
      }),
      status,
      `${scope.name} rejected form`,
      code,
    );

  /** Revokes every login still owned and disposes every context; returns what could not be confirmed. */
  const cleanup = async (): Promise<Error[]> => {
    const failures: Error[] = [];
    for (const session of logins)
      if (!session.revoked) {
        try {
          await callRpc("Logout", {}, session.caller, AbortSignal.timeout(10_000));
          session.revoked = true;
        } catch {
          failures.push(new Error("Secondary Flow API login revocation could not be confirmed"));
        }
      }
    const disposed = await Promise.allSettled(contexts.map((context) => context.dispose()));
    if (disposed.some((result) => result.status === "rejected"))
      failures.push(new Error("Flow API request context cleanup failed"));
    return failures;
  };

  return { newContext, login, call, read, load, search, configurationProbe, cleanup };
}
