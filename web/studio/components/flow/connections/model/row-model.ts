// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowApiScopeBinding } from "@/lib/flow/FlowApiProvider";
import {
  connectionKey,
  connectorDisplayName,
  type ConnectionView,
  type ConnectionsResponse,
  type ConnectorUIUnit,
  type ReleaseManifest,
  type TriggerUse,
} from "@/lib/api/flow/connector";
import { draftItemKey, type ConfigurationDraftQueue } from "@/lib/api/flow/draft/configuration-draft";
import type { ConnectorCatalogEntry, TeamSpaceConnection } from "@/lib/api/flow/team-space-connection";
import type { ConfigurationItem__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItem";
import { needsSignIn, triggerWords } from "./connection-words";
import type { ConnectionSession } from "./use-scope-catalog";

/** A column of the table: Preview, or the next publish, which the Live tab shows alone. */
export type SetupSideName = "preview" | "next";

/** A side's words in a control's accessible name: "on Preview", or "for the next publish". */
export const SIDE_WORDS = { preview: "on Preview", next: "for the next publish" } as const satisfies Record<
  SetupSideName,
  string
>;

/** What the server says of one item: its item, if it reported one, and whether a gap names it. */
export interface SetupItem {
  item: ConfigurationItem__Output | null;
  missing: boolean;
}

/** One column's configuration host, with what it read: its catalog, sessions, queue and the server's items. */
export interface SetupSide {
  name: SetupSideName;
  scope: FlowApiScopeBinding;
  queue: ConfigurationDraftQueue;
  catalog: ConnectionsResponse;
  sessions: Map<string, ConnectionSession>;
  /** The server's items with the gaps that name them, by autosave key; null while they are read. */
  items: Map<string, SetupItem> | null;
}

/** What every row shares: the TeamSpace, its connections, the catalog and how to name what Live runs now. */
export interface SetupTable {
  teamSpaceId: string;
  /** Live's name in a waiting dot's words, "Live V12"; null before the first publish, when nothing waits. */
  live: string | null;
  /** The TeamSpace's connections by connector, null while they are read. */
  choices: Map<string, TeamSpaceConnection[] | string> | null;
  reloadChoices: () => Promise<void>;
  catalog: Map<string, ConnectorCatalogEntry>;
  /** Each side's address, which a new connection tied to one address is set up for. */
  addresses: Record<SetupSideName, string>;
  /** The Live tab: one column, the next publish's. */
  liveTab: boolean;
}

/** A setting group under a row: a trigger binding or a Step, with the units it is set with. */
export interface SettingGroup {
  key: string;
  title: string;
  /** The raw name, in a hover hint. */
  hint: string;
  detail: string;
  units: ConnectorUIUnit[];
  /** The binding or operation the settings write. */
  use:
    | { kind: "trigger"; triggerName: string; bindingName: string }
    | { kind: "operation"; operationId: string; flowType: string; stepType: string };
  /** Why the group has no control, such as a binding no process declares. */
  problem: string;
}

const MISSING: SetupItem = { item: null, missing: false };

/** The autosave key of a connection name's selection. */
export function connectionItemKey(connection: ConnectionView): string {
  return draftItemKey({
    kind: "KIND_CONNECTION",
    connectorId: connection.connectorId,
    connectionName: connection.connectionName,
  });
}

/** The trigger bindings and Steps of a connection name that have settings, each a group under its row. */
export function settingGroups(connection: ConnectionView, manifest: ReleaseManifest | null): SettingGroup[] {
  const named = { connectorId: connection.connectorId, connectionName: connection.connectionName };
  const triggers = (connection.triggerUses ?? [])
    .filter((use) => use.configurationUI === undefined || use.configurationUI.units.length > 0)
    .map((use: TriggerUse): SettingGroup => ({
      key: draftItemKey({
        kind: "KIND_TRIGGER_BINDING",
        ...named,
        triggerName: use.triggerName,
        bindingName: use.bindingName,
      }),
      title: triggerWords(manifest, use.triggerName),
      hint: `${use.triggerName}/${use.bindingName}`,
      detail: "",
      units: use.configurationUI?.units ?? [],
      use: { kind: "trigger", triggerName: use.triggerName, bindingName: use.bindingName },
      problem: use.configurationUI === undefined ? "No process of this version declares this trigger." : "",
    }));
  const steps = connection.uses
    .filter((use) => use.configurationUI.units.length > 0)
    .map((use): SettingGroup => ({
      key: draftItemKey({
        kind: "KIND_STEP_SETTING",
        ...named,
        operationId: use.operationId,
        flowType: use.flowName,
        stepType: use.stepName,
      }),
      title: use.stepName || use.stepId,
      hint: `${use.operationId} in ${use.flowName}`,
      detail: use.flowName,
      units: use.configurationUI.units,
      use: { kind: "operation", operationId: use.operationId, flowType: use.flowName, stepType: use.stepName },
      problem: "",
    }));
  return [...triggers, ...steps];
}

/** Every autosave key a row holds: its selection, then each trigger binding and Step it names. */
export function rowItemKeys(connection: ConnectionView): string[] {
  const named = { connectorId: connection.connectorId, connectionName: connection.connectionName };
  return [
    connectionItemKey(connection),
    ...(connection.triggerUses ?? []).map((use) =>
      draftItemKey({
        kind: "KIND_TRIGGER_BINDING",
        ...named,
        triggerName: use.triggerName,
        bindingName: use.bindingName,
      }),
    ),
    ...connection.uses.map((use) =>
      draftItemKey({
        kind: "KIND_STEP_SETTING",
        ...named,
        operationId: use.operationId,
        flowType: use.flowName,
        stepType: use.stepName,
      }),
    ),
  ];
}

/** What the server says of an item on a side. */
export function sideItem(side: SetupSide | null, key: string): SetupItem {
  return side?.items?.get(key) ?? MISSING;
}

/** The side's view of a connection name, null before the side read it. */
export function sideConnection(side: SetupSide | null, connection: ConnectionView): ConnectionView | null {
  return side?.catalog.connections.find((item) => connectionKey(item) === connectionKey(connection)) ?? null;
}

/**
 * Whether the next publish listens where Preview listens: a trigger binding that follows Preview's
 * settings over the same TeamSpace connection, so both would answer each event.
 */
export function listensWithPreview(side: SetupSide | null, connection: ConnectionView, key: string): boolean {
  if (side?.name !== "next") return false;
  const binding = sideItem(side, key).item;
  const selection = sideItem(side, connectionItemKey(connection)).item;
  if (binding?.key?.kind !== "KIND_TRIGGER_BINDING" || !selection) return false;
  if (binding.previewValueJson === "" || binding.previewValueJson === "{}") return false;
  return !binding.differsFromPreview && !selection.differsFromPreview && selection.previewValueJson !== "";
}

/**
 * A side's state of a row: red when an item is missing, amber when its connection needs a new sign-in. A channel
 * Preview or another Project also listens in is a choice, not a fault: its menu note says so and the Publish
 * popover lists it once.
 */
export function rowTone(side: SetupSide | null, connection: ConnectionView): "none" | "warn" | "ok" {
  const keys = rowItemKeys(connection);
  if (!side || keys.some((key) => sideItem(side, key).missing)) return "none";
  const own = sideConnection(side, connection);
  return own && needsSignIn(own) ? "warn" : "ok";
}

/** Why a connector's connection serves one address, as the next publish's menu note says it. */
export function endpointBoundReason(entry: ConnectorCatalogEntry | undefined, connection: ConnectionView): string {
  return (
    entry?.endpointBoundReason ||
    `${connectorDisplayName(connection)} signs its events for one address, so a connection serves one environment.`
  );
}
