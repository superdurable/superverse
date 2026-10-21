// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { Client, credentials, Metadata, status } from "@grpc/grpc-js";
import type { ServiceError } from "@grpc/grpc-js";
import { loadSync } from "@grpc/proto-loader";
import type { MessageTypeDefinition, MethodDefinition, PackageDefinition, ServiceDefinition } from "@grpc/proto-loader";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import type { ErrorInfo__Output } from "@/lib/generated/protobuf/google/rpc/ErrorInfo";
import { isAbortError, isTimeoutError } from "@/lib/shared/async";
import type { Method, RpcInput, RpcOutput, Service } from "@/lib/shared/rpc-methods";
import type { Caller } from "./caller-envelope";
import { httpError } from "./request-body";

export type { Caller };

/** One RPC of the loaded business protocol: its service and generated codecs. */
export interface RpcMethod {
  service: Service;
  definition: MethodDefinition<object, object>;
}

/**
 * Where a business call goes: the merged Go server (grpcBackend) or the Studio mock. The /api/rpc
 * allowlist, decoder, caller check, ErrorInfo table and SSE relay run the same in front of both.
 */
export interface RpcBackend {
  unary<M extends Method>(
    method: M,
    input: RpcInput<M>,
    caller: Caller | null,
    signal?: AbortSignal,
  ): Promise<RpcOutput<M>>;
  /**
   * Server-streamed Watch frames; aborting the signal ends the stream. A backend whose upstream
   * can fail while frames wait unread calls `upstreamFailed` at once, so the relay discards them.
   */
  watch<M extends Method>(
    method: M,
    input: RpcInput<M>,
    caller: Caller,
    signal: AbortSignal,
    upstreamFailed: () => void,
  ): AsyncIterable<RpcOutput<M>>;
}

/** The only RPCs without a caller; only /api/auth calls them. */
export const UNAUTHENTICATED_METHODS: ReadonlySet<Method> = new Set<Method>(["RegisterAccount", "Login"]);

// The browser JSON contract: lowerCamel names, 64-bit integers as decimal strings, enum value
// names, defaults filled in and each oneof named by its own key.
const LOAD_OPTIONS = { keepCase: false, longs: String, enums: String, defaults: true, oneofs: true };
const PACKAGE = "superverse.v2.";

/** api/superverse/v2/superverse.proto, the same file the generated types come from. */
function superverseProtoPath(): string {
  const candidates = [
    process.env.SUPERVERSE_PROTO_PATH,
    path.resolve(process.cwd(), "../../api/superverse/v2/superverse.proto"),
    path.resolve(process.cwd(), "api/superverse/v2/superverse.proto"),
  ];
  const proto = candidates.find((candidate) => candidate && existsSync(candidate));
  if (!proto) throw httpError(503, "Platform protobuf contract is unavailable");
  return proto;
}

function errorProtoDirectory(): string {
  const directory = [
    path.resolve(process.cwd(), "proto/google/rpc"),
    path.resolve(process.cwd(), "web/studio/proto/google/rpc"),
  ].find((candidate) => existsSync(path.join(candidate, "error_details.proto")));
  if (!directory) throw httpError(503, "RPC error descriptors are unavailable");
  return directory;
}

function messageDescriptor(definition: PackageDefinition, name: string): MessageTypeDefinition<object, object> {
  const value = definition[name];
  if (!value || !("format" in value) || value.format !== "Protocol Buffer 3 DescriptorProto")
    throw httpError(503, "RPC error descriptors are unavailable");
  return value;
}

let protocol:
  | {
      methods: ReadonlyMap<string, RpcMethod>;
      status: MessageTypeDefinition<object, object>;
      errorInfo: MessageTypeDefinition<object, object>;
    }
  | undefined;

// Loaded once per process; a missing contract or a repeated RPC name keeps every call at 503.
function loadedProtocol() {
  if (protocol) return protocol;
  const errorProtos = errorProtoDirectory();
  const definition = loadSync(
    [superverseProtoPath(), path.join(errorProtos, "status.proto"), path.join(errorProtos, "error_details.proto")],
    LOAD_OPTIONS,
  );
  const methods = new Map<string, RpcMethod>();
  for (const [name, value] of Object.entries(definition)) {
    if (!name.startsWith(PACKAGE) || "format" in value) continue;
    for (const [method, methodDefinition] of Object.entries(value as ServiceDefinition)) {
      if (methods.has(method)) throw httpError(503, "Platform RPC names are not unique");
      methods.set(method, { service: name.slice(PACKAGE.length) as Service, definition: methodDefinition });
    }
  }
  protocol = {
    methods,
    status: messageDescriptor(definition, "google.rpc.Status"),
    errorInfo: messageDescriptor(definition, "google.rpc.ErrorInfo"),
  };
  return protocol;
}

/** Every RPC of the 11 business services, by its unique method name. */
export function rpcMethods(): ReadonlyMap<string, RpcMethod> {
  return loadedProtocol().methods;
}

