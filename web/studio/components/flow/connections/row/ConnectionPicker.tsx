// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useConfigurationDraftQueue } from "@/lib/flow/FlowApiProvider";
import { connectorDisplayName, type ConnectionView } from "@/lib/api/flow/connector";
import { selectionDraftItem } from "@/lib/api/flow/draft/scope-writes";
import { FOLLOW_PREVIEW, followsPreview, nextPublishSelectionItem } from "@/lib/api/flow/draft/next-publish-writes";
import type { TeamSpaceConnection } from "@/lib/api/flow/team-space-connection";
import { useDraftSnapshot } from "@/components/flow/connector/native/DraftItemNote";
import { Dropdown, type DropdownItem, type DropdownTone } from "@/components/ui/menu/Dropdown";
import { connectionWords, isWorking, needsSignIn } from "@/components/flow/connections/model/connection-words";
import {
  connectionItemKey,
  endpointBoundReason,
  sideItem,
  SIDE_WORDS,
  type SetupSide,
  type SetupTable,
} from "@/components/flow/connections/model/row-model";

/** The option that makes the next publish's selection follow Preview's; no TeamSpace connection has this ID. */
const SAME_AS_PREVIEW = "\u0000same-as-preview";

const CREDENTIAL_WORDS: Record<string, string> = {
  CREDENTIAL_STATUS_READY: "connected",
  CREDENTIAL_STATUS_AUTHORIZING: "not signed in",
  CREDENTIAL_STATUS_REAUTHORIZATION_REQUIRED: "sign in again",
};

/** The name of the TeamSpace connection an item's JSON value selects, or what it says without one. */
function selectedName(json: string, choices: readonly TeamSpaceConnection[], none: string): string {
  let id = "";
  try {
    id = json ? ((JSON.parse(json) as { teamSpaceConnectionId?: string }).teamSpaceConnectionId ?? "") : "";
  } catch {
    return none;
  }
  return choices.find((choice) => choice.connectionId === id)?.name ?? (id ? "another connection" : none);
}

/**
 * The connection a connection name uses on one side: the TeamSpace's connections of its connector, major
 * version and method, and New connection…, which `onNew` opens in the row. The next publish's starts with
 * "Same as Preview", unless the connector is tied to one address, which the menu note then says. Green only
 * marks a connection that works; red, nothing that works yet; a blue dot, a value Live does not run yet.
 */
export function ConnectionPicker({
  side,
  connection,
  table,
  onNew,
}: {
  side: SetupSide;
  /** The side's view of the connection name. */
  connection: ConnectionView;
  table: SetupTable;
  onNew: () => void;
}) {
  const queue = useConfigurationDraftQueue();
  useDraftSnapshot(queue);
  const key = connectionItemKey(connection);
  const { item, missing } = sideItem(side, key);
  const next = side.name === "next";
  const refusal = item?.followRefusal === "CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND";
  const canFollow = next && !refusal;
  const nextItem = nextPublishSelectionItem(side.catalog, connection, item?.state === "STATE_FOLLOWS_PREVIEW");
  const plainItem = selectionDraftItem(side.catalog, connection);
  const stored = queue.valueOf(key) ?? (next ? nextItem.base : plainItem.base);
  const following = canFollow && followsPreview(stored);
  // A follower shows what it resolves to; a pending choice shows itself.
  const value = following || typeof stored !== "string" ? (connection.sharedConnectionId ?? "") : stored;
  const list = table.choices?.get(connection.connectorId);
  const all = typeof list === "object" ? list : [];
  const compatible = all.filter(
    (choice) =>
      choice.authMethodId === (connection.authMethodId ?? "") &&
      (!connection.versionLine || choice.versionLine === connection.versionLine),
  );
  const chosen = all.find((choice) => choice.connectionId === value) ?? null;
  const name = connectorDisplayName(connection);
  const working = Boolean(value) && isWorking(connection);
  const ownTone: DropdownTone = !value || missing ? "none" : needsSignIn(connection) ? "warn" : working ? "ok" : "none";
  // A follower is quiet unless it needs a look, when it shows the connection it resolves to, Preview's.
  const attention = following && Boolean(value) && ownTone !== "ok";
  const tone: DropdownTone = following ? (!value ? "none" : attention ? ownTone : "same") : ownTone;
  const reason = refusal ? endpointBoundReason(table.catalog.get(connection.connectorId), connection) : "";
  const note = [
    attention ? `${chosen?.name ?? "This connection"} is Preview's connection, which the next publish follows.` : "",
    refusal && !value ? `${reason} Set up a connection for Live's own address.` : "",
    refusal && value ? `${reason} Live keeps a connection of its own.` : "",
    value && needsSignIn(connection)
      ? `${chosen?.name ?? "This connection"} needs a new sign-in. Edit it in Connection settings below.`
      : "",
    value && !working && !needsSignIn(connection) ? `This connection: ${connectionWords(connection).words}.` : "",
    typeof list === "string" ? list : "",
  ]
    .filter(Boolean)
    .join(" ");
  const previewName = selectedName(item?.previewValueJson ?? "", all, "Preview has none yet");
  const waiting =
    next && table.live && item?.differsFromLive
      ? `${table.live} runs ${selectedName(item.liveValueJson, all, "no connection")} now. This goes out with the next publish.`
      : undefined;
  const select = (id: string) =>
    next
      ? queue.save(nextItem, () => (id === SAME_AS_PREVIEW ? FOLLOW_PREVIEW : id), true)
      : queue.save(plainItem, () => id, true);
  const items: DropdownItem<string>[] = [
    ...(canFollow
      ? [{ value: SAME_AS_PREVIEW, label: "Same as Preview", sub: previewName }, "separator" as const]
      : []),
    ...(value && !following && !compatible.some((choice) => choice.connectionId === value)
      ? [{ value, label: chosen?.name ?? connection.selectedConnection?.name ?? "The selected connection" }]
      : []),
    ...compatible.map((choice) => ({
      value: choice.connectionId,
      label: choice.name,
      sub: CREDENTIAL_WORDS[choice.credentialStatus] ?? "not signed in",
      tone: choice.credentialStatus === "CREDENTIAL_STATUS_READY" ? ("ok" as const) : ("warn" as const),
    })),
    "separator",
    { action: onNew, label: "New connection…", glyph: "plus" },
  ];
  return (
    <Dropdown
      label={`${name} connection ${connection.connectionName} ${SIDE_WORDS[side.name]}`}
      tone={tone}
      note={note || undefined}
      pending={waiting}
      display={attention ? (chosen?.name ?? "The selected connection") : undefined}
      title={following && value && !attention ? `Same as Preview: ${chosen?.name ?? previewName}` : undefined}
      disabled={!table.teamSpaceId || table.choices === null}
      align={next ? "end" : "start"}
      placeholder={
        table.choices === null
          ? "Loading the TeamSpace's connections…"
          : refusal && next
            ? "Needs its own"
            : "Not connected"
      }
      value={following ? (value ? SAME_AS_PREVIEW : null) : value || null}
      onChange={select}
      items={items}
      data-tone={tone}
      data-testid={`setup-connection-${side.name}`}
    />
  );
}
