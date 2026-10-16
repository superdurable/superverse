// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Development-only Agent commands and typed connection-dialog fixtures; never acceptance evidence.
import { randomBytes } from "node:crypto";
import type { AgentModel__Output } from "@/lib/generated/protobuf/superverse/v2/AgentModel";
import type { CallerContext__Output } from "@/lib/generated/protobuf/superverse/v2/CallerContext";
import type { RemoteAgentConnection } from "@/lib/generated/protobuf/superverse/v2/RemoteAgentConnection";
import { rpcFailure } from "@/lib/server/grpc-client";
import { httpError } from "@/lib/server/request-body";
import { REVISION_CHANGED } from "@/lib/shared/revision-conflict";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import { mockAccount } from "@/mock/store/accounts";
import { AGENT_COMMAND_NOT_ADMITTED } from "@/lib/shared/session-command-error";
import { MOCK_FLOW_DESIGN } from "@/mock/fixture/agent-requests";
import { answerQuestions, approveTool, sendMessage } from "@/mock/store/agent-scripts";
import { deleteQueuedMessage, steerQueuedMessage } from "@/mock/store/agent-state";
import { sendDeliveryScript } from "@/mock/store/delivery/delivery-scripts";
import { advanceMockProgress, MOCK_SCENARIOS } from "@/mock/store/scenarios/catalog";
import type { MockStore } from "@/mock/store/store";
import { requireMockProjectRole } from "@/mock/store/tenancy/project-members";
import { mockTenancy } from "@/mock/store/tenancy/team-spaces";
import { interruptAgent, sendTaskScript } from "@/mock/store/tasks/agent-tasks";
import type { MockService } from "./mock-backend";
import { projectSession } from "./session-service";

/** Like Go, a command the Agent did not take is a typed AGENT_COMMAND_NOT_ADMITTED rejection. */
function notAdmitted(status: number, message: string): Error {
  return rpcFailure(status, message, AGENT_COMMAND_NOT_ADMITTED);
}

/** Runs one admitted command; the response is empty and the Session Watch reports its effect. */
function command(store: MockStore, { projectId }: { projectId: string }, run: () => boolean): Record<string, never> {
  projectSession(store, projectId);
  if (!run()) throw notAdmitted(409, "command conflicts with current session state");
  return {};
}

/** Like the server's mock model, a Session on the mock model reports where its scenario stands. */
function reportProgress(store: MockStore, projectId: string, text: string | null): boolean {
  const description = store.agents.get(projectId)?.description;
  if (description?.model === "AGENT_MODEL_MOCK")
    description.mockProgress = advanceMockProgress(description.mockProgress, text);
  return true;
}

/** One user message as the mock Agent takes it: a task or delivery script, else a phrase or an ordinary turn. */
function deliverMessage(store: MockStore, projectId: string, text: string): boolean {
  return (
    (sendTaskScript(store, projectId, text) ??
      sendDeliveryScript(store, projectId, text) ??
      sendMessage(store, projectId, text)) &&
    reportProgress(store, projectId, text)
  );
}

/** Like Go, the Session a creation starts runs on its model, when it names one, and takes its first message once. */
export function startMockSession(store: MockStore, projectId: string, model: AgentModel__Output, text: string) {
  const description = store.agents.get(projectId)?.description;
  if (!description) return;
  if (model !== "AGENT_MODEL_UNSPECIFIED") description.model = model;
  if (text) deliverMessage(store, projectId, text);
}

function required(value: string | undefined, name: string): string {
  if (!value) throw notAdmitted(400, `${name} is required`);
  return value;
}

