// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState } from "react";
import { FlowApiProvider, FlowApiScope, useFlowApiScope } from "@/lib/flow/FlowApiProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { connectionKey, connectorDisplayName, type ConnectionView } from "@/lib/api/flow/connector";
import type { ApplicationEnvironmentField } from "@/lib/api/flow/application-environment";
import { configurationDraftQueue, draftItemKey } from "@/lib/api/flow/draft/configuration-draft";
import { selectionDraftItem } from "@/lib/api/flow/draft/scope-writes";
import { Glyph } from "@/components/ui/Glyph";
import { humanFieldName } from "@/components/flow/connector/manifest/manifest-fields";
import { isWorking, usesWords } from "@/components/flow/connections/model/connection-words";
import {
  connectionItemKey,
  rowItemKeys,
  settingGroups,
  sideConnection,
  sideItem,
  type SetupItem,
  type SetupSide,
  type SetupTable,
} from "@/components/flow/connections/model/row-model";
import { useReplyChannelDefaults } from "@/components/flow/connections/model/setting-defaults";
import {
  useConnectorCatalog,
  useReleaseManifests,
  useScopeSide,
  useTeamSpaceChoices,
} from "@/components/flow/connections/model/use-scope-catalog";
import { ConnectionPicker } from "@/components/flow/connections/row/ConnectionPicker";
import { NewConnectionInline, useAuthorizationReturn } from "@/components/flow/connections/row/NewConnectionInline";
import { SettingCell } from "@/components/flow/connections/row/SettingCell";
import { ConnectorLogo } from "@/components/flow/connections/row/SetupRow";
import { useEnvironment, VariableCell } from "@/components/flow/connections/VariablesBlock";

const ROW = "grid gap-2 border-line-faint py-2.5 not-first:border-t";
const DONE = (
  <span role="img" aria-label="Done" className="shrink-0 text-pass">
    <Glyph name="check" className="size-4" />
  </span>
);

/** What Try it shows of the version's needs: how many still need someone, once they are read. */
export interface ChecklistState {
  missing: number;
}

/**
 * Exactly what Preview's version needs, from Setup's own model: each connection name, with a check once it
 * works, else Setup's connection list, which offers the Teamspace's saved connections first, and its New
 * connection form; then each missing setting of a connected one, and each required variable.
 */
export function TryItChecklist({
  configurationPath,
  ...body
}: {
  /** Preview's configuration host, which Setup's Preview column writes too. */
  configurationPath: string;
} & BodyProps) {
  return (
    <FlowApiProvider key={configurationPath} basePath={configurationPath}>
      <ChecklistBody {...body} />
    </FlowApiProvider>
  );
}

interface BodyProps {
  /** The server's items of Preview's configuration with the gaps that name them; null while they are read. */
  items: Map<string, SetupItem> | null;
  /** Changes when something outside the checklist changed the configuration, so it is read again. */
  version: string;
  /** Preview's address, for a connection tied to one. */
  address: string;
  onState: (state: ChecklistState) => void;
}

function ChecklistBody({ items, version, address, onState }: BodyProps) {
  const { side, error, catalog } = useScopeSide("preview", useFlowApiScope(), version, items);
  const teamSpaceId = useFlowHost().teamSpaceId ?? "";
  const choices = useTeamSpaceChoices(
    teamSpaceId,
    (catalog?.connections ?? []).map((connection) => connection.connectorId),
  );
  const connectors = useConnectorCatalog();
  const manifests = useReleaseManifests(catalog, teamSpaceId);
  useReplyChannelDefaults(side);
  const environment = useEnvironment(side);
  const variables = (environment.snapshot?.fields ?? []).filter((field) => field.required);
  const missing =
    side && items
      ? (catalog?.connections ?? []).filter((connection) => !rowReady(side, connection)).length +
        variables.filter((field) => sideItem(side, variableKey(field)).missing).length
      : 0;
  useEffect(() => onState({ missing }), [missing, onState]);
  if (!catalog || !side)
    return (
      <p className={error ? "text-caption text-fail" : "text-caption text-ink-faint"} role={error ? "alert" : "status"}>
        {error || "Reading what this version needs…"}
      </p>
    );
  if (catalog.connections.length === 0 && variables.length === 0)
    return (
      <p className="flex items-center gap-2 text-body text-ink-dim">
        {DONE}
        This version needs no connection.
      </p>
    );
  const table: SetupTable = {
    teamSpaceId,
    live: null,
    choices: choices.choices,
    reloadChoices: choices.reload,
    catalog: connectors,
    addresses: { preview: address, next: "" },
    liveTab: false,
  };
  return (
    <ul aria-label="What this version needs" data-testid="try-it-checklist">
      {catalog.connections.map((connection) => (
        <ConnectionRow
          key={connectionKey(connection)}
          connection={connection}
          side={side}
          table={table}
          manifest={manifests.get(connection.connectorId) ?? null}
        />
      ))}
      {environment.snapshot &&
        variables.map((field) => (
          <li key={field.name} className={ROW} data-testid="try-it-variable">
            <span className="flex items-center gap-2.5">
              <b className="min-w-0 flex-1 truncate text-body font-semibold text-ink" title={field.name}>
                {humanFieldName(field.name)}
              </b>
              {!sideItem(side, variableKey(field)).missing && DONE}
            </span>
            {sideItem(side, variableKey(field)).missing && environment.snapshot && (
              <VariableCell
                side={side}
                field={field}
                snapshot={environment.snapshot}
                onWritten={environment.setSnapshot}
                live={null}
              />
            )}
          </li>
        ))}
    </ul>
  );
}

