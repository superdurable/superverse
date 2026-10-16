// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock TeamSpaceService, TeamSpaceMemberService, ProjectMemberService and TeamSpaceConnectionService over
// the in-memory store.
import type { CallerContext__Output } from "@/lib/generated/protobuf/superverse/v2/CallerContext";
import { httpError } from "@/lib/server/request-body";
import {
  MOCK_CONNECTOR,
  MOCK_MODELS,
  MOCK_SLACK_CONNECTOR,
  MOCK_STRIPE_CONNECTOR,
  MOCK_STRIPE_ENDPOINT_REASON,
  mockConnectorManifest,
  mockSlackManifest,
  mockStripeManifest,
} from "@/mock/fixture/connector-manifest";
import { mockAccount } from "@/mock/store/accounts";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import { withStatuses } from "@/mock/store/resource-snapshots";
import { deleteProject, getProject, type MockStore } from "@/mock/store/store";
import {
  completeMockAuthorization,
  createMockConnection,
  deleteMockConnection,
  listMockConnections,
  managedTeamSpace,
  mockConnection,
  mockConnectionSnapshot,
  startMockAuthorization,
  updateMockConnection,
} from "@/mock/store/tenancy/connections";
import {
  addMockProjectMember,
  mockProjectMembers,
  removeMockProjectMember,
  setMockProjectMemberRole,
} from "@/mock/store/tenancy/project-members";
import {
  addMockTeamSpaceMember,
  createMockTeamSpace,
  deleteMockTeamSpace,
  listMockTeamSpaces,
  mockTenancy,
  readableTeamSpace,
  removeMockTeamSpaceMember,
  renameMockTeamSpace,
  setMockTeamSpaceMemberRole,
  type MockTeamSpace,
  type MockTenancy,
} from "@/mock/store/tenancy/team-spaces";
import {
  mockProjectMemberSnapshots,
  mockTeamSpaceMembers,
  mockTeamSpaceOverview,
  mockTeamSpaceSnapshot,
} from "@/mock/store/tenancy/tenancy-snapshots";
import type { MockService } from "./mock-backend";

/** The caller's account and the store's Teamspaces, which the caller has joined. */
function tenancyOf(store: MockStore, caller: CallerContext__Output | null) {
  const account = mockAccount(caller);
  return { account, tenancy: mockTenancy(store, account) };
}

/** Every member change answers with all the members and the Teamspace's new revision. */
function teamSpaceMembers(teamSpace: MockTeamSpace) {
  return { members: mockTeamSpaceMembers(teamSpace), revision: teamSpace.revision };
}

function existingProject(store: MockStore, projectId: string) {
  const project = getProject(store, projectId);
  if (!project) throw httpError(404, "Project not found");
  return project;
}

function projectMembers(tenancy: MockTenancy, store: MockStore, projectId: string) {
  const project = existingProject(store, projectId);
  return {
    members: mockProjectMemberSnapshots(tenancy, projectId, tenancy.projectMembers.get(projectId) ?? new Map()),
    revision: project.revision,
  };
}

