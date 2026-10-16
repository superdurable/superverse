// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { draftItemKey, type DraftItem } from "@/lib/api/flow/draft/configuration-draft";
import {
  connectionKey,
  invokeProviderCommand,
  listConnections,
  writeConnector,
  type CommandRunner,
  type ConnectionsResponse,
  type ConnectionView,
} from "@/lib/api/flow/connector";
import {
  getApplicationEnvironment,
  saveApplicationEnvironment,
  type ApplicationEnvironmentSnapshot,
  type ApplicationEnvironmentWrite,
} from "@/lib/api/flow/application-environment";
import type { FlowApiClient } from "@/lib/api/transport/flow-api-client";

/*
 * The writes of one Project scope's configuration items, each fenced on the revision the autosave queue
 * names and answering the revision it made: a connection name's Teamspace connection, and the settings
 * of a Flow operation or trigger binding that uses it.
 */

/** The Flow operation, by its Step, whose settings use a connection. */
export type ConnectorOperation = { operationId: string; flowType: string; stepType: string };
export type ConnectorTriggerBinding = { triggerName: string; bindingName: string };

/** A connection name of a Project scope as its forms use it: the catalog it was read from and its commands. */
export interface ProjectConnectionWriter extends CommandRunner {
  catalog: ConnectionsResponse;
  connection: ConnectionView;
}

/** What a configuration write answers: the revision it made. */
interface ConfigurationWritten {
  configurationRevision: string;
}

/**
 * The writer of one connection name in a Project scope's configuration host, fenced by the catalog the page
 * read; its commands run under the UI session `sessionNonce`.
 */
export function projectConnectionWriter(
  client: FlowApiClient,
  catalog: ConnectionsResponse,
  connection: ConnectionView,
  sessionNonce: string,
): ProjectConnectionWriter {
  return {
    catalog,
    connection,
    invokeCommand: (commandId, parameters, signal) =>
      invokeProviderCommand(client, catalog, connection, { sessionNonce, commandId, parameters }, signal),
  };
}

/** The settings of a Flow operation or trigger binding that uses a connection name, as an autosave item. */
export function settingsDraftItem(
  catalog: ConnectionsResponse,
  connection: ConnectionView,
  use: ConnectorOperation | ConnectorTriggerBinding,
  base: Record<string, unknown>,
): DraftItem<Record<string, unknown>> {
  const named = { connectorId: connection.connectorId, connectionName: connection.connectionName };
  return {
    key: draftItemKey(
      "triggerName" in use
        ? { kind: "KIND_TRIGGER_BINDING", ...named, triggerName: use.triggerName, bindingName: use.bindingName }
        : {
            kind: "KIND_STEP_SETTING",
            ...named,
            operationId: use.operationId,
            flowType: use.flowType,
            stepType: use.stepType,
          },
    ),
    base,
    revision: catalog.configurationRevision ?? "0",
    write: (client, configuration, revision) => saveSettings(client, catalog, connection, use, configuration, revision),
    read: async (client) => {
      const current = await listConnections(client);
      const found = current.connections.find((item) => connectionKey(item) === connectionKey(connection));
      const saved =
        "triggerName" in use
          ? found?.triggerUses?.find(
              (item) => item.triggerName === use.triggerName && item.bindingName === use.bindingName,
            )
          : found?.uses.find(
              (item) =>
                item.operationId === use.operationId &&
                item.flowName === use.flowType &&
                item.stepName === use.stepType,
            );
      return { value: saved?.configuration ?? {}, revision: current.configurationRevision ?? "0" };
    },
  };
}

/**
 * Selects the Teamspace connection a connection name uses in this scope, on `configurationRevision`; a Live
 * selection lands on the draft. Answers the revision the selection made.
 */
async function selectTeamSpaceConnection(
  client: FlowApiClient,
  catalog: ConnectionsResponse,
  connection: ConnectionView,
  teamSpaceConnectionId: string,
  configurationRevision: string,
): Promise<string> {
  return writeItem(
    client,
    catalog,
    connection,
    selectionPath(connection),
    { teamSpaceConnectionId },
    configurationRevision,
  );
}

/** The route of a connection name's selection. */
export function selectionPath(connection: ConnectionView): string {
  return `/api/v2/connector-selections/${encodeURIComponent(connection.connectorId)}/${encodeURIComponent(connection.connectionName)}`;
}