/** The loaded definition of a generated method name; the contract and the generated types match. */
export function rpcMethod(method: Method): RpcMethod {
  const entry = rpcMethods().get(method);
  if (!entry) throw new Error(`The loaded protocol has no ${method} RPC`);
  return entry;
}

let client: Client | undefined;

function grpcClient(): Client {
  if (client) return client;
  const address = process.env.SUPERVERSE_GRPC_ADDRESS;
  if (!address || !/^[a-zA-Z0-9.\-]+:[0-9]+$/.test(address)) throw httpError(503, "Platform gRPC is not configured");
  const certificate = process.env.SUPERVERSE_GRPC_CA_FILE;
  const transport =
    process.env.SUPERVERSE_GRPC_TLS === "true"
      ? credentials.createSsl(certificate ? readFileSync(certificate) : undefined)
      : credentials.createInsecure();
  client = new Client(address, transport, {
    "grpc.max_receive_message_length": 8 * 1024 * 1024,
    "grpc.max_send_message_length": 2 * 1024 * 1024,
  });
  return client;
}

/** The request as sent: the BFF sets only the top-level caller, and only from the login cookie. */
function requestPayload(method: Method, input: object, caller: Caller | null): object {
  if (UNAUTHENTICATED_METHODS.has(method)) return input;
  if (!caller) throw httpError(401, "Sign in to continue");
  const { accountId, canonicalEmail, sessionToken } = caller;
  return { ...input, caller: { accountId, canonicalEmail, sessionToken } };
}

/** One unary RPC of the merged Go server, by method name. */
export async function callRpc<M extends Method>(
  method: M,
  input: RpcInput<M>,
  caller: Caller | null,
  signal?: AbortSignal,
): Promise<RpcOutput<M>> {
  if (signal?.aborted) throw httpError(499, "Request canceled");
  const { definition } = rpcMethod(method);
  const payload = requestPayload(method, input, caller);
  return new Promise((resolve, reject) => {
    const cancel = () => call.cancel();
    const call = grpcClient().makeUnaryRequest(
      definition.path,
      definition.requestSerialize,
      definition.responseDeserialize,
      payload,
      new Metadata(),
      { deadline: Date.now() + 35_000 },
      (error, response) => {
        signal?.removeEventListener("abort", cancel);
        if (error) {
          logGrpcFailure(method, error, signal?.aborted);
          reject(rpcError(error, method));
        } else if (response === undefined) reject(new Error("Platform RPC returned no response"));
        else resolve(response as RpcOutput<M>);
      },
    );
    signal?.addEventListener("abort", cancel, { once: true });
  });
}

/**
 * One server-streamed Watch of the merged Go server; aborting the signal cancels the call once.
 * grpc-js emits a failed status as an error at once, while the iterator throws it only after the
 * frames already received, so the error listener reports the failure straight away.
 */
async function* watchRpc<M extends Method>(
  method: M,
  input: RpcInput<M>,
  caller: Caller,
  signal: AbortSignal,
  upstreamFailed: () => void,
): AsyncGenerator<RpcOutput<M>> {
  if (signal.aborted) return;
  const { definition } = rpcMethod(method);
  const call = grpcClient().makeServerStreamRequest(
    definition.path,
    definition.requestSerialize,
    definition.responseDeserialize,
    requestPayload(method, input, caller),
    new Metadata(),
  );
  let logged = false;
  const reportFailure = (error: unknown) => {
    if (!logged) logGrpcFailure(method, error, signal.aborted);
    logged = true;
    upstreamFailed();
  };
  call.on("error", reportFailure);
  let ended = false;
  const cancel = () => {
    if (ended) return;
    ended = true;
    call.cancel();
  };
  signal.addEventListener("abort", cancel, { once: true });
  try {
    for await (const frame of call) yield frame as RpcOutput<M>;
    ended = true;
  } catch (error) {
    if (!logged) logGrpcFailure(method, error, signal.aborted);
    throw isServiceError(error) ? rpcError(error) : error;
  } finally {
    signal.removeEventListener("abort", cancel);
    cancel();
  }
}

export const grpcBackend: RpcBackend = { unary: callRpc, watch: watchRpc };

const HTTP_STATUS: Record<number, number> = {
  [status.CANCELLED]: 499,
  [status.INVALID_ARGUMENT]: 400,
  [status.UNAUTHENTICATED]: 401,
  [status.PERMISSION_DENIED]: 403,
  [status.NOT_FOUND]: 404,
  [status.ALREADY_EXISTS]: 409,
  [status.ABORTED]: 409,
  [status.FAILED_PRECONDITION]: 409,
  [status.RESOURCE_EXHAUSTED]: 429,
  // A server that predates or postdates this Studio's protocol, as during a rolling upgrade.
  [status.UNIMPLEMENTED]: 503,
  [status.DEADLINE_EXCEEDED]: 504,
  [status.UNAVAILABLE]: 503,
};

