// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, type ReactNode } from "react";
import type { FlowApiScopeBinding } from "@/lib/flow/FlowApiProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { connectionKey, connectorDisplayName } from "@/lib/api/flow/connector";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { cx } from "@/components/ui/cx";
import {
  HEAD_CELLS,
  HEAD_COLUMNS,
  HEAD_ONE_COLUMN,
  SETUP_HINTS,
} from "@/components/flow/connections/model/connection-words";
import { ReleaseUnused } from "./ReleaseUnused";
import { type SetupItem, type SetupSideName, type SetupTable } from "@/components/flow/connections/model/row-model";
import { useReplyChannelDefaults } from "@/components/flow/connections/model/setting-defaults";
import { SetupRow } from "@/components/flow/connections/row/SetupRow";
import {
  useConnectorCatalog,
  useReleaseManifests,
  useScopeSide,
  useTeamSpaceChoices,
} from "@/components/flow/connections/model/use-scope-catalog";
import { VariablesBlock } from "./VariablesBlock";

/** What the bar above the table says from what only the table reads: the connector names. */
export interface SetupNotes {
  /** Each connector's name, by connector ID. */
  connectors: Record<string, string>;
}

/** Each column's state, under its name in the table's head. */
export interface SetupStates {
  /** Preview's: what it lacks, the changes it is being updated with, or what it runs. */
  preview?: ReactNode;
  /** The next publish's: what to set up, what waits, or what is ready to publish. */
  next?: ReactNode;
}

const LABEL = cx("text-[11px] tracking-[0.07em] text-ink-faint uppercase");
const STATE = cx("flex min-w-0 flex-wrap items-baseline gap-x-1.5 gap-y-1 text-[12.5px] leading-snug text-ink");

/** A column's name, with its hover hint, and its state under it; beside it once the head stacks. */
function HeadCell({
  name,
  hint,
  dot,
  state,
  className,
  testId,
}: {
  name: string;
  hint: string;
  dot?: boolean;
  state?: ReactNode;
  className: string;
  testId: string;
}) {
  return (
    <div
      className={cx(
        className,
        "flex min-w-0 flex-col gap-1 @max-[780px]/tool:flex-row @max-[780px]/tool:items-baseline @max-[780px]/tool:gap-2",
      )}
    >
      <span className={cx(LABEL, "flex shrink-0 cursor-help items-center gap-1.5")} title={hint} aria-hidden="true">
        {dot && <span className="size-1.75 rounded-full bg-pass" />}
        {name}
      </span>
      {state && (
        <div className={STATE} role="status" aria-label={name} data-testid={testId}>
          {state}
        </div>
      )}
    </div>
  );
}

/** The table's head: Connection, then each column by name alone, never "Live" on the Preview tab, with its state. */
function Head({ liveTab, states }: { liveTab: boolean; states: SetupStates }) {
  return (
    <div className={liveTab ? HEAD_ONE_COLUMN : HEAD_COLUMNS}>
      <span className={cx(LABEL, "col-start-1 row-start-1 leading-[18px]")} aria-hidden="true">
        Connection
      </span>
      {liveTab ? (
        <HeadCell
          name="Live"
          hint={SETUP_HINTS.live}
          dot
          state={states.next}
          className={HEAD_CELLS.live}
          testId="next-publish-status"
        />
      ) : (
        <>
          <HeadCell
            name="Preview"
            hint={SETUP_HINTS.preview}
            state={states.preview}
            className={HEAD_CELLS.preview}
            testId="preview-status"
          />
          <HeadCell
            name="Next publish"
            hint={SETUP_HINTS.nextPublish}
            state={states.next}
            className={HEAD_CELLS.next}
            testId="next-publish-status"
          />
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
  states = {},
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
  /** Each column's state, under its name. */
  states?: SetupStates;
  /** Takes what the bar says from the table's reads, whenever it changes. */
  onNotes?: (notes: SetupNotes) => void;
}) {
  const liveTab = preview === null;
  const previewSide = useScopeSide("preview", preview, version, items.preview);
  const nextSide = useScopeSide("next", next, version, items.next);
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
        <Head liveTab={liveTab} states={states} />
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
          The Teamspace&apos;s connections ›
        </a>
      )}
      {liveTab && <ReleaseUnused path={next.basePath} catalog={catalog} />}
    </div>
  );
}
