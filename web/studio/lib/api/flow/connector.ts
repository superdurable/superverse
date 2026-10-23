// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readFlowApiJson } from "@/lib/api/transport/api-error";
import { sendFlowApiJson, type FlowApiClient } from "@/lib/api/transport/flow-api-client";

/**
 * The project's Connector connection names and their release manifests, and the ConnectorService
 * operations on them: each name selects a Teamspace connection, and each Flow operation and trigger
 * has its settings. Every write is fenced by the revisions the page read, so a concurrent change is
 * rejected instead of overwritten.
 */

export const CONNECTOR_REQUEST_FAILED = "Connector request failed";

export type ConnectionStatus =
  "Missing" | "Ready" | "Expired" | "Conflict" | "Unsupported" | "Reauthorization required";

interface ConnectionUse {
  flowName: string;
  stepId: string;
  stepName: string;
  operationId: string;
  operationKind: string;
  configurationUI: ConnectorConfigurationUI;
  configuration: Record<string, unknown>;
  configured: boolean;
}

export interface ConnectorUIBinding {
  port: string;
  jsonPointer: string;
}
export interface ConnectorUIUnit {
  id: string;
  unitId: string;
  label: string;
  description?: string;
  required: boolean;
  bindings: ConnectorUIBinding[];
}
interface ConnectorConfigurationUI {
  units: ConnectorUIUnit[];
}
export interface TriggerUse {
  /** The Connector UI units the binding's Flow composes; absent when no Flow of the source declares the binding. */
  configurationUI?: ConnectorConfigurationUI;
  triggerName: string;
  bindingName: string;
  configuration: Record<string, unknown>;
  configured: boolean;
}

export interface ConnectionView {
  connectorId: string;
  /** The release manifest metadata.displayName, absent when the release could not be resolved. */
  displayName?: string;
  /** The one authorization method the AppManifest declares, empty for a connector without methods. */
  authMethodId?: string;
  connectionName: string;
  /** Absence is permitted; configured connections still require valid settings and authorization. */
  optional?: boolean;
  modulePath?: string;
  /** The connector release the AppManifest declares; stored settings and credentials name no release. */
  moduleVersion?: string;
  /** The major version, which a selected Teamspace connection shares. */
  versionLine?: string;
  /** The Teamspace connection the name selects, and what the Project's page may show of it. */
  sharedConnectionId?: string;
  selectedConnection?: { name: string; connectionStatus: string; credentialStatus: string; manifestVersion: string };
  provider?: string;
  status: ConnectionStatus;
  configuration?: Record<string, unknown>;
  storedCredentialFields?: string[];
  credentialExpiresAt?: string;
  credentialStatus?: string;
  credentialRevision?: number;
  uses: ConnectionUse[];
  triggerUses?: TriggerUse[];
}

export interface ConnectionsResponse {
  configurationRevision?: string;
  configurationState?: "Draft" | "Valid" | "Ready to deploy" | "Reauthorization required";
  definitionRevision: string;
  appManifestRevision?: number;
  csrfToken: string;
  connections: ConnectionView[];
}

export interface ManifestField {
  uniqueItems?: boolean;
  name: string;
  type: string;
  description: string;
  required: boolean;
  default?: unknown;
  enum?: string[];
  studioUnit?: { unit: string; port: string };
}

/** Where a person creates the provider application an authorization method needs. */
export interface ManifestGuide {
  startURL: string;
  steps: string[];
}

interface ManifestOAuth2 {
  /** The scopes, or the setting they come from, as a generic connector declares them. */
  scopes: string[] | { fromSetting: string };
  clientIDCredential?: string;
  clientSecretCredential?: string;
  userScopes?: string[];
  credentialMappings?: { credential: string; source: string }[];
  credentialDerivations?: { credential: string; endpoint: string; source: string; verifiedBy?: string }[];
}

export interface ManifestAuthMethod {
  id: string;
  displayName: string;
  description: string;
  recommended?: boolean;
  type: string;
  fields: ManifestField[];
  configuration?: { fields: ManifestField[] };
  guide?: ManifestGuide;
  oauth2?: ManifestOAuth2;
}

export interface ReleaseManifest {
  metadata: { displayName: string; description: string };
  spec: {
    provider: string;
    configuration: { fields: ManifestField[] };
    /** The triggers the release declares, each with its description in the connector's words. */
    triggers?: { name: string; description: string }[];
    auth: {
      type?: string;
      fields: ManifestField[];
      guide?: ManifestGuide;
      oauth2?: ManifestOAuth2;
      defaultMethod?: string;
      /** The singular noun, such as Provider, for one method; a connection selects exactly one. */
      methodLabel?: string;
      methods?: ManifestAuthMethod[];
    };
    studio?: {
      setup: { backendCapabilities: string[] };
      commands?: { id: string; capability: string }[];
      units?: {
        id: string;
        description: string;
        backendCapabilities?: string[];
        inputs?: { name: string; type: string }[];
        outputs: { name: string; type: string }[];
      }[];
    };
  };
}

export interface UISessionResponse {
  connectorId: string;
  connectionName: string;
  sessionNonce?: string;
  oauthRedirectUri?: string;
  manifest: ReleaseManifest;
}

export function connectionKey(connection: ConnectionView) {
  return `${connection.connectorId}\u0000${connection.connectionName}`;
}

