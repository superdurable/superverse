// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { loadSync, type MessageTypeDefinition } from "@grpc/proto-loader";
import { decimal64, httpError } from "@/lib/server/request-body";
import { isRecord } from "@/lib/shared/json";

/**
 * The strict decoder of /api/rpc requests. It walks the request message's descriptor at any depth
 * and accepts only the browser JSON the generated input types describe: lowerCamel keys, JSON
 * strings and booleans, in-range integers, 64-bit integers as safe numbers or decimal strings,
 * declared nonzero enum names or numbers, arrays for repeated fields and at most one member per
 * oneof. Everything else is a 400, including null and a body `caller`, which only the BFF sets.
 */

type FieldKind = "string" | "bool" | "int32" | "uint32" | "int64" | "uint64" | "enum" | "message";

interface SchemaField {
  key: string;
  number: number;
  /** Absent for a kind browser requests never carry: bytes, floating point and fixed-width integers. */
  kind?: FieldKind;
  repeated: boolean;
  /** The full name of the field's message or enum. */
  typeName: string;
  /** A real oneof; a proto3 optional field is an ordinary field. */
  oneof?: string;
}

interface SchemaMessage {
  kind: "message";
  mapEntry: boolean;
  fields: Map<string, SchemaField>;
  oneofs: Map<string, string[]>;
}

interface SchemaEnum {
  kind: "enum";
  values: Map<string, number>;
  names: Map<number, string>;
}

/** Every message and enum of the loaded protocol, by full name. */
export type RequestSchema = ReadonlyMap<string, SchemaMessage | SchemaEnum>;

// The google.protobuf.FileDescriptorProto fields this decoder reads, as proto-loader decodes them.
interface FieldDescriptor {
  name: string;
  number: number;
  label: string;
  type: string;
  typeName?: string;
  oneofIndex?: number;
}
interface MessageDescriptor {
  name: string;
  field?: FieldDescriptor[];
  nestedType?: MessageDescriptor[];
  enumType?: EnumDescriptor[];
  oneofDecl?: { name: string }[];
  options?: { mapEntry?: boolean };
}
interface EnumDescriptor {
  name: string;
  value?: { name: string; number: number }[];
}
interface FileDescriptor {
  package?: string;
  messageType?: MessageDescriptor[];
  enumType?: EnumDescriptor[];
}

const KINDS: Record<string, FieldKind> = {
  TYPE_STRING: "string",
  TYPE_BOOL: "bool",
  TYPE_INT32: "int32",
  TYPE_UINT32: "uint32",
  TYPE_INT64: "int64",
  TYPE_UINT64: "uint64",
  TYPE_ENUM: "enum",
  TYPE_MESSAGE: "message",
};
const PACKAGE = "superverse.v2.";

/**
 * The schema of the file descriptors proto-loader attaches to every message definition.
 * proto-loader's own descriptor objects fill unset fields with defaults, which hides oneof
 * membership, so the serialized descriptors are decoded here without defaults.
 */
