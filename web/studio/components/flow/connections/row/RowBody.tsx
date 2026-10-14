// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState, type ReactNode } from "react";
import { FlowApiScope } from "@/lib/flow/FlowApiProvider";
import { CONNECTOR_REQUEST_FAILED, type ConnectionView } from "@/lib/api/flow/connector";
import { draftItemAnchor, nextPublishItemAnchor } from "@/lib/api/flow/draft/configuration-draft";
import { getTeamSpaceConnection } from "@/lib/api/flow/team-space-connection";
import { errorMessage } from "@/lib/api/transport/api-error";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import {
  CAPTION,
  NARROW_GRID,
  ONE_COLUMN_SETTING,
  ONE_COLUMN_SETTING_NAME,
  SETTING_NAME,
  TWO_COLUMN_SETTING,
} from "@/components/flow/connections/model/connection-words";
import { ConnectionPicker } from "./ConnectionPicker";
import { ConnectionSettingsInline, NewConnectionInline } from "./NewConnectionInline";
import {
  connectionItemKey,
  endpointBoundReason,
  sideConnection,
  sideItem,
  type SettingGroup,
  type SetupSide,
  type SetupSideName,
  type SetupTable,
} from "@/components/flow/connections/model/row-model";
import { sameChannelAs } from "@/components/flow/connections/model/setting-defaults";
import { SettingCell } from "./SettingCell";

const CAPTIONS = { preview: "Preview", next: "Next publish" } as const;

/** What a row's body has open inline: a side's new connection, or the editor of a side's connection. */
export type RowInline = { kind: "new"; side: SetupSideName } | { kind: "edit"; side: SetupSideName } | null;

/** One line of the body: its name, then a cell per side, each captioned once the panel is narrow. */
function Line({
  table,
  name,
  sides,
  cell,
  className,
}: {
  table: SetupTable;
  name: ReactNode;
  sides: (SetupSide | null)[];
  cell: (side: SetupSide) => ReactNode;
  className?: string;
}) {
  return (
    <div className={cx(table.liveTab ? ONE_COLUMN_SETTING : TWO_COLUMN_SETTING, className)}>
      <span className={table.liveTab ? ONE_COLUMN_SETTING_NAME : SETTING_NAME}>{name}</span>
      {sides.map((side, index) => (
        <span key={side?.name ?? index} className="flex min-w-0 flex-col gap-0.75">
          {!table.liveTab && side && <small className={CAPTION}>{CAPTIONS[side.name]}</small>}
          {side ? cell(side) : <span className="text-[12px] text-ink-faint">Reading…</span>}
        </span>
      ))}
      {!table.liveTab && <span className="@max-[780px]/tool:hidden" />}
    </div>
  );
}

/** A side's selected TeamSpace connection in a few words, with Edit, which opens its editor inline. */
function SettingsCell({
  side,
  connection,
  table,
  open,
  onEdit,
}: {
  side: SetupSide;
  connection: ConnectionView;
  table: SetupTable;
  open: boolean;
  onEdit: () => void;
}) {
  const own = sideConnection(side, connection);
  const id = own?.sharedConnectionId ?? "";
  if (!id) return <span className="text-[12px] text-ink-faint">No connection yet</span>;
  const list = table.choices?.get(connection.connectorId);
  const chosen = typeof list === "object" ? list.find((choice) => choice.connectionId === id) : undefined;
  const following = sideItem(side, connectionItemKey(connection)).item?.state === "STATE_FOLLOWS_PREVIEW";
  const facts = following
    ? "the same connection as Preview"
    : [
        chosen?.name,
        chosen?.clientSource === "CONNECTION_CLIENT_SOURCE_PLATFORM" ? "Superverse's app" : "",
        own?.status === "Ready" ? "" : "sign in again",
      ]
        .filter(Boolean)
        .join(" · ");
  return (
    <span className="flex min-w-0 items-center gap-1 text-[12px] whitespace-nowrap text-ink-faint">
      <span className="min-w-0 truncate">{facts || "a TeamSpace connection"}</span>
      <Button
        variant="quiet"
        size="sm"
        className="text-accent"
        aria-expanded={open}
        aria-label={`${open ? "Close" : "Edit"} the connection ${CAPTIONS[side.name].toLowerCase()} uses`}
        onClick={onEdit}
      >
        {open ? "Close" : "Edit"}
      </Button>
    </span>
  );
}

/**
 * Under a row: on a narrow panel, the connection on each side; then Connection settings, an Edit for each
 * side that opens the TeamSpace connection's editor inline, or the New connection form a dropdown opened;
 * then each trigger and Step that has settings, each setting with a control per side.
 */
