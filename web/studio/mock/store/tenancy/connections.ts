// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Explicit development data only: the TeamSpace connections of mock mode. Entered credential values are
// discarded; a connection keeps only the names of its stored credential fields.
import type { AccountSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AccountSnapshot";
import type { TeamSpaceConnectionSnapshot } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceConnectionSnapshot";
import { rpcFailure } from "@/lib/server/grpc-client";
import { httpError } from "@/lib/server/request-body";
import type { ConnectionRefusal } from "@/lib/shared/problem/refusals";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import { REVISION_CHANGED } from "@/lib/shared/revision-conflict";
import { MOCK_CONNECTOR, MOCK_SLACK_CONNECTOR, MOCK_STRIPE_CONNECTOR } from "@/mock/fixture/connector-manifest";
import { nowIso, randomHex } from "@/mock/store/clock";
import { readableTeamSpace, teamSpaceRoleOf, type MockTeamSpace, type MockTenancy } from "./team-spaces";

export interface MockConnection {
  /** The fixture connector, OpenAI unless the connection is a seeded Slack one or a Stripe one. */
  connector?: typeof MOCK_SLACK_CONNECTOR | typeof MOCK_STRIPE_CONNECTOR;
  teamSpaceId: string;
  connectionId: string;
  name: string;
  clientSource: "user" | "platform";
  settings: Record<string, unknown>;
  revision: number;
  credentialStatus: "AUTHORIZING" | "READY";
  credentialRevision: number;
  credentialFields: string[];
  status: "ACTIVE" | "DELETED";
  creatorAccountId: string;
  updatedByAccountId: string;
  createdAt: string;
  updatedAt: string;
  /**
   * The Project scopes that use the connection, `${projectId}:${scope}`. The Configuration fixture's
   * selections do not reach the TeamSpace's connections, so mock mode records none.
   */
  usages: Set<string>;
}

interface MockConnections {
  byId: Map<string, MockConnection>;
  /** Admitted creations, keyed `${accountId}:${requestId}`, with the name they were made with. */
  admissions: Map<string, { connectionId: string; name: string }>;
  /** Authorizations in progress by their state: of an existing connection, or of one they create. */
  attempts: Map<string, { accountId: string; connectionId: string } | { accountId: string; creation: MockCreation }>;
}

/** A new connection that its authorization creates once it completes. */
type MockCreation = { teamSpaceId: string; requestId: string; name: string; settings: Record<string, unknown> };

const stores = new WeakMap<MockTenancy, MockConnections>();

// The seed: Customer Ops holds one READY API key connection of the fixture connector.
function mockConnections(tenancy: MockTenancy): MockConnections {
  let connections = stores.get(tenancy);
  if (!connections) {
    const at = nowIso(-86_400_000);
    const seed: MockConnection = {
      teamSpaceId: "custops1",
      connectionId: "c0000000seed",
      name: "OpenAI (team key)",
      clientSource: "user",
      settings: { model: "gpt-4.1-mini" },
      revision: 1,
      credentialStatus: "READY",
      credentialRevision: 1,
      credentialFields: ["api_key"],
      status: "ACTIVE",
      creatorAccountId: "0000000000000000000000000000a001",
      updatedByAccountId: "0000000000000000000000000000a001",
      createdAt: at,
      updatedAt: at,
      usages: new Set(),
    };
    // And one Slack connection, which the `slack` Setup fixture's connection name selects.
    const slack: MockConnection = {
      ...seed,
      connector: MOCK_SLACK_CONNECTOR,
      connectionId: "c000000slack",
      name: "Support workspace",
      settings: {},
      credentialFields: ["bot_token"],
      usages: new Set(),
    };
    // And one Stripe test-mode connection, which the `endpoint-bound` Setup fixture's Preview selects.
    const stripe: MockConnection = {
      ...seed,
      connector: MOCK_STRIPE_CONNECTOR,
      connectionId: "c00000stripe",
      name: "Stripe test mode",
      settings: {},
      credentialFields: ["api_key", "webhook_signing_secret"],
      usages: new Set(),
    };
    connections = {
      byId: new Map([
        [seed.connectionId, seed],
        [slack.connectionId, slack],
        [stripe.connectionId, stripe],
      ]),
      admissions: new Map(),
      attempts: new Map(),
    };
    stores.set(tenancy, connections);
  }
  return connections;
}

function refused(status: number, reason: ConnectionRefusal, message: string) {
  return rpcFailure(status, message, { reason, domain: "superverse.v2", metadata: {} });
}

/** Whether the caller manages the TeamSpace's connections: an admin, or a builder of one of its Projects. */
function manages(tenancy: MockTenancy, teamSpace: MockTeamSpace, account: AccountSnapshot__Output): boolean {
  if (account.isAdmin || teamSpaceRoleOf(teamSpace, account) === "ADMIN") return true;
  return [...tenancy.projectTeamSpace].some(([projectId, teamSpaceId]) => {
    const role = tenancy.projectMembers.get(projectId)?.get(account.accountId)?.role;
    return teamSpaceId === teamSpace.id && (role === "OWNER" || role === "BUILDER");
  });
}

/** A READY TeamSpace whose connections the caller manages. */
export function managedTeamSpace(tenancy: MockTenancy, account: AccountSnapshot__Output, teamSpaceId: string) {
  const teamSpace = readableTeamSpace(tenancy, account, teamSpaceId);
  if (!manages(tenancy, teamSpace, account))
    throw httpError(403, "Only a TeamSpace admin or a builder of one of its Projects manages its connections");
  if (teamSpace.status !== "READY") throw httpError(409, "The TeamSpace is being deleted");
  return teamSpace;
}

/** The TeamSpace's connections of the statuses, newest first. */
export function listMockConnections(
  tenancy: MockTenancy,
  teamSpaceId: string,
  connectorId: string,
  statuses: string[],
) {
  return [...mockConnections(tenancy).byId.values()]
    .filter(
      (item) =>
        item.teamSpaceId === teamSpaceId &&
        (!connectorId || connectorId === (item.connector ?? MOCK_CONNECTOR).connectorId) &&
        (statuses.length ? statuses : ["ACTIVE"]).includes(item.status),
    )
    .sort((first, second) => second.createdAt.localeCompare(first.createdAt));
}

/** What a connection name's view shows of the TeamSpace connection it selects, as the configuration host does. */
export function mockSelectedConnection(tenancy: MockTenancy, connectionId: string) {
  const found = mockConnections(tenancy).byId.get(connectionId);
  return (
    found && {
      name: found.name,
      connectionStatus: found.status,
      credentialStatus: found.credentialStatus,
      clientSource: found.clientSource,
      manifestVersion: (found.connector ?? MOCK_CONNECTOR).version,
    }
  );
}

export function mockConnection(tenancy: MockTenancy, teamSpaceId: string, connectionId: string): MockConnection {
  const found = mockConnections(tenancy).byId.get(connectionId);
  if (!found || found.teamSpaceId !== teamSpaceId) throw httpError(404, "connection not found");
  if (found.status !== "ACTIVE") throw refused(409, "CONNECTION_DELETED", "The connection was deleted");
  return found;
}

function changed(connection: MockConnection, account: AccountSnapshot__Output) {
  connection.updatedByAccountId = account.accountId;
  connection.updatedAt = nowIso();
}

/** Creates a connection of the fixture connector; a replay answers the same ID. */
export function createMockConnection(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  input: {
    teamSpaceId: string;
    requestId: string;
    name: string;
    settings: Record<string, unknown>;
    apiKey: boolean;
    connector?: MockConnection["connector"];
  },
): string {
  const connections = mockConnections(tenancy);
  const key = `${account.accountId}:${input.requestId}`;
  const admitted = connections.admissions.get(key);
  if (admitted) {
    if (admitted.name !== input.name) throw httpError(409, "The request ID created another connection");
    return admitted.connectionId;
  }
  if (!input.name.trim() || input.name.length > 80) throw httpError(400, "A connection name has 1 to 80 characters");
  if (listMockConnections(tenancy, input.teamSpaceId, "", []).some((item) => item.name === input.name))
    throw refused(409, "CONNECTION_NAME_TAKEN", "Another connection of this TeamSpace has that name");
  const at = nowIso();
  const connectionId = `c${randomHex(11)}`;
  connections.byId.set(connectionId, {
    connector: input.connector,
    teamSpaceId: input.teamSpaceId,
    connectionId,
    name: input.name,
    clientSource: "user",
    settings: input.settings,
    revision: 1,
    credentialStatus: input.apiKey ? "READY" : "AUTHORIZING",
    credentialRevision: input.apiKey ? 1 : 0,
    credentialFields: input.apiKey ? ["api_key"] : [],
    status: "ACTIVE",
    creatorAccountId: account.accountId,
    updatedByAccountId: account.accountId,
    createdAt: at,
    updatedAt: at,
    usages: new Set(),
  });
  connections.admissions.set(key, { connectionId, name: input.name });
  return connectionId;
}

/** Applies an edit at the expected revisions: new settings, or a replaced or kept API key. */
export function updateMockConnection(
  connection: MockConnection,
  account: AccountSnapshot__Output,
  update: { expectedRevision: number; expectedCredentialRevision: number; settings?: object; apiKey?: boolean },
) {
  if (update.settings) {
    if (update.expectedRevision !== connection.revision)
      throw rpcFailure(409, "The connection changed since it was read", REVISION_CHANGED);
    connection.settings = { ...update.settings };
    connection.revision += 1;
  }
  if (update.apiKey !== undefined) {
    if (update.expectedCredentialRevision !== connection.credentialRevision)
      throw rpcFailure(409, "The connection changed since it was read", REVISION_CHANGED);
    connection.credentialFields = update.apiKey ? ["api_key"] : connection.credentialFields;
    connection.credentialStatus = "READY";
    connection.credentialRevision += 1;
  }
  changed(connection, account);
}

/** Deletes an unused connection at the expected revision. */
export function deleteMockConnection(
  connection: MockConnection,
  account: AccountSnapshot__Output,
  expectedRevision: number,
) {
  if (expectedRevision !== connection.revision)
    throw rpcFailure(409, "The connection changed since it was read", REVISION_CHANGED);
  if (connection.usages.size) throw refused(409, "CONNECTION_IN_USE", "A Project still uses this connection");
  connection.status = "DELETED";
  connection.revision += 1;
  changed(connection, account);
}

/**
 * Starts an authorization that the mock's own callback completes at once, as if the provider consented: of
 * an existing connection, or of a new one, whose name must be free now and when it completes.
 */
export function startMockAuthorization(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  target: MockConnection | MockCreation,
) {
  const state = `mock-${randomHex(16)}`;
  const attempts = mockConnections(tenancy).attempts;
  if ("connectionId" in target) {
    attempts.set(state, { accountId: account.accountId, connectionId: target.connectionId });
    return state;
  }
  if (listMockConnections(tenancy, target.teamSpaceId, "", []).some((item) => item.name === target.name))
    throw refused(409, "CONNECTION_NAME_TAKEN", "Another connection of this TeamSpace has that name");
  attempts.set(state, { accountId: account.accountId, creation: target });
  return state;
}

/** Completes the caller's own authorization: the connection keeps fixture tokens. */
export function completeMockAuthorization(
  tenancy: MockTenancy,
  account: AccountSnapshot__Output,
  completion: { state: string; providerError: string },
): MockConnection {
  const connections = mockConnections(tenancy);
  const attempt = connections.attempts.get(completion.state);
  if (!attempt || attempt.accountId !== account.accountId) throw httpError(400, "Unknown or expired authorization");
  connections.attempts.delete(completion.state);
  if (completion.providerError) throw refused(409, "OAUTH_DENIED", "The provider denied the authorization");
  const connectionId =
    "connectionId" in attempt
      ? attempt.connectionId
      : createMockConnection(tenancy, account, { ...attempt.creation, apiKey: false });
  const connection = connections.byId.get(connectionId);
  if (!connection || connection.status !== "ACTIVE")
    throw refused(409, "CONNECTION_DELETED", "The connection was deleted");
  connection.credentialFields = ["access_token"];
  connection.credentialStatus = "READY";
  connection.credentialRevision += 1;
  changed(connection, account);
  return connection;
}

/** The connection as TeamSpaceConnectionService answers it. */
export function mockConnectionSnapshot(connection: MockConnection): TeamSpaceConnectionSnapshot {
  return {
    teamSpaceId: connection.teamSpaceId,
    connectionId: connection.connectionId,
    name: connection.name,
    connectorId: (connection.connector ?? MOCK_CONNECTOR).connectorId,
    modulePath: (connection.connector ?? MOCK_CONNECTOR).modulePath,
    versionLine: (connection.connector ?? MOCK_CONNECTOR).versionLine,
    manifestVersion: (connection.connector ?? MOCK_CONNECTOR).version,
    authMethodId: "",
    clientSource:
      connection.clientSource === "platform" ? "CONNECTION_CLIENT_SOURCE_PLATFORM" : "CONNECTION_CLIENT_SOURCE_USER",
    settingsJson: JSON.stringify(connection.settings),
    revision: connection.revision,
    credentialStatus: `CREDENTIAL_STATUS_${connection.credentialStatus}`,
    credentialRevision: connection.credentialRevision,
    status: `TEAM_SPACE_CONNECTION_STATUS_${connection.status}`,
    creatorAccountId: connection.creatorAccountId,
    updatedByAccountId: connection.updatedByAccountId,
    createdAt: isoToTimestamp(connection.createdAt),
    updatedAt: isoToTimestamp(connection.updatedAt),
    usages: [...connection.usages].map((usage) => {
      const [projectId, scope] = usage.split(":");
      return { projectId, scope: scope === "live" ? "PROJECT_SCOPE_LIVE" : "PROJECT_SCOPE_PREVIEW" };
    }),
  };
}
