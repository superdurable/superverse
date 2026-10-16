// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useId, useRef, useState, type MouseEvent } from "react";
import { FlowApiScope } from "@/lib/flow/FlowApiProvider";
import { connectorDisplayName, type ConnectionView } from "@/lib/api/flow/connector";
import {
  configurationDraftQueue,
  draftItemAnchor,
  nextPublishItemAnchor,
} from "@/lib/api/flow/draft/configuration-draft";
import { followsPreview, nextPublishSelectionItem } from "@/lib/api/flow/draft/next-publish-writes";
import { selectionDraftItem } from "@/lib/api/flow/draft/scope-writes";
import { focusAnchorWithin, useAnchorRequests } from "@/lib/browser/hash-focus";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { ONE_COLUMN, TWO_COLUMNS, usesWords, WIDE_ONLY } from "@/components/flow/connections/model/connection-words";
import { ConnectionPicker } from "./ConnectionPicker";
import { useAuthorizationReturn } from "./NewConnectionInline";
import { RowBody, type RowInline } from "./RowBody";
import { RowChips } from "./RowChips";
import {
  connectionItemKey,
  rowItemKeys,
  rowTone,
  settingGroups,
  sideConnection,
  sideItem,
  type SetupSide,
  type SetupSideName,
  type SetupTable,
} from "@/components/flow/connections/model/row-model";

