// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useRef } from "react";
import type { ConnectionView, TriggerUse } from "@/lib/api/flow/connector";
import { settingsDraftItem } from "@/lib/api/flow/draft/scope-writes";
import { jsonPointerValue, mergeUnitValue } from "@/components/flow/connector/manifest/json-pointer";
import type { SetupSide } from "./row-model";

/**
 * The reply triggers whose channel defaults to their start trigger's: a thread reply arrives in the
 * channel where the thread started, so the reply binding listens there unless someone chose otherwise.
 */
const REPLY_STARTS: Readonly<Record<string, string>> = { threadReplyCreated: "channelThreadCreated" };

/** The unit whose value is a binding's channel. */
const CHANNEL_UNIT = "channelPicker";

/** The channel a binding listens to, from its channel unit's own JSON pointer; empty without one. */
export function channelOf(use: TriggerUse, configuration: Record<string, unknown> = use.configuration): string {
  const binding = use.configurationUI?.units.find((item) => item.unitId === CHANNEL_UNIT)?.bindings[0];
  if (!binding) return "";
  const value = jsonPointerValue(configuration, binding.jsonPointer);
  return typeof value === "string" ? value : "";
}

/** A binding's settings as this tab holds them: its pending or saved value, else the side's read. */
function configurationOf(side: SetupSide, connection: ConnectionView, use: TriggerUse): Record<string, unknown> {
  const key = settingsDraftItem(side.catalog, connection, use, use.configuration).key;
  const value = side.queue.valueOf(key);
  return value && typeof value === "object" && !("followPreview" in value)
    ? (value as Record<string, unknown>)
    : use.configuration;
}

/** The start binding a reply binding of the same connection name takes its channel from, if any. */
function startOf(connection: ConnectionView, use: TriggerUse): TriggerUse | null {
  return (connection.triggerUses ?? []).find((other) => other.triggerName === REPLY_STARTS[use.triggerName]) ?? null;
}

/**
 * Whether a reply binding listens in its start binding's channel on a side, which its control says; the
 * start binding's trigger name, or null.
 */
export function sameChannelAs(side: SetupSide, connection: ConnectionView, triggerName: string, bindingName: string) {
  const use = connection.triggerUses?.find(
    (item) => item.triggerName === triggerName && item.bindingName === bindingName,
  );
  const start = use && startOf(connection, use);
  if (!use || !start) return null;
  const channel = channelOf(use, configurationOf(side, connection, use));
  return channel && channel === channelOf(start, configurationOf(side, connection, start)) ? start.triggerName : null;
}

/**
 * Fills each empty reply channel of Preview's from its start binding once per start channel, through the
 * side's autosave queue. The next publish follows Preview, so it fills nothing.
 */
export function useReplyChannelDefaults(side: SetupSide | null) {
  const filled = useRef(new Set<string>());
  useEffect(() => {
    if (!side) return;
    for (const connection of side.catalog.connections)
      for (const use of connection.triggerUses ?? []) {
        const start = startOf(connection, use);
        const channel = start ? channelOf(start, configurationOf(side, connection, start)) : "";
        const unit = use.configurationUI?.units.find((item) => item.unitId === CHANNEL_UNIT);
        const item = settingsDraftItem(side.catalog, connection, use, use.configuration);
        const once = `${item.key}:${channel}`;
        if (!channel || !unit || channelOf(use, configurationOf(side, connection, use)) || filled.current.has(once))
          continue;
        filled.current.add(once);
        side.queue.save(
          item,
          (latest) => mergeUnitValue(latest, unit.bindings, { [unit.bindings[0].port]: channel }),
          true,
        );
      }
  });
}
