// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { FlowApiScope } from "@/lib/flow/FlowApiProvider";
import { connectionKey, type ConnectionView, type ConnectorUIUnit } from "@/lib/api/flow/connector";
import { jsonPointerValue } from "@/components/flow/connector/manifest/json-pointer";
import {
  ConfigurationUnit,
  configurationUnitTarget,
  type NextPublishUnit,
} from "@/components/flow/connector/native/NativeConnectorUnitHost";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import {
  listensWithPreview,
  sideItem,
  SIDE_WORDS,
  type SetupSide,
  type SettingGroup,
} from "@/components/flow/connections/model/row-model";

/** A unit's value in an item's non-secret JSON, as a string; empty when the item holds none. */
function valueAt(json: string, unit: ConnectorUIUnit): string {
  if (!json) return "";
  try {
    const value = jsonPointerValue(JSON.parse(json) as Record<string, unknown>, unit.bindings[0]?.jsonPointer ?? "");
    return typeof value === "string" ? value : "";
  } catch {
    return "";
  }
}

/**
 * One setting of a trigger binding or Step on one side: its connector's unit as a dropdown, once the side's
 * connection works, else "After connecting" with the reason in its tooltip. On the next publish it starts with
 * "Same as Preview" and carries the blue dot while it waits; red when Setup misses it. Where the next publish
 * would listen where Preview or another Project listens, its menu note says so without colouring it.
 */
export function SettingCell({
  side,
  connection,
  group,
  unit,
  live,
  note: extraNote,
}: {
  side: SetupSide;
  /** The side's view of the connection name. */
  connection: ConnectionView;
  group: SettingGroup;
  unit: ConnectorUIUnit;
  /** "Live V12", for the waiting dot's words; null before the first publish. */
  live: string | null;
  /** What the menu note adds, such as that a reply listens in its start trigger's channel. */
  note?: string;
}) {
  const label = `${unit.label}, ${group.title} ${SIDE_WORDS[side.name]}`;
  const opened = side.sessions.get(connectionKey(connection)) ?? null;
  const { item, missing } = sideItem(side, group.key);
  const wanted = group.use;
  const use =
    wanted.kind === "trigger"
      ? connection.triggerUses?.find(
          (entry) => entry.triggerName === wanted.triggerName && entry.bindingName === wanted.bindingName,
        )
      : connection.uses.find(
          (entry) =>
            entry.operationId === wanted.operationId &&
            entry.flowName === wanted.flowType &&
            entry.stepName === wanted.stepType,
        );
  if (!opened || !("session" in opened) || !use)
    return (
      <Dropdown
        label={label}
        disabled
        value={null}
        items={[]}
        onChange={() => undefined}
        placeholder={opened && "error" in opened ? opened.error : "After connecting"}
        title={
          opened && "error" in opened
            ? opened.error
            : `Pick after ${side.name === "next" ? "the next publish" : "Preview"} has a working connection: the choices come from it`
        }
      />
    );
  const listens = listensWithPreview(side, connection, group.key);
  const shared = item?.warnings.includes("CONFIGURATION_ITEM_WARNING_CHANNEL_SHARED");
  const next = side.name === "next";
  const nextPublish: NextPublishUnit | undefined = next
    ? {
        follows: item?.state === "STATE_FOLLOWS_PREVIEW",
        previewValue: valueAt(item?.previewValueJson ?? "", unit),
        waiting: live && item?.differsFromLive ? { live, value: valueAt(item.liveValueJson, unit) } : undefined,
      }
    : undefined;
  const target = configurationUnitTarget(unit, wanted, use.configuration);
  return (
    <FlowApiScope scope={side.scope}>
      <ConfigurationUnit
        writer={opened.writer}
        connection={connection}
        session={opened.session}
        compact={label}
        tone={missing ? "none" : undefined}
        note={
          [
            listens ? "Preview listens here too: while Preview runs, Preview and Live both answer each message." : "",
            shared ? `${item?.warningProjectName || "Another Project"} also listens here.` : "",
            extraNote ?? "",
          ]
            .filter(Boolean)
            .join(" ") || undefined
        }
        target={target}
        nextPublish={nextPublish}
      />
    </FlowApiScope>
  );
}