export const teamSpaceService: MockService<"TeamSpaceService"> = {
  CreateTeamSpace: ({ requestId, name, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    return { teamSpaceId: createMockTeamSpace(tenancy, account, requestId, name) };
  },
  ListTeamSpaces: ({ statuses, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    const snapshots = listMockTeamSpaces(tenancy, account).map((item) => mockTeamSpaceSnapshot(item, account));
    return { teamSpaces: withStatuses(snapshots, statuses), nextPageToken: "" };
  },
  GetTeamSpace: ({ teamSpaceId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    return { teamSpace: mockTeamSpaceSnapshot(readableTeamSpace(tenancy, account, teamSpaceId), account) };
  },
  RenameTeamSpace: ({ teamSpaceId, expectedRevision, name, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    return {
      teamSpace: mockTeamSpaceSnapshot(
        renameMockTeamSpace(tenancy, account, { teamSpaceId, expectedRevision, name }),
        account,
      ),
    };
  },
  DeleteTeamSpace: ({ teamSpaceId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    const accepted = deleteMockTeamSpace(tenancy, account, teamSpaceId, (projectId) => deleteProject(store, projectId));
    return { teamSpace: mockTeamSpaceSnapshot(accepted, account) };
  },
  GetTeamSpaceOverview: ({ teamSpaceId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    const teamSpace = readableTeamSpace(tenancy, account, teamSpaceId);
    return { overview: mockTeamSpaceOverview(store, tenancy, teamSpace, account) };
  },
};

export const teamSpaceMemberService: MockService<"TeamSpaceMemberService"> = {
  ListTeamSpaceMembers: ({ teamSpaceId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    return { members: mockTeamSpaceMembers(readableTeamSpace(tenancy, account, teamSpaceId)) };
  },
  AddTeamSpaceMember: ({ teamSpaceId, email, role, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    const added = addMockTeamSpaceMember(tenancy, account, { teamSpaceId, email, role });
    return { ...teamSpaceMembers(added.teamSpace), regainedProjectCount: added.regainedProjectCount };
  },
  SetTeamSpaceMemberRole: ({ teamSpaceId, accountId, role, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    return teamSpaceMembers(setMockTeamSpaceMemberRole(tenancy, account, { teamSpaceId, accountId, role }));
  },
  RemoveTeamSpaceMember: ({ teamSpaceId, accountId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    const projectName = (projectId: string) => {
      const project = getProject(store, projectId);
      return project && project.state !== "DELETED" ? project.displayName : null;
    };
    return teamSpaceMembers(removeMockTeamSpaceMember(tenancy, account, { teamSpaceId, accountId }, projectName));
  },
};

export const projectMemberService: MockService<"ProjectMemberService"> = {
  ListProjectMembers: ({ projectId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    return { members: mockProjectMemberSnapshots(tenancy, projectId, mockProjectMembers(tenancy, account, projectId)) };
  },
  AddProjectMember: ({ projectId, accountId, role, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    addMockProjectMember(tenancy, account, existingProject(store, projectId), { accountId, role });
    return projectMembers(tenancy, store, projectId);
  },
  SetProjectMemberRole: ({ projectId, accountId, role, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    setMockProjectMemberRole(tenancy, account, existingProject(store, projectId), { accountId, role });
    return projectMembers(tenancy, store, projectId);
  },
  RemoveProjectMember: ({ projectId, accountId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    removeMockProjectMember(tenancy, account, existingProject(store, projectId), accountId);
    return projectMembers(tenancy, store, projectId);
  },
};

/** A request's JSON object, as Go parses its `*_json` fields; empty is absent. */
function jsonObject(value: string, field: string): Record<string, unknown> | undefined {
  if (!value) return undefined;
  const parsed: unknown = JSON.parse(value);
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed))
    throw httpError(400, `${field} must be a JSON object`);
  return parsed as Record<string, unknown>;
}

/** The fixture connector's form; the mock offers no platform OAuth application. */
const mockForm = {
  sessionNonce: "mock-connection-form",
  manifestJson: JSON.stringify(mockConnectorManifest),
  platformClientMethodIds: [],
};

export const teamSpaceConnectionService: MockService<"TeamSpaceConnectionService"> = {
  ListConnectorCatalog: () => ({
    connectors: [
      {
        connectorId: MOCK_CONNECTOR.connectorId,
        displayName: MOCK_CONNECTOR.displayName,
        description: mockConnectorManifest.metadata.description,
        modulePath: MOCK_CONNECTOR.modulePath,
        version: MOCK_CONNECTOR.version,
        versionLine: MOCK_CONNECTOR.versionLine,
      },
      {
        connectorId: MOCK_STRIPE_CONNECTOR.connectorId,
        displayName: MOCK_STRIPE_CONNECTOR.displayName,
        description: mockStripeManifest.metadata.description,
        modulePath: MOCK_STRIPE_CONNECTOR.modulePath,
        version: MOCK_STRIPE_CONNECTOR.version,
        versionLine: MOCK_STRIPE_CONNECTOR.versionLine,
        endpointBound: true,
        endpointBoundReason: MOCK_STRIPE_ENDPOINT_REASON,
      },
    ],
  }),
  ListTeamSpaceConnections: ({ teamSpaceId, connectorId, statuses, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    readableTeamSpace(tenancy, account, teamSpaceId);
    const values = statuses.map((status) => status.replace("TEAM_SPACE_CONNECTION_STATUS_", ""));
    return {
      connections: listMockConnections(tenancy, teamSpaceId, connectorId, values).map(mockConnectionSnapshot),
      nextPageToken: "",
    };
  },
  GetTeamSpaceConnection: ({ teamSpaceId, connectionId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    readableTeamSpace(tenancy, account, teamSpaceId);
    const connection = mockConnection(tenancy, teamSpaceId, connectionId);
    return { connection: mockConnectionSnapshot(connection), storedCredentialFields: connection.credentialFields };
  },
  CreateTeamSpaceConnection: (request, { store }) => {
    const { account, tenancy } = tenancyOf(store, request.caller);
    managedTeamSpace(tenancy, account, request.teamSpaceId);
    const stripe = request.connectorId === MOCK_STRIPE_CONNECTOR.connectorId ? MOCK_STRIPE_CONNECTOR : undefined;
    if (request.connectorId !== MOCK_CONNECTOR.connectorId && !stripe)
      throw httpError(404, "Mock mode creates only the fixture connector's and Stripe's connections");
    const credentials = jsonObject(request.credentialsJson, "credentials_json");
    const connectionId = createMockConnection(tenancy, account, {
      teamSpaceId: request.teamSpaceId,
      requestId: request.requestId,
      name: request.name,
      settings: jsonObject(request.settingsJson, "settings_json") ?? {},
      apiKey: typeof credentials?.api_key === "string" && credentials.api_key !== "",
      connector: stripe,
    });
    return { connectionId };
  },
  UpdateTeamSpaceConnection: (request, { store }) => {
    const { account, tenancy } = tenancyOf(store, request.caller);
    managedTeamSpace(tenancy, account, request.teamSpaceId);
    const connection = mockConnection(tenancy, request.teamSpaceId, request.connectionId);
    const credentials = jsonObject(request.credentialsJson, "credentials_json");
    updateMockConnection(connection, account, {
      expectedRevision: Number(request.expectedRevision),
      expectedCredentialRevision: Number(request.expectedCredentialRevision),
      settings: jsonObject(request.settingsJson, "settings_json"),
      apiKey:
        credentials && (typeof credentials.api_key === "string" || request.keepCredentialFields.includes("api_key")),
    });
    return { connection: mockConnectionSnapshot(connection) };
  },
  DeleteTeamSpaceConnection: ({ teamSpaceId, connectionId, expectedRevision, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    managedTeamSpace(tenancy, account, teamSpaceId);
    deleteMockConnection(mockConnection(tenancy, teamSpaceId, connectionId), account, Number(expectedRevision));
    return {};
  },
  // The provider is the mock's own callback, which completes at once.
  StartTeamSpaceConnectionOAuth: ({ teamSpaceId, connectionId, newConnection, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    managedTeamSpace(tenancy, account, teamSpaceId);
    const state = startMockAuthorization(
      tenancy,
      account,
      newConnection
        ? {
            teamSpaceId,
            requestId: newConnection.requestId,
            name: newConnection.name,
            settings: jsonObject(newConnection.settingsJson, "new_connection.settings_json") ?? {},
          }
        : mockConnection(tenancy, teamSpaceId, connectionId),
    );
    const callback = new URL("/api/connector-oauth/callback", store.origin);
    callback.searchParams.set("state", state);
    callback.searchParams.set("code", "mock-code");
    return {
      authorizationUrl: callback.toString(),
      expiresAt: isoToTimestamp(new Date(Date.now() + 600_000).toISOString()),
    };
  },
  CompleteTeamSpaceConnectionOAuth: ({ state, providerError, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    return {
      connection: mockConnectionSnapshot(completeMockAuthorization(tenancy, account, { state, providerError })),
    };
  },
  StartTeamSpaceConnectionUiSession: ({ teamSpaceId, connectorId: requested, connectionId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    managedTeamSpace(tenancy, account, teamSpaceId);
    // A connection's editor opens its own connector's form, as the server reads it from the connection.
    const connectorId = connectionId
      ? (mockConnection(tenancy, teamSpaceId, connectionId).connector?.connectorId ?? MOCK_CONNECTOR.connectorId)
      : requested;
    // The Slack fixture's form only shows its manifest; its connection is seeded, not created here.
    if (connectorId === MOCK_SLACK_CONNECTOR.connectorId)
      return { ...mockForm, manifestJson: JSON.stringify(mockSlackManifest) };
    if (connectorId === MOCK_STRIPE_CONNECTOR.connectorId)
      return { ...mockForm, manifestJson: JSON.stringify(mockStripeManifest) };
    return mockForm;
  },
  InvokeTeamSpaceConnectionCommand: ({ teamSpaceId, sessionNonce, commandId, caller }, { store }) => {
    const { account, tenancy } = tenancyOf(store, caller);
    managedTeamSpace(tenancy, account, teamSpaceId);
    if (sessionNonce !== mockForm.sessionNonce || commandId !== "listModels")
      throw httpError(404, "Unknown provider command");
    return { resultJson: JSON.stringify(MOCK_MODELS) };
  },
};
