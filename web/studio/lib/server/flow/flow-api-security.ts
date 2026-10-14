// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { createHash, createHmac, timingSafeEqual } from "node:crypto";
import type { FlowApiTarget__Output } from "@/lib/generated/protobuf/superverse/v2/FlowApiTarget";
import type { Caller } from "@/lib/server/caller-envelope";
import { httpError, parseUint64 } from "@/lib/server/request-body";
import { assertSameOrigin } from "@/lib/server/request-origin";

/** What a /dex/projects route serves: Runs and Inbox, or Configuration. */
export type FlowApiPurpose = "runs" | "configuration";

/**
 * The key for browser request integrity only: Flow API CSRF tokens and the
 * connector OAuth route cookie. Go signs login tokens with its own key; this
 * one never reaches the browser or gRPC.
 */
export function browserSigningKey(): Buffer {
  const encoded = process.env.BROWSER_SIGNING_KEY ?? "";
  const key = Buffer.from(encoded, "base64url");
  if (!/^[A-Za-z0-9_-]{43}$/.test(encoded) || key.length !== 32 || key.toString("base64url") !== encoded)
    throw httpError(503, "Browser request signing is unavailable");
  return key;
}

/**
 * The exact resource a Flow API request may touch: the Project's Preview, whose
 * Runs name the observed Sandbox instance and whose configuration names none,
 * one Live Release (and, for Runs, its observed deployment), or Live's
 * next-publish layer (`nextPublish`), checked against a Release's source or,
 * without one, the Preview's.
 */
export function flowApiTarget(
  request: Request,
  projectId: string,
  preview: boolean,
  releaseId: string,
  purpose: FlowApiPurpose,
  nextPublish = false,
): FlowApiTarget__Output {
  if (
    !/^[a-z0-9]{1,32}$/.test(projectId) ||
    (preview && (releaseId || nextPublish)) ||
    (nextPublish && purpose !== "configuration") ||
    (!preview && !(nextPublish && !releaseId) && !/^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(releaseId))
  )
    throw httpError(400, "Invalid Flow API resource selection");
  if (purpose === "configuration" && !preview && !(nextPublish && !releaseId) && !/^[0-9a-f]{40}$/.test(releaseId))
    throw httpError(400, "Configuration requires an exact commit");
  const deploymentId = request.headers.get("x-superverse-deployment-id") ?? "";
  const workspaceRequestId = request.headers.get("x-superverse-workspace-request-id") ?? "";
  const generation = parseUint64(request.headers.get("x-superverse-generation"), "Preview generation");
  // Only Preview Runs reach one Sandbox instance; the configuration belongs to a prepared source.
  const instance = preview && purpose === "runs";
  if (
    instance
      ? Boolean(deploymentId) || !workspaceRequestId || generation === "0"
      : Boolean(workspaceRequestId) || generation !== "0"
  )
    throw httpError(
      400,
      instance ? "The observed Preview instance is required" : "This target names no Preview instance",
    );
  if (purpose === "runs" && !preview && !deploymentId) throw httpError(400, "The observed Live deployment is required");
  if (purpose === "configuration" && deploymentId) throw httpError(400, "Configuration does not select a deployment");
  if ([deploymentId, workspaceRequestId].some((value) => value.length > 128 || /[^A-Za-z0-9_-]/.test(value)))
    throw httpError(400, "Invalid observed resource identity");
  return {
    projectId,
    scope: preview ? "PROJECT_SCOPE_PREVIEW" : "PROJECT_SCOPE_LIVE",
    releaseId,
    deploymentId,
    workspaceRequestId,
    generation,
    nextPublish,
  };
}

/** A CSRF token bound to the login, the purpose and the exact target. */
export function csrfToken(caller: Caller, target: FlowApiTarget__Output, purpose: FlowApiPurpose): string {
  const identity = [
    target.projectId,
    target.scope,
    target.releaseId,
    target.deploymentId,
    target.workspaceRequestId,
    target.generation,
    target.nextPublish,
  ];
  return createHmac("sha256", browserSigningKey())
    .update(
      JSON.stringify([
        "superverse-flow-api-csrf-v1",
        createHash("sha256").update(caller.sessionToken).digest("hex"),
        caller.accountId,
        caller.canonicalEmail,
        purpose,
        identity,
      ]),
    )
    .digest("base64url");
}

export function checkCSRF(request: Request, expected: string): void {
  const supplied = request.headers.get("x-csrf-token") ?? request.headers.get("x-dex-csrf-token") ?? "";
  if (!/^[A-Za-z0-9_-]{43}$/.test(supplied) || !timingSafeEqual(Buffer.from(supplied), Buffer.from(expected)))
    throw httpError(403, "Configuration or environment changed; reload before saving");
}

/** The checks every /dex/projects request passes, in mock mode too: method, path segments, origin and headers. */
export function admitFlowApiRequest(request: Request, path: string[]): void {
  if (
    !["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"].includes(request.method) ||
    !path.every((part) => Boolean(part) && part !== "." && part !== ".." && !/[\\/\u0000-\u001f\u007f%]/.test(part))
  )
    throw httpError(400, "Invalid Flow API route");
  assertSameOrigin(request);
  rejectTrustedBrowserHeaders(request);
}

/** Headers that carry server authority in Dex Web; the browser may never send them. */
export function rejectTrustedBrowserHeaders(request: Request): void {
  const forbidden = [
    "x-dex-start-worker-target",
    "x-dex-start-target-revision",
    "x-dex-actor-id",
    "x-dex-work-queue-permissions",
    "x-dex-web-csrf-token",
    "x-dex-web-embedded",
    "x-dex-configuration-commit",
    "x-dex-connector-oauth-redirect-uri",
  ];
  if (forbidden.some((name) => request.headers.has(name)))
    throw httpError(400, "Server authority cannot be supplied by the browser");
}
