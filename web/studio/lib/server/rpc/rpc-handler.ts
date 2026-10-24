// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ErrorInfo__Output } from "@/lib/generated/protobuf/google/rpc/ErrorInfo";
import { callerFromRequest } from "@/lib/server/caller-envelope";
import { eventStream } from "@/lib/server/event-stream";
import { errorResponse, rpcErrorInfo, rpcMethods, type RpcBackend, type RpcMethod } from "@/lib/server/grpc-client";
import { httpError, readJSONObject } from "@/lib/server/request-body";
import { assertSameOrigin, requireSurface } from "@/lib/server/request-origin";
import {
  connectionRefusalInfo,
  deploymentRefusalInfo,
  operationRefusalInfo,
  releaseRefusalInfo,
  withDeclaredRemedy,
} from "@/lib/shared/problem/refusals";
import { isRevisionChanged, REVISION_CHANGED } from "@/lib/shared/revision-conflict";
import type { BrowserService, RpcInput, UnaryMethod, WatchMethod } from "@/lib/shared/rpc-methods";
import {
  AGENT_COMMAND_NOT_ADMITTED,
  AGENT_COMMAND_NOT_DISPATCHED,
  commandErrorInfo,
  isSessionExecutionStopped,
} from "@/lib/shared/session-command-error";
import {
  decodeRequestBody,
  decodeWatchQuery,
  hasCallerField,
  requestSchema,
  unsupportedRequestField,
  type RequestSchema,
} from "./request-decoder";

/**
 * The services /api/rpc/<Method> serves. AccountService is reached only through /api/auth and
 * the Flow API services only through /dex/...; a new service stays unreachable until it joins
 * this list. It must name exactly the BrowserService type the browser client compiles against,
 * so a service missing from either side fails to compile instead of answering 404.
 */
const BROWSER_SERVICES = {
  ProjectService: true,
  SessionService: true,
  AgentService: true,
  ModelCredentialService: true,
  RemoteAgentService: true,
  PreviewService: true,
  ReleaseService: true,
  DeploymentService: true,
  TeamSpaceService: true,
  TeamSpaceMemberService: true,
  ProjectMemberService: true,
  TeamSpaceConnectionService: true,
  AttachmentService: true,
  EvolveService: true,
} satisfies Record<BrowserService, true>;

/**
 * Methods of browser services that only their own routes call, never /api/rpc: the start of a
 * connection's OAuth sets the route cookie that its callback, the only caller of the completion,
 * reads (lib/server/connector-oauth).
 */
const OWN_ROUTE_METHODS: ReadonlySet<string> = new Set<UnaryMethod>([
  "StartTeamSpaceConnectionOAuth",
  "CompleteTeamSpaceConnectionOAuth",
]);

const METHOD_NAME = /^[A-Z][A-Za-z]{2,63}$/;

interface BrowserRpc {
  method: string;
  entry: RpcMethod;
  requestType: string;
  watch: boolean;
}

let browserProtocol:
  | { rpcs: ReadonlyMap<string, BrowserRpc>; ownRoutes: ReadonlyMap<string, BrowserRpc>; schema: RequestSchema }
  | undefined;

// Asserted once at load: a request shape this decoder was not written for refuses every call
// with 503 instead of being served.
function loadedBrowserProtocol() {
  if (browserProtocol) return browserProtocol;
  const methods = rpcMethods();
  // Every definition carries the file descriptors of the whole loaded protocol.
  const [first] = methods.values();
  const schema = requestSchema(first.definition.requestType.fileDescriptorProtos);
  const rpcs = new Map<string, BrowserRpc>();
  const ownRoutes = new Map<string, BrowserRpc>();
  for (const [method, entry] of methods) {
    if (!Object.hasOwn(BROWSER_SERVICES, entry.service)) continue;
    const requestType = `superverse.v2.${(entry.definition.requestType.type as { name: string }).name}`;
    const watch = entry.definition.responseStream;
    if (
      entry.definition.requestStream ||
      watch !== method.startsWith("Watch") ||
      !hasCallerField(schema, requestType) ||
      unsupportedRequestField(schema, requestType)
    )
      throw httpError(503, "Platform browser protocol is unsupported");
    (OWN_ROUTE_METHODS.has(method) ? ownRoutes : rpcs).set(method, { method, entry, requestType, watch });
  }
  browserProtocol = { rpcs, ownRoutes, schema };
  return browserProtocol;
}

