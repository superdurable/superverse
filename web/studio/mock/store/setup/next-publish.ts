// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock's next-publish layer: which items Live owns, how each item of the next publish resolves from the
// layer, Preview's applied values and current Live, as the configuration host resolves them, and the item
// routes Setup writes on any scope. Development only.
import type { ConfigurationItem } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItem";
import type { ConfigurationItemKey } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItemKey";
import { httpError } from "@/lib/server/request-body";
import { MOCK_CONNECTOR, MOCK_SLACK_CONNECTOR, MOCK_STRIPE_CONNECTOR } from "@/mock/fixture/connector-manifest";
import type { MockNativeValues } from "./preview-setup";

/**
 * What a Live configuration keeps beyond its values: the layer's items Live owns, and, for a version's
 * frozen copy, the layer, Preview and Live revisions it was built from.
 */
export interface MockLiveSetup {
  own: string[];
  copiedFrom?: string;
}

export function newLiveSetup(): MockLiveSetup {
  return { own: [] };
}

/** What a version declares beyond the fixture connection: the fixtures' connection names and its environment. */
export interface MockDeclared {
  slack: boolean;
  stripe: boolean;
  environment: { name: string; secret: boolean }[];
}

type Refusal = "CONFIGURATION_FOLLOW_REFUSAL_SECRET" | "CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND";

/** A write's body as the item routes read it. */
export interface MockItemBody {
  teamSpaceConnectionId?: unknown;
  configuration?: { model?: unknown; channelId?: unknown };
  followPreview?: unknown;
}

/** One item: its key, its route, how its value reads and is written, and why it never follows Preview. */
interface MockItem {
  key: ConfigurationItemKey;
  route: string;
  refusal?: Refusal;
  valueOf: (values: MockNativeValues) => string | undefined;
  write: (values: MockNativeValues, value: string | undefined) => void;
  /** The value as the configuration host encodes it. */
  encode: (value: string) => object;
  /** The value a write names: undefined to clear it, null when the body is invalid. */
  decode: (body: MockItemBody) => string | undefined | null;
}

const selection = (body: MockItemBody) =>
  typeof body.teamSpaceConnectionId === "string" && body.teamSpaceConnectionId ? body.teamSpaceConnectionId : null;
const text = (value: unknown) => (typeof value === "string" && value ? value : undefined);

/** A connection name's selection, kept where `slot` reads and writes it. */
function connectionItem(
  named: { connectorId: string; connectionName: string },
  slot: { get: (values: MockNativeValues) => string | undefined; set: (values: MockNativeValues, id?: string) => void },
  refusal?: Refusal,
): MockItem {
  return {
    key: { kind: "KIND_CONNECTION", ...named },
    route: `api/v2/connector-selections/${named.connectorId}/${named.connectionName}`,
    refusal,
    valueOf: slot.get,
    write: slot.set,
    encode: (value) => ({ teamSpaceConnectionId: value }),
    decode: selection,
  };
}

/** An environment setting; a secret shows only whether it is set and never follows. */
function environmentItem(name: string, secret: boolean): MockItem {
  return {
    key: { kind: "KIND_ENVIRONMENT", environmentName: name },
    route: "",
    refusal: secret ? "CONFIGURATION_FOLLOW_REFUSAL_SECRET" : undefined,
    valueOf: (values) =>
      secret ? (values.configuredSecrets.includes(name) ? "set" : undefined) : values.environment[name],
    encode: (value) => ({ value }),
    write: (values, value) => {
      if (secret) return;
      if (value === undefined) delete values.environment[name];
      else values.environment[name] = value;
    },
    decode: () => null,
  };
}

/** The Slack fixture's start and reply bindings, each with a channel. */
const SLACK_BINDINGS = [
  { triggerName: "channelThreadCreated", bindingName: "start" },
  { triggerName: "threadReplyCreated", bindingName: "reply" },
] as const;