export const agentService: MockService<"AgentService"> = {
  SendMessage: (request, { store }) => {
    required(request.messageId, "messageId");
    const text = required(request.text, "text");
    return command(store, request, () => deliverMessage(store, request.projectId, text));
  },
  // Answers are the scenario's next input, which carries no marker.
  AnswerQuestions: (request, { store }) =>
    command(
      store,
      request,
      () =>
        answerQuestions(store, request.projectId, request.input?.callId ?? "") &&
        reportProgress(store, request.projectId, null),
    ),
  UpdateSessionModel: (request, { store }) => {
    const { model, expectedRevision } = request;
    if (model === "AGENT_MODEL_UNSPECIFIED" || expectedRevision === undefined)
      throw notAdmitted(400, "Invalid model change");
    // Like Go, the change is taken in any Agent status; the next turn starts on the new model.
    return command(store, request, () => {
      const description = store.agents.get(request.projectId)?.description;
      if (!description) return false;
      const revision = BigInt(description.modelRevision);
      const expected = BigInt(expectedRevision);
      // A retry of the change that already happened is acknowledged again.
      if (revision === expected + BigInt(1) && description.model === model) return true;
      if (revision !== expected) return false;
      description.model = model;
      description.modelRevision = String(revision + BigInt(1));
      return true;
    });
  },
  DeleteQueuedMessage: (request, { store }) =>
    command(store, request, () => deleteQueuedMessage(store, request.projectId, request.messageId)),
  SteerQueuedMessage: (request, { store }) =>
    command(store, request, () => steerQueuedMessage(store, request.projectId, request.messageId)),
  ResolveToolApproval: (request, { store }) => {
    const { isApproved } = request;
    if (isApproved === undefined) throw notAdmitted(400, "isApproved is required");
    return command(store, request, () => approveTool(store, request.projectId, request.callId, isApproved));
  },
  // Like Go, a Stop names the turn the person saw; a stale one or one with nothing running is refused.
  InterruptAgent: (request, { store }) =>
    command(store, request, () => interruptAgent(store, request.projectId, Number(request.turn))),
  // Mock mode is a local environment: every account may select the mock model and run its scenarios.
  ListMockScenarios: () => ({ selectable: true, scenarios: MOCK_SCENARIOS }),
  // Like Go, a slot without a design is NOT_FOUND, and another digest than the slot's is REVISION_CHANGED.
  GetFlowDesign: (request, { store }) => {
    projectSession(store, request.projectId);
    const description = store.agents.get(request.projectId)?.description;
    const reference =
      request.slot === "AGENT_FLOW_DESIGN_SLOT_CONFIRMED" ? description?.confirmedRequirements?.spec?.design : null;
    if (!reference) throw httpError(404, "the Session holds no flow design in this slot");
    if (reference.digest !== request.digest)
      throw rpcFailure(409, "the Session's flow design changed", REVISION_CHANGED);
    return { design: MOCK_FLOW_DESIGN };
  },
};

// Only Studio development state: no token, redeem endpoint, executor or provider
// binding exists. Weak store keys release the bounded rows with their mock store.
type DialogConnection = { expires: number; row: RemoteAgentConnection };
const dialogConnections = new WeakMap<MockStore, Map<string, Map<string, DialogConnection>>>();

function remoteConnections(store: MockStore, projectId: string, caller: CallerContext__Output | null) {
  const account = mockAccount(caller);
  if (store.projects.get(projectId)?.state !== "ACTIVE") {
    dialogConnections.get(store)?.delete(projectId);
    throw httpError(404, "Project not active");
  }
  if (projectSession(store, projectId).state !== "ACTIVE") throw httpError(404, "Session not active");
  if (requireMockProjectRole(mockTenancy(store, account), account, projectId) === "OPERATOR")
    throw httpError(403, "Builder capability required");
  let projects = dialogConnections.get(store);
  if (!projects) dialogConnections.set(store, (projects = new Map()));
  let rows = projects.get(projectId);
  if (!rows) projects.set(projectId, (rows = new Map()));
  for (const [id, connection] of rows) if (connection.expires <= Date.now()) rows.delete(id);
  return { account, rows };
}

/** Functional dialog fixtures through generated codecs, not a Bridge protocol substitute. */
export const remoteAgentService: MockService<"RemoteAgentService"> = {
  SetSessionExecutor: ({ projectId, caller }, { store }) => {
    remoteConnections(store, projectId, caller);
    throw httpError(503, "Executor switching is unavailable in Studio mock mode");
  },
  CreateRemoteAgentPairing: ({ projectId, caller }, { store }) => {
    const { account, rows } = remoteConnections(store, projectId, caller);
    if (rows.size >= 6)
      throw rpcFailure(429, "Remove an unused connection before creating another", {
        reason: "REMOTE_AGENT_CONNECTIONS_FULL",
        domain: "superverse.v2",
        metadata: {},
      });
    const connectionId = randomBytes(16).toString("hex");
    const expires = Date.now() + 600_000;
    rows.set(connectionId, {
      expires,
      row: {
        connectionId,
        owner: { accountId: account.accountId, email: account.email },
        status: "REMOTE_AGENT_CONNECTION_STATUS_PAIRING",
        createdAt: isoToTimestamp(new Date().toISOString()),
      },
    });
    return {
      connectionId,
      code: Array.from(randomBytes(10), (value) => "0123456789ABCDEFGHJKMNPQRSTVWXYZ"[value & 31]).join(""),
      expiresAt: isoToTimestamp(new Date(expires).toISOString()),
      endpoint: "http://remote-agent.invalid",
      basisEpoch: "0",
      currentConnectionId: "",
    };
  },
  ListRemoteAgentConnections: ({ projectId, caller }, { store }) => {
    const { rows } = remoteConnections(store, projectId, caller);
    return {
      connections: [...rows.values()].map(({ row }) => row),
      epoch: "0",
      currentConnectionId: "",
      endpoint: "http://remote-agent.invalid",
    };
  },
  RevokeRemoteAgentConnection: ({ projectId, connectionId, caller }, { store }) => {
    const { rows } = remoteConnections(store, projectId, caller);
    if (!rows.delete(connectionId)) throw httpError(404, "Connection not found");
    return {};
  },
};
