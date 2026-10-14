// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import type { FlowApiScopeBinding } from "@/lib/flow/FlowApiProvider";
import { errorMessage } from "@/lib/api/transport/api-error";
import {
  CONNECTOR_REQUEST_FAILED,
  connectionKey,
  listConnections,
  startConnectorUiSession,
  type ConnectionsResponse,
  type ReleaseManifest,
  type UISessionResponse,
} from "@/lib/api/flow/connector";
import { configurationDraftQueue, type ConfigurationDraftQueue } from "@/lib/api/flow/draft/configuration-draft";
import {
  listConnectorCatalog,
  listTeamSpaceConnections,
  openConnectionForm,
  type ConnectorCatalogEntry,
  type TeamSpaceConnection,
} from "@/lib/api/flow/team-space-connection";
import { projectConnectionWriter, type ProjectConnectionWriter } from "@/lib/api/flow/draft/scope-writes";
import { loadPickerOptions } from "@/components/flow/connector/native/picker-options";
import { isWorking } from "./connection-words";
import { channelOf } from "./setting-defaults";

/** A working connection's UI session and its writer, or why the session did not open. */
export type ConnectionSession = { session: UISessionResponse; writer: ProjectConnectionWriter } | { error: string };

const IDLE_QUEUE = { subscribe: () => () => undefined, snapshot: () => null };

/** The autosave queue of a scope, null without one, and whether it has nothing waiting. */
function useSettledQueue(scope: FlowApiScopeBinding | null): {
  queue: ConfigurationDraftQueue | null;
  settled: boolean;
} {
  const queue = scope ? configurationDraftQueue(scope.basePath) : null;
  const draft = useSyncExternalStore(
    queue?.subscribe ?? IDLE_QUEUE.subscribe,
    queue?.getSnapshot ?? IDLE_QUEUE.snapshot,
    queue?.getSnapshot ?? IDLE_QUEUE.snapshot,
  );
  return { queue, settled: !draft || (draft.status !== "saving" && draft.pending.length === 0) };
}

/**
 * The connection names of one configuration host (`scope`) as it reads them, again whenever `version`
 * changes or the scope's autosave queue settles; a re-read keeps the previous catalog on screen. A failed
 * read keeps it too, with the reason. Without a scope nothing is read.
 */
export function useScopeCatalog(scope: FlowApiScopeBinding | null, version: string) {
  const { queue, settled } = useSettledQueue(scope);
  // Each settled write is a new read: its revision, or the queue's newest one.
  const [reads, setReads] = useState(0);
  const wasSettled = useRef(settled);
  useEffect(() => {
    if (settled && !wasSettled.current) setReads((current) => current + 1);
    wasSettled.current = settled;
  }, [settled]);
  const [state, setState] = useState<{ catalog: ConnectionsResponse | null; error: string }>({
    catalog: null,
    error: "",
  });
  const client = scope?.client ?? null;
  useEffect(() => {
    if (!client) return;
    const controller = new AbortController();
    listConnections(client, controller.signal).then(
      (catalog) => !controller.signal.aborted && setState({ catalog, error: "" }),
      (reason: unknown) =>
        !controller.signal.aborted &&
        setState((current) => ({ catalog: current.catalog, error: errorMessage(reason, CONNECTOR_REQUEST_FAILED) })),
    );
    return () => controller.abort();
  }, [client, version, reads]);
  return { ...state, queue };
}

/**
 * The UI session of each working connection of a scope, opened once per connection, TeamSpace connection
 * and credential, so a save elsewhere neither reopens it nor remounts the forms on it. The writer follows
 * the newest catalog, whose revisions fence its writes.
 */
export function useConnectionSessions(
  scope: FlowApiScopeBinding | null,
  catalog: ConnectionsResponse | null,
): Map<string, ConnectionSession> {
  const client = scope?.client ?? null;
  const working = (catalog?.connections ?? []).filter(isWorking);
  const identity = JSON.stringify(
    working.map((connection) => [
      connectionKey(connection),
      connection.sharedConnectionId,
      connection.credentialRevision,
    ]),
  );
  const [opened, setOpened] = useState<{ identity: string; sessions: Map<string, UISessionResponse | string> }>({
    identity: "",
    sessions: new Map(),
  });
  const latest = useRef(catalog);
  useEffect(() => {
    latest.current = catalog;
  });
  useEffect(() => {
    const current = latest.current;
    if (!current || !client) return;
    const controller = new AbortController();
    const connections = current.connections.filter(isWorking);
    void Promise.all(
      connections.map((connection) =>
        startConnectorUiSession(client, current, connection, controller.signal).then(
          (session): [string, UISessionResponse | string] => [connectionKey(connection), session],
          (reason: unknown): [string, UISessionResponse | string] => [
            connectionKey(connection),
            errorMessage(reason, CONNECTOR_REQUEST_FAILED),
          ],
        ),
      ),
    ).then((entries) => !controller.signal.aborted && setOpened({ identity, sessions: new Map(entries) }));
    return () => controller.abort();
  }, [client, identity]);
  return useMemo(() => {
    const sessions = new Map<string, ConnectionSession>();
    if (!catalog || !client || opened.identity !== identity) return sessions;
    for (const connection of catalog.connections) {
      const session = opened.sessions.get(connectionKey(connection));
      if (session === undefined) continue;
      sessions.set(
        connectionKey(connection),
        typeof session === "string"
          ? { error: session }
          : { session, writer: projectConnectionWriter(client, catalog, connection, session.sessionNonce ?? "") },
      );
    }
    return sessions;
  }, [client, catalog, identity, opened]);
}

