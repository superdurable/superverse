// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { draftItemAnchor, draftItemKey, nextPublishItemAnchor } from "@/lib/api/flow/draft/configuration-draft";
import type { ConfigurationItem__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItem";
import type { ConfigurationItemKey__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItemKey";
import type { ConfigurationRequirementGap__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationRequirementGap";
import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";

/**
 * The item a requirement gap belongs to, as finely as the gap names it: a missing environment value is
 * its variable, a trigger binding gap its binding, a Step's missing setting its Step row, and anything
 * else about a connection its row.
 */
export function gapItemKey(gap: ConfigurationRequirementGap__Output): string {
  switch (gap.kind) {
    case "KIND_ENVIRONMENT_REQUIRED":
      return draftItemKey({ kind: "KIND_ENVIRONMENT", environmentName: gap.environmentName });
    case "KIND_TRIGGER_BINDING_REQUIRED":
    case "KIND_TRIGGER_BINDING_INVALID":
      return draftItemKey({
        kind: "KIND_TRIGGER_BINDING",
        connectorId: gap.connectorId,
        connectionName: gap.connectionName,
        triggerName: gap.triggerName,
        bindingName: gap.bindingName,
      });
    case "KIND_OPERATION_CONFIGURATION_REQUIRED":
      return draftItemKey({
        kind: "KIND_STEP_SETTING",
        connectorId: gap.connectorId,
        connectionName: gap.connectionName,
        operationId: gap.operationId,
        flowType: gap.flowName,
        stepType: gap.stepName,
      });
    case "KIND_UNSPECIFIED":
    case "KIND_CONNECTION_REQUIRED":
    case "KIND_AUTHORIZATION_REQUIRED":
    case "KIND_CONNECTION_INCOMPLETE":
      return draftItemKey({
        kind: "KIND_CONNECTION",
        connectorId: gap.connectorId,
        connectionName: gap.connectionName,
      });
  }
}

/** The element ID a gap's link jumps to and focuses. */
export function gapAnchor(gap: ConfigurationRequirementGap__Output): string {
  return draftItemAnchor(gapItemKey(gap));
}

/** The element ID of a gap's Next publish control, which a link asking Live to set it up focuses. */
export function nextPublishGapAnchor(gap: ConfigurationRequirementGap__Output): string {
  return nextPublishItemAnchor(gapItemKey(gap));
}

/** The queue's and the editors' key of a server item. */
export function itemKeyOf(key: ConfigurationItemKey__Output): string {
  return draftItemKey({ ...key, kind: key.kind === "KIND_UNSPECIFIED" ? "KIND_CONNECTION" : key.kind });
}

/** What Setup knows of one item: its server item, if the server reported one, and whether a gap names it. */
export interface SetupItem {
  item: ConfigurationItem__Output | null;
  missing: boolean;
}

/** Every item of a configuration by key, with the gaps that name it. */
export function setupItems(configuration: ProjectConfigurationSnapshot__Output): Map<string, SetupItem> {
  const items = new Map<string, SetupItem>();
  for (const item of configuration.items) if (item.key) items.set(itemKeyOf(item.key), { item, missing: false });
  for (const gap of configuration.requirementGaps) {
    const key = gapItemKey(gap);
    items.set(key, { item: items.get(key)?.item ?? null, missing: true });
  }
  return items;
}

/** How many items of Live's next publish wait for it: they differ from what current Live runs. */
export function liveDifferenceCount(configuration: ProjectConfigurationSnapshot__Output): number {
  return configuration.items.filter((item) => item.differsFromLive).length;
}

/** An item's name in a list, such as "support · channelThreadCreated/start" or "environment APP_LABEL". */
export function itemName(key: ConfigurationItemKey__Output): string {
  switch (key.kind) {
    case "KIND_ENVIRONMENT":
      return `environment ${key.environmentName}`;
    case "KIND_TRIGGER_BINDING":
      return `${key.connectionName} · trigger ${key.triggerName}/${key.bindingName}`;
    case "KIND_STEP_SETTING":
      return `${key.connectionName} · ${key.stepType} in ${key.flowType}`;
    case "KIND_CONNECTION":
    case "KIND_UNSPECIFIED":
      return `connection ${key.connectionName}`;
  }
}

/**
 * A non-secret value of an item as the server encodes it, in a few words: an environment value's
 * `value`, a connection's selection as "the connection Preview uses" (its ID names nothing a person
 * reads), and a setting's fields as "channelId: C024BE91L".
 */
export function itemValueText(json: string): string {
  if (!json) return "";
  let value: unknown;
  try {
    value = JSON.parse(json);
  } catch {
    return json;
  }
  const record = value !== null && typeof value === "object" ? (value as Record<string, unknown>) : null;
  const text =
    record && "teamSpaceConnectionId" in record
      ? "the same connection"
      : record && Object.keys(record).length === 1 && "value" in record
        ? String(record.value ?? "")
        : record
          ? Object.entries(record)
              .map(([name, entry]) => `${name}: ${typeof entry === "string" ? entry : JSON.stringify(entry)}`)
              .join(", ")
          : String(value);
  return text.length > 80 ? `${text.slice(0, 79)}…` : text;
}