/** A connector's logo from the catalog, or its initials on a tile. */
export function ConnectorLogo({ name, svg }: { name: string; svg: string }) {
  if (svg)
    return (
      // A data URI keeps the logo out of the network and the page's markup alike.
      // eslint-disable-next-line @next/next/no-img-element
      <img
        alt=""
        className="size-6 shrink-0 rounded-md"
        src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`}
      />
    );
  return (
    <span
      aria-hidden="true"
      className="grid size-6 shrink-0 place-items-center rounded-md bg-band-1 text-[11px] font-bold text-ink-dim"
    >
      {name.charAt(0).toUpperCase() || "?"}
    </span>
  );
}

/**
 * One connection name of the table: its connector's logo and name and what uses it, its connection on each
 * side, and a fold that opens its body. A row with something missing or worth a look opens by default and
 * stays open once it is set up, and a link to any of its items opens it and focuses that item's control.
 * Unfolding keeps the page where it is.
 */
export function SetupRow({
  connection,
  sides,
  table,
  manifest,
}: {
  /** The connection name as the row's first side reads it. */
  connection: ConnectionView;
  sides: (SetupSide | null)[];
  table: SetupTable;
  manifest: Parameters<typeof settingGroups>[1];
}) {
  const bodyId = useId();
  const rowRef = useRef<HTMLDivElement>(null);
  const keys = rowItemKeys(connection);
  const anchors = new Set(keys.flatMap((key) => [draftItemAnchor(key), nextPublishItemAnchor(key)]));
  // Only something red or amber on a side already read opens a row; a value that only waits does not.
  const attention = sides.some((side) => side?.items && rowTone(side, connection) !== "ok");
  const [open, setOpen] = useState<boolean | null>(null);
  // A row opened because it needed a look stays open once the look is given, so the control the person just
  // set does not fold away under them; only folding it closes it.
  if (open === null && attention) setOpen(true);
  // Live's tab shows every row open, as its one column has room; Preview's starts folded unless it needs a look.
  const shown = open ?? (table.liveTab || attention);
  const [inline, setInline] = useState<RowInline>(null);
  const [requested, setRequested] = useState<string | null>(null);
  useAnchorRequests(
    (id) => anchors.has(id),
    (id) => {
      setOpen(true);
      setRequested(id);
    },
  );
  useEffect(() => {
    if (!requested || !rowRef.current) return;
    const row = rowRef.current;
    const frame = requestAnimationFrame(() => {
      focusAnchorWithin(row, requested);
      setRequested(null);
    });
    return () => cancelAnimationFrame(frame);
  }, [requested, shown]);

  // A connection a side created here, by key or by a sign-in that returned, is selected for that side.
  const select = (name: SetupSideName, connectionId: string) => {
    const side = sides.find((entry) => entry?.name === name);
    const own = side && sideConnection(side, connection);
    if (!side || !own) return;
    const queue = configurationDraftQueue(side.scope.basePath);
    if (name === "next") {
      const item = nextPublishSelectionItem(
        side.catalog,
        own,
        sideItem(side, connectionItemKey(own)).item?.state === "STATE_FOLLOWS_PREVIEW",
      );
      const current = queue.valueOf(item.key);
      if (followsPreview(current) || current !== connectionId) queue.save(item, () => connectionId, true);
    } else queue.save(selectionDraftItem(side.catalog, own), () => connectionId, true);
  };
  const [returned, setReturned] = useState("");
  const onReturn = (name: SetupSideName) => (outcome: { connectionId: string } | { problem: string }) => {
    setOpen(true);
    if ("connectionId" in outcome) {
      setReturned("");
      void table.reloadChoices().then(() => select(name, outcome.connectionId));
    } else setReturned(outcome.problem);
  };
  useAuthorizationReturn(connection, "preview", onReturn("preview"));
  useAuthorizationReturn(connection, "next", onReturn("next"));

  const name = connectorDisplayName(connection);
  const groups = settingGroups(connection, manifest);
  const entry = table.catalog.get(connection.connectorId);
  const nameOf = (id: string) => {
    const list = table.choices?.get(connection.connectorId);
    return (typeof list === "object" && list.find((choice) => choice.connectionId === id)?.name) || "a connection";
  };
  // A press on the row's own space folds it; one on a control or its menu does what the control does.
  const toggleFromRow = (event: MouseEvent<HTMLDivElement>) => {
    if ((event.target as HTMLElement).closest("button, a, input, [role=listbox], [data-cell]")) return;
    setOpen(!shown);
  };
  return (
    <div
      ref={rowRef}
      id={draftItemAnchor(connectionItemKey(connection))}
      className="border-b border-line-faint last:border-b-0"
      data-testid="setup-connection"
      data-connection-name={connection.connectionName}
      data-open={shown}
    >
      <div
        className={cx(
          table.liveTab ? ONE_COLUMN : TWO_COLUMNS,
          "min-h-13.5 cursor-pointer py-2 text-[13px] hover:bg-hover",
          shown && "bg-raised",
        )}
        onClick={toggleFromRow}
      >
        <span className="flex min-w-0 items-center gap-2.5">
          <ConnectorLogo name={name} svg={entry?.logoSvg ?? ""} />
          <span className="min-w-0">
            <b className="block truncate font-semibold text-ink">{name}</b>
            <small className="block truncate text-[12px] text-ink-faint" title={connection.connectionName}>
              {usesWords(connection) || "no step yet"}
            </small>
            {returned && (
              <small className="block text-[12px] text-fail" role="alert">
                {returned}
              </small>
            )}
          </span>
        </span>
        {sides.map((side, index) => {
          const own = side && sideConnection(side, connection);
          return (
            <span
              key={side?.name ?? index}
              data-cell
              id={
                side?.name === "next" && !table.liveTab
                  ? nextPublishItemAnchor(connectionItemKey(connection))
                  : undefined
              }
              data-anchor={
                side?.name === "next" && table.liveTab
                  ? nextPublishItemAnchor(connectionItemKey(connection))
                  : undefined
              }
              className={cx(
                "min-w-0",
                table.liveTab
                  ? "@max-[520px]/tool:col-span-full @max-[520px]/tool:row-start-2 @max-[520px]/tool:pl-8.5"
                  : WIDE_ONLY,
              )}
            >
              {side && own ? (
                <FlowApiScope scope={side.scope}>
                  <ConnectionPicker
                    side={side}
                    connection={own}
                    table={table}
                    onNew={() => {
                      setOpen(true);
                      setInline({ kind: "new", side: side.name });
                    }}
                  />
                </FlowApiScope>
              ) : (
                <span className="text-[12px] text-ink-faint">Reading…</span>
              )}
            </span>
          );
        })}
        {!table.liveTab && <RowChips sides={sides} connection={connection} nameOf={nameOf} />}
        <button
          type="button"
          aria-expanded={shown}
          aria-controls={shown ? bodyId : undefined}
          aria-label={`${shown ? "Fold" : "Unfold"} ${name}`}
          onClick={() => setOpen(!shown)}
          className="grid size-5.5 place-items-center rounded text-ink-faint hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none"
        >
          <Glyph name="chevronDown" className={cx("size-4 transition-transform", shown && "rotate-180")} />
        </button>
      </div>
      {shown && (
        <div id={bodyId}>
          <RowBody
            connection={connection}
            sides={sides}
            groups={groups}
            table={table}
            inline={inline}
            setInline={setInline}
            onCreated={select}
          />
        </div>
      )}
    </div>
  );
}