export function requestSchema(files: readonly Buffer[]): RequestSchema {
  const codec = loadSync("google/protobuf/descriptor.proto", { keepCase: false, enums: String, longs: String })[
    "google.protobuf.FileDescriptorProto"
  ] as MessageTypeDefinition<object, FileDescriptor>;
  const schema = new Map<string, SchemaMessage | SchemaEnum>();
  const pending: { scope: string[]; field: SchemaField; reference: string }[] = [];
  const addEnum = (name: string, descriptor: EnumDescriptor) => {
    const values = new Map((descriptor.value ?? []).map((value) => [value.name, value.number] as const));
    schema.set(name, { kind: "enum", values, names: new Map([...values].map(([key, value]) => [value, key])) });
  };
  const addMessage = (scope: string[], descriptor: MessageDescriptor) => {
    const name = [...scope, descriptor.name].join(".");
    const fields = new Map<string, SchemaField>();
    const oneofs = new Map<string, string[]>();
    for (const item of descriptor.field ?? []) {
      const field: SchemaField = {
        key: item.name,
        number: item.number,
        kind: KINDS[item.type],
        repeated: item.label === "LABEL_REPEATED",
        typeName: "",
      };
      fields.set(item.name, field);
      if (item.typeName) pending.push({ scope: [...scope, descriptor.name], field, reference: item.typeName });
    }
    (descriptor.oneofDecl ?? []).forEach((oneof, index) => {
      const members = (descriptor.field ?? []).filter((item) => item.oneofIndex === index).map((item) => item.name);
      // protobufjs names the synthetic oneof of a proto3 optional field `_<field>`.
      if (members.length === 1 && oneof.name === `_${members[0]}`) return;
      oneofs.set(oneof.name, members);
      for (const member of members) fields.get(member)!.oneof = oneof.name;
    });
    schema.set(name, { kind: "message", mapEntry: descriptor.options?.mapEntry === true, fields, oneofs });
    for (const nested of descriptor.nestedType ?? []) addMessage([...scope, descriptor.name], nested);
    for (const nested of descriptor.enumType ?? []) addEnum(`${name}.${nested.name}`, nested);
  };
  for (const bytes of files) {
    const file = codec.deserialize(bytes);
    const scope = file.package ? file.package.split(".") : [];
    for (const message of file.messageType ?? []) addMessage(scope, message);
    for (const item of file.enumType ?? []) addEnum([...scope, item.name].join("."), item);
  }
  // Type references are relative to the declaring message, resolved outward as protoc does.
  for (const { scope, field, reference } of pending) {
    const candidates = reference.startsWith(".")
      ? [reference.slice(1)]
      : scope.map((_, index) => [...scope.slice(0, scope.length - index), reference].join(".")).concat(reference);
    field.typeName = candidates.find((candidate) => schema.has(candidate)) ?? "";
  }
  return schema;
}

function messageOf(schema: RequestSchema, name: string): SchemaMessage {
  const message = schema.get(name);
  if (message?.kind !== "message") throw httpError(503, "Superverse request descriptors are unavailable");
  return message;
}

/**
 * The first field of a request, recursively, that a browser request cannot carry: one outside the
 * business package (Struct, Timestamp), a map, bytes or another unsupported kind.
 */
export function unsupportedRequestField(
  schema: RequestSchema,
  name: string,
  seen = new Set<string>(),
): string | undefined {
  if (seen.has(name)) return;
  seen.add(name);
  const message = schema.get(name);
  if (message?.kind !== "message" || !name.startsWith(PACKAGE) || message.mapEntry) return name;
  for (const field of message.fields.values()) {
    if (!field.kind || (["enum", "message"].includes(field.kind) && !field.typeName)) return `${name}.${field.key}`;
    if (field.kind === "message") {
      const nested = unsupportedRequestField(schema, field.typeName, seen);
      if (nested) return nested;
    }
  }
}

/** The request's CallerContext field, which must be `caller = 100`. */
export function hasCallerField(schema: RequestSchema, name: string): boolean {
  const caller = messageOf(schema, name).fields.get("caller");
  return caller?.number === 100 && caller.kind === "message" && caller.typeName === `${PACKAGE}CallerContext`;
}

// The path names browser-supplied keys, so it is bounded before it is echoed.
function reject(path: string, problem: string): never {
  throw httpError(400, `${path ? `${path.slice(0, 200)}: ` : ""}${problem}`);
}

/** A uint64 or int64 as its decimal string. */
function sixtyFourBit(kind: "int64" | "uint64", value: unknown, path: string): string {
  return decimal64(kind, value) ?? reject(path, `expected a ${kind}`);
}