/** The route of the settings of a Flow operation or trigger binding that uses a connection name. */
export function settingsPath(connection: ConnectionView, use: ConnectorOperation | ConnectorTriggerBinding): string {
  const named = `${encodeURIComponent(connection.connectorId)}/${encodeURIComponent(connection.connectionName)}`;
  return "triggerName" in use
    ? `/api/v2/connector-trigger-bindings/${named}/${encodeURIComponent(use.triggerName)}/${encodeURIComponent(use.bindingName)}`
    : `/api/v2/connector-use-configurations/${named}/${encodeURIComponent(use.operationId)}/${encodeURIComponent(use.flowType)}/${encodeURIComponent(use.stepType)}`;
}

/** Writes `body` to a connection name's item route on `configurationRevision` and answers the revision it made. */
export async function writeItem(
  client: FlowApiClient,
  catalog: ConnectionsResponse,
  connection: ConnectionView,
  path: string,
  body: object,
  configurationRevision: string,
): Promise<string> {
  const written = await writeConnector<ConfigurationWritten>(client, catalog, connection, path, {
    method: "PUT",
    body,
    configurationRevision,
  });
  return written.configurationRevision;
}

/** The Teamspace connection a connection name selects, as an item of the scope's autosave queue. */
export function selectionDraftItem(catalog: ConnectionsResponse, connection: ConnectionView): DraftItem<string> {
  return {
    key: draftItemKey({
      kind: "KIND_CONNECTION",
      connectorId: connection.connectorId,
      connectionName: connection.connectionName,
    }),
    base: connection.sharedConnectionId ?? "",
    revision: catalog.configurationRevision ?? "0",
    write: (client, teamSpaceConnectionId, revision) =>
      selectTeamSpaceConnection(client, catalog, connection, teamSpaceConnectionId, revision),
    read: async (client) => {
      const current = await listConnections(client);
      const named = current.connections.find((item) => connectionKey(item) === connectionKey(connection));
      return { value: named?.sharedConnectionId ?? "", revision: current.configurationRevision ?? "0" };
    },
  };
}

/** Saves the configuration a trigger binding or Flow operation uses the connection with. */
function saveSettings(
  client: FlowApiClient,
  catalog: ConnectionsResponse,
  connection: ConnectionView,
  use: ConnectorOperation | ConnectorTriggerBinding,
  configuration: Record<string, unknown>,
  configurationRevision: string,
): Promise<string> {
  return writeItem(
    client,
    catalog,
    connection,
    settingsPath(connection, use),
    { configuration },
    configurationRevision,
  );
}

/**
 * One environment setting as the autosave queue writes it: its value (never a secret's, which is only
 * configured or not), or removed. A secret's typed value is sent once and compared only by presence.
 */
export interface EnvironmentValue {
  configured: boolean;
  value?: string;
}

/** A declared or obsolete setting of the environment as an item of the scope's autosave queue. */
export function environmentDraftItem(
  snapshot: ApplicationEnvironmentSnapshot,
  definitionRevision: string,
  name: string,
  secret: boolean,
  onWritten: (snapshot: ApplicationEnvironmentSnapshot) => void,
): DraftItem<EnvironmentValue> {
  const valueOf = (current: ApplicationEnvironmentSnapshot): EnvironmentValue => {
    const field = current.fields.find((item) => item.name === name);
    if (!field) return { configured: current.obsolete.includes(name) };
    return secret ? { configured: field.configured } : { configured: field.configured, value: field.value ?? "" };
  };
  return {
    key: draftItemKey({ kind: "KIND_ENVIRONMENT", environmentName: name }),
    base: valueOf(snapshot),
    revision: String(snapshot.configurationRevision),
    write: async (client, next, revision) => {
      const write: ApplicationEnvironmentWrite = { values: {}, secrets: {}, remove: [] };
      if (!next.configured) write.remove.push(name);
      else (secret ? write.secrets : write.values)[name] = next.value ?? "";
      const written = await saveApplicationEnvironment(
        client,
        { ...snapshot, configurationRevision: Number(revision) },
        definitionRevision,
        write,
      );
      onWritten(written);
      return String(written.configurationRevision);
    },
    read: async (client) => {
      const current = await getApplicationEnvironment(client);
      return { value: valueOf(current), revision: String(current.configurationRevision) };
    },
    same: (a, b) => a.configured === b.configured && (secret || a.value === b.value),
  };
}