/**
 * The ErrorInfo a failed call may show the browser, with only the remedy the server declared in
 * its metadata and the deployment a refused build cancellation names; nothing else crosses. An
 * Agent command that never reached the backend, or that the Agent refused, definitely did not run.
 * Any Session operation may report that the Session's engine execution stopped, any write that its
 * revision fence no longer held, any Preview or configuration operation its refusal, a connection
 * request its refusal, a Release build its admission or cancellation refusal (RELEASE_BUILD_DEPLOYING
 * with the deployment it names) and a deployment its cancellation refusal.
 */
function crossingErrorInfo(rpc: BrowserRpc, dispatched: boolean, error: unknown): ErrorInfo__Output | undefined {
  if (!dispatched) return rpc.entry.service === "AgentService" ? AGENT_COMMAND_NOT_DISPATCHED : undefined;
  const original = rpcErrorInfo(error);
  const info = allowedErrorInfo(rpc, original);
  return info && withDeclaredRemedy(info, original);
}

/** The ErrorInfo of a dispatched call that may cross, without its metadata. */
function allowedErrorInfo(rpc: BrowserRpc, original: unknown): ErrorInfo__Output | undefined {
  if (isRevisionChanged(original)) return REVISION_CHANGED;
  const refused = operationRefusalInfo(original);
  if (refused) return refused;
  const info = commandErrorInfo(original);
  // Any Session operation, an Agent command included, may find the engine execution stopped.
  if (isSessionExecutionStopped(info)) return info;
  if (rpc.entry.service === "AgentService")
    return info?.reason === AGENT_COMMAND_NOT_ADMITTED.reason ? info : undefined;
  if (rpc.entry.service === "TeamSpaceConnectionService") return connectionRefusalInfo(original);
  if (rpc.entry.service === "ReleaseService") return releaseRefusalInfo(original);
  if (rpc.entry.service === "DeploymentService") return deploymentRefusalInfo(original);
}

/**
 * Decodes the request of a method that only its own route calls, as strictly as /api/rpc decodes
 * the others.
 */
export function decodeOwnRouteRequest(method: UnaryMethod, body: unknown): Record<string, unknown> {
  const { ownRoutes, schema } = loadedBrowserProtocol();
  const rpc = ownRoutes.get(method);
  if (!rpc) throw httpError(503, "Platform browser protocol is unsupported");
  return decodeRequestBody(schema, rpc.requestType, body);
}

/**
 * /api/rpc/<Method>: one generated RPC of a browser service, by its exact name. The checks run in
 * order: method (404), verb (405), same origin for POST (403), the caller cookie before the body
 * is read (401) and strict decoding (400). The BFF then sets only the caller; it supplies no ID,
 * fence or default.
 */
export async function handleRpcRequest(request: Request, method: string, backend: RpcBackend): Promise<Response> {
  let rpc: BrowserRpc | undefined;
  let dispatched = false;
  try {
    requireSurface(request, "studio");
    rpc = METHOD_NAME.test(method) ? loadedBrowserProtocol().rpcs.get(method) : undefined;
    if (!rpc) throw httpError(404, "Unknown business RPC");
    if (request.method !== (rpc.watch ? "GET" : "POST")) throw httpError(405, "Method not allowed");
    assertSameOrigin(request);
    const caller = callerFromRequest(request);
    const { schema } = loadedBrowserProtocol();
    const input = rpc.watch
      ? decodeWatchQuery(
          schema,
          rpc.requestType,
          new URL(request.url).searchParams,
          request.headers.get("last-event-id"),
        )
      : decodeRequestBody(schema, rpc.requestType, await readJSONObject(request));
    dispatched = true;
    if (rpc.watch) {
      const watched = input as RpcInput<WatchMethod>;
      return eventStream(request, (signal, upstreamFailed) =>
        backend.watch(method as WatchMethod, watched, caller, signal, upstreamFailed),
      );
    }
    const output = await backend.unary(method as UnaryMethod, input as RpcInput<UnaryMethod>, caller, request.signal);
    return Response.json(output, { headers: { "cache-control": "no-store" } });
  } catch (error) {
    const response = errorResponse(error, rpc && crossingErrorInfo(rpc, dispatched, error));
    if (rpc && response.status === 405) response.headers.set("allow", rpc.watch ? "GET" : "POST");
    return response;
  }
}