/**
 * The release manifest of each connector the catalog declares, read through the TeamSpace's connection
 * form, so a trigger reads in its connector's words before it is connected.
 */
export function useReleaseManifests(catalog: ConnectionsResponse | null, teamSpaceId: string) {
  const releases = [
    ...new Map(
      (catalog?.connections ?? [])
        .filter((connection) => (connection.triggerUses ?? []).length > 0)
        .map((connection) => [
          connection.connectorId,
          {
            connectorId: connection.connectorId,
            modulePath: connection.modulePath ?? "",
            version: connection.moduleVersion ?? "",
          },
        ]),
    ).values(),
  ];
  const key = teamSpaceId ? JSON.stringify(releases) : "";
  const [read, setRead] = useState<{ key: string; manifests: Map<string, ReleaseManifest> } | null>(null);
  useEffect(() => {
    if (!key) return;
    const controller = new AbortController();
    const wanted = JSON.parse(key) as { connectorId: string; modulePath: string; version: string }[];
    void Promise.all(
      wanted.map((release) =>
        openConnectionForm(teamSpaceId, release, controller.signal).then(
          (form): [string, ReleaseManifest] | null => [release.connectorId, form.manifest],
          // Without the manifest a trigger keeps its own name.
          () => null,
        ),
      ),
    ).then((entries) => {
      if (!controller.signal.aborted) setRead({ key, manifests: new Map(entries.filter((entry) => entry !== null)) });
    });
    return () => controller.abort();
  }, [key, teamSpaceId]);
  return read?.key === key ? read.manifests : new Map<string, ReleaseManifest>();
}

/** The TeamSpace's connections of each connector, by connector ID, or why they could not be read. */
export type TeamSpaceChoices = Map<string, TeamSpaceConnection[] | string>;

/**
 * The TeamSpace's connections of each connector of `connectorIds`, which every connection dropdown of
 * the table offers; `reload` reads them again, as after a new connection.
 */
export function useTeamSpaceChoices(teamSpaceId: string, connectorIds: readonly string[]) {
  const key = [...new Set(connectorIds)].sort().join(",");
  const [choices, setChoices] = useState<{ key: string; choices: TeamSpaceChoices } | null>(null);
  const load = useCallback(
    (signal: AbortSignal) =>
      Promise.all(
        key
          .split(",")
          .filter(Boolean)
          .map((connectorId) =>
            listTeamSpaceConnections(teamSpaceId, connectorId, signal).then(
              (list): [string, TeamSpaceConnection[] | string] => [connectorId, list],
              (reason: unknown): [string, TeamSpaceConnection[] | string] => [
                connectorId,
                errorMessage(reason, CONNECTOR_REQUEST_FAILED),
              ],
            ),
          ),
      ).then((entries) => !signal.aborted && setChoices({ key, choices: new Map(entries) })),
    [key, teamSpaceId],
  );
  useEffect(() => {
    if (!teamSpaceId) return;
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load, teamSpaceId]);
  return {
    choices: choices?.key === key ? choices.choices : null,
    reload: () => load(new AbortController().signal).then(() => undefined),
  };
}

/**
 * The connector catalog by connector ID, for each row's logo and why a connector tied to one address
 * cannot follow Preview; empty until it is read or when it cannot be.
 */
export function useConnectorCatalog(): Map<string, ConnectorCatalogEntry> {
  const [entries, setEntries] = useState<Map<string, ConnectorCatalogEntry>>(new Map());
  useEffect(() => {
    const controller = new AbortController();
    listConnectorCatalog(controller.signal).then(
      (catalog) =>
        !controller.signal.aborted && setEntries(new Map(catalog.map((entry) => [entry.connectorId, entry]))),
      // Without the catalog a row shows its connector's initials and a generic reason.
      () => undefined,
    );
    return () => controller.abort();
  }, []);
  return entries;
}

/**
 * The channel names of each connection whose session lists channels, by connection key and channel ID,
 * read once per connection and UI session; a channel the list lacks keeps its ID.
 */
export function useChannelNames(catalog: ConnectionsResponse | null, sessions: Map<string, ConnectionSession>) {
  const listing = (catalog?.connections ?? []).filter((connection) =>
    (connection.triggerUses ?? []).some((use) => channelOf(use) !== ""),
  );
  const key = JSON.stringify(
    listing.map((connection) => {
      const opened = sessions.get(connectionKey(connection));
      return [connectionKey(connection), opened && "session" in opened ? opened.session.sessionNonce : ""];
    }),
  );
  const latest = useRef({ listing, sessions });
  useEffect(() => {
    latest.current = { listing, sessions };
  });
  const [names, setNames] = useState<Map<string, Map<string, string>>>(new Map());
  useEffect(() => {
    const controller = new AbortController();
    for (const connection of latest.current.listing) {
      const opened = latest.current.sessions.get(connectionKey(connection));
      if (!opened || !("session" in opened)) continue;
      const client = {
        executeProviderCommand: (commandId: string, _capability: string, parameters: Record<string, string> = {}) =>
          opened.writer.invokeCommand(commandId, parameters, controller.signal),
      };
      loadPickerOptions({ unit: "channelPicker", spreadsheetId: "", manifest: opened.session.manifest }, client, {
        configuration: {},
      }).then(
        (result) =>
          !controller.signal.aborted &&
          setNames((current) =>
            new Map(current).set(
              connectionKey(connection),
              new Map(result.models.map((option) => [option.id, option.label ?? option.id])),
            ),
          ),
        // An unlisted channel shows its ID.
        () => undefined,
      );
    }
    return () => controller.abort();
  }, [key]);
  return names;
}
