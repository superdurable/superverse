// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The Studio mock's RpcBackend: development only, never a production fallback or acceptance evidence.
import { rpcMethod, UNAUTHENTICATED_METHODS, type Caller, type RpcBackend } from "@/lib/server/grpc-client";
import { httpError } from "@/lib/server/request-body";
import type { Method, MethodDefinitionOf, MethodOf, RpcInput, RpcOutput, Service } from "@/lib/shared/rpc-methods";
import { mockAccount } from "@/mock/store/accounts";
import { advanceMockPublishes } from "@/mock/store/delivery/publish-runs";
import { getStudioMockStore, type MockStore } from "@/mock/store/store";
import { accountService } from "./account-service";
import { agentService, remoteAgentService } from "./agent-service";
import { deploymentService } from "./deployment-service";
import { previewService } from "./preview-service";
import { exampleService, projectService } from "./project-service";
import { releaseService } from "./release-service";
import { sessionService } from "./session-service";
import {
  projectMemberService,
  teamSpaceConnectionService,
  teamSpaceMemberService,
  teamSpaceService,
} from "./tenancy-service";

/** What a mock service reads: the generated <Rpc>Request__Output after the codec round trip. */
type MockRequest<M extends Method> = ReturnType<MethodDefinitionOf<M>["requestDeserialize"]>;
/** What a mock service returns: the generated <Rpc>Response, which the codec round trip completes. */
export type MockResponse<M extends Method> = Parameters<MethodDefinitionOf<M>["responseSerialize"]>[0];

interface MockContext {
  /** Resolved on first use, so an AccountService call never sets the store's origin. */
  readonly store: MockStore;
}

/** One service's methods by name; a Watch is an async generator that ends when the signal aborts. */
export type MockService<S extends Service> = {
  [M in MethodOf<S>]: M extends `Watch${string}`
    ? (request: MockRequest<M>, context: MockContext, signal: AbortSignal) => AsyncIterable<MockResponse<M>>
    : (request: MockRequest<M>, context: MockContext) => MockResponse<M> | Promise<MockResponse<M>>;
};

type Handler = (request: unknown, context: MockContext, signal: AbortSignal) => unknown;

const SERVICES: Partial<Record<Method, Handler>> = {
  ...accountService,
  ...projectService,
  ...exampleService,
  ...sessionService,
  ...agentService,
  ...remoteAgentService,
  ...previewService,
  ...releaseService,
  ...deploymentService,
  ...teamSpaceService,
  ...teamSpaceMemberService,
  ...projectMemberService,
  ...teamSpaceConnectionService,
} as Partial<Record<Method, Handler>>;

// Requests and responses cross the real protobuf codecs, so the mock reads and returns exactly the
// wire shapes the BFF sees from Go: defaults, 64-bit strings, enum names and named oneofs.
function wireRequest<M extends Method>(method: M, input: RpcInput<M>, caller: Caller | null): MockRequest<M> {
  const { definition } = rpcMethod(method);
  return definition.requestDeserialize(definition.requestSerialize({ ...input, caller })) as MockRequest<M>;
}

function wireResponse<M extends Method>(method: M, value: unknown): RpcOutput<M> {
  const { definition } = rpcMethod(method);
  return definition.responseDeserialize(definition.responseSerialize(value as object)) as RpcOutput<M>;
}

// Like Go, every call except registration and login needs a live login of the caller's account.
function handler(method: Method, caller: Caller | null): Handler {
  if (!UNAUTHENTICATED_METHODS.has(method)) mockAccount(caller);
  const found = SERVICES[method];
  if (!found) throw httpError(503, `${method} is unavailable in Studio mock mode`);
  return found;
}

/** The mock business services over the in-memory store, for the Studio origin the browser uses. */
export function mockBackend(origin: string): RpcBackend {
  const context: MockContext = {
    // Every call reads the store with its running publishes moved to the clock's time.
    get store() {
      const store = getStudioMockStore(origin);
      advanceMockPublishes(store);
      return store;
    },
  };
  return {
    async unary<M extends Method>(method: M, input: RpcInput<M>, caller: Caller | null, signal?: AbortSignal) {
      const response = await handler(method, caller)(
        wireRequest(method, input, caller),
        context,
        signal ?? new AbortController().signal,
      );
      return wireResponse(method, response);
    },
    async *watch<M extends Method>(method: M, input: RpcInput<M>, caller: Caller, signal: AbortSignal) {
      const frames = handler(method, caller)(wireRequest(method, input, caller), context, signal);
      for await (const frame of frames as AsyncIterable<unknown>) yield wireResponse(method, frame);
    },
  };
}
