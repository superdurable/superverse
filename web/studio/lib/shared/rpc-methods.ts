// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Long, MethodDefinition } from "@grpc/proto-loader";
import type { ProtoGrpcType } from "@/lib/generated/protobuf/superverse";

// The business RPCs by name, derived from the generated service definitions so that a proto
// change reaches every caller through the compiler. RPC names are unique across services
// (check-proto-services.py), so a method name alone selects its service.

type Package = ProtoGrpcType["superverse"]["v2"];

/** A generated service: its constructor carries the method definitions in `service`. */
export type Service = { [K in keyof Package]: Package[K] extends { service: object } ? K : never }[keyof Package];

type Definitions = { [S in Service]: Package[S] extends { service: infer D } ? D : never };

// A generated definition extends grpc.ServiceDefinition, whose string index signature would
// swallow the declared names; keep only those.
type DeclaredMethods<D> = keyof { [K in keyof D as string extends K ? never : K]: D[K] } & string;

type MethodsOf = { [S in Service]: DeclaredMethods<Definitions[S]> };

export type Method = MethodsOf[Service];

/** The methods of the named services. */
export type MethodOf<S extends Service> = MethodsOf[S];

type ServiceOf<M extends Method> = { [S in Service]: M extends MethodsOf[S] ? S : never }[Service];

/** The services the browser reaches through /api/rpc/<Method>; the BFF allowlist names exactly these. */
export type BrowserService =
  | "ProjectService"
  | "SessionService"
  | "AgentService"
  | "ModelCredentialService"
  | "RemoteAgentService"
  | "PreviewService"
  | "ReleaseService"
  | "DeploymentService"
  | "TeamSpaceService"
  | "TeamSpaceMemberService"
  | "ProjectMemberService"
  | "TeamSpaceConnectionService"
  | "AttachmentService"
  | "EvolveService";

type BrowserMethod = MethodOf<BrowserService>;

/** A method name starts with Watch exactly when the RPC streams from the server. */
export type WatchMethod = Extract<BrowserMethod, `Watch${string}`>;

export type UnaryMethod = Exclude<BrowserMethod, WatchMethod>;

/** Every method's generated definition, by method name. */
type MethodDefinitions = {
  [M in Method]: Extract<
    Definitions[ServiceOf<M>][M & keyof Definitions[ServiceOf<M>]],
    MethodDefinition<object, object>
  >;
};

/** A method's generated definition with its request and response codecs. */
export type MethodDefinitionOf<M extends Method> = MethodDefinitions[M];

type GeneratedRequest<M extends Method> = Parameters<MethodDefinitionOf<M>["requestSerialize"]>[0];

// The JSON that the BFF decoder accepts for a generated input message: numbers or decimal strings
// for 64-bit integers, names or numbers for enums, and neither null nor the synthetic `_field`
// keys of proto3 optional fields. A real oneof keeps its own key, which may name the member set.
type RequestJson<T> = {
  [K in keyof T as K extends `_${string}` ? never : K]: RequestJsonValue<Exclude<T[K], null | Long>>;
};

type RequestJsonValue<V> = V extends Uint8Array
  ? V
  : V extends readonly (infer E)[]
    ? RequestJsonValue<Exclude<E, null | Long>>[]
    : V extends object
      ? RequestJson<V>
      : V;

/** The browser's request body: the generated <Rpc>Request without the caller, which only the BFF sets. */
export type RpcInput<M extends Method> = RequestJson<Omit<GeneratedRequest<M>, "caller">>;

/** The decoded <Rpc>Response__Output: 64-bit integers as strings, enums as names, defaults set, oneofs named. */
export type RpcOutput<M extends Method> = ReturnType<MethodDefinitionOf<M>["responseDeserialize"]>;