function scalar(schema: RequestSchema, field: SchemaField, value: unknown, path: string): unknown {
  switch (field.kind) {
    case "string":
      return typeof value === "string" ? value : reject(path, "expected a string");
    case "bool":
      return typeof value === "boolean" ? value : reject(path, "expected a boolean");
    case "int32":
      return typeof value === "number" && Number.isInteger(value) && value >= -(2 ** 31) && value < 2 ** 31
        ? value
        : reject(path, "expected an int32");
    case "uint32":
      return typeof value === "number" && Number.isInteger(value) && value >= 0 && value < 2 ** 32
        ? value
        : reject(path, "expected a uint32");
    case "int64":
    case "uint64":
      return sixtyFourBit(field.kind, value, path);
    case "enum": {
      const values = schema.get(field.typeName);
      if (values?.kind !== "enum") return reject(path, "unsupported field");
      const name = typeof value === "string" ? value : typeof value === "number" ? values.names.get(value) : undefined;
      // Zero means absent, so a value that names it is a mistake rather than an omission.
      if (name === undefined || !values.values.get(name)) reject(path, "expected a declared nonzero enum value");
      return name;
    }
    case "message":
      return message(schema, field.typeName, value, path);
    case undefined:
      return reject(path, "unsupported field");
  }
}

function message(schema: RequestSchema, name: string, value: unknown, path: string): Record<string, unknown> {
  if (!isRecord(value)) reject(path, "expected an object");
  const { fields, oneofs } = messageOf(schema, name);
  const decoded: Record<string, unknown> = {};
  const members = new Map<string, string>();
  const named = new Map<string, unknown>();
  for (const [key, item] of Object.entries(value)) {
    const at = path ? `${path}.${key}` : key;
    const field = fields.get(key);
    if (!field) {
      if (oneofs.has(key)) named.set(key, item);
      else reject(at, "unknown field");
      continue;
    }
    if (item === null) reject(at, "null is not a value");
    if (field.oneof) {
      if (members.has(field.oneof)) reject(path, `more than one ${field.oneof}`);
      members.set(field.oneof, key);
    }
    if (!field.repeated) decoded[key] = scalar(schema, field, item, at);
    else if (!Array.isArray(item)) reject(at, "expected an array");
    else decoded[key] = item.map((element, index) => scalar(schema, field, element, `${at}[${index}]`));
  }
  // A oneof's own key may only name the member that is present; protobuf ignores it otherwise.
  for (const [oneof, member] of named)
    if (members.get(oneof) !== member) reject(path ? `${path}.${oneof}` : oneof, "does not name the member set");
  return decoded;
}

/** A POST body of one request message, without the caller. */
export function decodeRequestBody(schema: RequestSchema, name: string, body: unknown): Record<string, unknown> {
  if (body && typeof body === "object" && "caller" in body) reject("caller", "set by Studio, never by the browser");
  return message(schema, name, body, "");
}

/**
 * A Watch's GET query: one value per top-level string or 64-bit field. Last-Event-ID, which an
 * EventSource sends when it reconnects, overrides resumeCursor.
 */
export function decodeWatchQuery(
  schema: RequestSchema,
  name: string,
  query: URLSearchParams,
  lastEventId: string | null,
): Record<string, unknown> {
  const { fields } = messageOf(schema, name);
  const decoded: Record<string, unknown> = {};
  for (const key of new Set(query.keys())) {
    const values = query.getAll(key);
    const field = fields.get(key);
    if (key === "caller") reject(key, "set by Studio, never by the browser");
    if (!field || field.repeated || field.oneof || !["string", "int64", "uint64"].includes(field.kind ?? ""))
      reject(key, "unknown parameter");
    if (values.length !== 1) reject(key, "repeated parameter");
    decoded[key] = field.kind === "string" ? values[0] : sixtyFourBit(field.kind as "int64" | "uint64", values[0], key);
  }
  if (lastEventId && fields.get("resumeCursor")?.kind === "string") decoded.resumeCursor = lastEventId;
  return decoded;
}