/** The items a version declares, plus the environment names only `extra` holds. */
function mockItems(declared: MockDeclared, extra: string[] = []): MockItem[] {
  const slack = (values: MockNativeValues) => (values.slack ??= { channels: {} });
  const stripe = (values: MockNativeValues) => (values.stripe ??= {});
  const step = {
    kind: "KIND_STEP_SETTING" as const,
    ...MOCK_CONNECTOR,
    operationId: "generateText",
    flowType: "TextSummaryFlow",
    stepType: "GenerateSummary",
  };
  return [
    connectionItem(MOCK_CONNECTOR, {
      get: (values) => values.selectedConnectionId,
      set: (values, id) => void (values.selectedConnectionId = id),
    }),
    {
      key: {
        kind: step.kind,
        connectorId: step.connectorId,
        connectionName: step.connectionName,
        operationId: step.operationId,
        flowType: step.flowType,
        stepType: step.stepType,
      },
      route: `api/v2/connector-use-configurations/${step.connectorId}/${step.connectionName}/${step.operationId}/${step.flowType}/${step.stepType}`,
      valueOf: (values) => values.useConfiguration.model,
      encode: (value) => ({ model: value }),
      write: (values, value) => void (values.useConfiguration = value === undefined ? {} : { model: value }),
      decode: (body) => text(body.configuration?.model),
    },
    ...(declared.slack
      ? [
          connectionItem(MOCK_SLACK_CONNECTOR, {
            get: (values) => values.slack?.selectedConnectionId,
            set: (values, id) => void (slack(values).selectedConnectionId = id),
          }),
          ...SLACK_BINDINGS.map((binding): MockItem => ({
            key: { kind: "KIND_TRIGGER_BINDING", ...MOCK_SLACK_CONNECTOR, ...binding },
            route: `api/v2/connector-trigger-bindings/${MOCK_SLACK_CONNECTOR.connectorId}/${MOCK_SLACK_CONNECTOR.connectionName}/${binding.triggerName}/${binding.bindingName}`,
            valueOf: (values) => values.slack?.channels[binding.bindingName],
            encode: (value) => ({ channelId: value }),
            write: (values, value) => {
              if (value === undefined) delete slack(values).channels[binding.bindingName];
              else slack(values).channels[binding.bindingName] = value;
            },
            decode: (body) => text(body.configuration?.channelId),
          })),
        ]
      : []),
    ...(declared.stripe
      ? [
          connectionItem(
            MOCK_STRIPE_CONNECTOR,
            {
              get: (values) => values.stripe?.selectedConnectionId,
              set: (values, id) => void (stripe(values).selectedConnectionId = id),
            },
            "CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND",
          ),
        ]
      : []),
    ...declared.environment.map((field) => environmentItem(field.name, field.secret)),
    ...extra.map((name) => environmentItem(name, false)),
  ];
}

/** The layer's key of an item. */
const mockItemKey = (key: ConfigurationItemKey) =>
  [
    key.kind,
    key.connectorId,
    key.connectionName,
    key.triggerName,
    key.bindingName,
    key.operationId,
    key.flowType,
    key.stepType,
    key.environmentName,
  ]
    .map((part) => part ?? "")
    .join("/");

const json = (item: MockItem, value: string | undefined) =>
  value === undefined ? "" : JSON.stringify(item.encode(value));

/** Whether Live owns an item of the layer: it chose its own value, or the item can never follow. */
const owns = (layer: MockLiveSetup, item: MockItem) =>
  Boolean(item.refusal) || layer.own.includes(mockItemKey(item.key));

/** The layer's values with every declared fixture's slot, so a resolved value has somewhere to go. */
function withSlots(values: MockNativeValues, declared: MockDeclared): MockNativeValues {
  const copy = JSON.parse(JSON.stringify(values)) as MockNativeValues;
  if (declared.slack) copy.slack ??= { channels: {} };
  else delete copy.slack;
  if (declared.stripe) copy.stripe ??= {};
  else delete copy.stripe;
  return copy;
}

