// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test } from "@/tests/e2e/support/coverage/spec-coverage";
import { randomUUID } from "node:crypto";
import { callRpc } from "@/lib/server/grpc-client";
import {
  checked,
  firstReadPath,
  flowApiFixture,
  flowApiProbe,
  ProbeFailure,
  requireProbe,
  staleDefinition,
  type Scope,
} from "./flow-api-probe";

// The Preview commands, each fenced on the Sandbox instance and Preview revision it observed.
const PREVIEW_COMMANDS = ["StartPreview", "StopPreview", "PreparePreviewSource"] as const;

// Actual HTTP BFF, generated gRPC, Account/Project/Session Flows and scoped Dex
// resources only. This scenario creates/revokes secondary logins, performs reads,
// and rejects native-form requests for undeclared connections. It never starts a
// Flow, creates a Project, changes a deployment or writes configuration/secrets.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("Flow API binds exact targets and rejects revoked sessions immediately", async ({ playwright }, testInfo) => {
  test.setTimeout(4 * 60_000);
  const input = flowApiFixture();
  const probe = flowApiProbe(playwright, process.env.SUPERVERSE_E2E_BASE_URL!);
  const { newContext, login, call, read, load, search, configurationProbe } = probe;
  const failures: Error[] = [];
  let phase = "secondary owner login";
  let evidence: Record<string, unknown> = {};
  try {
    const first = await login(input.owner);
    phase = "fresh resource observations";
    const ids = { projectId: input.projectId };
    const { project } = await call(first.context, "GetProject", { projectId: input.projectId });
    const { session } = await call(first.context, "GetSession", ids);
    requireProbe(
      project?.projectId === input.projectId &&
        project.callerRole === "PROJECT_ROLE_OWNER" &&
        project.status === "PROJECT_STATUS_READY",
      "Fixture login must be an owner of the READY Project",
    );
    requireProbe(session, "Fixture Session must be readable");
    const live = project.live,
      workspace = session.workspace,
      preview = session.preview;
    requireProbe(
      live &&
        !live.isRemoving &&
        live.releaseId === input.releaseId &&
        live.deploymentId &&
        session.projectId === input.projectId &&
        session.status === "SESSION_STATUS_ACTIVE" &&
        workspace?.status === "WORKSPACE_STATUS_READY" &&
        preview?.status === "PREVIEW_STATUS_READY" &&
        preview.preparedSource?.status === "PREVIEW_SOURCE_STATUS_READY" &&
        preview.workspaceRequestId === workspace.workspaceRequestId &&
        preview.generation === workspace.generation &&
        workspace.workspaceRequestId &&
        /^[1-9][0-9]*$/.test(workspace.generation),
      "Fixture requires current READY Live and Preview with exact observed workspace identity",
    );
    requireProbe(
      Number(workspace.expiresAt?.seconds) * 1000 > Date.now() + 5 * 60_000,
      "The Preview workspace needs at least five minutes remaining; this scenario never initializes or renews it",
    );
    phase = "Preview command instance admission through BFF and direct gRPC";
    const observed = {
      expectedRevision: preview.revision,
      generation: preview.generation,
      workspaceRequestId: preview.workspaceRequestId,
    };
    for (const rpc of PREVIEW_COMMANDS) {
      const command = { ...ids, requestId: randomUUID() };
      // Go rejects an incomplete observation; the BFF forwards the request as sent. A command
      // without an instance observed a Preview that never acquired one, which Go accepts and
      // this Preview, which acquired one, rejects as stale.
      for (const omitted of ["expectedRevision", "generation", "workspaceRequestId"] as const) {
        const partial: Record<string, string> = { ...observed };
        delete partial[omitted];
        const status = omitted === "workspaceRequestId" ? 409 : 400;
        await call(first.context, rpc, { ...command, ...partial }, status);
      }
      const stale = { ...command, ...observed, workspaceRequestId: `ws-rejected-${randomUUID()}` };
      await call(first.context, rpc, stale, 409);
      let rejected = false;
      try {
        await callRpc(rpc, stale, first.caller, AbortSignal.timeout(10_000));
      } catch (error) {
        requireProbe(
          (error as { status?: number }).status === 409,
          `${rpc} must reject stale identity in Go admission`,
        );
        rejected = true;
      }
      requireProbe(rejected, `${rpc} accepted a different instance through direct gRPC`);
    }
    const { preview: unchanged } = await call(first.context, "GetPreview", ids);
    requireProbe(
      unchanged?.workspaceRequestId === preview.workspaceRequestId &&
        unchanged.generation === preview.generation &&
        unchanged.revision === preview.revision &&
        unchanged.status === preview.status &&
        JSON.stringify(unchanged.preparedSource) === JSON.stringify(preview.preparedSource),
      "Rejected Preview commands must preserve instance, revision and prepared source",
    );
    const prepared = await call(first.context, "GetRelease", {
      projectId: input.projectId,
      releaseId: input.sourceCommit,
    });
    requireProbe(
      prepared.release?.sourceCommit === input.sourceCommit &&
        prepared.release.preparedSource?.status === "RELEASE_STATUS_READY",
      "Commit configuration must already be prepared",
    );
    const { configuration } = await call(first.context, "GetProjectConfiguration", {
      target: { projectId: input.projectId, scope: "PROJECT_SCOPE_LIVE", releaseId: input.sourceCommit },
    });
    requireProbe(
      configuration?.releaseId === input.sourceCommit && configuration?.sourceCommit === input.sourceCommit,
      "Live configuration must be bound to the selected commit",
    );
    const previewHeaders = {
      "x-superverse-workspace-request-id": workspace.workspaceRequestId,
      "x-superverse-generation": workspace.generation,
    };
    const unloaded = { csrf: "", definition: "", flowType: "" };
    const initial: Scope[] = [
      {
        name: "Live Runs",
        purpose: "runs",
        base: `/dex/projects/${input.projectId}/environments/live/releases/${input.releaseId}`,
        headers: { "x-superverse-deployment-id": live.deploymentId },
        ...unloaded,
      },
      {
        name: "Preview Runs",
        purpose: "runs",
        base: `/dex/projects/${input.projectId}/environments/preview`,
        headers: previewHeaders,
        ...unloaded,
      },
      {
        // Live's next-publish layer, checked against the Release's source; a Release's copy is read only.
        name: "Live configuration",
        purpose: "configuration",
        base: `/dex/projects/${input.projectId}/configuration/live/next/releases/${input.sourceCommit}`,
        headers: {},
        ...unloaded,
      },
      {
        name: "Preview configuration",
        purpose: "configuration",
        base: `/dex/projects/${input.projectId}/configuration/preview`,
        headers: {},
        ...unloaded,
      },
    ];
    const scopes: Scope[] = [];
    for (const scope of initial) scopes.push(await load(first.context, scope));
    const [liveRuns, previewRuns, liveConfig, previewConfig] = scopes;
    phase = "actual Flow API reads and supported POST admission";
    for (const scope of scopes) {
      if (scope.purpose === "runs") {
        await read(first.context, scope, "/api/flow-definitions");
        await search(first.context, scope);
      } else {
        await read(first.context, scope, "/api/v2/application-environment");
        await configurationProbe(first.context, scope, 404, {}, "CONNECTOR_CONNECTION_UNSUPPORTED");
      }
    }
    phase = "missing, stale and cross-environment target fences";
    await read(first.context, liveRuns, "/api/v2/catalog", 400, {});
    await read(first.context, liveRuns, "/api/v2/catalog", 409, { "x-superverse-deployment-id": randomUUID() });
    await read(
      first.context,
      { ...liveRuns, base: `/dex/projects/${input.projectId}/environments/live/releases/not-current` },
      "/api/v2/catalog",
      409,
    );
    await read(first.context, liveRuns, "/api/v2/catalog", 400, { ...liveRuns.headers, ...previewHeaders });
    await read(first.context, previewRuns, firstReadPath(previewRuns), 400, {});
    await read(first.context, previewRuns, firstReadPath(previewRuns), 409, {
      ...previewRuns.headers,
      "x-superverse-workspace-request-id": `probe-${randomUUID()}`,
    });
    await read(first.context, previewRuns, firstReadPath(previewRuns), 409, {
      ...previewRuns.headers,
      "x-superverse-generation": workspace.generation === "1" ? "2" : "1",
    });
    await read(first.context, previewRuns, firstReadPath(previewRuns), 400, {
      ...previewRuns.headers,
      "x-superverse-deployment-id": live.deploymentId,
    });
    // The Preview configuration belongs to the prepared source and names no Sandbox instance.
    await read(first.context, previewConfig, firstReadPath(previewConfig), 400, previewHeaders);
    await read(first.context, previewConfig, firstReadPath(previewConfig), 400, {
      "x-superverse-deployment-id": live.deploymentId,
    });
    await read(first.context, liveConfig, "/api/v2/connector-connections", 400, liveRuns.headers);
    await read(
      first.context,
      { ...liveConfig, base: `/dex/projects/${input.projectId}/configuration/live/next/releases/not-a-commit` },
      "/api/v2/connector-connections",
      400,
    );
    // The legacy route returns navigation guidance before Flow API dispatch.
    const legacy = await first.context.get(
      `/dex/projects/${input.projectId}/configuration/live/api/v2/connector-connections`,
    );
    try {
      requireProbe(legacy.status() === 409, "Live configuration must require explicit commit selection");
    } finally {
      await legacy.dispose();
    }
    for (const scope of [liveRuns, previewRuns])
      await read(first.context, scope, "/api/v2/application-environment", 404);
    for (const scope of [liveConfig, previewConfig]) await read(first.context, scope, "/api/v2/catalog", 404);
    phase = "CSRF, source revision and forged authority rejection";
    for (const scope of [liveRuns, previewRuns]) {
      await search(first.context, scope, 403, { "x-csrf-token": "" });
      await search(first.context, scope, 403, { "x-csrf-token": "invalid" });
      await search(first.context, scope, 403, { origin: "https://untrusted.invalid" });
      await search(first.context, scope, 403, { "sec-fetch-site": "cross-site" });
      await search(first.context, scope, 403, { "x-csrf-token": liveConfig.csrf });
      await search(
        first.context,
        scope,
        409,
        { "x-dex-flow-definition-revision": staleDefinition(scope.definition) },
        {},
        "FLOW_DEFINITION_CHANGED",
      );
      for (const field of ["workerTargetAddress", "bypassWorkerHealthCheck", "permissions"])
        await search(
          first.context,
          scope,
          400,
          {},
          { [field]: field === "bypassWorkerHealthCheck" ? true : "untrusted" },
        );
    }
    await search(first.context, previewRuns, 403, { "x-csrf-token": liveRuns.csrf });
    for (const scope of [liveConfig, previewConfig]) {
      await configurationProbe(first.context, scope, 403, { "x-csrf-token": "" });
      await configurationProbe(first.context, scope, 403, { "x-csrf-token": liveRuns.csrf });
      await configurationProbe(
        first.context,
        scope,
        409,
        { "x-dex-flow-definition-revision": staleDefinition(scope.definition) },
        "FLOW_DEFINITION_REVISION_CONFLICT",
      );
    }
    const differentCommit = `${input.sourceCommit[0] === "0" ? "1" : "0"}${input.sourceCommit.slice(1)}`;
    // This rejection is intentionally at BFF CSRF admission, before any source
    // read: the fixture's token must not authorize a different commit selector.
    await configurationProbe(
      first.context,
      { ...liveConfig, base: `/dex/projects/${input.projectId}/configuration/live/next/releases/${differentCommit}` },
      403,
    );
    await read(first.context, liveRuns, "/api/v2/catalog", 400, { ...liveRuns.headers, "x-dex-actor-id": "" });
    for (const header of [
      "x-dex-start-worker-target",
      "x-dex-start-target-revision",
      "x-dex-actor-id",
      "x-dex-work-queue-permissions",
      "x-dex-web-csrf-token",
      "x-dex-web-embedded",
      "x-dex-configuration-commit",
      "x-dex-connector-oauth-redirect-uri",
    ]) {
      await read(first.context, liveRuns, "/api/v2/catalog", 400, { ...liveRuns.headers, [header]: "untrusted" });
      await read(first.context, liveConfig, "/api/v2/connector-connections", 400, { [header]: "untrusted" });
    }
    phase = "different principals and forged session identity";
    const anonymous = await newContext();
    const forged = `sv2.${Buffer.from(JSON.stringify({ ...first.caller, accountId: `untrusted-${randomUUID()}` })).toString("base64url")}`;
    const forgedContext = await newContext(forged);
    const other = await login(input.nonOwner);
    // Outside the Project's Teamspace, or without a role in the Project, the reads below are refused.
    requireProbe(
      other.account.accountId !== first.account.accountId && other.account.isAdmin === false,
      "Non-owner fixture must be a different ordinary ACTIVE account without a role in the Project",
    );
    for (const scope of scopes) {
      await read(anonymous, scope, firstReadPath(scope), 401);
      await read(forgedContext, scope, firstReadPath(scope), 401);
      await read(other.context, scope, firstReadPath(scope), 403);
      await read(first.context, scope, firstReadPath(scope)); // Repeat reads before server-side revocation.
    }
    phase = "immediate revocation through actual gRPC";
    await callRpc("Logout", {}, first.caller, AbortSignal.timeout(10_000));
    first.revoked = true;
    // The very next supported request is required to fail; no poll or TTL grace.
    for (const scope of scopes) {
      await read(first.context, scope, firstReadPath(scope), 401);
      if (scope.purpose === "runs") await search(first.context, scope, 401);
      else await configurationProbe(first.context, scope, 401);
    }
    phase = "new login token binding and real HTTP logout";
    const second = await login(input.owner);
    const renewed: Scope[] = [];
    for (const scope of initial) renewed.push(await load(second.context, scope));
    requireProbe(renewed[0].csrf !== liveRuns.csrf, "A new login must issue a different session-bound CSRF token");
    await search(second.context, renewed[0], 403, { "x-csrf-token": liveRuns.csrf });
    await search(second.context, renewed[0]);
    await checked(await second.context.post("/api/auth/logout", { data: {} }), 200, "HTTP logout");
    second.revoked = true;
    const replay = await newContext(second.cookie);
    for (const scope of renewed) await read(replay, scope, firstReadPath(scope), 401);
    await search(replay, renewed[0], 401);
    evidence = {
      projectId: input.projectId,
      releaseId: input.releaseId,
      configurationCommit: input.sourceCommit,
      deploymentId: live.deploymentId,
      buildRevisionId: live.buildRevisionId,
      workspaceRequestId: workspace.workspaceRequestId,
      generation: workspace.generation,
      scopes: scopes.map((scope) => ({ name: scope.name, definitionRevision: scope.definition })),
      immediateRevocation: true,
      realHTTPLogoutReplayRejected: true,
      resourceMutations: false,
      boundary: "real HTTP BFF; named generated gRPC; fresh account/resource admission; scoped Dex/configuration",
    };
  } catch (error) {
    failures.push(
      new Error(
        error instanceof ProbeFailure
          ? `${phase}: ${error.message}`
          : `Flow API authorization failed during ${phase}; transport details omitted`,
      ),
    );
  } finally {
    failures.push(...(await probe.cleanup()));
  }
  if (failures.length) throw new AggregateError(failures, "Real Flow API authorization failed");
  await testInfo.attach("flow-api-authorization", { body: JSON.stringify(evidence), contentType: "application/json" });
});