/** The release manifest displayName, or the Connector ID when no release metadata is available. */
export function connectorDisplayName(connection: Pick<ConnectionView, "connectorId" | "displayName">): string {
  return connection.displayName?.trim() || connection.connectorId;
}

/**
 * The CSRF token and the configuration revisions a write was made against, so a concurrent change
 * is rejected instead of overwritten. The definition revision travels as sendFlowApiJson's own
 * option. Only a write to a connection names its credential revision.
 */
export function revisionFenceHeaders({
  csrfToken,
  appManifestRevision,
  configurationRevision,
  credentialRevision,
}: {
  csrfToken: string;
  appManifestRevision: number;
  configurationRevision: string | number;
  credentialRevision?: number;
}): Record<string, string> {
  return {
    "X-Dex-CSRF-Token": csrfToken,
    "X-Dex-App-Manifest-Revision": String(appManifestRevision),
    "X-Dex-Configuration-Revision": String(configurationRevision),
    ...(credentialRevision === undefined ? {} : { "X-Dex-Credential-Revision": String(credentialRevision) }),
  };
}

/** A connection write: its module and authorization method, configuration, and new or kept credentials. */
export interface ConnectionWrite {
  modulePath?: string;
  provider: string;
  authMethodId: string;
  configuration: Record<string, unknown>;
  credentials: Record<string, string>;
  keepCredentialFields: string[];
  credentialExpiresAt: string | null;
}

/**
 * What an OAuth authorization starts from; the provider then returns the person to the signed callback.
 * `platform` uses the platform's own OAuth application instead of the entered client.
 */
interface ConnectorOAuthStart {
  clientSource?: "user" | "platform";
  authMethodId: string;
  clientId: string;
  clientSecret: string;
  configuration: Record<string, unknown>;
  credentialValues: Record<string, string>;
  credentialSecrets: Record<string, string>;
}

/** Runs a provider command the release declares, within a form's UI session. */
export interface CommandRunner {
  invokeCommand(
    commandId: string,
    parameters: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<Record<string, unknown>>;
}

/** Where one connection's form and native units write: its credentials and settings and its OAuth start. */
export interface ConnectionWriter extends CommandRunner {
  save(write: ConnectionWrite): Promise<void>;
  /** Answers with the provider page to open. */
  startOAuth(start: ConnectorOAuthStart): Promise<{ authorizationUrl: string }>;
}

export async function listConnections(client: FlowApiClient, signal?: AbortSignal): Promise<ConnectionsResponse> {
  return readFlowApiJson<ConnectionsResponse>(await client.fetch("/api/v2/connector-connections", { signal }));
}

/** Opens the connection's UI session: the exact release manifest and the nonce its provider commands run under. */
export function startConnectorUiSession(
  client: FlowApiClient,
  catalog: ConnectionsResponse,
  connection: ConnectionView,
  signal: AbortSignal,
): Promise<UISessionResponse> {
  return writeConnector<UISessionResponse>(client, catalog, connection, "/api/v2/connector-native-sessions", {
    method: "POST",
    body: { connectorId: connection.connectorId, connectionName: connection.connectionName },
    signal,
  });
}

/**
 * Releases the Live usages of the connections the current Live version does not select, so they can be
 * deleted; refused with DEPLOYMENT_IN_PROGRESS while a deployment runs.
 */
export async function releaseUnusedConnections(client: FlowApiClient, catalog: ConnectionsResponse): Promise<void> {
  await sendFlowApiJson(
    client,
    "/api/v2/connector-usages/release-unused",
    "POST",
    {},
    {
      definitionRevision: catalog.definitionRevision,
      headers: revisionFenceHeaders({
        csrfToken: catalog.csrfToken,
        appManifestRevision: catalog.appManifestRevision ?? 0,
        configurationRevision: catalog.configurationRevision ?? "0",
      }),
    },
  );
}

/** Runs a provider command the release declares, within the connection's UI session. */
export function invokeProviderCommand(
  client: FlowApiClient,
  catalog: ConnectionsResponse,
  connection: ConnectionView,
  command: { sessionNonce: string; commandId: string; parameters: Record<string, string> },
  signal?: AbortSignal,
): Promise<Record<string, unknown>> {
  const path = `/api/v2/connector-ui-sessions/${encodeURIComponent(command.sessionNonce)}/commands/${encodeURIComponent(command.commandId)}`;
  return writeConnector<Record<string, unknown>>(client, catalog, connection, path, {
    method: "POST",
    body: { parameters: command.parameters },
    signal,
  });
}

/**
 * A JSON write to the connections, fenced by the catalog's revisions, or by `configurationRevision` when the
 * autosave queue names a newer one, and by the connection's credentials.
 */
export function writeConnector<T>(
  client: FlowApiClient,
  catalog: ConnectionsResponse,
  connection: ConnectionView,
  path: string,
  {
    method,
    body,
    signal,
    configurationRevision = catalog.configurationRevision ?? "0",
  }: { method: "POST" | "PUT" | "DELETE"; body?: unknown; signal?: AbortSignal; configurationRevision?: string },
): Promise<T> {
  return sendFlowApiJson<T>(client, path, method, body, {
    definitionRevision: catalog.definitionRevision,
    headers: revisionFenceHeaders({
      csrfToken: catalog.csrfToken,
      appManifestRevision: catalog.appManifestRevision ?? 0,
      configurationRevision,
      credentialRevision: connection.credentialRevision ?? 0,
    }),
    signal,
  });
}