/**
 * The next publish's values: Live's own value of each item, else Preview's applied one, else, when Preview
 * applied none, current Live's.
 */
export function resolveLayer(
  declared: MockDeclared,
  layer: { values: MockNativeValues; setup: MockLiveSetup },
  live: MockNativeValues | null,
  preview: MockNativeValues | null,
): MockNativeValues {
  const resolved = withSlots(layer.values, declared);
  for (const item of mockItems(declared)) {
    if (owns(layer.setup, item)) continue;
    const applied = preview ? item.valueOf(preview) : undefined;
    item.write(resolved, applied !== undefined ? applied : live ? item.valueOf(live) : undefined);
  }
  return resolved;
}

/**
 * The items of Live's next publish: OWN where Live chose its value (a secret and a connection tied to one
 * address always), FOLLOWS_PREVIEW otherwise, each marked where it differs from current Live; on Live but
 * not declared, NO_LONGER_USED.
 */
export function liveItems(
  declared: MockDeclared,
  layer: { values: MockNativeValues; setup: MockLiveSetup },
  live: MockNativeValues | null,
  preview: MockNativeValues | null,
): ConfigurationItem[] {
  const names = new Set(declared.environment.map((field) => field.name));
  const removed = Object.keys(live?.environment ?? {}).filter((name) => !names.has(name));
  const resolved = resolveLayer(declared, layer, live, preview);
  return mockItems(declared, removed).map((item): ConfigurationItem => {
    const secret = item.refusal === "CONFIGURATION_FOLLOW_REFUSAL_SECRET";
    const value = item.valueOf(resolved);
    const onLive = live ? item.valueOf(live) : undefined;
    const applied = preview && !secret ? item.valueOf(preview) : undefined;
    const base = {
      key: item.key,
      differsFromLive: value !== onLive,
      differsFromPreview: value !== applied,
      liveValueJson: secret ? "" : json(item, onLive),
      previewValueJson: json(item, applied),
      followRefusal: item.refusal,
    };
    if (removed.includes(item.key.environmentName ?? ""))
      return { ...base, state: "STATE_NO_LONGER_USED", differsFromLive: true };
    return { ...base, state: owns(layer.setup, item) ? "STATE_OWN" : "STATE_FOLLOWS_PREVIEW" };
  });
}

/** Makes the layer's items of `keys` Live's own, or, with `own` false ("Same as Preview"), follow Preview again. */
export function markOwn(setup: MockLiveSetup, keys: ConfigurationItemKey[], own: boolean) {
  const changed = new Set(keys.map(mockItemKey));
  setup.own = own ? [...new Set([...setup.own, ...changed])] : setup.own.filter((key) => !changed.has(key));
}

/**
 * Answers a write to an item route, or undefined for another route: Live's own value, or on the layer
 * `followPreview`, which the server refuses on Preview and for an item that never follows. The caller moves
 * the configuration's revision.
 */
export function writeMockItem(
  values: MockNativeValues,
  layer: MockLiveSetup | null,
  declared: MockDeclared,
  route: string,
  body: MockItemBody,
): object | undefined {
  const item = mockItems(declared).find((candidate) => candidate.route && candidate.route === route);
  if (!item) return undefined;
  const follow = body.followPreview === true;
  if (follow && (!layer || item.refusal))
    throw Object.assign(
      httpError(409, layer ? "This setting cannot follow Preview" : "Only Live's next publish follows Preview"),
      {
        code: "CONFIGURATION_ITEM_CANNOT_FOLLOW",
      },
    );
  if (!follow) {
    const value = item.decode(body);
    if (value === null && item.key.kind === "KIND_CONNECTION")
      throw httpError(400, "Connection selection request is invalid");
    item.write(values, value ?? undefined);
  }
  if (layer) markOwn(layer, [item.key], !follow);
  return item.key.kind === "KIND_CONNECTION"
    ? {
        connectorId: item.key.connectorId,
        connectionName: item.key.connectionName,
        sharedConnectionId: item.valueOf(values),
      }
    : { saved: true };
}