const variableKey = (field: ApplicationEnvironmentField) =>
  draftItemKey({ kind: "KIND_ENVIRONMENT", environmentName: field.name });

/** Whether a connection name has a working connection and nothing it holds is missing. */
function rowReady(side: SetupSide, connection: ConnectionView): boolean {
  const own = sideConnection(side, connection) ?? connection;
  return (
    Boolean(own.sharedConnectionId) &&
    isWorking(own) &&
    rowItemKeys(connection).every((key) => !sideItem(side, key).missing)
  );
}

/**
 * One connection name: a check once it works, else Setup's connection list and its New connection form in
 * place; once connected, each setting it still misses. A sign-in that returns here selects what it created.
 */
function ConnectionRow({
  connection,
  side,
  table,
  manifest,
}: {
  connection: ConnectionView;
  side: SetupSide;
  table: SetupTable;
  manifest: Parameters<typeof settingGroups>[1];
}) {
  const own = sideConnection(side, connection) ?? connection;
  const [adding, setAdding] = useState(false);
  const [returned, setReturned] = useState("");
  const select = (connectionId: string) =>
    configurationDraftQueue(side.scope.basePath).save(selectionDraftItem(side.catalog, own), () => connectionId, true);
  useAuthorizationReturn(connection, "preview", (outcome) => {
    if ("connectionId" in outcome) {
      setReturned("");
      void table.reloadChoices().then(() => select(outcome.connectionId));
    } else setReturned(outcome.problem);
  });
  const connected =
    Boolean(own.sharedConnectionId) && isWorking(own) && !sideItem(side, connectionItemKey(connection)).missing;
  const ready = rowReady(side, connection);
  const groups = settingGroups(connection, manifest).filter((group) => sideItem(side, group.key).missing);
  const name = connectorDisplayName(connection);
  const chosen = own.selectedConnection?.name;
  return (
    <li
      className={ROW}
      data-testid="try-it-connection"
      data-connection-name={connection.connectionName}
      data-ready={ready}
    >
      <span className="flex items-center gap-2.5">
        <ConnectorLogo name={name} svg={table.catalog.get(connection.connectorId)?.logoSvg ?? ""} />
        <span className="min-w-0 flex-1">
          <b className="block truncate text-body font-semibold text-ink" title={connection.connectionName}>
            {name}
          </b>
          <small className="block truncate text-caption text-ink-faint">
            {ready
              ? `Connected${chosen ? ` · ${chosen}` : ""}`
              : connected
                ? "Connected · one more setting"
                : usesWords(connection)
                  ? `Used by ${usesWords(connection)}`
                  : "Not connected yet"}
          </small>
        </span>
        {ready && DONE}
      </span>
      {!connected && (
        <FlowApiScope scope={side.scope}>
          <ConnectionPicker side={side} connection={own} table={table} onNew={() => setAdding(true)} />
        </FlowApiScope>
      )}
      {returned && (
        <small className="text-caption text-fail" role="alert">
          {returned}
        </small>
      )}
      {adding && !connected && (
        <NewConnectionInline
          compact
          teamSpaceId={table.teamSpaceId}
          connection={connection}
          side="preview"
          endpointBound=""
          address={table.addresses.preview}
          onSaved={async (id) => {
            setAdding(false);
            await table.reloadChoices();
            if (id) select(id);
          }}
          onCancel={() => setAdding(false)}
        />
      )}
      {connected &&
        groups.flatMap((group) =>
          group.units.map((unit) => (
            <span key={`${group.key}:${unit.id}`} className="grid gap-1" title={group.hint}>
              <span className="text-caption text-ink-dim">
                {unit.label} · {group.title}
              </span>
              <SettingCell side={side} connection={own} group={group} unit={unit} live={null} />
            </span>
          )),
        )}
    </li>
  );
}
