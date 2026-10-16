// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ConnectorCatalogEntry__Output } from "@/lib/generated/protobuf/superverse/v2/ConnectorCatalogEntry";
import type { TeamSpaceConnectionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceConnectionSnapshot";
import type {
  ConnectionStatus,
  ConnectionView,
  ConnectionWrite,
  ConnectionWriter,
  ReleaseManifest,
  UISessionResponse,
} from "@/lib/api/flow/connector";
import { newRequestId } from "@/lib/api/transport/api-error";
import { readUnary } from "@/lib/api/transport/read-unary";
import { postJson, unary } from "@/lib/api/transport/unary";
import { required } from "@/lib/shared/protobuf";

// Browser client of TeamSpaceConnectionService: a Teamspace's connections, which the database owns, and the
// connection editor's writer. Requests carry settings and credentials as JSON strings.

export type TeamSpaceConnection = TeamSpaceConnectionSnapshot__Output;
export type ConnectorCatalogEntry = ConnectorCatalogEntry__Output;

/** Where Studio receives a provider's OAuth redirect; Go builds the same URI from its Studio origin. */
const OAUTH_CALLBACK_PATH = "/api/connector-oauth/callback";
const MAX_PAGES = 20;

export async function listConnectorCatalog(signal: AbortSignal): Promise<ConnectorCatalogEntry[]> {
  return (await readUnary("ListConnectorCatalog", {}, { signal })).connectors;
}

/** The Teamspace's ACTIVE connections, newest first, of one connector when `connectorId` names it. */
export async function listTeamSpaceConnections(
  teamSpaceId: string,
  connectorId: string,
  signal: AbortSignal,
): Promise<TeamSpaceConnection[]> {
  const connections: TeamSpaceConnection[] = [];
  let pageToken = "";
  for (let page = 0; page < MAX_PAGES; page++) {
    const answer = await readUnary(
      "ListTeamSpaceConnections",
      { teamSpaceId, connectorId, pageSize: 100, pageToken },
      { signal },
    );
    connections.push(...answer.connections);
    if (!(pageToken = answer.nextPageToken)) break;
  }
  return connections;
}

export async function getTeamSpaceConnection(
  teamSpaceId: string,
  connectionId: string,
  signal: AbortSignal,
): Promise<{ connection: TeamSpaceConnection; storedCredentialFields: string[] }> {
  const answer = await readUnary("GetTeamSpaceConnection", { teamSpaceId, connectionId }, { signal });
  return {
    connection: required(answer.connection, "connection"),
    storedCredentialFields: answer.storedCredentialFields,
  };
}

/** Deletes an unused connection at the revision the page read; one a Project uses is CONNECTION_IN_USE. */
export async function deleteTeamSpaceConnection(connection: TeamSpaceConnection, signal: AbortSignal): Promise<void> {
  await unary(
    "DeleteTeamSpaceConnection",
    {
      teamSpaceId: connection.teamSpaceId,
      connectionId: connection.connectionId,
      expectedRevision: connection.revision,
    },
    { signal },
  );
}

const CREDENTIAL_STATUS: Record<string, ConnectionStatus> = {
  CREDENTIAL_STATUS_READY: "Ready",
  CREDENTIAL_STATUS_REAUTHORIZATION_REQUIRED: "Reauthorization required",
};

/** A connection as the connection editor shows it; usages live on the snapshot, not on the view. */
export function teamSpaceConnectionView(
  connection: TeamSpaceConnection,
  storedCredentialFields: string[] = [],
  displayName?: string,
): ConnectionView {
  return {
    connectorId: connection.connectorId,
    displayName,
    authMethodId: connection.authMethodId,
    connectionName: connection.name,
    modulePath: connection.modulePath,
    moduleVersion: connection.manifestVersion,
    status: CREDENTIAL_STATUS[connection.credentialStatus] ?? "Missing",
    configuration: JSON.parse(connection.settingsJson || "{}") as Record<string, unknown>,
    storedCredentialFields,
    credentialRevision: Number(connection.credentialRevision),
    uses: [],
  };
}

/** The connector release a new connection is created from: a catalog entry, or what an application declares. */
export interface ConnectorRelease {
  connectorId: string;
  modulePath: string;
  version: string;
  displayName?: string;
}

/** The view of a connection that does not exist yet, which the editor fills in. */
export function newConnectionView(release: ConnectorRelease, name: string, authMethodId = ""): ConnectionView {
  return {
    connectorId: release.connectorId,
    displayName: release.displayName,
    authMethodId,
    connectionName: name,
    modulePath: release.modulePath,
    moduleVersion: release.version,
    status: "Missing",
    configuration: {},
    storedCredentialFields: [],
    uses: [],
  };
}

/** The editor's form: the release manifest, the nonce its provider commands run under and the platform OAuth offer. */
export interface ConnectionForm extends UISessionResponse {
  platformClientMethodIds: string[];
}

/** Opens the editor's form for a connection, or for a release when no connection exists yet. */
export async function openConnectionForm(
  teamSpaceId: string,
  target: { connectionId: string } | ConnectorRelease,
  signal: AbortSignal,
): Promise<ConnectionForm> {
  const input =
    "connectionId" in target
      ? { teamSpaceId, connectionId: target.connectionId }
      : { teamSpaceId, connectorId: target.connectorId, modulePath: target.modulePath, version: target.version };
  const answer = await unary("StartTeamSpaceConnectionUiSession", input, { signal });
  return {
    connectorId: "connectorId" in target ? target.connectorId : "",
    connectionName: "",
    sessionNonce: answer.sessionNonce,
    oauthRedirectUri: `${window.location.origin}${OAUTH_CALLBACK_PATH}`,
    manifest: JSON.parse(answer.manifestJson) as ReleaseManifest,
    platformClientMethodIds: answer.platformClientMethodIds,
  };
}

/**
 * The editor's writer for one Teamspace connection. A new connection is created on the first save, under one
 * request ID, so a retried save is the same connection; `onCreated` hears its ID. A new OAuth connection is
 * created instead when its authorization completes, which returns to `returnPath` with its ID.
 */
export function teamSpaceConnectionWriter({
  teamSpaceId,
  existing,
  release,
  name,
  sessionNonce,
  returnPath,
  onCreated,
}: {
  teamSpaceId: string;
  existing: TeamSpaceConnection | null;
  release: ConnectorRelease;
  name: () => string;
  sessionNonce: string;
  returnPath: () => string;
  onCreated: (connectionId: string) => void;
}): ConnectionWriter {
  const requestId = newRequestId();
  let created = existing?.connectionId ?? "";
  const create = async (
    write: Pick<ConnectionWrite, "authMethodId" | "configuration">,
    credentials: object | null,
    platform: boolean,
  ) => {
    if (created) return created;
    const answer = await unary(
      "CreateTeamSpaceConnection",
      {
        teamSpaceId,
        requestId,
        name: name(),
        connectorId: release.connectorId,
        modulePath: release.modulePath,
        version: release.version,
        authMethodId: write.authMethodId,
        clientSource: platform ? "CONNECTION_CLIENT_SOURCE_PLATFORM" : "CONNECTION_CLIENT_SOURCE_USER",
        settingsJson: JSON.stringify(write.configuration),
        credentialsJson: credentials ? JSON.stringify(credentials) : "",
      },
      { signal: new AbortController().signal },
    );
    created = answer.connectionId;
    onCreated(created);
    return created;
  };
  return {
    async save(write) {
      if (!existing) {
        await create(write, write.credentials, false);
        return;
      }
      const replacesCredential = Object.keys(write.credentials).length > 0;
      await unary(
        "UpdateTeamSpaceConnection",
        {
          teamSpaceId,
          connectionId: existing.connectionId,
          expectedRevision: existing.revision,
          settingsJson: JSON.stringify(write.configuration),
          credentialsJson: replacesCredential ? JSON.stringify(write.credentials) : "",
          keepCredentialFields: replacesCredential ? write.keepCredentialFields : [],
          expectedCredentialRevision: existing.credentialRevision,
        },
        { signal: new AbortController().signal },
      );
    },
    async startOAuth(start) {
      const platform = start.clientSource === "platform";
      // A new connection is created by its authorization once it completes, so an abandoned one leaves none.
      const target = created
        ? { connectionId: created, version: release.version }
        : {
            newConnection: {
              requestId,
              name: name(),
              connectorId: release.connectorId,
              modulePath: release.modulePath,
              version: release.version,
              authMethodId: start.authMethodId,
              clientSource: platform ? "CONNECTION_CLIENT_SOURCE_PLATFORM" : "CONNECTION_CLIENT_SOURCE_USER",
              settingsJson: JSON.stringify(start.configuration),
            },
          };
      return postJson<{ authorizationUrl: string }>("/api/connector-oauth/start", {
        teamSpaceId,
        ...target,
        clientId: platform ? "" : start.clientId,
        clientSecret: platform ? "" : start.clientSecret,
        credentialValuesJson: JSON.stringify(start.credentialValues),
        credentialSecretsJson: JSON.stringify(start.credentialSecrets),
        returnPath: returnPath(),
      });
    },
    async invokeCommand(commandId, parameters, signal) {
      const answer = await unary(
        "InvokeTeamSpaceConnectionCommand",
        { teamSpaceId, sessionNonce, commandId, parametersJson: JSON.stringify(parameters) },
        { signal: signal ?? new AbortController().signal },
      );
      return JSON.parse(answer.resultJson) as Record<string, unknown>;
    },
  };
}