export function RowBody({
  connection,
  sides,
  groups,
  table,
  inline,
  setInline,
  onCreated,
}: {
  connection: ConnectionView;
  sides: (SetupSide | null)[];
  groups: SettingGroup[];
  table: SetupTable;
  inline: RowInline;
  setInline: (inline: RowInline) => void;
  /** Selects a connection the row created for a side. */
  onCreated: (side: SetupSideName, connectionId: string) => void;
}) {
  const [editing, setEditing] = useState<{
    side: SetupSideName;
    existing: Parameters<typeof ConnectionSettingsInline>[0]["existing"];
  } | null>(null);
  const [problem, setProblem] = useState("");
  const editingSide = inline?.kind === "edit" ? inline.side : null;
  const editedId = editingSide
    ? (sideConnection(sides.find((side) => side?.name === editingSide) ?? null, connection)?.sharedConnectionId ?? "")
    : "";
  useEffect(() => {
    if (!editingSide || !editedId) return;
    const controller = new AbortController();
    getTeamSpaceConnection(table.teamSpaceId, editedId, controller.signal).then(
      (existing) => !controller.signal.aborted && setEditing({ side: editingSide, existing }),
      (reason: unknown) => !controller.signal.aborted && setProblem(errorMessage(reason, CONNECTOR_REQUEST_FAILED)),
    );
    return () => controller.abort();
  }, [editingSide, editedId, table.teamSpaceId]);
  const newSide = inline?.kind === "new" ? (sides.find((side) => side?.name === inline.side) ?? null) : null;
  const newRefusal =
    newSide?.name === "next" &&
    sideItem(newSide, connectionItemKey(connection)).item?.followRefusal ===
      "CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND";
  return (
    <div className="border-b border-line-soft bg-raised pt-0.5 pb-2.5 last:border-b-0" data-testid="setup-row-body">
      {!table.liveTab && (
        <Line
          table={table}
          className={NARROW_GRID}
          name="Connection"
          sides={sides}
          cell={(side) => {
            const own = sideConnection(side, connection);
            return own ? (
              <span
                data-anchor={
                  side.name === "next"
                    ? nextPublishItemAnchor(connectionItemKey(connection))
                    : draftItemAnchor(connectionItemKey(connection))
                }
              >
                <FlowApiScope scope={side.scope}>
                  <ConnectionPicker
                    side={side}
                    connection={own}
                    table={table}
                    onNew={() => setInline({ kind: "new", side: side.name })}
                  />
                </FlowApiScope>
              </span>
            ) : null;
          }}
        />
      )}
      <Line
        table={table}
        name="Connection settings"
        sides={sides}
        cell={(side) => (
          <SettingsCell
            side={side}
            connection={connection}
            table={table}
            open={editingSide === side.name}
            onEdit={() => {
              setEditing(null);
              setProblem("");
              setInline(editingSide === side.name ? null : { kind: "edit", side: side.name });
            }}
          />
        )}
      />
      {problem && (
        <p className="mx-3.5 text-[12px] text-fail" role="alert">
          {problem}
        </p>
      )}
      {editing && editing.side === editingSide && (
        <ConnectionSettingsInline
          teamSpaceId={table.teamSpaceId}
          connection={connection}
          existing={editing.existing}
          side={editing.side}
          onDone={async () => {
            setInline(null);
            await table.reloadChoices();
          }}
        />
      )}
      {newSide && (
        <NewConnectionInline
          teamSpaceId={table.teamSpaceId}
          connection={connection}
          side={newSide.name}
          endpointBound={newRefusal ? endpointBoundReason(table.catalog.get(connection.connectorId), connection) : ""}
          address={table.addresses[newSide.name]}
          onSaved={async (id) => {
            setInline(null);
            await table.reloadChoices();
            if (id) onCreated(newSide.name, id);
          }}
          onCancel={() => setInline(null)}
        />
      )}
      {groups.map((group) => (
        <div key={group.key} id={draftItemAnchor(group.key)} data-testid="setup-setting-group">
          <div className="min-h-7.5 truncate px-3.5 pt-1.5 pl-8.5 text-[12.5px] text-ink-dim" title={group.hint}>
            <b className="font-semibold text-ink">{group.title}</b>
            {group.detail && <> · {group.detail}</>}
          </div>
          {group.problem ? (
            <p className="px-3.5 pb-1 pl-12 text-[12px] text-fail" role="alert">
              {group.problem}
            </p>
          ) : (
            group.units.map((unit, index) => (
              <Line
                key={unit.id}
                table={table}
                name={
                  <>
                    {unit.label}
                    {unit.required && <span className="ml-1.25 text-[11px] text-ink-faint">required</span>}
                  </>
                }
                sides={sides}
                cell={(side) => {
                  const own = sideConnection(side, connection);
                  if (!own) return null;
                  const start =
                    group.use.kind === "trigger" &&
                    sameChannelAs(side, own, group.use.triggerName, group.use.bindingName);
                  const startTitle =
                    start &&
                    (groups.find((other) => other.use.kind === "trigger" && other.use.triggerName === start)?.title ??
                      start);
                  return (
                    <span
                      id={side.name === "next" && index === 0 ? nextPublishItemAnchor(group.key) : undefined}
                      className="min-w-0"
                    >
                      <SettingCell
                        side={side}
                        connection={own}
                        group={group}
                        unit={unit}
                        live={table.live}
                        note={startTitle ? `Same channel as “${startTitle}”.` : undefined}
                      />
                    </span>
                  );
                }}
              />
            ))
          )}
        </div>
      ))}
    </div>
  );
}
