// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useMemo } from "react";
import type { FlowApiScopeBinding } from "@/lib/flow/FlowApiProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { connectionKey, connectorDisplayName, type ConnectionsResponse } from "@/lib/api/flow/connector";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { cx } from "@/components/ui/cx";
import { ONE_COLUMN, SETUP_HINTS, TWO_COLUMNS, WIDE_ONLY } from "@/components/flow/connections/model/connection-words";
import { ReleaseUnused } from "./ReleaseUnused";
import {
  type SetupItem,
  type SetupSide,
  type SetupSideName,
  type SetupTable,
} from "@/components/flow/connections/model/row-model";
import { useReplyChannelDefaults } from "@/components/flow/connections/model/setting-defaults";
import { SetupRow } from "@/components/flow/connections/row/SetupRow";
import {
  useConnectionSessions,
  useConnectorCatalog,
  useReleaseManifests,
  useScopeCatalog,
  useTeamSpaceChoices,
} from "@/components/flow/connections/model/use-scope-catalog";
import { VariablesBlock } from "./VariablesBlock";

/** One column's reads: its catalog, its UI sessions and the server's items, as a side of the table. */
function useSide(
  name: SetupSideName,
  scope: FlowApiScopeBinding | null,
  version: string,
  items: Map<string, SetupItem> | null,
): { side: SetupSide | null; error: string; catalog: ConnectionsResponse | null } {
  const { catalog, error, queue } = useScopeCatalog(scope, version);
  const sessions = useConnectionSessions(scope, catalog);
  const side = useMemo(
    () => (scope && catalog && queue ? { name, scope, queue, catalog, sessions, items } : null),
    [name, scope, catalog, queue, sessions, items],
  );
  return { side, error, catalog };
}

/** What the bar above the table says from what only the table reads: the connector names. */
export interface SetupNotes {
  /** Each connector's name, by connector ID. */
  connectors: Record<string, string>;
}

/** The table's head: Connection, then each column by name alone, never "Live" on the Preview tab; the icons are on the bar's buttons above. */
function Head({ liveTab }: { liveTab: boolean }) {
  return (
    <div
      className={cx(
        liveTab ? ONE_COLUMN : TWO_COLUMNS,
        "h-9 border-b border-line-soft text-[11px] tracking-[0.07em] text-ink-faint uppercase",
      )}
      aria-hidden="true"
    >
      <span>Connection</span>
      {liveTab ? (
        <span className="flex cursor-help items-center gap-1.5 @max-[520px]/tool:hidden" title={SETUP_HINTS.live}>
          <span className="size-1.75 rounded-full bg-pass" />
          Live
        </span>
      ) : (
        <>
          <span className={cx(WIDE_ONLY, "cursor-help")} title={SETUP_HINTS.preview}>
            Preview
          </span>
          <span className={cx(WIDE_ONLY, "cursor-help")} title={SETUP_HINTS.nextPublish}>
            Next publish
          </span>
        </>
      )}
    </div>
  );
}

/**
 * Setup's table: one row per connection name, with its triggers and Step settings that have settings under
 * it, then the variables. On the Preview tab each setting has a Preview control and a Next publish control,
 * the next-publish layer that the Live tab shows alone; on the Live tab `preview` is null. Every change saves
 * as it is made through its column's own autosave queue. Runs inside the next publish's FlowApiProvider.
 */
export function SetupChecklist({
  preview,
  next,
  version,
  items,
  live,
  addresses,
  framed = false,
  onNotes,
}: {
  /** Preview's configuration host, null on the Live tab. */
  preview: FlowApiScopeBinding | null;
  /** The next-publish layer's configuration host. */
  next: FlowApiScopeBinding;
  /** Changes when something outside the table changed the configuration, so it is read again. */
  version: string;
  /** The server's items of each side, with the gaps that name them. */
  items: Record<SetupSideName, Map<string, SetupItem> | null>;
  /** Live's name for the waiting dot's words, "Live V12"; null before the first publish. */
  live: string | null;
  addresses: Record<SetupSideName, string>;
  /** Drawn under a banner, which takes the top edge. */
  framed?: boolean;
  /** Takes what the bar says from the table's reads, whenever it changes. */
  onNotes?: (notes: SetupNotes) => void;
}) {
  const liveTab = preview === null;
  const previewSide = useSide("preview", preview, version, items.preview);
  const nextSide = useSide("next", next, version, items.next);
  const primary = liveTab ? nextSide : previewSide;
  const teamSpaceId = useFlowHost().teamSpaceId ?? "";
  const catalog = primary.catalog;
  const choices = useTeamSpaceChoices(
    teamSpaceId,
    (catalog?.connections ?? []).map((connection) => connection.connectorId),
  );
  const connectors = useConnectorCatalog();
  const manifests = useReleaseManifests(catalog, teamSpaceId);
  useReplyChannelDefaults(previewSide.side);
  const notes = JSON.stringify({
    connectors: Object.fromEntries(
      (catalog?.connections ?? []).map((connection) => [connection.connectorId, connectorDisplayName(connection)]),
    ),
  });
  useEffect(() => onNotes?.(JSON.parse(notes) as SetupNotes), [notes, onNotes]);
  const sides = liveTab ? [nextSide.side] : [previewSide.side, nextSide.side];
  const table: SetupTable = {
    teamSpaceId,
    live,
    choices: choices.choices,
    reloadChoices: choices.reload,
    catalog: connectors,
    addresses,
    liveTab,
  };
  const errors = [...new Set([previewSide.error, nextSide.error].filter(Boolean))];
  if (!catalog)
    return (
      <p
        className={cx("px-1 py-6 text-[13px]", primary.error ? "text-fail" : "text-ink-faint")}
        role={primary.error ? "alert" : "status"}
      >
        {primary.error || "Reading what this version declares…"}
      </p>
    );
  return (
    <div className="grid gap-0" data-testid="setup-checklist" data-revision={catalog.configurationRevision}>
      {errors.map((error) => (
        <p key={error} className="mb-2 text-[12px] text-fail" role="alert">
          {error}
        </p>
      ))}
      <div
        className={cx("border border-line-soft bg-panel", framed ? "rounded-b-[10px] border-t-0" : "rounded-[10px]")}
      >
        <Head liveTab={liveTab} />
        {catalog.connections.length === 0 ? (
          <p className="px-3.5 py-3 text-[13px] text-ink-faint">No step uses an outside service yet.</p>
        ) : (
          catalog.connections.map((connection) => (
            <SetupRow
              key={connectionKey(connection)}
              connection={connection}
              sides={sides}
              table={table}
              manifest={manifests.get(connection.connectorId) ?? null}
            />
          ))
        )}
      </div>
      <VariablesBlock sides={sides} liveTab={liveTab} live={live} />
      {teamSpaceId && (
        <a
          className="mt-2.5 justify-self-start text-[12.5px] text-accent hover:underline"
          href={studioRoutes.teamSpaceConnections(teamSpaceId)}
        >
          The TeamSpace&apos;s connections ›
        </a>
      )}
      {liveTab && <ReleaseUnused path={next.basePath} catalog={catalog} />}
    </div>
  );
}
