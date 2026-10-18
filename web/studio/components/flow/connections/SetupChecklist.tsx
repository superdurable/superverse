// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowApiScopeBinding } from "@/lib/flow/FlowApiProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { connectionKey } from "@/lib/api/flow/connector";
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

const LABEL = cx("text-[11px] leading-[18px] tracking-[0.07em] text-ink-faint uppercase");

/** A column's name, with its hover hint. */
function HeadCell({ name, hint, dot, className }: { name: string; hint: string; dot?: boolean; className: string }) {
  return (
    <span
      className={cx(LABEL, className, "flex min-w-0 cursor-help items-center gap-1.5")}
      title={hint}
      aria-hidden="true"
    >
      {dot && <span className="size-1.75 rounded-full bg-pass" />}
      {name}
    </span>
  );
}

/**
 * The table's head: Connection, then each column by name, never "Live" on the Preview tab. The rows' colours
 * say what each column lacks, so the head says nothing more.
 */
function Head({ liveTab }: { liveTab: boolean }) {
  return (
    <div className={liveTab ? HEAD_ONE_COLUMN : HEAD_COLUMNS}>
      <span className={LABEL} aria-hidden="true">
        Connection
      </span>
      {liveTab ? (
        <HeadCell name="Live" hint={SETUP_HINTS.live} dot className={HEAD_CELLS.live} />
      ) : (
        <>
          <HeadCell name="Preview" hint={SETUP_HINTS.preview} className={HEAD_CELLS.preview} />
          <HeadCell name="Next publish" hint={SETUP_HINTS.nextPublish} className={HEAD_CELLS.next} />
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
          The Teamspace&apos;s connections ›
        </a>
      )}
      {liveTab && <ReleaseUnused path={next.basePath} catalog={catalog} />}
    </div>
  );
}