/** The message the browser may see for an HTTP status; server-side detail never crosses for 401 or 5xx. */
function browserMessage(httpStatus: number, detail: string): string {
  return httpStatus === 401
    ? "Sign in to continue"
    : httpStatus === 503
      ? "Platform service is unavailable"
      : httpStatus >= 500
        ? "Platform request failed"
        : detail;
}

function isServiceError(error: unknown): error is ServiceError {
  return error instanceof Error && "code" in error && "metadata" in error && error.metadata instanceof Metadata;
}

/** Keep transport failures visible without logging request, response or metadata. */
function logGrpcFailure(method: Method, error: unknown, aborted = false): void {
  if (!isServiceError(error) || error.code === status.OK || error.code === status.CANCELLED || aborted) return;
  const code = status[error.code] ?? String(error.code);
  const line = `Studio gRPC call failed method=${method} code=${code}`;
  if (error.code === status.INTERNAL || error.code === status.UNKNOWN || error.code === status.DATA_LOSS)
    console.error(line);
  else console.warn(line);
}

type RpcError = Error & { status: number; rpcErrorInfo?: unknown; retryAfterSeconds?: number };

// A GetRelease read has no durable effects. Its transport failure may be
// retried, unlike a command whose deadline leaves acceptance uncertain.
const RELEASE_READ_RETRY_SECONDS = 1;

/**
 * A failed RPC as the BFF sees it: its HTTP status, a browser-safe message and
 * the status's one google.rpc.ErrorInfo detail, if it carried one. The detail
 * is unchecked; the /api/rpc ErrorInfo table decides whether it may cross. The
 * Studio mock raises its typed rejections the same way.
 */
export function rpcFailure(httpStatus: number, message: string, errorInfo?: unknown): RpcError {
  return Object.assign(httpError(httpStatus, message), { rpcErrorInfo: errorInfo });
}

function rpcError(error: ServiceError, method?: Method): RpcError {
  const retryableReleaseRead =
    method === "GetRelease" && (error.code === status.DEADLINE_EXCEEDED || error.code === status.UNAVAILABLE);
  // The BFF's own 35-second deadline can win before the Go server returns its
  // read status. Preserve the same unavailable-read meaning in that case.
  const httpStatus = retryableReleaseRead ? 503 : (HTTP_STATUS[error.code] ?? 500);
  const failure = rpcFailure(httpStatus, browserMessage(httpStatus, error.details), decodedErrorInfo(error));
  if (retryableReleaseRead) failure.retryAfterSeconds = RELEASE_READ_RETRY_SECONDS;
  return failure;
}

/** The unchecked ErrorInfo detail of a failed RPC, if the failure was one. */
export function rpcErrorInfo(error: unknown): unknown {
  return error instanceof Error && "rpcErrorInfo" in error ? (error as RpcError).rpcErrorInfo : undefined;
}

// Anything but exactly one bounded ErrorInfo detail, or anything malformed, is no detail.
function decodedErrorInfo(error: ServiceError): unknown {
  const values = error.metadata.get("grpc-status-details-bin");
  if (values.length !== 1 || !Buffer.isBuffer(values[0]) || values[0].length > 8192) return;
  try {
    const decoded = loadedProtocol().status.deserialize(values[0]);
    if (
      !("code" in decoded) ||
      decoded.code !== error.code ||
      !("details" in decoded) ||
      !Array.isArray(decoded.details) ||
      decoded.details.length !== 1
    )
      return;
    const detail: unknown = decoded.details[0];
    if (
      !detail ||
      typeof detail !== "object" ||
      !("type_url" in detail) ||
      detail.type_url !== "type.googleapis.com/google.rpc.ErrorInfo" ||
      !("value" in detail) ||
      !Buffer.isBuffer(detail.value) ||
      detail.value.length > 4096
    )
      return;
    return loadedProtocol().errorInfo.deserialize(detail.value);
  } catch {
    return;
  }
}

/** The only error body the BFF writes; ErrorInfo crosses only when the route passes it. */
export function errorResponse(error: unknown, errorInfo?: ErrorInfo__Output): Response {
  const failure = isAbortError(error)
    ? { status: 499, message: "Request canceled" }
    : isTimeoutError(error)
      ? { status: 504, message: "Platform request timed out" }
      : error instanceof Error && "status" in error && typeof error.status === "number"
        ? { status: error.status, message: browserMessage(error.status, error.message) }
        : { status: 500, message: "Platform request failed" };
  const retryableReleaseRead =
    failure.status === 503 &&
    error instanceof Error &&
    "retryAfterSeconds" in error &&
    error.retryAfterSeconds === RELEASE_READ_RETRY_SECONDS;
  return Response.json(
    { error: failure.message, ...(errorInfo ? { errorInfo } : {}) },
    {
      status: failure.status,
      headers: {
        "cache-control": "no-store",
        ...(retryableReleaseRead ? { "retry-after": String(RELEASE_READ_RETRY_SECONDS) } : {}),
      },
    },
  );
}
